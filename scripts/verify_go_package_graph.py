#!/usr/bin/env python3
"""Verify the closed Partiful Go package catalog and internal import DAG."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/KalebCole/partiful"

ALLOWED_IMPORTS = {
    "cmd/partiful": {"internal/compose"},
    "cmd/partiful-mcp": {"internal/compose"},
    "internal/domain": set(),
    "internal/version": set(),
    "internal/command": {"internal/domain"},
    "internal/app": {
        "internal/domain",
        "internal/command",
        "internal/auth",
        "internal/transport",
    },
    "internal/auth": {"internal/domain"},
    "internal/transport": set(),
    "internal/transport/callable": {"internal/transport"},
    "internal/transport/firestore": {"internal/transport"},
    "internal/transport/firebaseauth": {"internal/auth"},
    "internal/transport/poster": {"internal/transport"},
    "internal/credentialstore": {"internal/auth"},
    "internal/cli": {"internal/domain", "internal/command", "internal/app"},
    "internal/mcp": {"internal/domain", "internal/command", "internal/app"},
    "internal/compose": {
        "internal/domain",
        "internal/command",
        "internal/app",
        "internal/auth",
        "internal/transport",
        "internal/transport/callable",
        "internal/transport/firestore",
        "internal/transport/firebaseauth",
        "internal/transport/poster",
        "internal/credentialstore",
        "internal/cli",
        "internal/mcp",
    },
}

OWNED_EXTERNAL_PREFIXES = {
    "internal/cli": (
        "github.com/charmbracelet/",
        "github.com/spf13/cobra",
        "github.com/urfave/cli",
    ),
    "internal/mcp": (
        "github.com/mark3labs/mcp-go",
        "github.com/modelcontextprotocol/",
    ),
    "internal/credentialstore": (
        "github.com/99designs/keyring",
        "github.com/keybase/go-keychain",
        "github.com/zalando/go-keyring",
    ),
}
NETWORK_OWNERS = {
    "internal/transport/callable",
    "internal/transport/firestore",
    "internal/transport/firebaseauth",
    "internal/transport/poster",
}


def fail(message: str) -> None:
    raise AssertionError(message)


def relative(import_path: str) -> str:
    prefix = MODULE + "/"
    if not import_path.startswith(prefix):
        fail(f"unexpected module package: {import_path}")
    return import_path[len(prefix) :]


def load_packages(root: Path) -> list[dict[str, Any]]:
    process = subprocess.run(
        ["go", "list", "-json", "./..."],
        cwd=root,
        capture_output=True,
        text=True,
        check=False,
    )
    if process.returncode:
        fail(f"go list failed:\n{process.stderr.strip()}")
    decoder = json.JSONDecoder()
    packages: list[dict[str, Any]] = []
    source = process.stdout
    index = 0
    while index < len(source):
        while index < len(source) and source[index].isspace():
            index += 1
        if index == len(source):
            break
        package, index = decoder.raw_decode(source, index)
        packages.append(package)
    return packages


def verify(root: Path) -> tuple[int, int]:
    packages = load_packages(root)
    paths = {relative(str(package["ImportPath"])) for package in packages}
    unknown = paths - ALLOWED_IMPORTS.keys()
    if unknown:
        fail(f"packages outside the closed catalog: {sorted(unknown)}")

    production_count = 0
    internal_edges = 0
    all_owned_prefixes = tuple(
        prefix for prefixes in OWNED_EXTERNAL_PREFIXES.values() for prefix in prefixes
    )

    for package in packages:
        package_path = relative(str(package["ImportPath"]))
        go_files = package.get("GoFiles", [])
        if not go_files:
            continue
        production_count += 1
        imports = [str(value) for value in package.get("Imports", [])]
        internal = {
            relative(value) for value in imports if value.startswith(MODULE + "/")
        }
        disallowed = internal - ALLOWED_IMPORTS[package_path]
        if disallowed:
            fail(f"disallowed internal imports from {package_path}: {sorted(disallowed)}")
        internal_edges += len(internal)

        if "net/http" in imports and package_path not in NETWORK_OWNERS:
            fail(f"network protocol import outside concrete transport: {package_path} -> net/http")
        for imported in imports:
            if not imported.startswith(all_owned_prefixes):
                continue
            owners = {
                owner
                for owner, prefixes in OWNED_EXTERNAL_PREFIXES.items()
                if imported.startswith(prefixes)
            }
            if package_path not in owners:
                fail(
                    f"framework import outside owning package: {package_path} -> {imported}"
                )

    return production_count, internal_edges


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    args = parser.parse_args()
    try:
        production_count, internal_edges = verify(args.root.resolve())
    except (AssertionError, OSError, UnicodeError, json.JSONDecodeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    print(
        f"PASS: production_packages={production_count} internal_edges={internal_edges}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
