# Administration

## Users

Admin → Users: add people, make someone an admin, disable or delete an account. Admins manage accounts but **cannot see other people's locations**.

Locked out? Reset a password from the command line (see [Command line](#command-line)).

## Backups and restore

**What is backed up:** the whole database (all users' points, places, devices and settings) as a verified, compressed snapshot in `data/backups/`. Snapshots are taken **while the app is running** and checked with an integrity check.

| Backup | When | Kept |
|---|---|---|
| `geotracker-*.db.gz` | Daily at the configured hour (Admin → Settings) | Newest N (default 7) |
| `manual-*.db.gz` | "Back up now" or `geotracker backup` | Until you delete them |
| `pre-upgrade-*.db.gz` | Automatically before any database migration | Newest 3 |
| `pre-restore-*.db.gz` | Automatically before a restore replaces data | Newest 3 |

### Keep a copy off the machine

Backups on the same disk don't survive a dead disk. For example, nightly with restic:

```bash
restic -r sftp:nas:/backups/geotracker backup /srv/geotracker/data/backups
```

or `rclone sync data/backups remote:geotracker-backups`.

**Treat backups as secrets.** Besides location history they contain the SMTP password and each person's Gotify token and Immich API key. Keep off-site copies encrypted (restic encrypts by default; with rclone use a `crypt` remote).

### Restore

**From the UI:** Admin → System → Backups → **Restore** (or **Restore from file** for a downloaded backup). GeoTracker verifies the file, saves a safety copy of the current database and restarts to swap it in. The process must be supervised (Docker restart policy, systemd, NSSM) so it comes back by itself.

**From the shell:**

```bash
# Docker
docker exec geotracker /geotracker restore /data/backups/geotracker-20260101-030000.db.gz
docker restart geotracker

# systemd
sudo -u geotracker geotracker restore /var/lib/geotracker/backups/geotracker-20260101-030000.db.gz
sudo systemctl restart geotracker
```

**Per-person export:** Settings → Import & export → *GeoTracker archive* is a lossless copy of one person's data that can be imported on any instance.

## Upgrading

| Install | Command |
|---|---|
| Docker Compose | `docker compose pull && docker compose up -d` |
| Docker run | pull the new tag, then recreate the container with the same `-v` mount |
| Linux install script | run the install command again (it keeps your data and settings) |
| Manual / macOS / Windows | stop it, replace the binary with the new release, start it |

On start, if the new version needs database changes, GeoTracker first writes `pre-upgrade-*.db.gz`, then migrates in a transaction. If anything fails it stops without touching your data and logs the backup to restore.

**Rolling back:** run the previous version and restore the `pre-upgrade` backup. Migrations are forward-only on purpose, because restoring a backup is the reliable way back. Pin versions (`:0.2` rather than `:latest`) and read the [release notes](https://github.com/anand34577/geo-tracker/releases) before upgrading across versions.

## Moving to a new server

1. Stop GeoTracker on the old server.
2. Copy the whole `data/` folder to the new server.
3. Start GeoTracker there and point your DNS at it.

Phones keep working if the URL stays the same. Alternatively, download a backup in the UI and **Restore from file** on a fresh instance.

**Coming from Dawarich or elsewhere?** Export GPX or GeoJSON there, import it here, then point your phone apps at the new server.

## Command line

The same binary has admin commands (Docker: `docker exec geotracker /geotracker <command>`):

| Command | Does |
|---|---|
| `geotracker serve` | Run the server (the default). |
| `geotracker backup [file]` | Write a backup now. |
| `geotracker restore <file>` | Verify and stage a backup; it is applied on the next start. |
| `GT_NEW_PASSWORD=… geotracker reset-password <email>` | Set a new password (10+ characters) and sign out all sessions. |
| `geotracker healthcheck` | Exit 0 if the server answers (used by Docker). |
| `geotracker version` | Print the version. |

## Monitoring

- `GET /healthz` returns `ok` (HTTP 200) when the server and database are healthy, and 503 otherwise: use it for uptime monitors.
- **Admin → System** shows version, database size, counts and the last backup.
- **Admin → Audit log** lists sign-ins, failed logins and sensitive changes.
- Set `GT_LOG_FORMAT=json` to feed logs to a collector.

## Security checklist

- Serve over [HTTPS](HTTPS) and set `GT_BASE_URL` and `GT_TRUSTED_PROXIES`.
- Keep backups off-site and encrypted.
- Use long passwords (10 characters minimum is enforced) or [single sign-on](Single-Sign-On).
- Pin an image version and update regularly.
- Don't expose the data folder; the container and systemd unit already run as an unprivileged user.
