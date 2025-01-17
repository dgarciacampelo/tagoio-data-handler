from fastapi import APIRouter, Depends
from fastapi.security import HTTPBasic
from typing import Annotated

from config import version  # noqa: F401
from schedule_utils import conditional_database_backup
from security import check_credentials
from tagoio.data_deletion import all_pools_variable_cleanup, delete_variable_in_cloud

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


@router.delete("/{version}/trigger-task/single-variable/{pool_code}/{variable_name}")
async def delete_single_cloud_variable(
    pool_code: int,
    variable_name: str,
    username: Annotated[str, Depends(check_credentials)],
):
    "Removes al occurrences of a variable from the given device (by pool code) in TagoIO"
    result = await delete_variable_in_cloud(pool_code, variable_name, 0)
    delete_count: int = 0
    if "status" in result and result["status"]:
        result_msg: str = result["result"]  # X Data Removed
        delete_count = int(result_msg.split(" ")[0])
    return {"message": f"{delete_count} deleted {variable_name} from {pool_code}"}
