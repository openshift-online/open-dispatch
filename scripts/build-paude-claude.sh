#!/bin/bash
#
# Build Paude + Claude Code Integration Image
#
# Creates a container image with Paude network filtering + Claude Code + Agent Boss coordination
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Image configuration
BASE_IMAGE="localhost/paude-proxy-centos9:latest"
TARGET_IMAGE="localhost/paude-claude:latest"
DOCKERFILE="${PROJECT_ROOT}/docker/Dockerfile.paude-claude"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check podman
    if ! command -v podman &> /dev/null; then
        log_error "podman is required but not installed"
        exit 1
    fi
    
    # Check base image exists
    if ! podman image exists "${BASE_IMAGE}"; then
        log_error "Base image not found: ${BASE_IMAGE}"
        log_info "Build Paude base image first"
        exit 1
    fi
    
    # Check Dockerfile exists
    if [[ ! -f "${DOCKERFILE}" ]]; then
        log_error "Dockerfile not found: ${DOCKERFILE}"
        exit 1
    fi
    
    log_success "Prerequisites check complete"
}

# Build the integrated image
build_image() {
    log_info "Building Paude + Claude Code integration image..."
    log_info "Base: ${BASE_IMAGE}"
    log_info "Target: ${TARGET_IMAGE}"
    log_info "Dockerfile: ${DOCKERFILE}"
    
    # Build with podman
    if podman build \
        -f "${DOCKERFILE}" \
        -t "${TARGET_IMAGE}" \
        "${PROJECT_ROOT}"; then
        log_success "Image built successfully: ${TARGET_IMAGE}"
    else
        log_error "Image build failed"
        exit 1
    fi
}


# Show usage information
usage() {
    cat << EOF
Build Paude + Claude Code Integration Image

USAGE:
    $0 [OPTIONS]

OPTIONS:
    --help          Show this help

DESCRIPTION:
    Builds a container image that combines:
    - Paude network filtering and security
    - Claude Code installation
    - Agent Boss coordination hooks
    - Tmux session management

REQUIREMENTS:
    - Base image: ${BASE_IMAGE}
    - Podman installed and running

OUTPUT:
    - Image: ${TARGET_IMAGE}
    - Ready for use with boot-paude-workspace.sh

EXAMPLES:
    $0                      # Build image

EOF
}

# Parse command line options
while [[ $# -gt 0 ]]; do
    case $1 in
        --help|-h)
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

# Main execution
main() {
    log_info "Building Paude + Claude Code integration"
    
    check_prerequisites
    build_image
    
    log_success "Build complete!"
    log_info "Image ready: ${TARGET_IMAGE}"
    log_info "Use with: ./scripts/boot-paude-workspace.sh"
}

# Run main function
main "$@"