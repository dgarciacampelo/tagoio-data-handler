from datetime import datetime, timedelta
from typing import Optional

from loguru import logger

from charge_points import register_charge_point
from database.query_database import (
    get_all_connector_statuses,
    insert_database_charging_session_history,
    update_station_noc_if_needed,
    upsert_connector_status,
)
from enumerations import ChargePointStatus
from schemas import ChargePointData, ChargePointUpdate, ChargingSessionUpdate
from tagoio.data_parsing import (
    update_charge_point_status,
    update_management_dashboard_charging_session,
    update_public_dashboard_values,
)

# Data of the known charge points
charge_points: dict[tuple, ChargePointData] = dict()

# Active charging sessions, stored with a tuple key (pool_code, station_name, connector_id) for template rendering
active_sessions: dict[tuple, ChargingSessionUpdate] = dict()


def get_search_key(pool_code: int, station_name: str, connector_id: int = 1) -> tuple:
    """Provides a deterministic tuple key for the charge point data"""
    return (pool_code, station_name, connector_id)


def get_charge_point(pool_code: int, station_name: str, connector_id: int = 1) -> Optional[ChargePointData]:
    """Returns the charge point data, if it exists"""
    search_key = get_search_key(pool_code, station_name, connector_id)
    return charge_points.get(search_key, None)


def get_active_session(pool_code: int, station_name: str, connector_id: int = 1) -> Optional[ChargingSessionUpdate]:
    """Returns the ongoing charging session data, if it exists"""
    search_key = get_search_key(pool_code, station_name, connector_id)
    return active_sessions.get(search_key, None)


def load_statuses_from_db():
    """Used on startup to rehydrate the charge_points dictionary."""
    rows = get_all_connector_statuses()
    for pool_code, station_name, connector_id, status in rows:
        search_key = get_search_key(pool_code, station_name, connector_id)
        # Register in the general known endpoints map
        register_charge_point(pool_code, station_name, connector_id)

        # Populate in-memory dict
        charge_points[search_key] = ChargePointData(
            pool_code=pool_code,
            station_name=station_name,
            connector_id=connector_id,
            charge_point_status=ChargePointStatus(status),
            is_quarantined=False,
        )


async def manage_charge_point_update(update: ChargePointUpdate) -> ChargePointData:
    """Updates the dashboards with the charge point data, if conditions are met"""
    new_quarantine, is_quarantined, quarantine_end = check_quarantine(update)

    search_params = [update.pool_code, update.station_name, update.connector_id]
    search_key = get_search_key(*search_params)
    register_charge_point(*search_params)

    # Dynamically infer and update the noc based on the payload
    update_station_noc_if_needed(update.pool_code, update.station_name, update.connector_id)

    omitted_updates = 0  # When new_quarantine, the error status is sent
    if search_key in charge_points:
        charge_point_data = charge_points[search_key]
        omitted_updates += charge_point_data.omitted_updates

    # Keep the charge point quarantined and update omitted_updates only
    if is_quarantined and not new_quarantine:
        omitted_updates += 1

    charge_point_data = ChargePointData(
        pool_code=update.pool_code,
        station_name=update.station_name,
        connector_id=update.connector_id,
        charge_point_status=ChargePointStatus(update.charge_point_status),
        is_quarantined=is_quarantined,
        quarantine_end=quarantine_end,
        last_update=datetime.now(),
        omitted_updates=omitted_updates,
    )
    charge_points[search_key] = charge_point_data

    # Save the status to local SQLite
    upsert_connector_status(update.pool_code, update.station_name, update.connector_id, update.charge_point_status)

    # Update the TagoIO device for the charging pool that has the charge point
    if not is_quarantined or new_quarantine:
        await update_charge_point_status(update)

    return charge_points[search_key]


def check_quarantine(update: ChargePointUpdate, quarantine_minutes: int = 30):
    """Checks if the charge point should be quarantined or not"""
    new_quarantine, is_quarantined, quarantine_end = False, False, None

    search_params = [update.pool_code, update.station_name, update.connector_id]
    search_key = get_search_key(*search_params)
    if search_key in charge_points:
        charge_point_data = charge_points[search_key]
        is_quarantined = charge_point_data.is_quarantined
        quarantine_end = charge_point_data.quarantine_end

    # An EV has been connected to the charge point, reset the quarantine:
    if update.charge_point_status == "Preparing":
        is_quarantined = False

    # The charge point has been disconnected from the Central System:
    elif update.connection_status == "Offline":
        is_quarantined = False

    # Check if the quarantine time has ended
    elif (
        is_quarantined
        and update.charge_point_status != "Faulted"
        and quarantine_end is not None
        and datetime.now() > quarantine_end
    ):
        is_quarantined = False

    # Known error in some stations that motive the usage on this handler service
    elif (
        not is_quarantined
        and update.charge_point_status == "Faulted"
        and update.charge_point_error_code == "UnderVoltage"
    ):
        new_quarantine = True
        is_quarantined = True
        quarantine_end = datetime.now() + timedelta(minutes=quarantine_minutes)

    return new_quarantine, is_quarantined, quarantine_end


async def manage_charging_session_update(update: ChargingSessionUpdate) -> None:
    """Updates the management, and the public dashboard if any, with the session data"""

    search_key = get_search_key(update.pool_code, update.station_name, update.connector_id)

    # Store the charging session in the local db, when completed (has a time band)
    if update.time_band:  # The session has ended, we can store it in the history and remove it from the active sessions
        active_sessions.pop(search_key, None)

        transaction_id = insert_database_charging_session_history(update)
        if transaction_id is None:
            # Likely a duplicate transaction_id insert attempt, skip setting the dashboards...
            return
    else:  # The session is active. We update it in memory for the HTMX dashboard.
        active_sessions[search_key] = update

        # * If we receive metering data, the station is definitively engaged in a session.
        # Force the status to CHARGING if it's currently showing as available or preparing.
        cp_data = charge_points.get(search_key)
        if cp_data and cp_data.charge_point_status not in [
            ChargePointStatus.CHARGING,
            ChargePointStatus.SUSPENDEDEV,
            ChargePointStatus.SUSPENDEDEVSE,
        ]:
            prefix: str = "Inferring CHARGING status from session telemetry for"
            logger.info(f"{prefix} {update.station_name} [{update.connector_id}]")
            cp_data.charge_point_status = ChargePointStatus.CHARGING

            # Persist inferred status to SQLite so it survives hot-reloads
            cp_status: str = ChargePointStatus.CHARGING.value
            upsert_connector_status(update.pool_code, update.station_name, update.connector_id, cp_status)

    # Update the TagoIO dashboard/s
    await update_management_dashboard_charging_session(update)
    await update_public_dashboard_values(update)
