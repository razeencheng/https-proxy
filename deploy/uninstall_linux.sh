#!/bin/bash

set -e

# Check if script is run as root
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "HTTPS Proxy Uninstallation Script for Linux"
echo "==========================================="

# Define directories
INSTALL_DIR="/opt/https-proxy"
CONFIG_DIR="/etc/https-proxy"
LOG_DIR="/var/log/https-proxy"
SERVICE_FILE="/etc/systemd/system/https-proxy.service"

# Prompt for confirmation
read -p "This will uninstall the HTTPS Proxy. Configuration files can be kept. Continue? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Uninstallation cancelled."
  exit 1
fi

# Stop and disable service
echo "Stopping and disabling service..."
systemctl stop https-proxy || true
systemctl disable https-proxy || true

# Remove service file
echo "Removing service file..."
rm -f "$SERVICE_FILE"
systemctl daemon-reload

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
read -p "Remove https-proxy user and group? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
  echo "Removing user and group..."
  userdel https-proxy 2>/dev/null || true
  groupdel https-proxy 2>/dev/null || true
  echo "User and group removed."
else
  echo "Keeping https-proxy user and group."
fi

echo "Uninstallation complete!"
echo "Thank you for using HTTPS Proxy." 