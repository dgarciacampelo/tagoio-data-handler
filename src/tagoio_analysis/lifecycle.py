"""
This module is responsible for running all the TagoIO analysis related to OCPP
station management, through the use of the TagoIO IoT platform and its analysis
feature. Each analysis is a WebSocket connection that listens for changes in
the TagoIO platform and triggers the corresponding function in the backend to
manage the OCPP stations accordingly.
"""

import asyncio
from contextlib import asynccontextmanager
from fastapi import FastAPI
from loguru import logger

# Utilities and setup handlers
from data_handling import load_statuses_from_db
from schedule_utils import register_schedules, run_schedule_loop
from tagoio.pool_setup_fetching import init_pool_configs
from tagoio.token_fetching import get_all_devices_data

# Analysis callables & worker
from config import analysis_tokens
from tagoio_analysis.analysis_callable import (
    change_availability,
    change_cpo_info,
    change_load_balancing_mode,
    change_max_grid_power,
    change_rate_list,
    manage_rfid,
    power_consumption_update,
)
from tagoio_analysis.debug_ocpp_request import ocpp_requests
from tagoio_analysis.analysis_runner import TagoAnalysisWorker


@asynccontextmanager
async def application_lifespan(app: FastAPI):
    """
    Encapsulates all system operational startup initialization routines
    and background worker tasks inside Uvicorn's native loop context.
    """
    logger.info("Initializing baseline startup analysis configurations...")

    # 1. Direct synchronous/initial data preparations
    devices_data = get_all_devices_data()
    known_pools = list(devices_data.keys())
    load_statuses_from_db()
    register_schedules()

    # 2. Spawn core internal background loops
    schedule_task = asyncio.create_task(run_schedule_loop())
    pool_configs_task = asyncio.create_task(init_pool_configs(known_pools))

    # 3. Instantiate and cluster your TagoIO Analysis workers cooperatively
    workers = [
        TagoAnalysisWorker(analysis_tokens.change_availability_token, change_availability),
        TagoAnalysisWorker(analysis_tokens.change_max_grid_power_token, change_max_grid_power),
        TagoAnalysisWorker(analysis_tokens.manage_rfid_token, manage_rfid),
        TagoAnalysisWorker(analysis_tokens.change_cpo_info_token, change_cpo_info),
        TagoAnalysisWorker(analysis_tokens.change_rate_list_token, change_rate_list),
        TagoAnalysisWorker(analysis_tokens.change_dlb_mode_token, change_load_balancing_mode),
        TagoAnalysisWorker(analysis_tokens.ocpp_requests_token, ocpp_requests),
        TagoAnalysisWorker(analysis_tokens.power_consumption_update_token, power_consumption_update),
    ]

    # Start all analysis workers concurrently
    worker_tasks = [asyncio.create_task(worker.start()) for worker in workers]
    logger.info(f"Successfully mounted {len(worker_tasks)} real-time TagoIO analysis streams.")

    # All loops are running concurrently now
    yield  # FastAPI stays here handling incoming HTTP REST & SSE Stream Traffic

    # SHUTDOWN SEQUENCE
    logger.info("Server shutdown intercepted. Commencing graceful task terminations...")

    # 1. Cancel background loop routines
    schedule_task.cancel()
    pool_configs_task.cancel()

    # 2. Tell the workers to stop and disconnect websockets
    for worker in workers:
        try:
            await worker.stop()
        except Exception as e:
            logger.error(f"Error disconnecting worker: {e}")

    # 3. Cancel the remaining task handles
    for task in worker_tasks:
        task.cancel()

    # 4. Await everything to finalize cleanly using return_exceptions=True
    await asyncio.gather(schedule_task, pool_configs_task, *worker_tasks, return_exceptions=True)
    logger.info("Application context dissolved. All background systems down.")
