import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { round } from "./metrics.mjs";

function median(values) {
  const sorted = values.filter((v) => v !== null && Number.isFinite(v)).sort((a, b) => a - b);
  if (!sorted.length) return null;
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

export function summarizeRuns(results) {
  const groups = new Map();
  for (const result of results) {
    const key = JSON.stringify(result.scenario);
    const group = groups.get(key) ?? [];
    group.push(result); groups.set(key, group);
  }
  return [...groups.values()].map((runs) => {
    const rates = runs.map((r) => r.load.filesPerSecond);
    const throughput = rates.map((rate) => rate * runs[0].scenario.bytes / 1048576);
    const rps = runs.map((r) => Object.values(r.measurements.operations).reduce((sum, op) => sum + op.requestsPerSecond, 0));
    const rss = runs.map((r) => r.resources.serverPeakSampledRssBytes).filter((value) => value !== null);
    return { ...runs[0].scenario, repetitions: runs.length,
      filesPerSecond: round(median(rates)), payloadMiBPerSecond: round(median(throughput)),
      minPayloadMiBPerSecond: round(Math.min(...throughput)), maxPayloadMiBPerSecond: round(Math.max(...throughput)),
      successfulRequestsPerSecond: round(median(rps)),
      medianRunP99ResponseMs: round(median(runs.map((r) => r.load.responseLatencyMs.p99))),
      medianRunP99ServiceMs: round(median(runs.map((r) => r.load.serviceLatencyMs.p99))),
      peakSampledServerRssMiB: rss.length ? round(Math.max(...rss) / 1048576) : null,
      failedFiles: runs.reduce((sum, r) => sum + r.load.failed, 0),
      droppedFiles: runs.reduce((sum, r) => sum + r.load.dropped, 0),
      minimumCompletedSamples: Math.min(...runs.map((r) => r.load.successful)),
      verified: runs.every((r) => r.verification.complete && (r.scenario.workload !== "quarantine" || r.leftovers.count === 0)),
      diskHeadroomMaintained: runs.every((r) => r.resources.diskHeadroomMaintained),
    };
  });
}

function escape(value) { return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;"); }

export function chart(rows) {
  const points = rows.filter((r) => !r.soak);
  const maxThroughput = Math.max(1, ...points.map((r) => r.payloadMiBPerSecond));
  const positiveLatencies = points.map((r) => r.medianRunP99ResponseMs).filter((value) => value > 0);
  const minPower = Math.floor(Math.log10(Math.min(1, ...positiveLatencies)));
  const maxPower = Math.max(minPower + 1, Math.ceil(Math.log10(Math.max(1, ...positiveLatencies))));
  const colors = { put: "#2563eb", get: "#059669", quarantine: "#9333ea" };
  const dots = points.map((r) => {
    const x = 80 + r.payloadMiBPerSecond / maxThroughput * 630;
    const y = 370 - (Math.log10(Math.max(10 ** minPower, r.medianRunP99ResponseMs ?? 0)) - minPower) / (maxPower - minPower) * 280;
    const label = `${r.backend} ${r.workload}; ${r.bytes} B; concurrency ${r.concurrency}; offered ${r.rate || "closed loop"}; ${r.payloadMiBPerSecond} MiB/s; p99 ${r.medianRunP99ResponseMs} ms; failures ${r.failedFiles}; drops ${r.droppedFiles}`;
    return `<circle cx="${x}" cy="${y}" r="${r.backend === "memory" ? 6 : 9}" fill="${r.backend === "memory" ? colors[r.workload] : "white"}" stroke="${colors[r.workload]}" stroke-width="2"><title>${escape(label)}</title></circle>`;
  }).join("\n");
  const xTicks = [0, 0.25, 0.5, 0.75, 1].map((fraction) => `<text x="${80 + fraction * 630}" y="395" text-anchor="middle">${round(fraction * maxThroughput)}</text>`).join("");
  const yTicks = Array.from({ length: maxPower - minPower + 1 }, (_, index) => {
    const power = minPower + index;
    const y = 370 - index / (maxPower - minPower) * 280;
    return `<path d="M80 ${y}H720" stroke="#e5e7eb"/><text x="65" y="${y + 5}" text-anchor="end">${10 ** power}</text>`;
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" width="850" height="490" viewBox="0 0 850 490" role="img" aria-label="Measured throughput versus response latency"><rect width="850" height="490" fill="white"/><g font-family="sans-serif" font-size="13" fill="#172033"><text x="80" y="30" font-size="20">Stow: throughput and p99 response latency</text><text x="80" y="54">Medians across repetitions; hover over a point for its configuration.</text><path d="M80 85V370H720" fill="none" stroke="#172033"/>${xTicks}${yTicks}${dots}<text x="410" y="425" text-anchor="middle">Completed file payload MiB/s</text><text transform="translate(20 230) rotate(-90)" text-anchor="middle">Median run p99 response (ms, log scale)</text><text x="80" y="458">Blue: PUT · Green: GET · Purple: quarantine · Filled: memory · Hollow: filesystem</text></g></svg>`;
}

export async function writeReport(out, suite) {
  const rows = summarizeRuns(suite.results);
  const config = suite.config;
  const protocolComplete = suite.complete && config.durationSeconds >= 300 && config.warmupSeconds >= 30 && config.repetitions >= 5 && config.soakSeconds >= 1800;
  const lines = ["# Stow performance measurements", "", `Profile: **${config.label.replaceAll("\n", " ")}**. Recorded ${suite.metadata.startedAt}.`, "",
    `Measurement windows: ${config.durationSeconds} s; warm-up: ${config.warmupSeconds} s; repetitions: ${config.repetitions}; soak: ${config.soakSeconds} s per backend.`,
    `Scanner delay: ${config.scannerDelayMs} ms; retained fixtures: ${config.retainedObjects}; queue capacity: ${config.maxQueue}; file deadline: ${config.timeoutMs} ms.`,
    `Suite complete: **${suite.complete}**. See [effective configuration](config.json) and [raw run index](suite.json).`, "",
    protocolComplete ? "The configured duration and repetition protocol is complete. Review each workload before publishing a claim." : "**Exploratory measurements. The public capacity protocol has not been completed.**",
    "", `Source commit: \`${suite.metadata.commit}\`. Dirty checkout: **${suite.metadata.dirty}**.`,
    `Machine: ${suite.metadata.cpu}; ${round(suite.metadata.memoryBytes / 1073741824)} GiB RAM; ${suite.metadata.platform} ${suite.metadata.arch}; ${suite.metadata.osRelease}.`,
    "", "| Backend / workload | Bytes/file | Concurrency | Offered files/s | Runs | Completed files/s | Payload MiB/s (min–max) | Successful RPS | Median run p99 response ms | Peak sampled server MiB | Failed / dropped |",
    "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ...rows.map((r) => `| ${r.backend} / ${r.workload}${r.soak ? " (soak)" : ""} | ${r.bytes} | ${r.concurrency} | ${r.rate || "closed loop"} | ${r.repetitions} | ${r.filesPerSecond} | ${r.payloadMiBPerSecond} (${r.minPayloadMiBPerSecond}–${r.maxPayloadMiBPerSecond}) | ${r.successfulRequestsPerSecond} | ${r.medianRunP99ResponseMs} | ${r.peakSampledServerRssMiB} | ${r.failedFiles} / ${r.droppedFiles} |`),
    "", "![Throughput and latency](throughput-latency.svg)", "", "## Measurement scope", "",
    "- S3 over loopback HTTP, SigV4 signing, AWS SDK, keep-alive connections and one attempt per request. TLS and separate-host networking are unmeasured.",
    "- Quarantine performs PUT, verified GET, simulated scanner delay, verdict PUT and two DELETE requests per successful file. No real scanner or traffic blocking runs.",
    "- Payload throughput counts completed file workflows during the measurement window. RPS counts successful individual API requests during that window. Verdict bytes are separate in raw operation metrics.",
    "- Response latency includes scheduled-arrival queue wait. Service latency starts when a worker takes the file. Dropped arrivals have no latency sample and are reported separately.",
    "- File deadlines include queue wait. Queue-expired files count as failures without issuing API requests or contributing service-time samples. A timed-out mutation can have an unknown outcome; it is not reported as confirmed corruption.",
    "- Accepted work drains after the window. Drain completions enter latency histograms but do not increase window throughput. Resource observations include drain.",
    "- Histograms use bounded bins with approximately 1% resolution. p99 needs enough observations; inspect minimumCompletedSamples in summary.json.",
    "- Fixtures contain random bytes. PUTs reuse one key per worker and change a sequence prefix. Quarantine reuses worker keys after deletion. These test bounded retained sets, not unlimited ingestion.",
    "- Reads stream through SHA-256 verification. Generation, hashing, SDK and verification costs are included in client-observed results.",
    "- Warm-up and measured activity share a warmed server and filesystem cache. Cold-cache disk performance is unmeasured.",
    "- RSS is sampled, not an OS high-water guarantee. CPU sampling uses process CPU time. Allocation rate is unavailable here; embedded Go benchmarks report allocations separately.",
    "- Linux process I/O counters are recorded when available. Other platforms report null. Retained footprint is not cumulative disk write volume.",
    "- Disk headroom is checked before each run and sampled during load. Inspect diskHeadroomMaintained in summary.json before accepting a result.",
    "", "## Recovery", "", suite.recovery ? `Acknowledged files verified after process SIGKILL: **${suite.recovery.verified}** (${suite.recovery.objects} files). Restart to verified: ${round(suite.recovery.restartToVerifiedMs)} ms.` : "Not measured.",
    "This checks process restart after completed writes. Power failure and interrupted publication are unmeasured.",
    "", "## Publication", "",
    "Publish a workload-specific claim with its hardware, backend, object size, concurrency, duration, configuration and raw results. Publish variation and failed/dropped counts.",
    "Review scheduler lag and generator CPU before accepting a capacity result. Growing queue wait or large scheduling lag can indicate an overloaded client or server.",
    "The chart includes overloaded points. Zero errors, verified data and stable queueing are required before describing a rate as sustainable.",
    "This report does not establish production readiness, a latency SLA, crash durability under power loss, or DPI line-rate capacity.", ""];
  await writeFile(join(out, "report.md"), lines.join("\n"));
  await writeFile(join(out, "summary.json"), JSON.stringify({ protocolComplete, rows }, null, 2) + "\n");
  await writeFile(join(out, "throughput-latency.svg"), chart(rows));
}
