import asyncio
from pathlib import Path
from loguru import logger
from telegram import Bot
from telegram.error import NetworkError

from config import telegram_bot_token as bot_token, telegram_backups_chat_id as chat_id


async def upload_document(
    file_to_send: str, bot_token: str = bot_token, chat_id: int = chat_id
):
    "Sends a file to a Telegram chat, providing the bot_token and chat_id"
    result_ok: bool = False
    with Path.open(file_to_send, "rb") as file:
        try:
            telegram_bot = Bot(bot_token)
            prefix: str = f"Uploading file: {file_to_send.split('/')[-1]}..."
            message: str = f"TagoIO data handler, telegram module. {prefix}"
            logger.info(message)
            await telegram_bot.send_message(chat_id, message)
            await telegram_bot.send_document(chat_id, file)
            result_ok = True
        except NetworkError:
            logger.error("TagoIO data handler, NetworkError on telegram module.")
            await asyncio.sleep(1)
        except Exception as e:
            logger.info(f"TagoIO data handler, telegram module, error: {e}")
        finally:
            return result_ok


def run_upload_document(
    file_to_send: str, bot_token: str = bot_token, chat_id: int = chat_id
):
    "Wraps the async upload_document function, to be called from a synchronous context"
    asyncio.run(upload_document(file_to_send, bot_token, chat_id))
