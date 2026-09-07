import asyncio
import sys
from logging import DEBUG, INFO, WARNING  # noqa: F401
from typing import Annotated

import uvicorn
from fastapi import Depends, FastAPI
from fastapi.security import HTTPBasic
from fastapi.staticfiles import StaticFiles
from loguru import logger
from starlette.middleware.sessions import SessionMiddleware

from config import dashboard_secret_key
from config import port as api_port
from routes.audit_dashboard import router as audit_dashboard_router  # For the "Audit Dashboard" for internal use
from routes.charge_point_alias import router as charge_point_alias_router
from routes.charge_point_update import router as charge_point_update_router
from routes.charging_pool_update import router as charging_pool_update_router
from routes.charging_session_update import router as charging_session_update_router
from routes.cpo_provisioning import router as cpo_provisioning_router  # For provisioning new CPOs on TagoIO
from routes.device_token import router as device_token_router
from routes.emsp_dashboard import router as emsp_dashboard_router  # For the "eMSP Dashboard" for eMSP managers
from routes.feedback_message import router as feedback_message_router
from routes.pool_management import router as pool_management_router  # For managing the charging pool configurations
from routes.public_dashboard import router as public_dashboard_router  # For the "Smart Dashboard" for OCPP Stations
from routes.sse_stream import router as sse_stream_router  # For the SSE event stream for CSMS instances
from routes.station_management import router as station_management_router
from routes.trigger_task import router as trigger_task_router
from security import check_credentials
from tagoio_analysis.lifecycle import application_lifespan
from utils.http_client import GlobalHTTPClient

# ? https://loguru.readthedocs.io/en/stable/api/logger.html#sink
logger.remove()
logger.add(sys.stderr, level=INFO, colorize=True)


app = FastAPI(lifespan=application_lifespan)  # With TagoIO Analysis Worker Lifespan handler
security = HTTPBasic()
app.add_middleware(SessionMiddleware, secret_key=dashboard_secret_key, max_age=604800)

# Mount the static directory
app.mount("/static", StaticFiles(directory="static"), name="static")

# Add the imported routers to the FastAPI app
app.include_router(audit_dashboard_router)
app.include_router(charge_point_alias_router)
app.include_router(charge_point_update_router)
app.include_router(charging_pool_update_router)
app.include_router(charging_session_update_router)
app.include_router(cpo_provisioning_router)
app.include_router(device_token_router)
app.include_router(emsp_dashboard_router)
app.include_router(feedback_message_router)
app.include_router(pool_management_router)
app.include_router(public_dashboard_router)
app.include_router(sse_stream_router)
app.include_router(station_management_router)
app.include_router(trigger_task_router)


@app.get("/{version}/credentials-check")
async def do_credentials_check(username: Annotated[str, Depends(check_credentials)]):
    "Simple endpoint to check if the credentials validation is working..."
    return {"message": f"Welcome, {username}!"}


async def setup_rest_api_server():
    "Starts the FastAPI REST server."
    config = {"app": app, "host": "0.0.0.0", "port": api_port, "log_level": "warning"}
    rest_server = uvicorn.Server(config=uvicorn.Config(**config))
    await rest_server.serve()


async def main():
    """
    Uses asyncio tasks to avoid the schedule library blocking uvicorn.

    All background tasks (Schedules, Pool Configs, TagoIO Workers) are now strictly
    managed by the FastAPI lifespan handler in lifecycle.py. We only need to start
    the REST server here.
    """
    try:
        GlobalHTTPClient.get_client()
        await setup_rest_api_server()

    except KeyboardInterrupt:
        logger.warning("Shutting down Service due to manual shutdown...")
    except Exception as e:
        logger.critical(f"Main app server-side error: {e}")
    finally:  # Safely close the global HTTP client when shutting down
        await GlobalHTTPClient.close()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
    except Exception as e:
        logger.error(f"Error in main function: {e}")
