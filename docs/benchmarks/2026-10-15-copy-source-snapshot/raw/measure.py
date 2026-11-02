from pathlib import Path
import hashlib, json, platform, subprocess

out = Path(__file__).resolve().parent
meta = {
    "baseline": "b56ee81",
    "candidate": "captured-source COPY change on b56ee81",
    "go": "go1.25.6",
    "platform": platform.platform(),
    "cpu": subprocess.check_output(["sysctl", "-n", "machdep.cpu.brand_string"], text=True).strip(),
    "memory_bytes": subprocess.check_output(["sysctl", "-n", "hw.memsize"], text=True).strip(),
    "method": "Serial baseline/candidate/candidate/baseline blocks. Standalone processes with no concurrent build/test jobs. Three timed COPY operations per large-object sample plus calibration. Peak RSS includes setup, object storage and allocator retention. Fixture streams known-length data to filesystem, transfers owned bytes to memory and runs GC before timing."
}
meta["binaries"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in out.glob("*.test")}
(out / "metadata.json").write_text(json.dumps(meta, indent=2) + "\n")
for block, version in enumerate(["baseline", "candidate", "candidate", "baseline"], 1):
    with (out / f"{block}-{version}-conditions.txt").open("w") as f:
        subprocess.run([str(out / (version + "-runthrough.test")), "-test.run", "^$", "-test.bench", "^BenchmarkCopyConditions$", "-test.benchtime=300ms", "-test.count=3", "-test.benchmem"], stdout=f, stderr=subprocess.STDOUT, check=True)
    for profile in ["native", "fallback"]:
        for backend in ["filesystem", "memory"]:
            for size in [1, 16, 64]:
                args = ["/usr/bin/time", "-l", str(out / (version + "-s3.test")), "-test.run", "^$", "-test.bench", f"^BenchmarkConditionalCopy$/{profile}$/{backend}$/{size}MiB$", "-test.benchtime=3x", "-test.count=1", "-test.benchmem"]
                with (out / f"{block}-{version}-{profile}-{backend}-{size}MiB.txt").open("w") as f:
                    subprocess.run(args, stdout=f, stderr=subprocess.STDOUT, check=True)
    print(f"Completed serial measurement block {block}: {version}", flush=True)
