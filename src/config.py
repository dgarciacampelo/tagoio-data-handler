import os
from dotenv import load_dotenv


service_name: str = "TagoIO data handler"
not_int_error: str = "is not a valid integer!"
not_set_error: str = "environment variable is not set!"

# Load environment variables from .env file and assign to variables
load_dotenv()

port_env = os.getenv("API_PORT")
if port_env is None:
    raise EnvironmentError(f"API_PORT {not_set_error}")
try:
    port: int = int(port_env)
except ValueError:
    raise EnvironmentError(f"API_PORT ('{port_env}') {not_int_error}")

version = os.getenv("API_VERSION")
app_default_user = os.getenv("APP_DEFAULT_USER")
app_default_token = os.getenv("APP_DEFAULT_TOKEN")

tago_account_token_env = os.getenv("TAGO_ACCOUNT_TOKEN")
if tago_account_token_env is None:
    raise EnvironmentError(f"TAGO_ACCOUNT_TOKEN {not_set_error}")
tago_account_token: str = tago_account_token_env

tago_api_endpoint = os.getenv("TAGO_API_ENDPOINT")
tago_device_prefix = os.getenv("TAGO_DEVICE_PREFIX")
# ? To be able to check the data amount inside each TagoIO device:
tago_data_amount_token = os.getenv("TAGO_DATA_AMOUNT_TOKEN")

test_pool_code_env = os.getenv("TEST_POOL_CODE")
if test_pool_code_env is None:
    raise EnvironmentError(f"TEST_POOL_CODE {not_set_error}!")
try:
    test_pool_code: int = int(test_pool_code_env)
except ValueError:
    raise EnvironmentError(f"TEST_POOL_CODE ('{test_pool_code_env}') {not_int_error}")

test_device_id = os.getenv("TEST_DEVICE_ID")
test_device_token = os.getenv("TEST_DEVICE_TOKEN")

tg_bot_token_env = os.getenv("TELEGRAM_BOT_TOKEN")
if tg_bot_token_env is None:
    raise EnvironmentError(f"TELEGRAM_BOT_TOKEN {not_set_error}")
telegram_bot_token: str = tg_bot_token_env

tg_notices_chat_id_env = os.getenv("TELEGRAM_NOTICES_CHAT_ID")
if tg_notices_chat_id_env is None:
    raise EnvironmentError(f"TELEGRAM_NOTICES_CHAT_ID {not_set_error}")
try:
    telegram_notices_chat_id: int = int(tg_notices_chat_id_env)
except ValueError:
    name: str = "TELEGRAM_NOTICES_CHAT_ID"
    raise EnvironmentError(f"{name} ('{tg_notices_chat_id_env}') {not_int_error}")

tg_backups_chat_id_env = os.getenv("TELEGRAM_BACKUPS_CHAT_ID")
if tg_backups_chat_id_env is None:
    raise EnvironmentError(f"TELEGRAM_BACKUPS_CHAT_ID {not_set_error}")
try:
    telegram_backups_chat_id: int = int(tg_backups_chat_id_env)
except ValueError:
    name: str = "TELEGRAM_BACKUPS_CHAT_ID"
    raise EnvironmentError(f"{name} ('{tg_backups_chat_id_env}') {not_int_error}")
