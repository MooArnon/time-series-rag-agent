###########
# Imports #
##############################################################################

import time
import schedule

from config.config import config
from flow.ingestion_flow import run_backfill_bulk_ingest_to_postgresql
from rag.ai.pattern_ai import PatternAI
from rag.db.postgresql_db import PostgreSQLDB
from market.binance import BinanceMarket
from utils.logger import get_utc_logger 

############
# Stattics #
##############################################################################

symbol = 'ADAUSDT'
vector_window = 60
# days = 365*3
# days = 1
total_candles = 5

logger = get_utc_logger(__name__)

ai = PatternAI(
    symbol=symbol,
    timeframe="15m",
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
)

#############
# Functions #
##############################################################################

def main() -> None:
    run_backfill_bulk_ingest_to_postgresql(
        logger=logger,
        ai=ai,
        db=db,
        market=market,
        symbol=symbol,
        total_candles=total_candles,
    )

##############################################################################

# Schedule it at specific minutes
schedule.every().hour.at(":00").do(main)
schedule.every().hour.at(":15").do(main)
schedule.every().hour.at(":30").do(main)
schedule.every().hour.at(":45").do(main)

if __name__ == "__main__":
    while True:
        time.sleep(10)
        schedule.run_pending()
        time.sleep(1)

##############################################################################
