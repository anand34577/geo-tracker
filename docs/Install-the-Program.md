# Install the program (no Docker)

GeoTracker is a single static binary with the web UI built in. Download it, run it, done. Binaries are published with every [release](https://github.com/anand34577/geo-tracker/releases) for:

| Platform | File |
|---|---|
| Linux x86-64 | `geotracker-linux-amd64` |
| Linux ARM64 (Raspberry Pi 4/5 on a 64-bit OS, many ARM servers) | `geotracker-linux-arm64` |
| Linux ARMv7 (Raspberry Pi 3 / 32-bit Raspberry Pi OS) | `geotracker-linux-armv7` |
| macOS Apple Silicon | `geotracker-darwin-arm64` |
| macOS Intel | `geotracker-darwin-amd64` |
| Windows 64-bit | `geotracker-windows-amd64.exe` |

Every release also has a `SHA256SUMS` file to verify downloads.

## Linux and Raspberry Pi: one-line install

Installs the binary, creates a `geotracker` system user and a systemd service that starts on boot and restarts if it stops. It verifies the download against `SHA256SUMS` first.

```bash
curl -fsSL https://github.com/anand34577/geo-tracker/releases/latest/download/install.sh | sudo sh
```

Set options with environment variables:

```bash
curl -fsSL https://github.com/anand34577/geo-tracker/releases/latest/download/install.sh \
  | sudo GT_BASE_URL=https://track.example.com GT_VERSION=v0.1.0 sh
```

| Variable | Default | Meaning |
|---|---|---|
| `GT_VERSION` | latest | Release tag to install |
| `GT_BASE_URL` | empty | Your public URL (set it for production) |
| `GT_LISTEN` | `127.0.0.1:8080` | Listen address. Local-only by default, because you put a [reverse proxy with HTTPS](HTTPS) in front. Use `0.0.0.0:8080` for LAN access without a proxy. |
| `GT_DATA_DIR` | `/var/lib/geotracker` | Where the database and backups live |

Afterwards:

- **Settings** are in `/etc/geotracker.env`. Edit it and run `sudo systemctl restart geotracker`.
- **Logs:** `journalctl -u geotracker -f`
- **Status:** `systemctl status geotracker`
- **Upgrade:** run the same install command again. Your data and settings are kept, and a database backup is made automatically.
- **Uninstall:** `sudo systemctl disable --now geotracker && sudo rm /usr/local/bin/geotracker /etc/systemd/system/geotracker.service` (your data in `/var/lib/geotracker` stays until you delete it).

Next, put Caddy or nginx in front for HTTPS: see [HTTPS](HTTPS).

## Linux: manual install

If you'd rather not run a script:

```bash
# pick the file for your CPU from the table above
curl -LO https://github.com/anand34577/geo-tracker/releases/latest/download/geotracker-linux-amd64
curl -LO https://github.com/anand34577/geo-tracker/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS

sudo useradd --system --home /var/lib/geotracker --create-home --shell /usr/sbin/nologin geotracker
sudo install -m 755 geotracker-linux-amd64 /usr/local/bin/geotracker
sudo curl -L https://raw.githubusercontent.com/anand34577/geo-tracker/main/deploy/geotracker.service \
  -o /etc/systemd/system/geotracker.service
sudoedit /etc/systemd/system/geotracker.service      # set GT_BASE_URL
sudo systemctl enable --now geotracker
```

To try it without a service, run `./geotracker-linux-amd64` and open <http://localhost:8080>. Data goes to `./data`.

> **Keep it supervised.** Restoring a backup from the UI exits the process on purpose so the database can be swapped safely. systemd's `Restart=always` starts it again immediately. Without a supervisor, start it again by hand.

## macOS

```bash
curl -LO https://github.com/anand34577/geo-tracker/releases/latest/download/geotracker-darwin-arm64   # or -amd64 on Intel
chmod +x geotracker-darwin-arm64
xattr -d com.apple.quarantine geotracker-darwin-arm64     # the binary isn't notarised
sudo mv geotracker-darwin-arm64 /usr/local/bin/geotracker
GT_DATA_DIR=~/geotracker-data geotracker
```

Open <http://localhost:8080>. To start it at login, save this as `~/Library/LaunchAgents/com.geotracker.plist`, replacing `YOU` with your user name:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.geotracker</string>
  <key>ProgramArguments</key><array><string>/usr/local/bin/geotracker</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>GT_DATA_DIR</key><string>/Users/YOU/geotracker-data</string>
    <key>GT_LISTEN</key><string>127.0.0.1:8080</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/Users/YOU/geotracker-data/geotracker.log</string>
</dict></plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.geotracker.plist
```

## Windows

1. Download `geotracker-windows-amd64.exe` from the [releases page](https://github.com/anand34577/geo-tracker/releases), put it in a folder such as `C:\GeoTracker\` and rename it `geotracker.exe`.
2. If Windows SmartScreen warns about an unrecognised app, choose **More info → Run anyway** (the binary isn't code-signed). You can compare `Get-FileHash .\geotracker.exe` with `SHA256SUMS` first.
3. Try it in PowerShell:
   ```powershell
   cd C:\GeoTracker
   $env:GT_DATA_DIR = "C:\GeoTracker\data"
   .\geotracker.exe
   ```
   Open <http://localhost:8080>.

### Run it as a Windows service (starts at boot, restarts on failure)

Use [NSSM](https://nssm.cc) (the Non-Sucking Service Manager). In an **administrator** PowerShell:

```powershell
nssm install GeoTracker C:\GeoTracker\geotracker.exe serve
nssm set GeoTracker AppEnvironmentExtra GT_DATA_DIR=C:\GeoTracker\data GT_LISTEN=127.0.0.1:8080 GT_BASE_URL=https://track.example.com
nssm set GeoTracker AppExit Default Restart
nssm set GeoTracker AppStdout C:\GeoTracker\geotracker.log
nssm set GeoTracker AppStderr C:\GeoTracker\geotracker.log
nssm start GeoTracker
```

Open the firewall only if you serve your LAN directly: `New-NetFirewallRule -DisplayName GeoTracker -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow` (and set `GT_LISTEN=0.0.0.0:8080`).

For HTTPS on Windows, run [Caddy for Windows](https://caddyserver.com/docs/install#windows) in front, or use a [Cloudflare Tunnel](HTTPS#cloudflare-tunnel).

## Build from source

Needs Go 1.26+ and Node.js 22+.

```bash
git clone https://github.com/anand34577/geo-tracker && cd geo-tracker
make build          # builds the UI, then ./geotracker with the UI embedded
```

Cross-compile with `GOOS`/`GOARCH` after `make web`:

```bash
make web && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o geotracker-arm64 ./cmd/geotracker
```

## Verify it works

```bash
geotracker version
curl http://127.0.0.1:8080/healthz       # prints: ok
```

## Where things are

| | Linux install script | Manual / Windows / macOS |
|---|---|---|
| Binary | `/usr/local/bin/geotracker` | wherever you put it |
| Data (database, backups, uploads) | `/var/lib/geotracker` | `GT_DATA_DIR` (default `./data`) |
| Settings | `/etc/geotracker.env` | environment variables |

Back up the **data folder**: it is everything. See [Administration](Administration#backups-and-restore).
