"""Planning helpers for integrating the official PIA DDPM attack flow."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from diffaudit.config import AuditConfig


@dataclass(frozen=True)
class PiaPlan:
    entrypoint: str
    dataset: str
    data_root: str
    model_dir: str
    num_samples: int
    attacker_name: str
    attack_num: int
    interval: int
    batch_size: int


REQUIRED_PIA_WORKSPACE_FILES = (
    "DDPM/attack.py",
    "DDPM/components.py",
    "DDPM/dataset_utils.py",
    "DDPM/model.py",
)

PIA_MEMBER_SPLIT_FILENAMES = {
    "CIFAR10": "CIFAR10_train_ratio0.5.npz",
    "TINY-IN": "TINY-IN_train_ratio0.5.npz",
}

PIA_DATASET_LAYOUTS = {
    "CIFAR10": "cifar10",
    "TINY-IN": "tiny-imagenet-200",
}


def build_pia_plan(config: AuditConfig) -> PiaPlan:
    if config.attack.method != "pia":
        raise ValueError(f"Unsupported attack method for PIA plan: {config.attack.method}")
    if config.task.access_level != "semi_white_box":
        raise ValueError("PIA currently requires task.access_level = semi_white_box")

    dataset = config.assets.dataset_name
    data_root = config.assets.dataset_root
    model_dir = config.assets.model_dir
    params = config.attack.parameters

    if not dataset:
        raise ValueError("PIA requires assets.dataset_name")
    if not data_root:
        raise ValueError("PIA requires assets.dataset_root")
    if not model_dir:
        raise ValueError("PIA requires assets.model_dir")
    if "attacker_name" not in params:
        raise ValueError("PIA requires attack.parameters.attacker_name")
    if "attack_num" not in params:
        raise ValueError("PIA requires attack.parameters.attack_num")
    if "interval" not in params:
        raise ValueError("PIA requires attack.parameters.interval")

    attacker_name = str(params["attacker_name"]).strip()
    if not attacker_name:
        raise ValueError("PIA requires a non-empty attack.parameters.attacker_name")

    attack_num = int(params["attack_num"])
    interval = int(params["interval"])
    if attack_num <= 0:
        raise ValueError("PIA requires attack.parameters.attack_num > 0")
    if interval <= 0:
        raise ValueError("PIA requires attack.parameters.interval > 0")

    return PiaPlan(
        entrypoint="DDPM/attack.py",
        dataset=dataset,
        data_root=data_root,
        model_dir=model_dir,
        num_samples=int(config.attack.num_samples),
        attacker_name=attacker_name,
        attack_num=attack_num,
        interval=interval,
        batch_size=int(params.get("batch_size", 64)),
    )


def resolve_pia_checkpoint_path(model_dir: str | Path) -> Path:
    model_dir_path = Path(model_dir)
    checkpoint_path = model_dir_path / "checkpoint.pt"
    if checkpoint_path.exists():
        return checkpoint_path

    candidates = sorted(
        path
        for pattern in ("ckpt-step*.pt", "*.ckpt", "*.pt")
        for path in model_dir_path.glob(pattern)
    )
    if candidates:
        return candidates[-1]
    return checkpoint_path


def validate_pia_workspace(workspace_dir: str | Path) -> dict[str, str]:
    workspace_path = Path(workspace_dir)
    missing = [
        relative_path
        for relative_path in REQUIRED_PIA_WORKSPACE_FILES
        if not (workspace_path / relative_path).exists()
    ]
    if missing:
        raise FileNotFoundError(
            f"PIA workspace is missing required files: {', '.join(missing)}"
        )

    return {
        "status": "ready",
        "workspace_dir": str(workspace_path),
        "entrypoint": str(workspace_path / "DDPM" / "attack.py"),
    }


def resolve_pia_member_split_path(
    dataset: str,
    member_split_root: str | Path,
) -> Path | None:
    split_name = PIA_MEMBER_SPLIT_FILENAMES.get(dataset.upper())
    if split_name is None:
        return None
    return Path(member_split_root) / split_name


def resolve_pia_dataset_dir(dataset: str, dataset_root: str | Path) -> Path | None:
    layout_name = PIA_DATASET_LAYOUTS.get(dataset.upper())
    if layout_name is None:
        return None
    return Path(dataset_root) / layout_name


def probe_pia_assets(
    dataset: str,
    dataset_root: str | Path,
    model_dir: str | Path,
    member_split_root: str | Path,
) -> dict[str, object]:
    dataset_root_path = Path(dataset_root)
    model_dir_path = Path(model_dir)
    member_split_root_path = Path(member_split_root)
    dataset_dir = resolve_pia_dataset_dir(dataset, dataset_root_path)
    checkpoint_path = resolve_pia_checkpoint_path(model_dir_path)
    member_split_path = resolve_pia_member_split_path(dataset, member_split_root_path)

    checks = {
        "checkpoint": checkpoint_path.exists(),
        "dataset_root": dataset_root_path.exists(),
        "dataset_layout": bool(dataset_dir and dataset_dir.exists()),
        "member_split": bool(member_split_path and member_split_path.exists()),
    }
    status = "ready" if all(checks.values()) else "blocked"
    paths = {
        "model_dir": str(model_dir_path),
        "dataset_root": str(dataset_root_path),
        "dataset_dir": str(dataset_dir) if dataset_dir else "",
        "member_split_root": str(member_split_root_path),
        "checkpoint": str(checkpoint_path),
        "member_split": str(member_split_path) if member_split_path else "",
    }
    labels = {
        "checkpoint": "checkpoint",
        "dataset_root": "dataset_root",
        "dataset_layout": "dataset layout",
        "member_split": "member split",
    }
    path_lookup = {
        "checkpoint": paths["checkpoint"],
        "dataset_root": paths["dataset_root"],
        "dataset_layout": paths["dataset_dir"] or labels["dataset_layout"],
        "member_split": paths["member_split"] or labels["member_split"],
    }
    missing_keys = [name for name, is_ready in checks.items() if not is_ready]
    missing = [path_lookup[name] for name in missing_keys]

    return {
        "status": status,
        "dataset": dataset,
        "checks": checks,
        "paths": paths,
        "missing": missing,
        "missing_keys": missing_keys,
        "missing_items": [Path(item).name for item in missing],
        "missing_description": " / ".join(labels[name] for name in missing_keys),
    }


def explain_pia_assets(
    config: AuditConfig,
    member_split_root: str | Path = "external/PIA/DDPM",
) -> dict[str, object]:
    plan = build_pia_plan(config)
    summary = probe_pia_assets(
        dataset=plan.dataset,
        dataset_root=plan.data_root,
        model_dir=plan.model_dir,
        member_split_root=member_split_root,
    )
    return {
        **summary,
        "model_dir": plan.model_dir,
        "num_samples": plan.num_samples,
        "data_root": plan.data_root,
        "attacker_name": plan.attacker_name,
        "attack_num": plan.attack_num,
        "interval": plan.interval,
    }


def probe_pia_dry_run(
    config: AuditConfig,
    repo_root: str | Path,
    member_split_root: str | Path | None = None,
) -> tuple[int, dict[str, object]]:
    try:
        plan = build_pia_plan(config)
        workspace = validate_pia_workspace(repo_root)
        split_root = Path(member_split_root) if member_split_root else Path(repo_root) / "DDPM"
        summary = probe_pia_assets(
            dataset=plan.dataset,
            dataset_root=plan.data_root,
            model_dir=plan.model_dir,
            member_split_root=split_root,
        )
        components_path = Path(repo_root) / "DDPM" / "components.py"
        model_path = Path(repo_root) / "DDPM" / "model.py"
        components_text = components_path.read_text(encoding="utf-8")
        model_text = model_path.read_text(encoding="utf-8")
    except (FileNotFoundError, ValueError) as exc:
        return 1, {
            "status": "blocked",
            "error": str(exc),
            "repo_root": str(repo_root),
        }
    except Exception as exc:
        return 2, {
            "status": "error",
            "error": f"{type(exc).__name__}: {exc}",
            "repo_root": str(repo_root),
        }

    checks = {
        "workspace_files": True,
        **summary["checks"],
        "components_has_pia": "class PIA(" in components_text or "class PIA:" in components_text,
        "model_has_unet": "class UNet(" in model_text or "class UNet:" in model_text,
    }
    extra_labels = {
        "components_has_pia": "PIA attacker marker",
        "model_has_unet": "UNet class",
    }
    extra_paths = {
        "components_has_pia": str(components_path),
        "model_has_unet": str(model_path),
    }
    missing_keys = list(summary["missing_keys"])
    missing = list(summary["missing"])
    for key in ("components_has_pia", "model_has_unet"):
        if not checks[key]:
            missing_keys.append(key)
            missing.append(extra_paths[key])

    status = "ready" if all(checks.values()) else "blocked"
    payload = {
        "status": status,
        "entrypoint": workspace["entrypoint"],
        "repo_root": str(Path(repo_root)),
        "checks": checks,
        "paths": {
            **summary["paths"],
            "components": str(components_path),
            "model": str(model_path),
        },
        "dataset": plan.dataset,
        "num_samples": plan.num_samples,
        "attacker_name": plan.attacker_name,
        "attack_num": plan.attack_num,
        "interval": plan.interval,
        "missing": missing,
        "missing_keys": missing_keys,
        "missing_items": [Path(item).name for item in missing],
        "missing_description": " / ".join(
            [summary["missing_description"], *[extra_labels[key] for key in extra_labels if not checks[key]]]
        ).strip(" /"),
    }
    return (0 if status == "ready" else 1), payload
