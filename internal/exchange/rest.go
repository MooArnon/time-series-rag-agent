package exchange

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func FetchLatestCandles(klineService KlineService, symbol string, interval string, limit int) ([]RestCandle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Call /fapi/v1/klines
	klines, err := klineService.FetchKlines(ctx, symbol, interval, limit)

	if err != nil {
		return nil, err
	}

	data, err := parseKLinesToRestCandle(klines)
	if err != nil {
		return nil, err
	}

	if len(data) > 0 {
		data = data[:len(data)-1]
	}

	return data, nil
}

func FetchHistoryByTime(
	client *futures.Client,
	symbol string,
	interval string,
	startTime time.Time,
	endTime time.Time,
) ([]RestCandle, error) {

	var allData []RestCandle
	limit := 1000
	currentStart := startTime.UnixMilli()
	endMs := endTime.UnixMilli()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

		klines, err := client.NewKlinesService().
			Symbol(symbol).
			Interval(interval).
			Limit(limit).
			StartTime(currentStart).
			EndTime(endMs).
			Do(ctx)
		cancel()

		if err != nil {
			return nil, err
		}
		if len(klines) == 0 {
			break
		}

		for _, k := range klines {
			op, _ := strconv.ParseFloat(k.Open, 64)
			hi, _ := strconv.ParseFloat(k.High, 64)
			lo, _ := strconv.ParseFloat(k.Low, 64)
			cl, _ := strconv.ParseFloat(k.Close, 64)
			vl, _ := strconv.ParseFloat(k.Volume, 64)

			allData = append(allData, RestCandle{
				Time:   k.OpenTime / 1000,
				Open:   op,
				High:   hi,
				Low:    lo,
				Close:  cl,
				Volume: vl,
			})
		}

		// ถ้าได้น้อยกว่า limit = หมดแล้ว
		if len(klines) < limit {
			break
		}

		// เลื่อน startTime ไปต่อ
		lastCandle := klines[len(klines)-1]
		currentStart = lastCandle.OpenTime + 1

		// ป้องกัน infinite loop
		if currentStart >= endMs {
			break
		}

		time.Sleep(100 * time.Millisecond) // rate limit
	}

	return allData, nil
}

func parseKLinesToRestCandle(klines []*futures.Kline) ([]RestCandle, error) {
	data := make([]RestCandle, len(klines))
	for i, k := range klines {
		op, err := strconv.ParseFloat(k.Open, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Open price: %w", err)
		}
		hi, err := strconv.ParseFloat(k.High, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse High price: %w", err)
		}
		lo, err := strconv.ParseFloat(k.Low, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Low price: %w", err)
		}
		cl, err := strconv.ParseFloat(k.Close, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Close price: %w", err)
		}
		vl, err := strconv.ParseFloat(k.Volume, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Volume: %w", err)
		}

		data[i] = RestCandle{
			Time:   k.OpenTime / 1000,
			Open:   op,
			High:   hi,
			Low:    lo,
			Close:  cl,
			Volume: vl,
		}
	}
	return data, nil
}
