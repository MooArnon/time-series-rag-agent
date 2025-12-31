###########
# Imports #
##############################################################################

import ast
from logging import Logger
import requests
import json
import re
import io
import base64
import os
import traceback

import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
from numpy.lib.stride_tricks import sliding_window_view

from.__base_ai import BaseAI
from config.config import config
from utils.logger import get_utc_logger

###########
# Classes #
##############################################################################

class PatternAI(BaseAI):
    """
    Base class for AI components.
    """
    bot_type="LLMPatternAI"
    
    def __init__(
            self, 
            symbol: str,
            timeframe: int,
            model: str,
            vector_window: int = 60,
            logger = None,
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
        self.model = model
        
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
    
    def generate_signal(
            self, 
            current_time: str,
            matched_data: object,
            current_vector: object,
            plot_file_name: os.PathLike = None,
    ) -> pd.DataFrame:
        self.logger.info("Generating signal using LLM...")
        
        image_b64 = self.plot_patterns_to_base64(
            current_vec=current_vector,
            rag_matches=matched_data
        )
        
        if plot_file_name is not None:
            import base64
            image_data = base64.b64decode(image_b64)
            with open(plot_file_name, "wb") as file:
                file.write(image_data)
        
        message = self.generate_trading_prompt(
            current_time=current_time,
            matched_data=matched_data,
            image_b64=image_b64
        )

        # B. Define Headers
        headers = {
            "Authorization": f"Bearer {config['OPENAI_API_KEY']}",
            "Content-Type": "application/json"
        }

        # C. Define Payload (Multimodal)
        payload = {
            # Use the correct slug for Claude 3.5 Sonnet
            "model": self.model, 
            "messages": message,
        }
        
        # D. Send POST Request
        try:
            self.logger.info(f"Sending payload to LLM: {self.model}")
            response = requests.post(
                url="https://openrouter.ai/api/v1/chat/completions",
                headers=headers,
                data=json.dumps(payload)
            )
            
            # Check for errors
            response.raise_for_status()
            
            # E. Parse Response
            result = response.json()
            content = self.extract_content(response=result)
            
            self.logger.info(f"LLM Signal: {content['signal']}, Confidence: {content['confidence']}")
            self.logger.info(f"LLM Reasoning: {content['reasoning']}")
            
            # Filter LLM noise using confidence score
            if content["confidence"] < config['LLM_CONFIDENCE_PERCENTAGE_THRESHOLD']:
                self.logger.warning("Low confidence in signal. Defaulting to HOLD.")
                return 0
            return content

        except Exception as e:
            self.logger.info(f"Error calling OpenRouter: {e}")
            traceback.print_exc()
        return None
    
    ##########################################################################
    
    @staticmethod
    def generate_trading_prompt(current_time, matched_data, image_b64):
        """
        Constructs a prompt for GPT-4o combining Visual + Statistical Data.
        """
        
        # 1. Summarize the Statistical Data
        # We calculate the "Average Outcome" to give the LLM a hint
        returns = [m['next_return'] for m in matched_data]
        avg_return = sum(returns) / len(returns)
        positive_outcomes = len([r for r in returns if r > 0])
        
        # Clean the data (remove embeddings to save tokens)
        clean_data = []
        for m in matched_data:
            clean_data.append({
                "time": m['time'],
                "similarity_score": round(m['similarity_score'], 4),
                "next_return_pct": f"{m['next_return']*100:.4f}%",  # Convert to %
                "outcome": "UP" if m['next_return'] > 0 else "DOWN"
            })

        # 2. Build the System Prompt (The Persona)
        system_message = """
        You are an expert Quantitative Trader AI. Your job is to analyze current market patterns 
        by comparing them to historical precedents (RAG).
        
        You have two inputs:
        1. An Image showing the Current Market Pattern (Black Line) vs. Top 10 Historical Matches.
            The green lines mean the historical data that will move up (plus return).
            And the red lines mean the historical data that will move down (minus return).
        2. A Data List showing what happened immediately after those historical matches.
        
        Your Output must be a strict JSON object:
        {
            "reasoning": "Brief analysis of the visual pattern and statistical consensus...",
            "signal": "LONG" | "SHORT" | "HOLD",
            "confidence": 0.0 to 1.0
        }
        
        Rules for Signal:
        - If Visuals are messy/divergent AND Stats are mixed -> HOLD.
        - If Visuals look tight/consistent AND Stats are >70% aligned -> LONG/SHORT.
        - Also consider the appearance of lines they might be not tight but looks the same shape -> LONG/SHORT.
        """

        # 3. Build the User Message (The Evidence)
        user_content = f"""
        ### 1. Current Context
        - **Analysis Time:** {current_time}
        - **Statistical Consensus:** {positive_outcomes}/{len(returns)} precedents went UP.
        - **Average Return of Matches:** {avg_return*100:.4f}%

        ### 2. Historical Data (Top Matches)
        {json.dumps(clean_data, indent=2)}

        ### 3. Visual Analysis
        Please look at the attached chart. 
        - The BLACK line is the current price action.
        - The COLORED lines are the historical matches.
        - The DOTTED lines to the right are the "Future" outcomes of those matches.
        
        **Task:**
        Does the Black line follow a clear pattern that aligns with the historical outcomes?
        Predict the next move.
        """

        payload = [
            {"role": "system", "content": system_message},
            {"role": "user", "content": [
                {"type": "text", "text": user_content},
                {
                    "type": "image_url",
                    "image_url": {
                        "url": f"data:image/png;base64,{image_b64}"
                    }
                }
            ]}
        ]
        
        return payload
    
    ##########################################################################
    
    def plot_patterns_to_base64(self, current_vec, rag_matches):
        """
        Plots the Current Market (Black) vs Historical Matches (Red/Green).
        """
        plt.figure(figsize=(12, 6))
        
        # ---------------------------------------------------------
        # 1. PROCESS CURRENT MARKET (Black Line)
        # ---------------------------------------------------------
        # Parse and reconstruct price shape
        curr_data = self.parse_vector(current_vec)
        if not curr_data: return "Error: No current data"
        
        # Turn vector into a line shape
        curr_prices = self.vector_to_price_shape(curr_data)
        
        # Normalize to start at 0% (Standard Percentage Yield)
        curr_norm = (curr_prices / curr_prices[0]) - 1
        
        x_current = np.arange(len(curr_norm))
        plt.plot(x_current, curr_norm, color='black', linewidth=3, label='Current Market', zorder=10)

        # ---------------------------------------------------------
        # 2. PROCESS HISTORICAL MATCHES (Red/Green Lines)
        # ---------------------------------------------------------
        for match in rag_matches:
            # A. Parse the historical vector
            hist_vec = self.parse_vector(match.get('embedding'))
            if not hist_vec: continue
            
            # B. Reconstruct its shape
            hist_prices = self.vector_to_price_shape(hist_vec)
            hist_norm = (hist_prices / hist_prices[0]) - 1
            
            # C. Prepare "Future" Tail
            # We simulate the future path using the scalar 'next_return'
            future_return = match.get('next_return', 0.0)
            
            # Determine Color: Green if future is UP, Red if DOWN
            color = '#2ecc71' if future_return > 0 else '#e74c3c' # Flat UI colors
            
            # Plot the PAST (The Pattern)
            plt.plot(x_current, hist_norm, color=color, alpha=0.3, linewidth=1.5)
            
            # Plot the FUTURE (The Outcome Projection)
            # We draw a dashed line from the last point to the target return
            last_val = hist_norm[-1]
            target_val = last_val + future_return # Approximate visual target
            
            # Create X-axis for future (e.g., next 12 steps)
            future_steps = 12 
            x_future = np.arange(len(hist_norm)-1, len(hist_norm) + future_steps)
            y_future = np.linspace(last_val, target_val, len(x_future))
            
            plt.plot(x_future, y_future, color=color, alpha=0.6, linestyle='--', linewidth=1.5)

        # ---------------------------------------------------------
        # 3. FORMATTING
        # ---------------------------------------------------------
        # Vertical Line at "Right Now"
        plt.axvline(x=len(curr_norm)-1, color='blue', linestyle='--', label='Right Now')
        
        plt.title(f"Pattern Analysis: Top {len(rag_matches)} Historical Matches", fontsize=14)
        plt.xlabel("Time Steps (Candles)")
        plt.ylabel("Reconstructed Price Action (Normalized)")
        plt.legend(loc='upper left')
        plt.grid(True, alpha=0.3)
        
        # ---------------------------------------------------------
        # 4. SAVE TO BASE64 (For LLM / Web UI)
        # ---------------------------------------------------------
        buf = io.BytesIO()
        plt.savefig(buf, format='png', bbox_inches='tight')
        buf.seek(0)
        image_base64 = base64.b64encode(buf.read()).decode('utf-8')
        plt.close()
        buf.close()
        return image_base64
    
    ##########################################################################
    
    @staticmethod
    def parse_vector(vec_data):
        """
        Converts database string output (e.g. "[-0.1, 0.5]") into a list of floats.
        """
        # Case 1: Already a list? Return it.
        if isinstance(vec_data, list):
            return vec_data
        
        # Case 2: It's a string. Parse it.
        if isinstance(vec_data, str):
            # Clean up Postgres formatting just in case
            clean_str = vec_data.strip()
            
            # Handle Postgres Array format '{1,2,3}' if necessary
            if clean_str.startswith('{') and clean_str.endswith('}'):
                clean_str = clean_str.replace('{', '[').replace('}', ']')
                
            try:
                # Try JSON parsing first (fastest/standard for pgvector)
                return json.loads(clean_str)
            except json.JSONDecodeError:
                try:
                    # Fallback to literal eval (safer python parsing)
                    return ast.literal_eval(clean_str)
                except:
                    # Last resort: manual string splitting
                    return [float(x) for x in clean_str.strip('[]').split(',')]
                    
        return []

    ##########################################################################

    @staticmethod
    def vector_to_price_shape(embedding, start_price=100):
        """
        Reconstructs a 'price-like' curve from a Z-score vector.
        Since Embedding ~= Log Returns, CumSum(Embedding) ~= Log Price Curve.
        """
        vec = np.array(embedding)
        # 1. De-normalize roughly (assuming std deviation of 1%)
        # This restores the "magnitude" of movement for visualization
        simulated_returns = vec * 0.01 
        
        # 2. Reconstruct Price Path
        price_path = [start_price]
        for r in simulated_returns:
            price_path.append(price_path[-1] * (1 + r))
        
        # Drop the dummy start
        return np.array(price_path[1:]) 
    
    ############################
    # Extract content from LLM #
    ##########################################################################
    
    def extract_content(self, response: json):
        if self.model in [
            "google/gemini-2.5-flash", 
            "openai/gpt-4o",
        ]:
            # Structure response
            content = response['choices'][0]['message']['content']
            content = re.sub(r"```json\n|\n```", "", content).strip()
            content = json.loads(content)
        
        elif self.model in ["anthropic/claude-3.5-sonnet"]:
            content = response['choices'][0]['message']['content']
            content = json.loads(content)
        return content
    
    ##########################################################################
    
    
    ##########################################################################

##############################################################################
