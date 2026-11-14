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

```powershell
.\dev.ps1            # http://localhost:8787
.\dev.ps1 -Test      # run the page tests and the server checks first
.\dev.ps1 -Port 9090   # if 8787 is taken
```

This runs the real Go server against this folder. Edit `index.html` and refresh the browser; the server reads it from disk on every request. A `services.json` created here is ignored by git.

To try the Docker image instead: `docker compose up --build`, then open `http://localhost:8080`. Data goes to `./.data`.

## Build for the Pi

`.\build.ps1` cross-compiles for 64-bit ARM, 32-bit ARM (v7 and v6) and x86-64. Nothing is run on the build machine.

## Publish to Docker Hub

```powershell
docker login
.\publish.ps1 -Image youruser/toran -Version 1.0
```

This vets and tests the server, then builds one multi-arch image (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) and pushes it as `:1.0` and `:latest`. Add `-NoPush` to build without pushing, which checks the Dockerfile. See [Deploy](deploy.md#run-the-docker-image).

## Conventions

- Small, focused commits with an imperative subject (`feat:`, `fix:`, `docs:`, `test:`, `build:`, `chore:`).
- Keep the page dependency-free and the server standard-library-only; the footprint is a feature.
- User data (`services.json`, `backups/`, uploads) is never committed.
