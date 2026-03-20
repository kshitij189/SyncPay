package config

import (
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	SecretKey          string
	Debug              bool
	DatabaseName       string
	DatabaseUser       string
	DatabasePassword   string
	DatabaseHost       string
	DatabasePort       string
	CORSAllowedOrigins string
	GeminiAPIKey       string
	GoogleClientID     string
	DatabaseURL        string
	Port               string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
}

var AppConfig Config

func Load() {
	godotenv.Load()

	AppConfig = Config{
		SecretKey:          getEnv("SECRET_KEY", "default-secret-key"),
		Debug:              getEnv("DEBUG", "true") == "true",
		DatabaseName:       getEnv("DATABASE_NAME", "syncpay"),
		DatabaseUser:       getEnv("DATABASE_USER", "root"),
		DatabasePassword:   getEnv("DATABASE_PASSWORD", ""),
		DatabaseHost:       getEnv("DATABASE_HOST", "localhost"),
		DatabasePort:       getEnv("DATABASE_PORT", "3306"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		Port:               getEnv("PORT", "8000"),
		AccessTokenTTL:     30 * time.Minute,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
