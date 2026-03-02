package main

import (
	"fmt"

	markettrend "time-series-rag-agent/internal/market_trend"
)

const (
	Symbol       = "BTCUSDT"
	Interval     = "15m"
	VectorWindow = 200
)

func main() {
	regime := markettrend.RegimeTrend{}
	trend, err := regime.PredictTrend(Symbol, Interval, VectorWindow)
	if err != nil {
		fmt.Println("Error predicting trend:", err)
		return
	}
	fmt.Println("Predicted trend:", trend)
	fmt.Println("Predicted Direction:", trend.Direction)
}
