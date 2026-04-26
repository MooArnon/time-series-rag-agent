package config

import (
	"context"
	"encoding/json"
	"fmt"
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
	Que        QueConfig
	Regime     RegimeConfig
	LLM        LLMConfig
}

type RegimeConfig struct {
	ADXTrendThreshold    float64
	ADXRangeThreshold    float64
	ATRVolatileThreshold float64
	BandWidthThreshold   float64
	BandWidthPeriod      int
}

type AgentConfig struct {
	AviableTradeRatio          float64
	Leverage                   int
	SLPercentage               float64
	TPPercentage               float64
	StopROI                    float64
	StopLossROI                float64
	ReduceRoiTrigger           float64
	ReductionAviableTradeRatio float64
}

type LLMConfig struct {
	NumPnLLookback      int
	TopN                int
	ConfidenceThreshold int
	LimitTradeHistory   int
	MaxDailyTokens      int
	PrefilterThreshold  float64 // minimum score (0-100) to proceed to LLM; 0 = use package default (35)
}

type QueConfig struct {
	QueUrl string
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
			AviableTradeRatio:          getEnvAsFloat("AVIABLE_TRADE_RATIO", 0.90),
			Leverage:                   getEnvAsInt("LEVERAGE", 5),
			SLPercentage:               getEnvAsFloat("SL_PERCENTAGE", 0.03),
			TPPercentage:               getEnvAsFloat("TP_PERCENTAGE", 0.7),
			StopROI:                    getEnvAsFloat("STOP_ROI", 15.0),
			StopLossROI:                getEnvAsFloat("STOP_LOSS_ROI", -5.0),
			ReduceRoiTrigger:           getEnvAsFloat("REDUCE_ROI_TRIGGER", 5.0),
			ReductionAviableTradeRatio: getEnvAsFloat("REDUCTION_AVIABLE_TRADE_RATIO", 0.70),
		},
		Que: QueConfig{
			QueUrl: getEnv("SQS_URL", ""),
		},
		Regime: RegimeConfig{
			ADXTrendThreshold:    getEnvAsFloat("ADX_TREND_THRESHOLD", 25.0),
			ADXRangeThreshold:    getEnvAsFloat("ADX_RANGE_THRESHOLD", 20.0),
			ATRVolatileThreshold: getEnvAsFloat("ATR_VOLATILE_THRESHOLD", 1.5),
			BandWidthThreshold:   getEnvAsFloat("BANDWIDTH_THRESHOLD", 0.025),
			BandWidthPeriod:      getEnvAsInt("BANDWIDTH_PERIOD", 30),
		},
		LLM: LLMConfig{
			NumPnLLookback:      getEnvAsInt("NUM_PNL_LOOKBACK", 5),
			TopN:                getEnvAsInt("TOPN_MATCHED", 30),
			ConfidenceThreshold: getEnvAsInt("CONFIDENCE_THRESHOLD", 30),
			LimitTradeHistory:   getEnvAsInt("LimitTradeHistory", 5),
			MaxDailyTokens:      getEnvAsInt("MAX_DAILY_TOKENS", 0),
			PrefilterThreshold:  getEnvAsFloat("PREFILTER_THRESHOLD", 35.0),
		},
	}

	// 2. Fetch Secrets from AWS to overwrite sensitive fields
	secretName := os.Getenv("AWS_SECRET_NAME")
	if secretName != "" {
		secrets, err := fetchAwsSecrets(secretName)
		if err != nil {
			log.Printf("Warning: could not fetch AWS secret '%s' (falling back to env vars): %v", secretName, err)
		} else {
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
		}
	} else {
		log.Println("Warning: AWS_SECRET_NAME not set. Using environment variables only.")
	}

	return cfg
}

func fetchAwsSecrets(secretName string) (AwsSecretData, error) {
	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return AwsSecretData{}, fmt.Errorf("load SDK config: %w", err)
	}

	svc := secretsmanager.NewFromConfig(awsCfg)
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := svc.GetSecretValue(context.TODO(), input)
	if err != nil {
		return AwsSecretData{}, fmt.Errorf("get secret value: %w", err)
	}

	var secretData AwsSecretData
	if result.SecretString != nil {
		if err = json.Unmarshal([]byte(*result.SecretString), &secretData); err != nil {
			return AwsSecretData{}, fmt.Errorf("unmarshal secret JSON: %w", err)
		}
	}

	return secretData, nil
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
