import logging
import time

def get_utc_logger(name: str, level=logging.INFO) -> logging.Logger:
    """
    Creates and configures a Python logger with timestamps in UTC.

    Parameters
    ----------
    name : str
        The name of the logger.
    level : int, optional
        The logging level (e.g., logging.DEBUG, logging.INFO).
        The default is `logging.INFO`.

    Returns
    -------
    logging.Logger
        The configured logger instance.
    """
    print(f"Creating UTC logger with level: {level}")
    # Create a new logger instance
    logger = logging.getLogger(name)
    logger.setLevel(level)

    # Prevent the logger from adding multiple handlers if the function is called
    # more than once in the same script.
    if not logger.handlers:
        # Create a console handler to output logs to the standard output
        handler = logging.StreamHandler()

        # Define the log format string. %(asctime)s will contain the timestamp.
        # Other useful formats include %(levelname)s, %(name)s, and %(message)s.
        log_format = '%(asctime)s - %(name)s - %(levelname)s - %(message)s'
        formatter = logging.Formatter(log_format)

        # IMPORTANT: Set the converter to use gmtime (UTC) instead of the default local time.
        formatter.converter = time.gmtime
        handler.setFormatter(formatter)

        # Add the handler to the logger
        logger.addHandler(handler)

    return logger
