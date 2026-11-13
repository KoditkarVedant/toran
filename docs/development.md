# Development

## Layout

- `index.html` is the whole page: HTML, CSS and JavaScript in one file, no build step, no dependencies.
- `server/main.go` is the Go server (standard library only). `server/main_test.go` tests it.
- `tests/` holds simulated-browser tests for the page (jsdom).

## Run the checks

```powershell
# page
cd tests; npm install; npm test

# server
cd server; gofmt -l .; go vet ./...; go test ./...
```

`npm test` runs two suites: the page on its own, and the page against a stand-in for the server's API (status, backups).

The server tests cover config saving, backups and restore, upload validation, the path allow-list, LAN-only writes and status checks.

## Run it locally

Any static server works for the page alone. For the full server:

```powershell
cd server
$env:ROOT = (Resolve-Path ..).Path
go run .
```

then open `http://localhost:8080`. A real `services.json` created there is ignored by git.

## Build for the Pi

`.\build.ps1` cross-compiles for 64-bit ARM, 32-bit ARM (v7 and v6) and x86-64. Nothing is run on the build machine.

## Conventions

- Small, focused commits with an imperative subject (`feat:`, `fix:`, `docs:`, `test:`, `build:`, `chore:`).
- Keep the page dependency-free and the server standard-library-only; the footprint is a feature.
- User data (`services.json`, `backups/`, uploads) is never committed.
