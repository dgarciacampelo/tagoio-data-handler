import asyncio
from typing import Any, Optional

import httpx
from loguru import logger
from pydantic import BaseModel

from config import tago_api_endpoint
from schemas import PoolConfigUpdate
from tagoio.token_fetching import delete_device_data_by_pool_code, get_headers_by_pool_code


class PoolConfig(BaseModel):
    is_loaded: bool = False  # Track the loading status of the pool configuration

    # CPO Info Defaults
    cpo_name: str = "PRUEBA DE DATOS S.L."
    cpo_fiscal_id: str = "B74454166"
    cpo_address: str = "OLLONIEGO"
    cpo_phone: str = "985790454"
    cpo_email: str = "comercial@velo-energy.com"
    cpo_web: str = "https://velo-energy.com"

    # Rates Defaults
    rate_off_peak: float = 0.4
    rate_flat: float = 0.4
    rate_peak: float = 0.4
    vat: float = 0.10

    # Installation Info Defaults
    preauth_amount: float = 40.0
    max_power: float = 5000.0


# In-memory store for pool configurations
pool_configs: dict[int, PoolConfig] = {}


def get_pool_config(pool_code: int) -> PoolConfig:
    """Returns the configuration for a given pool, generating a default one if not found."""
    return pool_configs.get(pool_code, PoolConfig())


async def fetch_variable_last_value(pool_code: int, variable: str) -> Optional[dict[str, Any]]:
    """Fetches the last value of a variable from TagoIO."""
    url = f"{tago_api_endpoint}/data"
    params = {"variable": variable, "qty": 1}
    headers = get_headers_by_pool_code(pool_code)

    try:
        async with httpx.AsyncClient() as client:
            response = await client.get(url, headers=headers, params=params)
            response.raise_for_status()
            data = response.json()
            if data.get("status") and data.get("result"):
                return data["result"][0]

    except httpx.HTTPStatusError as e:
        # Re-raise HTTP errors so the parent function can handle the routing logic
        logger.warning(f"HTTP {e.response.status_code} fetching {variable} for pool {pool_code}.")
        raise
    except Exception as e:
        logger.warning(f"Error fetching {variable} for pool {pool_code}: {e}")

    return None


async def init_pool_configs(known_pools: list[int]):
    """Fetches CPO, rates, power info, and POS withholding for all known pools concurrently."""
    logger.info(f"Initializing {len(known_pools)} pool configurations concurrently...")

    # Limit concurrency to 10 simultaneous pools to protect against API rate limits
    semaphore = asyncio.Semaphore(10)

    # Create a task for each pool and run them concurrently
    tasks = [asyncio.create_task(_process_single_pool(pool_code, semaphore)) for pool_code in known_pools]

    # Wait for all background tasks to finish
    await asyncio.gather(*tasks)
    logger.info("Finished initializing all pool configurations.")


async def _process_single_pool(pool_code: int, semaphore: asyncio.Semaphore):
    """Worker function to process a single pool's configuration."""
    async with semaphore:
        config = pool_configs.get(pool_code, PoolConfig())

        try:
            # 1. Fetch CPO Info (If this 400s, it jumps straight to the except block)
            cpo_data = await fetch_variable_last_value(pool_code, "operator_info")
            if cpo_data and "metadata" in cpo_data:
                meta = cpo_data["metadata"]
                config.cpo_name = meta.get("nombre", config.cpo_name)
                config.cpo_fiscal_id = meta.get("CIF", config.cpo_fiscal_id)
                config.cpo_address = meta.get("direccion", config.cpo_address)
                config.cpo_phone = meta.get("telefono", config.cpo_phone)
                config.cpo_email = meta.get("correo", config.cpo_email)
                config.cpo_web = meta.get("web", config.cpo_web)

            # 2. Fetch Rates Info
            rates_data = await fetch_variable_last_value(pool_code, "rate_costs")
            if rates_data and "metadata" in rates_data:
                meta = rates_data["metadata"]
                try:
                    config.rate_off_peak = float(meta.get("valle", config.rate_off_peak))
                    config.rate_flat = float(meta.get("llanas", config.rate_flat))
                    config.rate_peak = float(meta.get("punta", config.rate_peak))
                    vat = float(meta.get("IVA", config.vat))
                    config.vat = vat / 100 if vat > 1 else vat
                except ValueError:
                    logger.warning(f"Invalid rate format in pool {pool_code}")

            # 3. Fetch Max Power
            power_data = await fetch_variable_last_value(pool_code, "max_installation_power")
            if power_data and "value" in power_data:
                try:
                    config.max_power = float(power_data["value"])
                except ValueError:
                    pass

            # 4. Fetch Withholding Amount (POS Pre-auth monetary value)
            withholding_data = await fetch_variable_last_value(pool_code, "withholding_amount")
            if withholding_data and "value" in withholding_data:
                try:
                    config.preauth_amount = float(withholding_data["value"])
                except (ValueError, TypeError):
                    logger.warning(f"Invalid withholding_amount format in pool {pool_code}")

            rates: str = f"Off-Peak: {config.rate_off_peak}, Flat: {config.rate_flat}, Peak: {config.rate_peak}, VAT: {int(100 * config.vat)} %"
            logger.info(f"Pool {pool_code}: {config.cpo_name}, Rates {rates}, holds: {int(config.preauth_amount)} €")

        except httpx.HTTPStatusError as e:
            # Handle invalid devices (400 Bad Request, 401 Unauthorized, 403 Forbidden)
            if e.response.status_code in (400, 401, 403, 404):
                prefix: str = f"Pool {pool_code} is invalid or deleted in TagoIO (HTTP {e.response.status_code})."
                logger.error(f"{prefix} Scrubbing from local database.")
                delete_device_data_by_pool_code(pool_code)
            else:
                logger.error(f"Unexpected HTTP error for pool {pool_code}: {e}")
        except Exception as e:
            logger.error(f"Unexpected error initializing config for pool {pool_code}: {e}")

        finally:  # Whether it succeeded or failed, mark as loaded so the UI drops the spinner
            config.is_loaded = True
            pool_configs[pool_code] = config


def update_pool_config_in_memory(update: PoolConfigUpdate):
    """
    Surgically updates the in-memory pool configuration without requiring
    a full TagoIO API fetch cycle.
    """
    config = pool_configs.get(update.pool_code)

    if not config:
        # If the pool wasn't loaded yet, initialize it
        config = PoolConfig(is_loaded=True)
        pool_configs[update.pool_code] = config

    # Update CPO fields if they exist in the payload
    if update.cpo_name is not None:
        config.cpo_name = update.cpo_name
    if update.cpo_fiscal_id is not None:
        config.cpo_fiscal_id = update.cpo_fiscal_id
    if update.cpo_address is not None:
        config.cpo_address = update.cpo_address
    if update.cpo_phone is not None:
        config.cpo_phone = update.cpo_phone
    if update.cpo_email is not None:
        config.cpo_email = update.cpo_email
    if update.cpo_web is not None:
        config.cpo_web = update.cpo_web

    # Update Rate fields if they exist in the payload
    if update.rate_off_peak is not None:
        config.rate_off_peak = update.rate_off_peak
    if update.rate_flat is not None:
        config.rate_flat = update.rate_flat
    if update.rate_peak is not None:
        config.rate_peak = update.rate_peak
    if update.vat is not None:
        config.vat = update.vat
    if update.preauth_amount is not None:
        config.preauth_amount = update.preauth_amount

    logger.info(f"Hot-reloaded configuration for Pool {update.pool_code}")
