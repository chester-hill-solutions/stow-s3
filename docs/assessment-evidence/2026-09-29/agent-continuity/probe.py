"""Disposable continuity experiment; two deterministic workers, not model agents."""
import hashlib, json, os, pathlib, select, shutil, subprocess, sys, tempfile
import boto3
from botocore.config import Config

BIN = '/Users/ladmin/.codex/worktrees/stow-product-assessment/stow/bin/stow-s3'
BASE = pathlib.Path(tempfile.mkdtemp(prefix='stow-agent-continuity-', dir='/private/tmp'))
sender = BASE/'sender'; sender.mkdir()
receiver = BASE/'receiver'; receiver.mkdir()
env = {'PATH': '/usr/bin:/bin:/opt/homebrew/bin', 'HOME': str(BASE/'empty-home'),
       'TMPDIR': str(BASE), 'AWS_EC2_METADATA_DISABLED': 'true'}
pathlib.Path(env['HOME']).mkdir()
processes = []; results = []

def note(name, passed, detail=None):
    row = dict(name=name, passed=bool(passed), detail=detail)
    results.append(row); print(json.dumps(row), flush=True)

def cli(*args, extra=None, check=True):
    p = subprocess.run([BIN, *map(str,args)], env=env | (extra or {}),
                       capture_output=True, text=True, timeout=30)
    if check and p.returncode: raise RuntimeError(p.stderr)
    return p

def start(name, *args, extra=None):
    r,w = os.pipe()
    log = open(BASE/(name+'.log'), 'wb')
    p = subprocess.Popen([BIN,'serve','--port','0','--data-dir',str(sender/name),
                          '--ready-fd',str(w),*args], env=env | (extra or {}),
                         pass_fds=(w,), stdout=log, stderr=log)
    processes.append((p,log)); os.close(w)
    if not select.select([r],[],[],15)[0]: raise RuntimeError('readiness timeout')
    with os.fdopen(r) as stream: ready=json.loads(stream.readline())
    client=boto3.client('s3', endpoint_url=ready['endpoint'],
        aws_access_key_id=ready['accessKeyId'], aws_secret_access_key=ready['secretAccessKey'],
        region_name='us-east-1', config=Config(s3={'addressing_style':'path'},
        request_checksum_calculation='when_required', response_checksum_validation='when_required'))
    return p, ready, client

def stop(p):
    if p.poll() is None:
        p.terminate()
        try: p.wait(5)
        except subprocess.TimeoutExpired: p.kill(); p.wait()

worker = '''import csv,hashlib,json,os,pathlib,sys
root=pathlib.Path.cwd()
assert not any(k.startswith(('AWS_','STOW_','S3_')) for k in os.environ)
brief=json.loads((root/'task.json').read_text())
source=root/brief['input']
assert hashlib.sha256(source.read_bytes()).hexdigest()==brief['input_sha256']
progress=root/'progress.json'
if sys.argv[1]=='prepare':
    rows=list(csv.DictReader(source.open()))
    valid=[r for r in rows if r['status']=='paid']
    (root/'validated.json').write_text(json.dumps(valid))
    progress.write_text(json.dumps({'completed':['validate input','select paid rows'],
        'next':'aggregate paid amounts by customer and write report.json',
        'rows':len(rows),'paid_rows':len(valid),
        'validated_sha256':hashlib.sha256((root/'validated.json').read_bytes()).hexdigest()}))
    print('planned pause: validated input; aggregation remains')
    sys.exit(75)
state=json.loads(progress.read_text())
assert state['next']=='aggregate paid amounts by customer and write report.json'
assert hashlib.sha256((root/'validated.json').read_bytes()).hexdigest()==state['validated_sha256']
totals={}
for row in json.loads((root/'validated.json').read_text()):
    totals[row['customer']]=totals.get(row['customer'],0)+int(row['amount_cents'])
answer={'currency':'USD','by_customer_cents':totals,'total_cents':sum(totals.values()),
        'source_sha256':brief['input_sha256'],'continued_from_saved_progress':True}
(root/'report.json').write_text(json.dumps(answer,indent=2))
state['completed'].append('aggregate paid amounts by customer');state['next']=None
progress.write_text(json.dumps(state,indent=2))
print(json.dumps(answer))
'''

try:
    p, upstream, s3 = start('upstream')
    csv=b'customer,amount_cents,status\nalice,1200,paid\nbob,900,paid\nalice,300,paid\nbob,500,pending\n'
    s3.create_bucket(Bucket='task-inputs')
    s3.put_object(Bucket='task-inputs',Key='orders.csv',Body=csv,ChecksumAlgorithm='CRC32')
    upstream_env={'STOW_ENDPOINT':upstream['endpoint'],'STOW_ACCESS_KEY_ID':upstream['accessKeyId'],
        'STOW_SECRET_ACCESS_KEY':upstream['secretAccessKey'],'STOW_REGION':'us-east-1', 'STOW_BUCKET':'task-inputs'}
    cli('prewarm','--data-dir',sender/'cache','--bucket','task-inputs','--keys','orders.csv',extra=upstream_env)
    stop(p)
    q, local, cache = start('cache','--mode','run-through','--offline',extra=upstream_env)
    captured=cache.get_object(Bucket='task-inputs',Key='orders.csv')['Body'].read()
    stop(q)
    note('input recovered from offline cache after upstream stopped',captured==csv)
    stage=sender/'stage';stage.mkdir();(stage/'orders.csv').write_bytes(captured)
    (stage/'worker.py').write_text(worker)
    (stage/'task.json').write_text(json.dumps({'task':'Sum paid orders by customer in integer cents',
        'input':'orders.csv','input_sha256':hashlib.sha256(csv).hexdigest(),
        'runtime':'Python 3 standard library','output':'report.json'},indent=2))
    manifest=sender/'manifest.json'
    manifest.write_text(json.dumps({'version':1,'root':str(sender/'work'),
        'registry_dir':str(sender/'registry'),'inputs':[{'source':str(stage),'destination':'.'}]}))
    prepared=json.loads(cli('workspace','prepare','--manifest',manifest).stdout)
    first=subprocess.run([sys.executable,'worker.py','prepare'],cwd=sender/'work',
        env={k:v for k,v in env.items() if not k.startswith('AWS_')},capture_output=True,text=True,timeout=10)
    note('worker A pauses with saved progress',first.returncode==75,first.stdout.strip())
    if first.returncode!=75: raise RuntimeError(first.stderr)
    cp=json.loads(cli('workspace','checkpoint','--id',prepared['workspace_id'],
        '--registry-dir',sender/'registry').stdout)
    bundle=sender/'bundle';bundle.mkdir()
    cli('workspace','handoff','--id',prepared['workspace_id'],'--checkpoint-id',cp['checkpoint_id'],
        '--registry-dir',sender/'registry','--archive',bundle/'checkpoint.tar.gz','--output',bundle/'handoff.json')
    shutil.copytree(bundle,receiver/'bundle')
    # Make old locations unavailable, without deleting source evidence.
    sender.rename(BASE/'sender-retired')
    args=['workspace','adopt','--handoff',receiver/'bundle'/'handoff.json',
        '--root',receiver/'work','--registry-dir',receiver/'registry']
    original=cli(*args,check=False)
    note('unmodified relocated handoff works',original.returncode==0,original.stderr.strip())
    handoff=receiver/'bundle'/'handoff.json';doc=json.loads(handoff.read_text())
    doc['archive']['path']='checkpoint.tar.gz';handoff.write_text(json.dumps(doc,indent=2))
    adopted=json.loads(cli(*args).stdout)
    note('adoption with explicit relative-path workaround',True,{'workspace_id':adopted['workspace_id']})
    secrets=[upstream['accessKeyId'].encode(),upstream['secretAccessKey'].encode(),
             local['accessKeyId'].encode(),local['secretAccessKey'].encode()]
    leaks=[str(path.relative_to(receiver/'work')) for path in (receiver/'work').rglob('*')
           if path.is_file() and any(s in path.read_bytes() for s in secrets)]
    note('generated upstream and local credentials absent from adopted files',not leaks,leaks)
    baseline=json.loads(cli('workspace','checkpoint','--id',adopted['workspace_id'],
        '--registry-dir',receiver/'registry').stdout)
    second=subprocess.run([sys.executable,'worker.py','continue'],cwd=receiver/'work',
        env={k:v for k,v in env.items() if not k.startswith('AWS_')},capture_output=True,text=True,timeout=10)
    if second.returncode:raise RuntimeError(second.stderr)
    answer=json.loads((receiver/'work'/'report.json').read_text())
    note('worker B finishes from adopted files',answer['by_customer_cents']=={'alice':1500,'bob':900}
         and answer['total_cents']==2400,answer)
    done=json.loads(cli('workspace','checkpoint','--id',adopted['workspace_id'],
        '--parent',baseline['checkpoint_id'],'--registry-dir',receiver/'registry').stdout)
    diff=json.loads(cli('workspace','diff','--from',baseline['checkpoint_id'],'--to',done['checkpoint_id'],
        '--registry-dir',receiver/'registry').stdout)
    note('completion diff contains only progress and report',
         {c['path'] for c in diff['changes']}=={'progress.json','report.json'},diff)
    note('original sender paths unavailable during continuation',not sender.exists())
    # Test tamper refusal independently of the successful bundle.
    corrupt=receiver/'corrupt';shutil.copytree(receiver/'bundle',corrupt)
    with (corrupt/'checkpoint.tar.gz').open('ab') as f:f.write(b'tamper')
    refusal=cli('workspace','adopt','--handoff',corrupt/'handoff.json','--root',receiver/'should-not-exist',
        '--registry-dir',receiver/'corrupt-registry',check=False)
    note('tampered archive refused before workspace creation',refusal.returncode!=0
         and not (receiver/'should-not-exist').exists(),refusal.stderr.strip())
except Exception as e:
    note('experiment exception',False,str(e))
finally:
    for p,log in processes:stop(p);log.close()
    (BASE/'results.json').write_text(json.dumps(results,indent=2))
    pathlib.Path('/private/tmp/stow-agent-continuity-location.txt').write_text(str(BASE))
    print('Evidence: '+str(BASE))
