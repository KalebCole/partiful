#!/usr/bin/env python3
"""Fail closed unless Partiful implementation profiles are isolated."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

REQUIRED_PROFILES = (
    "partiful-implementer",
    "partiful-code-reviewer",
    "partiful-integrator",
)
ALLOWED_TOOLSETS = frozenset({"terminal", "file", "skills"})


def _toolsets(config_text: str) -> frozenset[str]:
    lines = config_text.splitlines()
    for index, line in enumerate(lines):
        if not line.startswith("toolsets:"):
            continue
        remainder = line.split(":", 1)[1].strip()
        if remainder.startswith("[") and remainder.endswith("]"):
            return frozenset(
                item.strip().strip("'\"")
                for item in remainder[1:-1].split(",")
                if item.strip()
            )
        values: list[str] = []
        for child in lines[index + 1 :]:
            if not child.startswith((" ", "\t")):
                break
            stripped = child.strip()
            if stripped.startswith("-"):
                values.append(stripped[1:].strip().strip("'\""))
        return frozenset(values)
    return frozenset()


def _env_names(env_text: str) -> tuple[str, ...]:
    names: list[str] = []
    for line in env_text.splitlines():
        stripped = line.strip()
        if stripped and not stripped.startswith("#") and "=" in stripped:
            names.append(stripped.split("=", 1)[0].strip())
    return tuple(sorted(names))


def _section_value(config_text: str, section: str, key: str) -> str | None:
    in_section = False
    for line in config_text.splitlines():
        if line == f"{section}:":
            in_section = True
            continue
        if in_section and line and not line[0].isspace():
            return None
        if in_section and line.startswith("  "):
            stripped = line.strip()
            if stripped.startswith(f"{key}:"):
                return stripped.split(":", 1)[1].strip()
    return None


def verify_worker_profiles(profiles_root: Path) -> dict[str, dict[str, object]]:
    result: dict[str, dict[str, object]] = {}
    errors: list[str] = []
    for name in REQUIRED_PROFILES:
        directory = profiles_root / name
        config_path = directory / "config.yaml"
        env_path = directory / ".env"
        if not config_path.is_file():
            errors.append(f"{name}: missing config.yaml")
            continue
        config_text = config_path.read_text(encoding="utf-8")
        tools = _toolsets(config_text)
        env_names = _env_names(env_path.read_text(encoding="utf-8")) if env_path.exists() else ()
        auto_source_bashrc = _section_value(
            config_text, "terminal", "auto_source_bashrc"
        )
        env_passthrough = _section_value(config_text, "terminal", "env_passthrough")
        shell_init_files = _section_value(config_text, "terminal", "shell_init_files")
        if tools != ALLOWED_TOOLSETS:
            errors.append(
                f"{name}: toolsets must be {sorted(ALLOWED_TOOLSETS)}, got {sorted(tools)}"
            )
        if env_names:
            errors.append(f"{name}: profile .env contains variables {list(env_names)}")
        if auto_source_bashrc != "false":
            errors.append(
                f"{name}: terminal.auto_source_bashrc must be false, got {auto_source_bashrc!r}"
            )
        if env_passthrough != "[]":
            errors.append(
                f"{name}: terminal.env_passthrough must be [], got {env_passthrough!r}"
            )
        if shell_init_files != "[]":
            errors.append(
                f"{name}: terminal.shell_init_files must be [], got {shell_init_files!r}"
            )
        result[name] = {
            "toolsets": sorted(tools),
            "env_variables": list(env_names),
            "auto_source_bashrc": auto_source_bashrc,
            "env_passthrough": env_passthrough,
            "shell_init_files": shell_init_files,
        }
    if errors:
        raise RuntimeError("profile isolation audit failed: " + "; ".join(errors))
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--profiles-root",
        type=Path,
        default=Path.home() / ".hermes" / "profiles",
    )
    args = parser.parse_args()
    print(json.dumps(verify_worker_profiles(args.profiles_root), sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
