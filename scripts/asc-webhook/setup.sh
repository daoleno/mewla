#!/usr/bin/env bash
# Deploy the App Store Connect webhook receiver and point App Store Connect at it, in one go.
#
#   CLOUDFLARE_API_TOKEN=...    # "Edit Cloudflare Workers" token for the account that holds mewla.ai
#                               # (unset: wrangler asks you to log in in a browser)
#   GITHUB_DISPATCH_TOKEN=...   # fine-grained: repository daoleno/mewla, Actions: read and write
#   scripts/asc-webhook/setup.sh --app-id 6821246662 --key-id <ASC key id> --key-path <AuthKey.p8>
#
# Re-running it rotates the webhook secret and updates the same Worker and webhook in place.
# Runbook: docs/ios-ci-release.md, "App Store Connect webhook".
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_ID="" KEY_ID="" KEY_PATH=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --app-id) APP_ID="$2"; shift 2 ;;
    --key-id) KEY_ID="$2"; shift 2 ;;
    --key-path) KEY_PATH="$2"; shift 2 ;;
    *) echo "error: unknown argument $1" >&2; exit 2 ;;
  esac
done
[[ "$APP_ID" =~ ^[1-9][0-9]*$ && -n "$KEY_ID" && -f "$KEY_PATH" ]] || {
  echo "usage: $0 --app-id <Mewla Preview app id> --key-id <ASC key id> --key-path <AuthKey.p8>" >&2
  exit 2
}
[[ -n "${GITHUB_DISPATCH_TOKEN:-}" ]] || { echo "error: set GITHUB_DISPATCH_TOKEN" >&2; exit 2; }

WRANGLER=(npx --yes wrangler@4)
command -v npx >/dev/null || WRANGLER=(bunx wrangler@4)

# The token must be able to run the workflow the receiver dispatches.
STATUS="$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $GITHUB_DISPATCH_TOKEN" -H "Accept: application/vnd.github+json" \
  https://api.github.com/repos/daoleno/mewla/actions/workflows/ios-testflight-review.yml)"
[[ "$STATUS" == 200 ]] || { echo "error: GITHUB_DISPATCH_TOKEN cannot read the workflow (HTTP $STATUS)" >&2; exit 1; }

cd "$HERE"
DEPLOY_LOG="$(mktemp)"
trap 'rm -f "$DEPLOY_LOG"' EXIT
"${WRANGLER[@]}" deploy | tee "$DEPLOY_LOG"
BASE="$(grep -oE 'https://mewla-asc-webhook\.[A-Za-z0-9-]+\.workers\.dev' "$DEPLOY_LOG" | head -1)"
[[ -n "$BASE" ]] || { echo "error: no workers.dev URL in the deploy output" >&2; exit 1; }

SECRET="$(openssl rand -hex 32)"
printf '%s' "$SECRET" | "${WRANGLER[@]}" secret put ASC_WEBHOOK_SECRET >/dev/null
printf '%s' "$GITHUB_DISPATCH_TOKEN" | "${WRANGLER[@]}" secret put GITHUB_TOKEN >/dev/null
echo "Worker secrets set (ASC_WEBHOOK_SECRET, GITHUB_TOKEN)"

ASC_WEBHOOK_SECRET="$SECRET" python3 "$HERE/register.py" \
  --app-id "$APP_ID" --key-id "$KEY_ID" --key-path "$KEY_PATH" \
  --url "$BASE/app-store-connect" --ping
