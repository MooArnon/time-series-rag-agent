package postgresql

import "time"

type TradeSignalLog struct {
	Time            time.Time
	Symbol          string
	Interval        string
	Signal          string
	Confidence      int
	RegimeRead      string
	PatternRead     string
	PriceActionRead string
	Synthesis       string
	RiskNote        string
	Invalidation    string
	WsClose         float64
	Executed        bool
	SkipReason      string
}
