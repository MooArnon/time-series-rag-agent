package exchange

import (
	"github.com/adshao/go-binance/v2/futures"
)

// Rules:
//
//	1 SL            → wait 2 bars
//	consecutive SL  → wait 4 bars
//	1 win           → reset all (unlock immediately)
func NewStopLossCoolDown(client *futures.Client) {

}
