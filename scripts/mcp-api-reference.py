"""Extract input and output schemas from the native MCP server."""
import json
import selectors
import subprocess
import tempfile
from pathlib import Path


def catalog(binary, args):
    process = subprocess.Popen([str(binary), "mcp", *args], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    selected = selectors.DefaultSelector()
    selected.register(process.stdout, selectors.EVENT_READ)
    def send(message):
        process.stdin.write(json.dumps(message) + "\n")
        process.stdin.flush()
    def response(identifier):
        for _ in range(20):
            if not selected.select(timeout=1):
                continue
            line = process.stdout.readline()
            if not line:
                raise RuntimeError("MCP server exited before schema response: " + process.stderr.read())
            message = json.loads(line)
            if message.get("id") == identifier:
                if "error" in message:
                    raise RuntimeError(message["error"])
                return message["result"]
        raise RuntimeError("MCP schema request timed out")
    try:
        send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-03-26", "capabilities": {}, "clientInfo": {"name": "api-docs", "version": "1"}}})
        response(1)
        send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        send({"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
        return response(2)["tools"]
    finally:
        selected.close()
        process.stdin.close()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.terminate()
            process.wait(timeout=5)
        process.stdout.close()
        process.stderr.close()


with tempfile.TemporaryDirectory(prefix="stow-api-docs-") as directory:
    root = Path(directory)
    binary = root / "stow-s3"
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/stow-s3"], check=True, capture_output=True)
    objects = catalog(binary, ["--object-dir", str(root / "objects"), "--bucket", "docs", "--create-bucket", "--browser"])
    (root / "seed.txt").write_text("API fixture\n")
    manifest = root / "task.json"
    manifest.write_text(json.dumps({"version": 1, "root": str(root / "workspace"), "registry_dir": str(root / "registry"), "inputs": [{"source": str(root / "seed.txt"), "destination": "seed.txt"}]}))
    task = json.loads(subprocess.check_output([str(binary), "workspace", "prepare", "--manifest", str(manifest)]))
    for name in ["export", "adopt"]:
        (root / name).mkdir()
    workspace = catalog(binary, ["--registry-dir", str(root / "registry"), "--workspace-id", task["workspace_id"], "--timeout", "30s", "--max-bytes", "1048576", "--max-files", "100", "--export-root", str(root / "export"), "--adopt-root", str(root / "adopt")])
    tools = []
    for profile, entries in [("Object storage", objects), ("Workspaces", workspace)]:
        for tool in entries:
            tools.append({"name": tool["name"], "profile": profile, "description": tool["description"], "inputSchema": tool["inputSchema"], "outputSchema": tool.get("outputSchema")})
    print(json.dumps(sorted(tools, key=lambda tool: tool["name"])))
