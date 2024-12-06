import json
from fastapi import APIRouter, Depends, status, HTTPException, Request
from fastapi.security import HTTPBasic
from loguru import logger
from pydantic import ValidationError
from typing import Annotated

from config import version  # noqa: F401
from data_handling import manage_charge_point_update, get_charge_point
from schemas import ChargePointUpdateBody, ChargePointUpdate
from security import check_credentials

router = APIRouter()
security = HTTPBasic()


"""
REST API router to manage charge point status updates from the OCPP server to
the TagoIO platform (as user interface) with this service as middleware.
"""


@router.get(
    "/{version}/charge-point-update/{pool_code}/{station_name}/{connector_id}",
    status_code=status.HTTP_200_OK,
)
def get_charge_point_last_update(
    pool_code: int,
    station_name: str,
    connector_id: int,
    username: Annotated[str, Depends(check_credentials)],
):
    "Provides the last charge point status update"
    charge_point_data = get_charge_point(pool_code, station_name, connector_id)
    if charge_point_data is None:
        status_code = status.HTTP_404_NOT_FOUND
        detail = "Charge point update not found"
        raise HTTPException(status_code=status_code, detail=detail)

    return charge_point_data.model_dump()


@router.post(
    "/{version}/charge-point-update/{pool_code}/{station_name}/{connector_id}",
    status_code=status.HTTP_200_OK,
)
# username: Annotated[str, Depends(check_credentials)],
async def charge_point_update(
    pool_code: int,
    station_name: str,
    connector_id: int,
    request: Request,
    username: Annotated[str, Depends(check_credentials)],
):
    "Manages the notification of a charge point status update"
    try:
        # Parse the request as a dictionary:
        json_string = await request.json()
        data = json.loads(json_string)
        update_body = ChargePointUpdateBody(
            connector_id=connector_id,
            connection_status=data["connection_status"],
            charge_point_status=data["charge_point_status"],
            availability_type=data["availability_type"],
            charge_point_error_code=data["charge_point_error_code"],
            has_pulic_dashboard=data["has_pulic_dashboard"],
        )
        update_data = ChargePointUpdate(
            pool_code=pool_code, station_name=station_name, **update_body.model_dump()
        )
        charge_point_data = await manage_charge_point_update(update_data)
        if charge_point_data is None:
            status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
            detail = "Error updating charge point"
            raise HTTPException(status_code=status_code, detail=detail)

        return charge_point_data.model_dump()

    except json.JSONDecodeError:
        logger.error("Error parsing JSON response at charge_point_update")
        status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
        raise HTTPException(status_code=status_code, detail="Error parsing JSON")
    except ValidationError as e:
        status_code = status.HTTP_422_UNPROCESSABLE_ENTITY
        raise HTTPException(status_code=status_code, detail=str(e))
