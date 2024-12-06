import httpx

from config import tago_account_token, tago_api_endpoint

# Default headers with account token
default_headers: dict[str, str] = {
    "content-type": "application/json",
    "Account-Token": tago_account_token,
}

# ! Default quantity is 20, can be passed as parameter
AMOUNT: int = 10000


def fix_filter(params: dict, filter: dict):
    "From TagoIO SDK, fixes filter for requests"
    q = {}
    for f in filter:
        if isinstance(filter[f], list):
            for i in range(len(filter[f])):
                q["filter[{}][{}][{}]".format(f, i, "key")] = filter[f][i]["key"]
                q["filter[{}][{}][{}]".format(f, i, "value")] = filter[f][i]["value"]
        else:
            q["filter[{}]".format(f, "value")] = filter[f]  # noqa: F523

    params.pop("filter", None)
    params.update(q)

    return params


def list_devices(
    page=1,
    fields=["id", "name"],
    filter={},
    amount=AMOUNT,
    orderBy="name,asc",
    resolveBucketName=False,
):
    params = {
        "page": page,
        "fields": fields,
        "amount": amount,
        "orderBy": orderBy,
        "resolveBucketName": resolveBucketName,
    }

    params = fix_filter(params, filter)
    url: str = f"{tago_api_endpoint}/device"
    return httpx.get(url, headers=default_headers, params=params).json()


def get_device_last_token(
    device_id,
    page=1,
    amount=AMOUNT,
    filter={},
    fields=["name", "token", "permission"],
    orderBy="created_at,desc",
) -> str:
    params = {
        "page": page,
        "amount": amount,
        "orderBy": orderBy,
        "fields": fields,
    }
    params = fix_filter(params, filter)
    url: str = f"{tago_api_endpoint}/device/token/{device_id}"
    request_json = httpx.get(url, headers=default_headers, params=params).json()
    if "result" not in request_json:
        return None

    # [{'name': 'Default', 'token': UUIDv4, 'permission': 'full', 'expire_time': 'never'}]
    token_dict = request_json["result"][-1]
    return token_dict["token"]
