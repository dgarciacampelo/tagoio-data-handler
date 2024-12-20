import sqlite3
from loguru import logger
from typing import Tuple


database_file: str = "database_files/database.sqlite3"


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


def get_database_tagoio_devices_count(db_file: str = database_file) -> int:
    "Returns the number of tagoio_device rows in the database table."
    query = "SELECT COUNT(pool_code) FROM tagoio_device;"
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query).fetchone()[0]
    except Exception as e:
        logger.error(f"Exception during get_all_database_tagoio_devices_count: {e}")
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
