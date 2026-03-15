"""
RoomRepository — all database operations for rooms.
"""
from __future__ import annotations
import uuid

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.models.models import Room


class RoomRepository:
    def __init__(self, db: AsyncSession):
        self.db = db

    async def get_all(self, skip: int = 0, limit: int = 100) -> list[Room]:
        result = await self.db.execute(
            select(Room).options(selectinload(Room.devices))
            .order_by(Room.floor_number, Room.name)
            .offset(skip).limit(limit)
        )
        return list(result.scalars().all())

    async def get_by_id(self, room_id: uuid.UUID) -> Room | None:
        result = await self.db.execute(
            select(Room).options(selectinload(Room.devices)).where(Room.id == room_id)
        )
        return result.scalar_one_or_none()

    async def create(self, name: str, icon: str = "room", floor_number: int = 0) -> Room:
        room = Room(name=name, icon=icon, floor_number=floor_number)
        self.db.add(room)
        await self.db.flush()
        return room

    async def update(self, room_id: uuid.UUID, **kwargs) -> Room | None:
        room = await self.get_by_id(room_id)
        if room:
            for key, value in kwargs.items():
                if value is not None:
                    setattr(room, key, value)
        return room

    async def delete(self, room_id: uuid.UUID) -> bool:
        room = await self.get_by_id(room_id)
        if room:
            await self.db.delete(room)
            return True
        return False
