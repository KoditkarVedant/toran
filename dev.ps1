# Run Toran locally to check changes: the page and the Go server from this folder.
#   .\dev.ps1                run on http://localhost:8787
#   .\dev.ps1 -Port 9090     another port
#   .\dev.ps1 -Test          run the page tests and the server checks first
#   .\dev.ps1 -Docker        build and run the Go server inside a container instead of on this PC
#                            (use it when Windows blocks freshly built programs, e.g. Smart App Control)
# Edit index.html and refresh the browser: the server reads it from disk on every request.
# Your own services.json, backups and uploads are created here and ignored by git.
param([int]$Port = 8787, [switch]$Test, [switch]$Docker)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$goImage = 'golang:1.24-alpine'   # keep in step with the builder image in the Dockerfile

if ($Docker -and -not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'docker was not found. Install Docker Desktop, or run without -Docker.' }

# The dev container has a fixed name so it can be stopped reliably. Ctrl+C ends the docker
# command in this window but not always the container, which would keep the port taken.
$devName = "toran-dev-$Port"
function Stop-DevContainer { if (docker ps -aq --filter "name=^$devName$") { docker rm -f $devName | Out-Null } }
if ($Docker) { Stop-DevContainer }   # a leftover from an earlier run

# Run a shell command in the Go container with the repo mounted at /app.
# The Go build cache is kept in a volume, so only the first run is slow.
function Invoke-InGo([string]$Script, [string[]]$Extra = @()) {
  docker run --rm --init -v "${root}:/app" -v toran-gocache:/root/.cache/go-build -v toran-gomod:/go/pkg/mod -w /app/server @Extra $goImage sh -c $Script | Out-Host   # Out-Host: print it, don't let it become the function's return value
  return $LASTEXITCODE
}

if ($Test) {
  Push-Location (Join-Path $root 'tests')
  try { if (-not (Test-Path node_modules)) { npm install }; npm test; if ($LASTEXITCODE -ne 0) { throw 'page tests failed' } } finally { Pop-Location }
  if ($Docker) {
    # inside the container the server tests can run too
    if ((Invoke-InGo 'test -z "$(gofmt -l .)" && go vet ./... && go test ./...') -ne 0) { throw 'server checks failed (gofmt, go vet or go test)' }
  } else {
    Push-Location (Join-Path $root 'server')
    try { gofmt -l . | ForEach-Object { throw "gofmt needed: $_" }; go vet ./...; if ($LASTEXITCODE -ne 0) { throw 'go vet failed' } } finally { Pop-Location }
  }
}
if (-not (Test-Path (Join-Path $root 'services.json'))) { Copy-Item (Join-Path $root 'services.sample.json') (Join-Path $root 'services.json') }
if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
  throw "Port $Port is already in use. Pick another: .\dev.ps1 -Port 9090"
}

Write-Host "Toran on http://localhost:$Port  (Ctrl+C to stop)"
if ($Docker) {
  try { $null = Invoke-InGo 'exec go run .' @('--name', $devName, '-p', "${Port}:8080", '-e', 'ROOT=/app', '-e', 'ADDR=:8080') }
  finally { Stop-DevContainer }
  return
}

$env:ROOT = $root; $env:ADDR = ":$Port"
Push-Location (Join-Path $root 'server')
$started = Get-Date
try { go run . } finally { Pop-Location }
if ($LASTEXITCODE -ne 0 -and ((Get-Date) - $started).TotalSeconds -lt 15) {
  Write-Host "`nThe server stopped right after starting. If the message above mentions an Application Control policy," -ForegroundColor Yellow
  Write-Host "Windows blocked the freshly built program. Run it in a container instead:  .\dev.ps1 -Docker" -ForegroundColor Yellow
}
