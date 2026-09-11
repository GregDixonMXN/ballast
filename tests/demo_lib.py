#!/usr/bin/env python3
"""Shared harness for Ballast no-model demos: temp repo, temp server,
real ballast-runner pipeline. No API keys anywhere by construction."""
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

STRIP_ENV = {'DATABASE_URL', 'BALLAST_TOKEN', 'BALLAST_SERVER', 'GIT_DIR',
             'GIT_WORK_TREE', 'GIT_INDEX_FILE', 'META_API_KEY',
             'OPENAI_API_KEY', 'ANTHROPIC_API_KEY'}


class Demo:
    def __init__(self, bin_dir):
        self.tmp = tempfile.TemporaryDirectory(prefix='ballast-demo-')
        self.root = Path(self.tmp.name)
        self.bins = Path(bin_dir).resolve()
        env = {k: v for k, v in os.environ.items() if k not in STRIP_ENV}
        env.update(HOME=str(self.root), GIT_CONFIG_NOSYSTEM='1',
                   GIT_CONFIG_GLOBAL='/dev/null')
        self.env = env
        self.repo = self.root / 'repo'
        self.server = None
        self.token = ''
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            self.port = sock.getsockname()[1]
        self.url = f'http://127.0.0.1:{self.port}'

    def git(self, *cmd, cwd=None):
        return subprocess.check_output(
            ['git', '-C', str(cwd or self.repo), *cmd],
            env=self.env, stderr=subprocess.PIPE).decode().strip()

    def init_repo(self, files):
        self.repo.mkdir()
        self.git('init', '-q', '-b', 'main')
        self.git('config', 'user.name', 'Ballast demo')
        self.git('config', 'user.email', 'demo@example.invalid')
        for name, content in files.items():
            p = self.repo / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(content)
        self.git('add', '.')
        self.git('commit', '-qm', 'demo baseline')
        return self.git('rev-parse', 'HEAD')

    def start_server(self):
        log = open(self.root / 'server.log', 'w+')
        self.server = subprocess.Popen(
            [str(self.bins / 'ballast-server'), '--addr', f'127.0.0.1:{self.port}',
             '--data', str(self.root / 'state.json'),
             '--worktrees', str(self.root / 'worktrees'),
             '--token-file', str(self.root / 'operator.token')],
            cwd=self.root, env=self.env, stdout=log, stderr=log)
        for _ in range(200):
            if self.server.poll() is not None:
                raise RuntimeError('demo server exited')
            try:
                with urllib.request.urlopen(self.url + '/readyz', timeout=.2) as r:
                    if r.status == 200:
                        self.token = (self.root / 'operator.token').read_text().strip()
                        return
            except (OSError, urllib.error.URLError):
                time.sleep(.05)
        raise TimeoutError('demo server startup')

    def stop_server(self):
        if self.server and self.server.poll() is None:
            self.server.terminate()
            try:
                self.server.wait(timeout=12)
            except subprocess.TimeoutExpired:
                self.server.kill()
                self.server.wait()

    def api(self, path, method='GET', body=None):
        req = urllib.request.Request(
            self.url + path, method=method,
            headers={'Authorization': 'Bearer ' + self.token,
                     'Content-Type': 'application/json'},
            data=None if body is None else json.dumps(body).encode())
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.status, json.load(resp)
        except urllib.error.HTTPError as err:
            try:
                return err.code, json.load(err)
            except Exception:
                return err.code, {}

    def create_project(self, name):
        code, p = self.api('/projects', 'POST',
                            {'name': name, 'repo': str(self.repo), 'branch': 'main'})
        assert code == 201, f'project create: {code} {p}'
        return p['id']

    def create_task(self, pid, title, command):
        code, t = self.api(f'/projects/{pid}/tasks', 'POST',
                            {'title': title, 'command': command})
        assert code == 201, f'task create: {code} {t}'
        return t['id']

    def run_task_once(self, tid, name):
        """Assign tid to a live runner running the real ballast-runner
        pipeline (claim, worktree, TASK_CMD, gate, changeset, exit).
        The runner is stopped once its changeset lands."""
        runner = subprocess.Popen(
            [str(self.bins / 'ballast-runner'), '-server', self.url,
             '-token-file', str(self.root / 'operator.token'),
             '--worktrees', str(self.root / 'worktrees'),
             '-name', name, '-task-timeout', '60s'],
            env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            rid = None
            for _ in range(200):
                if runner.poll() is not None:
                    _, err = runner.communicate()
                    raise RuntimeError(f'runner {name} exited early: {err.decode()[-500:]}')
                _, runners = self.api('/runners')
                for r in runners:
                    if r.get('hostname') == name:
                        rid = r['id']
                        break
                if rid:
                    break
                time.sleep(.1)
            assert rid, f'runner {name} never registered'
            code, assigned = self.api(
                f'/tasks/{tid}/assign', 'POST',
                {'runner_id': rid, 'adapter': 'cmd'})
            assert code == 201, f'assign: {code} {assigned}'
            wsid = assigned['workspace']['id']
            for _ in range(900):
                if runner.poll() is not None:
                    raise RuntimeError(f'runner {name} exited mid-task')
                ws_state = self.api(f"/workspaces/{wsid}")
                if ws_state[1].get('status') in ('COMPLETED', 'FAILED'):
                    break
                time.sleep(.1)
            else:
                raise RuntimeError(f'runner {name} never reported')
            return assigned['workspace']
        finally:
            if runner.poll() is None:
                runner.terminate()
                try:
                    runner.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    runner.kill()
                    runner.wait()

    def changesets_for(self, pid):
        _, css = self.api(f'/projects/{pid}/changesets')
        return css

    def close(self):
        self.stop_server()
        self.tmp.cleanup()


def check(cond, name):
    if not cond:
        raise AssertionError(f'FAIL {name}')
    print('PASS', name)
