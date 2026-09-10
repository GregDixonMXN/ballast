#!/usr/bin/env python3
"""Actual runner/server smoke; all repositories, homes and credentials are temporary."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument("--bin-dir", default="bin")
binaries = Path(parser.parse_args().bin_dir).resolve()
checks = 0

def check(ok, name):
    global checks
    if not ok:
        raise AssertionError(name)
    checks += 1
    print("PASS", name, flush=True)

def until(fn, label, seconds=20):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = fn()
        if value:
            return value
        time.sleep(.1)
    raise TimeoutError(label)

with tempfile.TemporaryDirectory(prefix="ballast-runner-check-") as tmp:
    root = Path(tmp)
    home = root / "agent-home"
    home.mkdir(mode=0o700)
    (home / "fixture-config").write_text("synthetic-config")
    env = {"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": str(root),
           "LANG": "C.UTF-8", "GIT_CONFIG_NOSYSTEM": "1",
           "GIT_CONFIG_GLOBAL": "/dev/null", "BALLAST_TOKEN": "synthetic-must-not-inherit"}
    repo = root / "repo"
    repo.mkdir()
    def git(*args):
        return subprocess.check_output(["git", "-C", str(repo), *args], env=env).decode().strip()
    git("init", "-q", "-b", "main")
    git("config", "user.name", "Synthetic fixture")
    git("config", "user.email", "fixture@example.invalid")
    (repo / "baseline").write_text("base\n")
    git("add", ".")
    git("commit", "-qm", "baseline")
    base = git("rev-parse", "HEAD")
    fixture = root / "fixture"
    fixture.write_text("""#!/bin/sh
set -eu
test -z "${BALLAST_TOKEN:-}"
test "$(cat "$HOME/fixture-config")" = synthetic-config
printf fixture > result.txt
case "$1" in
fail) exit 7 ;;
slow) (sleep 3; printf leaked > orphan.txt) & wait ;;
esac
""")
    fixture.chmod(0o700)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    url = f"http://127.0.0.1:{port}"
    token_file = root / "operator.token"
    processes = []
    logs = []
    token = ""
    def launch(name, args):
        log = open(root / (name + ".log"), "a+")
        logs.append(log)
        proc = subprocess.Popen(args, cwd=root, env=env, stdout=log, stderr=log)
        processes.append(proc)
        return proc
    def stop(proc):
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=8)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
                raise AssertionError("process failed graceful shutdown")
    def ready():
        try:
            return urllib.request.urlopen(url + "/readyz", timeout=.2).status == 200
        except OSError:
            return False
    def start_server():
        global token
        proc = launch("server", [str(binaries / "ballast-server"), "--addr", f"127.0.0.1:{port}",
            "--data", str(root / "state.json"), "--token-file", str(token_file),
            "--worktrees", str(root / "worktrees")])
        until(ready, "server readiness")
        token = token_file.read_text().strip()
        return proc
    def api(path, body=None):
        req = urllib.request.Request(url + path, data=None if body is None else json.dumps(body).encode(),
            headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=5) as resp:
            return json.load(resp)
    def start_runner():
        return launch("runner", [str(binaries / "ballast-runner"), "--server", url,
            "--token-file", str(token_file), "--worktrees", str(root / "worktrees"),
            "--agent", str(fixture), "--agent-home", str(home), "--name", "synthetic",
            "--task-timeout", "2s"])
    def runner_id():
        entries = api("/runners")
        return entries[-1]["id"] if entries else None
    def assign(prompt, tests=""):
        task = api(f"/projects/{pid}/tasks", {"title": prompt})
        assigned = api(f"/tasks/{task['id']}/assign",
            {"runner_id": rid, "adapter": "custom", "prompt": prompt, "test_command": tests})
        return task["id"], assigned["workspace"]
    def status(tid):
        return api("/tasks/" + tid)["status"]
    try:
        server = start_server()
        pid = api("/projects", {"name": "Runner fixture", "repo": str(repo), "branch": "main"})["id"]
        runner = start_runner()
        rid = until(runner_id, "runner registration")
        check("custom" in api("/runners")[0]["capabilities"], "actual runner advertises custom adapter")
        tid, ws = assign("success", "/usr/bin/test -f result.txt")
        until(lambda: status(tid) == "REVIEW", "successful task review")
        check(Path(ws["path"], "result.txt").read_text() == "fixture", "adapter executes in isolated worktree with selected home")
        changes = api(f"/projects/{pid}/changesets")
        check(len(changes) == 1 and changes[0]["status"] == "IN_REVIEW", "success plus tests creates review")
        check(git("rev-parse", "HEAD") == base and not (repo / "result.txt").exists(), "runner never integrates canonical")
        for prompt, tests, label in [("fail", "", "agent failure"), ("success", "/usr/bin/false", "test failure"), ("slow", "", "timeout cancellation")]:
            tid, failed = assign(prompt, tests)
            until(lambda: status(tid) == "BLOCKED", label)
            check(api("/workspaces/" + failed["id"])["status"] == "FAILED", label + " fails closed")
            if prompt == "slow":
                time.sleep(3.2)
                check(not Path(failed["path"], "orphan.txt").exists(), "timeout kills descendant process group")
        check(len(api(f"/projects/{pid}/changesets")) == 1, "failures produce no reviewable changesets")
        tid, interrupted = assign("slow")
        until(lambda: Path(interrupted["path"], "result.txt").exists(), "active subprocess")
        stop(runner)
        until(lambda: status(tid) == "BLOCKED", "SIGTERM report")
        time.sleep(3.2)
        check(not Path(interrupted["path"], "orphan.txt").exists(), "runner SIGTERM cancels descendants and reports failure")
        runner = start_runner()
        rid = until(lambda: (v if (v := runner_id()) != rid else None), "new runner registration")
        tid, queued = assign("success")
        stop(server)
        server = start_server()
        check(status(tid) == "BLOCKED" and api("/workspaces/" + queued["id"])["status"] == "FAILED", "server restart blocks interrupted dispatch")
        rid = until(runner_id, "runner re-registers after server restart")
        check(not Path(queued["path"], "result.txt").exists(), "restart does not replay queued command")
        tid, ws = assign("success", "/usr/bin/test -f result.txt")
        until(lambda: status(tid) == "REVIEW", "post-restart execution")
        check(True, "runner executes fresh assignment after re-registration")
        check((home / "fixture-config").read_text() == "synthetic-config", "explicit agent home preserved")
        for log in logs:
            log.flush()
        check(all(token not in path.read_text() for path in root.glob("*.log")), "operator token absent from process logs")
        print(f"{checks} runner smoke checks passed")
    finally:
        for proc in reversed(processes):
            stop(proc)
        for log in logs:
            log.close()
