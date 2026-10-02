# Getting started

GeoTracker runs as **one process with one data folder** (SQLite inside). Phones send their location to it, and you browse your timeline in any web browser.

## 1. Choose how to run it

| You have | Use | Time |
|---|---|---|
| A VPS or home server with Docker | **[Docker Compose + Caddy](Install-with-Docker#option-a-docker-compose-with-automatic-https-recommended)**: HTTPS is automatic | ~10 min |
| Just want to try it | **[One docker command](Install-with-Docker#option-b-single-docker-command-testing--lan)** | 1 min |
| Linux server or Raspberry Pi, no Docker | **[Install script](Install-the-Program#linux-and-raspberry-pi-one-line-install)** (sets up a systemd service) | ~5 min |
| macOS or Windows | **[Download the program](Install-the-Program#macos)** and run it | ~5 min |

**Requirements:** any 64-bit machine or a Raspberry Pi 3 or newer, 128 MB of free RAM, and about 100 MB of disk per person per year of tracking. To track phones on mobile data you also need a **domain name and HTTPS** (not needed for a LAN-only trial).

## 2. Create your account

Open the address in your browser. On first start a **setup wizard** creates the admin account. Add family members later under **Admin → Users**; everyone's data is private to them (admins cannot see other people's locations).

## 3. Connect a phone

**Settings → Devices → Add device** shows a QR code and the exact values for your tracking app. See **[Connecting phones](Connecting-Phones)**. The first location appears on the map within a minute or two.

## 4. Bring your history (optional)

**Settings → Import & export** accepts Google Timeline (all three export formats), GPX, GeoJSON, CSV and OwnTracks files. See the **[User guide](User-Guide#importing-your-history)**.

## 5. Set up backups

Daily backups are on by default and stored in `data/backups/`. Copy them off the machine: see **[Administration → Backups](Administration#backups-and-restore)**.

## Next steps
[HTTPS](HTTPS) · [Configuration](Configuration) · [Single sign-on](Single-Sign-On) · [Troubleshooting](Troubleshooting)
