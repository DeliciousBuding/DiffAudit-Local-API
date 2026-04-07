"""Helpers for the reconstruction-based black-box attack line."""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

import numpy as np
import torch

from diffaudit.config import AuditConfig


RECON_WORKSPACE_FILES = (
    "inference.py",
    "cal_embedding.py",
    "test_accuracy.py",
    "train_text_to_image_lora.py",
)
RECON_RUNTIME_DATASET_KEYS = ("text", "image")
SUPPORTED_RECON_BACKENDS = {"stable_diffusion", "kandinsky_v22"}
SUPPORTED_RECON_SCHEDULERS = {"default", "ddim", "dpm_solver_multistep"}


@dataclass(frozen=True)
class ReconPlan:
    finetune_entrypoint: str
    inference_entrypoint: str
    embedding_entrypoint: str
    evaluation_entrypoint: str
    pretrained_model_name_or_path: str
    dataset_root: str
    model_dir: str
    num_validation_images: int
    inference_steps: int
    eval_method: str


def build_recon_plan(config: AuditConfig) -> ReconPlan:
    if config.attack.method != "recon":
        raise ValueError(f"Unsupported attack method for reconstruction plan: {config.attack.method}")
    if config.task.access_level != "black_box":
        raise ValueError("Reconstruction attack requires task.access_level = black_box")

    dataset_root = config.assets.dataset_root
    model_dir = config.assets.model_dir
    params = config.attack.parameters
    pretrained_model_name_or_path = str(params.get("pretrained_model_name_or_path", "")).strip()
    if not dataset_root:
        raise ValueError("Reconstruction attack requires assets.dataset_root")
    if not model_dir:
        raise ValueError("Reconstruction attack requires assets.model_dir")
    if not pretrained_model_name_or_path:
        raise ValueError("Reconstruction attack requires attack.parameters.pretrained_model_name_or_path")

    num_validation_images = int(params.get("num_validation_images", 3))
    inference_steps = int(params.get("inference_steps", 30))
    if num_validation_images <= 0:
        raise ValueError("Reconstruction attack requires attack.parameters.num_validation_images > 0")
    if inference_steps <= 0:
        raise ValueError("Reconstruction attack requires attack.parameters.inference_steps > 0")

    return ReconPlan(
        finetune_entrypoint="train_text_to_image_lora.py",
        inference_entrypoint="inference.py",
        embedding_entrypoint="cal_embedding.py",
        evaluation_entrypoint="test_accuracy.py",
        pretrained_model_name_or_path=pretrained_model_name_or_path,
        dataset_root=dataset_root,
        model_dir=model_dir,
        num_validation_images=num_validation_images,
        inference_steps=inference_steps,
        eval_method=str(params.get("eval_method", "threshold")),
    )


def validate_recon_workspace(workspace_dir: str | Path) -> dict[str, str]:
    workspace_path = Path(workspace_dir)
    missing = [
        relative_path
        for relative_path in RECON_WORKSPACE_FILES
        if not (workspace_path / relative_path).exists()
    ]
    if missing:
        raise FileNotFoundError(
            f"Reconstruction attack workspace is missing required files: {', '.join(missing)}"
        )
    return {
        "status": "ready",
        "workspace_dir": str(workspace_path),
        "inference_entrypoint": str(workspace_path / "inference.py"),
    }


def probe_recon_assets(
    dataset_root: str | Path,
    model_dir: str | Path,
    pretrained_model_name_or_path: str,
) -> dict[str, object]:
    dataset_path = Path(dataset_root)
    model_path = Path(model_dir)
    checks = {
        "dataset_root": dataset_path.exists(),
        "model_dir": model_path.exists(),
        "pretrained_model_name_or_path": bool(pretrained_model_name_or_path),
    }
    status = "ready" if all(checks.values()) else "blocked"
    paths = {
        "dataset_root": str(dataset_path),
        "model_dir": str(model_path),
        "pretrained_model_name_or_path": pretrained_model_name_or_path,
    }
    labels = {
        "dataset_root": "dataset_root",
        "model_dir": "model_dir",
        "pretrained_model_name_or_path": "pretrained model ref",
    }
    missing_keys = [name for name, is_ready in checks.items() if not is_ready]
    missing = [paths[name] or labels[name] for name in missing_keys]
    return {
        "status": status,
        "checks": checks,
        "paths": paths,
        "missing": missing,
        "missing_keys": missing_keys,
        "missing_items": [Path(item).name if Path(item).name else item for item in missing],
        "missing_description": " / ".join(labels[name] for name in missing_keys),
    }


def explain_recon_assets(config: AuditConfig) -> dict[str, object]:
    plan = build_recon_plan(config)
    summary = probe_recon_assets(
        dataset_root=plan.dataset_root,
        model_dir=plan.model_dir,
        pretrained_model_name_or_path=plan.pretrained_model_name_or_path,
    )
    return {
        **summary,
        "num_validation_images": plan.num_validation_images,
        "inference_steps": plan.inference_steps,
        "eval_method": plan.eval_method,
    }


def _load_recon_dataset_profile(dataset_path: str | Path) -> dict[str, object]:
    path = Path(dataset_path)
    profile: dict[str, object] = {
        "path": str(path),
        "exists": path.exists(),
        "keys": [],
        "sample_count": 0,
        "has_text": False,
        "has_image": False,
        "non_empty": False,
    }
    if not path.exists():
        return profile

    try:
        try:
            payload = torch.load(path, map_location="cpu", weights_only=False)
        except TypeError:
            payload = torch.load(path, map_location="cpu")
    except Exception as exc:
        profile["error"] = str(exc)
        return profile

    if not isinstance(payload, dict):
        profile["error"] = "dataset payload must be a dict saved by torch.save"
        return profile

    keys = sorted(str(key) for key in payload.keys())
    text_items = payload.get("text")
    image_items = payload.get("image")
    has_text = isinstance(text_items, (list, tuple))
    has_image = isinstance(image_items, (list, tuple))
    sample_count = min(len(text_items), len(image_items)) if has_text and has_image else 0
    profile.update(
        {
            "keys": keys,
            "has_text": has_text,
            "has_image": has_image,
            "sample_count": int(sample_count),
            "non_empty": bool(sample_count > 0),
        }
    )
    if has_text and has_image and len(text_items) != len(image_items):
        profile["error"] = "dataset text/image columns must have the same length"
    return profile


def probe_recon_runtime_assets(
    target_member_dataset: str | Path,
    target_nonmember_dataset: str | Path,
    shadow_member_dataset: str | Path,
    shadow_nonmember_dataset: str | Path,
    target_model_dir: str | Path,
    shadow_model_dir: str | Path,
    backend: str = "stable_diffusion",
    target_decoder_dir: str | Path | None = None,
    target_prior_dir: str | Path | None = None,
    shadow_decoder_dir: str | Path | None = None,
    shadow_prior_dir: str | Path | None = None,
    repo_root: str | Path = "external/Reconstruction-based-Attack",
) -> dict[str, object]:
    dataset_paths = {
        "target_member": Path(target_member_dataset),
        "target_non_member": Path(target_nonmember_dataset),
        "shadow_member": Path(shadow_member_dataset),
        "shadow_non_member": Path(shadow_nonmember_dataset),
    }
    dataset_profiles = {
        name: _load_recon_dataset_profile(path) for name, path in dataset_paths.items()
    }
    model_paths = {
        "target_model_dir": Path(target_model_dir),
        "shadow_model_dir": Path(shadow_model_dir),
    }

    checks = {
        "target_member_dataset": dataset_profiles["target_member"]["exists"],
        "target_non_member_dataset": dataset_profiles["target_non_member"]["exists"],
        "shadow_member_dataset": dataset_profiles["shadow_member"]["exists"],
        "shadow_non_member_dataset": dataset_profiles["shadow_non_member"]["exists"],
        "target_model_dir": model_paths["target_model_dir"].exists(),
        "shadow_model_dir": model_paths["shadow_model_dir"].exists(),
    }
    for name, profile in dataset_profiles.items():
        checks[f"{name}_has_text"] = bool(profile["has_text"])
        checks[f"{name}_has_image"] = bool(profile["has_image"])
        checks[f"{name}_non_empty"] = bool(profile["non_empty"])
        checks[f"{name}_valid_shape"] = "error" not in profile

    workspace_status = validate_recon_workspace(repo_root)
    checks["repo_workspace_ready"] = workspace_status["status"] == "ready"
    repo_path = Path(repo_root)
    if backend == "kandinsky_v22":
        decoder_prior_paths = {
            "target_decoder_dir": Path(target_decoder_dir) if target_decoder_dir else None,
            "target_prior_dir": Path(target_prior_dir) if target_prior_dir else None,
            "shadow_decoder_dir": Path(shadow_decoder_dir) if shadow_decoder_dir else None,
            "shadow_prior_dir": Path(shadow_prior_dir) if shadow_prior_dir else None,
        }
        for key, path in decoder_prior_paths.items():
            checks[key] = bool(path and path.exists())
        checks["kandinsky_entrypoint"] = (repo_path / "kandinsky2_2_inference.py").exists()
    else:
        decoder_prior_paths = {}

    labels = {
        "target_member_dataset": "target_member dataset",
        "target_non_member_dataset": "target_non_member dataset",
        "shadow_member_dataset": "shadow_member dataset",
        "shadow_non_member_dataset": "shadow_non_member dataset",
        "target_model_dir": "target_model_dir",
        "shadow_model_dir": "shadow_model_dir",
        "repo_workspace_ready": "recon repo workspace",
    }
    if backend == "kandinsky_v22":
        labels.update(
            {
                "target_decoder_dir": "target_decoder_dir",
                "target_prior_dir": "target_prior_dir",
                "shadow_decoder_dir": "shadow_decoder_dir",
                "shadow_prior_dir": "shadow_prior_dir",
                "kandinsky_entrypoint": "kandinsky entrypoint",
            }
        )
    for dataset_name in dataset_profiles:
        labels[f"{dataset_name}_has_text"] = f"{dataset_name} text column"
        labels[f"{dataset_name}_has_image"] = f"{dataset_name} image column"
        labels[f"{dataset_name}_non_empty"] = f"{dataset_name} sample count"
        labels[f"{dataset_name}_valid_shape"] = f"{dataset_name} dataset shape"

    paths = {
        "target_member_dataset": str(dataset_paths["target_member"]),
        "target_non_member_dataset": str(dataset_paths["target_non_member"]),
        "shadow_member_dataset": str(dataset_paths["shadow_member"]),
        "shadow_non_member_dataset": str(dataset_paths["shadow_non_member"]),
        "target_model_dir": str(model_paths["target_model_dir"]),
        "shadow_model_dir": str(model_paths["shadow_model_dir"]),
        "repo_root": str(Path(repo_root)),
    }
    if backend == "kandinsky_v22":
        for key, path in decoder_prior_paths.items():
            paths[key] = str(path) if path else ""
        paths["kandinsky_entrypoint"] = str(repo_path / "kandinsky2_2_inference.py")
    missing_keys = [name for name, is_ready in checks.items() if not is_ready]
    missing = [paths.get(name, labels[name]) for name in missing_keys]
    return {
        "status": "ready" if all(checks.values()) else "blocked",
        "checks": checks,
        "paths": paths,
        "dataset_profiles": dataset_profiles,
        "missing_keys": missing_keys,
        "missing": missing,
        "missing_items": [Path(item).name if Path(item).name else item for item in missing],
        "missing_description": " / ".join(labels[name] for name in missing_keys),
    }


def probe_recon_dry_run(
    config: AuditConfig,
    repo_root: str | Path,
) -> tuple[int, dict[str, object]]:
    try:
        plan = build_recon_plan(config)
        workspace = validate_recon_workspace(repo_root)
        summary = probe_recon_assets(
            dataset_root=plan.dataset_root,
            model_dir=plan.model_dir,
            pretrained_model_name_or_path=plan.pretrained_model_name_or_path,
        )
        if summary["status"] != "ready":
            return 1, {
                "status": "blocked",
                "repo_root": str(repo_root),
                **summary,
            }
        inference_text = (Path(repo_root) / "inference.py").read_text(encoding="utf-8")
        embedding_text = (Path(repo_root) / "cal_embedding.py").read_text(encoding="utf-8")
        evaluation_text = (Path(repo_root) / "test_accuracy.py").read_text(encoding="utf-8")
    except (FileNotFoundError, ValueError) as exc:
        return 1, {
            "status": "blocked",
            "error": str(exc),
            "repo_root": str(repo_root),
        }

    checks = {
        **summary["checks"],
        "workspace_files": True,
        "inference_has_pipeline": "StableDiffusionPipeline" in inference_text,
        "embedding_has_feature_extractor": (
            "DeiTFeatureExtractor" in embedding_text or "AutoImageProcessor" in embedding_text
        ),
        "evaluation_has_classifier": "DefineClassifier" in evaluation_text,
    }
    status = "ready" if all(checks.values()) else "blocked"
    labels = {
        "dataset_root": "dataset_root",
        "model_dir": "model_dir",
        "pretrained_model_name_or_path": "pretrained model ref",
        "workspace_files": "workspace files",
        "inference_has_pipeline": "StableDiffusionPipeline import",
        "embedding_has_feature_extractor": "feature extractor import",
        "evaluation_has_classifier": "classifier definition",
    }
    extra_paths = {
        "workspace_files": str(Path(repo_root)),
        "inference_has_pipeline": str(Path(repo_root) / "inference.py"),
        "embedding_has_feature_extractor": str(Path(repo_root) / "cal_embedding.py"),
        "evaluation_has_classifier": str(Path(repo_root) / "test_accuracy.py"),
    }
    missing_keys = [name for name, is_ready in checks.items() if not is_ready]
    missing = list(summary["missing"])
    for key in missing_keys:
        if key not in summary["missing_keys"]:
            missing.append(extra_paths[key])

    payload = {
        "status": status,
        "repo_root": str(Path(repo_root)),
        "entrypoint": workspace["inference_entrypoint"],
        "checks": checks,
        "paths": {
            **summary["paths"],
            "inference_entrypoint": str(Path(repo_root) / "inference.py"),
            "embedding_entrypoint": str(Path(repo_root) / "cal_embedding.py"),
            "evaluation_entrypoint": str(Path(repo_root) / "test_accuracy.py"),
        },
        "missing": missing,
        "missing_keys": missing_keys,
        "missing_items": [Path(item).name if Path(item).name else item for item in missing],
        "missing_description": " / ".join(labels[name] for name in missing_keys),
    }
    return (0 if status == "ready" else 1), payload


def _extract_recon_scores(score_file: str | Path) -> tuple[np.ndarray, np.ndarray]:
    try:
        payload = torch.load(score_file, map_location="cpu", weights_only=False)
    except TypeError:
        payload = torch.load(score_file, map_location="cpu")
    features = []
    labels = []
    for item in payload:
        feature = item[0]
        label = int(item[1])
        if isinstance(feature, list):
            features.append(float(feature[0]))
        else:
            features.append(float(feature))
        labels.append(label)
    return np.asarray(features, dtype=float), np.asarray(labels, dtype=int)


def _run_recon_subprocess(
    command: list[str],
    cwd: str | Path,
) -> dict[str, object]:
    completed = subprocess.run(
        command,
        cwd=str(Path(cwd)),
        capture_output=True,
        text=True,
        check=False,
    )
    return {
        "returncode": completed.returncode,
        "stdout_tail": completed.stdout.strip().splitlines()[-5:] if completed.stdout.strip() else [],
        "stderr_tail": completed.stderr.strip().splitlines()[-5:] if completed.stderr.strip() else [],
        "command": command,
    }


def _count_generated_images(generated_dir: str | Path) -> int:
    return len(list(Path(generated_dir).glob("image_*.jpg")))


def prepare_recon_public_subset(
    bundle_root: str | Path,
    output_dir: str | Path,
    target_count: int,
    shadow_count: int,
) -> dict[str, object]:
    bundle_path = Path(bundle_root)
    output_path = Path(output_dir)
    output_path.mkdir(parents=True, exist_ok=True)
    mapping = {
        "target_member": bundle_path / "source-datasets" / "partial-100-target" / "member" / "dataset.pkl",
        "target_non_member": bundle_path / "source-datasets" / "partial-100-target" / "non_member" / "dataset.pkl",
        "shadow_member_proxy": bundle_path / "source-datasets" / "100-target" / "non_member" / "dataset.pkl",
        "shadow_non_member": bundle_path / "source-datasets" / "100-shadow" / "non_member" / "dataset.pkl",
    }
    counts = {
        "target_member": int(target_count),
        "target_non_member": int(target_count),
        "shadow_member_proxy": int(shadow_count),
        "shadow_non_member": int(shadow_count),
    }
    paths: dict[str, str] = {}
    for name, src in mapping.items():
        profile = _load_recon_dataset_profile(src)
        if not profile["exists"] or "error" in profile:
            raise FileNotFoundError(f"Missing or unreadable public recon dataset: {src}")
        try:
            payload = torch.load(src, map_location="cpu", weights_only=False)
        except TypeError:
            payload = torch.load(src, map_location="cpu")
        subset = {
            "text": list(payload["text"][: counts[name]]),
            "image": list(payload["image"][: counts[name]]),
        }
        dst = output_path / f"{name}.pt"
        torch.save(subset, dst)
        paths[name] = str(dst)

    mapping_note = output_path / "mapping-note.md"
    mapping_note.write_text(
        "\n".join(
            [
                "# Recon Public Subset Mapping",
                "",
                "- target_member <- source-datasets/partial-100-target/member/dataset.pkl",
                "- target_non_member <- source-datasets/partial-100-target/non_member/dataset.pkl",
                "- shadow_non_member <- source-datasets/100-shadow/non_member/dataset.pkl",
                "- shadow_member_proxy <- source-datasets/100-target/non_member/dataset.pkl",
                "",
                "Note: shadow_member_proxy is an engineering proxy, not a fully validated paper-equivalent shadow-member split.",
            ]
        ),
        encoding="utf-8",
    )
    return {
        "status": "ready",
        "bundle_root": str(bundle_path),
        "output_dir": str(output_path),
        "paths": {
            **paths,
            "mapping_note": str(mapping_note),
        },
        "counts": counts,
        "notes": [
            "This helper materializes a runnable public subset from the current public recon asset bundle.",
            "shadow_member_proxy remains a proxy semantic mapping until better asset evidence is found.",
        ],
    }


def _threshold_metrics(member_scores: np.ndarray, nonmember_scores: np.ndarray) -> dict[str, float]:
    min_value = float(min(member_scores.min(), nonmember_scores.min()))
    max_value = float(max(member_scores.max(), nonmember_scores.max()))
    if min_value == max_value:
        return {
            "threshold": min_value,
            "asr": 0.5,
            "auc": 0.5,
            "tpr_at_1pct_fpr": 0.0,
        }

    thresholds = np.linspace(min_value, max_value, num=1000, endpoint=True)
    best_threshold = min_value
    best_asr = -1.0
    best_tpr_at_1pct = 0.0
    for threshold in thresholds:
        tp = float((member_scores > threshold).sum())
        tn = float((nonmember_scores <= threshold).sum())
        fp = float((nonmember_scores > threshold).sum())
        fn = float((member_scores <= threshold).sum())
        asr = (tp + tn) / (tp + tn + fp + fn)
        tpr = tp / (tp + fn) if tp + fn else 0.0
        fpr = fp / (fp + tn) if fp + tn else 0.0
        if asr > best_asr:
            best_asr = asr
            best_threshold = float(threshold)
        if fpr >= 0.01:
            best_tpr_at_1pct = float(tpr)
            break

    wins = (member_scores[:, None] > nonmember_scores[None, :]).astype(float)
    ties = (member_scores[:, None] == nonmember_scores[None, :]).astype(float) * 0.5
    auc = float((wins + ties).mean())
    return {
        "threshold": best_threshold,
        "asr": float(best_asr),
        "auc": auc,
        "tpr_at_1pct_fpr": best_tpr_at_1pct,
    }


def run_recon_eval_smoke(workspace: str | Path) -> dict[str, object]:
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    artifact_root = workspace_path / "synthetic-score-artifacts"
    artifact_root.mkdir(parents=True, exist_ok=True)
    score_payloads = {
        "target_member": [([0.90], 1), ([0.82], 1), ([0.88], 1), ([0.79], 1)],
        "target_non_member": [([0.11], 0), ([0.19], 0), ([0.23], 0), ([0.15], 0)],
        "shadow_member": [([0.86], 1), ([0.80], 1), ([0.84], 1), ([0.78], 1)],
        "shadow_non_member": [([0.17], 0), ([0.14], 0), ([0.20], 0), ([0.12], 0)],
    }
    score_paths = {}
    for name, payload in score_payloads.items():
        score_path = artifact_root / f"{name}.pt"
        torch.save(payload, score_path)
        score_paths[name] = str(score_path)

    shadow_member_scores, _ = _extract_recon_scores(score_paths["shadow_member"])
    shadow_nonmember_scores, _ = _extract_recon_scores(score_paths["shadow_non_member"])
    target_member_scores, _ = _extract_recon_scores(score_paths["target_member"])
    target_nonmember_scores, _ = _extract_recon_scores(score_paths["target_non_member"])
    shadow_metrics = _threshold_metrics(shadow_member_scores, shadow_nonmember_scores)
    target_metrics = _threshold_metrics(target_member_scores, target_nonmember_scores)

    result = {
        "status": "ready",
        "track": "black-box",
        "method": "recon",
        "paper": "BlackBox_Reconstruction_ArXiv2023",
        "mode": "eval-smoke",
        "device": "cpu",
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
        },
        "checks": {
            "synthetic_scores_created": True,
            "shadow_metrics_computed": True,
            "target_metrics_computed": True,
            "synthetic_artifacts_cleaned": True,
        },
        "metrics": {
            "shadow_auc": round(float(shadow_metrics["auc"]), 6),
            "shadow_asr": round(float(shadow_metrics["asr"]), 6),
            "target_auc": round(float(target_metrics["auc"]), 6),
            "target_asr": round(float(target_metrics["asr"]), 6),
            "tpr_at_1pct_fpr": round(float(target_metrics["tpr_at_1pct_fpr"]), 6),
            "auc": round(float(target_metrics["auc"]), 6),
        },
        "notes": [
            "Eval smoke uses synthetic score artifacts compatible with the upstream accuracy script shape.",
            "Smoke verifies artifact format and threshold-style black-box evaluation only.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    shutil.rmtree(artifact_root, ignore_errors=True)
    return result


def _write_synthetic_recon_score_artifacts(target_dir: str | Path) -> dict[str, str]:
    artifact_root = Path(target_dir)
    artifact_root.mkdir(parents=True, exist_ok=True)
    score_payloads = {
        "target_member": [([0.90], 1), ([0.82], 1), ([0.88], 1), ([0.79], 1)],
        "target_non_member": [([0.11], 0), ([0.19], 0), ([0.23], 0), ([0.15], 0)],
        "shadow_member": [([0.86], 1), ([0.80], 1), ([0.84], 1), ([0.78], 1)],
        "shadow_non_member": [([0.17], 0), ([0.14], 0), ([0.20], 0), ([0.12], 0)],
    }
    score_paths: dict[str, str] = {}
    for name, payload in score_payloads.items():
        score_path = artifact_root / f"{name}.pt"
        torch.save(payload, score_path)
        score_paths[name] = str(score_path.resolve())
    return score_paths


def probe_recon_score_artifacts(artifact_dir: str | Path) -> dict[str, object]:
    artifact_path = Path(artifact_dir)
    score_paths = {
        "target_member": artifact_path / "target_member.pt",
        "target_non_member": artifact_path / "target_non_member.pt",
        "shadow_member": artifact_path / "shadow_member.pt",
        "shadow_non_member": artifact_path / "shadow_non_member.pt",
    }
    checks = {name: path.exists() for name, path in score_paths.items()}
    checks["all_required_files_present"] = all(checks.values())
    missing_keys = [name for name, is_ready in checks.items() if name in score_paths and not is_ready]

    result: dict[str, object] = {
        "status": "blocked",
        "artifact_dir": str(artifact_path),
        "checks": checks,
        "paths": {name: str(path) for name, path in score_paths.items()},
        "missing_keys": missing_keys,
        "missing": [str(score_paths[name]) for name in missing_keys],
    }
    if not checks["all_required_files_present"]:
        return result

    sample_counts: dict[str, int] = {}
    label_profiles: dict[str, dict[str, int]] = {}
    binary_label_checks: dict[str, bool] = {}
    try:
        for name, path in score_paths.items():
            scores, labels = _extract_recon_scores(path)
            sample_counts[name] = int(len(scores))
            positive = int(labels.sum())
            negative = int(len(labels) - positive)
            label_profiles[name] = {
                "positive": positive,
                "negative": negative,
            }
            binary_label_checks[name] = bool(np.isin(labels, [0, 1]).all())
    except Exception as exc:
        result["error"] = str(exc)
        return result

    checks["all_non_empty"] = all(count > 0 for count in sample_counts.values())
    checks["all_binary_labels"] = all(binary_label_checks.values())
    result["sample_counts"] = sample_counts
    result["label_profiles"] = label_profiles
    result["status"] = "ready" if checks["all_non_empty"] and checks["all_binary_labels"] else "blocked"
    return result


def run_recon_runtime_mainline(
    target_member_dataset: str | Path,
    target_nonmember_dataset: str | Path,
    shadow_member_dataset: str | Path,
    shadow_nonmember_dataset: str | Path,
    target_model_dir: str | Path,
    shadow_model_dir: str | Path,
    workspace: str | Path,
    repo_root: str | Path = "external/Reconstruction-based-Attack",
    pretrained_model_name_or_path: str = "runwayml/stable-diffusion-v1-5",
    num_validation_images: int = 3,
    inference_steps: int = 30,
    gpu: int = 0,
    backend: str = "stable_diffusion",
    scheduler: str = "default",
    target_decoder_dir: str | Path | None = None,
    target_prior_dir: str | Path | None = None,
    shadow_decoder_dir: str | Path | None = None,
    shadow_prior_dir: str | Path | None = None,
    method: str = "threshold",
    similarity_method: str = "cosine",
    image_encoder: str = "deit",
) -> dict[str, object]:
    if backend not in SUPPORTED_RECON_BACKENDS:
        raise ValueError(f"Unsupported recon backend: {backend}")
    if scheduler not in SUPPORTED_RECON_SCHEDULERS:
        raise ValueError(f"Unsupported recon scheduler: {scheduler}")
    runtime_assets = probe_recon_runtime_assets(
        target_member_dataset=target_member_dataset,
        target_nonmember_dataset=target_nonmember_dataset,
        shadow_member_dataset=shadow_member_dataset,
        shadow_nonmember_dataset=shadow_nonmember_dataset,
        target_model_dir=target_model_dir,
        shadow_model_dir=shadow_model_dir,
        backend=backend,
        target_decoder_dir=target_decoder_dir,
        target_prior_dir=target_prior_dir,
        shadow_decoder_dir=shadow_decoder_dir,
        shadow_prior_dir=shadow_prior_dir,
        repo_root=repo_root,
    )
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    if runtime_assets["status"] != "ready":
        return {
            "status": "blocked",
            "track": "black-box",
            "method": "recon",
            "paper": "BlackBox_Reconstruction_ArXiv2023",
            "mode": "runtime-mainline",
            "workspace": str(workspace_path),
            "checks": {
                "runtime_assets_ready": False,
            },
            "runtime_assets": runtime_assets,
            "notes": [
                "Runtime mainline requires caption-known dataset payloads with both text and image columns.",
            ],
        }

    repo_path = Path(repo_root).resolve()
    score_root = workspace_path / "score-artifacts"
    score_root.mkdir(parents=True, exist_ok=True)
    image_root = workspace_path / "generated-images"
    jobs = {
        "target_member": {
            "dataset": Path(target_member_dataset),
            "model_dir": Path(target_model_dir),
            "membership": 1,
        },
        "target_non_member": {
            "dataset": Path(target_nonmember_dataset),
            "model_dir": Path(target_model_dir),
            "membership": 0,
        },
        "shadow_member": {
            "dataset": Path(shadow_member_dataset),
            "model_dir": Path(shadow_model_dir),
            "membership": 1,
        },
        "shadow_non_member": {
            "dataset": Path(shadow_nonmember_dataset),
            "model_dir": Path(shadow_model_dir),
            "membership": 0,
        },
    }
    artifacts: dict[str, object] = {}
    all_artifacts_generated = True
    for artifact_name, spec in jobs.items():
        generated_dir = image_root / artifact_name
        generated_dir.mkdir(parents=True, exist_ok=True)
        score_path = score_root / f"{artifact_name}.pt"
        if score_path.exists():
            score_values, score_labels = _extract_recon_scores(score_path)
            artifacts[artifact_name] = {
                "status": "ready",
                "stage": "reused",
                "dataset_path": str(Path(spec["dataset"]).resolve()),
                "model_dir": str(Path(spec["model_dir"]).resolve()),
                "membership": int(spec["membership"]),
                "generated_dir": str(generated_dir.resolve()),
                "score_path": str(score_path.resolve()),
                "sample_count": int(len(score_values)),
                "label_sum": int(score_labels.sum()),
            }
            continue
        if backend == "stable_diffusion":
            inference_command = [
                sys.executable,
                str((repo_path / "inference.py").resolve()),
                "--pretrained_model_name_or_path",
                pretrained_model_name_or_path,
                "--num_validation_images",
                str(num_validation_images),
                "--inference",
                str(inference_steps),
                "--output_dir",
                str(Path(spec["model_dir"]).resolve()),
                "--data_dir",
                str(Path(spec["dataset"]).resolve()),
                "--save_dir",
                str(generated_dir.resolve()),
            ]
            inference_command.extend(["--scheduler", scheduler])
        else:
            decoder_path = target_decoder_dir if artifact_name.startswith("target_") else shadow_decoder_dir
            prior_path = target_prior_dir if artifact_name.startswith("target_") else shadow_prior_dir
            inference_command = [
                sys.executable,
                str((repo_path / "kandinsky2_2_inference.py").resolve()),
                "--decoder_dir",
                str(Path(decoder_path).resolve()),
                "--prior_dir",
                str(Path(prior_path).resolve()),
                "--save_dir",
                str(generated_dir.resolve()),
                "--gpu",
                str(gpu),
                "--dataset_dir",
                str(Path(spec["dataset"]).resolve()),
                "--num_validation_images",
                str(num_validation_images),
                "--inference_steps",
                str(inference_steps),
            ]
        expected_generated_images = (
            int(runtime_assets["dataset_profiles"][artifact_name]["sample_count"]) * num_validation_images
        )
        inference_reused = _count_generated_images(generated_dir) >= expected_generated_images
        if inference_reused:
            inference_result = {
                "returncode": 0,
                "stdout_tail": [],
                "stderr_tail": [],
                "command": inference_command,
                "stage": "reused",
            }
        else:
            inference_result = _run_recon_subprocess(inference_command, cwd=repo_path)
        if inference_result["returncode"] != 0:
            all_artifacts_generated = False
            artifacts[artifact_name] = {
                "status": "error",
                "stage": "inference",
                **inference_result,
            }
            continue

        embedding_command = [
            sys.executable,
            str((repo_path / "cal_embedding.py").resolve()),
            "--data_dir",
            str(Path(spec["dataset"]).resolve()),
            "--sample_file",
            str(generated_dir.resolve()),
            "--membership",
            str(spec["membership"]),
            "--img_num",
            str(num_validation_images),
            "--gpu",
            str(gpu),
            "--save_dir",
            str(score_path.resolve()),
            "--method",
            similarity_method,
            "--image_encoder",
            image_encoder,
        ]
        embedding_result = _run_recon_subprocess(embedding_command, cwd=repo_path)
        if embedding_result["returncode"] != 0:
            all_artifacts_generated = False
            artifacts[artifact_name] = {
                "status": "error",
                "stage": "embedding",
                **embedding_result,
            }
            continue

        score_values, score_labels = _extract_recon_scores(score_path)
        artifacts[artifact_name] = {
            "status": "ready",
            "stage": "generated",
            "dataset_path": str(Path(spec["dataset"]).resolve()),
            "model_dir": str(Path(spec["model_dir"]).resolve()),
            "membership": int(spec["membership"]),
            "generated_dir": str(generated_dir.resolve()),
            "score_path": str(score_path.resolve()),
            "sample_count": int(len(score_values)),
            "label_sum": int(score_labels.sum()),
            "inference": inference_result,
            "embedding": embedding_result,
        }

    artifact_probe = probe_recon_score_artifacts(score_root)
    if all_artifacts_generated and artifact_probe["status"] == "ready":
        artifact_mainline = run_recon_artifact_mainline(
            artifact_dir=score_root,
            workspace=workspace_path / "artifact-mainline",
            repo_root=repo_root,
            method=method,
        )
    else:
        artifact_mainline = {
            "status": "blocked",
            "mode": "artifact-mainline",
        }

    checks = {
        "runtime_assets_ready": runtime_assets["status"] == "ready",
        "all_artifacts_generated": all_artifacts_generated,
        "artifact_probe_ready": artifact_probe["status"] == "ready",
        "artifact_mainline_ready": artifact_mainline["status"] == "ready",
    }
    result = {
        "status": "ready" if all(checks.values()) else "error",
        "track": "black-box",
        "method": "recon",
        "paper": "BlackBox_Reconstruction_ArXiv2023",
        "mode": "runtime-mainline",
        "device": f"cuda:{gpu}" if torch.cuda.is_available() else "cpu",
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
            "score_artifact_dir": str(score_root.resolve()),
            "artifact_mainline_summary": str(workspace_path / "artifact-mainline" / "summary.json"),
        },
        "checks": checks,
        "runtime_assets": runtime_assets,
        "artifacts": artifacts,
        "stages": {
            "artifact_probe": artifact_probe,
            "artifact_mainline": {
                "status": artifact_mainline["status"],
                "mode": artifact_mainline.get("mode", "artifact-mainline"),
                "summary_path": artifact_mainline.get("artifact_paths", {}).get("summary"),
            },
        },
        "evaluation_method": method,
        "runtime": {
            "backend": backend,
            "scheduler": scheduler,
            "pretrained_model_name_or_path": pretrained_model_name_or_path,
            "target_decoder_dir": str(Path(target_decoder_dir).resolve()) if target_decoder_dir else None,
            "target_prior_dir": str(Path(target_prior_dir).resolve()) if target_prior_dir else None,
            "shadow_decoder_dir": str(Path(shadow_decoder_dir).resolve()) if shadow_decoder_dir else None,
            "shadow_prior_dir": str(Path(shadow_prior_dir).resolve()) if shadow_prior_dir else None,
            "num_validation_images": num_validation_images,
            "inference_steps": inference_steps,
            "gpu": gpu,
        },
        "metrics": artifact_mainline.get("metrics", {}),
        "notes": [
            "Runtime mainline bridges caption-known reconstruction datasets to repository artifact and evaluation summaries.",
            "This path assumes dataset payloads already contain both text and image columns and does not fine-tune BLIP captioning models.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    return result


def summarize_recon_artifacts(
    artifact_dir: str | Path,
    workspace: str | Path,
) -> dict[str, object]:
    artifact_path = Path(artifact_dir)
    score_paths = {
        "target_member": artifact_path / "target_member.pt",
        "target_non_member": artifact_path / "target_non_member.pt",
        "shadow_member": artifact_path / "shadow_member.pt",
        "shadow_non_member": artifact_path / "shadow_non_member.pt",
    }
    for name, score_path in score_paths.items():
        if not score_path.exists():
            raise FileNotFoundError(f"Missing reconstruction artifact: {name} -> {score_path}")

    shadow_member_scores, _ = _extract_recon_scores(score_paths["shadow_member"])
    shadow_nonmember_scores, _ = _extract_recon_scores(score_paths["shadow_non_member"])
    target_member_scores, _ = _extract_recon_scores(score_paths["target_member"])
    target_nonmember_scores, _ = _extract_recon_scores(score_paths["target_non_member"])
    shadow_metrics = _threshold_metrics(shadow_member_scores, shadow_nonmember_scores)
    target_metrics = _threshold_metrics(target_member_scores, target_nonmember_scores)

    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    result = {
        "status": "ready",
        "track": "black-box",
        "method": "recon",
        "paper": "BlackBox_Reconstruction_ArXiv2023",
        "mode": "artifact-summary",
        "device": "cpu",
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
        },
        "assets": {
            "artifact_files": sorted(score_paths.keys()),
        },
        "checks": {
            "target_member_loaded": True,
            "target_non_member_loaded": True,
            "shadow_member_loaded": True,
            "shadow_non_member_loaded": True,
            "threshold_metrics_computed": True,
        },
        "metrics": {
            "shadow_auc": round(float(shadow_metrics["auc"]), 6),
            "shadow_asr": round(float(shadow_metrics["asr"]), 6),
            "shadow_threshold": round(float(shadow_metrics["threshold"]), 6),
            "target_auc": round(float(target_metrics["auc"]), 6),
            "target_asr": round(float(target_metrics["asr"]), 6),
            "target_threshold": round(float(target_metrics["threshold"]), 6),
            "tpr_at_1pct_fpr": round(float(target_metrics["tpr_at_1pct_fpr"]), 6),
            "auc": round(float(target_metrics["auc"]), 6),
        },
        "notes": [
            "Summary is recomputed from reconstruction score artifacts compatible with the upstream accuracy script.",
            "Current implementation reproduces the threshold-style path, not the neural classifier training loop.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    return result


def _parse_recon_eval_stdout(stdout: str) -> dict[str, float]:
    accuracy_match = re.search(r"Accuracy(?: with Best Threshold)?:\s*([0-9.]+)", stdout)
    auc_match = re.search(r"AUC-ROC:\s*([0-9.]+)", stdout)
    return {
        "reported_accuracy": float(accuracy_match.group(1)) if accuracy_match else None,
        "reported_auc": float(auc_match.group(1)) if auc_match else None,
    }


def _headline_metrics(payload: dict[str, object]) -> dict[str, float | None]:
    metrics = payload.get("metrics", {})
    if not isinstance(metrics, dict):
        return {"auc": None, "asr": None, "tpr_at_1pct_fpr": None}
    return {
        "auc": metrics.get("reported_auc", metrics.get("auc")),
        "asr": metrics.get("reported_accuracy", metrics.get("asr")),
        "tpr_at_1pct_fpr": metrics.get("tpr_at_1pct_fpr"),
    }


def run_recon_upstream_eval(
    artifact_dir: str | Path,
    workspace: str | Path,
    repo_root: str | Path = "external/Reconstruction-based-Attack",
    method: str = "threshold",
    mode: str = "upstream-eval",
) -> dict[str, object]:
    artifact_path = Path(artifact_dir)
    score_paths = {
        "target_member": artifact_path / "target_member.pt",
        "target_non_member": artifact_path / "target_non_member.pt",
        "shadow_member": artifact_path / "shadow_member.pt",
        "shadow_non_member": artifact_path / "shadow_non_member.pt",
    }
    for name, score_path in score_paths.items():
        if not score_path.exists():
            raise FileNotFoundError(f"Missing reconstruction artifact: {name} -> {score_path}")

    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    repo_path = Path(repo_root).resolve()
    script_path = repo_path / "test_accuracy.py"
    if not script_path.exists():
        raise FileNotFoundError(f"Reconstruction attack evaluation script not found: {script_path}")

    command = [
        sys.executable,
        str(script_path),
        "--target_member_dir",
        str(score_paths["target_member"].resolve()),
        "--target_non_member_dir",
        str(score_paths["target_non_member"].resolve()),
        "--shadow_member_dir",
        str(score_paths["shadow_member"].resolve()),
        "--shadow_non_member_dir",
        str(score_paths["shadow_non_member"].resolve()),
        "--method",
        method,
    ]
    completed = subprocess.run(
        command,
        cwd=str(repo_path),
        capture_output=True,
        text=True,
        check=False,
    )
    parsed = _parse_recon_eval_stdout(completed.stdout)
    status = "ready" if completed.returncode == 0 else "error"
    result = {
        "status": status,
        "track": "black-box",
        "method": "recon",
        "paper": "BlackBox_Reconstruction_ArXiv2023",
        "mode": mode,
        "device": "cpu",
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
        },
        "assets": {
            "artifact_files": sorted(score_paths.keys()),
        },
        "checks": {
            "artifact_files_found": True,
            "upstream_script_found": True,
            "upstream_script_succeeded": completed.returncode == 0,
        },
        "evaluation_method": method,
        "metrics": parsed,
        "notes": [
            "Evaluation executes the upstream reconstruction accuracy script with provided score artifacts.",
        ],
        "stdout_tail": completed.stdout.strip().splitlines()[-5:],
    }
    if completed.stderr.strip():
        result["stderr_tail"] = completed.stderr.strip().splitlines()[-5:]
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    return result


def run_recon_upstream_eval_smoke(
    workspace: str | Path,
    repo_root: str | Path = "external/Reconstruction-based-Attack",
    method: str = "threshold",
) -> dict[str, object]:
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    artifact_root = workspace_path / "synthetic-score-artifacts"
    _write_synthetic_recon_score_artifacts(artifact_root)
    result = run_recon_upstream_eval(
        artifact_dir=artifact_root,
        workspace=workspace_path,
        repo_root=repo_root,
        method=method,
        mode="upstream-eval-smoke",
    )
    result["checks"] = {
        "synthetic_scores_created": True,
        "upstream_script_found": result["checks"]["upstream_script_found"],
        "upstream_script_succeeded": result["checks"]["upstream_script_succeeded"],
        "synthetic_assets_cleaned": True,
    }
    result["notes"] = [
        "Smoke executes the upstream reconstruction evaluation script with synthetic score artifacts.",
        "Current workflow is intended to validate CLI compatibility before real artifacts are available.",
    ]
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    shutil.rmtree(artifact_root, ignore_errors=True)
    return result


def run_recon_mainline_smoke(
    workspace: str | Path,
    repo_root: str | Path = "external/Reconstruction-based-Attack",
    method: str = "threshold",
) -> dict[str, object]:
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)

    eval_payload = run_recon_eval_smoke(workspace_path / "eval-smoke")
    artifact_root = workspace_path / "synthetic-score-artifacts"
    score_paths = _write_synthetic_recon_score_artifacts(artifact_root)
    artifact_payload = summarize_recon_artifacts(
        artifact_dir=artifact_root,
        workspace=workspace_path / "artifact-summary",
    )
    upstream_payload = run_recon_upstream_eval_smoke(
        workspace=workspace_path / "upstream-eval-smoke",
        repo_root=repo_root,
        method=method,
    )
    shutil.rmtree(artifact_root, ignore_errors=True)

    stages = {
        "eval_smoke": {
            "status": eval_payload["status"],
            "mode": eval_payload["mode"],
            "summary_path": eval_payload["artifact_paths"]["summary"],
            "headline_metrics": _headline_metrics(eval_payload),
        },
        "artifact_summary": {
            "status": artifact_payload["status"],
            "mode": artifact_payload["mode"],
            "summary_path": artifact_payload["artifact_paths"]["summary"],
            "headline_metrics": _headline_metrics(artifact_payload),
            "artifact_files": sorted(Path(path).name for path in score_paths.values()),
        },
        "upstream_eval_smoke": {
            "status": upstream_payload["status"],
            "mode": upstream_payload["mode"],
            "summary_path": upstream_payload["artifact_paths"]["summary"],
            "headline_metrics": _headline_metrics(upstream_payload),
        },
    }
    checks = {
        "eval_smoke_ready": eval_payload["status"] == "ready",
        "artifact_summary_ready": artifact_payload["status"] == "ready",
        "upstream_eval_smoke_ready": upstream_payload["status"] == "ready",
        "all_stages_ready": all(stage["status"] == "ready" for stage in stages.values()),
    }
    result = {
        "status": "ready" if checks["all_stages_ready"] else "error",
        "track": "black-box",
        "method": "recon",
        "paper": "BlackBox_Reconstruction_ArXiv2023",
        "mode": "mainline-smoke",
        "device": "cpu",
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
            "eval_smoke_summary": eval_payload["artifact_paths"]["summary"],
            "artifact_summary": artifact_payload["artifact_paths"]["summary"],
            "upstream_eval_smoke_summary": upstream_payload["artifact_paths"]["summary"],
        },
        "checks": checks,
        "evaluation_method": method,
        "metrics": {
            "auc": _headline_metrics(upstream_payload)["auc"],
            "asr": _headline_metrics(upstream_payload)["asr"],
            "tpr_at_1pct_fpr": _headline_metrics(artifact_payload)["tpr_at_1pct_fpr"],
        },
        "stages": stages,
        "notes": [
            "Mainline smoke orchestrates the reconstruction black-box path into one machine-readable workspace.",
            "Current mainline remains synthetic and is intended to stabilize the stage contract before real artifacts are attached.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    return result


def run_recon_artifact_mainline(
    artifact_dir: str | Path,
    workspace: str | Path,
    repo_root: str | Path = "external/Reconstruction-based-Attack",
    method: str = "threshold",
) -> dict[str, object]:
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)

    artifact_payload = summarize_recon_artifacts(
        artifact_dir=artifact_dir,
        workspace=workspace_path / "artifact-summary",
    )
    upstream_payload = run_recon_upstream_eval(
        artifact_dir=artifact_dir,
        workspace=workspace_path / "upstream-eval",
        repo_root=repo_root,
        method=method,
        mode="upstream-eval",
    )
    stages = {
        "artifact_summary": {
            "status": artifact_payload["status"],
            "mode": artifact_payload["mode"],
            "summary_path": artifact_payload["artifact_paths"]["summary"],
            "headline_metrics": _headline_metrics(artifact_payload),
        },
        "upstream_eval": {
            "status": upstream_payload["status"],
            "mode": upstream_payload["mode"],
            "summary_path": upstream_payload["artifact_paths"]["summary"],
            "headline_metrics": _headline_metrics(upstream_payload),
        },
    }
    checks = {
        "artifact_summary_ready": artifact_payload["status"] == "ready",
        "upstream_eval_ready": upstream_payload["status"] == "ready",
        "all_stages_ready": all(stage["status"] == "ready" for stage in stages.values()),
    }
    result = {
        "status": "ready" if checks["all_stages_ready"] else "error",
        "track": "black-box",
        "method": "recon",
        "paper": "BlackBox_Reconstruction_ArXiv2023",
        "mode": "artifact-mainline",
        "device": "cpu",
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
            "artifact_summary": artifact_payload["artifact_paths"]["summary"],
            "upstream_eval_summary": upstream_payload["artifact_paths"]["summary"],
        },
        "asset_source_dir": str(Path(artifact_dir)),
        "checks": checks,
        "evaluation_method": method,
        "metrics": {
            "auc": _headline_metrics(upstream_payload)["auc"],
            "asr": _headline_metrics(upstream_payload)["asr"],
            "tpr_at_1pct_fpr": _headline_metrics(artifact_payload)["tpr_at_1pct_fpr"],
        },
        "stages": stages,
        "notes": [
            "Artifact mainline consumes provided target/shadow reconstruction score artifacts.",
            "This path is intended as the shortest bridge from repository smoke workflows to real black-box evidence.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    return result
