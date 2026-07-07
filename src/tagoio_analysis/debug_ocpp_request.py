from typing import Literal, cast

from loguru import logger

from schemas.analysis import (
    ChangeAvailabilityPayload,
    OCPPRequestEvent,
    RemoteStartPayload,
    RemoteStopPayload,
    ResetPayload,
    StatusNotificationPayload,
    UnlockConnectorPayload,
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

        raw_station_names = str(scope[0]["value"])
        station_names = [sn.strip() for sn in raw_station_names.split(",") if sn.strip()]
        # Cast the request type literal to satisfy Pylance for the StatusNotification payload mapping
        request_type = str(scope[1]["value"])

        payload_model = None

        if request_type == "status_notification":
            payload_model = StatusNotificationPayload(
                request="status_notification", station_names=station_names, connector_id=int(scope[2]["value"])
            )

        elif request_type == "change_availability":
            # Map TagoIO dashboard's "available"/"unavailable" to OCPP's "Operative"/"Inoperative"
            ui_value = str(scope[3]["value"]).lower()
            ocpp_avail = "Operative" if ui_value == "available" else "Inoperative"

            payload_model = ChangeAvailabilityPayload(
                request="change_availability",
                station_names=station_names,
                connector_id=int(scope[2]["value"]),
                availability_type=cast(Literal["Operative", "Inoperative"], ocpp_avail),
            )

        elif request_type == "reset":
            # Cast the dynamic string to the specific Literal expected by Pydantic
            raw_reset = str(scope[2]["value"]).capitalize()
            reset_type = cast(Literal["Soft", "Hard"], raw_reset)

            payload_model = ResetPayload(request="reset", station_names=station_names, reset_type=reset_type)

        elif request_type == "remote_start_transaction":
            payload_model = RemoteStartPayload(
                request="remote_start_transaction",
                station_names=station_names,
                connector_id=int(scope[2]["value"]),
                id_tag=str(scope[3]["value"]),
            )

        elif request_type == "remote_stop_transaction":
            payload_model = RemoteStopPayload(request="remote_stop_transaction", transaction_id=int(scope[2]["value"]))

        elif request_type == "unlock_connector":
            payload_model = UnlockConnectorPayload(
                request="unlock_connector", station_names=station_names, connector_id=int(scope[2]["value"])
            )

        if payload_model is None:
            logger.warning(f"Ignored unsupported OCPP request: '{request_type}'. Raw payload: {scope}")
            return

        event = OCPPRequestEvent(pool_code=pool_code, payload=payload_model)

        logger.info(f"Broadcasting OCPP '{request_type}' Event for Pool {pool_code}")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

    except Exception as e:
        logger.error(f"Failed to parse ocpp_request payload: {e}")
