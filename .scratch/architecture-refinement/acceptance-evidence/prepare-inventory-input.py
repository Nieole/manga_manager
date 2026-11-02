import pathlib,re,json
root=pathlib.Path('.')
s=pathlib.Path('C:/Users/saber/AppData/Local/Temp/manga-refinement-204-story-evidence-inventory.md').read_text(encoding='utf8')
rows=[]
for l in s.splitlines():
 if not l.startswith('| **US'):continue
 c=l.split('|')
 n=int(re.search(r'US(\d+)',c[1])[1]); group=c[2].strip(); refs=re.findall(r'`([^`]+)::([^`]+)`',c[3])
 web=re.findall(r'`(web/[^`]+)`\s*::\s*(.*?)(?:$|;)',c[3])
 for p,t in refs:
  assert pathlib.Path(p).exists(),p
  assert t in pathlib.Path(p).read_text(encoding='utf8'),t
 rows.append(dict(id=f'US{n:03}',group=group,tests=[{'path':p,'symbol':t} for p,t in refs],web=[{'path':p,'case':t.strip()} for p,t in web]))
assert len(rows)==204
pathlib.Path('.tmp/stories.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2),encoding='utf8')
tests=sorted({x['symbol'] for r in rows for x in r['tests'] if x['path'].endswith('.go')})
pkgs=sorted({'./'+str(pathlib.PurePosixPath(x['path']).parent) for r in rows for x in r['tests'] if x['path'].endswith('.go')})
# Current production lifecycle witness supplements the superseded inventory.
p=pathlib.Path('internal/bootstrap/sse_shutdown_test.go').read_text(encoding='utf8')
tests+=re.findall(r'func (Test\w+)\(',p)
cmd=['go','test','-json','-count=1','-timeout=10m','-run','^('+'|'.join(tests)+')$']+pkgs
pathlib.Path('.tmp/story-command.json').write_text(json.dumps(cmd),encoding='utf8')
print(len(rows),len(tests),len(pkgs))
