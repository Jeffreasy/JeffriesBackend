"""
WizService — Local UDP control for WiZ smart bulbs.

WiZ bulbs respond on UDP port 38899 to JSON pilot commands.
No cloud, no BLE, no Matter commissioning needed — direct LAN control.

Protocol docs: https://github.com/sbidy/pywizlight
"""
from __future__ import annotations
import asyncio
import json
import logging
import socket
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)

WIZ_UDP_PORT = 38899
WIZ_BROADCAST = "255.255.255.255"
UDP_TIMEOUT = 3.0  # seconds


@dataclass
class WizState:
    """Parsed state returned by WiZ getPilot response."""
    on: bool = False
    brightness: int = 100        # 10–100 (WiZ uses %, not 0-254)
    color_temp: int = 4000       # Kelvin (2200–6500)
    r: int = 0
    g: int = 0
    b: int = 0
    speed: int = 100
    scene_id: int = 0
    raw: dict = field(default_factory=dict)

    @classmethod
    def from_response(cls, data: dict) -> "WizState":
        p = data.get("result", data.get("params", {}))
        return cls(
            on=p.get("state", False),
            brightness=p.get("dimming", 100),
            color_temp=p.get("temp", 4000),
            r=p.get("r", 0),
            g=p.get("g", 0),
            b=p.get("b", 0),
            speed=p.get("speed", 100),
            scene_id=p.get("sceneId", 0),
            raw=p,
        )


class WizService:
    """Async local UDP controller for WiZ bulbs."""

    # ──────────────────────────────────────────────────────────────
    # Low-level UDP
    # ──────────────────────────────────────────────────────────────

    async def send_command(self, ip: str, method: str, params: dict | None = None) -> dict | None:
        """Send a UDP command to a WiZ bulb and await its response."""
        payload = {"method": method, "params": params or {}}
        message = json.dumps(payload).encode()

        loop = asyncio.get_running_loop()
        future: asyncio.Future = loop.create_future()

        def _protocol_factory():
            return _UdpProtocol(future)

        transport, _ = await loop.create_datagram_endpoint(
            _protocol_factory,
            remote_addr=(ip, WIZ_UDP_PORT),
        )
        transport.sendto(message)

        try:
            data = await asyncio.wait_for(asyncio.shield(future), timeout=UDP_TIMEOUT)
            return json.loads(data)
        except asyncio.TimeoutError:
            logger.warning(f"WiZ bulb {ip} did not respond in {UDP_TIMEOUT}s")
            return None
        finally:
            transport.close()

    async def get_state(self, ip: str) -> WizState | None:
        """Read current bulb state (on/off, brightness, CT, RGB)."""
        resp = await self.send_command(ip, "getPilot")
        if resp:
            return WizState.from_response(resp)
        return None

    # ──────────────────────────────────────────────────────────────
    # High-level Control Commands
    # ──────────────────────────────────────────────────────────────

    async def turn_on(self, ip: str) -> None:
        await self.send_command(ip, "setPilot", {"state": True})

    async def turn_off(self, ip: str) -> None:
        await self.send_command(ip, "setPilot", {"state": False})

    async def set_brightness(self, ip: str, brightness_pct: int) -> None:
        """brightness_pct: 10–100 (WiZ native %). Clipped to valid range."""
        dimming = max(10, min(100, brightness_pct))
        await self.send_command(ip, "setPilot", {"state": True, "dimming": dimming})

    async def set_color_temp(self, ip: str, kelvin: int) -> None:
        """
        Set white color temperature.
        WiZ GU10 range: 2200 (warm) – 6500 (cool) Kelvin.
        Accepts mireds too — auto-converts if value < 100.
        """
        if kelvin < 100:  # treat as mireds
            kelvin = round(1_000_000 / kelvin)
        kelvin = max(2200, min(6500, kelvin))
        await self.send_command(ip, "setPilot", {"state": True, "temp": kelvin})

    async def set_color(self, ip: str, r: int, g: int, b: int) -> None:
        """Full RGB color control (0–255 each channel)."""
        await self.send_command(ip, "setPilot", {
            "state": True,
            "r": r, "g": g, "b": b,
            "dimming": 100,
        })

    async def set_scene(self, ip: str, scene_id: int) -> None:
        """Activate a WiZ preset scene (1–32)."""
        await self.send_command(ip, "setPilot", {"sceneId": scene_id})

    async def set_state(self, ip: str, **kwargs) -> None:
        """
        Generic state setter — pass any combination:
        on, brightness (10-100), color_temp (K), r, g, b
        """
        params: dict[str, Any] = {}

        if "on" in kwargs:
            params["state"] = kwargs["on"]
        if "brightness" in kwargs:
            params["dimming"] = max(10, min(100, kwargs["brightness"]))
        if "color_temp" in kwargs:
            params["temp"] = max(2200, min(6500, kwargs["color_temp"]))
        if all(k in kwargs for k in ("r", "g", "b")):
            params["r"] = kwargs["r"]
            params["g"] = kwargs["g"]
            params["b"] = kwargs["b"]

        if params:
            params.setdefault("state", True)
            await self.send_command(ip, "setPilot", params)

    # ──────────────────────────────────────────────────────────────
    # Discovery (broadcast ping)
    # ──────────────────────────────────────────────────────────────

    async def discover(self, timeout: float = 3.0) -> list[dict]:
        """
        Broadcast a registration ping and collect responses from all WiZ
        bulbs on the LAN. Returns list of {ip, mac, ...} dicts.

        Note: requires UDP broadcast to work from the Docker container network.
        """
        found: list[dict] = []
        loop = asyncio.get_running_loop()
        done = loop.create_future()

        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.setblocking(False)

        transport, protocol = await loop.create_datagram_endpoint(
            lambda: _BroadcastProtocol(found),
            sock=sock,
        )

        payload = json.dumps({"method": "registration", "params": {
            "phoneIp": "0.0.0.0",
            "register": True,
            "phoneMac": "aabbccddeeff",
        }}).encode()
        transport.sendto(payload, (WIZ_BROADCAST, WIZ_UDP_PORT))

        await asyncio.sleep(timeout)
        transport.close()
        return found


# ──────────────────────────────────────────────────────────────────────────────
# Internal asyncio UDP protocol helpers
# ──────────────────────────────────────────────────────────────────────────────

class _UdpProtocol(asyncio.DatagramProtocol):
    def __init__(self, future: asyncio.Future):
        self._future = future

    def datagram_received(self, data: bytes, addr):
        if not self._future.done():
            self._future.set_result(data)

    def error_received(self, exc):
        if not self._future.done():
            self._future.set_exception(exc)


class _BroadcastProtocol(asyncio.DatagramProtocol):
    def __init__(self, results: list):
        self._results = results

    def datagram_received(self, data: bytes, addr):
        try:
            parsed = json.loads(data)
            parsed["_ip"] = addr[0]
            self._results.append(parsed)
        except Exception:
            pass
