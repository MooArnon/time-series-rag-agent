package exchange

import (
	"context"
	"log/slog"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

const wsStaleTimeout = 2 * time.Minute

type CandleHandler func(candle WsCandle)

// StartKlineWebsocket connects and blocks until ctx is cancelled, reconnecting on any stream drop.
// A watchdog goroutine forces a reconnect if no WS frames arrive for wsStaleTimeout (handles
// the case where the TCP connection is alive but the ISP silently drops data frames).
func StartKlineWebsocket(ctx context.Context, symbol string, interval string, logger *slog.Logger, handler CandleHandler) {
	backoff := 3 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		doneCh, stopCh, err := futures.WsKlineServe(symbol, interval, WsHandler(handler), ErrHandler())
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
		lastEvent.Store(time.Now().Unix()) // seed so watchdog doesn't fire immediately
		logger.Info("[WS] connected", "symbol", symbol, "interval", interval)

		// Watchdog: if no frame arrives for wsStaleTimeout, close the stream so the
		// outer loop reconnects. This handles silent data-frame drops at network level.
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if time.Since(time.Unix(lastEvent.Load(), 0)) > wsStaleTimeout {
						logger.Warn("[WS] no frames for 2 min — forcing reconnect")
						stopCh <- struct{}{}
						return
					}
				case <-doneCh:
					return
				case <-ctx.Done():
					return
				}
			}
		}()

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
