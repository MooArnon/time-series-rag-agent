package exchange

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type CandleHandler func(candle WsCandle)

// StartKlineWebsocket polls the REST futures API for closed candles and calls handler
// each time a new candle closes. This replaces the WS approach because
// fstream.binance.com silently drops WS frames for certain IPs/regions while
// the REST endpoint on the same host remains fully accessible.
func StartKlineWebsocket(ctx context.Context, adapter KlineService, symbol string, interval string, logger *slog.Logger, handler CandleHandler) {
	duration, err := parseIntervalDuration(interval)
	if err != nil {
		logger.Error("[Poll] unsupported interval", "interval", interval, "err", err)
		return
	}

	pollEvery := duration / 4
	if pollEvery < 30*time.Second {
		pollEvery = 30 * time.Second
	}
	if pollEvery > 2*time.Minute {
		pollEvery = 2 * time.Minute
	}

	logger.Info("[Poll] starting REST candle poll", "symbol", symbol, "interval", interval, "poll_every", pollEvery)

	var lastCandleTime int64

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			candles, err := FetchLatestCandles(ctx, adapter, symbol, interval, 2)
			if err != nil {
				logger.Error("[Poll] fetch failed", "err", err)
				continue
			}
			if len(candles) == 0 {
				continue
			}

			latest := candles[len(candles)-1]

			if lastCandleTime == 0 {
				// seed on first poll — don't fire, just remember where we are
				lastCandleTime = latest.Time
				logger.Info("[Poll] seeded", "symbol", symbol, "candle_time", latest.Time)
				continue
			}

			if latest.Time == lastCandleTime {
				continue
			}

			lastCandleTime = latest.Time
			logger.Info("[Poll] new closed candle", "symbol", symbol, "time", latest.Time, "close", latest.Close)
			handler(WsCandle{
				Time:   latest.Time,
				Open:   latest.Open,
				High:   latest.High,
				Low:    latest.Low,
				Close:  latest.Close,
				Volume: latest.Volume,
			})

		case <-ctx.Done():
			return
		}
	}
}

func parseIntervalDuration(s string) (time.Duration, error) {
	r := strings.NewReplacer("1d", "24h", "2d", "48h", "3d", "72h", "1w", "168h")
	return time.ParseDuration(r.Replace(s))
}
