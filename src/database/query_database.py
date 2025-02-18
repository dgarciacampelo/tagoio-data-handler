import sqlite3
from loguru import logger
from typing import Tuple

from database import database_file
from schemas import ChargingSessionUpdate


def get_modified_rows_count(
    table_name: str, db_file: str = database_file
) -> int | None:
    """
    Returns the count of modified rows in the specified table. Used to find out
    if there are any modified rows in the database, to determine if a backup
    needs to be taken. In case of error, returns None to be handled by caller.
    """
    query = f"SELECT COUNT(is_modified) FROM {table_name} WHERE is_modified = 1;"
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query).fetchone()[0]
    except Exception as e:
        logger.error(f"Exception during get_modified_rows_count: {e}")
        return None


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
    query = "SELECT pool_code, device_id, device_token FROM tagoio_device"
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
) -> int | None:
    "Inserts a new charging session into the history database table."
    query = """
        INSERT INTO charging_session_history
        (pool_code, station_name, connector_id, card_alias, start_date, time_band, star_meter_value, last_meter_value, cost, is_modified, transaction_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
        RETURNING transaction_id;
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
                update.transaction_id,
            )
            transaction_id = conn.execute(query, values).fetchone()[0]
            conn.commit()
            return transaction_id
    except sqlite3.IntegrityError:
        prefix = "Duplicate session history index attempt with"
        logger.warning(f"{prefix} transaction_id: {update.transaction_id}")
        return None
    except Exception as e:
        logger.error(f"Exception during insert_database_charging_session_history: {e}")
        return None


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


def get_charging_sessions_from_pool_code(pool_code: int, db_file: str = database_file):
    "Returns all charging sessions for a given pool code."
    select_query = """
        SELECT
            created_at, pool_code, station_name, connector_id, card_alias,
            start_date, time_band, star_meter_value, last_meter_value, cost
        FROM charging_session_history
        WHERE pool_code = ?
        ORDER BY created_at ASC;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(select_query, (pool_code,)).fetchall()
    except Exception as e:
        logger.error(f"Exception during get_charging_sessions_from_pool_code: {e}")
        return []
