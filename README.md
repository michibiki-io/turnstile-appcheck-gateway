# Turnstile + Firebase App Check Gateway (Go/Gin)
Cloudflare Turnstile と Firebase App Check Custom Provider を連携するための Go マイクロサービスです。

このサービスは次の2用途を分離して提供します。
- フロントエンド向けトークン取得: `POST /appcheck/api/v1/exchange`
- Traefik forwardAuth 向け検証: `ANY /appcheck/api/v1/verify`

## アーキテクチャ概要
- `POST {SUBPATH}/api/v1/exchange`
1. フロントエンドから Turnstile トークンを受信
2. Cloudflare Siteverify でサーバーサイド検証
3. Firebase App Check custom token(JWT)を生成
4. Firebase App Check REST `exchangeCustomToken` で App Check token に交換
5. `CustomProvider.getToken()` がそのまま使える `token + expireTimeMillis` を返却

- `ANY {SUBPATH}/api/v1/verify`
1. `X-Firebase-AppCheck` (設定変更可) からトークン取得
2. Firebase Admin Go SDK でトークン検証
3. 成功時2xx(既定204)、失敗時非2xx(既定401)
4. Traefik forwardAuthは2xxを許可、非2xxを拒否として扱う

## エンドポイント
- `POST {SUBPATH}/api/v1/exchange` (既定: `/appcheck/api/v1/exchange`)
- `ANY  {SUBPATH}/api/v1/verify`   (既定: `/appcheck/api/v1/verify`)
- `GET /healthz`
- `GET /readyz`

## ディレクトリ構成
```text
cmd/turnstile-appcheck-gateway/main.go
internal/config
internal/httpserver
internal/handlers
internal/turnstile
internal/firebaseappcheck
internal/auth
internal/middleware
internal/model
internal/logging
internal/errors
internal/health
```

## 環境変数
| 変数 | 必須 | 既定値 | 説明 |
|---|---|---:|---|
| `APPCHECK_SUBPATH` | 任意 | `/appcheck` | APIサブパス |
| `HTTP_ADDR` | 任意 | `:8080` | リッスンアドレス |
| `TURNSTILE_SECRET_KEY` | 必須 | - | Turnstileシークレット |
| `TURNSTILE_SITEVERIFY_URL` | 任意 | `https://challenges.cloudflare.com/turnstile/v0/siteverify` | Siteverify URL |
| `FIREBASE_PROJECT_ID` | 必須 | - | Firebase Project ID |
| `FIREBASE_APP_ID` | 条件付き必須 | - | Firebase App ID (`FIREBASE_APP_RESOURCE`未指定時) |
| `FIREBASE_APP_RESOURCE` | 任意 | - | `projects/{project}/apps/{appId}` を直接指定 |
| `GOOGLE_SERVICE_ACCOUNT_JSON` | 条件付き必須 | - | 生JSON(複数行/`\n`対応) |
| `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64` | 条件付き必須 | - | base64 JSON。JSONより優先 |
| `LOG_LEVEL` | 任意 | `info` | `debug/info/warn/error` |
| `REQUEST_TIMEOUT` | 任意 | `10s` | 外部呼び出しタイムアウト |
| `APPCHECK_TOKEN_TTL` | 任意 | `30m` | App Check token のTTL。`30m`〜`168h` |
| `ALLOWED_EXCHANGE_ORIGINS` | 任意 | - | `/exchange` の許可Origin一覧(カンマ区切り) |
| `TRUST_PROXY_HEADERS` | 任意 | `false` | `X-Forwarded-For` 等をremoteipに使うか |
| `VERIFY_HEADER_NAME` | 任意 | `X-Firebase-AppCheck` | `/verify` の検証対象ヘッダ名 |
| `VERIFY_SUCCESS_STATUS` | 任意 | `204` | `/verify` 成功時HTTPコード |
| `VERIFY_FAILURE_STATUS` | 任意 | `401` | `/verify` 失敗時HTTPコード |
| `HEALTH_PATH` | 任意 | `/healthz` | ヘルスチェックパス |
| `READY_PATH` | 任意 | `/readyz` | レディネスパス |

## `.env` 例
`.env.example` を参照してください。

## `/exchange` のcurl例
```bash
curl -i -X POST 'http://localhost:8080/appcheck/api/v1/exchange' \
  -H 'Content-Type: application/json' \
  -d '{
    "turnstileToken": "token-from-client",
    "limitedUse": false
  }'
```

成功レスポンス例:
```json
{
  "token": "eyJhbGciOiJSUzI1NiIs...",
  "expireTimeMillis": 1735689600000
}
```

## Traefik forwardAuth 設定例
`/verify` を forwardAuth に接続する例です。

```yaml
http:
  middlewares:
    appcheck-forward-auth:
      forwardAuth:
        address: "http://turnstile-appcheck-gateway:8080/appcheck/api/v1/verify"
        trustForwardHeader: true
        authResponseHeaders:
          - X-AppCheck-Verified
          - X-AppCheck-AppID
```

適用例:
```yaml
http:
  routers:
    api:
      rule: "Host(`api.example.com`)"
      service: api-svc
      middlewares:
        - appcheck-forward-auth
```

- `forwardAuth` は **2xxを許可**、**非2xxを拒否** として扱います。

## Firebase Web CustomProvider 利用例
`/exchange` はフロントエンドが呼び出します。

Turnstile のサイトキーは公開情報なので、**フロント側の環境変数**として埋め込みます（シークレットキーは絶対に埋め込まない）。

```bash
# 例: Vite
VITE_TURNSTILE_SITE_KEY=0x4AAAA...
```

```javascript
import { initializeAppCheck, CustomProvider } from "firebase/app-check";

const TURNSTILE_SITE_KEY = import.meta.env.VITE_TURNSTILE_SITE_KEY;
let widgetId;

function ensureTurnstileWidget() {
  if (widgetId !== undefined) return widgetId;
  widgetId = turnstile.render("#turnstile-widget", {
    sitekey: TURNSTILE_SITE_KEY,
  });
  return widgetId;
}

const provider = new CustomProvider({
  getToken: async () => {
    // Turnstileウィジェットからトークン取得
    const id = ensureTurnstileWidget();
    const turnstileToken = turnstile.getResponse(id);
    if (!turnstileToken) {
      throw new Error("turnstile token is empty");
    }

    const res = await fetch("/appcheck/api/v1/exchange", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        turnstileToken,
        limitedUse: false,
      }),
    });

    if (!res.ok) {
      throw new Error("failed to exchange app check token");
    }

    const data = await res.json();
    return {
      token: data.token,
      expireTimeMillis: data.expireTimeMillis,
    };
  },
});

initializeAppCheck(firebaseApp, {
  provider,
  isTokenAutoRefreshEnabled: true,
});
```

```html
<!-- index.html -->
<script src="https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit" async defer></script>
<div id="turnstile-widget"></div>
```

## ローカル実行
### 1. Dockerで実行
```bash
docker build -t turnstile-appcheck-gateway:local .
docker run --rm -p 8080:8080 --env-file .env turnstile-appcheck-gateway:local
```

multiarch イメージを作る場合の例 (`linux/amd64,linux/arm64`):
## Build

```bash
docker buildx build \
  --builder multiarch \
  --pull \
  -t michibiki/turnstile-appcheck-gateway:0.1.0 \
  --platform=linux/amd64,linux/arm64 \
  --provenance=mode=max \
  --sbom=true \
  --push \
  ./
```

### 2. Go直接実行
```bash
go run ./cmd/turnstile-appcheck-gateway
```

## テスト
```bash
go test ./...
```

ローカルにGoがない場合の例:
```bash
docker run --rm -v "$PWD":/src -w /src golang:1.26.2 sh -lc '/usr/local/go/bin/go test ./...'
```

## 開発用 Compose
`dev/` に Traefik + frontend + backend + turnstile-appcheck-gateway の検証環境を用意しています。

```bash
cp dev/.env.example dev/.env
docker compose --env-file dev/.env -f dev/docker-compose.yml up --build -d
```

- frontend: `http://localhost:8080/`
- protected backend: `http://localhost:8080/backend`
- Traefik dashboard: `http://localhost:8088/`

ポート衝突時は `dev/.env` の `TRAEFIK_PORT` / `TRAEFIK_DASHBOARD_PORT` を変更してください。

停止:
```bash
docker compose --env-file dev/.env -f dev/docker-compose.yml down -v
```

## セキュリティ実装メモ
- シークレットをログ出力しない
- `/exchange` は strict JSON (`DisallowUnknownFields`) を使用
- Turnstileのサーバーサイド検証を必須化
- 外部HTTP呼び出しにタイムアウト
- graceful shutdown 実装
- `CGO_ENABLED=0` の静的リンクバイナリ
- final image は `scratch` + CA bundle のみ
- non-rootコンテナ(`UID/GID 65532`)
- `APPCHECK_SUBPATH` を正規化してスラッシュ揺れを吸収

## 参照
- Firebase App Check custom provider (web)  
  https://firebase.google.com/docs/app-check/web/custom-provider
- Firebase App Check custom backend verification  
  https://firebase.google.com/docs/app-check/custom-resource-backend
- Firebase App Check REST exchangeCustomToken  
  https://firebase.google.com/docs/reference/appcheck/rest/v1/projects.apps/exchangeCustomToken
- Cloudflare Turnstile server-side validation  
  https://developers.cloudflare.com/turnstile/get-started/server-side-validation/
- Cloudflare reference implementations  
  https://github.com/cloudflare/turnstile-firebase-app-check-provider  
  https://github.com/cloudflare/turnstile-firebase-app-check
