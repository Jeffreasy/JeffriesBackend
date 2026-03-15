"""
SceneRepository — all database operations for scenes and scene actions.
"""
from __future__ import annotations
import uuid

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.models.models import Scene, SceneAction


class SceneRepository:
    def __init__(self, db: AsyncSession):
        self.db = db

    # ─── Scenes ───────────────────────────────────────────────────────────────

    async def get_all(self, skip: int = 0, limit: int = 100) -> list[Scene]:
        result = await self.db.execute(
            select(Scene).options(selectinload(Scene.actions))
            .order_by(Scene.name)
            .offset(skip).limit(limit)
        )
        return list(result.scalars().all())

    async def get_by_id(self, scene_id: uuid.UUID) -> Scene | None:
        result = await self.db.execute(
            select(Scene)
            .options(selectinload(Scene.actions))
            .where(Scene.id == scene_id)
        )
        return result.scalar_one_or_none()

    async def create(
        self,
        name: str,
        icon: str = "scene",
        color_hex: str = "#6366f1",
        actions: list[dict] | None = None,
    ) -> Scene:
        scene = Scene(name=name, icon=icon, color_hex=color_hex)
        self.db.add(scene)
        await self.db.flush()  # get scene.id before inserting actions

        for action_data in (actions or []):
            action = SceneAction(
                scene_id=scene.id,
                device_id=action_data["device_id"],
                target_state=action_data["target_state"],
                execution_order=action_data.get("execution_order", 0),
                transition_ms=action_data.get("transition_ms", 1000),
            )
            self.db.add(action)

        await self.db.flush()
        return scene

    async def delete(self, scene_id: uuid.UUID) -> bool:
        scene = await self.get_by_id(scene_id)
        if scene:
            await self.db.delete(scene)  # cascade deletes SceneActions
            return True
        return False
