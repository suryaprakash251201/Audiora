# Stops the local development server and the Vite dev server.
#   powershell -File scripts/dev-stop.ps1

$ErrorActionPreference = 'Continue'
$root = Split-Path -Parent $PSScriptRoot
$work = Join-Path $root '.local-test'
$pidFile = Join-Path $work 'dev-pids.json'

function Stop-ByPid($id, $label) {
    if (-not $id) { return }
    $p = Get-Process -Id $id -ErrorAction SilentlyContinue
    if ($p) {
        try {
            # Kill the whole tree: Vite spawns esbuild children that would
            # otherwise keep the port bound.
            & taskkill /PID $id /T /F | Out-Null
            Write-Host "stopped $label (pid $id)" -ForegroundColor DarkGray
        } catch {
            Write-Host "could not stop $label (pid $id)" -ForegroundColor Yellow
        }
    }
}

if (Test-Path $pidFile) {
    $state = Get-Content $pidFile -Raw | ConvertFrom-Json
    Stop-ByPid $state.webPid 'the web server'
    Stop-ByPid $state.serverPid 'the API server'
    Remove-Item $pidFile -Force
} else {
    Write-Host 'no recorded pids; stopping by process name' -ForegroundColor DarkGray
    Get-Process audiora -ErrorAction SilentlyContinue | ForEach-Object {
        & taskkill /PID $_.Id /T /F | Out-Null
    }
}

Write-Host 'done' -ForegroundColor Cyan
