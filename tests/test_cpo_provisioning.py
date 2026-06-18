import pytest
from unittest.mock import patch
from datetime import datetime

from src.tagoio.provisioning import resolve_new_pool_code


@pytest.mark.asyncio
@patch("src.tagoio.provisioning.get_device_list")
async def test_resolve_new_pool_code_with_existing_devices(mock_get_device_list):
    """Tests that the sequence increments correctly based on the highest current year sequence."""
    current_year = datetime.now().strftime("%y")

    # Mock the return value of get_device_list to simulate TagoIO's response
    mock_get_device_list.return_value = [
        {"name": f"MASTER-BUSINESS-{current_year}1001"},
        {"name": f"MASTER-BUSINESS-{current_year}1003"},  # Highest valid sequence
        {"name": f"MASTER-BUSINESS-{current_year}1002"},
        {"name": "MASTER-BUSINESS-251099"},  # Ignored: previous year
        {"name": "MASTER-BUSINESS-INVALID"},  # Ignored: malformed suffix
        {"name": "OTHER-DEVICE-12345"},  # Ignored: wrong prefix
    ]

    result = await resolve_new_pool_code()

    # If the highest sequence is 1003, the next should be 1004 appended to the current year
    expected_code = int(f"{current_year}1004")
    assert result == expected_code


@pytest.mark.asyncio
@patch("src.tagoio.provisioning.get_device_list")
async def test_resolve_new_pool_code_new_year_or_empty(mock_get_device_list):
    """Tests that a new year sequence starts at 1001."""
    current_year = datetime.now().strftime("%y")

    # Simulate a scenario where there are NO devices for the current year
    mock_get_device_list.return_value = [
        {"name": "MASTER-BUSINESS-251050"},
        {"name": "MASTER-BUSINESS-251051"},
        {"name": "PAYMENTS-GATEWAY-DEVICE"},
    ]

    result = await resolve_new_pool_code()

    # Should default to max_sequence (1000) + 1
    expected_code = int(f"{current_year}1001")
    assert result == expected_code
