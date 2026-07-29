from datetime import datetime
from typing import Optional

from pydantic import BaseModel, EmailStr, Field, model_validator

from enumerations import ChargePointStatus, ValidationAlert


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
    charge_point_status: ChargePointStatus
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
    return DeviceData(pool_code=pool_code, device_id=device_id, device_token=hidden_token).model_dump()


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
    start_meter_value: int  # Meter value when the charging session started (Wh)
    last_meter_value: int  # Meter value received in the last update (Wh)
    last_meter_ts: str  # Timestamp of the last meter value received, in ISO format
    current_tariff_band: str  # Evaluated tariff band for the meter value update: "Peak", "Flat", "Off-Peak"

    # Energy rates, taken from the pool configuration. They should be frozen at the start of the charging session
    rate_off_peak: float  # Rate in €/kWh for the off-peak time band
    rate_flat: float  # Rate in €/kWh for the flat time band
    rate_peak: float  # Rate in €/kWh for the peak time band

    # Energy values consumed by the charging session for each time band, updated each meter values update
    energy_off_peak: int  # Energy consumed in the off-peak time band (Wh)
    energy_flat: int  # Energy consumed in the flat time band (Wh)
    energy_peak: int  # Energy consumed in the peak time band (Wh)

    energy: float  # As kWh: (last_meter_value - start_meter_value) / 1000
    energy_unit: str = "KWh"
    cost: float  # Total evaluated cost of the charging session
    cost_unit: str = "€"
    power: int  # Last updated power of the charging session
    power_unit: str = "W"
    time: str  # Translated in minutes

    with_payment: bool = False  # Whether the session has an associated payment
    has_public_dashboard: bool = False  # The station has a public dashboard
    stop_motive: Optional[str] = None  # StrEnum with the stop motive
    time_band: Optional[str] = None  # Total session time in HH:MM - HH:MM format, only included after completion


class FeedbackMessage(BaseModel):
    "Data of a feedback message. The variable links the validation field."

    pool_code: int
    variable: str
    message: str
    group: str = "validation_feedback"
    type: ValidationAlert = ValidationAlert.ACCEPT


class PaymentAuthRequest(BaseModel):
    pool_code: int
    station_name: str
    connector_id: int
    email: EmailStr  # Payment link email
    requires_invoice: bool

    # Invoice fields
    receipt_fiscal_id: Optional[str] = None
    receipt_name: Optional[str] = None
    receipt_address: Optional[str] = None
    receipt_email: Optional[EmailStr] = None

    @model_validator(mode="after")
    def check_invoice_fields(self):
        if self.requires_invoice and not all(
            [self.receipt_fiscal_id, self.receipt_name, self.receipt_address, self.receipt_email]
        ):
            raise ValueError("All invoice fields are mandatory when requires_invoice is true")
        return self


class PoolConfigUpdate(BaseModel):
    """Payload to hot-reload pool configuration caching in the TagoIO handler."""

    pool_code: int

    # CPO Info
    cpo_name: Optional[str] = None
    cpo_fiscal_id: Optional[str] = None
    cpo_address: Optional[str] = None
    cpo_phone: Optional[str] = None
    cpo_email: Optional[str] = None
    cpo_web: Optional[str] = None

    # Rates Info
    rate_off_peak: Optional[float] = None
    rate_flat: Optional[float] = None
    rate_peak: Optional[float] = None
    vat: Optional[float] = None
    preauth_amount: Optional[float] = None


class RFIDCard(BaseModel):
    """Represents an RFID card that can be used to authorize charging sessions."""

    card_id: str = Field(..., description="The unique physical or virtual card identifier.")
    card_alias: str = Field(..., description="Display name for dashboards and UI.")
    linked_to: set[str] = Field(default_factory=set, description="Serial IDs of Charge Points this card can activate.")
    email: Optional[str] = Field(default=None, description="Email of the cardholder or CPO.")
    pk: int = Field(default=-1, exclude=True)  # Local PostgreSQL PK, excluded from JSON dumps by default.

    def __hash__(self) -> int:
        return hash(self.card_id)

    def __eq__(self, other: object) -> bool:
        if isinstance(other, RFIDCard):
            return self.card_id == other.card_id
        if isinstance(other, str):
            return self.card_id == other
        return False


class PoolDeviceSetupRequest(BaseModel):
    """
    Request payload sent by the CSMS to ensure a TagoIO device exists
    for a given charging pool, creating it with defaults if missing.
    """

    pool_code: int = Field(..., description="The unique code of the charging pool")
    server_alias: str = Field(default="Neos", description="Alias of the CSMS server")
    advanced_plan: bool = Field(default=True, description="Whether to provision the device with an ADVANCED plan")


class PoolDeviceSetupResponse(BaseModel):
    """
    Response returned to the CSMS containing the device credentials
    and the pool's remote configuration (fetched or newly defaulted).
    """

    pool_code: int
    is_newly_created: bool

    # Installation & Power Configuration
    load_balancing_mode: str = Field(default="Ninguno")
    max_installation_power: float = Field(default=5000.0)

    # Financial Rates and VAT (0.1: 10% VAT)
    rate_off_peak: float = Field(default=0.4)
    rate_flat: float = Field(default=0.4)
    rate_peak: float = Field(default=0.4)
    vat: float = Field(default=0.1)
    preauth_amount: float = Field(default=40.0, description="Amount to preauthorize for charging sessions, in €")

    # Optional CPO Info (Will be None if newly created, or populated if fetched)
    cpo_name: Optional[str] = None
    cpo_fiscal_id: Optional[str] = None
    cpo_address: Optional[str] = None
    cpo_phone: Optional[str] = None
    cpo_email: Optional[str] = None
    cpo_web: Optional[str] = None

    # Authorization
    cards: list[RFIDCard] = Field(default_factory=list)
