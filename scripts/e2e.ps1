# End-to-end smoke test: starts the real server against a real music folder,
# exercises the full API, then shuts everything down.
# Run:  powershell -File scripts/e2e.ps1

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$work = Join-Path $root '.local-test'
$port = 8099
$base = "http://127.0.0.1:$port"

$env:MUSIC_PATH       = Join-Path $work 'music'
$env:DATA_PATH        = Join-Path $work 'data'
$env:AUDIORA_DOMAIN   = 'localhost'
$env:AUDIORA_SECRET   = 'test-secret-value-that-is-definitely-long-enough-32'
$env:LISTEN_ADDR      = ":$port"
$env:ADMIN_EMAIL      = 'admin@example.com'
$env:ADMIN_PASSWORD   = 'correct-horse-battery'
$env:ADMIN_NAME       = 'Admin'
$env:SCAN_INTERVAL_MIN = '0'
$env:CORS_ORIGINS     = 'http://localhost:5173'

$pass = 0
$fail = 0
function Check($name, $condition, $detail = '') {
    if ($condition) {
        $script:pass++
        Write-Host "  PASS  $name" -ForegroundColor Green
    } else {
        $script:fail++
        Write-Host "  FAIL  $name  $detail" -ForegroundColor Red
    }
}

# Start from an empty database every run, so the scan assertions are about
# this run's behaviour rather than whatever a previous one left behind.
# A server left behind by an interrupted run would hold the file open, so
# those are cleared out first.
Get-Process audiora -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "stopping a leftover audiora process (pid $($_.Id))" -ForegroundColor DarkGray
    $_.Kill()
    $_.WaitForExit(5000)
}
Start-Sleep -Milliseconds 500

$dataPath = Join-Path $work 'data'
if (Test-Path $dataPath) {
    Write-Host "removing previous data directory`n" -ForegroundColor DarkGray
    Remove-Item $dataPath -Recurse -Force
}

Write-Host "`n=== Audiora end-to-end test ===`n" -ForegroundColor Cyan

$server = Start-Process -FilePath (Join-Path $work 'audiora.exe') -PassThru `
    -RedirectStandardOutput (Join-Path $work 'server.log') `
    -RedirectStandardError  (Join-Path $work 'server.err') `
    -NoNewWindow

try {
    # --- wait for the server to accept connections ---
    $ready = $false
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Milliseconds 250
        try {
            $null = Invoke-WebRequest "$base/healthz" -UseBasicParsing -TimeoutSec 2
            $ready = $true
            break
        } catch { }
    }
    if (-not $ready) { throw "server did not become ready" }
    Write-Host "server up on $base`n" -ForegroundColor DarkGray

    $health = Invoke-RestMethod "$base/healthz"
    Check "health reports ok" ($health.status -eq 'ok') $health
    Check "FTS5 search is available" ($health.ftsEnabled -eq $true) $health

    # --- log in as the bootstrapped admin ---
    $login = Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' `
        -Body (@{ email = 'admin@example.com'; password = 'correct-horse-battery' } | ConvertTo-Json)
    $h = @{ Authorization = "Bearer $($login.accessToken)" }
    Check "login returns a user" ($null -ne $login.user) $login.user
    Check "login user is an admin" ($login.user.isAdmin -eq $true) $login.user
    Check "an access token is issued" ($login.accessToken.Length -gt 100)

    # --- scan the library ---
    $null = Invoke-RestMethod "$base/api/admin/scan" -Method Post -Headers $h
    for ($i = 0; $i -lt 120; $i++) {
        Start-Sleep -Milliseconds 250
        $scan = Invoke-RestMethod "$base/api/admin/scan" -Headers $h
        if (-not $scan.running) { break }
    }
    Check "scan completed" ($scan.phase -eq 'done') $scan
    Check "scan imported 6 tracks" ($scan.added -eq 6) "added=$($scan.added)"
    Check "scan reported no errors" ([string]::IsNullOrEmpty($scan.error)) $scan.error

    # --- library contents ---
    $stats = Invoke-RestMethod "$base/api/library/stats" -Headers $h
    Check "stats count 6 tracks" ($stats.tracks -eq 6) $stats
    Check "stats count 3 albums" ($stats.albums -eq 3) $stats
    Check "stats count 3 artists" ($stats.artists -eq 3) $stats

    $albums = Invoke-RestMethod "$base/api/library/albums" -Headers $h
    Check "three albums listed" ($albums.albums.Count -eq 3) $albums.count
    $pastel = $albums.albums | Where-Object { $_.title -eq 'Pastel Blues' } | Select-Object -First 1
    Check "album has 3 tracks" ($pastel.trackCount -eq 3) $pastel
    Check "sidecar cover art was extracted" (-not [string]::IsNullOrEmpty($pastel.coverPath)) $pastel
    Check "album has a dominant colour" (-not [string]::IsNullOrEmpty($pastel.dominantColor)) $pastel.dominantColor
    Check "album year was read from tags" ($pastel.year -eq 1965) $pastel.year

    $geogaddi = $albums.albums | Where-Object { $_.title -eq 'Geogaddi' } | Select-Object -First 1
    Check "embedded ID3 cover art was extracted" (-not [string]::IsNullOrEmpty($geogaddi.coverPath)) $geogaddi

    $detail = Invoke-RestMethod "$base/api/library/albums/$($pastel.id)" -Headers $h
    Check "album detail returns 3 tracks" ($detail.tracks.Count -eq 3) $detail.tracks.Count
    Check "tracks are in disc order" ($detail.tracks[0].title -eq 'Sinnerman') $detail.tracks[0].title
    Check "track numbers were read" ($detail.tracks[1].trackNo -eq 2) $detail.tracks[1].trackNo
    Check "one track is flagged as having lyrics" ($detail.tracks[0].hasLyrics -eq $true)

    $track = $detail.tracks[0]
    Check "track duration is plausible" ($track.durationMs -gt 5500 -and $track.durationMs -lt 6500) $track.durationMs
    Check "track format is flac" ($track.format -eq 'flac') $track.format

    # --- cover art is really served and really a JPEG ---
    $coverBytes = (Invoke-WebRequest "$base/api/covers/$($pastel.coverPath)" -Headers $h -UseBasicParsing).Content
    $isJpeg = ($coverBytes[0] -eq 0xFF -and $coverBytes[1] -eq 0xD8)
    Check "cover art is served as a JPEG" ($isJpeg -and $coverBytes.Length -gt 1000) "bytes=$($coverBytes.Length)"

    # --- lyrics ---
    $lyrics = Invoke-RestMethod "$base/api/library/tracks/$($track.id)/lyrics" -Headers $h
    Check "lyrics were attached from the sidecar" ($lyrics.source -eq 'sidecar') $lyrics.source
    Check "lyrics contain an LRC timestamp" ($lyrics.lyrics -match '\[\d\d:\d\d\.\d\d\]') $lyrics.lyrics

    # --- search ---
    $search = Invoke-RestMethod "$base/api/search?q=Sinnerman" -Headers $h
    Check "search finds the track" ($search.results.Count -ge 1) $search.count
    $bad = Invoke-RestMethod "$base/api/search?q=%22%3A%3A%3A" -Headers $h
    Check "search survives a malformed FTS query" ($null -ne $bad)

    # --- lossless streaming with a byte range ---
    $rangeReq = [System.Net.HttpWebRequest]::Create("$base/api/stream/$($track.id)")
    $rangeReq.Headers['Authorization'] = "Bearer $($login.accessToken)"
    $rangeReq.AddRange(0, 99)
    $rangeResp = $rangeReq.GetResponse()
    $rangeBytes = (New-Object IO.StreamReader($rangeResp.GetResponseStream())).ReadToEnd()
    Check "range request returns 206" ([int]$rangeResp.StatusCode -eq 206) $rangeResp.StatusCode
    Check "range request is audio/flac" ($rangeResp.ContentType -eq 'audio/flac') $rangeResp.ContentType
    Check "range request advertises byte ranges" ($rangeResp.Headers['Accept-Ranges'] -eq 'bytes')
    Check "range request returned bytes" ($rangeBytes.Length -gt 0) "len=$($rangeBytes.Length)"
    $rangeResp.Close()

    # --- transcoding ---
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $trans = Invoke-WebRequest "$base/api/stream/$($track.id)?profile=aac96" -Headers $h -UseBasicParsing
    $sw.Stop()
    Check "transcode returns audio" ($trans.RawContentLength -gt 1000) "bytes=$($trans.RawContentLength)"
    Check "transcode is audio/mp4" ($trans.Headers['Content-Type'] -eq 'audio/mp4') $trans.Headers['Content-Type']
    Check "transcode finished quickly" ($sw.ElapsedMilliseconds -lt 15000) "$($sw.ElapsedMilliseconds)ms"

    # Second play must come from the cache and support ranges.
    $transReq = [System.Net.HttpWebRequest]::Create("$base/api/stream/$($track.id)?profile=aac96")
    $transReq.Headers['Authorization'] = "Bearer $($login.accessToken)"
    $transReq.AddRange(0, 49)
    $transResp = $transReq.GetResponse()
    Check "cached transcode supports ranges" ([int]$transResp.StatusCode -eq 206) $transResp.StatusCode
    $transResp.Close()

    # --- the WAV: an explicit lossless request must serve it untouched ---
    $richter = $albums.albums | Where-Object { $_.title -eq 'On the Nature of Daylight' } | Select-Object -First 1
    $richterDetail = Invoke-RestMethod "$base/api/library/albums/$($richter.id)" -Headers $h
    $wavTrack = $richterDetail.tracks[0]
    Check "WAV track format detected" ($wavTrack.format -eq 'wav') $wavTrack.format
    $wav = Invoke-WebRequest "$base/api/stream/$($wavTrack.id)" -Headers $h -UseBasicParsing
    Check "bare stream URL serves the original WAV" ($wav.Headers['Content-Type'] -eq 'audio/wav') $wav.Headers['Content-Type']

    # The WAV should be much larger than its AAC version, which is exactly
    # why the client never streams it lossless on a metered link.
    $wavAac = Invoke-WebRequest "$base/api/stream/$($wavTrack.id)?profile=aac96" -Headers $h -UseBasicParsing
    Check "transcoded WAV is smaller than the original" ($wavAac.RawContentLength -lt $wav.RawContentLength) `
        "aac=$($wavAac.RawContentLength) wav=$($wav.RawContentLength)"

    # --- playlists ---
    $pl = Invoke-RestMethod "$base/api/playlists" -Method Post -ContentType 'application/json' -Headers $h `
        -Body (@{ name = 'Smoke Test'; trackIds = @($track.id, $detail.tracks[1].id) } | ConvertTo-Json)
    Check "playlist created with 2 tracks" ($pl.playlist.trackCount -eq 2) $pl.playlist.trackCount
    Check "playlist duration is summed" ($pl.playlist.durationMs -gt 0) $pl.playlist.durationMs
    $reordered = Invoke-RestMethod "$base/api/playlists/$($pl.playlist.id)/reorder" -Method Post `
        -ContentType 'application/json' -Headers $h `
        -Body (@{ trackIds = @($detail.tracks[1].id, $track.id) } | ConvertTo-Json)
    Check "reorder took effect" ($reordered.tracks[0].id -eq $detail.tracks[1].id) $reordered.tracks[0].title

    # --- favourites ---
    $null = Invoke-WebRequest "$base/api/favorites/$($track.id)" -Method Post -Headers $h -UseBasicParsing
    $favs = Invoke-RestMethod "$base/api/favorites" -Headers $h
    Check "favourite recorded" ($favs.tracks.Count -eq 1) $favs.tracks.Count

    # --- history ---
    $null = Invoke-RestMethod "$base/api/history" -Method Post -ContentType 'application/json' -Headers $h `
        -Body (@{ trackId = $track.id; completion = 0.9; positionMs = 2900 } | ConvertTo-Json)
    $recent = Invoke-RestMethod "$base/api/history/recent" -Headers $h
    Check "history recorded" ($recent.tracks.Count -eq 1) $recent.tracks.Count

    # --- sync state over plain HTTP ---
    $sync = Invoke-RestMethod "$base/api/sync/state" -Headers $h
    Check "sync state is readable" ($null -ne $sync) $sync

    # --- authorisation ---
    try {
        $null = Invoke-WebRequest "$base/api/library/stats" -UseBasicParsing
        Check "unauthenticated request is rejected" $false "it succeeded"
    } catch {
        Check "unauthenticated request is rejected" ([int]$_.Exception.Response.StatusCode -eq 401)
    }

    # A token in the query string, which is how the <audio> element and the
    # WebSocket authenticate, must work. This is the path the web player
    # actually uses for every track.
    try {
        $q = Invoke-WebRequest "$base/api/stream/$($track.id)?t=$($login.accessToken)" -UseBasicParsing
        Check "query-string token is accepted for streaming" ($q.StatusCode -eq 200) $q.StatusCode
    } catch {
        Check "query-string token is accepted for streaming" $false $_.Exception.Message
    }

    # A bearer header must still work on the same route.
    $q2 = Invoke-WebRequest "$base/api/stream/$($track.id)" -Headers $h -UseBasicParsing
    Check "bearer token is accepted for streaming" ($q2.StatusCode -eq 200) $q2.StatusCode

    # Path traversal must be refused.
    try {
        $bad = Invoke-WebRequest "$base/api/covers/..%2F..%2Faudiora.db" -Headers $h -UseBasicParsing
        Check "cover path traversal is refused" ($bad.StatusCode -ne 200) $bad.StatusCode
    } catch {
        Check "cover path traversal is refused" ([int]$_.Exception.Response.StatusCode -in @(400, 404))
    }

    # --- admin surface ---
    $users = Invoke-RestMethod "$base/api/admin/users" -Headers $h
    Check "admin sees the user list" ($users.users.Count -ge 1) $users.users.Count
    $cacheInfo = Invoke-RestMethod "$base/api/admin/cache" -Headers $h
    Check "transcode cache is reported" ($cacheInfo.sizeBytes -gt 0) $cacheInfo.sizeBytes
    Check "profiles are advertised" ($cacheInfo.profiles.Count -ge 3) $cacheInfo.profiles.Count

} finally {
    if ($server -and -not $server.HasExited) {
        $server.CloseMainWindow() | Out-Null
        Start-Sleep -Milliseconds 400
        if (-not $server.HasExited) { $server.Kill() }
    }
}

Write-Host "`n=== $pass passed, $fail failed ===`n" -ForegroundColor Cyan
if ($fail -gt 0) { exit 1 }
