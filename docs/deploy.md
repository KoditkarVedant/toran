# Deploy

Two ways: the published Docker image (simplest), or plain binaries copied to the Pi (below).

## Run the Docker image

```sh
docker run -d --name toran --restart unless-stopped -p 8080:8080 -v toran-data:/data youruser/toran:latest
```

Or in Portainer, a stack with `image: youruser/toran:latest`, `ports: ["8080:8080"]` and a volume on `/data`. Then open `http://<host>:8080`.

- `/data` holds the accounts (`users.json`, `secret.key`) and each user's dashboard, backups and uploads. The page files in it are refreshed from the image on every start, so updating is `docker pull` and a restart. Your data is kept.
- **First start: create the admin.** Read the setup code from the log with `docker logs toran`, open `http://<host>:8080` and fill in the code, a user name and a password. Details, users and the "forgot my password" command: [Accounts and logging in](configuration.md#accounts-and-logging-in).
- **Updating from a version without accounts:** the first admin you create takes over your existing dashboard (it is copied, not moved).
- Publish your own build with `publish.ps1` ([Development](development.md#publish-to-docker-hub)).
- **The container does not run as root.** The server runs as user `65532`, and a new volume mounted on `/data` is created with that owner, so a fresh install needs nothing. A volume or folder that already exists and belongs to root (made by an older image, or a Linux bind mount that Docker created) cannot be written, and the container stops with a message saying so. Hand it over once:

  ```sh
  docker run --rm -v toran-data:/data --user 0 --entrypoint chown youruser/toran:latest -R 65532:65532 /data
  ```

  For a bind mount, `sudo chown -R 65532:65532 ./your-folder` does the same. Or keep the folder as it is and run the container as its owner with `--user <uid>:<gid>` (in compose: `user: "1000:1000"`). Docker Desktop on Windows and macOS needs none of this.
- Everything personal needs a login, and saving also needs a private-network address (`LAN_ONLY`). Behind Docker's default bridge network clients appear as private addresses, so `LAN_ONLY` alone would not protect an instance you expose to the internet; the login does. Toran does not encrypt traffic, so for anything beyond your home network put an HTTPS reverse proxy in front ([Behind a reverse proxy](configuration.md#behind-a-reverse-proxy)).

# Deploy with Portainer and plain binaries

Toran runs as one small container. The stack uses the plain `alpine` image and runs the Go binary from a folder on the Pi, so there is no registry and no build on the Pi.

## 1. Build the binaries

On a PC with [Go](https://go.dev/dl/) installed (PowerShell):

```powershell
.\build.ps1
```

This runs formatting and vet checks and the Go tests, then writes `deploy\` containing the page, the wallpapers and four binaries named after `uname -m` on the Pi:

| File | Pi |
|---|---|
| `toran-aarch64` | Pi 3/4/5 on a 64-bit OS |
| `toran-armv7l` | 32-bit Pi OS |
| `toran-armv6l` | Pi Zero / Pi 1 |
| `toran-x86_64` | A regular PC or VM |

If your machine blocks running freshly compiled test programs, use `.\build.ps1 -SkipTests` and run `go test ./...` in `server/` elsewhere.

## 2. Copy to the Pi

Copy the contents of `deploy\` to `/srv/toran`. On later updates copy only `index.html` and `bin/`, so your `services.json` is kept.

## 3. Create the stack

In Portainer: **Stacks → Add stack → Web editor**, paste `stack.yml`, **Deploy the stack**. Then open `http://<your-pi>:8080`.

Check the server is up: `http://<your-pi>:8080/api/health` should answer `{"ok":true,...}`.

## Notes

- **This path runs as root.** The stack runs the binary in a plain `alpine` container with the folder mounted, so the server has the permissions of the container's default user. The Docker image above runs as an ordinary user; prefer it if you can.
- **Host networking** lets the server check your services on `localhost` and see real client addresses. The port comes from `ADDR`.
- **Memory:** `GOMEMLIMIT` and `mem_limit` in the stack keep the footprint small.
- **Already running another stack on 8080?** Change `ADDR`.
- **No Go server?** The page also works behind plain nginx using [`contrib/nginx.conf`](../contrib/nginx.conf). You lose backups and server-side status checks.
