package handlers

import (
	"fmt"
	"net/http"
	"syncpay/database"
	"syncpay/middleware"
	"syncpay/models"
	"syncpay/services"
	"strings"

	"github.com/gin-gonic/gin"
)

func serializeExpense(e *models.Expense) gin.H {
	lenders := make([]gin.H, len(e.Lenders))
	for i, l := range e.Lenders {
		lenders[i] = gin.H{"username": l.Username, "amount": l.Amount}
	}
	borrowers := make([]gin.H, len(e.Borrowers))
	for i, b := range e.Borrowers {
		borrowers[i] = gin.H{"username": b.Username, "amount": b.Amount}
	}
	comments := make([]gin.H, len(e.Comments))
	for i, c := range e.Comments {
		comments[i] = gin.H{
			"id":         c.ID,
			"expense":    c.ExpenseID,
			"author":     c.Username,
			"text":       c.Text,
			"created_at": c.CreatedAt,
		}
	}

	return gin.H{
		"id":         e.ID,
		"group":      e.GroupID,
		"title":      e.Title,
		"author":     e.Author,
		"lender":     e.Lender,
		"lenders":    lenders,
		"borrowers":  borrowers,
		"comments":   comments,
		"amount":     e.Amount,
		"created_at": e.CreatedAt,
	}
}


// parseLendersBorrowers handles both array and object formats
func parseLendersBorrowers(data interface{}) []services.LenderBorrower {
	var result []services.LenderBorrower

	items, ok := data.([]interface{})
	if !ok {
		return result
	}

	for _, item := range items {
		switch v := item.(type) {
		case []interface{}: // ["username", amount]
			if len(v) >= 2 {
				username := fmt.Sprintf("%v", v[0])
				amount := toInt(v[1])
				result = append(result, services.LenderBorrower{Username: strings.ToLower(username), Amount: amount})
			}
		case map[string]interface{}: // {"username": "...", "amount": ...}
			username := fmt.Sprintf("%v", v["username"])
			amount := toInt(v["amount"])
			result = append(result, services.LenderBorrower{Username: strings.ToLower(username), Amount: amount})
		}
	}
	return result
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func ListExpenses(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	_, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var expenses []models.Expense
	database.DB.Preload("Lenders").Preload("Borrowers").Preload("Comments").
		Where("group_id = ?", c.Param("groupId")).
		Order("created_at DESC").
		Find(&expenses)

	result := make([]gin.H, len(expenses))
	for i, e := range expenses {
		result[i] = serializeExpense(&e)
	}
	c.JSON(http.StatusOK, result)
}

func CreateExpense(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	group, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	title := fmt.Sprintf("%v", body["title"])
	amount := toInt(body["amount"])

	lenders := parseLendersBorrowers(body["lenders"])
	borrowers := parseLendersBorrowers(body["borrowers"])

	// Legacy single-lender fallback
	if len(lenders) == 0 {
		if lender, ok := body["lender"].(string); ok && lender != "" {
			lenders = []services.LenderBorrower{{Username: strings.ToLower(lender), Amount: amount}}
		}
	}

	// Validate sums
	lenderSum := 0
	for _, l := range lenders {
		lenderSum += l.Amount
	}
	borrowerSum := 0
	for _, b := range borrowers {
		borrowerSum += b.Amount
	}

	if lenderSum != amount || borrowerSum != amount {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Lender or borrower amounts do not add up to the total amount."})
		return
	}

	primaryLender := ""
	if len(lenders) > 0 {
		primaryLender = lenders[0].Username
	}

	expense := models.Expense{
		GroupID: group.ID,
		Title:   title,
		Author:  user.Username,
		Lender:  primaryLender,
		Amount:  amount,
	}
	database.DB.Create(&expense)

	// Create lender records
	for _, l := range lenders {
		database.DB.Create(&models.ExpenseLender{
			ExpenseID: expense.ID,
			Username:  l.Username,
			Amount:    l.Amount,
		})
	}

	// Create borrower records
	for _, b := range borrowers {
		database.DB.Create(&models.ExpenseBorrower{
			ExpenseID: expense.ID,
			Username:  b.Username,
			Amount:    b.Amount,
		})
	}

	// Process debts
	services.ProcessMultiPayerDebt(group.ID, lenders, borrowers, amount)
	services.SimplifyDebts(group.ID)

	// Log activity
	desc := fmt.Sprintf("Added expense '%s' for %.2f", title, float64(amount)/100)
	if len(lenders) > 1 {
		desc += " (multi-payer)"
	}
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "expense_added",
		Description: desc,
	})

	// Reload and return
	database.DB.Preload("Lenders").Preload("Borrowers").Preload("Comments").First(&expense, expense.ID)
	c.JSON(http.StatusCreated, serializeExpense(&expense))
}

func GetExpense(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	_, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var expense models.Expense
	if err := database.DB.Preload("Lenders").Preload("Borrowers").Preload("Comments").
		Where("group_id = ? AND id = ?", c.Param("groupId"), c.Param("expenseId")).
		First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Expense not found"})
		return
	}

	c.JSON(http.StatusOK, serializeExpense(&expense))
}

func DeleteExpense(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	group, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var expense models.Expense
	if err := database.DB.Preload("Lenders").Preload("Borrowers").
		Where("group_id = ? AND id = ?", group.ID, c.Param("expenseId")).
		First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Expense not found"})
		return
	}

	// Get existing lenders/borrowers
	var lenders []services.LenderBorrower
	for _, l := range expense.Lenders {
		lenders = append(lenders, services.LenderBorrower{Username: l.Username, Amount: l.Amount})
	}
	var borrowers []services.LenderBorrower
	for _, b := range expense.Borrowers {
		borrowers = append(borrowers, services.LenderBorrower{Username: b.Username, Amount: b.Amount})
	}

	// Reverse debts
	services.ReverseMultiPayerDebt(group.ID, lenders, borrowers, expense.Amount)

	// Delete expense (cascades to lenders, borrowers, comments)
	database.DB.Select("Lenders", "Borrowers", "Comments").Delete(&expense)

	services.SimplifyDebts(group.ID)

	// Log
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "expense_deleted",
		Description: fmt.Sprintf("Deleted expense '%s'", expense.Title),
	})

	c.JSON(http.StatusOK, gin.H{"message": "Expense deleted successfully."})
}

func UpdateExpense(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	group, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var expense models.Expense
	if err := database.DB.Preload("Lenders").Preload("Borrowers").
		Where("group_id = ? AND id = ?", group.ID, c.Param("expenseId")).
		First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Expense not found"})
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Get old lenders/borrowers
	var oldLenders []services.LenderBorrower
	for _, l := range expense.Lenders {
		oldLenders = append(oldLenders, services.LenderBorrower{Username: l.Username, Amount: l.Amount})
	}
	var oldBorrowers []services.LenderBorrower
	for _, b := range expense.Borrowers {
		oldBorrowers = append(oldBorrowers, services.LenderBorrower{Username: b.Username, Amount: b.Amount})
	}

	// Reverse old debts
	services.ReverseMultiPayerDebt(group.ID, oldLenders, oldBorrowers, expense.Amount)

	// Delete old records
	database.DB.Where("expense_id = ?", expense.ID).Delete(&models.ExpenseLender{})
	database.DB.Where("expense_id = ?", expense.ID).Delete(&models.ExpenseBorrower{})

	// Parse new data
	title := fmt.Sprintf("%v", body["title"])
	amount := toInt(body["amount"])
	newLenders := parseLendersBorrowers(body["lenders"])
	newBorrowers := parseLendersBorrowers(body["borrowers"])

	if len(newLenders) == 0 {
		if lender, ok := body["lender"].(string); ok && lender != "" {
			newLenders = []services.LenderBorrower{{Username: strings.ToLower(lender), Amount: amount}}
		}
	}

	// Validate
	lenderSum := 0
	for _, l := range newLenders {
		lenderSum += l.Amount
	}
	borrowerSum := 0
	for _, b := range newBorrowers {
		borrowerSum += b.Amount
	}
	if lenderSum != amount || borrowerSum != amount {
		// Revert: re-apply old debts
		services.ProcessMultiPayerDebt(group.ID, oldLenders, oldBorrowers, expense.Amount)
		services.SimplifyDebts(group.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Lender or borrower amounts do not add up to the total amount."})
		return
	}

	// Update expense
	primaryLender := ""
	if len(newLenders) > 0 {
		primaryLender = newLenders[0].Username
	}
	database.DB.Model(&expense).Updates(map[string]interface{}{
		"title":  title,
		"amount": amount,
		"lender": primaryLender,
	})

	// Create new records
	for _, l := range newLenders {
		database.DB.Create(&models.ExpenseLender{ExpenseID: expense.ID, Username: l.Username, Amount: l.Amount})
	}
	for _, b := range newBorrowers {
		database.DB.Create(&models.ExpenseBorrower{ExpenseID: expense.ID, Username: b.Username, Amount: b.Amount})
	}

	// Process new debts
	services.ProcessMultiPayerDebt(group.ID, newLenders, newBorrowers, amount)
	services.SimplifyDebts(group.ID)

	// Log
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "expense_edited",
		Description: fmt.Sprintf("Edited expense '%s'", title),
	})

	database.DB.Preload("Lenders").Preload("Borrowers").Preload("Comments").First(&expense, expense.ID)
	c.JSON(http.StatusOK, serializeExpense(&expense))
}

func ListComments(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	_, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var comments []models.ExpenseComment
	database.DB.Where("expense_id = ?", c.Param("expenseId")).
		Order("created_at ASC").
		Find(&comments)

	result := make([]gin.H, len(comments))
	for i, cm := range comments {
		result[i] = gin.H{
			"id":         cm.ID,
			"expense":    cm.ExpenseID,
			"author":     cm.Username,
			"text":       cm.Text,
			"created_at": cm.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, result)
}

func CreateComment(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	group, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var expense models.Expense
	if err := database.DB.Where("group_id = ? AND id = ?", group.ID, c.Param("expenseId")).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Expense not found"})
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Text is required"})
		return
	}

	// Create user comment
	comment := models.ExpenseComment{
		ExpenseID: expense.ID,
		Username:  user.Username,
		Text:      req.Text,
	}
	database.DB.Create(&comment)

	// Check for @SplitBot trigger
	if strings.HasPrefix(req.Text, "@SplitBot") {
		botQuery := strings.TrimPrefix(req.Text, "@SplitBot")
		botQuery = strings.TrimSpace(botQuery)

		// Gather context
		var userDebts []models.UserDebt
		database.DB.Where("group_id = ?", group.ID).Find(&userDebts)
		balances := make(map[string]int)
		for _, ud := range userDebts {
			balances[ud.Username] = ud.NetDebt
		}

		var expenses []models.Expense
		database.DB.Where("group_id = ?", group.ID).Order("created_at DESC").Limit(100).Find(&expenses)
		recentExpenses := make([]map[string]interface{}, len(expenses))
		for i, e := range expenses {
			recentExpenses[i] = map[string]interface{}{
				"title": e.Title, "amount": e.Amount, "lender": e.Lender, "date": e.CreatedAt.Format("2006-01-02"),
			}
		}

		reply, _ := services.GetBotResponse(botQuery, balances, recentExpenses)
		botComment := models.ExpenseComment{
			ExpenseID: expense.ID,
			Username:  "SplitBot",
			Text:      reply,
		}
		database.DB.Create(&botComment)
	}

	// Log activity
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "expense_edited",
		Description: fmt.Sprintf("Commented on '%s'", expense.Title),
	})

	// Return all comments
	var allComments []models.ExpenseComment
	database.DB.Where("expense_id = ?", expense.ID).Order("created_at ASC").Find(&allComments)

	result := make([]gin.H, len(allComments))
	for i, cm := range allComments {
		result[i] = gin.H{
			"id":         cm.ID,
			"expense":    cm.ExpenseID,
			"author":     cm.Username,
			"text":       cm.Text,
			"created_at": cm.CreatedAt,
		}
	}
	c.JSON(http.StatusCreated, result)
}

func DeleteComment(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	group, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var expense models.Expense
	if err := database.DB.Where("group_id = ? AND id = ?", group.ID, c.Param("expenseId")).First(&expense).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Expense not found"})
		return
	}

	var comment models.ExpenseComment
	if err := database.DB.Where("id = ? AND expense_id = ?", c.Param("commentId"), expense.ID).First(&comment).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Comment not found"})
		return
	}

	if comment.Username != user.Username {
		c.JSON(http.StatusForbidden, gin.H{"error": "You can only delete your own comments."})
		return
	}

	// Log before deletion
	truncatedText := comment.Text
	if len(truncatedText) > 20 {
		truncatedText = truncatedText[:20] + "..."
	}
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "comment_deleted",
		Description: fmt.Sprintf("Deleted comment: '%s' on '%s'", truncatedText, expense.Title),
	})

	database.DB.Delete(&comment)

	// Return remaining comments
	var allComments []models.ExpenseComment
	database.DB.Where("expense_id = ?", expense.ID).Order("created_at ASC").Find(&allComments)

	result := make([]gin.H, len(allComments))
	for i, cm := range allComments {
		result[i] = gin.H{
			"id":         cm.ID,
			"expense":    cm.ExpenseID,
			"author":     cm.Username,
			"text":       cm.Text,
			"created_at": cm.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, result)
}

func CreateSettlement(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	group, err := loadGroup(c.Param("groupId"), user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	title := fmt.Sprintf("%v", body["title"])
	amount := toInt(body["amount"])
	lender := ""
	if l, ok := body["lender"].(string); ok {
		lender = strings.ToLower(l)
	}
	borrowers := parseLendersBorrowers(body["borrowers"])

	expense := models.Expense{
		GroupID: group.ID,
		Title:   title,
		Author:  user.Username,
		Lender:  lender,
		Amount:  amount,
	}
	database.DB.Create(&expense)

	for _, b := range borrowers {
		database.DB.Create(&models.ExpenseBorrower{
			ExpenseID: expense.ID,
			Username:  b.Username,
			Amount:    b.Amount,
		})
	}

	// Log settlement activity
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "settlement",
		Description: title,
	})

	database.DB.Preload("Lenders").Preload("Borrowers").Preload("Comments").First(&expense, expense.ID)
	c.JSON(http.StatusCreated, serializeExpense(&expense))
}
