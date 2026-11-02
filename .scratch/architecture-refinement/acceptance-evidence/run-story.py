import pathlib,json,subprocess,os
os.environ.update(GOTOOLCHAIN='go1.26.10',GOCACHE='C:/Users/saber/dev/manga_manager/.gocache',GOTMPDIR='C:/Users/saber/dev/manga-refinement-48/.tmp/go')
cmd=json.loads(pathlib.Path('.tmp/story-command.json').read_text())
pathlib.Path('.tmp/evidence/story-tests.command').write_text(subprocess.list2cmdline(cmd))
with pathlib.Path('.tmp/evidence/story-tests.log').open('w',encoding='utf8') as log:
 result=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT)
pathlib.Path('.tmp/evidence/story-tests.exit').write_text(str(result.returncode))
