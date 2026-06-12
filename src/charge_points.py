# Tools to manage charge points

# ? Known charge points as (station_name, connector_id) for each pool_code
known_charge_points: dict[int, set[tuple[str, int]]] = dict()


def register_charge_point(pool_code: int, station_name: str, connector_id: int):
    """Registers a new charge point in the known charge points"""
    if pool_code not in known_charge_points:
        known_charge_points[pool_code] = set()

    known_charge_points[pool_code].add((station_name, connector_id))


def unregister_charge_point(pool_code: int, station_name: str):
    """Removes a station and all its known connectors from memory."""
    if pool_code in known_charge_points:
        # ! Create a list of tuples to remove (avoiding modifying the set during iteration)
        to_remove = [(s_name, cid) for s_name, cid in known_charge_points[pool_code] if s_name == station_name]
        for item in to_remove:
            known_charge_points[pool_code].remove(item)


def get_pool_known_charge_points(pool_code: int) -> set[tuple[str, int]]:
    """Returns the known charge points for a pool, by pool code"""
    return known_charge_points.get(pool_code, set())


def get_all_known_charge_points() -> dict[int, set[tuple[str, int]]]:
    """Returns the known charge points for all pools"""
    return known_charge_points


def get_charge_point_alias(station_name: str, connector_id: int) -> str:
    return f"{station_name}/{connector_id}"


def get_pool_known_charge_point_aliases(pool_code: int) -> list[str]:
    """Returns aliases of the known charge points for a pool, by pool code"""
    pool_charge_points = get_pool_known_charge_points(pool_code)
    return [get_charge_point_alias(name, cid) for name, cid in pool_charge_points]


def get_all_known_charge_point_aliases() -> dict[int, list[str]]:
    """Returns aliases of the known charge points for all pools"""
    return {pool_code: get_pool_known_charge_point_aliases(pool_code) for pool_code in known_charge_points}
