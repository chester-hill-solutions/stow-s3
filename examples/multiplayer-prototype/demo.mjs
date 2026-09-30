import assert from "node:assert/strict";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

export const ordersCSV = "order_id,region,channel,units,unit_price\n1001,North,Online,4,25\n1002,South,Retail,2,40\n1003,North,Retail,3,30\n1004,West,Online,5,20\n1005,South,Online,1,60\n1006,West,Retail,2,45\n";
export const participants = [
  { id: "analyst", name: "Mira / Analyst", role: "Calculate revenue and publish the data contract", files: ["data/metrics.json"], subscriptions: [] },
  { id: "designer", name: "Jules / Designer", role: "Build a revenue dashboard and follow data changes", files: ["site/index.html"], subscriptions: ["data/metrics.json"] },
  { id: "auditor", name: "Ren / Auditor", role: "Inspect source quality independently", files: ["docs/data-quality.md"], subscriptions: ["docs/"] },
];

export function calculateMetrics(csv) {
  const rows = csv.trim().split("\n").slice(1).map(line => line.split(","));
  const result = { revenueCents: 0, orderCount: rows.length, units: 0, byRegionCents: {}, byChannelCents: {} };
  for (const [, region, channel, unitsText, priceText] of rows) {
    const units = Number(unitsText);
    const cents = units * Math.round(Number(priceText) * 100);
    result.units += units; result.revenueCents += cents;
    result.byRegionCents[region] = (result.byRegionCents[region] ?? 0) + cents;
    result.byChannelCents[channel] = (result.byChannelCents[channel] ?? 0) + cents;
  }
  return result;
}

export async function seedDemo(root) {
  for (const directory of ["data", "site", "docs"]) await mkdir(join(root, directory), { recursive: true });
  await writeFile(join(root, "data/orders.csv"), ordersCSV);
  await writeFile(join(root, "data/metrics.json"), JSON.stringify({ total: 0, rows: 0, status: "awaiting analyst" }, null, 2));
  await writeFile(join(root, "TASK.md"), "Build a revenue observatory together. All unit_price values in data/orders.csv are US dollars, not cents; revenueCents = sum(units * unit_price * 100). The data contract is initially incomplete. Analyst owns data/metrics.json; designer owns site/index.html; auditor owns docs/data-quality.md. Collaborate through subscribed change reports. Keep this small demo focused and concise.\n");
}

export async function runDemo(coordinator, { interruptDesigner = false, onPhase = () => {} } = {}) {
  const root = coordinator.root;
  for (const spec of participants) await coordinator.register(spec);
  await coordinator.room.claim("analyst", ["data/metrics.json"]);
  await assert.rejects(coordinator.room.claim("designer", ["data/metrics.json"]), /ownership conflicts/);
  await coordinator.room.release("analyst");
  const write = (path, text) => writeFile(join(root, path), text);
  const notes = (id, text) => write(`collaboration/notes/${id}.md`, text);
  const pause = (ms, signal) => delay(ms, undefined, { signal });
  onPhase("Three agents working in parallel");
  const tasks = [
    coordinator.turn("analyst", "Read data/orders.csv. unit_price is in US dollars. Replace data/metrics.json with computed JSON containing exactly revenueCents (integer cents), orderCount, units, byRegionCents, byChannelCents. Compute all monetary fields as units times unit_price times 100. Explain the new contract briefly in your notes. Do not change any other participant's output.", {
      simulateAction: async signal => { await pause(900, signal); const metrics = calculateMetrics(await readFile(join(root, "data/orders.csv"), "utf8")); await write("data/metrics.json", JSON.stringify(metrics, null, 2)); await notes("analyst", "Replaced the provisional total/rows contract. Consumers now use revenueCents, orderCount, units, byRegionCents and byChannelCents; divide cents by 100 for display. Revenue is units times unit price. Update your dashboard to consume this contract."); },
    }),
    coordinator.turn("designer", "Read data/metrics.json and create site/index.html promptly. Build a compact revenue observatory under 100 lines and 6000 characters: dark ink background, amber accents, three KPI cards and regional/channel bars. It may still be provisional; show Pending if needed. Embed its current JSON in <script id=\"room-data\" type=\"application/json\">...</script>. Inline CSS and JavaScript only, no external resources, no extra features. Your data subscription will deliver the analyst's explanation at a later safe turn boundary. Do not inspect other agents' notes.", {
      simulateAction: async signal => { await pause(1400, signal); await write("site/index.html", simpleSite({ total: 0, rows: 0 }, "Awaiting the analyst's data report")); await notes("designer", "Built the first revenue observatory layout against the provisional schema. Listening for the analyst's new contract before completing the KPI cards."); },
    }),
    coordinator.turn("auditor", "Read data/orders.csv and write docs/data-quality.md in at most 200 words. Describe the column meanings, data-row count, order ID uniqueness, and validation limits of this small fixture. unit_price is in US dollars. You are listening only to docs/; data contract messages should not wake you. Do not inspect other agents' notes.", {
      simulateAction: async signal => { await pause(650, signal); await write("docs/data-quality.md", "# Data quality\n\nSix data rows; all six order IDs are unique. Columns: order_id, region, channel, units, unit_price. All fixture fields are populated. This checks a small local fixture, not arbitrary production data.\n"); await notes("auditor", "Inspected six source rows with unique IDs and documented column meaning and fixture limits. No downstream data contract work is needed for this source-quality report."); },
    }),
  ];
  // Attach rejection handlers immediately while the controlled interruption waits.
  const settled = Promise.allSettled(tasks);
  let interruptionError;
  if (interruptDesigner) {
    try {
      for (let i = 0; i < 600 && !coordinator.participants.get("designer").admitted; i++) {
        if (coordinator.signal?.aborted) throw coordinator.signal.reason;
        if (coordinator.participants.get("designer").status === "failed") throw new Error("designer failed before admission");
        await delay(50);
      }
      await coordinator.interrupt("designer");
      onPhase("Designer interrupted; peers continue");
    } catch (error) { interruptionError = error; }
  }
  const outcomes = await settled;
  if (interruptionError) throw interruptionError;
  for (let i = 0; i < outcomes.length; i++) {
    if (outcomes[i].status === "rejected" && !(interruptDesigner && i === 1 && coordinator.participants.get("designer").status === "interrupted")) throw outcomes[i].reason;
  }
  const dataMessage = outcomes[0].value.message;
  assert.deepEqual(dataMessage.recipients, ["designer"]);
  const pending = await coordinator.room.pending("designer");
  assert.ok(pending.some(message => message.id === dataMessage.id));
  assert.equal((await coordinator.room.pending("auditor")).length, 0);
  onPhase(interruptDesigner ? "Fresh designer resumes from files and subscribed report" : "Designer consumes its subscribed report");
  const metricSource = await readFile(join(root, "data/metrics.json"), "utf8");
  await coordinator.turn("designer", `Continue the compact revenue observatory from your saved files and notes. Read only the subscribed change report and data/metrics.json, plus your own files. Complete site/index.html promptly, under 100 lines and 6000 characters, showing revenueCents / 100 as dollars, orderCount, units, region and channel bars. Embed the exact current metrics JSON in <script id="room-data" type="application/json">...</script>. Include this acknowledgment marker in an HTML comment: STOW_APPLIED_MESSAGE:${dataMessage.id}. Inline CSS and JavaScript only, no external resources or extra features. Explain which subscribed report you applied in at most 150 words in your notes.`, {
    messages: pending,
    simulateAction: async signal => { await pause(450, signal); await write("site/index.html", simpleSite(JSON.parse(metricSource), "Shared data, received at a safe turn boundary", dataMessage.id)); await notes("designer", `Applied analyst report ${dataMessage.id}. Dashboard now renders revenueCents / 100 and the new orderCount, units and breakdown fields. No unrelated auditor report was delivered.`); },
  });
  const expected = calculateMetrics(await readFile(join(root, "data/orders.csv"), "utf8"));
  const actual = JSON.parse(await readFile(join(root, "data/metrics.json"), "utf8"));
  assert.deepEqual(actual, expected);
  const html = await readFile(join(root, "site/index.html"), "utf8");
  const embedded = html.match(/<script\b(?=[^>]*\bid\s*=\s*["']room-data["'])[^>]*>([\s\S]*?)<\/script>/i)?.[1];
  assert.ok(embedded, "designer must embed verifiable metrics");
  assert.deepEqual(JSON.parse(embedded), expected);
  assert.ok(html.includes(`STOW_APPLIED_MESSAGE:${dataMessage.id}`));
  assert.ok((await readFile(join(root, "docs/data-quality.md"), "utf8")).length > 50);
  assert.equal(coordinator.participants.get("auditor").turns, 1);
  assert.equal(coordinator.participants.get("auditor").received.length, 0);
  assert.equal(coordinator.room.snapshot().claims.length, 0);
  const active = new Set();
  let peakConcurrent = 0;
  for (const event of coordinator.activity) {
    if (event.event === "admitted") active.add(event.participant);
    if (["idle", "failed", "interrupted"].includes(event.event)) active.delete(event.participant);
    peakConcurrent = Math.max(peakConcurrent, active.size);
  }
  assert.ok(peakConcurrent >= 2, "at least two admitted agent turns must overlap");
  return { expected, dataMessage, applied: pending.map(message => message.id), verification: {
    correctMetrics: true, matchingEmbeddedDashboard: true, subscribedReportApplied: true, irrelevantAgentNotWoken: true, conflictingClaimRefused: true,
    concurrentTurns: true, peakConcurrent,
  } };
}

function simpleSite(metrics, subtitle, messageID = "") {
  const total = metrics.revenueCents === undefined ? "Pending" : "$" + (metrics.revenueCents / 100).toFixed(2);
  return `<!doctype html><html><head><meta charset="utf-8"><title>Revenue Observatory</title><style>body{background:#10161c;color:#f4efe4;font:16px system-ui;margin:0;padding:42px}h1{font-size:42px}small{color:#d5a952}.cards{display:flex;gap:20px}.card{padding:25px;background:#1d2831;border:1px solid #374650;border-radius:12px;min-width:140px}.card strong{display:block;font-size:36px;color:#eabe74}pre{color:#9cbfb5}p{color:#aab6c0}</style></head><body><small>REVENUE OBSERVATORY / SHARED WORKSPACE</small><h1>The numbers, together.</h1><p>${subtitle}</p><div class="cards"><div class="card">Revenue<strong>${total}</strong></div><div class="card">Orders<strong>${metrics.orderCount ?? 0}</strong></div><div class="card">Units<strong>${metrics.units ?? 0}</strong></div></div><pre>${JSON.stringify(metrics.byRegionCents ?? {}, null, 2)}</pre><script id="room-data" type="application/json">${JSON.stringify(metrics)}</script><!-- STOW_APPLIED_MESSAGE:${messageID} --></body></html>`;
}
