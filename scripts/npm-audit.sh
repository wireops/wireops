#!/usr/bin/env bash
# Runs npm audit against the frontend and fails only on high/critical
# advisories not listed in .npm-audit-allowlist.txt.
# Usage: scripts/npm-audit.sh [npm-prefix-dir]   (default: frontend)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="$REPO_ROOT/.npm-audit-allowlist.txt"
PROJECT_DIR="$REPO_ROOT/${1:-frontend}"

if [ ! -f "$PROJECT_DIR/package.json" ]; then
  echo "npm-audit: no package.json in $PROJECT_DIR" >&2
  exit 2
fi

ALLOWED_IDS=""
if [ -f "$ALLOWLIST" ]; then
  # `|| true`: grep exits 1 when the allowlist holds nothing but comments,
  # which under `set -e` would otherwise kill the script before it can report
  # the advisories it found.
  ALLOWED_IDS=$(grep -vE '^\s*(#|$)' "$ALLOWLIST" | tr -d ' \t' || true)
fi

TMP_JSON="$(mktemp)"
trap 'rm -f "$TMP_JSON"' EXIT

# npm audit exits non-zero whenever it finds anything, so the exit code is not
# the signal we gate on -- the parsed advisory list below is.
set +e
(cd "$PROJECT_DIR" && npm audit --json) > "$TMP_JSON"
set -e

if ! jq -e . >/dev/null 2>&1 < "$TMP_JSON"; then
  echo "::error::npm audit produced no parseable JSON (network or registry failure?)" >&2
  exit 1
fi

# Each advisory is reported once per package in the chain that carries it, so
# collapse to the distinct set of GHSA IDs at high/critical severity.
FOUND_IDS=$(jq -r '
  (.vulnerabilities // {})
  | to_entries[]
  | .value.via[]?
  | select(type == "object")
  | select(.severity == "high" or .severity == "critical")
  | .url
' "$TMP_JSON" | sed 's|.*/||' | sort -u)

if [ -z "$FOUND_IDS" ]; then
  echo "npm audit: no high or critical advisories found"
  exit 0
fi

UNEXPECTED=""
for id in $FOUND_IDS; do
  if ! grep -qxF "$id" <<< "$ALLOWED_IDS"; then
    UNEXPECTED="$UNEXPECTED $id"
  fi
done

echo "npm audit found: $FOUND_IDS"
echo "allowlisted (see .npm-audit-allowlist.txt): $(echo "$ALLOWED_IDS" | tr '\n' ' ')"

if [ -n "$UNEXPECTED" ]; then
  echo "::error::Unallowed advisories found:$UNEXPECTED" >&2
  exit 1
fi

echo "all advisories are allowlisted"
