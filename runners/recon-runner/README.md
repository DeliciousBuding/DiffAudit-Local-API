# Recon Artifact Runner

Minimal runner that satisfies the black-box recon artifact contract.

## Local execution

1. Place artifacts under some directory, e.g. `D:/data/recon-artifacts`.
2. Run the module directly:

```powershell
python -m diffaudit_runner run-recon-artifact-mainline `
  --artifact-dir D:/data/recon-artifacts `
  --workspace D:/local-api/jobs/recon-artifact-001 `
  --repo-root D:/Code/DiffAudit/Project `
  --method threshold
```

The runner creates the workspace with `summary.json` containing metadata and simple metrics.

## Build and run via Docker

```powershell
docker build -t diffaudit/recon-runner:latest .
docker run --rm `
  -v D:/data/recon-artifacts:/job/artifacts:ro `
  -v D:/local-api/jobs/recon-artifact-001:/job/output `
  diffaudit/recon-runner:latest run-recon-artifact-mainline `
    --artifact-dir /job/artifacts `
    --workspace /job/output `
    --repo-root /workspace/project `
    --method threshold
```

The image only bundles this runner and uses the Python 3.11 slim base.
