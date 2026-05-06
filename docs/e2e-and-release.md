# E2E, CI, and Release Notes

## English

This document is for contributors and operators who need to run e2e tests, validate the Helm chart, or understand release automation. 

### PR mock e2e

Pull Request CI uses mock mode. It does not contact real Cloudflare Turnstile or Firebase App Check.

Commands:

```bash
make compose-e2e-mock
make compose-e2e-mock-pg
make compose-e2e-mock-mariadb
make compose-e2e-down
make e2e-kind-mock
```

`compose-e2e-mock` uses the Compose override in `dev/docker-compose.e2e.yml`.
PostgreSQL and MariaDB audit storage variants add `dev/docker-compose.pg.yml` or `dev/docker-compose.mariadb.yml` with Compose `-f`.

For k6 audit storage load tests, see [Load Testing](load-testing.md).

`e2e-kind-mock` uses a repository-local kubeconfig under `.tmp/` and never targets `~/.kube/config`.
It installs Traefik in the kind cluster, routes all smoke checks through Traefik, and verifies protected-route CORS behavior for same-origin requests, allowed cross-origin preflight, allowed cross-origin requests, denied origins, and spoofed `X-Forwarded-Method` bypass attempts.

Keep the kind cluster:

```bash
KEEP_CLUSTER=true make e2e-kind-mock
```

Cleanup:

```bash
make e2e-kind-clean
```

### Local real e2e

Local real e2e has two modes.

- `full-real`: real Turnstile + real Firebase
- `half-real`: dummy Turnstile + real Firebase

Direct full-real:

```bash
REAL_E2E_TURNSTILE_TOKEN='...' make e2e-real-local
```

Manual full-real preparation:

```bash
make e2e-real-local
```

This starts the local stack and prints a `localhost` frontend URL. Open that page, obtain a Turnstile token, then run the continuation snippet shown in the page.

Force the dummy fallback:

```bash
LOCAL_E2E_FORCE_HALF_REAL=true make e2e-real-local
```

Mode is printed explicitly in the logs.

- `turnstile mode: full-real (REAL_E2E_TURNSTILE_TOKEN)`
- `turnstile mode: half-real (dummy-turnstile-real-firebase)`

### Release e2e

The release workflow runs integration e2e before tag creation, image push, Helm chart push, and GitHub Release creation.

Release modes:

- `full-real`: only when `REAL_E2E_TURNSTILE_TOKEN` is supplied
- `half-real`: default path, using dummy Turnstile + real Firebase

Required GitHub Secrets:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_APP_ID`
- `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

Optional GitHub Secrets:

- `FIREBASE_APP_RESOURCE`
- `TURNSTILE_SECRET_KEY`
- `REAL_E2E_TURNSTILE_TOKEN`

The default release path is half-real, so Turnstile secrets are not required unless the optional full-real path is enabled.

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

- `.github/workflows/compose-e2e.yml`
- `.github/workflows/helm-e2e.yml`
- `.github/workflows/release.yml`
- `.github/workflows/release-e2e.yml`
- `scripts/act-release-e2e.sh`
- `scripts/e2e-smoke-mock.sh`
- `scripts/helm-e2e-kind.sh`
- `scripts/e2e-real-local.sh`
- `scripts/e2e-real-release.sh`

## 日本語

このドキュメントは、e2e test、Helm chart 検証、release automation を扱う contributor / operator 向けです。

### PR 用 mock e2e

Pull Request CI は mock mode を使います。real Cloudflare Turnstile / Firebase App Check には接続しません。

実行コマンド:

```bash
make compose-e2e-mock
make compose-e2e-mock-pg
make compose-e2e-mock-mariadb
make compose-e2e-down
make e2e-kind-mock
```

`compose-e2e-mock` は `dev/docker-compose.e2e.yml` を使います。
PostgreSQL / MariaDB の audit storage variant は `dev/docker-compose.pg.yml` または `dev/docker-compose.mariadb.yml` を Compose `-f` で追加します。

k6 による audit storage load test は [Load Testing](load-testing.md) を参照してください。

`e2e-kind-mock` は `.tmp/` 配下の repo-local kubeconfig を使い、`~/.kube/config` は使いません。
kind cluster 内に Traefik を install し、smoke check は Traefik 経由で実行します。protected route の CORS 挙動として same-origin request、allowed cross-origin preflight、allowed cross-origin request、denied origin、`X-Forwarded-Method` spoofing による bypass 試行を検証します。

kind cluster を残す:

```bash
KEEP_CLUSTER=true make e2e-kind-mock
```

cleanup:

```bash
make e2e-kind-clean
```

### local real e2e

local real e2e には 2 つの mode があります。

- `full-real`: real Turnstile + real Firebase
- `half-real`: dummy Turnstile + real Firebase

direct full-real:

```bash
REAL_E2E_TURNSTILE_TOKEN='...' make e2e-real-local
```

manual full-real 準備:

```bash
make e2e-real-local
```

この実行で local stack が起動し、`localhost` の frontend URL が表示されます。そのページで Turnstile token を取得し、画面に表示される continuation snippet を repo root で実行してください。

dummy fallback を明示する:

```bash
LOCAL_E2E_FORCE_HALF_REAL=true make e2e-real-local
```

mode は log に明示されます。

- `turnstile mode: full-real (REAL_E2E_TURNSTILE_TOKEN)`
- `turnstile mode: half-real (dummy-turnstile-real-firebase)`

### release e2e

release workflow は tag 作成、image push、Helm chart push、GitHub Release 作成の前に integration e2e を実行します。

release mode:

- `full-real`: `REAL_E2E_TURNSTILE_TOKEN` がある場合のみ
- `half-real`: 既定。dummy Turnstile + real Firebase

必須 GitHub Secrets:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_APP_ID`
- `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64`

任意 GitHub Secrets:

- `FIREBASE_APP_RESOURCE`
- `TURNSTILE_SECRET_KEY`
- `REAL_E2E_TURNSTILE_TOKEN`

既定 path は half-real なので、optional な full-real を使わない限り Turnstile secret は不要です。

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

- `.github/workflows/compose-e2e.yml`
- `.github/workflows/helm-e2e.yml`
- `.github/workflows/release.yml`
- `.github/workflows/release-e2e.yml`
- `scripts/act-release-e2e.sh`
- `scripts/e2e-smoke-mock.sh`
- `scripts/helm-e2e-kind.sh`
- `scripts/e2e-real-local.sh`
- `scripts/e2e-real-release.sh`
