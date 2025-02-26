import asyncio
import sys
import uvicorn
from fastapi import FastAPI, Depends
from fastapi.security import HTTPBasic
from loguru import logger
from typing import Annotated

from config import port as api_port
from routes.charge_point_alias import router as charge_point_alias_router
from routes.charge_point_update import router as charge_point_update_router
from routes.charging_session_update import router as charging_session_update_router
from routes.device_token import router as device_token_router
from routes.feedback_message import router as feedback_message_router
from routes.trigger_task import router as trigger_task_router
from security import check_credentials
from schedule_utils import setup_schedules

# ? https://loguru.readthedocs.io/en/stable/api/logger.html#sink
logger.remove()
logger.add(sys.stderr, colorize=True)


app = FastAPI()
security = HTTPBasic()

# Add the imported routers to the FastAPI app
app.include_router(charge_point_alias_router)
app.include_router(charge_point_update_router)
app.include_router(charging_session_update_router)
app.include_router(device_token_router)
app.include_router(feedback_message_router)
app.include_router(trigger_task_router)


@app.get("/{version}/credentials-check")
async def do_credentials_check(username: Annotated[str, Depends(check_credentials)]):
    "Simple endpoint to check if the credentials validation is working..."
    return {"message": f"Welcome, {username}!"}


async def setup_rest_api_server():
    "Starts the FastAPI REST server."
    config_params = {"app": app, "host": "0.0.0.0", "port": api_port}
    rest_server = uvicorn.Server(config=uvicorn.Config(**config_params))
    await rest_server.serve()


async def main():
    "Uses asyncio tasks to avoid the schedule library blocking uvicorn."
    rest_server_task = asyncio.create_task(setup_rest_api_server())
    schedules_task = asyncio.create_task(setup_schedules())
    await asyncio.gather(rest_server_task, schedules_task)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
    except Exception as e:
        logger.error(f"Error in main function: {e}")
