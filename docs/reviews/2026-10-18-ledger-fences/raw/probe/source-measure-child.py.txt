import os,resource,subprocess,sys,json
assert os.getuid()==10001 and os.environ.get('RAILWAY_SERVICE_ID')=='83497842-c51d-420e-8ba1-181589730ad4'
q=subprocess.run(sys.argv[1:],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=80)
sys.stdout.write(q.stdout);sys.stderr.write(q.stderr)
u=resource.getrusage(resource.RUSAGE_CHILDREN)
print('STOW_CHILD_RESOURCE:'+json.dumps({'maxRSSBytes':int(u.ru_maxrss)*1024,'userSeconds':u.ru_utime,'systemSeconds':u.ru_stime}))
sys.exit(q.returncode)
