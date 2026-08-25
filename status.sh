#!/bin/bash
# ============================================================
# DBridge Status Script
# Shows the current status of the server
# Usage: ./status.sh
# ============================================================
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

PID_FILE=".dbridge.pid"
BACKEND_BIN="db-sync-web-server"
PORT="${PORT:-8083}"

echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}  DBridge Status${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""

# ============================================================
# PID File Status
# ============================================================
echo -e "${BOLD}PID File:${NC}"
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo -e "  ${GREEN}✓ Process running (PID: $PID)${NC}"
        
        # Show process info
        echo ""
        echo -e "${BOLD}Process Info:${NC}"
        ps -p "$PID" -o pid,ppid,user,%cpu,%mem,etime,command 2>/dev/null | tail -n +2 | while read line; do
            echo "  $line"
        done
        
        # Show uptime
        ETIME=$(ps -p "$PID" -o etime= 2>/dev/null | tr -d ' ')
        if [ -n "$ETIME" ]; then
            echo -e "  Uptime: ${ETIME}"
        fi
    else
        echo -e "  ${RED}✗ PID file exists but process is dead (PID: $PID)${NC}"
        echo -e "  ${YELLOW}Run ./start.sh to restart${NC}"
    fi
else
    echo -e "  ${YELLOW}No PID file${NC}"
fi

# ============================================================
# Port Status
# ============================================================
echo ""
echo -e "${BOLD}Port ${PORT}:${NC}"
if lsof -i ":${PORT}" -sTCP:LISTEN &>/dev/null 2>&1; then
    echo -e "  ${GREEN}✓ Port is listening${NC}"
    lsof -i ":${PORT}" -sTCP:LISTEN 2>/dev/null | tail -n +2 | while read line; do
        echo "  $line"
    done
else
    echo -e "  ${RED}✗ Port ${PORT} is NOT listening${NC}"
fi

# ============================================================
# Health Check
# ============================================================
echo ""
echo -e "${BOLD}Health Check:${NC}"

# Try health endpoints
for path in "/health/live" "/health/liveness" "/api/v1/health/live" "/api/v1/health/liveness"; do
    response=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:${PORT}${path}" 2>/dev/null)
    if [ "$response" = "200" ]; then
        # Check if response is JSON (not HTML SPA fallback)
        body=$(curl -s "http://localhost:${PORT}${path}" 2>/dev/null)
        if echo "$body" | python3 -c "import json,sys; json.load(sys.stdin)" 2>/dev/null; then
            echo -e "  ${GREEN}✓ $path → 200 OK (JSON)${NC}"
            break
        fi
    fi
done

# Try API endpoint
if curl -s -o /dev/null -w "%{http_code}" "http://localhost:${PORT}/api/v1/dashboard/stats" 2>/dev/null | grep -q "200\|401"; then
    echo -e "  ${GREEN}✓ API responds (dashboard)${NC}"
fi

# ============================================================
# Binary Info
# ============================================================
echo ""
echo -e "${BOLD}Binary:${NC}"
if [ -f "$BACKEND_BIN" ]; then
    SIZE=$(ls -lh "$BACKEND_BIN" | awk '{print $5}')
    MODIFIED=$(stat -f "%Sm" -t "%Y-%m-%d %H:%M:%S" "$BACKEND_BIN" 2>/dev/null || stat -c "%y" "$BACKEND_BIN" 2>/dev/null | cut -d. -f1)
    echo -e "  File: ${BACKEND_BIN}"
    echo -e "  Size: ${SIZE}"
    echo -e "  Modified: ${MODIFIED}"
else
    echo -e "  ${RED}Binary not found: ${BACKEND_BIN}${NC}"
    echo -e "  ${YELLOW}Run ./build.sh to build${NC}"
fi

# ============================================================
# Database
# ============================================================
echo ""
echo -e "${BOLD}Database:${NC}"
if [ -f "data/dbbridge.db" ]; then
    DB_SIZE=$(ls -lh data/dbbridge.db | awk '{print $5}')
    echo -e "  SQLite: data/dbbridge.db (${DB_SIZE})"
    # Count tables
    TABLE_COUNT=$(python3 -c "
import sqlite3
try:
    conn = sqlite3.connect('data/dbbridge.db')
    cur = conn.cursor()
    cur.execute(\"SELECT COUNT(*) FROM sqlite_master WHERE type='table'\")
    print(cur.fetchone()[0])
except: print('N/A')
" 2>/dev/null)
    echo -e "  Tables: ${TABLE_COUNT:-N/A}"
else
    echo -e "  ${YELLOW}No SQLite database found${NC}"
fi

echo ""
echo -e "${CYAN}============================================================${NC}"
