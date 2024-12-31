import httpx
from loguru import logger
from typing import Any

from config import tago_api_endpoint
from enumerations import ChargingSessionStep
from schemas import ChargePointUpdate, ChargingSessionUpdate
from tagoio.data_deletion import pool_variable_cleanup
from tagoio.token_fetching import get_headers_by_pool_code
from user_interface import translate_status

device_full_message: str = "The device has reached the limit of 50000 data registers"


def get_status_key(pool_code: int, station_name: str) -> int:
    """Provides the logic that allows to find a charge point translated status"""
    return hash((pool_code, station_name))


translated_statuses: dict[int, dict[int, str]] = {}


async def insert_data_in_cloud(pool_code: int, data: dict = {}):
    url: str = f"{tago_api_endpoint}/data"
    headers = get_headers_by_pool_code(pool_code)
    # ! To avoid error: Cannot reopen a client instance, once it has been closed.
    async with httpx.AsyncClient() as client:
        response = await client.post(url, headers=headers, json=data)
        return response.json()


async def handle_variable_insert(pool_code: int, data: dict = {}):
    "Handles the data insertion using the insert_data_in_cloud function"
    try:
        result = await insert_data_in_cloud(pool_code, data)
        if "status" in result and result["status"]:
            return result

        # ? Clean device variables and retry, when the capacity limit is reached:
        if "message" in result and result["message"] == device_full_message:
            logger.warning(f"Result of cloud variable insertion: {result}")
            await pool_variable_cleanup(pool_code)

            return await insert_data_in_cloud(pool_code, data)
    except Exception as e:
        logger.error(f"Exception during cloud variable insert: {e}")


async def update_charge_point_status(update: ChargePointUpdate):
    save_charge_point_status(update)
    await update_management_dashboard_status(update)

    if update.has_public_dashboard:
        await update_public_dashboard_status(update)


def save_charge_point_status(update: ChargePointUpdate):
    status = translate_status(update.charge_point_status, update.connection_status)
    status_key = get_status_key(update.pool_code, update.station_name)

    if status_key not in translated_statuses:
        translated_statuses[status_key] = dict()

    translated_statuses[status_key][update.connector_id] = status


async def update_management_dashboard_status(update: ChargePointUpdate):
    "Updates the charge point status in the management dashboard (for owners)"
    status_key = get_status_key(update.pool_code, update.station_name)
    station_statuses = translated_statuses[status_key]

    metadata: dict[str, Any] = {}
    metadata["connection_state"] = update.connection_status
    for connector_id in station_statuses:
        metadata[f"state_{connector_id}"] = station_statuses[connector_id]

    data = {
        "variable": "state",
        "value": update.station_name,
        "group": update.station_name,
        "metadata": metadata,
        "unit": None,
        "time": None,
    }

    return await handle_variable_insert(update.pool_code, data)


async def update_public_dashboard_status(update: ChargePointUpdate):
    "Updates the charge point status in the public dashboard (for EV users)"
    status_key = get_status_key(update.pool_code, update.station_name)
    station_statuses = translated_statuses[status_key]
    data = {
        "variable": f"state_{update.station_name}_{update.connector_id}",
        "value": station_statuses[update.connector_id],
        "group": update.station_name,
        "metadata": None,
        "unit": None,
        "time": None,
    }

    return await handle_variable_insert(update.pool_code, data)


async def update_public_dashboard_values(update: ChargingSessionUpdate):
    "Updates the charging session values in the public dashboard"
    if not update.has_public_dashboard:
        return

    session_is_completed: bool = update.step == ChargingSessionStep.COMPLETED
    energy_value = 0.0 if session_is_completed else update.energy
    cost_value = 0.0 if session_is_completed else update.cost
    energy: str = f"{energy_value} {update.energy_unit}"
    cost: str = f"{cost_value} {update.cost_unit}"
    time = "0 min" if session_is_completed else update.time

    value_pairs: dict[str, str] = {"energy": energy, "cost": cost, "time": time}
    for prefix, value in value_pairs.items():
        data = {
            "variable": f"{prefix}_{update.station_name}_{update.connector_id}",
            "value": value,
            "group": f"{update.station_name}_[{update.connector_id}]",
            "metadata": None,
            "unit": None,
            "time": None,
        }
        await handle_variable_insert(update.pool_code, data)


async def update_management_dashboard_charging_session(update: ChargingSessionUpdate):
    "Updates the charging session values in the management dashboard"
    await add_charging_session_to_history(update)

    value = f"{update.station_name}_[{update.connector_id}]"
    metadata = {
        "card_alias": update.card_alias,
        "display_id": update.display_id,
        "meter_values": f"[{update.star_meter_value}, {update.last_meter_value}]",
        "start_date": update.start_date,
        "start_time": update.start_time,
        "step": update.step,
        "power": update.power,
        "energy": update.energy,
        "time": update.time,
    }
    data = {
        "variable": "active_cs_data",
        "value": value,
        "group": value,
        "metadata": metadata,
        "unit": None,
        "time": None,
    }
    return await handle_variable_insert(update.pool_code, data)


async def add_charging_session_to_history(update: ChargingSessionUpdate):
    "Adds the charging session to the private dashboard history, once completed"
    if update.step != ChargingSessionStep.COMPLETED:
        return

    if update.cost == 0.0:  # Dont add sessions without cost to the history
        return

    metadata = {
        "card_alias": update.card_alias,
        "card_code": update.card_code,
        "display_id": update.display_id,
        "start_date": update.start_date,
        "step": update.step,
        "energy": update.energy,
        "energy_unit": update.energy_unit,
        "cost": update.cost,
        "cost_unit": update.cost_unit,
        "stop_motive": update.stop_motive,
        "time_band": update.time_band,
    }
    data = {
        "variable": "charging_session_data",
        "value": update.transaction_id,
        "group": f"{update.station_name}_[{update.connector_id}]",
        "metadata": metadata,
        "unit": None,
        "time": None,
    }
    return await handle_variable_insert(update.pool_code, data)
