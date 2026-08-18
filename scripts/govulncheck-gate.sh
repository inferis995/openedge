#!/usr/bin/env bash
#
# Run govulncheck and fail on any vulnerability that is not explicitly accepted.
#
# Why an allowlist exists at all: a few advisories have NO fixed version in any
# release of the module. Leaving the gate red forever would train everyone to
# ignore it, which is worse than having no gate — so each exception is listed
# here with the reason it is tolerable, and ANY other finding, including a new
# release of an accepted one, still fails the build.
#
# Review this list on every dependency bump. An entry is only legitimate while
# the reasoning next to it still holds.
set -euo pipefail

# ── Accepted, with justification ─────────────────────────────────────────────
#
# All three are Moby DAEMON vulnerabilities with "Fixed in: N/A" — they exist in
# every published version of github.com/docker/docker. driver-manager uses the
# CLIENT half of that module to talk to a local socket; it never executes the
# daemon code paths involved. govulncheck reports them because client and daemon
# ship in one module and the symbols are reachable through its package init
# graph, not because this project can reach the vulnerable behaviour.
#
# The residual exposure is the Docker daemon itself, which is the host's
# responsibility to patch — and the deployment already treats socket access as
# host-root-equivalent (see docs/RELEASE-ACCEPTANCE.md, "Known limits").
ACCEPTED=(
  "GO-2026-5668"  # docker cp: race allows creating empty files on the host via symlink swap
  "GO-2026-4887"  # Moby: AuthZ plugin bypass on oversized request bodies
  "GO-2026-4883"  # Moby: off-by-one in plugin privilege validation

  # ── Withdrawn upstream, and REMOVE these once the database settles ─────────
  #
  # Five reports filed against github.com/lib/pq on 2026-08-18 and WITHDRAWN by
  # the Go vulnerability database the same day, every one of them marked "false
  # positive". They are not accepted-with-risk like the three above: there is no
  # vulnerability to carry. They are here because the retraction is propagating
  # unevenly — two CI runs minutes apart on identical go.mod files, one clean
  # and one reporting all five — and a gate that flips a coin gets ignored,
  # which is the failure this whole script exists to prevent.
  #
  # Delete this block once a few consecutive runs are clean without it. If any
  # of these IDs is ever un-withdrawn, the gate must fail again — that is the
  # reason this is a dated exception and not a permanent one.
  "GO-2026-6166"  # withdrawn — lib/pq: alleged GSSAPI exchange not completed
  "GO-2026-6168"  # withdrawn — CVE-2026-56869, lib/pq/scram: unbounded SCRAM iteration count
  "GO-2026-6170"  # withdrawn — CVE-2026-56871, lib/pq: unchecked backend frame length
  "GO-2026-6171"  # withdrawn — CVE-2026-56872, lib/pq: unvalidated RowDescription/DataRow
  "GO-2026-6172"  # withdrawn — CVE-2026-56873, lib/pq: frame payload allocated before bound
)

echo "Running govulncheck…"
# Exit code 3 means vulnerabilities were found; anything else is a real failure
# of the tool itself and must not be swallowed.
set +e
OUTPUT=$(govulncheck -format json ./... 2>/tmp/govulncheck.err)
STATUS=$?
set -e

if [ $STATUS -ne 0 ] && [ $STATUS -ne 3 ] && [ -z "$OUTPUT" ]; then
  echo "govulncheck failed to run (exit $STATUS):" >&2
  cat /tmp/govulncheck.err >&2
  exit $STATUS
fi

# Collect the IDs govulncheck says this code actually CALLS. The JSON stream
# emits a "finding" object per trace; only those with a non-empty trace frame
# containing a function are call-reachable.
FOUND=$(echo "$OUTPUT" \
  | jq -r 'select(.finding != null) | select(.finding.trace[0].function != null) | .finding.osv' \
  | sort -u)

if [ -z "$FOUND" ]; then
  echo "No call-reachable vulnerabilities."
  exit 0
fi

UNEXPECTED=""
for id in $FOUND; do
  accepted=false
  for a in "${ACCEPTED[@]}"; do
    [ "$id" = "$a" ] && accepted=true && break
  done
  if [ "$accepted" = true ]; then
    echo "  accepted: $id"
  else
    echo "  NOT ACCEPTED: $id  (https://pkg.go.dev/vuln/$id)"
    UNEXPECTED="$UNEXPECTED $id"
  fi
done

if [ -n "$UNEXPECTED" ]; then
  echo >&2
  echo "New call-reachable vulnerabilities:$UNEXPECTED" >&2
  echo "Fix them by upgrading, or — only if no fix exists and the code cannot" >&2
  echo "reach the vulnerable behaviour — add the ID to ACCEPTED in $0 with the" >&2
  echo "reasoning." >&2
  exit 1
fi

echo "All call-reachable vulnerabilities are explicitly accepted."
