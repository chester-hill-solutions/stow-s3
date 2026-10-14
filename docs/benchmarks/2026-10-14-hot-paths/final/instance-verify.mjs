import fs from "node:fs";
import { spawn } from "node:child_process";

const root = process.argv[2];
if (!root?.startsWith("/results/hot-paths-") || !fs.existsSync("/src/benchmark/source.json")) throw new Error("Railway instance verification only");
const log = fs.openSync(`${root}/qualification.log`, "a");
const command = "cd /src && go mod download && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && protoc --go_out=/tmp --go_opt=paths=source_relative internal/storage/fs/recordpb/record.proto && cmp /tmp/internal/storage/fs/recordpb/record.pb.go internal/storage/fs/recordpb/record.pb.go && go test -race ./internal/parentwatch ./internal/storage -run '^(TestChildExitsWhenParentIsKilled|TestStorePreparationFailurePreservesObject)$' -count=1 && node benchmark/verify.mjs";
const startedAt = new Date().toISOString();
const child = spawn("python3", ["/src/benchmark/subreaper.py", "su", "nobody", "-s", "/bin/bash", "-c", command], {
  env: { ...process.env, PATH: `/go/bin:${process.env.PATH}`, BENCH_HOT_PATHS_REUSE: process.env.BENCH_HOT_PATHS_REUSE ?? "0" },
  stdio: ["ignore", log, log],
});
fs.writeFileSync(`${root}/status.json`, JSON.stringify({ running: true, pid: child.pid, startedAt }));
child.once("error", error => {
  fs.writeFileSync(`${root}/status.json`, JSON.stringify({ running: false, exitCode: 1, error: error.message, startedAt, finishedAt: new Date().toISOString() }));
});
child.once("close", code => {
  fs.writeFileSync(`${root}/status.json`, JSON.stringify({ running: false, exitCode: code, startedAt, finishedAt: new Date().toISOString() }));
});
