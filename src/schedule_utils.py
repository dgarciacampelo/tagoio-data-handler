import schedule
from loguru import logger
from random import randint
from time import sleep

from database import zip_database_file
from telegram_utils import run_upload_document


def backup_database_to_telegram(max_sleep_seconds: int = 60):
    "Zips the local database file and sends it to Telegram, using a bot."
    # Wait up to 60s, in case of multiple services running on the same host...
    sleep(randint(0, max_sleep_seconds))

    zip_file = zip_database_file()
    if zip_file is None:
        logger.error("Error zipping database file, no backup was created.")
        return

    result_ok: bool = run_upload_document(zip_file)
    if not result_ok:
        pass  # TODO: Error handling (apart from sending a telegram message)


def setup_schedules():
    logger.info("Setting up schedules, using schedule.run_pending...")
    schedule.every().friday.at("21:15", "Europe/Madrid").do(backup_database_to_telegram)

    while True:
        schedule.run_pending()
        sleep(15)
