#!/bin/bash
# seneschal 启动脚本
# 由于 Workspace 目录在 virtiofs 共享文件系统上，不支持 chmod
# 此脚本将二进制复制到 /tmp 并执行

BINARY="/tmp/seneschal"
SRC="$(dirname "$0")/seneschal"

# 如果 /tmp 下的二进制不存在或比源文件旧，则重新复制
if [ ! -f "$BINARY" ] || [ "$SRC" -nt "$BINARY" ]; then
    cp "$SRC" "$BINARY"
    chmod +x "$BINARY"
fi

exec "$BINARY" "$@"
