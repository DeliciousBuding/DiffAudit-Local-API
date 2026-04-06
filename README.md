# Local API Service

Standalone local research control plane for DiffAudit.

## Path

- `D:\Code\DiffAudit\Services\Local-API`

## Purpose

This service exposes the local audit HTTP API and delegates real experiment execution to the Python research CLI in:

- `D:\Code\DiffAudit\Project`

## Run

```powershell
cd D:\Code\DiffAudit\Services\Local-API
go run ./cmd/local-api --host 127.0.0.1 --port 8765
```

Preferred handoff entry:

```powershell
powershell -ExecutionPolicy Bypass -File D:\Code\DiffAudit\Services\Local-API\run-local-api.ps1
```

The launcher now prints a timestamped startup banner, resolved path summary, and live Go service logs in the same console so operators can immediately see initialization state, listen address, and request activity.

Override roots only when the local workspace layout is different:

```powershell
powershell -ExecutionPolicy Bypass -File D:\Code\DiffAudit\Services\Local-API\run-local-api.ps1 `
  -ListenHost 127.0.0.1 `
  -ListenPort 8765 `
  -ProjectRoot D:\Code\DiffAudit\Project `
  -ExperimentsRoot D:\Code\DiffAudit\Project\experiments `
  -JobsRoot D:\Code\DiffAudit\Project\workspaces\local-api\jobs
```

## Defaults

- experiments root: `D:\Code\DiffAudit\Project\experiments`
- jobs root: `D:\Code\DiffAudit\Project\workspaces\local-api\jobs`
- project root: `D:\Code\DiffAudit\Project`

## Remote / Backup

Current strategy notes live in:

- `D:\Code\DiffAudit\Services\Local-API\REMOTE_STRATEGY.md`
- gpu scheduler: `D:\Code\DiffAudit\LocalOps\paper-resource-scheduler\gpu-scheduler.exe`
- gpu request doc: `D:\Code\DiffAudit\LocalOps\paper-resource-scheduler\gpu-resource-requests.md`

## Governance Boundary

- Source of truth for the local research control plane stays in `D:\Code\DiffAudit\Services\Local-API`.
- `D:\Code\DiffAudit\Platform\apps\api-go` is a gateway only and must not absorb local job execution logic.
- `D:\Code\DiffAudit\Project` remains a read-only fact source plus Python execution target for this service.

## Repository Ownership

`Services\Local-API` remains under the `Services` top-level boundary and is now tracked by its own local Git repository.

Current repository baseline:

- branch: `main`
- bootstrap commit: `7d8b22c`

Until a remote strategy is decided, treat this directory as the only writable source of truth for the local API and avoid recreating the same control-plane code inside `Platform` or `Project`.
