#!/bin/bash

# Build script for DynaStub Docker images
# This script automates the Docker image build process for both operator and sidecar

set -e

# Configuration
IMAGE_TYPE="${IMAGE_TYPE:-operator}"  # operator or sidecar
IMAGE_NAME="${IMAGE_NAME:-}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
DOCKERFILE="${DOCKERFILE:-}"
BUILD_CONTEXT="${BUILD_CONTEXT:-..}"
NO_CACHE="${NO_CACHE:-false}"
SKIP_BUILD="${SKIP_BUILD:-true}"
IMPORT_TO_CONTAINERD="${IMPORT_TO_CONTAINERD:-true}"
PUSH_IMAGE="${PUSH_IMAGE:-false}"

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

Build Docker image for DynaStub (operator or sidecar) and optionally import to containerd

OPTIONS:
    -t, --type IMAGE_TYPE       Image type: operator or sidecar (default: operator)
    -n, --name IMAGE_NAME       Docker image name (default: dynastub-operator or dynastub-sidecar)
    -t, --tag IMAGE_TAG         Docker image tag (default: latest)
    -f, --dockerfile DOCKERFILE Path to Dockerfile (default: build/Dockerfile or build/Dockerfile.sidecar)
    -c, --context BUILD_CONTEXT Build context directory (default: ..)
    --no-cache                  Build without using cache
    --skip-build                Skip binary build step (default: true, uses existing binary)
    --no-import                 Skip importing to containerd
    --push                      Push image to registry
    -h, --help                  Show this help message

ENVIRONMENT VARIABLES:
    IMAGE_TYPE                  Image type: operator or sidecar
    IMAGE_NAME                  Docker image name
    IMAGE_TAG                   Docker image tag
    DOCKERFILE                  Path to Dockerfile
    BUILD_CONTEXT               Build context directory
    NO_CACHE                    Build without using cache (true/false)
    SKIP_BUILD                  Skip binary build step (true/false)
    IMPORT_TO_CONTAINERD        Import image to containerd after build (true/false)
    PUSH_IMAGE                  Push image to registry (true/false)

EXAMPLES:
    # Build operator image with default settings
    $0 --type operator

    # Build sidecar image with default settings
    $0 --type sidecar

    # Build with custom name and tag
    $0 --type operator --name my-operator --tag v1.0.0

    # Build and push image
    $0 --type sidecar --push

    # Build without cache
    $0 --type operator --no-cache

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --type)
            IMAGE_TYPE="$2"
            shift 2
            ;;
        -n|--name)
            IMAGE_NAME="$2"
            shift 2
            ;;
        -t|--tag)
            IMAGE_TAG="$2"
            shift 2
            ;;
        -f|--dockerfile)
            DOCKERFILE="$2"
            shift 2
            ;;
        -c|--context)
            BUILD_CONTEXT="$2"
            shift 2
            ;;
        --no-cache)
            NO_CACHE="true"
            shift
            ;;
        --skip-build)
            SKIP_BUILD="true"
            shift
            ;;
        --no-import)
            IMPORT_TO_CONTAINERD="false"
            shift
            ;;
        --push)
            PUSH_IMAGE="true"
            shift
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

# Import image to containerd
import_to_containerd() {
    local tar_file="$1"
    
    log_step "Importing image to containerd..."
    
    # Check if ctr command exists
    if ! command -v ctr &> /dev/null; then
        log_warn "ctr command not found, skipping containerd import"
        log_warn "Please ensure containerd is installed and ctr is in PATH"
        return 0
    fi
    
    # Check if running with sudo or as root
    if [ "$EUID" -ne 0 ]; then
        log_info "Using sudo for containerd import..."
        SUDO="sudo"
    else
        SUDO=""
    fi
    
    # Import image to containerd k8s.io namespace
    log_info "Importing $tar_file to containerd (namespace: k8s.io)..."
    if $SUDO ctr -n k8s.io images import "$tar_file"; then
        log_info "Image imported to containerd successfully!"
        
        # Verify import
        log_info "Verifying image in containerd..."
        $SUDO ctr -n k8s.io images list | grep "$IMAGE_NAME" || true
        
        return 0
    else
        log_error "Failed to import image to containerd"
        return 1
    fi
}

# Main build process
main() {
    # Set default values based on image type
    if [ "$IMAGE_TYPE" = "operator" ]; then
        IMAGE_NAME="${IMAGE_NAME:-dynastub-operator}"
        DOCKERFILE="${DOCKERFILE:-build/Dockerfile}"
        BINARY_NAME="manager"
    elif [ "$IMAGE_TYPE" = "sidecar" ]; then
        IMAGE_NAME="${IMAGE_NAME:-dynastub-sidecar}"
        DOCKERFILE="${DOCKERFILE:-build/Dockerfile.sidecar}"
        BINARY_NAME="sidecar"
    else
        log_error "Invalid image type: $IMAGE_TYPE"
        log_error "Valid types: operator, sidecar"
        exit 1
    fi

    log_info "Starting Docker image build process..."
    log_info "Image type: $IMAGE_TYPE"
    log_info "Image name: $IMAGE_NAME"
    log_info "Image tag: $IMAGE_TAG"
    log_info "Dockerfile: $DOCKERFILE"
    log_info "Build context: $BUILD_CONTEXT"
    log_info "No cache: $NO_CACHE"
    log_info "Skip build: $SKIP_BUILD"
    log_info "Import to containerd: $IMPORT_TO_CONTAINERD"
    log_info "Push image: $PUSH_IMAGE"

    # Check if Docker is installed
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed or not in PATH"
        exit 1
    fi

    # Check if Dockerfile exists
    if [ ! -f "$BUILD_CONTEXT/$DOCKERFILE" ]; then
        log_error "Dockerfile not found: $BUILD_CONTEXT/$DOCKERFILE"
        exit 1
    fi

    # Check for binary in build directory
    if [ ! -f "./$BINARY_NAME" ]; then
        log_error "Binary not found: ./$BINARY_NAME"
        log_error "Please ensure the $BINARY_NAME binary exists in the build directory"
        log_error "The binary should be cross-compiled for Linux amd64"
        exit 1
    fi
    log_info "Using existing binary: ./$BINARY_NAME"

    # Build Docker image
    log_step "Building Docker image..."

    BUILD_CMD="docker build --pull=false"
    
    if [ "$NO_CACHE" = "true" ]; then
        BUILD_CMD="$BUILD_CMD --no-cache"
    fi

    BUILD_CMD="$BUILD_CMD -t $IMAGE_NAME:$IMAGE_TAG"
    BUILD_CMD="$BUILD_CMD -f $BUILD_CONTEXT/$DOCKERFILE"
    BUILD_CMD="$BUILD_CMD $BUILD_CONTEXT"
    
    log_info "Executing: $BUILD_CMD"
    
    if $BUILD_CMD; then
        log_info "Docker image built successfully!"
        log_info "Image: $IMAGE_NAME:$IMAGE_TAG"

        # Show image size
        IMAGE_SIZE=$(docker images $IMAGE_NAME:$IMAGE_TAG --format "{{.Size}}")
        log_info "Image size: $IMAGE_SIZE"

        # List the image
        docker images $IMAGE_NAME:$IMAGE_TAG

        # Save image to build directory
        log_step "Saving image to build directory..."
        TAR_FILE="./$IMAGE_NAME-$IMAGE_TAG.tar.gz"
        docker save $IMAGE_NAME:$IMAGE_TAG | gzip > "$TAR_FILE"
        log_info "Image saved to: $TAR_FILE"
        ls -lh "$TAR_FILE"

        # Import to containerd if enabled
        if [ "$IMPORT_TO_CONTAINERD" = "true" ]; then
            import_to_containerd "$TAR_FILE"
        fi

        # Push image if enabled
        if [ "$PUSH_IMAGE" = "true" ]; then
            log_step "Pushing image to registry..."
            if docker push "$IMAGE_NAME:$IMAGE_TAG"; then
                log_info "Image pushed successfully!"
                log_info "Image: $IMAGE_NAME:$IMAGE_TAG"
            else
                log_error "Failed to push image!"
                exit 1
            fi
        fi

        log_step "Build complete!"
        log_info "Summary:"
        log_info "  - Docker image: $IMAGE_NAME:$IMAGE_TAG"
        log_info "  - Saved to: $TAR_FILE"
        if [ "$IMPORT_TO_CONTAINERD" = "true" ]; then
            log_info "  - Imported to containerd: Yes"
        fi
        if [ "$PUSH_IMAGE" = "true" ]; then
            log_info "  - Pushed to registry: Yes"
        fi
        log_info ""
        log_info "You can now deploy using Helm:"
        log_info "  helm install k8s-http-fake-operator ./charts/k8s-http-fake-operator"

    else
        log_error "Docker image build failed!"
        exit 1
    fi
}

# Run main function
main
