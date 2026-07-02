"""
Thin handlers for the SSE Broker Service.
These functions parse the raw TagoIO scope into strict Pydantic events
and broadcast them to the SSE stream.
"""

import asyncio
import json
from typing import Optional

from loguru import logger

from schemas.analysis import (
    ChangeAvailabilityEvent,
    CPOInfoEvent,
    LoadBalancingEvent,
    MaxGridPowerEvent,
    PowerUpdateEvent,
    RateListEvent,
    RFIDManagementEvent,
)
from database.query_database import get_database_pool_code_by_device_id
from sse_broker import event_broker
from tagoio.data_deletion import delete_variable_in_cloud
from tagoio.data_parsing import handle_variable_insert, show_validation_feedback
from tagoio.setup_devices import feed_and_return_all_devices_tokens
from tagoio.token_fetching import get_device_data_by_pool_code

known_devices: dict[str, int] = {}  # Maps device_id to pool_code for quick lookup


def get_pool_code_by_device_id(device_id: str) -> Optional[int]:
    """Retrieves the Pool code based on the device ID, utilizing a local cache and triggering a cloud sync if missing."""

    if device_id not in known_devices:
        # 1. Check the local SQLite database
        pool_candidate = get_database_pool_code_by_device_id(device_id)

        # 2. If completely missing, the local DB is out of sync with TagoIO
        if pool_candidate is None:
            logger.warning(f"Device ID {device_id} missing from local DB. Triggering global TagoIO refresh...")

            # Trigger a global fetch to resync the local database
            refreshed_devices = feed_and_return_all_devices_tokens()

            # Scan the newly fetched data to find the Pool code for our mystery device
            for p_code, (d_id, d_token) in refreshed_devices.items():
                if d_id == device_id:
                    pool_candidate = p_code
                    break

        # 3. If it is STILL None, the device literally doesn't exist in the TagoIO account
        if pool_candidate is None:
            logger.error(f"FATAL: Device ID {device_id} not found in TagoIO account after global refresh.")
            return None

        # 4. Cache it in active memory for next time
        known_devices[device_id] = pool_candidate

    return known_devices[device_id]


async def change_availability(context, scope):
    """Translates a TagoIO availability scope into an SSE event."""
    try:
        device_id = scope[0]["device"]
        pool_code = get_pool_code_by_device_id(device_id)

        if pool_code is None:
            logger.error(f"Cannot process Availability Event: Unknown Pool code for device {device_id}")
            return

        event = ChangeAvailabilityEvent(pool_code=pool_code, station_name=str(scope[0]["value"]))

        logger.info(f"Broadcasting Availability Event for Pool {pool_code}")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse change_availability payload: {e}")


async def manage_rfid(context, scope):
    """Translates a TagoIO RFID scope into an SSE event and updates Cloud UI."""

    device_id = scope[0]["device"]
    pool_code = get_pool_code_by_device_id(device_id)
    if pool_code is None:
        logger.error(f"Cannot process RFID Event: Unknown Pool code for device {device_id}")
        return

    try:
        card_id = str(scope[0]["value"])
        value, group = card_id.lower(), card_id.upper()
        is_create = len(scope) > 1 and scope[4].get("value") == "create"

        if is_create:
            linked_cps_str = str(scope[3].get("value", ""))
            alias = str(scope[1].get("value", "")) if "value" in scope[1] else None
            email = str(scope[2].get("value", "")) if "value" in scope[2] else None

            event = RFIDManagementEvent(
                pool_code=pool_code,
                card_id=card_id.lower(),
                action="create",
                linked_cps=[cp.strip() for cp in linked_cps_str.split(",") if cp.strip()],
                alias=alias.upper() if alias else card_id.upper(),
                email=email,
            )

            # * 1. Broadcast to CSMS
            logger.info(f"Broadcasting RFID {event.action} for Pool {pool_code}")
            await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

            # * 2. Update TagoIO Device Data
            rfid_data = {
                "variable": "card_id",
                "value": value,
                "group": group,
                "metadata": {
                    "alias": alias.upper() if alias else group,
                    "email": email if email else "",
                    "cps": linked_cps_str,
                },
            }
            # Remove old variable before insert to simulate remove_and_insert_variable
            await delete_variable_in_cloud(pool_code, "card_id", keep_weeks=0, group=group)
            await asyncio.sleep(1.0)
            await handle_variable_insert(pool_code, rfid_data)

            # * 3. UI Feedback
            await show_validation_feedback(pool_code, "validation_rfid", "OK", True)

        else:
            linked_cps_str = str(scope[0]["metadata"].get("cps", ""))
            event = RFIDManagementEvent(
                pool_code=pool_code,
                card_id=value,
                action="delete",
                linked_cps=[cp.strip() for cp in linked_cps_str.split(",") if cp.strip()],
            )

            # * 1. Broadcast to CSMS
            logger.info(f"Broadcasting RFID {event.action} for Pool {pool_code}")
            await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

            # * 2. Clean TagoIO Device Data
            await delete_variable_in_cloud(pool_code, "card_id", keep_weeks=0, group=group)

            # * 3. UI Feedback
            await show_validation_feedback(pool_code, "validation_rfid", "OK", True)

    except Exception as e:
        logger.error(f"Failed to parse manage_rfid payload: {e}")
        await show_validation_feedback(pool_code, "validation_rfid", "ERROR", False)


async def change_max_grid_power(context, scope):
    """Translates a TagoIO max power scope into an SSE event."""
    try:
        device_id = scope[0]["device"]
        pool_code = get_pool_code_by_device_id(device_id)

        if pool_code is None:
            logger.error(f"Cannot process Max Grid Power Event: Unknown Pool code for device {device_id}")
            return

        event = MaxGridPowerEvent(pool_code=pool_code, max_power_watts=float(scope[0]["value"]))

        logger.info(f"Broadcasting Max Power Event for Pool {pool_code}")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse change_max_grid_power payload: {e}")


async def change_cpo_info(context, scope):
    """Translates a TagoIO CPO info scope into an SSE event."""
    try:
        device_id = scope[0]["device"]
        pool_code = get_pool_code_by_device_id(device_id)

        if pool_code is None:
            logger.error(f"Cannot process CPO Info Event: Unknown Pool code for device {device_id}")
            return

        # Extract fields based on the legacy scope array order
        name = str(scope[0].get("value", ""))
        raw_fiscal_id = str(scope[1].get("value", ""))
        address = str(scope[2].get("value", ""))
        phone = str(scope[3].get("value", ""))
        email = str(scope[4].get("value", ""))

        # Handle the optional web field gracefully
        raw_web = scope[5].get("value")
        web = str(raw_web) if raw_web else ""

        # Clean up the fiscal ID (remove spaces and periods)
        fiscal_id = "".join(raw_fiscal_id.split()).replace(".", "")

        # Instantiate the strict Pydantic event
        event = CPOInfoEvent(
            pool_code=pool_code,
            name=name,
            fiscal_id=fiscal_id,
            address=address,
            phone=phone,
            email=email,
            web=web,
        )

        logger.info(f"Broadcasting CPO Info Event for Pool {pool_code}")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse change_cpo_info payload: {e}")


async def change_rate_list(context, scope):
    """Translates a TagoIO Rate List scope into an SSE event."""
    try:
        device_id = scope[0]["device"]
        pool_code = get_pool_code_by_device_id(device_id)

        if pool_code is None:
            logger.error(f"Cannot process Rate List Event: Unknown Pool code for device {device_id}")
            return

        # Extract floats safely, defaulting withholding to 40.0 if missing or malformed
        rate_off_peak = float(scope[0]["value"])
        rate_flat = float(scope[1]["value"])
        rate_peak = float(scope[2]["value"])
        vat = float(scope[3]["value"])

        try:
            withholding_amount = float(scope[4]["value"])
        except (ValueError, KeyError, IndexError):
            withholding_amount = 40.0

        # Instantiate the strict Pydantic event
        event = RateListEvent(
            pool_code=pool_code,
            rate_off_peak=rate_off_peak,
            rate_flat=rate_flat,
            rate_peak=rate_peak,
            vat=vat,
            withholding_amount=withholding_amount,
        )

        logger.info(f"Broadcasting Rate List Event for Pool {pool_code} (Withholding: {withholding_amount}€)")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse change_rate_list payload: {e}")


async def change_load_balancing_mode(context, scope):
    """Translates a TagoIO Load Balancing Mode scope into an SSE event."""
    try:
        device_id = scope[0]["device"]
        pool_code = get_pool_code_by_device_id(device_id)

        if pool_code is None:
            logger.error(f"Cannot process Load Balancing Event: Unknown Pool code for device {device_id}")
            return

        # Extract the selected mode directly from the payload
        selected_mode = str(scope[0].get("value", ""))

        # Instantiate the strict Pydantic event
        event = LoadBalancingEvent(pool_code=pool_code, selected_mode=selected_mode)

        logger.info(f"Broadcasting Load Balancing Event for Pool {pool_code} (Mode: '{selected_mode}')")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse change_load_balancing_mode payload: {e}")


async def power_consumption_update(context, scope):
    """Translates a TagoIO Power Consumption scope into an SSE event."""
    try:
        if not scope:
            return

        data = scope[0]
        device_id = data.get("device")
        pool_code = None
        meter_watts = 0.0

        # 1. Extract the Power Value (Supporting 3 different payload shapes)
        if "params" in data and "P" in data["params"]:
            # Shape A: Pre-parsed MQTT JSON (Current Scenario)
            meter_watts = float(data["params"]["P"])

        elif "topic" in data and "payload" in data:
            # Shape B: Raw stringified MQTT JSON
            try:
                mqtt_payload = json.loads(data["payload"])
                meter_watts = float(mqtt_payload.get("params", {}).get("P", 0.0))
            except (json.JSONDecodeError, ValueError, KeyError) as e:
                logger.error(f"Error parsing nested MQTT JSON payload: {e}")
                return

        else:
            # Shape C: Standard TagoIO Variable
            meter_watts = float(data.get("value", 0.0))

        # 2. Resolve the Pool Code
        if device_id:
            pool_code = get_pool_code_by_device_id(device_id)

        # Fallback: Extract from topic if device_id was stripped by TagoIO MQTT integration
        if pool_code is None:  # Look for the topic either at the root, or inside metadata
            topic = data.get("topic") or data.get("metadata", {}).get("mqtt_topic")
            if topic:
                try:
                    # e.g., "Bivocom/MASTER-BUSINESS-221006" -> 221006
                    pool_code = int(topic.split("-")[-1])
                except (ValueError, IndexError):
                    pass

        if pool_code is None:
            logger.error(f"Cannot process Power Update Event: Unknown Pool code for payload: {data}")
            return

        # 3. Resolve Missing Device ID (Reverse Lookup)
        if not device_id:
            # With the pool_code extracted from the string, we need to fetch the device_id for the Pydantic schema
            fetched_device_id, _ = get_device_data_by_pool_code(pool_code)
            device_id = fetched_device_id or f"unmapped-pool-{pool_code}"

        # 4. Instantiate and Broadcast
        event = PowerUpdateEvent(pool_code=pool_code, meter_watts=meter_watts)

        logger.debug(f"Broadcasting Power Update Event for Pool {pool_code} ({meter_watts} W)")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse power_consumption_update payload: {repr(e)}")
