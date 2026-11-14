# Configuration

Almost everything is changed in the page: the gear button opens the settings drawer, and the pencil turns on edit mode. Changes save automatically to `services.json`.

## `services.json`

```json
{
  "groups": [
    { "name": "Media", "items": [
      { "name": "Jellyfin", "url": ":8096", "desc": "Movies & TV", "icon": "" }
    ]}
  ],
  "settings": { "layout": "bento", "theme": "nord", "nav": "tabs" }
}
```

- `url` can be a bare port (`:8096` or `8096`, which uses the host you opened the page with) or a full URL.
- `icon` can be empty (look up the app's logo from its name), an emoji, an image URL, or a lowercase app name such as `home-assistant`.

## Settings

| Key | Values |
|---|---|
| `layout` | `grid` (default), `list`, `compact`, `launcher`, `rows`, `bento`, `cubes` |
| `theme` | `dark`, `amoled`, `catppuccin`, `nord`, `dracula`, `gruvbox`, `tokyo`, `rosepine`, `solarized`, `terminal`, `light`, `paper`, `latte`, `solarlight`. Absent means follow the system. |
| `surface` | `solid` (default), `glass`, `outline` |
| `nav` | `sections` (default), `tabs`, `sidebar` |
| `topbar` | `bar` (default), `seamless`, `floating`, `minimal` |
| `featured`, `featuredItem` | Show the banner; which service (default: most recently opened) |
| `recent`, `recentBy`, `recentPos` | Show the row; `recent` or `used`; `bottom` or `top` |
| `wall` | Absent = soft default, `""` = none, `preset:aurora` (also `sunset`, `ocean`, `midnight`, `forest`, `rose`), `wallpapers/<name>.jpg`, an uploaded file, or a URL |
| `dim`, `wallBlur`, `op`, `radius`, `tileMin`, `scale` | Numbers: wallpaper darkening %, blur px, card opacity %, corner radius px, tile size px, UI size % |
| `accent`, `font` | A colour; `system`, `rounded`, `serif`, `mono` |
| `title`, `city` | Page title; city for the weather chip |
| `desc`, `status`, `newTab`, `autoIcons`, `clock24`, `tempF` | Booleans (`desc`, `status`, `autoIcons` default to true) |
| `ss`, `ssMin` | Screensaver `clock`, `aurora`, `stars`, `photo`; minutes idle |

## Server environment variables

| Variable | Default | Meaning |
|---|---|---|
| `ADDR` | `:8080` | Listen address |
| `ROOT` | `.` | Folder holding the page, `services.json`, backups and uploads |
| `LAN_ONLY` | `true` | Only private-network clients may save. `false` allows anyone. |
| `TRUSTED_ORIGINS` | | Comma-separated browser addresses (`scheme://host[:port]`) allowed to save when a reverse proxy changes the `Host` header. See [Behind a reverse proxy](#behind-a-reverse-proxy). |
| `CHECK_HOST` | `127.0.0.1` | Host used for bare-port status checks |
| `GOMEMLIMIT` | | Go runtime soft memory limit, for example `24MiB` |

## HTTP API

| Request | Purpose |
|---|---|
| `GET /api/health` | Server version |
| `GET /api/status` | Health of every service in `services.json` (cached 15 s) |
| `GET /api/backups`, `POST /api/backups/restore` | List and restore backups |
| `PUT /services.json` | Save the config (a backup of the previous one is kept) |
| `PUT` / `DELETE /wallpaper/{name}`, `PUT /icons/{name}` | Uploads |

## Security

- There is no authentication. `LAN_ONLY` limits writes to loopback and private addresses; reads are open.
- **Cross-site protection.** A page on another website cannot make your browser save changes to Toran. Every `PUT`, `POST` and `DELETE` is checked with Go's standard `http.CrossOriginProtection`: browsers send `Sec-Fetch-Site` (on HTTPS and localhost) or `Origin` (on plain HTTP), and a request that comes from another origin gets `403`. Requests with neither header (`curl`, scripts) are let through, because they are not a browser steered by another site; `LAN_ONLY` still applies to them. Toran never sends CORS headers. One limit: when only `Origin` is available the check cannot tell `http://host` from `https://host`; serve Toran over HTTPS with HSTS if that matters to you.
- Uploaded images are served with a sandboxing `Content-Security-Policy` and `nosniff`.
- Only an allow-list of paths is served: the page, `custom.css`, `services.json`, `wallpapers/`, `wallpaper/` and `icons/`. Binaries and backups are not served.

## Behind a reverse proxy

The cross-site check compares the browser's `Origin` with the `Host` header the server receives. If your proxy passes the original `Host` through, nothing needs configuring. If it rewrites it (the server sees `toran:8080` while the browser is on `https://toran.example.org`), saving fails with `403 request blocked`. Fix it either way:

- keep the original host: nginx `proxy_set_header Host $host;`, Caddy does this by default; or
- tell Toran the public address: `TRUSTED_ORIGINS=https://toran.example.org`.

The server logs every blocked request with its `Origin` and `Host`, so you can see what to put in `TRUSTED_ORIGINS`.
