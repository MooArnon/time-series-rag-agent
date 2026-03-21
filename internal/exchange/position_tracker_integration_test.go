//go:build integration

// internal/exchange/position_tracker_integration_test.go
package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// รัน: go test ./internal/exchange/... -tags integration -v -run TestIntegration
// ต้องตั้ง env ก่อน:
//   export BINANCE_API_KEY=xxx
//   export BINANCE_API_SECRET=xxx
//   export BINANCE_TESTNET=true  (optional: ใช้ testnet แทน mainnet)

func newIntegrationExecutor(t *testing.T) *Executor {
	t.Helper()

	apiKey := os.Getenv("BINANCE_API_KEY")
	apiSecret := os.Getenv("BINANCE_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Skip("BINANCE_API_KEY / BINANCE_API_SECRET not set — skipping integration test")
	}

	useTestnet := os.Getenv("BINANCE_TESTNET") == "true"
	futures.UseTestnet = useTestnet

	client := futures.NewClient(apiKey, apiSecret)

	return &Executor{
		Client:            client,
		Symbol:            "ETHUSDT",
		AviableTradeRatio: 0.95,
		Leverage:          5,
		SLPercentage:      0.05,
		TPPercentage:      0.05,
		Log:               *testIntegrationLogger(t),
	}
}

// ── 1. เช็ค open position ─────────────────────────────────────────────────

func TestIntegration_HasOpenPosition(t *testing.T) {
	e := newIntegrationExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hasPos, side, amt, err := e.HasOpenPosition(ctx)
	if err != nil {
		t.Fatalf("HasOpenPosition error: %v", err)
	}
	t.Logf("hasPos=%v side=%s amt=%f", hasPos, side, amt)
}

// ── 2. เช็ค detectCloseReason กับ order history จริง ────────────────────

func TestIntegration_DetectCloseReason(t *testing.T) {
	e := newIntegrationExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reason, pnl, err := e.detectCloseReason(ctx)
	if err != nil {
		t.Fatalf("detectCloseReason error: %v", err)
	}
	t.Logf("reason=%s pnl=%.4f", reason, pnl)
}

// ── 3. Full cycle: CheckIfClosed ──────────────────────────────────────────

func TestIntegration_CheckIfClosed(t *testing.T) {
	e := newIntegrationExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := e.CheckIfClosed(ctx)
	if err != nil {
		t.Fatalf("CheckIfClosed error: %v", err)
	}

	if result == nil {
		t.Log("position still open — no close result")
		return
	}
	t.Logf("closed: reason=%s pnl=%.4f", result.Reason, result.RealizedPnL)
}

// ── helpers ──────────────────────────────────────────────────────────────

func parseFloat(t *testing.T, s string) float64 {
	t.Helper()
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		t.Fatalf("parseFloat(%q): %v", s, err)
	}
	return f
}

func testIntegrationLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}
