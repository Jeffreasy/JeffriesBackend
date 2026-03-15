from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from contextlib import asynccontextmanager
import logging

from app.core.config import settings
from app.core.logging import setup_logging
from app.api.v1.router import api_router

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Startup & shutdown lifecycle events."""
    setup_logging()
    logger.info(f"Starting Homeapp API (env={settings.APP_ENV})")

    # WiZ Local UDP — stateless, no connection needed at startup
    try:
        from app.services.matter.client import MatterClient
        matter_client = MatterClient(url=settings.MATTER_SERVER_URL)
        await matter_client.connect()
        app.state.matter = matter_client
        logger.info("Matter Server connected (optional feature)")
    except Exception as e:
        logger.warning(f"Matter Server unavailable — WiZ UDP control still works: {e}")
        app.state.matter = None

    # 🤖 Automation Engine — alleen starten als expliciet ingeschakeld
    # (true in lokale Docker service, false op Render/cloud)
    engine = None
    if settings.AUTOMATION_ENGINE_ENABLED:
        from app.services.automation.engine import AutomationEngine
        engine = AutomationEngine(
            convex_site_url=settings.CONVEX_SITE_URL,
            gas_secret=settings.HOMEAPP_GAS_SECRET,
            user_id=settings.HOMEAPP_USER_ID,
            db_url=settings.DATABASE_URL,
        )
        engine.start()
        app.state.automation_engine = engine
        logger.info("🤖 Automation Engine actief (server-side, geen browser nodig)")
    else:
        logger.info("⏭️  Automation Engine uitgeschakeld (AUTOMATION_ENGINE_ENABLED=false)")

    yield  # ← application runs here

    if engine:
        await engine.stop()
    if getattr(app.state, "matter", None):
        await app.state.matter.disconnect()




def create_app() -> FastAPI:
    app = FastAPI(
        title="Homeapp API",
        description=(
            "Local smart home backend for WiZ GU10 lamps and future Matter devices.\n\n"
            "Control lamps via direct LAN UDP — no cloud, no Bluetooth needed."
        ),
        version="0.2.0",
        docs_url="/docs",
        redoc_url="/redoc",
        lifespan=lifespan,
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.CORS_ORIGINS,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    app.include_router(api_router, prefix="/api/v1")

    return app


app = create_app()
