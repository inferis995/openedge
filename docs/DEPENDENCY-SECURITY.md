# Keeping dependencies patched

Four layers of this platform pull in third-party code, and each one rots in a
different way. Something automated has to watch all four, because the failure
mode is not a broken build — it is a build that stays green while the code
underneath it ages.

| Layer | Manifest | Watched by |
|---|---|---|
| Go modules + stdlib | `go.mod` / `go.sum` | `govulncheck` gate |
| Frontend packages | `services/web-ui/package.json` | `npm audit` gate |
| Container userland (Alpine, nginx) | `FROM` lines in Dockerfiles | Trivy |
| GitHub Actions | `.github/workflows/*` | Dependabot |

The container layer is the one that surprises people: an OpenSSL CVE in the
runtime image appears in no manifest at all, so a project can be green on both
`go.sum` and `package-lock.json` and still ship a vulnerable userland.

## The two halves

**Dependabot opens the PRs** (`.github/dependabot.yml`) — weekly, Monday
morning. Patch and minor updates are batched into one PR per ecosystem; majors
arrive alone, because those are the ones that need somebody to think. Nothing
merges by itself: a dependency bump here ships to industrial plants, so a human
approves it. What the automation buys is that the update is always on offer and
never silently missed.

**The gates decide whether a version is acceptable** — on every PR (in `CI`)
and again nightly against unchanged code (in `Security`). The nightly run is
not redundant: most vulnerabilities arrive without anybody pushing anything.
An advisory published this morning against a dependency frozen six months ago
is invisible to every push-triggered check, because there is no push.

If the nightly scan fails, it opens a tracking issue labelled `security-scan`
and comments on it each following night rather than filing a duplicate. Closing
it without fixing the cause just means it reopens.

## When a gate fails

**Go** — `scripts/govulncheck-gate.sh`. Fails on call-reachable advisories only:
a vulnerable function that this code never invokes is noise, and a gate full of
noise is a gate people mute. Fix by upgrading. If no fixed version exists in any
release *and* the vulnerable path is genuinely unreachable, add the ID to
`ACCEPTED` with the reasoning — the existing Moby daemon entries show the shape
of an acceptable justification.

**Frontend** — `scripts/npm-audit-gate.sh`. Split by where the code runs:

- *Production* dependencies ship to the browser and are reachable by anyone who
  can load the UI. Any advisory fails, at any severity, with no allowlist.
- *Dev* dependencies never leave CI or a laptop, so a dev-server path traversal
  is not a customer exposure. They fail at high/critical — that is also where
  build-time supply-chain compromise lives — and can be accepted with a written
  reason.

Usually `npm audit fix` inside `services/web-ui` is the whole fix, because the
declared semver ranges already permit the patched version and only the lockfile
is stale.

**Base images** — bump the `FROM` tag. The scan runs with `--ignore-unfixed`,
so anything it reports has a patched tag available; there is always something
to do. The image list is read out of the Dockerfiles at scan time rather than
being listed in the workflow, so it cannot drift from what is actually built.

## Keeping the toolchain honest

`GO_VERSION` in the CI workflows must match the toolchain in `go.mod`, and the
`golang:` tag in every Dockerfile must match both. This is not tidiness:
`govulncheck` reports against whatever Go version runs it, so if CI scans with
1.25 while a Dockerfile builds with 1.24, that service ships with the stdlib
advisories 1.25 fixed and the scan reports clean. The same applies to the
runtime `alpine:` tag, which is why they are all pinned to one version.
