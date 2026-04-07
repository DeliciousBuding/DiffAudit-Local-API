"""Entrypoint for the recon artifact runner."""

from __future__ import annotations

import argparse
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path

CONTRACT_KEY = "black-box/recon/sd15-ddim"
JOB_TYPE = "recon_artifact_mainline"


def _hash_samples(file_names: list[str], limit: int = 5) -> list[str]:
    return [
        f"{name}:{hashlib.sha256(name.encode('utf-8')).hexdigest()[:16]}"
        for name in file_names[:limit]
    ]


def _collect_metrics(artifact_dir: Path) -> dict[str, list[str] | int]:
    files = sorted(entry.name for entry in artifact_dir.iterdir() if entry.is_file())
    return {
        "num_artifacts": len(files),
        "sample_hashes": _hash_samples(files),
    }


def run_recon_artifact_mainline(args: argparse.Namespace) -> None:
    artifact_dir = Path(args.artifact_dir)
    if not artifact_dir.exists() or not artifact_dir.is_dir():
        raise SystemExit(f"{artifact_dir} is not a readable directory")

    workspace_root = Path(args.workspace)
    workspace_root.mkdir(parents=True, exist_ok=True)
    summary_path = workspace_root / "summary.json"

    metrics = _collect_metrics(artifact_dir)
    payload = {
        "contract_key": CONTRACT_KEY,
        "job_type": JOB_TYPE,
        "method": args.method,
        "artifact_dir": str(artifact_dir),
        "repo_root": str(Path(args.repo_root)),
        "metrics": metrics,
        "created_at": datetime.now(timezone.utc).isoformat(),
    }

    print(
        "Recon artifact runner:",
        f"artifacts={metrics['num_artifacts']}",
        f"workspace={workspace_root}",
        f"method={args.method}",
    )

    with summary_path.open("w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=2)

    print(f"summary written to {summary_path}")


def run_recon_runtime_mainline(args: argparse.Namespace) -> None:
    raise SystemExit("run-recon-runtime-mainline is not implemented for this runner")


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(prog="run.py")
    subparsers = parser.add_subparsers(dest="command", required=True)

    artifact_parser = subparsers.add_parser(
        "run-recon-artifact-mainline", help="Produce a summary for artifact jobs"
    )
    artifact_parser.add_argument("--artifact-dir", required=True)
    artifact_parser.add_argument("--workspace", required=True)
    artifact_parser.add_argument("--repo-root", required=True)
    artifact_parser.add_argument("--method", required=True)
    artifact_parser.set_defaults(func=run_recon_artifact_mainline)

    runtime_parser = subparsers.add_parser(
        "run-recon-runtime-mainline", help="Not implemented yet"
    )
    runtime_parser.set_defaults(func=run_recon_runtime_mainline)

    return parser.parse_args()


def main() -> None:
    args = _parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
