# Development

## Requirements

Go 1.26+ and Node.js 22+. No CGO and no database server are needed: SQLite is built in (pure Go).

```bash
git clone https://github.com/anand34577/geo-tracker && cd geo-tracker
```

## Run it locally

```bash
make web        # build the UI once (the Go binary embeds web/dist)
make dev-api    # API + embedded UI on :8080
make dev-web    # hot-reloading UI on :5173, proxies the API to :8080
```

Skip the setup wizard with a throwaway admin and data folder:

```bash
GT_DATA_DIR=./data-dev GT_ADMIN_EMAIL=admin@example.com GT_ADMIN_PASSWORD=change-me-please go run ./cmd/geotracker
```

## Checks (what CI runs)

```bash
make check                  # go vet, backend tests, UI typecheck
go test -race ./internal/...
```

## Layout

| Path | Contents |
|---|---|
| `cmd/geotracker` | Entry point and command line |
| `internal/server` | HTTP API, ingest endpoints, auth, workers |
| `internal/store` | SQLite: migrations, queries, backups |
| `internal/timeline` | Visit and trip detection |
| `internal/ingest` | Parsers for OwnTracks, Overland, Colota, GPSLogger, Traccar |
| `internal/transfer` | Import and export |
| `internal/geocode`, `geo`, `maps`, `notify` | Geocoding, geometry, MBTiles, email and Gotify |
| `web/` | React, Vite and Tailwind UI |
| `deploy/` | Compose, Caddy, nginx, systemd |
| `scripts/install.sh` | Linux installer |
| `docs/` | These wiki pages |

The full product and architecture spec, with every design decision and its reason, is [PROJECT_SPEC.md](https://github.com/anand34577/geo-tracker/blob/main/PROJECT_SPEC.md).

## Documentation

The wiki is generated from `docs/` by a GitHub Action whenever `docs/` changes on `main`. Edit the files there, not in the wiki. Page names are the file names (`Install-the-Program.md` becomes *Install the Program*); link between pages with `[text](Page-Name)`.

## Releasing

1. Make sure `main` is green in CI.
2. Tag and push:
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```
3. GitHub Actions then:
   - builds the **Docker image** for amd64, arm64 and armv7 and pushes `ghcr.io/anand34577/geo-tracker:0.2.0`, `:0.2` (and `:edge` for every push to main),
   - builds **binaries** for Linux, macOS and Windows, writes `SHA256SUMS`, and publishes them with `install.sh` as a GitHub Release.

The version is injected at build time (`-X main.version`) and shown in Admin → System and `geotracker version`.

## Contributing

Fork, branch, run `make check`, open a pull request. Keep changes small and include a test for logic changes. The API under `/ingest/*` never changes (phones are configured once); `/api/v1/*` changes only additively. See the [API](API).
