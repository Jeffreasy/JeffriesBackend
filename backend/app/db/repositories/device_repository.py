"""
DeviceRepository — all database operations for devices.
"""
from __future__ import annotations
import uuid
from datetime import datetime, timezone

from sqlalchemy import select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.models import Device


class DeviceRepository:
    def __init__(self, db: AsyncSession):
        self.db = db

    async def get_all(self, skip: int = 0, limit: int | None = None) -> list[Device]:
        """Return all devices. Pass limit= to cap results (e.g. for paginated API endpoints)."""
        q = select(Device).offset(skip)
        if limit is not None:
            q = q.limit(limit)
        result = await self.db.execute(q)
        return list(result.scalars().all())

    async def get_by_id(self, device_id: uuid.UUID) -> Device | None:
        result = await self.db.execute(select(Device).where(Device.id == device_id))
        return result.scalar_one_or_none()

    async def get_by_node_id(self, node_id: int) -> Device | None:
        result = await self.db.execute(select(Device).where(Device.matter_node_id == node_id))
        return result.scalar_one_or_none()

    async def get_by_ip(self, ip_address: str) -> Device | None:
        result = await self.db.execute(select(Device).where(Device.ip_address == ip_address))
        return result.scalar_one_or_none()

    async def get_by_mac(self, mac_address: str) -> Device | None:
        result = await self.db.execute(select(Device).where(Device.mac_address == mac_address))
        return result.scalar_one_or_none()

    async def create(
        self,
        name: str,
        device_type: str,
        ip_address: str | None = None,
        mac_address: str | None = None,
        matter_node_id: int = 0,
        matter_endpoint_id: int = 1,
        room_id: uuid.UUID | None = None,
        manufacturer: str | None = None,
        model: str | None = None,
    ) -> Device:
        # Kies de juiste default state op basis van device type
        if device_type == "airco":
            default_state = {
                "on":          False,
                "temperature": 22.0,
                "mode":        "cool",
                "fan_speed":   "auto",
            }
        else:
            default_state = {
                "on": False,
                "brightness": 100,
                "color_temp": 4000,
                "r": 0, "g": 0, "b": 0,
            }

        device = Device(
            matter_node_id=matter_node_id,
            matter_endpoint_id=matter_endpoint_id,
            ip_address=ip_address,
            mac_address=mac_address,
            name=name,
            device_type=device_type,
            room_id=room_id,
            manufacturer=manufacturer,
            model=model,
            status="online",
            last_seen=datetime.now(timezone.utc),
            current_state=default_state,
        )
        self.db.add(device)
        await self.db.flush()
        return device

    async def update_state(self, device_id: uuid.UUID, state_patch: dict) -> None:
        """Merge a partial state update into current_state JSONB."""
        device = await self.get_by_id(device_id)
        if device:
            merged = {**device.current_state, **state_patch}
            await self.db.execute(
                update(Device)
                .where(Device.id == device_id)
                .values(current_state=merged, last_seen=datetime.now(timezone.utc))
            )

    async def set_status(self, device_id: uuid.UUID, status: str) -> None:
        await self.db.execute(
            update(Device).where(Device.id == device_id).values(status=status, last_seen=datetime.now(timezone.utc))
        )

    async def update(self, device_id: uuid.UUID, **kwargs) -> Device | None:
        await self.db.execute(
            update(Device).where(Device.id == device_id).values(**kwargs)
        )
        return await self.get_by_id(device_id)

    async def delete(self, device_id: uuid.UUID) -> bool:
        device = await self.get_by_id(device_id)
        if device:
            await self.db.delete(device)
            return True
        return False
