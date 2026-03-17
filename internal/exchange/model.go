package exchange

type RestCandle struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type WsCandle struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type WsRestCandle struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}
