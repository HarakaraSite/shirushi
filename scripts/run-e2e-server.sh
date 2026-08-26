#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir=$(mktemp -d)
server_pid=

cleanup() {
    trap - EXIT INT TERM
    if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
    rm -rf "$test_dir"
}
trap cleanup EXIT INT TERM

(
    cd "$project_dir"
    CGO_ENABLED=0 go build -o "$test_dir/shirushi" .
)

cd "$test_dir"
SHIRUSHI_ADDR="${SHIRUSHI_E2E_ADDR:-127.0.0.1:18181}" \
SHIRUSHI_PASSWORD="${SHIRUSHI_E2E_PASSWORD:-playwright-test-password}" \
SHIRUSHI_API_TOKEN="${SHIRUSHI_E2E_API_TOKEN:-playwright-test-token}" \
    "$test_dir/shirushi" &
server_pid=$!
wait "$server_pid"
