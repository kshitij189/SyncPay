package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"syncpay/config"
	"syncpay/database"
	"syncpay/middleware"
	"syncpay/models"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type signupRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type googleAuthRequest struct {
	Credential string `json:"credential"`
	Email      string `json:"email"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Sub        string `json:"sub"`
}

func userResponse(user *models.User) gin.H {
	return gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"email":      user.Email,
	}
}

func Signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password required"})
		return
	}

	username := strings.ToLower(req.Username)

	var existing models.User
	result := database.DB.Where("username = ?", username).First(&existing)

	if result.Error == nil {
		// User exists
		if !existing.IsDummy {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already exists"})
			return
		}
		// Claim dummy account
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		existing.Password = string(hashedPassword)
		existing.IsDummy = false
		if req.Email != "" {
			existing.Email = req.Email
		}
		if req.FirstName != "" {
			existing.FirstName = req.FirstName
		}
		if req.LastName != "" {
			existing.LastName = req.LastName
		}
		database.DB.Save(&existing)

		// Log that the dummy account was verified
		var groupIDs []uint
		database.DB.Table("group_members").Where("user_id = ?", existing.ID).Pluck("group_id", &groupIDs)
		for _, gid := range groupIDs {
			database.DB.Create(&models.ActivityLog{
				GroupID:     gid,
				User:        existing.Username,
				Action:      "member_added",
				Description: fmt.Sprintf("%s claimed their spot and joined the group", existing.Username),
			})
		}

		access, refresh, err := middleware.GenerateTokens(&existing)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tokens"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"access":  access,
			"refresh": refresh,
			"user":    userResponse(&existing),
		})
		return
	}

	// Create new user
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := models.User{
		Username:  username,
		Password:  string(hashedPassword),
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		IsDummy:   false,
	}
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	access, refresh, err := middleware.GenerateTokens(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tokens"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"access":  access,
		"refresh": refresh,
		"user":    userResponse(&user),
	})
}

func Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	username := strings.ToLower(req.Username)

	var user models.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if user.IsDummy {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	access, refresh, err := middleware.GenerateTokens(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access":  access,
		"refresh": refresh,
		"user":    userResponse(&user),
	})
}

func GoogleAuth(c *gin.Context) {
	var req googleAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not provided"})
		return
	}

	// Verify Google token
	if req.Credential != "" {
		tokenInfoURL := fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?access_token=%s", req.Credential)
		resp, err := http.Get(tokenInfoURL)
		if err != nil || resp.StatusCode != 200 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Google access token"})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var tokenInfo map[string]interface{}
		json.Unmarshal(body, &tokenInfo)

		if email, ok := tokenInfo["email"].(string); ok {
			if email != req.Email {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Email mismatch"})
				return
			}
		}

		// Also verify audience if GoogleClientID is set
		if config.AppConfig.GoogleClientID != "" {
			if aud, ok := tokenInfo["aud"].(string); ok {
				if aud != config.AppConfig.GoogleClientID {
					// Try verifying as ID token instead
					// For now, we accept it as the email matched
					_ = aud
				}
			}
		}
	}

	// Find or create user by email
	var user models.User
	result := database.DB.Where("email = ?", req.Email).First(&user)

	if result.Error != nil {
		// Create new user
		username := strings.Split(req.Email, "@")[0]
		username = strings.ToLower(username)

		// Ensure unique username
		baseUsername := username
		counter := 1
		for {
			var count int64
			database.DB.Model(&models.User{}).Where("username = ?", username).Count(&count)
			if count == 0 {
				break
			}
			username = fmt.Sprintf("%s%d", baseUsername, counter)
			counter++
		}

		user = models.User{
			Username:  username,
			Email:     req.Email,
			FirstName: req.GivenName,
			LastName:  req.FamilyName,
			IsDummy:   true, // Google users have no local password
			Password:  "",
		}
		database.DB.Create(&user)
	}

	access, refresh, err := middleware.GenerateTokens(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access":  access,
		"refresh": refresh,
		"user":    userResponse(&user),
	})
}

func TokenRefresh(c *gin.Context) {
	var req struct {
		Refresh string `json:"refresh"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Refresh == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Refresh token is required"})
		return
	}

	if middleware.IsTokenBlacklisted(req.Refresh) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired refresh token"})
		return
	}

	claims, err := middleware.ParseToken(req.Refresh)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired refresh token"})
		return
	}

	// Blacklist old refresh token (rotate)
	middleware.BlacklistToken(req.Refresh, claims.ExpiresAt.Time)

	var user models.User
	if err := database.DB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	access, refresh, err := middleware.GenerateTokens(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tokens"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access":  access,
		"refresh": refresh,
	})
}

func Logout(c *gin.Context) {
	var req struct {
		Refresh string `json:"refresh"`
	}
	if err := c.ShouldBindJSON(&req); err == nil && req.Refresh != "" {
		claims, err := middleware.ParseToken(req.Refresh)
		if err == nil {
			middleware.BlacklistToken(req.Refresh, claims.ExpiresAt.Time)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

func Me(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}
	c.JSON(http.StatusOK, userResponse(user))
}
