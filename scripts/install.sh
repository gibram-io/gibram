#!/bin/sh
# GibRAM Installer Script
# Usage: curl -fsSL https://gibram.io/install.sh | sh

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Constants
REPO="gibram-io/gibram"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/gibram"
BINARY_NAME="gibram-server"

# Helper functions
info() {
    printf "${GREEN}[INFO]${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1"
}

error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1"
    exit 1
}

# Detect OS
detect_os() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$OS" in
        linux*)
            OS="linux"
            ;;
        darwin*)
            OS="darwin"
            ;;
        freebsd*)
            OS="freebsd"
            ;;
        *)
            error "Unsupported OS: $OS. Supported: linux, darwin, freebsd"
            ;;
    esac
    echo "$OS"
}

# Detect architecture
detect_arch() {
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH. Supported: amd64, arm64"
            ;;
    esac
    echo "$ARCH"
}

# Get latest release version
get_latest_version() {
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        error "Failed to fetch latest version from GitHub"
    fi
    echo "$VERSION"
}

# Download and install binary
install_binary() {
    OS=$(detect_os)
    ARCH=$(detect_arch)
    VERSION=$(get_latest_version)
    
    info "Detected platform: $OS/$ARCH"
    info "Latest version: $VERSION"
    
    BINARY_URL="https://github.com/$REPO/releases/download/$VERSION/gibram-$OS-$ARCH.tar.gz"
    TMPDIR=$(mktemp -d)
    
    info "Downloading from $BINARY_URL..."
    if ! curl -fsSL "$BINARY_URL" -o "$TMPDIR/gibram.tar.gz"; then
        error "Failed to download binary. Make sure release exists for $OS/$ARCH"
    fi
    
    info "Extracting archive..."
    tar -xzf "$TMPDIR/gibram.tar.gz" -C "$TMPDIR"
    
    info "Installing to $INSTALL_DIR/$BINARY_NAME..."
    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMPDIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
        chmod +x "$INSTALL_DIR/$BINARY_NAME"
    else
        sudo mv "$TMPDIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
        sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"
    fi
    
    rm -rf "$TMPDIR"
    info "Binary installed successfully!"
}

# Create config file
create_config() {
    if [ -f "$CONFIG_DIR/config.yaml" ]; then
        warn "Config file already exists at $CONFIG_DIR/config.yaml"
        return
    fi
    
    info "Creating config directory at $CONFIG_DIR..."
    if [ -w "/etc" ]; then
        mkdir -p "$CONFIG_DIR"
    else
        sudo mkdir -p "$CONFIG_DIR"
    fi
    
    info "Creating default config file..."
    CONFIG_CONTENT="# GibRAM Configuration File
# Graph in-Buffer Retrieval & Associative Memory
# Copy from config.example.yaml and customize

server:
  addr: \":6161\"
  data_dir: \"./data\"
  vector_dim: 1536

tls:
  # PRODUCTION: Use custom certificates
  # Generate with: openssl req -x509 -newkey rsa:4096 -nodes \\
  #   -keyout server.key -out server.crt -days 365 -subj \"/CN=yourdomain.com\"
  cert_file: \"\"  # Path to certificate file
  key_file: \"\"   # Path to private key file
  
  # DEVELOPMENT: Auto-generate self-signed certificate
  auto_cert: true
  
  # INSECURE MODE (DEV ONLY): Start with --insecure flag to disable TLS
  # gibram-server --insecure

auth:
  keys:
    # Admin key - full access
    - id: \"admin\"
      key: \"gibram_admin_change_me_in_production\"
      permissions: [\"admin\"]
    
    # Application key - read/write access
    - id: \"app-service\"
      key: \"gibram_app_change_me_in_production\"
      permissions: [\"write\"]
    
    # Read-only key
    - id: \"query-service\"
      key: \"gibram_query_change_me_in_production\"
      permissions: [\"read\"]

security:
  max_frame_size: 4194304  # 4MB
  rate_limit: 1000         # requests per second
  rate_burst: 100
  idle_timeout: 300s
  unauth_timeout: 10s
  max_conns_per_ip: 50

logging:
  level: \"info\"    # debug, info, warn, error
  format: \"text\"   # json, text
  output: \"stdout\" # stdout, file
  file: \"\"         # log file path if output=file
"
    
    if [ -w "$CONFIG_DIR" ]; then
        echo "$CONFIG_CONTENT" > "$CONFIG_DIR/config.yaml"
    else
        echo "$CONFIG_CONTENT" | sudo tee "$CONFIG_DIR/config.yaml" > /dev/null
    fi
    
    info "Config file created at $CONFIG_DIR/config.yaml"
}

# Setup systemd service (Linux only)
setup_systemd() {
    if [ "$OS" != "linux" ]; then
        return
    fi
    
    printf "\nDo you want to install systemd service? (y/N): "
    read -r REPLY
    if [ "$REPLY" != "y" ] && [ "$REPLY" != "Y" ]; then
        return
    fi
    
    SERVICE_FILE="/etc/systemd/system/gibram.service"
    SERVICE_CONTENT="[Unit]
Description=GibRAM - Graph in-Buffer Retrieval & Associative Memory
After=network.target

[Service]
Type=simple
User=gibram
Group=gibram
ExecStart=$INSTALL_DIR/$BINARY_NAME --config $CONFIG_DIR/config.yaml
Restart=on-failure
RestartSec=5s

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$CONFIG_DIR /var/log/gibram

[Install]
WantedBy=multi-user.target
"
    
    info "Creating systemd service..."
    echo "$SERVICE_CONTENT" | sudo tee "$SERVICE_FILE" > /dev/null
    
    info "Creating gibram user..."
    if ! id -u gibram > /dev/null 2>&1; then
        sudo useradd -r -s /bin/false gibram
    fi
    
    sudo chown -R gibram:gibram "$CONFIG_DIR"
    
    sudo systemctl daemon-reload
    sudo systemctl enable gibram.service
    
    info "Systemd service installed!"
    info "Start with: sudo systemctl start gibram"
    info "Check status: sudo systemctl status gibram"
}

# Print success message
print_success() {
    echo ""
    info "============================================"
    info "GibRAM installed successfully!"
    info "============================================"
    echo ""
    info "Binary location: $INSTALL_DIR/$BINARY_NAME"
    info "Config location: $CONFIG_DIR/config.yaml"
    echo ""
    info "Quick start:"
    info "  1. Edit config: ${YELLOW}sudo nano $CONFIG_DIR/config.yaml${NC}"
    info "  2. Generate TLS cert (recommended): ${YELLOW}sh <(curl -fsSL https://gibram.io/generate-cert.sh)${NC}"
    info "  3. Start server: ${YELLOW}gibram-server --config $CONFIG_DIR/config.yaml${NC}"
    info ""
    info "Or start insecure (DEV ONLY): ${YELLOW}gibram-server --config $CONFIG_DIR/config.yaml --insecure${NC}"
    echo ""
    info "Documentation: https://github.com/$REPO"
    info "Python SDK: ${YELLOW}pip install gibram${NC}"
    echo ""
}

# Main installation flow
main() {
    info "Installing GibRAM..."
    echo ""
    
    OS=$(detect_os)
    
    install_binary
    create_config
    setup_systemd
    print_success
}

main
