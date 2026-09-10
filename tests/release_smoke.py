#!/usr/bin/env python3
"""Exercise the actual server against synthetic local state; never a user DB."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

p = argparse.ArgumentParser()
p.add_argument('--bin-dir', default='bin')
args = p.parse_args()
binaries = Path(args.bin_dir).resolve()
checks = 0

def check(condition, name):
    global checks
    if not condition:
        raise AssertionError(name)
    checks += 1
    print('PASS', name)

with tempfile.TemporaryDirectory(prefix='ballast-release-check-') as tmp:
    root = Path(tmp)
    repo = root / 'repo'
    repo.mkdir()
    env = {k: v for k, v in os.environ.items() if k not in {'DATABASE_URL', 'BALLAST_TOKEN', 'BALLAST_SERVER', 'GIT_DIR', 'GIT_WORK_TREE', 'GIT_INDEX_FILE'}}
    env.update(HOME=str(root), GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null')
    def git(*cmd, cwd=repo):
        return subprocess.check_output(['git', '-C', str(cwd), *cmd], env=env, stderr=subprocess.PIPE).decode().strip()
    git('init', '-q', '-b', 'main')
    git('config', 'user.name', 'Ballast fixture')
    git('config', 'user.email', 'fixture@example.invalid')
    (repo/'notes.txt').write_text('baseline\n')
    (repo/'blob.bin').write_bytes(b'\x00\x01\x02')
    git('add', '.')
    git('commit', '-qm', 'fixture baseline')
    base = git('rev-parse', 'HEAD')
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    url = f'http://127.0.0.1:{port}'
    token_file = root / 'operator.token'
    state = root / 'state.json'
    log = open(root/'server.log', 'w+')
    process = None
    token = ''
    def request(path, method='GET', body=None, credential=None):
        headers = {'Authorization': 'Bearer '+(token if credential is None else credential)}
        if body is not None:
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(url+path, method=method, headers=headers,
            data=None if body is None else json.dumps(body).encode())
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                return resp.status, json.load(resp)
        except urllib.error.HTTPError as err:
            return err.code, json.load(err)
    def start():
        global process, token
        process = subprocess.Popen([str(binaries/'ballast-server'), '--addr', f'127.0.0.1:{port}', '--data', str(state), '--worktrees', str(root/'worktrees'), '--token-file', str(token_file)], cwd=root, env=env, stdout=log, stderr=log)
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError('fixture server exited; no user data was used')
            try:
                with urllib.request.urlopen(url+'/readyz', timeout=.2) as response:
                    if response.status == 200:
                        token = token_file.read_text().strip()
                        return
            except (OSError, urllib.error.URLError):
                time.sleep(.05)
        raise TimeoutError('fixture startup')
    def stop():
        if process and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=12)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    try:
        start()
        check(token_file.stat().st_mode & 0o777 == 0o600, 'private operator credential')
        check(request('/projects', credential='invalid')[0] == 401, 'unauthorized API denied')
        check(request('/events', credential='invalid')[0] == 401, 'unauthorized event stream denied')
        code, project = request('/projects', 'POST', {'name':'Release fixture', 'repo':str(repo), 'branch':'main'})
        check(code == 201, 'project created')
        pid = project['id']
        check(project['canonical_sha'] == base, 'canonical branch pinned')
        check(len(request('/projects')[1]) == 1, 'project discoverable')
        code, task = request(f'/projects/{pid}/tasks', 'POST', {'title':'Review exact file bytes'})
        check(code == 201, 'task created')
        tid = task['id']
        code, ws = request(f'/projects/{pid}/workspaces', 'POST', {'task_id':tid})
        check(code == 201, 'isolated workspace created')
        wid = ws['id']
        worktree = Path(ws['path'])
        (worktree/'notes.txt').write_bytes(b'changed  \n')
        (worktree/'blob.bin').write_bytes(b'\x00\x03\x04')
        (worktree/' leading.txt').write_bytes(b'new file\n')
        # A normal worker commit must remain visible in review.
        git('add', '.', cwd=worktree)
        git('commit', '-qm', 'fixture worker edit', cwd=worktree)
        check(git('rev-parse', 'main') == base, 'worker commit did not move canonical')
        files = request(f'/workspaces/{wid}/files')[1]['files']
        check(set(files) == {'notes.txt','blob.bin',' leading.txt'}, 'committed and exact-name files listed')
        diff = request(f'/workspaces/{wid}/diff')[1]['diff']
        check('GIT binary patch' in diff, 'binary review patch included')
        request(f'/tasks/{tid}/transition', 'POST', {'to':'RUNNING'})
        request(f'/tasks/{tid}/transition', 'POST', {'to':'REVIEW'})
        code, cs = request(f'/workspaces/{wid}/changesets', 'POST', {'project_id':pid, 'task_id':tid})
        check(code == 201 and cs['status'] == 'IN_REVIEW', 'changeset submitted for review')
        cid = cs['id']
        check(request(f'/changesets/{cid}/integrate', 'POST', {})[0] == 409, 'unapproved integration refused')
        stop()
        start()
        check(request(f'/changesets/{cid}')[1]['diff'] == diff, 'review survives server restart')
        check(request(f'/workspaces/{wid}/diff')[1]['diff'] == diff, 'workspace reattached after restart')
        check(request(f'/changesets/{cid}/decision', 'POST', {'decision':'approve'})[1]['status'] == 'APPROVED', 'explicit approval recorded')
        check(git('rev-parse', 'main') == base, 'approval alone does not integrate')
        (repo/'keep.txt').write_text('uncommitted user work\n')
        code, refused = request(f'/changesets/{cid}/integrate', 'POST', {})
        check(not refused.get('merged', False) and git('rev-parse','main') == base, 'dirty canonical tree preserved')
        (repo/'keep.txt').unlink() # only the synthetic file created immediately above
        code, result = request(f'/changesets/{cid}/integrate', 'POST', {})
        check(code == 200 and result.get('merged') is True, 'approved changeset integrated')
        check((repo/'notes.txt').read_bytes() == b'changed  \n', 'trailing spaces preserved in canonical')
        check((repo/'blob.bin').read_bytes() == b'\x00\x03\x04', 'binary bytes preserved in canonical')
        check((repo/' leading.txt').read_bytes() == b'new file\n', 'leading-space filename preserved')
        check(git('status', '--porcelain') == '', 'canonical index and worktree consistent')
        check(request(f'/changesets/{cid}/integrate', 'POST', {})[0] == 409, 'duplicate integration refused')
        check(request(f'/tasks/{tid}')[1]['status'] == 'DONE', 'task completed')
        stop()
        start()
        check(request(f'/changesets/{cid}')[1]['status'] == 'MERGED', 'merged state survives restart')
        check(request(f'/projects/{pid}')[1]['canonical_sha'] == git('rev-parse','main'), 'durable canonical state agrees with Git')
        log.flush()
        check(token not in (root/'server.log').read_text(), 'operator credential absent from logs')
        print(f'{checks} release smoke checks passed')
    finally:
        stop()
        log.close()
