#!/bin/bash
# ============================================================
# DBridge Restart Script
# Stops and restarts the backend server
# Usage: ./restart.sh [--dev] [--port PORT]
# ============================================================
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

MODE="${1:-prod}"

echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}  DBridge Restart Script${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""

# Stop
echo -e "  ${BOLD}1. Stopping server...${NC}"
bash "$SCRIPT_DIR/stop.sh" --force 2>/dev/null
echo ""

# Rebuild if binary doesn't exist
if [ ! -f "dbridge-web-server" ]; then
    echo -e "  ${BOLD}1.5 Binary not found, building...${NC}"
    bash "$SCRIPT_DIR/build.sh" backend 2>/dev/null || true
    echo ""
fi

# Start
echo -e "  ${BOLD}2. Starting server...${NC}"
bash "$SCRIPT_DIR/start.sh" "$MODE"

echo ""
echo -e "${CYAN}Restart complete.${NC}"
