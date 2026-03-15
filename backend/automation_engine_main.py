"""
automation_engine_main.py — Standalone entry point voor de automation engine.

Dit is GEEN FastAPI app. Draait als aparte Docker service (24/7 lokaal).
Start: python -m automation_engine_main

Verbindt met Convex voor automation config + schedule.
Bestuurt WiZ lampen via unicast UDP over het lokale netwerk.

NOOIT deployen naar Render/cloud — WiZ is alleen via LAN bereikbaar.
"""

import asyncio
import logging
import sys

from app.core.config import settings
from app.core.logging import setup_logging
from app.services.automation.engine import AutomationEngine

logger = logging.getLogger("automation-engine")


async def main():
    setup_logging()
    logger.info("=" * 60)
    logger.info("🤖 Homeapp Automation Engine")
    logger.info(f"   Convex: {settings.CONVEX_SITE_URL}")
    logger.info(f"   User:   {settings.HOMEAPP_USER_ID[:12]}...")
    logger.info("=" * 60)

    engine = AutomationEngine(
        convex_site_url=settings.CONVEX_SITE_URL,
        gas_secret=settings.HOMEAPP_GAS_SECRET,
        user_id=settings.HOMEAPP_USER_ID,
        db_url=settings.DATABASE_URL,
    )

    engine.start()

    # Blijf draaien totdat het proces gestopt wordt (SIGTERM / Ctrl+C)
    try:
        while True:
            await asyncio.sleep(60)
    except (KeyboardInterrupt, asyncio.CancelledError):
        logger.info("🛑 Shutdown signaal ontvangen...")
    finally:
        await engine.stop()
        logger.info("✅ Automation Engine netjes gestopt")


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        sys.exit(0)
