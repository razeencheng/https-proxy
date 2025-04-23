# HTTPS Proxy Deployment Guide

This directory contains scripts and configuration files to deploy the HTTPS Proxy as a system service.

## System Requirements

- Linux (with systemd) or macOS
- Root/sudo access
- Go 1.19 or later (for building from source)

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

### Step 5: Start the Service

#### On Linux

```bash
sudo systemctl start https-proxy
sudo systemctl enable https-proxy  # To enable auto-start at boot
```

#### On macOS

```bash
sudo launchctl load /Library/LaunchDaemons/com.proxy.https.plist
```

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
└── stats/           # Directory for statistics data

/etc/https-proxy/
├── config.json      # Configuration file
└── certs/           # Directory for certificates
    ├── cert.pem
    ├── key.pem
    └── trustroot.pem

/var/log/https-proxy/  # Log files (macOS only)
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