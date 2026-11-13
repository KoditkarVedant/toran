# Deploy with Portainer

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

- **Host networking** lets the server check your services on `localhost` and see real client addresses. The port comes from `ADDR`.
- **Memory:** `GOMEMLIMIT` and `mem_limit` in the stack keep the footprint small.
- **Already running another stack on 8080?** Change `ADDR`.
- **No Go server?** The page also works behind plain nginx using [`contrib/nginx.conf`](../contrib/nginx.conf). You lose backups and server-side status checks.
