# scripts/tunnel.ps1
# Opens a localhost tunnel to the Hive orchestrator.
# Keep this terminal running while using hive.exe.
#
# In another terminal:
#   $env:HIVE_URL = "http://localhost:30800"
#   .\hive.exe "your task"

Write-Host ""
Write-Host "Hive tunnel open at http://localhost:30800" -ForegroundColor Green
Write-Host "Keep this terminal running. Press Ctrl+C to stop." -ForegroundColor Yellow
Write-Host ""

kubectl port-forward service/hive-orchestrator 30800:9000 -n hive
