import httpx
from asyncio import sleep as asyncio_sleep
from datetime import datetime, timedelta
from loguru import logger

from config import tago_api_endpoint, app_default_user, app_default_token, port, version
from charge_points import known_charge_points
from tagoio.token_fetching import get_headers_by_pool_code, get_all_devices_data
from telegram_utils import send_telegram_notification

"""
! The TagoIO platform allows 50_000 registers per device at most. When the
limit is reached, following requests will error and no new data will be stored.
"""

base_url: str = f"{tago_api_endpoint}/data?variable="


async def delete_variable_in_cloud(
    pool_code: int, variable: str, keep_weeks: int = 26
) -> dict:
    """
    Uses TagoIO API for variable deletion, keeping the remain weeks of data
    Returns: {'status': True, 'result': 'X Data Removed'} with X: integer

    Maximum quantity of data to be deleted: 5000 registers at once.
    """
    end_datetime = datetime.now() - timedelta(weeks=keep_weeks)
    end_date = end_datetime.strftime("%Y-%m-%d")
    start_date = "2020-01-01"
    qty = 1000  # ? Otherwise the default is 15

    headers = get_headers_by_pool_code(pool_code)
    url = f"{base_url}{variable}&start_date={start_date}&end_date={end_date}&qty={qty}"
    if keep_weeks == 0:
        url = f"{base_url}{variable}&qty=5000"

    delete_count: int = 0
    try:
        # ! To avoid error:  Cannot open a client instance more than once.
        async with httpx.AsyncClient() as client:
            response = await client.delete(url, headers=headers)
            delete_count = handle_delete_response(pool_code, response.json())
            telegram_prefix: str = "No se han borrado registros"
            if delete_count > 0:
                telegram_prefix = f"{delete_count} {variable} registros borrados"
        await send_telegram_notification(f"{telegram_prefix} de {pool_code}")
        return {"status": True, "result": f"{delete_count} Data Removed"}
    except Exception as e:
        logger.error(f"Exception deleting variable {variable} in cloud: {e}")
        return {"status": False, "result": "0 Data Removed"}


def handle_delete_response(pool_code: int, result: dict):
    "Handles the response from delete_variable_in_cloud function, to avoid code repetition"
    delete_count: int = 0
    try:
        if result and "status" in result and result["status"]:
            result_msg: str = result["result"]  # X Data Removed
            delete_count = int(result_msg.split(" ")[0])
        else:
            logger.warning(f"Cloud variable deletion at {pool_code} result: {result}")
    except Exception as e:
        logger.error(f"Exception during cloud variable deletion at {pool_code}: {e}")
    finally:
        return delete_count


async def clean_charging_session_history(
    pool_code: int, variable: str = "charging_session_data", keep_weeks: int = 26
) -> int:
    "Deletes old elements from the charging session history of the provided pool"
    result = await delete_variable_in_cloud(pool_code, variable, keep_weeks)
    return handle_delete_response(pool_code, result)


async def clean_active_charging_session_data(
    pool_code: int, variable: str = "active_cs_data", keep_weeks: int = 2
) -> int:
    "Deletes old data produced by charging sessions during charging"
    result = await delete_variable_in_cloud(pool_code, variable, keep_weeks)
    return handle_delete_response(pool_code, result)


async def clean_station_variables(
    pool_code: int,
    station_name: str,
    connector_id: int,
    prefixes: list[str] = ["cost", "energy", "state", "time"],
    keep_weeks: int = 2,
) -> int:
    """
    Deletes old variables generated during a station normal operation. This
    variables with the connector as suffix are shown in the public dashboards.
    """
    removed_count: int = 0
    for prefix in prefixes:
        variable = f"{prefix}_{station_name.lower()}_{connector_id}"
        result = await delete_variable_in_cloud(pool_code, variable, keep_weeks)
        removed_count += handle_delete_response(pool_code, result)
    return removed_count


async def clean_pool_private_variables(
    pool_code: int, variable: str = "state", keep_weeks: int = 2
) -> int:
    "Deletes variables shown in the private dashboard of each pool"
    result = await delete_variable_in_cloud(pool_code, variable, keep_weeks)
    return handle_delete_response(pool_code, result)


async def clean_pool_public_variables(
    pool_code: int, pool_known_charge_points: set[tuple[str, int]]
):
    "Wraps the clean_station_variables to delete the pool public variables"
    removed_count: int = 0
    for charge_point_tuples in pool_known_charge_points[pool_code]:
        station, cid = charge_point_tuples
        removed_count += await clean_station_variables(pool_code, station, cid)
    return removed_count


async def pool_variable_cleanup(pool_code: int):
    "Deletes old variables from TagoIO (considering the 50.000 registers limit)"
    removed_count: int = 0
    removed_count += await clean_active_charging_session_data(pool_code)
    removed_count += await clean_charging_session_history(pool_code)
    removed_count += await clean_pool_private_variables(pool_code)

    if pool_code in known_charge_points:
        known_stations = known_charge_points[pool_code]
        removed_count += await clean_pool_public_variables(pool_code, known_stations)

    logger.info(f"Removed {removed_count} variables from pool {pool_code}")


async def all_pools_variable_cleanup():
    "Deletes old variables from TagoIO, for all the registered pools"
    logger.warning("Performing variable cleanup for all registered pools...")
    devices_data_by_pool_code: dict[int, tuple[str, str]] = get_all_devices_data()
    for pool_code in devices_data_by_pool_code:
        await pool_variable_cleanup(pool_code)
        await asyncio_sleep(30)


def all_pools_variable_cleanup_trigger():
    "Calls a GET endpoint to trigger the async function, without awaiting it"
    request_url = f"http://localhost:{port}/{version}/all-pools-variable-cleanup"
    auth = httpx.BasicAuth(username=app_default_user, password=app_default_token)
    try:
        with httpx.Client() as client:
            client.get(request_url, auth=auth)
    except Exception as e:
        logger.error(f"Exception during all pools cleanup trigger: {e}")
