package exchange

import (
	"context"

	"github.com/adshao/go-binance/v2/futures"
)

type BinanceAdapter struct {
	client *futures.Client
}

func NewBinanceAdapter(client *futures.Client) *BinanceAdapter {
	return &BinanceAdapter{client: client}
}

func (b *BinanceAdapter) FetchKlines(ctx context.Context, symbol, interval string, limit int) ([]*futures.Kline, error) {
	return b.client.NewKlinesService().
		Symbol(symbol).Interval(interval).Limit(limit).
		Do(ctx)
}
