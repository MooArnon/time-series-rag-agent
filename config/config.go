package config

import (
	"os"
	"strconv"
)

type AppConfig struct {
	Market     BinanceMarketConfig
	Database   DatabaseConfig
	OpenRouter OpenRouterConfig
}

type BinanceMarketConfig struct {
	ApiKey    string
	ApiSecret string
}

type OpenRouterConfig struct {
	ApiKey string
}

type DatabaseConfig struct {
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
}

func LoadConfig() *AppConfig {
	return &AppConfig{
		Market: BinanceMarketConfig{
			ApiKey:    getEnv("BINANCE_API_KEY", ""),
			ApiSecret: getEnv("BINANCE_API_SECRET", ""),
		},
		Database: DatabaseConfig{
			DBHost:     getEnv("DB_HOST", ""),
			DBPort:     getEnvAsInt("DB_PORT", 5432),
			DBUser:     getEnv("DB_USER", ""),
			DBPassword: getEnv("DB_PASSWORD", ""),
			DBName:     getEnv("DB_NAME", ""),
		},
		OpenRouter: OpenRouterConfig{
			ApiKey: getEnv("OPENAI_API_KEY", ""),
		},
	}
}

func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if valueStr, exists := os.LookupEnv(key); exists {
		if value, err := strconv.Atoi(valueStr); err == nil {
			return value
		}
	}
	return fallback
}
