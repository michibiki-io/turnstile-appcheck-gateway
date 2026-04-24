# dev 環境 (Traefik + frontend + backend + turnstile-appcheck-gateway)
このディレクトリは、以下の動線をローカルで確認するための開発環境です。

- frontend -> `POST /appcheck/api/v1/exchange`
- Traefik forwardAuth -> `ANY /appcheck/api/v1/verify`
- protected backend -> `/backend`

## 構成
- `traefik`: 入口 (`http://localhost:8080`)
- `turnstile-appcheck-gateway`: このリポジトリのサービス本体
- `frontend`: 簡易UI (`/`)
- `backend`: `traefik/whoami` (forwardAuthで保護)

## 事前準備
1. `dev/.env.example` を `dev/.env` にコピー
2. `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET_KEY`, `FIREBASE_*`, `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64` を設定

```bash
cp dev/.env.example dev/.env
```

## 起動
```bash
docker compose --env-file dev/.env -f dev/docker-compose.yml up --build -d
```

確認:
- frontend: `http://localhost:8080/`
- Traefik dashboard: `http://localhost:8088/`
- `TRAEFIK_PORT` を変えた場合はそのポートに読み替えてください（例: `18080`）。

## 動作確認
### A. 失敗ケース（ヘッダなし）
`/backend` は非2xxになるはずです。

```bash
curl -i http://localhost:8080/backend
```

### B. 成功ケース（有効 App Check token）
1. frontend UIを開く（`TURNSTILE_SITE_KEY` が `dev/.env` から自動注入されます）
2. `Turnstile ウィジェット描画` を押して認証を通す
3. `Exchange 実行` を押す（`turnstileToken` は自動入力されます）
4. `App Check token` が取得できたら `Protected backend 呼び出し` を押す
5. `/backend` が2xxなら forwardAuth 検証通過

補助スクリプト:
```bash
./dev/check.sh
# 成功ケースも確認する場合
APPCHECK_TOKEN='<valid-appcheck-token>' ./dev/check.sh
```

## 停止
```bash
docker compose --env-file dev/.env -f dev/docker-compose.yml down -v
```

## 注意
- 有効な `FIREBASE_*` とサービスアカウントがないと `/exchange` は成功しません。
- Turnstile token は必ず server-side Siteverify で検証されます。
- `APPCHECK_TOKEN_TTL` の既定値は `30m` です（範囲: `30m`〜`168h`）。
- `VERIFY_SUCCESS_STATUS` は2xx、`VERIFY_FAILURE_STATUS` は非2xxである必要があります。
- サイトキーはURLクエリでも指定できます: `http://localhost:8080/?sitekey=<YOUR_SITE_KEY>`
- `TURNSTILE_SITE_KEY` 未設定時は、UI上で手入力してください。
- `HTTP_ADDR` は dev compose 側で内部 `:8080` に固定しています（Traefik 連携のため）。

## トラブルシュート
- `/appcheck/api/v1/exchange` で `nginx` の `404` が返る場合:
  - 多くは `turnstile-appcheck-gateway` が起動失敗しており、Traefik が frontend へフォールバックしています。
  - 次を確認してください:

```bash
docker compose --env-file dev/.env -f dev/docker-compose.yml ps
docker compose --env-file dev/.env -f dev/docker-compose.yml logs --tail=200 turnstile-appcheck-gateway traefik
```

- よくある原因:
  - `GOOGLE_SERVICE_ACCOUNT_JSON_BASE64` がダミー値で、秘密鍵PEMのパースに失敗
  - `FIREBASE_PROJECT_ID` / `FIREBASE_APP_ID` の不整合
