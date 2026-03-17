// pkg/hooks.go
package pkg

type PipelineHooks struct {
	OnOrderExecuted func(symbol, signal string, price float64)
	OnPipelineError func(phase string, err error)
}
