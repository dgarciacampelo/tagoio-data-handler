from fastapi import APIRouter, Request
from fastapi.templating import Jinja2Templates

from charge_points import get_all_known_charge_points
from config import tagoio_handler_url

router = APIRouter()
templates = Jinja2Templates(directory="templates")


@router.get("/emsp-dashboard")
async def render_emsp_dashboard(request: Request):
    """Renders the eMSP dashboard with all known charging stations."""
    pools_data = get_all_known_charge_points()

    # Extract unique station names per pool code
    stations_by_pool: dict[int, list[str]] = {}
    for pool_code, cp_set in pools_data.items():
        # cp_set contains tuples of (station_name, connector_id)
        unique_stations = sorted(list(set([station_name for station_name, cid in cp_set])))
        if unique_stations:
            stations_by_pool[pool_code] = unique_stations

    return templates.TemplateResponse(
        "emsp-dashboard.html",
        {"request": request, "stations_by_pool": stations_by_pool, "base_url": tagoio_handler_url},
    )
