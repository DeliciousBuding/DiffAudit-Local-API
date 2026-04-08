# Local API Service

Standalone audit service for running local research workflows.

Public repository:

- [DeliciousBuding/DiffAudit-Local-API](https://github.com/DeliciousBuding/DiffAudit-Local-API)

## Repository Scope

This repository contains the Local API service as a unified audit surface.

It now combines:

- an HTTP control plane
- a database-backed contract registry
- a profile-driven execution engine
- a bundled runner codebase for black-box / gray-box / white-box execution

The service may still read assets, configs, and upstream repos from a separate
workspace, but the executable attack/runtime code no longer has to come from
`Project/src/diffaudit`.

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
  -ServiceRoot C:\path\to\DiffAudit-Local-API `
  -ProjectRoot C:\path\to\research-project `
  -ExperimentsRoot C:\path\to\research-project\experiments `
  -JobsRoot C:\path\to\local-api\jobs `
  -ExecutionMode local
```

Use an isolated env profile instead of hardcoding machine-specific paths:

```powershell
powershell -ExecutionPolicy Bypass -File .\run-local-api.ps1 `
  -EnvFile .\config\dev.env
```

Execution mode:

- `local`
  - default
  - executes the resolved runner script on the host
- `docker`
  - uses the built-in Docker executor
  - resolves a method profile into a `docker run ...` command against the runner images

New launcher flags and env:

- `-ServiceRoot` / `DIFFAUDIT_LOCAL_API_SERVICE_ROOT`
- `-RegistryDBPath` / `DIFFAUDIT_LOCAL_API_REGISTRY_DB_PATH`
- `-RunnersRoot` / `DIFFAUDIT_LOCAL_API_RUNNERS_ROOT`
- `-ExecutionMode` / `DIFFAUDIT_LOCAL_API_EXECUTION_MODE`
- `-DockerBinary` / `DIFFAUDIT_LOCAL_API_DOCKER_BINARY`

## Runner System

Runner code now lives in this repository under:

- `runners/shared`
- `runners/recon-runner`
- `runners/pia-runner`
- `runners/gsa-runner`

Current intent:

- `Project` remains the research source of truth
- `Local-API` dispatches bundled runner code from this repo
- contract metadata is persisted in SQLite rather than compiled Go constants
- assets, configs, and upstream repos are passed in as inputs rather than imported from `Project/src/diffaudit`

This is the path used to decouple black-box / gray-box / white-box execution
from the research repo while keeping the current assets and external upstream
repos usable.

Current runner images:

- `diffaudit/recon-runner:latest`
- `diffaudit/pia-runner:latest`
- `diffaudit/gsa-runner:latest`

Build commands:

```powershell
docker build -t diffaudit/recon-runner:latest -f runners/recon-runner/Dockerfile .
docker build -t diffaudit/pia-runner:latest -f runners/pia-runner/Dockerfile .
docker build -t diffaudit/gsa-runner:latest -f runners/gsa-runner/Dockerfile .
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

If you want the Local-API container itself to launch Docker jobs, the host must
also expose a Docker daemon to the container. On Linux that usually means:

```powershell
docker run --rm -p 8765:8765 `
  -e DIFFAUDIT_LOCAL_API_EXECUTION_MODE=docker `
  -v /var/run/docker.sock:/var/run/docker.sock `
  -v C:\path\to\project:/workspace/project `
  -v C:\path\to\repo:/workspace/repo `
  -v local-api-jobs:/workspace/jobs `
  diffaudit-local-api:dev
```

Recommended operational stance:

- host-native `Local-API` is simplest when iterating locally
- containerized `Local-API` is fine for deployment, but Docker execution mode
  requires access to a usable Docker daemon

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
- `GET /diagnostics`
- `GET /api/v1/catalog`
- `GET /api/v1/evidence/attack-defense-table`
- `GET /api/v1/evidence/contracts/best?contract_key=...`
- `GET /api/v1/experiments/recon/best`
- `GET /api/v1/experiments/{workspace}/summary`
- `GET /api/v1/audit/jobs`
- `POST /api/v1/audit/jobs`
- `GET /api/v1/audit/jobs/{job_id}`

`POST /api/v1/audit/jobs` requires an explicit `contract_key`. The service now
uses a profile-driven execution layer internally:

- registry resolves `contract_key + job_type`
- profile resolves a method-specific execution spec
- runtime executes that spec in `local` or `docker` mode

Current live executable contracts:

- `black-box/recon/sd15-ddim`
- `gray-box/pia/cifar10-ddpm`
- `white-box/gsa/ddpm-cifar10`

Admitted evidence read path:

- `GET /api/v1/evidence/attack-defense-table`
  - returns the unified admitted black-box / gray-box / white-box table from
    `Project/workspaces/implementation/artifacts/unified-attack-defense-table.json`
- `GET /api/v1/evidence/contracts/best?contract_key=gray-box/pia/cifar10-ddpm`
  - returns the best admitted summary envelope for the requested live contract
- `GET /api/v1/experiments/recon/best`
  - legacy convenience alias for `black-box/recon/sd15-ddim`

Registry source of truth:

- runtime store: SQLite database at `registry_db_path`
- default seed payload: `internal/api/registry_seed.json`
- current runtime behavior: initialize or reuse the SQLite registry, then query it for `catalog`, `models`, and admitted jobs

Example black-box job:

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

Example gray-box job:

```json
{
  "job_type": "pia_runtime_mainline",
  "contract_key": "gray-box/pia/cifar10-ddpm",
  "workspace_name": "api-pia-runtime-mainline-001",
  "runtime_profile": "docker-default",
  "repo_root": "D:/Code/DiffAudit/Project/external/PIA",
  "assets": {
    "member_split_root": "D:/Code/DiffAudit/Project/external/PIA/DDPM"
  },
  "job_inputs": {
    "config": "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
    "device": "cpu",
    "num_samples": "16"
  }
}
```

Example white-box job:

```json
{
  "job_type": "gsa_runtime_mainline",
  "contract_key": "white-box/gsa/ddpm-cifar10",
  "workspace_name": "api-gsa-runtime-mainline-001",
  "runtime_profile": "docker-default",
  "repo_root": "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
  "assets": {
    "assets_root": "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa"
  },
  "job_inputs": {
    "resolution": "32",
    "ddpm_num_steps": "20",
    "sampling_frequency": "2",
    "attack_method": "1"
  }
}
```

The service rejects job bodies that omit `contract_key` or pair a `job_type`
with the wrong contract. `runtime_profile` selects the execution target for a
single job, `assets` carries reusable asset roots, and `job_inputs` carries
method-specific parameters. The service normalizes `assets` into `job_inputs`
before resolving the execution profile, while `runtime_profile` remains a
string selector such as `local`, `local-default`, `docker`, or
`docker-default`.

When `project_root` points at a DiffAudit `Project` checkout, `GET /api/v1/catalog`
also hydrates intake metadata from `Project/workspaces/intake/index.json`,
including `admission_status`, `admission_level`, `provenance_status`, and
`intake_manifest`. The same catalog payload also carries `system_gap`, so
callers can render what still blocks each live contract without scraping plan
docs. Current admitted intake covers:

- `gray-box/pia/cifar10-ddpm`
  - current `provenance_status` is expected to hydrate as `workspace-verified`
- `white-box/gsa/ddpm-cifar10`
  - current `provenance_status` is expected to hydrate as `workspace-verified`

Current recon callers may still send legacy
top-level `artifact_dir` and `method`, and the service normalizes them into
`job_inputs` internally.

Internally, `Local-API` now keeps:

- a contract registry
- method execution profiles
- runtime executors

The registry currently exposes live contracts for:

- `black-box/recon/sd15-ddim`
- `gray-box/pia/cifar10-ddpm`
- `white-box/gsa/ddpm-cifar10`

Future lines are still admitted through explicit promotion gates rather than by
code presence alone. Those gates currently cover items such as:

- stable admitted `job_type` and runner support
- line-owned promoted asset roots
- summary hydration rules proven against non-smoke evidence
- asset-grade / provenance approval for live catalog exposure

Job observability note:

- the service now resolves an execution spec before running any job
- `local` mode executes the resolved command directly on the host
- `docker` mode wraps the same execution spec in a `docker run` command
- `GET /diagnostics` now reports `service_root`, `runners_root`, and per-runner script/dockerfile presence
- `GET /diagnostics` now also reports `registry_db_path`
- failure records still persist `command`, `output_capture`, and `output_tail`
- the current default error path still treats combined output as the
  authoritative fallback stream

## Environment Variables

- `DIFFAUDIT_LOCAL_API_HOST`
- `DIFFAUDIT_LOCAL_API_PORT`
- `DIFFAUDIT_LOCAL_API_SERVICE_ROOT`
- `DIFFAUDIT_LOCAL_API_REGISTRY_DB_PATH`
- `DIFFAUDIT_LOCAL_API_RUNNERS_ROOT`
- `DIFFAUDIT_LOCAL_API_PROJECT_ROOT`
- `DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT`
- `DIFFAUDIT_LOCAL_API_JOBS_ROOT`
- `DIFFAUDIT_LOCAL_API_EXECUTION_MODE`
- `DIFFAUDIT_LOCAL_API_DOCKER_BINARY`
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
  `go build ./cmd/local-api` check, plus runner Dockerfile builds.
- Tags matching `v*` run the same verification, build a Linux `amd64` release
  binary, generate a SHA256 checksum, attach both files to the GitHub Release,
  and validate the Local-API + runner Docker images with `docker build`.

Release flow:

```powershell
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag is the release trigger. This workflow is validation-only for now: it
does not publish a container image, but it now turns a tagged release into a
directly downloadable GitHub Release asset set:

- `local-api-linux-amd64`
- `local-api-linux-amd64.sha256`

Current release-consumption path:

1. Open the tagged GitHub Release page.
2. Download `local-api-linux-amd64`.
3. Download `local-api-linux-amd64.sha256`.
4. Verify the checksum before promoting the binary into your runtime path.

Example verification on Linux:

```bash
sha256sum -c local-api-linux-amd64.sha256
```

Current scope and limitation:

- the workflow now produces a consumable Linux `amd64` binary release
- the workflow still does not publish a container image to a registry
- Windows and macOS consumers still need to build from source unless a broader
  release matrix is added later

## License

This repository is licensed under the Apache License, Version 2.0.

See:

- `LICENSE`
- `THIRD_PARTY_NOTICES.md`

## Governance Boundary

- This repo is the source of truth for the Local API service code.
- External research execution and GPU scheduling are configured by paths, not embedded here.
- Keep client integrations via documented HTTP interfaces and base URL configuration.
