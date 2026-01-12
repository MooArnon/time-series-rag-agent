package trade

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// Executor holds the client and the target symbol
type Executor struct {
	Client            *futures.Client
	Symbol            string
	AviableTradeRatio float64 // e.g. 0.95 for 95%
	Leverage          int
	SLPercentage      float64
	TPPercentage      float64
	Log               slog.Logger
}

func NewExecutor(
	Client *futures.Client,
	Symbol string,
	AviableTradeRatio float64,
	Leverage int,
	SLPercentage float64,
	TPPercentage float64,
	Log slog.Logger,
) *Executor {
	return &Executor{
		Client:            Client,
		Symbol:            Symbol,
		AviableTradeRatio: AviableTradeRatio,
		Leverage:          Leverage,
		SLPercentage:      SLPercentage,
		TPPercentage:      TPPercentage,
		Log:               Log,
	}
}

// 1. HasOpenPosition: Checks if you are currently LONG or SHORT (Active Trade)
func (e *Executor) HasOpenPosition(ctx context.Context) (bool, string, float64, error) {
	// Matches Python: futures_position_information
	positions, err := e.Client.NewGetPositionRiskService().Symbol(e.Symbol).Do(ctx)
	if err != nil {
		return false, "", 0, fmt.Errorf("API error: %v", err)
	}

	for _, p := range positions {
		if p.Symbol == e.Symbol {
			amt, _ := strconv.ParseFloat(p.PositionAmt, 64)

			if amt > 0 {
				return true, "LONG", amt, nil
			} else if amt < 0 {
				return true, "SHORT", amt, nil
			}
			return false, "HOLD", 0, nil
		}
	}
	return false, "HOLD", 0, nil
}

// 2. HasOpenOrders: Checks for pending Limit/SL/TP orders (Using your snippet)
func (e *Executor) HasOpenOrders(ctx context.Context) (bool, error) {
	// Matches your snippet: NewListOpenOrdersService
	orders, err := e.Client.NewListOpenOrdersService().Symbol(e.Symbol).Do(ctx)
	if err != nil {
		return false, fmt.Errorf("API error: %v", err)
	}

	// If the list is not empty, you have open orders
	if len(orders) > 0 {
		return true, nil
	}

	return false, nil
}

// SetLeverage tells Binance to update the leverage for this symbol
func (e *Executor) SetLeverage(ctx context.Context, leverage int) error {
	_, err := e.Client.NewChangeLeverageService().
		Symbol(e.Symbol).
		Leverage(leverage).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to set leverage: %v", err)
	}
	e.Log.Info(fmt.Sprintf("[Executor] Leverage set to %dx on Binance\n", leverage))
	return nil
}

// PlaceTrade executes the Main Order + Stop Loss + Take Profit
// PlaceTrade executes the Main Order (Standard) + SL/TP (Algo)
func (e *Executor) PlaceTrade(ctx context.Context, side string, priceToPlace float64) error {

	e.Log.Info(fmt.Sprintln("[Executor] 🧹 Cleaning up open orders..."))
	if err := e.CancelAllOpenOrders(ctx); err != nil {
		e.Log.Info(fmt.Sprintf("[Executor] Warning: %v\n", err))
	}
	if err := e.CancelAllAlgoOrders(ctx); err != nil {
		e.Log.Info(fmt.Sprintf("[Executor] Warning: %v\n", err))
	}

	slPrice := e.CalculateSL(priceToPlace, side)
	tpPrice := e.CalculateTP(priceToPlace, side)

	_, errWaitBalance := e.WaitForBalanceRelease(ctx, 21.0)
	if errWaitBalance != nil {
		e.Log.Info(fmt.Sprintf("[Executor] Failed while wating for balance: %v", errWaitBalance))
	}

	quantity, err := e.CalculateQuantity(ctx, priceToPlace)

	e.Log.Info(fmt.Sprintf("[Executor] ⚡ PLACING TRADE: %s | Qty: %s | SL: %.4f | TP: %.4f\n", side, quantity, slPrice, tpPrice))

	slPriceStr, err := e.FormatPrice(ctx, slPrice)
	if err != nil {
		return fmt.Errorf("failed to format SL price: %v", err)
	}

	tpPriceStr, err := e.FormatPrice(ctx, tpPrice)
	if err != nil {
		return fmt.Errorf("failed to format TP price: %v", err)
	}

	// 1. Determine Sides
	var mainSide, closeSide futures.SideType
	if side == "LONG" {
		mainSide = futures.SideTypeBuy
		closeSide = futures.SideTypeSell
	} else {
		mainSide = futures.SideTypeSell
		closeSide = futures.SideTypeBuy
	}

	// -------------------------------------------------------------
	// 2. MAIN ENTRY (Standard Order API)
	// Market entries still go through the standard endpoint
	// -------------------------------------------------------------

	priceToPlaceStr := strconv.FormatFloat(priceToPlace, 'f', -1, 64)
	mainOrder, err := e.Client.NewCreateOrderService().
		Symbol(e.Symbol).
		Side(mainSide).
		Type(futures.OrderTypeLimit).            // <--- Change to Limit
		TimeInForce(futures.TimeInForceTypeGTC). // <--- Required (Good Till Cancel)
		Price(priceToPlaceStr).                  // <--- Required for Limit
		Quantity(quantity).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("limit order failed: %v", err)
	}
	e.Log.Info(fmt.Sprintf("[Executor] ✅ Limit Order Placed: %d @ %s\n", mainOrder.OrderID, priceToPlaceStr))

	// -------------------------------------------------------------
	// 3. STOP LOSS (Algo Order API)
	// Note: We use AlgoType, TriggerPrice, and ReduceOnly
	// -------------------------------------------------------------
	_, err = e.Client.NewCreateAlgoOrderService().
		Symbol(e.Symbol).
		Side(closeSide).
		AlgoType("CONDITIONAL").
		Type("STOP_MARKET").      // Uses "STOP_MARKET"
		Quantity(quantity).       // Explicit Quantity
		ReduceOnly(true).         // Close-only
		TriggerPrice(slPriceStr). // Algo uses TriggerPrice
		Do(ctx)

	if err != nil {
		e.Log.Info(fmt.Sprintf("[Executor] ⚠️ Stop Loss Failed: %v\n", err))
	} else {
		e.Log.Info(fmt.Sprintln("[Executor] 🛡️ Stop Loss Set (Algo)"))
	}

	// -------------------------------------------------------------
	// 4. TAKE PROFIT (Algo Order API)
	// -------------------------------------------------------------
	_, err = e.Client.NewCreateAlgoOrderService().
		Symbol(e.Symbol).
		Side(closeSide).
		AlgoType("CONDITIONAL").
		Type("TAKE_PROFIT_MARKET"). // Uses "TAKE_PROFIT_MARKET"
		Quantity(quantity).
		ReduceOnly(true).
		TriggerPrice(tpPriceStr). // Algo uses TriggerPrice
		Do(ctx)

	if err != nil {
		e.Log.Info(fmt.Sprintf("[Executor] ⚠️ Take Profit Failed: %v\n", err))
	} else {
		e.Log.Info(fmt.Sprintln("[Executor] 💰 Take Profit Set (Algo)"))
	}

	return nil
}

func (e *Executor) CancelAllOpenOrders(ctx context.Context) error {
	// Standard Endpoint: DELETE /fapi/v1/allOpenOrders
	err := e.Client.NewCancelAllOpenOrdersService().
		Symbol(e.Symbol).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("failed to cancel open orders: %v", err)
	}
	e.Log.Info(fmt.Sprintln("[Executor] ✅ All Standard Open Orders Cancelled"))
	return nil
}

// CancelAllAlgoOrders cancels Strategy Orders (SL/TP)
// CancelAllAlgoOrders cancels Strategy Orders (SL/TP)
func (e *Executor) CancelAllAlgoOrders(ctx context.Context) error {
	// 1. Fetch Open Algo Orders
	// Note: 'NewListOpenAlgoOrdersService' might not exist in all versions.
	// If this errors, your version of go-binance might be old.
	// You can try 'NewListAlgoOrdersService' or check your library docs.
	openAlgos, err := e.Client.NewListOpenAlgoOrdersService().
		Symbol(e.Symbol).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("failed to fetch algo orders: %v", err)
	}

	if len(openAlgos) == 0 {
		return nil
	}

	e.Log.Info(fmt.Sprintf("[Executor] found %d active algo orders. cancelling...\n", len(openAlgos)))

	// 2. Iterate and Cancel
	for _, algo := range openAlgos {
		_, err := e.Client.NewCancelAlgoOrderService().
			AlgoID(algo.AlgoId).
			Do(ctx)

		if err != nil {
			e.Log.Info(fmt.Sprintf("[Executor] ⚠️ Failed to cancel Algo %d: %v\n", algo.AlgoId, err))
		} else {
			e.Log.Info(fmt.Sprintf("[Executor] 🗑️ Cancelled Algo Order %d\n", algo.AlgoId))
		}
	}

	return nil
}

func (e *Executor) CalculateQuantity(ctx context.Context, currentPrice float64) (string, error) {
	// 1. Get Available USDT in Port
	// We use a helper function to loop through assets and find "USDT"
	aviableUsdtInPort, err := e.getUSDTAvailableBalance(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch balance: %v", err)
	}

	// 2. Calculate Buying Power (USDT to Trade)
	// Formula: Balance * Ratio * Leverage
	// Example: 100 USDT * 0.90 * 5 = 450 USDT
	usdtToTrade := aviableUsdtInPort * e.AviableTradeRatio * float64(e.Leverage)

	// 3. Calculate Raw Quantity
	// Example: 450 USDT / 2000 Price = 0.225
	rawQty := usdtToTrade / currentPrice

	// 4. Adjust to Step Size (Crucial for API acceptance)
	// This fetches the "LOT_SIZE" filter from Binance and rounds down
	// Example: If BTC step is 0.001, it ensures we don't send 0.0015 (which causes error)
	qtyString, err := e.adjustQuantity(ctx, rawQty)
	if err != nil {
		return "", fmt.Errorf("failed to adjust quantity: %v", err)
	}
	e.Log.Info(fmt.Sprintln("[Executor] qtyString", qtyString))

	return qtyString, nil
}

// SL, TP
func (e *Executor) CalculateSL(price float64, side string) float64 {
	// 1. Calculate the Price Movement required to hit your Equity Risk target
	// Formula: Target_Equity_Risk / Leverage
	// Example: 0.05 (5%) / 5x Leverage = 0.01 (1% Price Move)
	priceMovement := e.SLPercentage / float64(e.Leverage)

	if side == "SHORT" {
		// Short SL is ABOVE entry
		return price * (1 + priceMovement)
	}

	if side == "LONG" {
		// Long SL is BELOW entry
		return price * (1 - priceMovement)
	}

	return 0.0
}

func (e *Executor) CalculateTP(price float64, side string) float64 {
	// 1. Calculate the Price Movement required to hit your Equity Risk target
	// Formula: Target_Equity_Risk / Leverage
	// Example: 0.05 (5%) / 5x Leverage = 0.01 (1% Price Move)
	priceMovement := e.TPPercentage / float64(e.Leverage)

	if side == "SHORT" {
		// Short SL is ABOVE entry
		return price * (1 - priceMovement)
	}

	if side == "LONG" {
		// Long SL is BELOW entry
		return price * (1 + priceMovement)
	}

	return 0.0
}

// Helper functions
func (e *Executor) getUSDTAvailableBalance(ctx context.Context) (float64, error) {
	balances, err := e.Client.NewGetBalanceService().Do(ctx)
	if err != nil {
		return 0, err
	}
	for _, b := range balances {
		if b.Asset == "USDT" {
			// "AvailableBalance" is the field for tradeable funds
			return strconv.ParseFloat(b.AvailableBalance, 64)
		}
	}
	return 0, fmt.Errorf("USDT wallet not found")
}

func (e *Executor) adjustQuantity(ctx context.Context, rawQty float64) (string, error) {
	info, err := e.Client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return "", err
	}

	var stepSize float64 = 0.001 // Default Fallback
	var precision int = 3        // Default Fallback

	// Find our specific symbol's rules
	for _, s := range info.Symbols {
		if s.Symbol == e.Symbol {
			precision = s.QuantityPrecision
			for _, f := range s.Filters {
				if f["filterType"] == "LOT_SIZE" {
					stepSize, _ = strconv.ParseFloat(f["stepSize"].(string), 64)
				}
			}
			break
		}
	}

	// Math: Round down to nearest step (e.g. 10.5678 -> 10.5 if step is 0.1)
	qty := math.Floor(rawQty/stepSize) * stepSize

	// Format to fixed string to prevent "0.10000000001" errors
	format := "%." + strconv.Itoa(precision) + "f"
	return fmt.Sprintf(format, qty), nil
}

func (e *Executor) WaitForBalanceRelease(ctx context.Context, minExpectedBalance float64) (float64, error) {
	ticker := time.NewTicker(200 * time.Millisecond) // Check every 200ms
	defer ticker.Stop()

	timeout := time.After(10 * time.Second) // Give up after 3 seconds

	e.Log.Info(fmt.Sprintln("[Executor] ⏳ Waiting for funds to be released..."))

	for {
		select {
		case <-timeout:
			return 0, fmt.Errorf("timeout waiting for balance to recover")
		case <-ticker.C:
			balance, err := e.getUSDTAvailableBalance(ctx)
			if err != nil {
				e.Log.Info(fmt.Sprintf("[Executor]    Error fetching balance: %v\n", err))
				continue
			}

			// If balance is back above your threshold (e.g. $20), we are good!
			usdtToTrade := balance * e.AviableTradeRatio * float64(e.Leverage)
			if usdtToTrade >= minExpectedBalance {
				e.Log.Info(fmt.Sprintf("[Executor] ✅ Balance recovered: %.2f USDT\n", balance))
				return balance, nil
			}
		}
	}
}

// FormatPrice adjusts a float price to the symbol's specific Tick Size
func (e *Executor) FormatPrice(ctx context.Context, price float64) (string, error) {
	// 1. Fetch Exchange Info (Cached in a real app, but fetched here for safety)
	info, err := e.Client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return "", err
	}

	var tickSize float64 = 0.01 // Default fallback
	var precision int = 2       // Default fallback

	// 2. Find the Symbol & PRICE_FILTER
	for _, s := range info.Symbols {
		if s.Symbol == e.Symbol {
			precision = s.PricePrecision
			for _, f := range s.Filters {
				if f["filterType"] == "PRICE_FILTER" {
					tickSize, _ = strconv.ParseFloat(f["tickSize"].(string), 64)
				}
			}
			break
		}
	}

	// 3. Math: Round to nearest Tick Size
	// e.g. Price 3000.1234, Tick 0.1 -> 3000.1
	roundedPrice := math.Round(price/tickSize) * tickSize

	// 4. Format string with correct decimal places
	// If TickSize is 1.00 (0 decimals), this ensures we don't send "3000.0" if API wants "3000"
	// However, usually PricePrecision covers the decimal count.
	format := "%." + strconv.Itoa(precision) + "f"

	return fmt.Sprintf(format, roundedPrice), nil
}
