#!/bin/sh
# GeoTracker installer for Linux (systemd). Run as root:
#   curl -fsSL https://github.com/anand34577/geo-tracker/releases/latest/download/install.sh | sudo sh
# Options (environment): GT_VERSION=v0.1.0 (default: latest)  GT_BASE_URL=https://track.example.com
#                        GT_LISTEN=127.0.0.1:8080 (default)   GT_DATA_DIR=/var/lib/geotracker
# Re-running upgrades the binary and keeps your data and settings.
set -eu

REPO="anand34577/geo-tracker"
BIN=/usr/local/bin/geotracker
UNIT=/etc/systemd/system/geotracker.service
ENVFILE=/etc/geotracker.env

[ "$(id -u)" -eq 0 ] || { echo "Run as root (use sudo)." >&2; exit 1; }
[ "$(uname -s)" = Linux ] || { echo "This installer is for Linux. See the wiki for macOS and Windows." >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd not found. See the wiki for a manual install." >&2; exit 1; }

case "$(uname -m)" in
  x86_64|amd64)  asset=geotracker-linux-amd64 ;;
  aarch64|arm64) asset=geotracker-linux-arm64 ;;
  armv7l|armv6l) asset=geotracker-linux-armv7 ;;
  *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "${GT_VERSION:-}" ]; then base="https://github.com/$REPO/releases/download/$GT_VERSION"
else base="https://github.com/$REPO/releases/latest/download"; fi

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
echo "Downloading $asset ..."
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"
(cd "$tmp" && grep " $asset\$" SHA256SUMS | sha256sum -c -) || { echo "Checksum mismatch, aborting." >&2; exit 1; }

id geotracker >/dev/null 2>&1 || useradd --system --home "${GT_DATA_DIR:-/var/lib/geotracker}" --create-home --shell /usr/sbin/nologin geotracker
upgrade=0; [ -x "$BIN" ] && upgrade=1
systemctl stop geotracker 2>/dev/null || true
install -m 755 "$tmp/$asset" "$BIN"

# Settings live in /etc/geotracker.env and are never overwritten on upgrade.
if [ ! -f "$ENVFILE" ]; then
  cat > "$ENVFILE" <<EOF
GT_DATA_DIR=${GT_DATA_DIR:-/var/lib/geotracker}
GT_LISTEN=${GT_LISTEN:-127.0.0.1:8080}
GT_BASE_URL=${GT_BASE_URL:-}
# GT_TRUSTED_PROXIES=127.0.0.1/32
# TZ=Europe/Berlin
EOF
  chmod 640 "$ENVFILE"; chown root:geotracker "$ENVFILE"
fi

cat > "$UNIT" <<EOF
[Unit]
Description=GeoTracker location timeline
After=network-online.target
Wants=network-online.target

[Service]
User=geotracker
Group=geotracker
EnvironmentFile=$ENVFILE
ExecStart=$BIN serve
Restart=always
RestartSec=2
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${GT_DATA_DIR:-/var/lib/geotracker}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now geotracker
echo
if [ "$upgrade" = 1 ]; then echo "Upgraded to $("$BIN" version)."; else echo "Installed GeoTracker $("$BIN" version)."; fi
echo "Settings: $ENVFILE (edit, then: systemctl restart geotracker)"
echo "Listening on $(grep ^GT_LISTEN= "$ENVFILE" | cut -d= -f2). Put a reverse proxy with HTTPS in front of it:"
echo "  https://github.com/$REPO/wiki/HTTPS"
