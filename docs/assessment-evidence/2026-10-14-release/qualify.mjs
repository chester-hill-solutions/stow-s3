import fs from 'node:fs';
import {execFileSync,spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
const commit='55fc5282f6b40cac6631e88761ed2fa7c6929fad';
const root='/results/release-0.3.0-55fc528';
if(process.env.RAILWAY_SERVICE_ID!=='83497842-c51d-420e-8ba1-181589730ad4')throw Error('Railway release runner only');
fs.mkdirSync(root);
const status={commit,version:'0.3.0',complete:false,published:false,startedAt:new Date().toISOString(),checks:[]};
const log=fs.openSync(root+'/qualification.log','a');
function save(phase){status.phase=phase;status.updatedAt=new Date().toISOString();fs.writeFileSync(root+'/status.json',JSON.stringify(status,null,2)+'\n');}
function exec(command,args,options={}){return execFileSync(command,args,{stdio:['ignore',log,log],timeout:900000,...options});}
async function check(name,command){
  save(name);
  await new Promise((resolve,reject)=>{
    const child=spawn('python3',['/app/benchmark/subreaper.py','su','nobody','-s','/bin/bash','-c','cd /src && '+command],{env:{...process.env,PATH:'/go/bin:'+process.env.PATH,PYTHON_VENV:'/tmp/stow-release-python',GOFLAGS:'-p=4'},stdio:['ignore',log,log]});
    const timeout=setTimeout(()=>child.kill('SIGTERM'),1800000);
    child.once('error',error=>{clearTimeout(timeout);reject(error);});
    child.once('close',code=>{clearTimeout(timeout);code===0?resolve():reject(Error(name+' exited '+code));});
  });
  status.checks.push(name);save(name+' passed');
}
try{
  save('source');
  exec('git',['clone','--depth','3','--branch','codex/release-0.3.0','https://github.com/chester-hill-solutions/stow-s3.git','/src']);
  const actual=execFileSync('git',['rev-parse','HEAD'],{cwd:'/src',encoding:'utf8'}).trim();if(actual!==commit)throw Error('Release branch moved');
  if(fs.existsSync('/src/site/benchmark-data.json')||fs.readFileSync('/src/site/index.html','utf8').includes('<!-- benchmark-section -->'))throw Error('Private marketing charts entered release source');
  fs.mkdirSync('/out/release',{recursive:true});
  for(const dir of ['/tmp/benchmark-go-cache','/tmp/benchmark-npm','/tmp/benchmark-playwright'])fs.mkdirSync(dir,{recursive:true});
  exec('go',['mod','download'],{cwd:'/src'});
  exec('go',['install','google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12']);
  exec('chown',['-R','nobody:nogroup','/src','/out','/tmp/benchmark-go-cache','/tmp/benchmark-npm','/tmp/benchmark-playwright','/go/pkg/mod']);
  await check('release policy and workflow regressions','node --test scripts/release-plan.test.mjs scripts/release-workflow.test.mjs && RELEASE_VERSION=v0.3.0 node scripts/release-plan.mjs');
  await check('pinned protobuf regeneration','PATH=/go/bin:$PATH protoc --go_out=/tmp --go_opt=paths=source_relative internal/storage/fs/recordpb/record.proto && cmp /tmp/internal/storage/fs/recordpb/record.pb.go internal/storage/fs/recordpb/record.pb.go');
  await check('make test-all','make test-all');
  await check('make standards','make standards');
  await check('make check-generated','make check-generated');
  await check('real browser tests','node packages/stow-s3/node_modules/playwright/cli.js install chromium && make test-browser');
  await check('tracked generated-source drift','git diff --exit-code -- packages/stow-s3/dist docs/api internal/mcpstorage/browser.html.gz scripts/baselines');
  const artifacts=[];
  for(const [os,arch] of [['linux','amd64'],['linux','arm64'],['darwin','amd64'],['darwin','arm64']]){
    await check('native archive '+os+'/'+arch,`mkdir -p /out/release/${os}-${arch} && GOOS=${os} GOARCH=${arch} CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/release/${os}-${arch}/stow-s3 ./cmd/stow-s3 && cp LICENSE README.md /out/release/${os}-${arch}/ && tar -czf /out/release/stow-${os}-${arch}.tar.gz -C /out/release/${os}-${arch} stow-s3 LICENSE README.md`);
    const path=`/out/release/stow-${os}-${arch}.tar.gz`;artifacts.push({name:path.split('/').at(-1),bytes:fs.statSync(path).size,sha256:createHash('sha256').update(fs.readFileSync(path)).digest('hex')});
  }
  await check('native Linux archive version','/out/release/linux-amd64/stow-s3 --version | grep -Fx "stow-s3 0.3.0"');
  const browser=JSON.parse(fs.readFileSync('/src/.cache/browser-test-results/report.json','utf8'));
  fs.copyFileSync('/src/.cache/browser-test-results/report.json',root+'/browser-report.json');
  status.complete=true;status.artifacts=artifacts;status.browser=browser.stats;status.finishedAt=new Date().toISOString();
  status.resourceLimits={cpuMax:fs.readFileSync('/sys/fs/cgroup/cpu.max','utf8').trim(),memoryMax:fs.readFileSync('/sys/fs/cgroup/memory.max','utf8').trim()};
  status.nativeExecutionScope='Railway executes Linux amd64; GitHub release rehearsal checks consumers on all four native target platforms.';
  save('complete');
}catch(error){status.error=error.message;status.finishedAt=new Date().toISOString();save('failed');process.exitCode=1;}
