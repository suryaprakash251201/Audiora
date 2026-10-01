# Starts Audiora locally for development: the Go API server plus the Vite dev
# server, both left running in the background.
#
#   powershell -File scripts/dev-start.ps1
#   powershell -File scripts/dev-stop.ps1
#
# Docker is the supported way to run Audiora in production; this is the loop
# for working on the code, where hot reload and a fast rebuild matter more.

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$work = Join-Path $root '.local-test'
$apiPort = 8080
$webPort = 5173

New-Item -ItemType Directory -Path $work -Force | Out-Null

# Wait-For polls a URL until it answers, or gives up.
function Wait-For($url, $seconds, $label) {
    for ($i = 0; $i -lt ($seconds * 4); $i++) {
        Start-Sleep -Milliseconds 250
        try {
            $null = Invoke-WebRequest $url -UseBasicParsing -TimeoutSec 3
            return $true
        } catch { }
    }
    Write-Host "$label never answered at $url" -ForegroundColor Red
    return $false
}

# Start-Background launches a cmd wrapper, which in turn launches the real
# program.
#
# Two details are doing real work here:
#
#  - cmd, rather than Start-Process on the program itself. The environment
#    this runs under tears down direct child processes when a command
#    finishes, so a program started straight from PowerShell dies with the
#    script that started it. Going through cmd makes it a grandchild.
#  - output appended to a file inside the cmd line, rather than through
#    -RedirectStandardOutput. Handing node pipes that nobody drains fills
#    them and stalls the build.
function Start-Background($label, $cmdLine, $workingDir) {
    Start-Process -FilePath 'cmd.exe' `
        -ArgumentList @('/c', $cmdLine) `
        -WorkingDirectory $workingDir `
        -WindowStyle Hidden | Out-Null
}

# --- preflight -------------------------------------------------------------

# Anything from a previous run. A server still holding the database, or a Vite
# still holding the port, would make the new one fail to start.
Get-Process audiora -ErrorAction SilentlyContinue | ForEach-Object {
    & taskkill /PID $_.Id /T /f 2>&1 | Out-Null
}
$ErrorActionPreference = 'Continue'
# Vite's process is named node, which is too broad to kill wholesale, so the
# process holding the port is targeted instead.
$onPort = Get-NetTCPConnection -LocalPort $webPort -State Listen -ErrorAction SilentlyContinue
if ($onPort) {
    & taskkill /PID $onPort.OwningProcess /T /f 2>&1 | Out-Null
}
$ErrorActionPreference = 'Stop'
Start-Sleep -Milliseconds 400

$binary = Join-Path $work 'audiora.exe'
$newestSource = Get-ChildItem -Recurse -File -Path (Join-Path $root 'server') `
    -Include *.go, go.mod, go.sum, *.sql -ErrorAction SilentlyContinue |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1

# Rebuild whenever the binary is missing or older than any source file.
# Checking only for absence would leave a running server on stale code after
# an edit, which is exactly the kind of thing that wastes an afternoon.
$needsBuild = $true
if (Test-Path $binary) {
    $binaryTime = (Get-Item $binary).LastWriteTime
    if ($newestSource -and $binaryTime -ge $newestSource.LastWriteTime) { $needsBuild = $false }
}

if ($needsBuild) {
    Write-Host 'building the server' -ForegroundColor DarkGray
    Push-Location (Join-Path $root 'server')
    try { & go build -o $binary ./cmd/audiora } finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw 'the server build failed' }
}

if (-not (Test-Path (Join-Path $work 'music'))) {
    Write-Host 'no music folder found, generating the fixture library' -ForegroundColor Yellow
    & (Join-Path $PSScriptRoot 'make-fixtures.ps1') | Out-Null
}

foreach ($log in @('dev-server.log', 'dev-web.log')) {
    $path = Join-Path $work $log
    if (Test-Path $path) { Remove-Item $path -Force }
    New-Item -ItemType File -Path $path -Force | Out-Null
}

# --- API server ------------------------------------------------------------
Start-Background 'api' "`"$PSScriptRoot\run-server.cmd`"" $root
if (-not (Wait-For "http://127.0.0.1:$apiPort/healthz" 45 'the API server')) {
    Get-Content (Join-Path $work 'dev-server.log') -Tail 25 -ErrorAction SilentlyContinue
    exit 1
}
$health = Invoke-RestMethod "http://127.0.0.1:$apiPort/healthz"
Write-Host "  API up on :$apiPort  (health $($health.status), search index $($health.ftsEnabled))" -ForegroundColor DarkGray

# --- web dev server --------------------------------------------------------
Start-Background 'web' "`"$PSScriptRoot\run-web.cmd`"" $root
if (-not (Wait-For "http://127.0.0.1:$webPort/" 120 'the web server')) {
    Get-Content (Join-Path $work 'dev-web.log') -Tail 25 -ErrorAction SilentlyContinue
    exit 1
}

# --- report ----------------------------------------------------------------
Write-Host ''
Write-Host '=== Audiora is running ===' -ForegroundColor Green
Write-Host "  web app    http://localhost:$webPort" -ForegroundColor Green
Write-Host "  api        http://localhost:$apiPort/healthz" -ForegroundColor DarkGray
Write-Host '  sign in    admin@example.com / audiora-dev' -ForegroundColor Green
Write-Host "  web log    .local-test\dev-web.log" -ForegroundColor DarkGray
Write-Host '  stop       powershell -File scripts/dev-stop.ps1' -ForegroundColor DarkGray
Write-Host ''
