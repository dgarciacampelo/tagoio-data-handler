from fastapi import APIRouter, Depends, HTTPException, status
from loguru import logger

from security import check_admin_credentials
from database.query_database import delete_station_from_db
from data_handling import remove_station_from_memory

router = APIRouter()


@router.delete("/api/stations/{pool_code}/{station_name}", dependencies=[Depends(check_admin_credentials)])
async def delete_station(pool_code: int, station_name: str):
    """Deletes a charging station from the database and active memory."""
    try:
        # 1. Remove from SQLite Database
        db_deleted = delete_station_from_db(pool_code, station_name)

        if not db_deleted:
            raise_detail: str = f"Station {station_name} not found in pool {pool_code}."
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=raise_detail)

        # 2. Remove from active application memory
        remove_station_from_memory(pool_code, station_name)

        logger.info(f"Station {station_name} from pool {pool_code} successfully deleted.")
        return {"status": "success", "message": f"Station {station_name} deleted successfully."}

    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Error deleting station {station_name}: {e}")
        raise_detail: str = f"An error occurred while trying to delete station {station_name} from pool {pool_code}."
        raise HTTPException(status_code=status.HTTP_500_INTERNAL_SERVER_ERROR, detail=raise_detail)
