from fastapi import APIRouter, Depends, status, HTTPException
from fastapi.security import HTTPBasic
from typing import Annotated, Any

from config import version  # noqa: F401
from schemas import device_data_dump
from security import check_credentials
from tagoio.token_fetching import (
    get_all_devices_data,
    get_device_data_by_pool_code,
    insert_device_data_by_pool_code,
    update_device_data_by_pool_code,
    delete_device_data_by_pool_code,
)

router = APIRouter()
security = HTTPBasic()


"""
REST API router to manage the device tokens required to send data to the
TagoIO platform. Each charging pool uses its own TagoIO device.
"""


@router.get("/{version}/device-token")
def get_all_device_tokens(username: Annotated[str, Depends(check_credentials)]):
    "Return all the devices data, one by one, for each pool code"
    result: list[dict[str, Any]] = list()
    devices_data = get_all_devices_data()
    for pool_code, (device_id, device_token) in devices_data.items():
        result.append(device_data_dump(pool_code, device_id, device_token))
    return result


@router.get("/{version}/device-token/{pool_code}")
def get_device_token(
    pool_code: int,
    username: Annotated[str, Depends(check_credentials)],
):
    "Return the device data by pool code, if exists"
    device_id, device_token = get_device_data_by_pool_code(pool_code)
    if device_id is None or device_token is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Device {pool_code} not found",
        )

    return device_data_dump(pool_code, device_id, device_token)


@router.post(
    "/{version}/device-token/{pool_code}/{device_id}/{device_token}",
    status_code=status.HTTP_201_CREATED,
)
def set_device_token(
    pool_code: int,
    device_id: str,
    device_token: str,
    username: Annotated[str, Depends(check_credentials)],
):
    "Defines a new device data by pool code, if it does not already exists"
    result_ok = insert_device_data_by_pool_code(pool_code, device_id, device_token)
    if not result_ok:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail=f"Device {pool_code} already exists",
        )

    return device_data_dump(pool_code, device_id, device_token)


@router.put(
    "/{version}/device-token/{pool_code}/{device_id}/{device_token}",
    status_code=status.HTTP_200_OK,
)
async def update_device_token(
    pool_code: int,
    device_id: str,
    device_token: str,
    username: Annotated[str, Depends(check_credentials)],
):
    "Updates an existing device data by pool code"
    result_ok = update_device_data_by_pool_code(pool_code, device_id, device_token)
    if not result_ok:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Device {pool_code} not found",
        )

    return device_data_dump(pool_code, device_id, device_token)


@router.delete(
    "/{version}/device-token/{pool_code}",
    status_code=status.HTTP_200_OK,
)
async def delete_device_token(
    pool_code: int,
    username: Annotated[str, Depends(check_credentials)],
):
    "Deletes an existing device data by pool code"
    device_id, device_token = delete_device_data_by_pool_code(pool_code)
    if device_id is None or device_token is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Device {pool_code} not found",
        )

    return device_data_dump(pool_code, device_id, device_token)
