from typing import Literal, cast

from loguru import logger

from analysis_schemas import (
    ChangeAvailabilityPayload,
    OCPPRequestEvent,
    RemoteStartPayload,
    RemoteStopPayload,
    ResetPayload,
    StatusNotificationPayload,
)
from sse_broker import event_broker
from tagoio_analysis.analysis_callable import get_pool_code_by_device_id


async def ocpp_requests(context, scope):
    """Translates Debug Tab scopes into strict OCPP request events."""
    try:
        device_id = scope[0]["device"]
        pool_code = get_pool_code_by_device_id(device_id)

        if pool_code is None:
            logger.error(f"Cannot process OCPP Request: Unknown Pool code for device {device_id}")
            return

        serial_ids_str = str(scope[0]["value"])
        # Cast the request type literal to satisfy Pylance for the StatusNotification payload mapping
        request_type = str(scope[1]["value"])

        payload_model = None

        if request_type == "status_notification":
            payload_model = StatusNotificationPayload(
                request="status_notification", connector_id=int(scope[2]["value"])
            )

        elif request_type == "change_availability":
            # Cast the dynamic string to the specific Literal expected by Pydantic
            avail_type = cast(Literal["available", "unavailable"], str(scope[3]["value"]))

            payload_model = ChangeAvailabilityPayload(
                request="change_availability", connector_id=int(scope[2]["value"]), availability_type=avail_type
            )

        elif request_type == "reset":
            # Cast the dynamic string to the specific Literal expected by Pydantic
            reset_type = cast(Literal["soft", "hard"], str(scope[2]["value"]))

            payload_model = ResetPayload(request="reset", reset_type=reset_type)

        elif request_type == "remote_start_transaction":
            payload_model = RemoteStartPayload(
                request="remote_start_transaction",
                connector_id=int(scope[2]["value"]),
                id_tag=str(scope[3]["value"]),
                station_name="Placeholder",  # Handled downstream
            )

        elif request_type == "remote_stop_transaction":
            payload_model = RemoteStopPayload(request="remote_stop_transaction", transaction_id=int(scope[2]["value"]))

        if payload_model is None:
            logger.warning(f"Ignored unsupported or deprecated OCPP request: {request_type}")
            return

        event = OCPPRequestEvent(
            tago_device_id=device_id,
            pool_code=pool_code,
            serial_ids=[sid.strip() for sid in serial_ids_str.split(",") if sid.strip()],
            payload=payload_model,
        )

        logger.info(f"Broadcasting OCPP '{request_type}' Event for Pool {pool_code}")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse ocpp_request payload: {e}")
