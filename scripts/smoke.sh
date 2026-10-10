#!/usr/bin/env bash
# Verifies the project end to end: Go formatting, vet, and tests (with the
# race detector), both builds, then the comparison page and API through a
# running Next.js server. The API reads exchange rates from the saved ECB
# file in its test data and never fetches them. It uses its own local ports,
# so it can run next to development servers.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
api_port="${SMOKE_API_PORT:-18080}"
web_port="${SMOKE_WEB_PORT:-13000}"
api_origin="http://127.0.0.1:${api_port}"
web_origin="http://127.0.0.1:${web_port}"

fail() {
  echo "smoke: $*" >&2
  exit 1
}

for tool in go node curl; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
done
[ -d "$root/frontend/node_modules" ] || fail "frontend dependencies are missing; run 'npm ci' in frontend"

# curl exits with status 7 when nothing accepts the connection.
port_in_use() {
  local rc=0
  curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$1/" || rc=$?
  [ "$rc" -ne 7 ]
}

for port in "$api_port" "$web_port"; do
  if port_in_use "$port"; then
    fail "port $port is in use; stop that process or set SMOKE_API_PORT and SMOKE_WEB_PORT"
  fi
done

echo "smoke: Go format, vet, and tests"
cd "$root/backend"
unformatted="$(gofmt -l .)"
[ -z "$unformatted" ] || fail "gofmt needed for: $unformatted"
go vet ./...
go test -race ./...

tmp="$(mktemp -d "${TMPDIR:-/tmp}/flightbound-smoke.XXXXXX")"
api_pid=""
web_pid=""
cleanup() {
  for pid in $web_pid $api_pid; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  rm -rf "$tmp"
  for port in "$api_port" "$web_port"; do
    if port_in_use "$port"; then
      echo "smoke: warning: port $port is still in use after cleanup" >&2
    fi
  done
}
trap cleanup EXIT

echo "smoke: building the backend and frontend"
go build -o "$tmp/server" ./cmd/server
cd "$root/frontend"
# next start serves the rewrite target captured here, at build time.
API_ORIGIN="$api_origin" npm run build

echo "smoke: starting servers on ports $api_port and $web_port"
"$tmp/server" -addr "127.0.0.1:${api_port}" \
  -fx-rates-file "$root/backend/internal/fxrates/testdata/eurofxref-daily.xml" >"$tmp/api.log" 2>&1 &
api_pid=$!
./node_modules/.bin/next start --hostname 127.0.0.1 --port "$web_port" >"$tmp/web.log" 2>&1 &
web_pid=$!

wait_for() {
  local url="$1" pid="$2" log="$3" attempt
  for attempt in $(seq 1 120); do
    if ! kill -0 "$pid" 2>/dev/null; then
      cat "$log" >&2
      fail "the server for $url exited during startup"
    fi
    if curl -s -o /dev/null --max-time 2 "$url"; then
      return 0
    fi
    sleep 0.5
  done
  cat "$log" >&2
  fail "timed out waiting for $url"
}
wait_for "$api_origin/api/trip-comparisons" "$api_pid" "$tmp/api.log"
wait_for "$web_origin/compare" "$web_pid" "$tmp/web.log"

echo "smoke: checking the page and API through $web_origin"
node "$root/scripts/smoke-checks.mjs" "$web_origin"
echo "smoke: ok"
