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
TMP_ERR="$(mktemp)"
trap 'rm -f "$TMP_JSON" "$TMP_ERR"' EXIT

# In -json mode govulncheck exits 0 whether or not it found anything -- the
# JSON output itself carries the result -- so a non-zero exit here is a real
# execution failure (crash, unreachable vuln database, bad arguments), never a
# clean run. It has to fail the script: a crash writes no findings, and the
# filter below would otherwise report "no vulnerabilities found" and exit 0,
# turning the whole gate into a silent no-op.
set +e
govulncheck -json "$@" > "$TMP_JSON" 2> "$TMP_ERR"
GOVULNCHECK_EXIT=$?
set -e

if [ "$GOVULNCHECK_EXIT" -ne 0 ]; then
  echo "::error::govulncheck failed to run (exit $GOVULNCHECK_EXIT)" >&2
  tail -n 20 "$TMP_ERR" >&2
  exit 1
fi

if ! jq -e . >/dev/null 2>&1 < "$TMP_JSON"; then
  echo "::error::govulncheck produced no parseable JSON (crashed or truncated output?)" >&2
  tail -n 20 "$TMP_ERR" >&2
  exit 1
fi

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
