import boto3
import time

# Initialize CloudWatch Client


def send_trading_signal(
        symbol: str, 
        signal_type: str, 
        bot_type: str,
        logger=None,
        region_name: str='ap-southeast-1',
):
    """
    Sends a trading signal metric to CloudWatch.
    
    Parameters
    
    
    :param symbol: e.g., 'BTCUSDT'
    :param signal_type: 'LONG' or 'SHORT'
    :param price: The price at which the signal occurred (optional but useful)
    """
    cloudwatch = boto3.client('cloudwatch', region_name=region_name)
    
    logger.info(f"Sending {signal_type} signal for {symbol}")
    
    try:
        cloudwatch.put_metric_data(
            Namespace='TradingBot',  # Group name for your metrics
            MetricData=[
                {
                    'MetricName': 'SignalCount',
                    'Dimensions': [
                        {
                            'Name': 'Symbol',
                            'Value': symbol
                        },
                        {
                            'Name': 'Action',
                            'Value': signal_type  # This splits data into LONG vs SHORT lines
                        },
                        {
                            'Name': 'BotType',
                            'Value': bot_type  # This splits data into LONG vs SHORT lines
                        },
                    ],
                    'Value': 1,  # Count this as 1 event
                    'Unit': 'Count',
                    'Timestamp': time.time()
                },
            ]
        )
        logger.info("Metric sent successfully.")
    except Exception as e:
        logger.info(f"Error sending metric: {e}")
