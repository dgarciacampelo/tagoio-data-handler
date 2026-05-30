import json
from fastapi import APIRouter, Depends, status, HTTPException, Request
from fastapi.security import HTTPBasic
from loguru import logger
from pydantic import ValidationError
from typing import Annotated

from config import version  # noqa: F401
from data_handling import manage_charging_session_update
from schemas import ChargingSessionUpdate
from security import check_credentials

router = APIRouter()
security = HTTPBasic()


"""
REST API router to manage charging session updates from the OCPP server to
the TagoIO platform (as user interface) with this service as middleware.
"""


@router.post(
    "/{version}/charging-session-update/{pool_code}/{station_name}/{connector_id}",
    status_code=status.HTTP_200_OK,
)
async def charging_session_update(
    pool_code: int,
    station_name: str,
    connector_id: int,
    update: ChargingSessionUpdate,
    username: Annotated[str, Depends(check_credentials)],
):
    """Manages the notification of a charging session update"""
    try:
        # Sync path variables with the body for easier handling in the data management function
        update.pool_code = pool_code
        update.station_name = station_name
        update.connector_id = connector_id
        await manage_charging_session_update(update)

        return {"status": "success"}

    except json.JSONDecodeError:
        logger.error("Error parsing JSON response at charging_session_update")
        status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
        raise HTTPException(status_code=status_code, detail="Error parsing JSON")
    except ValidationError as e:
        status_code = status.HTTP_422_UNPROCESSABLE_ENTITY
        raise HTTPException(status_code=status_code, detail=str(e))
