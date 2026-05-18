# E2E, CI, and Release Notes

## English

This document is for contributors and operators who need to run e2e tests, validate the Helm chart, or understand release automation. 

### PR Helm e2e

Pull Request CI runs Helm e2e on kind. The mock SQLite path does not contact real Cloudflare Turnstile or Firebase App Check. PostgreSQL and MariaDB variants run half-real mode with dummy Turnstile + real Firebase.

Commands:

```bash
make e2e-kind-mock
make e2e-kind-half-real-pg
make e2e-kind-half-real-mariadb
```

For k6 audit storage load tests, see [Load Testing](load-testing.md).

The kind e2e scripts use a repository-local kubeconfig under `.tmp/` and never target `~/.kube/config`.
They install Traefik in the kind cluster, route checks through Traefik, and verify protected-route CORS behavior for same-origin requests, allowed cross-origin preflight, allowed cross-origin requests, denied origins, and spoofed `X-Forwarded-Method` bypass attempts.

Keep the kind cluster:

```bash
KEEP_CLUSTER=true make e2e-kind-mock
```

The convenience target for browser inspection keeps the cluster and injects e2e-only admin identity headers through Traefik for `/appcheck/admin` and `/appcheck/_admin`:

```bash
make e2e-kind-keep
kubectl --kubeconfig .tmp/kind-turnstile-appcheck-gateway-e2e.kubeconfig \
  -n traefik-e2e port-forward --address 127.0.0.1 svc/traefik 18080:80
```

Then open `http://127.0.0.1:18080/appcheck/admin/`.
This target also passes dummy Docker build args, `BUILD_VERSION=0.1.0` and `BUILD_COMMIT=0123456789abcdef0123456789abcdef01234567`, so the admin application information dialog can show the release GitHub icon and git hash without needing a real release build.

Cleanup:

```bash
make e2e-kind-clean
```

### Development Compose smoke

Compose checks are local development smoke tests only. They are not production-like e2e and are not run as pull request CI.

```bash
make compose-e2e-mock
make compose-e2e-mock-pg
make compose-e2e-mock-mariadb
make compose-e2e-down
```

`compose-e2e-mock` uses the Compose override in `dev/docker-compose.e2e.yml`.
PostgreSQL and MariaDB audit storage variants add `dev/docker-compose.pg.yml` or `dev/docker-compose.mariadb.yml` with Compose `-f`.

### Production-like kind e2e

Production-like e2e is centralized on kind. It installs the Helm chart, installs Traefik, creates the protected backend route, and validates the forwardAuth + CORS path through Traefik.

Modes:

- `mock`: no external Cloudflare Turnstile or Firebase App Check calls
- `half-real`: dummy Turnstile + real Firebase
- `full-real`: real Turnstile + real Firebase

Commands:

```bash
make e2e-kind-mock
make e2e-kind-half-real
make e2e-kind-half-real-pg
make e2e-kind-half-real-mariadb
REAL_E2E_TURNSTILE_TOKEN='...' make e2e-kind-full-real
```

`e2e-kind-half-real` uses SQLite with `emptyDir`. The `-pg` and `-mariadb` targets create an ephemeral database Deployment and configure the chart with `AUDIT_STORAGE_TYPE` and `AUDIT_DSN`.

If you need to obtain a real Turnstile token in a browser first, prepare the local kind frontend:

```bash
make e2e-kind-full-real-prepare
```

Open the printed URL, complete Turnstile, then run the snippet shown in the page from the repository root. The snippet uses a different local port, so it can run while the prepare process is still active.

For `half-real` and `full-real`, the script reads required Firebase values from the environment first, then `dev/.env`, then `.env`.

Required values:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_APP_ID` or `FIREBASE_APP_RESOURCE`
- `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

`full-real` also requires:

- `TURNSTILE_SECRET_KEY`
- `REAL_E2E_TURNSTILE_TOKEN`

### Release e2e

The release workflow runs kind production-like integration e2e before tag creation, image push, Helm chart push, and GitHub Release creation.

Merge-triggered release always uses `half-real`: dummy Turnstile + real Firebase.
Manual `release-e2e` workflow runs use the same wrapper, and switch to `full-real` only when the `REAL_E2E_TURNSTILE_TOKEN` secret is supplied.

Required GitHub Secrets:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_APP_ID`
- `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

Optional GitHub Secrets:

- `FIREBASE_APP_RESOURCE`
- `TURNSTILE_SECRET_KEY`
- `REAL_E2E_TURNSTILE_TOKEN`

The merge-triggered release path does not require Turnstile secrets.

Release e2e routes a protected backend through Traefik and validates same-origin 2xx/4xx, allowed cross-origin 2xx/4xx, allowed preflight CORS headers, denied-origin preflight behavior, and spoofed `X-Forwarded-Method` bypass prevention.

### Manual branch validation

You can also run the same release-oriented integration e2e manually on any branch, tag, or SHA through GitHub Actions.

Workflow:

- `Actions` -> `release-e2e`
- optionally set `ref`
- run the workflow

This workflow validates the selected code but does not create tags, push images, push Helm charts, or create a GitHub Release.

### Local workflow rehearsal with act

You can rehearse the `release-e2e` workflow locally with `act` before pushing.

Install `act`:

```bash
brew install act
```

Alternative official installer:

```bash
curl -s https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash
```

Commands:

```bash
make act-release-e2e
REF=main make act-release-e2e
ENV_FILE=.env make act-release-e2e
```

Notes:

- Installing the host `act` binary is recommended for repeated use because it is faster than the Dockerized fallback
- The helper looks for `./bin/act` first, then `act` on `PATH`, then Dockerized fallback
- The helper resolves `GITHUB_TOKEN` from `GITHUB_TOKEN`, then `GH_TOKEN`, then `gh auth token`
- On Linux, the helper passes the host Docker socket group ID to the `act` runner container
- The helper script defaults to `dev/.env`, then `.env`
- The helper passes the same Firebase secrets that the workflow expects
- `REAL_E2E_TURNSTILE_TOKEN` is optional here too
- If the local `act` binary is not installed, the helper falls back to a Dockerized `act` runner
- This is a local rehearsal, not a perfect substitute for a GitHub-hosted runner
- Docker must be installed locally

### Relevant files

- `.github/workflows/helm-e2e.yml`
- `.github/workflows/release.yml`
- `.github/workflows/release-e2e.yml`
- `scripts/act-release-e2e.sh`
- `scripts/e2e-appcheck-real.sh`
- `scripts/e2e-smoke-mock.sh`
- `scripts/e2e-traefik-cors.sh`
- `scripts/helm-e2e-kind.sh`
- `scripts/e2e-real-release.sh`

## 日本語

このドキュメントは、e2e test、Helm chart 検証、release automation を扱う contributor / operator 向けです。

### PR 用 Helm e2e

Pull Request CI は kind 上の Helm e2e を実行します。mock SQLite path は real Cloudflare Turnstile / Firebase App Check には接続しません。PostgreSQL / MariaDB variant は dummy Turnstile + real Firebase の half-real mode で実行します。

実行コマンド:

```bash
make e2e-kind-mock
make e2e-kind-half-real-pg
make e2e-kind-half-real-mariadb
```

k6 による audit storage load test は [Load Testing](load-testing.md) を参照してください。

kind e2e script は `.tmp/` 配下の repo-local kubeconfig を使い、`~/.kube/config` は使いません。
kind cluster 内に Traefik を install し、check は Traefik 経由で実行します。protected route の CORS 挙動として same-origin request、allowed cross-origin preflight、allowed cross-origin request、denied origin、`X-Forwarded-Method` spoofing による bypass 試行を検証します。

kind cluster を残す:

```bash
KEEP_CLUSTER=true make e2e-kind-mock
```

browser で admin 画面を確認する場合は、便利 target が cluster を残し、`/appcheck/admin` と `/appcheck/_admin` に対して e2e 専用の admin identity header を Traefik で注入します。

```bash
make e2e-kind-keep
kubectl --kubeconfig .tmp/kind-turnstile-appcheck-gateway-e2e.kubeconfig \
  -n traefik-e2e port-forward --address 127.0.0.1 svc/traefik 18080:80
```

その後 `http://127.0.0.1:18080/appcheck/admin/` を開きます。
この target は dummy Docker build args として `BUILD_VERSION=0.1.0`、`BUILD_COMMIT=0123456789abcdef0123456789abcdef01234567` も渡すため、real release build なしで admin の application information dialog に release GitHub icon と git hash を表示できます。

cleanup:

```bash
make e2e-kind-clean
```

### 開発用 Compose smoke

Compose check は local development smoke test 専用です。production-like e2e ではなく、pull request CI としては実行しません。

```bash
make compose-e2e-mock
make compose-e2e-mock-pg
make compose-e2e-mock-mariadb
make compose-e2e-down
```

`compose-e2e-mock` は `dev/docker-compose.e2e.yml` を使います。
PostgreSQL / MariaDB の audit storage variant は `dev/docker-compose.pg.yml` または `dev/docker-compose.mariadb.yml` を Compose `-f` で追加します。

### production-like kind e2e

production-like e2e は kind に一本化しています。Helm chart と Traefik を kind cluster に install し、protected backend route を作成して、Traefik 経由の forwardAuth + CORS path を検証します。

mode:

- `mock`: real Cloudflare Turnstile / Firebase App Check には接続しません
- `half-real`: dummy Turnstile + real Firebase
- `full-real`: real Turnstile + real Firebase

実行コマンド:

```bash
make e2e-kind-mock
make e2e-kind-half-real
make e2e-kind-half-real-pg
make e2e-kind-half-real-mariadb
REAL_E2E_TURNSTILE_TOKEN='...' make e2e-kind-full-real
```

`e2e-kind-half-real` は SQLite + `emptyDir` を使います。`-pg` / `-mariadb` target は ephemeral な database Deployment を作成し、chart に `AUDIT_STORAGE_TYPE` と `AUDIT_DSN` を設定します。

browser で real Turnstile token を先に取得する場合は、local kind frontend を準備します。

```bash
make e2e-kind-full-real-prepare
```

表示された URL を開き、Turnstile を完了した後、画面に表示される snippet を repository root で実行します。snippet は別の local port を使うため、prepare process を起動したまま次の full-real e2e を実行できます。

`half-real` / `full-real` では、必要な Firebase 値を環境変数、`dev/.env`、`.env` の順に読みます。

必須値:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_APP_ID` または `FIREBASE_APP_RESOURCE`
- `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

`full-real` では追加で以下が必要です。

- `TURNSTILE_SECRET_KEY`
- `REAL_E2E_TURNSTILE_TOKEN`

### release e2e

release workflow は tag 作成、image push、Helm chart push、GitHub Release 作成の前に kind production-like integration e2e を実行します。

merge trigger の release は常に `half-real`: dummy Turnstile + real Firebase を使います。
手動の `release-e2e` workflow は同じ wrapper を使い、`REAL_E2E_TURNSTILE_TOKEN` secret が渡された場合のみ `full-real` に切り替わります。

必須 GitHub Secrets:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_APP_ID`
- `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

任意 GitHub Secrets:

- `FIREBASE_APP_RESOURCE`
- `TURNSTILE_SECRET_KEY`
- `REAL_E2E_TURNSTILE_TOKEN`

merge trigger の release path では Turnstile secret は不要です。

release e2e は Traefik 経由の protected backend を検証します。same-origin の 2xx/4xx、allowed cross-origin の 2xx/4xx、allowed preflight の CORS header、denied origin の preflight 挙動、`X-Forwarded-Method` spoofing による bypass 防止を確認します。

### 任意 branch の手動検証

GitHub Actions から、同じ release 向け integration e2e を任意 branch / tag / SHA に対して手動実行できます。

Workflow:

- `Actions` -> `release-e2e`
- 必要なら `ref` を指定
- 実行

この workflow は選択した code を検証しますが、tag 作成、image push、Helm chart push、GitHub Release 作成は行いません。

### act による local workflow rehearsal

push 前に `release-e2e` workflow を local で rehearse したい場合は `act` を使えます。

`act` の install:

```bash
brew install act
```

代替の公式 installer:

```bash
curl -s https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash
```

実行コマンド:

```bash
make act-release-e2e
REF=main make act-release-e2e
ENV_FILE=.env make act-release-e2e
```

注意:

- 繰り返し使うなら、Docker fallback より host の `act` binary を install した方が速いです
- helper はまず `./bin/act`、次に `PATH` 上の `act`、最後に Dockerized fallback を探します
- helper は `GITHUB_TOKEN`、次に `GH_TOKEN`、最後に `gh auth token` の順で GitHub token を解決します
- Linux では helper が host の Docker socket の group ID を `act` runner container に渡します
- helper script は既定で `dev/.env`、無ければ `.env` を使います
- workflow が期待する Firebase secrets をそのまま `act` に渡します
- `REAL_E2E_TURNSTILE_TOKEN` はここでも optional です
- local に `act` binary が無い場合は、helper が Dockerized `act` fallback を使います
- これは local rehearsal であり、GitHub hosted runner の完全な代替ではありません
- local に Docker が必要です

### 関連ファイル

- `.github/workflows/helm-e2e.yml`
- `.github/workflows/release.yml`
- `.github/workflows/release-e2e.yml`
- `scripts/act-release-e2e.sh`
- `scripts/e2e-appcheck-real.sh`
- `scripts/e2e-smoke-mock.sh`
- `scripts/e2e-traefik-cors.sh`
- `scripts/helm-e2e-kind.sh`
- `scripts/e2e-real-release.sh`
