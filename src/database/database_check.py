import sqlite3
from loguru import logger

from database import database_file


def insert_modified_column(table_name: str, db_file: str = database_file) -> bool:
    "Inserts a new modified column into the specified table."
    query = f"""
        ALTER TABLE {table_name}
        ADD COLUMN is_modified INTEGER NOT NULL DEFAULT 1;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(query)
            conn.commit()
            return True
    except Exception as e:
        logger.error(f"Exception during insert_modified_column in {table_name}: {e}")
        return False


def clear_modified_column(table_name: str, db_file: str = database_file) -> bool:
    "Clears the modified flag in the specified table (after a backup)."
    query = f"UPDATE {table_name} SET is_modified = 0;"
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(query)
            conn.commit()
            return True
    except Exception as e:
        logger.error(f"Exception during clear_modified_column in {table_name}: {e}")
        return False


def check_database_tables(db_file: str = database_file):
    "Checks if al the used tables exist in the database or creates new ones."
    check_tagoio_device_table(db_file)
    check_charging_session_history_table(db_file)


def check_tagoio_device_table(db_file: str = database_file):
    "Checks if the table exists in the database or creates a new one."

    create_table_query = """
    CREATE TABLE IF NOT EXISTS tagoio_device(
        pool_code INTEGER NOT NULL PRIMARY KEY,
        device_id TEXT NOT NULL,
        device_token TEXT NOT NULL,
        is_modified INTEGER NOT NULL DEFAULT 1
        );
    """

    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(create_table_query)
    except Exception as e:
        logger.error(f"Exception during check_tagoio_device_table: {e}")


def check_charging_session_history_table(db_file: str = database_file):
    "Checks if the table exists in the database or creates a new one."

    create_table_query = """
    CREATE TABLE IF NOT EXISTS charging_session_history(
        created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        pool_code INTEGER NOT NULL,
        station_name TEXT NOT NULL,
        connector_id INTEGER NOT NULL,
        card_alias TEXT NOT NULL,
        start_date TEXT NOT NULL,
        time_band TEXT NOT NULL,
        star_meter_value INTEGER NOT NULL,
        last_meter_value INTEGER NOT NULL,
        cost REAL NOT NULL,
        is_modified INTEGER NOT NULL DEFAULT 1
        );
    """
    # TODO: Add transaction_id to the table

    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(create_table_query)
    except Exception as e:
        logger.error(f"Exception during check_charging_session_history_table: {e}")
