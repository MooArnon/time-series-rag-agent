package config

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type AppConfig struct {
	Market     BinanceMarketConfig
	Database   DatabaseConfig
	OpenRouter OpenRouterConfig
	Discord    DiscordConfig
	Agent      AgentConfig
}

type AgentConfig struct {
	AviableTradeRatio float64
	Leverage          int
	SLPercentage      float64
	TPPercentage      float64
}

type AwsSecretData struct {
	TRADING_BOT_DB_POSTGRESQL_HOST     string `json:"TRADING_BOT_DB_POSTGRESQL_HOST"`
	TRADING_BOT_DB_POSTGRESQL_PASSWORD string `json:"TRADING_BOT_DB_POSTGRESQL_PASSWORD"`
	BinanceApiKey                      string `json:"BINANCE_API_KEY"`
	BinanceApiSecret                   string `json:"BINANCE_SECRET_KEY"`
	OPENAI_API_KEY                     string `json:"OPENAI_API_KEY"`
}

type BinanceMarketConfig struct {
	ApiKey    string
	ApiSecret string
}

type DiscordConfig struct {
	DISCORD_ALERT_WEBHOOK_URL  string
	DISCORD_NOTIFY_WEBHOOK_URL string
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
	// 1. Initialize the base config with Env vars (fallbacks or non-secret values)
	cfg := &AppConfig{
		Market: BinanceMarketConfig{
			// These might be empty initially if they are only in AWS
			ApiKey:    getEnv("BINANCE_API_KEY", ""),
			ApiSecret: getEnv("BINANCE_API_SECRET", ""),
		},
		Database: DatabaseConfig{
			DBHost:     getEnv("DB_HOST", ""),
			DBPort:     getEnvAsInt("DB_PORT", 5432),
			DBUser:     getEnv("DB_USER", ""),
			DBPassword: getEnv("DB_PASSWORD", ""), // Will be overwritten
			DBName:     getEnv("DB_NAME", ""),
		},
		OpenRouter: OpenRouterConfig{
			ApiKey: getEnv("OPENAI_API_KEY", ""),
		},
		Discord: DiscordConfig{
			DISCORD_ALERT_WEBHOOK_URL:  getEnv("DISCORD_ALERT_WEBHOOK_URL", ""),
			DISCORD_NOTIFY_WEBHOOK_URL: getEnv("DISCORD_NOTIFY_WEBHOOK_URL", ""),
		},
		Agent: AgentConfig{
			AviableTradeRatio: getEnvAsFloat("AviableTradeRatio", 0.90),
			Leverage:          getEnvAsInt("Leverage", 3),
			SLPercentage:      getEnvAsFloat("SLPercentage", 0.03),
			TPPercentage:      getEnvAsFloat("TPPercentage", 0.7),
		},
	}

	// 2. Fetch Secrets from AWS to overwrite sensitive fields
	secretName := os.Getenv("AWS_SECRET_NAME")
	if secretName != "" {
		secrets := fetchAwsSecrets(secretName)

		// Overwrite fields if the secret value exists
		if secrets.TRADING_BOT_DB_POSTGRESQL_HOST != "" {
			cfg.Database.DBHost = secrets.TRADING_BOT_DB_POSTGRESQL_HOST
		}
		if secrets.TRADING_BOT_DB_POSTGRESQL_PASSWORD != "" {
			cfg.Database.DBPassword = secrets.TRADING_BOT_DB_POSTGRESQL_PASSWORD
		}
		if secrets.BinanceApiKey != "" {
			cfg.Market.ApiKey = secrets.BinanceApiKey
		}
		if secrets.BinanceApiSecret != "" {
			cfg.Market.ApiSecret = secrets.BinanceApiSecret
		}
		if secrets.OPENAI_API_KEY != "" {
			cfg.OpenRouter.ApiKey = secrets.OPENAI_API_KEY
		}
	} else {
		log.Println("Warning: AWS_SECRET_NAME not set. Using environment variables only.")
	}

	return cfg
}

func fetchAwsSecrets(secretName string) AwsSecretData {
	// Load the default AWS config (credentials, region from env/profile)
	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("Unable to load SDK config: %v", err)
	}

	// Create Secrets Manager client
	svc := secretsmanager.NewFromConfig(awsCfg)

	// Get the secret value
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := svc.GetSecretValue(context.TODO(), input)
	if err != nil {
		log.Fatalf("Failed to retrieve secret '%s': %v", secretName, err)
	}

	// Parse JSON
	var secretData AwsSecretData
	if result.SecretString != nil {
		err = json.Unmarshal([]byte(*result.SecretString), &secretData)
		if err != nil {
			log.Fatalf("Failed to unmarshal secret JSON: %v", err)
		}
	}

	return secretData
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

func getEnvAsFloat(key string, fallback float64) float64 {
	if valueStr, exists := os.LookupEnv(key); exists {
		if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
			return value
		}
	}
	return fallback
}
