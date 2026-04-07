"""Configuration loading for research audit runs."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml


SUPPORTED_ACCESS_LEVELS = {"black_box", "white_box", "semi_white_box"}


@dataclass(frozen=True)
class TaskConfig:
    name: str
    model_family: str
    access_level: str


@dataclass(frozen=True)
class AssetConfig:
    dataset_id: str
    model_id: str
    dataset_name: str | None = None
    dataset_root: str | None = None
    model_dir: str | None = None
    dataset_train_ref: str | None = None
    dataset_test_ref: str | None = None


@dataclass(frozen=True)
class AttackConfig:
    method: str
    num_samples: int
    parameters: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True)
class ReportConfig:
    output_dir: str


@dataclass(frozen=True)
class AuditConfig:
    task: TaskConfig
    assets: AssetConfig
    attack: AttackConfig
    report: ReportConfig


def load_audit_config(config_path: str | Path) -> AuditConfig:
    path = Path(config_path)
    with path.open("r", encoding="utf-8") as handle:
        payload = yaml.safe_load(handle)

    task = TaskConfig(**payload["task"])
    if task.access_level not in SUPPORTED_ACCESS_LEVELS:
        raise ValueError(f"Unsupported access_level: {task.access_level}")

    return AuditConfig(
        task=task,
        assets=AssetConfig(**payload["assets"]),
        attack=AttackConfig(**payload["attack"]),
        report=ReportConfig(**payload["report"]),
    )
