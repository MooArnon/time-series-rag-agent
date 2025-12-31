###########
# Imports #
##############################################################################

from datetime import datetime
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
) -> None:
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
    
    # 2. Get data at the point
    top_k, target_vec = db.find_similar_patterns(
        symbol=symbol,
        target_time=current_timestamp_tunc,
        time_column=time_column,
        vector_column=vector_column,
        table_name=table_name, 
        top_k=top_k,
    )
    
    logger.info(f"Generating plot")
    
    ai_content = ai.generate_signal(
        current_time=current_timestamp,
        matched_data=top_k,
        current_vector=target_vec,
        plot_file_name=plot_file_name
        
    )
    
    if is_open_order is True:
        signal = ai_content['signal']
        market.open_order_flow(
            signal=signal,
            bot_type=ai.bot_type,
        )
        
        confidence = ai_content['confidence']
        reasoning = ai_content['reasoning']
        
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
