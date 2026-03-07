package pipeline

type Feature struct {
	Time      string
	Symbol    string
	Interval  string
	Embedding []float64
}

type Label struct {
	Time       string
	Symbol     string
	Interval   string
	ClosePrice float64
	NextReturn float64
	NextSlope3 float64
	NextSlope5 float64
	NextSlope9 float64
}

type MarketPattern struct {
	Feature Feature
	Label   Label
}
