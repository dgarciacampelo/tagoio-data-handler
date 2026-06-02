import json
from fastapi import APIRouter, Depends, status, HTTPException
from fastapi.security import HTTPBasic
from loguru import logger
from pydantic import ValidationError
from typing import Annotated

from config import version  # noqa: F401
from schemas import PoolConfigUpdate
from security import check_credentials
from tagoio.pool_setup_fetching import update_pool_config_in_memory

router = APIRouter()
security = HTTPBasic()


"""Receives notifications of CPO info or Charging Pool rates changes"""


@router.post("/{version}/charging-pool-update", status_code=status.HTTP_200_OK)
async def charging_pool_update(
    update: PoolConfigUpdate,
    username: Annotated[str, Depends(check_credentials)],
):
    """Manages the notification of a charging pool update"""
    try:
        update_pool_config_in_memory(update)
        return {"status": "success"}

    except json.JSONDecodeError:
        logger.error("Error parsing JSON response at charging_pool_update")
        status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
        raise HTTPException(status_code=status_code, detail="Error parsing JSON")
    except ValidationError as e:
        status_code = status.HTTP_422_UNPROCESSABLE_ENTITY
        raise HTTPException(status_code=status_code, detail=str(e))
