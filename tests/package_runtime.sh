#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$1" && pwd)
checks=$(cd "$(dirname "$0")" && pwd)
python3 "$checks/release_smoke.py" --bin-dir "$root/bin"
state=$(mktemp -d)
launch_pid=''
cleanup() {
  if [[ -n $launch_pid ]]; then kill -TERM "$launch_pid" 2>/dev/null || true; wait "$launch_pid" 2>/dev/null || true; fi
  rm -rf -- "$state"
}
trap cleanup EXIT
BALLAST_STATE_DIR="$state/state" BALLAST_API_PORT=18080 BALLAST_WEB_PORT=13000 "$root/scripts/start.sh" >"$state/launch.log" 2>&1 &
launch_pid=$!
ready=false
for ((attempt=0; attempt<100; attempt++)); do
  kill -0 "$launch_pid" 2>/dev/null || { cat "$state/launch.log"; exit 1; }
  if curl --noproxy '*' -fsS --max-time 1 http://127.0.0.1:13000/ >"$state/page.html" 2>/dev/null; then ready=true; break; fi
  sleep .1
done
[[ $ready == true ]] || { cat "$state/launch.log"; exit 1; }
python3 - "$state/page.html" <<'PY'
import sys
from pathlib import Path
assert 'Ballast' in Path(sys.argv[1]).read_text(), 'packaged dashboard did not render'
print('PASS packaged dashboard launches and renders')
PY
