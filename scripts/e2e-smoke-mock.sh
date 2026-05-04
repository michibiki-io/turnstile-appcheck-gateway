#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${1:-}"
if [[ -z "${BASE_URL}" ]]; then
  echo "usage: $0 <base-url>" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
HTTP_STATUS=""
trap 'rm -rf "${TMP_DIR}"' EXIT

run_curl() {
  local outfile="$1"
  local method="$2"
  local url="$3"
  shift 3
  HTTP_STATUS="$(curl -sS -o "${outfile}" -w "%{http_code}" -X "${method}" "${url}" "$@")"
}

assert_status() {
  local expected="$1"
  local actual="$2"
  local label="$3"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "${label}: expected HTTP ${expected}, got ${actual}" >&2
    exit 1
  fi
}

wait_for_status() {
  local path="$1"
  local expected="$2"
  local label="$3"
  local outfile="${TMP_DIR}/wait.out"
  local attempt
  for attempt in $(seq 1 60); do
    if run_curl "${outfile}" GET "${BASE_URL}${path}"; then
      if [[ "${HTTP_STATUS}" == "${expected}" ]]; then
        echo "${label}: HTTP ${expected}"
        return 0
      fi
    fi
    sleep 2
  done
  echo "${label}: service did not become ready" >&2
  exit 1
}

extract_json_field() {
  local file="$1"
  local field="$2"
  python3 - "$file" "$field" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
value = data[sys.argv[2]]
if isinstance(value, (dict, list)):
    raise SystemExit(f"field {sys.argv[2]} must be scalar")
print(value)
PY
}

assert_audit_sanitized() {
  local file="$1"
  python3 - "$file" <<'PY'
import pathlib
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
for banned in (
    "e2e-turnstile-pass",
    "e2e-turnstile-fail",
    "e2e-appcheck-valid-token",
    "Bearer ",
    "cookie",
):
    if banned in text:
        raise SystemExit(f"audit response leaked sensitive marker: {banned}")
PY
}

assert_request_metrics_nonzero() {
  local file="$1"
  python3 - "$file" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
summary = data.get("summary") or {}
total = int(summary.get("total") or 0)
exchange_successes = int(summary.get("exchangeSuccesses") or 0)
points = data.get("points") or []
point_total = sum(int(point.get("count") or 0) for point in points)
if total < 1 or exchange_successes < 1 or point_total < 1:
    raise SystemExit(f"request metrics did not include exchange rollups: summary={summary} pointTotal={point_total}")
PY
}

assert_audit_events_nonzero() {
  local file="$1"
  python3 - "$file" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
total = int(data.get("total") or 0)
items = data.get("items") or []
if total < 1 or not items:
    raise SystemExit(f"audit events did not include persisted rows: total={total} items={len(items)}")
PY
}

wait_for_request_metrics() {
  local outfile="${TMP_DIR}/admin-metrics.out"
  local attempt
  for attempt in $(seq 1 30); do
    run_curl "${outfile}" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics" "${ADMIN_HEADERS[@]}"
    if [[ "${HTTP_STATUS}" == "200" ]] && assert_request_metrics_nonzero "${outfile}" >/dev/null 2>&1; then
      echo "admin metrics rollups: OK"
      return 0
    fi
    sleep 1
  done
  assert_status "200" "${HTTP_STATUS}" "admin metrics"
  assert_request_metrics_nonzero "${outfile}"
}

ADMIN_HEADERS=(
  -H "X-Forwarded-User: e2e-admin"
  -H "X-Forwarded-Email: e2e-admin@example.com"
  -H "X-Forwarded-Groups: gateway-admins"
)

wait_for_status "/healthz" "200" "healthz"
wait_for_status "/readyz" "200" "readyz"

cat > "${TMP_DIR}/exchange-valid.json" <<'JSON'
{"turnstileToken":"e2e-turnstile-pass","limitedUse":false}
JSON
run_curl "${TMP_DIR}/exchange-valid.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary @"${TMP_DIR}/exchange-valid.json"
assert_status "200" "${HTTP_STATUS}" "exchange valid"
APP_CHECK_TOKEN="$(extract_json_field "${TMP_DIR}/exchange-valid.out" token)"
EXPIRE_TIME_MILLIS="$(extract_json_field "${TMP_DIR}/exchange-valid.out" expireTimeMillis)"
if [[ -z "${APP_CHECK_TOKEN}" || -z "${EXPIRE_TIME_MILLIS}" ]]; then
  echo "exchange valid: missing token or expireTimeMillis" >&2
  exit 1
fi
echo "exchange valid: HTTP 200"

run_curl "${TMP_DIR}/verify-valid.out" GET "${BASE_URL}/appcheck/api/v1/verify" \
  -H "X-Firebase-AppCheck: ${APP_CHECK_TOKEN}"
assert_status "204" "${HTTP_STATUS}" "verify valid"
echo "verify valid: HTTP 204"

run_curl "${TMP_DIR}/verify-missing.out" GET "${BASE_URL}/appcheck/api/v1/verify"
assert_status "401" "${HTTP_STATUS}" "verify missing token"
echo "verify missing token: HTTP 401"

run_curl "${TMP_DIR}/verify-invalid.out" POST "${BASE_URL}/appcheck/api/v1/verify" \
  -H "X-Firebase-AppCheck: invalid-token"
assert_status "401" "${HTTP_STATUS}" "verify invalid token"
echo "verify invalid token: HTTP 401"

cat > "${TMP_DIR}/exchange-fail.json" <<'JSON'
{"turnstileToken":"e2e-turnstile-fail","limitedUse":false}
JSON
run_curl "${TMP_DIR}/exchange-fail.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary @"${TMP_DIR}/exchange-fail.json"
assert_status "400" "${HTTP_STATUS}" "exchange invalid token"
echo "exchange invalid token: HTTP 400"

cat > "${TMP_DIR}/exchange-unknown.json" <<'JSON'
{"turnstileToken":"e2e-turnstile-pass","limitedUse":false,"unexpected":"attack"}
JSON
run_curl "${TMP_DIR}/exchange-unknown.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary @"${TMP_DIR}/exchange-unknown.json"
assert_status "400" "${HTTP_STATUS}" "exchange unknown field"
echo "exchange unknown field: HTTP 400"

run_curl "${TMP_DIR}/exchange-text.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: text/plain" \
  --data-binary 'turnstileToken=e2e-turnstile-pass'
assert_status "415" "${HTTP_STATUS}" "exchange wrong content type"
echo "exchange wrong content type: HTTP 415"

run_curl "${TMP_DIR}/admin-page.out" GET "${BASE_URL}/appcheck/admin/" "${ADMIN_HEADERS[@]}"
assert_status "200" "${HTTP_STATUS}" "admin dashboard"
echo "admin dashboard: HTTP 200"

wait_for_request_metrics

run_curl "${TMP_DIR}/admin-audit.out" GET "${BASE_URL}/appcheck/_admin/api/v1/audit-events?limit=20" "${ADMIN_HEADERS[@]}"
assert_status "200" "${HTTP_STATUS}" "admin audit"
assert_audit_events_nonzero "${TMP_DIR}/admin-audit.out"
assert_audit_sanitized "${TMP_DIR}/admin-audit.out"
echo "admin audit sanitization: OK"
