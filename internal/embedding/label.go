package embedding

import "time-series-rag-agent/internal/exchange"

// LabelCalculatorI allows mocking in tests.
type LabelCalculatorI interface {
	CalculateFromHistory(history []exchange.WsRestCandle) []LabelUpdate
	CalculateLookahead(history []exchange.WsRestCandle, idx int, targetTime int64) []LabelUpdate
}

// LabelCalculator computes label updates for training data.
type LabelCalculator struct{}

func NewLabelCalculator() *LabelCalculator {
	return &LabelCalculator{}
}

// CalculateFromHistory generates label updates for past candles based on recent data.
// Used in streaming/live mode: each new candle unlocks labels for earlier candles.
func (l *LabelCalculator) CalculateFromHistory(history []exchange.WsRestCandle) []LabelUpdate {
	updates := []LabelUpdate{}
	n := len(history)
	if n < 2 {
		return updates
	}

	// Label A: Next Return for candle at T-1
	if update, ok := l.calcNextReturn(history, n-2, n-1); ok {
		updates = append(updates, update)
	}

	// Label B: Slope 3 for candle at T-3
	targetIdx3 := n - 4
	if targetIdx3 >= 0 {
		futurePrices := closesSlice(history, n-3, n)
		updates = append(updates, LabelUpdate{
			TargetTime: history[targetIdx3].Time,
			Column:     "next_slope_3",
			Value:      CalculateSlope(futurePrices),
		})
	}

	// Label C: Slope 5 for candle at T-5
	targetIdx5 := n - 6
	if targetIdx5 >= 0 {
		futurePrices := closesSlice(history, targetIdx5+1, n)
		updates = append(updates, LabelUpdate{
			TargetTime: history[targetIdx5].Time,
			Column:     "next_slope_5",
			Value:      CalculateSlope(futurePrices),
		})
	}

	return updates
}

func (l *LabelCalculator) CalculateCanelFromHistory(history []exchange.WsRestCandle) []LabelUpdate {
	updates := []LabelUpdate{}
	n := len(history)
	if n < 2 {
		return updates
	}

	// Label A: Next Return for candle at T-1
	if update, ok := l.calcNextReturn(history, n-2, n-1); ok {
		updates = append(updates, update)
	}

	// Label B: Slope 3 for candle at T-3
	targetIdx3 := n - 4
	if targetIdx3 >= 0 {
		futurePrices := closesSlice(history, n-3, n)
		updates = append(updates, LabelUpdate{
			TargetTime: history[targetIdx3].Time,
			Column:     "next_slope_3",
			Value:      CalculateSlope(futurePrices),
		})
	}

	// Label C: Slope 5 for candle at T-5
	targetIdx5 := n - 6
	if targetIdx5 >= 0 {
		futurePrices := closesSlice(history, targetIdx5+1, n)
		updates = append(updates, LabelUpdate{
			TargetTime: history[targetIdx5].Time,
			Column:     "next_slope_5",
			Value:      CalculateSlope(futurePrices),
		})
	}

	return updates
}

// CalculateLookahead generates labels by looking AHEAD from idx.
// Used in bulk mode: we know the future, so we compute labels directly.
func (l *LabelCalculator) CalculateLookahead(history []exchange.WsRestCandle, idx int, targetTime int64) []LabelUpdate {
	updates := []LabelUpdate{}
	n := len(history)

	// Label A: Next Return (T+1)
	if idx+1 < n {
		if update, ok := l.calcNextReturn(history, idx, idx+1); ok {
			update.TargetTime = targetTime
			updates = append(updates, update)
		}
	}

	// Label B: Slope 3 (T+1 to T+3)
	if idx+3 < n {
		futurePrices := closesSlice(history, idx+1, idx+4)
		updates = append(updates, LabelUpdate{
			TargetTime: targetTime,
			Column:     "next_slope_3",
			Value:      CalculateSlope(futurePrices),
		})
	}

	// Label C: Slope 5 (T+1 to T+5)
	if idx+5 < n {
		futurePrices := closesSlice(history, idx+1, idx+6)
		updates = append(updates, LabelUpdate{
			TargetTime: targetTime,
			Column:     "next_slope_5",
			Value:      CalculateSlope(futurePrices),
		})
	}

	return updates
}

// --- helpers ---

func (l *LabelCalculator) calcNextReturn(history []exchange.WsRestCandle, prevIdx, currIdx int) (LabelUpdate, bool) {
	prevClose := history[prevIdx].Close
	currClose := history[currIdx].Close
	if prevClose == 0 {
		return LabelUpdate{}, false
	}
	return LabelUpdate{
		TargetTime: history[prevIdx].Time,
		Column:     "next_return",
		Value:      (currClose - prevClose) / prevClose,
	}, true
}

func closesSlice(history []exchange.WsRestCandle, from, to int) []float64 {
	prices := make([]float64, 0, to-from)
	for i := from; i < to; i++ {
		prices = append(prices, history[i].Close)
	}
	return prices
}
