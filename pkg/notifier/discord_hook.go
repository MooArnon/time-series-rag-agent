package pkg

import "fmt"

func (d *DiscordClient) NewPipelineHooks(symbol, interval string) *PipelineHooks {
	return &PipelineHooks{
		OnOrderExecuted: func(sym, signal string, price float64) {
			d.NotifyOrder(
				fmt.Sprintf("%s `%s` @ `%.2f`\nInterval: %s", signal, sym, price, interval),
				"",
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
