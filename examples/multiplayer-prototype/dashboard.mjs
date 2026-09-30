import http from "node:http";
import { readFile, realpath, stat } from "node:fs/promises";
import { resolve, sep } from "node:path";

const artifacts = new Map([
  ["/artifact/index.html", ["site/index.html", "text/html; charset=utf-8"]],
  ["/artifact/metrics.json", ["data/metrics.json", "application/json; charset=utf-8"]],
]);

/** A read-only local view; the coordinator remains responsible for state and lifecycle. */
export async function startDashboard({ getState, root }) {
  const workspaceRoot = resolve(root);
  async function artifactPath(relative) {
    const [base, file] = await Promise.all([realpath(workspaceRoot), realpath(resolve(workspaceRoot, relative))]);
    if (!file.startsWith(base + sep) || !(await stat(file)).isFile()) throw new Error("Artifact unavailable");
    return file;
  }
  const server = http.createServer(async (request, response) => {
    response.setHeader("Cache-Control", "no-store");
    response.setHeader("X-Content-Type-Options", "nosniff");
    response.setHeader("Referrer-Policy", "no-referrer");
    if (request.method !== "GET") {
      response.writeHead(405, { Allow: "GET" });
      response.end("Read-only dashboard");
      return;
    }
    try {
      const path = new URL(request.url, "http://localhost").pathname;
      if (path === "/") {
        response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        response.end(html);
      } else if (path === "/api/state") {
        const state = await getState();
        const artifactReady = await artifactPath("site/index.html").then(() => true, () => false);
        response.writeHead(200, { "Content-Type": "application/json; charset=utf-8" });
        response.end(JSON.stringify({ ...state, artifactReady }));
      } else if (artifacts.has(path)) {
        const [relative, type] = artifacts.get(path);
        const content = await readFile(await artifactPath(relative));
        response.writeHead(200, { "Content-Type": type });
        response.end(content);
      } else {
        response.writeHead(404);
        response.end("Not found");
      }
    } catch {
      response.writeHead(503, { "Content-Type": "text/plain; charset=utf-8" });
      response.end("State or artifact temporarily unavailable");
    }
  });
  await new Promise((accept, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", accept);
  });
  return {
    url: `http://127.0.0.1:${server.address().port}`,
    close: () => new Promise((accept, reject) => server.close(error => error ? reject(error) : accept())),
  };
}

const html = String.raw`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Stow / Multiplayer lab</title>
<style>
:root{color-scheme:dark;--bg:#0c1017;--panel:#141c28;--line:#29364a;--ink:#ecf2ff;--muted:#a5b3c9;--green:#8ae8c0;--purple:#c1b2ff;--orange:#ffc595}
*{box-sizing:border-box}body{margin:0;background:radial-gradient(ellipse at 90% 0%,#25203c 0%,transparent 40%),var(--bg);color:var(--ink);font:14px/1.5 ui-sans-serif,system-ui,sans-serif}
main{max-width:1500px;margin:auto;padding:34px}header{display:flex;gap:20px;justify-content:space-between;align-items:flex-start;margin-bottom:25px}
.eyebrow{font-size:11px;letter-spacing:.16em;text-transform:uppercase;color:var(--purple);font-weight:700}h1{font-size:32px;letter-spacing:-.04em;margin:7px 0}h2{font-size:16px;margin:0}p{margin:8px 0}.muted{color:var(--muted)}
.connection,.badge{display:inline-flex;align-items:center;gap:7px;border:1px solid var(--line);border-radius:100px;padding:6px 11px;font-size:12px}.connection{color:var(--green)}.dot{width:7px;height:7px;background:currentColor;border-radius:50%}
.workspace{padding:17px 20px;border:1px solid var(--line);border-radius:12px;display:grid;grid-template-columns:1fr auto;gap:8px 20px;background:#111925}.path{font-family:ui-monospace,SFMono-Regular,monospace;font-size:12px;overflow-wrap:anywhere;color:var(--muted)}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin:18px 0}.stat{padding:15px 18px;background:var(--panel);border:1px solid var(--line);border-radius:10px}.stat strong{display:block;font-size:25px;line-height:1.3}.stat span{font-size:12px;color:var(--muted)}
.layout{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(0,.85fr);gap:20px}.panel{background:var(--panel);border:1px solid var(--line);border-radius:12px;overflow:hidden;margin-bottom:20px}.panelhead{display:flex;align-items:center;justify-content:space-between;padding:18px 20px;border-bottom:1px solid var(--line)}.panelbody{padding:18px 20px}
.agents{display:grid;grid-template-columns:repeat(auto-fit,minmax(235px,1fr));gap:12px}.agent{padding:16px;border:1px solid var(--line);border-radius:9px;background:#101824}.agenthead{display:flex;justify-content:space-between;gap:8px;align-items:center}.agentname{font-size:15px;font-weight:650}.agent .badge{padding:3px 8px;color:var(--green)}.agent p{font-size:12px}.label{font-size:11px;text-transform:uppercase;letter-spacing:.09em;color:var(--muted);margin-top:14px}.chips{display:flex;gap:5px;flex-wrap:wrap;margin-top:5px}.chip{font:11px/1.5 ui-monospace,monospace;background:#242d40;border-radius:5px;padding:4px 7px;overflow-wrap:anywhere;max-width:100%}
.feed{max-height:510px;overflow:auto}.event{padding:17px 20px;border-bottom:1px solid var(--line)}.event:last-child{border:0}.eventtop{display:flex;justify-content:space-between;gap:12px;font-size:12px;color:var(--purple)}.eventsummary{margin:7px 0;color:var(--ink)}.recipients{font-size:12px;color:var(--green);margin-top:9px}.empty{color:var(--muted);font-size:13px;padding:8px 0}.claim{display:flex;justify-content:space-between;gap:15px;padding:9px 0;border-bottom:1px solid var(--line)}.claim:last-child{border:0}.claim .path{color:var(--ink)}.claimowner{color:var(--purple);font-size:12px;white-space:nowrap}
dl{margin:0}dt{font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.09em;margin-top:14px}dt:first-child{margin-top:0}dd{margin:5px 0;overflow-wrap:anywhere}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.6 ui-monospace,monospace;color:var(--green);margin:0}a{color:var(--purple);text-decoration:none}a:hover{text-decoration:underline}.artifact iframe{width:100%;height:380px;border:0;background:white}.artifactwait{padding:50px 25px;text-align:center;color:var(--muted)}.error{border:1px solid #f39999;background:#331c29;border-radius:9px;padding:14px;margin-bottom:18px;color:#ffd5d5}.error:empty{display:none}footer{font-size:12px;color:var(--muted);margin-top:10px}button{font:inherit}
@media(max-width:850px){main{padding:20px}.layout{grid-template-columns:1fr}.stats{grid-template-columns:repeat(2,1fr)}header{flex-direction:column}.workspace{grid-template-columns:1fr}h1{font-size:27px}}
</style></head><body><main>
<header><div><div class="eyebrow">Stow / Multiplayer lab</div><h1>One workspace. A team in motion.</h1><p class="muted" id="title">Agents collaborate through selective, durable change reports.</p></div><div class="connection" id="connection"><span class="dot"></span><span id="connectiontext">Connecting</span></div></header>
<div id="error" class="error" role="alert"></div>
<section class="workspace"><div><div class="eyebrow">Shared workspace</div><div id="root" class="path"></div></div><span class="badge" id="phase">Starting</span><div class="path" id="workspaceid"></div><div class="path" id="model"></div></section>
<div class="stats"><div class="stat"><strong id="agentcount">0</strong><span>Participants</span></div><div class="stat"><strong id="messagecount">0</strong><span>Change reports</span></div><div class="stat"><strong id="deliverycount">0</strong><span>Selective routes</span></div><div class="stat"><strong id="claimcount">0</strong><span>Active file claims</span></div></div>
<div class="layout"><div>
<section class="panel"><div class="panelhead"><h2>The team</h2><span class="muted">Presence & interests</span></div><div id="agents" class="panelbody agents"></div></section>
<section class="panel"><div class="panelhead"><h2>Change reports</h2><span class="muted">Relevant messages only</span></div><div id="feed" class="feed"></div></section>
<section class="panel"><div class="panelhead"><h2>File ownership</h2><span class="muted">Cooperative claims</span></div><div id="claims" class="panelbody"></div></section>
</div><div>
<section class="panel artifact"><div class="panelhead"><h2>What the team built</h2><a id="artifactlink" href="/artifact/index.html" target="_blank" rel="noopener noreferrer" hidden>Open artifact ↗</a></div><div id="artifactwait" class="artifactwait">The shared artifact will appear here as the team builds.</div><iframe id="preview" title="Team artifact preview" sandbox="allow-scripts" hidden></iframe></section>
<section class="panel"><div class="panelhead"><h2>Checkpoint & evidence</h2><span class="muted">Recoverable work</span></div><div class="panelbody"><dl><dt>Checkpoint</dt><dd id="checkpoint" class="path">Not captured yet</dd><dt>Verification</dt><dd><pre id="verification">Pending</pre></dd><dt>Result</dt><dd><pre id="result">In progress</pre></dd><dt>Coordination metrics</dt><dd><pre id="metrics">Waiting for activity</pre></dd></dl></div></section>
</div></div><footer>Local prototype · Ownership is cooperative; participants must honor claims. This view polls ordinary code, without model calls.</footer>
</main><script>
const $ = id => document.getElementById(id);
const el = (tag, cls, text) => { const node = document.createElement(tag); if(cls) node.className=cls; if(text!==undefined) node.textContent=String(text); return node; };
const display = value => typeof value === 'string' ? value : JSON.stringify(value, null, 2);
const list = value => Array.isArray(value) ? value : value ? [value] : [];
function chips(parent, items, fallback) { const holder=el('div','chips'); for(const item of list(items)) holder.append(el('span','chip',typeof item==='string'?item:display(item))); if(!holder.children.length) holder.append(el('span','muted',fallback)); parent.append(holder); }
let artifactLoaded=false, lastArtifactRevision='', rendering='';
function render(s) {
  const participants=list(s.participants), messages=list(s.messages), claims=list(s.claims), deliveries=list(s.deliveries);
  const name = id => participants.find(p=>p.id===id)?.name || id || 'Unknown';
  $('title').textContent=s.title || 'Agents collaborate through selective, durable change reports.';
  $('root').textContent=s.root || ''; $('workspaceid').textContent='Workspace '+(s.workspaceID || 'initializing');
  $('model').textContent=s.model ? 'Model '+s.model : ''; $('phase').textContent=s.phase || 'Starting'; $('error').textContent=s.error ? display(s.error) : '';
  const routeCount=messages.reduce((count,m)=>count+list(m.recipients).length,0);
  const claimCount=claims.reduce((count,c)=>count+(list(c.paths || c.files).length || 1),0);
  for(const [id,value] of [['agentcount',participants.length],['messagecount',messages.length],['deliverycount',routeCount],['claimcount',claimCount]]) $(id).textContent=value;
  $('agents').replaceChildren();
  for(const p of participants) {
    const card=el('article','agent'), head=el('div','agenthead'); head.append(el('span','agentname',p.name || p.id),el('span','badge',p.status || 'Joined')); card.append(head);
    if(p.role || p.task) card.append(el('p','muted',p.role || p.task));
    card.append(el('div','label','Assigned files')); chips(card,p.ownedFiles,'No files assigned'); card.append(el('div','label','Listening to')); chips(card,p.subscriptions,'No subscriptions');
    const inbox=deliveries.find(d=>d.id===p.id);
    const received=Array.isArray(p.received)?p.received.length:p.received ?? list(inbox?.acknowledged).length; card.append(el('p','muted',(p.turns || 0)+' turns · '+received+' reports received')); $('agents').append(card);
  }
  if(!participants.length) $('agents').append(el('div','empty','Participants will appear when they join.'));
  $('feed').replaceChildren();
  for(const m of [...messages].reverse()) {
    const event=el('article','event'), top=el('div','eventtop'); top.append(el('span','',name(m.from)),el('span','','#'+(m.sequence ?? m.id))); event.append(top,el('p','eventsummary',m.summary || m.type || 'Change report')); chips(event,m.files,'Workspace event');
    const matching=deliveries.filter(d=>(d.messageID || d.messageId || d.message?.id)===m.id);
    const recipients=list(m.recipients).length?list(m.recipients):matching.map(d=>d.to || d.recipient || d.participantID);
    const recipientLabel=r=>{ const id=typeof r==='string'?r:r.id; const inbox=deliveries.find(d=>d.id===id); const receipt=matching.find(d=>(d.to || d.recipient || d.participantID)===id); const status=inbox ? (list(inbox.acknowledged).includes(m.id)?'acknowledged':'queued') : receipt?.status || 'queued'; return name(id)+' · '+status; };
    event.append(el('div','recipients',recipients.length?'Routed to '+recipients.map(recipientLabel).join(', '):'No matching subscribers'));
    $('feed').append(event);
  }
  if(!messages.length) $('feed').append(el('div','panelbody empty','Waiting for the first published change report.'));
  $('claims').replaceChildren();
  for(const c of claims) { const row=el('div','claim'); row.append(el('span','path',c.path || c.file || list(c.paths || c.files).join(', ')),el('span','claimowner',name(c.owner || c.participantID || c.agentID || c.id))); $('claims').append(row); }
  if(!claims.length) $('claims').append(el('div','empty','No active claims.'));
  $('checkpoint').textContent=s.checkpointID || 'Not captured yet';
  $('verification').textContent=s.verification ? display(s.verification) : 'Pending'; $('result').textContent=s.result ? display(s.result) : 'In progress'; $('metrics').textContent=s.metrics ? display(s.metrics) : 'Waiting for activity';
  if(s.artifactReady) {
    $('artifactlink').hidden=false; $('artifactwait').hidden=true; $('preview').hidden=false;
    const revision=s.checkpointID || s.phase || '';
    if(!artifactLoaded || revision!==lastArtifactRevision) { $('preview').src='/artifact/index.html?v='+encodeURIComponent(revision); artifactLoaded=true; lastArtifactRevision=revision; }
  }
}
async function poll() {
  try { const response=await fetch('/api/state',{cache:'no-store'}); if(!response.ok) throw new Error('Unavailable'); const state=await response.json(); const serialized=JSON.stringify(state); if(serialized!==rendering) { render(state); rendering=serialized; } $('connectiontext').textContent='Live · '+new Date().toLocaleTimeString(); $('connection').style.color='var(--green)'; }
  catch { $('connectiontext').textContent='Reconnecting'; $('connection').style.color='var(--orange)'; }
  setTimeout(poll,1000);
}
poll();
</script></body></html>`;
