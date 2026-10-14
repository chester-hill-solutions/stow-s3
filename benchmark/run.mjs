import { mkdir, writeFile, readFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { createWriteStream } from "node:fs";
import { resolve, join, basename } from "node:path";
import { runS3 } from "./s3.mjs";
import { systemMetadata } from "./resources.mjs";
import { runNodeAgents } from "./node-agents.mjs";

const profileName = process.argv[2];
const out = resolve(process.argv[3]);
if (!["smoke", "comparison", "regression", "controlled"].includes(profileName)) throw new Error("choose smoke, comparison, regression or controlled");
const host = process.env.BENCH_TARGET_HOST;
if (!host || !/^[a-z0-9.-]+$/i.test(host)) throw new Error("BENCH_TARGET_HOST is required");
const profile = JSON.parse(await readFile(`benchmark/profiles/${profileName}.json`, "utf8"));
await mkdir(out, { recursive: true });
await writeFile(join(out, "profile.json"), JSON.stringify(profile, null, 2) + "\n", { flag: "wx" });
const receipt = { version: 1, runId: basename(out), startedAt: new Date().toISOString(), profileName, metadata: await systemMetadata(), complete: false };
await writeFile(join(out, "run.json"), JSON.stringify(receipt, null, 2) + "\n");
try {
  if (profile.nodeAgent) receipt.nodeAgent = await runNodeAgents(profile.nodeAgent, out);
  if (profile.agent) {
    if (!Number.isSafeInteger(profile.agent.samples) || profile.agent.samples < 2 || profile.agent.samples > 100) throw new Error("invalid agent sample count");
    const args = ["-test.run=^$", "-test.bench=^BenchmarkAgentStartup$", `-test.benchtime=${profile.agent.samples}x`, "-test.count=1", "-test.cpu=1"];
    await runEmbedded("agent.txt", args);
    receipt.agent = { complete: true, args };
  }
  const args = ["-test.run=^$", `-test.bench=${profile.embedded.pattern ?? "^BenchmarkComparison$"}`, "-test.benchmem",
    `-test.benchtime=${profile.embedded.benchtime}`, `-test.count=${profile.embedded.count}`, `-test.cpu=${profile.embedded.cpu}`];
  if (profile.pairedVariants) {
    const outputs = { before: [], after: [] };
    for (let repetition = 1; repetition <= profile.repetitions; repetition++) {
      for (const variant of repetition % 2 ? ["before", "after"] : ["after", "before"]) {
        const name = `embedded-${variant}-${repetition}.txt`;
        await runEmbedded(name, args, variant === "before" ? "bin/embedded-before.test" : "bin/embedded.test");
        outputs[variant].push(await readFile(join(out, name), "utf8"));
        await runEmbedded(`managed-${variant}-${repetition}.txt`, ["-test.run=^$", "-test.bench=^BenchmarkManagedSave$", "-test.benchmem", "-test.benchtime=64x", "-test.count=1", "-test.cpu=4"], variant === "before" ? "bin/embedded-before.test" : "bin/embedded.test");
      }
    }
    for (const [variant, pieces] of Object.entries(outputs)) await writeFile(join(out, `embedded-${variant}.txt`), pieces.join("\n"));
    receipt.embedded = { complete: true, args, pairedVariants: true, rounds: profile.repetitions };
  } else {
    await runEmbedded("embedded.txt", args);
    receipt.embedded = { complete: true, args };
  }
  receipt.s3 = await runS3(profile, join(out, "s3"), host);
  receipt.complete = true;
  console.log(`Saved complete comparison to ${out}`);
} catch (error) { receipt.error = error.message; process.exitCode = 1; }
finally {
  receipt.finishedAt = new Date().toISOString();
  await writeFile(join(out, "run.json"), JSON.stringify(receipt, null, 2) + "\n");
}

async function runEmbedded(name, args, binary = "bin/embedded.test") {
  const output = createWriteStream(join(out, name), { flags: "wx" });
  await new Promise((done, reject) => {
    const child = spawn(binary, args, { env: { ...process.env, TMPDIR: "/tmp" }, stdio: ["ignore", "pipe", "pipe"] });
    child.stdout.pipe(output); child.stderr.pipe(process.stderr);
    child.once("error", reject); output.once("error", reject);
    child.once("close", code=>code === 0 ? output.writableFinished ? done() : output.once("finish", done) : reject(new Error(`embedded benchmark exited ${code}`)));
  });
}
