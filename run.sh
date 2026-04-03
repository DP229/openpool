#!/bin/bash

# OpenPool Development Script
# Usage: ./run.sh [command]
# Commands: build, run, dev, stop, status, logs, clean

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_NAME="openpool"
BINARY_PATH="$PROJECT_DIR/$BINARY_NAME"
PID_FILE="$PROJECT_DIR/.openpool.pid"
LOG_FILE="$PROJECT_DIR/openpool.log"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[OpenPool]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[OpenPool]${NC} $1"
}

error() {
    echo -e "${RED}[OpenPool]${NC} $1"
}

# Check if Go is available
check_go() {
    if ! command -v go &> /dev/null; then
        # Try to find Go in common locations
        if [ -f "$HOME/go125/bin/go" ]; then
            export PATH="$HOME/go125/bin:$PATH"
        elif [ -f "$HOME/go/bin/go" ]; then
            export PATH="$HOME/go/bin:$PATH"
        elif [ -f "/usr/local/go/bin/go" ]; then
            export PATH="/usr/local/go/bin:$PATH"
        else
            error "Go not found. Install Go or set PATH to Go binary."
            exit 1
        fi
    fi
    log "Using Go: $(go version | head -1)"
}

# Build the binary
build() {
    check_go
    log "Building OpenPool..."
    cd "$PROJECT_DIR"
    go build -o "$BINARY_NAME" ./cmd/node2
    chmod +x "$BINARY_NAME"
    log "Build complete: $BINARY_PATH"
}

# Run the binary
run() {
    check_go
    
    if [ ! -f "$BINARY_PATH" ]; then
        warn "Binary not found. Building..."
        build
    fi
    
    log "Starting OpenPool..."
    cd "$PROJECT_DIR"
    
    # Default port
    HTTP_PORT="${HTTP_PORT:-8080}"
    
    nohup ./$BINARY_NAME --http $HTTP_PORT > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"
    
    sleep 2
    
    if curl -s "http://localhost:$HTTP_PORT/" > /dev/null 2>&1; then
        log "OpenPool started on http://localhost:$HTTP_PORT"
        log "Dashboard: http://localhost:$HTTP_PORT/"
        log "Logs: $LOG_FILE"
    else
        error "Failed to start. Check logs:"
        tail -20 "$LOG_FILE"
    fi
}

# Development mode (build + run with auto-reload would need additional tooling)
dev() {
    check_go
    
    log "Development mode: build + run"
    build
    run
}

# Stop the binary
stop() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p $PID > /dev/null 2>&1; then
            log "Stopping OpenPool (PID: $PID)..."
            kill $PID
            rm -f "$PID_FILE"
            log "Stopped"
        else
            warn "Process not running"
            rm -f "$PID_FILE"
        fi
    else
        # Try to find by process name
        PIDS=$(pgrep -f "$BINARY_NAME")
        if [ -n "$PIDS" ]; then
            log "Stopping OpenPool processes: $PIDS"
            pkill -f "$BINARY_NAME"
        else
            warn "No running instance found"
        fi
    fi
}

# Status
status() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p $PID > /dev/null 2>&1; then
            log "Running (PID: $PID)"
            HTTP_PORT="${HTTP_PORT:-8080}"
            if curl -s "http://localhost:$HTTP_PORT/" > /dev/null 2>&1; then
                echo "  Dashboard: http://localhost:$HTTP_PORT/"
            fi
        else
            warn "Not running (stale PID file)"
        fi
    else
        PIDS=$(pgrep -f "$BINARY_NAME")
        if [ -n "$PIDS" ]; then
            log "Running (PIDs: $PIDS)"
        else
            warn "Not running"
        fi
    fi
}

# View logs
logs() {
    if [ -f "$LOG_FILE" ]; then
        tail -50 "$LOG_FILE"
    else
        warn "No log file found"
    fi
}

# Clean build artifacts
clean() {
    log "Cleaning..."
    cd "$PROJECT_DIR"
    rm -f "$BINARY_NAME" "$BINARY_PATH" "$PID_FILE" "$LOG_FILE"
    go clean
    log "Clean complete"
}

# Full restart
restart() {
    stop
    sleep 1
    run
}

# Show help
help() {
    echo "OpenPool Development Script"
    echo ""
    echo "Usage: ./run.sh <command>"
    echo ""
    echo "Commands:"
    echo "  build    - Build the binary"
    echo "  run      - Run the binary (default port 8080)"
    echo "  dev      - Build + run (development)"
    echo "  stop     - Stop running instance"
    echo "  restart  - Stop + run"
    echo "  status   - Check if running"
    echo "  logs     - View recent logs"
    echo "  clean    - Remove build artifacts"
    echo ""
    echo "Environment:"
    echo "  HTTP_PORT=8080 ./run.sh run  - Custom port"
    echo ""
    echo "Examples:"
    echo "  ./run.sh dev              # Build and run"
    echo "  ./run.sh run              # Run existing binary"
    echo "  ./run.sh restart          # Restart"
    echo "  HTTP_PORT=9000 ./run.sh  # Run on port 9000"
}

# Main
case "${1:-run}" in
    build)  build ;;
    run)    run ;;
    dev)    dev ;;
    stop)   stop ;;
    restart) restart ;;
    status) status ;;
    logs)   logs ;;
    clean)  clean ;;
    help|--help|-h) help ;;
    *)
        error "Unknown command: $1"
        echo ""
        help
        exit 1
        ;;
esac
