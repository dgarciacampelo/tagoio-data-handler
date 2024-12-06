import secrets
from fastapi import Depends, status, HTTPException
from fastapi.security import HTTPBasic, HTTPBasicCredentials
from loguru import logger
from typing import Annotated, Union

from config import app_default_user, app_default_token


security = HTTPBasic()

# Log the HTTPBasic credentials used for authentication
logger.info(f"Using HTTPBasic credentials: {app_default_user}, {app_default_token}")


def compare_values(provided_value: str, correct_value: str):
    """Compares a pair of values using compare_digest to avoid timing attacks"""
    provided_value_bytes = provided_value.encode("utf8")
    correct_value_bytes = correct_value.encode("utf8")
    return secrets.compare_digest(provided_value_bytes, correct_value_bytes)


def get_username(
    credentials: Annotated[HTTPBasicCredentials, Depends(security)],
    correct_username: str,
    correct_password: str,
) -> Union[str, HTTPException]:
    """
    Returns the current username when good credentials are provided, raises an
    HTTPException otherwise. Uses compare_digest to avoid timing attacks.
    If no HTTPBasic credentials are provided, Depends(security) throws a 401.
    """

    is_correct_username = compare_values(credentials.username, correct_username)
    is_correct_password = compare_values(credentials.password, correct_password)
    if not (is_correct_username and is_correct_password):
        logger.warning(f"Incorrect username or password: {credentials}")
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Incorrect username or password",
            headers={"WWW-Authenticate": "Basic"},
        )
    return credentials.username


def check_credentials(credentials: Annotated[HTTPBasicCredentials, Depends(security)]):
    """Passing default values to parameters show its value in the docs"""
    return get_username(credentials, app_default_user, app_default_token)
