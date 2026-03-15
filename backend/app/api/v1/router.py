from fastapi import APIRouter
from app.api.v1.endpoints import devices, rooms, scenes, automations, health


api_router = APIRouter()

api_router.include_router(health.router,       prefix="/health",      tags=["Health"])
api_router.include_router(rooms.router,        prefix="/rooms",       tags=["Rooms"])
api_router.include_router(devices.router,      prefix="/devices",     tags=["Devices"])
api_router.include_router(scenes.router,       prefix="/scenes",      tags=["Scenes"])
api_router.include_router(automations.router,  prefix="/automations", tags=["Automations"])
