# k6 Audit Load Test Results - 2026-05-04

These results were produced with the local Docker Compose k6 audit storage
matrix. Each rate ran for `30s` against the mock e2e stack.

## Summary

- Async audit recording handled the configured targets cleanly in this run.
- `verify-missing` with async audit reached `2000 rps` for both PostgreSQL and
  MariaDB with no HTTP failures. PostgreSQL dropped 55 iterations out of a
  60,000 target; MariaDB dropped none.
- `exchange-valid` with async audit reached `500 rps` for both PostgreSQL and
  MariaDB with no HTTP failures and no dropped iterations.
- Sync audit recording was much more sensitive to database write latency.
  PostgreSQL sync audit collapsed at `1000 rps`: only 54.9% of target
  iterations completed, p95 latency was about 3.0s, and compose logs showed
  PostgreSQL connection exhaustion/timeouts.
- MariaDB sync audit performed better than PostgreSQL sync in this run, but
  showed rising tail latency at `1000 rps` with p95 around 154ms.

## Results

| Case | DB | Async | Mode | Rate | Iterations | Target | Achieved | Dropped | p95 ms | p99 ms | Max ms |
| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | PostgreSQL | false | verify-missing | 100 | 3,001 | 3,000 | 100.0% | 0 | 2.70 | 3.98 | 33.41 |
| 1 | PostgreSQL | false | verify-missing | 500 | 14,997 | 15,000 | 100.0% | 4 | 2.74 | 27.82 | 255.13 |
| 1 | PostgreSQL | false | verify-missing | 1000 | 16,466 | 30,000 | 54.9% | 13,534 | 3,013.43 | 6,369.03 | 18,334.54 |
| 2 | MariaDB | false | verify-missing | 100 | 3,001 | 3,000 | 100.0% | 0 | 2.94 | 6.10 | 41.58 |
| 2 | MariaDB | false | verify-missing | 500 | 14,930 | 15,000 | 99.5% | 71 | 7.73 | 70.13 | 408.56 |
| 2 | MariaDB | false | verify-missing | 1000 | 29,601 | 30,000 | 98.7% | 399 | 153.68 | 473.59 | 1,179.49 |
| 3 | PostgreSQL | true | verify-missing | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.33 | 0.90 | 27.48 |
| 3 | PostgreSQL | true | verify-missing | 1000 | 30,001 | 30,000 | 100.0% | 0 | 0.38 | 1.16 | 12.49 |
| 3 | PostgreSQL | true | verify-missing | 2000 | 59,946 | 60,000 | 99.9% | 55 | 0.74 | 2.53 | 150.34 |
| 4 | MariaDB | true | verify-missing | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.35 | 1.01 | 21.47 |
| 4 | MariaDB | true | verify-missing | 1000 | 30,001 | 30,000 | 100.0% | 0 | 0.37 | 1.00 | 19.39 |
| 4 | MariaDB | true | verify-missing | 2000 | 60,000 | 60,000 | 100.0% | 0 | 0.51 | 1.27 | 8.46 |
| 5 | PostgreSQL | true | exchange-valid | 100 | 3,001 | 3,000 | 100.0% | 0 | 0.79 | 1.55 | 5.17 |
| 5 | PostgreSQL | true | exchange-valid | 300 | 9,001 | 9,000 | 100.0% | 0 | 0.39 | 1.09 | 4.47 |
| 5 | PostgreSQL | true | exchange-valid | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.37 | 1.23 | 12.15 |
| 6 | MariaDB | true | exchange-valid | 100 | 3,001 | 3,000 | 100.0% | 0 | 0.62 | 0.88 | 3.41 |
| 6 | MariaDB | true | exchange-valid | 300 | 9,001 | 9,000 | 100.0% | 0 | 0.45 | 0.92 | 36.78 |
| 6 | MariaDB | true | exchange-valid | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.35 | 0.73 | 3.51 |

## Interpretation

The main behavioral difference is sync versus async audit writes.

With sync audit, each request waits for audit persistence. At higher rates,
database connection limits and write latency directly become API latency. In
this run, PostgreSQL sync audit at `1000 rps` exceeded the local database
capacity. The application logs included PostgreSQL `too many clients already`
and TCP read timeout errors while recording audit events. MariaDB sync audit
continued to complete nearly all requests at `1000 rps`, but tail latency was
already materially higher than the lower-rate runs.

With async audit, the request path only enqueues audit work. That keeps API
latency low for these rates, while the recorder batches database writes in the
background. This is the operating mode that matches high-throughput public
verification traffic.

## Caveats

- This is a local Docker Compose result, not a production benchmark.
- The duration was `30s` per rate, so this is a short stress pass rather than a
  long soak test.
- `verify-missing` intentionally treats HTTP `401` as a successful response in
  k6, because the endpoint is expected to reject missing tokens.
- `exchange-valid` uses mock upstreams from `dev/docker-compose.e2e.yml`; it
  measures gateway and audit path behavior, not real Cloudflare/Firebase
  upstream latency.
- The PostgreSQL sync failure is still useful: it shows that sync audit writes
  can exhaust the DB connection path under high request concurrency in this
  environment.

## Recommendation

For public gateway traffic, keep `AUDIT_ASYNC_ENABLED=true`. Treat
`AUDIT_ASYNC_ENABLED=false` as a diagnostic or low-throughput mode, especially
when audit storage is PostgreSQL/MariaDB and every public request is counted.

## Japanese

### 概要

この結果は、ローカル Docker Compose の k6 audit storage matrix で取得した
ものです。各 rate は mock e2e stack に対して `30s` 実行しています。

主な結論:

- async audit recording は、この実行で設定した target を概ね問題なく処理しました。
- `verify-missing` + async audit は PostgreSQL / MariaDB とも `2000 rps` まで到達しました。HTTP failure はありません。PostgreSQL は 60,000 target 中 55 iteration drop、MariaDB は drop なしでした。
- `exchange-valid` + async audit は PostgreSQL / MariaDB とも `500 rps` まで到達しました。HTTP failure と dropped iteration はありません。
- sync audit recording は DB write latency の影響を強く受けます。PostgreSQL sync audit の `1000 rps` は崩れており、target の 54.9% しか完了せず、p95 latency は約 3.0 秒でした。compose log には PostgreSQL の connection exhaustion / timeout が出ています。
- MariaDB sync audit はこの実行では PostgreSQL sync より耐えましたが、`1000 rps` では p95 latency が約 154ms まで上がっています。

### 結果

| Case | DB | Async | Mode | Rate | Iterations | Target | Achieved | Dropped | p95 ms | p99 ms | Max ms |
| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | PostgreSQL | false | verify-missing | 100 | 3,001 | 3,000 | 100.0% | 0 | 2.70 | 3.98 | 33.41 |
| 1 | PostgreSQL | false | verify-missing | 500 | 14,997 | 15,000 | 100.0% | 4 | 2.74 | 27.82 | 255.13 |
| 1 | PostgreSQL | false | verify-missing | 1000 | 16,466 | 30,000 | 54.9% | 13,534 | 3,013.43 | 6,369.03 | 18,334.54 |
| 2 | MariaDB | false | verify-missing | 100 | 3,001 | 3,000 | 100.0% | 0 | 2.94 | 6.10 | 41.58 |
| 2 | MariaDB | false | verify-missing | 500 | 14,930 | 15,000 | 99.5% | 71 | 7.73 | 70.13 | 408.56 |
| 2 | MariaDB | false | verify-missing | 1000 | 29,601 | 30,000 | 98.7% | 399 | 153.68 | 473.59 | 1,179.49 |
| 3 | PostgreSQL | true | verify-missing | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.33 | 0.90 | 27.48 |
| 3 | PostgreSQL | true | verify-missing | 1000 | 30,001 | 30,000 | 100.0% | 0 | 0.38 | 1.16 | 12.49 |
| 3 | PostgreSQL | true | verify-missing | 2000 | 59,946 | 60,000 | 99.9% | 55 | 0.74 | 2.53 | 150.34 |
| 4 | MariaDB | true | verify-missing | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.35 | 1.01 | 21.47 |
| 4 | MariaDB | true | verify-missing | 1000 | 30,001 | 30,000 | 100.0% | 0 | 0.37 | 1.00 | 19.39 |
| 4 | MariaDB | true | verify-missing | 2000 | 60,000 | 60,000 | 100.0% | 0 | 0.51 | 1.27 | 8.46 |
| 5 | PostgreSQL | true | exchange-valid | 100 | 3,001 | 3,000 | 100.0% | 0 | 0.79 | 1.55 | 5.17 |
| 5 | PostgreSQL | true | exchange-valid | 300 | 9,001 | 9,000 | 100.0% | 0 | 0.39 | 1.09 | 4.47 |
| 5 | PostgreSQL | true | exchange-valid | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.37 | 1.23 | 12.15 |
| 6 | MariaDB | true | exchange-valid | 100 | 3,001 | 3,000 | 100.0% | 0 | 0.62 | 0.88 | 3.41 |
| 6 | MariaDB | true | exchange-valid | 300 | 9,001 | 9,000 | 100.0% | 0 | 0.45 | 0.92 | 36.78 |
| 6 | MariaDB | true | exchange-valid | 500 | 15,001 | 15,000 | 100.0% | 0 | 0.35 | 0.73 | 3.51 |

### 読み取り

一番大きな差は sync audit と async audit の違いです。

sync audit では、各 request が audit persistence を待ちます。そのため高い
rate では、DB connection limit や write latency がそのまま API latency に
なります。この実行では PostgreSQL sync audit の `1000 rps` がローカル DB の
処理能力を超えました。application log には PostgreSQL の
`too many clients already` と TCP read timeout が出ています。

MariaDB sync audit は `1000 rps` でもほぼ全 request を完了できていますが、
tail latency は低 rate と比べて明確に悪化しています。

async audit では、request path は audit work を enqueue するだけです。そのため
今回の rate では API latency が低く保たれ、recorder が background で batch
write できます。public verification traffic のような高 throughput 用途では、
この動作が望ましいです。

### 注意点

- これはローカル Docker Compose の結果であり、production benchmark ではありません。
- 各 rate の duration は `30s` なので、長時間 soak test ではなく短時間 stress pass です。
- `verify-missing` は token なし request を意図的に送るため、HTTP `401` を k6 上の成功 response として扱っています。
- `exchange-valid` は `dev/docker-compose.e2e.yml` の mock upstream を使います。実 Cloudflare / Firebase の upstream latency は測っていません。
- PostgreSQL sync の失敗は、sync audit write が高 concurrency で DB connection path を枯渇させ得ることを示す有用な結果です。

### 推奨

public gateway traffic では `AUDIT_ASYNC_ENABLED=true` を維持してください。
`AUDIT_ASYNC_ENABLED=false` は diagnostic または low-throughput mode として扱うのが妥当です。特に audit storage が PostgreSQL / MariaDB で、public request をすべて audit 対象にする場合は async が前提になります。
