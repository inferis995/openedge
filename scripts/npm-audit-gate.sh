#!/usr/bin/env bash
#
# Fail the build on frontend dependency advisories.
#
# Split by where the code actually runs, because the two halves carry very
# different risk and lumping them together is what makes audit gates get muted:
#
#   production deps  — shipped to the browser, reachable by anyone who can load
#                      the UI. ANY advisory fails, at any severity. No allowlist.
#
#   dev deps         — build and test tooling. Never leaves CI or a developer's
#                      laptop, so a dev-server path traversal is not a customer
#                      exposure. Still fails at high/critical, because that is
#                      also where build-time supply-chain compromise lives, but
#                      an entry can be accepted here with a written reason.
#
# The allowlist is deliberately empty. It exists so that the day an unfixable
# dev advisory appears, the answer is a documented exception rather than someone
# deleting this job from the workflow.
set -euo pipefail

cd "$(dirname "$0")/../services/web-ui"

# ── Accepted dev-only advisories, with justification ─────────────────────────
# Format: "GHSA-xxxx  # why this is tolerable, and what would change it"
ACCEPTED_DEV=()

fail=0

# npm audit exits non-zero BOTH when it finds advisories and when it fails to
# run at all. Swallowing that with `|| true` would turn a broken registry
# connection into a silent pass — the one outcome a security gate must never
# have. So: run it, keep the output, and only accept the exit code if what came
# back is really an audit report.
run_audit() {
  local out status
  set +e
  out=$("$@" --json 2>/tmp/npm-audit.err)
  status=$?
  set -e
  if ! echo "$out" | jq -e '.metadata.vulnerabilities' >/dev/null 2>&1; then
    echo "npm audit did not return a report (exit $status):" >&2
    head -20 /tmp/npm-audit.err >&2
    exit 2
  fi
  echo "$out"
}

echo "── Production dependencies ─────────────────────────────────────────"
# --omit=dev walks only what ends up in the bundle.
prod_json=$(run_audit npm audit --omit=dev)
prod_total=$(echo "$prod_json" | jq -r '.metadata.vulnerabilities.total // 0')

if [ "$prod_total" -eq 0 ]; then
  echo "  clean."
else
  echo "$prod_json" | jq -r '
    .vulnerabilities
    | to_entries[]
    | "  \(.value.severity | ascii_upcase)\t\(.key)\t\(
        [.value.via[] | if type == "object" then .title else empty end] | first // "transitive"
      )"'
  echo
  echo "  $prod_total advisory in code shipped to the browser." >&2
  echo "  These have no allowlist: upgrade, replace, or remove the package." >&2
  fail=1
fi

echo
echo "── Development dependencies ────────────────────────────────────────"
all_json=$(run_audit npm audit)
# Everything the full audit reports that the production-only audit did not.
# `via` mixes shapes: an object for a direct advisory, a bare package name
# string when the path runs through another dependency. Select the objects
# rather than indexing blindly, or jq aborts the whole gate on the first
# transitive entry.
prod_names=$(echo "$prod_json" | jq -r '.vulnerabilities | keys[]?' | sort -u)
dev_rows=$(echo "$all_json" | jq -r --arg prod "$prod_names" '
  ($prod | split("\n") | map(select(length > 0))) as $p
  | .vulnerabilities
  | to_entries[]
  | select(.key as $k | ($p | index($k)) | not)
  | "\(.value.severity)\t\(.key)\t\(
      [.value.via[] | if type == "object" then .title else empty end] | first // "transitive"
    )"')

if [ -z "$dev_rows" ]; then
  echo "  clean."
else
  while IFS=$'\t' read -r severity name title; do
    [ -z "$name" ] && continue
    accepted=false
    for a in ${ACCEPTED_DEV[@]+"${ACCEPTED_DEV[@]}"}; do
      [ "$a" = "$name" ] && accepted=true && break
    done
    case "$severity" in
      high | critical)
        if [ "$accepted" = true ]; then
          echo "  accepted: $name ($severity)"
        else
          echo "  NOT ACCEPTED: $name ($severity)"
          fail=1
        fi
        ;;
      *) echo "  informational: $name ($severity)" ;;
    esac
  done <<<"$dev_rows"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "Frontend dependency gate FAILED." >&2
  echo "Fix with a version bump (npm audit fix), or — for a dev-only advisory" >&2
  echo "with no fix available — add the package to ACCEPTED_DEV in $0 together" >&2
  echo "with the reason it cannot affect a customer." >&2
  exit 1
fi

echo "Frontend dependency gate passed."
