import httpx
from datetime import datetime, timedelta

"""
! The TagoIO platform allows 50_000 registers per device at most. When the
limit is reached, following requests will error and no new data will be stored.
"""

# TODO: Properly integrate the delete function in the code and use async client

client = httpx.Client()


base_url = "https://api.tago.io/data?variable="
variable = "charging_session_data"
device_token = "token for the device to be cleaned"


def test_variable_delete(months_to_keep: int = 6):
    "Uses TagoIO API for variable deletion"
    end_datetime = datetime.now() - timedelta(days=30 * months_to_keep)
    end_date = end_datetime.strftime("%Y-%m-") + "01"
    start_date = "2020-01-01"
    qty = 1000  # ? Otherwise the default is 15

    url = f"{base_url}{variable}&start_date={start_date}&end_date={end_date}&qty={qty}"
    headers = {
        "content-type": "application/json",
        "Device-Token": device_token,
    }

    with client:
        response = client.delete(url, headers=headers)
        print(response.json())
        # ? {'status': True, 'result': 'X Data Removed'}
        # X: [0, qty or result size of the request]


if __name__ == "__main__":
    test_variable_delete()
