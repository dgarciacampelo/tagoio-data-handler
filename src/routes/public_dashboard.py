from typing import Any

import httpx
from fastapi import APIRouter, HTTPException, Request
from fastapi.templating import Jinja2Templates
from loguru import logger

from config import payments_gateway_device_token as payments_gateway_token
from data_handling import charge_points, get_charge_point
from database.query_database import get_noc_from_db
from schemas import PaymentAuthRequest

router = APIRouter()

# Templates folder, holds the HTML files for rendering the public dashboard
templates = Jinja2Templates(directory="templates")


@router.get("/dashboard/{pool_code}/{station_name}")
async def render_public_dashboard(request: Request, pool_code: int, station_name: str, noc: int = 1, cid: int = 1):
    """Renders the public dashboard for a specific charging station, with the possibility to select the connector."""
    logger.info(f"CURRENT IN-MEMORY KEYS: {list(charge_points.keys())}")

    # Fetch total number of connectors from your database cache
    actual_noc = get_noc_from_db(station_name) or noc

    # Boundary check to prevent users from requesting non-existent connectors
    if cid > actual_noc or cid < 1:
        cid = 1

    # Fetch live status for the SPECIFIC connector requested
    cp_data = get_charge_point(pool_code, station_name, connector_id=cid)
    status = cp_data.charge_point_status if cp_data else "Available"

    return templates.TemplateResponse(
        "smart-station-dashboard.html",
        {
            "request": request,
            "pool_code": pool_code,
            "station_name": station_name,
            "noc": actual_noc,
            "current_cid": cid,  # Pass the active connector ID
            "station_status": status,  # Status of the active connector
        },
    )


@router.post("/api/charge-request")
async def trigger_payment_authorization(request: PaymentAuthRequest):
    cp_id = f"{request.pool_code}/{request.station_name}"

    # Base payload for Paycomet pre-auth (using Any as type to avoid static type checking issues with the dynamic payload construction)
    tago_payload: list[dict[str, Any]] = [
        {"variable": "qr_cpid", "value": cp_id},
        {"variable": "qr_connid", "value": str(request.connector_id)},
        {"variable": "qr_email", "value": request.email},
    ]

    # Append Receipt variables if requested
    if request.requires_invoice:
        tago_payload.extend(
            [
                {"variable": "receipt_fiscal_id", "value": request.nif},
                {"variable": "receipt_name", "value": request.billing_name},
                {"variable": "receipt_address", "value": request.billing_address},
                {"variable": "receipt_email", "value": request.invoice_email},
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
