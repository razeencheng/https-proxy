#!/bin/bash

set -e

# Check if script is run as root
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "HTTPS Proxy Upgrade Script for Linux"
echo "======================================"

# Define directories
INSTALL_DIR="/opt/https-proxy"
CONFIG_DIR="/etc/https-proxy"
STATS_DIR="$INSTALL_DIR/stats"
DATA_DIR="$INSTALL_DIR/data"

# Verify existing installation
if [ ! -d "$INSTALL_DIR" ]; then
  echo "Error: No existing installation found at $INSTALL_DIR"
  echo "Please run install_linux.sh for a fresh installation."
  exit 1
fi

# Create new directories (may not exist in older versions)
echo "Ensuring directories exist..."
mkdir -p "$STATS_DIR"
mkdir -p "$DATA_DIR"

# ── Step 1: Update binary ──
echo ""
echo "=== Step 1: Updating binary ==="
if [ -f "https-proxy" ]; then
  echo "Using local binary..."
  # Stop service before replacing binary
  echo "Stopping service..."
  systemctl stop https-proxy 2>/dev/null || true
  cp -f https-proxy "$INSTALL_DIR/"
  chmod 755 "$INSTALL_DIR/https-proxy"
  echo "Binary updated."
else
  echo "Error: Local binary 'https-proxy' not found in current directory."
  echo "Please build the binary first: make build"
  exit 1
fi

# ── Step 2: Update systemd service ──
echo ""
echo "=== Step 2: Updating systemd service ==="
if [ -f "deploy/systemd/https-proxy.service" ]; then
  cp -f deploy/systemd/https-proxy.service /etc/systemd/system/
  systemctl daemon-reload
  echo "Systemd service updated."
else
  echo "Warning: deploy/systemd/https-proxy.service not found, skipping."
fi

# ── Step 3: Download GeoIP database (if not present) ──
echo ""
echo "=== Step 3: GeoIP Database ==="
GEOIP_URL="https://cdn.jsdelivr.net/gh/wp-statistics/GeoLite2-Country/GeoLite2-Country.mmdb"
GEOIP_FALLBACK_URL="https://raw.githubusercontent.com/wp-statistics/GeoLite2-Country/main/GeoLite2-Country.mmdb"
if [ ! -f "$DATA_DIR/GeoLite2-Country.mmdb" ]; then
  echo "Downloading GeoIP database..."
  if command -v curl &> /dev/null; then
    curl -fsSL -o "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_URL" 2>/dev/null || \
    curl -fsSL -o "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_FALLBACK_URL" 2>/dev/null || \
    echo "Warning: Failed to download GeoIP database."
  elif command -v wget &> /dev/null; then
    wget -q -O "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_URL" 2>/dev/null || \
    wget -q -O "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_FALLBACK_URL" 2>/dev/null || \
    echo "Warning: Failed to download GeoIP database."
  fi
  if [ -f "$DATA_DIR/GeoLite2-Country.mmdb" ]; then
    echo "GeoIP database downloaded."
  fi
else
  echo "GeoIP database already exists. To update, delete and re-run:"
  echo "  rm $DATA_DIR/GeoLite2-Country.mmdb && sudo ./deploy/upgrade_linux.sh"
fi

# ── Step 4: Update config.json (add new fields if missing) ──
echo ""
echo "=== Step 4: Configuration ==="
CONFIG_FILE="$CONFIG_DIR/config.json"
if [ -f "$CONFIG_FILE" ]; then
  NEEDS_UPDATE=false

  # Check if geoip section exists
  if ! grep -q '"geoip"' "$CONFIG_FILE" 2>/dev/null; then
    NEEDS_UPDATE=true
    echo "Note: 'geoip' section missing in config.json"
  fi

  # Check if db_path exists in stats section
  if ! grep -q '"db_path"' "$CONFIG_FILE" 2>/dev/null; then
    NEEDS_UPDATE=true
    echo "Note: 'db_path' missing in stats config"
  fi

  if [ "$NEEDS_UPDATE" = true ]; then
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║  Your config.json needs manual updates for the new fields.  ║"
    echo "║  Please add the following to your $CONFIG_FILE:             ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo '  "geoip": {'
    echo '    "enabled": true,'
    echo "    \"db_path\": \"$DATA_DIR/GeoLite2-Country.mmdb\""
    echo '  },'
    echo ''
    echo '  In the "stats" section, add:'
    echo "    \"db_path\": \"$STATS_DIR/proxy_stats.db\","
    echo '    "flush_interval_seconds": 30,'
    echo '    "retention": {'
    echo '      "minute_stats_days": 7,'
    echo '      "hourly_stats_days": 90'
    echo '    }'
    echo ""
  else
    echo "Config appears up-to-date."
  fi
else
  echo "Warning: $CONFIG_FILE not found!"
fi

# ── Step 5: Fix permissions ──
echo ""
echo "=== Step 5: Fixing permissions ==="
chown -R https-proxy:https-proxy "$INSTALL_DIR"
echo "Permissions set."

# ── Step 6: Restart service ──
echo ""
echo "=== Step 6: Restarting service ==="
systemctl start https-proxy
echo "Service restarted."

echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║           Upgrade complete!                       ║"
echo "╠═══════════════════════════════════════════════════╣"
echo "║  Check status:  systemctl status https-proxy      ║"
echo "║  View logs:     journalctl -u https-proxy -f      ║"
echo "║  Dashboard:     https://<admin>:<port>/dashboard/  ║"
echo "╚═══════════════════════════════════════════════════╝"
