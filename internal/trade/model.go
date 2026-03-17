package trade

import (
	"time"
)

type TransactionPnL struct {
	Time   time.Time
	NetPnL float64
	Symbol string
	Side   string // LONG, SHORT
	TranID int64
}

type PositionHistory struct {
	Symbol        string
	PositionSide  string
	OpenTime      time.Time
	CloseTime     time.Time
	EntryPrice    float64 // weighted avg
	AvgClosePrice float64 // weighted avg
	RealizedPnL   float64
	ROI           float64
	ClosedVol     float64 // qty closed
	MaxQty        float64
}
