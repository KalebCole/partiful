#!/usr/bin/env python3
"""Build and black-box test the release-shaped Partiful binaries."""

from __future__ import annotations

import json
import os
from pathlib import Path
import queue
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
from typing import NoReturn, TextIO, cast

ROOT = Path(__file__).resolve().parents[1]
SHUTDOWN_BOUND_SECONDS = 5.0
EXPECTED_MCP_TOOLS = (
    "auth_status",
    "auth_logout",
    "events_list",
    "events_get",
    "events_create",
    "events_update",
    "events_cancel",
    "guests_list",
    "guests_invite",
    "rsvp_get",
    "rsvp_set",
    "contacts_list",
    "cohosts_invite",
    "cohosts_revoke_invite",
    "cohosts_remove",
    "cohosts_link_create",
    "cohosts_link_revoke",
    "blasts_send",
    "posters_list",
    "posters_search",
    "schema",
    "doctor",
    "version",
)


def fail(message: str) -> NoReturn:
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


class PipeReader:
    """Read a subprocess pipe on every supported host, including Windows."""

    def __init__(self, pipe: TextIO) -> None:
        self.lines: queue.Queue[str | None] = queue.Queue()
        self.thread = threading.Thread(target=self._read, args=(pipe,), daemon=True)
        self.thread.start()

    def _read(self, pipe: TextIO) -> None:
        while True:
            line = pipe.readline()
            if not line:
                self.lines.put(None)
                return
            self.lines.put(line)

    def line(self, timeout: float, context: str) -> str:
        try:
            line = self.lines.get(timeout=timeout)
        except queue.Empty:
            fail(f"MCP response to {context} exceeded {timeout} seconds")
        if line is None:
            fail(f"MCP server closed before response to {context}")
        return line

    def remaining(self, context: str) -> list[dict]:
        frames = []
        while True:
            try:
                line = self.lines.get(timeout=SHUTDOWN_BOUND_SECONDS)
            except queue.Empty:
                fail(f"MCP stdout did not close during {context}")
            if line is None:
                return frames
            try:
                frame = json.loads(line)
            except json.JSONDecodeError as error:
                fail(f"MCP stdout contained non-protocol data during {context}: {line!r} ({error})")
            if frame.get("jsonrpc") != "2.0":
                fail(f"MCP stdout contained a non-protocol frame during {context}: {frame!r}")
            frames.append(frame)


PIPE_READERS: dict[int, PipeReader] = {}


def pipe_reader(process: subprocess.Popen[str]) -> PipeReader:
    return PIPE_READERS[id(process)]


def read_response(process: subprocess.Popen[str], method: str, timeout: float = 5) -> dict:
    line = pipe_reader(process).line(timeout, method)
    response = json.loads(line)
    if response.get("jsonrpc") != "2.0":
        fail(f"MCP stdout contained a non-protocol frame: {response!r}")
    return response


def read_remaining_protocol_frames(process: subprocess.Popen[str], context: str) -> list[dict]:
    return pipe_reader(process).remaining(context)


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
    creationflags = subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0
    process = subprocess.Popen(
        [str(binary)], cwd=ROOT, env=env, text=True,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        creationflags=creationflags,
    )
    assert process.stdout is not None
    PIPE_READERS[id(process)] = PipeReader(cast(TextIO, process.stdout))
    return process


class BlockingProxy:
    """A local HTTP proxy that proves a request is in flight without going live."""

    def __init__(self) -> None:
        self.listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen()
        self.listener.settimeout(0.1)
        self.accepted = threading.Event()
        self.stop = threading.Event()
        self.connections: list[socket.socket] = []
        self.connection_threads: list[threading.Thread] = []
        self.lock = threading.Lock()
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
        with self.lock:
            connections = list(self.connections)
        for connection in connections:
            connection.close()
        self.thread.join(timeout=1)
        for thread in self.connection_threads:
            thread.join(timeout=1)

    def _serve(self) -> None:
        while not self.stop.is_set():
            try:
                connection, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            with self.lock:
                self.connections.append(connection)
            self.accepted.set()
            thread = threading.Thread(target=self._hold, args=(connection,), daemon=True)
            self.connection_threads.append(thread)
            thread.start()

    def _hold(self, connection: socket.socket) -> None:
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

    def connection_count(self) -> int:
        with self.lock:
            return len(self.connections)


def assert_tool_error(response: dict, error_type: str, code: str, context: str) -> None:
    result = response.get("result", {})
    error = result.get("structuredContent", {}).get("error", {})
    if not result.get("isError") or error.get("type") != error_type or error.get("code") != code:
        fail(f"{context} returned an unexpected tool error: {response!r}")


def install_fake_credential(env: dict[str, str]) -> None:
    if os.name == "nt":
        root = Path(env["LOCALAPPDATA"]) / "Partiful"
    elif sys.platform == "darwin":
        root = Path(env["HOME"]) / "Library" / "Application Support" / "partiful"
    else:
        root = Path(env["XDG_DATA_HOME"]) / "partiful"
    credential_dir = root / "credentials"
    credential_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    credential = credential_dir / "slot-a.json"
    credential.write_text(json.dumps({
        "schema_version": 1,
        "generation": 1,
        "account_identity": "local-fixture-account",
        "access_token": "local-fixture-access",
        "refresh_token": "local-fixture-refresh",
        "installation_secret": "local-fixture-installation",
    }), encoding="utf-8")
    credential.chmod(0o600)


def prepare_windows_credentialless_store(env: dict[str, str]) -> None:
    """Select the native vault without probing or writing a credential."""
    root = Path(env["LOCALAPPDATA"]) / "Partiful"
    root.mkdir(parents=True, exist_ok=True)
    (root / "credential-backend").write_text("os-credential-store", encoding="utf-8")


def shutdown_signals() -> list[tuple[str, signal.Signals | int]]:
    if os.name == "nt":
        return [("CTRL_BREAK_EVENT", signal.CTRL_BREAK_EVENT)]
    return [("SIGINT", signal.SIGINT), ("SIGTERM", signal.SIGTERM)]


def verify_signal_shutdown(binary: Path, env: dict[str, str], name: str, shutdown_signal: signal.Signals | int) -> None:
    with BlockingProxy() as proxy:
        signal_env = env.copy()
        signal_env["HTTPS_PROXY"] = proxy.url
        signal_env["https_proxy"] = proxy.url
        signal_env["NO_PROXY"] = ""
        signal_env["no_proxy"] = ""
        process = launch(binary, signal_env)
        initialized = request(process, 100, "initialize", {
            "protocolVersion": "2025-06-18",
            "capabilities": {},
            "clientInfo": {"name": "shutdown-smoke", "version": "1"},
        })
        if initialized.get("result", {}).get("serverInfo", {}).get("name") != "partiful":
            fail(f"MCP server was not ready before {name}: {initialized!r}")
        shutdown_tool = "posters_list" if os.name == "nt" else "events_cancel"
        shutdown_arguments = {} if os.name == "nt" else {
            "event_id": "event_local_fixture", "notify_guests": False,
        }
        write_frame(process, {
            "jsonrpc": "2.0",
            "id": 101,
            "method": "tools/call",
            "params": {
                "name": shutdown_tool,
                "arguments": shutdown_arguments,
            },
        })
        proxy.wait_for_request()
        started = time.monotonic()
        process.send_signal(shutdown_signal)
        # The cancellation reply proves the signal has reached the server. A
        # later frame must not receive a response or reach the loopback proxy.
        frames = [read_response(process, f"{name} cancellation")]
        post_signal_frame = {
            "jsonrpc": "2.0",
            "id": 102,
            "method": "tools/call",
            "params": {"name": shutdown_tool, "arguments": shutdown_arguments},
        }
        try:
            write_frame(process, post_signal_frame)
        except BrokenPipeError:
            pass
        assert process.stdin is not None
        try:
            process.stdin.close()
        except BrokenPipeError:
            pass
        process.wait(timeout=SHUTDOWN_BOUND_SECONDS)
        elapsed = time.monotonic() - started
        if elapsed > SHUTDOWN_BOUND_SECONDS:
            fail(f"{name} shutdown took {elapsed:.3f}s, bound is {SHUTDOWN_BOUND_SECONDS:.1f}s")
        frames.extend(read_remaining_protocol_frames(process, name))
        stderr = process.stderr.read() if process.stderr is not None else ""
        if process.returncode != 0 or stderr:
            fail(f"{name} shutdown failed ({process.returncode}): stderr={stderr!r}")
        if len(frames) != 1 or frames[0].get("id") != 101:
            fail(f"{name} accepted a post-signal frame or did not drain exactly one request: {frames!r}")
        assert_tool_error(frames[0], "internal.failure", "REQUEST_CANCELLED", name)
        if '"submitted":true' in json.dumps(frames[0], separators=(",", ":")):
            fail(f"{name} reported mutation confirmation after cancellation: {frames[0]!r}")
        if proxy.connection_count() != 1:
            fail(f"{name} mutation dispatch count = {proxy.connection_count()}, want exactly 1")


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="partiful-smoke-") as temporary:
        temp = Path(temporary)
        executable_suffix = ".exe" if os.name == "nt" else ""
        cli_binary = temp / f"partiful{executable_suffix}"
        mcp_binary = temp / f"partiful-mcp{executable_suffix}"
        run(["go", "build", "-trimpath", "-o", str(cli_binary), "./cmd/partiful"])
        run(["go", "build", "-trimpath", "-o", str(mcp_binary), "./cmd/partiful-mcp"])

        env = os.environ.copy()
        env["HOME"] = str(temp / "home")
        env["XDG_DATA_HOME"] = str(temp / "data")
        env["LOCALAPPDATA"] = str(temp / "data")
        # Prevent discovery of a host OS credential helper. Windows uses its
        # native PasswordVault for credentialless startup on a clean hosted
        # runner, so its system PATH must remain available for PowerShell.
        if os.name != "nt":
            env["PATH"] = str(temp / "empty-path")
        else:
            prepare_windows_credentialless_store(env)

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
            names = tuple(tool.get("name") for tool in tools)
            if names != EXPECTED_MCP_TOOLS:
                fail(f"tool listing = {names!r}, want {EXPECTED_MCP_TOOLS!r}")

            public_result = request(process, 3, "tools/call", {"name": "version", "arguments": {}})
            if public_result.get("result", {}).get("isError"):
                fail(f"credentialless version tool failed: {public_result!r}")

            protected = request(process, 4, "tools/call", {"name": "events_cancel", "arguments": {"event_id": "event_test"}})
            assert_tool_error(protected, "auth.required", "AUTH_REQUIRED", "protected tool")

            write_frame(process, {"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": {"name": "posters_list", "arguments": {}}})
            proxy.wait_for_request()
            write_frame(process, {"jsonrpc": "2.0", "method": "notifications/cancelled", "params": {"requestId": 5, "reason": "smoke"}})
            cancelled = read_response(process, "notifications/cancelled")
            if cancelled.get("id") != 5:
                fail(f"in-flight request was not cancelled: {cancelled!r}")
            assert_tool_error(cancelled, "internal.failure", "REQUEST_CANCELLED", "in-flight request")

            survived = request(process, 6, "tools/call", {"name": "version", "arguments": {}})
            if survived.get("result", {}).get("isError"):
                fail(f"MCP server did not survive cancellation: {survived!r}")

            assert process.stdin is not None
            process.stdin.close()
            process.wait(timeout=SHUTDOWN_BOUND_SECONDS)
            remaining_frames = read_remaining_protocol_frames(process, "EOF shutdown")
            stderr = process.stderr.read() if process.stderr is not None else ""
            if process.returncode != 0 or remaining_frames or stderr:
                fail(f"EOF shutdown or stdout purity failed ({process.returncode}): frames={remaining_frames!r} stderr={stderr!r}")

        if os.name != "nt":
            install_fake_credential(env)
        verified_signals = []
        for name, shutdown_signal in shutdown_signals():
            verify_signal_shutdown(mcp_binary, env, name, shutdown_signal)
            verified_signals.append(name)

    print(
        "binary smoke tests passed: "
        "protocol=2025-06-18 tools=23 public=version protected=auth.required "
        "cancellation=REQUEST_CANCELLED eof=clean "
        f"shutdown={','.join(verified_signals)}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, subprocess.TimeoutExpired, json.JSONDecodeError) as error:
        print(f"smoke failure: {error}", file=sys.stderr)
        raise SystemExit(1)
