# HTTPS Proxy Deployment Guide

This directory contains scripts and configuration files to deploy the HTTPS Proxy as a system service.

## System Requirements

- Linux (with systemd) or macOS
- Root/sudo access
- Go 1.24 or later (for building from source)

## Installation

### Step 1: Build the Proxy (if not already built)

```bash
# From the project root directory
go build -o https-proxy main.go
```

### Step 2: Installation

#### On Linux (systemd)

```bash
# From the project root directory
sudo ./deploy/install_linux.sh
```

#### On macOS

```bash
# From the project root directory
sudo ./deploy/install_macos.sh
```

The installation script will:
1. Create necessary directories
2. Create a service user
3. Copy the binary and configuration
4. Install the service
5. Set appropriate permissions

### Step 3: Configure Certificates

Before starting the service, you need to configure your certificates:

1. Place your certificate files in `/etc/https-proxy/certs/`:
   - `cert.pem` - Server certificate
   - `key.pem` - Server private key
   - `trustroot.pem` - CA certificate for client authentication

### Step 4: Update Configuration

Edit the configuration file at `/etc/https-proxy/config.json` to match your requirements.

### Step 5: (Optional) Enable GeoIP Region Stats

1. The install/upgrade scripts automatically download `GeoLite2-Country.mmdb` to `/opt/https-proxy/data/`
2. If the download failed, manually download from [P3TERX/GeoLite.mmdb](https://github.com/P3TERX/GeoLite.mmdb)
3. Ensure `"geoip": { "enabled": true }` in your config.json

### Step 6: Start the Service

#### On Linux

```bash
sudo systemctl start https-proxy
sudo systemctl enable https-proxy  # To enable auto-start at boot
```

#### On macOS

```bash
sudo launchctl load /Library/LaunchDaemons/com.proxy.https.plist
```

## Upgrade (from a previous version)

If you already have an older version deployed, use the upgrade scripts instead of install scripts:

### On Linux

```bash
# Build the new binary first
make build
# From the project root directory
sudo ./deploy/upgrade_linux.sh
```

### On macOS

```bash
make build
sudo ./deploy/upgrade_macos.sh
```

The upgrade script will:
1. Stop the running service
2. Replace the binary
3. Update the systemd/launchd service file
4. Download GeoIP database (if not present)
5. Check your config.json and prompt for any missing new fields
6. Fix permissions and restart the service

> **Note**: The upgrade script does NOT overwrite your `config.json`. It will tell you which fields need to be added manually.

## Uninstallation

### On Linux

```bash
sudo ./deploy/uninstall_linux.sh
```

### On macOS

```bash
sudo ./deploy/uninstall_macos.sh
```

## Service Logs

### On Linux

```bash
# View service logs
sudo journalctl -u https-proxy
```

### On macOS

```bash
# View service logs
cat /var/log/https-proxy/output.log
cat /var/log/https-proxy/error.log
```

## Directory Structure

After installation, the files will be organized as follows:

```
/opt/https-proxy/
├── https-proxy      # The binary executable
├── stats/           # Directory for statistics data
│   └── proxy_stats.db  # SQLite database
└── data/            # Directory for GeoIP data
    └── GeoLite2-Country.mmdb  # (Optional) MaxMind GeoIP database

/etc/https-proxy/
├── config.json      # Configuration file
└── certs/           # Directory for certificates
    ├── cert.pem
    ├── key.pem
    └── trustroot.pem

/var/log/https-proxy/  # Log files (macOS only)
```

### Dashboard Access

After installation, access the new dashboard at:
```
https://<admin-host>:<admin-port>/dashboard/
```

## Troubleshooting

If you encounter issues:

1. Check the service status:
   - Linux: `sudo systemctl status https-proxy`
   - macOS: `sudo launchctl list | grep com.proxy.https`

2. Verify file permissions:
   - The service user must have access to all necessary files
   - Certificate files should have restrictive permissions

3. Check logs:
   - Linux: `sudo journalctl -u https-proxy`
   - macOS: Check files in `/var/log/https-proxy/`
``` 