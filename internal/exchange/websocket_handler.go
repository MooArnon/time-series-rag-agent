package exchange

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// lastEvent is updated on every WS frame so the watchdog can detect a silently-dead stream.
// lastHeartbeatLog gates the 30-second "stream alive" log line.
var (
	lastEvent        atomic.Int64
	lastHeartbeatLog atomic.Int64
)

func ErrHandler() futures.ErrHandler {
	return func(err error) {
		slog.Error("[WS] error", "err", err)
	}
}

func WsHandler(handler CandleHandler) futures.WsKlineHandler {
	return func(event *futures.WsKlineEvent) {
		now := time.Now().Unix()
		lastEvent.Store(now) // refresh on every frame for watchdog
		if !event.Kline.IsFinal {
			if now-lastHeartbeatLog.Load() >= 30 {
				lastHeartbeatLog.Store(now)
				slog.Info("[WS] stream alive", "symbol", event.Symbol, "startTime", event.Kline.StartTime, "close", event.Kline.Close)
			}
			return
		}
		slog.Info("[WS] final kline — dispatching pipeline", "symbol", event.Symbol, "startTime", event.Kline.StartTime, "close", event.Kline.Close)
		candle, err := parseWsKlineToWsCandle(event)
		if err != nil {
			slog.Error("[WS] error converting kline to candle", "err", err)
			return
		}
		handler(candle)
	}
}

func parseWsKlineToWsCandle(kline *futures.WsKlineEvent) (WsCandle, error) {

	op, err := strconv.ParseFloat(kline.Kline.Open, 64)
	if err != nil {
		return WsCandle{}, fmt.Errorf("failed to parse Open price: %w", err)
	}
	hi, err := strconv.ParseFloat(kline.Kline.High, 64)
	if err != nil {
		return WsCandle{}, fmt.Errorf("failed to parse High price: %w", err)
	}
	lo, err := strconv.ParseFloat(kline.Kline.Low, 64)
	if err != nil {
		return WsCandle{}, fmt.Errorf("failed to parse Low price: %w", err)
	}
	cl, err := strconv.ParseFloat(kline.Kline.Close, 64)
	if err != nil {
		return WsCandle{}, fmt.Errorf("failed to parse Close price: %w", err)
	}
	vl, err := strconv.ParseFloat(kline.Kline.Volume, 64)
	if err != nil {
		return WsCandle{}, fmt.Errorf("failed to parse Volume: %w", err)
	}
	return WsCandle{
		Time:   kline.Kline.StartTime / 1000,
		Open:   op,
		High:   hi,
		Low:    lo,
		Close:  cl,
		Volume: vl,
	}, nil
}
