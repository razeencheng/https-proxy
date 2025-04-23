#!/bin/bash
#
# Quick installer for HTTPS Proxy
# Downloads the latest version from GitHub and runs the appropriate install script
#

set -e

# Check if script is run as root
if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "HTTPS Proxy Installer"
echo "====================="

# Create temp directory
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# Function to detect OS and architecture
detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  
  case "$OS" in
    linux)
      PLATFORM="linux"
      ;;
    darwin)
      PLATFORM="darwin"
      ;;
    *)
      echo "Unsupported operating system: $OS"
      exit 1
      ;;
  esac
  
  case "$ARCH" in
    x86_64)
      GOARCH="amd64"
      ;;
    amd64)
      GOARCH="amd64"
      ;;
    arm64|aarch64)
      GOARCH="arm64"
      ;;
    *)
      echo "Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac
  
  echo "Detected platform: $PLATFORM/$GOARCH"
}

# Function to determine latest release version
get_latest_version() {
  echo "Determining latest release version..."
  
  if command -v curl &> /dev/null; then
    VERSION=$(curl -s https://api.github.com/repos/razeencheng/https-proxy/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
  elif command -v wget &> /dev/null; then
    VERSION=$(wget -qO- https://api.github.com/repos/razeencheng/https-proxy/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
  else
    echo "Error: Neither curl nor wget found. Please install one of them."
    exit 1
  fi
  
  if [ -z "$VERSION" ]; then
    echo "Could not determine latest version."
    exit 1
  fi
  
  echo "Latest version: $VERSION"
}

# Function to download the release
download_release() {
  echo "Downloading HTTPS Proxy $VERSION for $PLATFORM/$GOARCH..."
  
  DOWNLOAD_URL="https://github.com/razeencheng/https-proxy/releases/download/${VERSION}/https-proxy-${VERSION}-${PLATFORM}-${GOARCH}.tar.gz"
  
  if command -v curl &> /dev/null; then
    curl -L -o "$TEMP_DIR/https-proxy.tar.gz" "$DOWNLOAD_URL"
  elif command -v wget &> /dev/null; then
    wget -O "$TEMP_DIR/https-proxy.tar.gz" "$DOWNLOAD_URL"
  fi
  
  echo "Extracting archive..."
  tar -xzf "$TEMP_DIR/https-proxy.tar.gz" -C "$TEMP_DIR"
}

# Function to download install scripts
download_install_scripts() {
  echo "Downloading installation scripts..."
  
  # Define URLs for raw content from GitHub
  REPO_RAW_URL="https://raw.githubusercontent.com/razeencheng/https-proxy/$VERSION"
  
  # Download necessary files
  if command -v curl &> /dev/null; then
    curl -L -o "$TEMP_DIR/deploy/install_linux.sh" "$REPO_RAW_URL/deploy/install_linux.sh"
    curl -L -o "$TEMP_DIR/deploy/install_macos.sh" "$REPO_RAW_URL/deploy/install_macos.sh"
    curl -L -o "$TEMP_DIR/config.sample.json" "$REPO_RAW_URL/config.sample.json"
    
    # Download service files
    mkdir -p "$TEMP_DIR/deploy/systemd" "$TEMP_DIR/deploy/launchd"
    curl -L -o "$TEMP_DIR/deploy/systemd/https-proxy.service" "$REPO_RAW_URL/deploy/systemd/https-proxy.service"
    curl -L -o "$TEMP_DIR/deploy/launchd/com.proxy.https.plist" "$REPO_RAW_URL/deploy/launchd/com.proxy.https.plist"
  elif command -v wget &> /dev/null; then
    wget -O "$TEMP_DIR/deploy/install_linux.sh" "$REPO_RAW_URL/deploy/install_linux.sh"
    wget -O "$TEMP_DIR/deploy/install_macos.sh" "$REPO_RAW_URL/deploy/install_macos.sh"
    wget -O "$TEMP_DIR/config.sample.json" "$REPO_RAW_URL/config.sample.json"
    
    # Download service files
    mkdir -p "$TEMP_DIR/deploy/systemd" "$TEMP_DIR/deploy/launchd"
    wget -O "$TEMP_DIR/deploy/systemd/https-proxy.service" "$REPO_RAW_URL/deploy/systemd/https-proxy.service"
    wget -O "$TEMP_DIR/deploy/launchd/com.proxy.https.plist" "$REPO_RAW_URL/deploy/launchd/com.proxy.https.plist"
  fi
  
  # Make scripts executable
  chmod +x "$TEMP_DIR/deploy/install_linux.sh" "$TEMP_DIR/deploy/install_macos.sh"
}

# Function to run the appropriate installer
run_installer() {
  echo "Running installer for $PLATFORM..."
  cd "$TEMP_DIR"
  
  # Run the appropriate installer based on platform
  if [ "$PLATFORM" = "linux" ]; then
    ./deploy/install_linux.sh
  elif [ "$PLATFORM" = "darwin" ]; then
    ./deploy/install_macos.sh
  fi
}

# Main function
main() {
  # Detect platform
  detect_platform
  
  # Get latest version
  get_latest_version
  
  # Create directory structure
  mkdir -p "$TEMP_DIR/deploy"
  
  # Download release
  download_release
  
  # Download install scripts
  download_install_scripts
  
  # Run installer
  run_installer
  
  echo "Installation completed!"
}

# Run main function
main 