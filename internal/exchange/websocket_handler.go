package exchange

import (
	"fmt"
	"log"
	"strconv"

	"github.com/adshao/go-binance/v2/futures"
)

func ErrHandler() futures.ErrHandler {
	return func(err error) {
		log.Printf("[WS] error: %v", err)
	}
}

func WsHandler(handler CandleHandler) futures.WsKlineHandler {
	return func(event *futures.WsKlineEvent) {
		if !event.Kline.IsFinal {
			return
		}
		candle, err := parseWsKlineToCandle(event)
		if err != nil {
			log.Printf("[WS] error converting kline to candle: %v", err)
			return
		}
		handler(candle)
	}
}

func parseWsKlineToCandle(kline *futures.WsKlineEvent) (Candle, error) {

	op, err := strconv.ParseFloat(kline.Kline.Open, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("failed to parse Open price: %w", err)
	}
	hi, err := strconv.ParseFloat(kline.Kline.High, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("failed to parse High price: %w", err)
	}
	lo, err := strconv.ParseFloat(kline.Kline.Low, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("failed to parse Low price: %w", err)
	}
	cl, err := strconv.ParseFloat(kline.Kline.Close, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("failed to parse Close price: %w", err)
	}
	vl, err := strconv.ParseFloat(kline.Kline.Volume, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("failed to parse Volume: %w", err)
	}
	return Candle{
		Time:   kline.Kline.StartTime / 1000,
		Open:   op,
		High:   hi,
		Low:    lo,
		Close:  cl,
		Volume: vl,
	}, nil
}
