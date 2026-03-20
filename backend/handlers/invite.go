package handlers

import (
	"fmt"
	"net/http"
	"syncpay/database"
	"syncpay/middleware"
	"syncpay/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetInvite(c *gin.Context) {
	inviteCode := c.Param("inviteCode")

	var group models.Group
	if err := database.DB.Preload("Members").Where("invite_code = ?", inviteCode).First(&group).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid invite link"})
		return
	}

	members := make([]gin.H, len(group.Members))
	for i, m := range group.Members {
		members[i] = gin.H{
			"id":       m.ID,
			"username": m.Username,
			"is_dummy": m.IsDummy,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"group_id":    group.ID,
		"group_name":  group.Name,
		"invite_code": group.InviteCode,
		"members":     members,
	})
}

func ClaimInvite(c *gin.Context) {
	inviteCode := c.Param("inviteCode")
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var group models.Group
	if err := database.DB.Preload("Members").Where("invite_code = ?", inviteCode).First(&group).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid invite link"})
		return
	}

	var req struct {
		MemberID *uint `json:"member_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MemberID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "member_id is required"})
		return
	}

	// Find the member to be claimed
	var dummyUser models.User
	if err := database.DB.First(&dummyUser, *req.MemberID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Member not found"})
		return
	}

	// Check member is in the group
	isMember := false
	for _, m := range group.Members {
		if m.ID == dummyUser.ID {
			isMember = true
			break
		}
	}
	if !isMember {
		c.JSON(http.StatusNotFound, gin.H{"error": "Member not found"})
		return
	}

	// Check member is dummy
	if !dummyUser.IsDummy {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This member already has an account and cannot be claimed."})
		return
	}

	// Check current user is not already in the group
	for _, m := range group.Members {
		if m.ID == user.ID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You are already a member of this group"})
			return
		}
	}

	oldUsername := dummyUser.Username
	newUsername := user.Username

	// Start Transaction
	tx := database.DB.Begin()

	// Remove dummy from group, add current user
	if err := tx.Model(&group).Association("Members").Delete(&dummyUser); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update members"})
		return
	}
	if err := tx.Model(&group).Association("Members").Append(user); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update members"})
		return
	}

	// Update ALL username references across the group
	groupID := group.ID

	// UserDebt
	tx.Model(&models.UserDebt{}).
		Where("group_id = ? AND username = ?", groupID, oldUsername).
		Update("username", newUsername)

	// Debt from_user and to_user
	tx.Model(&models.Debt{}).
		Where("group_id = ? AND from_user = ?", groupID, oldUsername).
		Update("from_user", newUsername)
	tx.Model(&models.Debt{}).
		Where("group_id = ? AND to_user = ?", groupID, oldUsername).
		Update("to_user", newUsername)

	// OptimisedDebt
	tx.Model(&models.OptimisedDebt{}).
		Where("group_id = ? AND from_user = ?", groupID, oldUsername).
		Update("from_user", newUsername)
	tx.Model(&models.OptimisedDebt{}).
		Where("group_id = ? AND to_user = ?", groupID, oldUsername).
		Update("to_user", newUsername)

	// Expense author and lender
	tx.Model(&models.Expense{}).
		Where("group_id = ? AND author = ?", groupID, oldUsername).
		Update("author", newUsername)
	tx.Model(&models.Expense{}).
		Where("group_id = ? AND lender = ?", groupID, oldUsername).
		Update("lender", newUsername)

	// ExpenseLender - need to find expenses in this group
	var expenseIDs []uint
	tx.Model(&models.Expense{}).Where("group_id = ?", groupID).Pluck("id", &expenseIDs)

	if len(expenseIDs) > 0 {
		tx.Model(&models.ExpenseLender{}).
			Where("expense_id IN ? AND username = ?", expenseIDs, oldUsername).
			Update("username", newUsername)

		tx.Model(&models.ExpenseBorrower{}).
			Where("expense_id IN ? AND username = ?", expenseIDs, oldUsername).
			Update("username", newUsername)

		tx.Model(&models.ExpenseComment{}).
			Where("expense_id IN ? AND author = ?", expenseIDs, oldUsername).
			Update("author", newUsername)
	}

	// ActivityLog performer
	tx.Model(&models.ActivityLog{}).
		Where("group_id = ? AND user = ?", groupID, oldUsername).
		Update("user", newUsername)

	// Update names IN description strings (if name actually changed)
	if oldUsername != newUsername {
		tx.Model(&models.ActivityLog{}).
			Where("group_id = ?", groupID).
			Update("description", gorm.Expr("REPLACE(description, ?, ?)", oldUsername, newUsername))
	}

	// Delete dummy user if not in any other groups
	var otherGroupCount int64
	database.DB.Table("group_members").Where("user_id = ?", dummyUser.ID).Count(&otherGroupCount)
	if otherGroupCount == 0 {
		tx.Delete(&dummyUser)
	}

	// Log join activity
	joinLog := models.ActivityLog{
		GroupID:     groupID,
		User:        newUsername,
		Action:      "member_added",
		Description: fmt.Sprintf("%s joined the group via invite", newUsername),
	}
	if oldUsername != newUsername {
		joinLog.Description = fmt.Sprintf("%s claimed %s's spot via invite link", newUsername, oldUsername)
	}
	tx.Create(&joinLog)

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit join transaction"})
		return
	}

	fmt.Printf("[DEBUG] Successfully processed claim for group %d: %s -> %s\n", groupID, oldUsername, newUsername)

	// Reload group
	database.DB.Preload("CreatedBy").Preload("Members").First(&group, group.ID)

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Successfully joined as %s", newUsername),
		"group":   serializeGroup(&group),
	})
}
