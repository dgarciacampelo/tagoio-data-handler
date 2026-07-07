from fastapi import APIRouter, BackgroundTasks, Depends
from loguru import logger

from database.query_database import insert_database_tagoio_device
from schemas.google_forms import CPOProvisioningResponse, GoogleFormPayload
from security import check_credentials
from tagoio.device_management import create_new_tago_device
from tagoio.provisioning import (
    create_tagoio_user,
    generate_secure_password,
    resolve_new_pool_code,
    setup_default_device_variables,
)

router = APIRouter()


@router.post("/api/provision-cpo", response_model=CPOProvisioningResponse, dependencies=[Depends(check_credentials)])
async def provision_new_cpo(payload: GoogleFormPayload, background_tasks: BackgroundTasks):
    """
    Receives CPO data, provisions the user and device in TagoIO,
    and returns the status and generated credentials.
    """
    # 1. Resolve the new pool code
    pool_code = await resolve_new_pool_code()

    password = generate_secure_password()
    device_name = f"MASTER-BUSINESS-{pool_code}"

    response = CPOProvisioningResponse(
        user_created=False, device_created=False, pool_code=pool_code, generated_password=password, form_data=payload
    )

    # 2. Create the User in TagoIO
    user_success = await create_tagoio_user(payload, pool_code, password)
    response.user_created = user_success

    if not user_success:
        response.error_message = "Failed to create TagoIO user. Aborting device creation."
        logger.error(f"User creation failed for {payload.user_email}")
        return response

    # 3. Create the Device using the logic ported from the Notification Management Service
    advanced_plan = "TPV" in payload.payment_system

    device_id, device_token = await create_new_tago_device(
        name=device_name, device_type="MASTER", installation="BUSINESS", advanced_plan=advanced_plan
    )

    if device_id and device_token:
        response.device_created = True
        response.device_id = device_id

        # 4. Save to local SQLite so the Handler can manage this pool immediately
        insert_database_tagoio_device(pool_code, device_id, device_token)

        # 5. Apply default variables
        # ! await setup_default_device_variables(pool_code, payload)
        background_tasks.add_task(setup_default_device_variables, pool_code, payload)
        logger.info(f"Successfully provisioned CPO {payload.company_name} at Pool {pool_code}")
    else:
        response.error_message = "User created, but device provisioning failed."
        logger.error(f"Device creation failed for Pool {pool_code}")

    return response
