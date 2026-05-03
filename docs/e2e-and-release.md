# E2E, CI, and Release Notes

## English

This document is for contributors and operators who need to run e2e tests, validate the Helm chart, or understand release automation. 

### PR mock e2e

Pull Request CI uses mock mode. It does not contact real Cloudflare Turnstile or Firebase App Check.

Commands:

```bash
make compose-e2e-mock
make compose-e2e-down
make e2e-kind-mock
```

`compose-e2e-mock` uses the Compose override in `dev/docker-compose.e2e.yml`.

`e2e-kind-mock` uses a repository-local kubeconfig under `.tmp/` and never targets `~/.kube/config`.

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

### Relevant files

- `.github/workflows/compose-e2e.yml`
- `.github/workflows/helm-e2e.yml`
- `.github/workflows/release.yml`
- `.github/workflows/release-e2e.yml`
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
make compose-e2e-down
make e2e-kind-mock
```

`compose-e2e-mock` は `dev/docker-compose.e2e.yml` を使います。

`e2e-kind-mock` は `.tmp/` 配下の repo-local kubeconfig を使い、`~/.kube/config` は使いません。

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

### 関連ファイル

- `.github/workflows/compose-e2e.yml`
- `.github/workflows/helm-e2e.yml`
- `.github/workflows/release.yml`
- `.github/workflows/release-e2e.yml`
- `scripts/e2e-smoke-mock.sh`
- `scripts/helm-e2e-kind.sh`
- `scripts/e2e-real-local.sh`
- `scripts/e2e-real-release.sh`
