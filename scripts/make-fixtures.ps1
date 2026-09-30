# Builds a small but realistic music library for local testing.
#   powershell -File scripts/make-fixtures.ps1
#
# Produces three formats and both cover-art layouts, because those are the
# cases most likely to break the scanner:
#   - FLAC with a sidecar cover.jpg  (the usual tag-ripped layout)
#   - MP3 with art embedded in the ID3 tag
#   - WAV, which is uncompressed and the format the quality picker must
#     always avoid sending over the wire

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$music = Join-Path $root '.local-test\music'

if (Test-Path $music) { Remove-Item $music -Recurse -Force }

function New-Dir($path) {
    New-Item -ItemType Directory -Path $path -Force | Out-Null
    $path
}

# --- album 1: FLAC + sidecar cover ---
$dir1 = New-Dir (Join-Path $music 'Nina Simone\Pastel Blues')
$notes = @(@(1, 'Sinnerman', 1965), @(2, 'Blue in Green', 1965), @(3, 'Feeling Good', 1965))
foreach ($n in $notes) {
    $num = '{0:d2}' -f $n[0]
    $out = Join-Path $dir1 "$num - $($n[1]).flac"
    # Copy the codec from a lossless source so the duration survives intact;
    # re-encoding a raw tone straight to FLAC is fine too, but going through
    # WAV keeps the pipeline identical to how a real rip is built.
    & ffmpeg -v error -y -f lavfi -i "sine=frequency=300:duration=6" `
        -c:a flac `
        -metadata "title=$($n[1])" `
        -metadata "artist=Nina Simone" `
        -metadata "album=Pastel Blues" `
        -metadata "track=$($n[0])" `
        -metadata "date=$($n[2])" `
        $out 2>&1 | Out-Null
}
& ffmpeg -v error -y -f lavfi -i "gradients=size=800x800:duration=1:c0=0x1a2f6b:c1=0x8b3a5f" `
    -frames:v 1 (Join-Path $dir1 'cover.jpg') 2>&1 | Out-Null

[System.IO.File]::WriteAllLines((Join-Path $dir1 '01 - Sinnerman.lrc'), @(
    '[ti:Sinnerman]',
    '[ar:Nina Simone]',
    '',
    '[00:00.50]Lord, life has been good to me',
    '[00:05.00]More than I can tell you',
    '[00:09.50]And I have lived'
))

# --- album 2: MP3 with art embedded in the ID3 tag ---
$dir2 = New-Dir (Join-Path $music 'Boards of Canada\Geogaddi')
# ffmpeg infers the output format from the extension, so the temporary cover
# needs a real one rather than New-TemporaryFile's .tmp.
$coverDir = New-Dir (Join-Path $music '.fixture-art')
$cover = Join-Path $coverDir 'cover.jpg'
& ffmpeg -v error -y -f lavfi -i "gradients=size=800x800:duration=1:c0=0x0d3b2e:c1=0x4a5c1a" `
    -frames:v 1 $cover 2>&1 | Out-Null
foreach ($n in @(@(1, 'Music Is Math'), @(2, 'Daylight Sine'))) {
    $num = '{0:d2}' -f $n[0]
    $mp3 = Join-Path $dir2 "$num - $($n[1]).mp3"
    & ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=6" `
        -c:a libmp3lame -b:a 192k `
        -metadata "title=$($n[1])" `
        -metadata "artist=Boards of Canada" `
        -metadata "album=Geogaddi" `
        -metadata "track=$($n[0])" `
        $mp3 2>&1 | Out-Null
    # Attach the cover to the first track only, which is the realistic case.
    # ffmpeg cannot write to the file it is reading, so this goes via a
    # temporary file in the same directory and is then moved into place.
    if ($n[0] -eq 1) {
        $staged = Join-Path $dir2 'staged.mp3'
        & ffmpeg -v error -y -i $mp3 -i $cover `
            -map 0:a -map 1:v -c copy `
            -id3v2_version 3 `
            -metadata:s:v title="Album cover" `
            -metadata:s:v comment="Cover (front)" `
            $staged 2>&1 | Out-Null
        Move-Item -LiteralPath $staged -Destination $mp3 -Force
    }
}

# --- album 3: WAV, uncompressed ---
$dir3 = New-Dir (Join-Path $music 'Max Richter\On the Nature of Daylight')
& ffmpeg -v error -y -f lavfi -i "sine=frequency=220:duration=4" `
    -c:a pcm_s16le `
    -metadata "title=On the Nature of Daylight" `
    -metadata "artist=Max Richter" `
    -metadata "album=On the Nature of Daylight" `
    -metadata "track=1" `
    (Join-Path $dir3 '01 - On the Nature of Daylight.wav') 2>&1 | Out-Null

# --- verify the fixtures are actually decodable ---
Write-Host "`nVerifying fixtures with ffprobe:" -ForegroundColor Cyan
$all = Get-ChildItem -Recurse -File -Path $music -Include *.flac, *.mp3, *.wav
foreach ($f in $all) {
    $probe = & ffprobe -v error -show_entries format=duration -of csv=p=0 $f.FullName 2>&1
    $dur = [math]::Round([double]$probe, 2)
    $status = if ($dur -ge 3.9 -and $dur -le 6.1) { 'ok' } else { 'BAD DURATION' }
    Write-Host ("  {0,-42} {1,7}s  {2}" -f $f.Name, $dur, $status) -ForegroundColor $(if ($status -eq 'ok') { 'Green' } else { 'Red' })
}
Write-Host "`n$($all.Count) audio files in $music`n" -ForegroundColor Cyan
