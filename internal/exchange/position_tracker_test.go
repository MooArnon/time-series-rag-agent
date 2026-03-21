// internal/exchange/position_tracker_test.go
package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

// helper: สร้าง Executor ที่ชี้ไป mock server
func newTestExecutor(t *testing.T, handler http.HandlerFunc) *Executor {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := futures.NewClient("test-key", "test-secret")
	client.BaseURL = srv.URL // ชี้ไป mock server แทน fapi.binance.com

	return &Executor{
		Client: client,
		Symbol: "ETHUSDT",
	}
}

// helper: สร้าง mock Order response
func mockOrderList(orders []map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestDetectCloseReason_SL(t *testing.T) {
	e := newTestExecutor(t, mockOrderList([]map[string]any{
		{
			"symbol":     "ETHUSDT",
			"orderId":    1001,
			"type":       "STOP_MARKET",
			"status":     "FILLED",
			"origQty":    "0.01",
			"price":      "0",
			"side":       "SELL",
			"time":       1700000000000,
			"updateTime": 1700000001000,
			"reduceOnly": true,
		},
	}))

	reason, _, err := e.detectCloseReason(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != CloseReasonSL {
		t.Errorf("want CloseReasonSL, got %s", reason)
	}
}

func TestDetectCloseReason_TP(t *testing.T) {
	e := newTestExecutor(t, mockOrderList([]map[string]any{
		{
			"symbol":     "ETHUSDT",
			"orderId":    1002,
			"type":       "TAKE_PROFIT_MARKET",
			"status":     "FILLED",
			"origQty":    "0.01",
			"price":      "0",
			"side":       "SELL",
			"time":       1700000000000,
			"updateTime": 1700000001000,
			"reduceOnly": true,
		},
	}))

	reason, _, err := e.detectCloseReason(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != CloseReasonTP {
		t.Errorf("want CloseReasonTP, got %s", reason)
	}
}

func TestDetectCloseReason_StillOpen(t *testing.T) {
	// มีแค่ LIMIT order ที่ยัง NEW → ยังไม่ปิด
	e := newTestExecutor(t, mockOrderList([]map[string]any{
		{
			"symbol":     "ETHUSDT",
			"orderId":    1003,
			"type":       "LIMIT",
			"status":     "NEW",
			"origQty":    "0.01",
			"price":      "2000",
			"side":       "BUY",
			"time":       1700000000000,
			"updateTime": 1700000000000,
		},
	}))

	reason, _, err := e.detectCloseReason(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != CloseReasonUnknown {
		t.Errorf("want CloseReasonUnknown, got %s", reason)
	}
}

func TestDetectCloseReason_PicksLatest(t *testing.T) {
	// มี SL เก่า + TP ใหม่ → ควร return TP (วนจากท้าย slice)
	e := newTestExecutor(t, mockOrderList([]map[string]any{
		{
			"symbol":  "ETHUSDT",
			"orderId": 1001,
			"type":    "STOP_MARKET",
			"status":  "FILLED",
			"origQty": "0.01", "price": "0", "side": "SELL",
			"time": 1700000000000, "updateTime": 1700000001000,
		},
		{
			"symbol":  "ETHUSDT",
			"orderId": 1002,
			"type":    "TAKE_PROFIT_MARKET",
			"status":  "FILLED",
			"origQty": "0.01", "price": "0", "side": "SELL",
			"time": 1700000002000, "updateTime": 1700000003000, // ใหม่กว่า
		},
	}))

	reason, _, err := e.detectCloseReason(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != CloseReasonTP {
		t.Errorf("want CloseReasonTP (latest), got %s", reason)
	}
}

func TestDetectCloseReason_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := futures.NewClient("key", "secret")
	client.BaseURL = srv.URL
	e := &Executor{Client: client, Symbol: "ETHUSDT"}

	_, _, err := e.detectCloseReason(context.Background())
	if err == nil {
		t.Fatal("expected error from API 500, got nil")
	}
}
