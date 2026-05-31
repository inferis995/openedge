# OpenEdge — operator guide (on-prem)

The 10-minute reference for someone who just turned on the OpenEdge PC
on the factory floor. For the install / sysadmin guide, see
`docs/DEPLOY.md`.

---

## 1. Daily logins

The UI is reachable at one of:

| How the box was deployed | Open in browser |
|---|---|
| Vanilla install (port 3000) | `http://<openedge-pc>:3000` |
| TLS via Caddy (`make onprem-tls`) | `https://<openedge-host>` |
| Kiosk mode | already open in fullscreen at boot |

Default credentials: **`admin / admin123`** — change them on first login
(or set `OPENEDGE_INITIAL_ADMIN_PASSWORD` in `.env` before first start so
they rotate automatically).

The Sidebar shows the language picker at the bottom — flip between EN
and IT at any time; the choice persists per browser.

---

## 2. The pages an operator uses every day

### Trend
Pick one or more tags, see live values + history on the same chart.
Useful for "is line 1 stable today?".

### Alarms
Active and recent alarms across the plant. Operators can acknowledge an
alarm from here (audited).

### Recipes
The killer SCADA feature. A *recipe* is a named bundle of (tag, value)
pairs. The operator opens a recipe, clicks **Load**, and the system
writes every value to the PLC in one shot. Each load is recorded with
the operator's name in the run history — perfect for "who loaded recipe
Verde Blu at 03:00?" investigations.

> Tip: make a recipe per product / per shift / per cleaning cycle.
> Mistakes still happen, but they happen *once* and the audit shows it.

### Reports
CSV exports of:
- **Tag history** — raw historian samples for a range + tag filter.
- **Alarm events** — all triggers/clears with severity and value.
- **Audit log** *(admin only)* — every operator action across the
  system.

Open the file in Excel / LibreOffice. Times are UTC; if your reports
need local time, format the column in Excel after import.

### Diagnostics *(admin only)*
Real-time health: disk usage, CPU load, memory, network link state and
the per-service pings. Refresh every 5s. **Use this BEFORE calling
support** — most issues show up here first (a disk approaching 100%, a
network port flapping, Postgres slow to respond).

---

## 3. Things you should NOT do without a backup

These actions are destructive — take a snapshot first
(`make backup-now`):

- Loading a recipe you didn't write
- Editing alarm thresholds in production hours
- Deleting tags / gateways / sites
- Running `make update` (it backs up automatically, but a second copy
  on a USB key is the industrial standard)

---

## 4. Day-to-day commands the system admin runs

```bash
make backup-now                                     # ad-hoc backup
make backup-to-usb USB=/media/usbkey                # copy newest backup to USB
make restore BACKUP=./backups/openedge-...dump      # restore (destructive!)
make update                                         # safe upgrade (snapshot+pull+health)
make update-check                                   # show what an upgrade would change
sudo systemctl status openedge                      # is the stack running?
sudo journalctl -u openedge -f                      # follow logs (Linux/systemd)
sudo systemctl restart openedge                     # restart everything
```

For the Windows equivalent, see `windows/README.md`.

---

## 5. Setting up notifications (email or Telegram)

Alarms become useful only when they reach the operator at 03:00. Open
**System → Settings** and fill in either or both:

### Email (any SMTP relay)
- `notif_email_enabled` = true
- `notif_email_smtp_host` / `_port` / `_username` / `_password`
- `notif_email_from` (envelope sender)
- `notif_email_to` (comma-separated)
- `notif_email_use_tls` = true for port 465, false for 587 (STARTTLS).

Gmail: create an **app password**
(https://myaccount.google.com/apppasswords) — your regular password
won't work.

### Telegram (recommended — easier, free, push notifications on phones)
1. Talk to [@BotFather](https://t.me/BotFather), `/newbot`, get the token.
2. Add the bot to a group (or DM it), send any message.
3. Open `https://api.telegram.org/bot<TOKEN>/getUpdates`, copy the
   `chat.id` (negative for groups, positive for DMs).
4. Set `notif_telegram_enabled` = true, paste the token + chat id.

### Filters (apply to both channels)
- `notif_min_severity` — drop events below this level (low / medium /
  high / critical).
- `notif_on_cleared` — also notify when an alarm clears (default off).
- `notif_rate_limit_per_min` — cap (default 60) to survive flapping
  sensors.

Then click **Test notification** to fire a fake alarm to every enabled
channel. The result page lists per-channel success / error so you can
fix typos quickly.

---

## 6. Kiosk / HMI mode

For a fixed touchscreen PC the operator should never see the desktop on.

### Linux
```bash
./scripts/install-kiosk-linux.sh https://openedge.local
# Reboot the PC. The browser autostarts fullscreen on the OpenEdge UI.
# To undo:
./scripts/install-kiosk-linux.sh https://openedge.local --uninstall
```

### Windows
```powershell
# As Administrator:
.\windows\install-kiosk.ps1 -Url https://openedge.local
# Then enable auto-login for the operator user (netplwiz) and disable
# the screensaver. Reboot to see the kiosk start.
```

---

## 7. Troubleshooting one-liners

| Symptom | Try first |
|---|---|
| UI shows "Services starting up..." for >2 min | `sudo journalctl -u openedge -f` (Linux) or container logs (Windows). Usually Postgres is initialising. |
| Tags show as "stale" | Diagnostics page → check the driver-manager container is up. Then check the gateway's MQTT health topic in MQTT Monitor. |
| Recipe load reports "failed" | The MQTT publish to the gateway failed. Check the broker is up (Diagnostics) and the gateway's driver container is running. |
| Alarms not delivering email | System → Settings → "Test notification". Per-channel error tells you exactly what's wrong (auth, host, recipient). |
| Backup container stopped | Check `docker compose ps` — likely DB credentials in `.env` mismatch what Postgres was initialised with. |
| Disk filling fast | Diagnostics → Disk card. Usually the historian retention isn't pruning fast enough — check `db_retention_days` in Settings. |
| Update failed, can't reach UI | `make restore BACKUP=./backups/openedge-pre-update-...dump`. The pre-update snapshot was named at upgrade time. |
| Operator forgot password | Admin → Users → reset. There is no email-reset path in on-prem mode (intentionally — no SMTP guarantee). |

---

## 8. Glossary

- **Tag** — one named PLC variable (e.g. `temperature_oven_1`).
- **Gateway** — one PLC (or one driver instance). Has many tags.
- **Site / Area** — physical grouping (factory / production line).
- **Driver** — a container that talks to one gateway via its native
  protocol (Modbus, S7, OPC-UA, MQTT).
- **Driver-manager** — supervises driver containers; lives next to the
  PLCs.
- **Historian** — TimescaleDB hypertable that stores tag samples.
- **Alarm** — a rule + the events that fire when the rule trips.
- **Recipe** — a named set of (tag, value) pairs an operator loads in
  one shot.
- **Audit log** — append-only record of every operator-significant
  action.
