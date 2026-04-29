package exchange

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

type CandleHandler func(candle WsCandle)

// StartKlineWebsocket detects closed candles in real time by using the book-ticker
// WebSocket stream (which works) as a sub-second heartbeat. Each tick checks whether
// the wall clock has crossed a new interval boundary; when it has, the just-closed
// candle is fetched from the REST API (also works) and dispatched to handler.
//
// This replaces a direct kline WebSocket subscription because fstream.binance.com
// silently delivers no frames for the @kline stream while all other stream types
// (including @bookTicker) work normally.
func StartKlineWebsocket(ctx context.Context, adapter KlineService, symbol string, interval string, logger *slog.Logger, handler CandleHandler) {
	duration, err := parseIntervalDuration(interval)
	if err != nil {
		logger.Error("[Trigger] unsupported interval", "interval", interval, "err", err)
		return
	}
	intervalSecs := int64(duration.Seconds())

	var lastCandleTime atomic.Int64
	var fetching atomic.Bool

	// Seed lastCandleTime from REST so we don't re-fire the most recent closed candle on startup.
	if candles, err := FetchLatestCandles(ctx, adapter, symbol, interval, 2); err == nil && len(candles) > 0 {
		lastCandleTime.Store(candles[len(candles)-1].Time)
		logger.Info("[Trigger] seeded", "symbol", symbol, "candle_time", lastCandleTime.Load())
	}

	checkAndFire := func() {
		now := time.Now().Unix()
		currentBoundary := (now / intervalSecs) * intervalSecs

		if currentBoundary <= lastCandleTime.Load() {
			return
		}
		if !fetching.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer fetching.Store(false)
			// Brief pause so the REST server has the finalized candle available.
			time.Sleep(2 * time.Second)

			candles, err := FetchLatestCandles(ctx, adapter, symbol, interval, 2)
			if err != nil {
				logger.Error("[Trigger] REST fetch failed", "err", err)
				return
			}
			if len(candles) == 0 {
				return
			}
			latest := candles[len(candles)-1]
			if latest.Time <= lastCandleTime.Load() {
				return
			}
			lastCandleTime.Store(latest.Time)
			logger.Info("[Trigger] new closed candle", "symbol", symbol, "time", latest.Time, "close", latest.Close)
			handler(WsCandle{
				Time:   latest.Time,
				Open:   latest.Open,
				High:   latest.High,
				Low:    latest.Low,
				Close:  latest.Close,
				Volume: latest.Volume,
			})
		}()
	}

	connectBackoff := 3 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}

		doneCh, _, err := futures.WsBookTickerServe(
			strings.ToUpper(symbol),
			func(_ *futures.WsBookTickerEvent) { checkAndFire() },
			func(err error) { logger.Error("[Trigger] WS error", "err", err) },
		)
		if err != nil {
			logger.Error("[Trigger] connect failed, retrying", "err", err, "backoff", connectBackoff)
			select {
			case <-time.After(connectBackoff):
				if connectBackoff < 60*time.Second {
					connectBackoff *= 2
				}
				continue
			case <-ctx.Done():
				return
			}
		}

		connectBackoff = 3 * time.Second
		logger.Info("[Trigger] book-ticker WS connected", "symbol", symbol)

		select {
		case <-doneCh:
			logger.Warn("[Trigger] WS dropped, reconnecting in 3s")
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

func parseIntervalDuration(s string) (time.Duration, error) {
	r := strings.NewReplacer("1d", "24h", "2d", "48h", "3d", "72h", "1w", "168h")
	return time.ParseDuration(r.Replace(s))
}
