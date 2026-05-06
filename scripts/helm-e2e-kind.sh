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
ALLOWED_CORS_ORIGIN="${ALLOWED_CORS_ORIGIN:-https://allowed-origin.e2e.test}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-${ROOT_DIR}/.tmp/kind-${CLUSTER_NAME}.kubeconfig}"
KEEP_CLUSTER="${KEEP_CLUSTER:-false}"
KEEP_ON_FAILURE="${KEEP_ON_FAILURE:-true}"
KIND_E2E_MODE="${KIND_E2E_MODE:-mock}"
KIND_E2E_AUDIT_STORAGE="${KIND_E2E_AUDIT_STORAGE:-sqlite}"
ENV_FILE="${ENV_FILE:-}"
FULL_REAL_RUN_LOCAL_PORT="${FULL_REAL_RUN_LOCAL_PORT:-18081}"
PORT_FORWARD_PID=""

TURNSTILE_TEST_SECRET_KEY="1x0000000000000000000000000000000AA"
TURNSTILE_TEST_TOKEN="XXXX.DUMMY.TOKEN.XXXX"
TURNSTILE_SITE_KEY_EFFECTIVE=""
TURNSTILE_SECRET_KEY_EFFECTIVE=""
TURNSTILE_EXCHANGE_TOKEN=""
TURNSTILE_MODE_DETAIL=""
APP_CHECK_TOKEN=""
APP_CHECK_TOKEN_FILE=""
VALUES_FILE=""
AUDIT_DSN_EFFECTIVE=""

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

for cmd in docker kind kubectl helm curl python3; do
  require_command "${cmd}"
done

case "${KIND_E2E_MODE}" in
  mock|half-real|full-real|full-real-prepare)
    ;;
  *)
    echo "KIND_E2E_MODE must be one of: mock, half-real, full-real, full-real-prepare" >&2
    exit 1
    ;;
esac

case "${KIND_E2E_AUDIT_STORAGE}" in
  sqlite|postgres|mariadb)
    ;;
  *)
    echo "KIND_E2E_AUDIT_STORAGE must be one of: sqlite, postgres, mariadb" >&2
    exit 1
    ;;
esac

mkdir -p "${ROOT_DIR}/.tmp"

env_file_value() {
  local file="$1"
  local key="$2"
  if [[ ! -f "${file}" ]]; then
    return 0
  fi
  python3 - "$file" "$key" <<'PY'
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
}

env_value() {
  local key="$1"
  local current="${!key:-}"
  if [[ -n "${current}" ]]; then
    printf '%s' "${current}"
    return 0
  fi
  if [[ -n "${ENV_FILE}" ]]; then
    env_file_value "${ENV_FILE}" "${key}"
  fi
}

require_env_value() {
  local value="$1"
  local label="$2"
  if [[ -z "${value}" ]]; then
    echo "missing required value: ${label}" >&2
    if [[ -n "${ENV_FILE}" ]]; then
      echo "looked in env and ${ENV_FILE}" >&2
    fi
    exit 1
  fi
}

debug_dump() {
  echo "collecting kind debug output" >&2
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${TRAEFIK_NAMESPACE}" get all || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${TRAEFIK_NAMESPACE}" logs deploy/"${TRAEFIK_RELEASE_NAME}" || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get all || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get ingressroute,middleware || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get events --sort-by=.lastTimestamp || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" logs deploy/audit-postgres || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" logs deploy/audit-mariadb || true
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

  rm -f "${VALUES_FILE}" "${APP_CHECK_TOKEN_FILE}"

  if [[ ${exit_code} -ne 0 && "${KIND_E2E_MODE}" != "full-real-prepare" ]]; then
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

configure_audit_storage() {
  case "${KIND_E2E_AUDIT_STORAGE}" in
    sqlite)
      AUDIT_DSN_EFFECTIVE=""
      ;;
    postgres)
      AUDIT_DSN_EFFECTIVE="postgres://turnstile:turnstile@audit-postgres.${NAMESPACE}.svc.cluster.local:5432/turnstile_appcheck_gateway?sslmode=disable"
      ;;
    mariadb)
      AUDIT_DSN_EFFECTIVE="turnstile:turnstile@tcp(audit-mariadb.${NAMESPACE}.svc.cluster.local:3306)/turnstile_appcheck_gateway?parseTime=true&charset=utf8mb4&loc=UTC"
      ;;
  esac
}

configure_real_mode() {
  resolve_env_file || true

  FIREBASE_PROJECT_ID_EFFECTIVE="$(env_value FIREBASE_PROJECT_ID)"
  FIREBASE_APP_ID_EFFECTIVE="$(env_value FIREBASE_APP_ID)"
  FIREBASE_APP_RESOURCE_EFFECTIVE="$(env_value FIREBASE_APP_RESOURCE)"
  GOOGLE_SERVICE_ACCOUNT_JSON_BASE64_EFFECTIVE="$(env_value GOOGLE_SERVICE_ACCOUNT_JSON_BASE64)"

  require_env_value "${FIREBASE_PROJECT_ID_EFFECTIVE}" "FIREBASE_PROJECT_ID"
  if [[ -z "${FIREBASE_APP_RESOURCE_EFFECTIVE}" ]]; then
    require_env_value "${FIREBASE_APP_ID_EFFECTIVE}" "FIREBASE_APP_ID"
  fi
  require_env_value "${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64_EFFECTIVE}" "GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"

  if [[ "${KIND_E2E_MODE}" == "full-real" || "${KIND_E2E_MODE}" == "full-real-prepare" ]]; then
    TURNSTILE_SITE_KEY_EFFECTIVE="$(env_value TURNSTILE_SITE_KEY)"
    TURNSTILE_SECRET_KEY_EFFECTIVE="$(env_value TURNSTILE_SECRET_KEY)"
    require_env_value "${TURNSTILE_SITE_KEY_EFFECTIVE}" "TURNSTILE_SITE_KEY"
    require_env_value "${TURNSTILE_SECRET_KEY_EFFECTIVE}" "TURNSTILE_SECRET_KEY"
    TURNSTILE_MODE_DETAIL="REAL_E2E_TURNSTILE_TOKEN"
    if [[ "${KIND_E2E_MODE}" == "full-real" ]]; then
      TURNSTILE_EXCHANGE_TOKEN="${REAL_E2E_TURNSTILE_TOKEN:-}"
      require_env_value "${TURNSTILE_EXCHANGE_TOKEN}" "REAL_E2E_TURNSTILE_TOKEN"
    fi
  else
    TURNSTILE_SECRET_KEY_EFFECTIVE="${TURNSTILE_TEST_SECRET_KEY}"
    TURNSTILE_EXCHANGE_TOKEN="${TURNSTILE_TEST_TOKEN}"
    TURNSTILE_MODE_DETAIL="dummy-turnstile-real-firebase"
  fi
}

write_values_file() {
  VALUES_FILE="$(mktemp "${ROOT_DIR}/.tmp/kind-values.XXXXXX.yaml")"

  export KIND_E2E_MODE
  export TURNSTILE_SECRET_KEY_EFFECTIVE
  export FIREBASE_PROJECT_ID_EFFECTIVE="${FIREBASE_PROJECT_ID_EFFECTIVE:-}"
  export FIREBASE_APP_ID_EFFECTIVE="${FIREBASE_APP_ID_EFFECTIVE:-}"
  export FIREBASE_APP_RESOURCE_EFFECTIVE="${FIREBASE_APP_RESOURCE_EFFECTIVE:-}"
  export GOOGLE_SERVICE_ACCOUNT_JSON_BASE64_EFFECTIVE="${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64_EFFECTIVE:-}"
  export KIND_E2E_AUDIT_STORAGE
  export AUDIT_DSN_EFFECTIVE="${AUDIT_DSN_EFFECTIVE:-}"

  python3 - "${VALUES_FILE}" <<'PY'
import json
import os
import sys

mode = os.environ["KIND_E2E_MODE"]

config = {
    "ADMIN_AUTH_MODE": "header",
    "ADMIN_ALLOWED_GROUPS": "gateway-admins",
    "AUDIT_STORAGE_TYPE": os.environ["KIND_E2E_AUDIT_STORAGE"],
    "RATE_LIMIT_ENABLED": "false",
}
secrets = {
    "TURNSTILE_SECRET_KEY": "dummy",
    "GOOGLE_SERVICE_ACCOUNT_JSON_BASE64": "dummy",
}
extra_env = []

if mode == "mock":
    extra_env = [
        {"name": "E2E_UPSTREAM_MODE", "value": "mock"},
        {"name": "E2E_TURNSTILE_PASS_TOKEN", "value": "e2e-turnstile-pass"},
        {"name": "E2E_TURNSTILE_FAIL_TOKEN", "value": "e2e-turnstile-fail"},
        {"name": "E2E_APPCHECK_TOKEN", "value": "e2e-appcheck-valid-token"},
        {"name": "E2E_APPCHECK_APP_ID", "value": "e2e-app-id"},
    ]
else:
    config.update({
        "FIREBASE_PROJECT_ID": os.environ.get("FIREBASE_PROJECT_ID_EFFECTIVE", ""),
        "FIREBASE_APP_ID": os.environ.get("FIREBASE_APP_ID_EFFECTIVE", ""),
        "FIREBASE_APP_RESOURCE": os.environ.get("FIREBASE_APP_RESOURCE_EFFECTIVE", ""),
        "TRUST_PROXY_HEADERS": "true",
    })
    secrets = {
        "TURNSTILE_SECRET_KEY": os.environ["TURNSTILE_SECRET_KEY_EFFECTIVE"],
        "GOOGLE_SERVICE_ACCOUNT_JSON_BASE64": os.environ["GOOGLE_SERVICE_ACCOUNT_JSON_BASE64_EFFECTIVE"],
    }

audit_dsn = os.environ.get("AUDIT_DSN_EFFECTIVE", "")
if audit_dsn:
    secrets["AUDIT_DSN"] = audit_dsn

def dump_map(name, values):
    print(f"{name}:")
    for key, value in values.items():
        print(f"  {key}: {json.dumps(value)}")

with open(sys.argv[1], "w", encoding="utf-8") as fh:
    old_stdout = sys.stdout
    sys.stdout = fh
    dump_map("config", config)
    dump_map("secrets", secrets)
    if extra_env:
        print("extraEnv:")
        for item in extra_env:
            print(f"  - name: {json.dumps(item['name'])}")
            print(f"    value: {json.dumps(item['value'])}")
    else:
        print("extraEnv: []")
    sys.stdout = old_stdout
PY
  chmod 600 "${VALUES_FILE}"
}

check_k8s_logs_for_secret() {
  local log_file="${ROOT_DIR}/.tmp/kind-e2e-service.log"
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" logs deploy/"${RELEASE_NAME}" \
    > "${log_file}" 2>/dev/null || true

  local marker
  for marker in \
    "${TURNSTILE_SECRET_KEY_EFFECTIVE:-}" \
    "${GOOGLE_SERVICE_ACCOUNT_JSON_BASE64_EFFECTIVE:-}" \
    "${AUDIT_DSN_EFFECTIVE:-}" \
    "${TURNSTILE_EXCHANGE_TOKEN:-}" \
    "${APP_CHECK_TOKEN:-}"; do
    if [[ -n "${marker}" ]] && grep -Fq "${marker}" "${log_file}"; then
      echo "kubernetes logs leaked a secret or token" >&2
      exit 1
    fi
  done
  rm -f "${log_file}"
}

apply_audit_database() {
  case "${KIND_E2E_AUDIT_STORAGE}" in
    sqlite)
      return 0
      ;;
    postgres)
      kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: audit-postgres
  labels:
    app.kubernetes.io/name: audit-postgres
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: audit-postgres
  template:
    metadata:
      labels:
        app.kubernetes.io/name: audit-postgres
    spec:
      containers:
        - name: postgres
          image: postgres:17-alpine
          ports:
            - containerPort: 5432
              name: postgres
          env:
            - name: POSTGRES_DB
              value: turnstile_appcheck_gateway
            - name: POSTGRES_USER
              value: turnstile
            - name: POSTGRES_PASSWORD
              value: turnstile
          readinessProbe:
            exec:
              command:
                - pg_isready
                - -U
                - turnstile
                - -d
                - turnstile_appcheck_gateway
            initialDelaySeconds: 3
            periodSeconds: 2
            timeoutSeconds: 2
            failureThreshold: 30
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: audit-postgres
spec:
  selector:
    app.kubernetes.io/name: audit-postgres
  ports:
    - name: postgres
      port: 5432
      targetPort: postgres
EOF
      kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" rollout status deploy/audit-postgres --timeout=5m
      ;;
    mariadb)
      kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: audit-mariadb
  labels:
    app.kubernetes.io/name: audit-mariadb
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: audit-mariadb
  template:
    metadata:
      labels:
        app.kubernetes.io/name: audit-mariadb
    spec:
      containers:
        - name: mariadb
          image: mariadb:11.4
          ports:
            - containerPort: 3306
              name: mariadb
          env:
            - name: MARIADB_DATABASE
              value: turnstile_appcheck_gateway
            - name: MARIADB_USER
              value: turnstile
            - name: MARIADB_PASSWORD
              value: turnstile
            - name: MARIADB_ROOT_PASSWORD
              value: root
          readinessProbe:
            exec:
              command:
                - /bin/sh
                - -ec
                - mariadb-admin ping -h 127.0.0.1 -uturnstile -pturnstile --silent
            initialDelaySeconds: 5
            periodSeconds: 2
            timeoutSeconds: 2
            failureThreshold: 60
          volumeMounts:
            - name: data
              mountPath: /var/lib/mysql
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: audit-mariadb
spec:
  selector:
    app.kubernetes.io/name: audit-mariadb
  ports:
    - name: mariadb
      port: 3306
      targetPort: mariadb
EOF
      kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" rollout status deploy/audit-mariadb --timeout=5m
      ;;
  esac
}

apply_full_real_prepare_frontend() {
  local frontend_dir
  local frontend_url
  frontend_dir="$(mktemp -d "${ROOT_DIR}/.tmp/kind-full-real-frontend.XXXXXX")"

  python3 - "${ROOT_DIR}/dev/frontend/index.html" "${frontend_dir}/index.html" "${TURNSTILE_SITE_KEY_EFFECTIVE}" <<'PY'
import pathlib
import sys

source = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
target = source.replace("__TURNSTILE_SITE_KEY__", sys.argv[3])
pathlib.Path(sys.argv[2]).write_text(target, encoding="utf-8")
PY

  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" delete configmap appcheck-e2e-frontend \
    >/dev/null 2>&1 || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" create configmap appcheck-e2e-frontend \
    --from-file=index.html="${frontend_dir}/index.html"
  rm -rf "${frontend_dir}"

  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: appcheck-e2e-frontend
  labels:
    app.kubernetes.io/name: appcheck-e2e-frontend
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: appcheck-e2e-frontend
  template:
    metadata:
      labels:
        app.kubernetes.io/name: appcheck-e2e-frontend
    spec:
      containers:
        - name: nginx
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
              name: http
          volumeMounts:
            - name: frontend
              mountPath: /usr/share/nginx/html/index.html
              subPath: index.html
              readOnly: true
      volumes:
        - name: frontend
          configMap:
            name: appcheck-e2e-frontend
---
apiVersion: v1
kind: Service
metadata:
  name: appcheck-e2e-frontend
spec:
  selector:
    app.kubernetes.io/name: appcheck-e2e-frontend
  ports:
    - name: http
      port: 80
      targetPort: http
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: appcheck-e2e-frontend
spec:
  entryPoints:
    - web
  routes:
    - kind: Rule
      match: PathPrefix(\`/\`) && !PathPrefix(\`/appcheck\`) && !PathPrefix(\`/mx-api\`) && !Path(\`/healthz\`) && !Path(\`/readyz\`)
      services:
        - name: appcheck-e2e-frontend
          port: 80
EOF

  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" rollout status deploy/appcheck-e2e-frontend --timeout=5m

  frontend_url="http://127.0.0.1:${LOCAL_PORT}/?next_local_port=${FULL_REAL_RUN_LOCAL_PORT}"
  echo
  echo "kind full-real prepare is ready"
  echo "open frontend: ${frontend_url}"
  echo
  echo "After Turnstile issues a token, copy the snippet shown in the page and run it from the repo root."
  echo "The generated snippet uses LOCAL_PORT=${FULL_REAL_RUN_LOCAL_PORT} so it can run while this prepare process is still active."
  echo "Press Ctrl-C here after you no longer need the browser page."
  echo

  while true; do
    sleep 3600
  done
}

helm lint "${ROOT_DIR}/deploy/chart" \
  --set-string secrets.TURNSTILE_SECRET_KEY=dummy \
  --set-string secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64=dummy

configure_audit_storage
if [[ "${KIND_E2E_MODE}" != "mock" ]]; then
  configure_real_mode
fi
write_values_file

echo "kind e2e mode: ${KIND_E2E_MODE}"
echo "kind audit storage: ${KIND_E2E_AUDIT_STORAGE}"

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

apply_audit_database

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
  -f "${VALUES_FILE}" \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=IfNotPresent \
  --set persistence.enabled=false

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

BASE_URL="http://127.0.0.1:${LOCAL_PORT}"

if [[ "${KIND_E2E_MODE}" == "mock" ]]; then
  "${ROOT_DIR}/scripts/e2e-smoke-mock.sh" "${BASE_URL}"
  ALLOWED_ORIGIN="${ALLOWED_CORS_ORIGIN}" "${ROOT_DIR}/scripts/e2e-traefik-cors.sh" "${BASE_URL}"
elif [[ "${KIND_E2E_MODE}" == "full-real-prepare" ]]; then
  apply_full_real_prepare_frontend
else
  APP_CHECK_TOKEN_FILE="$(mktemp "${ROOT_DIR}/.tmp/kind-appcheck-token.XXXXXX")"
  TURNSTILE_MODE="${KIND_E2E_MODE}" \
    TURNSTILE_MODE_DETAIL="${TURNSTILE_MODE_DETAIL}" \
    TURNSTILE_EXCHANGE_TOKEN="${TURNSTILE_EXCHANGE_TOKEN}" \
    APP_CHECK_TOKEN_OUT_FILE="${APP_CHECK_TOKEN_FILE}" \
    "${ROOT_DIR}/scripts/e2e-appcheck-real.sh" "${BASE_URL}"
  APP_CHECK_TOKEN="$(cat "${APP_CHECK_TOKEN_FILE}")"
  APP_CHECK_TOKEN="${APP_CHECK_TOKEN}" \
    ALLOWED_ORIGIN="${ALLOWED_CORS_ORIGIN}" \
    "${ROOT_DIR}/scripts/e2e-traefik-cors.sh" "${BASE_URL}"
  check_k8s_logs_for_secret
  echo "log sanitization: OK"
fi
