#!/usr/bin/env bash
set -euo pipefail

# Start long-lived port-forwards for services in the kind control-plane so a vEN VM
# (libvirt NAT network) can reach them via the host.
#
# This is a helper for vEN development. It is not wired into `make test` yet.
#
# Default ports are chosen to avoid clashing with test suites (8080/8081).
#
# Usage:
#   ./scripts/ven/portforward_kind_services.sh start
#   ./scripts/ven/portforward_kind_services.sh stop
#   ./scripts/ven/portforward_kind_services.sh status

ACTION="${1:-}"

PF_DIR="${VEN_PORTFORWARD_DIR:-/tmp/cluster-tests-ven-portforwards}"
CM_LOCAL_PORT="${VEN_CM_LOCAL_PORT:-18080}"
GW_LOCAL_PORT="${VEN_GW_LOCAL_PORT:-18081}"
ADDRESS="${VEN_PORTFORWARD_ADDRESS:-0.0.0.0}"

mkdir -p "$PF_DIR"

pidfile() { echo "$PF_DIR/$1.pid"; }
logfile() { echo "$PF_DIR/$1.log"; }

is_running() {
  local pf="$1"
  local pid
  pid="$(cat "$(pidfile "$pf")" 2>/dev/null || true)"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

start_one() {
  local name="$1"; shift
  local svc="$1"; shift
  local local_port="$1"; shift
  local remote_port="$1"; shift

  if is_running "$name"; then
    echo "$name already running (pid $(cat "$(pidfile "$name")"))" >&2
    return 0
  fi

  echo "Starting port-forward for $svc: $ADDRESS:$local_port -> :$remote_port" >&2
  # shellcheck disable=SC2086
  kubectl port-forward "$svc" "$local_port:$remote_port" --address "$ADDRESS" \
    >"$(logfile "$name")" 2>&1 &

  echo $! >"$(pidfile "$name")"
}

stop_one() {
  local name="$1"
  if ! is_running "$name"; then
    echo "$name not running" >&2
    rm -f "$(pidfile "$name")" || true
    return 0
  fi

  local pid
  pid="$(cat "$(pidfile "$name")")"
  echo "Stopping $name (pid $pid)" >&2
  kill "$pid" 2>/dev/null || true
  rm -f "$(pidfile "$name")" || true
}

case "$ACTION" in
  start)
    start_one cluster-manager svc/cluster-manager "$CM_LOCAL_PORT" 8080
    start_one connect-gateway svc/cluster-connect-gateway "$GW_LOCAL_PORT" 8080
    ;;
  stop)
    stop_one cluster-manager
    stop_one connect-gateway
    ;;
  status)
    for pf in cluster-manager connect-gateway; do
      if is_running "$pf"; then
        echo "$pf: running (pid $(cat "$(pidfile "$pf")"))"
      else
        echo "$pf: stopped"
      fi
    done
    echo "logs: $PF_DIR" >&2
    ;;
  *)
    echo "Usage: $0 {start|stop|status}" >&2
    exit 2
    ;;
esac
