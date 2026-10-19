from pathlib import Path
import hashlib, json, platform, subprocess

out = Path(__file__).resolve().parent
meta = {
    "versions": {"baseline": "b56ee81", "regressed": "959f3b5", "restored": "production sources restored to b56ee81, with added regression coverage"},
    "go": "go1.25.6",
    "platform": platform.platform(),
    "cpu": subprocess.check_output(["sysctl", "-n", "machdep.cpu.brand_string"], text=True).strip(),
    "memory_bytes": subprocess.check_output(["sysctl", "-n", "hw.memsize"], text=True).strip(),
    "method": "Serial baseline/regressed/restored/restored/regressed/baseline blocks. Same benchmark functions and fixture method. No concurrent builds or tests. Each large-object process executes three timed copies plus calibration. RSS includes setup, calibration fixture lifetimes and allocator retention. Regressed binaries are reused from the preceding source-snapshot evidence with hashes verified against its metadata."
}
prior = json.loads(Path('/private/tmp/stow-copy-snapshot-evidence/metadata.json').read_text())
meta["binaries"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in out.glob("*.test")}
for suffix in ["s3.test", "runthrough.test"]:
    assert meta["binaries"]["regressed-" + suffix] == prior["binaries"]["candidate-" + suffix]
(out / "metadata.json").write_text(json.dumps(meta, indent=2) + "\n")
for block, version in enumerate(["baseline", "regressed", "restored", "restored", "regressed", "baseline"], 1):
    with (out / f"{block}-{version}-conditions.txt").open("w") as f:
        subprocess.run([str(out / (version + "-runthrough.test")), "-test.run", "^$", "-test.bench", "^BenchmarkCopyConditions$", "-test.benchtime=300ms", "-test.count=3", "-test.benchmem"], stdout=f, stderr=subprocess.STDOUT, check=True)
    for profile, backend, size in [("fallback", "memory", 1), ("fallback", "memory", 64), ("native", "memory", 64), ("fallback", "filesystem", 64)]:
        args = ["/usr/bin/time", "-l", str(out / (version + "-s3.test")), "-test.run", "^$", "-test.bench", f"^BenchmarkConditionalCopy$/{profile}$/{backend}$/{size}MiB$", "-test.benchtime=3x", "-test.count=1", "-test.benchmem"]
        with (out / f"{block}-{version}-{profile}-{backend}-{size}MiB.txt").open("w") as f:
            subprocess.run(args, stdout=f, stderr=subprocess.STDOUT, check=True)
    print(f"Completed serial measurement block {block}: {version}", flush=True)
