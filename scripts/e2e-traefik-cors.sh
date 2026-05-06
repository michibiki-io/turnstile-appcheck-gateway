#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${1:-}"
if [[ -z "${BASE_URL}" ]]; then
  echo "usage: $0 <base-url>" >&2
  exit 1
fi

ALLOWED_ORIGIN="${ALLOWED_ORIGIN:-https://allowed-origin.e2e.test}"
DENIED_ORIGIN="${DENIED_ORIGIN:-https://evil.example}"
PROTECTED_PATH="${PROTECTED_PATH:-/mx-api/api/v1/validate}"
VERIFY_HEADER_NAME="${VERIFY_HEADER_NAME:-X-Firebase-AppCheck}"
APP_CHECK_TOKEN="${APP_CHECK_TOKEN:-}"
CORS_EXCHANGE_TURNSTILE_TOKEN="${CORS_EXCHANGE_TURNSTILE_TOKEN:-e2e-turnstile-pass}"
TMP_DIR="$(mktemp -d)"
HTTP_STATUS=""
trap 'rm -rf "${TMP_DIR}"' EXIT

log() {
  printf '%s\n' "$*"
}

redact_e2e_output() {
  sed -E \
    -e 's/^((X-Firebase-App[Cc]heck|Authorization|Cookie|Set-Cookie):[[:space:]]*).*/\1<redacted>/' \
    -e 's/("token"[[:space:]]*:[[:space:]]*")[^"]+(")/\1<redacted>\2/g'
}

run_curl() {
  local label="$1"
  local method="$2"
  local path="$3"
  shift 3

  local safe_label
  safe_label="$(printf '%s' "${label}" | tr -c 'A-Za-z0-9_.-' '_')"
  local headers="${TMP_DIR}/${safe_label}.headers"
  local body="${TMP_DIR}/${safe_label}.body"

  HTTP_STATUS="$(curl -sS -D "${headers}" -o "${body}" -w "%{http_code}" -X "${method}" "${BASE_URL}${path}" "$@")"
  log "${label}: ${method} ${path} -> HTTP ${HTTP_STATUS}"
  sed -n '1,20p' "${headers}" | redact_e2e_output | sed 's/^/  header: /'
  if [[ -s "${body}" ]]; then
    sed -n '1,20p' "${body}" | redact_e2e_output | sed 's/^/  body: /'
  fi
  LAST_HEADERS="${headers}"
  LAST_BODY="${body}"
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

assert_2xx() {
  local actual="$1"
  local label="$2"
  if [[ ! "${actual}" =~ ^2 ]]; then
    echo "${label}: expected HTTP 2xx, got ${actual}" >&2
    exit 1
  fi
}

assert_header_equals() {
  local file="$1"
  local header_name="$2"
  local expected="$3"
  local label="$4"
  local actual
  actual="$(awk -v name="${header_name}" 'BEGIN{IGNORECASE=1} index($0, name ":") == 1 {sub("^[^:]+:[[:space:]]*", ""); gsub("\r$", ""); print; exit}' "${file}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "${label}: expected ${header_name}: ${expected}, got ${actual:-<missing>}" >&2
    exit 1
  fi
}

assert_header_contains() {
  local file="$1"
  local header_name="$2"
  local expected_part="$3"
  local label="$4"
  local actual
  actual="$(awk -v name="${header_name}" 'BEGIN{IGNORECASE=1} index($0, name ":") == 1 {sub("^[^:]+:[[:space:]]*", ""); gsub("\r$", ""); print; exit}' "${file}")"
  local actual_lower="${actual,,}"
  local expected_lower="${expected_part,,}"
  if [[ "${actual_lower}" != *"${expected_lower}"* ]]; then
    echo "${label}: expected ${header_name} to contain ${expected_part}, got ${actual:-<missing>}" >&2
    exit 1
  fi
}

assert_header_missing() {
  local file="$1"
  local header_name="$2"
  local label="$3"
  if awk -v name="${header_name}" 'BEGIN{IGNORECASE=1} index($0, name ":") == 1 {found=1} END{exit found ? 0 : 1}' "${file}"; then
    echo "${label}: unexpected ${header_name} header" >&2
    exit 1
  fi
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

log "traefik cors e2e: base=${BASE_URL} protected=${PROTECTED_PATH} allowed_origin=${ALLOWED_ORIGIN}"

if [[ -z "${APP_CHECK_TOKEN}" ]]; then
  cat > "${TMP_DIR}/exchange-valid.json" <<JSON
{"turnstileToken":"${CORS_EXCHANGE_TURNSTILE_TOKEN}","limitedUse":false}
JSON
  run_curl "cors.exchange-valid" POST "/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary @"${TMP_DIR}/exchange-valid.json"
  assert_status "200" "${HTTP_STATUS}" "cors exchange valid"
  APP_CHECK_TOKEN="$(extract_json_field "${LAST_BODY}" token)"
fi
if [[ -z "${APP_CHECK_TOKEN}" ]]; then
  echo "cors exchange valid: missing token" >&2
  exit 1
fi

run_curl "same-origin.missing-token" GET "${PROTECTED_PATH}"
assert_status "401" "${HTTP_STATUS}" "same-origin missing token"

run_curl "same-origin.valid-token" GET "${PROTECTED_PATH}" \
  -H "${VERIFY_HEADER_NAME}: ${APP_CHECK_TOKEN}"
assert_2xx "${HTTP_STATUS}" "same-origin valid token"
assert_header_missing "${LAST_HEADERS}" "Access-Control-Allow-Origin" "same-origin valid token"

run_curl "cross-origin.allowed-preflight" OPTIONS "${PROTECTED_PATH}" \
  -H "Origin: ${ALLOWED_ORIGIN}" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: content-type,x-firebase-appcheck"
assert_2xx "${HTTP_STATUS}" "allowed cross-origin preflight"
assert_header_equals "${LAST_HEADERS}" "Access-Control-Allow-Origin" "${ALLOWED_ORIGIN}" "allowed cross-origin preflight"
assert_header_contains "${LAST_HEADERS}" "Access-Control-Allow-Methods" "POST" "allowed cross-origin preflight"
assert_header_contains "${LAST_HEADERS}" "Access-Control-Allow-Headers" "X-Firebase-AppCheck" "allowed cross-origin preflight"

run_curl "cross-origin.allowed-valid-token" POST "${PROTECTED_PATH}" \
  -H "Origin: ${ALLOWED_ORIGIN}" \
  -H "${VERIFY_HEADER_NAME}: ${APP_CHECK_TOKEN}" \
  --data-binary '{"message":"ok"}'
assert_2xx "${HTTP_STATUS}" "allowed cross-origin valid token"
assert_header_equals "${LAST_HEADERS}" "Access-Control-Allow-Origin" "${ALLOWED_ORIGIN}" "allowed cross-origin valid token"

run_curl "cross-origin.allowed-missing-token" POST "${PROTECTED_PATH}" \
  -H "Origin: ${ALLOWED_ORIGIN}" \
  --data-binary '{"message":"missing"}'
assert_status "401" "${HTTP_STATUS}" "allowed cross-origin missing token"
assert_header_equals "${LAST_HEADERS}" "Access-Control-Allow-Origin" "${ALLOWED_ORIGIN}" "allowed cross-origin missing token"

run_curl "cross-origin.denied-preflight" OPTIONS "${PROTECTED_PATH}" \
  -H "Origin: ${DENIED_ORIGIN}" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: content-type,x-firebase-appcheck"
assert_header_missing "${LAST_HEADERS}" "Access-Control-Allow-Origin" "denied cross-origin preflight"

run_curl "bypass.spoofed-forwarded-method" POST "${PROTECTED_PATH}" \
  -H "Origin: ${ALLOWED_ORIGIN}" \
  -H "X-Forwarded-Method: OPTIONS"
assert_status "401" "${HTTP_STATUS}" "spoofed forwarded method bypass check"
assert_header_equals "${LAST_HEADERS}" "Access-Control-Allow-Origin" "${ALLOWED_ORIGIN}" "spoofed forwarded method bypass check"
if ! grep -q "APPCHECK_INVALID" "${LAST_BODY}"; then
  echo "spoofed forwarded method bypass check: expected APPCHECK_INVALID body" >&2
  exit 1
fi

log "traefik cors e2e: OK"
