# scripts/deploy.ps1
# Applies all Kubernetes manifests to Minikube and waits for rollout.
# Run from the repo root: .\scripts\deploy.ps1

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

Assert-Command 'kubectl'

Write-Step 'Applying namespace'
kubectl apply -f k8s/namespace.yaml

Write-Step 'Applying ConfigMap and Secret'
kubectl apply -f k8s/configmap.yaml -n hive
kubectl apply -f k8s/secret.yaml    -n hive

Write-Step 'Applying agent Deployments and Services'
kubectl apply -f k8s/agents/ -n hive

Write-Step 'Applying orchestrator Deployment and Service'
kubectl apply -f k8s/orchestrator/ -n hive

Write-Step 'Waiting for all Deployments to be ready'
$deployments = @(
    'agent-code-reviewer',
    'agent-planner',
    'agent-researcher',
    'agent-debugger',
    'agent-architect',
    'hive-orchestrator'
)

foreach ($d in $deployments) {
    Write-Host "  Waiting for $d ..."
    kubectl rollout status deployment/$d -n hive --timeout=120s
}

Write-Step 'All pods ready'
kubectl get pods -n hive

Write-Host ''
$nodeIp = minikube ip
Write-Host "Orchestrator available at: http://${nodeIp}:30800" -ForegroundColor Green
Write-Host ''
Write-Host 'Quick test (open a new terminal):'
Write-Host "  `$env:HIVE_URL = 'http://${nodeIp}:30800'" -ForegroundColor Yellow
Write-Host "  .\hive.exe 'Plan and review a Python login endpoint'" -ForegroundColor Yellow
