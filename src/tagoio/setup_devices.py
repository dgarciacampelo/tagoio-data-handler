from loguru import logger

from config import tago_device_prefix
from database import (
    check_tagoio_device_table,
    get_all_database_tagoio_devices,
    insert_database_tagoio_device,
)
from tagoio.aux_functions import list_devices, get_device_last_token


def setup_all_devices_tokens():
    check_tagoio_device_table()

    devices: dict[int, tuple[str, str]] = dict()
    local_device_rows = list()
    logger.info("Preparing devices data...")
    try:
        local_device_rows = get_all_database_tagoio_devices()
    except Exception as e:
        logger.error(f"Exception during get_all_database_tagoio_devices: {e}")

    if len(local_device_rows) > 0:
        # Local data is not empty, use it
        for row in local_device_rows:
            pool_code, device_id, device_token = row
            devices[pool_code] = device_id, device_token
        logger.info(f"Database contains {len(devices)} devices data.")
        return devices
    else:
        logger.info("Local data is empty, fetch tokens from TagoIO...")
        try:
            devices = fetch_all_devices_tokens()
            for pool_code, (device_id, device_token) in devices.items():
                insert_database_tagoio_device(pool_code, device_id, device_token)
            return devices
        except Exception as e:
            logger.error(f"Exception during fetch_all_devices_tokens: {e}")
            return dict()


def fetch_all_devices_tokens() -> dict[int, tuple[str, str]]:
    "Provides the device_id, device_token for each TagoIO device, by pool code"
    devices: dict[int, tuple[str, str]] = dict()

    try:
        device_list = list_devices()["result"]
        for device in device_list:
            if tago_device_prefix not in device["name"]:
                continue
            pool_code = int(device["name"].split("-")[2])
            device_id = device["id"]
            device_token = get_device_last_token(device_id)
            devices[pool_code] = device_id, device_token
    except Exception as e:
        logger.error(f"Exception during fetch_all_devices_tokens: {e}")

    return devices