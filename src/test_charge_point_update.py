import httpx

from config import test_pool_code, port, version, app_default_user, app_default_token
from enumerations import (
    AvailabilityType,
    ChargePointErrorCode,
    ChargePointStatus,
    ConnectionStatus,
)
from schemas import ChargePointUpdateBody

station_name = "TEST"
connector_id = 1
request_suffix = f"{test_pool_code}/{station_name}/{connector_id}"
request_url = f"http://localhost:{port}/{version}/charge-point-update/{request_suffix}"

auth = httpx.BasicAuth(username=app_default_user, password=app_default_token)
client = httpx.Client()  # Using sync client due to pytest lack of async support

update = ChargePointUpdateBody(
    connector_id=connector_id,
    connection_status=ConnectionStatus.BOOTING,
    charge_point_status=ChargePointStatus.AVAILABLE,
    availability_type=AvailabilityType.OPERATIVE,
    charge_point_error_code=ChargePointErrorCode.NOERROR,
    has_public_dashboard=True,
)


def test_charge_point_update():
    "Tests the function to update a charge point status"
    headers = {"Content-Type": "application/json"}
    data = update.model_dump_json()
    with client:
        response = client.post(request_url, auth=auth, headers=headers, json=data)
        assert response.status_code == 200
