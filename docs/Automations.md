# Automations (geofencing)

An automation runs when **you arrive at or leave a saved place**. Use it to tell family you got home, switch on the lights through Home Assistant, ping a chat channel, or call any web address.

Open **Automations** in the sidebar, then **New automation**:

1. **Name** it and pick one of your [saved places](User-Guide#the-map-and-timeline) (add places first; the radius of the place is the geofence).
2. Choose **when**: *I arrive*, *I leave*, or both.
3. Add one or more **actions** (up to six).
4. Press **Save**, then the play button (**Run a test**) to fire it once with sample data and see whether each action worked.

Rules only ever act for their owner. They run on the server, so they work while your phone is locked and no browser is open. They need your phone to keep reporting locations ([Connecting phones](Connecting-Phones)).

## Actions

| Action | What it does |
|---|---|
| **Tell family** | Sends a notification to chosen people from your family groups, on the channels they set up. They can only be people who share an active group with you; if someone leaves the group they stop receiving it. |
| **Notify me** | Sends a message to you on the channels you turned on in Settings → Notifications (email, Gotify, ntfy, Telegram). |
| **Webhook** | Calls any URL (GET, POST or PUT) with an optional JSON body and headers. Works with Home Assistant, n8n, Node-RED, IFTTT, Zapier, Make and your own scripts. |
| **ntfy** | Push notification through [ntfy](https://ntfy.sh) or your own ntfy server. |
| **Telegram** | A message from your own bot to a chat. |
| **Discord** | Posts to a channel through a Discord webhook. |
| **Slack** | Posts through a Slack incoming webhook (also works with Mattermost and Rocket.Chat). |
| **Email** | Sends an email to any address through the server's mail settings ([Configuration](Configuration#email-smtp)). |

## Message placeholders

Messages, webhook bodies and webhook URLs can contain placeholders that are filled in when the automation runs:

| Placeholder | Example |
|---|---|
| `{{user}}` | Sam |
| `{{place}}` | Home |
| `{{verb}}` | `arrived at` or `left` |
| `{{event}}` | `arrive` or `leave` |
| `{{time}}`, `{{date}}` | `18:42`, `2026-10-02` (server time zone) |
| `{{lat}}`, `{{lon}}` | `52.520000`, `13.400000` |
| `{{map_url}}` | link to the position on OpenStreetMap |

The default message is `{{user}} {{verb}} {{place}} at {{time}}.`

A webhook with an **empty body** sends this JSON automatically:

```json
{"event": "arrive", "user": "Sam", "place": "Home", "lat": 52.52, "lon": 13.4, "ts": 1790921518797, "map_url": "https://www.openstreetmap.org/…"}
```

Values are escaped for you when the body is JSON, so a place called `Sam's "Home"` can't break it.

## Recipes

### Tell family you are home
Action **Tell family**, tick the people to tell, trigger *I arrive* at Home. Everyone needs to be in a [family group](User-Guide#family-groups) with you, and they get it on their own notification channels.

### Home Assistant
In Home Assistant create an automation with the trigger *Webhook* and copy its ID. In GeoTracker add a **Webhook** action with the address `http://homeassistant.local:8123/api/webhook/YOUR_ID`, method POST, and leave the body empty. In Home Assistant, read the values from `trigger.json` (for example `{{ trigger.json.place }}`).

### n8n, Node-RED, IFTTT, Zapier
Create a *Webhook* trigger node, paste its URL into a GeoTracker **Webhook** action and send the default JSON. For IFTTT use `https://maker.ifttt.com/trigger/EVENT/with/key/KEY` with a body such as `{"value1": "{{user}}", "value2": "{{place}}"}`.

### ntfy
Pick a long topic name (anyone who knows it can read it on ntfy.sh), subscribe to it in the ntfy app, and add an **ntfy** action with that topic. For your own server, fill in the server address (and an access token if it needs one).

### Telegram
Create a bot with [@BotFather](https://t.me/BotFather) and copy its token. Send your bot a message, then get your chat id from [@userinfobot](https://t.me/userinfobot) (or use a group id). Add a **Telegram** action with the token and chat id.

### Discord and Slack
In Discord: channel settings → Integrations → Webhooks → copy the URL. In Slack: create an *Incoming Webhook* app and copy its URL. Paste it into a **Discord** or **Slack** action.

### Close the garage when you leave
Webhook action, trigger *I leave* at Home, address of your garage controller or Home Assistant webhook. Add a second action **Notify me** so you see that it ran.

## Avoiding noise

- **Wait at least** (default 5 minutes) stops the same event firing again and again when GPS wobbles at the edge of a place. An arrival is never swallowed just because you left a moment before.
- GeoTracker ignores low-accuracy fixes and old batch uploads, and needs one position before the place to know you were really outside.
- Automations are limited to 60 runs per hour per person, and tests to 20 per ten minutes.
- Use a slightly larger place radius (100 m or more) for reliable arrival detection.

## Privacy and security

- Webhook addresses, headers and tokens are stored on the server. After saving, the web interface shows them masked (`https://host/********`) and never returns them again; leave them as they are to keep the saved value.
- Your location is only sent where you configure it. A webhook receives the place, name and coordinates in the message, so use addresses you trust.
- Automations can reach services on your own network (Home Assistant and similar). Link-local addresses such as the cloud metadata service `169.254.169.254` are blocked.
- Creating, changing and deleting automations is recorded in the audit log (names only, never addresses or tokens).

## Troubleshooting

| Symptom | Fix |
|---|---|
| Nothing happens when I arrive | Check the phone is sending locations ([Troubleshooting](Troubleshooting#phones)), that the place radius covers where you stop, and that the automation is enabled. Arrival needs a fix outside the place first. |
| Test works but real events don't | Look at the **Last run** line on the automation. The wait time may still be running, or the position was less accurate than 100 m. |
| Webhook says `HTTP 4xx/5xx` | The other service refused it. Check the address, the method and any required header (for example `Authorization: Bearer …`). |
| Webhook says `connection refused` or times out | GeoTracker must be able to reach the address from the server (in Docker, `localhost` means the container; use the host name or IP instead). |
| `no notification channel is on` | **Notify me** needs at least one channel enabled in Settings → Notifications. |
| `none of the chosen people share a group with you any more` | The people you picked left your family group. Edit the action and choose again. |
