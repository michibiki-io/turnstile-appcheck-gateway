#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-}"
WORK_DIR=""
PRESERVE_WORK_DIR="false"
HTTP_STATUS=""

TURNSTILE_TEST_SITE_KEY="1x00000000000000000000AA"
TURNSTILE_TEST_SECRET_KEY="1x0000000000000000000000000000000AA"
TURNSTILE_TEST_TOKEN="XXXX.DUMMY.TOKEN.XXXX"

TURNSTILE_MODE=""
TURNSTILE_MODE_DETAIL=""
TURNSTILE_SITE_KEY_EFFECTIVE=""
TURNSTILE_SECRET_KEY_EFFECTIVE=""
TURNSTILE_TOKEN="${REAL_E2E_TURNSTILE_TOKEN:-}"
VERIFY_SUCCESS_STATUS=""
BASE_URL=""
STATE_DIR="${REAL_E2E_LOCAL_STATE_DIR:-}"
CONTINUE_MODE="false"

usage() {
  cat <<'EOF'
Usage:
  scripts/e2e-real-local.sh
  REAL_E2E_TURNSTILE_TOKEN=... scripts/e2e-real-local.sh --continue

Modes:
  - full-real:
      REAL_E2E_TURNSTILE_TOKEN を使って real Turnstile + real Firebase を検証する
  - half-real:
      dummy Turnstile + real Firebase を検証する

Interactive local flow:
  1. scripts/e2e-real-local.sh を実行
  2. frontend で token を取得
  3. frontend に表示された snippet を repo root で実行
EOF
}

cleanup() {
  if [[ "${PRESERVE_WORK_DIR}" != "true" && -n "${WORK_DIR}" ]]; then
    docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/dev/docker-compose.yml" -f "${WORK_DIR}/override.yml" down -v >/dev/null 2>&1 || true
  fi
  if [[ "${PRESERVE_WORK_DIR}" != "true" && -n "${WORK_DIR}" ]]; then
    rm -rf "${WORK_DIR}"
  fi
}
trap cleanup EXIT

env_file_value() {
  python3 - "$1" "$2" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
key = sys.argv[2]
for raw in path.read_text(encoding="utf-8").splitlines():
    line = raw.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    current_key, value = line.split("=", 1)
    if current_key.strip() == key:
        print(value.strip())
        break
PY
}

env_value() {
  local key="$1"
  local current="${!key:-}"
  if [[ -n "${current}" ]]; then
    printf '%s' "${current}"
    return
  fi
  env_file_value "${ENV_FILE}" "${key}"
}

resolve_env_file() {
  if [[ -n "${ENV_FILE}" ]]; then
    return 0
  fi
  if [[ -f "${ROOT_DIR}/dev/.env" ]]; then
    ENV_FILE="${ROOT_DIR}/dev/.env"
    return 0
  fi
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    ENV_FILE="${ROOT_DIR}/.env"
    return 0
  fi
  echo "missing env file: set ENV_FILE or create dev/.env or .env" >&2
  exit 1
}

require_env_value() {
  local value="$1"
  local label="$2"
  if [[ -z "${value}" ]]; then
    echo "missing required value: ${label}" >&2
    exit 1
  fi
}

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
  local outfile="${WORK_DIR}/wait.out"
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

urlencode() {
  python3 - "$1" <<'PY'
import sys
from urllib.parse import quote
print(quote(sys.argv[1], safe=""))
PY
}

shell_escape() {
  printf '%q' "$1"
}

is_interactive() {
  [[ "${CI:-}" != "true" && -t 0 && -t 1 ]]
}

build_override_file() {
  cat > "${WORK_DIR}/override.yml" <<EOF
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
}

write_context_file() {
  cat > "${WORK_DIR}/context.env" <<EOF
ENV_FILE=$(shell_escape "${ENV_FILE}")
BASE_URL=$(shell_escape "${BASE_URL}")
VERIFY_SUCCESS_STATUS=$(shell_escape "${VERIFY_SUCCESS_STATUS}")
TURNSTILE_MODE=$(shell_escape "${TURNSTILE_MODE}")
TURNSTILE_MODE_DETAIL=$(shell_escape "${TURNSTILE_MODE_DETAIL}")
TURNSTILE_SITE_KEY_EFFECTIVE=$(shell_escape "${TURNSTILE_SITE_KEY_EFFECTIVE}")
TURNSTILE_SECRET_KEY_EFFECTIVE=$(shell_escape "${TURNSTILE_SECRET_KEY_EFFECTIVE}")
EOF
}

load_context_file() {
  require_env_value "${STATE_DIR}" "REAL_E2E_LOCAL_STATE_DIR"
  if [[ ! -f "${STATE_DIR}/context.env" ]]; then
    echo "missing local e2e state: ${STATE_DIR}/context.env" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  source "${STATE_DIR}/context.env"
  WORK_DIR="${STATE_DIR}"
}

configure_turnstile_mode() {
  local real_site_key="$1"
  local real_secret_key="$2"

  if [[ -n "${REAL_E2E_TURNSTILE_TOKEN:-}" ]]; then
    require_env_value "${real_secret_key}" "TURNSTILE_SECRET_KEY"
    TURNSTILE_MODE="full-real"
    TURNSTILE_MODE_DETAIL="REAL_E2E_TURNSTILE_TOKEN"
    TURNSTILE_SITE_KEY_EFFECTIVE="${real_site_key}"
    TURNSTILE_SECRET_KEY_EFFECTIVE="${real_secret_key}"
    TURNSTILE_TOKEN="${REAL_E2E_TURNSTILE_TOKEN}"
    return 0
  fi

  TURNSTILE_MODE="half-real"
  TURNSTILE_MODE_DETAIL="dummy-turnstile-real-firebase"
  TURNSTILE_SITE_KEY_EFFECTIVE="${TURNSTILE_TEST_SITE_KEY}"
  TURNSTILE_SECRET_KEY_EFFECTIVE="${TURNSTILE_TEST_SECRET_KEY}"
  TURNSTILE_TOKEN="${TURNSTILE_TEST_TOKEN}"
}

start_stack() {
  docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/dev/docker-compose.yml" -f "${WORK_DIR}/override.yml" up -d --build
  wait_for_status "/healthz" "200" "healthz"
  wait_for_status "/readyz" "200" "readyz"
}

prepare_manual_full_real() {
  local real_site_key="$1"
  local real_secret_key="$2"
  local encoded_state_dir
  local encoded_env_file

  TURNSTILE_MODE="full-real"
  TURNSTILE_MODE_DETAIL="manual-token-snippet"
  TURNSTILE_SITE_KEY_EFFECTIVE="${real_site_key}"
  TURNSTILE_SECRET_KEY_EFFECTIVE="${real_secret_key}"

  WORK_DIR="$(mktemp -d "${ROOT_DIR}/.tmp/e2e-real-local.XXXXXX")"
  mkdir -p "${WORK_DIR}"
  build_override_file
  write_context_file
  start_stack

  encoded_state_dir="$(urlencode "${WORK_DIR}")"
  encoded_env_file="$(urlencode "${ENV_FILE}")"
  PRESERVE_WORK_DIR="true"

  cat <<EOF
local e2e prepared in full-real manual mode
turnstile mode: full-real (${TURNSTILE_MODE_DETAIL})
open frontend: ${BASE_URL}/?e2e_state_dir=${encoded_state_dir}&e2e_env_file=${encoded_env_file}

After the widget issues a token, copy the snippet shown in the page and run it from the repo root.
If you want the fallback path instead, run:
  LOCAL_E2E_FORCE_HALF_REAL=true make e2e-real-local
EOF
}

check_logs_for_secret() {
  local compose_log="${WORK_DIR}/compose.log"
  docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/dev/docker-compose.yml" -f "${WORK_DIR}/override.yml" logs --no-color \
    > "${compose_log}" 2>/dev/null || true

  local marker
  for marker in \
    "$(env_value TURNSTILE_SECRET_KEY)" \
    "$(env_value GOOGLE_SERVICE_ACCOUNT_JSON_BASE64)" \
    "${TURNSTILE_TOKEN:-}" \
    "${APP_CHECK_TOKEN:-}"; do
    if [[ -n "${marker}" ]] && grep -Fq "${marker}" "${compose_log}"; then
      echo "compose logs leaked a secret or token" >&2
      exit 1
    fi
  done
}

run_local_e2e_assertions() {
  echo "turnstile mode: ${TURNSTILE_MODE} (${TURNSTILE_MODE_DETAIL})"

  cat > "${WORK_DIR}/exchange-valid.json" <<JSON
{"turnstileToken":"${TURNSTILE_TOKEN}","limitedUse":false}
JSON
  run_curl "${WORK_DIR}/exchange-valid.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary @"${WORK_DIR}/exchange-valid.json"
  assert_status "200" "${HTTP_STATUS}" "exchange valid"
  APP_CHECK_TOKEN="$(extract_json_field "${WORK_DIR}/exchange-valid.out" token)"
  EXPIRE_TIME_MILLIS="$(extract_json_field "${WORK_DIR}/exchange-valid.out" expireTimeMillis)"
  if [[ -z "${APP_CHECK_TOKEN}" || -z "${EXPIRE_TIME_MILLIS}" ]]; then
    echo "exchange valid: missing token or expireTimeMillis" >&2
    exit 1
  fi
  echo "exchange valid: HTTP 200"

  run_curl "${WORK_DIR}/verify-valid.out" GET "${BASE_URL}/appcheck/api/v1/verify" \
    -H "X-Firebase-AppCheck: ${APP_CHECK_TOKEN}"
  assert_status "${VERIFY_SUCCESS_STATUS}" "${HTTP_STATUS}" "verify valid"
  echo "verify valid: HTTP ${VERIFY_SUCCESS_STATUS}"

  run_curl "${WORK_DIR}/exchange-missing.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary '{"turnstileToken":"","limitedUse":false}'
  assert_status "400" "${HTTP_STATUS}" "exchange missing token"
  echo "exchange missing token: HTTP 400"

  if [[ "${TURNSTILE_MODE}" == "full-real" ]]; then
    run_curl "${WORK_DIR}/exchange-invalid.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
      -H "Content-Type: application/json" \
      --data-binary '{"turnstileToken":"invalid-token","limitedUse":false}'
    assert_status_class_4xx "${HTTP_STATUS}" "exchange invalid token"
    echo "exchange invalid token: HTTP ${HTTP_STATUS}"
  else
    echo "exchange invalid token: skipped in half-real mode"
  fi

  run_curl "${WORK_DIR}/exchange-malformed.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary '{"turnstileToken":'
  assert_status "400" "${HTTP_STATUS}" "exchange malformed json"
  echo "exchange malformed json: HTTP 400"

  run_curl "${WORK_DIR}/exchange-unknown.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: application/json" \
    --data-binary '{"turnstileToken":"invalid-token","limitedUse":false,"unexpected":"attack"}'
  assert_status "400" "${HTTP_STATUS}" "exchange unknown field"
  echo "exchange unknown field: HTTP 400"

  run_curl "${WORK_DIR}/exchange-text.out" POST "${BASE_URL}/appcheck/api/v1/exchange" \
    -H "Content-Type: text/plain" \
    --data-binary 'turnstileToken=invalid-token'
  assert_status "415" "${HTTP_STATUS}" "exchange wrong content type"
  echo "exchange wrong content type: HTTP 415"

  run_curl "${WORK_DIR}/verify-missing.out" GET "${BASE_URL}/appcheck/api/v1/verify"
  assert_status "401" "${HTTP_STATUS}" "verify missing token"
  echo "verify missing token: HTTP 401"

  run_curl "${WORK_DIR}/verify-invalid.out" GET "${BASE_URL}/appcheck/api/v1/verify" \
    -H "X-Firebase-AppCheck: invalid-token"
  assert_status "401" "${HTTP_STATUS}" "verify invalid token"
  echo "verify invalid token: HTTP 401"

  run_curl "${WORK_DIR}/admin-missing.out" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics"
  assert_status "401" "${HTTP_STATUS}" "admin missing auth"
  echo "admin missing auth: HTTP 401"

  run_curl "${WORK_DIR}/admin-wrong-group.out" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics" \
    -H "X-Forwarded-User: e2e-admin" \
    -H "X-Forwarded-Email: e2e-admin@example.com" \
    -H "X-Forwarded-Groups: forbidden-group"
  assert_status "403" "${HTTP_STATUS}" "admin wrong group"
  echo "admin wrong group: HTTP 403"

  run_curl "${WORK_DIR}/admin-valid.out" GET "${BASE_URL}/appcheck/_admin/api/v1/request-metrics" \
    -H "X-Forwarded-User: e2e-admin" \
    -H "X-Forwarded-Email: e2e-admin@example.com" \
    -H "X-Forwarded-Groups: gateway-admins"
  assert_status "200" "${HTTP_STATUS}" "admin valid auth"
  echo "admin valid auth: HTTP 200"

  check_logs_for_secret
  echo "log sanitization: OK"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --continue)
        CONTINUE_MODE="true"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "unknown argument: $1" >&2
        usage >&2
        exit 1
        ;;
    esac
  done
}

main() {
  parse_args "$@"
  resolve_env_file

  if [[ "${CONTINUE_MODE}" == "true" ]]; then
    require_env_value "${REAL_E2E_TURNSTILE_TOKEN:-}" "REAL_E2E_TURNSTILE_TOKEN"
    load_context_file
    TURNSTILE_TOKEN="${REAL_E2E_TURNSTILE_TOKEN}"
    TURNSTILE_MODE="full-real"
    TURNSTILE_MODE_DETAIL="REAL_E2E_TURNSTILE_TOKEN"
    wait_for_status "/healthz" "200" "healthz"
    wait_for_status "/readyz" "200" "readyz"
    run_local_e2e_assertions
    return 0
  fi

  local real_site_key
  local real_secret_key

  mkdir -p "${ROOT_DIR}/.tmp"

  real_site_key="$(env_value TURNSTILE_SITE_KEY)"
  real_secret_key="$(env_value TURNSTILE_SECRET_KEY)"
  require_env_value "$(env_value FIREBASE_PROJECT_ID)" "FIREBASE_PROJECT_ID"
  require_env_value "$(env_value FIREBASE_APP_ID)" "FIREBASE_APP_ID"
  require_env_value "$(env_value GOOGLE_SERVICE_ACCOUNT_JSON_BASE64)" "GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"

  TRAEFIK_PORT="$(env_value TRAEFIK_PORT)"
  if [[ -z "${TRAEFIK_PORT}" ]]; then
    TRAEFIK_PORT="8080"
  fi
  BASE_URL="http://localhost:${TRAEFIK_PORT}"
  VERIFY_SUCCESS_STATUS="$(env_value VERIFY_SUCCESS_STATUS)"
  if [[ -z "${VERIFY_SUCCESS_STATUS}" ]]; then
    VERIFY_SUCCESS_STATUS="204"
  fi

  if [[ -z "${REAL_E2E_TURNSTILE_TOKEN:-}" && "${LOCAL_E2E_FORCE_HALF_REAL:-false}" != "true" ]] && is_interactive; then
    require_env_value "${real_site_key}" "TURNSTILE_SITE_KEY"
    require_env_value "${real_secret_key}" "TURNSTILE_SECRET_KEY"
    prepare_manual_full_real "${real_site_key}" "${real_secret_key}"
    return 0
  fi

  if [[ -z "${REAL_E2E_TURNSTILE_TOKEN:-}" && "${LOCAL_E2E_FORCE_HALF_REAL:-false}" != "true" && "${CI:-}" != "true" ]]; then
    echo "interactive terminal not detected; using half-real fallback" >&2
  fi

  configure_turnstile_mode "${real_site_key}" "${real_secret_key}"
  WORK_DIR="$(mktemp -d "${ROOT_DIR}/.tmp/e2e-real-local-run.XXXXXX")"
  build_override_file
  start_stack
  run_local_e2e_assertions
}

main "$@"
