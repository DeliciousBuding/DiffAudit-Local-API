"""Simple runner that produces a summary for black-box recon artifact jobs."""

from __future__ import annotations

import argparse
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path


CONTRACT_KEY = "black-box/recon/sd15-ddim"
JOB_TYPE = "recon_artifact_mainline"


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(prog="diffaudit_runner")
    subparsers = parser.add_subparsers(dest="command", required=True)
    recon_parser = subparsers.add_parser("run-recon-artifact-mainline")
    recon_parser.add_argument("--artifact-dir", required=True)
    recon_parser.add_argument("--workspace", required=True)
    recon_parser.add_argument("--repo-root", required=True)
    recon_parser.add_argument("--method", required=True)
    return parser.parse_args()


def _hash_samples(files: list[str], limit: int = 5) -> list[str]:
    hashes: list[str] = []
    for file_path in files[:limit]:
        name = Path(file_path).name
        digest = hashlib.sha256(name.encode("utf-8")).hexdigest()
        hashes.append(f"{name}:{digest[:16]}")
    return hashes


def _collect_metrics(artifact_dir: Path) -> dict[str, int | list[str]]:
    entries = sorted(t.name for t in artifact_dir.iterdir() if t.is_file())
    return {
        "num_artifacts": len(entries),
        "sample_hashes": _hash_samples(entries),
    }


def run_recon_artifact_mainline(args: argparse.Namespace) -> None:
    artifact_dir = Path(args.artifact_dir)
    if not artifact_dir.exists():
        raise SystemExit(f"artifact dir {artifact_dir} does not exist")
    if not artifact_dir.is_dir():
        raise SystemExit(f"artifact dir {artifact_dir} is not a directory")

    workspace = Path(args.workspace)
    workspace.mkdir(parents=True, exist_ok=True)
    summary_path = workspace / "summary.json"
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
        f"workspace={workspace}",
        f"method={args.method}",
    )
    with summary_path.open("w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=2)
    print(f"summary written to {summary_path}")


def main() -> None:
    args = _parse_args()
    if args.command == "run-recon-artifact-mainline":
        run_recon_artifact_mainline(args)


if __name__ == "__main__":
    main()
