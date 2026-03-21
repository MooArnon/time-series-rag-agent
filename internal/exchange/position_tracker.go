// internal/exchange/position_tracker.go
package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

type CloseReason string

const (
	CloseReasonSL      CloseReason = "STOP_LOSS"
	CloseReasonTP      CloseReason = "TAKE_PROFIT"
	CloseReasonUnknown CloseReason = "UNKNOWN"
)

const (
	orderTypeSL = futures.OrderType(futures.AlgoOrderTypeStopMarket)
	orderTypeTP = futures.OrderType(futures.AlgoOrderTypeTakeProfitMarket)
)

type CloseResult struct {
	Reason      CloseReason
	RealizedPnL float64
}

// WasLastCloseStopLoss เช็คแค่ว่า order ที่ FILLED ล่าสุดคือ STOP_MARKET ไหม
// executor.go — return slTime ด้วย
func (e *Executor) WasLastCloseStopLoss(ctx context.Context) (bool, time.Time, error) {
	orders, err := e.Client.NewListAllAlgoOrdersService().
		Symbol(e.Symbol).Limit(5).Do(ctx)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("list algo orders: %w", err)
	}

	for i := range orders {
		o := orders[i]
		status := string(o.AlgoStatus)
		if status != "FINISHED" && status != "EXPIRED" {
			continue
		}
		if o.OrderType != futures.AlgoOrderTypeStopMarket || status != "FINISHED" {
			return false, time.Time{}, nil
		}
		pnl, _ := e.getLastRealizedPnL(ctx)
		slTime := time.UnixMilli(o.UpdateTime)
		slog.Debug("WasLastCloseStopLoss",
			"type", o.OrderType,
			"status", status,
			"pnl", pnl,
			"slTime", slTime,
		)
		return pnl < 0, slTime, nil
	}
	return false, time.Time{}, nil
}

func (e *Executor) getLastRealizedPnL(ctx context.Context) (float64, error) {
	incomes, err := e.Client.NewGetIncomeHistoryService().
		Symbol(e.Symbol).
		IncomeType("REALIZED_PNL").
		Limit(1).
		Do(ctx)
	if err != nil {
		return 0, err
	}
	if len(incomes) == 0 {
		return 0, nil
	}
	pnl, _ := strconv.ParseFloat(incomes[0].Income, 64)
	return pnl, nil
}

// CheckIfClosed เรียกตอน bar ใหม่มา ถ้า position ยังเปิดอยู่ return nil
func (e *Executor) CheckIfClosed(ctx context.Context) (*CloseResult, error) {
	// 1. เช็คว่ายัง open position อยู่มั้ย
	hasPos, _, _, err := e.HasOpenPosition(ctx)
	if err != nil {
		return nil, fmt.Errorf("check position: %w", err)
	}
	if hasPos {
		return nil, nil // ยังเปิดอยู่ ยังไม่ต้องทำอะไร
	}

	// 2. Position หายไปแล้ว → ดู algo order history ว่าอันไหน FILLED
	reason, pnl, err := e.detectCloseReason(ctx)
	if err != nil {
		// log แต่ไม่ return error เพื่อไม่ให้ block pipeline
		e.Log.Warn(fmt.Sprintf("[Tracker] could not detect close reason: %v", err))
		return &CloseResult{Reason: CloseReasonUnknown}, nil
	}

	return &CloseResult{Reason: reason, RealizedPnL: pnl}, nil
}

func (e *Executor) detectCloseReason(ctx context.Context) (CloseReason, float64, error) {
	orders, err := e.Client.NewListOrdersService().
		Limit(20).
		Do(ctx)
	if err != nil {
		return CloseReasonUnknown, 0, fmt.Errorf("list orders: %w", err)
	}

	// วนจากใหม่ → เก่า
	for i := len(orders) - 1; i >= 0; i-- {
		o := orders[i]

		// กรอง symbol เอง และเฉพาะ FILLED
		if o.Symbol != e.Symbol || o.Status != futures.OrderStatusTypeFilled {
			continue
		}

		switch o.Type {
		case orderTypeSL:
			return CloseReasonSL, 0, nil
		case orderTypeTP:
			return CloseReasonTP, 0, nil
		}
	}

	return CloseReasonUnknown, 0, nil
}

func (e *Executor) GetCooldownState(ctx context.Context, interval time.Duration) (isInCooldown bool, barsRemaining int, err error) {
	orders, err := e.Client.NewListAllAlgoOrdersService().
		Symbol(e.Symbol).
		Limit(10).
		Do(ctx)
	if err != nil {
		return false, 0, err
	}

	// นับ consecutive SL จากใหม่ → เก่า
	consecutiveSL := 0
	var lastSLTime time.Time

	for i := range orders {
		o := orders[i]
		if string(o.AlgoStatus) != "FINISHED" && string(o.AlgoStatus) != "EXPIRED" {
			continue
		}

		isFinishedSL := o.OrderType == futures.AlgoOrderTypeStopMarket &&
			string(o.AlgoStatus) == "FINISHED"

		if isFinishedSL {
			pnl, _ := e.getLastRealizedPnL(ctx)
			if pnl >= 0 {
				break // trailing SL ได้กำไร = จบ streak
			}
			consecutiveSL++
			if consecutiveSL == 1 {
				lastSLTime = time.UnixMilli(o.UpdateTime)
			}
		} else {
			break // เจอ TP หรือ EXPIRED = จบ streak
		}
	}

	if consecutiveSL == 0 {
		return false, 0, nil
	}

	// คำนวณ resumeAfter จาก lastSLTime + bars * interval
	barsToWait := 2
	if consecutiveSL >= 2 {
		barsToWait = 4
	}
	resumeAfter := lastSLTime.Add(time.Duration(barsToWait) * interval)

	now := time.Now()
	if now.Before(resumeAfter) {
		diff := resumeAfter.Sub(now)
		barsRemaining = int(diff/interval) + 1
		return true, barsRemaining, nil
	}

	return false, 0, nil
}
