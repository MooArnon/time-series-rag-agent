package main

import (
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/sqs"

	"fmt"
)

func main() {
	cfg := config.LoadConfig()
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser, cfg.Database.DBPassword, cfg.Database.DBHost, cfg.Database.DBPort, cfg.Database.DBName)

	sqs.ConsumeTradingLogs(connString)
}
