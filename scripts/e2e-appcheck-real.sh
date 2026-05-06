#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${1:-}"
if [[ -z "${BASE_URL}" ]]; then
  echo "usage: $0 <base-url>" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
HTTP_STATUS=""
VERIFY_SUCCESS_STATUS="${VERIFY_SUCCESS_STATUS:-204}"
TURNSTILE_MODE="${TURNSTILE_MODE:-half-real}"
TURNSTILE_MODE_DETAIL="${TURNSTILE_MODE_DETAIL:-dummy-turnstile-real-firebase}"
TURNSTILE_EXCHANGE_TOKEN="${TURNSTILE_EXCHANGE_TOKEN:-}"
APP_CHECK_TOKEN_OUT_FILE="${APP_CHECK_TOKEN_OUT_FILE:-}"
trap 'rm -rf "${TMP_DIR}"' EXIT

if [[ -z "${TURNSTILE_EXCHANGE_TOKEN}" ]]; then
  echo "missing required environment variable: TURNSTILE_EXCHANGE_TOKEN" >&2
  exit 1
fi

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

assert_status_class_4xx() {
  local actual="$1"
  local label="$2"
  if [[ ! "${actual}" =~ ^4[0-9][0-9]$ ]]; then
    echo "${label}: expected 4xx, got ${actual}" >&2
    exit 1
  fi
}

wait_for_status() {
  local path="$1"
  local expected="$2"
  local label="$3"
  local outfile="${TMP_DIR}/wait.out"
  local attempt
  for attempt in $(seq 1 90); do
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

wait_for_status "/healthz" "200" "healthz"
wait_for_status "/readyz" "200" "readyz"

echo "turnstile mode: ${TURNSTILE_MODE} (${TURNSTILE_MODE_DETAIL})"

cat > "${TMP_DIR}/exchange-valid.json" <<JSON
{"turnstileToken":"${TURNSTILE_EXCHANGE_TOKEN}","limitedUse":false}
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
if [[ -n "${APP_CHECK_TOKEN_OUT_FILE}" ]]; then
  printf '%s' "${APP_CHECK_TOKEN}" > "${APP_CHECK_TOKEN_OUT_FILE}"
  chmod 600 "${APP_CHECK_TOKEN_OUT_FILE}"
fi
echo "exchange valid: HTTP 200"

run_curl "${TMP_DIR}/verify-valid.out" GET "${BASE_URL}/appcheck/api/v1/verify" \
  -H "X-Firebase-AppCheck: ${APP_CHECK_TOKEN}"
assert_status "${VERIFY_SUCCESS_STATUS}" "${HTTP_STATUS}" "verify valid"
echo "verify valid: HTTP ${VERIFY_SUCCESS_STATUS}"

run_curl "${TMP_DIR}/exchange-missing.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary '{"turnstileToken":"","limitedUse":false}'
assert_status "400" "${HTTP_STATUS}" "exchange missing token"
echo "exchange missing token: HTTP 400"

if [[ "${TURNSTILE_MODE}" == "full-real" ]]; then
  run_curl "${TMP_DIR}/exchange-invalid.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary '{"turnstileToken":"invalid-token","limitedUse":false}'
  assert_status_class_4xx "${HTTP_STATUS}" "exchange invalid token"
  echo "exchange invalid token: HTTP ${HTTP_STATUS}"
else
  echo "exchange invalid token: skipped in half-real mode"
fi

run_curl "${TMP_DIR}/exchange-malformed.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary '{"turnstileToken":'
assert_status "400" "${HTTP_STATUS}" "exchange malformed json"
echo "exchange malformed json: HTTP 400"

run_curl "${TMP_DIR}/exchange-unknown.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary '{"turnstileToken":"invalid-token","limitedUse":false,"unexpected":"attack"}'
assert_status "400" "${HTTP_STATUS}" "exchange unknown field"
echo "exchange unknown field: HTTP 400"

run_curl "${TMP_DIR}/exchange-text.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: text/plain" \
  --data-binary 'turnstileToken=invalid-token'
assert_status "415" "${HTTP_STATUS}" "exchange wrong content type"
echo "exchange wrong content type: HTTP 415"

run_curl "${TMP_DIR}/verify-missing.out" GET "${BASE_URL}/appcheck/api/v1/verify"
assert_status "401" "${HTTP_STATUS}" "verify missing token"
echo "verify missing token: HTTP 401"

run_curl "${TMP_DIR}/verify-invalid.out" GET "${BASE_URL}/appcheck/api/v1/verify" \
  -H "X-Firebase-AppCheck: invalid-token"
assert_status "401" "${HTTP_STATUS}" "verify invalid token"
echo "verify invalid token: HTTP 401"

run_curl "${TMP_DIR}/admin-missing.out" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics"
assert_status "401" "${HTTP_STATUS}" "admin missing auth"
echo "admin missing auth: HTTP 401"

run_curl "${TMP_DIR}/admin-wrong-group.out" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics" \
  -H "X-Forwarded-User: e2e-admin" \
  -H "X-Forwarded-Email: e2e-admin@example.com" \
  -H "X-Forwarded-Groups: forbidden-group"
assert_status "403" "${HTTP_STATUS}" "admin wrong group"
echo "admin wrong group: HTTP 403"

run_curl "${TMP_DIR}/admin-valid.out" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics" \
  -H "X-Forwarded-User: e2e-admin" \
  -H "X-Forwarded-Email: e2e-admin@example.com" \
  -H "X-Forwarded-Groups: gateway-admins"
assert_status "200" "${HTTP_STATUS}" "admin valid auth"
echo "admin valid auth: HTTP 200"

echo "appcheck real e2e: OK"
