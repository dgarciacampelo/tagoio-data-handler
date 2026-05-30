from fastapi import APIRouter, Depends, status, HTTPException
from fastapi.security import HTTPBasic
from loguru import logger
from pydantic import ValidationError
from typing import Annotated, Optional

from config import version  # noqa: F401
from data_handling import manage_charge_point_update, get_charge_point
from schemas import ChargePointData, ChargePointUpdateBody, ChargePointUpdate
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
    charge_point_data: Optional[ChargePointData] = get_charge_point(pool_code, station_name, connector_id)
    if charge_point_data is None:
        status_code = status.HTTP_404_NOT_FOUND
        detail = "Charge point update not found"
        raise HTTPException(status_code=status_code, detail=detail)

    model_dump: dict = {}
    try:
        model_dump = charge_point_data.model_dump()
    except ValidationError as e:
        status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
        logger.error(f"Error dumping charge point data model: {str(e)}")
        raise HTTPException(status_code=status_code, detail=str(e))

    return model_dump


@router.post(
    "/{version}/charge-point-update/{pool_code}/{station_name}/{connector_id}",
    status_code=status.HTTP_200_OK,
)
async def charge_point_update(
    pool_code: int,
    station_name: str,
    connector_id: int,
    update_body: ChargePointUpdateBody,
    username: Annotated[str, Depends(check_credentials)],
):
    """Manages the notification of a station status update"""

    # Overwrite/sync the connector_id if it comes from the path parameter
    update_body.connector_id = connector_id

    update_data = ChargePointUpdate(pool_code=pool_code, station_name=station_name, **update_body.model_dump())

    charge_point_data = await manage_charge_point_update(update_data)
    if charge_point_data is None:
        raise HTTPException(status_code=status.HTTP_500_INTERNAL_SERVER_ERROR, detail="Error updating station data.")

    return charge_point_data.model_dump()
