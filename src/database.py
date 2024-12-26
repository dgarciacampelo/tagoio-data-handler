import sqlite3
from loguru import logger
from typing import Tuple

from schemas import ChargingSessionUpdate


database_file: str = "database_files/database.sqlite3"


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
        device_token TEXT NOT NULL
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
        cost REAL NOT NULL
        );
    """

    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(create_table_query)
    except Exception as e:
        logger.error(f"Exception during check_charging_session_history_table: {e}")


def get_database_tagoio_devices_count(db_file: str = database_file) -> int:
    "Returns the number of tagoio_device rows in the database table."
    query = "SELECT COUNT(pool_code) FROM tagoio_device;"
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query).fetchone()[0]
    except Exception as e:
        logger.error(f"Exception counting tagoio_device table rows: {e}")
        return 0


def get_database_charging_session_history_count(db_file: str = database_file) -> int:
    "Returns the number of charging session rows in the history database table."
    query = "SELECT COUNT(ROWID) FROM charging_session_history;"
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query).fetchone()[0]
    except Exception as e:
        logger.error(f"Exception counting charging_session_history table rows: {e}")
        return 0


def get_all_database_tagoio_devices(
    db_file: str = database_file,
) -> list[Tuple[int, str, str]]:
    "Returns all tagoio_device rows in the database table."
    query = "SELECT * FROM tagoio_device"
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query).fetchall()
    except Exception as e:
        logger.error(f"Exception during get_all_database_tagoio_devices: {e}")
        return []


def get_database_tagoio_device(pool_code: int, db_file: str = database_file):
    "Returns the tagoio_device for a given pool code."
    query = "SELECT device_id, device_token FROM tagoio_device WHERE pool_code = ?;"
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query, (pool_code,)).fetchone()
    except Exception as e:
        logger.error(f"Exception during get_database_tagoio_device: {e}")
        return None


def insert_database_tagoio_device(
    pool_code: int, device_id: str, device_token: str, db_file: str = database_file
):
    "Inserts a new tagoio_device into the database table."
    query = "INSERT INTO tagoio_device (pool_code, device_id, device_token) VALUES (?, ?, ?);"
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(query, (pool_code, device_id, device_token))
            conn.commit()
    except Exception as e:
        logger.error(f"Exception during insert_database_tagoio_device: {e}")


def insert_database_charging_session_history(
    update: ChargingSessionUpdate, db_file: str = database_file
):
    "Inserts a new charging session into the history database table."
    query = """
        INSERT INTO charging_session_history
        (pool_code, station_name, connector_id, card_alias, start_date, time_band, star_meter_value, last_meter_value, cost)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
    """
    try:
        with sqlite3.connect(db_file) as conn:
            values = (
                update.pool_code,
                update.station_name,
                update.connector_id,
                update.card_alias,
                update.start_date,
                update.time_band,
                update.star_meter_value,
                update.last_meter_value,
                update.cost,
            )
            conn.execute(query, values)
            conn.commit()
    except Exception as e:
        logger.error(f"Exception during insert_database_charging_session_history: {e}")


def update_database_tagoio_device(
    pool_code: int, device_id: str, device_token: str, db_file: str = database_file
):
    "Updates an existing tagoio_device in the database table."
    query = """
        UPDATE tagoio_device
        SET device_id = ?, device_token = ?
        WHERE pool_code = ?;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(query, (device_id, device_token, pool_code))
            conn.commit()
    except Exception as e:
        logger.error(f"Exception during update_database_tagoio_device: {e}")


def delete_database_tagoio_device(pool_code: int, db_file: str = database_file):
    "Deletes an existing tagoio_device from the database table."
    query = "DELETE FROM tagoio_device WHERE pool_code = ?;"
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(query, (pool_code,))
            conn.commit()
    except Exception as e:
        logger.error(f"Exception during delete_database_tagoio_device: {e}")
