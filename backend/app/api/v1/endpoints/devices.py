"""
Devices endpoint — WiZ Local UDP for direct lamp control.
Devices are registered by LAN IP address, no Matter/BLE commissioning needed.
"""
import colorsys
import uuid
import logging
from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.db.session import get_db
from app.db.repositories.device_repository import DeviceRepository
from app.schemas.schemas import DeviceUpdate, DeviceResponse, DeviceCommandRequest, DeviceRegisterRequest
from app.services.wiz.service import WizService
from app.api.security import require_api_key

logger = logging.getLogger(__name__)
router = APIRouter()

wiz = WizService()


# ─── CRUD ─────────────────────────────────────────────────────────────────────

@router.get("", response_model=list[DeviceResponse])
async def list_devices(
    skip: int = 0,
    limit: int = 100,
    db: AsyncSession = Depends(get_db),
):
    """Return registered devices. Use skip/limit for pagination."""
    return await DeviceRepository(db).get_all(skip=skip, limit=limit)


@router.get("/discover", response_model=list[dict])
async def discover_devices():
    """
    Broadcast a UDP ping on the LAN to find WiZ bulbs.

    Note: requires the API to run on a host with direct Wi-Fi LAN access.
    Inside Docker on Windows this may not return results — run the scan
    from the Windows host instead (see README).
    """
    return await wiz.discover(timeout=3.0)


@router.get("/{device_id}", response_model=DeviceResponse)
async def get_device(device_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    repo = DeviceRepository(db)
    device = await repo.get_by_id(device_id)
    if not device:
        raise HTTPException(status_code=404, detail="Device not found")
    return device


@router.patch("/{device_id}", response_model=DeviceResponse, dependencies=[Depends(require_api_key)])
async def update_device(device_id: uuid.UUID, data: DeviceUpdate, db: AsyncSession = Depends(get_db)):
    repo = DeviceRepository(db)
    device = await repo.get_by_id(device_id)
    if not device:
        raise HTTPException(status_code=404, detail="Device not found")

    update_data = data.model_dump(exclude_none=True)

    # ── IP-adres wijziging: verifieer bereikbaarheid first ────────────────────
    if "ip_address" in update_data and update_data["ip_address"] != device.ip_address:
        new_ip = update_data["ip_address"]

        # Check duplicate
        existing = await repo.get_by_ip(new_ip)
        if existing and existing.id != device_id:
            raise HTTPException(
                status_code=409,
                detail=f"IP {new_ip} is al in gebruik door '{existing.name}'",
            )

        # Ping-verificatie
        state = await wiz.get_state(new_ip)
        if state is None:
            raise HTTPException(
                status_code=502,
                detail=f"WiZ lamp op {new_ip} niet bereikbaar. Controleer het IP-adres.",
            )

        # Sync live state
        update_data["status"] = "online"
        logger.info(f"IP bijgewerkt: '{device.name}' {device.ip_address} → {new_ip}")

    result = await repo.update(device_id, **update_data)
    if not result:
        raise HTTPException(status_code=404, detail="Device not found")
    return result



@router.delete("/{device_id}", status_code=status.HTTP_204_NO_CONTENT,
               dependencies=[Depends(require_api_key)])
async def delete_device(device_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    repo = DeviceRepository(db)
    if not await repo.delete(device_id):
        raise HTTPException(status_code=404, detail="Device not found")


# ─── Registration ─────────────────────────────────────────────────────────────

@router.post("/register", response_model=DeviceResponse, status_code=status.HTTP_201_CREATED,
             dependencies=[Depends(require_api_key)])
async def register_device(data: DeviceRegisterRequest, db: AsyncSession = Depends(get_db)):
    """
    Add a WiZ bulb to the system by its LAN IP address.

    The endpoint pings the bulb via UDP to verify it's reachable and syncs
    its current state (on/off, brightness, color temp) before saving to DB.

    **Finding the IP:**
    - Run `GET /api/v1/devices/discover` (requires host network access)
    - Or check your router's DHCP table for 'Espressif' / 'WiZ' devices
    """
    state = await wiz.get_state(data.ip_address)
    if state is None:
        raise HTTPException(
            status_code=502,
            detail=(
                f"Cannot reach WiZ bulb at {data.ip_address}:38899. "
                "Check: (1) IP is correct, (2) bulb is powered on, "
                "(3) host and bulb are on the same Wi-Fi network."
            ),
        )

    repo = DeviceRepository(db)

    existing = await repo.get_by_ip(data.ip_address)
    if existing:
        raise HTTPException(
            status_code=409,
            detail=f"Device with IP {data.ip_address} already exists (id={existing.id})",
        )

    device = await repo.create(
        ip_address=data.ip_address,
        name=data.name,
        device_type="color_light",
        room_id=data.room_id,
        manufacturer="WiZ",
        model="GU10 Color",
    )

    await repo.update_state(device.id, {
        "on": state.on,
        "brightness": state.brightness,
        "color_temp": state.color_temp,
        "r": state.r,
        "g": state.g,
        "b": state.b,
    })
    logger.info(f"Registered WiZ bulb '{data.name}' at {data.ip_address}")
    return await repo.get_by_id(device.id)


# ─── Device Control ───────────────────────────────────────────────────────────

@router.post("/{device_id}/command", status_code=status.HTTP_204_NO_CONTENT)
async def send_command(
    device_id: uuid.UUID,
    cmd: DeviceCommandRequest,
    db: AsyncSession = Depends(get_db),
):
    """
    Send a control command to a WiZ bulb via local UDP.
    All fields are optional — include only what you want to change.

    | Field | Range | Notes |
    |---|---|---|
    | `on` | bool | Turn on/off |
    | `brightness` | 10–100 | Percent |
    | `color_temp_mireds` | 153–500 | Auto-converted to Kelvin |
    | `r` / `g` / `b` | 0–255 | Direct RGB |
    | `hue` / `saturation` | 0–254 | Converted to RGB internally |
    """
    repo = DeviceRepository(db)
    device = await repo.get_by_id(device_id)
    if not device:
        raise HTTPException(status_code=404, detail="Device not found")

    # Use the dedicated ip_address column — not current_state JSONB
    ip = device.ip_address
    if not ip:
        raise HTTPException(
            status_code=422,
            detail="Device has no IP address. Re-register with POST /api/v1/devices/register.",
        )

    kwargs: dict = {}
    state_patch: dict = {}

    if cmd.on is not None:
        kwargs["on"] = cmd.on
        state_patch["on"] = cmd.on

    if cmd.brightness is not None:
        kwargs["brightness"] = cmd.brightness
        state_patch["brightness"] = cmd.brightness

    if cmd.color_temp_mireds is not None:
        kelvin = round(1_000_000 / cmd.color_temp_mireds)
        kwargs["color_temp"] = kelvin
        state_patch["color_temp"] = kelvin

    if cmd.hue is not None and cmd.saturation is not None:
        r_val, g_val, b_val = colorsys.hsv_to_rgb(cmd.hue / 254, cmd.saturation / 254, 1.0)
        kwargs["r"] = round(r_val * 255)
        kwargs["g"] = round(g_val * 255)
        kwargs["b"] = round(b_val * 255)
        state_patch.update({"r": kwargs["r"], "g": kwargs["g"], "b": kwargs["b"]})
    elif cmd.r is not None or cmd.g is not None or cmd.b is not None:
        kwargs["r"] = cmd.r or 0
        kwargs["g"] = cmd.g or 0
        kwargs["b"] = cmd.b or 0
        state_patch.update({"r": kwargs["r"], "g": kwargs["g"], "b": kwargs["b"]})

    try:
        if cmd.scene_id is not None:
            # WiZ native scene: bypasses set_state kwargs, uses setPilot{sceneId}
            await wiz.set_scene(ip, cmd.scene_id)
            state_patch["on"] = True
        elif kwargs:
            await wiz.set_state(ip, **kwargs)
    except Exception as e:
        logger.error(f"WiZ command failed for device {device_id} ({ip}): {e}")
        raise HTTPException(status_code=502, detail=f"WiZ command failed: {e}")

    if state_patch:
        await repo.update_state(device_id, state_patch)
