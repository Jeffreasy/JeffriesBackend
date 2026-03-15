"""
Scenes endpoint — full CRUD + WiZ activation.
Scenes define a named lighting preset that can be applied to multiple devices.
"""
import uuid
import asyncio
import logging
from fastapi import APIRouter, Depends, HTTPException, status

from sqlalchemy.ext.asyncio import AsyncSession

from app.db.session import get_db
from app.db.repositories.scene_repository import SceneRepository
from app.db.repositories.device_repository import DeviceRepository
from app.schemas.schemas import SceneCreate, SceneResponse
from app.services.wiz.service import WizService
from app.api.security import require_api_key

logger = logging.getLogger(__name__)
router = APIRouter()
wiz = WizService()


# ─── CRUD ─────────────────────────────────────────────────────────────────────

@router.get("", response_model=list[SceneResponse])
async def list_scenes(
    skip: int = 0,
    limit: int = 100,
    db: AsyncSession = Depends(get_db),
):
    """Return all scenes. Use skip/limit for pagination."""
    return await SceneRepository(db).get_all(skip=skip, limit=limit)


@router.get("/{scene_id}", response_model=SceneResponse)
async def get_scene(scene_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    """Get a single scene by ID."""
    scene = await SceneRepository(db).get_by_id(scene_id)
    if not scene:
        raise HTTPException(status_code=404, detail="Scene not found")
    return scene


@router.post(
    "",
    response_model=SceneResponse,
    status_code=status.HTTP_201_CREATED,
    dependencies=[Depends(require_api_key)],
)
async def create_scene(data: SceneCreate, db: AsyncSession = Depends(get_db)):
    """
    Create a scene with optional device actions.
    Each action defines a target state (brightness, color_temp, RGB) for a device.
    """
    actions = [
        {
            "device_id": str(a.device_id),
            "target_state": a.target_state,
            "execution_order": a.execution_order,
            "transition_ms": a.transition_ms,
        }
        for a in data.actions
    ]
    scene = await SceneRepository(db).create(
        name=data.name,
        icon=data.icon,
        color_hex=data.color_hex,
        actions=actions,
    )
    return await SceneRepository(db).get_by_id(scene.id)


@router.delete(
    "/{scene_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(require_api_key)],
)
async def delete_scene(scene_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    """Delete a scene and all its actions (cascade)."""
    if not await SceneRepository(db).delete(scene_id):
        raise HTTPException(status_code=404, detail="Scene not found")


# ─── Activation ───────────────────────────────────────────────────────────────

@router.post(
    "/{scene_id}/activate",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(require_api_key)],
)
async def activate_scene(scene_id: uuid.UUID, db: AsyncSession = Depends(get_db)):
    """
    Activate a scene — sends WiZ commands to all scene devices in parallel.
    Actions are sorted by execution_order before firing.
    """
    scene = await SceneRepository(db).get_by_id(scene_id)
    if not scene:
        raise HTTPException(status_code=404, detail="Scene not found")

    if not scene.actions:
        return  # Nothing to activate

    device_repo = DeviceRepository(db)
    sorted_actions = sorted(scene.actions, key=lambda a: a.execution_order)

    async def _apply(action):
        device = await device_repo.get_by_id(action.device_id)
        if not device or not device.ip_address:
            logger.warning(
                "Scene %s: device %s has no IP — skipping",
                scene_id, action.device_id,
            )
            return

        state = action.target_state
        try:
            await wiz.set_state(
                device.ip_address,
                on=state.get("on", True),
                brightness=state.get("brightness"),
                color_temp=state.get("color_temp"),
                r=state.get("r"),
                g=state.get("g"),
                b=state.get("b"),
            )
            logger.info(
                "Scene '%s': %s → state applied ✓",
                scene.name, device.name,
            )
        except Exception as exc:
            logger.error("WiZ command failed for %s: %s", device.ip_address, exc)

    # Execute all in parallel
    await asyncio.gather(*[_apply(a) for a in sorted_actions], return_exceptions=True)
