package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// Telegram
	TelegramBotToken string
	AdminIDs         []int64

	// Database
	DatabaseURL string
	DBHost      string
	DBPort      string
	DBUser      string
	DBPassword  string
	DBName      string

	// Shop
	ShopName         string
	ShopAddress      string
	SupportUserID    int64
	KaspiLink        string
	AboutChannelLink string

	// Environment
	Env string
}

// LoadConfig загружает конфиг из .env файла
func LoadConfig() (*Config, error) {
	// Игнорируем ошибку если файл не найден
	_ = godotenv.Load()

	cfg := &Config{
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		DBHost:           os.Getenv("DB_HOST"),
		DBPort:           os.Getenv("DB_PORT"),
		DBUser:           os.Getenv("DB_USER"),
		DBPassword:       os.Getenv("DB_PASSWORD"),
		DBName:           os.Getenv("DB_NAME"),
		ShopName:         os.Getenv("SHOP_NAME"),
		ShopAddress:      os.Getenv("SHOP_ADDRESS"),
		KaspiLink:        os.Getenv("KASPI_LINK"),
		AboutChannelLink: os.Getenv("ABOUT_CHANNEL_LINK"),
		Env:              os.Getenv("ENV"),
	}

	// Парсим AdminIDs
	adminIDsStr := os.Getenv("ADMIN_IDS")
	if adminIDsStr != "" {
		parts := strings.Split(adminIDsStr, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if id, err := strconv.ParseInt(part, 10, 64); err == nil {
				cfg.AdminIDs = append(cfg.AdminIDs, id)
			}
		}
	}

	// Парсим SupportUserID
	supportIDStr := os.Getenv("SUPPORT_USER_ID")
	if supportIDStr != "" {
		if id, err := strconv.ParseInt(supportIDStr, 10, 64); err == nil {
			cfg.SupportUserID = id
		}
	}

	return cfg, nil
}
