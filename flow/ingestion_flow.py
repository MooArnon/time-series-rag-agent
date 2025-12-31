###########
# Imports #
##############################################################################

import datetime

from logging import Logger

from market.__base import BaseMarket
from rag.ai.__base_ai import BaseAI
from rag.db.__base_db import BaseDB

###########
# Statics #
##############################################################################

def ingest_to_postgresql(
        logger: Logger,
        ai: BaseAI,
        db: BaseDB,
        market: BaseMarket,
        symbol: str,
        limit: int,
        interval: str = "15m"
) -> None:
    logger.info('Start flow Ingest to PostgreSQL')
    
    current_time = datetime.datetime.now(tz=datetime.timezone.utc)
    timestamp_step = current_time.replace(second=0)

    if interval == "15m":
        # Truncate to nearest 15 minutes
        minute = (timestamp_step.minute // 15) * 15
        timestamp_truncated = timestamp_step.replace(
            minute=minute, second=0, microsecond=0
        )
    
    data = market.get_data(
        symbol=symbol,
        target_date=timestamp_truncated,
        interval=interval,
        total_rows=limit,
    )
    
    latest_timestamp = data.iloc[-1]['timestamp']
    logger.info(f"Proceeding data with latest timestamp: {latest_timestamp}")
    
    features = ai.calculate_features(data)
    label = ai.calculate_labels(data)
    
    db.ingest_feature_label_to_postgresql(features, label)
    
    logger.info('End flow Ingest to PostgreSQL')
    return None

##############################################################################

def run_backfill_ingest_to_postgresql(
        logger: Logger,
        ai: BaseAI,
        db: BaseDB,
        market: BaseMarket,
        symbol: str,
        days: int,  # How many days back to fill
        interval: str = "15m"
) -> None:
    logger.info(f"--- STARTING BACKFILL: {symbol} for last {days} days ---")
    
    # 1. CALCULATE LIMIT
    # 15m candles: 4 per hour * 24 hours = 96 per day.
    # Add buffer for indicators (e.g. +100)
    total_candles = (96 * days) + 100
    
    # Binance max limit is usually 1000. If you need more, you'd need pagination.
    # For now, let's cap it at 1000 to be safe, or assume your market class handles pagination.
    # limit = min(total_candles, 100000) 
    limit = total_candles
    
    logger.info(f"Fetching {limit} candles from {interval} timeframe...")

    # 2. FETCH ALL DATA ONCE
    # We pretend "now" is the current time to get the latest chunk of history
    current_time = datetime.datetime.now(tz=datetime.timezone.utc)
    
    # Reuse your market class to get the dataframe
    # Note: We assume your market.get_data can handle large limits
    full_data = market.get_data(
        symbol=symbol,
        target_date=current_time,
        interval=interval,
        total_rows=limit
    )
    
    if len(full_data) < 65:
        logger.error("Not enough data to backfill. Need at least 65 rows.")
        return

    # 3. ITERATE THROUGH HISTORY (The Simulation)
    # We simulate the bot running at every single candle in the past.
    # Start at index 65 (need 60 for vector + 5 for slopes)
    
    success_count = 0
    
    # We loop from the 65th candle up to the very last one
    for i in range(65, len(full_data)):
        
        # SLICE THE WINDOW
        # Imagine we are at time 'i'. The bot only sees data up to 'i'.
        # We pass this slice to the AI, just like the live bot.
        historical_window = full_data.iloc[:i+1].copy()
        
        
        # Optional: Print progress every 50 rows
        if i % 50 == 0:
            timestamp_at_i = historical_window.iloc[-1]['timestamp'] # or 'timestamp'
            logger.info(f"Backfilling row {i}/{len(full_data)} at {timestamp_at_i}")

        try:
            # A. Calculate Features (Vector for time 'i')
            features = ai.calculate_features(historical_window)
            
            # B. Calculate Labels (Future outcomes relative to 'i')
            # Since 'historical_window' actually ends at 'i', calculate_labels 
            # will look at i-1, i-3, i-5 and calculate their outcomes using 'i'.
            label = ai.calculate_labels(historical_window)
            
            # C. Ingest
            if features:
                db.ingest_feature_label_to_postgresql(features, label)
                success_count += 1
                
        except Exception as e:
            logger.error(f"Failed at index {i}: {e}")

    logger.info(f"--- BACKFILL COMPLETE. Ingested {success_count} rows. ---")

##############################################################################

def run_backfill_bulk_ingest_to_postgresql(
        logger: Logger,
        ai: BaseAI, # Changed type hint to use our new class
        db: BaseDB,
        market: BaseMarket,
        symbol: str,
        total_candles = None,
        days: int = None,
        interval: str = "15m"
) -> None:
    logger.info(f"--- STARTING OPTIMIZED BACKFILL: {symbol} ---")
    
    db.set_connection()
    
    # 1. CALCULATE LIMIT
    if days is not None:
        total_candles = (96 * days) + 100
    else: 
        total_candles = total_candles + 60

    limit = total_candles # Assume market handles pagination or large limits
    
    # 2. FETCH ALL DATA ONCE
    current_time = datetime.datetime.now(tz=datetime.timezone.utc)
    
    logger.info(f"Fetching {limit} candles...")
    full_data = market.get_data(
        symbol=symbol,
        target_date=current_time,
        interval=interval,
        total_rows=limit
    )
    
    if len(full_data) < 65:
        logger.error("Not enough data.")
        return

    # 3. CALCULATE EVERYTHING IN BATCH (The Speedup)
    logger.info("Calculating features and labels (Vectorized)...")
    # This returns a list of tuples: (feature_dict, label_updates_list)
    bulk_data = ai.calculate_bulk_data(full_data)
    
    logger.info(f"Calculation complete. Ingesting {len(bulk_data)} rows...")

    # 4. INGEST
    # ideally, you should add a method db.bulk_ingest(bulk_data) to your DB class.
    # If not, we iterate, but it will still be faster because calculations are done.
    
    success_count = 0
    
    # Use a transaction block if your DB library supports it for speed
    for features, labels in bulk_data:
        try:
            # We assume your DB class can handle the format
            db.ingest_feature_label_to_postgresql(features, labels, auto_commit=True)
            success_count += 1
            
            if success_count % 500 == 0:
                db.connector.commit()
                logger.info(f"Ingested {success_count} / {len(bulk_data)}")
                
        except Exception as e:
            logger.info(f"Failed ingest: {e}")
    db.connector.commit()
    logger.info(f"--- BACKFILL COMPLETE. Ingested {success_count} rows. ---")

##############################################################################
