#!/bin/bash

set -e

# Check if script is run as root
if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "HTTPS Proxy Uninstallation Script for macOS"
echo "==========================================="

# Define directories
INSTALL_DIR="/opt/https-proxy"
CONFIG_DIR="/etc/https-proxy"
LOG_DIR="/var/log/https-proxy"
PLIST_PATH="/Library/LaunchDaemons/com.proxy.https.plist"

# Prompt for confirmation
read -p "This will uninstall the HTTPS Proxy. Configuration files can be kept. Continue? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Uninstallation cancelled."
  exit 1
fi

# Stop service
echo "Stopping service..."
launchctl unload "$PLIST_PATH" 2>/dev/null || true

# Remove service file
echo "Removing service file..."
rm -f "$PLIST_PATH"

# Ask if configuration should be kept
read -p "Keep configuration files? (Y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Nn]$ ]]; then
  echo "Removing configuration files..."
  rm -rf "$CONFIG_DIR"
else
  echo "Keeping configuration files in $CONFIG_DIR"
fi

# Remove application files
echo "Removing application files..."
rm -rf "$INSTALL_DIR"

# Remove logs
echo "Removing log files..."
rm -rf "$LOG_DIR"

# Prompt about user and group
read -p "Remove _https-proxy user and group? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
  echo "Removing user and group..."
  dscl . -delete /Users/_https-proxy 2>/dev/null || true
  dscl . -delete /Groups/_https-proxy 2>/dev/null || true
  echo "User and group removed."
else
  echo "Keeping _https-proxy user and group."
fi

echo "Uninstallation complete!"
echo "Thank you for using HTTPS Proxy." 