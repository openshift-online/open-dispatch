#!/bin/bash
#
# Claude Code Wrapper for Paude Integration
#
# Wraps Claude Code execution with coordination hooks
# Handles status updates and error reporting
#

set -euo pipefail

# Configuration
AGENT_NAME="${AGENT_NAME:-agent}"
WORKSPACE_NAME="${WORKSPACE_NAME:-default}"

log_info() { echo "[INFO] $*"; }
log_error() { echo "[ERROR] $*" >&2; }

# Pre-execution hook
pre_execution() {
    /usr/local/bin/coordination-client.py status "active" "${AGENT_NAME}: starting Claude Code session" || true
}

# Post-execution hook  
post_execution() {
    local exit_code=$1
    
    if [[ $exit_code -eq 0 ]]; then
        /usr/local/bin/coordination-client.py status "idle" "${AGENT_NAME}: Claude Code session ended normally" || true
    else
        /usr/local/bin/coordination-client.py status "error" "${AGENT_NAME}: Claude Code session ended with error (exit code: $exit_code)" || true
    fi
}

# Error handler
handle_error() {
    local exit_code=$?
    log_error "Claude Code execution failed with exit code: $exit_code"
    post_execution $exit_code
    exit $exit_code
}

# Trap errors
trap handle_error ERR

# Main execution
main() {
    log_info "Starting Claude Code with coordination hooks"
    
    # Pre-execution
    pre_execution
    
    # Execute Claude Code with all arguments
    claude-code "$@"
    local exit_code=$?
    
    # Post-execution
    post_execution $exit_code
    
    exit $exit_code
}

# Run main function with all arguments
main "$@"