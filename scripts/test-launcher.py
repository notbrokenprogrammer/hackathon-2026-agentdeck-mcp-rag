#!/usr/bin/env python3
"""Check a compiled launcher and its real MCP stdio mode without TCP listeners."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def main():
    require(len(sys.argv) <= 2, "usage: test-launcher.py [path/to/lastcrew]")
    binary = Path(sys.argv[1] if len(sys.argv) == 2 else "./lastcrew").resolve()
    require(binary.is_file(), f"Build the launcher first: missing {binary}")
    checks = []
    with tempfile.TemporaryDirectory(prefix="lastcrew-launcher-") as temporary:
        data = Path(temporary) / "unused-data"
        env = dict(os.environ, DATA_DIR=str(data), OLLAMA_ENABLED="false",
                   RAG_CHUNKS_FILE=str(ROOT / "knowledge"),
                   HOST_ADDR="invalid-host-address")
        for name in ("MCP_WORLD_URL", "MCP_WORLD_TOKEN", "MCP_BINARY"):
            env.pop(name, None)

        def run(args, payload="", extra_env=None):
            result = subprocess.run([str(binary), *args], input=payload, text=True,
                                    capture_output=True, cwd=ROOT, timeout=15,
                                    env=dict(env, **(extra_env or {})))
            require(not data.exists(), f"{args}: launcher created DATA_DIR before host startup")
            return result

        def passed(name):
            checks.append(name)
            print("PASS:", name, flush=True)

        result = run(["--help"])
        require(result.returncode == 0 and "go run ." in result.stdout and
                "--mcp" in result.stdout and not result.stderr, "Launcher help failed")
        passed("help works without server configuration or database writes")

        result = run(["--unknown-launcher-flag"])
        require(result.returncode != 0 and result.stderr and not result.stdout,
                "Unknown launcher flag was accepted or started the host")
        passed("unknown launcher flag is rejected")

        for flag in ("--share", "--install-tunnel"):
            result = run([flag, "--unexpected"])
            require(result.returncode == 2 and "Use:" in result.stderr and not result.stdout,
                    f"Extra arguments to {flag} were accepted")
        passed("sharing and installation reject extra arguments before side effects")

        result = run(["--mcp", "-check"])
        require(result.returncode == 0 and not result.stderr, f"MCP corpus check failed: {result.stderr}")
        status = json.loads(result.stdout)
        require(status.get("chunks", 0) > 0 and status.get("total_loaded_chunks", 0) > 0,
                f"MCP did not load the shipped corpus: {status}")
        passed("embedded MCP validates the shipped corpus")

        requests = [
            {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                "protocolVersion": "2025-11-25", "capabilities": {},
                "clientInfo": {"name": "launcher-check", "version": "1"}}},
            {"jsonrpc": "2.0", "method": "notifications/initialized"},
            {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
             "params": {"name": "rag_status", "arguments": {}}},
            {"jsonrpc": "2.0", "id": 3, "method": "tools/list", "params": {}},
        ]
        result = run(["--mcp"], "".join(json.dumps(item) + "\n" for item in requests))
        require(result.returncode == 0 and not result.stderr, f"MCP protocol failed: {result.stderr}")
        replies = [json.loads(line) for line in result.stdout.splitlines()]
        require(len(replies) == 3 and [item.get("id") for item in replies] == [1, 2, 3],
                "MCP emitted host logs, notification replies, or unexpected response IDs")
        require(all(item.get("jsonrpc") == "2.0" and "error" not in item for item in replies),
                f"MCP returned protocol errors: {replies}")
        require(replies[0]["result"]["protocolVersion"] == "2025-11-25", "MCP negotiation failed")
        call = replies[1]["result"]
        require(not call.get("isError") and call["structuredContent"]["chunks"] == status["chunks"],
                "MCP rag_status did not return the loaded corpus")
        names = {tool["name"] for tool in replies[2]["result"]["tools"]}
        require({"rag_status", "agents_list", "dzhanibekov_search"} <= names,
                "MCP tool definitions are missing")
        passed("real MCP initialize, notification, tools/call and tools/list return clean JSON-RPC")

        view = str(Path(temporary) / "view.json")
        mission = str(Path(temporary) / "mission.json")
        invalid_bindings = [
            ["-player-id", "alice"],
            ["-view-file", view],
            ["-mission-file", mission],
            ["-player-id", "alice", "-view-file", view, "-mission-file", mission],
        ]
        for flags in invalid_bindings:
            result = run(["--mcp", "-check", *flags])
            require(result.returncode != 0 and not result.stdout and
                    "use -player-id with exactly one" in result.stderr,
                    f"Invalid trusted actor binding was accepted: {flags}")
        passed("incomplete and conflicting trusted actor bindings are rejected")

        result = run(["--mcp", "-check", "-player-id", "alice", "-view-file", view],
                     extra_env={"MCP_WORLD_URL": "http://example.invalid:8080",
                                "MCP_WORLD_TOKEN": "disposable-test-token"})
        require(result.returncode != 0 and not result.stdout and "must be loopback" in result.stderr,
                "MCP accepted a non-loopback world endpoint")
        passed("non-loopback MCP world endpoint is rejected before any network request")
        passed("all launcher and MCP commands leave DATA_DIR absent")
    print(json.dumps({"passed": True, "binary": str(binary), "checks": checks}))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.TimeoutExpired) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
