# HTTPS PROXY

A secure HTTPS proxy server that requires clients to authenticate using client certificates, with administration panel and statistics tracking.

## Features

- Uses client certificates to authenticate clients
- Verifies client certificate chain against trusted CA
- Verifies certificate is intended for client authentication (using KeyUsage)
- Tracks user traffic statistics by client certificate CommonName
- Provides a web-based admin panel for monitoring user traffic
- Bilingual interface with English/Chinese language support
- Handles unauthorized requests with appropriate responses
- Configurable via JSON configuration file
- Available as a system service for Linux (systemd) and macOS
- Docker support for containerized deployment

## Getting Started

### Prerequisites

- Go 1.19 or higher (for building from source)
- OpenSSL (for generating certificates)
- Linux with systemd or macOS (for service installation)
- Docker and Docker Compose (optional, for containerized deployment)

### Quick Setup

For local development and testing, use the quick setup script:

```bash
# Clone the repository
git clone https://git.isw.app/homelab/https-proxy.git
cd https-proxy

# Run the quick setup script
./scripts/quick_setup.sh
```

This script will:
1. Generate self-signed certificates if they don't exist
2. Create a default configuration file
3. Build the application
4. Optionally start the proxy

### Easy Installation

The easiest way to install the latest release is by using our installer script:

```bash
# For Linux (requires sudo)
curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash

# For macOS (requires sudo)
curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash
```

This script automatically:
1. Detects your operating system and architecture
2. Downloads the latest release binary
3. Installs the proxy as a system service
4. Creates default configuration and certificate directories

### Manual Installation as a Service

If you prefer to install manually:

#### Linux (systemd)

```bash
# Download and extract the latest release manually, then run:
sudo ./deploy/install_linux.sh
```

#### macOS

```bash
# Download and extract the latest release manually, then run:
sudo ./deploy/install_macos.sh
```

For more details, see the [deployment guide](deploy/README.md).

### Docker Deployment

```bash
# Build and start the container
docker-compose up -d

# View logs
docker-compose logs -f
```

## Authentication Process

The proxy performs certificate verification:

1. **Certificate Chain Verification**: Ensures the client certificate is signed by the trusted CA certificate specified in the configuration.
2. **Certificate Usage Verification**: Ensures the certificate is intended for client authentication.

Only clients passing the verification steps are authorized to use the proxy for CONNECT requests.

## Configuration

The proxy is configured through a JSON configuration file. You can use the sample configuration as a starting point:

```bash
cp config.sample.json config.json
```

### Configuration Structure

```json
{
  "server": {
    "address": "0.0.0.0:8443",
    "cert_file": "./certs/cert.pem",
    "key_file": "./certs/key.pem",
    "language": "en"
  },
  "proxy": {
    "trust_root_file": "./certs/trustroot.pem",
    "auth_required": true
  },
  "stats": {
    "enabled": true,
    "save_interval": 300,
    "file_path": "./stats/proxy_stats.json"
  },
  "admin": {
    "enabled": true,
    "address": "127.0.0.1:8444",
    "cert_file": "./certs/admin_cert.pem",
    "key_file": "./certs/admin_key.pem"
  }
}
```

See the [user manual](docs/user_manual.md) for detailed configuration options.

## User Statistics

The proxy tracks usage statistics for authenticated users:

- Users are identified by their certificate's Subject CommonName
- Traffic volume (bytes) is tracked in real-time
- Connection counts and request counts are maintained
- Statistics are saved to a JSON file periodically (configurable)
- Statistics are preserved between server restarts

## Admin Panel

The admin panel provides visibility into the proxy's operations:

- Displays real-time user statistics and connection details
- Provides per-user detailed views with usage graphs
- Supports language switching between English and Chinese
- Offers REST API endpoints for integration with other systems
- Auto-refreshes data periodically

### Admin Panel API Endpoints

- `GET /api/stats`: Get statistics for all users
- `GET /api/stats/user/{username}`: Get statistics for a specific user
- `GET /api/config`: Get server configuration information

All API endpoints require client certificate authentication.

## Certificate Management

For testing purposes, you can generate self-signed certificates:

```bash
./scripts/generate_certs.sh
```

This will create:
- A self-signed CA certificate
- A server certificate for the proxy
- An admin server certificate
- A client certificate for testing

For production use, you should use certificates from a trusted Certificate Authority or your organization's internal CA.

## Uninstallation

### Linux

```bash
sudo ./deploy/uninstall_linux.sh
```

### macOS

```bash
sudo ./deploy/uninstall_macos.sh
```

## Docker Management

```bash
# Start the service
docker-compose up -d

# Stop the service
docker-compose down

# View logs
docker-compose logs -f

# Rebuild and restart
docker-compose up -d --build
```

## Creating New Releases

This project uses GitHub Actions to automatically build and release binaries. To create a new release:

1. Tag the commit you want to release:
   ```bash
   git tag -a v1.0.0 -m "Version 1.0.0"
   ```

2. Push the tag to GitHub:
   ```bash
   git push github v1.0.0
   ```

3. The GitHub Action will automatically:
   - Build binaries for multiple platforms (Linux, macOS, Windows)
   - Create a GitHub Release with the binaries attached
   - Make the release available for download

## Troubleshooting

See the [user manual](docs/user_manual.md) for common issues and troubleshooting steps.

## License

This software is provided under the terms of the MIT License.
