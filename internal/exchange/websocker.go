package exchange

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/adshao/go-binance/v2/futures"
)

type CandleHandler func(candle WsCandle)

// --- Websocket ---
func StartKlineWebsocket(ctx context.Context, symbol string, interval string, logger *slog.Logger, handler CandleHandler) error {
	doneCh, _, err := futures.WsKlineServe(
		symbol,
		interval,
		WsHandler(handler),
		ErrHandler(),
	)
	if err != nil {
		logger.Info(fmt.Sprintf("[] Error parsing kline to candle: %v\n", err))
		return err
	}

	select {
	case <-doneCh:
		// Binance ตัด connection เอง → reconnect
		return fmt.Errorf("stream closed unexpectedly")
	case <-ctx.Done():
		// เราสั่งหยุดเอง → shutdown gracefully
		return nil
	}
}
