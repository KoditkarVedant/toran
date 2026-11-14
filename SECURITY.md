# Security

## Reporting a problem

Please do not open a public issue for a security problem. Use GitHub's private reporting instead: on the repository page open **Security → Report a vulnerability**. Include what you found, how to reproduce it and the version (`/api/health` shows it). You will get an answer as soon as I can give one; this is a hobby project, so please be patient.

## Supported versions

Toran is pre-release. Only the latest `0.x` release gets fixes.

## What Toran protects, and what it does not

Toran is built for a trusted home network. Read this before you expose it.

**It does:**

- Require a login for everything personal. Every user has their own dashboard, wallpapers and backups, and cannot reach another's. Passwords are stored as salted PBKDF2-HMAC-SHA256 hashes (`users.json`, readable only by the server), login cookies are signed, `HttpOnly` and `SameSite=Lax`, and a password change, reset or disabled account ends that user's logins. Wrong passwords are rate-limited, and an unknown name takes as long to refuse as a wrong password. The first admin can only be created with a code printed in the server's log.
- Allow saving, uploading and managing users only from the local network as a second lock (`LAN_ONLY`, on by default), and judge the real visitor behind a reverse proxy when you list the proxy in `TRUSTED_PROXIES`.
- Refuse changes that another website steers your browser into making (cross-site request protection), and, when you set `ALLOWED_HOSTS`, requests that arrive under a name that is not yours (DNS rebinding).
- Neutralise `javascript:` and similar links in tiles, so a planted tile cannot run script.
- Keep the status checker away from cloud-metadata and other link-local addresses.
- Send security headers (a Content-Security-Policy, no framing, no content sniffing), serve only an allow-list of files, sandbox uploaded images, and run the container as an ordinary user.

**It does not (yet):**

- Encrypt traffic. Passwords and login cookies cross the network as plain HTTP unless you serve Toran through an HTTPS proxy. On a home network that may be fine; beyond it, it is not.
- Offer two-factor login, single sign-on or password reset by email. The way back in for a forgotten admin password is a command on the server (`toran reset-password`), so anyone who can run commands on the server, or read its data folder, controls the accounts.
- Protect one user from another's script. Users cannot read each other's data through Toran, but the page's own scripts are still inline (see below), so script that a user manages to get into the page runs with that user's login.
- Stop a script that is already running in the page (the page's own scripts are still inline; see `docs/configuration.md`).

Details and settings: [docs/configuration.md](docs/configuration.md#security).
