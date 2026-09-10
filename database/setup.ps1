$ErrorActionPreference = "Stop"

Set-Location $PSScriptRoot

Write-Host "Stopping old database..."
docker compose down -v

if (Test-Path ".\data") {
    Remove-Item ".\data" -Recurse -Force
}

Write-Host "Starting PostgreSQL..."
docker compose up -d

Write-Host "Waiting for PostgreSQL..."

do {
    Start-Sleep 1
    docker exec fleet-postgres pg_isready -U fleet | Out-Null
} until ($LASTEXITCODE -eq 0)

Write-Host ""
Write-Host "PostgreSQL is ready."
Write-Host "Database: fleetdb"
Write-Host "User: fleet"
Write-Host "Port: 5432"
