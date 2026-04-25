package exchange

import (
	"context"
	"log/slog"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

type CandleHandler func(candle WsCandle)

// StartKlineWebsocket connects and blocks until ctx is cancelled, reconnecting on any stream drop.
func StartKlineWebsocket(ctx context.Context, symbol string, interval string, logger *slog.Logger, handler CandleHandler) {
	backoff := 3 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		doneCh, _, err := futures.WsKlineServe(symbol, interval, WsHandler(handler), ErrHandler())
		if err != nil {
			logger.Error("[WS] connect failed, retrying", "err", err, "backoff", backoff)
			select {
			case <-time.After(backoff):
				if backoff < 60*time.Second {
					backoff *= 2
				}
				continue
			case <-ctx.Done():
				return
			}
		}

		backoff = 3 * time.Second
		logger.Info("[WS] connected", "symbol", symbol, "interval", interval)

		select {
		case <-doneCh:
			logger.Warn("[WS] stream dropped, reconnecting in 3s")
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
