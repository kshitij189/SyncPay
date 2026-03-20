package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"syncpay/config"
)

type geminiRequest struct {
	Contents         []geminiContent       `json:"contents"`
	SystemInstruction *geminiContent       `json:"systemInstruction,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func GetBotResponse(userQuery string, balances map[string]int, recentExpenses []map[string]interface{}) (string, error) {
	apiKey := config.AppConfig.GeminiAPIKey
	if apiKey == "" {
		return "SplitBot is not configured. Please set GEMINI_API_KEY.", nil
	}

	systemPrompt := fmt.Sprintf(`You are 'SplitBot', a helpful and concise financial assistant for SyncPay.
Your job is to answer questions about the group's expenses and debts.

Context provided:
- Members & Balances: %v
- Recent Expenses: %v

Knowledge about Smart Settlement:
- Uses Greedy Transaction Minimization Algorithm
- Complexity: O(n log n)
- Goal: Minimize total transactions needed
- Example: Instead of A→B→C, suggests A→C directly

Rules:
1. Be concise and friendly
2. Use emojis
3. Negative balance = owes money
4. All amounts in cents (1000 = ₹10.00) — convert in responses
5. Always use usernames as provided
6. If answer not in context, say so politely`, balances, recentExpenses)

	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Parts: []geminiPart{{Text: userQuery}},
				Role:  "user",
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s", apiKey)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Sprintf("Oops! I encountered a technical glitch while thinking: %s", err.Error()), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Oops! I encountered a technical glitch while thinking: %s", err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Gemini API Error (Status %d): %s\n", resp.StatusCode, string(body))
		return fmt.Sprintf("Oops! Gemini API error (Status %d): %s", resp.StatusCode, string(body)), nil
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(body, &gemResp); err != nil {
		return fmt.Sprintf("Oops! I encountered a technical glitch while thinking: %s", err.Error()), nil
	}

	if len(gemResp.Candidates) > 0 && len(gemResp.Candidates[0].Content.Parts) > 0 {
		return gemResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "Oops! I couldn't generate a response. Please try again.", nil
}
