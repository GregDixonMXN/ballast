#!/usr/bin/env python3
"""Collect dependency license texts from the installed, locked build inputs."""
import json
import os
from pathlib import Path
import subprocess
import sys

out = Path(sys.argv[1])
go = os.environ.get('GO', 'go')
texts = ['# Third-party notices\n\nDependency notices for this local build. This file does not select a license for Ballast itself.\n']

def licenses(label, directory):
    candidates = [p for p in directory.iterdir() if p.is_file() and p.name.lower().startswith(('license', 'licence', 'copying', 'notice'))]
    if not candidates:
        return
    texts.append('\n## '+label+'\n')
    for p in sorted(candidates):
        text = p.read_text(errors='replace')
        texts.append('\n### '+p.name+'\n\n'+text+'\n')

licenses('Go standard library and runtime', Path(subprocess.check_output([go, 'env', 'GOROOT'], text=True).strip()))
raw = subprocess.check_output([go, 'list', '-m', '-json', 'all'], text=True)
decoder = json.JSONDecoder()
while raw.strip():
    obj, end = decoder.raw_decode(raw.lstrip())
    raw = raw.lstrip()[end:]
    if not obj.get('Main') and obj.get('Dir'):
        licenses(obj['Path']+' '+obj.get('Version',''), Path(obj['Dir']))
for path in sorted(Path('apps/web/node_modules').glob('*/package.json')) + sorted(Path('apps/web/node_modules').glob('@*/*/package.json')):
    info = json.loads(path.read_text())
    licenses(info.get('name',path.parent.name)+' '+info.get('version',''), path.parent)
out.write_text('\n'.join(texts))
