# Installer for ofs-gateway-agent (Oldtian's FRP Service) on Windows.
#
# Usage (run in an elevated PowerShell):
#   ./install.ps1 [-BinSrc path\to\ofs-gateway-agent.exe]
#
# Installs the binary + config under %ProgramData%\oldtians-frp-service and
# prints how to run it (optionally as a service via nssm / sc.exe).

param(
    [string]$BinSrc = "$PSScriptRoot\ofs-gateway-agent.exe"
)

$ErrorActionPreference = "Stop"

$InstallDir = Join-Path $env:ProgramData "oldtians-frp-service"
$BinName    = "ofs-gateway-agent.exe"
$BinDest    = Join-Path $InstallDir $BinName
$ConfigDest = Join-Path $InstallDir "config.yaml"
$ExampleSrc = Join-Path $PSScriptRoot "..\config.example.yaml"

if (-not (Test-Path $BinSrc)) {
    Write-Error "agent binary not found at $BinSrc. Build it first: (cd agent; go build -o $BinName ./cmd/gateway-agent)"
}

Write-Host ">> creating $InstallDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

Write-Host ">> installing $BinName"
Copy-Item -Force $BinSrc $BinDest

if (-not (Test-Path $ConfigDest)) {
    if (Test-Path $ExampleSrc) {
        Copy-Item $ExampleSrc $ConfigDest
        Write-Host ">> wrote example config to $ConfigDest -- EDIT IT before starting"
    } else {
        Write-Host "!! config.example.yaml not found; create $ConfigDest manually"
    }
} else {
    Write-Host ">> $ConfigDest already exists, leaving it untouched"
}

Write-Host ""
Write-Host "Run the agent with:"
Write-Host "    & '$BinDest' -config '$ConfigDest'"
Write-Host ""
Write-Host "To run it as a Windows service, install nssm (https://nssm.cc) then:"
Write-Host "    nssm install OldtiansFrpService '$BinDest' -config '$ConfigDest'"
Write-Host "    nssm start OldtiansFrpService"
Write-Host ""
Write-Host "Remember to place frpc.exe at the bin_path configured in config.yaml."
