# Builds the Pi binaries and assembles ./deploy (everything you copy to /srv/toran on the Pi).
#   .\build.ps1              vet + test + build
#   .\build.ps1 -SkipTests   skip "go test" (e.g. when this PC blocks running unsigned test programs)
param([switch]$SkipTests, [string]$Version = '0.2.0')
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$out  = Join-Path $root 'deploy'

Push-Location (Join-Path $root 'server')
try {
  gofmt -l . | ForEach-Object { throw "gofmt needed: $_" }
  go vet ./...
  if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
  if (-not $SkipTests) {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed (use -SkipTests only if the tests cannot run on this machine)' }
  }

  Remove-Item (Join-Path $out 'bin') -Recurse -Force -ErrorAction SilentlyContinue
  New-Item -ItemType Directory -Force (Join-Path $out 'bin') | Out-Null
  # names match `uname -m` on the Pi, so the stack can pick the right file by itself
  $targets = @(
    @{ name = 'aarch64'; arch = 'arm64' },              # Pi 3/4/5 on a 64-bit OS
    @{ name = 'armv7l';  arch = 'arm'; goarm = '7' },   # 32-bit Pi OS
    @{ name = 'armv6l';  arch = 'arm'; goarm = '6' },   # Pi Zero / Pi 1
    @{ name = 'x86_64';  arch = 'amd64' }               # regular PC / VM
  )
  foreach ($t in $targets) {
    $env:GOOS = 'linux'; $env:GOARCH = $t.arch; $env:CGO_ENABLED = '0'
    if ($t.goarm) { $env:GOARM = $t.goarm } else { Remove-Item Env:GOARM -ErrorAction SilentlyContinue }
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $out "bin\toran-$($t.name)") .
    if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.name)" }
  }
}
finally {
  Remove-Item Env:GOOS, Env:GOARCH, Env:GOARM, Env:CGO_ENABLED -ErrorAction SilentlyContinue
  Pop-Location
}

foreach ($f in 'index.html', 'custom.css') { Copy-Item (Join-Path $root $f) $out -Force }
Copy-Item (Join-Path $root 'services.sample.json') (Join-Path $out 'services.json') -Force   # first deploy only
Copy-Item (Join-Path $root 'wallpapers') $out -Recurse -Force
Copy-Item (Join-Path $root 'stack.yml') $out -Force
Get-ChildItem (Join-Path $out 'bin') | Select-Object Name, @{ n = 'MB'; e = { [math]::Round($_.Length / 1MB, 1) } }
Write-Host "`nReady: $out  (copy its contents to /srv/toran on the Pi; on later updates copy only index.html and bin/ so your services.json is kept)"
