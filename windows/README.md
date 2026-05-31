# OpenEdge — Windows install (industrial-grade)

This folder contains the bits needed to run OpenEdge on a Windows
industrial PC as a **proper Windows Service** that:

- starts automatically at boot, **before any user logs in**;
- restarts itself on failure with backoff;
- writes rotating logs under `..\logs\`;
- can be controlled with `Start-Service` / `Restart-Service` like any
  other Windows service.

The legacy `scripts\install-autostart.ps1` registers a Task Scheduler
trigger that fires **at user logon** — fine for a developer laptop, wrong
for an unattended industrial PC. Prefer the script in this folder.

## Prerequisites

- Windows 10 / 11 Pro **or** Windows Server 2019+
- **Docker Desktop** (Win 10/11 — enable "Start Docker Desktop when you
  log in to your computer" in Settings → General) **or** **Docker EE**
  (Windows Server — installs as a real service, works headless).
- An elevated PowerShell session.

## Install

```powershell
# From the repo root, as Administrator:
cd C:\OpenEdge
Set-ExecutionPolicy -Scope Process Bypass -Force
.\windows\install-service.ps1
```

The script will:

1. Download **WinSW v2.12.0** (`WinSW.NET4.exe`, ~6 MB) on first run
   and verify its SHA-256. On an air-gapped machine, drop a verified
   copy at `windows\openedge-service.exe` manually before running.
2. Render the `openedge.xml` service definition with your repo path.
3. Register the **OpenEdge** Windows Service running as `LocalSystem`.
4. Start it.

Verify:

```powershell
Get-Service OpenEdge
# Should show Status = Running

# Tail the logs as the stack comes up:
Get-Content .\logs\openedge.out.log -Wait
```

## Day-to-day operations

```powershell
Restart-Service OpenEdge        # restart the whole stack
Stop-Service    OpenEdge        # graceful docker compose down
Start-Service   OpenEdge        # docker compose up -d
Get-Service     OpenEdge        # status
```

The Service Control Manager will restart OpenEdge automatically if it
exits unexpectedly (3 attempts within 5 minutes, then it stops trying
and waits for an operator).

## Uninstall

```powershell
.\windows\install-service.ps1 -Uninstall
```

Logs under `..\logs\` are preserved; remove them by hand if you don't
want them.

## Troubleshooting

| Symptom | Where to look |
|---|---|
| Service installed but stays in "Stopping" | `logs\openedge.err.log` — usually `docker compose up` failed because the Docker engine isn't ready yet. Increase `delayedAutoStart` or set a startup script to wait for `com.docker.service`. |
| `WinSW install failed` | Re-run from an **elevated** PowerShell. WinSW writes to the registry under `HKLM`. |
| Service runs but no UI on `http://localhost:3000` | `docker compose -f docker-compose.yml ps` — check whether containers actually came up. Often `.env` is missing or `JWT_SECRET` not set; `make setup-env` (or `setup-env.bat`) generates it. |
| SHA-256 mismatch on WinSW download | We pin a specific WinSW version. If you trust a newer version, edit `install-service.ps1` and update both `$WinSWVersion` and `$WinSWSha256` together. |

## Why WinSW (and not NSSM or sc.exe directly)?

- **`sc.exe`** alone cannot supervise an arbitrary command — Windows SCM
  requires the binary to call `SetServiceStatus()`. Wrapping `docker
  compose` directly never reports "Running" correctly.
- **NSSM** is excellent but ships only as a binary; we'd have the same
  download/verify story.
- **WinSW** is a single self-contained .NET binary with a git-trackable
  XML config (you can review what the service does just by reading
  `openedge.xml`). It's used in production by Jenkins and many others.
