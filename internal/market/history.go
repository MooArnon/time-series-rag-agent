package market

import (
	"context"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

type InputData struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

func FetchRealHistory(client *futures.Client, symbol string, interval string, limit int) ([]InputData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Call /fapi/v1/klines
	klines, err := client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	// Convert Binance Response -> []ai.InputData
	data := make([]InputData, len(klines))
	for i, k := range klines {
		// 1. Parse TIME
		openTime := k.OpenTime / 1000

		// 2. Parse ALL Prices (Open, High, Low, Close)
		// Crucial: You must parse these, or they default to 0.0
		op, _ := strconv.ParseFloat(k.Open, 64)
		hi, _ := strconv.ParseFloat(k.High, 64)
		lo, _ := strconv.ParseFloat(k.Low, 64)
		cl, _ := strconv.ParseFloat(k.Close, 64)
		vl, _ := strconv.ParseFloat(k.Volume, 64)

		data[i] = InputData{
			Time:   openTime,
			Open:   op, // <--- This was missing
			High:   hi, // <--- This was missing
			Low:    lo, // <--- This was missing
			Close:  cl,
			Volume: vl, // <--- This was missing
		}
	}

	return data, nil
}

func FetchHistoryByTime(
	client *futures.Client,
	symbol string,
	interval string,
	startTime time.Time,
	endTime time.Time,
) ([]InputData, error) {

	var allData []InputData
	limit := 1000
	currentStart := startTime.UnixMilli()
	endMs := endTime.UnixMilli()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

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

			allData = append(allData, InputData{
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
