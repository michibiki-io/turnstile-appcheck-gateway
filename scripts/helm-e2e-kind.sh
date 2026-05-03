#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-turnstile-appcheck-gateway-e2e}"
NAMESPACE="${NAMESPACE:-appcheck-e2e}"
RELEASE_NAME="${RELEASE_NAME:-turnstile-appcheck-gateway}"
IMAGE_NAME="${IMAGE_NAME:-turnstile-appcheck-gateway:e2e}"
LOCAL_PORT="${LOCAL_PORT:-18080}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-${ROOT_DIR}/.tmp/kind-${CLUSTER_NAME}.kubeconfig}"
KEEP_CLUSTER="${KEEP_CLUSTER:-false}"
KEEP_ON_FAILURE="${KEEP_ON_FAILURE:-true}"
PORT_FORWARD_PID=""

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

for cmd in docker kind kubectl helm curl python3; do
  require_command "${cmd}"
done

mkdir -p "${ROOT_DIR}/.tmp"

debug_dump() {
  echo "collecting kind debug output" >&2
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get all || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get events --sort-by=.lastTimestamp || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" describe deploy "${RELEASE_NAME}" || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" logs deploy/"${RELEASE_NAME}" || true
  helm --kubeconfig "${KUBECONFIG_PATH}" status "${RELEASE_NAME}" -n "${NAMESPACE}" || true
}

cleanup() {
  local exit_code=$?

  if [[ -n "${PORT_FORWARD_PID}" ]]; then
    kill "${PORT_FORWARD_PID}" >/dev/null 2>&1 || true
    wait "${PORT_FORWARD_PID}" 2>/dev/null || true
  fi

  if [[ ${exit_code} -ne 0 ]]; then
    debug_dump
    if [[ "${KEEP_ON_FAILURE}" == "true" ]]; then
      echo "kind resources preserved after failure" >&2
      exit "${exit_code}"
    fi
  fi

  if [[ ${exit_code} -eq 0 && "${KEEP_CLUSTER}" == "true" ]]; then
    echo "kind resources preserved after success" >&2
    exit 0
  fi

  kind delete cluster --name "${CLUSTER_NAME}" >/dev/null 2>&1 || true
  rm -f "${KUBECONFIG_PATH}"
  exit "${exit_code}"
}
trap cleanup EXIT

helm lint "${ROOT_DIR}/deploy/chart" \
  --set-string secrets.TURNSTILE_SECRET_KEY=dummy \
  --set-string secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64=dummy

if kind get clusters | grep -Fxq "${CLUSTER_NAME}"; then
  kind get kubeconfig --name "${CLUSTER_NAME}" > "${KUBECONFIG_PATH}"
else
  kind create cluster --name "${CLUSTER_NAME}" --kubeconfig "${KUBECONFIG_PATH}"
fi
chmod 600 "${KUBECONFIG_PATH}"

kubectl --kubeconfig "${KUBECONFIG_PATH}" get nodes

docker build -t "${IMAGE_NAME}" "${ROOT_DIR}"
kind load docker-image "${IMAGE_NAME}" --name "${CLUSTER_NAME}"

kubectl --kubeconfig "${KUBECONFIG_PATH}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | \
  kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f -

IMAGE_REPOSITORY="${IMAGE_NAME%:*}"
IMAGE_TAG="${IMAGE_NAME##*:}"
if [[ "${IMAGE_REPOSITORY}" == "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="latest"
fi

helm --kubeconfig "${KUBECONFIG_PATH}" upgrade --install "${RELEASE_NAME}" "${ROOT_DIR}/deploy/chart" \
  -n "${NAMESPACE}" \
  --wait \
  --timeout 5m \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=IfNotPresent \
  --set persistence.enabled=false \
  --set-string config.ADMIN_AUTH_MODE=header \
  --set-string config.ADMIN_ALLOWED_GROUPS=gateway-admins \
  --set-string config.RATE_LIMIT_ENABLED=false \
  --set-string secrets.TURNSTILE_SECRET_KEY=dummy \
  --set-string secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64=dummy \
  --set extraEnv[0].name=E2E_UPSTREAM_MODE \
  --set extraEnv[0].value=mock \
  --set extraEnv[1].name=E2E_TURNSTILE_PASS_TOKEN \
  --set extraEnv[1].value=e2e-turnstile-pass \
  --set extraEnv[2].name=E2E_TURNSTILE_FAIL_TOKEN \
  --set extraEnv[2].value=e2e-turnstile-fail \
  --set extraEnv[3].name=E2E_APPCHECK_TOKEN \
  --set extraEnv[3].value=e2e-appcheck-valid-token \
  --set extraEnv[4].name=E2E_APPCHECK_APP_ID \
  --set extraEnv[4].value=e2e-app-id

kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" rollout status deploy/"${RELEASE_NAME}" --timeout=5m

kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" port-forward svc/"${RELEASE_NAME}" "${LOCAL_PORT}:80" \
  > "${ROOT_DIR}/.tmp/kind-port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!

"${ROOT_DIR}/scripts/e2e-smoke-mock.sh" "http://127.0.0.1:${LOCAL_PORT}"
