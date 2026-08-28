#!/usr/bin/env python3
"""Build and black-box test the release-shaped Partiful binaries."""

from __future__ import annotations

import json
import os
from pathlib import Path
import selectors
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]


def fail(message: str) -> None:
    raise AssertionError(message)


def run(command: list[str], *, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        fail(f"{' '.join(command)} failed ({result.returncode}): {result.stderr}")
    return result


def write_frame(process: subprocess.Popen[str], frame: dict) -> None:
    assert process.stdin is not None
    process.stdin.write(json.dumps(frame, separators=(",", ":")) + "\n")
    process.stdin.flush()


def read_response(process: subprocess.Popen[str], method: str, timeout: float = 5) -> dict:
    assert process.stdout is not None
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ)
        if not selector.select(timeout):
            fail(f"MCP response to {method} exceeded {timeout} seconds")
    line = process.stdout.readline()
    if not line:
        fail(f"MCP server closed before response to {method}")
    response = json.loads(line)
    if response.get("jsonrpc") != "2.0":
        fail(f"MCP stdout contained a non-protocol frame: {response!r}")
    return response


def request(process: subprocess.Popen[str], identifier: int, method: str, params: dict | None = None) -> dict:
    frame = {"jsonrpc": "2.0", "id": identifier, "method": method}
    if params is not None:
        frame["params"] = params
    write_frame(process, frame)
    response = read_response(process, method)
    if response.get("id") != identifier:
        fail(f"MCP response ID mismatch: {response!r}")
    return response


def launch(binary: Path, env: dict[str, str]) -> subprocess.Popen[str]:
    return subprocess.Popen(
        [str(binary)], cwd=ROOT, env=env, text=True,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )


class BlockingProxy:
    """A local HTTP proxy that proves a request is in flight without going live."""

    def __init__(self) -> None:
        self.listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen(1)
        self.accepted = threading.Event()
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self._serve, daemon=True)

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.listener.getsockname()[1]}"

    def __enter__(self) -> BlockingProxy:
        self.thread.start()
        return self

    def __exit__(self, *_: object) -> None:
        self.stop.set()
        self.listener.close()
        self.thread.join(timeout=1)

    def _serve(self) -> None:
        try:
            connection, _ = self.listener.accept()
        except OSError:
            return
        self.accepted.set()
        connection.settimeout(0.1)
        with connection:
            while not self.stop.is_set():
                try:
                    if not connection.recv(4096):
                        return
                except socket.timeout:
                    continue
                except OSError:
                    return

    def wait_for_request(self, timeout: float = 5) -> None:
        if not self.accepted.wait(timeout):
            fail(f"MCP request did not reach the local blocking proxy within {timeout} seconds")


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

        with BlockingProxy() as proxy:
            env["HTTPS_PROXY"] = proxy.url
            env["https_proxy"] = proxy.url
            env["NO_PROXY"] = ""
            env["no_proxy"] = ""

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

            write_frame(process, {"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": {"name": "posters_list", "arguments": {}}})
            proxy.wait_for_request()
            write_frame(process, {"jsonrpc": "2.0", "method": "notifications/cancelled", "params": {"requestId": 5, "reason": "smoke"}})
            cancelled = read_response(process, "notifications/cancelled")
            cancelled_encoded = json.dumps(cancelled, separators=(",", ":"))
            if cancelled.get("id") != 5 or not cancelled.get("result", {}).get("isError") or "REQUEST_CANCELLED" not in cancelled_encoded:
                fail(f"in-flight request was not cancelled: {cancelled!r}")

            survived = request(process, 6, "tools/call", {"name": version_name, "arguments": {}})
            if survived.get("result", {}).get("isError"):
                fail(f"MCP server did not survive cancellation: {survived!r}")

            assert process.stdin is not None
            process.stdin.close()
            process.wait(timeout=5)
            remaining_stdout = process.stdout.read() if process.stdout is not None else ""
            stderr = process.stderr.read() if process.stderr is not None else ""
            if process.returncode != 0 or remaining_stdout or stderr:
                fail(f"EOF shutdown or stdout purity failed ({process.returncode}): stdout={remaining_stdout!r} stderr={stderr!r}")

        signaled = launch(mcp_binary, env)
        time.sleep(0.1)
        signaled.send_signal(signal.SIGTERM)
        signaled.wait(timeout=5)
        signal_stdout = signaled.stdout.read() if signaled.stdout is not None else ""
        signal_stderr = signaled.stderr.read() if signaled.stderr is not None else ""
        if signaled.returncode != 0 or signal_stdout or signal_stderr:
            fail(f"signal shutdown or stdout purity failed ({signaled.returncode}): stdout={signal_stdout!r} stderr={signal_stderr!r}")

    print("binary smoke tests passed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, subprocess.TimeoutExpired, json.JSONDecodeError) as error:
        print(f"smoke failure: {error}", file=sys.stderr)
        raise SystemExit(1)
