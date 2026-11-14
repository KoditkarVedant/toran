# Toran

A fast, good-looking start page for your home server. One tile per service, so you stop remembering ports.

*Toran* is the ceremonial arched gateway at the entrance of an Indian home or temple.

## Start here

- [Deploy with Portainer](deploy.md): get it running on a Raspberry Pi.
- [Configuration](configuration.md): settings, `services.json`, environment variables.
- [Development](development.md): build, test and contribute.

## Status

Pre-release. The page is tested in a simulated browser and in headless Chromium against the real server, and the server has unit tests. It has not been run on real Raspberry Pi hardware, or in GitHub Actions, yet.

## Security in one paragraph

Toran has accounts: everyone logs in and has their own dashboard ([Accounts and logging in](configuration.md#accounts-and-logging-in)). It does not encrypt traffic, so put an HTTPS proxy in front of it before exposing it beyond your home network.
