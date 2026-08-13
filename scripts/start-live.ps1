param(
    [string]$TikTokUsername = "quartermeat",
    [int]$ArenaPort = 8090,
    [switch]$SkipBrowser,
    [switch]$SkipAdapter
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$connectorRoot = "D:\Codex\Projects\work\tiktok-connector"
$python = "C:\Users\jerem\scratch\venvs\tiktok-live-adapter\Scripts\python.exe"
$pwsh = "C:\Program Files\PowerShell\7\pwsh.exe"
if (-not (Test-Path -LiteralPath $pwsh)) { $pwsh = "powershell.exe" }
if (-not (Test-Path -LiteralPath $python)) { $python = "python" }

function Test-Port([int]$Port) {
    return [bool](Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue)
}

function Wait-Port([int]$Port, [int]$Seconds = 20) {
    for ($i = 0; $i -lt ($Seconds * 2); $i++) {
        if (Test-Port $Port) { return }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for port $Port."
}

if (-not (Test-Port 8787)) {
    Start-Process -FilePath $pwsh -ArgumentList @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
        (Join-Path $connectorRoot "scripts\run.ps1")
    ) -WorkingDirectory $connectorRoot -WindowStyle Normal | Out-Null
    Wait-Port 8787
    Write-Host "TikTok connector ready on http://127.0.0.1:8787"
} else {
    Write-Host "TikTok connector already running on port 8787"
}

if (-not $SkipAdapter) {
    if (-not $TikTokUsername -or $TikTokUsername -notmatch '^[A-Za-z0-9._-]+$') {
        throw "TikTokUsername must be a public TikTok handle, without the @ symbol."
    }
    $env:TIKTOK_USERNAME = $TikTokUsername
    $adapterRunning = Get-CimInstance Win32_Process -Filter "Name = 'python.exe'" |
        Where-Object { $_.CommandLine -like '*tiktok_live_adapter.py*' }
    if (-not $adapterRunning) {
        Start-Process -FilePath $python -ArgumentList @(
            "-u", (Join-Path $connectorRoot "adapters\tiktok_live_adapter.py")
        ) -WorkingDirectory $connectorRoot -WindowStyle Normal | Out-Null
        Write-Host "TikTok LIVE adapter starting for @$TikTokUsername"
    } else {
        Write-Host "TikTok LIVE adapter already running"
    }
}

if (-not (Test-Port $ArenaPort)) {
    $env:PORT = "$ArenaPort"
    Start-Process -FilePath "C:\Python311\python.exe" -ArgumentList @(
        (Join-Path $root "server.py")
    ) -WorkingDirectory $root -WindowStyle Hidden | Out-Null
    Wait-Port $ArenaPort
    Write-Host "Arena server ready on http://localhost:$ArenaPort"
} else {
    Write-Host "Arena server already running on port $ArenaPort"
}

if (-not $SkipBrowser) {
    Add-Type -AssemblyName System.Windows.Forms
    $screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
    $margin = 40
    $availableW = $screen.Width - $margin
    $availableH = $screen.Height - $margin
    $ratio = 1080.0 / 1980.0
    $windowW = [math]::Min($availableW, [math]::Floor($availableH * $ratio))
    $windowH = [math]::Floor($windowW / $ratio)
    $url = "http://localhost:$ArenaPort/stream-wasm?native=1"
    Start-Process -FilePath "C:\Program Files\Google\Chrome\Application\chrome.exe" -ArgumentList @(
        "--new-window", "--app=$url", "--window-size=$windowW,$windowH"
    ) | Out-Null
    Write-Host "Stream window opened at ${windowW}x${windowH} for monitor $($screen.Width)x$($screen.Height)"
}

Write-Host "Live setup ready. Open http://localhost:$ArenaPort/control for controls."
