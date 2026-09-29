import json
import tempfile
from pathlib import Path
from stow_s3 import __version__, with_session, prepare_workspace, serve_workspace

with with_session() as session:
    assert session.endpoint.startswith("http://127.0.0.1:")
    assert session.binary_version == __version__
with tempfile.TemporaryDirectory(prefix="stow-consumer-") as directory:
    root = Path(directory)
    (root / "input.txt").write_text("portable")
    manifest = {"version": 1, "root": str(root / "workspace"), "registry_dir": str(root / "registry"),
                "inputs": [{"source": str(root / "input.txt"), "destination": "input.txt"}]}
    (root / "task.json").write_text(json.dumps(manifest))
    prepared = prepare_workspace(str(root / "task.json"))
    with serve_workspace(prepared["workspace_id"], registry_dir=prepared["registry_dir"]):
        pass
    assert (root / "workspace" / "input.txt").read_text() == "portable"
print("Clean Python consumer session and persistent workspace passed")
