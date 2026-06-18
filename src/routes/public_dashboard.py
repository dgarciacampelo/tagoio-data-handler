from typing import Optional

from fastapi import APIRouter, Form, HTTPException, Query, Request
from fastapi.templating import Jinja2Templates
from loguru import logger

from schemas.analysis import VPOSStartEvent, VPOSStopEvent
from data_handling import get_active_session, get_charge_point
from database.query_database import get_noc_from_db
from enumerations import ChargePointStatus
from schemas.ocpp_csms import PaymentAuthRequest
from sse_broker import event_broker
from tagoio.pool_setup_fetching import get_pool_config

router = APIRouter()

# Templates folder, holds the HTML files for rendering the public dashboard
templates = Jinja2Templates(directory="templates")

# Make ChargePointStatus enum available in Jinja2 templates as "ChargePointStatus"
templates.env.globals["ChargePointStatus"] = ChargePointStatus

# Mapping of lowercase status strings to ChargePointStatus enum members for flexible string handling (e.g., from URL parameters)
cp_status_map = {e.value.lower(): e for e in ChargePointStatus}


@router.get("/{pool_code:int}/{station_name}")
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

    session_data = get_active_session(pool_code, station_name, connector_id=cid)
    pool_config = get_pool_config(pool_code)

    return templates.TemplateResponse(
        request=request,
        name="smart-station-dashboard.html",
        context={
            "request": request,
            "pool_code": pool_code,
            "station_name": station_name,
            "noc": actual_noc,
            "current_cid": cid,  # Pass the active connector ID
            "station_status": status,  # Status of the active connector
            "session_data": session_data,  # Data for the active charging session
            "pool_config": pool_config,  # Pass the pool configuration to the template for dynamic rendering
            "force_status_str": force_status_str,  # Raw status string for the template (HTMX refreshes each ~5 seconds)
        },
    )


@router.post("/api/charge-request")
async def trigger_payment_authorization(request: PaymentAuthRequest):
    try:
        # 1. Securely fetch the required holding amount from the server-side cache
        pool_config = get_pool_config(request.pool_code)
        if not pool_config:
            raise HTTPException(status_code=404, detail="Pool configuration not found")

        # Fallback to 40.0 if not explicitly set in the config
        amount = getattr(pool_config, "preauth_amount", 40.0)

        # 2. Package the event for the CSMS
        event = VPOSStartEvent(
            pool_code=request.pool_code,
            station_name=request.station_name,
            connector_id=request.connector_id,
            email=request.email,
            amount=amount,
            # Unpack the invoice fields safely
            requires_invoice=request.requires_invoice,
            receipt_fiscal_id=request.receipt_fiscal_id,
            receipt_name=request.receipt_name,
            receipt_address=request.receipt_address,
            receipt_email=request.receipt_email,
        )

        # 3. Broadcast to the SSE stream
        pool_code, station_name, connector_id = request.pool_code, request.station_name, request.connector_id
        logger.info(f"Broadcasting VPOS Start Request for {pool_code}/{station_name} [{connector_id}] ({amount}€)")

        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

        return {"status": "pending_authorization", "amount_requested": amount}

    except ValueError as ve:  # Catch Pydantic validation errors explicitly
        logger.warning(f"VPOS Request Validation Error: {ve}")
        raise HTTPException(status_code=422, detail=str(ve))
    except Exception as e:
        logger.error(f"Failed to broadcast VPOS Start Request: {e}")
        raise HTTPException(status_code=500, detail="Internal server error")


@router.post("/api/stop-request")
async def trigger_stop_request(
    pool_code: int = Form(...), station_name: str = Form(...), connector_id: int = Form(...)
):
    """
    Triggers a SSE event to stop the charging session for a specific connector.
    Expects form data natively sent by HTMX hx-vals.
    """
    try:
        event = VPOSStopEvent(pool_code=pool_code, station_name=station_name, connector_id=connector_id)

        logger.info(f"Broadcasting VPOS Stop Request for {pool_code}/{station_name} [{connector_id}]")
        await event_broker.broadcast(event_name=event.event_type.value, payload=event.model_dump(mode="json"))

        return {"status": "success", "message": "Stop request dispatched"}
    except Exception as e:
        logger.error(f"Failed to broadcast VPOS Stop Request: {e}")
        return {"status": "error", "message": "Internal server error"}


@router.get("/partial/status/{pool_code}/{station_name}")
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
        # logger.info(f"Dashboard -> GETS: ({pool_code}, {station_name}, {cid}) | AS cp_data: {cp_data}")

    # Retrieve the active session data for the specified connector to include in the partial metering template
    session_data = get_active_session(pool_code, station_name, connector_id=cid)
    pool_config = get_pool_config(pool_code)

    return templates.TemplateResponse(
        request=request,
        name="partials/poll-update.html",
        context={
            "request": request,
            "pool_code": pool_code,
            "station_name": station_name,
            "current_cid": cid,
            "station_status": status,
            "session": session_data,
            "pool_config": pool_config,
            "force_status_str": force_status_str,  # Keep passing it forward in case the partial triggers the next poll
        },
    )
