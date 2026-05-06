#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -n "${REAL_E2E_TURNSTILE_TOKEN:-}" ]]; then
  export KIND_E2E_MODE="${KIND_E2E_MODE:-full-real}"
else
  export KIND_E2E_MODE="${KIND_E2E_MODE:-half-real}"
fi

echo "e2e-real-release delegates to kind production-like e2e"
exec "${ROOT_DIR}/scripts/helm-e2e-kind.sh"
