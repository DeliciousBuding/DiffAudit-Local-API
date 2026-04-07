"""Runtime helpers for the official PIA DDPM implementation."""

from __future__ import annotations

import importlib.util
import json
import shutil
import time
from dataclasses import dataclass
from pathlib import Path
from types import ModuleType
from typing import Any

import numpy as np
import torch
from torch.utils.data import DataLoader, Subset
from torchvision import datasets as tv_datasets
from torchvision import transforms as tv_transforms

from diffaudit.attacks.pia import (
    PiaPlan,
    build_pia_plan,
    probe_pia_assets,
    validate_pia_workspace,
)
from diffaudit.config import (
    AssetConfig,
    AttackConfig,
    AuditConfig,
    ReportConfig,
    TaskConfig,
)


@dataclass(frozen=True)
class PiaDefaults:
    T: int = 1000
    ch: int = 128
    ch_mult: tuple[int, ...] = (1, 2, 2, 2)
    attn: tuple[int, ...] = (1,)
    num_res_blocks: int = 2
    dropout: float = 0.1
    beta_1: float = 0.0001
    beta_T: float = 0.02
    image_size: int = 32


@dataclass(frozen=True)
class PiaAdapterContext:
    plan: PiaPlan
    components_module: ModuleType
    model_module: ModuleType
    checkpoint_path: str
    weights_key: str


def _load_module_from_path(module_name: str, module_path: Path) -> ModuleType:
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise ImportError(f"Unable to load module from {module_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_pia_ddpm_modules(repo_root: str | Path) -> tuple[ModuleType, ModuleType]:
    repo_path = Path(repo_root)
    components_module = _load_module_from_path(
        f"diffaudit_pia_components_{abs(hash(repo_path.resolve()))}",
        repo_path / "DDPM" / "components.py",
    )
    model_module = _load_module_from_path(
        f"diffaudit_pia_model_{abs(hash(repo_path.resolve()))}",
        repo_path / "DDPM" / "model.py",
    )
    return components_module, model_module


def build_pia_unet(model_module: ModuleType, defaults: PiaDefaults | None = None) -> torch.nn.Module:
    config = defaults or PiaDefaults()
    return model_module.UNet(
        T=config.T,
        ch=config.ch,
        ch_mult=list(config.ch_mult),
        attn=list(config.attn),
        num_res_blocks=config.num_res_blocks,
        dropout=config.dropout,
    )


def resolve_pia_weights_key(checkpoint: dict[str, Any]) -> str:
    if "ema_model" in checkpoint:
        return "ema_model"
    if "net_model" in checkpoint:
        return "net_model"
    raise KeyError("PIA checkpoint must contain 'ema_model' or 'net_model'")


def load_pia_model(
    checkpoint_path: str | Path,
    model_module: ModuleType,
    device: str = "cpu",
    defaults: PiaDefaults | None = None,
) -> tuple[torch.nn.Module, str]:
    model = build_pia_unet(model_module, defaults=defaults)
    try:
        checkpoint = torch.load(checkpoint_path, map_location=device, weights_only=False)
    except TypeError:
        checkpoint = torch.load(checkpoint_path, map_location=device)
    if not isinstance(checkpoint, dict):
        raise TypeError("PIA checkpoint must be a dict-like torch checkpoint")
    weights_key = resolve_pia_weights_key(checkpoint)
    weights = checkpoint[weights_key]
    state_dict = {
        (key[7:] if key.startswith("module.") else key): value
        for key, value in weights.items()
    }
    model.load_state_dict(state_dict)
    model.to(device)
    model.eval()
    return model, weights_key


def bootstrap_pia_smoke_assets(
    target_dir: str | Path,
    repo_root: str | Path,
    defaults: PiaDefaults | None = None,
) -> dict[str, Any]:
    target_path = Path(target_dir)
    target_path.mkdir(parents=True, exist_ok=True)
    _, model_module = load_pia_ddpm_modules(repo_root)
    model = build_pia_unet(model_module, defaults=defaults)
    state_dict = model.state_dict()
    checkpoint = {
        "ema_model": state_dict,
        "net_model": state_dict,
    }
    checkpoint_path = target_path / "checkpoint.pt"
    torch.save(checkpoint, checkpoint_path)
    return {
        "checkpoint_path": str(checkpoint_path),
        "weights_key": "ema_model",
    }


def _build_pia_eps_getter(
    components_module: ModuleType,
    model: torch.nn.Module,
):
    class RuntimeEpsGetter(components_module.EpsGetter):
        def __call__(
            self,
            xt: torch.Tensor,
            condition: torch.Tensor | None = None,
            noise_level: torch.Tensor | None = None,
            t: int | None = None,
        ) -> torch.Tensor:
            del condition, noise_level
            timestep = torch.ones([xt.shape[0]], device=xt.device, dtype=torch.long) * int(t or 0)
            return self.model(xt, t=timestep)

    return RuntimeEpsGetter(model)


def _build_pia_attacker(
    components_module: ModuleType,
    model: torch.nn.Module,
    attacker_name: str,
    attack_num: int,
    interval: int,
    device: str,
    defaults: PiaDefaults | None = None,
):
    config = defaults or PiaDefaults()
    attacker_cls = getattr(components_module, attacker_name, None)
    if attacker_cls is None:
        raise ValueError(f"PIA attacker class not found: {attacker_name}")
    betas = torch.linspace(config.beta_1, config.beta_T, config.T, device=device)
    eps_getter = _build_pia_eps_getter(components_module, model)
    return attacker_cls(
        betas,
        interval,
        attack_num,
        eps_getter,
        normalize=lambda x: x * 2 - 1,
    )


def prepare_pia_runtime(
    config: AuditConfig,
    repo_root: str | Path,
    member_split_root: str | Path | None = None,
    device: str = "cpu",
) -> PiaAdapterContext:
    plan = build_pia_plan(config)
    validate_pia_workspace(repo_root)
    split_root = Path(member_split_root) if member_split_root else Path(repo_root) / "DDPM"
    summary = probe_pia_assets(
        dataset=plan.dataset,
        dataset_root=plan.data_root,
        model_dir=plan.model_dir,
        member_split_root=split_root,
    )
    if summary["status"] != "ready":
        missing_description = summary["missing_description"] or "PIA assets"
        raise FileNotFoundError(f"PIA assets not ready: {missing_description}")

    components_module, model_module = load_pia_ddpm_modules(repo_root)
    checkpoint_path = summary["paths"]["checkpoint"]
    model, weights_key = load_pia_model(checkpoint_path, model_module, device=device)
    _build_pia_attacker(
        components_module=components_module,
        model=model,
        attacker_name=plan.attacker_name,
        attack_num=plan.attack_num,
        interval=plan.interval,
        device=device,
    )
    return PiaAdapterContext(
        plan=plan,
        components_module=components_module,
        model_module=model_module,
        checkpoint_path=checkpoint_path,
        weights_key=weights_key,
    )


def probe_pia_runtime(
    config: AuditConfig,
    repo_root: str | Path,
    member_split_root: str | Path | None = None,
    device: str = "cpu",
) -> tuple[int, dict[str, Any]]:
    try:
        plan = build_pia_plan(config)
        validate_pia_workspace(repo_root)
        split_root = Path(member_split_root) if member_split_root else Path(repo_root) / "DDPM"
        summary = probe_pia_assets(
            dataset=plan.dataset,
            dataset_root=plan.data_root,
            model_dir=plan.model_dir,
            member_split_root=split_root,
        )
        if summary["status"] != "ready":
            return 1, {
                "status": "blocked",
                "repo_root": str(repo_root),
                "checks": summary["checks"],
                "paths": summary["paths"],
                "missing": summary["missing"],
                "missing_keys": summary["missing_keys"],
                "missing_items": summary["missing_items"],
                "missing_description": summary["missing_description"],
            }

        components_module, model_module = load_pia_ddpm_modules(repo_root)
        model, weights_key = load_pia_model(summary["paths"]["checkpoint"], model_module, device=device)
        attacker = _build_pia_attacker(
            components_module=components_module,
            model=model,
            attacker_name=plan.attacker_name,
            attack_num=plan.attack_num,
            interval=plan.interval,
            device=device,
        )
        preview_batch = torch.rand(1, 3, 32, 32, device=device)
        preview_scores = attacker(preview_batch)
    except (FileNotFoundError, ValueError, KeyError, TypeError, ImportError) as exc:
        return 1, {
            "status": "blocked",
            "error": str(exc),
            "repo_root": str(repo_root),
            "device": device,
        }
    except Exception as exc:
        return 2, {
            "status": "error",
            "error": f"{type(exc).__name__}: {exc}",
            "repo_root": str(repo_root),
            "device": device,
        }

    return 0, {
        "status": "ready",
        "track": "gray-box",
        "method": "pia",
        "paper": "PIA_ICLR2024",
        "mode": "runtime-probe",
        "device": device,
        "repo_root": str(Path(repo_root)),
        "dataset": plan.dataset,
        "model_dir": plan.model_dir,
        "attack_num": plan.attack_num,
        "interval": plan.interval,
        "attacker_name": plan.attacker_name,
        "checks": {
            **summary["checks"],
            "components_loaded": hasattr(components_module, plan.attacker_name),
            "model_loaded": True,
            "attacker_instantiated": True,
            "preview_forward": True,
        },
        "paths": summary["paths"],
        "weights_key": weights_key,
        "preview_score_shape": list(preview_scores.shape),
    }


def _load_pia_preview_batches(
    dataset: str,
    dataset_dir: str | Path,
    member_split_path: str | Path,
    preview_batch_size: int,
    device: str,
) -> tuple[torch.Tensor, torch.Tensor]:
    if dataset.upper() != "CIFAR10":
        raise ValueError(f"PIA runtime preview currently supports CIFAR10 only: {dataset}")

    split_payload = np.load(member_split_path)
    member_indices = split_payload["mia_train_idxs"].tolist()
    nonmember_indices = split_payload["mia_eval_idxs"].tolist()
    if not member_indices or not nonmember_indices:
        raise ValueError("PIA member split must contain non-empty member and non-member indices")

    transform = tv_transforms.Compose([tv_transforms.ToTensor()])
    dataset_obj = tv_datasets.CIFAR10(
        root=str(dataset_dir),
        train=True,
        download=False,
        transform=transform,
    )
    member_subset = Subset(dataset_obj, member_indices)
    nonmember_subset = Subset(dataset_obj, nonmember_indices)
    member_loader = DataLoader(
        member_subset,
        batch_size=min(preview_batch_size, len(member_subset)),
        shuffle=False,
    )
    nonmember_loader = DataLoader(
        nonmember_subset,
        batch_size=min(preview_batch_size, len(nonmember_subset)),
        shuffle=False,
    )
    member_batch = next(iter(member_loader))[0].to(device)
    nonmember_batch = next(iter(nonmember_loader))[0].to(device)
    return member_batch, nonmember_batch


def probe_pia_runtime_preview(
    config: AuditConfig,
    repo_root: str | Path,
    member_split_root: str | Path | None = None,
    device: str = "cpu",
    preview_batch_size: int = 2,
) -> tuple[int, dict[str, Any]]:
    try:
        plan = build_pia_plan(config)
        validate_pia_workspace(repo_root)
        split_root = Path(member_split_root) if member_split_root else Path(repo_root) / "DDPM"
        summary = probe_pia_assets(
            dataset=plan.dataset,
            dataset_root=plan.data_root,
            model_dir=plan.model_dir,
            member_split_root=split_root,
        )
        if summary["status"] != "ready":
            return 1, {
                "status": "blocked",
                "repo_root": str(repo_root),
                "checks": summary["checks"],
                "paths": summary["paths"],
                "missing": summary["missing"],
                "missing_keys": summary["missing_keys"],
                "missing_items": summary["missing_items"],
                "missing_description": summary["missing_description"],
            }

        components_module, model_module = load_pia_ddpm_modules(repo_root)
        model, weights_key = load_pia_model(summary["paths"]["checkpoint"], model_module, device=device)
        attacker = _build_pia_attacker(
            components_module=components_module,
            model=model,
            attacker_name=plan.attacker_name,
            attack_num=plan.attack_num,
            interval=plan.interval,
            device=device,
        )
        member_batch, nonmember_batch = _load_pia_preview_batches(
            dataset=plan.dataset,
            dataset_dir=summary["paths"]["dataset_dir"],
            member_split_path=summary["paths"]["member_split"],
            preview_batch_size=preview_batch_size,
            device=device,
        )
        member_scores = attacker(member_batch)
        nonmember_scores = attacker(nonmember_batch)
    except (FileNotFoundError, ValueError, KeyError, TypeError, ImportError) as exc:
        return 1, {
            "status": "blocked",
            "error": str(exc),
            "repo_root": str(repo_root),
            "device": device,
        }
    except Exception as exc:
        return 2, {
            "status": "error",
            "error": f"{type(exc).__name__}: {exc}",
            "repo_root": str(repo_root),
            "device": device,
        }

    return 0, {
        "status": "ready",
        "track": "gray-box",
        "method": "pia",
        "paper": "PIA_ICLR2024",
        "mode": "runtime-preview",
        "device": device,
        "repo_root": str(Path(repo_root)),
        "dataset": plan.dataset,
        "model_dir": plan.model_dir,
        "attack_num": plan.attack_num,
        "interval": plan.interval,
        "attacker_name": plan.attacker_name,
        "preview_batch_size": preview_batch_size,
        "checks": {
            **summary["checks"],
            "components_loaded": hasattr(components_module, plan.attacker_name),
            "model_loaded": True,
            "attacker_instantiated": True,
            "member_preview_loaded": True,
            "nonmember_preview_loaded": True,
            "member_preview_forward": True,
            "nonmember_preview_forward": True,
        },
        "paths": summary["paths"],
        "weights_key": weights_key,
        "member_batch_shape": list(member_batch.shape),
        "nonmember_batch_shape": list(nonmember_batch.shape),
        "member_score_shape": list(member_scores.shape),
        "nonmember_score_shape": list(nonmember_scores.shape),
        "member_score_mean": float(member_scores.mean().item()),
        "nonmember_score_mean": float(nonmember_scores.mean().item()),
    }


def _compute_auc(member_scores: torch.Tensor, nonmember_scores: torch.Tensor) -> float:
    member = member_scores.detach().cpu().flatten()
    nonmember = nonmember_scores.detach().cpu().flatten()
    wins = (member[:, None] > nonmember[None, :]).float()
    ties = (member[:, None] == nonmember[None, :]).float() * 0.5
    return float((wins + ties).mean().item())


def _compute_threshold_metrics(
    member_scores: np.ndarray,
    nonmember_scores: np.ndarray,
) -> dict[str, float]:
    scores = np.concatenate([member_scores, nonmember_scores])
    labels = np.concatenate(
        [
            np.ones(member_scores.shape[0], dtype=np.int64),
            np.zeros(nonmember_scores.shape[0], dtype=np.int64),
        ]
    )
    thresholds = np.unique(scores)[::-1]
    if thresholds.size == 0:
        thresholds = np.asarray([0.0], dtype=float)

    best_asr = -1.0
    best_threshold = float(thresholds[0])
    best_tpr_at_1pct = 0.0
    best_tpr_at_0_1pct = 0.0
    for threshold in thresholds:
        predictions = (scores >= threshold).astype(np.int64)
        tp = int(((predictions == 1) & (labels == 1)).sum())
        tn = int(((predictions == 0) & (labels == 0)).sum())
        fp = int(((predictions == 1) & (labels == 0)).sum())
        fn = int(((predictions == 0) & (labels == 1)).sum())
        asr = float((tp + tn) / labels.shape[0])
        if asr > best_asr:
            best_asr = asr
            best_threshold = float(threshold)
        tpr = float(tp / (tp + fn)) if (tp + fn) else 0.0
        fpr = float(fp / (fp + tn)) if (fp + tn) else 0.0
        if fpr <= 0.01:
            best_tpr_at_1pct = max(best_tpr_at_1pct, tpr)
        if fpr <= 0.001:
            best_tpr_at_0_1pct = max(best_tpr_at_0_1pct, tpr)

    return {
        "asr": round(best_asr, 6),
        "threshold": round(best_threshold, 6),
        "tpr_at_1pct_fpr": round(best_tpr_at_1pct, 6),
        "tpr_at_0_1pct_fpr": round(best_tpr_at_0_1pct, 6),
    }


def _select_preview_indices(
    split_indices: list[int],
    max_samples: int | None,
) -> list[int]:
    if max_samples is None or max_samples <= 0:
        return list(split_indices)
    return list(split_indices[: max_samples])


def _build_pia_subset_loader(
    dataset: str,
    dataset_dir: str | Path,
    member_split_path: str | Path,
    batch_size: int,
    max_samples: int | None,
    membership: str,
):
    if dataset.upper() != "CIFAR10":
        raise ValueError(f"PIA runtime mainline currently supports CIFAR10 only: {dataset}")

    split_payload = np.load(member_split_path)
    if membership == "member":
        raw_indices = split_payload["mia_train_idxs"].tolist()
    else:
        raw_indices = split_payload["mia_eval_idxs"].tolist()
    selected_indices = _select_preview_indices(raw_indices, max_samples=max_samples)
    if not selected_indices:
        raise ValueError(f"PIA split has no samples for membership={membership}")

    transform = tv_transforms.Compose([tv_transforms.ToTensor()])
    dataset_obj = tv_datasets.CIFAR10(
        root=str(dataset_dir),
        train=True,
        download=False,
        transform=transform,
    )
    subset = Subset(dataset_obj, selected_indices)
    loader = DataLoader(
        subset,
        batch_size=min(batch_size, len(subset)),
        shuffle=False,
    )
    return loader, selected_indices


def run_synthetic_pia_smoke(
    workspace: str | Path,
    repo_root: str | Path = "external/PIA",
    device: str = "cpu",
    attacker_name: str = "PIA",
    attack_num: int = 3,
    interval: int = 10,
    seed: int = 0,
) -> dict[str, Any]:
    torch.manual_seed(seed)
    if device.startswith("cuda"):
        torch.cuda.manual_seed_all(seed)

    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    smoke_assets = bootstrap_pia_smoke_assets(
        target_dir=workspace_path / "synthetic-pia-assets",
        repo_root=repo_root,
    )
    components_module, model_module = load_pia_ddpm_modules(repo_root)
    model, weights_key = load_pia_model(smoke_assets["checkpoint_path"], model_module, device=device)
    attacker = _build_pia_attacker(
        components_module=components_module,
        model=model,
        attacker_name=attacker_name,
        attack_num=attack_num,
        interval=interval,
        device=device,
    )

    member_batch = torch.rand(4, 3, 32, 32, device=device)
    nonmember_batch = torch.clamp(torch.rand(4, 3, 32, 32, device=device) * 0.7 + 0.15, 0.0, 1.0)
    member_distances = attacker(member_batch).mean(dim=0)
    nonmember_distances = attacker(nonmember_batch).mean(dim=0)
    member_scores = -member_distances
    nonmember_scores = -nonmember_distances
    auc = _compute_auc(member_scores, nonmember_scores)
    threshold = torch.cat([member_scores, nonmember_scores]).median()
    asr = float(
        (
            torch.cat([member_scores > threshold, nonmember_scores <= threshold]).float().mean().item()
        )
    )

    result = {
        "status": "ready",
        "track": "gray-box",
        "method": "pia",
        "paper": "PIA_ICLR2024",
        "mode": "synthetic-smoke",
        "device": device,
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
        },
        "assets": {
            "repo_root": str(Path(repo_root)),
            "weights_key": weights_key,
            "synthetic_checkpoint_path": smoke_assets["checkpoint_path"],
        },
        "checks": {
            "components_loaded": True,
            "model_loaded": True,
            "attacker_instantiated": True,
            "member_forward": True,
            "nonmember_forward": True,
            "synthetic_assets_cleaned": True,
        },
        "metrics": {
            "auc": round(float(auc), 6),
            "asr": round(float(asr), 6),
            "member_score_mean": round(float(member_scores.mean().item()), 6),
            "nonmember_score_mean": round(float(nonmember_scores.mean().item()), 6),
        },
        "cost": {
            "queries_per_sample": attack_num + 1,
            "timesteps": attack_num,
            "interval": interval,
            "batch_size": 4,
        },
        "notes": [
            "Synthetic smoke only verifies the PIA DDPM runtime path.",
            "Metrics are not benchmark numbers and should not be compared to paper results.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    shutil.rmtree(workspace_path / "synthetic-pia-assets", ignore_errors=True)
    return result


def run_pia_runtime_smoke(
    workspace: str | Path,
    repo_root: str | Path = "external/PIA",
    device: str = "cpu",
    attacker_name: str = "PIA",
    attack_num: int = 3,
    interval: int = 10,
) -> dict[str, Any]:
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)
    synthetic_root = workspace_path / "synthetic-assets"
    dataset_root = synthetic_root / "datasets"
    model_dir = synthetic_root / "model"
    member_split_root = synthetic_root / "member_splits"
    (dataset_root / "cifar10").mkdir(parents=True, exist_ok=True)
    member_split_root.mkdir(parents=True, exist_ok=True)
    (member_split_root / "CIFAR10_train_ratio0.5.npz").write_bytes(b"split")
    bootstrap_pia_smoke_assets(model_dir, repo_root)

    config = AuditConfig(
        task=TaskConfig(
            name="pia-runtime-smoke",
            model_family="diffusion",
            access_level="semi_white_box",
        ),
        assets=AssetConfig(
            dataset_id="synthetic-cifar10-half",
            model_id="synthetic-pia-ddpm",
            dataset_name="cifar10",
            dataset_root=dataset_root.as_posix(),
            model_dir=model_dir.as_posix(),
        ),
        attack=AttackConfig(
            method="pia",
            num_samples=8,
            parameters={
                "attacker_name": attacker_name,
                "attack_num": attack_num,
                "interval": interval,
            },
        ),
        report=ReportConfig(
            output_dir="experiments/pia-runtime-smoke"
        ),
    )
    exit_code, payload = probe_pia_runtime(
        config,
        repo_root=repo_root,
        member_split_root=member_split_root,
        device=device,
    )

    result = {
        "status": payload["status"],
        "track": "gray-box",
        "method": "pia",
        "paper": "PIA_ICLR2024",
        "mode": "runtime-smoke",
        "device": device,
        "workspace": str(workspace_path),
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
        },
        "assets": {
            "repo_root": str(Path(repo_root)),
            "dataset_name": "cifar10",
            "attacker_name": attacker_name,
            "attack_num": attack_num,
            "interval": interval,
        },
        "checks": {
            **payload.get("checks", {}),
            "runtime_probe_ready": exit_code == 0 and payload["status"] == "ready",
            "synthetic_assets_cleaned": True,
        },
        "notes": [
            "Runtime smoke uses synthetic assets to verify config-driven PIA readiness.",
            "Synthetic assets are removed after the summary is written.",
        ],
    }
    if "preview_score_shape" in payload:
        result["preview_score_shape"] = payload["preview_score_shape"]
    if "weights_key" in payload:
        result["weights_key"] = payload["weights_key"]
    if exit_code != 0 and "error" in payload:
        result["error"] = payload["error"]

    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    shutil.rmtree(synthetic_root, ignore_errors=True)
    return result


def run_pia_runtime_mainline(
    config: AuditConfig,
    workspace: str | Path,
    repo_root: str | Path = "external/PIA",
    member_split_root: str | Path | None = None,
    device: str = "cpu",
    max_samples: int | None = None,
    batch_size: int = 8,
    stochastic_dropout_defense: bool = False,
    provenance_status: str = "source-retained-unverified",
) -> dict[str, Any]:
    started_at = time.perf_counter()
    workspace_path = Path(workspace)
    workspace_path.mkdir(parents=True, exist_ok=True)

    exit_code, readiness = probe_pia_runtime(
        config,
        repo_root=repo_root,
        member_split_root=member_split_root,
        device=device,
    )
    if exit_code != 0 or readiness["status"] != "ready":
        result = {
            "status": "blocked",
            "track": "gray-box",
            "method": "pia",
            "paper": "PIA_ICLR2024",
            "mode": "runtime-mainline",
            "workspace": str(workspace_path),
            "workspace_name": workspace_path.name,
            "contract_stage": "target",
            "asset_grade": "single-machine-real-asset",
            "provenance_status": provenance_status,
            "evidence_level": "runtime-mainline",
            "checks": {
                "runtime_probe_ready": False,
            },
            "runtime_probe": readiness,
            "artifact_paths": {
                "summary": str(workspace_path / "summary.json"),
            },
            "notes": [
                "Runtime mainline requires the canonical checkpoint, dataset root, and member split to be ready.",
            ],
        }
        (workspace_path / "summary.json").write_text(
            json.dumps(result, indent=2, ensure_ascii=True),
            encoding="utf-8",
        )
        return result

    runtime_context = prepare_pia_runtime(
        config,
        repo_root=repo_root,
        member_split_root=member_split_root,
        device=device,
    )
    model, weights_key = load_pia_model(
        runtime_context.checkpoint_path,
        runtime_context.model_module,
        device=device,
    )
    if stochastic_dropout_defense:
        model.train()
    else:
        model.eval()
    attacker = _build_pia_attacker(
        components_module=runtime_context.components_module,
        model=model,
        attacker_name=runtime_context.plan.attacker_name,
        attack_num=runtime_context.plan.attack_num,
        interval=runtime_context.plan.interval,
        device=device,
    )

    split_root = Path(member_split_root) if member_split_root else Path(repo_root) / "DDPM"
    asset_summary = probe_pia_assets(
        dataset=runtime_context.plan.dataset,
        dataset_root=runtime_context.plan.data_root,
        model_dir=runtime_context.plan.model_dir,
        member_split_root=split_root,
    )
    selected_max_samples = max_samples or runtime_context.plan.num_samples
    member_loader, member_indices = _build_pia_subset_loader(
        dataset=runtime_context.plan.dataset,
        dataset_dir=asset_summary["paths"]["dataset_dir"],
        member_split_path=asset_summary["paths"]["member_split"],
        batch_size=batch_size,
        max_samples=selected_max_samples,
        membership="member",
    )
    nonmember_loader, nonmember_indices = _build_pia_subset_loader(
        dataset=runtime_context.plan.dataset,
        dataset_dir=asset_summary["paths"]["dataset_dir"],
        member_split_path=asset_summary["paths"]["member_split"],
        batch_size=batch_size,
        max_samples=selected_max_samples,
        membership="nonmember",
    )

    member_scores: list[torch.Tensor] = []
    nonmember_scores: list[torch.Tensor] = []
    with torch.no_grad():
        for batch, _ in member_loader:
            batch_scores = attacker(batch.to(device)).mean(dim=0)
            member_scores.append((-batch_scores).detach().cpu())
        for batch, _ in nonmember_loader:
            batch_scores = attacker(batch.to(device)).mean(dim=0)
            nonmember_scores.append((-batch_scores).detach().cpu())

    member_score_tensor = torch.cat(member_scores)
    nonmember_score_tensor = torch.cat(nonmember_scores)
    metrics = {
        "auc": round(_compute_auc(member_score_tensor, nonmember_score_tensor), 6),
        **_compute_threshold_metrics(
            member_score_tensor.numpy(),
            nonmember_score_tensor.numpy(),
        ),
        "member_score_mean": round(float(member_score_tensor.mean().item()), 6),
        "nonmember_score_mean": round(float(nonmember_score_tensor.mean().item()), 6),
    }
    scores_path = workspace_path / "scores.json"
    scores_payload = {
        "member_scores": [round(float(value), 6) for value in member_score_tensor.tolist()],
        "nonmember_scores": [round(float(value), 6) for value in nonmember_score_tensor.tolist()],
        "member_indices": member_indices,
        "nonmember_indices": nonmember_indices,
    }
    scores_path.write_text(
        json.dumps(scores_payload, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )

    result = {
        "status": "ready",
        "track": "gray-box",
        "method": "pia",
        "paper": "PIA_ICLR2024",
        "mode": "runtime-mainline",
        "device": device,
        "workspace": str(workspace_path),
        "workspace_name": workspace_path.name,
        "contract_stage": "target",
        "asset_grade": "single-machine-real-asset",
        "provenance_status": provenance_status,
        "evidence_level": "runtime-mainline",
        "artifact_paths": {
            "summary": str(workspace_path / "summary.json"),
            "scores": str(scores_path),
        },
        "checks": {
            **asset_summary["checks"],
            "runtime_probe_ready": True,
            "member_scores_generated": True,
            "nonmember_scores_generated": True,
        },
        "runtime": {
            "repo_root": str(Path(repo_root)),
            "model_dir": runtime_context.plan.model_dir,
            "dataset_root": runtime_context.plan.data_root,
            "member_split_root": str(split_root),
            "batch_size": int(batch_size),
            "max_samples": int(selected_max_samples),
            "num_samples": int(runtime_context.plan.num_samples),
            "attack_num": runtime_context.plan.attack_num,
            "interval": runtime_context.plan.interval,
            "weights_key": weights_key,
            "elapsed_seconds": round(time.perf_counter() - started_at, 6),
        },
        "defense": {
            "name": "stochastic-dropout" if stochastic_dropout_defense else "none",
            "enabled": bool(stochastic_dropout_defense),
        },
        "sample_count_per_split": int(member_score_tensor.shape[0]),
        "metrics": metrics,
        "notes": [
            "Runtime mainline executes the current PIA DDPM path on canonical real assets.",
            "Current evidence is suitable for a local baseline and must not be overstated as benchmark-ready.",
        ],
    }
    (workspace_path / "summary.json").write_text(
        json.dumps(result, indent=2, ensure_ascii=True),
        encoding="utf-8",
    )
    return result
