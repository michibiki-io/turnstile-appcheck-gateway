#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-turnstile-appcheck-gateway-e2e}"
NAMESPACE="${NAMESPACE:-appcheck-e2e}"
RELEASE_NAME="${RELEASE_NAME:-turnstile-appcheck-gateway}"
IMAGE_NAME="${IMAGE_NAME:-turnstile-appcheck-gateway:e2e}"
LOCAL_PORT="${LOCAL_PORT:-18080}"
TRAEFIK_NAMESPACE="${TRAEFIK_NAMESPACE:-traefik-e2e}"
TRAEFIK_RELEASE_NAME="${TRAEFIK_RELEASE_NAME:-traefik}"
ALLOWED_CORS_ORIGIN="${ALLOWED_CORS_ORIGIN:-https://sinensis-lab.timelessbond.org}"
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
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${TRAEFIK_NAMESPACE}" get all || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${TRAEFIK_NAMESPACE}" logs deploy/"${TRAEFIK_RELEASE_NAME}" || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get all || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get ingressroute,middleware || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get events --sort-by=.lastTimestamp || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" describe deploy "${RELEASE_NAME}" || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" logs deploy/"${RELEASE_NAME}" || true
  helm --kubeconfig "${KUBECONFIG_PATH}" status "${RELEASE_NAME}" -n "${NAMESPACE}" || true
  helm --kubeconfig "${KUBECONFIG_PATH}" status "${TRAEFIK_RELEASE_NAME}" -n "${TRAEFIK_NAMESPACE}" || true
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

kubectl --kubeconfig "${KUBECONFIG_PATH}" create namespace "${TRAEFIK_NAMESPACE}" --dry-run=client -o yaml | \
  kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f -

if ! helm repo list | awk '{print $1}' | grep -Fxq traefik; then
  helm repo add traefik https://traefik.github.io/charts
fi
helm repo update traefik

helm --kubeconfig "${KUBECONFIG_PATH}" upgrade --install "${TRAEFIK_RELEASE_NAME}" traefik/traefik \
  -n "${TRAEFIK_NAMESPACE}" \
  --wait \
  --timeout 5m \
  --set deployment.replicas=1 \
  --set service.type=ClusterIP

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

kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mx-api
  labels:
    app.kubernetes.io/name: mx-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: mx-api
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mx-api
    spec:
      containers:
        - name: whoami
          image: traefik/whoami:v1.11
          ports:
            - containerPort: 80
              name: http
---
apiVersion: v1
kind: Service
metadata:
  name: mx-api
spec:
  selector:
    app.kubernetes.io/name: mx-api
  ports:
    - name: http
      port: 80
      targetPort: http
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: mx-api-cors
spec:
  headers:
    accessControlAllowOriginList:
      - "${ALLOWED_CORS_ORIGIN}"
    accessControlAllowMethods:
      - GET
      - POST
      - OPTIONS
    accessControlAllowHeaders:
      - Content-Type
      - X-Firebase-AppCheck
    accessControlMaxAge: 86400
    addVaryHeader: true
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: strip-spoofed-forwarded-method
spec:
  headers:
    customRequestHeaders:
      X-Forwarded-Method: ""
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: appcheck-forward-auth
spec:
  forwardAuth:
    address: http://${RELEASE_NAME}.${NAMESPACE}.svc.cluster.local/appcheck/api/v1/verify
    trustForwardHeader: false
    authResponseHeaders:
      - X-AppCheck-Verified
      - X-AppCheck-AppID
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: appcheck-gateway
spec:
  entryPoints:
    - web
  routes:
    - kind: Rule
      match: PathPrefix(\`/appcheck\`) || Path(\`/healthz\`) || Path(\`/readyz\`)
      services:
        - name: ${RELEASE_NAME}
          port: 80
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: mx-api
spec:
  entryPoints:
    - web
  routes:
    - kind: Rule
      match: PathPrefix(\`/mx-api\`)
      middlewares:
        - name: mx-api-cors
        - name: strip-spoofed-forwarded-method
        - name: appcheck-forward-auth
      services:
        - name: mx-api
          port: 80
EOF

kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" rollout status deploy/mx-api --timeout=5m

kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${TRAEFIK_NAMESPACE}" port-forward svc/"${TRAEFIK_RELEASE_NAME}" "${LOCAL_PORT}:80" \
  > "${ROOT_DIR}/.tmp/kind-port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!

"${ROOT_DIR}/scripts/e2e-smoke-mock.sh" "http://127.0.0.1:${LOCAL_PORT}"
ALLOWED_ORIGIN="${ALLOWED_CORS_ORIGIN}" "${ROOT_DIR}/scripts/e2e-traefik-cors.sh" "http://127.0.0.1:${LOCAL_PORT}"
