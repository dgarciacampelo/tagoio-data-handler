import httpx
from typing import Any

from config import tago_api_endpoint
from schemas import ChargePointUpdate
from tagoio.token_fetching import get_headers_by_pool_code
from user_interface import translate_status


def get_status_key(pool_code: int, station_name: str) -> int:
    """Provides the logic that allows to find a charge point translated status"""
    return hash((pool_code, station_name))


translated_statuses: dict[int, dict[int, str]] = {}


async def insert_data_in_cloud(pool_code: int, data: dict = {}):
    url: str = f"{tago_api_endpoint}/data"
    headers = get_headers_by_pool_code(pool_code)
    async with httpx.AsyncClient() as client:
        response = await client.post(url, headers=headers, json=data)
        return response.json()


async def update_charge_point_status(update: ChargePointUpdate):
    save_charge_point_status(update)
    await update_management_dashboard(update)

    if update.has_pulic_dashboard:
        await update_public_dashboard(update)


def save_charge_point_status(update: ChargePointUpdate):
    status = translate_status(update.charge_point_status, update.connection_status)
    status_key = get_status_key(update.pool_code, update.station_name)

    if status_key not in translated_statuses:
        translated_statuses[status_key] = dict()

    translated_statuses[status_key][update.connector_id] = status


async def update_management_dashboard(update: ChargePointUpdate):
    status_key = get_status_key(update.pool_code, update.station_name)
    station_statuses = translated_statuses[status_key]

    metadata: dict[str, Any] = {}
    metadata["connection_state"] = update.connection_status
    for connector_id in station_statuses:
        metadata[f"state_{connector_id}"] = station_statuses[connector_id]

    data = {
        "variable": "state",
        "value": update.station_name,
        "group": update.station_name,
        "metadata": metadata,
        "unit": None,
        "time": None,
    }

    return await insert_data_in_cloud(update.pool_code, data)


async def update_public_dashboard(update: ChargePointUpdate):
    status_key = get_status_key(update.pool_code, update.station_name)
    station_statuses = translated_statuses[status_key]
    data = {
        "variable": f"state_{update.station_name}_{update.connector_id}",
        "value": station_statuses[update.connector_id],
        "group": update.station_name,
        "metadata": None,
        "unit": None,
        "time": None,
    }

    return await insert_data_in_cloud(update.pool_code, data)
