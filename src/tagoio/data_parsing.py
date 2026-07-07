import asyncio
from typing import Any, Optional

import httpx
from loguru import logger

from config import tago_api_endpoint
from enumerations import AvailabilityType, ChargePointStatus, ChargingSessionStep, ConnectionStatus, ValidationAlert
from schemas.ocpp_csms import ChargePointUpdate, ChargingSessionUpdate, FeedbackMessage
from tagoio.data_deletion import delete_variable_in_cloud, pool_variable_cleanup
from tagoio.token_fetching import get_headers_by_pool_code
from user_interface import translate_status
from utils.http_client import GlobalHTTPClient

device_full_message: str = "The device has reached the limit of 50000 data registers"


def get_status_key(pool_code: int, station_name: str) -> int:
    "Provides the logic that allows to find a charge point translated status"
    return hash((pool_code, station_name))


translated_statuses: dict[int, dict[int, str]] = {}


async def insert_data_in_cloud(pool_code: int, data: dict = {}):
    url: str = f"{tago_api_endpoint}/data"
    headers = get_headers_by_pool_code(pool_code)
    client = GlobalHTTPClient.get_client()
    response = await client.post(url, headers=headers, json=data)
    response.raise_for_status()
    return response.json()


async def handle_variable_insert(pool_code: int, data: Optional[dict] = None):
    """
    Handles the data insertion using the insert_data_in_cloud function
    # * Positive result: {"status": true, "result": 20700}
    # ! Negative result: {"status": false, "message": "Authorization denied"}
    """
    if data is None:  # To avoid mutable default argument issues
        data = {}

    try:
        result = await insert_data_in_cloud(pool_code, data)

        # * Use .get() to safely access dictionary keys
        if result.get("status"):
            return result

        # ? Clean device variables and retry when the capacity limit is reached
        error_message = result.get("message")
        if error_message:
            logger.warning(f"Result of cloud variable insertion ({pool_code}): {result}")

            if error_message == device_full_message:
                logger.info(f"Capacity limit reached for Pool {pool_code}. Executing cleanup and retrying...")
                # ! Disabled (long background task): await device_data_amount_check()
                # Fast, targeted cleanup for this specific Pool
                await pool_variable_cleanup(pool_code)

                # Retry the insertion. If this fails, it will trigger the except block below.
                return await insert_data_in_cloud(pool_code, data)

        else:
            logger.error(f"Failed cloud variable insertion ({pool_code}) - Unknown format: {result}")

    except httpx.TimeoutException as e:  # Expected behavior when TagoIO platform is not behaving properly.
        logger.warning(f"TagoIO timeout dropping payload for Pool {pool_code}: {e}")

    except httpx.HTTPStatusError as e:  # E.g., 401 Unauthorized, 500 Internal Server Error
        response_text = getattr(e.response, "text", "")
        logger.error(f"TagoIO HTTP error ({e.response.status_code}) for Pool {pool_code} | Body: {response_text}")

    except Exception as e:  # Truly unexpected exceptions (e.g., TypeError, KeyError)
        error_details = str(e)  # ? logger.exception appends the full stack trace to the log output.
        logger.exception(f"Unexpected exception during cloud variable insertion ({pool_code}): {error_details}")


async def send_feedback_message(feedback: FeedbackMessage):
    "Inserts a feedback message to be shown in a dashboard linkend to the Pool"
    data = {
        "variable": feedback.variable,
        "value": feedback.message,
        "group": feedback.group,
        "metadata": {"type": feedback.type},
        "unit": None,
        "time": None,
    }
    await handle_variable_insert(feedback.pool_code, data)


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
        "metadata": metadata,  # ConnectionStatus enum used for the connection status icon
        "unit": None,
        "time": None,
    }

    logger.debug(f"Updating Management Dashboard for {update.pool_code}/{update.station_name} status: {data}")
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

    # STATE SYNC FIX (Fires if Handler restarts during an active session)
    if update.step != ChargingSessionStep.COMPLETED:
        status_key = get_status_key(update.pool_code, update.station_name)
        current_status = translated_statuses.get(status_key, {}).get(update.connector_id)
        expected_status = translate_status(ChargePointStatus.CHARGING, ConnectionStatus.ONLINE)

        if current_status != expected_status:
            target: str = f"{update.pool_code}/{update.station_name} [{update.connector_id}]"
            logger.info(f"Handler restart / sync loss detected for {target}. Forcing CHARGING status.")

            # Forge a status update to explicitly correct the cache and both dashboards
            cp_update = ChargePointUpdate(
                pool_code=update.pool_code,
                station_name=update.station_name,
                connector_id=update.connector_id,
                connection_status=ConnectionStatus.ONLINE,  # Implicitly online if sending session updates
                charge_point_status=ChargePointStatus.CHARGING,
                availability_type=AvailabilityType.OPERATIVE,
                charge_point_error_code="NoError",
                has_public_dashboard=update.has_public_dashboard,
            )
            await update_charge_point_status(cp_update)

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
    pool_code: int = update.pool_code
    history_result = await add_charging_session_to_history(update)
    if history_result:
        trans_id: int = update.transaction_id
        logger.warning(f"cs history log {trans_id} result {(pool_code)}: {history_result}")

    value = f"{update.station_name}_[{update.connector_id}]"
    metadata = {
        "card_alias": update.card_alias,
        "display_id": update.display_id,
        "meter_values": f"[{update.start_meter_value}, {update.last_meter_value}]",
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
    if update.step != ChargingSessionStep.COMPLETED or update.cost == 0.0:
        """
        message_prefix = f"Session {update.transaction_id} history log skipped"
        logger.info(f"{message_prefix} due step {update.step} or cost {update.cost}")
        """
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
    # ! "group": update.transaction_id is necessary for session not to be grouped in the dashboard
    data = {
        "variable": "charging_session_data",
        "value": update.transaction_id,
        "group": str(update.transaction_id),
        "metadata": metadata,
        "unit": None,
        "time": None,
    }
    # logger.info(f"Adding charging session to history: {data}")
    return await handle_variable_insert(update.pool_code, data)


async def show_validation_feedback(pool_code: int, variable: str, message: str, result_ok: bool = True):
    """
    Triggers a form validation toast in the TagoIO dashboard.
    Inserts the variable with the alert metadata, then immediately deletes it.
    """
    alert_type = ValidationAlert.ACCEPT if result_ok else ValidationAlert.REJECT

    # Construct the data payload matching TagoIO's expectation
    data_payload = {
        "variable": variable,
        "value": message,
        "group": "validation_feedback",
        "metadata": {"type": alert_type.value},
    }

    await handle_variable_insert(pool_code, data_payload)
    await asyncio.sleep(10)
    await delete_variable_in_cloud(pool_code, variable, keep_weeks=0)
