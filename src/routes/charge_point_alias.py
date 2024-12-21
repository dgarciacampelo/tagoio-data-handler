from fastapi import APIRouter, Depends, status, HTTPException
from fastapi.security import HTTPBasic
from typing import Annotated

from config import version  # noqa: F401
from charge_points import (
    get_pool_known_charge_point_aliases,
    get_all_known_charge_point_aliases,
)
from security import check_credentials

router = APIRouter()
security = HTTPBasic()


"""
REST API router to manage charge point status updates from the OCPP server to
the TagoIO platform (as user interface) with this service as middleware.
"""


@router.get(
    "/{version}/charge-point-alias/{pool_code}",
    status_code=status.HTTP_200_OK,
)
def get_pool_charge_point_alias(
    pool_code: int,
    username: Annotated[str, Depends(check_credentials)],
):
    "Provides the known charge point aliases for a pool code"
    charge_point_aliases = get_pool_known_charge_point_aliases(pool_code)
    if len(charge_point_aliases) == 0:
        status_code = status.HTTP_404_NOT_FOUND
        detail = "Charge point aliases not found"
        raise HTTPException(status_code=status_code, detail=detail)

    return {f"charge_point_aliases_({pool_code})": charge_point_aliases}


@router.get(
    "/{version}/charge-point-alias",
    status_code=status.HTTP_200_OK,
)
def get_all_charge_point_alias(
    username: Annotated[str, Depends(check_credentials)],
):
    "Provides the known charge point aliases for a pool code"
    charge_point_aliases = get_all_known_charge_point_aliases()
    if len(charge_point_aliases) == 0:
        status_code = status.HTTP_404_NOT_FOUND
        detail = "Charge point aliases not found"
        raise HTTPException(status_code=status_code, detail=detail)

    return {
        f"charge_point_aliases_({pool_code})": charge_point_alias
        for pool_code, charge_point_alias in charge_point_aliases.items()
    }
