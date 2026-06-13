"""This module defines Pydantic models for parsing TagoIO Analysis payloads."""

from typing import Any, Optional, Union
from typing_extensions import Literal
from enumerations import SSEEventType
from pydantic import BaseModel, Field


class TagoDataPoint(BaseModel):
    """Parses an individual data block within a TagoIO scope payload."""

    id: str
    device: str = Field(..., description="The unique TagoIO device/pool identifier")
    variable: str
    value: Any
    group: Optional[str] = None  # TagoIO variable group, not related to OCPP pools
    time: Optional[str] = None
    metadata: dict[str, Any] = Field(default_factory=dict)


class BaseSSEEvent(BaseModel):
    """The generic envelope structure broadcasted out of the SSE Broker."""

    # event_type: SSEEventType (commented out to avoid: overrides symbol of same name in class "BaseSSEEvent")
    tago_device_id: str
    pool_code: int = Field(..., description="The Charging Pool ID")


# endregion
# region Pool Mgmt.


class LoadBalancingEvent(BaseSSEEvent):
    event_type: Literal[SSEEventType.LOAD_BALANCING] = SSEEventType.LOAD_BALANCING
    selected_mode: str


class MaxGridPowerEvent(BaseSSEEvent):
    event_type: Literal[SSEEventType.MAX_GRID_POWER] = SSEEventType.MAX_GRID_POWER
    max_power_watts: float


class RFIDManagementEvent(BaseSSEEvent):
    event_type: Literal[SSEEventType.RFID_MANAGEMENT] = SSEEventType.RFID_MANAGEMENT
    card_id: str
    action: Literal["create", "delete"]
    linked_cps: list[str] = Field(default_factory=list, description="List of serial IDs")
    alias: Optional[str] = None
    email: Optional[str] = None


class CPOInfoEvent(BaseSSEEvent):
    event_type: Literal[SSEEventType.CPO_INFO] = SSEEventType.CPO_INFO
    name: str
    fiscal_id: str
    address: str
    phone: str
    email: str
    web: str = ""


class RateListEvent(BaseSSEEvent):
    event_type: Literal[SSEEventType.RATE_LIST] = SSEEventType.RATE_LIST
    rate_off_peak: float
    rate_flat: float
    rate_peak: float
    vat: float
    withholding_amount: float


class PowerUpdateEvent(BaseSSEEvent):
    event_type: Literal[SSEEventType.POWER_UPDATE] = SSEEventType.POWER_UPDATE
    meter_watts: float


# endregion
# region Station Mgmt.


class ChangeAvailabilityEvent(BaseSSEEvent):
    """Dedicated event for toggling overall operativity directly via the dashboard."""

    event_type: Literal[SSEEventType.AVAILABILITY] = SSEEventType.AVAILABILITY
    serial_id: str


# Discriminators for Debug Tab OCPP Requests


class StatusNotificationPayload(BaseModel):
    request: Literal["status_notification"]
    connector_id: int


class ChangeAvailabilityPayload(BaseModel):
    request: Literal["change_availability"]
    connector_id: int
    availability_type: Literal["available", "unavailable"]


class ResetPayload(BaseModel):
    request: Literal["reset"]
    reset_type: Literal["soft", "hard"]


class RemoteStartPayload(BaseModel):
    request: Literal["remote_start_transaction"]
    station_name: str
    connector_id: int
    id_tag: str


class RemoteStopPayload(BaseModel):
    request: Literal["remote_stop_transaction"]
    transaction_id: int


# Pydantic will route the validation to the correct subclass based on the 'request' field
OCPPActionPayload = Union[
    StatusNotificationPayload, ChangeAvailabilityPayload, ResetPayload, RemoteStartPayload, RemoteStopPayload
]


class OCPPRequestEvent(BaseSSEEvent):
    """Handles commands dispatched from the Debug Tag in the Management Dashboard."""

    event_type: Literal[SSEEventType.OCPP_REQUEST] = SSEEventType.OCPP_REQUEST
    serial_ids: list[str] = Field(..., description="Target charge points")
    payload: OCPPActionPayload


# endregion
# region Virtual POS Events


class VPOSStartEvent(BaseSSEEvent):
    """Parameters from the public dashboard to identify charging place and authorize the transaction."""

    event_type: Literal[SSEEventType.VPOS_START] = SSEEventType.VPOS_START
    station_name: str
    connector_id: int

    email: str  # EV user email to receive URL link to the virtual POS payment page
    # Optional fields for the receipt information (requested to the Charging Pool CPO):
    receipt_email: Optional[str] = None
    receipt_name: Optional[str] = None
    receipt_fiscal_id: Optional[str] = None
    receipt_address: Optional[str] = None

    amount: float = 40.0  # Amount in currency units (EUR)
    currency: str = "EUR"


class VPOSStopEvent(BaseSSEEvent):
    """Parameters from the public dashboard where the EV user has requested to stop the session."""

    event_type: Literal[SSEEventType.VPOS_STOP] = SSEEventType.VPOS_STOP
    station_name: str
    connector_id: int
