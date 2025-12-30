import os
import numpy as np
import pandas as pd
from sqlalchemy import create_engine, text
from dotenv import load_dotenv
import ast

# 1. SETUP
load_dotenv()
HOST = os.getenv("DB_HOST")
PORT = os.getenv("DB_PORT")
NAME = os.getenv("DB_NAME")
USER = os.getenv("DB_USER")
PASS = os.getenv("DB_PASSWORD")

# Connection (SSL Disabled for local test)
DATABASE_URI = f"postgresql://{USER}:{PASS}@{HOST}:{PORT}/{NAME}?sslmode=disable"
engine = create_engine(DATABASE_URI)

# CONFIGURATION
TABLE_NAME = "market_pattern"   # Table with embeddings
PRICE_TABLE = "market_pattern"     # Table with raw OHLCV prices (to calc returns)
VECTOR_COL = "embedding"
TIME_COL = "time"                 # Adjust if your column is 'timestamp'
TOP_K = 10
LOOKAHEAD_STEPS = 4             # How far into the future we try to predict (e.g. 5 candles)
TEST_NUMBER = 10

def parse_vector(vec_str):
    if isinstance(vec_str, list): return np.array(vec_str)
    return np.array(ast.literal_eval(vec_str))

def get_future_return(conn, timestamp, steps):
    """Calculates the price change % over the next 'steps' intervals."""
    query = text(f"""
        SELECT close_price 
        FROM {PRICE_TABLE} 
        WHERE {TIME_COL} >= :ts 
        ORDER BY {TIME_COL} ASC 
        LIMIT :limit
    """)
    prices = pd.read_sql(query, conn, params={"ts": timestamp, "limit": steps + 1})
    
    if len(prices) < 2:
        return 0.0 # Not enough data
    
    start_price = prices.iloc[0]['close_price']
    end_price = prices.iloc[-1]['close_price']
    return (end_price - start_price) / start_price

def generate_signal(t_str):
    """
    Core Logic:
    1. Get Embedding at time t
    2. Search Top K similar history
    3. Calc average outcome of those history points
    4. Validate against reality
    """
    with engine.connect() as conn:
        # A. FETCH INPUT VECTOR (The "Situation")
        query_vec = text(f"SELECT * FROM {TABLE_NAME} WHERE {TIME_COL} = :t")
        result = conn.execute(query_vec, {"t": t_str}).mappings().one_or_none()
        
        if not result:
            print(f"❌ No data found for time {t_str}")
            return None

        current_vec_str = result[VECTOR_COL]
        current_id = result[TIME_COL] # Used to exclude self from search
        
        # B. RETRIEVE TOP K (The "Precedents")
        query_neighbors = text(f"""
            SELECT {TIME_COL}, {VECTOR_COL} <=> :vec as distance
            FROM {TABLE_NAME}
            WHERE {VECTOR_COL} IS NOT NULL
            AND {TIME_COL} < :t  -- Strict Backtesting: Only look at PAST data
            ORDER BY distance ASC
            LIMIT :k
        """)
        
        neighbors = conn.execute(query_neighbors, {
            "vec": current_vec_str,
            "t": current_id,
            "k": TOP_K
        }).mappings().all()

        if not neighbors:
            print("❌ Not enough historical data for RAG lookup.")
            return 
        
        distances = [n['distance'] for n in neighbors]
        avg_distance = np.mean(distances)

        # C. PREDICT (The "Slope" of History)
        neighbor_outcomes = []
        for n in neighbors:
            ret = get_future_return(conn, n[TIME_COL], steps=LOOKAHEAD_STEPS)
            neighbor_outcomes.append(ret)
        
        predicted_slope = np.mean(neighbor_outcomes)
        actual_return = get_future_return(conn, current_id, steps=LOOKAHEAD_STEPS)
        
        position = 1 if predicted_slope > 0 else -1
        pnl = position * actual_return
        
        return {
            "time": current_id,
            "predicted_slope": predicted_slope,
            "actual_return": actual_return,
            "pnl": pnl,
            "confidence": np.std(neighbor_outcomes),
            "similarity_score": avg_distance  # <--- ADD THIS (0.0 is a perfect match)
        }

# --- MAIN LOOP ---
if __name__ == "__main__":
    print(f"--- BACKTESTING SIGNAL LOGIC (Top K={TOP_K}) ---")
    
    # 1. Get random sample dates to test
    with engine.connect() as conn:
        dates = pd.read_sql(f"SELECT {TIME_COL} FROM {TABLE_NAME} ORDER BY RANDOM() LIMIT {TEST_NUMBER}", conn)
    
    total_pnl = 0
    wins = 0
    
    for t in dates[TIME_COL]:
        # Convert timestamp to string format if needed
        t_str = str(t)
        res = generate_signal(t_str)
        
        if res:
            is_win = res['pnl'] > 0
            if is_win: wins += 1
            total_pnl += res['pnl']
            
            # Interpret the Score
            # Distance < 0.2 is usually a "Strong Match"
            # Distance > 0.5 is usually "Weak/Random"
            match_quality = "⭐⭐⭐" if res['similarity_score'] < 0.1 else "⭐"

            print(f"\n📅 Time: {t_str}")
            print(f"   🔍 Prediction: {'📈 UP' if res['predicted_slope'] > 0 else '📉 DOWN'} (Exp: {res['predicted_slope']:.4%})")
            print(f"   🧬 Similarity: {res['similarity_score']:.4f} (Lower is better) {match_quality}")
            print(f"   🎯 Actual:     {res['actual_return']:.4%}")
            print(f"   💰 PnL:        {'✅' if is_win else '❌'} {res['pnl']:.4%}")

    print("\n" + "="*30)
    print(f"TOTAL PnL: {total_pnl:.4%}")
    print(f"WIN RATE:  {wins}/{len(dates)}")