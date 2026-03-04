package exchange

import (
	"context"

	"github.com/adshao/go-binance/v2/futures"
)

type MockKlineService struct {
	ReturnData []*futures.Kline
	ReturnErr  error
}

func (m *MockKlineService) FetchKlines(ctx context.Context, symbol, interval string, limit int) ([]*futures.Kline, error) {
	return m.ReturnData, m.ReturnErr
}
