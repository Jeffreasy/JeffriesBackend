"""
Automation Engine — Server-side scheduler voor WiZ lamp automations.

Draait als asyncio background task in de FastAPI lifespan.
Leest automations + rooster van Convex, voert WiZ acties uit zonder open browsertab.

Flow (elke 30 seconden):
  1. Haal enabled automations op van Convex (/automations)
  2. Haal vandaag's diensten op (/schedule/today) voor schedule-type triggers
  3. Voor elke automation: controleer shouldFire (tijd + dag + shiftType)
  4. Voer WiZ actie uit op alle bekende apparaten
  5. Update lastFiredAt in Convex (/mark-fired)
"""

from __future__ import annotations

import asyncio
import logging
import os
import zoneinfo
from datetime import datetime, timedelta, timezone

import httpx

from app.services.wiz.service import WizService

logger = logging.getLogger(__name__)

# Timezone voor automation tijdvergelijking — altijd Amsterdam, ongeacht Docker TZ env
_AMS = zoneinfo.ZoneInfo("Europe/Amsterdam")

# ──────────────────────────────────────────────────────────────────────────────
# Scene definitions — mirrors lib/automations.ts SCENE_DEFINITIONS.
# sceneId is a string key ("avond", "helder"…), NOT a WiZ scene number.
# Values are translated to WizService.set_state() kwargs.
# ──────────────────────────────────────────────────────────────────────────────
def _mireds_to_kelvin(m: int) -> int:
    return round(1_000_000 / m) if m > 0 else 4000

SCENE_DEFINITIONS: dict[str, dict] = {
    "helder":  {"on": True, "brightness": 100, "color_temp": _mireds_to_kelvin(200)},
    "avond":   {"on": True, "brightness": 60,  "color_temp": _mireds_to_kelvin(370)},
    "nacht":   {"on": True, "brightness": 15,  "color_temp": _mireds_to_kelvin(455)},
    "film":    {"on": True, "r": 100, "g": 0,  "b": 180, "brightness": 30},
    "focus":   {"on": True, "brightness": 90,  "color_temp": _mireds_to_kelvin(165)},
    "ochtend": {"on": True, "brightness": 40,  "color_temp": _mireds_to_kelvin(400)},
}

# Hoe vaak te controleren (seconden)
ENGINE_INTERVAL = 30

# Minimale tijd tussen twee fires van dezelfde automation (seconden)
MIN_FIRE_INTERVAL = 55

# Elke N ticks device-status pollen (30s × 10 = 5 minuten)
STATUS_POLL_EVERY = 10


class AutomationEngine:
    """
    Server-side automation engine — draait onafhankelijk van de browser.
    Heeft directe LAN-toegang tot WiZ lampen via UDP.
    """

    def __init__(
        self,
        convex_site_url: str,
        gas_secret: str,
        user_id: str,
        db_url: str,
    ):
        self.convex_url = convex_site_url.rstrip("/")
        self.secret = gas_secret
        self.user_id = user_id
        self.db_url = db_url
        self.wiz = WizService()
        self._task: asyncio.Task | None = None
        self._fired_at: dict[str, datetime] = {}  # automationId → last fired
        self._tick_count: int = 0                  # voor status poller interval

    # ── Public API ─────────────────────────────────────────────────────────────

    def start(self):
        """Start de engine als background asyncio task."""
        if self._task and not self._task.done():
            return
        self._task = asyncio.create_task(self._loop(), name="automation-engine")
        logger.info("🤖 Automation Engine gestart (interval=%ds)", ENGINE_INTERVAL)

    async def stop(self):
        """Graceful shutdown."""
        if self._task:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        logger.info("🛑 Automation Engine gestopt")

    # ── Main loop ──────────────────────────────────────────────────────────────

    async def _loop(self):
        while True:
            try:
                await self._tick()
            except Exception as exc:
                logger.warning("Automation tick fout: %s", exc, exc_info=True)
            await asyncio.sleep(ENGINE_INTERVAL)

    async def _tick(self):
        now = datetime.now(_AMS)  # altijd Amsterdam tijd, ongeacht TZ env var
        today = now.strftime("%Y-%m-%d")

        async with httpx.AsyncClient(timeout=10) as client:
            headers = {"Authorization": f"Bearer {self.secret}"}

            # 1. Haal automations op
            automations = await self._fetch_automations(client, headers)
            if not automations:
                return

            # 2. Haal vandaag's diensten op (voor schedule triggers)
            diensten = await self._fetch_schedule(client, headers, today)
            # Sluit afgewerkte/verwijderde diensten uit — zelfde als frontend _hasDienstToday()
            today_shift_types = {
                d["shiftType"] for d in diensten
                if d.get("status") not in ("VERWIJDERD", "Gedraaid")
            }

            # 3. Haal alle bekende devices op als {id → ip} map
            device_map = await self._get_device_map()
            if not device_map:
                logger.debug("Geen WiZ apparaten gevonden — skip tick")
                return

            # 4. Check elke automation
            for auto in automations:
                if not auto.get("enabled", False):
                    continue

                auto_id = auto["_id"]

                # Anti-spam: niet opnieuw vuren binnen MIN_FIRE_INTERVAL seconden
                last = self._fired_at.get(auto_id)
                if last and (now - last).total_seconds() < MIN_FIRE_INTERVAL:
                    continue

                if not self._should_fire(auto, now, today_shift_types):
                    continue

                logger.info("⚡ Automation '%s' fires!", auto.get("name", auto_id))
                await self._execute_action(auto, device_map)

                self._fired_at[auto_id] = now
                await self._mark_fired(client, headers, auto_id)

        # Cleanup in-memory fired_at dict (verwijder entries ouder dan 1 uur)
        cutoff = now - timedelta(hours=1)
        self._fired_at = {k: v for k, v in self._fired_at.items() if v > cutoff}

        # Elke STATUS_POLL_EVERY ticks: ping lampen en update online/offline status
        self._tick_count += 1
        if self._tick_count % STATUS_POLL_EVERY == 0:
            await self._poll_device_status()

    # ── shouldFire logic ───────────────────────────────────────────────────────

    def _should_fire(
        self,
        auto: dict,
        now: datetime,
        today_shift_types: set[str],
    ) -> bool:
        trigger = auto.get("trigger", {})
        trigger_type = trigger.get("triggerType", "time")
        time_str = trigger.get("time", "")   # "HH:MM"
        days = trigger.get("days")           # [0,1,2...6] ma=0

        if not time_str:
            return False

        # Check tijdstip — 2-minuten venster om engine-vertraging op te vangen.
        # MIN_FIRE_INTERVAL (55s) voorkomt dat dezelfde automation dubbel vist.
        try:
            t_h, t_m = map(int, time_str.split(":"))
        except ValueError:
            return False

        now_total = now.hour * 60 + now.minute
        target_total = t_h * 60 + t_m
        within_window = abs(now_total - target_total) <= 1
        if not within_window:
            return False

        # Controleer Convex lastFiredAt — voorkomt dubbel vuren na herstart
        last_fired_at = auto.get("lastFiredAt")
        if last_fired_at:
            try:
                last = datetime.fromisoformat(last_fired_at.replace("Z", "+00:00"))
                # Converteer now naar UTC voor vergelijking
                now_utc = now.astimezone(timezone.utc)
                if (now_utc - last).total_seconds() < MIN_FIRE_INTERVAL:
                    return False
            except ValueError:
                pass  # Ongeldige datum → negeer de check

        # Check dag van de week (0=maandag, 6=zondag)
        current_weekday = now.weekday()  # Python: 0=maandag
        if days is None:
            # None → ALL_DAYS (altijd vuren)
            effective_days = list(range(7))
        elif len(days) == 0:
            # Expliciet lege array → nooit vuren
            return False
        else:
            effective_days = days
        if current_weekday not in effective_days:
            return False

        # Specifiek voor schedule-trigger: check shiftType vandaag
        if trigger_type == "schedule":
            shift_type = trigger.get("shiftType", "any")
            if shift_type != "any" and shift_type not in today_shift_types:
                return False

        return True

    # ── Convex API calls ───────────────────────────────────────────────────────

    async def _fetch_automations(
        self, client: httpx.AsyncClient, headers: dict
    ) -> list[dict]:
        try:
            resp = await client.get(
                f"{self.convex_url}/automations",
                params={"userId": self.user_id},
                headers=headers,
            )
            resp.raise_for_status()
            data = resp.json()
            if not data.get("ok"):
                logger.warning("Automations API fout: %s", data)
                return []
            return data.get("automations", [])
        except Exception as e:
            logger.warning("Automations ophalen mislukt: %s", e)
            return []

    async def _fetch_schedule(
        self, client: httpx.AsyncClient, headers: dict, date: str
    ) -> list[dict]:
        try:
            resp = await client.get(
                f"{self.convex_url}/schedule/today",
                params={"userId": self.user_id, "date": date},
                headers=headers,
            )
            resp.raise_for_status()
            data = resp.json()
            if not data.get("ok"):
                logger.warning("Schedule API fout: %s", data)
                return []
            return data.get("diensten", [])
        except Exception as e:
            logger.warning("Schedule ophalen mislukt: %s", e)
            return []

    async def _mark_fired(
        self, client: httpx.AsyncClient, headers: dict, automation_id: str
    ):
        try:
            resp = await client.post(
                f"{self.convex_url}/mark-fired",
                json={"automationId": automation_id},
                headers=headers,
            )
            resp.raise_for_status()
        except Exception as e:
            logger.warning("markFired mislukt voor %s: %s", automation_id, e)

    # ── Device status poller ──────────────────────────────────────────────────

    async def _poll_device_status(self):
        """Ping alle geregistreerde lampen en update online/offline status.
        Wordt elke STATUS_POLL_EVERY ticks uitgevoerd (~5 minuten)."""
        logger.info("🔍 Device status poll gestart...")
        try:
            from app.db.session import AsyncSessionLocal
            from app.db.repositories.device_repository import DeviceRepository

            async with AsyncSessionLocal() as session:
                repo = DeviceRepository(session)
                devices = await repo.get_all()

                for device in devices:
                    if not device.ip_address:
                        continue
                    try:
                        state = await self.wiz.get_state(device.ip_address)
                        new_status = "online" if state else "offline"
                    except Exception:
                        new_status = "offline"

                    if device.status != new_status:
                        await repo.set_status(device.id, new_status)
                        logger.info(
                            "Device '%s' status: %s → %s",
                            device.name, device.status, new_status,
                        )

                await session.commit()
                logger.info("✅ Device status poll klaar (%d apparaten)", len(devices))

        except Exception as e:
            logger.warning("Device status poll mislukt: %s", e)



    # ── Device IPs ophalen ────────────────────────────────────────────────────

    async def _get_device_map(self) -> dict[str, dict]:
        """
        Haal {device_id_str → {ip}} op voor alle geregistreerde WiZ lampen.
        """
        try:
            from app.db.session import AsyncSessionLocal
            from app.db.repositories.device_repository import DeviceRepository

            async with AsyncSessionLocal() as session:
                repo = DeviceRepository(session)
                devices = await repo.get_all()
                return {
                    str(d.id): {
                        "ip":          d.ip_address,
                        "mac":         d.mac_address,
                        "device_type": d.device_type,
                    }
                    for d in devices
                    if d.ip_address  # skip devices zonder IP
                }

        except Exception as e:
            logger.warning("Device map ophalen mislukt (DB): %s", e)
            # Fallback: bekende IPs uit env — alleen lampen (geen airco-fallback)
            fallback = os.getenv("WIZ_DEVICE_IPS", "")
            return {
                ip: {"ip": ip, "mac": None, "device_type": "color_light"}
                for ip in fallback.split(",") if ip.strip()
            }


    # ── WiZ actie uitvoeren ────────────────────────────────────────────────────

    async def _execute_action(self, auto: dict, device_map: dict[str, dict]):
        action = auto.get("action", {})
        action_type = action.get("type", "on")
        device_ids = action.get("deviceIds")  # lijst van device UUID strings, of None

        if device_ids:
            infos = [device_map[did] for did in device_ids if did in device_map]
        else:
            infos = list(device_map.values())

        if not infos:
            logger.warning("Geen apparaten voor automation '%s' — skip", auto.get("name"))
            return

        tasks = [self._apply_action(info, action_type, action) for info in infos]
        await asyncio.gather(*tasks, return_exceptions=True)

    async def _apply_action(self, device_info: dict, action_type: str, action: dict):
        ip          = device_info["ip"]
        mac         = device_info.get("mac")
        device_type = device_info.get("device_type", "color_light")

        try:
            if action_type == "off":
                await self.wiz.turn_off(ip)

            elif action_type == "on":
                await self.wiz.turn_on(ip)

            elif action_type == "brightness":
                brightness = action.get("brightness", 80)
                await self.wiz.set_brightness(ip, brightness)

            elif action_type == "color_temp":
                mireds = action.get("colorTempMireds", 250)
                kelvin = round(1_000_000 / mireds) if mireds > 0 else 4000
                await self.wiz.set_color_temp(ip, kelvin)

            elif action_type == "scene":
                # sceneId is a string key ("avond", "helder" etc.) — NOT a WiZ int scene ID
                scene_key = str(action.get("sceneId", "helder"))
                scene_def = SCENE_DEFINITIONS.get(scene_key, SCENE_DEFINITIONS["helder"])
                await self.wiz.set_state(ip, **scene_def)

            elif action_type == "color":
                hex_color = action.get("colorHex", "#ffffff")
                r, g, b = _hex_to_rgb(hex_color)
                await self.wiz.set_color(ip, r, g, b)

            else:
                logger.warning(
                    "Onbekend action type: '%s' — overgeslagen voor %s",
                    action_type, ip,
                )

        except Exception as e:
            logger.warning("WiZ actie %s op %s mislukt: %s", action_type, ip, e)


def _hex_to_rgb(hex_color: str) -> tuple[int, int, int]:
    hex_color = hex_color.lstrip("#")
    r = int(hex_color[0:2], 16)
    g = int(hex_color[2:4], 16)
    b = int(hex_color[4:6], 16)
    return r, g, b
