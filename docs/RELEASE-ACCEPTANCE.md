# On-prem release acceptance

What to verify before an OpenEdge installation is handed to a customer.

This is **not** a generic checklist. Every item below corresponds to a defect
that was present in this codebase and has been fixed — they are the things most
likely to be wrong again after a change, and several of them are invisible from
the UI unless you look for them deliberately.

Automated coverage: `make test-go` (unit, ~420 tests) and `make test-e2e`
(acceptance, against a running stack). **Neither runs against real hardware** —
that is what section 3 is for.

---

## 0. Before you start

```bash
git log --oneline -1          # note the commit being released
make start                    # first-run: generates all secrets into .env
```

`make start` prints the generated admin password. Record it in the customer's
password manager and **do not** reuse it across installations.

| Check | Expected |
|---|---|
| `grep ENCRYPTION_KEY .env` | present, 32 characters |
| `grep OPENEDGE_INITIAL_ADMIN_PASSWORD .env` | present, not `CHANGE_ME` |
| `make logs \| grep -i "SECURITY WARNING"` | **no output** |

That last one matters: core-api warns loudly if any global admin still uses the
default password or if `ENCRYPTION_KEY` is unusable. A clean start prints
neither.

---

## 1. Automated suites

```bash
make test-go                        # unit + race detector
make test-e2e                       # acceptance, needs the stack running
cd services/web-ui && npm run test:run && npx tsc --noEmit
```

All three must pass. `make test-e2e` is the one that proves the assembled system
works — it drives the real API and the real broker and asserts on the far side.

---

## 2. Multi-tenancy (only if the customer will host more than one organization)

Covered by `make test-e2e`, but worth confirming by hand on the real
installation, because it is the failure with the worst consequences:

1. Create two organizations, and one **admin** user inside each.
2. Logged in as the admin of org A, confirm you **cannot**:
   - see org B's users in Users
   - open org B's tags, gateways or trends
   - download org B's edge installer
   - reach `/api/organizations/<B>/api-keys`
3. Confirm the full-database backup and the audit CSV are refused to both, and
   available only to the global admin.

Any success here is a stop-ship.

---

## 3. Field commissioning — requires the real PLC

**Nothing in CI covers this.** These are the driver-level defects that were
fixed; each one produced plausible-looking but wrong values, which is precisely
what an operator cannot spot.

### 3.1 Bit-addressed booleans (S7)

Configure a tag on a **non-zero bit**, e.g. `M0.3` or `DB1.DBX0.5`.

- Force the bit true on the PLC → the tag must read **true**.
- Force it false → **false**.

> This used to always read `false` with GOOD quality regardless of the PLC,
> because the read request ignored the bit offset. Testing only bit 0 hides it.

### 3.2 Engineering-unit scaling on a setpoint

Configure a tag with scaling (e.g. raw 0–27648 ↔ EU 0–100) and a setpoint widget.

- Write **50** from the UI.
- Read the raw register on the PLC: it must be ≈ **13824**, not 50.

> A wrong reverse-scaling sent the EU number straight to the register — the
> read-back is EU-scaled, so the UI looked consistent while the machine ran at
> 0.2% instead of 50%.

### 3.3 Reconnection

With the driver polling normally:

- Unplug the PLC's network cable for ~1 minute, then plug it back in.
- Values must resume **without restarting any container**.
- During the outage the UI must show the tags as bad/stale, not as the last good
  value in green.
- Repeat for the MQTT broker (`docker restart openedge-mosquitto`).

### 3.4 Alarm to notification, end to end

Configure a notification channel (email, Telegram, Slack, Teams or PagerDuty)
and set `notif_min_severity` appropriately.

1. Configure a high alarm on a real tag.
2. Drive the process past the threshold.
3. Confirm **all** of:
   - the alarm appears in the UI
   - a row appears in `alarm_events`
   - **the notification actually arrives**
4. Bring the value back below the threshold minus the deadband → the alarm
   clears, and PagerDuty (if used) auto-resolves the incident.

> This whole path was silently dead: alarms were persisted, the dispatcher was
> implemented, and nothing published to the topic joining them. Verify the
> message arrives — do not infer it from the UI.

### 3.5 A transient spike must not raise an alarm

With a delay configured (e.g. 30 s) on a threshold of 100 with deadband 5:

- Push the value to 101 for one scan, then let it settle at 97.
- **No alarm** must fire.

### 3.6 Sparkplug (only if the customer uses a Sparkplug host)

Point Ignition, HiveMQ or the customer's host at the broker.

- The edge node must appear after NBIRTH, with its devices.
- Values must arrive without the host looping rebirth requests.
- Stop a driver → the host must see the node die.

---

## 4. Data integrity

1. Let the system run for ~15 minutes with real values.
2. Open a trend and confirm the history is continuous and the values match the
   PLC.
3. Use a tag whose alias contains a **hyphen or a space** — those were silently
   never historised.
4. Stop a driver for 2 minutes → the trend must show a **gap**, not a straight
   line across the outage.

---

## 5. Backup and restore

A backup nobody has restored is not a backup.

```bash
make backup-now
ls -lh backups/                 # non-trivial size, recent timestamp
gzip -t backups/<file>.sql.gz   # integrity
```

Then, **on a throwaway machine or VM** (never the customer's live system),
restore that archive and confirm the tags, users and history come back.

Also confirm the scheduled backup actually runs: check the timestamps the day
after installation.

---

## 6. Security posture

| Check | Expected |
|---|---|
| `docker compose ps` | Postgres, Redis and core-api bound to `127.0.0.1`, not `0.0.0.0` |
| `curl http://<host>:8081/metrics` from another machine | refused, or requires `METRICS_TOKEN` |
| Login page over the network | HTTPS (`make onprem-tls`) — otherwise the JWT crosses the plant LAN in cleartext |
| `git status` in the deployment directory | `mosquitto/config/dynamic-security.json` **not** tracked |
| Default admin password | changed, or the generated one recorded and distinct per site |

Enable MFA for every administrator before handover.

---

## 6b. Permissions, when upgrading an existing installation

Skip this on a fresh install; it only matters where accounts already exist.

Seven per-user permissions were stored, shown as checkboxes on the Users page,
and enforced on nothing: the middleware that checks them was mounted on no
route. Unticking a box saved, redisplayed as unticked, and changed nothing an
account could do. They are enforced now, which moves the goalposts in two
directions at once.

**Accounts that lose something.** A non-admin with no row in `role_permissions`
is denied by default, so on the first start after this upgrade a plain user
cannot:

- write a tag or load a recipe — every synoptic button, setpoint and recipe
- export tags or OEE reports
- read the audit log

Administrators are unaffected: `RequirePermission` admits them before it
consults the table.

**Accounts that gain something.** Acknowledging alarms, editing recipes,
managing shifts and downloading the edge installer used to require an
administrator. They now follow their permission, so those tasks can be
delegated without handing out an admin account — which is how a control room
stops sharing one login that nobody can attribute anything to.

Before the shift starts, not during it:

- [ ] List the non-admin accounts and agree with the customer what each does
- [ ] Grant the permissions from Users → Permissions, one account at a time
- [ ] Have an operator write one setpoint and acknowledge one alarm, and watch
      it work — the first time somebody discovers this is mid-incident is the
      worst possible time

## 6c. Upgrading to 3.2.0 — behaviour changes

Skip this on a fresh install. On an installation that is already running, each
of these changes what the plant sees **without an error to announce it**. Do
them before the upgrade, with the customer, not after. The first one can move
machines.

### External writes (`sys/write`) — first, before anything else

Writes published on `sys/write/{org}/...` by other systems used to reach only
OPC UA gateways; to S7, Modbus and MQTT gateways they were logged and lost.
After the upgrade they are executed.

- [ ] Before upgrading, find out whether anything publishes there:

      docker logs openedge-core-api 2>&1 | grep "\[WRITE CMD\] Received" | tail -20

      Any line means something publishes writes. Logs rotate, so an empty
      result covers only the retained window: ask the customer too.

- [ ] If something does, agree with the customer what it is and whether those
      writes should run. If not, withdraw that system's broker credentials
      **before** upgrading.

### Clocks

Write commands now expire (30 s by default) and a command dated in the future
is refused as a clock problem.

- [ ] `timedatectl` on the server and on every box: "System clock synchronized:
      yes". Without NTP, writes to a box whose clock drifts are refused.
- [ ] After the upgrade, write one setpoint from the UI and see it arrive.

### Boxes

The server now skips gateways assigned to a box, and a new box polls nothing
until something is assigned to it.

- [ ] Organization → Edge: the server line shows how many gateways it polls,
      and no "polled by nobody" warning is shown.
- [ ] For each box, its gateways and scope are what the customer expects.


### Engineering-unit scaling

Only matters if some tag has `scaling_enabled`. Alarms are now compared against
engineering units instead of raw counts, `historize_deadband` filters in
engineering units, and the trend shows a step at the moment of the upgrade.

- [ ] Before upgrading, run the read-only review and keep the output:

      psql "$DATABASE_URL" -f scripts/eu-scaling-alarm-review.sql

- [ ] Go through section 1 of the output with the customer. Rows marked
      "era SEMPRE attivo" or "interveniva quasi sempre" will clear; rows marked
      "non scattava MAI" or "comincia a scattare" will start firing. **Also look
      at "la soglia effettiva si sposta"** — a threshold that becomes ten times
      more sensitive is the one nobody expects.
- [ ] Section 2 lists boolean alarms on tags with `invert`: their state flips.
- [ ] Agree whether to convert the stored history (irreversible — the SQL and
      its caveats are in `docs/EU_SCALING.md`) or to live with the step.
- [ ] After the upgrade, confirm a scaled tag shows the same number on a gauge
      and on its trend.

### driver-mqtt booleans

A BOOL tag receiving a word outside `true/1/on/yes/t/y`, `false/0/off/no/f/n`
or a number used to read **false**; it now produces no value.

- [ ] After the upgrade: `docker logs <driver-mqtt container> | grep -E "not a recognizable|as BOOL"`.
      Each line is a tag that was reading a fabricated "off" before. Fix it
      with a `json_path` or by changing what the device publishes.

### Names containing `/`

- [ ] `SELECT name FROM organizations WHERE name LIKE '%/%' UNION ALL
      SELECT name FROM sites WHERE name LIKE '%/%' UNION ALL
      SELECT name FROM areas WHERE name LIKE '%/%' UNION ALL
      SELECT name FROM gateways WHERE name LIKE '%/%' UNION ALL
      SELECT alias FROM tags WHERE alias LIKE '%/%';`
- [ ] If anything comes back: those tags start being historised, and their MQTT
      topic changes (`/` becomes `-`). Re-point any external subscriber —
      Ignition, a third-party host — to the new topic.

## 7. Handover

- [ ] Credentials in the customer's password manager, not in an email
- [ ] Backup destination and schedule agreed, and one restore rehearsed
- [ ] Someone on the customer's side knows how to run `make logs` and
      `make restart`
- [ ] Commit hash and image tags recorded for this installation
- [ ] Contact and escalation path for alarms agreed

---

## Known limits at this release

State these to the customer rather than letting them be discovered:

- **The OTA update path over MQTT has no signature check.** It validates the
  image against an allowlist prefix (`OTA_IMAGE_PREFIX`); the HTTP update poller,
  which does verify a SHA-256, is the preferred mechanism.
- **A failed OTA does not roll back.** It reports `apply_failed` and leaves the
  previous version running; recovery is manual.
- **Nothing in CI installs an edge box.** The images it pulls are checked
  against what the release publishes, but `install.sh` / `install.ps1` are
  never run: install one box on a real machine before handing over a release
  that relies on boxes.
- **Two topic families are still readable across tenants.** Gateway health now
  has an organization-scoped shape (`sys/health/{org}/{gateway}`), but the old
  shape (`sys/health/{gateway}`) is still granted to every tenant so drivers not
  yet upgraded keep working — any tenant can read or publish it. Commands
  (`sys/command/#`) are receive-only, so no tenant can send one to another's
  gateway, but any tenant can observe them. Single-tenant on-prem installations
  are unaffected.
- **Traffic on the plant LAN is in cleartext by default.** The web UI is plain
  HTTP on port 3000 until `make onprem-tls` is run, and the broker listener on
  18830 has no TLS: gateway and edge-box credentials cross the network in
  clear. Acceptable on a segregated OT network if stated; not for a customer
  preparing NIS2 evidence.
- **The web UI is the least tested part.** One test file for about a hundred
  components; section 3 and a walk through every page are the real check.
- **driver-manager holds the Docker socket.** It runs as a non-root user, but
  Docker API access is equivalent to host root; isolate the machine accordingly.
