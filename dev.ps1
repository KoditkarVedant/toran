# Run Toran locally to check changes: the page and the Go server from this folder.
#   .\dev.ps1                run on http://localhost:8787
#   .\dev.ps1 -Port 9090     another port
#   .\dev.ps1 -Test          run the page tests and the server checks first
# Edit index.html and refresh the browser: the server reads it from disk on every request.
# Your own services.json, backups and uploads are created here and ignored by git.
param([int]$Port = 8787, [switch]$Test)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
if ($Test) {
  Push-Location (Join-Path $root 'tests')
  try { if (-not (Test-Path node_modules)) { npm install }; npm test; if ($LASTEXITCODE -ne 0) { throw 'page tests failed' } } finally { Pop-Location }
  Push-Location (Join-Path $root 'server')
  try { gofmt -l . | ForEach-Object { throw "gofmt needed: $_" }; go vet ./...; if ($LASTEXITCODE -ne 0) { throw 'go vet failed' } } finally { Pop-Location }
}
if (-not (Test-Path (Join-Path $root 'services.json'))) { Copy-Item (Join-Path $root 'services.sample.json') (Join-Path $root 'services.json') }
if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
  throw "Port $Port is already in use. Pick another: .\dev.ps1 -Port 9090"
}
$env:ROOT = $root; $env:ADDR = ":$Port"
Write-Host "Toran on http://localhost:$Port  (Ctrl+C to stop)"
Push-Location (Join-Path $root 'server')
try { go run . } finally { Pop-Location }
