# Security

## Reporting a problem

Please do not open a public issue for a security problem. Use GitHub's private reporting instead: on the repository page open **Security → Report a vulnerability**. Include what you found, how to reproduce it and the version (`/api/health` shows it). You will get an answer as soon as I can give one; this is a hobby project, so please be patient.

## Supported versions

Toran is pre-release. Only the latest `0.x` release gets fixes.

## What Toran protects, and what it does not

Toran is built for a trusted home network. Read this before you expose it.

**It does:**

- Allow saving and uploading only from the local network (`LAN_ONLY`, on by default), and judge the real visitor behind a reverse proxy when you list the proxy in `TRUSTED_PROXIES`.
- Refuse changes that another website steers your browser into making (cross-site request protection).
- Keep the status checker away from cloud-metadata and other link-local addresses.
- Send security headers (a Content-Security-Policy, no framing, no content sniffing), serve only an allow-list of files, sandbox uploaded images, and run the container as an ordinary user.

**It does not (yet):**

- **Ask anyone to log in.** Anyone on your local network can change the page, and anyone who can reach it can read it. Do not expose Toran to the internet. If you must, put a proxy with authentication in front of it.
- Encrypt traffic. Serve it through an HTTPS proxy if you need that.
- Stop a script that is already running in the page (the page's own scripts are still inline; see `docs/configuration.md`).

Details and settings: [docs/configuration.md](docs/configuration.md#security).
