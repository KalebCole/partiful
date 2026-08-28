#!/usr/bin/env python3
"""Build and black-box test the release-shaped Partiful binaries."""

from __future__ import annotations

import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def fail(message: str) -> None:
    raise AssertionError(message)


def run(command: list[str], *, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        fail(f"{' '.join(command)} failed ({result.returncode}): {result.stderr}")
    return result


def request(process: subprocess.Popen[str], identifier: int, method: str, params: dict | None = None) -> dict:
    frame = {"jsonrpc": "2.0", "id": identifier, "method": method}
    if params is not None:
        frame["params"] = params
    assert process.stdin is not None
    assert process.stdout is not None
    process.stdin.write(json.dumps(frame, separators=(",", ":")) + "\n")
    process.stdin.flush()
    line = process.stdout.readline()
    if not line:
        fail(f"MCP server closed before response to {method}")
    response = json.loads(line)
    if response.get("id") != identifier:
        fail(f"MCP response ID mismatch: {response!r}")
    return response


def launch(binary: Path, env: dict[str, str]) -> subprocess.Popen[str]:
    return subprocess.Popen(
        [str(binary)], cwd=ROOT, env=env, text=True,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="partiful-smoke-") as temporary:
        temp = Path(temporary)
        cli_binary = temp / "partiful"
        mcp_binary = temp / "partiful-mcp"
        run(["go", "build", "-trimpath", "-o", str(cli_binary), "./cmd/partiful"])
        run(["go", "build", "-trimpath", "-o", str(mcp_binary), "./cmd/partiful-mcp"])

        env = os.environ.copy()
        env["HOME"] = str(temp / "home")
        env["XDG_DATA_HOME"] = str(temp / "data")
        # Prevent discovery of a host OS credential helper. The executables are
        # launched by absolute path and need no PATH entries.
        env["PATH"] = str(temp / "empty-path")

        version = run([str(cli_binary), "--version", "--json"], env=env)
        fields = json.loads(version.stdout)
        expected = {"cli_version", "command_contract_revision", "transport_contract_revision"}
        if set(fields) != expected or any(not fields[name] for name in expected):
            fail(f"unexpected version output: {fields!r}")
        help_result = run([str(cli_binary), "--help"], env=env)
        if "Usage: partiful <command> [flags]" not in help_result.stdout:
            fail("CLI help did not contain the root usage line")

        process = launch(mcp_binary, env)
        initialized = request(process, 1, "initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "smoke", "version": "1"}})
        if initialized.get("result", {}).get("serverInfo", {}).get("name") != "partiful":
            fail(f"unexpected initialize response: {initialized!r}")
        if initialized.get("result", {}).get("serverInfo", {}).get("version") != fields["cli_version"]:
            fail(f"MCP and CLI versions differ: {initialized!r} vs {fields!r}")
        listed = request(process, 2, "tools/list", {})
        tools = listed.get("result", {}).get("tools", [])
        if len(tools) != 23:
            fail(f"tool count = {len(tools)}, want 23")
        names = {tool["name"] for tool in tools}

        version_name = next((name for name in names if name.endswith("version")), None)
        if version_name is None:
            fail("version tool is missing")
        public_result = request(process, 3, "tools/call", {"name": version_name, "arguments": {}})
        if public_result.get("result", {}).get("isError"):
            fail(f"credentialless version tool failed: {public_result!r}")

        protected_name = next((name for name in names if "cancel" in name and "event" in name), None)
        if protected_name is None:
            fail("protected event cancellation tool is missing")
        protected = request(process, 4, "tools/call", {"name": protected_name, "arguments": {"event_id": "event_test"}})
        encoded = json.dumps(protected, separators=(",", ":"))
        if not protected.get("result", {}).get("isError") or "auth.required" not in encoded:
            fail(f"protected tool did not return auth.required: {protected!r}")

        assert process.stdin is not None
        process.stdin.close()
        process.wait(timeout=5)
        stderr = process.stderr.read() if process.stderr is not None else ""
        if process.returncode != 0 or stderr:
            fail(f"EOF shutdown failed ({process.returncode}): {stderr}")

        signaled = launch(mcp_binary, env)
        time.sleep(0.1)
        signaled.send_signal(signal.SIGTERM)
        signaled.wait(timeout=5)
        signal_stderr = signaled.stderr.read() if signaled.stderr is not None else ""
        if signaled.returncode != 0 or signal_stderr:
            fail(f"signal shutdown failed ({signaled.returncode}): {signal_stderr}")

    print("binary smoke tests passed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, subprocess.TimeoutExpired, json.JSONDecodeError) as error:
        print(f"smoke failure: {error}", file=sys.stderr)
        raise SystemExit(1)
