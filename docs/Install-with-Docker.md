# Install with Docker

The image is published to the GitHub Container Registry for **amd64, arm64 and armv7** (Raspberry Pi included): `ghcr.io/anand34577/geotracker`. It is distroless, runs as a non-root user and has a built-in health check.

Pin a version tag in production (`:0.2` follows patch releases of 0.2; `:0.2.0` is fixed). `:edge` tracks the main branch.

## Option A: Docker Compose with automatic HTTPS (recommended)

Needs a domain pointing at the server, and ports 80 and 443 open.

```bash
mkdir geotracker && cd geotracker
curl -LO https://raw.githubusercontent.com/anand34577/geo-tracker/main/deploy/docker-compose.yml
curl -LO https://raw.githubusercontent.com/anand34577/geo-tracker/main/deploy/Caddyfile
curl -L  https://raw.githubusercontent.com/anand34577/geo-tracker/main/deploy/.env.example -o .env
mkdir -p data && sudo chown 65532:65532 data    # the container runs as an unprivileged user
```

1. Create a DNS `A`/`AAAA` record, e.g. `track.example.com`, pointing at the server.
2. Edit `.env`: set `GT_BASE_URL=https://track.example.com` and your `TZ` (for example `Europe/Berlin`).
3. Edit `Caddyfile`: replace `track.example.com` with your domain.
4. Start it:
   ```bash
   docker compose up -d
   ```
5. Open `https://track.example.com` and create the admin account.

Caddy obtains and renews the certificate by itself. Check progress with `docker compose logs -f`.

Your data is in `./data` next to the compose file. That folder is everything: back it up.

## Option B: Single docker command (testing / LAN)

```bash
mkdir -p data && sudo chown 65532:65532 data
docker run -d --name geotracker -p 8080:8080 -v ./data:/data \
  --restart unless-stopped ghcr.io/anand34577/geotracker:0.2
```

Open `http://localhost:8080`. Phones outside your network need HTTPS: see [HTTPS](HTTPS).

> `--restart unless-stopped` matters: restoring a backup from the web UI exits the process so it can swap the database safely, and the restart policy brings it straight back.

## Option C: Behind a proxy you already run

Use the compose file without the Caddy service, publish `127.0.0.1:8080:8080` instead, and follow [HTTPS](HTTPS) for nginx, Traefik or Cloudflare Tunnel. Set `GT_TRUSTED_PROXIES` to the proxy's network so rate limiting sees real client IPs.

## Upgrading

```bash
docker compose pull && docker compose up -d
```

A backup is written automatically before any database change. See [Administration → Upgrading](Administration#upgrading).

## Useful commands

```bash
docker compose logs -f geotracker                       # logs
docker exec geotracker /geotracker backup               # backup now
docker exec geotracker /geotracker version
docker exec -e GT_NEW_PASSWORD='a-new-long-password' geotracker /geotracker reset-password you@example.com
```

## Build the image yourself

```bash
git clone https://github.com/anand34577/geo-tracker && cd geo-tracker
docker build -t geotracker .
```

## Platform notes

- **Synology / QNAP / Unraid / Portainer:** use the compose file or the `docker run` line. Map `/data` to a persistent share.
- **Windows (Docker Desktop):** `-v D:/geotracker/data:/data`. For a Windows install without Docker see [Install the program](Install-the-Program#windows).
- **Raspberry Pi:** use a 64-bit OS if you can; the armv7 image works on 32-bit Raspberry Pi OS.
