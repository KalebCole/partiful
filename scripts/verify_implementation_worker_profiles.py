#!/usr/bin/env python3
"""Fail closed unless Partiful implementation profiles are isolated."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

REQUIRED_PROFILES = ("project-manager", "coding-worker")
REQUIRED_TOOLSETS = {
    "project-manager": frozenset(
        {"file", "terminal", "skills", "todo", "clarify", "session_search", "kanban"}
    ),
    "coding-worker": frozenset(
        {
            "file",
            "terminal",
            "skills",
            "todo",
            "clarify",
            "session_search",
            "delegation",
            "web",
            "browser",
        }
    ),
}
ALLOWED_ENV_NAMES = {
    "project-manager": frozenset({"GH_TOKEN", "GITHUB_TOKEN", "NOTION_API_KEY"}),
    "coding-worker": frozenset({"GH_TOKEN", "GITHUB_TOKEN"}),
}


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
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.startswith("export "):
            stripped = stripped.removeprefix("export ").lstrip()
        name = stripped.split("=", 1)[0].strip()
        if name:
            names.append(name)
    return tuple(sorted(names))


def _duplicate_contract_keys(config_text: str) -> tuple[str, ...]:
    counts: dict[str, int] = {}
    section: str | None = None
    terminal_keys = {"auto_source_bashrc", "env_passthrough", "shell_init_files"}
    for line in config_text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        indentation = len(line) - len(line.lstrip(" "))
        if indentation == 0:
            key = stripped.split(":", 1)[0]
            section = key
            if key in {"toolsets", "terminal"}:
                counts[key] = counts.get(key, 0) + 1
            continue
        if section == "terminal" and indentation == 2:
            key = stripped.split(":", 1)[0]
            if key in terminal_keys:
                label = f"terminal.{key}"
                counts[label] = counts.get(label, 0) + 1
    return tuple(sorted(key for key, count in counts.items() if count > 1))


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


def _section_list(
    config_text: str, section: str, key: str
) -> tuple[str, ...] | None:
    lines = config_text.splitlines()
    in_section = False
    for index, line in enumerate(lines):
        if line == f"{section}:":
            in_section = True
            continue
        if in_section and line and not line[0].isspace():
            return None
        if not in_section or not line.startswith("  "):
            continue
        stripped = line.strip()
        if not stripped.startswith(f"{key}:"):
            continue
        remainder = stripped.split(":", 1)[1].strip()
        if remainder == "[]":
            return ()
        if remainder.startswith("[") and remainder.endswith("]"):
            return tuple(
                item.strip().strip("'\"")
                for item in remainder[1:-1].split(",")
                if item.strip()
            )
        values: list[str] = []
        for child in lines[index + 1 :]:
            if not child.startswith("    "):
                break
            child = child.strip()
            if child.startswith("-"):
                values.append(child[1:].strip().strip("'\""))
        return tuple(values)
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
        duplicate_keys = _duplicate_contract_keys(config_text)
        tools = _toolsets(config_text)
        env_names = _env_names(env_path.read_text(encoding="utf-8")) if env_path.exists() else ()
        unexpected_env_names = sorted(set(env_names) - ALLOWED_ENV_NAMES[name])
        auto_source_bashrc = _section_value(
            config_text, "terminal", "auto_source_bashrc"
        )
        env_passthrough = _section_list(config_text, "terminal", "env_passthrough")
        shell_init_files = _section_list(config_text, "terminal", "shell_init_files")
        required_shell_init_files = (str(directory / "shell" / "profile-env.sh"),)
        required_tools = REQUIRED_TOOLSETS[name]
        if duplicate_keys:
            errors.append(f"{name}: duplicate config keys {list(duplicate_keys)}")
        if tools != required_tools:
            errors.append(
                f"{name}: toolsets must be {sorted(required_tools)}, got {sorted(tools)}"
            )
        if unexpected_env_names:
            errors.append(
                f"{name}: profile .env contains unapproved variables {unexpected_env_names}"
            )
        if auto_source_bashrc != "false":
            errors.append(
                f"{name}: terminal.auto_source_bashrc must be false, got {auto_source_bashrc!r}"
            )
        if env_passthrough != ():
            errors.append(
                f"{name}: terminal.env_passthrough must be empty, got {env_passthrough!r}"
            )
        if shell_init_files != required_shell_init_files:
            errors.append(
                f"{name}: terminal.shell_init_files must be {required_shell_init_files!r}, "
                f"got {shell_init_files!r}"
            )
        result[name] = {
            "toolsets": sorted(tools),
            "env_variables": list(env_names),
            "auto_source_bashrc": auto_source_bashrc,
            "env_passthrough": list(env_passthrough or ()),
            "shell_init_files": list(shell_init_files or ()),
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
