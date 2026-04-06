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

## Docker

Build the container image from the repository root:

```powershell
docker build -t diffaudit-local-api:dev .
```

The container defaults to `0.0.0.0:8765` so it can accept traffic from the
host. Bind or name the writable paths you want the service to use:

```powershell
docker run --rm -p 8765:8765 `
  -e DIFFAUDIT_LOCAL_API_PROJECT_ROOT=/workspace/project `
  -e DIFFAUDIT_LOCAL_API_REPO_ROOT=/workspace/repo `
  -e DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT=/workspace/project/experiments `
  -e DIFFAUDIT_LOCAL_API_JOBS_ROOT=/workspace/jobs `
  -v C:\path\to\project:/workspace/project `
  -v C:\path\to\repo:/workspace/repo `
  -v local-api-jobs:/workspace/jobs `
  diffaudit-local-api:dev
```

`DIFFAUDIT_LOCAL_API_PROJECT_ROOT`, `DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT`,
`DIFFAUDIT_LOCAL_API_JOBS_ROOT`, and `DIFFAUDIT_LOCAL_API_REPO_ROOT` remain the
same API contract as the native process. The only Docker-specific requirement is
that the container paths must exist and be writable when job endpoints are used.

## Configuration Isolation

- Real env files are local-only and ignored by git.
- Commit examples only: `.env.example`, `config/dev.example.env`, `config/deploy.example.env`.
- Keep dev and deploy values in separate files to avoid accidental cross-use.

## Base URL for Callers

Callers should configure their own base URL pointing at this service, for example:

- `http://127.0.0.1:8765`
- `https://api.example.com/local-api` (behind a reverse proxy)

Do not assume loopback or a fixed workspace path in client configs.

## HTTP Contract

Current discovery and control routes:

- `GET /health`
- `GET /api/v1/catalog`
- `GET /api/v1/experiments/recon/best`
- `GET /api/v1/experiments/{workspace}/summary`
- `GET /api/v1/audit/jobs`
- `POST /api/v1/audit/jobs`
- `GET /api/v1/audit/jobs/{job_id}`

`POST /api/v1/audit/jobs` now requires an explicit `contract_key`. The current
publicly supported executable contract is the black-box recon mainline:

```json
{
  "job_type": "recon_artifact_mainline",
  "contract_key": "black-box/recon/sd15-ddim",
  "workspace_name": "api-job-001",
  "job_inputs": {
    "artifact_dir": "D:/artifacts/recon-scores",
    "method": "threshold"
  }
}
```

The service rejects job bodies that omit `contract_key` or pair a `job_type`
with the wrong contract. Contract-specific fields should go under `job_inputs`.
Current recon callers may still send legacy top-level `artifact_dir` and
`method`, and the service normalizes them into `job_inputs` internally.

Internally, `Local-API` now keeps a contract registry that separates:

- live contracts that power the current public `catalog`, `models`, and job routes
- target contracts that describe future gray-box / white-box admission without
  pretending those lines are executable today

The current live executable line remains `black-box/recon/sd15-ddim`. Gray-box
and white-box rows are registry-only placeholders until they have admitted job
definitions and stable runnable assets.

A target contract is not promoted to live just because code or smoke assets
exist. The registry now carries explicit promotion gates for future lines, such
as:

- stable admitted `job_type` and runner support
- line-owned promoted asset roots
- summary hydration rules proven against non-smoke evidence
- asset-grade / provenance approval for live catalog exposure

Job observability note:

- the default execution path still uses Go `CombinedOutput()`
- when that path fails, `command` is recorded, `output_capture` is set to
  `combined`, and the merged command-output tail is stored in `output_tail`
- in that default combined mode, `stdout_tail` and `stderr_tail` are not
  treated as authoritative split streams
- a future runner can provide stronger stream semantics if needed

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

## Release Workflow

This repository uses a minimal release gate in
`.github/workflows/release.yml`:

- Pull requests and pushes to `main` run `go test ./...` and a Linux
  `go build ./cmd/local-api` check.
- Tags matching `v*` run the same verification, upload the built Linux binary
  as a workflow artifact, and validate the Docker image with `docker build`.

Release flow:

```powershell
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag is the release trigger. This workflow is validation-only for now: it
does not publish a container image or create a GitHub Release automatically.

## Governance Boundary

- This repo is the source of truth for the Local API service code.
- External research execution and GPU scheduling are configured by paths, not embedded here.
- Keep client integrations via documented HTTP interfaces and base URL configuration.
