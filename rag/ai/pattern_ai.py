###########
# Imports #
##############################################################################

from logging import Logger
import os

import pandas as pd
import numpy as np
from numpy.lib.stride_tricks import sliding_window_view

from.__base_ai import BaseAI
from utils.logger import get_utc_logger

###########
# Classes #
##############################################################################

class PatternAI(BaseAI):
    """
    Base class for AI components.
    """
    def __init__(
            self, 
            symbol: str,
            timeframe: int,
            vector_window: int = 60,
            logger = None
    ) -> None:
        if logger is None:
            self.__logger: Logger = get_utc_logger(
                name=self.__class__.__name__,
                level=os.environ.get('LOG_LEVEL', 'INFO')
            )
        else:
            self.__logger = logger
        super().__init__()
        
        self.__symbol = symbol
        self.__time_frame = timeframe
        self.__vector_window = vector_window
        
    ##############
    # Properties #
    ##########################################################################
    
    @property
    def symbol(self) -> str:
        return self.__symbol
    
    ##########################################################################
    
    @property
    def timeframe(self) -> str:
        return self.__time_frame
    
    ##########################################################################
    
    @property
    def vector_window(self) -> int:
        return self.__vector_window
    
    ##########
    # Lables #
    ##########################################################################
    
    def calculate_labels(self, data):
        
        # Your data format "2025-12-28 15:00:00" is standard string format
        data['dt'] = pd.to_datetime(data['timestamp'])

        # We only need the closing price and the time
        data = data.rename(columns={'timestamp': 'dt'})
        
        data = data.loc[:, ~data.columns.duplicated()]

        # Binance data often comes as strings. We must force it to be numbers.
        data['close'] = data['close'].astype(float) 

        # (Optional) Ensure 'dt' is actually a datetime object, not a string
        data['dt'] = pd.to_datetime(data['dt']) # or what

        # records = [{'dt': Timestamp('2025...'), 'close': 0.3717}, ...]
        data = data[['dt', 'close']].to_dict('records')
        
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
            return float(slope)

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
    
    ############
    # Features #
    ##########################################################################
    
    def calculate_features(self, data):
        """ A function to calculate features from raw time-series data.
        
        Parameters
        ----------
        
        """
        # Your data format "2025-12-28 15:00:00" is standard string format
        data['dt'] = pd.to_datetime(data['timestamp'])

        # We only need the closing price and the time
        data = data.rename(columns={'timestamp': 'dt'})
        
        data = data.loc[:, ~data.columns.duplicated()]

        # Binance data often comes as strings. We must force it to be numbers.
        data['close'] = data['close'].astype(float) 

        # (Optional) Ensure 'dt' is actually a datetime object, not a string
        data['dt'] = pd.to_datetime(data['dt']) # or what

        # records = [{'dt': Timestamp('2025...'), 'close': 0.3717}, ...]
        data = data[['dt', 'close']].to_dict('records')
        
        # We need the last N+1 candles to calculate N log-returns
        if len(data) < self.vector_window + 1:
            self.logger.info("Not enough data for features.")
            return None

        # Get the specific window for feature creation (last 61 points)
        window = data[-(self.vector_window + 1):]
        closes = np.array([d['close'] for d in window])

        # A. Calculate Log Returns
        log_returns = np.diff(np.log(closes))

        # B. Normalize (Z-Score)
        # This makes the "shape" comparable regardless of volatility
        vector = (log_returns - np.mean(log_returns)) / (np.std(log_returns) + 1e-9)

        return {
            'time': window[-1]['dt'],
            'symbol': self.symbol,
            'interval': self.timeframe,
            'embedding': vector.tolist(),
            'close_price': window[-1]['close']
        }
    
    ##########################################################################
    
    def calculate_bulk_data(self, data: pd.DataFrame):
        """
        Vectorized calculation of features and labels for the entire history at once.
        Returns a list of dicts ready for ingestion.
        """
        # 1. PREPARE DATA
        df = data.copy()
        df['dt'] = pd.to_datetime(df['timestamp'])
        df['close'] = df['close'].astype(float)
        
        # Calculate Log Returns globally
        # We use a small epsilon to avoid log(0)
        closes = df['close'].values
        log_closes = np.log(closes + 1e-9)
        log_returns = np.diff(log_closes, prepend=log_closes[0])

        # ---------------------------------------------------------
        # 2. VECTORIZED FEATURE GENERATION (Corrected)
        # ---------------------------------------------------------
        
        # A. Calculate Global Log Returns once
        closes = df['close'].values
        log_closes = np.log(closes + 1e-9)
        
        # Global diff array
        # This array is 1 element shorter than closes (unless we used prepend, 
        # but let's keep it simple: index 0 is the first return)
        global_log_returns = np.diff(log_closes) 

        # B. Create Windows from the Returns directly
        # We need a window of exactly 'vector_window' size (e.g., 60)
        # We do NOT need vector_window + 1 here because these are already returns.
        window_size = self.vector_window
        
        # Shape: (N_windows, 60)
        # This creates a view, it's instant and uses almost no memory
        windows = sliding_window_view(global_log_returns, window_shape=window_size)
        
        # C. Align Indices
        # We need to know which row in the original DF these windows belong to.
        # If we have 100 prices -> 99 returns.
        # If window size is 60.
        # The first window (index 0 of 'windows') spans returns 0 to 59.
        # This corresponds to the price at index 60 (since return 0 is price 1 vs 0).
        # So the dataframe index for the *end* of the first window is 'window_size'.
        
        # Indices in 'global_log_returns' that correspond to the end of a window
        valid_indices = np.arange(window_size, len(global_log_returns) + 1)
        
        # D. Normalize (Z-Score)
        # Use the 'windows' variable we actually created!
        means = np.mean(windows, axis=1, keepdims=True)
        stds = np.std(windows, axis=1, keepdims=True)
        
        # The Embeddings
        embeddings = (windows - means) / (stds + 1e-9)

        # ---------------------------------------------------------
        # 3. VECTORIZED LABEL GENERATION
        # ---------------------------------------------------------
        # Instead of "updating past rows", we calculate the future outcome 
        # for the CURRENT row.
        
        # Label A: Next Return (Shifted back by 1)
        # At row i, we want (close[i+1] - close[i]) / close[i]
        df['next_return'] = df['close'].pct_change().shift(-1)
        
        # Helper for Vectorized Slope
        def calc_rolling_slope(series, window):
            # Rolling correlation/slope is expensive, so we use apply with raw=True
            # for reasonable speed, or explicit simple formula for small windows.
            def slope_func(y):
                # Normalized slope as per your original code
                start = y[0] if y[0] != 0 else 1e-9
                y_norm = (y - start) / start
                x = np.arange(len(y))
                s, _ = np.polyfit(x, y_norm, 1)
                return s
            
            return series.rolling(window=window).apply(slope_func, raw=True)

        # Label B: Slope 3 (Look ahead 3 candles)
        # We calculate slope on valid data, then shift it BACK to where the prediction happens.
        # We need slope of i+1, i+2, i+3. 
        # So we shift close by -1 (to start at i+1), calculate rolling(3), then shift -2 (alignment).
        # Easier way: Calculate rolling slope on original data, then shift results back by 'window'.
        
        slope3 = calc_rolling_slope(df['close'], 3).shift(-3)
        slope5 = calc_rolling_slope(df['close'], 5).shift(-5)
        
        df['next_slope_3'] = slope3
        df['next_slope_5'] = slope5

        # ---------------------------------------------------------
        # 4. MERGE AND FORMAT
        # ---------------------------------------------------------
        results = []
        
        for i, idx in enumerate(valid_indices):
            # FIX 1: Convert numpy scalar to python float
            row_dt = df['dt'].iloc[idx] # Pandas Timestamps usually behave fine
            row_close = float(df['close'].iloc[idx])  # <--- FORCE FLOAT
            
            feature_dict = {
                'time': row_dt,
                'symbol': self.symbol,
                'interval': self.timeframe,
                'embedding': embeddings[i].tolist(), # .tolist() already handles conversion
                'close_price': row_close
            }
            
            label_updates = []
            
            # Helper to safely extract float
            def safe_float(val):
                return float(val) if not pd.isna(val) else None

            # FIX 2: Check logic and force float for labels
            ret_val = df['next_return'].iloc[idx]
            if not np.isnan(ret_val):
                label_updates.append({
                    'target_time': row_dt,
                    'column': 'next_return',
                    'value': float(ret_val)  # <--- FORCE FLOAT
                })
                
            s3_val = df['next_slope_3'].iloc[idx]
            if not np.isnan(s3_val):
                label_updates.append({
                    'target_time': row_dt,
                    'column': 'next_slope_3',
                    'value': float(s3_val)   # <--- FORCE FLOAT
                })
                
            s5_val = df['next_slope_5'].iloc[idx]
            if not np.isnan(s5_val):
                label_updates.append({
                    'target_time': row_dt,
                    'column': 'next_slope_5',
                    'value': float(s5_val)   # <--- FORCE FLOAT
                })

            results.append((feature_dict, label_updates))
            
        return results

    ##########################################################################
    
##############################################################################
