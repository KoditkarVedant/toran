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

## Accounts and logging in

With the Go server, Toran has accounts. Everyone logs in, and everyone has their **own dashboard** (services, look, wallpapers, backups). Without the Go server (the page behind plain nginx) there is no login and the page works as it always did.

**First run.** The first time Toran starts with no accounts, it prints a setup code in its log and the page shows a "create the admin account" screen. Enter the code, a user name and a password (at least 10 characters). That first account is the administrator.

```sh
docker logs toran          # look for: first run: ... Setup code: ABCD-EFGH-JKLM
```

The code is what stops a stranger who reaches a brand-new server before you do from becoming its admin: only someone who can read the server's log can create the first account. Set `SETUP_CODE` (at least 8 characters) to choose it yourself. Creating the first account also needs a private-network address (`LAN_ONLY`).

**Users.** The account button (top right) opens the account drawer: change your password, log out and, for administrators, add users, disable or enable them, make them admin or plain users, reset a password or remove them. A plain user can only use their own dashboard. You cannot change or remove yourself there (another admin can), and there is always at least one active admin. Removing a user keeps their files: the folder is renamed `users/.removed-<id>-<time>` and can be restored or deleted by hand. Changing a password, resetting it or disabling an account ends every login of that user.

**Where things are** (inside `ROOT`, the Docker volume `/data`):

| File | What |
|---|---|
| `users.json` | Accounts and password hashes (PBKDF2-HMAC-SHA256). Readable only by the server's user (`0600`). |
| `secret.key` | The key that signs login cookies (`0600`). Deleting it logs everyone out. |
| `users/<id>/` | One user's `services.json`, `wallpaper/` and `backups/`. |
| `icons/` | App logos, a cache of public icons shared by everyone. |
| `wallpapers/` | The wallpapers that ship with Toran. |

**Upgrading from a version without accounts.** The first admin takes over your existing dashboard: `services.json`, `wallpaper/` and `backups/` in `ROOT` are *copied* into that admin's folder. The originals stay where they were (and are no longer served); delete them yourself once you are happy. Other users start with the built-in starter dashboard.

**Forgot the admin password?** Anyone who can run commands on the server can set a new one. The account, its dashboard and its role are kept, every login of that user ends, and a disabled account is enabled again. A running server notices within a second.

```sh
docker exec -it toran toran reset-password vedant      # type the new password, then Enter
```

Without Docker: `echo 'the new password' | ROOT=/srv/toran ./toran reset-password vedant`. The password is read from standard input so it does not appear in the process list; typing it in a terminal shows it on screen, and putting it in a shell command saves it in your history.

**Logins.** A login is a signed cookie (`HttpOnly`, `SameSite=Lax`, and `Secure` when you use HTTPS) that lasts 30 days and is renewed while you use Toran, so a wall display stays logged in. Nothing about sessions is stored on the server. Wrong passwords are limited: five failures for one name from one address, or twenty from one address, in 15 minutes, then a wait. Behind a reverse proxy set `TRUSTED_PROXIES`, or every visitor looks like one address and shares one limit.

## Server environment variables

| Variable | Default | Meaning |
|---|---|---|
| `ADDR` | `:8080` | Listen address |
| `ROOT` | `.` | Folder holding the page, `services.json`, backups and uploads |
| `LAN_ONLY` | `true` | A second lock on top of the login: only private-network clients may save or manage users. `false` allows saving from anywhere (a logged-in user is still required). |
| `SETUP_CODE` | random | The code the first admin must enter (at least 8 characters). When unset, a random one is printed in the log. |
| `SESSION_DAYS` | `30` | How long a login lasts, 1 to 365. |
| `PBKDF2_ITERATIONS` | `600000` | Work per password check, 1000 to 10000000. Higher is safer and slower; a login takes about 0.2 s on a desktop PC. Raising it upgrades each hash the next time that user logs in. |
| `TRUSTED_ORIGINS` | | Comma-separated browser addresses (`scheme://host[:port]`) allowed to save when a reverse proxy changes the `Host` header. See [Behind a reverse proxy](#behind-a-reverse-proxy). |
| `TRUSTED_PROXIES` | | Comma-separated IP addresses and CIDR ranges of reverse proxies whose `X-Forwarded-For` header is believed. See [Behind a reverse proxy](#behind-a-reverse-proxy). |
| `ALLOWED_HOSTS` | | Comma-separated host names Toran answers to (`toran.home.lan,pi`). Off when empty. Protects against DNS rebinding; see [Security](#security). |
| `CHECK_HOST` | `127.0.0.1` | Host used for bare-port status checks |
| `GOMEMLIMIT` | | Go runtime soft memory limit, for example `24MiB` |

## HTTP API

| Request | Purpose |
|---|---|
| `GET /api/health` | Server version (public) |
| `GET /api/session` | `{state: "setup" or "login" or "ok", user}` (public) |
| `POST /api/setup`, `POST /api/login`, `POST /api/logout` | First admin, log in, log out |
| `POST /api/password` | Change your own password `{current, new}` |
| `GET` / `POST /api/users`, `PATCH` / `DELETE /api/users/{id}` | Manage users (admins only) |
| `GET /api/status` | Health of every service in *your* `services.json` (cached 15 s) |
| `GET /api/backups`, `POST /api/backups/restore` | List and restore *your* backups |
| `GET` / `PUT /services.json` | Read and save *your* config (a backup of the previous one is kept) |
| `GET` / `PUT` / `DELETE /wallpaper/{name}` | *Your* wallpapers |
| `GET` / `PUT /icons/{name}` | The shared icon cache |

Everything except `health`, `session`, `setup` and `login` needs a login (`401` without one). Only the page, `custom.css` and the bundled `wallpapers/` are served to visitors who are not logged in.

## Security

- **Login.** Reading or saving anything personal needs an account (see [Accounts and logging in](#accounts-and-logging-in)): users cannot see each other's services, wallpapers or backups, the status checker only looks at the services in the asker's own dashboard, passwords are stored as salted PBKDF2 hashes, and wrong guesses are rate-limited. `LAN_ONLY` stays as a second lock: saving and user management also need a private-network address. Toran does not encrypt traffic; use an HTTPS proxy if the network is not trusted.
- **Cross-site protection.** A page on another website cannot make your browser save changes to Toran. Every `PUT`, `POST` and `DELETE` is checked with Go's standard `http.CrossOriginProtection`: browsers send `Sec-Fetch-Site` (on HTTPS and localhost) or `Origin` (on plain HTTP), and a request that comes from another origin gets `403`. Requests with neither header (`curl`, scripts) are let through, because they are not a browser steered by another site; `LAN_ONLY` still applies to them. Toran never sends CORS headers. One limit: when only `Origin` is available the check cannot tell `http://host` from `https://host`; serve Toran over HTTPS with HSTS if that matters to you.
- **Links that run script are disabled.** A tile's address comes from `services.json`, which anyone allowed to save can edit. A `javascript:`, `data:`, `vbscript:` or `blob:` link would run inside the page when clicked, so the page turns it into a link that does nothing (spaces and control characters hidden in the scheme are ignored by browsers, and are ignored here too). Other schemes such as `ssh://` and `smb://` work.
- **DNS rebinding: `ALLOWED_HOSTS`.** A malicious website can point its own name at your Toran address. Your browser then treats Toran as part of that site, the cross-site check passes (it only compares `Origin` with `Host`, and both hold the attacker's name) and the request comes from your own network. The one thing the attacker cannot change is the `Host` header. Set `ALLOWED_HOSTS` to the names you really use and any other name is refused with `403`, for reads as well as writes. Addresses written as numbers (`192.168.1.5`, `[::1]`) and `localhost` are always accepted, because rebinding needs a name; so the container health check keeps working. Entries are plain names (no scheme, port or wildcard) and match exactly, so a subdomain must be listed on its own. **It is off by default**, because Toran cannot know what you call it: if you reach it by a name (`pi.local`, a domain behind a proxy), set it.
- **Security headers.** Every response carries `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and a `Permissions-Policy` that switches off camera, microphone and location. In practice: nobody can put Toran in a frame, the browser will not guess file types, and the page cannot be steered into loading plugins, changing its `<base>` or posting forms elsewhere. The policy is written for what the page really does: it may load images and connect to any `http(s)` address (custom icons, the icon CDN, weather, and pings to your services when there is no server). It still allows the page's own inline scripts, so it limits what injected script can reach but does not stop it from running; that tightens once the page script moves into its own file. If you put your own scripts or fonts on the page, they need adding to `contentSecurityPolicy` in `server/headers.go`.
- **Status checks stay on safe ground.** The server fetches every URL in `services.json` to show its health, and anyone who may save can edit that list. So the checker refuses link-local addresses (this includes the cloud-metadata address `169.254.169.254`), `0.0.0.0` and multicast, judged at the moment of connecting so DNS tricks do not help; it follows no redirects, ignores proxy environment variables and only speaks `http` and `https`. Services on `localhost`, your home network and the public internet are checked as usual. HTTPS certificates are not verified, because home services often use self-signed ones and only the status code is read (no credentials are sent).
- Uploaded images are served with a sandboxing `Content-Security-Policy` and `nosniff`.
- Only an allow-list of paths is served to everyone: the page, `custom.css` and the bundled `wallpapers/`. Personal files are served only to their owner, and binaries, backups, `users.json` and `secret.key` are never served.

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
