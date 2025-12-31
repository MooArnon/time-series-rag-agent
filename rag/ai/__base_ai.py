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

class BaseAI:
    """
    Base class for AI components.
    """
    def __init__(self) -> None:
        self.__logger: Logger = get_utc_logger(
            name=self.__class__.__name__,
            level=os.environ.get('LOG_LEVEL', 'INFO')
        )
    
    ##############
    # Properties #
    ##########################################################################
    
    @property
    def logger(self) -> Logger:
        return self.__logger
    
    ##########################################################################
    
    @property
    def get_signal_mapper(self) -> dict:
        return {
            -1: "SHORT",
            0: "HOLD",
            1: "LONG",
        }
    
    ###########
    # Methods #
    ##########################################################################
    
    @abstractmethod
    def generate_response(self, *args, **kwargs) -> str:
        """
        Abstract method to generate a response based on input parameters.
        Must be implemented by subclasses.
        """
        pass
    
    ##########################################################################
    
##############################################################################
    