import sqlite3
from datetime import datetime
from loguru import logger
from typing import Optional
from zipfile import ZipFile

from config import service_name
from database import database_file, backup_file
from database.database_check import insert_modified_column
from database.query_database import get_modified_rows_count

service_prefix: str = service_name.lower().replace(" ", "_") + "_"


def zip_database_file(
    db_file: str = database_file,
    dest_file: str = backup_file,
    zip_params: dict = {"mode": "w", "compression": 8, "compresslevel": 8},
) -> Optional[str]:
    "Zips the database file and returns the path of the compressed file."
    dt_now: str = datetime.now().strftime("%Y-%m-%d_%H.%M.%S")
    db_folder, db_file_name = dest_file.split("/")
    backup_file_name: str = db_folder + "/" + service_prefix + db_file_name
    backup_file_name = backup_file_name.split(".")[0] + "_" + dt_now
    archive_file_name: str = backup_file_name.split("/")[1]

    try:
        # Using SQLite online backup API to backup the database
        # ? Reference: https://docs.python.org/3/library/sqlite3.html
        with sqlite3.connect(db_file) as original:
            with sqlite3.connect(dest_file) as backup:
                original.backup(backup)

        logger.info(f"Compressing database file {backup_file_name}.zip ...")
        with ZipFile(backup_file_name + ".zip", **zip_params) as zip_file:
            zip_file.write(db_file, archive_file_name + ".sqlite3")
            return backup_file_name + ".zip"
    except Exception as e:
        logger.error(f"Exception during zip_database_file: {e}")
        return None


def get_all_modified_rows_count(table_names: list, db_file: str = database_file) -> int:
    "Returns the count of modified rows in the specified table(s)."
    modified_rows_count: int = 0
    for table_name in table_names:
        actual_modified_count = get_modified_rows_count(table_name, db_file)
        if actual_modified_count is None:
            modified_rows_count += 1
            # Handle the missing modified column in a previous version of the database:
            result_ok = insert_modified_column(table_name, db_file)
            if not result_ok:
                logger.error(f"Error inserting modified column in {table_name}.")
        else:
            modified_rows_count += actual_modified_count

    return modified_rows_count
