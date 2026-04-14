package llm

type PnLData struct {
	PositionOpenAt string
	NetPnL         float64
	SignalSide     string
	Regime4H       string
	Regime1D       string
	DurationHours  string
}

type HistoricalDetail struct {
	Time            string `json:"time"`
	TrendSlope      string `json:"trend_slope"`
	TrendOutcome    string `json:"trend_outcome"`
	ImmediateReturn string `json:"immediate_return"`
	Distance        string `json:"distance"`         // <--- Added
	Similarity      string `json:"similarity_score"` // <--- Added
}

type TradeSignal struct {
	Signal          string  `json:"signal"`      // LONG, SHORT, HOLD
	Confidence      int     `json:"confidence"`  // 0-100 or 0.0-1.0 (handled dynamically)
	RegimeRead      string  `json:"regime_read"` // RegimeContext
	PatternRead     string  `json:"pattern_read"`
	PriceActionRead string  `json:"price_action_read"` // PriceAction
	Synthesis       string  `json:"synthesis"`         // reason
	RiskNote        string  `json:"risk_note"`
	Invalidation    float64 `json:"invalidation"`
}
