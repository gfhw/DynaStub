#!/bin/bash

# Build script for DynaStub Docker images
# This script automates the Docker image build process for both operator and sidecar

set -e

# Configuration
BUILD_ALL="${BUILD_ALL:-true}"  # Build both operator and sidecar by default
IMAGE_TYPE="${IMAGE_TYPE:-operator}"  # operator or sidecar (only used if BUILD_ALL=false)
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
    --all                       Build both operator and sidecar images (default: true)
    --no-all                    Build only specified image type
    -t, --type IMAGE_TYPE       Image type: operator or sidecar (default: operator, only used if --no-all)
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
    BUILD_ALL                   Build both operator and sidecar images (true/false, default: true)
    IMAGE_TYPE                  Image type: operator or sidecar (only used if BUILD_ALL=false)
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
        --all)
            BUILD_ALL="true"
            shift
            ;;
        --no-all)
            BUILD_ALL="false"
            shift
            ;;
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

# Build a single image
build_single_image() {
    local image_type="$1"
    local image_name=""
    local dockerfile=""
    local binary_name=""

    # Set default values based on image type
    if [ "$image_type" = "operator" ]; then
        image_name="${IMAGE_NAME:-dynastub-operator}"
        dockerfile="${DOCKERFILE:-build/Dockerfile}"
        binary_name="manager"
    elif [ "$image_type" = "sidecar" ]; then
        image_name="${IMAGE_NAME:-dynastub-sidecar}"
        dockerfile="${DOCKERFILE:-build/Dockerfile.sidecar}"
        binary_name="sidecar"
    else
        log_error "Invalid image type: $image_type"
        log_error "Valid types: operator, sidecar"
        return 1
    fi

    log_info "Starting Docker image build process for $image_type..."
    log_info "Image type: $image_type"
    log_info "Image name: $image_name"
    log_info "Image tag: $IMAGE_TAG"
    log_info "Dockerfile: $dockerfile"
    log_info "Build context: $BUILD_CONTEXT"
    log_info "No cache: $NO_CACHE"
    log_info "Skip build: $SKIP_BUILD"
    log_info "Import to containerd: $IMPORT_TO_CONTAINERD"
    log_info "Push image: $PUSH_IMAGE"

    # Check if Docker is installed
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed or not in PATH"
        return 1
    fi

    # Check if Dockerfile exists
    if [ ! -f "$BUILD_CONTEXT/$dockerfile" ]; then
        log_error "Dockerfile not found: $BUILD_CONTEXT/$dockerfile"
        return 1
    fi

    # Check for binary in build directory
    if [ ! -f "./$binary_name" ]; then
        log_warn "Binary not found: ./$binary_name"
        log_info "Building binary using build-binaries.sh..."
        if bash ./build-binaries.sh; then
            log_info "Binary built successfully: ./$binary_name"
        else
            log_error "Failed to build binary!"
            return 1
        fi
    else
        log_info "Using existing binary: ./$binary_name"
    fi

    # Build Docker image
    log_step "Building $image_type Docker image..."

    BUILD_CMD="docker build --pull=false"
    
    if [ "$NO_CACHE" = "true" ]; then
        BUILD_CMD="$BUILD_CMD --no-cache"
    fi

    BUILD_CMD="$BUILD_CMD -t $image_name:$IMAGE_TAG"
    BUILD_CMD="$BUILD_CMD -f $BUILD_CONTEXT/$dockerfile"
    BUILD_CMD="$BUILD_CMD $BUILD_CONTEXT"
    
    log_info "Executing: $BUILD_CMD"
    
    if $BUILD_CMD; then
        log_info "Docker image built successfully!"
        log_info "Image: $image_name:$IMAGE_TAG"

        # Show image size
        IMAGE_SIZE=$(docker images $image_name:$IMAGE_TAG --format "{{.Size}}")
        log_info "Image size: $IMAGE_SIZE"

        # List the image
        docker images $image_name:$IMAGE_TAG

        # Save image to build directory
        log_step "Saving image to build directory..."
        TAR_FILE="./$image_name-$IMAGE_TAG.tar.gz"
        docker save $image_name:$IMAGE_TAG | gzip > "$TAR_FILE"
        log_info "Image saved to: $TAR_FILE"
        ls -lh "$TAR_FILE"

        # Import to containerd if enabled
        if [ "$IMPORT_TO_CONTAINERD" = "true" ]; then
            import_to_containerd "$TAR_FILE"
        fi

        # Push image if enabled
        if [ "$PUSH_IMAGE" = "true" ]; then
            log_step "Pushing image to registry..."
            if docker push "$image_name:$IMAGE_TAG"; then
                log_info "Image pushed successfully!"
                log_info "Image: $image_name:$IMAGE_TAG"
            else
                log_error "Failed to push image!"
                return 1
            fi
        fi

        log_step "$image_type build complete!"
        log_info "Summary:"
        log_info "  - Docker image: $image_name:$IMAGE_TAG"
        log_info "  - Saved to: $TAR_FILE"
        if [ "$IMPORT_TO_CONTAINERD" = "true" ]; then
            log_info "  - Imported to containerd: Yes"
        fi
        if [ "$PUSH_IMAGE" = "true" ]; then
            log_info "  - Pushed to registry: Yes"
        fi
        log_info ""

        return 0
    else
        log_error "Docker image build failed!"
        return 1
    fi
}

# Main build process
main() {
    log_info "DynaStub Docker image build process"
    log_info "Build all images: $BUILD_ALL"
    log_info "Image tag: $IMAGE_TAG"
    log_info "Import to containerd: $IMPORT_TO_CONTAINERD"
    log_info "Push image: $PUSH_IMAGE"
    log_info ""

    if [ "$BUILD_ALL" = "true" ]; then
        log_step "Building both operator and sidecar images..."
        log_info ""

        # Build operator image
        if build_single_image "operator"; then
            log_info ""
            # Build sidecar image
            if build_single_image "sidecar"; then
                log_step "All builds complete!"
                log_info "You can now deploy using Helm:"
                log_info "  helm install k8s-http-fake-operator ./charts/k8s-http-fake-operator"
                return 0
            else
                log_error "Sidecar image build failed!"
                return 1
            fi
        else
            log_error "Operator image build failed!"
            return 1
        fi
    else
        log_step "Building single image..."
        log_info ""

        if build_single_image "$IMAGE_TYPE"; then
            log_step "Build complete!"
            log_info "You can now deploy using Helm:"
            log_info "  helm install k8s-http-fake-operator ./charts/k8s-http-fake-operator"
            return 0
        else
            log_error "Image build failed!"
            return 1
        fi
    fi
}

# Run main function
main
