# Security policy

GeoTracker handles location data, so security reports are taken seriously.

## Reporting a vulnerability

Please **don't open a public issue**. Use GitHub's private reporting: [Security → Report a vulnerability](https://github.com/anand34577/geo-tracker/security/advisories/new). Include the version (`geotracker version`), what you found and how to reproduce it.

## Supported versions

Security fixes go into the latest release. Please upgrade before reporting.

## What the project already does

Argon2id password hashing, hashed session and device tokens, ingest-only device keys, login rate limits, cross-site request protection, a strict Content-Security-Policy, an audit log, and a container that runs as an unprivileged user. CI runs `govulncheck` and `npm audit` on every change.
