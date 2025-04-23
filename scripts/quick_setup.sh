#!/bin/bash
#
# Quick setup script for HTTPS Proxy local development
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

# Create necessary directories
mkdir -p ./stats
mkdir -p ./certs

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is required but not installed."
    exit 1
fi

# Generate certificates if they don't exist
if [ ! -f "./certs/cert.pem" ] || [ ! -f "./certs/key.pem" ]; then
    echo "Certificates not found. Generating self-signed certificates..."
    bash "$SCRIPT_DIR/generate_certs.sh"
fi

# Create config.json if it doesn't exist
if [ ! -f "./config.json" ]; then
    echo "Config file not found. Creating from sample..."
    cp ./config.sample.json ./config.json
fi

# Build the application
echo "Building HTTPS Proxy..."
go build -o https-proxy .

echo ""
echo "Setup complete! You can now run the HTTPS Proxy using:"
echo "./https-proxy"
echo ""
echo "Or with Docker Compose:"
echo "docker-compose up --build"
echo ""
echo "After starting, access the admin panel at: https://localhost:8444"
echo "Configure your browser to use the proxy at: localhost:8443"
echo ""
echo "For testing with curl:"
echo "curl --proxy https://localhost:8443 --cert ./certs/client_combined.pem --cacert ./certs/ca.pem https://example.com"
echo ""

# Ask if user wants to run the proxy now
read -p "Do you want to run the HTTPS Proxy now? (y/n): " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Starting HTTPS Proxy..."
    ./https-proxy
fi

chmod +x "$0" 