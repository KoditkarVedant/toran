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
| `TRUSTED_PROXIES` | | Comma-separated IP addresses and CIDR ranges of reverse proxies whose `X-Forwarded-For` header is believed. See [Behind a reverse proxy](#behind-a-reverse-proxy). |
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
- **Status checks stay on safe ground.** The server fetches every URL in `services.json` to show its health, and anyone who may save can edit that list. So the checker refuses link-local addresses (this includes the cloud-metadata address `169.254.169.254`), `0.0.0.0` and multicast, judged at the moment of connecting so DNS tricks do not help; it follows no redirects, ignores proxy environment variables and only speaks `http` and `https`. Services on `localhost`, your home network and the public internet are checked as usual. HTTPS certificates are not verified, because home services often use self-signed ones and only the status code is read (no credentials are sent).
- Uploaded images are served with a sandboxing `Content-Security-Policy` and `nosniff`.
- Only an allow-list of paths is served: the page, `custom.css`, `services.json`, `wallpapers/`, `wallpaper/` and `icons/`. Binaries and backups are not served.

## Behind a reverse proxy

Two things change when a proxy (nginx, Caddy, Traefik, a Cloudflare tunnel...) sits in front of Toran, and each has a setting.

**1. Who is the visitor? `TRUSTED_PROXIES`.** Toran sees the proxy's address, not the visitor's, so `LAN_ONLY` would judge the proxy. List your proxy and Toran reads the visitor from the `X-Forwarded-For` header instead:

```
TRUSTED_PROXIES=127.0.0.1                # proxy on the same machine
TRUSTED_PROXIES=172.18.0.0/16            # proxy in the same Docker network (see: docker network inspect)
```

The header is only believed when the connection comes from a listed proxy, and it is read from the right, the end your proxy writes. Entries a visitor adds on the left are never used. An unreadable header is refused. Without this setting the header is ignored completely, which is the safe default. Never list a range that a visitor could be in.

**2. Which website made the request? `TRUSTED_ORIGINS`.** The cross-site check compares the browser's `Origin` with the `Host` header the server receives. If your proxy passes the original `Host` through, nothing needs configuring. If it rewrites it (the server sees `toran:8080` while the browser is on `https://toran.example.org`), saving fails with `403 request blocked`. Fix it either way:

- keep the original host, or
- tell Toran the public address: `TRUSTED_ORIGINS=https://toran.example.org`.

The server logs every blocked request with its `Origin` and `Host`, so you can see what to put there.

**nginx**

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;                                 # keeps the cross-site check working
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; # appends the visitor on the right
}
```

**Caddy**

```
toran.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy keeps `Host` and sets `X-Forwarded-For` by default.

Toran does not send `Strict-Transport-Security`, because most installs are plain HTTP on a home network. If you serve it over HTTPS, add that header at the proxy.
