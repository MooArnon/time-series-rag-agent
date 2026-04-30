package exchange

import (
	"context"
	"log/slog"
	"strings"
	"sync"
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

// MultiSymbolCandleHandler receives one closed candle per symbol, keyed by symbol name.
type MultiSymbolCandleHandler func(candles map[string]WsCandle)

// StartMultiSymbolKlineWebsocket watches multiple symbols on the same interval.
// It uses the first symbol's book-ticker stream as a sub-second heartbeat; when
// the wall clock crosses an interval boundary it fetches the latest closed candle
// for every symbol in parallel and delivers the full map to handler.
func StartMultiSymbolKlineWebsocket(ctx context.Context, adapter KlineService, symbols []string, interval string, logger *slog.Logger, handler MultiSymbolCandleHandler) {
	if len(symbols) == 0 {
		return
	}
	heartbeat := symbols[0]

	duration, err := parseIntervalDuration(interval)
	if err != nil {
		logger.Error("[MultiTrigger] unsupported interval", "interval", interval, "err", err)
		return
	}
	intervalSecs := int64(duration.Seconds())

	var lastCandleTime atomic.Int64
	var fetching atomic.Bool

	if candles, err := FetchLatestCandles(ctx, adapter, heartbeat, interval, 2); err == nil && len(candles) > 0 {
		lastCandleTime.Store(candles[len(candles)-1].Time)
		logger.Info("[MultiTrigger] seeded", "heartbeat", heartbeat, "candle_time", lastCandleTime.Load())
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
			time.Sleep(2 * time.Second)

			// Verify the boundary has a new candle via the heartbeat symbol.
			seed, err := FetchLatestCandles(ctx, adapter, heartbeat, interval, 2)
			if err != nil || len(seed) == 0 {
				return
			}
			latest := seed[len(seed)-1]
			if latest.Time <= lastCandleTime.Load() {
				return
			}
			lastCandleTime.Store(latest.Time)
			logger.Info("[MultiTrigger] new closed candle", "time", latest.Time)

			// Fetch latest candle for every symbol in parallel.
			type result struct {
				symbol string
				candle WsCandle
			}
			ch := make(chan result, len(symbols))
			var wg sync.WaitGroup
			for _, sym := range symbols {
				wg.Add(1)
				go func(sym string) {
					defer wg.Done()
					candles, err := FetchLatestCandles(ctx, adapter, sym, interval, 2)
					if err != nil || len(candles) == 0 {
						logger.Warn("[MultiTrigger] fetch failed", "symbol", sym, "err", err)
						return
					}
					c := candles[len(candles)-1]
					ch <- result{sym, WsCandle{
						Time: c.Time, Open: c.Open, High: c.High,
						Low: c.Low, Close: c.Close, Volume: c.Volume,
					}}
				}(sym)
			}
			wg.Wait()
			close(ch)

			candles := make(map[string]WsCandle, len(symbols))
			for r := range ch {
				candles[r.symbol] = r.candle
			}
			if len(candles) > 0 {
				handler(candles)
			}
		}()
	}

	connectBackoff := 3 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}

		doneCh, _, err := futures.WsBookTickerServe(
			strings.ToUpper(heartbeat),
			func(_ *futures.WsBookTickerEvent) { checkAndFire() },
			func(err error) { logger.Error("[MultiTrigger] WS error", "err", err) },
		)
		if err != nil {
			logger.Error("[MultiTrigger] connect failed, retrying", "err", err, "backoff", connectBackoff)
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
		logger.Info("[MultiTrigger] book-ticker WS connected", "heartbeat", heartbeat)

		select {
		case <-doneCh:
			logger.Warn("[MultiTrigger] WS dropped, reconnecting in 3s")
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
