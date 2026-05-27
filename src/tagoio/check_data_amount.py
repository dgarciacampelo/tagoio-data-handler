import asyncio
from typing import Optional

import httpx
from loguru import logger

from config import tago_account_token, tago_api_endpoint, tago_data_amount_token  # noqa: F401
from tagoio.aux_functions import AMOUNT, handle_response
from tagoio.data_deletion import delete_variable_in_cloud
from tagoio.token_fetching import get_headers_by_pool_code, pool_code_and_device_id_generator
from telegram_utils import send_telegram_notification

# Thresholds to setup different actions for each pool/TagoIO device:
individual_variable_threshold = 250  # Value to capture variables across pagination chunks
no_action_threshold = 25_000
warning_amount_threshold = 40_000

# TagoIO Rate Limits (Hard limits):
# ? https://help.tago.io/portal/en/kb/articles/rate-limits

# Prefix of variable names that can be removed during data cleanup:
removable_prefixes: set[str] = {"active_cs_data", "state", "cost_", "energy_", "time_"}


def run_tuple_generator():
    """Use the generator to get the pool_code and device_id for each TagoIO device."""
    for pool_code, device_id in pool_code_and_device_id_generator():
        logger.info(f"pool_code: {pool_code}, device_id: {device_id}")


async def check_all_devices_data_amount(check_only: Optional[set[int]] = None) -> dict[int, tuple[str, int]]:
    """Checks the data amount for each TagoIO device using the global Account-Token."""
    send_notification_flag: bool = False
    amounts_by_pool_code: dict[int, tuple[str, int]] = {}

    account_headers = {"content-type": "application/json", "Account-Token": tago_account_token}

    async with httpx.AsyncClient() as client:
        for pool_code, device_id in pool_code_and_device_id_generator():
            if check_only is not None and pool_code not in check_only:
                continue

            await asyncio.sleep(1)

            url = f"{tago_api_endpoint}/device/{device_id}/data_amount"
            response = await client.get(url, headers=account_headers)

            result = handle_response(response, "Bucket can't be found")
            amount = int(result) if result is not None else -1

            message_prefix = f"Data amount in TagoIO device for pool {pool_code}:"
            logger.info(f"{message_prefix} {amount}")

            if amount > warning_amount_threshold:
                send_notification_flag = True

            amounts_by_pool_code[pool_code] = (device_id, amount)

    if send_notification_flag:
        await send_telegram_notification("Some TagoIO devices are reaching the data limit.")

    return amounts_by_pool_code


async def fetch_pool_variables_info(pool_code: int, device_id: str, data_amount: int) -> dict[str, int]:
    """Fetches info regarding variables inside a pool's device bucket using Device-Token."""
    amounts_by_variable: dict[str, int] = {}
    if data_amount <= no_action_threshold:
        return amounts_by_variable

    url = f"{tago_api_endpoint}/data"
    headers = get_headers_by_pool_code(pool_code)

    # Increase chunk request performance up to the data amount limit
    async with httpx.AsyncClient() as client:
        # Step through the historical logs by 10k steps
        for page_step in range(0, data_amount, AMOUNT):
            params = {
                "amount": AMOUNT,
                "skip": page_step,
                "fields": ["variable"],  # Performance fix: tells TagoIO only to return string variables
            }
            try:
                response = await client.get(url, headers=headers, params=params)
                result = handle_response(response, "Bucket can't be found")
                if not result or not isinstance(result, list):
                    continue

                for data_dict in result:
                    variable = data_dict.get("variable")
                    if variable:
                        amounts_by_variable[variable] = amounts_by_variable.get(variable, 0) + 1
            except Exception as e:
                logger.error(f"Error fetching data chunk at skip {page_step} for pool {pool_code}: {e}")
                continue

            # Short breath to prevent API rate limiting issues, but fast enough to loop cleanly
            await asyncio.sleep(1)

    logger.info(f"Pool {pool_code} breakdown: {amounts_by_variable}")
    return amounts_by_variable


async def device_data_amount_check():
    """Takes measures deleting data from each TagoIO device when a threshold is reached."""
    amounts_by_pool_code = await check_all_devices_data_amount()

    for pool_code, (device_id, amount) in amounts_by_pool_code.items():
        # Only process devices that cross the cleanup threshold
        if amount <= no_action_threshold:
            continue

        result = await fetch_pool_variables_info(pool_code, device_id, amount)
        for variable_name, individual_amount in result.items():
            # If the variable count is lower than our threshold, evaluate prefix rules
            if individual_amount < individual_variable_threshold:
                # Fallback safeguard: If a device is almost full (40k+), wipe matches regardless of chunk counts
                if amount < warning_amount_threshold:
                    continue

            for removable_variable_prefix in removable_prefixes:
                if variable_name.startswith(removable_variable_prefix):
                    logger.warning(
                        f"Threshold rule matched ({individual_amount} entries). Cleaning variable {pool_code}: {variable_name} ..."
                    )
                    # Clear target records matching 0 retention weeks to immediately clear space
                    await delete_variable_in_cloud(pool_code, variable_name, 0)

                    # Yield control temporarily to let deletions complete smoothly
                    await asyncio.sleep(1)

    await asyncio.sleep(60)
