package trade

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func CalculateRealizedDailyPnL(client *futures.Client) float64 {

	// Calculate at 00:00:00 UTC, 7:00:00 Thailand
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startTime := startOfDay.UnixMilli()
	incomes, err := client.NewGetIncomeHistoryService().
		StartTime(startTime).
		Limit(1000).
		Do(context.Background())
	if err != nil {
		fmt.Print(err)
	}

	var totalPnL float64
	for _, income := range incomes {
		// Convert string amount to float
		var amt float64
		fmt.Sscanf(income.Income, "%f", &amt)

		switch income.IncomeType {
		case "REALIZED_PNL", "FUNDING_FEE", "COMMISSION":
			totalPnL += amt
		}
	}
	return totalPnL
}

func CalculateRealizedPnLHistory(client *futures.Client, lookbackDays int) ([]TransactionPnL, error) {
	now := time.Now().UTC()
	startTime := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -lookbackDays)

	incomes, err := client.NewGetIncomeHistoryService().
		StartTime(startTime.UnixMilli()).
		Limit(1000).
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("fetching income history: %w", err)
	}

	type groupKey struct {
		Time   int64
		Symbol string
	}
	grouped := make(map[groupKey]float64)
	tradeIDByTime := make(map[int64]string)
	symbols := make(map[string]struct{})

	for _, income := range incomes {
		switch income.IncomeType {
		case "REALIZED_PNL", "FUNDING_FEE", "COMMISSION":
			amt, err := strconv.ParseFloat(income.Income, 64)
			if err != nil {
				continue
			}
			key := groupKey{Time: income.Time, Symbol: income.Symbol}
			grouped[key] += amt
			if income.TradeID != "" {
				tradeIDByTime[income.Time] = income.TradeID
			}
			symbols[income.Symbol] = struct{}{}
		}
	}

	positionSideByTradeID := make(map[string]string)
	for symbol := range symbols {
		trades, err := client.NewListAccountTradeService().
			Symbol(symbol).
			Limit(1000).
			Do(context.Background())
		if err != nil {
			continue
		}
		for _, t := range trades {
			positionSideByTradeID[strconv.FormatInt(t.ID, 10)] = string(t.PositionSide)
		}
	}

	result := make([]TransactionPnL, 0, len(grouped))
	for key, netPnL := range grouped {
		tradeID := tradeIDByTime[key.Time]
		positionSide := positionSideByTradeID[tradeID] // LONG/SHORT, "" if FUNDING_FEE

		result = append(result, TransactionPnL{
			Time:   time.UnixMilli(key.Time).UTC(),
			NetPnL: netPnL,
			Symbol: key.Symbol,
			Side:   positionSide,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Time.Before(result[j].Time)
	})

	return result, nil
}

func GetPositionHistory(client *futures.Client, symbol string, lookbackDays int) ([]PositionHistory, error) {
	now := time.Now().UTC()
	startTime := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -lookbackDays)

	leverage := 1.0
	riskList, err := client.NewGetPositionRiskService().Symbol(symbol).Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("fetching position risk: %w", err)
	}
	for _, r := range riskList {
		if r.Symbol == symbol {
			leverage, _ = strconv.ParseFloat(r.Leverage, 64)
			break
		}
	}

	trades, err := client.NewListAccountTradeService().
		Symbol(symbol).
		Limit(1000).
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("fetching trades: %w", err)
	}

	var filtered []*futures.AccountTrade
	for _, t := range trades {
		if time.UnixMilli(t.Time).UTC().After(startTime) {
			filtered = append(filtered, t)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Time < filtered[j].Time
	})

	type posKey struct{ PositionSide string }
	type posState struct {
		runningQty    float64 // signed: + = long, - = short
		entryQtySum   float64
		entryPriceSum float64
		closeQtySum   float64
		closePriceSum float64
		realizedPnL   float64
		maxQty        float64
		openTime      time.Time
		positionLabel string
	}

	states := make(map[posKey]*posState)
	var result []PositionHistory

	for _, t := range filtered {
		qty, _ := strconv.ParseFloat(t.Quantity, 64)
		price, _ := strconv.ParseFloat(t.Price, 64)
		pnl, _ := strconv.ParseFloat(t.RealizedPnl, 64)
		commission, _ := strconv.ParseFloat(t.Commission, 64)
		key := posKey{PositionSide: string(t.PositionSide)}

		if _, ok := states[key]; !ok {
			states[key] = &posState{}
		}
		s := states[key]

		var isOpening bool
		switch string(t.PositionSide) {
		case "LONG":
			isOpening = string(t.Side) == "BUY"
		case "SHORT":
			isOpening = string(t.Side) == "SELL"
		case "BOTH":
			if string(t.Side) == "BUY" {
				isOpening = s.runningQty >= 0 // flat or long → opening/adding long
			} else {
				isOpening = s.runningQty <= 0 // flat or short → opening/adding short
			}
		}

		// infer label for BOTH mode when opening from flat
		if isOpening && s.runningQty == 0 {
			s.openTime = time.UnixMilli(t.Time).UTC()
			switch string(t.PositionSide) {
			case "BOTH":
				if string(t.Side) == "BUY" {
					s.positionLabel = "LONG"
				} else {
					s.positionLabel = "SHORT"
				}
			default:
				s.positionLabel = string(t.PositionSide)
			}
		}

		if isOpening {
			if string(t.Side) == "BUY" {
				s.runningQty += qty
			} else {
				s.runningQty -= qty
			}
			s.entryQtySum += qty
			s.entryPriceSum += price * qty
			s.realizedPnL -= commission // opening commission
			absQty := math.Abs(s.runningQty)
			if absQty > s.maxQty {
				s.maxQty = absQty
			}
		} else {
			if string(t.Side) == "BUY" {
				s.runningQty += qty
			} else {
				s.runningQty -= qty
			}
			s.closeQtySum += qty
			s.closePriceSum += price * qty
			s.realizedPnL += pnl - commission // closing pnl - commission

			if math.Abs(s.runningQty) <= 0.000001 {
				entryPrice := 0.0
				if s.entryQtySum > 0 {
					entryPrice = s.entryPriceSum / s.entryQtySum
				}
				avgClosePrice := 0.0
				if s.closeQtySum > 0 {
					avgClosePrice = s.closePriceSum / s.closeQtySum
				}
				roi := 0.0
				notional := entryPrice * s.maxQty
				if notional > 0 && leverage > 0 {
					margin := notional / leverage
					roi = (s.realizedPnL / margin) * 100
				}

				result = append(result, PositionHistory{
					Symbol:        symbol,
					PositionSide:  s.positionLabel,
					OpenTime:      s.openTime,
					CloseTime:     time.UnixMilli(t.Time).UTC(),
					EntryPrice:    entryPrice,
					AvgClosePrice: avgClosePrice,
					RealizedPnL:   s.realizedPnL,
					ROI:           roi,
					ClosedVol:     s.closeQtySum,
					MaxQty:        s.maxQty,
				})

				states[key] = &posState{}
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CloseTime.After(result[j].CloseTime)
	})

	return result, nil
}
func CalculateDailyROI(client *futures.Client) (float64, float64, error) {

	// 2. Get Current Account Balance
	acc, err := client.NewGetAccountService().Do(context.Background())
	if err != nil {
		return 0, 0, err
	}

	currentBalance, _ := strconv.ParseFloat(acc.TotalWalletBalance, 64)

	// 3. Get Today's PnL (from your existing logic)
	dailyPnL := CalculateRealizedDailyPnL(client)

	// 4. Determine "Beginning Balance"
	// For simplicity in a bot, Beginning Balance = Current Balance - Daily PnL
	// Note: This assumes no net transfers (deposits/withdrawals) occurred today.
	beginningBalance := currentBalance - dailyPnL

	if beginningBalance <= 0 {
		return dailyPnL, 0, nil // Avoid division by zero
	}

	// 5. Calculate ROI
	roi := (dailyPnL / beginningBalance) * 100

	return dailyPnL, roi, nil
}
