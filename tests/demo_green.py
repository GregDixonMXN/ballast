#!/usr/bin/env python3
"""One task, one file: the smallest end-to-end run."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from demo_lib import Demo, check

demo = Demo(sys.argv[1] if len(sys.argv) > 1 else 'bin')
try:
    demo.init_repo({'src/a.txt': 'base\n'})
    demo.start_server()
    pid = demo.create_project('green demo')
    t = demo.create_task(pid, 'writer', 'echo ok > src/ok.txt')
    demo.run_task_once(t, 'demo-green-r1')
    css = demo.changesets_for(pid)
    check(len(css) == 1 and css[0]['files'] == ['src/ok.txt'],
          f"changeset holds src/ok.txt ({css})")
    check(demo.api(f"/changesets/{css[0]['id']}/decision", 'POST',
                   {'decision': 'approve'})[1]['status'] == 'APPROVED', 'approved')
    code, res = demo.api(f"/changesets/{css[0]['id']}/integrate", 'POST', {})
    check(code == 200 and res.get('merged') is True, 'integrated')
    check((demo.repo / 'src' / 'ok.txt').read_text() == 'ok\n', 'canonical holds ok.txt')
    print('demo green: green')
finally:
    demo.close()
