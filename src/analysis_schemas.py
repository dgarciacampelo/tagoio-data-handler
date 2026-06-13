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
    station_name: str


# Discriminators for Debug Tab OCPP Requests


class StatusNotificationPayload(BaseModel):
    request: Literal["status_notification"]
    station_names: list[str] = Field(..., description="Target charge points")
    connector_id: int


class ChangeAvailabilityPayload(BaseModel):
    request: Literal["change_availability"]
    station_names: list[str]
    connector_id: int
    availability_type: Literal["Operative", "Inoperative"]  # Strict OCPP 1.6


class ResetPayload(BaseModel):
    request: Literal["reset"]
    station_names: list[str] = Field(..., description="Target charge points")
    reset_type: Literal["Soft", "Hard"]  # Capitalized to match OCPP spec


class RemoteStartPayload(BaseModel):
    request: Literal["remote_start_transaction"]
    station_names: list[str] = Field(..., description="Target charge points")
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
    payload: OCPPActionPayload


# endregion
# region Virtual POS Events


class VPOSStartEvent(BaseSSEEvent):
    """Parameters from the public dashboard to identify charging place and authorize the transaction."""

    event_type: Literal[SSEEventType.VPOS_START] = SSEEventType.VPOS_START
    station_name: str
    connector_id: int

    email: str  # EV user email to receive URL link to the virtual POS payment page
    amount: float = 40.0  # Amount in currency units (EUR)
    currency: str = "EUR"

    # Optional fields for the receipt information (requested to the Charging Pool CPO):
    requires_invoice: bool
    receipt_email: Optional[str] = None
    receipt_name: Optional[str] = None
    receipt_fiscal_id: Optional[str] = None
    receipt_address: Optional[str] = None


class VPOSStopEvent(BaseSSEEvent):
    """Parameters from the public dashboard where the EV user has requested to stop the session."""

    event_type: Literal[SSEEventType.VPOS_STOP] = SSEEventType.VPOS_STOP
    station_name: str
    connector_id: int
