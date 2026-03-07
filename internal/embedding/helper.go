package embedding

import (
	"sort"
	"time-series-rag-agent/internal/exchange"
)

func MergeCandles(ws []exchange.WsCandle, rest []exchange.RestCandle) []exchange.WsRestCandle {
	merged := make(map[int64]exchange.WsRestCandle)

	for _, c := range rest {
		merged[c.Time] = exchange.WsRestCandle{
			Time:   c.Time,
			Open:   c.Open,
			High:   c.High,
			Low:    c.Low,
			Close:  c.Close,
			Volume: c.Volume,
		}
	}

	for _, c := range ws {
		merged[c.Time] = exchange.WsRestCandle{
			Time:   c.Time,
			Open:   c.Open,
			High:   c.High,
			Low:    c.Low,
			Close:  c.Close,
			Volume: c.Volume,
		}
	}

	result := make([]exchange.WsRestCandle, 0, len(merged))
	for _, c := range merged {
		result = append(result, c)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Time < result[j].Time
	})

	return result
}
