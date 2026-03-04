package config

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type AppConfig struct {
	Market BinanceMarketConfig
}
type AwsSecretData struct {
	TRADING_BOT_DB_POSTGRESQL_HOST     string `json:"TRADING_BOT_DB_POSTGRESQL_HOST"`
	TRADING_BOT_DB_POSTGRESQL_PASSWORD string `json:"TRADING_BOT_DB_POSTGRESQL_PASSWORD"`
	BinanceApiKey                      string `json:"BINANCE_API_KEY"`
	BinanceApiSecret                   string `json:"BINANCE_SECRET_KEY"`
	OPENAI_API_KEY                     string `json:"OPENAI_API_KEY"`
}

type BinanceMarketConfig struct {
	BINANCE_API_KEY    string
	BINANCE_API_SECRET string
}

func LoadConfig() AppConfig {
	cfg := AppConfig{
		Market: BinanceMarketConfig{
			BINANCE_API_KEY:    "",
			BINANCE_API_SECRET: "",
		},
	}

	// Load config from environment variables
	secretName := os.Getenv("AWS_SECRET_NAME")
	secrets := fetchAwsSecrets(secretName)
	if cfg.Market.BINANCE_API_KEY == "" || cfg.Market.BINANCE_API_SECRET == "" {
		cfg.Market.BINANCE_API_KEY = secrets.BinanceApiKey
		cfg.Market.BINANCE_API_SECRET = secrets.BinanceApiSecret
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
