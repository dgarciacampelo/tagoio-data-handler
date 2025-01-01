import asyncio
import uvicorn
from fastapi import FastAPI, Depends
from fastapi.security import HTTPBasic
from typing import Annotated

from config import port as api_port
from routes.charge_point_alias import router as charge_point_alias_router
from routes.charge_point_update import router as charge_point_update_router
from routes.charging_session_update import router as charging_session_update_router
from routes.device_token import router as device_token_router
from security import check_credentials
from schedule_utils import setup_schedules
from tagoio.data_deletion import all_pools_variable_cleanup

app = FastAPI()
security = HTTPBasic()

# Add the imported routers to the FastAPI app
app.include_router(charge_point_alias_router)
app.include_router(charge_point_update_router)
app.include_router(charging_session_update_router)
app.include_router(device_token_router)


@app.get("/{version}/credentials-check")
async def do_credentials_check(username: Annotated[str, Depends(check_credentials)]):
    "Simple endpoint to check if the credentials validation is working..."
    return {"message": f"Welcome, {username}!"}


@app.get("/{version}/all-pools-variable-cleanup")
async def do_all_pools_cleanup(username: Annotated[str, Depends(check_credentials)]):
    "Deletes old variables from TagoIO, for all the registered pools"
    await all_pools_variable_cleanup()
    return {"message": "All pools variable cleanup completed"}


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
        print("Error in main:", e)
