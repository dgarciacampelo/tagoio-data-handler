import asyncio
from pathlib import Path
from loguru import logger
from telegram import Bot
from telegram.error import NetworkError

from config import (
    service_name,
    telegram_bot_token as bot_token,
    telegram_backups_chat_id as chat_id,
)

# Paths of documents to be uploaded, with the bot_token and chat_id:
pending_documents: list[tuple[str, str, int]] = list()


async def upload_document(
    file_to_send: str, bot_token: str = bot_token, chat_id: int = chat_id
):
    "Sends a file to a Telegram chat, providing the bot_token and chat_id"
    result_ok: bool = False
    with Path.open(file_to_send, "rb") as file:
        try:
            telegram_bot = Bot(bot_token)
            message: str = f"{service_name}, uploading: {file_to_send.split('/')[-1]}"
            logger.info(message)
            await telegram_bot.send_message(chat_id, message)
            await telegram_bot.send_document(chat_id, file)
            result_ok = True
        except NetworkError:
            logger.error(f"{service_name}, NetworkError on telegram module.")
            await asyncio.sleep(1)
        except Exception as e:
            logger.info(f"{service_name}, telegram module, error: {e}")
        finally:
            return result_ok


def append_doc_tuple(
    file_to_send: str, bot_token: str = bot_token, chat_id: int = chat_id
):
    "Appends the parameters of a document to be uploaded to Telegram later."
    pending_documents.append((file_to_send, bot_token, chat_id))


def pending_document_generator():
    "Yields the parameters of a document to be uploaded to Telegram."
    for document_tuple in pending_documents:
        file_to_send, bot_token, chat_id = document_tuple
        pending_documents.remove(document_tuple)
        yield file_to_send, bot_token, chat_id
