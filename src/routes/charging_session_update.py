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
    request: Request,
    username: Annotated[str, Depends(check_credentials)],
):
    "Manages the notification of a charging session update"
    try:
        # Parse the request as a dictionary:
        json_string = await request.json()
        data = json.loads(json_string)
        update = ChargingSessionUpdate(
            pool_code=pool_code,
            station_name=station_name,
            connector_id=connector_id,
            transaction_id=data["transaction_id"],
            card_alias=data["card_alias"],
            card_code=data["card_code"],
            display_id=data["display_id"],
            start_date=data["start_date"],
            start_time=data["start_time"],
            step=data["step"],
            star_meter_value=data["star_meter_value"],
            last_meter_value=data["last_meter_value"],
            energy=data["energy"],
            energy_unit=data["energy_unit"] if "energy_unit" in data else "kWh",
            cost=data["cost"],
            cost_unit=data["cost_unit"] if "cost_unit" in data else "€",
            power=data["power"],
            power_unit=data["power_unit"] if "power_unit" in data else "W",
            time=data["time"],
            has_public_dashboard=data["has_public_dashboard"],
            stop_motive=data["stop_motive"] if "stop_motive" in data else None,
            time_band=data["time_band"] if "time_band" in data else None,
        )
        await manage_charging_session_update(update)

    except json.JSONDecodeError:
        logger.error("Error parsing JSON response at charging_session_update")
        status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
        raise HTTPException(status_code=status_code, detail="Error parsing JSON")
    except ValidationError as e:
        status_code = status.HTTP_422_UNPROCESSABLE_ENTITY
        raise HTTPException(status_code=status_code, detail=str(e))
