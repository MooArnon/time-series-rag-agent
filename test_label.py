import unittest
import numpy as np
import pandas as pd
from datetime import datetime, timedelta

# --- MOCK AI CLASS (Part 2: Labels) ---
class MockPatternAI_Labels:
    def __init__(self):
        self.symbol = "ADAUSDT"
    
    # PASTE YOUR EXACT calculate_labels FUNCTION HERE
    # (or import it if your project structure allows)
    def calculate_labels(self, data):
        # Helper for slope
        def get_slope(prices):
            if len(prices) < 2: return 0.0
            y = np.array(prices)
            # Normalize y to % change from start
            # If start is 0 (rare edge case), prevent div by zero
            start_val = y[0] if y[0] != 0 else 1e-9
            y_norm = (y - start_val) / start_val
            
            x = np.arange(len(y))
            slope, intercept = np.polyfit(x, y_norm, 1)
            return slope

        updates = []
        # We convert negative index to positive absolute length for safe slicing
        n = len(data)
        
        # --- Label A: Next Return (For T-1, index -2) ---
        if n >= 2:
            prev_idx = -2
            current_idx = -1
            # Check for zero division protection
            prev_close = data[prev_idx]['close']
            if prev_close != 0:
                ret = (data[current_idx]['close'] - prev_close) / prev_close
                updates.append({
                    'target_time': data[prev_idx]['dt'],
                    'column': 'next_return',
                    'value': ret
                })

        # --- Label B: Slope 3 (For T-3, index -4) ---
        # Target Index: -4 (The row we want to update)
        # Future Window: Next 3 candles (Indices -3, -2, -1)
        target_idx_3 = -4
        
        # Calculate ABSOLUTE start index
        # If len is 10, target is 6 (10-4). Next starts at 7.
        start_3 = n + target_idx_3 + 1 
        end_3 = start_3 + 3
        
        if n >= abs(target_idx_3) and end_3 <= n:
            future_prices = [d['close'] for d in data[start_3 : end_3]]
            slope = get_slope(future_prices)
            updates.append({
                'target_time': data[target_idx_3]['dt'],
                'column': 'next_slope_3',
                'value': slope
            })

        # --- Label C: Slope 5 (For T-5, index -6) ---
        target_idx_5 = -6
        start_5 = n + target_idx_5 + 1
        end_5 = start_5 + 5
        
        if n >= abs(target_idx_5) and end_5 <= n:
            future_prices = [d['close'] for d in data[start_5 : end_5]]
            slope = get_slope(future_prices)
            updates.append({
                'target_time': data[target_idx_5]['dt'],
                'column': 'next_slope_5',
                'value': slope
            })
            
        return updates
    
# --- TEST SUITE ---
class TestLabelCalculation(unittest.TestCase):
    
    def setUp(self):
        self.ai = MockPatternAI_Labels()
        # Create a simple "perfect uptrend" dataset
        # Prices: 100, 101, 102, 103, 104, 105
        # This makes math easy: Slope should be positive & stable.
        self.base_time = datetime(2025, 1, 1, 12, 0)
        self.data = []
        for i in range(10):
            self.data.append({
                'dt': self.base_time + timedelta(minutes=15*i),
                'close': 100 + i  # 100, 101, 102...
            })

    def test_next_return_calculation(self):
        """Test if it calculates the return for the PREVIOUS candle correctly"""
        # We pass the full 10-item list.
        # It should calculate return for index -2 (Price=108) using index -1 (Price=109)
        updates = self.ai.calculate_labels(self.data)
        
        # Filter for 'next_return'
        return_update = next(u for u in updates if u['column'] == 'next_return')
        
        # Expected: (109 - 108) / 108 = 1/108 = 0.009259...
        expected = (109 - 108) / 108
        self.assertAlmostEqual(return_update['value'], expected, places=5)
        
        # Verify it targeted the correct timestamp (Index -2)
        self.assertEqual(return_update['target_time'], self.data[-2]['dt'])

    def test_slope_calculation(self):
        """Test if it calculates slope for T-3 correctly"""
        updates = self.ai.calculate_labels(self.data)
        
        # Filter for 'next_slope_3'
        # Targeted index is -4 (Price=106).
        # Future prices used: 107, 108, 109.
        slope_update = next(u for u in updates if u['column'] == 'next_slope_3')
        
        # Since price rises by exactly +1 every step, slope should be positive
        self.assertGreater(slope_update['value'], 0)
        
        # Verify target time (Index -4)
        self.assertEqual(slope_update['target_time'], self.data[-4]['dt'])

    def test_insufficient_data(self):
        """Test that it doesn't crash on tiny lists"""
        tiny_data = [{'dt': datetime.now(), 'close': 100}]
        updates = self.ai.calculate_labels(tiny_data)
        self.assertEqual(len(updates), 0, "Should return no updates for single row")

if __name__ == '__main__':
    unittest.main()