from config import test_pool_code
from test_charging_session_update import update  # noqa: F401
from database.query_database import (
    get_database_tagoio_devices_count,
    get_database_charging_session_history_count,
    get_all_database_tagoio_devices,
    get_database_tagoio_device,
    insert_database_tagoio_device,
    insert_database_charging_session_history,
    update_database_tagoio_device,
    delete_database_tagoio_device,
)


def test_get_all_database_tagoio_devices():
    "Tests the function to get all devices from the database"
    devices = get_all_database_tagoio_devices()
    devices_count = get_database_tagoio_devices_count()
    print("devices count:", devices_count)
    assert len(devices) >= 0 and len(devices) == devices_count


def test_get_database_tagoio_device(pool_code: int = test_pool_code):
    "Tests the function to get a device from the database"
    device_id, device_token = get_database_tagoio_device(pool_code)
    assert device_id is not None and device_token is not None


def test_insert_database_tagoio_device(
    pool_code: int = 1, device_id="ABCD", device_token="1234"
):
    "Tests the function to insert a device into the database"
    devices_count_before = get_database_tagoio_devices_count()
    insert_database_tagoio_device(pool_code, device_id, device_token)
    devices_count_after = get_database_tagoio_devices_count()
    assert devices_count_after == devices_count_before + 1


def test_insert_database_charging_session_history():
    "Tests the function to insert a charging session into the database"
    charging_sessions_count_before = get_database_charging_session_history_count()
    insert_database_charging_session_history(update)
    charging_sessions_count_after = get_database_charging_session_history_count()
    assert charging_sessions_count_after == charging_sessions_count_before + 1


def test_update_database_tagoio_device(
    pool_code: int = 1, device_id="EFGH", device_token="5678"
):
    "Tests the function to update a device in the database"
    update_database_tagoio_device(pool_code, device_id, device_token)
    test_device = get_database_tagoio_device(pool_code)
    assert test_device[0] == device_id and test_device[1] == device_token


def test_delete_database_tagoio_device(pool_code: int = 1):
    "Tests the function to delete a device from the database"
    devices_count_before = get_database_tagoio_devices_count()
    delete_database_tagoio_device(pool_code)
    devices_count_after = get_database_tagoio_devices_count()
    assert devices_count_after == devices_count_before - 1
