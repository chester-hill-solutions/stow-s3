import {fork}from"node:child_process";
import{remote}from"./http.mjs";
import{benchmarkClient}from"./client.mjs";
import{sampleRemote}from"./sample.mjs";
import{prepare,verifyFinal,leftovers}from"../packages/stow-s3/scripts/performance/workload.mjs";
import{Histogram}from"../packages/stow-s3/scripts/performance/metrics.mjs";

export function ownedOrder(repetition){const orders=[["baseline","staging","after"],["baseline","after","staging"],["staging","baseline","after"],["staging","after","baseline"],["after","baseline","staging"],["after","staging","baseline"]];if(!Number.isSafeInteger(repetition)||repetition<1||repetition>6)throw Error("six balanced rounds required");return orders[repetition-1];}
export function mergeHistograms(rows){const histogram=new Histogram();for(const row of rows){histogram.count+=row.count;histogram.sum+=row.sum;histogram.max=Math.max(histogram.max,row.max);for(const[bin,count]of row.bins)histogram.bins.set(bin,(histogram.bins.get(bin)??0)+count);}return histogram.summary();}
function waitFor(child,expected){return new Promise((resolve,reject)=>{const timer=setTimeout(()=>done(Error("worker timeout: "+expected)),90000);function done(error,value){clearTimeout(timer);child.off("message",message);child.off("exit",exit);error?reject(error):resolve(value);}function message(value){if(value.type==="error")done(Error(value.error));else if(value.type===expected)done(null,value);}function exit(code){done(Error("worker exited "+code));}child.on("message",message);child.once("exit",exit);});}
async function phase(children,type,durationMs){const pending=children.map(child=>waitFor(child,type)),startAt=Date.now()+1200;for(const child of children)child.send({type,durationMs,startAt,profile:false});const results=await Promise.all(pending);const skew=Math.max(...results.map(r=>r.startedAt))-Math.min(...results.map(r=>r.startedAt));if(skew>50||results.some(r=>Math.abs(r.startedAt-startAt)>50||r.load.failed||r.load.dropped||r.verification?.complete===false))throw Error("worker barrier, workload or verification failure");return{startAt,startSkewMs:skew,children:results};}
export async function measureOwned(base,host,variant,workload,config,durationMs=config.durationMs){
  const ready=await remote(base,"/start",{target:`stow-memory-${variant}`,bytes:config.bytes,maxBytes:config.maxBytes,maxObjects:config.maxObjects,serverConcurrency:128});
  const connection=benchmarkClient(`http://${host}:${ready.port}`,ready,4,config.timeoutMs),children=[];
  try{
    const fixtures=await prepare(connection.client,{bytes:config.bytes,concurrency:4},config);
    for(let index=0;index<4;index++){const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!/^(BENCH_|STOW_|AWS_|S3_)/.test(key)));const child=fork("benchmark/diagnosis-worker.mjs",[],{env,stdio:["ignore","inherit","inherit","ipc"]});children.push(child);const readyMessage=waitFor(child,"ready");child.send({type:"init",endpoint:`http://${host}:${ready.port}`,ready,config,scenario:{bytes:config.bytes,concurrency:1,workload},workerOffset:index});await readyMessage;}
    const warmup=await phase(children,"warmup",config.warmupMs),sampler=await sampleRemote(base,config.sampleMs);
    let measured,resources;try{measured=await phase(children,"measure",durationMs);}finally{resources=await sampler.stop();}
    const verification=await verifyFinal(connection.client,fixtures,config.timeoutMs),remaining=await leftovers(connection.client,config.timeoutMs);
    const writers=measured.children.reduce((sum,r)=>sum+r.verification.verifiedObjects,0);
    if(!resources.diskHeadroomMaintained||!verification.complete||verification.verifiedObjects!==16||writers!==(workload==="put"?4:0)||remaining.count||remaining.truncated)throw Error("final fixture or object verification failure");
    for(const row of[...warmup.children,...measured.children])delete row.profile;
    const completed=measured.children.reduce((sum,r)=>sum+r.load.windowSuccessful,0);
    return{variant,workload,clients:4,processes:4,durationMs,startSkewMs:measured.startSkewMs,completed,filesPerSecond:completed/(durationMs/1000),payloadMiBPerSecond:completed*config.bytes/1048576/(durationMs/1000),
      clientCpuPercent:measured.children.reduce((sum,r)=>sum+r.cpuSeconds,0)/(durationMs/1000)*100,latencyMs:mergeHistograms(measured.children.map(r=>r.latencyHistogram)),
      startupMs:ready.startupMs,firstWriteMs:ready.firstWriteMs,firstWriteVerified:ready.firstWriteVerified,warmup,measured,resources,verification,leftovers:remaining};
  }finally{for(const child of children)if(child.exitCode===null)child.kill("SIGTERM");connection.close();await remote(base,"/stop",{});}
}
