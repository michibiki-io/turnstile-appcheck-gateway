#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
ENV_FILE="${TMP_DIR}/release.env"
DEV_ENV_FILE="${ROOT_DIR}/dev/.env"
HTTP_STATUS=""
export TRAEFIK_PORT=8080
export TRAEFIK_DASHBOARD_PORT=8088

TURNSTILE_TEST_SITE_KEY="1x00000000000000000000AA"
TURNSTILE_TEST_SECRET_KEY="1x0000000000000000000000000000000AA"
TURNSTILE_TEST_TOKEN="XXXX.DUMMY.TOKEN.XXXX"

TURNSTILE_MODE=""
TURNSTILE_MODE_DETAIL=""
TURNSTILE_SITE_KEY_EFFECTIVE=""
TURNSTILE_SECRET_KEY_EFFECTIVE=""
TURNSTILE_TOKEN="${REAL_E2E_TURNSTILE_TOKEN:-}"

require_env() {
  if [[ -z "${!1:-}" ]]; then
    echo "missing required environment variable: $1" >&2
    exit 1
  fi
}

cleanup() {
  docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/dev/docker-compose.yml" -f "${TMP_DIR}/override.yml" down -v >/dev/null 2>&1 || true
  rm -f "${DEV_ENV_FILE}" >/dev/null 2>&1 || true
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

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
  local base_url="$1"
  local path="$2"
  local expected="$3"
  local label="$4"
  local outfile="${TMP_DIR}/wait.out"
  local attempt
  for attempt in $(seq 1 90); do
    if run_curl "${outfile}" GET "${base_url}${path}"; then
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

configure_turnstile_mode() {
  if [[ -n "${REAL_E2E_TURNSTILE_TOKEN:-}" ]]; then
    require_env TURNSTILE_SECRET_KEY
    TURNSTILE_MODE="full-real"
    TURNSTILE_MODE_DETAIL="REAL_E2E_TURNSTILE_TOKEN"
    TURNSTILE_SITE_KEY_EFFECTIVE="${TURNSTILE_SITE_KEY:-${TURNSTILE_TEST_SITE_KEY}}"
    TURNSTILE_SECRET_KEY_EFFECTIVE="${TURNSTILE_SECRET_KEY}"
    TURNSTILE_TOKEN="${REAL_E2E_TURNSTILE_TOKEN}"
    return 0
  fi

  TURNSTILE_MODE="half-real"
  TURNSTILE_MODE_DETAIL="dummy-turnstile-real-firebase"
  TURNSTILE_SITE_KEY_EFFECTIVE="${TURNSTILE_TEST_SITE_KEY}"
  TURNSTILE_SECRET_KEY_EFFECTIVE="${TURNSTILE_TEST_SECRET_KEY}"
  TURNSTILE_TOKEN="${TURNSTILE_TEST_TOKEN}"
}

check_logs_for_secret() {
  local compose_log="${TMP_DIR}/compose.log"
  docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/dev/docker-compose.yml" -f "${TMP_DIR}/override.yml" logs --no-color \
    > "${compose_log}" 2>/dev/null || true

  local marker
  for marker in \
    "${TURNSTILE_SECRET_KEY:-}" \
    "${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64}" \
    "${TURNSTILE_TOKEN:-}" \
    "${APP_CHECK_TOKEN:-}"; do
    if [[ -n "${marker}" ]] && grep -Fq "${marker}" "${compose_log}"; then
      echo "compose logs leaked a secret or token" >&2
      exit 1
    fi
  done
}

require_env FIREBASE_PROJECT_ID
require_env FIREBASE_APP_ID
require_env GOOGLE_SERVICE_ACCOUNT_JSON_BASE64

configure_turnstile_mode

cat > "${ENV_FILE}" <<EOF
TRAEFIK_PORT=8080
TRAEFIK_DASHBOARD_PORT=8088
APPCHECK_SUBPATH=/appcheck
LOG_LEVEL=info
REQUEST_TIMEOUT=10s
APPCHECK_TOKEN_TTL=30m
TURNSTILE_SITE_KEY=${TURNSTILE_SITE_KEY_EFFECTIVE}
TURNSTILE_SECRET_KEY=${TURNSTILE_SECRET_KEY_EFFECTIVE}
TURNSTILE_SITEVERIFY_URL=https://challenges.cloudflare.com/turnstile/v0/siteverify
FIREBASE_PROJECT_ID=${FIREBASE_PROJECT_ID}
FIREBASE_APP_ID=${FIREBASE_APP_ID}
FIREBASE_APP_RESOURCE=${FIREBASE_APP_RESOURCE:-}
GOOGLE_SERVICE_ACCOUNT_JSON_BASE64=${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64}
TRUST_PROXY_HEADERS=true
VERIFY_HEADER_NAME=X-Firebase-AppCheck
VERIFY_SUCCESS_STATUS=204
VERIFY_FAILURE_STATUS=401
HEALTH_PATH=/healthz
READY_PATH=/readyz
ADMIN_DASHBOARD_ENABLED=true
ADMIN_BASE_PATH=/admin
ADMIN_AUDIT_TIMESTAMP_FORMAT=2006-01-02 15:04:05 MST
ADMIN_AUDIT_TIMESTAMP_TIMEZONE=Asia/Tokyo
ADMIN_AUTH_MODE=header
ADMIN_AUTH_USER_HEADER=X-Forwarded-User
ADMIN_AUTH_EMAIL_HEADER=X-Forwarded-Email
ADMIN_AUTH_GROUPS_HEADER=X-Forwarded-Groups
ADMIN_ALLOWED_USERS=
ADMIN_ALLOWED_GROUPS=gateway-admins
AUDIT_ENABLED=true
AUDIT_SQLITE_PATH=/var/lib/turnstile-appcheck-gateway/audit.db
AUDIT_RETENTION_DAYS=90
RATE_LIMIT_ENABLED=false
RATE_LIMIT_REQUESTS=120
RATE_LIMIT_WINDOW=1m
EOF

cp "${ENV_FILE}" "${DEV_ENV_FILE}"

cat > "${TMP_DIR}/override.yml" <<EOF
services:
  turnstile-appcheck-gateway:
    environment:
      TURNSTILE_SECRET_KEY: ${TURNSTILE_SECRET_KEY_EFFECTIVE}
      ADMIN_AUTH_MODE: header
      ADMIN_ALLOWED_GROUPS: gateway-admins
      AUDIT_SQLITE_PATH: /var/lib/turnstile-appcheck-gateway/audit.db
      RATE_LIMIT_ENABLED: "false"
    labels:
      - "traefik.http.routers.appcheck-health.rule=Path(\`/healthz\`)"
      - "traefik.http.routers.appcheck-health.entrypoints=web"
      - "traefik.http.routers.appcheck-health.priority=110"
      - "traefik.http.routers.appcheck-ready.rule=Path(\`/readyz\`)"
      - "traefik.http.routers.appcheck-ready.entrypoints=web"
      - "traefik.http.routers.appcheck-ready.priority=110"
  frontend:
    environment:
      TURNSTILE_SITE_KEY: ${TURNSTILE_SITE_KEY_EFFECTIVE}
    labels:
      - "traefik.http.routers.frontend.rule=PathPrefix(\`/\`) && !PathPrefix(\`/appcheck\`) && !PathPrefix(\`/backend\`) && !Path(\`/healthz\`) && !Path(\`/readyz\`)"
EOF

BASE_URL="http://localhost:8080"
docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/dev/docker-compose.yml" -f "${TMP_DIR}/override.yml" up -d --build

wait_for_status "${BASE_URL}" "/healthz" "200" "healthz"
wait_for_status "${BASE_URL}" "/readyz" "200" "readyz"

echo "turnstile mode: ${TURNSTILE_MODE} (${TURNSTILE_MODE_DETAIL})"

cat > "${TMP_DIR}/exchange-valid.json" <<JSON
{"turnstileToken":"${TURNSTILE_TOKEN}","limitedUse":false}
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

if [[ "${TURNSTILE_MODE}" == "full-real" ]]; then
  run_curl "${TMP_DIR}/exchange-invalid.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary '{"turnstileToken":"invalid-token","limitedUse":false}'
  assert_status_class_4xx "${HTTP_STATUS}" "exchange invalid token"
  echo "exchange invalid token: HTTP ${HTTP_STATUS}"
else
  echo "exchange invalid token: skipped in half-real mode"
fi

run_curl "${TMP_DIR}/exchange-unknown.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
  -H "Content-Type: application/json" \
  --data-binary '{"turnstileToken":"invalid-token","limitedUse":false,"unexpected":"attack"}'
assert_status "400" "${HTTP_STATUS}" "exchange unknown field"
echo "exchange unknown field: HTTP 400"

run_curl "${TMP_DIR}/verify-invalid.out" GET "${BASE_URL}/appcheck/api/v1/verify" \
  -H "X-Firebase-AppCheck: invalid-token"
assert_status "401" "${HTTP_STATUS}" "verify invalid token"
echo "verify invalid token: HTTP 401"

check_logs_for_secret
echo "log sanitization: OK"
