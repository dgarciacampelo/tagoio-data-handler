from fastapi import APIRouter, Depends
from fastapi.security import HTTPBasic
from typing import Annotated

from config import version  # noqa: F401
from schedule_utils import conditional_database_backup
from security import check_credentials
from tagoio.data_deletion import all_pools_variable_cleanup

router = APIRouter()
security = HTTPBasic()


"REST API router to manage maintenance tasks"


@router.get("/{version}/trigger-task/pool-variable-cleanup")
async def do_all_pools_cleanup(username: Annotated[str, Depends(check_credentials)]):
    "Deletes old variables from TagoIO, for all the registered pools"
    await all_pools_variable_cleanup()
    return {"message": "All pools variable cleanup completed"}


@router.get("/{version}/trigger-task/backup-to-telegram")
async def do_backup_to_telegram(username: Annotated[str, Depends(check_credentials)]):
    "Does a user triggered backup to Telegram of the service database file"
    conditional_database_backup(True)
    return {"message": "Backup to Telegram has been triggered"}
