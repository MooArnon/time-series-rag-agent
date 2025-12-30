import os
from utils.secret_manager import get_secret

config = {}

trading_bot_secrets = get_secret("trading-bot")
secrets_dict = eval(trading_bot_secrets)
os.environ["BINANCE_API_KEY"] = secrets_dict['BINANCE_API_KEY']
os.environ["BINANCE_SECRET_KEY"] = secrets_dict['BINANCE_SECRET_KEY']

config['BINANCE_API_KEY'] = secrets_dict['BINANCE_API_KEY']
config['BINANCE_SECRET_KEY'] = secrets_dict['BINANCE_SECRET_KEY']
config['LEVERAGE'] = int(os.environ["LEVERAGE"])

config['BANDWIDTH_THRESHOLD'] = float(os.getenv("BANDWIDTH_THRESHOLD", 0.003)) 

config['OPENAI_API_KEY'] = os.getenv("OPENAI_API_KEY", None)

config['LLM_CONFIDENCE_PERCENTAGE_THRESHOLD'] = int(os.getenv("LLM_CONFIDENCE_PERCENTAGE_THRESHOLD", 75))

config['DB_HOST'] = os.environ['DB_HOST']
config['DB_PORT'] = os.environ['DB_PORT']
config['DB_USER'] = os.environ['DB_USER']
config['DB_PASSWORD'] = secrets_dict['TRADING_BOT_DB_POSTGRESQL_PASSWORD']
config['DB_NAME'] = os.environ['DB_NAME']