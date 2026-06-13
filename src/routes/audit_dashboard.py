from fastapi import APIRouter, Depends, Request, HTTPException
from fastapi.templating import Jinja2Templates
from fastapi.responses import StreamingResponse
from database.query_database import get_recent_sessions, get_session_history, get_telemetry_for_session
from export_utils import generate_telemetry_excel
from security import check_admin_credentials

router = APIRouter()
templates = Jinja2Templates(directory="templates")


@router.get("/emsp-dashboard/audits", dependencies=[Depends(check_admin_credentials)])
async def render_global_audit_dashboard(request: Request):
    """Renders the audit dashboard with the latest charging sessions."""
    recent_sessions = get_recent_sessions(limit=100)

    return templates.TemplateResponse(
        request=request,
        name="audit-dashboard.html",
        context={"request": request, "sessions": recent_sessions},
    )


@router.get("/emsp-dashboard/audits/{pool_code}", dependencies=[Depends(check_admin_credentials)])
async def render_pool_audit_dashboard(request: Request, pool_code: int):
    """Renders the audit dashboard with the latest charging sessions for a specific pool."""
    recent_sessions = get_recent_sessions(limit=100, pool_code=pool_code)

    return templates.TemplateResponse(
        request=request,
        name="audit-dashboard.html",
        context={"request": request, "sessions": recent_sessions},
    )


@router.get("/api/export-audit/{transaction_id}", dependencies=[Depends(check_admin_credentials)])
async def export_session_audit(transaction_id: int):
    """Generates an XLSX file containing session metadata and tick-by-tick telemetry."""

    # 1. Fetch metadata (frozen rates, final totals)
    session_metadata = get_session_history(transaction_id)
    if not session_metadata:
        raise HTTPException(status_code=404, detail="No session metadata found for this transaction.")

    # 2. Fetch the time-series arrays
    telemetry_data = get_telemetry_for_session(transaction_id)
    if not telemetry_data:
        raise HTTPException(status_code=404, detail="No telemetry data found for this transaction.")

    # 3. Generate the blob
    excel_stream = generate_telemetry_excel(session_metadata, telemetry_data)

    headers = {"Content-Disposition": f'attachment; filename="charging_session_audit_{transaction_id}.xlsx"'}

    media_type = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
    return StreamingResponse(excel_stream, media_type=media_type, headers=headers)
