###########
# Imports #
##############################################################################

from argparse import ArgumentParser
import os 

from config.config import config
from flow.retrive_flow import retrive_data
from rag.ai.pattern_ai import PatternAI
from rag.db.postgresql_db import PostgreSQLDB
from market.binance import BinanceMarket
from utils.logger import get_utc_logger 
from utils.discord import DiscordNotify

############
# Stattics #
##############################################################################

# model = "openai/gpt-4o"
# model = "google/gemini-2.5-flash"
# model = "anthropic/claude-3.5-sonnet"

symbol = 'ADAUSDT'
vector_window = 60
top_k = config['PATTERN_LLM_TOP_K']

logger = get_utc_logger(__name__)

discord_alert = DiscordNotify(webhook_url=os.environ["DISCORD_ALERT_WEBHOOK_URL"])
discord_notify = DiscordNotify(webhook_url=os.environ["DISCORD_NOTIFY_WEBHOOK_URL"])

ai = PatternAI(
    symbol=symbol,
    timeframe="15m",
    model=config['LLM_MODEL'],
    vector_window=60,
    logger=logger,
)

db = PostgreSQLDB(
    logger=logger
)

market = BinanceMarket(
    leverage=config['LEVERAGE'],
    logger=logger,
    symbol=symbol,
    notify_object=discord_notify,
    
)

#############
# Functions #
##############################################################################

def main(is_open_order: bool) -> None:
    try:
        retrive_data(
            logger=logger,
            ai=ai,
            db=db,
            market=market,
            symbol=symbol,
            notifier=discord_notify,
            is_open_order=is_open_order,
            top_k=top_k,
        )
    except:
        discord_alert.sent_message("Error at pipeline retrive_signal")
        raise SystemError

##############################################################################

def setup_args() -> None:
    parser  = ArgumentParser(description="Generate Signal using LLM pattern ai")
    parser.add_argument(
        "--is-open-order",
        type=bool,
        help="True if need to use the signal to open order | False if not"
    )
    
    args = parser.parse_args()
    return args

##############################################################################

if __name__ == "__main__":
    args = setup_args()
    logger.info("Running")
    main(is_open_order=args.is_open_order)

##############################################################################
