package exchange

import (
	"context"
	"fmt"
	"time"
	"time-series-rag-agent/config"

	"github.com/adshao/go-binance/v2/futures"
)

func NewBinanceClient(ctx context.Context, cfg *config.AppConfig) (*futures.Client, error) {
	client := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)

	serverTime, err := client.NewServerTimeService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch server time: %w", err)
	}

	client.TimeOffset = serverTime - time.Now().UnixMilli()

	return client, nil
}
