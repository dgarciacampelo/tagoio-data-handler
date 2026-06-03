import sqlite3
from loguru import logger
from typing import Optional

from database import database_file
from schemas import ChargingSessionUpdate


def get_modified_rows_count(table_name: str, db_file: str = database_file) -> Optional[int]:
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


def get_all_database_tagoio_devices(db_file: str = database_file) -> list[tuple[int, str, str]]:
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


def insert_database_tagoio_device(pool_code: int, device_id: str, device_token: str, db_file: str = database_file):
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
) -> Optional[int]:
    """Inserts a new charging session into the history database table."""
    query = """
        INSERT INTO charging_session_history
        (
            pool_code, station_name, connector_id, transaction_id, 
            card_alias, start_date, time_band, start_meter_value, 
            last_meter_value, cost, rate_off_peak, rate_flat, rate_peak, 
            energy_off_peak, energy_flat, energy_peak, is_modified
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
        RETURNING transaction_id;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            values = (
                update.pool_code,
                update.station_name,
                update.connector_id,
                update.transaction_id,
                update.card_alias,
                update.start_date,
                update.time_band,
                update.start_meter_value,
                update.last_meter_value,
                update.cost,
                update.rate_off_peak,
                update.rate_flat,
                update.rate_peak,
                update.energy_off_peak,
                update.energy_flat,
                update.energy_peak,
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


def insert_charging_session_telemetry(update: ChargingSessionUpdate, db_file: str = database_file):
    """Stores high-frequency telemetry with cumulative energy breakdowns."""
    query = """
        INSERT OR IGNORE INTO charging_session_telemetry 
        (transaction_id, timestamp, meter_value, power, cost, current_tariff_band, energy_off_peak, energy_flat, energy_peak)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    """

    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(
                query,
                (
                    update.transaction_id,
                    update.last_meter_ts,
                    update.last_meter_value,
                    update.power,
                    update.cost,
                    update.current_tariff_band,
                    update.energy_off_peak,
                    update.energy_flat,
                    update.energy_peak,
                ),
            )
            conn.commit()
    except Exception as e:
        logger.error(f"Error inserting telemetry for {update.transaction_id}: {e}")


def get_telemetry_for_session(transaction_id: int, db_file: str = database_file) -> list[tuple]:
    """Retrieves all telemetry data for a specific transaction."""
    query = """
        SELECT timestamp, meter_value, power, cost, current_tariff_band, energy_off_peak, energy_flat, energy_peak
        FROM charging_session_telemetry 
        WHERE transaction_id = ? 
        ORDER BY timestamp ASC
    """
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query, (transaction_id,)).fetchall()
    except Exception as e:
        logger.error(f"Error retrieving telemetry for {transaction_id}: {e}")
        return []


def update_database_tagoio_device(pool_code: int, device_id: str, device_token: str, db_file: str = database_file):
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
            start_date, time_band, start_meter_value, last_meter_value, cost
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


def get_noc_from_db(station_name: str, db_file: str = database_file) -> Optional[int]:
    """Retrieves the number of connectors (noc) for a given station."""
    query = "SELECT noc FROM station_config WHERE station_name = ?;"
    try:
        with sqlite3.connect(db_file) as conn:
            result = conn.execute(query, (station_name,)).fetchone()
            if result:
                return result[0]
            return None
    except Exception as e:
        logger.error(f"Error retrieving noc for station {station_name}: {e}")
        return None


def update_station_noc_if_needed(
    pool_code: int, station_name: str, connector_id: int, db_file: str = database_file
) -> None:
    """
    Infers and updates the number of connectors (noc) for a station.
    If the incoming connector_id is higher than the stored noc, the database is updated.
    """
    select_query = "SELECT noc FROM station_config WHERE station_name = ?;"
    insert_query = """
        INSERT INTO station_config (station_name, pool_code, noc)
        VALUES (?, ?, ?);
    """
    update_query = """
        UPDATE station_config
        SET noc = ?, is_modified = 1
        WHERE station_name = ?;
    """

    try:
        with sqlite3.connect(db_file) as conn:
            result = conn.execute(select_query, (station_name,)).fetchone()

            if result is None:  # Station not in DB, insert it with the current connector_id as noc
                conn.execute(insert_query, (station_name, pool_code, connector_id))
                conn.commit()
                logger.info(f"Registered new station {pool_code}/{station_name} with noc {connector_id}.")

            elif connector_id > result[0]:  # Station exists, but a higher connector_id was broadcasted
                conn.execute(update_query, (connector_id, station_name))
                conn.commit()
                logger.info(f"Updated station {pool_code}/{station_name} noc from {result[0]} to {connector_id}.")

    except Exception as e:
        logger.error(f"Error updating noc for station {pool_code}/{station_name}: {e}")


def upsert_connector_status(
    pool_code: int, station_name: str, connector_id: int, status: str, db_file: str = database_file
):
    """Inserts or updates the last known status of a connector."""
    query = """
        INSERT INTO connector_status (pool_code, station_name, connector_id, charge_point_status, is_modified)
        VALUES (?, ?, ?, ?, 1)
        ON CONFLICT(pool_code, station_name, connector_id) 
        DO UPDATE SET charge_point_status=excluded.charge_point_status, is_modified=1;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.execute(query, (pool_code, station_name, connector_id, status))
            conn.commit()
    except Exception as e:
        logger.error(f"Error upserting connector status: {e}")


def get_all_connector_statuses(db_file: str = database_file) -> list[tuple]:
    """Retrieves all stored connector statuses to rehydrate memory on startup."""
    query = """
        SELECT pool_code, station_name, connector_id, charge_point_status
        FROM connector_status
        ORDER BY pool_code, station_name, connector_id;
    """
    try:
        with sqlite3.connect(db_file) as conn:
            return conn.execute(query).fetchall()
    except Exception as e:
        logger.error(f"Error retrieving connector statuses: {e}")
        return []


def get_session_history(transaction_id: int, db_file: str = database_file) -> Optional[dict]:
    """Retrieves the full metadata and frozen rates for a specific charging session."""
    query = """
        SELECT transaction_id, pool_code, station_name, connector_id, 
            start_date, time_band, cost, card_alias,
            (last_meter_value - start_meter_value) / 1000.0 AS total_energy_kwh,
            rate_off_peak, rate_flat, rate_peak,
            energy_off_peak, energy_flat, energy_peak
        FROM charging_session_history 
        WHERE transaction_id = ?
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.row_factory = sqlite3.Row
            row = conn.execute(query, (transaction_id,)).fetchone()
            return dict(row) if row else None
    except Exception as e:
        logger.error(f"Error retrieving session history for {transaction_id}: {e}")
        return None


def get_recent_sessions(limit: int = 50, db_file: str = database_file) -> list[dict]:
    """Retrieves recent completed sessions with full rate and energy breakdown."""
    query = """
        SELECT transaction_id, pool_code, station_name, connector_id, 
            start_date, time_band, cost, card_alias,
            (last_meter_value - start_meter_value) / 1000.0 AS total_energy_kwh,
            rate_off_peak, rate_flat, rate_peak,
            energy_off_peak, energy_flat, energy_peak
        FROM charging_session_history 
        ORDER BY created_at DESC LIMIT ?
    """
    try:
        with sqlite3.connect(db_file) as conn:
            conn.row_factory = sqlite3.Row
            rows = conn.execute(query, (limit,)).fetchall()
            return [dict(row) for row in rows]
    except Exception as e:
        logger.error(f"Error retrieving recent sessions: {e}")
        return []


def delete_database_cs_telemetry(db_file: str = database_file, days_threshold: int = 30) -> tuple[int, int]:
    """
    Deletes old charging_session_telemetry records from the database table to avoid DB bloat.
    Returns tuple: (deleted_records_count, remaining_records_count)
    """
    delete_query = f"""
        DELETE FROM charging_session_telemetry
        WHERE timestamp < datetime('now', '-{days_threshold} days');
    """
    count_query = "SELECT COUNT(*) FROM charging_session_telemetry;"

    try:
        with sqlite3.connect(db_file) as conn:
            cursor = conn.cursor()

            cursor.execute(delete_query)  # Execute deletion and get the number of affected rows
            deleted_count = cursor.rowcount

            cursor.execute(count_query)  # Count the remaining rows
            remaining_count = cursor.fetchone()[0]

            conn.commit()
            return deleted_count, remaining_count
    except Exception as e:
        logger.error(f"Exception during delete_database_cs_telemetry: {e}")
        return 0, 0
