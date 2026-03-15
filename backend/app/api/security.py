"""
API Security — shared dependencies for authentication.
Applies an API key check on all mutating (POST/PATCH/DELETE) endpoints.

Usage:
    @router.post("", dependencies=[Depends(require_api_key)])
    async def create_something(...):
        ...

Header required: X-API-Key: <APP_SECRET_KEY from .env>
"""
from fastapi import Depends, HTTPException, Security, status
from fastapi.security import APIKeyHeader

from app.core.config import settings

_api_key_header = APIKeyHeader(name="X-API-Key", auto_error=False)


async def require_api_key(key: str | None = Security(_api_key_header)) -> None:
    """
    Validates the X-API-Key header against APP_SECRET_KEY.
    Returns 403 if missing or incorrect.
    """
    if not key or key != settings.APP_SECRET_KEY:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Ongeldige of ontbrekende API key. Stuur X-API-Key header.",
        )
