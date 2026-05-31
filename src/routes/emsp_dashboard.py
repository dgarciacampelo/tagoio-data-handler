from fastapi import APIRouter, Request
from fastapi.templating import Jinja2Templates

from charge_points import get_all_known_charge_points
from config import tagoio_handler_url
from data_handling import get_charge_point
from enumerations import ChargePointStatus
from tagoio.pool_setup_fetching import get_pool_config

router = APIRouter()
templates = Jinja2Templates(directory="templates")


@router.get("/emsp-dashboard")
async def render_emsp_dashboard(request: Request):
    """Renders the eMSP dashboard with all known charging stations and their connectors."""
    pools_data = get_all_known_charge_points()

    stations_by_pool: dict[int, list[str]] = {}
    cpo_names_by_pool: dict[int, str] = {}
    station_connectors: dict[int, dict[str, dict[int, str]]] = {}  # ? { pool: { station: { conn_id: status } } }

    for pool_code, cp_set in pools_data.items():
        if cp_set:
            config = get_pool_config(pool_code)
            cpo_names_by_pool[pool_code] = config.cpo_name

            station_connectors[pool_code] = {}

            # Group and fetch status for every connector ID associated with the station
            for station_name, cid in sorted(list(cp_set)):
                if cid == 0:  # Skip the input connector (Connector 0)
                    continue
                if station_name not in station_connectors[pool_code]:
                    station_connectors[pool_code][station_name] = {}

                cp_data = get_charge_point(pool_code, station_name, connector_id=cid)
                if cp_data:
                    status_val = cp_data.charge_point_status.value
                else:
                    status_val = ChargePointStatus.UNAVAILABLE.value

                station_connectors[pool_code][station_name][cid] = status_val

            # Maintain a sorted list of unique station names for template iteration
            stations_by_pool[pool_code] = sorted(list(station_connectors[pool_code].keys()))

    return templates.TemplateResponse(
        "emsp-dashboard.html",
        {
            "request": request,
            "stations_by_pool": stations_by_pool,
            "cpo_names_by_pool": cpo_names_by_pool,
            "station_connectors": station_connectors,
            "base_url": tagoio_handler_url,
        },
    )
