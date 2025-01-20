import asyncio
import httpx
from loguru import logger

from config import tago_data_amount_token, tago_api_endpoint
from tagoio.aux_functions import handle_response
from tagoio.data_deletion import delete_variable_in_cloud
from tagoio.token_fetching import pool_code_and_device_id_generator
from telegram_utils import send_telegram_notification

# Thresholds to setup different actions for each pool/TagoIO device:
individual_variable_threshold = 500  # per variable
no_action_threshold = 25_000
warning_amount_threshold = 45_000

# TagoIO Rate Limits (Hard limits):
# ? https://help.tago.io/portal/en/kb/articles/rate-limits

# Prefix of variable names that can be removed during data cleanup:
# ? state_ changed to state, to remove both state_[connector_id] and state variables
removable_prefixes: set[str] = {"active_cs_data", "state", "cost_", "energy_", "time_"}


def run_tuple_generator():
    "Use the generator to get the pool_code and device_id for each TagoIO device"
    for pool_code, device_id in pool_code_and_device_id_generator():
        print(f"pool_code: {pool_code}, device_id: {device_id}")


async def check_all_devices_data_amount(
    token: str = tago_data_amount_token, check_only: set[int] = None
):
    "Checks the data amount for each TagoIO device, to review its below limits (50_000)"
    amounts_by_pool_code: dict[int, tuple[str, int]] = dict()

    headers = {"device-token": token}
    async with httpx.AsyncClient() as client:
        for pool_code, device_id in pool_code_and_device_id_generator():
            if check_only is not None and pool_code not in check_only:
                continue

            await asyncio.sleep(1)
            url = f"{tago_api_endpoint}/device/{device_id}/data_amount"
            response = await client.get(url, headers=headers)
            result = handle_response(response, "Bucket can't be found")
            amount = int(result) if result is not None else -1
            message_prefix = f"Data amount in TagoIO device for pool {pool_code}:"
            if amount > warning_amount_threshold:
                logger.warning(f"{message_prefix} {amount}")
                telegram_message = f"Aviso: {pool_code} tiene {amount} datos en TagoIO"
                await send_telegram_notification(telegram_message)
            else:
                logger.info(f"{message_prefix} {amount}")

            if amount > no_action_threshold:
                amounts_by_pool_code[pool_code] = device_id, amount

    return amounts_by_pool_code


async def fetch_pool_variables_info(
    pool_code: int,
    device_id: str,
    data_amount: int,
    token: str = tago_data_amount_token,
    step_size: int = 4000,
):
    """
    Due to the TagoIO platform imposes a hard limit of 5000 data fetches per
    minute, this function retrieves all the data for a pool in batches of the
    provided step size, with a sleep of 1 minute between each batch, and then
    counts the total number of data and returns the result by variable name.
    Each sucessive request, the skip parameter is used to retreive fresh data

    data_amount: total data to retrieve from the pool in question,
    step_size: data amount to retrieve each minute.
    """
    amounts_by_variable: dict[str, int] = dict()
    total_data_amount: int = 0
    fetched_data_amount: int = None
    actual_step: int = 0

    headers = {"device-token": token}
    async with httpx.AsyncClient() as client:
        while fetched_data_amount is None or fetched_data_amount < data_amount:
            if fetched_data_amount == 0:
                break

            # logger.info(f"Fetching data for pool {pool_code}, step: {actual_step}")
            skip: int = actual_step * step_size
            url = f"{tago_api_endpoint}/device/{device_id}/data?details=false&qty={step_size}&skip={skip}"
            response = await client.get(url, headers=headers)
            data = response.json()
            if "result" not in data:
                continue

            actual_step += 1
            fetched_data_amount = len(data["result"])
            total_data_amount += fetched_data_amount
            for data_export in data["result"]:
                variable = data_export["variable"]
                amounts_by_variable[variable] = amounts_by_variable.get(variable, 0) + 1

            await asyncio.sleep(15)

    logger.info(f"Pool {pool_code} data amount: {data_amount} vs {total_data_amount}")
    logger.info(amounts_by_variable)
    return amounts_by_variable


async def device_data_amount_check(token: str = tago_data_amount_token):
    "Takes measures deleting data from each TagoIOdevice, when a threshold is reached"
    amounts_by_pool_code = await check_all_devices_data_amount(token)
    for pool_code, (device_id, amount) in amounts_by_pool_code.items():
        result = await fetch_pool_variables_info(pool_code, device_id, amount, token)
        for variable_name, individual_amount in result.items():
            if individual_amount < individual_variable_threshold:
                continue

            for removable_variable_prefix in removable_prefixes:
                if variable_name.startswith(removable_variable_prefix):
                    # Cleanup that variable name from the TagoIO device:
                    logger.info(f"Cleaning variable {pool_code}: {variable_name} ...")
                    await delete_variable_in_cloud(pool_code, variable_name, 0)

    # Do a new check, only with the keys from amounts_by_pool_code:
    include_only: set[int] = amounts_by_pool_code.keys()
    await check_all_devices_data_amount(token, include_only)
