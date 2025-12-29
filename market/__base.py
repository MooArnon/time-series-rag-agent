##########
# Import #
##############################################################################

from abc import abstractmethod

###########
# Classes #
##############################################################################

class BaseMarket:
    def __init__(self, logger, symbol):
        pass

    ##########################################################################

    @abstractmethod
    def get_data(self, timeframe: str, period: int, *args, **kawrgs) -> None:
        """The mandatory method to get data from market

        Parameters
        ----------
        timeframe: str
            Timeframe to get data
        period : int
            Number of data point in timeframe
            
        Raises
        ------
        NotImplementedError
        """
        raise NotImplementedError("Child class must implement seach method")
    
    ##########################################################################

##############################################################################
