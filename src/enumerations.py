from enum import StrEnum


# Open Charge Point Protocol 1.6 (OCPP 1.6) enumerations


class ChargePointStatus(StrEnum):
    """
    Status reported in StatusNotification.req. A status can be reported for the
    Charge Point main controller (connectorId = 0) or for a specific connector.
    Status for the Charge Point main controller is a subset of the enumeration:
    Available, Unavailable or Faulted.
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
