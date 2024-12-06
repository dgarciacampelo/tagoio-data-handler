es_dictionary: dict[str, str] = {
    "Available": "Disponible",
    "Preparing": "Preparando",
    "Charging": "Cargando",
    "SuspendedEVSE": "Suspendido (cargador)",
    "SuspendedEV": "Suspendido (coche)",
    "Finishing": "Finalizando carga",
    "Reserved": "Reservado",
    "Unavailable": "No disponible",
    "Faulted": "Error",
    "Booting": "Conectando",
    "Offline": "Desconectado",
    "Online": "Conectado",
}

en_dictionary: dict[str, str] = {
    "Available": "Available",
    "Preparing": "Preparing",
    "Charging": "Charging",
    "SuspendedEVSE": "Suspended (charger)",
    "SuspendedEV": "Suspended (car)",
    "Finishing": "Finishing charge",
    "Reserved": "Reserved",
    "Unavailable": "Unavailable",
    "Faulted": "Faulted",
    "Booting": "Booting",
    "Offline": "Offline",
    "Online": "Online",
}


def choose_language_dictionary(language: str) -> tuple[dict[str, str], str]:
    "Returns the dictionary corresponding to the language passed as argument"
    if language == "en":
        return en_dictionary, "Unknown"

    return es_dictionary, "Desconocido"


def translate_status(status: str, connection: str, language: str = "es") -> str:
    """
    Transalates charge point status from english to the dictionary language
    passed as argument (default is Spanish).
    """
    dictionary, fallback_status = choose_language_dictionary(language)

    if connection == "Booting" or connection == "Offline":
        return dictionary.get(connection, fallback_status)

    return dictionary.get(status, fallback_status)
