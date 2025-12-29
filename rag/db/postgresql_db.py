###########
# Imports #
##############################################################################

from functools import wraps
import os

import psycopg2

from .__base_db import BaseDB

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
        super().__init__()
        
        self.__connector = None
        self.__cursor = None
        
        self.set_connection(
            db_host=db_host,
            db_port=db_port,
            db_user=db_user,
            db_password=db_password,
            db_name=db_name
        )
    
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
            self.connect()
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
        db_host: str,
        db_port: int,
        db_user: str,
        db_password: str,
        db_name: str,
    ) -> None:
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
    
    def ingest_data(self, *args, **kwargs):
        raise NotImplementedError("ingest_data method not implemented.")
    
    #############
    # Retrieval #
    ##########################################################################
    
    def retrive_data(self, *args, **kwargs) -> tuple[int]:
        raise NotImplementedError("retrive_data method not implemented.")
    
    ##########################################################################
    
    def retrive_top_k_data(self, top_k, *args, **kwargs) -> tuple[int]:
        raise NotImplementedError("retrive_top_k_data method not implemented.")
    
    ##########################################################################
    
##############################################################################
