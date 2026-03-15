"""Rooms endpoint — CRUD with RoomRepository."""
import uuid
from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.db.session import get_db
from app.db.repositories.room_repository import RoomRepository
from app.schemas.schemas import RoomCreate, RoomUpdate, RoomResponse
from app.api.security import require_api_key

router = APIRouter()


@router.get("", response_model=list[RoomResponse])
async def list_rooms(
    skip: int = 0,
    limit: int = 100,
    db: AsyncSession = Depends(get_db),
):
    """Return all rooms. Use skip/limit for pagination."""
    return await RoomRepository(db).get_all(skip=skip, limit=limit)


@router.post("", response_model=RoomResponse, status_code=status.HTTP_201_CREATED,
             dependencies=[Depends(require_api_key)])
async def create_room(data: RoomCreate, db: AsyncSession = Depends(get_db)):
    """Create a new room."""
    return await RoomRepository(db).create(
        name=data.name, icon=data.icon, floor_number=data.floor_number
    )


@router.get("/{room_id}", response_model=RoomResponse)
async def get_room(room_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    """Get a single room by ID."""
    room = await RoomRepository(db).get_by_id(room_id)
    if not room:
        raise HTTPException(status_code=404, detail="Room not found")
    return room


@router.patch("/{room_id}", response_model=RoomResponse, dependencies=[Depends(require_api_key)])
async def update_room(room_id: uuid.UUID, data: RoomUpdate, db: AsyncSession = Depends(get_db)):
    """Update room name, icon or floor."""
    room = await RoomRepository(db).update(room_id, **data.model_dump(exclude_none=True))
    if not room:
        raise HTTPException(status_code=404, detail="Room not found")
    return room


@router.delete("/{room_id}", status_code=status.HTTP_204_NO_CONTENT,
               dependencies=[Depends(require_api_key)])
async def delete_room(room_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    """Delete a room. Devices in this room will have room_id set to null."""
    if not await RoomRepository(db).delete(room_id):
        raise HTTPException(status_code=404, detail="Room not found")
