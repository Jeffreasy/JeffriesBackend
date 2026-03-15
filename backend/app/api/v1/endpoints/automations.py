"""
Automations endpoint — DEPRECATED.

Automations worden beheerd via Convex (niet PostgreSQL).
Dit endpoint retourneert een 410 Gone met uitleg.

Voor automation beheer: gebruik de Homeapp frontend op /automations,
of roep de Convex HTTP API direct aan.
"""
from fastapi import APIRouter, HTTPException

router = APIRouter()

_DEPRECATED_MSG = (
    "Dit endpoint is deprecated. Automations worden beheerd via Convex. "
    "Gebruik de Homeapp frontend (/automations) of de Convex API."
)


@router.get("")
async def list_automations():
    raise HTTPException(status_code=410, detail=_DEPRECATED_MSG)


@router.post("")
async def create_automation():
    raise HTTPException(status_code=410, detail=_DEPRECATED_MSG)


@router.patch("/{automation_id}")
async def update_automation(automation_id: str):
    raise HTTPException(status_code=410, detail=_DEPRECATED_MSG)


@router.delete("/{automation_id}")
async def delete_automation(automation_id: str):
    raise HTTPException(status_code=410, detail=_DEPRECATED_MSG)
