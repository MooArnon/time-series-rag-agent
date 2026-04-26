package pkg

import "fmt"

const (
	PRICE_ACTION_FILE_NAME = "candle.png"
)

func (d *DiscordClient) NewPipelineHooks(symbol, interval string) *PipelineHooks {
	return &PipelineHooks{
		OnOrderExecuted: func(sym, signal string, price float64, synthesis string, patternRead string, priceActionRead string) {
			d.NotifyOrder(
				fmt.Sprintf("%s `%s` @ `%.2f`\nInterval: %s", signal, sym, price, interval),
				"",
			)
			d.NotifyOrder(
				fmt.Sprintln("Synthesis", synthesis),
				"",
			)

			d.NotifyOrder(
				fmt.Sprintln("PriceActionRead: ", priceActionRead),
				PRICE_ACTION_FILE_NAME,
			)
		},
		OnPipelineError: func(phase string, err error) {
			d.NotifyPipeline(
				fmt.Sprintf("[Pipeline Error] %s %s\nPhase: %s\n```%v```", symbol, interval, phase, err),
				"",
			)
		},
	}
}
