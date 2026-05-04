# Load Testing

## k6 audit storage matrix

The audit load runner starts the mock compose stack with PostgreSQL or MariaDB,
switches `AUDIT_ASYNC_ENABLED`, and runs k6 from inside the compose network.
Results are written under `.tmp/k6-audit-load/<timestamp>/`.

One captured result set is summarized in
[k6 Audit Load Test Results - 2026-05-04](load-test-results-20260504.md).

```bash
make audit-load-k6
```

The default k6 duration is `30s` per rate. Override it when you want a longer
run:

```bash
K6_DURATION=1m make audit-load-k6
```

The matrix is:

| Case | DB | `AUDIT_ASYNC_ENABLED` | k6 mode | Rates |
| --- | --- | --- | --- | --- |
| 1 | PostgreSQL | `false` | `verify-missing` | `100`, `500`, `1000` |
| 2 | MariaDB | `false` | `verify-missing` | `100`, `500`, `1000` |
| 3 | PostgreSQL | `true` | `verify-missing` | `500`, `1000`, `2000` |
| 4 | MariaDB | `true` | `verify-missing` | `500`, `1000`, `2000` |
| 5 | PostgreSQL | `true` | `exchange-valid` | `100`, `300`, `500` |
| 6 | MariaDB | `true` | `exchange-valid` | `100`, `300`, `500` |

Each k6 run writes:

- `summary.json`: raw k6 summary export
- `k6.log`: stdout/stderr from k6
- `summary.csv`: one row per case/rate with iterations, check rate, failed
  request rate, and latency `avg`, `p90`, `p95`, `p99`, and `max`
- `compose.log`: application and database logs for that case

Useful overrides:

```bash
TRAEFIK_PORT=39080 TRAEFIK_DASHBOARD_PORT=39088 make audit-load-k6
RESULT_ROOT=.tmp/my-load-run K6_DURATION=2m make audit-load-k6
PRE_ALLOCATED_VUS=500 MAX_VUS=2000 make audit-load-k6
KEEP_LOAD_STACK=true make audit-load-k6
LOAD_CASES=1,3 K6_DURATION=10s make audit-load-k6
LOAD_CASES=1 LOAD_RATES=100 K6_DURATION=5s make audit-load-k6
```

`verify-missing` expects HTTP `401`, so the k6 script marks that status as
successful for `http_req_failed`. `exchange-valid` expects HTTP `200` and uses
the mock Turnstile/App Check tokens from `dev/docker-compose.e2e.yml`.
