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
    has_public_dashboard: bool  # If false, management dashboard only.


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


class ChargingSessionUpdate(BaseModel):
    """
    Update on a charging session (in progress or completed). If the station
    related to the charging session has a public dashboard and the charging
    session is completed, the dashboard information must be reset.
    """

    pool_code: int  # Code of the charging pool where the station is located
    station_name: str  # Name of the station related to the charging session
    connector_id: int  # Id of the connector where the charge is taking place
    transaction_id: int  # Id of the charging session in the CSMS

    card_alias: str  # Alias of the RFID card used to authorize the charge
    card_code: str  # Code of the RFID card used to authorize the charge
    display_id: str  # Alias of the station with the connector

    start_date: str  # Date with the DD/MM/YYYY format
    start_time: str  # Time with the HH:MM format
    step: str  # StrEnum, usually "INPROGRESS" or "COMPLETED"
    star_meter_value: int  # Meter value when the charging session started (Wh)
    last_meter_value: int  # Meter value received in the last update (Wh)

    energy: float  # (last_meter_value - star_meter_value) / 1000
    energy_unit: str = "KWh"
    cost: float
    cost_unit: str = "€"
    power: int
    power_unit: str = "W"
    time: str  # Translated in minutes

    has_public_dashboard: bool = False  # The station has a public dashboard
    stop_motive: Optional[str] = None  # StrEnum with the stop motive
    time_band: Optional[str] = None  # Time band in the HH:MM - HH:MM format
