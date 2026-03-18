# run_local.ps1 — Start de Homeapp API direct op de host (geen Docker)
# Geeft directe LAN toegang voor Broadlink/WiZ devices
#
# Vereisten:
#   - Postgres + Redis draaien in Docker:  docker compose up -d postgres redis
#   - Venv aangemaakt:                     py -3 -m venv .venv
#   - Dependencies geïnstalleerd:          .venv\Scripts\pip install -r requirements.txt

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

Write-Host "🏠 Homeapp API — Host mode (directe LAN toegang)" -ForegroundColor Cyan
Write-Host "   DB:   localhost:5432" -ForegroundColor Gray
Write-Host "   Port: 8000" -ForegroundColor Gray
Write-Host ""

# Laad .env.local variabelen
Get-Content .env.local | Where-Object { $_ -match "^\s*[^#]" -and $_ -match "=" } | ForEach-Object {
    $parts = $_ -split "=", 2
    $key   = $parts[0].Trim()
    $value = $parts[1].Trim()
    [System.Environment]::SetEnvironmentVariable($key, $value, "Process")
}

# Start uvicorn via de venv
& ".venv\Scripts\uvicorn.exe" app.main:app --host 0.0.0.0 --port 8000 --reload
