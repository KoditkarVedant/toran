# Build the multi-arch image and push it to Docker Hub.
#   docker login                                   (once)
#   .\publish.ps1 -Image youruser/toran -Version 1.0     pushes :1.0 and :latest
#   .\publish.ps1 -Image youruser/toran -Version 1.0 -NoPush    build only (checks the Dockerfile)
param(
  [Parameter(Mandatory)][string]$Image,      # Docker Hub repository, e.g. youruser/toran
  [Parameter(Mandatory)][string]$Version,    # e.g. 1.0
  [string]$Platforms = 'linux/amd64,linux/arm64,linux/arm/v7',
  [switch]$NoPush, [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
Push-Location (Join-Path $root 'server')
try {
  gofmt -l . | ForEach-Object { throw "gofmt needed: $_" }
  go vet ./...; if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
  if (-not $SkipTests) { go test ./...; if ($LASTEXITCODE -ne 0) { throw 'go test failed (-SkipTests only if this PC cannot run them)' } }
} finally { Pop-Location }

if (-not (docker buildx inspect toran-builder 2>$null)) { docker buildx create --name toran-builder --use | Out-Null } else { docker buildx use toran-builder }
$buildArgs = @('buildx', 'build', '--platform', $Platforms, '--build-arg', "VERSION=$Version", '-t', "${Image}:$Version", '-t', "${Image}:latest")
if ($NoPush) { $buildArgs += @('--output', 'type=cacheonly') } else { $buildArgs += '--push' }
docker @buildArgs $root
if ($LASTEXITCODE -ne 0) { throw 'docker build failed' }
if (-not $NoPush) { Write-Host "Pushed ${Image}:$Version and ${Image}:latest" }
