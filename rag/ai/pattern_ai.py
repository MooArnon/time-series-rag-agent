###########
# Imports #
##############################################################################

from logging import Logger
import os

import numpy as np

from.__base_ai import BaseAI
from utils.logger import get_utc_logger

###########
# Classes #
##############################################################################

class PatternAI(BaseAI):
    """
    Base class for AI components.
    """
    def __init__(
            self, 
            symbol: str,
            timeframe: int,
            vector_window: int = 60,
            logger = None
    ) -> None:
        if logger is None:
            self.__logger: Logger = get_utc_logger(
                name=self.__class__.__name__,
                level=os.environ.get('LOG_LEVEL', 'INFO')
            )
        else:
            self.__logger = logger
        super().__init__()
        
        self.__symbol = symbol
        self.__time_frame = timeframe
        self.__vector_window = vector_window
        
    ##############
    # Properties #
    ##########################################################################
    
    @property
    def symbol(self) -> str:
        return self.__symbol
    
    ##########################################################################
    
    @property
    def timeframe(self) -> str:
        return self.__time_frame
    
    ##########################################################################
    
    @property
    def vector_window(self) -> int:
        return self.__vector_window
    
    ##########
    # Lables #
    ##########################################################################
    
    def get_slope(prices):
        """
        Calculates the linear regression slope of normalized prices.
        Returns the 'm' in y = mx + c.
        """
        if len(prices) < 2:
            return 0.0
        y = np.array(prices)
        # Normalize y to % change from start to make slope comparable across price levels
        y_norm = (y - y[0]) / y[0]
        x = np.arange(len(y))
        slope, intercept = np.polyfit(x, y_norm, 1)
        return slope 
    
    ############
    # Features #
    ##########################################################################
    
    def calculate_features(self, data):
        """ A function to calculate features from raw time-series data.
        
        Parameters
        ----------
        
        """
        # We need the last N+1 candles to calculate N log-returns
        if len(data) < self.vector_window + 1:
            self.logger.info("Not enough data for features.")
            return None

        # Get the specific window for feature creation (last 61 points)
        window = data[-(self.vector_window + 1):]
        closes = np.array([d['close'] for d in window])

        # A. Calculate Log Returns
        log_returns = np.diff(np.log(closes))

        # B. Normalize (Z-Score)
        # This makes the "shape" comparable regardless of volatility
        vector = (log_returns - np.mean(log_returns)) / (np.std(log_returns) + 1e-9)

        return {
            'time': window[-1]['dt'],
            'symbol': self.symbol,
            'interval': self.timeframe,
            'embedding': vector.tolist(),
            'close_price': window[-1]['close']
        }
    
    ##########################################################################
    
##############################################################################
