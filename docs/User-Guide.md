# User guide

## The map and timeline

- **Map:** your latest position, the path for the selected day or period, saved places and family members. Use the date picker for any day or range, and the playback bar to replay a day.
- **Timeline:** each day as **visits** (where you stayed, with place names) and **trips** (how you moved: walk, cycle, drive, train, flight). Tap a trip's transport mode to correct it.
- **Insights:** distance, time moving, places and countries over any period, with a year heatmap of tracked days.
- **Places:** name the places that matter (home, work, school). Your timeline becomes easier to read and arrival alerts become possible.
- **Search:** `Ctrl/Cmd + K` jumps to any page, place, person, or a date like `2024-07-15`.

Place names come from reverse geocoding of your *visits* only, never your full track (see [Configuration → Place names](Configuration#place-names)).

## Importing your history

**Settings → Import & export**, then drop the file. Imports are streamed (multi-GB files are fine, up to 8 GB), duplicates are skipped, and **Undo import** removes exactly what an import added.

| Source | How to get it |
|---|---|
| Android (on-device Timeline, 2024+) | Settings → Location → Location services → Timeline → **Export Timeline data** → `Timeline.json` |
| iPhone (on-device Timeline) | Google Maps → profile → Your Timeline → ⋯ → Location and privacy settings → **Export Timeline data** |
| Older Google Takeout | takeout.google.com → *Location History (Timeline)* → upload the **.zip as is** (`Records.json` is preferred automatically) |
| Others | GPX, GeoJSON, CSV (columns named lat/lon/time), OwnTracks Recorder `.rec`, GeoTracker exports |

After importing a lot of old data, use **Rebuild timeline** if visits look split or trips look wrong. Behind nginx keep `client_max_body_size 8g` ([HTTPS](HTTPS#nginx)).

## Exporting your data

Settings → Import & export: a lossless **GeoTracker archive**, or GPX, GeoJSON and CSV. Choose a format and period and press **Create export**. It is built in the background, so you can keep using GeoTracker (or close the page), and a **Download** button appears when it is ready. Finished exports are kept for 7 days. Your data is yours at any time.

## Family groups

Family → New group, then invite people by email (they need an account; admins create accounts under Admin → Users).

- **Consent first:** invited people share nothing until they accept and choose. They decide live location on or off, how much history (none, 24 hours, 7 or 30 days, a year, or everything), and exact or approximate (~1 km) precision.
- **Pause** sharing for 1–24 hours at any time. Others see "paused", not a stale position. Everyone sees **who can see them**.
- Family members appear on the map as avatars. With their permission you can open their timeline and set **arrive/leave alerts** on your saved places ("tell me when Sam arrives at School"). Alerts need exact sharing.
- The instance admin has no special access to anyone's location.

## Share links

Sharing → New share link: share your **live location** or a **time period** with anyone, without them needing an account. Links expire (1 hour to 90 days), can be revoked any time, and can show an **approximate** position rounded to about 1 km.

## Automations

Run actions when you arrive at or leave a saved place: tell family, call a webhook, message ntfy, Telegram, Discord or Slack. See [Automations](Automations).

## Notifications

Settings → Notifications. Choose email, Gotify, ntfy and/or Telegram, and which events you want: arriving at or leaving a saved place, a phone that stopped sending locations, low battery, import finished, and backup failed (admins). Email needs an admin to set up SMTP first ([Configuration → Email](Configuration#email-smtp)).

## Photos from Immich

Settings → Integrations: enter your Immich address and an API key (Immich → Account settings → API keys). Photos taken on a day appear at the top of that day's timeline. Requests go through GeoTracker, so the key stays on the server.

## Install as an app

- **Android / desktop Chrome and Edge:** use *Install app* in the browser menu.
- **iPhone:** Share → *Add to Home Screen*.

The app opens offline; your data loads when you're back online. Location data is never cached on the device by the app shell.

## Privacy and security

- **Privacy zones:** mark a place as a *Privacy zone* (Places → edit). While you're inside it, family members and share links don't see your position, path, visits or arrive/leave alerts there. You still see everything yourself.
- **Data retention:** Settings → Import & export → keep raw points for 30 days to 5 years. Visits and trips stay. Shortening the period asks for confirmation, because older points are deleted for good.
- **Signed-in devices:** Settings → Profile lists every browser you're signed in on; sign out any of them. Changing your password signs out all other sessions.
- **Audit log:** admins see sign-ins and sensitive changes for one year (Admin → Audit log). Coordinates are never logged.
