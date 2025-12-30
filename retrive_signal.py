###########
# Imports #
##############################################################################

from flow.retrive_flow import retrive_data
from rag.ai.pattern_ai import PatternAI
from rag.db.postgresql_db import PostgreSQLDB
from utils.logger import get_utc_logger 

############
# Stattics #
##############################################################################

symbol = 'ADAUSDT'
vector_window = 60

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

#############
# Functions #
##############################################################################

def main() -> None:
    retrive_data(
        logger=logger,
        ai=ai,
        db=db,
        symbol=symbol,
    )

##############################################################################

if __name__ == "__main__":
    main()

##############################################################################
