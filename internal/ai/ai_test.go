package ai

import (
	"math"
	"testing"
	"time"
)

// Helper: ฟังก์ชันเปรียบเทียบ Float ว่าเท่ากันไหม (โดยยอมให้คลาดเคลื่อนได้นิดหน่อย)
func assertFloatEquals(t *testing.T, name string, expected, actual float64) {
	epsilon := 1e-6 // ยอมรับความคลาดเคลื่อนทศนิยมตำแหน่งที่ 6
	if math.Abs(expected-actual) > epsilon {
		t.Errorf("%s: expected %v, got %v", name, expected, actual)
	}
}

// ---------------------------------------------------------
// Test 1: ทดสอบ Math Helper - Slope (Linear Regression)
// ---------------------------------------------------------
func TestGetSlope(t *testing.T) {
	// Scenario: ราคาขึ้นเป็นเส้นตรงเป๊ะๆ
	// 100 -> 110 (ขึ้น 10%) -> 120 (เทียบกับฐาน 100 คือ 20%)
	// Norm Y: [0.0, 0.1, 0.2]
	// X:      [0, 1, 2]
	// Slope ควรเท่ากับ 0.1
	prices := []float64{100, 110, 120}
	
	expectedSlope := 0.1
	actualSlope := GetSlope(prices)

	assertFloatEquals(t, "Slope Calculation", expectedSlope, actualSlope)
}

// ---------------------------------------------------------
// Test 2: ทดสอบ Feature Generation (Embedding)
// ---------------------------------------------------------
func TestCalculateFeatures_ZigZag(t *testing.T) {
	// Setup: Window = 2 (ดูย้อนหลัง 2 Return)
	// ต้องใช้ข้อมูล 3 แท่ง (N+1) เพื่อให้ได้ 2 Returns
	
	// Mock Data: ราคาเด้งขึ้นลงเท่ากัน (Zig-Zag)
	// T0: 100
	// T1: 200 (Log Return ≈ 0.693)
	// T2: 100 (Log Return ≈ -0.693)
	
	// Mean ของ Returns ≈ 0
	// Std ของ Returns ≈ 0.693
	// ดังนั้น Z-Score ควรจะเป็น [1.0, -1.0] (โดยประมาณ)
	
	mockTime := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	
	history := []InputData{
		{Time: mockTime.Add(-2 * time.Minute).UnixMilli(), Close: 100.0},
		{Time: mockTime.Add(-1 * time.Minute).UnixMilli(), Close: 200.0},
		{Time: mockTime.UnixMilli(),                       Close: 100.0},
	}

	// Init AI logic
	// Window = 2
	ai := NewPatternAI("BTCUSDT", "1m", "test-model", 2)

	// Action
	result := ai.CalcFeatures(history)

	// Assertions
	if result == nil {
		t.Fatal("Result should not be nil")
	}

	// 1. Check Metadata
	if result.Symbol != "BTCUSDT" {
		t.Errorf("Symbol mismatch: got %s", result.Symbol)
	}
	
	// 2. Check Embedding Length
	if len(result.Embedding) != 2 {
		t.Fatalf("Embedding length mismatch: expected 2, got %d", len(result.Embedding))
	}

	// 3. Check Z-Score Logic
	// Return 1: ln(200) - ln(100) = 0.693147
	// Return 2: ln(100) - ln(200) = -0.693147
	// Mean = 0
	// Std = 0.693147
	// Z-Score 1 = (0.693147 - 0) / 0.693147 = 1.0
	// Z-Score 2 = (-0.693147 - 0) / 0.693147 = -1.0
	
	assertFloatEquals(t, "Embedding[0] (Positive Move)", 1.0, result.Embedding[0])
	assertFloatEquals(t, "Embedding[1] (Negative Move)", -1.0, result.Embedding[1])

	t.Logf("Success! Embedding Result: %v", result.Embedding)
}

// ---------------------------------------------------------
// Test 3: ทดสอบกรณีข้อมูลไม่พอ (Not Enough Data)
// ---------------------------------------------------------
func TestCalculateFeatures_NotEnoughData(t *testing.T) {
	ai := NewPatternAI("BTCUSDT", "1m", "test-model", 60)
	
	// ใส่ข้อมูลแค่ 10 ตัว (ต้องการ 61)
	history := make([]InputData, 10)
	for i := range history {
		history[i] = InputData{Time: int64(i), Close: 100}
	}

	result := ai.CalculateFeatures(history)

	if result != nil {
		t.Error("Should return nil when data is insufficient")
	}
}

// ---------------------------------------------------------
// Test 4: ทดสอบ Planck Constant (Zero Variance Case)
// ---------------------------------------------------------
func TestCalculateFeatures_FlatLine(t *testing.T) {
	// ราคานิ่งสนิท 100, 100, 100
	// Returns = [0, 0]
	// Mean = 0, Std = 0
	// Z-Score = (0 - 0) / (0 + Planck) = 0
	
	history := []InputData{
		{Time: 1000, Close: 100},
		{Time: 2000, Close: 100},
		{Time: 3000, Close: 100},
	}

	ai := NewPatternAI("BTCUSDT", "1m", "test-model", 2)
	result := ai.CalculateFeatures(history)

	assertFloatEquals(t, "Flat Line Embedding[0]", 0.0, result.Embedding[0])
	assertFloatEquals(t, "Flat Line Embedding[1]", 0.0, result.Embedding[1])
}