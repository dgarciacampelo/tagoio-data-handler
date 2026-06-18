"""
This module handles the provisioning of new TagoIO users (for CPOs) and devices
(each charging pool uses a TagoIo device/bucket as cloud storage) as based on
validated Google Forms data.
"""

import secrets
import string
from datetime import datetime

import httpx
from loguru import logger

from config import tago_api_endpoint
from schemas.google_forms import GoogleFormPayload
from tagoio.data_parsing import handle_variable_insert
from tagoio.device_management import _get_account_headers, get_device_list


def generate_secure_password(length: int = 16) -> str:
    """Generates a secure random password for the new CPO."""
    alphabet = string.ascii_letters + string.digits + "!@#$%^&*"
    return "".join(secrets.choice(alphabet) for _ in range(length))


async def resolve_new_pool_code() -> int:
    """
    Calculates the next available pool code based on existing TagoIO devices.
    Format: YYXXXX (e.g., 261001, 261002, 271001).
    """
    current_year = datetime.now().strftime("%y")
    prefix = "MASTER-BUSINESS-"
    max_sequence = 1000  # Base sequence before the first increment

    async with httpx.AsyncClient(timeout=10.0) as client:
        # Utilizing the existing account-level fetcher
        devices = await get_device_list(client)

    for device in devices:
        name = device.get("name", "")
        if name.startswith(prefix):
            try:
                code_str = name.replace(prefix, "")

                # Only evaluate sequences belonging to the current year
                if code_str.startswith(current_year) and len(code_str) == 6:
                    sequence = int(code_str[2:])
                    if sequence > max_sequence:
                        max_sequence = sequence
            except ValueError:
                continue

    next_sequence = max_sequence + 1
    return int(f"{current_year}{next_sequence}")


async def create_tagoio_user(payload: GoogleFormPayload, pool_code: int, password: str) -> bool:
    """
    Creates a new user in TagoIO using the validated Google Forms data.
    Enforces a strict membership-based access model by binding the user
    to the specific pool_code via tags.
    """
    url = f"{tago_api_endpoint}/run/users"

    tag_list = [
        {"key": "name", "value": payload.manager_name},
        {"key": "group_id", "value": str(pool_code)},  # Organizational membership boundary
        {"key": "installation_type", "value": "BUSINESS"},
        {"key": "plan", "value": "ADVANCED"},
        {"key": "user_type", "value": "MANAGER"},
        {"key": "business_name", "value": payload.company_name},
        {"key": "business_email", "value": payload.contact_email},
        {"key": "business_phone", "value": payload.contact_phone},
    ]

    new_user = {
        "name": payload.manager_name,
        "email": payload.user_email,
        "password": password,
        "language": "es",
        "timezone": "Europe/Madrid",
        "phone": payload.contact_phone,
        "company": payload.company_name,
        "active": True,
        "tags": tag_list,
    }

    async with httpx.AsyncClient(timeout=10.0) as client:
        try:
            response = await client.post(url, headers=_get_account_headers(), json=new_user)
            result = response.json()

            logger.info(f"Result after creating new {payload.user_email} TagoIO user: {result}")
            return result.get("status", False)

        except Exception as e:
            logger.error(f"Error creating TagoIO user {payload.user_email}: {e}")
            return False


async def setup_default_device_variables(pool_code: int, payload: GoogleFormPayload):
    """
    Pushes the initial configuration state for a new charging pool device.
    Uses the handler's internal capacity-aware insertion wrapper.
    """

    # 1. Base Installation Variables
    base_variables = [
        {"variable": "load_balancing_mode", "value": "Ninguno", "group": "1"},
        {"variable": "max_installation_power", "value": 5000, "group": "1"},
        {"variable": "max_grid_power_consumption", "value": 5000, "group": "1"},
    ]

    for data in base_variables:
        await handle_variable_insert(pool_code, data)

    # 2. Rate Costs Configuration (flat 40 cents/kWh for all periods, 10% VAT)
    rates_payload = {
        "variable": "rate_costs",
        "value": 0,
        "group": "1",
        "metadata": {"valle": 0.4, "llanas": 0.4, "punta": 0.4, "IVA": 0.1},
    }
    await handle_variable_insert(pool_code, rates_payload)

    # 3. CPO Operator Metadata
    cpo_payload = {
        "variable": "operator_info",
        "value": 0,
        "group": "1",
        "metadata": {
            "web": "",
            "nombre": payload.company_name,
            "CIF": payload.cpo_fiscal_id,
            "direccion": payload.company_address,
            "telefono": payload.contact_phone,
            "correo": payload.contact_email,
        },
    }
    await handle_variable_insert(pool_code, cpo_payload)
