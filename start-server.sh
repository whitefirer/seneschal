#!/bin/bash

# Seneschal Server Start/Restart Script
# Usage: ./start-server.sh [port]

set -e

PORT=${1:-8888}
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_BIN="$SCRIPT_DIR/seneschal-server"
LOG_FILE="/tmp/seneschal-server.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🔄 Seneschal Server Manager${NC}"
echo ""

# Check if binary exists
if [ ! -f "$SERVER_BIN" ]; then
    echo -e "${RED}❌ Server binary not found: $SERVER_BIN${NC}"
    echo "   Please run ./build.sh first"
    exit 1
fi

# Kill existing process
EXISTING_PID=$(lsof -ti :$PORT 2>/dev/null || true)
if [ -n "$EXISTING_PID" ]; then
    echo -e "${YELLOW}🛑 Stopping existing server (PID: $EXISTING_PID)${NC}"
    kill -9 $EXISTING_PID 2>/dev/null || true
    sleep 1
fi

# Start new server
echo -e "${GREEN}🚀 Starting server on port $PORT${NC}"
$SERVER_BIN --port $PORT > "$LOG_FILE" 2>&1 &
SERVER_PID=$!

# Wait and verify
sleep 2
if lsof -i :$PORT -sTCP:LISTEN | grep -q LISTEN; then
    echo -e "${GREEN}✅ Server started successfully${NC}"
    echo -e "   PID: $SERVER_PID"
    echo -e "   Port: $PORT"
    echo -e "   URL: ${GREEN}http://localhost:$PORT${NC}"
    echo -e "   Logs: $LOG_FILE"
else
    echo -e "${RED}❌ Failed to start server${NC}"
    echo "   Check logs: $LOG_FILE"
    exit 1
fi