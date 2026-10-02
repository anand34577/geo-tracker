# Troubleshooting

First, look at the logs:

```bash
docker compose logs -f geotracker     # Docker Compose
journalctl -u geotracker -f           # Linux install script
```

Raise detail with `GT_LOG_LEVEL=debug`. Check health with `curl http://127.0.0.1:8080/healthz` (should print `ok`).

## Installing and starting

| Symptom | Fix |
|---|---|
| Container restarts in a loop | Read the log. Usual causes: `GT_TRUSTED_PROXIES` isn't a valid CIDR list (e.g. `172.16.0.0/12`), `GT_ADMIN_PASSWORD` shorter than 10 characters, or the data folder isn't writable. |
| "permission denied" on the data folder | Docker: the image runs as user `nonroot` (UID 65532); `chown -R 65532:65532 ./data`. systemd: `chown -R geotracker: /var/lib/geotracker`. |
| `address already in use` | Another program uses port 8080. Change `GT_LISTEN` (e.g. `:8090`) or the published port. |
| Certificate isn't issued (Caddy) | DNS must point at the server and ports 80 and 443 must be open to the internet. `docker compose logs caddy` explains. |
| Windows: "Windows protected your PC" | The binary isn't code-signed. More info → Run anyway, after checking `SHA256SUMS`. |
| macOS: "cannot be opened because the developer cannot be verified" | `xattr -d com.apple.quarantine /usr/local/bin/geotracker` |
| Page shows but sign-in fails behind a proxy | The proxy must pass the `Host` header unchanged (`proxy_set_header Host $host;` in nginx). See [HTTPS](HTTPS). |

## Phones

| Symptom | Fix |
|---|---|
| App says "unauthorized" / 401 | The device key is wrong or was rotated. Settings → Devices → **New key** and re-scan the QR code. |
| Phone can't connect at all | Open `GT_BASE_URL` in the phone's browser. It must load over valid HTTPS (iOS apps refuse anything else). A VPN-only setup needs the VPN on. |
| "Waiting for first location" forever | Force a send in the app (OwnTracks: the upload button). Check the logs for `/ingest/` requests. |
| Points arrive in bursts, or stop overnight | Android is stopping the app: exclude it from battery optimisation ([dontkillmyapp.com](https://dontkillmyapp.com)). On iOS allow location *Always*. |
| Points appear at the wrong time | Check the phone's clock and timezone. Points from a clock before 1990 are rejected. |

## Using it

| Symptom | Fix |
|---|---|
| Map is blank | The map style URL isn't reachable from your browser. Check Admin → Settings → Maps, and your ad blocker or firewall. |
| Visits have no place names | The geocoder is off, rate-limited or unreachable. Names fill in over time (about one per second). |
| A visit is split in two, or trips look wrong | Settings → Import & export → **Rebuild timeline**, especially after importing lots of old data. |
| Live updates lag behind a proxy | Turn off response buffering for `/api/v1/live` (see `deploy/nginx.conf`; Caddy: `flush_interval -1`). |
| Import fails or is cut off | Behind nginx set `client_max_body_size 8g;`. Cloudflare's free plan limits uploads to 100 MB: import over the LAN or via the tunnel-free address. |
| "too many attempts" at sign-in | Rate limit: 10 tries per email and 20 per address per 15 minutes. Wait, or restart the server to clear it. |
| Geofence automation doesn't fire | See [Automations → Troubleshooting](Automations#troubleshooting). |
| Email notifications don't arrive | Admin → Settings → Email → *Send test email to me*. Port 587 uses STARTTLS and 465 uses TLS. |

## Accounts and data

| Symptom | Fix |
|---|---|
| Forgot the admin password | `GT_NEW_PASSWORD='a-long-password' geotracker reset-password you@example.com` (Docker: `docker exec -e GT_NEW_PASSWORD=... geotracker /geotracker reset-password you@example.com`). |
| Restore from the UI didn't come back | The process isn't supervised. Start it again; the staged restore is applied on start. Use a restart policy (Docker, systemd, NSSM). |
| Upgrade failed to start | GeoTracker stops without touching data and logs which `pre-upgrade-*.db.gz` backup to restore. Run the previous version, then restore it ([Administration](Administration#upgrading)). |
| Database size is growing | Set a retention period in Settings → Import & export, or delete old backups in Admin → System. |

## Still stuck?

Open an [issue](https://github.com/anand34577/geo-tracker/issues) with your version (`geotracker version`), install method and the relevant log lines. Remove tokens and coordinates first; the server never logs them, but your proxy might.
