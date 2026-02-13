#!/bin/bash
#
# Quick upgrader for HTTPS Proxy
# Downloads the latest version from GitHub and runs the appropriate upgrade script
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/upgrade.sh | sudo bash
#

set -e

# Check if script is run as root
if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "╔═══════════════════════════════════════╗"
echo "║     HTTPS Proxy Upgrader              ║"
echo "╚═══════════════════════════════════════╝"
echo ""

# Define directories
INSTALL_DIR="/opt/https-proxy"
CONFIG_DIR="/etc/https-proxy"
STATS_DIR="$INSTALL_DIR/stats"
DATA_DIR="$INSTALL_DIR/data"

# Check existing installation
if [ ! -d "$INSTALL_DIR" ]; then
  echo "Error: No existing installation found at $INSTALL_DIR"
  echo "Please install first with:"
  echo "  curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash"
  exit 1
fi

# Create temp directory
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# ── Detect platform ──
detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$OS" in
    linux)  PLATFORM="linux"  ;;
    darwin) PLATFORM="darwin" ;;
    *)
      echo "Unsupported operating system: $OS"
      exit 1
      ;;
  esac

  case "$ARCH" in
    x86_64|amd64)   GOARCH="amd64" ;;
    arm64|aarch64)  GOARCH="arm64" ;;
    *)
      echo "Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac

  echo "[1/7] Detected platform: $PLATFORM/$GOARCH"
}

# ── Get latest version ──
get_latest_version() {
  echo "[2/7] Checking latest version..."

  if command -v curl &> /dev/null; then
    DOWNLOADER="curl"
    VERSION=$(curl -s https://api.github.com/repos/razeencheng/https-proxy/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
  elif command -v wget &> /dev/null; then
    DOWNLOADER="wget"
    VERSION=$(wget -qO- https://api.github.com/repos/razeencheng/https-proxy/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
  else
    echo "Error: Neither curl nor wget found."
    exit 1
  fi

  if [ -z "$VERSION" ]; then
    echo "Could not determine latest version."
    exit 1
  fi

  # Show current vs new version
  CURRENT=""
  if [ -f "$INSTALL_DIR/https-proxy" ]; then
    CURRENT=$("$INSTALL_DIR/https-proxy" --version 2>/dev/null || echo "unknown")
  fi
  echo "      Current: ${CURRENT:-unknown}"
  echo "      Latest:  $VERSION"
}

# ── Download release ──
download_release() {
  echo "[3/7] Downloading https-proxy $VERSION..."

  DOWNLOAD_URL="https://github.com/razeencheng/https-proxy/releases/download/${VERSION}/https-proxy-${VERSION}-${PLATFORM}-${GOARCH}.tar.gz"

  if [ "$DOWNLOADER" = "curl" ]; then
    curl -fsSL -o "$TEMP_DIR/https-proxy.tar.gz" "$DOWNLOAD_URL"
  else
    wget -q -O "$TEMP_DIR/https-proxy.tar.gz" "$DOWNLOAD_URL"
  fi

  tar -xzf "$TEMP_DIR/https-proxy.tar.gz" -C "$TEMP_DIR"
}

# ── Stop service ──
stop_service() {
  echo "[4/7] Stopping service..."

  if [ "$PLATFORM" = "linux" ]; then
    systemctl stop https-proxy 2>/dev/null || true
  elif [ "$PLATFORM" = "darwin" ]; then
    launchctl unload /Library/LaunchDaemons/com.proxy.https.plist 2>/dev/null || true
  fi
}

# ── Update binary and service files ──
update_binary() {
  echo "[5/7] Updating binary and service files..."

  # Update binary
  cp -f "$TEMP_DIR/https-proxy" "$INSTALL_DIR/"
  chmod 755 "$INSTALL_DIR/https-proxy"

  # Download and update service files
  REPO_RAW_URL="https://raw.githubusercontent.com/razeencheng/https-proxy/$VERSION"
  if [ "$PLATFORM" = "linux" ]; then
    if [ "$DOWNLOADER" = "curl" ]; then
      curl -fsSL -o /etc/systemd/system/https-proxy.service "$REPO_RAW_URL/deploy/systemd/https-proxy.service"
    else
      wget -q -O /etc/systemd/system/https-proxy.service "$REPO_RAW_URL/deploy/systemd/https-proxy.service"
    fi
    systemctl daemon-reload
  elif [ "$PLATFORM" = "darwin" ]; then
    if [ "$DOWNLOADER" = "curl" ]; then
      curl -fsSL -o /Library/LaunchDaemons/com.proxy.https.plist "$REPO_RAW_URL/deploy/launchd/com.proxy.https.plist"
    else
      wget -q -O /Library/LaunchDaemons/com.proxy.https.plist "$REPO_RAW_URL/deploy/launchd/com.proxy.https.plist"
    fi
    chmod 644 /Library/LaunchDaemons/com.proxy.https.plist
  fi

  # Create new directories
  mkdir -p "$STATS_DIR"
  mkdir -p "$DATA_DIR"
}

# ── Download GeoIP database ──
download_geoip() {
  echo "[6/7] Checking GeoIP database..."

  if [ ! -f "$DATA_DIR/GeoLite2-Country.mmdb" ]; then
    echo "      Downloading GeoIP database..."
    GEOIP_URL="https://cdn.jsdelivr.net/gh/wp-statistics/GeoLite2-Country/GeoLite2-Country.mmdb"
    GEOIP_FALLBACK="https://raw.githubusercontent.com/wp-statistics/GeoLite2-Country/main/GeoLite2-Country.mmdb"

    if [ "$DOWNLOADER" = "curl" ]; then
      curl -fsSL -o "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_URL" 2>/dev/null || \
      curl -fsSL -o "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_FALLBACK" 2>/dev/null || \
      echo "      Warning: Failed to download GeoIP database."
    else
      wget -q -O "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_URL" 2>/dev/null || \
      wget -q -O "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_FALLBACK" 2>/dev/null || \
      echo "      Warning: Failed to download GeoIP database."
    fi

    if [ -f "$DATA_DIR/GeoLite2-Country.mmdb" ]; then
      echo "      GeoIP database downloaded."
    fi
  else
    echo "      GeoIP database already exists."
  fi
}

# ── Check config and start service ──
finalize() {
  echo "[7/7] Finalizing..."

  # Fix permissions
  if [ "$PLATFORM" = "linux" ]; then
    chown -R https-proxy:https-proxy "$INSTALL_DIR" 2>/dev/null || true
  elif [ "$PLATFORM" = "darwin" ]; then
    chown -R _https-proxy:_https-proxy "$INSTALL_DIR" 2>/dev/null || true
  fi

  # Check config for missing new fields
  CONFIG_FILE="$CONFIG_DIR/config.json"
  CONFIG_NEEDS_UPDATE=false

  if [ -f "$CONFIG_FILE" ]; then
    if ! grep -q '"geoip"' "$CONFIG_FILE" 2>/dev/null; then
      CONFIG_NEEDS_UPDATE=true
    fi
    if ! grep -q '"db_path"' "$CONFIG_FILE" 2>/dev/null; then
      CONFIG_NEEDS_UPDATE=true
    fi
  fi

  # Start service
  if [ "$PLATFORM" = "linux" ]; then
    systemctl start https-proxy
  elif [ "$PLATFORM" = "darwin" ]; then
    launchctl load /Library/LaunchDaemons/com.proxy.https.plist
  fi

  echo ""
  echo "╔═══════════════════════════════════════════════════════════╗"
  echo "║  ✅ Upgrade to $VERSION complete!                        ║"
  echo "╠═══════════════════════════════════════════════════════════╣"
  echo "║  Dashboard: https://<admin-host>:<admin-port>/dashboard/  ║"
  echo "╚═══════════════════════════════════════════════════════════╝"

  if [ "$CONFIG_NEEDS_UPDATE" = true ]; then
    echo ""
    echo "⚠️  Your config.json needs new fields. Please add to $CONFIG_FILE:"
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
    echo "  Then restart: $([ "$PLATFORM" = "linux" ] && echo "sudo systemctl restart https-proxy" || echo "sudo launchctl unload /Library/LaunchDaemons/com.proxy.https.plist && sudo launchctl load /Library/LaunchDaemons/com.proxy.https.plist")"
  fi
}

# ── Main ──
main() {
  detect_platform
  get_latest_version
  download_release
  stop_service
  update_binary
  download_geoip
  finalize
}

main
