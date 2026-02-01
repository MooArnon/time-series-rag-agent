package trade

import (
	"context"
	"fmt"
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

func CalculateDailyROI(client *futures.Client) (float64, float64, error) {

	// 2. Get Current Account Balance
	acc, err := client.NewGetAccountService().Do(context.Background())
	if err != nil {
		return 0, 0, err
	}

	currentBalance, _ := strconv.ParseFloat(acc.TotalWalletBalance, 64)

	// 3. Get Today's PnL (from your existing logic)
	dailyPnL := CalculateRealizedDailyPnL(client)
	if err != nil {
		return 0, 0, err
	}

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
