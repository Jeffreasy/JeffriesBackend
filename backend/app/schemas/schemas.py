"""
Pydantic schemas — request/response contracts for the API.
Separate from ORM models to keep validation and persistence decoupled.
"""
from __future__ import annotations
import uuid
from datetime import datetime
from typing import Any
from pydantic import BaseModel, Field


# ─── Room ─────────────────────────────────────────────────────────────────────

class RoomBase(BaseModel):
    name: str = Field(..., max_length=100, examples=["Woonkamer"])
    icon: str = Field("room", max_length=50)
    floor_number: int = Field(0, ge=0)

class RoomCreate(RoomBase): pass

class RoomUpdate(BaseModel):
    name: str | None = None
    icon: str | None = None
    floor_number: int | None = None

class RoomResponse(RoomBase):
    id: uuid.UUID
    created_at: datetime
    model_config = {"from_attributes": True}


# ─── Device ───────────────────────────────────────────────────────────────────

class DeviceUpdate(BaseModel):
    """Fields that can be updated after registration."""
    name: str | None = None
    room_id: uuid.UUID | None = None
    ip_address: str | None = Field(None, examples=["192.168.1.200"], description="Nieuw LAN IP-adres (wordt geverifieerd via ping)")

class DeviceResponse(BaseModel):
    """Full device representation returned by the API."""
    id: str                          # Convex _id string
    name: str
    device_type: str
    room_id: str | None              # Convex string, not UUID
    ip_address: str | None
    mac_address: str | None = None
    current_state: dict[str, Any]
    status: str
    last_seen: str | None            # ISO string from Convex
    commissioned_at: str             # ISO string from Convex
    manufacturer: str | None = None
    model: str | None = None

class DeviceCommandRequest(BaseModel):
    """
    Control payload for a WiZ bulb. All fields are optional.
    Include only what you want to change.
    """
    on: bool | None = None
    brightness: int | None = Field(None, ge=10, le=100, description="Brightness 10–100%")
    color_temp_mireds: int | None = Field(None, ge=153, le=500, description="Color temp in mireds (auto-converted to Kelvin)")
    r: int | None = Field(None, ge=0, le=255, description="Red 0–255")
    g: int | None = Field(None, ge=0, le=255, description="Green 0–255")
    b: int | None = Field(None, ge=0, le=255, description="Blue 0–255")
    hue: int | None = Field(None, ge=0, le=254, description="Hue 0–254 (converted to RGB)")
    saturation: int | None = Field(None, ge=0, le=254, description="Saturation 0–254 (used with hue)")
    scene_id: int | None = Field(None, ge=1, le=32, description="WiZ native scene ID (1–32) — activeert ingebouwde lichteffecten")

class DeviceRegisterRequest(BaseModel):
    """Register a WiZ bulb by its LAN IP address — no Matter/BLE commissioning needed."""
    ip_address: str = Field(..., examples=["192.168.1.139"], description="LAN IP of the WiZ bulb")
    name: str = Field(..., max_length=150, examples=["WiZ GU10 Woonkamer"])
    room_id: str | None = None       # Optional Convex room ID


# ─── Scene ────────────────────────────────────────────────────────────────────

class SceneActionBase(BaseModel):
    device_id: uuid.UUID
    target_state: dict[str, Any]
    execution_order: int = 0
    transition_ms: int = 1000

class SceneCreate(BaseModel):
    name: str = Field(..., max_length=100, examples=["Film kijken"])
    icon: str = "scene"
    color_hex: str = Field("#6366f1", pattern=r"^#[0-9a-fA-F]{6}$")
    actions: list[SceneActionBase] = []

class SceneResponse(BaseModel):
    id: uuid.UUID
    name: str
    icon: str
    color_hex: str
    created_at: datetime
    actions: list[SceneActionBase] = []
    model_config = {"from_attributes": True}


# ─── Automation ───────────────────────────────────────────────────────────────

class AutomationCreate(BaseModel):
    name: str = Field(..., max_length=150)
    description: str | None = None
    trigger_config: dict[str, Any]
    condition_config: list[dict] = []
    action_config: list[dict]

class AutomationUpdate(BaseModel):
    name: str | None = None
    is_enabled: bool | None = None
    trigger_config: dict | None = None
    condition_config: list | None = None
    action_config: list | None = None

class AutomationResponse(AutomationCreate):
    id: uuid.UUID
    is_enabled: bool
    last_triggered: datetime | None
    created_at: datetime
    model_config = {"from_attributes": True}


