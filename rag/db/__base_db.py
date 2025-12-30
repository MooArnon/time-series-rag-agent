###########
# Imports #
##############################################################################

from abc import abstractmethod
from logging import Logger
import os

from utils.logger import get_utc_logger

###########
# Classes #
##############################################################################

class BaseDB:
    """Base Database Class for RAG implementations.
    
    Attributes:
    -----------
    connector: 
        Database connector object.
    cursor: 
        Database cursor object. 
    
    Methods:
    --------
    set_connection(data):
        Abstract method to set up the database connection.
    ingest_data(data, *args, **kwargs):
        Abstract method to ingest data into the database.
    retrive_data(data, *args, **kwargs) -> tuple[int]:
        Abstract method to retrieve data from the database.
    retrive_top_k_data(data, top_k, *args, **kwargs) -> tuple[int]:
        Abstract method to retrieve top-k data from the database.
    """
    def __init__(
        self,
        logger: Logger = None,
        *args,
        **kwargs,
    ) -> None:
        if logger is None:
            get_utc_logger(__name__)
    
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
    
    #################
    # Get connector #
    ##########################################################################
    
    @abstractmethod
    def set_connection(self,):
        raise NotImplementedError("set_connection method not implemented.")
    
    ##################
    # Data ingestion #
    ##########################################################################
    
    @abstractmethod
    def ingest_data(self, data, *args, **kwargs):
        raise NotImplementedError("ingest_data method not implemented.")
    
    #############
    # Retrieval #
    ##########################################################################
    
    @abstractmethod
    def retrive_data(self, data, *args, **kwargs) -> tuple[int]:
        raise NotImplementedError("retrive_data method not implemented.")
    
    ##########################################################################
    
    def retrive_top_k_data(self, data, top_k, *args, **kwargs) -> tuple[int]:
        raise NotImplementedError("retrive_top_k_data method not implemented.")
    
    ##########################################################################
    
##############################################################################
