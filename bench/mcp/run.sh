#!/usr/bin/env bash
# Benchmarks one MCP tool call through each way of serving it, every server
# in its own container with the same CPU and memory limits, on one podman
# network:
#
#   nest       NestJS MCP (stateful sessions) -> HTTP REST -> app server
#   loopback   dedo's Go MCP (<app>-mcp)      -> Connect   -> app server
#   inprocess  dedo's Go MCP inside the app server, no second hop
#
# for two apps: todo, and contacts, whose every call is also checked
# against the caller's location. Every call passes the same security
# checks: the app server enforces scopes, tenants, flow IDs and the error
# audit for each variant, and the load driver stops on any failed call. Results go to OUT (default: a temporary
# directory), never into the repository.
#
#   bench/mcp/run.sh                 # all variants
#   APPS=contacts VARIANTS="inprocess loopback" CLIENTS="1 32" DURATION=30s bench/mcp/run.sh
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${OUT:-$(mktemp -d -t mcp-bench)}
mkdir -p "$OUT"
APPS=${APPS:-"todo contacts"}
VARIANTS=${VARIANTS:-"nest loopback inprocess"}
CLIENTS=${CLIENTS:-"1 32"}
WARMUP=${WARMUP:-5s}
DURATION=${DURATION:-20s}
CPUS=${CPUS:-1}
MEMORY=${MEMORY:-1g}
LOAD_CPUS=${LOAD_CPUS:-2}
NET=mcpbench
NO_AUTH=$OUT/no-auth.json

# The calls each app serves: name, tool, arguments.
TODO_CALLS=(
  'get|get_todo|{"name":"todos/bench"}'
  'list|list_todos|{"page_size":10}'
  'update_mask|update_todo|{"todo":{"name":"todos/bench","title":"renamed"},"update_mask":"title"}'
)
CONTACTS_CALLS=(
  'get|get_contact|{"name":"locations/1/contacts/bench"}'
  'update_ref|update_contact|{"contact":{"name":"locations/1/contacts/bench","home_location":"locations/1"},"update_mask":"home_location"}'
)

log() { printf '==> %s\n' "$*" >&2; }

build() {
  echo '{}' > "$NO_AUTH"
  log "building images"
  for app in $APPS; do
    podman build -q --authfile "$NO_AUTH" -f "examples/$app/Containerfile" -t "localhost/mcpbench-$app" "$ROOT" >/dev/null
    podman build -q --authfile "$NO_AUTH" -f "examples/$app/Containerfile" --build-arg "CMD=./cmd/$app-mcp" -t "localhost/mcpbench-$app-mcp" "$ROOT" >/dev/null
  done
  podman build -q --authfile "$NO_AUTH" -f bench/mcp/load/Containerfile -t localhost/mcpbench-load "$ROOT" >/dev/null
  if [[ " $VARIANTS " == *" nest "* ]]; then podman build -q --authfile "$NO_AUTH" -t localhost/mcpbench-nest "$ROOT/bench/mcp/nest-todo" >/dev/null; fi
}

# serve starts one server container with the benchmark's limits.
serve() {
  local name=$1; shift
  podman run -d --name "$name" --network "$NET" --cpus "$CPUS" --memory "$MEMORY" "$@" >/dev/null
}

# load runs the load driver in its own container, with its own CPUs.
load() {
  podman run --rm --network "$NET" --cpus "$LOAD_CPUS" localhost/mcpbench-load "$@"
}

# cpu prints a container's total CPU time in seconds.
cpu() {
  podman stats --no-stream --format json "$1" | python3 -c '
import json, re, sys
t = json.load(sys.stdin)[0]["cpu_time"]
units = {"ns": 1e-9, "µs": 1e-6, "us": 1e-6, "ms": 1e-3, "s": 1, "m": 60, "h": 3600}
print(sum(float(v) * units[u] for v, u in re.findall(r"([\d.]+)(ns|µs|us|ms|h|m|s)", t)))'
}

mem() { podman stats --no-stream --format '{{.MemUsage}}' "$1" | cut -d/ -f1 | tr -d ' '; }

wait_ready() {
  local url=$1
  for _ in $(seq 60); do
    if podman run --rm --network "$NET" docker.io/curlimages/curl:8.14.1 -s -o /dev/null --max-time 1 "$url" 2>/dev/null; then return; fi
    sleep 1
  done
  log "not ready: $url"; exit 1
}

stop_all() { podman rm -f mcpb-app mcpb-mcp >/dev/null 2>&1 || true; }

# diagnose says why a run failed: each server's state and its last logs.
diagnose() {
  for ct in "$@"; do
    log "$ct: $(podman inspect "$ct" --format 'state={{.State.Status}} exit={{.State.ExitCode}} oom_killed={{.State.OOMKilled}}' 2>&1)"
    podman logs --tail 15 "$ct" 2>&1 | sed 's/^/    /' >&2
  done
}

# start_servers starts one variant's containers from scratch and seeds the
# app server; it sets url, session and containers.
start_servers() {
  local app=$1 variant=$2
  stop_all
  session=()
  serve mcpb-app "localhost/mcpbench-$app"
  case $variant in
    nest)
      local tokens=reader,writer
      [ "$app" = contacts ] && tokens=loc1-reader,loc1-writer,loc2-writer,all-reader
      serve mcpb-mcp -e TODO_API_URL=http://mcpb-app:8080 -e CONTACTS_API_URL=http://mcpb-app:8080 \
        -e MCP_TOOLSETS="$app" -e MCP_SERVER_NAME="$app" -e MCP_AUTH_TOKENS="$tokens" \
        -e API_PORT=3100 -e RATE_LIMIT_ENABLED=false localhost/mcpbench-nest
      url=http://mcpb-mcp:3100/mcp; session=(-session); containers="mcpb-mcp mcpb-app" ;;
    loopback)
      serve mcpb-mcp "localhost/mcpbench-$app-mcp" -backend http://mcpb-app:8080
      url=http://mcpb-mcp:8080/mcp; containers="mcpb-mcp mcpb-app" ;;
    inprocess)
      url=http://mcpb-app:8080/mcp; containers="mcpb-app" ;;
  esac
  wait_ready "${url%/mcp}/"
  wait_ready http://mcpb-app:8080/
  # Test data goes in through the app server's own /mcp, which every
  # variant reads from.
  for i in "" $(seq 9); do
    case $app in
      todo) load -url http://mcpb-app:8080/mcp -token writer -once -tool create_todo \
        -args "{\"todo_id\":\"bench${i:+-$i}\",\"todo\":{\"title\":\"seed\"}}" ;;
      contacts) load -url http://mcpb-app:8080/mcp -token loc1-writer -once -tool create_contact \
        -args "{\"parent\":\"locations/1\",\"contact_id\":\"bench${i:+-$i}\",\"contact\":{\"display_name\":\"seed\"}}" ;;
    esac
  done
}

# run_variant measures every call of an app on one variant. Each call and
# client count gets fresh containers, so no measurement inherits another's
# memory: the NestJS server keeps MCP sessions in memory, and five runs in
# one container exhaust Node's default heap.
run_variant() {
  local app=$1 variant=$2 url session=() containers token calls
  case $app in
    todo) token=writer; calls=("${TODO_CALLS[@]}") ;;
    contacts) token=loc1-writer; calls=("${CONTACTS_CALLS[@]}") ;;
  esac
  for spec in "${calls[@]}"; do
    IFS='|' read -r name tool args <<<"$spec"
    for c in $CLIENTS; do
      start_servers "$app" "$variant"
      local before=() after=() i=0
      for ct in $containers; do before+=("$(cpu "$ct")"); done
      local line
      line=$(load -url "$url" -token "$token" ${session[@]+"${session[@]}"} -tool "$tool" -args "$args" \
        -c "$c" -warmup "$WARMUP" -d "$DURATION" -label "$app/$variant/$name") || { diagnose $containers; stop_all; exit 1; }
      for ct in $containers; do after+=("$(cpu "$ct")"); done
      local cpu_s=0 mems=""
      for ct in $containers; do
        cpu_s=$(python3 -c "print(${after[$i]} - ${before[$i]} + $cpu_s)"); i=$((i + 1))
        mems+="$ct=$(mem "$ct") "
      done
      python3 -c '
import json, sys
r = json.loads(sys.argv[1]); cpu = float(sys.argv[2])
total = r["calls"] * (1 + float(sys.argv[4]) / float(sys.argv[5]))
r["cpu_ms_per_call"] = round(cpu * 1000 / total, 4)
r["memory"] = sys.argv[3].strip()
print(json.dumps(r))' "$line" "$cpu_s" "$mems" "${WARMUP%s}" "${DURATION%s}" | tee -a "$OUT/results.jsonl"
      stop_all
    done
  done
}

cd "$ROOT"
podman network exists "$NET" || podman network create "$NET" >/dev/null
[ -n "${SKIP_BUILD:-}" ] || build
log "results: $OUT/results.jsonl (cpus=$CPUS memory=$MEMORY load_cpus=$LOAD_CPUS)"
for app in $APPS; do
  for v in $VARIANTS; do log "$app: $v"; run_variant "$app" "$v"; done
done
