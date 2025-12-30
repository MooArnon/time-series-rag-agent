##########
# Import #
##############################################################################

import time
import os
import datetime
import math
import traceback
import requests
import hmac
import hashlib
from urllib.parse import urlencode

import pandas as pd
from binance.client import Client
from binance.exceptions import BinanceAPIException

from .__base import BaseMarket
from utils.cloud_watch import send_trading_signal

###########
# Classes #
##############################################################################

class BinanceMarket(BaseMarket):
    
    kline_columns = [
        'timestamp', 'open', 'high', 'low', 'close', 'volume', 
        'close_time', 'quote_asset_volume', 'number_of_trades', 
        'taker_buy_base', 'taker_buy_quote', 'ignore'
    ]

    base_url_usdm_market = "https://fapi.binance.com/fapi/v1/klines"

    def __init__(
            self,
            leverage: int,
            logger=None,
            symbol: str = "ADAUSDT",
            tp_percent=100.0,
            sl_percent=2.0,
            api_key: str = os.environ["BINANCE_API_KEY"],
            secret_key: str = os.environ["BINANCE_SECRET_KEY"],
            notify_object: object = None,
    ) -> None:
        super().__init__(logger=logger, symbol=symbol)
        
        self.client = Client(api_key, secret_key)
        
        # State variable to track if a contract is currently open (Decision 2)
        self.has_open_contract = True
        
        self.symbol = symbol
        self.tp_percent = tp_percent
        self.sl_percent = sl_percent
        self.leverage = leverage
        self.symbol_filters = {}
        self.notify_object = notify_object
        
        # Signal definitions
        self.SIGNALS = ["HOLD", "SHORT", "LONG"]
        
        if logger:
            self.logger = logger
        else:
            from utils.logger import get_utc_logger 
            self.logger = get_utc_logger(
                name=__name__,
            )
            
        self.__load_exchange_info()
        
        self.set_leverage(
            symbol=self.symbol,
            leverage=self.leverage,
        )
        
        self.base_url_usdm_market = "https://fapi.binance.com/fapi/v1/klines"
        
        self.logger.info("Press Ctrl+C to stop the bot.")

    ##########################################################################
    
    def set_leverage(self, symbol: str, leverage: int) -> dict:
        """Set leverage  of symbol

        Parameters
        ----------
        client : Client
            Client for binance
        symbol : str
            Target symbol
        leverage : int
            Leverage to adjust

        Returns
        -------
        dict
            respond
        """
        response = self.client.futures_change_leverage(
            symbol=symbol, 
            leverage=leverage,
        )
        return response
    
    ##########################################################################

    def fetch_batch(self, symbol, interval, end_time_ms, limit):
        """Helper function to fetch a single batch of data"""
        params = {
            "symbol": symbol.upper(),
            "interval": interval,
            "limit": limit,
            "endTime": end_time_ms
        }
        try:
            response = requests.get("https://fapi.binance.com/fapi/v1/klines", params=params)
            response.raise_for_status() # Check for HTTP errors
            return response.json()
        except Exception as e:
            print(f"Request failed: {e}")
            return []

    ##########################################################################
    
    def get_data(
            self, 
            symbol, 
            target_date, 
            total_rows, 
            interval,
            batch_limit = 500,
    ) -> pd.DataFrame:
        all_kline_data = []
        
        # Start from the target date (converted to milliseconds)
        current_end_time = int(target_date.timestamp() * 1000)
        
        rows_fetched = 0
        
        print(f"Fetching {total_rows} rows for {symbol}...")
        
        while rows_fetched < total_rows:
            # Calculate how many rows we still need for this batch
            remaining = total_rows - rows_fetched
            # Don't ask for more than the API allows (BATCH_LIMIT)
            limit = min(remaining, batch_limit)
            
            batch_data = self.fetch_batch(symbol, interval, current_end_time, limit)
            
            if not batch_data:
                print("No more data received or error occurred.")
                break
                
            # Binance returns data [oldest -> newest]. 
            # Since we are going backward in time, we prepend this batch to our main list.
            all_kline_data = batch_data + all_kline_data
            
            rows_fetched += len(batch_data)
            print(f"Fetched {len(batch_data)} rows. Total: {rows_fetched}/{total_rows}")
            
            # IMPORTANT: Set the endTime for the next loop to be just BEFORE
            # the oldest candle we just fetched.
            oldest_timestamp_in_batch = batch_data[0][0]
            current_end_time = oldest_timestamp_in_batch - 1
            
            # Sleep briefly to avoid hitting Binance rate limits (IP ban risk)
            time.sleep(0.1)

        df = pd.DataFrame(all_kline_data, columns=self.kline_columns)
        
        # Format timestamp
        df['timestamp'] = pd.to_datetime(df['timestamp'], unit='ms')
        return df
        
    ##########################################################################
    
    def open_order_flow(self, signal: str, bot_type: str) -> None:
        """
        """
        self.logger.info("Open order flow initialized")
        if signal == "HOLD":
            self.logger.info("Decision: Signal is HOLD. Returning to wait step.")
            return None
        
        try:
            
            # Check port attributes
            aviable_balance = self.available_balance
            self.logger.info(f"Available Balance: {aviable_balance} USDT")
            self.logger.info(f"Current State: Open Contract = {self.has_open_contract}")
            self.check_position_opening(symbol=self.symbol)
            self.logger.info(f"[{time.strftime('%H:%M:%S')}] Signal Received: {signal}")
            
            if self.has_open_contract is True:
                self.logger.info(
                    "Decision: Open contract EXISTS. Cannot open new position. Returning to wait step."
                )
                return None
            
            # Open SHORT or LONG Position
            # This path is taken only if (signal != HOLD) AND (has_open_contract == False)
            if (signal in ["SHORT", "LONG"]) \
                and (self.has_open_contract is False):
                    self.create_order(
                        symbol=self.symbol,
                        position_type=signal,
                        tp_percent=self.tp_percent,
                        sl_percent=self.sl_percent,
                        leverage=self.leverage,
                        bot_type=bot_type
                    )
            
        except Exception as e:
            self.logger.info(f"An unexpected error occurred: {e}")
    
    ##########################################################################
    
    @property
    def available_balance(self, asset: str = "USDT") -> float:
        """Return the amount balance for asset `asset`

        Parameters
        ----------
        asset : str, optional
            Target asset, by default "USDT"

        Returns
        -------
        float
            Float of available balance
        """
        future_account_asset = self.client.futures_account()['assets']
        return float(
            next(
                item['availableBalance'] \
                    for item in future_account_asset \
                        if item['asset'] == asset
            )
        )
    
    ##########################################################################
    
    def check_position_opening(
            self,
            symbol: str,
    ) -> dict:
        """
        Check open positions and add stop loss

        Parameters
        ----------
        symbol : str
            Target symbol
        """
        # Get the current position for the symbol
        positions_info = self.client.futures_position_information(symbol=symbol)
        if positions_info == []:
            self.logger.info(f"No position information for {symbol}")
            self.has_open_contract = False
            return None
        position_amt = 0.0
        entry_price = 0.0
        positions_info = positions_info[0]
        
        position_amt = float(positions_info['positionAmt'])
        entry_price = float(positions_info['entryPrice'])

        # If no open position, exit the function
        if position_amt == 0.0:
            self.logger.info(f"No open position for {symbol}")
            self.has_open_contract = False
            return

        self.logger.info(
            f"Open position for {symbol}: {position_amt} at entry price {entry_price}"
        )
        
        if position_amt > 0:
            position = "LONG"
        elif position_amt < 0:
            position = 'SHORT'
        elif position_amt == 0:
            position = 'HOLD'
        else:
            self.logger.info(f"Position is wrong with position_amt = {position_amt}")
            raise SystemError
        
        if position_amt != 0:
            self.has_open_contract = True

        return position
    
    ##########################################################################
    
    def create_order(
            self, 
            symbol: str, 
            position_type: str, 
            tp_percent: float, 
            sl_percent: float,
            leverage: int,
            bot_type: str,
    ) -> None:
        """Create order using current price.
        
        Cancel all open orders and positions, then the create new one.
        This method also create stop-loss and take-profit.

        Parameters
        ----------
        symbol : str
            Target symbol to crate order
        position_type : str
            Type of position, `LONG` or `SHORT`
        tp_percent : float
            Percentage for take profit, calculated from current
            price and leverage
        sl_percent : float
            Percentage for stop loss, calculated from current
            price and leverage
        leverage : int
            Multiplier open unit

        Raises
        ------
        ValueError
            If position type is not correct.
        """
        # Clear all past orders and position
        # Delay 5 second to ensure the respond from server
        self.cancel_all_open_orders(symbol)
        self.cancel_all_algo_orders(symbol)
        time.sleep(3)
        
        try:
            if position_type not in ('LONG', 'SHORT'):
                raise ValueError(f"{position_type} does not suitable")
            
            # Get available USDT balance
            # Use only 95 % of balance to prevent insufficient margin
            usdt_balance = self.available_balance * 0.95
            self.logger.info(f"Available USDT balance: {usdt_balance}")

            # Calculate the maximum quantity based on USDT balance
            # Also round the precision
            quantity = self.calculate_quantity(symbol, usdt_balance, leverage)
            quantity = self.round_to_precision(quantity)
            
            # Get current price
            ticker = self.client.futures_symbol_ticker(symbol=symbol)
            price = float(ticker['price'])
            price = self.round_to_precision(price)
            self.logger.info(f"Maximum quantity to buy: {quantity}")
            
            # Calculate TP and SL prices
            take_profit_price, stop_loss_price = self.calculate_tp_sl_prices(
                entry_price=price,
                tp_percent=tp_percent,
                sl_percent=sl_percent,
                position_type=position_type,
                leverage = leverage,
            )
            self.logger.info(f"Take Profit price: {take_profit_price}")
            self.logger.info(f"Stop Loss price: {stop_loss_price}")

            quantity = self.round_quantity(symbol,quantity)
            price = self.round_price(symbol,price)
            
            # Place a Market Order (long or short based on position_type)
            if position_type == 'LONG':
                market_order = self.client.futures_create_order(
                    symbol=symbol,
                    side=Client.SIDE_BUY,
                    type=Client.ORDER_TYPE_LIMIT,
                    quantity=quantity,
                    price=price,
                    timeInForce=Client.TIME_IN_FORCE_GTC,
                )
                send_trading_signal(symbol, 'LONG', bot_type, logger=self.logger)
                self.notify_object.sent_message("Placed LONG order")
            elif position_type == 'SHORT':
                market_order = self.client.futures_create_order(
                    symbol=symbol,
                    side=Client.SIDE_SELL,
                    type=Client.ORDER_TYPE_LIMIT,
                    quantity=quantity,
                    price=price,
                    timeInForce=Client.TIME_IN_FORCE_GTC,
                )
                send_trading_signal(symbol, 'SHORT', bot_type, logger=self.logger)
                self.notify_object.sent_message("Placed SHORT order")
            self.logger.info(f"Market order placed: {market_order}")
            
            tp_order = self.client.futures_create_order(
                symbol=symbol,
                side=Client.SIDE_SELL if position_type == 'LONG' else Client.SIDE_BUY,
                type='TAKE_PROFIT_MARKET',
                stopPrice=take_profit_price, 
                reduceOnly=True,             # Use this instead of closePosition
                quantity=quantity            # You must pass the quantity explicitly
            )
            self.logger.info(f"Take Profit order placed: {tp_order}")

            # Place a Stop Loss order
            sl_order = self.client.futures_create_order(
                symbol=symbol,
                side=Client.SIDE_SELL if position_type == 'LONG' else Client.SIDE_BUY,
                type='STOP_MARKET',
                stopPrice=stop_loss_price,  # Note: Use 'stopPrice', not 'triggerprice'
                reduceOnly=True,            # Safer than closePosition for immediate placement
                quantity=quantity           # You must pass the explicit quantity here
            )
            self.logger.info(f"Stop Loss order placed: {sl_order}")
            
        except Exception as e:
            self.logger.info(traceback.format_exc())
            self.logger.info(f"Error placing orders: {e}")
    
    ##########################################################################
    # Manage position #
    ###################
    
    def close_all_positions(self, symbol: str):
        """Force close all positions

        Parameters
        ----------
        symbol : str
            Target asset
        """
        try:
            # Get the current positions information
            positions_info = self.client.futures_position_information()
            
            # Loop all position found
            for position in positions_info:
                if position['symbol'] == symbol:
                    
                    # Get the position amount (positive for long, negative for short)
                    position_amt = float(position['positionAmt']) 
                    
                    # Close long position by selling
                    if position_amt > 0:
                        self.logger.info(
                            f"Closing LONG position for {symbol}, Amount: {position_amt}"
                        )
                        close_order = self.client.futures_create_order(
                            symbol=symbol,
                            side=Client.SIDE_SELL,
                            type=Client.ORDER_TYPE_MARKET,
                            quantity=abs(position_amt)
                        )
                        self.logger.info(
                            f"Closed LONG position: {close_order}"
                        )
                    
                    # Close short position by buying
                    elif position_amt < 0:
                        self.logger.info(
                            f"Closing SHORT position for {symbol}, Amount: {position_amt}"
                        )
                        close_order = self.client.futures_create_order(
                            symbol=symbol,
                            side=Client.SIDE_BUY,
                            type=Client.ORDER_TYPE_MARKET,
                            quantity=abs(position_amt)  
                        )
                        self.logger.info(
                            f"Closed SHORT position: {close_order}"
                        )
                        
                    else:
                        self.logger.info(f"No open positions for {symbol}")
                        
        except BinanceAPIException as e:
            self.logger.info(f"Error closing position: {e}")
            traceback.print_exc()
        except Exception as e:
            self.logger.info(f"Unexpected error: {e}")
            traceback.print_exc()
    
    ##########################################################################
    
    def cancel_all_algo_orders(self, symbol):
        self.logger.info(f"--- Checking for stuck ALGO orders on {symbol} ---")
        
        # 1. Fetch the open Algo orders specifically
        # The data you pasted comes from this endpoint
        open_algos = self.client.futures_get_open_algo_orders(symbol=symbol)
        
        # Handle response structure (sometimes it's a dict {'orders': [...]}, sometimes a list)
        algo_list = open_algos.get('orders', []) if isinstance(open_algos, dict) else open_algos

        if not algo_list:
            self.logger.info("No active Algo orders found.")
            return

        # 2. Iterate and cancel each one by algoId
        for order in algo_list:
            algo_id = order['algoId']
            self.logger.info(f"Cancelling Algo Order: {algo_id} ({order['orderType']})")
            
            try:
                self.client.futures_cancel_algo_order(
                    symbol=symbol,
                    algoId=algo_id
                )
                self.logger.info(f" -> Success: {algo_id}")
            except Exception as e:
                self.logger.info(f" -> Failed to cancel {algo_id}: {e}")

    
    ##########################################################################
    
    def profit_trailing(
            self,
            symbol: str,
            trailing_levels: list = [
                (1, 0.5),
                (3, 1.5), 
                (5, 3),
                (7, 5),
                (9, 7),
                (11, 9),
                (15, 12),
                (20, 18),
                (25, 20),
                (30, 25),
                (40, 35),
                (50, 45),
                (60, 55),
                (70, 65),
                (80, 75),
                (90, 85),
            ]
        ) -> None:
        """
        Dynamically adjust stop-loss based on current unrealized profit thresholds (ROI-based).
        
        The 'stop_loss_level' (the second value in the trailing_levels tuple) is treated 
        as a percentage of **ROI (Return on Investment)**, not a percentage of the price.
        The required price movement is calculated by dividing this ROI percentage by the leverage.

        Parameters
        ----------
        symbol : str
            Futures symbol (e.g., "BTCUSDT")
        trailing_levels : list of (float, float)
            Pairs of (profit_threshold%, new_stop_loss%), e.g. [(3, 1.5), (5, 3)] means:
                - If position is +3% in ROI profit, move stop-loss to lock in +1.5% ROI.
                - If position is +5% in ROI profit, move stop-loss to lock in +3% ROI.
        """
        try:
            # 1. Get position info
            # NOTE: In a real system, you should handle possible exceptions here (e.g., network error)
            position_info = self.client.futures_position_information(symbol=symbol)
            if (position_info is None) or (position_info == []) or (position_info[0]['positionAmt'] == '0'):
                self.logger.info(f"No position information for {symbol}. Exiting trailing function.")
                return
            roi = self.get_roi(symbol, position_info)  # ROI is returned in percentage

            position_amt = float(position_info[0]['positionAmt'])
            entry_price = float(position_info[0]['entryPrice'])
            leverage = self.leverage

            # If no open position or entry_price is 0, do nothing
            if position_amt == 0.0 or entry_price == 0.0:
                self.logger.info(f"No open position for {symbol} or entry_price=0. Skipping.")
                return

            # 2. Determine if LONG or SHORT
            side = "LONG" if position_amt > 0 else "SHORT"

            # 3. Get current mark price for logging
            mark_price_data = self.client.futures_mark_price(symbol=symbol)
            mark_price = float(mark_price_data['markPrice'])

            # Use roi as the current profit percentage
            profit_pct = roi  # already in %

            self.logger.info(
                f"Position side: {side}, Entry: {entry_price}, Mark: {mark_price}, "
                f"Leverage: {leverage}x, Unrealized PnL% (ROI) ~ {profit_pct:.2f}"
            )

            if profit_pct <= 0:
                self.logger.info("Currently not in profit, skipping trailing stop update.")
                return

            # 4. Check open orders for an existing stop-loss order
            open_orders = self.client.futures_get_open_orders(symbol=symbol)
            current_sl_order = None
            for order in open_orders:
                # Assuming STOP_MARKET is the desired type for stop-loss
                if order.get('type') in ['STOP_MARKET', 'STOP'] and order.get('reduceOnly') == True:
                    current_sl_order = order
                    break

            # 5. Determine which trailing level to trigger
            # Sort levels by trigger percentage (just in case the input list is unsorted)
            trailing_levels_sorted = sorted(trailing_levels, key=lambda x: x[0])
            triggered_level = None
            
            # Find the highest level that the current profit has reached
            for (profit_trigger, stop_loss_level) in trailing_levels_sorted:
                if profit_pct >= profit_trigger:
                    triggered_level = (profit_trigger, stop_loss_level)
                else:
                    break

            if not triggered_level:
                self.logger.info(
                    f"Current profit {profit_pct:.2f}% has not reached the lowest threshold "
                    f"{trailing_levels_sorted[0][0]}%."
                )
                return

            # 6. Compute the new stop-loss price based on the triggered trailing level.
            stop_loss_target_roi = triggered_level[1]  # ROI percentage to lock in

            # Price movement required to achieve stop_loss_target_roi is: (ROI / Leverage)
            price_movement_pct = stop_loss_target_roi / leverage
            
            if side == "LONG":
                # For LONG: new_sl_price = entry_price * (1 + (price_movement_pct/100))
                new_sl_price = entry_price * (1 + (price_movement_pct / 100))
            else:
                # For SHORT: new_sl_price = entry_price * (1 - (price_movement_pct/100))
                new_sl_price = entry_price * (1 - (price_movement_pct / 100))
            
            # 7. Round the computed stop-loss price to API precision.
            new_sl_price = self.round_down_to_precision(new_sl_price)

            # --- Start Lock-in and Tolerance Checks ---

            # 8. Check for existing order and prevent moving SL backward (reducing locked profit).
            # This is critical for stepped trailing.
            if current_sl_order:
                current_sl_price = float(current_sl_order.get('stopPrice', 0))
                
                # LONG: New SL must be greater than or equal to current SL
                if side == "LONG" and new_sl_price < current_sl_price:
                    self.logger.info(
                        f"New SL price {new_sl_price} is lower than current SL {current_sl_price}. "
                        "No update necessary as it would reduce locked profit."
                    )
                    return
                # SHORT: New SL must be less than or equal to current SL
                elif side == "SHORT" and new_sl_price > current_sl_price:
                    self.logger.info(
                        f"New SL price {new_sl_price} is higher than current SL {current_sl_price}. "
                        "No update necessary as it would reduce locked profit."
                    )
                    return

            self.logger.info(
                f"Triggered level {triggered_level[0]}% => Adjusting SL to lock in "
                f"{stop_loss_target_roi}% ROI. New SL Price ~ {new_sl_price}"
            )

            # 9. If an existing SL order is already within the correct range, do nothing.
            # Using a small tolerance to prevent excessive order cancellations/placements.
            tolerance = 0.0001
            if current_sl_order:
                current_sl_price = float(current_sl_order.get('stopPrice', 0))
                if abs(current_sl_price - new_sl_price) < tolerance:
                    self.logger.info(
                        f"Existing SL order at {current_sl_price} is within tolerance of new SL {new_sl_price}. "
                        "No update necessary."
                    )
                    return

            # --- End Lock-in and Tolerance Checks ---

            # 10. Cancel any existing stop-loss order if we determined a new one is needed.
            if current_sl_order:
                try:
                    cancel_response = self.client.futures_cancel_order(
                        symbol=symbol,
                        orderId=current_sl_order['orderId']
                    )
                    self.logger.info(f"Canceled existing stop-loss order: {cancel_response}")
                except BinanceAPIException as e:
                    self.logger.warning(f"Could not cancel existing stop-loss order {current_sl_order['orderId']}: {e}")
                    # Continue attempting to place the new order, as cancellation failure isn't always fatal

            # 11. Place the updated stop-loss order.
            sl_side = Client.SIDE_SELL if side == "LONG" else Client.SIDE_BUY
            
            # CRITICAL FIX: Round position size (quantity) to the correct precision
            position_size = self.round_down_to_precision(abs(position_amt), is_quantity=True)

            try:
                new_stop_loss_order = self.client.futures_create_order(
                    symbol=symbol,
                    side=sl_side,
                    type=Client.FUTURE_ORDER_TYPE_STOP_MARKET,
                    stopPrice=new_sl_price,
                    quantity=position_size, # Use the rounded quantity
                    reduceOnly=True,
                    # timeInForce=Client.TIME_IN_FORCE_GTC
                )
                self.logger.info(f"Updated stop-loss order placed: {new_stop_loss_order}")
            except BinanceAPIException as e:
                # Log the error but avoid calling check_and_create_stop_loss here, 
                # as this function is for trailing, not initial SL creation.
                if "Order would immediately trigger" in str(e):
                    self.logger.error(
                        f"Stop-loss at {new_sl_price} is too close or above current price. "
                        "This can happen due to high volatility. No order placed."
                    )
                else:
                    self.logger.error(f"Error placing updated stop-loss: {e}")
                traceback.print_exc()
            except Exception as e:
                self.logger.error(f"Unexpected error during order placement: {e}")
                traceback.print_exc()

        except Exception as e:
            self.logger.error(f"Critical error in profit_trailing execution: {e}")
            traceback.print_exc()
            
        # Example of how to call the function for testing
        # manager = TrailingStopManager()
        # manager.profit_trailing('BTCUSDT')
    ##########################################################################
    
    def get_roi(self, symbol: str, all_positions_data: list) -> float:
        """
        Calculate the percentage ROI for the open futures position on the given symbol.

        The formula calculates the Return on Initial Margin, which includes leverage.
        ROI = (Unrealized PnL / Initial Margin) * 100 
        (This is already calculated by Binance as 'unRealizedProfit' and 'positionInitialMargin'.)

        We will use the direct price change method you implemented, 
        but ensure we find the correct position dictionary first.

        Parameters
        ----------
        symbol : str
            Futures symbol (e.g., "ADAUSDT")
        all_positions_data : list
            The full list of position information returned by client.get_position_risk().

        Returns
        -------
        float
            The ROI in percentage. Returns 0.0 if there is no open position.
        """
        
        # 1. Find the specific position dictionary for the target symbol
        position_info = next((p for p in all_positions_data if p.get('symbol') == symbol), None)

        roi = float(position_info.get('unRealizedProfit', 0)) / \
                           float(position_info.get('positionInitialMargin', 0)) * 100 

        return roi
    ##########################################################################
    
    def check_and_create_stop_loss(
            self,
            symbol: str,
            stop_loss_percent: int = 0.01,
            max_stop_loss_percent: int = 15,
            leverage: int = 20,
    ) -> None:
        """
        Check open positions and add stop loss

        Parameters
        ----------
        symbol : str
            Target symbol
        stop_loss_percent : int
            Initial percentage of stop loss threshold, default is 5
        max_stop_loss_percent : int
            Maximum percentage for stop loss, default is 15 
            (to prevent endless loop)
        leverage: int 
            default is 20
        """
        leverage = self.leverage
        try:
            # Get the current position for the symbol
            positions_info = self.client.futures_position_information(symbol=symbol)
            position_amt = 0.0
            entry_price = 0.0

            position_amt = float(positions_info['positionAmt'])
            entry_price = float(positions_info['entryPrice'])

            # If no open position, exit the function
            if position_amt == 0.0:
                self.logger.info(f"No open position for {symbol}")
                return

            self.logger.info(
                f"Open position for {symbol}: {position_amt} at entry price {entry_price}"
            )

            # Check open orders to see if there's an existing stop-loss order
            open_orders = self.client.futures_get_open_orders(symbol=symbol)
            has_stop_loss = False
            for order in open_orders:
                if order['type'] == 'STOP_MARKET':
                    has_stop_loss = True
                    self.logger.info(f"Stop-loss already exists for {symbol}")
                    break

            # If a stop-loss exists, return without doing anything
            if has_stop_loss:
                return

            # If no stop-loss, calculate and create one with retry logic
            # Calculate stop-loss price based on position type (long/short)
            while stop_loss_percent <= max_stop_loss_percent:
                try:
                    
                    # Long position
                    if position_amt > 0: 
                        stop_loss_price = entry_price * (1 - (stop_loss_percent / 100)/leverage)
                        stop_loss_side = Client.SIDE_SELL
                    
                    # Short position
                    else: 
                        stop_loss_price = entry_price * (1 + (stop_loss_percent / 100)/leverage )
                        stop_loss_side = Client.SIDE_BUY

                    # Round stop-loss price to correct precision
                    stop_loss_price = self.round_down_to_precision(stop_loss_price)
                    self.logger.info(
                        f"Attempting to create stop-loss at {stop_loss_price} with {stop_loss_percent}% threshold for {symbol}"
                    )

                    # Place stop-loss order
                    stop_loss_order = self.client.futures_create_order(
                        symbol=symbol,
                        side=stop_loss_side,
                        type='STOP_MARKET',
                        stopPrice=stop_loss_price,
                        quantity=abs(position_amt)
                    )
                    self.logger.info(f"Stop-loss created successfully: {stop_loss_order}")
                    
                    # Exit the loop after successful order
                    break  

                except BinanceAPIException as e:
                    
                    # Check if the error is due to "Order would immediately trigger"
                    if "Order would immediately trigger" in str(e):
                        self.logger.warning(
                            f"Stop-loss failed: {e}. Increasing stop-loss percent to {stop_loss_percent + 0.5}%"
                        )
                        
                        # Increment stop loss by 0.5% and retry
                        stop_loss_percent += 0.5 
                    else:
                        self.logger.error(f"Error creating stop-loss: {e}")
                        traceback.print_exc()
                        
                        # Break loop for other API errors
                        break  
                except Exception as e:
                    self.logger.error(f"Unexpected error: {e}")
                    traceback.print_exc()
                    break

            if stop_loss_percent > max_stop_loss_percent:
                self.logger.error(
                    f"Failed to create stop-loss for {symbol} after reaching max stop-loss percent of {max_stop_loss_percent}%."
                )

        except BinanceAPIException as e:
            self.logger.error(f"Error checking/creating stop-loss: {e}")
            traceback.print_exc()
        except Exception as e:
            self.logger.error(f"Unexpected error: {e}")
            traceback.print_exc()

    ##########################################################################
    
    def cancel_all_open_orders(self, symbol: str):
        """Cancel all open orders

        Parameters
        ----------
        symbol : str
            Target symbol
        """
        try:
            # Cancel all open orders for the given symbol
            response = self.client.futures_cancel_all_open_orders(symbol=symbol)
            self.logger.info(
                f"Successfully canceled all open orders for {symbol}: {response}"
            )
        except BinanceAPIException as e:
            self.logger.info(f"Error cancelling orders: {e}")
            traceback.print_exc()
        except Exception as e:
            self.logger.info(f"Unexpected error: {e}")
            traceback.print_exc()

    #############
    # Utilities #
    ##########################################################################
    
    def calculate_quantity(
            self, 
            symbol: str, 
            usdt_balance: float, 
            leverage: int    
    ) -> float:
        """Calculate quantity to buy

        Parameters
        ----------
        symbol : str
            Target symbol to buy
        usdt_balance : float
            Balance to buy
        leverage : int
            Leverage to multiply quantity

        Returns
        -------
        float
            Float of quantity with 6th precision
        """
        # Fetch current price for the symbol (e.g., BTCUSDT)
        ticker = self.client.futures_symbol_ticker(symbol=symbol)
        current_price = float(ticker['price'])
        
        # Calculate maximum quantity you can buy with leverage
        quantity = (usdt_balance * leverage) / current_price
        return round(quantity, 6) 

    ##########################################################################
    
    @staticmethod
    def calculate_tp_sl_prices(
            entry_price: float, 
            tp_percent: float, 
            sl_percent: float, 
            position_type: str,
            leverage: int
    ) -> tuple[float, float]:
        """Return tuple(take-profit, stop-loss) with leverage factor.

        Parameters
        ----------
        entry_price : float
            Price placed at main order
        tp_percent : float
            Percentage for take profit
        sl_percent : float
            Percentage for stop loss
        position_type : str
            Type of position, LONG | SELL
        leverage : int
            Leverage to calculate

        Returns
        -------
        tuple[float, float]
        """
        # Calculate take profit 
        tp_price = \
            entry_price * (1 + (tp_percent / 100)/leverage) if position_type == 'LONG' \
                else entry_price * (1 - (tp_percent / 100)/leverage)
        
        # Calculate stop loss
        sl_price = \
            entry_price * (1 - (sl_percent / 100)/leverage) if position_type == 'LONG' \
                else entry_price * (1 + (sl_percent / 100)/leverage)
        return round(tp_price, 4), round(sl_price, 4)

    ##########################################################################
    
    @staticmethod
    def round_to_precision(value: float):
        return math.floor(value * 1000) / 1000
    
    ##########################################################################
    
    @staticmethod
    def round_down_to_precision(value, is_quantity=False):
        """
        Mock method: Rounds a price or quantity value down to the required precision 
        (based on the symbol's filters). Crucial for successful API calls.
        """
        if is_quantity:
            # BTCUSDT often has 3 decimal places for quantity
            return float(f"{value:.4f}")
        else:
            # BTCUSDT often has 2 decimal places for price
            return float(f"{value:.4f}")
    
    ##########################################################################
    
    def calculate_pnl(
            self, 
            end_time: datetime = None,  
            start_time: datetime = None,
    ) -> float:
        """Calculate PNL, if both `end_time` and `start_time` are none,
        it will use current timestamp as a end_time and 00:00:00
        of the same date as a `start_date`.

        Parameters
        ----------
        end_time : datetime
            End date in datetime object
            , by default is None
        start_time : datetime
            Start time in datetime object
            , by default is None

        Returns
        -------
        float
            PNL in usdt
        """
        try:
            
            # Get the current UTC time (end time)
            # Calculate the start time for the current day 
            # in UTC (midnight)
            if (end_time is None) and (start_time is None):
                end_time = datetime.now(datetime.timezone.utc)
                start_time = end_time.replace(
                    hour=0, 
                    minute=0,
                    second=0,
                    microsecond=0
                )
            
            # Convert datetime to milliseconds 
            # (Binance API uses timestamps in milliseconds)
            start_time_ms = int(start_time.timestamp() * 1000)
            end_time_ms = int(end_time.timestamp() * 1000)
            
            # Fetch daily income (realized PnL)
            pnl_data = self.client.futures_income_history(
                incomeType='REALIZED_PNL',
                startTime=start_time_ms,
                endTime=end_time_ms
            )
            
            # Calculate total daily PnL
            total_pnl = sum(float(entry['income']) for entry in pnl_data)
            
            self.logger.info(f"Total Daily PnL: {total_pnl} USDT")
            
            # Optionally, print detailed PnL entries
            for entry in pnl_data:
                timestamp = datetime.fromtimestamp(
                    entry['time'] / 1000, tz=datetime.timezone.utc
                )
                income = float(entry['income'])
                self.logger.info(f"Time: {timestamp}, PnL: {income} USDT")
            
            return total_pnl

        except Exception as e:
            self.logger.info(f"Unexpected error: {e}")
    
    ##########################################################################
    
    def __load_exchange_info(self):
        """Fetches exchange information and extracts PRICE_FILTER and LOT_SIZE rules."""
        print("Fetching exchange information for futures symbols...")
        try:
            info = self.client.futures_exchange_info()
            for symbol_data in info['symbols']:
                symbol = symbol_data['symbol']
                
                # Check if the symbol is tradable
                if symbol_data['status'] != 'TRADING':
                    continue

                filters = {}
                for f in symbol_data['filters']:
                    if f['filterType'] == 'LOT_SIZE':
                        # LOT_SIZE contains quantity rules (step size, min/max quantity)
                        filters['stepSize'] = float(f['stepSize'])
                    elif f['filterType'] == 'PRICE_FILTER':
                        # PRICE_FILTER contains price rules (tick size, min/max price)
                        filters['tickSize'] = float(f['tickSize'])

                if 'stepSize' in filters and 'tickSize' in filters:
                    self.symbol_filters[symbol] = filters
        except Exception as e:
            print(f"Error fetching exchange info: {e}")
            raise
    
    ##########################################################################
    
    def round_quantity(self, symbol: str, quantity: float) -> float:
        """Rounds the quantity to the nearest multiple of the exchange's step size."""
        filters = self.symbol_filters[symbol]
        step_size = filters['stepSize']
        
        # Calculate the number of steps and round it to the nearest integer
        num_steps = quantity / step_size
        
        # Round down to ensure we meet minimum quantity requirements
        rounded_quantity = math.floor(num_steps) * step_size
        
        # Return the rounded quantity, usually formatted to avoid floating point issues
        # We calculate the number of decimals required by the step size
        # Use abs() because math.log10(step_size) will be negative for step_size < 1
        precision = int(round(abs(math.log10(step_size)))) if step_size < 1 else 0

        # Use f-string formatting to ensure the final result is correctly precise
        return float(f"{rounded_quantity:.{precision}f}")
    
    ##########################################################################
    
    def round_price(self, symbol: str, price: float) -> float:
        """Rounds the price to the nearest multiple of the exchange's tick size."""
        filters = self.symbol_filters[symbol]
        tick_size = filters['tickSize']
        
        # Calculate the number of ticks
        num_ticks = price / tick_size
        
        # Round the number of ticks to the nearest integer
        rounded_price = round(num_ticks) * tick_size

        # We calculate the number of decimals required by the tick size
        # Use abs() because math.log10(tick_size) will be negative for tick_size < 1
        precision = int(round(abs(math.log10(tick_size)))) if tick_size < 1 else 0

        # Use f-string formatting to ensure the final result is correctly precise
        return float(f"{rounded_price:.{precision}f}")
    
    ##########################################################################

    def transform(self, *args, **kawrgs) -> None:
        pass
    
    ##########################################################################
    
    def get_signature(params):
        """Generates the HMAC SHA256 signature required by Binance."""
        query_string = urlencode(params)
        signature = hmac.new(
            os.environ["BINANCE_API_KEY"].encode('utf-8'),
            query_string.encode('utf-8'),
            hashlib.sha256
        ).hexdigest()
        return signature
    
    ##########################################################################
    
    def send_signed_request(self, method, endpoint, params=None):
        """Helper to send signed requests."""
        if params is None:
            params = {}
        
        # 1. Add timestamp
        params['timestamp'] = int(time.time() * 1000)
        
        # 2. Generate signature
        params['signature'] = self.get_signature(params)
        
        # 3. Send Request
        headers = {'X-MBX-APIKEY': os.environ["BINANCE_API_KEY"]}
        url = f"{self.url}{endpoint}"
        
        try:
            if method == 'POST':
                response = requests.post(url, headers=headers, params=params)
            elif method == 'GET':
                response = requests.get(url, headers=headers, params=params)
                
            response.raise_for_status() # Raise error for bad status codes
            return response.json()
        except requests.exceptions.HTTPError as err:
            print(f"HTTP Error: {err}")
            print(f"Body: {response.text}")
            return None
    
    ##########################################################################

##############################################################################
