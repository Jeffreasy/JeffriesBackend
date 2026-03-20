"""
Devices endpoint — WiZ Local UDP for direct lamp control.
Device metadata is stored in Convex (cloud-persistent).
Only UDP commands and registration verification happen here (require LAN access).
"""
import colorsys
import logging
from fastapi import APIRouter, HTTPException, status
import httpx

from app.core.config import settings
from app.schemas.schemas import DeviceCommandRequest, DeviceRegisterRequest
from app.services.wiz.service import WizService

logger = logging.getLogger(__name__)
router = APIRouter()
wiz = WizService()


# ─── Convex helpers ───────────────────────────────────────────────────────────

def _convex_headers() -> dict:
    return {
        "Authorization": f"Bearer {settings.HOMEAPP_GAS_SECRET}",
        "Content-Type": "application/json",
    }


def _convex_url(path: str) -> str:
    return f"{settings.CONVEX_SITE_URL}{path}"


def _map_device(doc: dict) -> dict:
    """Map a Convex device doc to the DeviceResponse shape."""
    state = doc.get("currentState", {})
    return {
        "id":             doc["_id"],
        "name":           doc.get("name", ""),
        "device_type":    doc.get("deviceType", "color_light"),
        "room_id":        doc.get("roomId"),
        "ip_address":     doc.get("ipAddress"),
        "mac_address":    None,
        "current_state":  {
            "on":          state.get("on", False),
            "brightness":  state.get("brightness", 100),
            "color_temp":  state.get("color_temp", 4000),
            "r":           state.get("r", 0),
            "g":           state.get("g", 0),
            "b":           state.get("b", 0),
        },
        "status":         doc.get("status", "offline"),
        "last_seen":      doc.get("lastSeen"),
        "commissioned_at": doc.get("commissionedAt", ""),
        "manufacturer":   doc.get("manufacturer"),
        "model":          doc.get("model"),
    }


# ─── CRUD (proxied to Convex) ─────────────────────────────────────────────────

@router.get("")
async def list_devices(skip: int = 0, limit: int = 100):
    """Return registered devices from Convex."""
    async with httpx.AsyncClient(timeout=10) as client:
        r = await client.get(
            _convex_url("/devices"),
            headers=_convex_headers(),
            params={"userId": settings.HOMEAPP_USER_ID},
        )
        r.raise_for_status()
    docs = r.json().get("devices", [])
    devices = [_map_device(d) for d in docs]
    return devices[skip: skip + limit] if limit else devices[skip:]


@router.get("/{device_id}")
async def get_device(device_id: str):
    async with httpx.AsyncClient(timeout=10) as client:
        r = await client.get(
            _convex_url(f"/devices/{device_id}"),
            headers=_convex_headers(),
        )
    if r.status_code == 404:
        raise HTTPException(status_code=404, detail="Device not found")
    r.raise_for_status()
    return _map_device(r.json()["device"])


@router.patch("/{device_id}")
async def update_device(device_id: str, data: dict):
    payload = {}
    if "name" in data:       payload["name"]      = data["name"]
    if "room_id" in data:    payload["roomId"]    = data["room_id"]
    if "ip_address" in data:
        new_ip = data["ip_address"]
        # Verify new IP is reachable
        state = await wiz.get_state(new_ip)
        if state is None:
            raise HTTPException(status_code=502, detail=f"WiZ lamp op {new_ip} niet bereikbaar.")
        payload["ipAddress"] = new_ip

    async with httpx.AsyncClient(timeout=10) as client:
        r = await client.patch(
            _convex_url(f"/devices/{device_id}"),
            headers=_convex_headers(),
            json=payload,
        )
    if r.status_code == 404:
        raise HTTPException(status_code=404, detail="Device not found")
    r.raise_for_status()
    return _map_device(r.json()["device"])


@router.delete("/{device_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_device(device_id: str):
    async with httpx.AsyncClient(timeout=10) as client:
        r = await client.delete(
            _convex_url(f"/devices/{device_id}"),
            headers=_convex_headers(),
        )
    if r.status_code == 404:
        raise HTTPException(status_code=404, detail="Device not found")
    r.raise_for_status()


# ─── Registration ─────────────────────────────────────────────────────────────

@router.post("/register", status_code=status.HTTP_201_CREATED)
async def register_device(data: DeviceRegisterRequest):
    """
    Register a WiZ bulb: verifies reachability via UDP, then stores in Convex.
    """
    state = await wiz.get_state(data.ip_address)
    if state is None:
        raise HTTPException(
            status_code=502,
            detail=f"Cannot reach WiZ bulb at {data.ip_address}:38899.",
        )

    payload = {
        "userId":       settings.HOMEAPP_USER_ID,
        "name":         data.name,
        "ipAddress":    data.ip_address,
        "deviceType":   "color_light",
        "roomId":       str(data.room_id) if data.room_id else None,
        "manufacturer": "WiZ",
        "model":        "GU10 Color",
        "currentState": {
            "on":         state.on,
            "brightness": state.brightness,
            "color_temp": state.color_temp,
            "r":          state.r,
            "g":          state.g,
            "b":          state.b,
        },
    }

    async with httpx.AsyncClient(timeout=10) as client:
        r = await client.post(
            _convex_url("/devices/create"),
            headers=_convex_headers(),
            json=payload,
        )

    if r.status_code == 409:
        raise HTTPException(status_code=409, detail=r.json().get("error", "Al geregistreerd"))
    r.raise_for_status()

    logger.info(f"Registered WiZ bulb '{data.name}' at {data.ip_address} in Convex")
    return _map_device(r.json()["device"])


# ─── Device Control ───────────────────────────────────────────────────────────

@router.post("/{device_id}/command", status_code=status.HTTP_204_NO_CONTENT)
async def send_command(device_id: str, cmd: DeviceCommandRequest):
    """
    Send a control command to a WiZ bulb via local UDP.
    Device IP is looked up from Convex. State is stored back in Convex after command.
    """
    # 1. Get device IP from Convex
    async with httpx.AsyncClient(timeout=10) as client:
        r = await client.get(
            _convex_url(f"/devices/{device_id}"),
            headers=_convex_headers(),
        )
    if r.status_code == 404:
        raise HTTPException(status_code=404, detail="Device not found")
    r.raise_for_status()
    doc = r.json()["device"]
    ip = doc.get("ipAddress")
    if not ip:
        raise HTTPException(status_code=422, detail="Device heeft geen IP-adres.")

    # 2. Build UDP kwargs + state patch
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

    # 3. Execute UDP command
    try:
        if cmd.scene_id is not None:
            await wiz.set_scene(ip, cmd.scene_id)
            state_patch["on"] = True
        elif kwargs:
            await wiz.set_state(ip, **kwargs)
    except Exception as e:
        logger.error(f"WiZ command failed for device {device_id} ({ip}): {e}")
        raise HTTPException(status_code=502, detail=f"WiZ command failed: {e}")

    # 4. Update state in Convex
    if state_patch:
        try:
            async with httpx.AsyncClient(timeout=5) as client:
                await client.patch(
                    _convex_url(f"/devices/{device_id}/state"),
                    headers=_convex_headers(),
                    json=state_patch,
                )
        except Exception as e:
            logger.warning(f"Convex state update failed for {device_id}: {e}")
