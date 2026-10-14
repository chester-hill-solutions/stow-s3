import{mkdir,writeFile,readFile}from"node:fs/promises";
import{execFile}from"node:child_process";
import{promisify}from"node:util";
import{resolve,join,basename}from"node:path";
import{systemMetadata}from"./resources.mjs";
import{remote}from"./http.mjs";
import{ownedOrder,measureOwned}from"./owned-load.mjs";
const execute=promisify(execFile),out=resolve(process.argv[2]),host=process.env.BENCH_TARGET_HOST;
if(process.platform!=="linux"||!out.startsWith("/results/")||!host||!/^[a-z0-9.-]+$/i.test(host))throw Error("Railway results and target required");
const base=`http://${host}:8081`,config={bytes:1048576,concurrency:4,retainedObjects:16,maxQueue:8,timeoutMs:30000,maxBytes:268435456,maxObjects:4096,sampleMs:250,warmupMs:5000,durationMs:20000,repetitions:6,scannerDelayMs:2};
await mkdir(out,{recursive:true});const receipt={runId:basename(out),startedAt:new Date().toISOString(),complete:false,config,metadata:await systemMetadata(),targetMetadata:await remote(base,"/metadata"),smoke:[],results:[],native:[]};
const save=()=>writeFile(join(out,"run.json"),JSON.stringify(receipt,null,2)+"\n");await save();
try{
  for(const variant of["baseline","staging","after"])for(const workflow of["put","quarantine"]){receipt.smoke.push(await measureOwned(base,host,variant,workflow,config,3000));await save();}
  for(let repetition=1;repetition<=6;repetition++)for(const variant of ownedOrder(repetition)){
    const args=["-test.run=^$","-test.bench=^BenchmarkAuthenticatedHTTPPut(Parallel)?$","-test.benchmem","-test.benchtime=500ms","-test.count=1","-test.cpu=4"];
    const result=await execute(`bin/http-${variant}.test`,args,{timeout:60000,maxBuffer:1024*1024});const name=`native-${variant}-${repetition}.txt`;await writeFile(join(out,name),result.stdout+result.stderr);receipt.native.push({variant,repetition,file:name,args});await save();
  }
  for(let repetition=1;repetition<=6;repetition++)for(const workflow of repetition%2?["put","quarantine"]:["quarantine","put"])for(const variant of ownedOrder(repetition)){
    const result=await measureOwned(base,host,variant,workflow,config);receipt.results.push({repetition,...result});await save();console.log(JSON.stringify({repetition,workflow,variant,filesPerSecond:result.filesPerSecond,p99Ms:result.latencyMs.p99,clientCpuPercent:result.clientCpuPercent}));
  }
  receipt.complete=true;receipt.completedAt=new Date().toISOString();
}catch(error){receipt.error=error.message;process.exitCode=1;}
await save();
