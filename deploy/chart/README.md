# turnstile-appcheck-gateway Helm Chart

This chart installs `turnstile-appcheck-gateway`, a Go/Gin gateway that bridges Cloudflare Turnstile and Firebase App Check Custom Provider. It exposes:

- `POST {SUBPATH}/api/v1/exchange` for frontend token exchange
- `ANY {SUBPATH}/api/v1/verify` for Traefik forwardAuth verification
- `GET /healthz`
- `GET /readyz`
- `GET {SUBPATH}/admin/` and `GET {SUBPATH}/_admin/api/v1/...` when the admin dashboard is enabled

The chart separates non-sensitive runtime configuration into a `ConfigMap` and sensitive values into a `Secret`. It supports SQLite audit log persistence with a PVC, an ephemeral `emptyDir` mode, and external PostgreSQL or MariaDB/MySQL audit databases through `config.AUDIT_STORAGE_TYPE` and `secrets.AUDIT_DSN`.

## Prerequisites

- Helm 3.8 or later
- Kubernetes 1.26 or later
- Access to `ghcr.io/michibiki-io/charts/turnstile-appcheck-gateway` for OCI installs
- Cloudflare Turnstile secret key
- Firebase project ID
- Firebase App ID or Firebase App Resource
- Google service account JSON or base64-encoded JSON

`GOOGLE_SERVICE_ACCOUNT_JSON_BASE64` is recommended. The application prefers it over `GOOGLE_SERVICE_ACCOUNT_JSON` when both are set.

## Install from Local Path

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"
```

`config.FIREBASE_PROJECT_ID` is required in real deployments. Set either `config.FIREBASE_APP_ID` or `config.FIREBASE_APP_RESOURCE`.

## Install from OCI Registry

```bash
helm install turnstile-appcheck-gateway \
  oci://ghcr.io/michibiki-io/charts/turnstile-appcheck-gateway \
  --version 0.1.0 \
  --namespace appcheck \
  --create-namespace \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"
```

The published chart version and `appVersion` are expected to be overridden during release packaging. If `image.tag` is empty, the chart uses `.Chart.AppVersion`.

## Upgrade

```bash
helm upgrade turnstile-appcheck-gateway \
  oci://ghcr.io/michibiki-io/charts/turnstile-appcheck-gateway \
  --version 0.1.0 \
  --namespace appcheck
```

## Uninstall

```bash
helm uninstall turnstile-appcheck-gateway --namespace appcheck
```

## Ingress Example

Keep `ingress.hosts[].paths[].path` aligned with `config.APPCHECK_SUBPATH`.

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set config.APPCHECK_SUBPATH=/appcheck \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64" \
  --set ingress.enabled=true \
  --set ingress.className=traefik \
  --set ingress.hosts[0].host=appcheck.example.com \
  --set ingress.hosts[0].paths[0].path=/appcheck \
  --set ingress.hosts[0].paths[0].pathType=Prefix
```

With the defaults, these endpoints become reachable below the same ingress path:

- `/appcheck/api/v1/exchange`
- `/appcheck/api/v1/verify`
- `/appcheck/admin/`
- `/appcheck/_admin/api/v1/...`

## Traefik forwardAuth Example

This chart deploys the gateway itself. Other services can use its `/verify` endpoint as a Traefik `forwardAuth` backend.

```yaml
http:
  middlewares:
    appcheck-forward-auth:
      forwardAuth:
        address: "http://turnstile-appcheck-gateway.appcheck.svc.cluster.local:80/appcheck/api/v1/verify"
        trustForwardHeader: true
        authResponseHeaders:
          - X-AppCheck-Verified
          - X-AppCheck-AppID
```

`VERIFY_SUCCESS_STATUS` must stay a 2xx status and `VERIFY_FAILURE_STATUS` must stay a non-2xx status.

## Admin Dashboard

The default dashboard URL is:

```text
https://appcheck.example.com/appcheck/admin/
```

The default admin API prefix is:

```text
https://appcheck.example.com/appcheck/_admin/api/v1/...
```

Enable or adjust it with values like:

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64" \
  --set config.ADMIN_DASHBOARD_ENABLED=true \
  --set config.ADMIN_BASE_PATH=/admin
```

## Header Auth Example for Admin Dashboard

`ADMIN_AUTH_MODE=header` assumes a trusted upstream authentication layer injects identity headers such as `X-Forwarded-User`, `X-Forwarded-Email`, and `X-Forwarded-Groups`.

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.className=traefik \
  --set ingress.annotations."traefik\\.ingress\\.kubernetes\\.io/router\\.middlewares"=appcheck-admin-auth@kubernetescrd \
  --set ingress.hosts[0].host=appcheck.example.com \
  --set ingress.hosts[0].paths[0].path=/appcheck \
  --set ingress.hosts[0].paths[0].pathType=Prefix \
  --set config.ADMIN_AUTH_MODE=header \
  --set config.ADMIN_AUTH_USER_HEADER=X-Forwarded-User \
  --set config.ADMIN_AUTH_EMAIL_HEADER=X-Forwarded-Email \
  --set config.ADMIN_AUTH_GROUPS_HEADER=X-Forwarded-Groups \
  --set config.ADMIN_ALLOWED_GROUPS=gateway-admins \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"
```

Only identities matching `ADMIN_ALLOWED_USERS` or `ADMIN_ALLOWED_GROUPS` are allowed.

## Warning for `ADMIN_AUTH_MODE=none`

`ADMIN_AUTH_MODE=none` disables service-side dashboard authentication. Use it only when ingress, reverse proxy, VPN, Basic Auth, Authelia, oauth2-proxy, or another trusted upstream control already protects `/admin` and `/_admin`.

Do not expose `ADMIN_AUTH_MODE=none` directly to the public internet.

## Audit Log Persistence with SQLite

When `config.AUDIT_ENABLED=true` and `config.AUDIT_STORAGE_TYPE=sqlite`, the application stores audit events in SQLite at `config.AUDIT_SQLITE_PATH`. The chart mounts `/var/lib/turnstile-appcheck-gateway` as writable storage and defaults to PVC-backed persistence.

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set persistence.enabled=true \
  --set persistence.size=1Gi \
  --set config.AUDIT_ENABLED=true \
  --set config.AUDIT_SQLITE_PATH=/var/lib/turnstile-appcheck-gateway/audit.db \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"
```

For SQLite persistence, a single replica is recommended. The chart can render multiple replicas, but SQLite state and audit history are local to the mounted filesystem and are not a distributed datastore.

`AUDIT_RETENTION_DAYS=0` disables retention cleanup. `AUDIT_PRUNE_INTERVAL=0` disables periodic pruning.

The current audit schema writes to `audit_events` and `audit_metric_rollups`. Existing SQLite audit rows from the earlier schema are intentionally not migrated; if an incompatible legacy `audit_events` table is found, it is dropped and recreated with the new schema.

The audit log is designed not to store secret values, tokens, service account JSON, `Authorization` headers, cookies, or raw request bodies.

## Production Audit Storage

PostgreSQL is recommended for production and high-frequency `/verify` traffic:

Because PostgreSQL and MariaDB/MySQL DSNs usually contain credentials, set `secrets.AUDIT_DSN` instead of `config.AUDIT_DSN`. `config.AUDIT_DSN` remains available for non-sensitive SQLite DSN overrides and backward compatibility. When `existingSecret` is used, include an `AUDIT_DSN` key in that Secret.

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set config.AUDIT_STORAGE_TYPE=postgres \
  --set-string secrets.AUDIT_DSN='postgres://user:password@postgres:5432/turnstile_appcheck_gateway?sslmode=disable' \
  --set config.AUDIT_DB_MAX_OPEN_CONNS=20 \
  --set config.AUDIT_DB_MAX_IDLE_CONNS=10 \
  --set config.AUDIT_DB_CONN_MAX_LIFETIME=30m \
  --set config.AUDIT_DB_CONN_MAX_IDLE_TIME=5m
```

MariaDB/MySQL is also supported:

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set config.AUDIT_STORAGE_TYPE=mariadb \
  --set-string secrets.AUDIT_DSN='user:password@tcp(mariadb:3306)/turnstile_appcheck_gateway?parseTime=true&charset=utf8mb4&loc=UTC'
```

By default, `/verify` success audit rows are not stored one-by-one (`AUDIT_VERIFY_SUCCESS_SAMPLE_RATE=0.0`). They still update rollup metrics used by the admin dashboard. `/verify` failures, denials, missing/invalid token outcomes, and `rate_limit.denied` are persisted. `/exchange` final summaries are persisted by default, step events follow `AUDIT_PUBLIC_MODE`, and admin/audit events are always persisted.

Async audit writes use a bounded channel and batch INSERTs. Best-effort events can be dropped when the channel is full and are logged/counted. Critical events wait only up to `AUDIT_CRITICAL_ENQUEUE_TIMEOUT`. Set Pod `terminationGracePeriodSeconds` longer than `AUDIT_SHUTDOWN_FLUSH_TIMEOUT` so shutdown can drain pending audit batches.

## Ephemeral Audit Log Example

If you do not want a PVC, disable persistence. The chart will mount `emptyDir` at `/var/lib/turnstile-appcheck-gateway`.

```bash
helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set persistence.enabled=false \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456' \
  --set secrets.TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --set secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"
```

In this mode, audit history is lost when the Pod restarts or is rescheduled.

## External Secret Example

Use `existingSecret` when another controller or manual process manages sensitive values.

```bash
kubectl create namespace appcheck

kubectl create secret generic turnstile-appcheck-gateway-secret \
  --namespace appcheck \
  --from-literal=TURNSTILE_SECRET_KEY="$TURNSTILE_SECRET_KEY" \
  --from-literal=GOOGLE_SERVICE_ACCOUNT_JSON_BASE64="$GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"

helm upgrade --install turnstile-appcheck-gateway ./deploy/chart \
  --namespace appcheck \
  --create-namespace \
  --set existingSecret=turnstile-appcheck-gateway-secret \
  --set config.FIREBASE_PROJECT_ID=my-firebase-project \
  --set config.FIREBASE_APP_ID='1:1234567890:web:abcdef123456'
```

## Creating `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

Linux:

```bash
base64 -w 0 firebase-service-account.json
```

macOS:

```bash
base64 < firebase-service-account.json | tr -d '\n'
```

## Rate Limit Notes

`RATE_LIMIT_ENABLED`, `RATE_LIMIT_REQUESTS`, and `RATE_LIMIT_WINDOW` control an in-memory limiter on `/exchange` and `/verify`.

- Counters are per Pod.
- Multi-replica deployments increase total effective capacity.
- If you need a strict global limit, enforce it in ingress, API gateway, or WAF layers.

## HPA Notes

The chart supports `autoscaling/v2` HPA objects.

- HPA scaling changes effective in-memory rate-limit capacity because each Pod has independent counters.
- HPA does not make SQLite audit state distributed.

## Major Values

- `config.APPCHECK_SUBPATH`: public subpath for `/exchange`, `/verify`, `/admin`, and `/_admin`
- `config.HEALTH_PATH` and `config.READY_PATH`: root-level probe paths; do not prefix them with `APPCHECK_SUBPATH`
- `config.FIREBASE_PROJECT_ID`: required
- `config.FIREBASE_APP_ID` or `config.FIREBASE_APP_RESOURCE`: one is required
- `config.ADMIN_*`: admin dashboard and header-auth behavior
- `config.AUDIT_*`: SQLite audit logging behavior
- `config.RATE_LIMIT_*`: in-memory rate limiting behavior
- `secrets.TURNSTILE_SECRET_KEY`: required unless `existingSecret` is used
- `secrets.GOOGLE_SERVICE_ACCOUNT_JSON` or `secrets.GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`: one is required unless `existingSecret` is used
- `persistence.enabled`: switch between PVC-backed audit storage and ephemeral `emptyDir`
- `securityContext.readOnlyRootFilesystem`: defaults to `true`; the chart still mounts writable `/tmp` and `/var/lib/turnstile-appcheck-gateway`

## Listing Published Versions

```bash
oras repo tags ghcr.io/michibiki-io/charts/turnstile-appcheck-gateway
```
