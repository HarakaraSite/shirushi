#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir=$(mktemp -d)
server_pid=
fixture_pid=

cleanup() {
    trap - EXIT INT TERM
    for pid in "$server_pid" "$fixture_pid"; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            wait "$pid" 2>/dev/null || true
        fi
    done
    rm -rf "$test_dir"
}
trap cleanup EXIT INT TERM

fixture_host=${SHIRUSHI_E2E_404_HOST:-127.0.0.1}
fixture_port=${SHIRUSHI_E2E_404_PORT:-18182}
node "$project_dir/scripts/run-e2e-404-fixture.js" &
fixture_pid=$!

(
    cd "$project_dir"
    CGO_ENABLED=0 go build -o "$test_dir/shirushi" .
)

if ! kill -0 "$fixture_pid" 2>/dev/null; then
    echo "404 fixture failed to start" >&2
    exit 1
fi
FIXTURE_URL="http://$fixture_host:$fixture_port/" node -e '
fetch(process.env.FIXTURE_URL).then(response => {
  if (!response.ok) process.exit(1);
}).catch(() => process.exit(1));
' || {
    echo "404 fixture is not reachable" >&2
    exit 1
}

cd "$test_dir"
SHIRUSHI_ADDR="${SHIRUSHI_E2E_ADDR:-127.0.0.1:18181}" \
SHIRUSHI_PASSWORD="${SHIRUSHI_E2E_PASSWORD:-playwright-test-password}" \
SHIRUSHI_API_TOKEN="${SHIRUSHI_E2E_API_TOKEN:-playwright-test-token}" \
SHIRUSHI_ALLOW_PRIVATE_FETCH=1 \
    "$test_dir/shirushi" &
server_pid=$!
wait "$server_pid"
