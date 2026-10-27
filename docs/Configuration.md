# Configuration

Environment variables only cover what's needed to **start** the server. Everything else lives in **Admin → Settings** and is included in backups.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `GT_BASE_URL` | derived from the request | Public URL, e.g. `https://track.example.com`. Used for phone setup links, secure cookies and share links. **Set it in production.** |
| `GT_DATA_DIR` | `./data` (`/data` in Docker) | Database, backups and uploads. |
| `GT_LISTEN` | `:8080` | Listen address, e.g. `127.0.0.1:8080` to accept only a local reverse proxy. |
| `GT_TRUSTED_PROXIES` | — | Comma-separated CIDRs whose `X-Forwarded-For` header is trusted, e.g. `172.16.0.0/12`. Without it, every client looks like the proxy and shares one rate-limit bucket; `X-Forwarded-Proto` is also only believed from these addresses. The Linux installer sets `127.0.0.1/32`. |
| `GT_BLOCK_PRIVATE_TARGETS` | `false` | `true` forbids webhooks, ntfy, Gotify and Immich from reaching loopback and private-network addresses. Recommended when several people share the server. Link-local (cloud metadata) addresses are always refused. |
| `GT_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `GT_LOG_FORMAT` | `text` | `text` or `json`. Coordinates and query strings are never logged. |
| `GT_ADMIN_EMAIL`, `GT_ADMIN_PASSWORD` | — | Create the first admin without the setup wizard (automation). The password needs at least 10 characters. Remove them after the first start. |
| `TZ` | `UTC` | Server timezone, used for the daily backup hour. |

Where to set them: Docker: the `.env` file next to `docker-compose.yml`. Linux install script: `/etc/geotracker.env`. Windows (NSSM): `nssm set GeoTracker AppEnvironmentExtra ...`. See [Install the program](Install-the-Program).

## Admin → Settings

### Maps

The default is [OpenFreeMap](https://openfreemap.org): free, no API key. Admins can add their own tile servers and styles (below). Available online maps: OpenFreeMap Positron/Dark (follows the theme), Liberty and Bright, OpenStreetMap Standard and OpenTopoMap. People switch maps with the layers button.

**Privacy note:** the tile server sees roughly which areas you view. For zero third-party traffic use an offline map.

### Custom maps (XYZ, TMS, WMS, styles)

Admin → Settings → Maps → **Custom maps → Add custom map**. Two types:

- **Tile URL (XYZ)**: a raster tile template such as `https://tiles.example.com/{z}/{x}/{y}.png`. Supported placeholders: `{z}` `{x}` `{y}`, `{-y}` for TMS servers, `{s}` for subdomains (a, b, c), `{r}` for retina suffixes, `{quadkey}` (Bing-style), and `{bbox-epsg-3857}` for WMS (`...&BBOX={bbox-epsg-3857}&WIDTH=256&HEIGHT=256&SRS=EPSG:3857`). Set the attribution the provider requires, the zoom range and the tile size.
- **Style URL**: an `https://` MapLibre/Mapbox style JSON, optionally with a separate dark-theme style.

**Test** fetches one tile (or the style) from the server and shows a preview before you save. Saved maps appear for everyone in the layers menu and can be the default base map. Removing the default map switches everyone back to *Standard*.

**Load tiles through this server** proxies tiles via GeoTracker: API keys in the URL stay on the server, and tile servers on your network or on plain `http://` work (browsers block insecure tiles on an https page, so `http://` URLs always use the proxy). It costs the server's bandwidth.

The *Custom map style URLs* fold-out replaces the style behind **Standard**; it only takes style JSON links, not tile templates.

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

Host, port, encryption, user, password and sender. **Send test email to me** verifies it. Used for notifications, including family invitations.

| Encryption | Usual port | Use for |
|---|---|---|
| STARTTLS | 587 | Most providers |
| TLS | 465 | Providers with implicit TLS |
| None | 25 | A local relay (Postfix, a router, a NAS) on a network you trust |

With **None**, leave the username empty when the relay accepts mail by network address. If the relay asks for a login, GeoTracker sends it unencrypted, which is why None is only for trusted networks. The sender may include a name: `GeoTracker <geotracker@example.com>`.

### Notification services

People add their own Gotify, ntfy or Telegram details under Settings → Notifications; nothing to configure on the server. ntfy works with the public ntfy.sh or your own server, and Telegram uses a bot each person creates with @BotFather. Email needs the SMTP settings above.

### Single sign-on

See [Single sign-on](Single-Sign-On).

### Backups

The daily backup hour and how many daily backups to keep (default 7). See [Administration](Administration#backups-and-restore).

## Sizing

One point every 30 seconds all day is about 1 million points (~90 MB) per person per year. A Raspberry Pi 3 with 512 MB handles a family comfortably. Use an SSD or good SD card, and keep backups on a different disk.
