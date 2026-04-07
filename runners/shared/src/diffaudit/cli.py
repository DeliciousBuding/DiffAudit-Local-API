from __future__ import annotations

import argparse
import json

from diffaudit.attacks.gsa import run_gsa_runtime_mainline
from diffaudit.attacks.pia_adapter import run_pia_runtime_mainline
from diffaudit.attacks.recon import run_recon_artifact_mainline, run_recon_runtime_mainline
from diffaudit.config import load_audit_config


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="diffaudit-runner")
    subparsers = parser.add_subparsers(dest="command", required=True)

    recon_artifact = subparsers.add_parser("run-recon-artifact-mainline")
    recon_artifact.add_argument("--artifact-dir", required=True)
    recon_artifact.add_argument("--workspace", required=True)
    recon_artifact.add_argument("--repo-root", required=True)
    recon_artifact.add_argument("--method", default="threshold")

    recon_runtime = subparsers.add_parser("run-recon-runtime-mainline")
    recon_runtime.add_argument("--workspace", required=True)
    recon_runtime.add_argument("--repo-root", required=True)
    recon_runtime.add_argument("--target-member-dataset", required=True)
    recon_runtime.add_argument("--target-nonmember-dataset", required=True)
    recon_runtime.add_argument("--shadow-member-dataset", required=True)
    recon_runtime.add_argument("--shadow-nonmember-dataset", required=True)
    recon_runtime.add_argument("--target-model-dir", required=True)
    recon_runtime.add_argument("--shadow-model-dir", required=True)
    recon_runtime.add_argument("--backend", default="stable_diffusion")
    recon_runtime.add_argument("--scheduler", default="default")
    recon_runtime.add_argument("--method", default="threshold")

    pia_runtime = subparsers.add_parser("run-pia-runtime-mainline")
    pia_runtime.add_argument("--config", required=True)
    pia_runtime.add_argument("--workspace", required=True)
    pia_runtime.add_argument("--repo-root", required=True)
    pia_runtime.add_argument("--member-split-root", required=True)
    pia_runtime.add_argument("--device", default="cpu")
    pia_runtime.add_argument("--max-samples", type=int, default=None)
    pia_runtime.add_argument("--batch-size", type=int, default=64)
    pia_runtime.add_argument("--stochastic-dropout-defense", action="store_true")
    pia_runtime.add_argument("--provenance-status", default="source-retained-unverified")

    gsa_runtime = subparsers.add_parser("run-gsa-runtime-mainline")
    gsa_runtime.add_argument("--workspace", required=True)
    gsa_runtime.add_argument("--assets-root", required=True)
    gsa_runtime.add_argument("--repo-root", required=True)
    gsa_runtime.add_argument("--resolution", type=int, default=32)
    gsa_runtime.add_argument("--ddpm-num-steps", type=int, default=20)
    gsa_runtime.add_argument("--sampling-frequency", type=int, default=2)
    gsa_runtime.add_argument("--attack-method", type=int, default=1)
    gsa_runtime.add_argument("--prediction-type", default="epsilon")
    gsa_runtime.add_argument("--provenance-status", default="workspace-verified")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)

    if args.command == "run-recon-artifact-mainline":
        payload = run_recon_artifact_mainline(
            artifact_dir=args.artifact_dir,
            workspace=args.workspace,
            repo_root=args.repo_root,
            method=args.method,
        )
        print(json.dumps(payload, indent=2, ensure_ascii=True))
        return 0 if payload.get("status") == "ready" else 1

    if args.command == "run-recon-runtime-mainline":
        payload = run_recon_runtime_mainline(
            workspace=args.workspace,
            repo_root=args.repo_root,
            target_member_dataset=args.target_member_dataset,
            target_nonmember_dataset=args.target_nonmember_dataset,
            shadow_member_dataset=args.shadow_member_dataset,
            shadow_nonmember_dataset=args.shadow_nonmember_dataset,
            target_model_dir=args.target_model_dir,
            shadow_model_dir=args.shadow_model_dir,
            backend=args.backend,
            scheduler=args.scheduler,
            method=args.method,
        )
        print(json.dumps(payload, indent=2, ensure_ascii=True))
        return 0 if payload.get("status") == "ready" else 1

    if args.command == "run-pia-runtime-mainline":
        config = load_audit_config(args.config)
        payload = run_pia_runtime_mainline(
            config,
            workspace=args.workspace,
            repo_root=args.repo_root,
            member_split_root=args.member_split_root,
            device=args.device,
            max_samples=args.max_samples,
            batch_size=args.batch_size,
            stochastic_dropout_defense=args.stochastic_dropout_defense,
            provenance_status=args.provenance_status,
        )
        print(json.dumps(payload, indent=2, ensure_ascii=True))
        return 0 if payload.get("status") == "ready" else 1

    if args.command == "run-gsa-runtime-mainline":
        payload = run_gsa_runtime_mainline(
            workspace=args.workspace,
            assets_root=args.assets_root,
            repo_root=args.repo_root,
            resolution=args.resolution,
            ddpm_num_steps=args.ddpm_num_steps,
            sampling_frequency=args.sampling_frequency,
            attack_method=args.attack_method,
            prediction_type=args.prediction_type,
            provenance_status=args.provenance_status,
        )
        print(json.dumps(payload, indent=2, ensure_ascii=True))
        return 0 if payload.get("status") == "ready" else 1

    raise SystemExit(2)
