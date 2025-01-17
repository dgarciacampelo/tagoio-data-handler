import os
from dotenv import load_dotenv

service_name: str = "TagoIO data handler"

# Load environment variables from .env file and assign to variables
load_dotenv()

port = int(os.getenv("API_PORT"))
version = os.getenv("API_VERSION")
app_default_user = os.getenv("APP_DEFAULT_USER")
app_default_token = os.getenv("APP_DEFAULT_TOKEN")
tago_account_token = os.getenv("TAGO_ACCOUNT_TOKEN")
tago_api_endpoint = os.getenv("TAGO_API_ENDPOINT")
tago_device_prefix = os.getenv("TAGO_DEVICE_PREFIX")
# ? To be able to check the data amount inside each TagoIO device:
tago_data_amount_token = os.getenv("TAGO_DATA_AMOUNT_TOKEN")

test_pool_code = int(os.getenv("TEST_POOL_CODE"))
test_device_id = os.getenv("TEST_DEVICE_ID")
test_device_token = os.getenv("TEST_DEVICE_TOKEN")

telegram_bot_token = os.getenv("TELEGRAM_BOT_TOKEN")
telegram_notices_chat_id = os.getenv("TELEGRAM_NOTICES_CHAT_ID")
telegram_backups_chat_id = os.getenv("TELEGRAM_BACKUPS_CHAT_ID")
