from pydantic import BaseModel
from datetime import datetime
from typing import Optional


class ChargePointUpdateBody(BaseModel):
    "Contains the HTTP request body data for a charge point status update"

    connector_id: int
    connection_status: str  # ? str(ConnectionStatus)
    charge_point_status: str  # ? str(ChargePointStatus)
    availability_type: str  # ? str(AvailabilityType)
    charge_point_error_code: str  # ? str(ChargePointErrorCode)
    has_pulic_dashboard: bool  # If false, management dashboard only.


class ChargePointUpdate(ChargePointUpdateBody):
    "Contains the data needed to store a charge point status update"

    pool_code: int  # Charging pool where the station is located
    station_name: str  # Name of the charging station


class ChargePointData(BaseModel):
    """
    Manages the data of a charge point.
    Due to hardware fault, a charge point may report status changes every few
    seconds, causing problems related to usage quotas in the TagoIO platform.
    To handle this problem, a charge point may be "quarantined" for a certain
    period of time in which no updates are send to the cloud. platform.
    """

    pool_code: int
    station_name: str
    connector_id: int
    charge_point_status: str
    is_quarantined: bool = False
    quarantine_end: Optional[datetime] = None
    last_update: Optional[datetime] = None
    omitted_updates: int = 0  # Due to quarantine being active


class DeviceData(BaseModel):
    "Data required to store variables in the TagoIO platform, by device"

    pool_code: int
    device_id: str
    device_token: str


def device_data_dump(pool_code: int, device_id: str, device_token: str):
    "Model dump of the DeviceData class, with device token partially hidden"
    hidden_token = device_token[:8] + "-****-****-****-************"
    return DeviceData(
        pool_code=pool_code, device_id=device_id, device_token=hidden_token
    ).model_dump()
