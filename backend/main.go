package main

import (
	"fmt"
	"syncpay/config"
	"syncpay/database"
	"syncpay/handlers"
	"syncpay/middleware"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	config.Load()

	if !config.AppConfig.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	database.Connect()

	r := gin.Default()

	// CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{config.AppConfig.CORSAllowedOrigins},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Accept", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * 3600,
	}))

	// Fallback for dev if needed
	if config.AppConfig.CORSAllowedOrigins == "" || config.AppConfig.CORSAllowedOrigins == "*" {
		r.Use(cors.Default())
	}

	// ---- Auth routes (public) ----
	auth := r.Group("/auth")
	{
		auth.POST("/signup", handlers.Signup)
		auth.POST("/login", handlers.Login)
		auth.POST("/google", handlers.GoogleAuth)
		auth.POST("/token/refresh", handlers.TokenRefresh)
		auth.POST("/logout", middleware.AuthRequired(), handlers.Logout)
		auth.GET("/me", middleware.AuthRequired(), handlers.Me)
	}

	// ---- Invite routes ----
	r.GET("/invite/:inviteCode", handlers.GetInvite)
	r.POST("/invite/:inviteCode/claim", middleware.AuthRequired(), handlers.ClaimInvite)

	// ---- Group routes (auth required) ----
	groups := r.Group("/groups", middleware.AuthRequired())
	{
		groups.GET("", handlers.ListGroups)
		groups.POST("", handlers.CreateGroup)
		groups.GET("/:groupId", handlers.GetGroup)
		groups.DELETE("/:groupId", handlers.DeleteGroup)
		groups.POST("/:groupId/members", handlers.AddMember)
		groups.GET("/:groupId/users", handlers.ListGroupUsers)
		groups.GET("/:groupId/activity", handlers.ListActivity)
		groups.POST("/:groupId/ai-chat", handlers.AIChat)

		// Expenses
		groups.GET("/:groupId/expenses", handlers.ListExpenses)
		groups.POST("/:groupId/expenses", handlers.CreateExpense)

		// Settlement expense (must be before :expenseId routes)
		groups.POST("/:groupId/expenses/settlement", handlers.CreateSettlement)

		groups.GET("/:groupId/expenses/:expenseId", handlers.GetExpense)
		groups.DELETE("/:groupId/expenses/:expenseId", handlers.DeleteExpense)
		groups.PUT("/:groupId/expenses/:expenseId", handlers.UpdateExpense)

		// Comments
		groups.GET("/:groupId/expenses/:expenseId/comments", handlers.ListComments)
		groups.POST("/:groupId/expenses/:expenseId/comments", handlers.CreateComment)
		groups.DELETE("/:groupId/expenses/:expenseId/comments/:commentId", handlers.DeleteComment)

		// Debts
		groups.GET("/:groupId/debts", handlers.ListDebts)
		groups.GET("/:groupId/optimisedDebts", handlers.ListOptimisedDebts)
		groups.GET("/:groupId/debts/:fromUser/:toUser", handlers.GetDebt)
		groups.DELETE("/:groupId/debts/:fromUser/:toUser", handlers.DeleteDebt)
		groups.POST("/:groupId/debts/add", handlers.AddDebt)
		groups.POST("/:groupId/debts/settle", handlers.SettleDebt)
	}

	port := config.AppConfig.Port
	fmt.Printf("SyncPay backend running on port %s\n", port)
	r.Run(":" + port)
}
