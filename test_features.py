import unittest
import pandas as pd
import numpy as np
from datetime import datetime, timedelta

# --- 1. MOCK CLASS ---
# We define a dummy class to simulate your 'PatternAI' or agent class
class MockPatternAI:
    def __init__(self, window_size=5):
        self.vector_window = window_size
        self.symbol = "ADAUSDT"
        self.timeframe = "15m"
        self.logger = MockLogger()

    def calculate_features(self, data):
        # --- PASTE YOUR EXACT FUNCTION CODE HERE ---
        # (I am pasting the logic you provided in the prompt)
        
        # 1. Prepare Data
        # Ensure we work on a copy to avoid SettingWithCopy warnings in tests
        data = data.copy() 
        
        # Your specific logic:
        data['dt'] = pd.to_datetime(data['timestamp'])
        data = data.rename(columns={'timestamp': 'dt'})
        data = data.loc[:, ~data.columns.duplicated()]
        data['close'] = data['close'].astype(float)
        data['dt'] = pd.to_datetime(data['dt'])
        
        # Convert to dictionary list
        data_records = data[['dt', 'close']].to_dict('records')

        # 2. Check Length
        if len(data_records) < self.vector_window + 1:
            self.logger.info("Not enough data for features.")
            return None

        # 3. Vector Math
        window = data_records[-(self.vector_window + 1):]
        closes = np.array([d['close'] for d in window])

        log_returns = np.diff(np.log(closes))
        
        # Z-Score Normalization
        vector = (log_returns - np.mean(log_returns)) / (np.std(log_returns) + 1e-9)

        return {
            'time': window[-1]['dt'],
            'symbol': self.symbol,
            'interval': self.timeframe,
            'embedding': vector.tolist(),
            'close_price': window[-1]['close']
        }

class MockLogger:
    def info(self, msg):
        print(f"[LOG] {msg}")

# --- 2. THE TEST SUITE ---
class TestFeatureCalculation(unittest.TestCase):
    
    def setUp(self):
        # We use a small window of 3 for easier manual math verification
        self.ai = MockPatternAI(window_size=3)

    def test_not_enough_data(self):
        """Should return None if rows < window + 1"""
        # Create a DF with only 3 rows (Need 3+1 = 4)
        df = pd.DataFrame({
            'timestamp': ['2025-01-01 10:00', '2025-01-01 10:15', '2025-01-01 10:30'],
            'close': ['100', '101', '102']
        })
        
        result = self.ai.calculate_features(df)
        self.assertIsNone(result, "Should return None for insufficient data")

    def test_valid_input_string_conversion(self):
        """Should correctly handle string inputs for price and time"""
        # Create DF with exactly 4 rows (Minimum needed for window=3)
        # Note: 'close' is string, 'timestamp' is string
        df = pd.DataFrame({
            'timestamp': [
                '2025-01-01 10:00:00', 
                '2025-01-01 10:15:00', 
                '2025-01-01 10:30:00',
                '2025-01-01 10:45:00'
            ],
            'close': ['10.0', '11.0', '12.1', '13.31'] # 10% growth each step
        })
        
        result = self.ai.calculate_features(df)
        
        self.assertIsNotNone(result)
        self.assertEqual(result['symbol'], "ADAUSDT")
        self.assertIsInstance(result['embedding'], list)
        self.assertEqual(len(result['embedding']), 3) # Window size is 3
        self.assertEqual(result['close_price'], 13.31) # Last price converted to float

    def test_math_correctness(self):
        """Verify the Z-Score math manually"""
        # Prices: 100 -> 200 -> 100 -> 200
        # Log prices: 4.605, 5.298, 4.605, 5.298
        # Diffs (Returns): +0.693, -0.693, +0.693
        # Mean: +0.231
        # Use this pattern to check if the function outputs the expected vector
        
        df = pd.DataFrame({
            'timestamp': pd.date_range(start='2025-01-01', periods=4, freq='15min'),
            'close': [100, 200, 100, 200]
        })
        
        result = self.ai.calculate_features(df)
        vector = result['embedding']
        
        # Check standard deviation logic
        # 1. Returns are roughly [0.69, -0.69, 0.69]
        # 2. Code normalizes them. The middle value should be the lowest (negative Z-score).
        
        self.assertTrue(vector[1] < vector[0], "Middle return (drop) should be lower than first return (pump)")
        self.assertTrue(vector[0] > 0, "First return is positive relative to mean")

if __name__ == '__main__':
    unittest.main()