from typing import Any, Optional
from uuid import uuid4

import httpx
from fastapi import APIRouter, HTTPException, Query, Request
from fastapi.templating import Jinja2Templates
from loguru import logger

from config import payments_gateway_device_token as payments_gateway_token
from data_handling import get_charge_point
from database.query_database import get_noc_from_db
from enumerations import ChargePointStatus
from schemas import PaymentAuthRequest

router = APIRouter()

# Templates folder, holds the HTML files for rendering the public dashboard
templates = Jinja2Templates(directory="templates")

# Make ChargePointStatus enum available in Jinja2 templates as "ChargePointStatus"
templates.env.globals["ChargePointStatus"] = ChargePointStatus

# Mapping of lowercase status strings to ChargePointStatus enum members for flexible string handling (e.g., from URL parameters)
cp_status_map = {e.value.lower(): e for e in ChargePointStatus}


@router.get("/dashboard/{pool_code}/{station_name}")
async def render_public_dashboard(
    request: Request,
    pool_code: int,
    station_name: str,
    noc: int = 1,
    cid: int = 1,
    force_status_str: Optional[str] = Query(default=None, alias="force-status"),
):
    """Renders the public dashboard for a specific charging station, with the possibility to select the connector."""

    # URL parameter validation and boundary checks (FastAPI already validates types, e.g., ChargePointStatus)
    noc = max(1, noc)  # Ensure at least 1 connector
    actual_noc = get_noc_from_db(station_name) or noc
    cid = max(1, min(cid, actual_noc))  # Ensure the connector ID does not exceed the noc, but is at least 1

    # Safely attempt to convert the string to the Enum (avoids 'Input should be' errors and allows for graceful handling of invalid values)
    force_status: Optional[ChargePointStatus] = None
    if force_status_str:
        if force_status_str.lower() in cp_status_map:
            force_status = cp_status_map[force_status_str.lower()]
        else:
            logger.warning(f"{pool_code}/{station_name} [{cid}]: Discarding invalid force-status: '{force_status_str}'")

    if force_status:
        status = force_status
        logger.debug(f"Status forced via URL for {pool_code}/{station_name} [{cid}]: {status.value}")
    else:
        cp_data = get_charge_point(pool_code, station_name, connector_id=cid)
        status = cp_data.charge_point_status if cp_data else ChargePointStatus.UNAVAILABLE

    return templates.TemplateResponse(
        "smart-station-dashboard.html",
        {
            "request": request,
            "pool_code": pool_code,
            "station_name": station_name,
            "noc": actual_noc,
            "current_cid": cid,  # Pass the active connector ID
            "station_status": status,  # Status of the active connector
            "force_status_str": force_status_str,  # Raw status string for the template (HTMX refreshes each ~5 seconds)
        },
    )


@router.post("/api/charge-request")
async def trigger_payment_authorization(request: PaymentAuthRequest):
    # Generate a unique group ID for this transaction request
    event_group = uuid4().hex

    cp_id = f"{request.pool_code}/{request.station_name}"

    # Base payload for Paycomet pre-auth (using Any as type to avoid static type checking issues with the dynamic payload construction)
    tago_payload: list[dict[str, Any]] = [
        {"variable": "cp_id", "value": cp_id, "group": event_group},
        {"variable": "connector_id", "value": str(request.connector_id), "group": event_group},
        {"variable": "email", "value": request.email, "group": event_group},
    ]

    # Append Receipt variables if requested
    if request.requires_invoice:
        tago_payload.extend(
            [
                {"variable": "receipt_fiscal_id", "value": request.nif, "group": event_group},
                {"variable": "receipt_name", "value": request.billing_name, "group": event_group},
                {"variable": "receipt_address", "value": request.billing_address, "group": event_group},
                {"variable": "receipt_email", "value": request.invoice_email, "group": event_group},
            ]
        )

    headers = {"Content-Type": "application/json", "Device-Token": payments_gateway_token}

    try:  # POST to TagoIO immutable bucket
        async with httpx.AsyncClient() as client:
            response = await client.post("https://api.tago.io/data", json=tago_payload, headers=headers)
            response.raise_for_status()

            logger.info(f"Payment analysis triggered for {cp_id} [{request.connector_id}]")
            return {"status": "success"}

    except httpx.HTTPStatusError as e:
        logger.error(f"TagoIO API Error: {e.response.text}")
        raise HTTPException(status_code=502, detail="Gateway error communicating with payment handler.")
    except Exception as e:
        logger.error(f"Unexpected error: {e}")
        raise HTTPException(status_code=500, detail="Internal server error.")


@router.get("/dashboard/partial/status/{pool_code}/{station_name}")
async def render_status_card_partial(
    request: Request,
    pool_code: int,
    station_name: str,
    cid: int = 1,
    force_status_str: Optional[str] = Query(default=None, alias="force-status"),
):
    """Returns ONLY the status card HTML block for HTMX polling."""

    # Apply the same override logic for the status as in the main dashboard route
    force_status: Optional[ChargePointStatus] = None
    if force_status_str and force_status_str.lower() in cp_status_map:
        force_status = cp_status_map[force_status_str.lower()]

    if force_status:
        status = force_status
    else:
        cp_data = get_charge_point(pool_code, station_name, connector_id=cid)
        status = cp_data.charge_point_status if cp_data else ChargePointStatus.UNAVAILABLE

    return templates.TemplateResponse(
        "partials/status-card.html",
        {
            "request": request,
            "pool_code": pool_code,
            "station_name": station_name,
            "current_cid": cid,
            "station_status": status,
            "force_status_str": force_status_str,  # Keep passing it forward in case the partial triggers the next poll
        },
    )
