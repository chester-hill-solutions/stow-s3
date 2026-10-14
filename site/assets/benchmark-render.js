const escape=value=>String(value).replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll(">","&gt;").replaceAll('"',"&quot;");
const format=value=>new Intl.NumberFormat("en-US",{maximumSignificantDigits:4}).format(value);
const axis=value=>{
  if(!(value>0))return 1;
  const power=10**Math.floor(Math.log10(value));
  return [1,2,2.5,5,10].map(n=>n*power).find(n=>n>=value*1.05)??power*20;
};
const backend=implementation=>implementation.endsWith("memory")?"memory":"filesystem";

export function renderBenchmarkPanel(data, selection) {
  const {view,storage,bytes}=selection;
  const match=row=>backend(row.implementation)===storage&&row.bytes===bytes;
  let charts=[],note;
  if(view==="agents") {
    const rows=data.serverAgent.filter(match);
    charts=[chart(data,rows,{title:"Ready to work",unit:"ms",lower:true,value:r=>r.startup.medianMs,min:r=>r.startup.minMs,max:r=>r.startup.maxMs}),
      chart(data,rows,{title:"Time to first write",unit:"ms",lower:true,value:r=>r.firstWrite.medianMs,min:r=>r.firstWrite.minMs,max:r=>r.firstWrite.maxMs})];
    note="Fresh S3 server processes with empty data directories. Startup ends on authenticated readiness; first write includes bucket creation and a 4 KiB PUT over target loopback. Nine launches per product; 10 ms readiness polls. Images and binaries are already present. This measures storage server startup, not an agent’s full startup.";
  } else if(view==="quarantine") {
    const rows=data.s3.filter(row=>match(row)&&row.workload==="quarantine");
    const factor=1048576/bytes;
    charts=[chart(data,rows,{title:"Completed quarantine workflows",unit:"files/s",value:r=>r.filesPerSecond,
      min:r=>r.minPayloadMiBPerSecond*factor,max:r=>r.maxPayloadMiBPerSecond*factor}),
      chart(data,rows,{title:"Tail workflow latency",unit:"ms",lower:true,value:r=>r.medianRunP99ResponseMs,summary:"Median of each run’s p99"})];
    note=loadNote(data)+` Quarantine: PUT → verified GET → simulated verdict PUT → two deletes, with a ${data.settings.scannerDelayMs} ms simulated scanner delay. Flagged-file retention and real scanning are unmeasured.`;
  } else {
    const memory=view==="memory";
    charts=["put","get"].map(workload=>{
      const rows=data.s3.filter(row=>match(row)&&row.workload===workload);
      return chart(data,rows,memory?{title:(workload==="put"?"Write":"Read")+" memory",unit:"MiB",lower:true,
        value:r=>r.peakSampledServerRssMiB,summary:"Peak sampled server RSS during load"}:
        {title:workload==="put"?"Upload throughput":"Download throughput",unit:"MiB/s",value:r=>r.payloadMiBPerSecond,
          min:r=>r.minPayloadMiBPerSecond,max:r=>r.maxPayloadMiBPerSecond});
    });
    note=loadNote(data)+(memory?" RSS excludes the generator, OS page cache and startup window. Sampling can miss short peaks.":"");
  }
  return `<div class="benchmark-charts">${charts.join("")}</div><p class="benchmark-context">${escape(note)}</p>`;
}

function loadNote(data) {
  return `${data.settings.repetitions} × ${data.settings.durationSeconds} s runs after ${data.settings.warmupSeconds} s warm-up · ${data.settings.concurrency[0]} workers · ${data.settings.retainedObjects} retained fixtures · 2 vCPU / 2 GiB target · private HTTP without TLS. Filesystem cases use the same persistent volume; memory cases retain objects in RAM. PUT overwrites one key per worker; GET cycles the fixtures.`;
}

function chart(data, rows, spec) {
  const values=rows.map(row=>({row,value:spec.value(row),min:spec.min?.(row),max:spec.max?.(row)}));
  const ceiling=axis(Math.max(...values.map(r=>r.max??r.value)));
  values.sort((a,b)=>spec.lower?a.value-b.value:b.value-a.value);
  const bars=values.map(({row,value,min,max})=>{
    const stow=row.implementation.startsWith("stow-");
    const range=min!==undefined&&max!==undefined?`<small>${format(min)}–${format(max)}</small>`:"";
    const marker=min!==undefined&&max!==undefined&&max>min?`<i class="benchmark-spread" style="left:${min/ceiling*100}%;width:${(max-min)/ceiling*100}%"></i>`:"";
    return `<li class="benchmark-row${stow?" is-stow":""}"><span class="benchmark-name">${escape(data.names[row.implementation]??row.implementation)}</span><span class="benchmark-track" aria-hidden="true"><i class="benchmark-bar" style="width:${value/ceiling*100}%"></i>${marker}</span><span class="benchmark-value">${format(value)}${range}</span></li>`;
  }).join("");
  return `<figure class="benchmark-chart"><figcaption><div><h3>${escape(spec.title)}</h3><p>${escape(spec.summary??"Median · whiskers show observed range")}</p></div><span class="benchmark-unit">${escape(spec.unit)}<small>${spec.lower?"↓ Lower is better":"↑ Higher is better"}</small></span></figcaption><ol>${bars}</ol><div class="benchmark-axis" aria-hidden="true"><span>0</span><span>${format(ceiling/2)}</span><span>${format(ceiling)}</span></div></figure>`;
}

export function renderBenchmarkSection(data) {
  if(data.complete!==true)throw new Error("Benchmark charts require complete evidence");
  const initial={view:"agents",storage:"filesystem",bytes:4096};
  return `<section class="performance container" id="performance" aria-labelledby="performance-title">
    <div class="performance-heading"><div><p class="eyebrow">Measured on Railway</p><h2 id="performance-title">Agent storage, measured.</h2><p>Stow, MinIO, S3Proxy and RustFS: compare S3 server startup, first write, transfer, quarantine and memory.</p></div><span class="benchmark-stamp">Development snapshot<span>${escape(data.measuredAt.slice(0,10))}</span></span></div>
    <div class="benchmark-board" data-benchmark-board>
      <div class="benchmark-toolbar"><div class="benchmark-views" role="group" aria-label="Benchmark workload"><button type="button" data-view="agents" aria-pressed="true">Storage startup</button><button type="button" data-view="transfer" aria-pressed="false">Ingress &amp; egress</button><button type="button" data-view="quarantine" aria-pressed="false">Quarantine</button><button type="button" data-view="memory" aria-pressed="false">Memory</button></div>
      <div class="benchmark-selects"><label>Storage<select data-storage><option value="filesystem">Filesystem</option><option value="memory">Memory</option></select></label><label>Payload<select data-bytes><option value="4096">4 KiB</option></select></label></div></div>
      <div data-benchmark-panel>${renderBenchmarkPanel(data,initial)}</div>
      <div class="benchmark-proof"><span><i aria-hidden="true">✓</i> SHA-256 + length verified</span><span>Zero failed or dropped workflows</span><a href="/assets/benchmark-data.json" download="stow-benchmark-results.json">Download results <span aria-hidden="true">↗</span></a></div>
    </div>
    <details class="benchmark-method"><summary>Test settings and what these comparisons cover</summary><div><p>${escape(data.hardware.cpu)}. One target at a time; separate generator. Each S3 server uses the same resource ceilings and client workflows. Filesystem and memory backends are compared separately. Bars use a linear scale from zero; ranges describe observed samples.</p><p>These peers expose S3-shaped blob storage. The workloads exercise bucket creation, PUT, GET and DELETE; they do not establish full S3 feature parity. Durability guarantees differ: Stow filesystem publishes with rename and fsync; S3Proxy’s filesystem adapter differs; RustFS uses one drive with strict durability.</p><p>These are exploratory development measurements. Image downloads, deployments, browser startup, cold OS caches, real scanners, flagged-file promotion, replication and failover are unmeasured. Some target runs reach the CPU ceiling; the fixed generator and concurrency can also constrain rates.</p></div></details>
    <script id="benchmark-data" type="application/json">${JSON.stringify(data).replaceAll("<","\\u003c")}</script>
  </section>`;
}
