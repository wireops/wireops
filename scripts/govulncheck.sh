#!/usr/bin/env bash
# Runs govulncheck and fails only on vulnerabilities not listed in
# .govulncheck-allowlist.txt. Usage: scripts/govulncheck.sh <package-patterns...>
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="$REPO_ROOT/.govulncheck-allowlist.txt"

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <package-patterns...>" >&2
  exit 2
fi

ALLOWED_IDS=""
if [ -f "$ALLOWLIST" ]; then
  ALLOWED_IDS=$(grep -vE '^\s*(#|$)' "$ALLOWLIST" | tr -d ' \t')
fi

TMP_JSON="$(mktemp)"
trap 'rm -f "$TMP_JSON"' EXIT

set +e
govulncheck -json "$@" > "$TMP_JSON"
set -e

# Only findings with a call trace longer than 1 frame are actually reachable
# (a 1-frame trace just means the module is imported, not that vulnerable
# code is called) -- matches what govulncheck's text output reports.
FOUND_IDS=$(jq -r 'select(.finding != null and (.finding.trace | length) > 1) | .finding.osv' "$TMP_JSON" | sort -u)

if [ -z "$FOUND_IDS" ]; then
  echo "govulncheck: no vulnerabilities found"
  exit 0
fi

UNEXPECTED=""
for id in $FOUND_IDS; do
  if ! grep -qxF "$id" <<< "$ALLOWED_IDS"; then
    UNEXPECTED="$UNEXPECTED $id"
  fi
done

echo "govulncheck found: $FOUND_IDS"
echo "allowlisted (see .govulncheck-allowlist.txt): $(echo "$ALLOWED_IDS" | tr '\n' ' ')"

if [ -n "$UNEXPECTED" ]; then
  echo "::error::Unallowed vulnerabilities found:$UNEXPECTED" >&2
  exit 1
fi

echo "all findings are allowlisted"
