from enum import StrEnum


# Open Charge Point Protocol 1.6 (OCPP 1.6) enumerations


class ChargePointStatus(StrEnum):
    """
    Matchs the OCPP enumeration. Stores as all possible OCPP connector statuses.
    Status reported in StatusNotification.req. A status can be reported for the
    Charge Point main controller (connectorId = 0) or for a specific connector.
    Status for the Charge Point main controller is a subset of the enumeration:
    Available, Unavailable or Faulted.

    Available: When a Connector becomes available for a new user (Operative).
    Preparing: When a Connector becomes no longer available for a new user but
    there is no ongoing Transaction (yet). Typically a Connector is in
    preparing state when a user presents a tag, inserts a cable or a vehicle
    occupies the parking bay (Operative).
    Charging: When the contactor closes and the EV charges (Operative).
    SuspendedEVSE: When the EV is connected to the EVSE but the EVSE is not
    offering energy to the EV, e.g. due to a smart charging restriction, local
    supply power constraints, or as the result of StartTransaction.conf
    indicating that charging is not allowed etc. (Operative).
    SuspendedEV: When the EV is connected to the EVSE and the EVSE is offering
    energy but the EV is not taking any energy (Operative).
    Finishing: When a Transaction has stopped at a Connector, but the
    Connector is not yet available for a new user, e.g. the cable has not
    been removed or the vehicle has not left the parking bay (Operative).
    Reserved: When a Connector becomes reserved as a result of a Reserve
    Now command (Operative).

    Unavailable: When a Connector becomes unavailable as the result of a
    Change Availability command or an event upon which the Charge Point
    transitions to unavailable at its discretion. Upon receipt of a Change
    Availability command, the status MAY change immediately or the change MAY
    be scheduled. When scheduled, the Status Notification shall be send when
    the availability change becomes effective (Inoperative).
    Faulted: When a Charge Point or connector has reported an error and is not
    available for energy delivery (Inoperative).
    """

    # States considered Operative:
    AVAILABLE = "Available"
    PREPARING = "Preparing"
    CHARGING = "Charging"
    SUSPENDEDEVSE = "SuspendedEVSE"
    SUSPENDEDEV = "SuspendedEV"
    FINISHING = "Finishing"
    RESERVED = "Reserved"

    # States considered Inoperative:
    UNAVAILABLE = "Unavailable"
    FAULTED = "Faulted"


class AvailabilityType(StrEnum):
    "Requested availability change in ChangeAvailability.req."

    INOPERATIVE = "Inoperative"
    OPERATIVE = "Operative"


class ChargePointErrorCode(StrEnum):
    "Charge Point status reported in StatusNotification.req."

    """
    ConnectorLockFailure: Failure to lock or unlock connector.
    EVCommunicationError: Communication failure with the vehicle. This is not a
    real error in the sense that the Charge Point does not need to go to the
    faulted state. Instead, it should go to the SuspendedEVSE state.
    GroundFailure: Ground fault circuit interrupter has been activated.
    HighTemperature: Temperature inside Charge Point is too high.
    InternalError: Error in internal hardware or software component.
    LocalListConflict: The authorization information received from the Central
    System is in conflict with the LocalAuthorizationList.
    NoError: No error to report.
    OtherError: Other type of error. More information in vendorErrorCode.
    OverCurrentFailure: Over current protection device has tripped.
    OverVoltage: Voltage has risen above an acceptable level.
    PowerMeterFailure: Failure to read electrical/energy/power meter.
    PowerSwitchFailure: Failure to control power switch.
    ReaderFailure: Failure with idTag reader.
    ResetFailure: Unable to perform a reset.
    UnderVoltage: Voltage has dropped below an acceptable level.
    WeakSignal: Wireless communication device reports a weak signal.
    """

    CONNECTORLOCKFAILURE = "ConnectorLockFailure"
    EVCOMMUNICATIONERROR = "EVCommunicationError"
    GROUNDFAILURE = "GroundFailure"
    HIGHTEMPERATURE = "HighTemperature"
    INTERNALERROR = "InternalError"
    LOCALLISTCONFLICT = "LocalListConflict"
    NOERROR = "NoError"
    OTHERERROR = "OtherError"
    OVERCURRENTFAILURE = "OverCurrentFailure"
    OVERVOLTAGE = "OverVoltage"
    POWERMETERFAILURE = "PowerMeterFailure"
    POWERSWITCHFAILURE = "PowerSwitchFailure"
    READERFAILURE = "ReaderFailure"
    RESETFAILURE = "ResetFailure"
    UNDERVOLTAGE = "UnderVoltage"
    WEAKSIGNAL = "WeakSignal"


# Custom enumerations


class ConnectionStatus(StrEnum):
    "Booting means during the configuration process after boot notification."

    ONLINE = "Online"
    BOOTING = "Booting"
    OFFLINE = "Offline"


class ChargingSessionStep(StrEnum):
    """
    Enum class with steps for defining a charging session progression.
    STARTED:  The charging session has been created/started.
    INPROGRESS: The charging session is in progress/charging.
    COMPLETED: The charging session (and related transactions) has been finished.
    """

    STARTED = "STARTED"
    INPROGRESS = "INPROGRESS"
    COMPLETED = "COMPLETED"


class ValidationAlert(StrEnum):
    """
    Alert feedback messages using validation fields on TagoIO dashboards forms.
    ? https://help.tago.io/portal/en/community/topic/adding-feedback-message-to-users-using-validation-field-on-input-forms-21-1-2022
    """

    ACCEPT = "accept"
    REJECT = "danger"
    WARNING = "warning"
