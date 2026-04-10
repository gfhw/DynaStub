#!/bin/bash

# Build script for DynaStub binaries
# This script cross-compiles both operator and sidecar binaries for Linux amd64

set -e

# Configuration
BUILD_DIR="$(pwd)"
GOOS="linux"
GOARCH="amd64"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# Print usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Cross-compile DynaStub binaries (operator and sidecar) for Linux amd64

OPTIONS:
    --os GOOS         Target operating system (default: linux)
    --arch GOARCH     Target architecture (default: amd64)
    --dir BUILD_DIR   Build directory (default: current directory)
    -h, --help        Show this help message

EXAMPLES:
    # Build with default settings
    $0

    # Build for specific OS and architecture
    $0 --os linux --arch amd64

    # Build to custom directory
    $0 --dir ./build

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --os)
            GOOS="$2"
            shift 2
            ;;
        --arch)
            GOARCH="$2"
            shift 2
            ;;
        --dir)
            BUILD_DIR="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Create build directory if it doesn't exist
if [ ! -d "$BUILD_DIR" ]; then
    log_info "Creating build directory: $BUILD_DIR"
    mkdir -p "$BUILD_DIR"
fi

# Change to project root directory
cd "$(dirname "$0")/.." || exit 1
PROJECT_ROOT="$(pwd)"
log_info "Project root directory: $PROJECT_ROOT"

# Build operator binary
log_step "Building operator binary..."
export CGO_ENABLED=0
export GOOS="linux"
export GOARCH="amd64"

log_info "Building operator (linux/amd64)..."
go build -a -o "$BUILD_DIR/manager" ./cmd/main.go
log_info "Operator binary built successfully: $BUILD_DIR/manager"

# Build sidecar binary
log_step "Building sidecar binary..."
export CGO_ENABLED=0
export GOOS="linux"
export GOARCH="amd64"

log_info "Building sidecar (linux/amd64)..."
go build -a -o "$BUILD_DIR/sidecar" ./cmd/sidecar/main.go
log_info "Sidecar binary built successfully: $BUILD_DIR/sidecar"

# Set executable permissions
log_step "Setting executable permissions..."
chmod +x "$BUILD_DIR/manager"
chmod +x "$BUILD_DIR/sidecar"

# Verify binaries
log_step "Verifying binaries..."
ls -la "$BUILD_DIR/manager"
ls -la "$BUILD_DIR/sidecar"

log_step "Build complete!"
log_info "Binaries built successfully:"
log_info "  - Operator: $BUILD_DIR/manager"
log_info "  - Sidecar: $BUILD_DIR/sidecar"
