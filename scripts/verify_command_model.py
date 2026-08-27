#!/usr/bin/env python3
"""Verify the normative Partiful command-model matrix and Markdown links."""

from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
MODEL = ROOT / "docs" / "command-model.md"

INVENTORY_HEADER = (
    "| ID | Disposition | CLI command | Shared operation | MCP tool | "
    "Authorization | Risk | MCP hints |"
)
RISK_HEADER = (
    "| Operation | Observable consequence | Primary risk | "
    "Recovery or verification contract |"
)
DISPOSITIONS = {"paired", "cli-only"}
RISKS = {
    "interactive",
    "credential-refresh",
    "credential-delete",
    "read",
    "write",
    "destructive-write",
    "diagnostic",
}
RISK_MATRIX_CLASSES = {"credential-delete", "write", "destructive-write"}


def fail(message: str) -> None:
    raise AssertionError(message)


def strip_code(value: str) -> str:
    value = value.strip()
    if value.startswith("`") and value.endswith("`"):
        return value[1:-1]
    return value


def parse_table(lines: list[str], header: str) -> list[list[str]]:
    try:
        start = lines.index(header)
    except ValueError:
        fail(f"missing table header: {header}")

    rows: list[list[str]] = []
    for line in lines[start + 2 :]:
        if not line.startswith("|"):
            break
        rows.append([cell.strip() for cell in line.strip().strip("|").split("|")])
    if not rows:
        fail(f"table has no rows: {header}")
    return rows


def public_operation(cli_command: str) -> str:
    words = [word for word in cli_command.split()[1:] if not word.startswith(("<", "["))]
    return ".".join(words)


def verify_inventory(text: str) -> tuple[int, int, int]:
    lines = text.splitlines()
    rows = parse_table(lines, INVENTORY_HEADER)
    if any(len(row) != 8 for row in rows):
        fail("every inventory row must have 8 cells")

    ids = [row[0] for row in rows]
    expected_ids = [f"CMD-{number:03d}" for number in range(1, len(rows) + 1)]
    if ids != expected_ids:
        fail(f"command IDs must be contiguous and ordered: {ids!r}")
    if len(ids) != len(set(ids)):
        fail("command IDs are not unique")

    dispositions = [row[1] for row in rows]
    unknown_dispositions = set(dispositions) - DISPOSITIONS
    if unknown_dispositions:
        fail(f"unknown dispositions: {sorted(unknown_dispositions)}")

    commands = [strip_code(row[2]) for row in rows]
    if len(commands) != len(set(commands)):
        fail("CLI command paths are not unique")
    if any(not command.startswith("partiful ") for command in commands):
        fail("every CLI command must start with 'partiful '")

    operations = [strip_code(row[3]) for row in rows]
    if len(operations) != len(set(operations)):
        fail("shared operation names are not unique")

    risks = [row[6] for row in rows]
    unknown_risks = set(risks) - RISKS
    if unknown_risks:
        fail(f"unknown risk classes: {sorted(unknown_risks)}")

    paired_rows = [row for row in rows if row[1] == "paired"]
    cli_only_rows = [row for row in rows if row[1] == "cli-only"]
    if [(strip_code(row[2]), row[4]) for row in cli_only_rows] != [
        ("partiful auth login", "none")
    ]:
        fail("auth.login must be the sole CLI-only command")

    tools = [strip_code(row[4]) for row in paired_rows]
    if any(tool == "none" for tool in tools):
        fail("every paired command must name an MCP tool")
    if len(tools) != len(set(tools)):
        fail("MCP tool names are not unique")
    if any(not re.fullmatch(r"[a-z][a-z0-9_]*", tool) for tool in tools):
        fail("MCP tool names must be lower snake case")

    for row in rows:
        risk = row[6]
        hints = strip_code(row[7])
        if row[1] == "cli-only":
            if hints != "n/a":
                fail(f"CLI-only command {row[0]} must have n/a MCP hints")
            continue
        if not re.fullmatch(r"\[[TF],[TF],[TF],[TF]\]", hints):
            fail(f"invalid MCP hints for {row[0]}: {hints}")
        read_only, destructive, _, _ = hints[1], hints[3], hints[5], hints[7]
        if risk in {"read", "diagnostic"} and read_only != "T":
            fail(f"{row[0]} risk {risk} must be read-only")
        if risk in {"write", "destructive-write", "credential-delete"} and read_only != "F":
            fail(f"{row[0]} risk {risk} must not be read-only")
        if risk in {"destructive-write", "credential-delete"} and destructive != "T":
            fail(f"{row[0]} risk {risk} must have destructiveHint")
        if risk == "write" and destructive != "F":
            fail(f"{row[0]} additive write must not have destructiveHint")

    risk_rows = parse_table(lines, RISK_HEADER)
    if any(len(row) != 4 for row in risk_rows):
        fail("every risk matrix row must have 4 cells")
    actual_risk_operations = [strip_code(row[0]) for row in risk_rows]
    expected_risk_operations = [
        public_operation(strip_code(row[2]))
        for row in rows
        if row[6] in RISK_MATRIX_CLASSES
    ]
    if actual_risk_operations != expected_risk_operations:
        fail(
            "risk matrix operations must exactly match ordered mutating operations: "
            f"expected {expected_risk_operations!r}, got {actual_risk_operations!r}"
        )

    published = {
        "cli": re.search(r"- (\d+) public CLI command paths;", text),
        "non_interactive": re.search(r"- (\d+) non-interactive CLI command paths;", text),
        "mcp": re.search(r"- (\d+) MCP tools;", text),
    }
    if any(match is None for match in published.values()):
        fail("published inventory totals are missing")

    cli_count = len(rows)
    non_interactive_count = sum(risk != "interactive" for risk in risks)
    mcp_count = len(paired_rows)
    computed = {
        "cli": cli_count,
        "non_interactive": non_interactive_count,
        "mcp": mcp_count,
    }
    for name, match in published.items():
        assert match is not None
        if int(match.group(1)) != computed[name]:
            fail(
                f"published {name} count {match.group(1)} does not match "
                f"computed {computed[name]}"
            )

    return cli_count, mcp_count, len(risk_rows)


def verify_markdown_links() -> int:
    link_pattern = re.compile(r"(?<!!)\[[^\]]*\]\(([^)]+)\)|!\[[^\]]*\]\(([^)]+)\)")
    checked = 0
    missing: list[str] = []

    for markdown in sorted(ROOT.rglob("*.md")):
        if ".git" in markdown.parts:
            continue
        text = markdown.read_text(encoding="utf-8")
        for match in link_pattern.finditer(text):
            raw_target = next(group for group in match.groups() if group is not None).strip()
            if raw_target.startswith("<") and raw_target.endswith(">"):
                raw_target = raw_target[1:-1]
            raw_target = raw_target.split(maxsplit=1)[0]
            split = urlsplit(raw_target)
            if split.scheme or raw_target.startswith(("#", "mailto:")):
                continue
            target_path = unquote(split.path)
            if not target_path:
                continue
            target = (ROOT / target_path.lstrip("/")) if raw_target.startswith("/") else (markdown.parent / target_path)
            checked += 1
            if not target.exists():
                missing.append(f"{markdown.relative_to(ROOT)} -> {raw_target}")

    if missing:
        fail("missing local Markdown links:\n" + "\n".join(missing))
    return checked


def main() -> int:
    try:
        text = MODEL.read_text(encoding="utf-8")
        cli_count, mcp_count, risk_count = verify_inventory(text)
        link_count = verify_markdown_links()
    except (AssertionError, OSError, UnicodeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1

    print(
        "PASS: "
        f"commands={cli_count} mcp_tools={mcp_count} "
        f"risk_rows={risk_count} local_links={link_count}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
