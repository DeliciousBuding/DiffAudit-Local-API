# Local API Service

Standalone HTTP control plane for running local research workflows.

Public repository:

- [DeliciousBuding/DiffAudit-Local-API](https://github.com/DeliciousBuding/DiffAudit-Local-API)

## Repository Scope

This repository only contains the Local API service. It integrates with a separate
research workspace via configured paths and does not assume any fixed parent
directory.

## Run

```powershell
go run ./cmd/local-api --host 127.0.0.1 --port 8765
```

The binary accepts both flags and environment variables. Flags override env values.

Preferred launcher:

```powershell
powershell -ExecutionPolicy Bypass -File .\run-local-api.ps1
```

Override roots only when your workspace layout is different:

```powershell
powershell -ExecutionPolicy Bypass -File .\run-local-api.ps1 `
  -ListenHost 127.0.0.1 `
  -ListenPort 8765 `
  -ProjectRoot C:\path\to\research-project `
  -ExperimentsRoot C:\path\to\research-project\experiments `
  -JobsRoot C:\path\to\local-api\jobs
```

Use an isolated env profile instead of hardcoding machine-specific paths:

```powershell
powershell -ExecutionPolicy Bypass -File .\run-local-api.ps1 `
  -EnvFile .\config\dev.env
```

## Configuration Isolation

- Real env files are local-only and ignored by git.
- Commit examples only: `.env.example`, `config/dev.example.env`, `config/deploy.example.env`.
- Keep dev and deploy values in separate files to avoid accidental cross-use.

## Base URL for Callers

Callers should configure their own base URL pointing at this service, for example:

- `http://127.0.0.1:8765`
- `https://api.example.com/local-api` (behind a reverse proxy)

Do not assume loopback or a fixed workspace path in client configs.

## Environment Variables

- `DIFFAUDIT_LOCAL_API_HOST`
- `DIFFAUDIT_LOCAL_API_PORT`
- `DIFFAUDIT_LOCAL_API_PROJECT_ROOT`
- `DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT`
- `DIFFAUDIT_LOCAL_API_JOBS_ROOT`
- `DIFFAUDIT_LOCAL_API_GPU_SCHEDULER`
- `DIFFAUDIT_LOCAL_API_GPU_REQUEST_DOC`
- `DIFFAUDIT_LOCAL_API_GPU_AGENT_PREFIX`

See `.env.example` and `config/*.example.env` for concrete templates.

## Remote / Backup

Strategy notes live in `REMOTE_STRATEGY.md`.

## Governance Boundary

- This repo is the source of truth for the Local API service code.
- External research execution and GPU scheduling are configured by paths, not embedded here.
- Keep client integrations via documented HTTP interfaces and base URL configuration.
