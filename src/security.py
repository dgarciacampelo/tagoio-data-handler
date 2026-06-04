import secrets
from typing import Annotated, Optional

from fastapi import Depends, HTTPException, Request, status
from fastapi.security import HTTPBasic, HTTPBasicCredentials
from loguru import logger

from config import app_admin_token, app_admin_user, app_default_token, app_default_user

# Standard strict Basic Auth for machine-to-machine API calls
security = HTTPBasic()

# Lenient Basic Auth for the dashboard (allows cookies to bypass the auth prompt)
dashboard_security = HTTPBasic(auto_error=False)

# Log the HTTPBasic credentials used for authentication
logger.info(f"Using API credentials:  {app_default_user}")
logger.info(f"Using Admin credentials: {app_admin_user}")


def compare_values(provided_value: str, correct_value: str) -> bool:
    """Compares a pair of values using compare_digest to avoid timing attacks"""
    provided_value_bytes = provided_value.encode("utf8")
    correct_value_bytes = correct_value.encode("utf8")
    return secrets.compare_digest(provided_value_bytes, correct_value_bytes)


def get_username(
    credentials: HTTPBasicCredentials,
    correct_username: str,
    correct_password: str,
) -> str:
    """
    Returns the current username when good credentials are provided, raises an
    HTTPException otherwise. Uses compare_digest to avoid timing attacks.
    """
    is_correct_username = compare_values(credentials.username, correct_username)
    is_correct_password = compare_values(credentials.password, correct_password)

    if not (is_correct_username and is_correct_password):
        logger.warning(f"Incorrect username or password attempt: {credentials.username}")
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Incorrect username or password",
            headers={"WWW-Authenticate": "Basic"},
        )
    return credentials.username


def check_credentials(credentials: Annotated[HTTPBasicCredentials, Depends(security)]):
    """
    Dependency for internal API endpoints.
    Strictly requires standard Basic Auth headers.
    """
    if not app_default_user or not app_default_token:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Missing server credentials configuration",
            headers={"WWW-Authenticate": "Basic"},
        )

    return get_username(credentials, app_default_user, app_default_token)


def check_admin_credentials(
    request: Request, credentials: Optional[HTTPBasicCredentials] = Depends(dashboard_security)
):
    """
    Dependency for admin dashboards.
    Checks for a valid 7-day session cookie first. If missing, prompts for Basic Auth.
    """
    # 1. Check if the user already has a valid signed session cookie
    if request.session.get("admin_authenticated"):
        return True

    # 2. Configuration safeguard
    if not app_admin_user or not app_admin_token:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="Admin credentials are not configured on the server.",
        )

    # 3. If no cookie and no auth headers, trigger the browser's Basic Auth popup
    if not credentials:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Not authenticated",
            headers={"WWW-Authenticate": "Basic"},
        )

    # 4. Validate the Basic Auth credentials provided in the prompt
    username = get_username(credentials, app_admin_user, app_admin_token)

    # 5. Credentials are correct. Set the session cookie for future visits.
    request.session["admin_authenticated"] = True
    return username
