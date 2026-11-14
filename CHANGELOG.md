# Changelog

Toran follows [semantic versioning](https://semver.org). While the version is `0.x`, anything
can change between releases, including the config format and the API.

## [Unreleased]

### Security
- Saving and uploading are now refused when a request comes from another website (cross-site request protection). Set `TRUSTED_ORIGINS` if a reverse proxy changes the `Host` header.
- New `TRUSTED_PROXIES` setting: behind a reverse proxy, `LAN_ONLY` now judges the visitor (from `X-Forwarded-For`, read from the right, only from listed proxies) instead of the proxy.
- The server-side status checker refuses link-local (cloud metadata), unspecified and multicast addresses, follows no redirects and only checks `http` and `https` addresses.
- Security headers on every response: a Content-Security-Policy (no framing, no plugins, fixed `<base>` and form targets), `X-Frame-Options`, `nosniff`, `Referrer-Policy` and `Permissions-Policy`.

## [0.1.0]

First tagged release.

- Page with 7 layouts, 14 themes, wallpapers, a settings drawer and in-page editing.
- Optional Go server: saves the config, dated backups with restore, wallpaper and icon uploads,
  server-side health checks.
- Multi-arch Docker image (`amd64`, `arm64`, `arm/v7`).
- CI and automated releases on GitHub.
