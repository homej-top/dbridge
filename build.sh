#!/bin/bash
# ============================================================
# DBridge Build Script
# Builds both backend (Go) and frontend (React/Vite)
# Usage: ./build.sh [backend|frontend|all]
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

TARGET="${1:-all}"
BACKEND_BIN="db-sync-web-server"

echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}  DBridge Build Script${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""

# ============================================================
# Find Go
# ============================================================
find_go() {
    # Try explicit paths first
    for p in /usr/local/go/bin/go /opt/homebrew/bin/go /usr/local/bin/go ~/go/bin/go; do
        if [ -x "$p" ]; then
            echo "$p"
            return 0
        fi
    done
    # Try gvm (Go Version Manager)
    for p in ~/.gvm/gos/*/bin/go; do
        if [ -x "$p" ]; then
            echo "$p"
            return 0
        fi
    done
    # Try SDK
    for p in ~/sdk/go*/bin/go; do
        if [ -x "$p" ]; then
            echo "$p"
            return 0
        fi
    done
    # Try PATH
    if command -v go &>/dev/null; then
        echo "$(command -v go)"
        return 0
    fi
    # Try loading gvm
    if [ -s "$HOME/.gvm/scripts/gvm" ]; then
        source "$HOME/.gvm/scripts/gvm" 2>/dev/null
        if command -v go &>/dev/null; then
            echo "$(command -v go)"
            return 0
        fi
    fi
    return 1
}

# ============================================================
# Build Backend
# ============================================================
build_backend() {
    echo -e "${YELLOW}--- Building Backend (Go) ---${NC}"

    GO_BIN=$(find_go)
    if [ -z "$GO_BIN" ]; then
        echo -e "  ${RED}WARNING: Go is not installed. Cannot compile backend.${NC}"
        echo -e "  ${YELLOW}Install Go: https://go.dev/dl/${NC}"
        echo -e "  ${YELLOW}Or on macOS: brew install go${NC}"
        if [ -f "$BACKEND_BIN" ]; then
            echo -e "  ${GREEN}Existing binary found: $BACKEND_BIN ($(ls -lh $BACKEND_BIN | awk '{print $5}'))${NC}"
            echo -e "  ${GREEN}Will use existing binary.${NC}"
        fi
        return 0
    fi

    echo -e "  Using Go: $GO_BIN ($($GO_BIN version))"

    # Install dependencies
    echo "  Downloading dependencies..."
    cd "$SCRIPT_DIR"

    # Sync vendor directory if it exists
    if [ -d "vendor" ]; then
        echo "  Syncing vendor directory..."
        $GO_BIN mod tidy 2>/dev/null || true
        $GO_BIN mod vendor 2>/dev/null || true
    else
        $GO_BIN mod tidy 2>/dev/null || true
        $GO_BIN mod download 2>/dev/null || true
    fi

    # Build
    echo "  Compiling..."
    cd "$SCRIPT_DIR"
    # Note: CGO must be enabled for go-sqlite3 driver
    export CGO_ENABLED=1

    # Build with -mod=mod (bypass vendor if inconsistent)
    BUILD_OUT=$($GO_BIN build -mod=mod -o "$BACKEND_BIN" ./cmd/server/ 2>&1)
    BUILD_EXIT=$?

    # If that fails, try syncing vendor and building
    if [ $BUILD_EXIT -ne 0 ]; then
        if [ -d "vendor" ]; then
            echo "  ${YELLOW}Vendor issue detected, syncing...${NC}"
            $GO_BIN mod tidy 2>/dev/null || true
            $GO_BIN mod vendor 2>/dev/null || true
            $GO_BIN build -o "$BACKEND_BIN" ./cmd/server/ 2>&1
            BUILD_EXIT=$?
        fi
    fi

    if [ -f "$BACKEND_BIN" ]; then
        echo -e "  ${GREEN}✓ Backend built successfully: $BACKEND_BIN ($(ls -lh $BACKEND_BIN | awk '{print $5}'))${NC}"
    else
        echo -e "  ${RED}✗ Backend build failed${NC}"
        return 1
    fi
}

# ============================================================
# Build Frontend
# ============================================================
build_frontend() {
    echo -e "${YELLOW}--- Building Frontend (React/Vite) ---${NC}"

    cd "$SCRIPT_DIR/web"

    if [ ! -f "package.json" ]; then
        echo -e "  ${RED}✗ package.json not found in web/${NC}"
        return 1
    fi

    # Check node
    if ! command -v node &>/dev/null; then
        # Try nvm
        if [ -s "$HOME/.nvm/nvm.sh" ]; then
            source "$HOME/.nvm/nvm.sh" 2>/dev/null
        fi
    fi

    if ! command -v node &>/dev/null; then
        echo -e "  ${RED}WARNING: Node.js is not available. Cannot build frontend.${NC}"
        return 1
    fi

    echo "  Node.js: $(node --version)"
    echo "  npm: $(npm --version)"

    # Install dependencies
    if [ ! -d "node_modules" ] || [ "$2" = "force" ]; then
        echo "  Installing npm dependencies..."
        npm install --silent 2>&1 | tail -3
    else
        echo "  node_modules exists, skipping npm install (use 'force' to reinstall)"
    fi

    # Build
    echo "  Building React app..."
    npx vite build 2>&1 | tail -5

    if [ -d "dist" ]; then
        echo -e "  ${GREEN}✓ Frontend built successfully: web/dist/ ($(du -sh dist | awk '{print $1}'))${NC}"
    else
        echo -e "  ${RED}✗ Frontend build failed${NC}"
        return 1
    fi
}

# ============================================================
# Main
# ============================================================
case "$TARGET" in
    backend)
        build_backend
        ;;
    frontend)
        build_frontend
        ;;
    all)
        build_frontend
        echo ""
        build_backend
        ;;
    force)
        build_frontend force
        echo ""
        build_backend
        ;;
    *)
        echo "Usage: $0 [backend|frontend|all|force]"
        echo "  backend   - Build Go backend only"
        echo "  frontend  - Build React frontend only"
        echo "  all       - Build both (default)"
        echo "  force     - Build both, reinstall npm deps"
        exit 1
        ;;
esac

echo ""
echo -e "${CYAN}============================================================${NC}"
echo -e "${GREEN}  Build complete!${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""
echo "  Next steps:"
echo "    ./start.sh    - Start the server"
echo "    ./test/run_all.sh - Run API tests"
