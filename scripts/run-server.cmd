@echo off
rem Launcher for the Audiora API server, used by scripts/dev-start.ps1.
rem
rem Going through cmd is deliberate. The shell this runs under tears down
rem direct child processes when a command finishes, so a process started
rem straight from PowerShell would not survive the script that started it.
rem A cmd wrapper makes it a grandchild, which does.

set "AUD_ROOT=%~dp0.."
set "AUD_ROOT=%AUD_ROOT:\=/%"

set "MUSIC_PATH=%AUD_ROOT%/.local-test/music"
set "DATA_PATH=%AUD_ROOT%/.local-test/data"
set "AUDIORA_DOMAIN=localhost"
set "LISTEN_ADDR=:8080"
rem Development-only secret. Never reuse this on a real server.
set "AUDIORA_SECRET=dev-only-secret-not-for-production-use-32ch"
set "ADMIN_EMAIL=admin@example.com"
set "ADMIN_PASSWORD=audiora-dev"
set "ADMIN_NAME=Admin"
set "SCAN_INTERVAL_MIN=0"
set "CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173"

cd /d "%AUD_ROOT%/server"
"%AUD_ROOT%/.local-test/audiora.exe" >> "%AUD_ROOT%/.local-test/dev-server.log" 2>&1
