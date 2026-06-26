$ErrorActionPreference = "Stop"
$env:GOPROXY = "https://goproxy.io,direct"
$root = $PSScriptRoot

$ver = (Select-String -Path "$root\app.go" -Pattern 'AppVersion\s*=\s*"([^"]+)"').Matches.Groups[1].Value
Write-Host "=== WinDTT v$ver build ===" -ForegroundColor Cyan

# Embed stubs (go:embed requires files to exist for go mod tidy)
if (-not (Test-Path "$root\wdtt-client.exe")) {
    [byte[]]$mz = @(0x4D,0x5A) + (New-Object byte[] 62)
    [IO.File]::WriteAllBytes("$root\wdtt-client.exe", $mz)
}
if (-not (Test-Path "$root\assets\server\wdtt-server")) {
    New-Item -ItemType Directory -Force -Path "$root\assets\server" | Out-Null
    [byte[]]$elf = @(0x7F,0x45,0x4C,0x46) + (New-Object byte[] 60)
    [IO.File]::WriteAllBytes("$root\assets\server\wdtt-server", $elf)
}

# 1. go_client
Write-Host "[1/3] go_client..." -ForegroundColor Yellow
Set-Location "$root\go_client"
go mod tidy
Remove-Item -Force -ErrorAction SilentlyContinue "$root\wdtt-client.exe"
go build -ldflags="-s -w" -o "$root\wdtt-client.exe" .
if ($LASTEXITCODE -ne 0) { Write-Host "FAIL: go_client" -ForegroundColor Red; exit 1 }
Write-Host "  OK" -ForegroundColor Green

# 2. wdtt-server
Write-Host "[2/3] wdtt-server..." -ForegroundColor Yellow
Set-Location "$root\server_src"
$env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go mod tidy
Remove-Item -Force -ErrorAction SilentlyContinue "$root\assets\server\wdtt-server"
go build -ldflags="-s -w" -o "$root\assets\server\wdtt-server" .
$env:GOOS = ""; $env:GOARCH = ""; $env:CGO_ENABLED = ""
if ($LASTEXITCODE -ne 0) { Write-Host "FAIL: wdtt-server" -ForegroundColor Red; exit 1 }
Write-Host "  OK" -ForegroundColor Green

# 3. wails
Write-Host "[3/3] wails build..." -ForegroundColor Yellow
Set-Location $root
go mod tidy
wails build -ldflags "-s -w"
if ($LASTEXITCODE -ne 0) { Write-Host "FAIL: wails build" -ForegroundColor Red; exit 1 }

$src = "$root\build\bin\WinDTT.exe"
$dst = "$root\build\bin\WinDTT-v$ver.exe"
Rename-Item $src $dst -Force
Write-Host "=== Done: $dst ===" -ForegroundColor Green
