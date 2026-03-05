package trade

import "fmt"

// CalculateStopLossPrice returns the take-profit price for a leveraged position.
// percentageTp is the desired profit percentage relative to the notional value.
// positionDir must be either "LONG" or "SHORT".
func CalculateStopLossPrice(leverage int, percentageTp float64, positionDir string, price float64) (float64, error) {
    if leverage <= 0 {
        return 0.0, fmt.Errorf("leverage must be greater than zero, got %d", leverage)
    }
    if percentageTp < 0 {
        return 0.0, fmt.Errorf("percentageTp must be non-negative, got %f", percentageTp)
    }
    if price <= 0 {
        return 0.0, fmt.Errorf("price must be greater than zero, got %f", price)
    }

    multiplier := percentageTp / float64(leverage) / 100

    switch positionDir {
    case "LONG":
        return (1 - multiplier) * price, nil
    case "SHORT":
        return (1 + multiplier) * price, nil
    default:
        return 0.0, fmt.Errorf("position direction must be LONG or SHORT, got %q", positionDir)
    }
}