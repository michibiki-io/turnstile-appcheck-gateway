#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
WORKFLOW_PATH="${ROOT_DIR}/.github/workflows/release-e2e.yml"
EVENT_NAME="workflow_dispatch"
JOB_NAME="release-e2e"
RUNNER_IMAGE="${ACT_RUNNER_IMAGE:-catthehacker/ubuntu:full-latest}"
CONTAINER_ARCH="${ACT_CONTAINER_ARCH:-linux/amd64}"
ACT_GO_IMAGE="${ACT_GO_IMAGE:-golang:1.26.2}"
LOCAL_ACT_BIN="${ROOT_DIR}/bin/act"
DOCKER_SOCKET_PATH="${DOCKER_SOCKET_PATH:-/var/run/docker.sock}"

cleanup() {
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_env_value() {
  local value="$1"
  local name="$2"
  if [[ -z "${value}" ]]; then
    echo "missing required value: ${name}" >&2
    exit 1
  fi
}

resolve_github_token() {
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    printf '%s\n' "${GITHUB_TOKEN}"
    return 0
  fi
  if [[ -n "${GH_TOKEN:-}" ]]; then
    printf '%s\n' "${GH_TOKEN}"
    return 0
  fi
  if command -v gh >/dev/null 2>&1; then
    if gh auth status >/dev/null 2>&1; then
      gh auth token
      return 0
    fi
  fi
  return 1
}

pick_env_file() {
  if [[ -n "${ENV_FILE:-}" ]]; then
    printf '%s\n' "${ENV_FILE}"
    return 0
  fi
  if [[ -f "${ROOT_DIR}/dev/.env" ]]; then
    printf '%s\n' "${ROOT_DIR}/dev/.env"
    return 0
  fi
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    printf '%s\n' "${ROOT_DIR}/.env"
    return 0
  fi
  echo "env file not found. Set ENV_FILE, or prepare dev/.env or .env." >&2
  exit 1
}

pick_ref() {
  if [[ -n "${REF:-}" ]]; then
    printf '%s\n' "${REF}"
    return 0
  fi
  local branch
  branch="$(git -C "${ROOT_DIR}" branch --show-current || true)"
  if [[ -n "${branch}" ]]; then
    printf '%s\n' "${branch}"
    return 0
  fi
  git -C "${ROOT_DIR}" rev-parse HEAD
}

write_secret() {
  local key="$1"
  local value="$2"
  if [[ -n "${value}" ]]; then
    printf '%s=%s\n' "${key}" "${value}" >> "${SECRETS_FILE}"
  fi
}

docker_socket_group_id() {
  if [[ ! -S "${DOCKER_SOCKET_PATH}" ]]; then
    return 1
  fi
  stat -c '%g' "${DOCKER_SOCKET_PATH}"
}

require_cmd docker
require_cmd git

ENV_SOURCE_FILE="$(pick_env_file)"
REF_VALUE="$(pick_ref)"
SECRETS_FILE="${WORK_DIR}/act.secrets"
GITHUB_TOKEN_VALUE=""
GITHUB_TOKEN_SOURCE="none"
DOCKER_SOCKET_GID=""

set -a
# shellcheck disable=SC1090
source "${ENV_SOURCE_FILE}"
set +a

require_env_value "${FIREBASE_PROJECT_ID:-}" "FIREBASE_PROJECT_ID"
require_env_value "${FIREBASE_APP_ID:-}" "FIREBASE_APP_ID"
require_env_value "${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64:-}" "GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"

if GITHUB_TOKEN_VALUE="$(resolve_github_token)"; then
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    GITHUB_TOKEN_SOURCE="env:GITHUB_TOKEN"
  elif [[ -n "${GH_TOKEN:-}" ]]; then
    GITHUB_TOKEN_SOURCE="env:GH_TOKEN"
  else
    GITHUB_TOKEN_SOURCE="gh auth token"
  fi
fi

if DOCKER_SOCKET_GID="$(docker_socket_group_id)"; then
  :
fi

: > "${SECRETS_FILE}"
write_secret GITHUB_TOKEN "${GITHUB_TOKEN_VALUE}"
write_secret FIREBASE_PROJECT_ID "${FIREBASE_PROJECT_ID:-}"
write_secret FIREBASE_APP_ID "${FIREBASE_APP_ID:-}"
write_secret FIREBASE_APP_RESOURCE "${FIREBASE_APP_RESOURCE:-}"
write_secret GOOGLE_SERVICE_ACCOUNT_JSON_BASE64 "${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64:-}"
write_secret TURNSTILE_SECRET_KEY "${TURNSTILE_SECRET_KEY:-}"
write_secret REAL_E2E_TURNSTILE_TOKEN "${REAL_E2E_TURNSTILE_TOKEN:-}"

echo "act release-e2e"
echo "  workflow: ${WORKFLOW_PATH}"
echo "  ref: ${REF_VALUE}"
echo "  env file: ${ENV_SOURCE_FILE}"
echo "  runner image: ${RUNNER_IMAGE}"
echo "  github token: ${GITHUB_TOKEN_SOURCE}"
echo "  docker socket: ${DOCKER_SOCKET_PATH}"
echo "  docker socket gid: ${DOCKER_SOCKET_GID:-unresolved}"

ACT_ARGS=(
  "${EVENT_NAME}"
  -W "${WORKFLOW_PATH}"
  -j "${JOB_NAME}"
  --input "ref=${REF_VALUE}"
  --secret-file "${SECRETS_FILE}"
  --container-architecture "${CONTAINER_ARCH}"
  -P "ubuntu-latest=${RUNNER_IMAGE}"
)

if [[ -n "${DOCKER_SOCKET_GID}" ]]; then
  ACT_ARGS+=(
    --container-options "--group-add=${DOCKER_SOCKET_GID}"
  )
fi

if [[ -x "${LOCAL_ACT_BIN}" ]]; then
  echo "  act binary: ${LOCAL_ACT_BIN}"
  exec "${LOCAL_ACT_BIN}" "${ACT_ARGS[@]}"
fi

if command -v act >/dev/null 2>&1; then
  echo "  act binary: $(command -v act)"
  exec act "${ACT_ARGS[@]}"
fi

echo "  act binary: dockerized fallback via ${ACT_GO_IMAGE}"

cat > "${WORK_DIR}/run-act-in-container.sh" <<EOF
#!/usr/bin/env bash
set -Eeuo pipefail
export GOBIN=/tmp/act-bin
mkdir -p "\${GOBIN}"
/usr/local/go/bin/go install github.com/nektos/act@latest
exec "\${GOBIN}/act" "\$@"
EOF
chmod +x "${WORK_DIR}/run-act-in-container.sh"

exec docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${ROOT_DIR}:${ROOT_DIR}" \
  -v "${WORK_DIR}:${WORK_DIR}" \
  -w "${ROOT_DIR}" \
  "${ACT_GO_IMAGE}" \
  bash "${WORK_DIR}/run-act-in-container.sh" "${ACT_ARGS[@]}"
