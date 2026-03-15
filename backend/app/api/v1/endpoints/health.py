from fastapi import APIRouter, Depends
from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncSession
from app.db.session import get_db

router = APIRouter()


@router.get("", summary="Health check")
async def health_check(db: AsyncSession = Depends(get_db)):
    """
    Returns 200 when API + database are healthy.
    Returns 503 when the database is unreachable.
    Used by Docker healthchecks and Render health monitoring.
    """
    try:
        await db.execute(text("SELECT 1"))
        return {"status": "ok", "service": "homeapp-api", "db": "ok"}
    except Exception as e:
        from fastapi import HTTPException
        raise HTTPException(
            status_code=503,
            detail={"status": "degraded", "service": "homeapp-api", "db": "error", "detail": str(e)},
        )
