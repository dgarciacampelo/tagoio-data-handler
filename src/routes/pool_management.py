from fastapi import APIRouter, Depends, HTTPException, status
from loguru import logger

from security import check_credentials
from tagoio.device_management import find_or_ensure_device
from tagoio.pool_setup_fetching import fetch_full_pool_config

router = APIRouter()


@router.get("/api/pools/{pool_code}", dependencies=[Depends(check_credentials)])
async def get_pool_configuration(pool_code: int):
    """
    Fetches and returns the consolidated configuration for a specific Charging Pool.
    Resolves or registers the underlying TagoIO device before fetching configuration data.
    """
    logger.info(f"API request to fetch configuration for Pool: {pool_code}")
    device_name = f"MASTER-BUSINESS-{pool_code}"

    try:
        context = await find_or_ensure_device(name=device_name, device_type="MASTER")

        if not context.device_id or not context.device_token:
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail=f"Failed to resolve or provision a valid TagoIO device for Pool {pool_code}.",
            )

        pool_config = await fetch_full_pool_config(pool_code=pool_code, is_newly_created=not context.is_found)

        if not pool_config:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Configuration data for Pool {pool_code} could not be retrieved.",
            )

        return pool_config

    except HTTPException:
        raise
    except Exception as e:  # noqa: BLE001
        logger.error(f"Unexpected error retrieving configuration for Pool {pool_code}: {e}")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="An unexpected error occurred while processing the TagoIO remote data fetch.",
        )
