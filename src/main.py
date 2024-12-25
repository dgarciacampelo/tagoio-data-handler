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


app = FastAPI()
security = HTTPBasic()

# Add the imported routers to the FastAPI app
app.include_router(charge_point_alias_router)
app.include_router(charge_point_update_router)
app.include_router(charging_session_update_router)
app.include_router(device_token_router)


@app.get("/")
async def get_root():
    return {"message": "Server is running"}


@app.get("/{version}/credentials-check")
async def do_credentials_check(username: Annotated[str, Depends(check_credentials)]):
    return {"message": f"Welcome, {username}!"}


if __name__ == "__main__":
    asyncio.run(uvicorn.run(app, host="0.0.0.0", port=api_port))
