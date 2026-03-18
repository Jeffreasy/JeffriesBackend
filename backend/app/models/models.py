"""
ORM Models — Database tables mapped to Python classes.
All models use UUID primary keys and JSONB for flexible state blobs.
"""
from __future__ import annotations
import uuid
from datetime import datetime, timezone
from sqlalchemy import String, Boolean, Integer, ForeignKey, Text, TIMESTAMP, func
from sqlalchemy.orm import Mapped, mapped_column, relationship
from sqlalchemy.dialects.postgresql import UUID, JSONB
from app.db.session import Base

# Shorthand: timezone-aware timestamp column type
TZ = TIMESTAMP(timezone=True)

def _now() -> datetime:
    """Current UTC time — timezone-aware (replaces deprecated datetime.utcnow)."""
    return datetime.now(timezone.utc)


class Room(Base):
    __tablename__ = "rooms"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    icon: Mapped[str] = mapped_column(String(50), default="room")
    floor_number: Mapped[int] = mapped_column(Integer, default=0)
    created_at: Mapped[datetime] = mapped_column(TZ, default=_now)

    devices: Mapped[list["Device"]] = relationship("Device", back_populates="room")


class Device(Base):
    __tablename__ = "devices"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    room_id: Mapped[uuid.UUID | None] = mapped_column(
        UUID(as_uuid=True), ForeignKey("rooms.id", ondelete="SET NULL"), nullable=True
    )

    # Primary control address — WiZ local UDP / Broadlink TCP
    ip_address: Mapped[str | None] = mapped_column(String(45), nullable=True, index=True)
    # MAC address — vereist voor Broadlink auth handshake (airco modules)
    mac_address: Mapped[str | None] = mapped_column(String(17), nullable=True)  # "aa:bb:cc:dd:ee:ff"

    # Matter identifiers — kept for future Matter device compatibility
    matter_node_id: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    matter_endpoint_id: Mapped[int] = mapped_column(Integer, default=1)

    # Metadata
    name: Mapped[str] = mapped_column(String(150), nullable=False)
    device_type: Mapped[str] = mapped_column(String(50), nullable=False)  # 'color_light', etc.
    manufacturer: Mapped[str | None] = mapped_column(String(100))
    model: Mapped[str | None] = mapped_column(String(100))
    firmware_version: Mapped[str | None] = mapped_column(String(50))

    # Live state — flexible JSONB blob (synced after every command)
    current_state: Mapped[dict] = mapped_column(JSONB, nullable=False, default=dict)

    status: Mapped[str] = mapped_column(String(20), default="offline")  # online | offline | error
    last_seen: Mapped[datetime | None] = mapped_column(TZ, nullable=True)
    commissioned_at: Mapped[datetime] = mapped_column(TZ, default=_now)

    room: Mapped["Room"] = relationship("Room", back_populates="devices")
    scene_actions: Mapped[list["SceneAction"]] = relationship("SceneAction", back_populates="device")


class Scene(Base):
    __tablename__ = "scenes"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    icon: Mapped[str] = mapped_column(String(50), default="scene")
    color_hex: Mapped[str] = mapped_column(String(7), default="#6366f1")
    created_at: Mapped[datetime] = mapped_column(TZ, default=_now)

    actions: Mapped[list["SceneAction"]] = relationship(
        "SceneAction", back_populates="scene", cascade="all, delete-orphan"
    )


class SceneAction(Base):
    __tablename__ = "scene_actions"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    scene_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("scenes.id", ondelete="CASCADE"), nullable=False
    )
    device_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("devices.id", ondelete="CASCADE"), nullable=False
    )
    target_state: Mapped[dict] = mapped_column(JSONB, nullable=False)
    execution_order: Mapped[int] = mapped_column(Integer, default=0)
    transition_ms: Mapped[int] = mapped_column(Integer, default=1000)

    scene: Mapped["Scene"] = relationship("Scene", back_populates="actions")
    device: Mapped["Device"] = relationship("Device", back_populates="scene_actions")


class Automation(Base):
    __tablename__ = "automations"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    name: Mapped[str] = mapped_column(String(150), nullable=False)
    description: Mapped[str | None] = mapped_column(Text)
    is_enabled: Mapped[bool] = mapped_column(Boolean, default=True)

    trigger_config: Mapped[dict] = mapped_column(JSONB, nullable=False)
    condition_config: Mapped[list] = mapped_column(JSONB, default=list)
    action_config: Mapped[list] = mapped_column(JSONB, nullable=False)

    last_triggered: Mapped[datetime | None] = mapped_column(TZ)
    created_at: Mapped[datetime] = mapped_column(TZ, default=_now)


class DeviceEvent(Base):
    """Append-only event log. Candidate for TimescaleDB hypertable in production."""
    __tablename__ = "device_events"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    time: Mapped[datetime] = mapped_column(TZ, default=_now, index=True)   # indexed, not PK
    device_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("devices.id", ondelete="CASCADE"), nullable=False, index=True
    )
    event_type: Mapped[str] = mapped_column(String(50), nullable=False)  # state_change | online | offline
    payload: Mapped[dict] = mapped_column(JSONB, default=dict)
