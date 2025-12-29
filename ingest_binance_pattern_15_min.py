###########
# Imports #
##############################################################################

from flow.ingestion_flow import ingest_to_postgresql
from rag.ai.pattern_ai import PatternAI
from rag.db.postgresql_db import PostgreSQLDB
from utils.logger import get_utc_logger 

############
# Stattics #
##############################################################################

logger = get_utc_logger(__name__)

ai = PatternAI(
    symbol="ADAUSDT",
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
    ingest_to_postgresql(
        logger=logger,
        ai=ai,
        db=db,
    )

##############################################################################

if __name__ == "__main__":
    main()

##############################################################################
