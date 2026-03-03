package markettrend

type MarketTrend interface {
	PredictTrend() (string, error)
}
