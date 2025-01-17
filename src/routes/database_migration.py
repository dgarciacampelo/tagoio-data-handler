from fastapi import APIRouter, Depends
from fastapi.security import HTTPBasic
from typing import Annotated

from config import version  # noqa: F401
from security import check_credentials

router = APIRouter()
security = HTTPBasic()


"REST API router to send SQL queries to the database (without login to the server)"

# TODO: Add endpoint to send SQL queries
