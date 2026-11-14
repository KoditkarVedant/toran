# Changelog

Toran follows [semantic versioning](https://semver.org). While the version is `0.x`, anything
can change between releases, including the config format and the API.

## [Unreleased]

### Added
- **Accounts.** With the Go server, everyone now logs in and has their own dashboard (services, look, wallpapers, backups). The first start prints a setup code in the log and the page asks you to create the administrator; admins add, disable, promote, reset and remove users from the account menu, and everyone can change their own password. Passwords are stored as salted PBKDF2-HMAC-SHA256 hashes; sessions are signed cookies; wrong guesses are rate-limited. New settings: `SETUP_CODE`, `SESSION_DAYS`, `PBKDF2_ITERATIONS`. `toran reset-password NAME` sets a new password from the server's command line.
- Without the Go server (the page behind plain nginx) nothing changes: there is no login.

### Changed
- **Breaking:** the API now needs a login (`401` otherwise), except `GET /api/health`, `GET /api/session`, `POST /api/setup` and `POST /api/login`. Only the page, `custom.css` and the bundled `wallpapers/` are served without one. Scripts that call the API must log in first.
- **Upgrading:** your existing `services.json`, `wallpaper/` and `backups/` become the dashboard of the first admin you create. They are copied into `users/<id>/`; the originals are left in place and are no longer served, so you can delete them once you are happy. `LAN_ONLY` stays as a second lock for saving and managing users.
- Wallpapers and the icon cache are now served only to logged-in users, and the status checker looks only at the services in the asker's own dashboard.
- The page keeps unsaved changes and recents per user in the browser, and shows the login screen again, without losing changes, when a login ends while it is open.

### Security
- Tiles whose address starts with `javascript:`, `data:`, `vbscript:` or `blob:` are turned into links that do nothing, so a planted tile cannot run script when clicked.
- New `ALLOWED_HOSTS` setting (off by default): refuse requests for any host name that is not listed, which stops DNS rebinding. Numeric addresses and `localhost` are always accepted.
- Saving and uploading are now refused when a request comes from another website (cross-site request protection). Set `TRUSTED_ORIGINS` if a reverse proxy changes the `Host` header.
- New `TRUSTED_PROXIES` setting: behind a reverse proxy, `LAN_ONLY` now judges the visitor (from `X-Forwarded-For`, read from the right, only from listed proxies) instead of the proxy.
- The server-side status checker refuses link-local (cloud metadata), unspecified and multicast addresses, follows no redirects and only checks `http` and `https` addresses.
- The Docker image now runs as an ordinary user (`65532`), not root. New volumes just work; an existing root-owned volume needs a one-time `chown` (the container says so and how, see `docs/deploy.md`).
- Security headers on every response: a Content-Security-Policy (no framing, no plugins, fixed `<base>` and form targets), `X-Frame-Options`, `nosniff`, `Referrer-Policy` and `Permissions-Policy`.

## [0.1.0]

First tagged release.

- Page with 7 layouts, 14 themes, wallpapers, a settings drawer and in-page editing.
- Optional Go server: saves the config, dated backups with restore, wallpaper and icon uploads,
  server-side health checks.
- Multi-arch Docker image (`amd64`, `arm64`, `arm/v7`).
- CI and automated releases on GitHub.
