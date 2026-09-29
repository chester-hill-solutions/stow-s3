import os, sys, json, tempfile, subprocess, select, pathlib, urllib.request, urllib.error, hashlib, time, shutil
ROOT=pathlib.Path('/Users/ladmin/.codex/worktrees/stow-product-assessment/stow')
BIN=str(ROOT/'bin/stow-s3'); AWS='/opt/homebrew/bin/aws'
BASE=pathlib.Path(tempfile.mkdtemp(prefix='stow-product-probe-',dir='/private/tmp'))
ENV={k:v for k,v in os.environ.items() if not k.startswith(('AWS_','STOW_','S3_'))}
ENV.update(AWS_EC2_METADATA_DISABLED='true',AWS_PAGER='',AWS_DEFAULT_REGION='us-east-1',XDG_CONFIG_HOME=str(BASE/'config'))
results=[]; servers=[]
def record(name,ok,detail=None):
 row=dict(name=name,passed=bool(ok),detail=detail);results.append(row);print(json.dumps(row),flush=True)
def start(name,extra=(),env_extra=None):
 r,w=os.pipe(); env=ENV.copy();env.update(env_extra or {})
 log=open(BASE/(name+'.log'),'ab')
 p=subprocess.Popen([BIN,'serve','--port','0','--data-dir',str(BASE/name),'--ready-fd',str(w),*extra],env=env,pass_fds=(w,),stdout=subprocess.DEVNULL,stderr=log)
 os.close(w);servers.append((p,log))
 if not select.select([r],[],[],15)[0]:raise RuntimeError('readiness timeout: '+name)
 with os.fdopen(r) as f: ready=json.loads(f.readline())
 return p,ready
def stop(p):
 if p.poll() is None:
  p.terminate()
  try:p.wait(8)
  except subprocess.TimeoutExpired:p.kill();p.wait()
def aws(s,args,ok=True,compat=True):
 if compat and (args[:2]==['s3api','put-object'] or (args[:2]==['s3','cp'] and not args[2].startswith('s3://'))):
  args=[*args,'--checksum-algorithm','CRC32']
 env=ENV.copy();env.update(AWS_ACCESS_KEY_ID=s['accessKeyId'],AWS_SECRET_ACCESS_KEY=s['secretAccessKey'])
 p=subprocess.run([AWS,'--endpoint-url',s['endpoint'],'--no-cli-pager',*args],env=env,capture_output=True,text=True,timeout=35)
 if ok and p.returncode:raise RuntimeError('aws '+str(args[:2])+': '+p.stderr[:1000])
 return p

def cli(args,extra=None,ok=True):
 env=ENV.copy();env.update(extra or {})
 p=subprocess.run([BIN,*args],env=env,capture_output=True,text=True,timeout=25)
 if ok and p.returncode:raise RuntimeError('stow '+str(args[:2])+': '+p.stderr[:1000])
 return p

def gethttp(url):
 try:
  with urllib.request.urlopen(url,timeout=5) as r:return r.status,r.read()
 except urllib.error.HTTPError as e:return e.code,e.read()
try:
 t=time.monotonic();p,s=start('native');record('native ready',True,{'elapsed_ms':round((time.monotonic()-t)*1000,2),'capabilities':s['capabilities']})
 data=BASE/'input.bin';data.write_bytes(b'abcdef0123456789'*4096)
 aws(s,['s3api','create-bucket','--bucket','fixtures'])
 default=aws(s,['s3api','put-object','--bucket','fixtures','--key','default.bin','--body',str(data)],False,False)
 record('default AWS CLI upload',default.returncode==0,default.stderr.strip())
 aws(s,['s3api','put-object','--bucket','fixtures','--key','nested/a.bin','--body',str(data),'--metadata','purpose=assessment','--content-type','application/x-stow-test'])
 out=BASE/'read.bin';aws(s,['s3api','get-object','--bucket','fixtures','--key','nested/a.bin',str(out)])
 record('AWS CLI put/get identical',out.read_bytes()==data.read_bytes())
 head=json.loads(aws(s,['s3api','head-object','--bucket','fixtures','--key','nested/a.bin']).stdout)
 record('metadata preserved',head.get('Metadata')=={'purpose':'assessment'} and head.get('ContentType')=='application/x-stow-test',{'metadata':head.get('Metadata'),'content_type':head.get('ContentType')})
 aws(s,['s3api','get-object','--bucket','fixtures','--key','nested/a.bin','--range','bytes=2-5',str(out)])
 record('range bytes',out.read_bytes()==b'cdef')
 presign=aws(s,['s3','presign','s3://fixtures/nested/a.bin','--expires-in','60']).stdout.strip()
 code,body=gethttp(presign);record('presigned get',code==200 and body==data.read_bytes())
 # Change signed path, leaving signature unchanged. Never print credentials/URL.
 code,_=gethttp(presign.replace('nested/a.bin','nested/tampered.bin'));record('tampered signature rejected',code==403,{'http':code})
 code,_=gethttp(s['endpoint']+'/fixtures/nested/a.bin');record('unsigned request rejected',code==403,{'http':code})
 wrong=aws(s,['s3api','put-object','--bucket','fixtures','--key','nested/a.bin','--body',str(data),'--if-match','"wrong"'],False)
 record('wrong If-Match rejected',wrong.returncode!=0 and 'PreconditionFailed' in wrong.stderr,wrong.stderr.strip())
 big=BASE/'big.bin';big.write_bytes(b'x'*(12*1024*1024))
 upload=aws(s,['s3','cp',str(big),'s3://fixtures/large.bin','--only-show-errors'],False)
 record('AWS CLI high-level 12 MiB upload',upload.returncode==0,upload.stderr.strip())
 if upload.returncode==0:
  aws(s,['s3','cp','s3://fixtures/large.bin',str(out),'--only-show-errors']);record('12 MiB roundtrip',hashlib.sha256(out.read_bytes()).digest()==hashlib.sha256(big.read_bytes()).digest())
 stop(p);p,s=start('native');aws(s,['s3api','get-object','--bucket','fixtures','--key','nested/a.bin',str(out)])
 record('filesystem restart persistence',out.read_bytes()==data.read_bytes())
 # Two real native Stow servers; upstream is synthetic and local.
 aws(s,['s3api','put-object','--bucket','fixtures','--key','cached-only.bin','--body',str(data)])
 aws(s,['s3api','create-bucket','--bucket','not-granted'])
 aws(s,['s3api','put-object','--bucket','not-granted','--key','synthetic.txt','--body',str(data)])
 upenv={'STOW_ENDPOINT':s['endpoint'],'STOW_ACCESS_KEY_ID':s['accessKeyId'],'STOW_SECRET_ACCESS_KEY':s['secretAccessKey'],'STOW_REGION':'us-east-1'}
 warm=cli(['prewarm','--data-dir',str(BASE/'downstream'),'--bucket','fixtures','--keys','nested/a.bin,cached-only.bin'],upenv)
 record('prewarm exact key',json.loads(warm.stdout).get('warmed')==2,json.loads(warm.stdout))
 q,d=start('downstream',['--mode','run-through'],upenv)
 aws(d,['s3api','get-object','--bucket','fixtures','--key','nested/a.bin',str(out)]);record('run-through cached read',out.read_bytes()==data.read_bytes())
 blocked=aws(d,['s3api','get-object','--bucket','not-granted','--key','synthetic.txt',str(out)],False)
 record('unseeded upstream bucket initially refused',blocked.returncode!=0,blocked.stderr.strip())
 aws(d,['s3api','create-bucket','--bucket','not-granted'])
 exposed=aws(d,['s3api','get-object','--bucket','not-granted','--key','synthetic.txt',str(out)],False)
 record('client can expand upstream bucket access via CreateBucket',exposed.returncode==0,{'observation':'namespace bootstrap is not a fixed bucket allowlist'})
 stop(q);upenv['STOW_BUCKET']='fixtures'
 q,d=start('downstream',['--mode','run-through'],upenv)
 restricted=aws(d,['s3api','get-object','--bucket','not-granted','--key','synthetic.txt',str(out)],False)
 record('explicit STOW_BUCKET refuses other bucket after local creation',restricted.returncode!=0 and 'NoSuchKey' in restricted.stderr,restricted.stderr.strip())
 edit=BASE/'edit.bin';edit.write_bytes(b'local-edit')
 aws(d,['s3api','put-object','--bucket','fixtures','--key','nested/a.bin','--body',str(edit)])
 aws(s,['s3api','get-object','--bucket','fixtures','--key','nested/a.bin',str(out)]);record('default downstream write leaves upstream intact',out.read_bytes()==data.read_bytes())
 stop(q);stop(p)
 q,d=start('downstream',['--mode','run-through','--offline'],upenv)
 aws(d,['s3api','get-object','--bucket','fixtures','--key','nested/a.bin',str(out)]);record('offline restart retains local override',out.read_bytes()==b'local-edit')
 aws(d,['s3api','get-object','--bucket','fixtures','--key','cached-only.bin',str(out)])
 record('offline restart serves unmodified warmed bytes with upstream stopped',out.read_bytes()==data.read_bytes())
 miss=aws(d,['s3api','get-object','--bucket','fixtures','--key','never-warmed',str(out)],False)
 record('offline miss explicit',miss.returncode!=0 and 'NoSuchKey' in miss.stderr,miss.stderr.strip())
 stop(q)
 # Independent workspace lifecycle, with transport to a second registry.
 source=BASE/'source';source.mkdir();(source/'task.txt').write_text('input\n')
 manifest=BASE/'manifest.json';manifest.write_text(json.dumps({'version':1,'root':str(BASE/'workspace'),'registry_dir':str(BASE/'reg-a'),'inputs':[{'source':str(source),'destination':'inputs'}]}))
 ws=json.loads(cli(['workspace','prepare','--manifest',str(manifest)]).stdout)
 wid=ws['workspace_id'];common=['--registry-dir',str(BASE/'reg-a')]
 cp1=json.loads(cli(['workspace','checkpoint','--id',wid,*common]).stdout)['checkpoint_id']
 (BASE/'workspace'/'result.txt').write_text('finished\n')
 cp2=json.loads(cli(['workspace','checkpoint','--id',wid,'--parent',cp1,*common]).stdout)['checkpoint_id']
 diff=json.loads(cli(['workspace','diff','--from',cp1,'--to',cp2,*common]).stdout)
 record('workspace prepare/checkpoint/diff',any(c['path']=='result.txt' and c['kind']=='added' for c in diff['changes']),diff)
 transfer=BASE/'transfer';transfer.mkdir();archive=transfer/'snapshot.tar.gz';handoff=transfer/'handoff.json'
 cli(['workspace','handoff','--id',wid,'--checkpoint-id',cp2,*common,'--archive',str(archive),'--output',str(handoff)])
 # Relocate both files and adjust no document fields, checking actual portability.
 receiver=BASE/'receiver';shutil.copytree(transfer,receiver)
 doc=json.loads((receiver/'handoff.json').read_text());record('handoff archive path form',not pathlib.Path(doc['archive']['path']).is_absolute(),{'archive_path':doc['archive']['path']})
 archive.rename(transfer/'removed-original.tar.gz')
 adopt=cli(['workspace','adopt','--handoff',str(receiver/'handoff.json'),'--root',str(BASE/'adopted'),'--registry-dir',str(BASE/'reg-b')],ok=False)
 record('relocated handoff adoption',adopt.returncode==0,adopt.stderr.strip())
 if adopt.returncode!=0:
  doc['archive']['path']='snapshot.tar.gz';(receiver/'handoff.json').write_text(json.dumps(doc))
  adopt=cli(['workspace','adopt','--handoff',str(receiver/'handoff.json'),'--root',str(BASE/'adopted'),'--registry-dir',str(BASE/'reg-b')],ok=False)
  record('adoption after explicitly making archive path relative',adopt.returncode==0,adopt.stderr.strip())
 if adopt.returncode==0:record('adopted bytes', (BASE/'adopted'/'result.txt').read_text()=='finished\n' and (BASE/'adopted'/'inputs'/'task.txt').read_text()=='input\n')
 refusal=cli(['serve','--backend','workspace','--data-dir',str(BASE/'workspace')],ok=False)
 record('workspace HTTP facade availability',False if refusal.returncode else True,refusal.stderr.strip())
except Exception as e:
 record('probe harness exception',False,str(e))
finally:
 for p,log in servers:stop(p);log.close()
 (BASE/'results.json').write_text(json.dumps(results,indent=2))
 pathlib.Path('/private/tmp/stow-assessment-probe-location.txt').write_text(str(BASE))
 print('Evidence directory: '+str(BASE),flush=True)
