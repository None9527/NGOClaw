#!/usr/bin/env bash
# CGO environment for LanceDB (dynamic linking)
# Source this file before building: source env.sh
GATEWAY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export CGO_ENABLED=1
export CGO_CFLAGS="-I${GATEWAY_DIR}/include"
export CGO_LDFLAGS="-L${GATEWAY_DIR}/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread"
export LD_LIBRARY_PATH="${GATEWAY_DIR}/lib/linux_amd64:${LD_LIBRARY_PATH}"
echo "✅ LanceDB CGO environment set (dynamic linking)"
