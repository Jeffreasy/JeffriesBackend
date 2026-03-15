"""
MatterClient — WebSocket RPC client for python-matter-server.
Acts as the single connection bridging FastAPI and the Matter network.
"""
from __future__ import annotations
import asyncio
import json
import logging
import uuid
from typing import Any, Callable, Coroutine

import websockets
from websockets.exceptions import ConnectionClosed

from app.core.config import settings

logger = logging.getLogger(__name__)


class MatterClient:
    def __init__(self, url: str = settings.MATTER_SERVER_URL):
        self.url = url
        self._ws: websockets.ClientConnection | None = None
        self._pending: dict[str, asyncio.Future] = {}
        self._event_handlers: list[Callable[[dict], Coroutine]] = []
        self._connected = False

    async def connect(self) -> None:
        self._ws = await websockets.connect(self.url, ping_interval=30)
        self._connected = True
        asyncio.create_task(self._listen())
        logger.info(f"Connected to Matter Server at {self.url}")

    async def disconnect(self) -> None:
        self._connected = False
        if self._ws:
            await self._ws.close()
        logger.info("Disconnected from Matter Server")

    # ──────────────────────────────────────────────────────────────
    # Core RPC
    # ──────────────────────────────────────────────────────────────

    async def send_command(self, command: str, args: dict | None = None) -> Any:
        """Send a WebSocket RPC command and await its response."""
        if not self._ws or not self._connected:
            raise RuntimeError("MatterClient is not connected")

        msg_id = str(uuid.uuid4())
        future: asyncio.Future = asyncio.get_running_loop().create_future()
        self._pending[msg_id] = future

        payload = {"message_id": msg_id, "command": command}
        if args:
            payload["args"] = args

        await self._ws.send(json.dumps(payload))
        result = await asyncio.wait_for(future, timeout=15.0)
        return result

    async def _listen(self) -> None:
        """Background task — routes incoming WS messages to futures or event handlers."""
        try:
            async for raw in self._ws:
                data: dict = json.loads(raw)
                msg_id = data.get("message_id")

                if msg_id and msg_id in self._pending:
                    self._pending.pop(msg_id).set_result(data.get("result"))
                else:
                    # Unsolicited event (e.g. device attribute changed)
                    await self._dispatch_event(data)
        except ConnectionClosed:
            logger.warning("Matter Server WebSocket connection closed")
            self._connected = False

    async def _dispatch_event(self, event: dict) -> None:
        for handler in self._event_handlers:
            try:
                await handler(event)
            except Exception as e:
                logger.error(f"Event handler error: {e}")

    def on_event(self, handler: Callable[[dict], Coroutine]) -> None:
        """Register a coroutine to be called on every Matter event."""
        self._event_handlers.append(handler)

    # ──────────────────────────────────────────────────────────────
    # High-level Device Commands
    # ──────────────────────────────────────────────────────────────

    async def get_nodes(self) -> list[dict]:
        return await self.send_command("get_nodes")

    async def commission_with_code(self, code: str) -> dict:
        """Commission a new device using a Matter QR code or manual pairing code."""
        return await self.send_command("commission_with_code", {"code": code})

    async def turn_on(self, node_id: int, endpoint_id: int = 1) -> None:
        await self.send_command("device_command", {
            "node_id": node_id, "endpoint_id": endpoint_id,
            "cluster_id": 6, "command": "on", "payload": {}
        })

    async def turn_off(self, node_id: int, endpoint_id: int = 1) -> None:
        await self.send_command("device_command", {
            "node_id": node_id, "endpoint_id": endpoint_id,
            "cluster_id": 6, "command": "off", "payload": {}
        })

    async def set_brightness(self, node_id: int, endpoint_id: int, level: int, transition_ms: int = 1000) -> None:
        """level: 0–254. transition_ms converted to 1/10s units."""
        await self.send_command("device_command", {
            "node_id": node_id, "endpoint_id": endpoint_id,
            "cluster_id": 8, "command": "moveToLevelWithOnOff",
            "payload": {"level": level, "transitionTime": transition_ms // 100}
        })

    async def set_color_temperature(self, node_id: int, endpoint_id: int, mireds: int, transition_ms: int = 1000) -> None:
        """
        WiZ GU10 range: 154 (6500K cool) – 455 (2200K warm).
        mireds = 1_000_000 / kelvin
        """
        await self.send_command("device_command", {
            "node_id": node_id, "endpoint_id": endpoint_id,
            "cluster_id": 768, "command": "moveToColorTemperature",
            "payload": {"colorTemperatureMireds": mireds, "transitionTime": transition_ms // 100}
        })

    async def set_hue_saturation(self, node_id: int, endpoint_id: int, hue: int, saturation: int, transition_ms: int = 1000) -> None:
        """Full RGB via Hue (0–254) + Saturation (0–254)."""
        await self.send_command("device_command", {
            "node_id": node_id, "endpoint_id": endpoint_id,
            "cluster_id": 768, "command": "moveToHueAndSaturation",
            "payload": {"hue": hue, "saturation": saturation, "transitionTime": transition_ms // 100}
        })
