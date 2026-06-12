from datetime import datetime
from typing import Any, NamedTuple, Optional

import httpx
from loguru import logger
from pytz import timezone as pytz_timezone

from config import tago_account_token, tago_api_endpoint

SERVER_ALIAS: str = "Neos"


class TagoDeviceContext(NamedTuple):
    """Structured container for resolved TagoIO device data."""

    device_id: str
    device_token: str
    is_found: bool
    advanced_plan: bool


def _safe_extract_group_id(device_name: str) -> str:
    """
    Safely extracts the group_id/pool_code from a standardized name string.
    Expected format: MASTER-{INSTALLATION}-{POOL_CODE}
    """
    try:
        parts = device_name.split("-")
        if len(parts) > 2:
            return str(int(parts[2]))
    except (ValueError, IndexError):
        logger.warning(f"Could not parse integer Pool code from device name: '{device_name}'. Defaulting to 'Unknown'.")
    return "Unknown"


def _get_account_headers() -> dict[str, str]:
    """Provides standard headers for account-level TagoIO API requests."""
    return {"Content-Type": "application/json", "Account-Token": tago_account_token}


async def get_device_list(client: httpx.AsyncClient, name_filter: Optional[str] = None) -> list[dict[str, Any]]:
    """Fetches the raw list of devices from the TagoIO account, optionally filtered by name."""
    url = f"{tago_api_endpoint}/device"
    params: dict[str, Any] = {"amount": 100}

    if name_filter:  # Push filtering to the TagoIO backend if a specific name is targeted
        params["filter[name]"] = name_filter

    response = await client.get(url, headers=_get_account_headers(), params=params)
    response.raise_for_status()
    data = response.json()

    return data.get("result", [])


async def get_device_token(client: httpx.AsyncClient, device_id: str) -> Optional[str]:
    """Retrieves the latest valid token for a given device ID."""
    url = f"{tago_api_endpoint}/device/token/{device_id}"
    params = {"amount": 1, "orderBy": "created_at,desc"}

    response = await client.get(url, headers=_get_account_headers(), params=params)
    response.raise_for_status()
    data = response.json()

    results = data.get("result", [])
    if results:
        return results[0].get("token")
    return None


async def find_tago_device(name: str) -> tuple[Optional[str], Optional[str], bool]:
    """
    Scans the account for a device matching the specified name.
    Returns (device_id, device_token, advanced_plan).
    """
    async with httpx.AsyncClient(timeout=10.0) as client:
        try:
            # Optimize by asking TagoIO to filter the list directly
            devices = await get_device_list(client, name_filter=name)

            for device in devices:
                if device.get("name") == name:
                    tags = device.get("tags", [])
                    advanced_plan = any(t.get("key") == "plan" and t.get("value") == "ADVANCED" for t in tags)

                    dev_id = device["id"]
                    dev_token = await get_device_token(client, dev_id)

                    plan_label = "ADV. PLAN" if advanced_plan else "BASIC"
                    logger.info(f"·TagoIO· Found device {dev_id}: {name} ({plan_label}).")
                    return dev_id, dev_token, advanced_plan

        except httpx.HTTPError as e:
            logger.error(f"HTTP error while searching for device '{name}': {e}")

    logger.warning(f"·TagoIO· Device with name: {name} not found.")
    return None, None, False


async def create_new_tago_device(
    name: str,
    device_type: str,
    installation: str,
    server_alias: str = SERVER_ALIAS,
    advanced_plan: bool = False,
) -> tuple[Optional[str], Optional[str]]:
    """Registers a new device on the TagoIO cloud infrastructure via REST API."""
    tz = pytz_timezone("Europe/Madrid")
    datetime_stamp = datetime.now(tz).isoformat()
    group_id = _safe_extract_group_id(name)

    tag_list = [
        {"key": "name", "value": name},
        {"key": "installation_type", "value": installation},
        {"key": "group_id", "value": group_id},
        {"key": "manager_id", "value": "Unknown"},
        {"key": "type", "value": device_type},
        {"key": "plan", "value": "ADVANCED" if advanced_plan else "BASIC"},
    ]

    if server_alias:
        tag_list.append({"key": "server_alias", "value": server_alias.upper()})

    new_device_payload = {
        "name": name,
        "description": f"Registrado automáticamente en la siguiente fecha: {datetime_stamp}.",
        "active": True,
        "visible": True,
        "type": "mutable",
        "tags": tag_list,
    }

    url = f"{tago_api_endpoint}/device"

    async with httpx.AsyncClient(timeout=10.0) as client:
        try:
            # await send_telegram(f"New TagoIO {device_type} device created: {name} ({server_alias})")
            response = await client.post(url, headers=_get_account_headers(), json=new_device_payload)
            response.raise_for_status()
            data = response.json()

            if data.get("status"):
                dev_id = data["result"]["device_id"]
                token = data["result"]["token"]
                return dev_id, token
            else:
                logger.error(f"·TagoIO· API rejected device creation. Message: {data.get('message')}")

        except httpx.HTTPError as e:
            logger.error(f"·TagoIO· HTTP error creating device '{name}': {e}")

    return None, None


async def find_or_ensure_device(
    name: str,
    device_type: str = "MASTER",
    installation: str = "BUSINESS",
    server_alias: str = SERVER_ALIAS,
    advanced_plan: bool = True,
) -> TagoDeviceContext:
    """
    High-level orchestration routine. Looks up an existing device by name,
    or transparently provisions it if missing.
    """
    dev_id, token, found_advanced_plan = await find_tago_device(name)

    if dev_id is None:
        dev_id, token = await create_new_tago_device(
            name=name,
            device_type=device_type,
            installation=installation,
            server_alias=server_alias,
            advanced_plan=advanced_plan,
        )
        is_found = False
        final_plan = advanced_plan
    else:
        is_found = True
        final_plan = found_advanced_plan

    if not dev_id or not token:
        raise RuntimeError(f"Failed to find or provision TagoIO device: {name}")

    return TagoDeviceContext(
        device_id=dev_id,
        device_token=token,
        is_found=is_found,
        advanced_plan=final_plan,
    )
