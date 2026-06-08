# Downloads ffmpeg.exe into assets/ so the build can embed it.
# Only contributors who want to build from source need to run this.
# End users just download QuickClip.exe from the Releases page.

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$assets = Join-Path $root 'assets'
$dest = Join-Path $assets 'ffmpeg.exe'

if (Test-Path $dest) {
    Write-Host "assets/ffmpeg.exe already exists. Delete it if you want to re-download."
    exit 0
}

New-Item -ItemType Directory -Force -Path $assets | Out-Null

$tmp = Join-Path $env:TEMP "quickclip-ffmpeg-$([guid]::NewGuid().ToString('N'))"
$zip = "$tmp.zip"

Write-Host "Downloading ffmpeg essentials build (about 100 MB)..."
$ProgressPreference = 'SilentlyContinue'
Invoke-WebRequest -Uri 'https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip' -OutFile $zip -UseBasicParsing

Write-Host "Extracting..."
Expand-Archive -LiteralPath $zip -DestinationPath $tmp -Force

$src = Get-ChildItem -Path $tmp -Recurse -Filter ffmpeg.exe | Select-Object -First 1
if (-not $src) { throw "ffmpeg.exe not found in archive" }
Copy-Item -LiteralPath $src.FullName -Destination $dest -Force

Remove-Item -LiteralPath $zip -Force
Remove-Item -LiteralPath $tmp -Recurse -Force

$size = [math]::Round((Get-Item $dest).Length / 1MB, 1)
Write-Host "Done. assets/ffmpeg.exe ($size MB)"
