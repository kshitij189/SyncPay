package database

import (
	"fmt"
	"log"
	"syncpay/config"
	"syncpay/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func Connect() {
	cfg := config.AppConfig

	// Connect to the database
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		cfg.DatabaseHost, cfg.DatabaseUser, cfg.DatabasePassword, cfg.DatabaseName, cfg.DatabasePort)
	
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	migrate()
}

func migrate() {
	err := DB.AutoMigrate(
		&models.User{},
		&models.Group{},
		&models.UserDebt{},
		&models.Debt{},
		&models.OptimisedDebt{},
		&models.Expense{},
		&models.ExpenseLender{},
		&models.ExpenseBorrower{},
		&models.ExpenseComment{},
		&models.ActivityLog{},
		&models.BlacklistedToken{},
	)
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Add unique constraints (ignore error if already exists)
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_user_debt_group_user ON user_debts(group_id, username)")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_debt_group_from_to ON debts(group_id, from_user, to_user)")
}
