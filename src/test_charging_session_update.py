import httpx

from config import test_pool_code, port, version, app_default_user, app_default_token
from enumerations import ChargingSessionStep
from schemas import ChargingSessionUpdate

station_name = "TEST"
connector_id = 1
request_suffix = f"{test_pool_code}/{station_name}/{connector_id}"
route = "charging-session-update"
request_url = f"http://localhost:{port}/{version}/{route}/{request_suffix}"

auth = httpx.BasicAuth(username=app_default_user, password=app_default_token)
client = httpx.Client()  # Using sync client due to pytest lack of async support

update = ChargingSessionUpdate(
    pool_code=test_pool_code,
    station_name=station_name,
    connector_id=connector_id,
    transaction_id=1011011,
    card_alias="John Doe",
    card_code="1234567890123456",
    display_id=f"{station_name} [{connector_id}]",
    start_date="01/01/2024",
    start_time="09:43",
    step=ChargingSessionStep.COMPLETED,
    star_meter_value=2104,
    last_meter_value=2152,
    energy=0.048,
    cost=0.0192,
    power=0,
    time="2 min",
    has_public_dashboard=True,
    stop_motive="CAR",
    time_band="09:43 - 09:45",
)


def test_charging_session_update():
    "Tests the function to update a charging session values"
    headers = {"Content-Type": "application/json"}
    data = update.model_dump_json()
    with client:
        response = client.post(request_url, auth=auth, headers=headers, json=data)
        assert response.status_code == 200
