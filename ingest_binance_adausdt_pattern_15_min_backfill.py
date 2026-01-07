###########
# Imports #
##############################################################################

import os
import time
import schedule
import subprocess
import traceback

from config.config import config
from flow.ingestion_flow import run_backfill_bulk_ingest_to_postgresql
from rag.ai.pattern_ai import PatternAI
from rag.db.postgresql_db import PostgreSQLDB
from market.binance import BinanceMarket
from utils.logger import get_utc_logger 
from utils.discord import DiscordNotify

############
# Stattics #
##############################################################################

symbol = 'ADAUSDT'
vector_window = 60
# days = 365*3
# days = 1
total_candles = 10

logger = get_utc_logger(__name__)

ai = PatternAI(
    symbol=symbol,
    timeframe="15m",
    vector_window=60,
    logger=logger,
    model="MOCKUP",
)

db = PostgreSQLDB(
    logger=logger
)

market = BinanceMarket(
    leverage=config['LEVERAGE'],
    logger=logger,
    symbol=symbol,
)

discord_alert = DiscordNotify(webhook_url=os.environ["DISCORD_ALERT_WEBHOOK_URL"])
discord_notify = DiscordNotify(webhook_url=os.environ["DISCORD_NOTIFY_WEBHOOK_URL"])

#############
# Functions #
##############################################################################

def main() -> None:
    try:
        time.sleep(5)
        run_backfill_bulk_ingest_to_postgresql(
            logger=logger,
            ai=ai,
            db=db,
            market=market,
            symbol=symbol,
            total_candles=total_candles,
        )
    except Exception as e:
        discord_alert.sent_message(
            message=f":warning: Error running live: {e}",
            username="ingest_binance_data"
        )
        logger.error(f"Error running live: {e}")
        traceback.print_exc()
    
    subprocess.run(["python", "retrive_signal.py", "--is-open-order", "True"])

##############################################################################

# Schedule it at specific minutes
schedule.every().hour.at(":01").do(main)
schedule.every().hour.at(":31").do(main)

if __name__ == "__main__":
    logger.info("Running ingest_binance_adausdt_pattern_15_min.py")
    logger.info("Wating for schedule")
    while True:
        schedule.run_pending()
        time.sleep(1)

##############################################################################
