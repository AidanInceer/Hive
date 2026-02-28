# scripts/build.ps1
# Builds all Docker images into Minikube's Docker daemon and compiles hive.exe.
# Run from the repo root: .\scripts\build.ps1

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Write-Step([string]$msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}

function Assert-Command([string]$name) {
    if (-not (Get-Command $name -ErrorAction SilentlyContinue)) {
        Write-Error "'$name' not found on PATH."
        exit 1
    }
}

Assert-Command 'minikube'
Assert-Command 'docker'

Write-Step 'Pointing Docker client at Minikubes daemon'
minikube docker-env | Invoke-Expression

Write-Step 'Building agent-image:latest'
docker build -t agent-image:latest ./agent
Write-Host 'agent-image:latest built OK' -ForegroundColor Green

Write-Step 'Building orchestrator-image:latest'
docker build -t orchestrator-image:latest ./orchestrator
Write-Host 'orchestrator-image:latest built OK' -ForegroundColor Green

if (Get-Command 'go' -ErrorAction SilentlyContinue) {
    Write-Step 'Compiling hive.exe (Go CLI)'
    Push-Location cli
    go build -o ..\hive.exe .
    Pop-Location
    Write-Host 'hive.exe compiled OK' -ForegroundColor Green

    # Auto-install to ~/.local/bin so "hive" works globally
    $localBin = Join-Path $env:USERPROFILE '.local\bin'
    if (-not (Test-Path $localBin)) { New-Item -ItemType Directory -Path $localBin -Force | Out-Null }
    Copy-Item '.\hive.exe' "$localBin\hive.exe" -Force
    Write-Host "Installed to $localBin\hive.exe" -ForegroundColor Green
} else {
    Write-Warning 'Go not found -- skipping hive.exe. Open a new terminal after installing Go, then run: cd cli; go build -o ..\hive.exe .'
}

# SSH_AUTH_SOCK may not exist on Windows -- suppress that specific error
Write-Step 'Restoring Docker client to host daemon'
$ErrorActionPreference = 'SilentlyContinue'
minikube docker-env --unset | Invoke-Expression
$ErrorActionPreference = 'Stop'

Write-Host ''
Write-Host 'Build complete. Run .\scripts\deploy.ps1 to deploy to Kubernetes.' -ForegroundColor Green
