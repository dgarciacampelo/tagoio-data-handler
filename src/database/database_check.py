import sqlite3
from loguru import logger

from database import database_file
# from decorators import benchmark


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
        logger.error(f"Exception adding modified column in {table_name}: {e}")
        return False


def check_table_has_column(
    table_name: str, column_name: str, db_file: str = database_file
) -> bool:
    "Checks if a column_name column exists in the specified table."
    check_query = f"PRAGMA table_info({table_name});"
    table_has_column: bool = False
    try:
        with sqlite3.connect(db_file) as conn:
            table_info: list = conn.execute(check_query).fetchall()
            for row_info in table_info:
                if row_info[1] == column_name:
                    logger.info(f"Column {column_name} already exists in {table_name}")
                    table_has_column = True
                    break

        if not table_has_column:
            return alter_table_add_column(table_name, column_name, db_file)

        return True
    except Exception as e:
        logger.error(f"Exception checking {column_name} column in {table_name}: {e}")
        return False


def alter_table_add_column(
    table_name: str, column_name: str, db_file: str = database_file
) -> bool:
    "Inserts a new column_name column into the specified table."
    alter_query = f"""
        ALTER TABLE {table_name}
        ADD COLUMN {column_name} INTEGER NOT NULL DEFAULT 0;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(alter_query)
            conn.commit()
            return True
    except Exception as e:
        logger.error(f"Exception adding {column_name} column in {table_name}: {e}")
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


def check_local_database(db_file: str = database_file):
    "Checks if al the used tables exist in the database or creates new ones."
    check_pragma_statements(db_file)
    check_tagoio_device_table(db_file)
    check_charging_session_history_table(db_file)
    check_table_has_column("charging_session_history", "transaction_id", db_file)


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
        transaction_id INTEGER NOT NULL DEFAULT 0,
        card_alias TEXT NOT NULL,
        start_date TEXT NOT NULL,
        time_band TEXT NOT NULL,
        star_meter_value INTEGER NOT NULL,
        last_meter_value INTEGER NOT NULL,
        cost REAL NOT NULL,
        is_modified INTEGER NOT NULL DEFAULT 1
        );
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(create_table_query)
    except Exception as e:
        logger.error(f"Exception during check_charging_session_history_table: {e}")


def check_pragma_statements(db_file: str = database_file):
    "Executes pragma statements to enable foreign keys and WAL journal mode."
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute("PRAGMA journal_mode = WAL;")
            conn.execute("PRAGMA foreign_keys = ON;")
            logger.debug(f"SQLite pragma statements executed on: {db_file}")
    except Exception as e:
        logger.error(f"Exception during SQLite pragma statements: {e}")
