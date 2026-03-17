package exchange

import (
	"context"

	"github.com/adshao/go-binance/v2/futures"
)

type KlineService interface {
	FetchKlines(ctx context.Context, symbol, interval string, limit int) ([]*futures.Kline, error)
}
