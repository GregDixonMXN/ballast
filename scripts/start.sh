#!/usr/bin/env bash
# Start the local API + dashboard; runners are explicitly started separately.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
state=${BALLAST_STATE_DIR:-"$root/.ballast"}
mkdir -p "$state"
state=$(cd "$state" && pwd)
chmod 700 "$state"
api_port=${BALLAST_API_PORT:-8080}
web_port=${BALLAST_WEB_PORT:-3000}
for port in "$api_port" "$web_port"; do
  if [[ ! $port =~ ^[0-9]+$ ]] || ((port < 1 || port > 65535)); then echo 'Ports must be integers from 1 to 65535.' >&2; exit 2; fi
done
command -v node >/dev/null || { echo 'Node.js 24 is required.' >&2; exit 1; }
command -v git >/dev/null || { echo 'Git is required.' >&2; exit 1; }
command -v curl >/dev/null || { echo 'curl is required for readiness checks.' >&2; exit 1; }
web="$root/web"
if [[ ! -f "$web/server.js" ]]; then web="$root/apps/web/.next/standalone"; fi
[[ -x "$root/bin/ballast-server" && -f "$web/server.js" ]] || { echo 'Build first: ./scripts/build.sh' >&2; exit 1; }
# Next standalone needs its static assets beside the generated server.
if [[ $web == "$root/apps/web/.next/standalone" ]]; then
  mkdir -p "$web/.next"
  cp -a "$root/apps/web/.next/static" "$web/.next/"
  if [[ -d "$root/apps/web/public" ]]; then cp -a "$root/apps/web/public" "$web/"; fi
fi
children=()
cleanup() {
  trap - EXIT INT TERM
  for child in "${children[@]}"; do kill "$child" 2>/dev/null || true; done
  for child in "${children[@]}"; do wait "$child" 2>/dev/null || true; done
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
(cd "$state" && exec "$root/bin/ballast-server" --addr "127.0.0.1:$api_port" --data "$state/state.json" --token-file "$state/operator.token" --worktrees "$state/worktrees") &
api_pid=$!
children+=("$api_pid")
ready=false
for ((attempt=0; attempt<100; attempt++)); do
  kill -0 "$api_pid" 2>/dev/null || { echo 'API failed to start.' >&2; exit 1; }
  if curl --noproxy '*' --max-time 1 -fsS "http://127.0.0.1:$api_port/readyz" >/dev/null; then ready=true; break; fi
  sleep 0.1
done
[[ $ready == true ]] || { echo 'API readiness timed out.' >&2; exit 1; }
(cd "$web" && HOSTNAME=127.0.0.1 PORT="$web_port" BALLAST_API_URL="http://127.0.0.1:$api_port" exec node server.js) &
children+=("$!")
printf '\nBallast: http://127.0.0.1:%s\nState: %s\nPrivate operator credential: %s/operator.token\nUse Ctrl+C to stop.\n\n' "$web_port" "$state" "$state"
wait -n "${children[@]}"
