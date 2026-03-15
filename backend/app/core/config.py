from pydantic_settings import BaseSettings, SettingsConfigDict
from typing import List


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )

    # Application
    APP_ENV: str = "development"
    APP_SECRET_KEY: str = "change-me"
    APP_DEBUG: bool = True
    APP_HOST: str = "0.0.0.0"
    APP_PORT: int = 8000

    # Database
    DATABASE_URL: str = "postgresql+asyncpg://homeapp:change-me@localhost:5432/homeapp"

    # Matter Server
    MATTER_SERVER_URL: str = "ws://localhost:5580/ws"

    # Redis
    REDIS_URL: str = "redis://localhost:6379/0"

    # Security
    CORS_ORIGINS: List[str] = ["http://localhost:3000"]

    # Convex + Automation Engine
    CONVEX_SITE_URL: str = "https://adorable-mink-458.eu-west-1.convex.site"
    HOMEAPP_GAS_SECRET: str = "homeapp-gas-sync-2026-secure"
    HOMEAPP_USER_ID: str = "user_3Ax561ZvuSkGtWpKFooeY65HNtY"
    # true = lokale Docker engine service, false = Render/cloud (WiZ onbereikbaar)
    AUTOMATION_ENGINE_ENABLED: bool = False
    # Fallback IPs als DB niet beschikbaar is (komma-gescheiden)
    WIZ_DEVICE_IPS: str = ""

    # Logging
    LOG_LEVEL: str = "INFO"

    @property
    def is_development(self) -> bool:
        return self.APP_ENV == "development"


settings = Settings()
