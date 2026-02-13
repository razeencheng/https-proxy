#!/bin/bash

set -e

# Check if script is run as root
if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "HTTPS Proxy Installation Script for macOS"
echo "=========================================="

# Define directories
INSTALL_DIR="/opt/https-proxy"
CONFIG_DIR="/etc/https-proxy"
LOG_DIR="/var/log/https-proxy"
CERT_DIR="$CONFIG_DIR/certs"
STATS_DIR="$INSTALL_DIR/stats"
DATA_DIR="$INSTALL_DIR/data"
PLIST_PATH="/Library/LaunchDaemons/com.proxy.https.plist"

# Create directories
echo "Creating directories..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$CONFIG_DIR"
mkdir -p "$LOG_DIR"
mkdir -p "$CERT_DIR"
mkdir -p "$STATS_DIR"
mkdir -p "$DATA_DIR"

# Create user and group if they don't exist
echo "Creating service user..."
dscl . -list /Users | grep -q "^_https-proxy$" || \
  dscl . -create /Users/_https-proxy UserShell /usr/bin/false

dscl . -list /Groups | grep -q "^_https-proxy$" || \
  dscl . -create /Groups/_https-proxy

# Determine system architecture
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
  GOARCH="amd64"
elif [ "$ARCH" = "arm64" ]; then
  GOARCH="arm64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi

# Copy or download application files
if [ -f "https-proxy" ]; then
  echo "Using local binary..."
  cp -f https-proxy "$INSTALL_DIR/"
else
  echo "Local binary not found. Attempting to download from GitHub..."
  
  # Check if curl or wget is available
  if command -v curl &> /dev/null; then
    DOWNLOADER="curl -L -o"
  elif command -v wget &> /dev/null; then
    DOWNLOADER="wget -O"
  else
    echo "Error: Neither curl nor wget found. Please install one of them or provide a local binary."
    exit 1
  fi
  
  # Try to determine latest version if not specified
  if [ -z "$VERSION" ]; then
    echo "Determining latest release version..."
    if command -v curl &> /dev/null; then
      VERSION=$(curl -s https://api.github.com/repos/razeencheng/https-proxy/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
    elif command -v wget &> /dev/null; then
      VERSION=$(wget -qO- https://api.github.com/repos/razeencheng/https-proxy/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
    fi
    
    if [ -z "$VERSION" ]; then
      echo "Could not determine latest version. Please specify VERSION manually."
      exit 1
    fi
    echo "Latest version is: $VERSION"
  fi
  
  DOWNLOAD_URL="https://github.com/razeencheng/https-proxy/releases/download/${VERSION}/https-proxy-${VERSION}-darwin-${GOARCH}.tar.gz"
  TEMP_DIR=$(mktemp -d)
  
  echo "Downloading from: $DOWNLOAD_URL"
  $DOWNLOADER "$TEMP_DIR/https-proxy.tar.gz" "$DOWNLOAD_URL"
  
  tar -xzf "$TEMP_DIR/https-proxy.tar.gz" -C "$TEMP_DIR"
  cp "$TEMP_DIR/https-proxy" "$INSTALL_DIR/"
  rm -rf "$TEMP_DIR"
fi

# Copy default configuration if it doesn't exist
if [ ! -f "$CONFIG_DIR/config.json" ]; then
  echo "Creating default configuration..."
  if [ -f "config.json" ]; then
    cp -f config.json "$CONFIG_DIR/"
  elif [ -f "config.sample.json" ]; then
    cp -f config.sample.json "$CONFIG_DIR/config.json"
  else
    # Basic configuration template
    cat > "$CONFIG_DIR/config.json" << EOF
{
  "server": {
    "address": "0.0.0.0:8443",
    "cert_file": "$CERT_DIR/cert.pem",
    "key_file": "$CERT_DIR/key.pem",
    "language": "en"
  },
  "proxy": {
    "trust_root_file": "$CERT_DIR/trustroot.pem",
    "auth_required": true
  },
  "geoip": {
    "enabled": true,
    "db_path": "$DATA_DIR/GeoLite2-Country.mmdb"
  },
  "stats": {
    "enabled": true,
    "db_path": "$STATS_DIR/proxy_stats.db",
    "file_path": "$STATS_DIR/proxy_stats.json",
    "flush_interval_seconds": 30,
    "retention": {
      "minute_stats_days": 7,
      "hourly_stats_days": 90
    }
  },
  "admin": {
    "enabled": true,
    "address": "127.0.0.1:8444",
    "cert_file": "$CERT_DIR/admin_cert.pem",
    "key_file": "$CERT_DIR/admin_key.pem"
  }
}
EOF
  fi
fi

# Copy certificate files if they exist, otherwise create placeholders
if [ -d "conf" ]; then
  echo "Copying certificate files..."
  cp -f conf/*.pem "$CERT_DIR/" 2>/dev/null || echo "No certificate files found. You'll need to add these manually."
else
  echo "Certificate directory not found. You'll need to add certificates manually."
  touch "$CERT_DIR/README"
  echo "Place your cert.pem, key.pem, and trustroot.pem files in this directory." > "$CERT_DIR/README"
fi

# Download GeoIP database
echo "Downloading GeoIP database..."
GEOIP_URL="https://raw.githubusercontent.com/P3TERX/GeoLite.mmdb/download/GeoLite2-Country.mmdb"
if [ ! -f "$DATA_DIR/GeoLite2-Country.mmdb" ]; then
  if command -v curl &> /dev/null; then
    curl -fsSL -o "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_URL" 2>/dev/null || \
    echo "Warning: Failed to download GeoIP database. Region stats will be disabled."
  elif command -v wget &> /dev/null; then
    wget -q -O "$DATA_DIR/GeoLite2-Country.mmdb" "$GEOIP_URL" 2>/dev/null || \
    echo "Warning: Failed to download GeoIP database. Region stats will be disabled."
  fi
  if [ -f "$DATA_DIR/GeoLite2-Country.mmdb" ]; then
    echo "GeoIP database downloaded successfully."
  fi
else
  echo "GeoIP database already exists, skipping download."
fi

# Install launchd service
echo "Installing launchd service..."
if [ -f "deploy/launchd/com.proxy.https.plist" ]; then
  cp -f deploy/launchd/com.proxy.https.plist "$PLIST_PATH"
else
  cat > "$PLIST_PATH" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.proxy.https</string>
    <key>ProgramArguments</key>
    <array>
        <string>$INSTALL_DIR/https-proxy</string>
        <string>--config</string>
        <string>$CONFIG_DIR/config.json</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>$INSTALL_DIR</string>
    <key>StandardErrorPath</key>
    <string>$LOG_DIR/error.log</string>
    <key>StandardOutPath</key>
    <string>$LOG_DIR/output.log</string>
    <key>ThrottleInterval</key>
    <integer>5</integer>
</dict>
</plist>
EOF
fi

# Set permissions
echo "Setting permissions..."
chown -R _https-proxy:_https-proxy "$INSTALL_DIR"
chown -R _https-proxy:_https-proxy "$LOG_DIR"
chmod 755 "$INSTALL_DIR/https-proxy"
chmod 750 "$CONFIG_DIR"
chmod 640 "$CONFIG_DIR/config.json"
if [ -d "$CERT_DIR" ] && [ "$(ls -A "$CERT_DIR")" ]; then
  chmod -R 600 "$CERT_DIR"/*
fi
chmod 750 "$CERT_DIR"
chmod 644 "$PLIST_PATH"

echo "Installation complete!"
echo
echo "To start the service, run: launchctl load $PLIST_PATH"
echo "The service will also start automatically on system boot."
echo
echo "Remember to:"
echo "1. Configure your certificates in $CERT_DIR/"
echo "2. Review and update your configuration in $CONFIG_DIR/config.json"
echo "3. (Optional) Download MaxMind GeoLite2-Country.mmdb to $DATA_DIR/ for region stats"
echo "4. Access the new Dashboard at https://<admin-host>:<admin-port>/dashboard/"
echo
echo "Thank you for installing HTTPS Proxy!" 