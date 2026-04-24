#!/usr/bin/env bash
set -euo pipefail

TRAEFIK_PORT="${TRAEFIK_PORT:-8080}"
VERIFY_HEADER_NAME="${VERIFY_HEADER_NAME:-X-Firebase-AppCheck}"
BASE_URL="http://localhost:${TRAEFIK_PORT}"

red() { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }

printf "[1/2] ヘッダ無しで /backend を呼び出し (拒否されることを確認)\n"
status_no_header=$(curl -sS -o /tmp/dev_backend_no_header.txt -w "%{http_code}" "${BASE_URL}/backend")
printf "status: %s\n" "$status_no_header"
cat /tmp/dev_backend_no_header.txt
printf "\n"

if [[ "$status_no_header" =~ ^2 ]]; then
  red "想定外: ヘッダ無しで許可されました。forwardAuth設定を確認してください。"
  exit 1
fi
green "OK: ヘッダ無しは拒否されました。"

if [[ -z "${APPCHECK_TOKEN:-}" ]]; then
  printf "\nAPPCHECK_TOKEN が未設定のため成功ケースはスキップします。\n"
  printf "有効トークンを使う場合: APPCHECK_TOKEN='<token>' ./dev/check.sh\n"
  exit 0
fi

printf "\n[2/2] App Check ヘッダ付きで /backend を呼び出し (許可されることを確認)\n"
status_with_header=$(curl -sS -o /tmp/dev_backend_with_header.txt -w "%{http_code}" \
  -H "${VERIFY_HEADER_NAME}: ${APPCHECK_TOKEN}" \
  "${BASE_URL}/backend")
printf "status: %s\n" "$status_with_header"
cat /tmp/dev_backend_with_header.txt
printf "\n"

if [[ "$status_with_header" =~ ^2 ]]; then
  green "OK: App Check 検証を通過しました。"
else
  red "NG: App Check 検証に失敗しました。トークン・Firebase設定・/exchange実行結果を確認してください。"
  exit 1
fi
