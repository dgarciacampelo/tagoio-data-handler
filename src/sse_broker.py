"""
This module implements a simple Server-Sent Events (SSE) broker for broadcasting
events to multiple connected clients (CSMS instances). These events are the
results of parsing the payloads received from TagoIO "Analysis", so this broker
works as an ACL: Anti-Corruption Layer between TagoIO and the CSMS instances.
The CSMS instances only have to listen to the events, filter the ones they are
interested in, and process them accordingly, independently of TagoIO platform.
"""

import asyncio
from typing import Any
from loguru import logger


class SSEBroker:
    def __init__(self):
        # Keeps track of all active CSMS connections
        self.subscribers: set[asyncio.Queue] = set()

    async def subscribe(self) -> asyncio.Queue:
        """Creates a new queue for a connecting CSMS instance."""
        queue = asyncio.Queue()
        self.subscribers.add(queue)
        logger.info(f"New SSE subscriber connected. Total active: {len(self.subscribers)}")
        return queue

    def unsubscribe(self, queue: asyncio.Queue):
        """Removes the queue when a CSMS disconnects."""
        self.subscribers.discard(queue)
        logger.info(f"SSE subscriber disconnected. Total active: {len(self.subscribers)}")

    async def broadcast(self, event_name: str, payload: dict[str, Any]):
        """Pushes an event to all connected CSMS instances."""
        if not self.subscribers:
            return  # No CSMS is listening, drop the event or log it

        message = {"event": event_name, "data": payload}

        # Fan-out the message to all active queues
        for queue in self.subscribers:
            try:
                queue.put_nowait(message)
            except asyncio.QueueFull:
                logger.warning("An SSE subscriber queue is full, dropping message.")


# Global singleton instance to be imported by routes
broker = SSEBroker()
