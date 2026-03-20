package handlers

import (
	"fmt"
	"math"
	"net/http"
	"syncpay/database"
	"syncpay/middleware"
	"syncpay/models"
	"syncpay/services"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListDebts(c *gin.Context) {
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

	var debts []models.Debt
	database.DB.Where("group_id = ?", c.Param("groupId")).Find(&debts)

	result := make([]gin.H, len(debts))
	for i, d := range debts {
		result[i] = gin.H{
			"id":        d.ID,
			"group":     d.GroupID,
			"from_user": d.FromUser,
			"to_user":   d.ToUser,
			"amount":    d.Amount,
		}
	}
	c.JSON(http.StatusOK, result)
}

func ListOptimisedDebts(c *gin.Context) {
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

	var debts []models.OptimisedDebt
	database.DB.Where("group_id = ?", c.Param("groupId")).Find(&debts)

	result := make([]gin.H, len(debts))
	for i, d := range debts {
		result[i] = gin.H{
			"id":        d.ID,
			"group":     d.GroupID,
			"from_user": d.FromUser,
			"to_user":   d.ToUser,
			"amount":    d.Amount,
		}
	}
	c.JSON(http.StatusOK, result)
}

func GetDebt(c *gin.Context) {
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

	var debt models.Debt
	if err := database.DB.Where("group_id = ? AND from_user = ? AND to_user = ?",
		c.Param("groupId"), c.Param("fromUser"), c.Param("toUser")).First(&debt).Error; err != nil {
		c.JSON(http.StatusOK, nil)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":        debt.ID,
		"group":     debt.GroupID,
		"from_user": debt.FromUser,
		"to_user":   debt.ToUser,
		"amount":    debt.Amount,
	})
}

func DeleteDebt(c *gin.Context) {
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

	fromUser := c.Param("fromUser")
	toUser := c.Param("toUser")

	database.DB.Where("group_id = ? AND from_user = ? AND to_user = ?", group.ID, fromUser, toUser).
		Delete(&models.Debt{})

	services.SimplifyDebts(group.ID)

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Debt from '%s' to '%s' deleted successfully.", fromUser, toUser)})
}

func AddDebt(c *gin.Context) {
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

	var req struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount int    `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	services.ProcessNewDebt(group.ID, req.From, req.To, req.Amount)
	services.SimplifyDebts(group.ID)

	c.JSON(http.StatusCreated, gin.H{"message": "Debt added successfully."})
}

func SettleDebt(c *gin.Context) {
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

	fromUser := fmt.Sprintf("%v", body["from"])
	toUser := fmt.Sprintf("%v", body["to"])

	// Parse amount - can be string (rupees) or number (cents)
	var amountCents int
	switch v := body["amount"].(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid amount."})
			return
		}
		amountCents = int(math.Round(f * 100))
	case float64:
		amountCents = int(v)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid amount."})
		return
	}

	if amountCents <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount must be greater than 0."})
		return
	}

	services.ReverseDebt(group.ID, fromUser, toUser, amountCents)
	services.SimplifyDebts(group.ID)

	// Log activity
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "settlement",
		Description: fmt.Sprintf("%s paid %s %.2f", fromUser, toUser, float64(amountCents)/100),
	})

	c.JSON(http.StatusOK, gin.H{"message": "Settlement recorded successfully."})
}
