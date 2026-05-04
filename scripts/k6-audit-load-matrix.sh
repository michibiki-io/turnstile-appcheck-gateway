#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/dev/.env}"
K6_IMAGE="${K6_IMAGE:-grafana/k6:0.54.0}"
K6_DURATION="${K6_DURATION:-30s}"
TRAEFIK_PORT="${TRAEFIK_PORT:-39080}"
TRAEFIK_DASHBOARD_PORT="${TRAEFIK_DASHBOARD_PORT:-39088}"
RESULT_ROOT="${RESULT_ROOT:-${ROOT_DIR}/.tmp/k6-audit-load/$(date -u +%Y%m%dT%H%M%SZ)}"

BASE_COMPOSE=(-f "${ROOT_DIR}/dev/docker-compose.yml" -f "${ROOT_DIR}/dev/docker-compose.e2e.yml")
CSV_FILE="${RESULT_ROOT}/summary.csv"
CURRENT_COMPOSE=()

mkdir -p "${RESULT_ROOT}"
cp -n "${ROOT_DIR}/dev/.env.example" "${ENV_FILE}" || true

cleanup() {
  if [[ "${KEEP_LOAD_STACK:-false}" == "true" ]]; then
    return
  fi
  if [[ ${#CURRENT_COMPOSE[@]} -gt 0 ]]; then
    docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

write_csv_header() {
  cat > "${CSV_FILE}" <<'CSV'
case,db,audit_async,mode,rate,duration,iterations,checks_rate,http_req_failed_rate,http_req_duration_avg_ms,http_req_duration_p90_ms,http_req_duration_p95_ms,http_req_duration_p99_ms,http_req_duration_max_ms
CSV
}

compose_db_file() {
  case "$1" in
    postgres) printf "%s\n" "${ROOT_DIR}/dev/docker-compose.pg.yml" ;;
    mariadb) printf "%s\n" "${ROOT_DIR}/dev/docker-compose.mariadb.yml" ;;
    *) echo "unsupported db $1" >&2; exit 1 ;;
  esac
}

rates_for_case() {
  local mode="$1"
  local async="$2"
  if [[ -n "${LOAD_RATES:-}" ]]; then
    printf "%s\n" "${LOAD_RATES//,/ }"
    return
  fi
  if [[ "${mode}" == "exchange-valid" ]]; then
    printf "%s\n" "100 300 500"
  elif [[ "${async}" == "true" ]]; then
    printf "%s\n" "500 1000 2000"
  else
    printf "%s\n" "100 500 1000"
  fi
}

wait_for_ready() {
  local url="http://127.0.0.1:${TRAEFIK_PORT}/readyz"
  local attempt
  for attempt in $(seq 1 90); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "service did not become ready at ${url}" >&2
  docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" ps >&2 || true
  docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" logs --no-color --tail=200 >&2 || true
  exit 1
}

start_stack() {
  local db="$1"
  local async="$2"
  local case_dir="$3"
  local override="${case_dir}/audit-async.override.yml"
  cat > "${override}" <<YAML
services:
  turnstile-appcheck-gateway:
    environment:
      AUDIT_ASYNC_ENABLED: "${async}"
YAML

  CURRENT_COMPOSE=("${BASE_COMPOSE[@]}" -f "$(compose_db_file "${db}")" -f "${override}")
  docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  TRAEFIK_PORT="${TRAEFIK_PORT}" TRAEFIK_DASHBOARD_PORT="${TRAEFIK_DASHBOARD_PORT}" \
    docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" up -d --build
  wait_for_ready
}

append_summary_csv() {
  local summary="$1"
  local case_id="$2"
  local db="$3"
  local async="$4"
  local mode="$5"
  local rate="$6"
  python3 - "$summary" "$CSV_FILE" "$case_id" "$db" "$async" "$mode" "$rate" "$K6_DURATION" <<'PY'
import csv
import json
import sys

summary_path, csv_path, case_id, db, async_enabled, mode, rate, duration = sys.argv[1:]
with open(summary_path, encoding="utf-8") as fh:
    data = json.load(fh)
metrics = data.get("metrics", {})

def metric_value(name, key, default=0):
    metric = metrics.get(name, {})
    if key in metric:
        return metric.get(key, default)
    return metric.get("values", {}).get(key, default)

row = {
    "case": case_id,
    "db": db,
    "audit_async": async_enabled,
    "mode": mode,
    "rate": rate,
    "duration": duration,
    "iterations": metric_value("iterations", "count"),
    "checks_rate": metric_value("checks", "rate", metric_value("checks", "value")),
    "http_req_failed_rate": metric_value("http_req_failed", "rate", metric_value("http_req_failed", "value")),
    "http_req_duration_avg_ms": metric_value("http_req_duration", "avg"),
    "http_req_duration_p90_ms": metric_value("http_req_duration", "p(90)"),
    "http_req_duration_p95_ms": metric_value("http_req_duration", "p(95)"),
    "http_req_duration_p99_ms": metric_value("http_req_duration", "p(99)"),
    "http_req_duration_max_ms": metric_value("http_req_duration", "max"),
}
with open(csv_path, "a", encoding="utf-8", newline="") as fh:
    writer = csv.DictWriter(fh, fieldnames=list(row))
    writer.writerow(row)
PY
}

run_k6() {
  local case_id="$1"
  local db="$2"
  local async="$3"
  local mode="$4"
  local rate="$5"
  local case_dir="$6"
  local run_dir="${case_dir}/${mode}-${rate}"
  mkdir -p "${run_dir}"
  chmod 0777 "${run_dir}"
  echo "k6 case=${case_id} db=${db} async=${async} mode=${mode} rate=${rate} duration=${K6_DURATION}"
  set +e
  docker run --rm --network appcheck-devnet \
    -v "${ROOT_DIR}:/src:ro" \
    -v "${run_dir}:/results" \
    -e BASE_URL="http://traefik" \
    -e MODE="${mode}" \
    -e RATE="${rate}" \
    -e DURATION="${K6_DURATION}" \
    -e PRE_ALLOCATED_VUS="${PRE_ALLOCATED_VUS:-}" \
    -e MAX_VUS="${MAX_VUS:-}" \
    "${K6_IMAGE}" run --summary-export "/results/summary.json" "/src/scripts/k6/audit-load.js" \
    2>&1 | tee "${run_dir}/k6.log"
  local status=${PIPESTATUS[0]}
  set -e
  if [[ -f "${run_dir}/summary.json" ]]; then
    append_summary_csv "${run_dir}/summary.json" "${case_id}" "${db}" "${async}" "${mode}" "${rate}"
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "k6 failed for case=${case_id} db=${db} async=${async} mode=${mode} rate=${rate}" >&2
    return "${status}"
  fi
}

run_case() {
  local case_id="$1"
  local db="$2"
  local async="$3"
  local mode="$4"
  local case_dir="${RESULT_ROOT}/case-${case_id}-${db}-async-${async}-${mode}"
  mkdir -p "${case_dir}"
  start_stack "${db}" "${async}" "${case_dir}"
  local rate
  for rate in $(rates_for_case "${mode}" "${async}"); do
    run_k6 "${case_id}" "${db}" "${async}" "${mode}" "${rate}" "${case_dir}"
  done
  docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" logs --no-color --tail=300 > "${case_dir}/compose.log" 2>/dev/null || true
  docker compose --env-file "${ENV_FILE}" "${CURRENT_COMPOSE[@]}" down -v --remove-orphans
  CURRENT_COMPOSE=()
}

should_run_case() {
  local case_id="$1"
  if [[ -z "${LOAD_CASES:-}" ]]; then
    return 0
  fi
  case ",${LOAD_CASES}," in
    *",${case_id},"*) return 0 ;;
    *) return 1 ;;
  esac
}

maybe_run_case() {
  local case_id="$1"
  shift
  if should_run_case "${case_id}"; then
    run_case "${case_id}" "$@"
  fi
}

main() {
  write_csv_header
  maybe_run_case 1 postgres false verify-missing
  maybe_run_case 2 mariadb false verify-missing
  maybe_run_case 3 postgres true verify-missing
  maybe_run_case 4 mariadb true verify-missing
  maybe_run_case 5 postgres true exchange-valid
  maybe_run_case 6 mariadb true exchange-valid
  echo "k6 audit load results: ${RESULT_ROOT}"
  echo "summary csv: ${CSV_FILE}"
}

main "$@"
