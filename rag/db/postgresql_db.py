###########
# Imports #
##############################################################################

from functools import wraps
import os

import pandas as pd
import psycopg2

from .__base_db import BaseDB
from utils.logger import get_utc_logger

###########
# Classes #
##############################################################################

class PostgreSQLDB(BaseDB):
    """
    """
    def __init__(
        self,
        db_host: str = os.environ['DB_HOST'],
        db_port: int = os.environ['DB_PORT'],
        db_user: str = os.environ['DB_USER'],
        db_password: str = os.environ['DB_PASSWORD'],
        db_name: str = os.environ['DB_NAME'],
        logger = None
    ) -> None:
        if logger is None:
            self.logger = get_utc_logger('PostgreSQLDB')
        else:
            self.logger = logger
        
        super().__init__()
        
        self.db_host=db_host
        self.db_port=db_port
        self.db_user=db_user
        self.db_password=db_password
        self.db_name=db_name
        
        self.__connector = None
        self.__cursor = None
        
        self.logger.info(f"Initialize database with db_name: {db_name} || user: {db_user}")
    
    ##############
    # Properties #
    ##########################################################################
    
    @property
    def connector(self):
        return self.__connector
    
    ##########################################################################
    
    @property
    def cursor(self):
        return self.__cursor
    
    ###########
    # Wrapper #
    ##########################################################################
    
    def _with_db_connection(func):
        """
        Decorator acts as a wrapper to open/close connections.
        Note: We don't pass 'self' to the outer function, but the wrapper
        receives 'self' when the decorated method is called.
        """
        @wraps(func)
        def wrapper(self, *args, **kwargs):
            # 'self' here refers to the instance of PostgreSQLDB
            self.set_connection(
                db_host=self.db_host,
                db_port=self.db_port,
                db_user=self.db_user,
                db_password=self.db_password,
                db_name=self.db_name
            )
            try:
                return func(self, *args, **kwargs)
            except Exception as e:
                if self.connector:
                    self.connector.rollback()
                self.logger.error(f"Error in {func.__name__}: {e}")
                raise e
            finally:
                self.disconnect()
        return wrapper
    
    ##############
    # Connectors #
    ##########################################################################
    
    def set_connection(self,
        db_host: str = os.environ['DB_HOST'],
        db_port: int = os.environ['DB_PORT'],
        db_user: str = os.environ['DB_USER'],
        db_password: str = os.environ['DB_PASSWORD'],
        db_name: str = os.environ['DB_NAME'],
    ) -> None:
        self.__connector = None
        self.__cursor = None
        conn = psycopg2.connect(
            host = db_host,
            database = db_name,
            user = db_user,
            password = db_password,
            port = db_port,
        )
        
        self.__connector = conn
        self.__cursor = conn.cursor()
    
    ##########################################################################
    
    def disconnect(self):
        """Closes connection and cursor."""
        if self.__cursor:
            self.__cursor.close()
        if self.__connector:
            self.__connector.close()
        # Reset to None to ensure safety
        self.__cursor = None
        self.__connector = None
    
    ##################
    # Data ingestion #
    ##########################################################################
    
    def ingest_feature_label_to_postgresql(
            self, 
            feature_row, 
            label_updates, 
            auto_commit=False,
    ) -> None:
        """
        Ingests the calculated features and updates labels in the PostgreSQL database.
        
        Parameters
        ----------
        feature_row : dict or None
            The output from ai.calculate_features(). 
            Expected keys: 'time', 'symbol', 'interval', 'embedding', 'close_price'.
        label_updates : list[dict]
            The output from ai.calculate_labels().
            Expected keys: 'target_time', 'column', 'value'.
        db_config : dict
            Connection details: {'host', 'database', 'user', 'password', 'port'}
        """
        
        if not feature_row:
            self.logger.info("Skipping ingestion: No feature row generated.")
            return
        # 2. Ingest Feature (The "Current" Pattern)
        # Use ON CONFLICT DO NOTHING to prevent crashing if run twice for same time
        insert_query = """
            INSERT INTO market_pattern (time, symbol, interval, embedding, close_price)
            VALUES (%s, %s, %s, %s, %s)
            ON CONFLICT (time, symbol, interval) DO NOTHING;
        """
        self.cursor.execute(insert_query, (
            feature_row['time'],
            feature_row['symbol'],
            feature_row['interval'],
            feature_row['embedding'],
            feature_row['close_price']
        ))
        
        # 3. Ingest Labels (Backfill "Past" Patterns)
        if label_updates:
            
            # Prepare a generic update query
            # We explicitly allow specific columns to avoid SQL injection risks
            allowed_columns = {'next_return', 'next_slope_3', 'next_slope_5'}
            
            for update in label_updates:
                col = update['column']
                if col not in allowed_columns:
                    self.logger.info(f"Warning: Attempted to update invalid column '{col}'")
                    continue
                
                # Safe because we checked col against allowed_columns
                update_query = f"UPDATE market_pattern SET {col} = %s WHERE time = %s AND symbol = %s"
                
                self.cursor.execute(update_query, (
                    update['value'],
                    update['target_time'],
                    feature_row['symbol']
                ))
                
        if auto_commit is True:           # <--- Only commit if requested
            self.connector.commit()
            self.logger.info(f"Success! Ingested feature for {feature_row['time']}...")

    #############
    # Retrieval #
    ##########################################################################
    
    @_with_db_connection
    def find_similar_patterns(
            self,
            symbol: str,
            target_time: str,
            time_column: str,
            vector_column: str,
            table_name="market_pattern", 
            top_k=5,
    ) -> list:
        """
        1. Fetches the embedding for the specific target_time.
        2. Searches the DB for the Top K most similar historical patterns.
        3. Returns the neighbors with their Distances.
        """
        
        # -------------------------------------------------------------------
        # A. Fetch the Source Embedding
        # -------------------------------------------------------------------
        # Note: We use f-strings for identifiers (table/col) and casting, 
        # but be careful with values.
        raw_sql = f"""
            SELECT {vector_column} 
            FROM {table_name} 
            WHERE symbol = '{symbol}' 
              AND {time_column} = '{target_time}'::timestamptz
        """
        
        self.cursor.execute(raw_sql)
        row = self.cursor.fetchone()
        
        if not row:
            self.logger.warning(f"⚠️ No embedding found for {target_time}. (Did the ETL job run?)")
            return []
            
        target_vec = row[0]
        
        # Ensure vector is a Python list (Psycopg2 needs list to convert to Postgres Array)
        if hasattr(target_vec, 'tolist'):
            target_vec = target_vec.tolist()
        
        # -------------------------------------------------------------------
        # B. RAG Search (Vector Similarity)
        # -------------------------------------------------------------------
        # Using %s for parameters is safer and standard for psycopg2
        query_neighbors = f"""
            SELECT 
                {time_column}, 
                next_return, 
                next_slope_3, 
                next_slope_5,
                {vector_column} <=> %s as distance,
                {vector_column}
            FROM {table_name}
            WHERE symbol = '{symbol}'
            AND {vector_column} IS NOT NULL
            AND {time_column} < %s::timestamptz
            ORDER BY distance ASC
            LIMIT %s
        """
        
        self.cursor.execute(
            query_neighbors, 
            (target_vec, target_time, top_k)  # <--- Pass params as tuple here
        )
        
        neighbors = self.cursor.fetchall() # Returns list of tuples: [(time, distance), ...]
        
        # -------------------------------------------------------------------
        # C. Format Results
        # -------------------------------------------------------------------
        results = []
        for n in neighbors:
            results.append({
                "time": str(n[0]),
                # Handle None values for recent data that might not have labels yet
                "next_return": float(n[1]) if n[1] is not None else 0.0,
                "next_slope_3": float(n[2]) if n[2] is not None else 0.0,
                "next_slope_5": float(n[3]) if n[3] is not None else 0.0,
                "distance": float(n[4]),
                "similarity_score": 1 - float(n[4]),
                "embedding": n[5]
            })
        return results, target_vec

    #############
    # Utilities #
    ##########################################################################
    
    @staticmethod
    def snap_to_window(timestamp, window):
        """
        Rounds a timestamp DOWN to the nearest window interval.
        Useful for aligning live event times with candle open times.
        
        Args:
            timestamp (str or pd.Timestamp): The raw time (e.g., '12:11:45')
            window (str): The interval (e.g., '15m', '1h', '4h', '1d')
            
        Returns:
            str: The snapped timestamp string (e.g., '12:00:00')
        """
        # 1. Convert to Pandas Timestamp
        ts = pd.to_datetime(timestamp)
        
        # 2. Normalize 'm' to 'min' because Pandas expects 'min' for minutes
        window = window.replace('m', 'min') \
            if 'min' not in window \
                and 'm' in window \
                    else window
        
        # 3. Floor the time
        snapped_ts = ts.floor(window)
        
        return str(snapped_ts)
    
    ##########################################################################
    
##############################################################################
