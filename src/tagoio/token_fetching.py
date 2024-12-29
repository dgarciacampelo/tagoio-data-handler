from loguru import logger

from config import tago_account_token, tago_device_prefix
from database.query_database import (
    insert_database_tagoio_device,
    update_database_tagoio_device,
    delete_database_tagoio_device,
)
from tagoio.aux_functions import list_devices, get_device_last_token
from tagoio.setup_devices import setup_all_devices_tokens


# device_id, device_token for each TagoIO device (one device for each pool)
devices_data_by_pool_code: dict[int, tuple[str, str]] = setup_all_devices_tokens()


def get_all_devices_data() -> dict[int, tuple[str, str]]:
    "Provides the device_id, device_token for each TagoIO device, by pool code"
    return devices_data_by_pool_code


def get_device_data_by_pool_code(pool_code: int) -> tuple[str, str]:
    "Provides the device_id, device_token for a single device, by pool code"
    if pool_code not in devices_data_by_pool_code:
        device_id, device_token = fetch_device_token_by_pool_code(pool_code)
        if device_id is None or device_token is None:
            return None, None

        insert_device_data_by_pool_code(pool_code, device_id, device_token)

    device_id, device_token = devices_data_by_pool_code[pool_code]
    return device_id, device_token


def insert_device_data_by_pool_code(
    pool_code: int, device_id: str, device_token: str
) -> bool:
    "Defines a new device data by pool code, if it does not already exists"
    if pool_code in devices_data_by_pool_code:
        return False

    devices_data_by_pool_code[pool_code] = device_id, device_token
    insert_database_tagoio_device(pool_code, device_id, device_token)
    return True


def update_device_data_by_pool_code(
    pool_code: int, device_id: str, device_token: str
) -> bool:
    "Updates an existing device data by pool code"
    if pool_code not in devices_data_by_pool_code:
        return False

    devices_data_by_pool_code[pool_code] = device_id, device_token
    update_database_tagoio_device(pool_code, device_id, device_token)
    return True


def delete_device_data_by_pool_code(pool_code: int) -> tuple[str, str]:
    "Deletes an existing device data by pool code"
    if pool_code not in devices_data_by_pool_code:
        return None, None

    device_id, device_token = devices_data_by_pool_code[pool_code]
    del devices_data_by_pool_code[pool_code]
    delete_database_tagoio_device(pool_code)
    return device_id, device_token


def get_headers(token: str = None) -> dict[str, str]:
    "Provides the headers for the TagoIO API, using account or device token"
    if token is None:
        return {
            "content-type": "application/json",
            "Account-Token": tago_account_token,
        }

    return {
        "content-type": "application/json",
        "Device-Token": token,
    }


def get_headers_by_pool_code(pool_code: int) -> dict[str, str]:
    "If a pool code is not found, the acount token is used for the headers"
    device_id, device_token = get_device_data_by_pool_code(pool_code)
    return get_headers(device_token)


def fetch_device_token_by_pool_code(pool_code: int) -> tuple[str, str]:
    "Setups the device_id, device_token for a single TagoIO device, by pool code"
    logger.info(f"Fetching device id and token for pool code: {pool_code}...")
    device_list = list_devices()["result"]
    for device in device_list:
        if tago_device_prefix not in device["name"]:
            continue
        if str(pool_code) not in device["name"]:
            continue
        try:
            device_id = device["id"]
            device_token = get_device_last_token(device_id)
            return device_id, device_token
        except Exception as e:
            logger.error(f"Exception during fetch_device_token_by_pool_code: {e}")

    return None, None


def search_device(search_for, account_token=tago_account_token) -> tuple[str, str]:
    "Searchs for a device by name, and returns the device_id, device_token"
    device_list = list_devices()["result"]
    for device in device_list:
        if tago_device_prefix not in device["name"]:
            continue
        pool_code = int(device["name"].split("-")[2])
        if pool_code == search_for:
            device_id = device["id"]
            device_token = get_device_last_token(device_id)
            return device_id, device_token

    return None, None
