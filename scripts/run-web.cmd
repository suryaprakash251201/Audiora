@echo off
rem Launcher for the Vite dev server.

cd /d "%~dp0../web"
rem Appending to the log rather than redirecting on the CreateProcess call:
rem the scheduler does not wait on the pipes, and a full stdout pipe would
rem otherwise block node during startup.
call npm run dev -- --port 5173 --strictPort --host 127.0.0.1 >> "%~dp0../.local-test/dev-web.log" 2>&1
