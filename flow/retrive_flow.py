###########
# Imports #
##############################################################################

import ast
from datetime import datetime
import json
from zoneinfo import ZoneInfo
from logging import Logger
import io
import base64

import numpy as np
import matplotlib.pyplot as plt

from config.config import config
from market.__base import BaseMarket
from rag.ai.__base_ai import BaseAI
from rag.db.__base_db import BaseDB

###########
# Statics #
##############################################################################


#########
# Flows #
##############################################################################

def retrive_data(
        logger: Logger,
        ai: BaseAI,
        db: BaseDB,
        symbol: str,
        interval: str = "15m",
        time_column: str = "time",
        vector_column: str = "embedding",
        table_name: str = "market_pattern",
        top_k: int = 10
) -> None:
    logger.info('Start flow to retrive data')
    
    # 1. Get current timestamp
    current_timestamp = datetime.now(ZoneInfo("Asia/Bangkok"))
    current_timestamp_tunc = db.snap_to_window(
        timestamp=current_timestamp, 
        window=interval,
    )
    logger.info(f"Current timestamp is {current_timestamp} and trunc to {current_timestamp_tunc}")
    
    # 2. Get data at the point
    top_k, target_vec = db.find_similar_patterns(
        symbol=symbol,
        target_time=current_timestamp_tunc,
        time_column=time_column,
        vector_column=vector_column,
        table_name=table_name, 
        top_k=top_k,
    )
    
    print(top_k)
    
    base64_image = plot_patterns_to_base64(target_vec, top_k)
    
    logger.info('End flow to retrive data')
    return None

#########
# Utils #
##############################################################################

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

##############################################################################

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
    
    return np.array(price_path[1:]) # Drop the dummy start

##############################################################################

def plot_patterns_to_base64(current_vec, rag_matches):
    """
    Plots the Current Market (Black) vs Historical Matches (Red/Green).
    """
    plt.figure(figsize=(12, 6))
    
    # ---------------------------------------------------------
    # 1. PROCESS CURRENT MARKET (Black Line)
    # ---------------------------------------------------------
    # Parse and reconstruct price shape
    curr_data = parse_vector(current_vec)
    if not curr_data: return "Error: No current data"
    
    # Turn vector into a line shape
    curr_prices = vector_to_price_shape(curr_data)
    
    # Normalize to start at 0% (Standard Percentage Yield)
    curr_norm = (curr_prices / curr_prices[0]) - 1
    
    x_current = np.arange(len(curr_norm))
    plt.plot(x_current, curr_norm, color='black', linewidth=3, label='Current Market', zorder=10)

    # ---------------------------------------------------------
    # 2. PROCESS HISTORICAL MATCHES (Red/Green Lines)
    # ---------------------------------------------------------
    for match in rag_matches:
        # A. Parse the historical vector
        hist_vec = parse_vector(match.get('embedding'))
        if not hist_vec: continue
        
        # B. Reconstruct its shape
        hist_prices = vector_to_price_shape(hist_vec)
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
    
    plt.show()
    
    # ---------------------------------------------------------
    # 4. SAVE TO BASE64 (For LLM / Web UI)
    # ---------------------------------------------------------
    buf = io.BytesIO()
    plt.savefig(buf, format='png', bbox_inches='tight')
    buf.seek(0)
    image_base64 = base64.b64encode(buf.read()).decode('utf-8')
    plt.close()
    buf.close()
    
    print("Plot generated successfully!")
    return image_base64

##############################################################################
