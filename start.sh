#!/bin/bash
# ============================================================
# DBridge Start Script
# Starts the backend server and serves the frontend
# Usage: ./start.sh [--dev] [--port PORT]
# ============================================================
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

BACKEND_BIN="db-sync-web-server"
PID_FILE=".dbridge.pid"
LOG_FILE="logs/dbridge.log"
PORT="${PORT:-8083}"
MODE="${1:-prod}"

echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}  DBridge Start Script${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""

# ============================================================
# Check if already running
# ============================================================
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo -e "  ${YELLOW}Server is already running (PID: $OLD_PID)${NC}"
        echo -e "  ${YELLOW}Use ./restart.sh to restart${NC}"
        echo ""
        # Show status
        if curl -s -o /dev/null -w "%{http_code}" "http://localhost:${PORT}/health/live" 2>/dev/null | grep -q "200"; then
            echo -e "  ${GREEN}Health check: OK${NC}"
            echo -e "  ${CYAN}URL: http://localhost:${PORT}${NC}"
        else
            echo -e "  ${YELLOW}Health check: not responding (may be starting up)${NC}"
        fi
        exit 0
    else
        # Stale PID file
        rm -f "$PID_FILE"
    fi
fi

# Also check port occupancy
if lsof -i ":${PORT}" -sTCP:LISTEN &>/dev/null 2>&1; then
    echo -e "  ${YELLOW}Port ${PORT} is already in use.${NC}"
    echo -e "  ${YELLOW}Run ./stop.sh first to kill existing process.${NC}"
    lsof -i ":${PORT}" -sTCP:LISTEN 2>/dev/null | tail -n +2
    echo ""
    if [ "$MODE" = "prod" ]; then
        echo -e "  ${RED}Cannot start - port in use.${NC}"
        exit 1
    fi
fi

# ============================================================
# Check binary
# ============================================================
if [ ! -f "$BACKEND_BIN" ]; then
    echo -e "  ${YELLOW}Backend binary not found. Attempting to build...${NC}"
    if bash "$SCRIPT_DIR/build.sh" backend; then
        echo ""
    else
        echo -e "  ${RED}Cannot build backend. Check Go installation.${NC}"
        exit 1
    fi
fi

# ============================================================
# Create log directory
# ============================================================
mkdir -p logs data export_files export_logs

# ============================================================
# Start backend server
# ============================================================
echo -e "${YELLOW}Starting DBridge server...${NC}"

# Set environment
export CONFIG_PATH="${CONFIG_PATH:-configs/config}"

# Start server in background
nohup ./$BACKEND_BIN > "$LOG_FILE" 2>&1 &
SERVER_PID=$!
echo "$SERVER_PID" > "$PID_FILE"

# Wait for server to start
echo -n "  Waiting for server to start"
for i in $(seq 1 30); do
    echo -n "."
    if curl -s -o /dev/null "http://localhost:${PORT}/health/live" 2>/dev/null; then
        echo ""
        echo -e "  ${GREEN}✓ Server started (PID: $SERVER_PID)${NC}"
        break
    fi
    
    # Also try API health endpoint
    if curl -s -o /dev/null "http://localhost:${PORT}/api/v1/health/liveness" 2>/dev/null; then
        echo ""
        echo -e "  ${GREEN}✓ Server started (PID: $SERVER_PID)${NC}"
        break
    fi
    
    # Check if process died
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo ""
        echo -e "  ${RED}✗ Server process died. Check logs:${NC}"
        echo "  tail -50 $LOG_FILE"
        rm -f "$PID_FILE"
        exit 1
    fi
    
    sleep 1
done

# Verify it's running
if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo -e "  ${RED}✗ Server failed to start.${NC}"
    echo "  Last 20 lines of log:"
    tail -20 "$LOG_FILE" | sed 's/^/    /'
    rm -f "$PID_FILE"
    exit 1
fi

# ============================================================
# Show status
# ============================================================
echo ""
echo -e "${CYAN}============================================================${NC}"
echo -e "${GREEN}  DBridge is running!${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""
echo -e "  ${BOLD}URL:  ${NC}http://localhost:${PORT}"
echo -e "  ${BOLD}PID:  ${NC}$SERVER_PID"
echo -e "  ${BOLD}Log:  ${NC}$LOG_FILE"
echo -e "  ${BOLD}Test: ${NC}./test/run_all.sh"
echo ""
echo -e "  ${BOLD}Stop: ${NC}./stop.sh"
echo ""
