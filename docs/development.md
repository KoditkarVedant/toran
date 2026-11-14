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
.\dev.ps1 -Docker     # run the Go server in a container instead (see below)
```

This runs the real Go server against this folder. Edit `index.html` and refresh the browser; the server reads it from disk on every request. A `services.json` created here is ignored by git.

**If Windows blocks the server.** Some machines (Smart App Control, WDAC, AppLocker) refuse to run a program that was just compiled, so `go run` and `go test` fail with "An Application Control policy has blocked this file". Use `.\dev.ps1 -Docker`: it runs the same server in a Go container with this folder mounted, so editing `index.html` still shows on refresh (restart after changing Go code). With `-Test` it also runs gofmt, `go vet` and `go test` inside the container. It needs Docker, and the first run downloads the Go image.

To try the Docker image instead: `docker compose up --build`, then open `http://localhost:8080`. Data goes to `./.data`.

## Build for the Pi

`.\build.ps1` cross-compiles for 64-bit ARM, 32-bit ARM (v7 and v6) and x86-64. Nothing is run on the build machine.

## Publish to Docker Hub

```powershell
docker login
.\publish.ps1 -Image youruser/toran -Version 0.1.0
```

This vets and tests the server, then builds one multi-arch image (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) and pushes it as `:0.1.0` and `:latest`. Add `-NoPush` to build without pushing, which checks the Dockerfile. See [Deploy](deploy.md#run-the-docker-image).

## Conventions

- Small, focused commits with an imperative subject (`feat:`, `fix:`, `docs:`, `test:`, `build:`, `chore:`).
- Keep the page dependency-free and the server standard-library-only; the footprint is a feature.
- User data (`services.json`, `backups/`, uploads) is never committed.

## Releases and CI

Every pull request runs `.github/workflows/ci.yml`: gofmt, `go vet`, `go test -race`, `govulncheck`, the page tests, and a build of the multi-arch image (no push).

`.github/workflows/release.yml` publishes:

| Trigger | Result |
|---|---|
| Push to `main` (after CI passes) | Image `ghcr.io/<owner>/toran:edge` |
| Tag `v0.2.0` | Images `:0.2.0`, `:0.2` and `:latest`, and a GitHub Release with the Pi binaries (`toran-aarch64`, `-armv7l`, `-armv6l`, `-x86_64`), `SHA256SUMS` and the page files |
| Tag `v0.2.0-rc.1` | Image `:0.2.0-rc.1` and a GitHub prerelease; never `:latest` |

To cut a release: move the `[Unreleased]` notes in `CHANGELOG.md` under the new version, commit, then

```sh
git tag v0.2.0
git push origin v0.2.0
```

The version number comes from the tag. It is passed to the build with `-ldflags "-X main.version=..."` and shows up in `GET /api/health`. The `0.1.0` in the source is only the fallback for a plain `go build`.

**Docker Hub (optional).** Images always go to GitHub Container Registry using the built-in `GITHUB_TOKEN`. To also push to Docker Hub, add the repository variable `DOCKERHUB_IMAGE` (for example `youruser/toran`) and the secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.

**First time on GitHub.** Under *Settings → Actions → General → Workflow permissions* leave the default read-only setting; the workflows ask for the extra permissions they need. After the first image is pushed, make the package public under the repository's *Packages* if you want others to pull it.

