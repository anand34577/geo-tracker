# Connecting phones

**Settings → Devices → Add device** shows a QR code and the exact values for each app. Every device gets its own key, which can only *send* locations: a lost phone can't read your history. Revoke or rotate keys at any time (Settings → Devices).

Your server must be reachable from the phone over **HTTPS** if the phone leaves your home network (see [HTTPS](HTTPS)).

| App | Platforms | Setup |
|---|---|---|
| **Colota** (recommended on Android) | Android | Scan the QR code (sets endpoint and bearer token), or: Connection → endpoint `https://your.domain/ingest/colota`, format *Custom*, auth *Bearer*. |
| **OwnTracks** (recommended on iPhone) | Android, iOS | Scan the QR code, or set Mode = HTTP, the URL, username (anything) and password (= device key). |
| **Overland** | iOS, Android | Scan the QR code, or paste the receiver endpoint. |
| **GPSLogger** | Android | Logging details → Log to custom URL → paste the URL, method GET. |
| **Traccar Client** | Android, iOS | Server URL = `https://your.domain/ingest/osmand`, device identifier = device key. |
| **Home Assistant** | — | Choose *Home Assistant*; copy the generated `rest_command` into `configuration.yaml`. |
| **Scripts** | — | `POST /ingest/json` with `Authorization: Bearer <key>` and `[{"ts": <unix ms>, "lat": 52.5, "lon": 13.4}]`. See the [API](API). |

## Battery and accuracy tips

- OwnTracks' *significant changes* mode and Overland's defaults are battery-friendly. GeoTracker copes well with sparse data: a long silent gap while you're still is treated as a stay.
- On Android, exclude the tracking app from battery optimisation, or Android will stop it. See [dontkillmyapp.com](https://dontkillmyapp.com).
- On iOS, allow location **Always** and keep *Background App Refresh* on.
- Prefer 30–60 second intervals while moving. Faster rarely improves the timeline and costs battery.

## Check that it works

Open the app and send a location manually (OwnTracks: the upload button). The point should appear on the map within seconds, and **Settings → Devices** shows the device's last-seen time and battery. If not, see [Troubleshooting](Troubleshooting#phones).

## No tracking app?

On the map, **Send my location** records your current position from the browser: useful for a quick check-in. Install GeoTracker as an app (see the [User guide](User-Guide#install-as-an-app)) to make it one tap.
