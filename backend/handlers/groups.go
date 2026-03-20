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

func serializeGroup(group *models.Group) gin.H {
	members := make([]gin.H, len(group.Members))
	for i, m := range group.Members {
		members[i] = userResponse(&m)
	}
	return gin.H{
		"id":          group.ID,
		"name":        group.Name,
		"created_by":  userResponse(&group.CreatedBy),
		"members":     members,
		"created_at":  group.CreatedAt,
		"invite_code": group.InviteCode,
	}
}

func loadGroup(groupID string, userID uint) (*models.Group, error) {
	var group models.Group
	if err := database.DB.Preload("CreatedBy").Preload("Members").First(&group, groupID).Error; err != nil {
		return nil, fmt.Errorf("not found")
	}
	// Check membership
	for _, m := range group.Members {
		if m.ID == userID {
			return &group, nil
		}
	}
	return nil, fmt.Errorf("not a member")
}

func ListGroups(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var groups []models.Group
	database.DB.Preload("CreatedBy").Preload("Members").
		Joins("JOIN group_members ON group_members.group_id = groups.id").
		Where("group_members.user_id = ?", user.ID).
		Order("groups.created_at DESC").
		Find(&groups)

	result := make([]gin.H, len(groups))
	for i, g := range groups {
		result[i] = serializeGroup(&g)
	}

	c.JSON(http.StatusOK, result)
}

func CreateGroup(c *gin.Context) {
	user := middleware.GetCurrentUser(c)

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}

	group := models.Group{
		Name:        req.Name,
		CreatedByID: user.ID,
		Members:     []models.User{*user},
	}

	if err := database.DB.Create(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create group"})
		return
	}

	// Create UserDebt for creator
	database.DB.Create(&models.UserDebt{
		GroupID:  group.ID,
		Username: user.Username,
		NetDebt:  0,
	})

	// Log activity
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "group_created",
		Description: fmt.Sprintf("Created group '%s'", group.Name),
	})

	// Reload with associations
	database.DB.Preload("CreatedBy").Preload("Members").First(&group, group.ID)
	c.JSON(http.StatusCreated, serializeGroup(&group))
}

func GetGroup(c *gin.Context) {
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
	c.JSON(http.StatusOK, serializeGroup(group))
}

func DeleteGroup(c *gin.Context) {
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

	if group.CreatedByID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only the creator can delete the group"})
		return
	}

	database.DB.Select("Members").Delete(&group)
	c.Status(http.StatusNoContent)
}

func AddMember(c *gin.Context) {
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
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
		return
	}

	memberUsername := strings.TrimSpace(req.Username)

	// Check if already a member (case-insensitive)
	for _, m := range group.Members {
		if strings.EqualFold(m.Username, memberUsername) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User is already a member"})
			return
		}
	}

	// Find or create user
	var memberUser models.User
	result := database.DB.Where("username = ?", memberUsername).First(&memberUser)
	if result.Error != nil {
		// Also try case-insensitive
		result = database.DB.Where("LOWER(username) = ?", strings.ToLower(memberUsername)).First(&memberUser)
	}
	if result.Error != nil {
		// Create dummy user
		memberUser = models.User{
			Username:  memberUsername,
			FirstName: memberUsername,
			IsDummy:   true,
			Password:  "",
		}
		database.DB.Create(&memberUser)
	}

	// Add to group
	database.DB.Model(&group).Association("Members").Append(&memberUser)

	// Create UserDebt
	database.DB.Create(&models.UserDebt{
		GroupID:  group.ID,
		Username: memberUser.Username,
		NetDebt:  0,
	})

	// Log activity
	database.DB.Create(&models.ActivityLog{
		GroupID:     group.ID,
		User:        user.Username,
		Action:      "member_added",
		Description: fmt.Sprintf("Added %s to the group", memberUser.Username),
	})

	// Reload and return
	database.DB.Preload("CreatedBy").Preload("Members").First(&group, group.ID)
	c.JSON(http.StatusOK, serializeGroup(group))
}

func ListGroupUsers(c *gin.Context) {
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

	members := make([]gin.H, len(group.Members))
	for i, m := range group.Members {
		members[i] = userResponse(&m)
	}
	c.JSON(http.StatusOK, members)
}

func ListActivity(c *gin.Context) {
	groupIDStr := c.Param("groupId")
	if groupIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Group ID is required"})
		return
	}

	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	_, err := loadGroup(groupIDStr, user.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	var activities []models.ActivityLog
	database.DB.Where("group_id = ?", c.Param("groupId")).
		Order("created_at DESC").
		Find(&activities)

	result := make([]gin.H, len(activities))
	for i, a := range activities {
		result[i] = gin.H{
			"id":          a.ID,
			"group":       a.GroupID,
			"user":        a.User,
			"action":      a.Action,
			"description": a.Description,
			"created_at":  a.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, result)
}

func AIChat(c *gin.Context) {
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

	var req struct {
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Message is required"})
		return
	}

	groupID := c.Param("groupId")

	// Gather context
	var userDebts []models.UserDebt
	database.DB.Where("group_id = ?", groupID).Find(&userDebts)

	balances := make(map[string]int)
	for _, ud := range userDebts {
		balances[ud.Username] = ud.NetDebt
	}

	var expenses []models.Expense
	database.DB.Where("group_id = ?", groupID).
		Order("created_at DESC").
		Limit(100).
		Find(&expenses)

	recentExpenses := make([]map[string]interface{}, len(expenses))
	for i, e := range expenses {
		recentExpenses[i] = map[string]interface{}{
			"title":  e.Title,
			"amount": e.Amount,
			"lender": e.Lender,
			"date":   e.CreatedAt.Format("2006-01-02"),
		}
	}

	reply, err := services.GetBotResponse(req.Message, balances, recentExpenses)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI service error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"reply": reply})
}
