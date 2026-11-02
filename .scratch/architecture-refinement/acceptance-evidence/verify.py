"""Validate retained acceptance evidence; does not run product tests."""
import hashlib
import json
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
def is_source(path):
    return not path.startswith(HERE.relative_to(ROOT).as_posix() + '/') and path.endswith(('.go', '.ts', '.tsx', '.sql', '.yaml', '.json'))


def snapshot_digest(paths, revision):
    """Read immutable Git blobs without changing the index or worktree."""
    refs = [f'{revision}:{path}' for path in paths]
    data = subprocess.check_output(['git', 'cat-file', '--batch'], cwd=ROOT,
                                   input=('\n'.join(refs) + '\n').encode())
    digest = hashlib.sha256()
    offset = 0
    for path in paths:
        end = data.index(b'\n', offset)
        header = data[offset:end].split()
        assert len(header) == 3 and header[1] == b'blob', (revision, path, header)
        size = int(header[2])
        body = data[end + 1:end + 1 + size]
        digest.update(path.encode() + b'\0' + body + b'\0')
        offset = end + size + 2
    assert offset == len(data)
    return digest.hexdigest()


rows = json.loads((HERE / 'stories.json').read_text(encoding='utf8'))
assert [r['id'] for r in rows] == [f'US{i:03}' for i in range(1, 205)]
catalogue = json.loads((HERE / 'catalogue.json').read_text(encoding='utf8'))
events = [json.loads(line) for line in (HERE / 'story-tests.log').read_text(encoding='utf8').splitlines() if line.startswith('{')]
passed = {(e.get('Package', '').removeprefix('manga-manager/'), e.get('Test')) for e in events if e['Action'] == 'pass'}
web = json.loads((HERE / 'web-results.json').read_text(encoding='utf8'))
heatmap = json.loads((HERE / 'heatmap-results.json').read_text(encoding='utf8'))
assert web['success'] and heatmap['success']
assert web['numPassedTests'] == 1008 and heatmap['numPassedTests'] == 16
assert web['numPendingTests'] == heatmap['numPendingTests'] == 0
story_command = json.loads((HERE / 'story-command.json').read_text(encoding='utf8'))
assert (HERE / 'story-tests.command').read_text().strip() == subprocess.list2cmdline(story_command)
assert story_command[:3] == ['go', 'test', '-json'] and '-count=1' in story_command
selection = re.compile(story_command[story_command.index('-run') + 1])
for row in rows:
    c = catalogue[row['group']]
    for path, symbol in re.findall(r'([\w/.-]+\.(?:go|tsx|ts))::(\w+)', c['implementation'] + '; ' + c['consumer']):
        assert re.search(r'\b' + symbol + r'\b', (ROOT / path).read_text(encoding='utf8')), (path, symbol)
    for test in row['tests']:
        assert test['symbol'] in (ROOT / test['path']).read_text(encoding='utf8')
        assert selection.fullmatch(test['symbol']), (row['id'], test)
        assert './' + Path(test['path']).parent.as_posix() in story_command, (row['id'], test)
        assert (str(Path(test['path']).parent).replace(chr(92), '/'), test['symbol']) in passed, (row['id'], test)
    for test in row['web']:
        assert any(a['title'] == test['case'] and a['status'] == 'passed'
                   for result in web['testResults']
                   if result['name'].replace(chr(92), '/').endswith(test['path'].removeprefix('web/'))
                   for a in result['assertionResults']), (row['id'], test)
text = (HERE.parent / 'acceptance.md').read_text(encoding='utf8')
assert len(re.findall(r'(?m)^\| \*\*US\d{3}\*\*', text)) == 204
failures = {'doc-style': 124, 'linux-build': 1, 'darwin-build': 1}
commands = {p.stem for p in HERE.glob('*.command')}
assert commands == {p.stem for p in HERE.glob('*.exit')}
assert commands == {p.stem for p in HERE.glob('*.log')} - {'verification'}
for name in sorted(commands):
    assert (HERE / (name + '.command')).read_text().strip(), name
    assert int((HERE / (name + '.exit')).read_text()) == failures.get(name, 0), name
manifest = json.loads((HERE / 'manifest.json').read_text(encoding='utf8'))
assert set(manifest['sha256']) == {p.name for p in HERE.iterdir() if p.is_file()} - {'manifest.json', 'verification.log'}
for artifact in HERE.iterdir():
    if artifact.is_file():
        raw = artifact.read_bytes()
        assert b'\r' not in raw and not re.search(rb'[ \t]+(?=\n|$)', raw), artifact.name
        assert not raw.endswith(b'\n\n'), artifact.name
for path, expected in manifest['sha256'].items():
    assert hashlib.sha256((HERE / path).read_bytes()).hexdigest() == expected, path
paths = sorted(p for p in subprocess.check_output(['git', 'ls-files'], cwd=ROOT, text=True).splitlines() if is_source(p))
head_paths = sorted(p for p in subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', 'HEAD'], cwd=ROOT, text=True).splitlines() if is_source(p))
assert paths == head_paths, 'HEAD/index source paths differ'
audit = json.loads((HERE / 'structure-audit.json').read_text())
assert len(paths) == audit['source_count']
digest = hashlib.sha256()
for path in paths:
    digest.update(path.encode() + b'\0' + (ROOT / path).read_bytes() + b'\0')
assert digest.hexdigest() == audit['source_digest'], 'worktree source changed'
assert snapshot_digest(paths, '') == audit['source_digest'], 'index source changed'
assert snapshot_digest(paths, 'HEAD') == audit['source_digest'], 'HEAD source changed'
print('PASS: 204 story rows, 25 capabilities, all referenced Go/web witnesses executed, 1024 frontend tests, gate states, evidence hashes and source digest validated.')
print('This validator is not test execution or independent acceptance review.')
