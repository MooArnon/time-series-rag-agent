###########
# Imports #
##############################################################################

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
days = 1

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
        total_candles=5,
    )

##############################################################################

if __name__ == "__main__":
    main()

##############################################################################
