import asyncio
import sys
from logging import DEBUG, INFO, WARNING  # noqa: F401
from typing import Annotated

import uvicorn
from fastapi import Depends, FastAPI
from fastapi.security import HTTPBasic
from fastapi.staticfiles import StaticFiles
from loguru import logger

from config import port as api_port
from routes.charge_point_alias import router as charge_point_alias_router
from routes.charge_point_update import router as charge_point_update_router
from routes.charging_session_update import router as charging_session_update_router
from routes.device_token import router as device_token_router
from routes.feedback_message import router as feedback_message_router
from routes.public_dashboard import router as public_dashboard_router  # For the "Smart Dashboard" for OCPP Stations
from routes.trigger_task import router as trigger_task_router
from schedule_utils import register_schedules, run_schedule_loop
from security import check_credentials
from tagoio.pool_setup_fetching import init_pool_configs
from tagoio.token_fetching import get_all_devices_data

# ? https://loguru.readthedocs.io/en/stable/api/logger.html#sink
logger.remove()
logger.add(sys.stderr, level=INFO, colorize=True)


app = FastAPI()
security = HTTPBasic()

# Mount the static directory
app.mount("/static", StaticFiles(directory="static"), name="static")

# Add the imported routers to the FastAPI app
app.include_router(charge_point_alias_router)
app.include_router(charge_point_update_router)
app.include_router(charging_session_update_router)
app.include_router(device_token_router)
app.include_router(feedback_message_router)
app.include_router(trigger_task_router)
app.include_router(public_dashboard_router)


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
    """Uses asyncio tasks to avoid the schedule library blocking uvicorn."""

    # Extract the known charging pool codes from the devices dictionary
    devices_data = get_all_devices_data()
    known_pools = list(devices_data.keys())

    # 1. Register schedules synchronously first
    register_schedules()

    # 2. Create the concurrent background tasks
    rest_server_task = asyncio.create_task(setup_rest_api_server())
    schedules_loop_task = asyncio.create_task(run_schedule_loop())
    pool_configs_task = asyncio.create_task(init_pool_configs(known_pools))

    # 3. Run them all concurrently
    await asyncio.gather(rest_server_task, schedules_loop_task, pool_configs_task)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
    except Exception as e:
        logger.error(f"Error in main function: {e}")
