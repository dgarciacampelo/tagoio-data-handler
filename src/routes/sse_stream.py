import asyncio
import json
from fastapi import APIRouter, Request, Depends
from sse_starlette.sse import EventSourceResponse

from sse_broker import event_broker
from security import check_credentials

router = APIRouter()

@router.get("/api/stream", dependencies=[Depends(check_credentials)])
async def sse_event_stream(request: Request):
    """
    Endpoint for CSMS instances to connect and listen for events.
    Provides an ACL between TagoIO "Analysis" and the CSMS instances.
    """
    queue = await event_broker.subscribe()

    async def event_generator():
        try:
            while True:
                # 1. Check if the client disconnected gracefully
                if await request.is_disconnected():
                    break

                # 2. Wait for a new event from the broker (with a timeout to allow disconnection checks)
                try:
                    message = await asyncio.wait_for(queue.get(), timeout=2.0)
                    
                    # 3. Serialize the Python dict to a strict JSON string
                    yield {"data": json.dumps(message)}
                    
                except asyncio.TimeoutError:  # Timeout reached, loop restarts and checks `is_disconnected()` again
                    continue

        except asyncio.CancelledError:
            pass  # Triggered when the client forcefully closes the connection
        finally:
            event_broker.unsubscribe(queue)

    # ping=15 sends a keep-alive comment every 15 seconds to prevent load balancers from dropping the connection
    return EventSourceResponse(event_generator(), ping=15)