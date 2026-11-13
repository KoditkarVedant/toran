# Toran

A fast, good-looking start page for your home server. One tile per service, so you stop remembering ports.

*Toran* is the ceremonial arched gateway at the entrance of an Indian home or temple. This is the gateway to your self-hosted apps.

> **Status: pre-release.** The page is tested in a simulated browser. The Go server has unit tests, but they have not been run yet and it has not been run on a real Pi. Treat the first deploy as the first test.

## What it does

- **Looks:** 7 layouts (grid, list, compact, launcher, TV rows, bento, cubes), 14 colour themes (dark and light), solid / glass / outline cards, four top bar styles, and ready-made *Looks* that combine them. All of it is changed live from a settings drawer.
- **Wallpapers:** bundled nature photos, gradient presets, or upload your own. Optional screensaver (clock, aurora, starfield).
- **Components:** group tabs or sidebar, a featured banner, a *Recent / Most used* row, open links in a new tab, TV-friendly UI size, arrow-key navigation.
- **App logos:** looked up from the service name (via the open [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) set) and cached on your server.
- **Editing:** add, edit, delete and drag to reorder in the page itself. Nothing to hand-edit.
- **Go server (optional, ~7 MB):** saves your config, keeps dated backups, checks service health from the server. Without it the page still works behind plain nginx.

## Quick start (Portainer)

1. Build the Pi binaries (needs [Go](https://go.dev/dl/)): `.\build.ps1` (PowerShell). This creates `deploy\`.
2. Copy the contents of `deploy\` to `/srv/toran` on the Pi.
3. In Portainer: **Stacks → Add stack → Web editor**, paste [`stack.yml`](stack.yml), **Deploy**.
4. Open `http://<your-pi>:8080`.

Details: [docs/deploy.md](docs/deploy.md).

## Security

There is **no login yet**. Saving is limited to private-network clients by default (`LAN_ONLY`), but anyone on your network can change the page. Do not expose it to the internet. See [docs/configuration.md](docs/configuration.md).

## Repository layout

| Path | What |
|---|---|
| `index.html` | The whole page (HTML, CSS, JS). No build step, no dependencies. |
| `server/` | Go server (standard library only) and its tests |
| `tests/` | Simulated-browser tests for the page (`npm test`) |
| `wallpapers/` | Bundled photos, with credits in `CREDITS.md` |
| `contrib/nginx.conf` | Config for running the page behind nginx with no Go server |
| `build.ps1`, `stack.yml` | Build the Pi binaries; the Portainer stack |
| `docs/` | Documentation site source |

## Credits

Wallpapers are from Wikimedia Commons under CC BY-SA; see [`wallpapers/CREDITS.md`](wallpapers/CREDITS.md). App logos come from [dashboard-icons](https://github.com/homarr-labs/dashboard-icons). Weather (optional) is from [Open-Meteo](https://open-meteo.com).

## License

[MIT](LICENSE) for the code. The bundled wallpapers are **not** covered by it: each keeps its own CC BY-SA licence and attribution, listed in [`wallpapers/CREDITS.md`](wallpapers/CREDITS.md).
