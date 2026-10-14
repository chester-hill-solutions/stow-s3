import { spawn } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";

export const nodeAgentImplementations = ["stow-wasm-memory", "node-buffer-memory", "node-sqlite-memory"];

export async function runNodeAgents(profile, out) {
  if (!Number.isSafeInteger(profile.samples) || profile.samples < 2 || profile.samples > 100) throw new Error("invalid Node agent sample count");
  const evidence = {version:1,complete:false,results:[]};
  const save = ()=>writeFile(join(out,"node-agent.json"),JSON.stringify(evidence,null,2)+"\n");
  await save();
  for (const bytes of [4096,1048576]) {
    for (let sample=0;sample<profile.samples;sample++) {
      const offset=sample%nodeAgentImplementations.length;
      const order=nodeAgentImplementations.slice(offset).concat(nodeAgentImplementations.slice(0,offset));
      for (const implementation of order) {
        evidence.results.push(await measure(implementation,bytes,sample));
        await save();
      }
    }
  }
  evidence.complete=true;
  await save();
  return {complete:true,samples:evidence.results.length};
}

function measure(implementation, bytes, sample) {
  return new Promise((resolve,reject)=>{
    const began=performance.now();
    const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!/^(STOW_|AWS_|S3_|MINIO_|RUSTFS_|BENCH_CONTROL_TOKEN|RAILWAY_)/.test(key)));
    const child=spawn(process.execPath,["benchmark/node-agent-child.mjs",implementation,String(bytes)],{env,stdio:["ignore","ignore","pipe","ipc"]});
    const deadline=setTimeout(()=>{child.kill("SIGKILL");reject(new Error("Node agent exceeded 30 s deadline"));},30000);
    let startupMs,firstWriteMs,result,stderr="";
    child.stderr.on("data",chunk=>{stderr=(stderr+chunk.toString()).slice(-8192);});
    child.on("message",message=>{
      if(message.phase==="ready") startupMs=performance.now()-began;
      if(message.phase==="written") firstWriteMs=performance.now()-began;
      if(message.phase==="complete") result=message;
    });
    child.once("error",error=>{clearTimeout(deadline);reject(error);});
    child.once("close",code=>{
      clearTimeout(deadline);
      if(code!==0||!result?.verified||!(startupMs>0)||!(firstWriteMs>=startupMs)) return reject(new Error(`Node agent failed ${implementation}: ${code} ${stderr}`));
      resolve({implementation,bytes,sample,startupMs,firstWriteMs,verified:true,peakRssBytes:result.peakRssBytes,...(result.sqliteVersion?{sqliteVersion:result.sqliteVersion}:{})});
    });
  });
}
