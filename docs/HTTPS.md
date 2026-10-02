# HTTPS and reverse proxies

Phones on mobile data must reach your server, and passwords and location data must not travel in plain text. iOS apps require valid HTTPS. Choose one setup:

| Setup | When |
|---|---|
| [Caddy](#caddy) | Easiest. Automatic certificates. |
| [nginx + certbot](#nginx) | You already run nginx. |
| [Traefik](#traefik) | You already run Traefik. |
| [Cloudflare Tunnel](#cloudflare-tunnel) | No open ports at home. |
| [Tailscale / WireGuard](#tailscale-or-wireguard) | Nothing public at all. |

**Always set** `GT_BASE_URL=https://your.domain` so setup links and secure cookies are right, and `GT_TRUSTED_PROXIES` to your proxy's address range so rate limiting and the audit log see real client IPs (Docker: `172.16.0.0/12`; a proxy on the same machine: `127.0.0.1/32`).

## Caddy

With Docker, the bundled [compose file](Install-with-Docker) already does this. For a bare-metal install, install Caddy and use:

```
track.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1    # stream live updates immediately
	}
}
```

Make sure DNS points at the server and ports 80 and 443 are open. Caddy fetches and renews the certificate on its own.

## nginx

Use [`deploy/nginx.conf`](https://github.com/anand34577/geo-tracker/blob/main/deploy/nginx.conf) and get a certificate with certbot:

```bash
sudo certbot --nginx -d track.example.com
```

Three details matter:
- `proxy_set_header Host $host;` is required: cross-site request protection compares the browser's Origin with Host.
- The `/api/v1/live` block turns buffering off so live updates arrive immediately.
- `client_max_body_size 8g;` allows large Google Takeout imports.

## Traefik

Route to port 8080 and don't buffer responses (live updates are Server-Sent Events). Example labels:

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.geotracker.rule=Host(`track.example.com`)
  - traefik.http.routers.geotracker.tls.certresolver=letsencrypt
  - traefik.http.services.geotracker.loadbalancer.server.port=8080
```

## Cloudflare Tunnel

No ports to open at home. Create a tunnel in the Cloudflare Zero Trust dashboard and add a public hostname with service `http://localhost:8080` (or `http://geotracker:8080` from another container). Set `GT_BASE_URL` to that hostname and `GT_TRUSTED_PROXIES` to the tunnel's source address.

## Tailscale or WireGuard

Nothing is exposed publicly: phones reach the server over the VPN. Great for privacy, but the VPN must be connected on the phone for tracking to work. With Tailscale you can get a real certificate with `tailscale serve --https=443 http://127.0.0.1:8080`.

## Checking it works

Open `GT_BASE_URL` in your phone's browser: you should see the padlock and the sign-in page. **Admin → System** shows a warning while the server isn't on HTTPS; it disappears once it is.
