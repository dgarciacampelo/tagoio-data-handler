"""TagoIO Analysis Runner, to integrate with the FastAPI Lifespan Handler"""

import asyncio
from typing import Any
from uuid import UUID
from loguru import logger
from socketio import AsyncClient

REALTIME_URL = "wss://realtime.tago.io"


class TagoAnalysisWorker:
    def __init__(self, token: UUID, callback):
        self.token = token
        self.callback = callback
        self.sio = AsyncClient(reconnection=True, reconnection_delay=5)
        self._is_running = False

    async def start(self):
        """Starts the Socket.IO client using FastAPI's existing event loop."""
        self._is_running = True

        # 1. Define your callback functions inside the scope
        def ready(analysis_obj):
            logger.info(f"·TagoIO· Analysis [{analysis_obj.get('name')}] connection online.")

        def error(e):
            logger.error(f"TagoIO connection error (Token: ...{str(self.token)[-6:]}): {e}")

        async def analysis_trigger(scope):
            # Pass execution payload directly down to your Pydantic parser/broker layer
            try:  # Mocking the legacy Tago 'context' object expected by old callbacks
                context = type("Context", (), {"token": scope["token"], "environment": scope["environment"]})()
                await self.callback(context, scope.get("data", []))
            except Exception as e:
                logger.error(f"Error executing analysis callback: {e}")

        # 2. Register them directly by passing the function as the second argument
        self.sio.on("ready", ready)
        self.sio.on("error", error)
        self.sio.on("analysis::trigger", analysis_trigger)

        try:
            url = f"{REALTIME_URL}?token={self.token}"
            await self.sio.connect(url=url, transports=["websocket"])
            # Instead of sio.wait() blocking the thread, we keep it alive cooperatively
            while self._is_running:
                await asyncio.sleep(1)
        except asyncio.CancelledError:
            logger.info(f"Analysis worker for token ...{str(self.token)[-6:]} was cancelled cleanly.")
        except Exception as e:
            logger.error(f"Failed running TagoIO worker loop: {e}")
        finally:
            await self.stop()

    async def stop(self):
        """Clean disconnection sequence."""
        self._is_running = False
        if self.sio.connected:
            await self.sio.disconnect()


def clean_comma_separated_list(value: Any) -> list[str]:
    """Helper to split and clean up comma-separated strings."""
    if not value:
        return []

    return [item.strip() for item in str(value).split(",") if item.strip()]
