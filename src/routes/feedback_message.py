import json
from fastapi import APIRouter, Depends, status, HTTPException, Request
from fastapi.security import HTTPBasic
from loguru import logger
from pydantic import ValidationError
from typing import Annotated

from config import version  # noqa: F401
from schemas import FeedbackMessage
from security import check_credentials
from tagoio.data_parsing import send_feedback_message

router = APIRouter()
security = HTTPBasic()


"REST API router to manage feedback messages for the dashboard users"


@router.post(
    "/{version}/feedback-message/{pool_code}",
    status_code=status.HTTP_200_OK,
)
async def send_dashboard_feedback_message(
    pool_code: int,
    request: Request,
    username: Annotated[str, Depends(check_credentials)],
):
    try:
        json_string = await request.json()
        data = json.loads(json_string)
        feedback = FeedbackMessage(
            pool_code=pool_code,
            variable=data["variable"],
            message=data["message"],
            type=data["type"],
        )
        await send_feedback_message(feedback)

    except json.JSONDecodeError:
        logger.error("Error parsing JSON response at charging_session_update")
        status_code = status.HTTP_500_INTERNAL_SERVER_ERROR
        raise HTTPException(status_code=status_code, detail="Error parsing JSON")
    except ValidationError as e:
        status_code = status.HTTP_422_UNPROCESSABLE_ENTITY
        raise HTTPException(status_code=status_code, detail=str(e))
