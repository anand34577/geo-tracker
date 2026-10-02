# GeoTracker API

Everything the web app does goes through this API, so a native app (for example a future GeoTracker Android app), scripts and integrations can do the same. JSON over HTTPS, versioned under `/api/v1`.

## Stability promise

| Path | Promise |
|---|---|
| `/ingest/*` | **Never changes.** Phones are configured once and forgotten. |
| `/api/v1/*` | Additive changes only (new fields/endpoints). Breaking changes get `/api/v2`. |
| `/api/v1/public/*` | Unauthenticated endpoints (share links). |

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem objects: `{"title": "human readable message", "status": 400}`.
Timestamps are **Unix milliseconds** (UTC). Ranges use `?from=&to=` (milliseconds or RFC 3339); both are optional and mean "all time" when omitted.

## Authentication

| Client | How |
|---|---|
| Browser | Session cookie from `POST /api/v1/auth/login`. |
| **Native app** | `POST /api/v1/auth/token` once, then `Authorization: Bearer <token>` on every request. |
| Tracking app | A per-device **ingest-only** token (Settings → Devices). It can upload but never read. |

```http
POST /api/v1/auth/token
Content-Type: application/json

{"email": "ana@example.com", "password": "…", "device_name": "Pixel 9 app"}
```
```json
201 {"token": "3f9c…", "user": {"id": 1, "name": "Ana", …}, "device": {"id": 7, "name": "Pixel 9 app", "client": "app"}}
```

The token has scopes `read,write,ingest`: the app can read history, change settings **and** upload its own locations with the same token. It appears under Settings → Devices, where the user can revoke it; deleting the device revokes the token immediately. `GET /api/v1/auth/methods` tells a login screen whether single sign-on is enabled.

## Uploading locations

`POST /ingest/json` with the bearer token, one point or a batch:

```json
[{"ts": 1727712000000, "lat": 52.5200, "lon": 13.4050, "acc": 8, "alt": 34, "speed": 1.2, "bearing": 90, "batt": 81}]
```
Response: `{"received": 1, "added": 1}`. Duplicate timestamps are ignored, so retrying a failed batch is always safe. Only `ts`, `lat` and `lon` are required. Units: meters, m/s, degrees, percent.

Other accepted formats (same token rules): `/ingest/owntracks`, `/ingest/overland`, `/ingest/colota`, `/ingest/gpslogger`, `/ingest/osmand` (Traccar).

## Live updates

`GET /api/v1/live` is a Server-Sent Events stream:

| Event | Data | Meaning |
|---|---|---|
| `point` | `{"device_id": 3, "point": {…}}` | A new location arrived |
| `timeline` | `{"from": 1727…}` | Visits/trips were recalculated from `from` onward |
| `import` | import object | Import progress |

## Main endpoints

| Area | Endpoints |
|---|---|
| Me | `GET/PATCH /me` · `POST /me/password` · `GET/PUT /me/notifications` · `POST /me/notifications/test?channel=email\|gotify` |
| Config | `GET /config` (version, public URL, available base maps) |
| Points | `GET /points?from&to&max&raw&user` → `{"total","step","points":[[ts,lat,lon,acc,speed,alt,batt]]}` · `POST /points` (one fix, e.g. "send my location") · `GET /points/latest` · `DELETE /points?from&to` · `GET /stats` |
| Timeline | `GET /timeline?from&to&user` → `{"visits":[…],"trips":[…]}` · `PUT /trips/mode {start, mode}` (correct a trip) · `POST /timeline/rebuild` |
| Family | `GET /family` (people + what they share + live position) · `GET/POST /groups` · `PATCH/DELETE /groups/{id}` · `POST /groups/{id}/members {email}` · `PUT /groups/{id}/me` (accept / change own sharing) · `DELETE /groups/{id}/members/{uid}` · `GET/POST /alerts` · `DELETE /alerts/{id}` |
| Photos | `GET/PUT /me/integrations` (Immich) · `GET /photos?from&to` · `GET /photos/{id}/thumb?size=thumbnail\|preview` |
| Security | `GET /me/sessions` · `DELETE /me/sessions/{id}` · `GET/PUT /me/retention {days}` · admin: `GET /admin/audit?before=` |
| Insights | `GET /insights?from&to&tz` · `GET /days?from&to&tz` (points per local day) |
| Places | `GET/POST /places` · `PUT/DELETE /places/{id}` |
| Geocoding | `GET /geocode/search?q=` · `GET /geocode/reverse?lat&lon` |
| Maps | `GET /map/style/{id}` · `GET /map/tiles/{z}/{x}/{y}` (offline MBTiles) |
| Devices | `GET/POST /devices` · `POST /devices/{id}/token` · `DELETE /devices/{id}` |
| Sharing | `GET/POST /shares` · `DELETE /shares/{id}` · public: `GET /public/shares/{token}` |
| Data | `GET/POST /imports` (multipart `file`) · `DELETE /imports/{id}` (undo) · `GET /export?format=native\|gpx\|geojson\|csv&from&to` |
| Admin | `/admin/users…` · `/admin/settings` · `/admin/system` · `/admin/backups…` |

`tz` is an IANA timezone name (e.g. `Asia/Kolkata`). Days are split in that zone.

`?user=<id>` on `/points` and `/timeline` reads a **family member's** data. The server limits it to what that person shares with you: the history window, approximate rounding (~1 km, no addresses) and pause. Otherwise it answers `403`. A `family` event on `/live` tells you when someone you can see moved or changed their sharing; refetch `/family`.

## Building an Android app on this API

- **Sign in:** `POST /auth/token`, then store the token in Android Keystore-backed storage.
- **Track:** a foreground service collects fixes, queues them locally (Room/SQLite) and uploads batches to `/ingest/json`. Duplicates are harmless, so retry freely.
- **Show:** `/timeline`, `/points` and `/insights` for the screens; `/live` (SSE) or polling for live updates.
- **Maps:** MapLibre Native Android can use the same style URLs from `/config`. For offline MBTiles styles, add the bearer token with a request interceptor.
- **Sign-out:** `DELETE /devices/{id}` with the app's own device id.
