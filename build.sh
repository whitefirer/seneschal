#!/bin/bash
set -e

START_TOTAL=$(date +%s%N)

echo "🔨 Building goworkflow..."
export PATH=$PATH:/usr/local/go/bin

# 1. Build frontend (outputs to web/static/)
echo "🎨 Building frontend..."
START_FE=$(date +%s%N)
cd web/frontend
npm run build 2>&1 | tail -1
cd ../..
END_FE=$(date +%s%N)
FE_MS=$(( (END_FE - START_FE) / 1000000 ))
echo "   ✅ Frontend done in ${FE_MS}ms"

# 2+3. Build CLI and Server binaries in parallel
echo "📦 Building CLI and Server binaries (parallel)..."
START_GO=$(date +%s%N)
go build -o goworkflow ./cmd/cli/ &
PID_CLI=$!
go build -o goworkflow-server ./cmd/server/ &
PID_SRV=$!
wait $PID_CLI $PID_SRV
END_GO=$(date +%s%N)
GO_MS=$(( (END_GO - START_GO) / 1000000 ))
echo "   ✅ Go binaries done in ${GO_MS}ms"

END_TOTAL=$(date +%s%N)
TOTAL_MS=$(( (END_TOTAL - START_TOTAL) / 1000000 ))

echo ""
echo "✅ Build complete!"
echo "   - Frontend: web/static/  (${FE_MS}ms)"
echo "   - Go:       CLI + Server (${GO_MS}ms)"
echo "   - Total:    ${TOTAL_MS}ms"
echo ""
echo "🚀 Start server: ./start-server.sh"
