###########
# Imports #
##############################################################################

from datetime import datetime, timezone
import os
from zoneinfo import ZoneInfo
from logging import Logger

from config.config import config
from utils.discord import DiscordNotify
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
        market,
        symbol: str,
        notifier: DiscordNotify,
        is_open_order: bool = False,
        interval: str = "15m",
        time_column: str = "time",
        vector_column: str = "embedding",
        table_name: str = "market_pattern",
        top_k: int = 10,
        plot_file_name: os.PathLike = "market_pattern_plot.png",
        plot_file_name_price_action: os.PathLike = "price_action_plot.png",
        price_action_limit: int = 200
) -> None:
    prediction_logs = {}
    prediction_logs['asset'] = symbol
    logger.info('Start flow to retrive data')
    logger.info('Checking position...')
    contract = market.check_position_opening(symbol)
    if contract in ["LONG", "SHORT"]:
        logger.info(f'Contract exists {contract} end flow.')
        return
    
    # 1. Get current timestamp
    current_timestamp = datetime.now(ZoneInfo("Asia/Bangkok"))
    current_timestamp_tunc = db.snap_to_window(
        timestamp=current_timestamp, 
        window=interval,
    )
    logger.info(f"Current timestamp is {current_timestamp} and trunc to {current_timestamp_tunc}")
    
    prediction_logs['timestamp'] = current_timestamp_tunc
    prediction_logs['model_type'] = ai.bot_type
    
    # 2. Get data at the point
    logger.info("Finding similar patterns")
    top_k, target_vec = db.find_similar_patterns(
        symbol=symbol,
        target_time=current_timestamp_tunc,
        time_column=time_column,
        vector_column=vector_column,
        table_name=table_name, 
        top_k=top_k,
    )
    
    _, _, average_distance = get_min_max_distinct(top_k)
    if average_distance > config['DISTANCE_THRESHOLD']:
        logger.info(
            "The average distance from query is " \
            + f"{average_distance} more than {config['DISTANCE_THRESHOLD']} skip pipeline"
        )
        return
    
    prediction_logs['average_distance'] = average_distance
    logger.info(f"Average distance is {config['DISTANCE_THRESHOLD']} continue pipeline")
    
    # Use in price action
    current_time = datetime.now(tz=timezone.utc)
    timestamp_step = current_time.replace(second=0)

    if interval == "15m":
        # Truncate to nearest 15 minutes
        minute = (timestamp_step.minute // 15) * 15
        timestamp_truncated = timestamp_step.replace(
            minute=minute, second=0, microsecond=0
        )
    hlov_data = market.get_data(
        symbol=symbol,
        target_date=timestamp_truncated,
        interval=interval,
        total_rows=price_action_limit,
    )
    
    ai_content = ai.generate_signal(
        current_time=current_timestamp,
        matched_data=top_k,
        current_vector=target_vec,
        plot_file_name=plot_file_name,
        plot_file_name_price_action=plot_file_name_price_action,
        hlov_data=hlov_data
    )
    
    if ai_content == 0:
        logger.info("Got HOLD position.... end flow")
        return 
    
    if is_open_order is True:
        signal = ai_content['signal']
        market.open_order_flow(
            signal=signal,
            bot_type=ai.bot_type,
        )
        
        confidence = ai_content['confidence']
        reasoning = f"""Reasons
        Chart A: {ai_content['chart_a_analysis']}
        
        Chart B: {ai_content['chart_b_analysis']}
        
        Synthesis: {ai_content['chart_b_analysis']}
        """
        
        message_to_sent = get_message_to_notify_open_order(
            bot_type=ai.bot_type,
            signal=signal,
            confidence=confidence,
            reason=reasoning,
        )
        notifier.sent_message(message_to_sent)
        notifier.sent_message_image(
            message = "Pattern image",
            file_path = plot_file_name,
        )
        notifier.sent_message_image(
            message = "Price action image",
            file_path = plot_file_name_price_action,
        )
        
        
        prediction_logs['signal'] = signal
        prediction_logs['reason'] = reasoning
        db.insert_logs(prediction_logs)
        
    logger.info('End flow to retrive data')
    return None

##############################################################################

def get_message_to_notify_open_order(
        bot_type: str, 
        signal: str,
        confidence: float,
        reason: str,
) -> str:
    return f"""{bot_type} opened contract {signal} with confidense {confidence}.
Reason: {reason}
"""

##############################################################################

def get_min_max_distinct(items, key='distance'):
    if not items:
        return []
    
    # 1. Sort the list by the chosen metric (e.g., distance)
    sorted_items = sorted(items, key=lambda x: x[key])
    
    # 2. Extract Min and Max
    min_item = sorted_items[1]
    max_item = sorted_items[-1]
    
    # 3. Ensure they are distinct (handle lists with 1 item or identical values)
    # We compare the 'time' or unique ID to ensure we don't return the same object twice if the list is short
    if min_item['time'] == max_item['time']:
        return [min_item]
    
    selected = {min_item['time']: min_item, max_item['time']: max_item}
    distinct_elements = list(selected.values())
    
    distance = [abs(item['distance']) for item in distinct_elements]
    average_distance = sum((distance)) / len(distance)
    
    return [min_item, max_item, average_distance]

##############################################################################
