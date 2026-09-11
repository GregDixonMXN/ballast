#!/usr/bin/env python3
"""Two writers, one file: first integrate wins, second never force-merges."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from demo_lib import Demo, check

demo = Demo(sys.argv[1] if len(sys.argv) > 1 else 'bin')
try:
    base = demo.init_repo({'src/a.txt': 'base\n'})
    demo.start_server()
    pid = demo.create_project('overlap demo')
    t1 = demo.create_task(pid, 'writer one', 'echo one >> src/a.txt')
    t2 = demo.create_task(pid, 'writer two', 'echo two >> src/a.txt')
    # Both workspaces pin the same base BEFORE either integrates.
    demo.run_task_once(t1, 'demo-r1')
    demo.run_task_once(t2, 'demo-r2')
    css = demo.changesets_for(pid)
    check(len(css) == 2, 'two changesets from two runs')
    for cs in css:
        check(cs['status'] == 'IN_REVIEW', f"changeset {cs['id'][:8]} awaiting review")
        print('changeset', cs['id'][:8], 'files:', cs['files'])
    first, second = css[0], css[1]
    check(demo.api(f"/changesets/{first['id']}/decision", 'POST',
                   {'decision': 'approve'})[1]['status'] == 'APPROVED', 'first approved')
    code, res = demo.api(f"/changesets/{first['id']}/integrate", 'POST', {})
    check(code == 200 and res.get('merged') is True, 'first integrates')
    check((demo.repo / 'src' / 'a.txt').read_text() == 'base\none\n', 'canonical holds writer one')
    _, sib = demo.api(f"/changesets/{second['id']}")
    if sib['status'] == 'IN_REVIEW':
        check(demo.api(f"/changesets/{second['id']}/decision", 'POST',
                       {'decision': 'approve'})[1]['status'] == 'APPROVED', 'second approved')
        code, res = demo.api(f"/changesets/{second['id']}/integrate", 'POST', {})
        merged = res.get('merged') is True
        check(not merged, f'second never force-merges (http {code}, {res})')
    else:
        # Sibling revalidation already marked the loser at first integrate.
        check(sib['status'] in ('NEEDS_REBASE', 'CONFLICTED'),
              f"sibling pre-marked {sib['status']}")
        code, res = demo.api(f"/changesets/{second['id']}/integrate", 'POST', {})
        check(res.get('merged') is not True, f'loser integrate refused ({code}, {res})')
        print('second never force-merges (pre-marked, integrate refused)')
    _, css2 = demo.api(f"/projects/{pid}/changesets")
    sib = [c for c in css2 if c['id'] == second['id']][0]
    check(sib['status'] in ('NEEDS_REBASE', 'CONFLICTED'),
          f"sibling marked {sib['status']}")
    check((demo.repo / 'src' / 'a.txt').read_text() == 'base\none\n', 'canonical untouched by loser')
    print('demo overlap: green')
finally:
    demo.close()
