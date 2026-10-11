# GeoTracker for Android

A native recorder and viewer for GeoTracker. It works fully on the phone, and with a server when you have one.

| | Phone only | With a GeoTracker server |
|---|---|---|
| Recording | Foreground GPS, accuracy and spike filters, slows down when still, pause zones | Same, plus upload to `/ingest/json` (instant / 15 min / hourly / 6 h, optional Wi-Fi only) |
| Timeline | Visits and trips computed on the phone (a port of `internal/timeline`), rename, hide, change the transport mode | The server's timeline: place names, imports, edits made on the web |
| Insights | Distance, moving time, modes, top places, daily chart | Adds countries and cities |
| Places | Local places and pause zones | Server places, privacy zones, address search |
| Maps | OpenFreeMap, downloadable offline areas, 300 MB automatic tile cache | Same |
| More | GPX / GeoJSON / CSV export | Family on the map, live share links |

If the server can't be reached, every screen falls back to what the phone computes from its own data. The app also copies the server's history (imports, other phones) to the phone, so that fallback has your whole history.

## Build

Requirements: JDK 17 and the Android SDK (platform 36).

```bash
cd android
./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
./gradlew testDebugUnitTest    # timeline parity test
```

Release builds are signed when `SIGNING_KEYSTORE_PATH`, `SIGNING_STORE_PASSWORD`, `SIGNING_KEY_ALIAS` and `SIGNING_KEY_PASSWORD` are set. `-PversionName=1.2.3 -PversionCode=10203` sets the version.

Pushing a `v*` tag makes `.github/workflows/release.yml` build a signed APK and attach it to the GitHub Release as `geotracker-android-<version>.apk`. It reads the repository secrets `ANDROID_KEYSTORE_BASE64` (the `.jks` file, base64-encoded), `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD`, and fails instead of publishing an unsigned APK. Keep the keystore safe: Android only installs updates signed with the same key.

`Timeline.kt` must stay in step with `internal/timeline/timeline.go`. `TimelineTest` mirrors the Go test.
