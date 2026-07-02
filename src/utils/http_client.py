"""
This module provides a global HTTP client using httpx.AsyncClient for
efficient connection pooling and reuse across the application.
"""

from typing import Optional

import httpx
from loguru import logger


class GlobalHTTPClient:
    _async_client: Optional[httpx.AsyncClient] = None
    _sync_client: Optional[httpx.Client] = None

    @classmethod
    def get_client(cls) -> httpx.AsyncClient:
        """Returns the shared httpx.AsyncClient instance. Initializes it if needed."""
        if cls._async_client is None:  # Setting default limits, timeouts, and standard headers...
            limits = httpx.Limits(max_keepalive_connections=50, max_connections=100)
            timeout = httpx.Timeout(timeout=10.0, connect=5.0)
            cls._async_client = httpx.AsyncClient(limits=limits, timeout=timeout)
            logger.info("Global HTTPX AsyncClient initialized.")
        return cls._async_client

    @classmethod
    def get_blocking_client(cls) -> httpx.Client:
        """Returns the shared httpx.Client instance. Initializes it if needed."""
        if cls._sync_client is None:  # Setting default limits, timeouts, and standard headers...
            limits = httpx.Limits(max_keepalive_connections=50, max_connections=100)
            timeout = httpx.Timeout(timeout=10.0, connect=5.0)
            cls._sync_client = httpx.Client(limits=limits, timeout=timeout)
            logger.info("Global HTTPX Client initialized.")
        return cls._sync_client

    @classmethod
    async def close(cls):
        """Gracefully closes the HTTPX client and its connection pools."""
        if cls._async_client is not None:
            await cls._async_client.aclose()
            cls._async_client = None
            logger.info("Global HTTPX AsyncClient closed.")

        if cls._sync_client is not None:
            cls._sync_client.close()
            cls._sync_client = None
            logger.info("Global HTTPX Client closed.")
