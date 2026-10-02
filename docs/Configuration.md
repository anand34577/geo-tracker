# Configuration

Environment variables only cover what's needed to **start** the server. Everything else lives in **Admin → Settings** and is included in backups.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `GT_BASE_URL` | derived from the request | Public URL, e.g. `https://track.example.com`. Used for phone setup links, secure cookies and share links. **Set it in production.** |
| `GT_DATA_DIR` | `./data` (`/data` in Docker) | Database, backups and uploads. |
| `GT_LISTEN` | `:8080` | Listen address, e.g. `127.0.0.1:8080` to accept only a local reverse proxy. |
| `GT_TRUSTED_PROXIES` | — | Comma-separated CIDRs whose `X-Forwarded-For` header is trusted, e.g. `172.16.0.0/12`. Without it, every client looks like the proxy. |
| `GT_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `GT_LOG_FORMAT` | `text` | `text` or `json`. Coordinates and query strings are never logged. |
| `GT_ADMIN_EMAIL`, `GT_ADMIN_PASSWORD` | — | Create the first admin without the setup wizard (automation). The password needs at least 10 characters. Remove them after the first start. |
| `TZ` | `UTC` | Server timezone, used for the daily backup hour. |

Where to set them: Docker: the `.env` file next to `docker-compose.yml`. Linux install script: `/etc/geotracker.env`. Windows (NSSM): `nssm set GeoTracker AppEnvironmentExtra ...`. See [Install the program](Install-the-Program).

## Admin → Settings

### Maps

The default is [OpenFreeMap](https://openfreemap.org): free, no API key. Any MapLibre style URL works (your own tile server, MapTiler with a key, …). Available online maps: OpenFreeMap Positron/Dark (follows the theme), Liberty and Bright, OpenStreetMap Standard and OpenTopoMap. People switch maps with the layers button.

**Privacy note:** the tile server sees roughly which areas you view. For zero third-party traffic use an offline map.

### Offline maps

GeoTracker serves tiles from an **MBTiles file**: vector (OpenMapTiles schema, e.g. from openmaptiles.org, MapTiler or Planetiler) or raster (PNG/JPG/WebP). It generates a light/dark style automatically.

1. Put the file where the server can read it.
   - Binary on Windows: any path, e.g. `D:/maps/india.mbtiles`.
   - Docker: mount the folder, e.g. `-v /srv/maps:/data/maps:ro`, then use `/data/maps/india.mbtiles`.
2. Admin → Settings → Maps → **MBTiles file on the server**: paste the path and Save. GeoTracker checks that the file opens.
3. Optionally make it the **default base map**.
4. **Labels:** vector maps need font files for names. By default they come from OpenFreeMap. Clear *Label fonts URL* for a fully offline map without labels, or point it at your own font server.

### Place names

Visits (never your full track) are reverse-geocoded, cached and shared across users. The default is the public OpenStreetMap **Nominatim** server at no more than one request per second, as its usage policy requires. Alternatives: **Photon**, your own Nominatim or Photon instance (custom URL), or **Off** for zero external requests.

### Email (SMTP)

Host, port, encryption (STARTTLS on 587 or TLS on 465), user, password and sender. **Send test email to me** verifies it. Used for notifications, including family invitations.

### Notification services

People add their own Gotify, ntfy or Telegram details under Settings → Notifications; nothing to configure on the server. ntfy works with the public ntfy.sh or your own server, and Telegram uses a bot each person creates with @BotFather. Email needs the SMTP settings above.

### Single sign-on

See [Single sign-on](Single-Sign-On).

### Backups

The daily backup hour and how many daily backups to keep (default 7). See [Administration](Administration#backups-and-restore).

## Sizing

One point every 30 seconds all day is about 1 million points (~90 MB) per person per year. A Raspberry Pi 3 with 512 MB handles a family comfortably. Use an SSD or good SD card, and keep backups on a different disk.
