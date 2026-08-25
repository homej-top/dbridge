#!/bin/bash
# ============================================================
# DBridge Stop Script
# Stops the running backend server
# Usage: ./stop.sh [--force]
# ============================================================
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

PID_FILE=".dbridge.pid"
BACKEND_BIN="db-sync-web-server"
PORT="${PORT:-8083}"

echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}  DBridge Stop Script${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""

# ============================================================
# Stop by PID file
# ============================================================
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo -n "  Stopping server (PID: $PID)..."
        kill "$PID" 2>/dev/null
        
        # Wait for graceful shutdown
        for i in $(seq 1 10); do
            if ! kill -0 "$PID" 2>/dev/null; then
                echo -e " ${GREEN}OK${NC}"
                rm -f "$PID_FILE"
                break
            fi
            sleep 1
        done
        
        # Force kill if still running
        if kill -0 "$PID" 2>/dev/null; then
            echo ""
            echo "  Server not responding, force killing..."
            kill -9 "$PID" 2>/dev/null
            sleep 1
            if ! kill -0 "$PID" 2>/dev/null; then
                echo -e "  ${YELLOW}✓ Force killed${NC}"
            fi
        fi
    else
        echo -e "  ${YELLOW}PID file exists but process is not running. Removing stale PID file.${NC}"
    fi
    rm -f "$PID_FILE"
else
    echo "  No PID file found."
fi

# ============================================================
# Stop by process name
# ============================================================
# Find any remaining dbridge processes
REMAINING=$(pgrep -f "$BACKEND_BIN" 2>/dev/null || true)
if [ -n "$REMAINING" ]; then
    echo ""
    echo -e "  ${YELLOW}Found remaining server processes:${NC}"
    echo "  $REMAINING"
    
    if [ "$1" = "--force" ]; then
        echo "  Force killing..."
        echo "$REMAINING" | xargs kill -9 2>/dev/null
        echo -e "  ${GREEN}✓ Killed${NC}"
    else
        echo "  Use './stop.sh --force' to force kill"
    fi
fi

# ============================================================
# Check port freed
# ============================================================
if lsof -i ":${PORT}" -sTCP:LISTEN &>/dev/null 2>&1; then
    echo ""
    echo -e "  ${YELLOW}Port ${PORT} still in use:${NC}"
    lsof -i ":${PORT}" -sTCP:LISTEN 2>/dev/null | tail -n +2
    
    if [ "$1" = "--force" ]; then
        PID_ON_PORT=$(lsof -ti ":${PORT}" -sTCP:LISTEN 2>/dev/null)
        if [ -n "$PID_ON_PORT" ]; then
            echo "  Force killing process on port ${PORT}..."
            kill -9 $PID_ON_PORT 2>/dev/null || true
            sleep 1
            if ! lsof -i ":${PORT}" -sTCP:LISTEN &>/dev/null 2>&1; then
                echo -e "  ${GREEN}✓ Port freed${NC}"
            fi
        fi
    fi
else
    echo ""
    echo -e "  ${GREEN}Port ${PORT} is free${NC}"
fi

echo ""
echo -e "  ${GREEN}Server stopped.${NC}"
