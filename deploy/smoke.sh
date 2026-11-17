#!/usr/bin/env bash
set -euo pipefail

base_url="${BASE_URL:-http://localhost:8080}"
key="smoke-$(python3 -c 'import uuid; print(uuid.uuid4())')"
book() {
    curl -fsS -X POST "$base_url/api/bookings" \
        -H 'Content-Type: application/json' -H "Idempotency-Key: $key" \
        -d '{"event_id":"night-jazz","quantity":1}'
}
read_id() {
    python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
}
expect_status() {
    python3 -c 'import json,sys; actual=json.load(sys.stdin)["status"]; sys.exit(0 if actual==sys.argv[1] else f"Expected {sys.argv[1]}, got {actual}")' "$1"
}
curl -fsS "$base_url/ready" >/dev/null
id="$(book | read_id)"
trap 'curl -fsS -X DELETE "$base_url/api/bookings/$id" >/dev/null || true' EXIT
test "$(book | read_id)" = "$id"
curl -fsS "$base_url/api/bookings/$id" | expect_status active
curl -fsS -X DELETE "$base_url/api/bookings/$id" | expect_status cancelled
trap - EXIT
test "$(book | read_id)" = "$id"
echo "Smoke test passed: $id"
