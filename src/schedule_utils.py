import asyncio
import schedule
from datetime import datetime
from loguru import logger

from config import service_name
from database import table_names_to_modified_check
from database.database_backup import zip_database_file, get_all_modified_rows_count
from database.database_check import clear_modified_column
from tagoio.check_data_amount import device_data_amount_check
from telegram_utils import upload_document, append_doc_tuple, pending_document_generator

device_data_amount_check_is_due: bool = False


def set_device_data_amount_check():
    "Marks the device data amount check as due, to trigger the corresponding job."
    global device_data_amount_check_is_due
    device_data_amount_check_is_due = True


def get_and_reset_device_data_amount_check() -> bool:
    "Resets the device data amount check flag (after providing its value)."
    global device_data_amount_check_is_due
    return_value = device_data_amount_check_is_due
    device_data_amount_check_is_due = False
    return return_value


def monthly_database_backup():
    "Performs a database backup, only on the first day of the month."
    if datetime.now().day != 1:
        return

    conditional_database_backup(True)


def conditional_database_backup(force_backup: bool = False, table_names: list = table_names_to_modified_check):
    "Performs a database backup if there are modified rows in the specified tables."
    if not force_backup:
        modified_rows_count: int = get_all_modified_rows_count(table_names)
        if modified_rows_count == 0:
            logger.info(f"No modified rows in the tracked tables for {service_name}.")
            return

    try:
        backup_database_to_telegram()
        for table_name in table_names:
            clear_modified_column(table_name)

        logger.info(f"Database backup completed for service: {service_name}.")
    except Exception as e:
        logger.error(f"Error during {service_name} database backup: {e}")


def backup_database_to_telegram():
    "Zips the local database file and sends it to Telegram, using a bot."
    zip_file = zip_database_file()
    if zip_file is None:
        logger.error("Error zipping database file, no backup was created.")
        return

    result_ok: bool = append_doc_tuple(zip_file)
    if not result_ok:
        pass  # TODO: Error handling (apart from sending a telegram message)


async def setup_schedules():
    """
    Setups the scheduled jobs to be executed. The monthly backup job executes
    before the conditional one, to clear the is_modified flag column at the end
    of its backup, so the oconditional job has nothing to do for the day.
    Uses its own asyncio event loop, to avoid blocking the FastAPI server.
    """
    await asyncio.sleep(5)  # Give time for the REST server to start...
    logger.info("Setting up schedules, using schedule.run_pending...")
    schedule.every().day.at("08:00", "Europe/Madrid").do(set_device_data_amount_check)
    schedule.every().day.at("20:45", "Europe/Madrid").do(monthly_database_backup)
    schedule.every().day.at("21:15", "Europe/Madrid").do(conditional_database_backup)

    while True:
        schedule.run_pending()  # runs all pending jobs from the schedule module
        if get_and_reset_device_data_amount_check():
            await device_data_amount_check()

        for file_to_send, bot_token, chat_id in pending_document_generator():
            result_ok: bool = await upload_document(file_to_send, bot_token, chat_id)
            if not result_ok:
                logger.warning(f"Error uploading document: {file_to_send}")
        await asyncio.sleep(15)
