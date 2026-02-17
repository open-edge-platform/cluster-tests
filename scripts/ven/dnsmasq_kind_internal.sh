#!/usr/bin/env bash
set -euo pipefail

# Minimal dnsmasq setup for resolving *.kind.internal to an IP reachable from a libvirt VM.
#
# This is intentionally simpler than the EMF script: cluster-tests only needs connect-gateway
# (and optionally cluster-manager) to be reachable from the vEN VM.
#
# WARNING: this script modifies system DNS services and requires sudo.
# Prefer running inside disposable CI runners.
#
# Usage:
#   sudo ./scripts/ven/dnsmasq_kind_internal.sh setup
#   sudo ./scripts/ven/dnsmasq_kind_internal.sh config
#
# Env:
#   KIND_INTERNAL_FQDN_SUFFIX (default: kind.internal)
#   KIND_INTERNAL_TARGET_IP   (default: IP of interface with 10.* address, else first non-loopback IPv4)

ACTION="${1:-}"

KIND_INTERNAL_FQDN_SUFFIX="${KIND_INTERNAL_FQDN_SUFFIX:-kind.internal}"
KIND_INTERNAL_TARGET_IP="${KIND_INTERNAL_TARGET_IP:-}"

pick_ip() {
  # Prefer a 10.* interface (common in GitHub runners); else pick first non-loopback IPv4.
  local ip
  ip=$(ip -o -4 addr show | awk '$4 ~ /^10\./ {print $4}' | head -n1 | cut -d/ -f1 || true)
  if [[ -z "$ip" ]]; then
    ip=$(ip -o -4 addr show scope global | awk 'NR==1{print $4}' | cut -d/ -f1 || true)
  fi
  echo "$ip"
}

if [[ -z "$KIND_INTERNAL_TARGET_IP" ]]; then
  KIND_INTERNAL_TARGET_IP="$(pick_ip)"
fi

if [[ -z "$KIND_INTERNAL_TARGET_IP" ]]; then
  echo "ERROR: could not determine KIND_INTERNAL_TARGET_IP; set it explicitly" >&2
  exit 2
fi

setup_dns() {
  apt-get update -y
  apt-get install -y dnsmasq

  # Back up original config
  if [[ -f /etc/dnsmasq.conf && ! -f /etc/dnsmasq.conf.bak ]]; then
    cp /etc/dnsmasq.conf /etc/dnsmasq.conf.bak
  fi

  cat >/etc/dnsmasq.conf <<EOF
# Managed by cluster-tests scripts/ven/dnsmasq_kind_internal.sh
bind-interfaces
listen-address=127.0.0.1
# Forward everything else to upstream resolvers
server=8.8.8.8

# Resolve any *.kind.internal name to the chosen target IP
address=/${KIND_INTERNAL_FQDN_SUFFIX}/${KIND_INTERNAL_TARGET_IP}
EOF

  systemctl restart dnsmasq
  systemctl enable dnsmasq

  # Ensure the host uses dnsmasq
  if command -v resolvectl >/dev/null 2>&1; then
    true
  fi

  if [[ -L /etc/resolv.conf ]]; then
    rm -f /etc/resolv.conf
  fi
  cat >/etc/resolv.conf <<EOF
nameserver 127.0.0.1
options trust-ad
EOF
}

config_dns() {
  if [[ ! -f /etc/dnsmasq.conf ]]; then
    echo "ERROR: /etc/dnsmasq.conf not found; run setup first" >&2
    exit 2
  fi

  # Update just the kind.internal mapping.
  sed -i -E "s#^address=/${KIND_INTERNAL_FQDN_SUFFIX}/.*#address=/${KIND_INTERNAL_FQDN_SUFFIX}/${KIND_INTERNAL_TARGET_IP}#" /etc/dnsmasq.conf
  systemctl restart dnsmasq
}

case "$ACTION" in
  setup)
    setup_dns
    echo "Configured dnsmasq: *.${KIND_INTERNAL_FQDN_SUFFIX} -> ${KIND_INTERNAL_TARGET_IP}" >&2
    ;;
  config)
    config_dns
    echo "Updated dnsmasq: *.${KIND_INTERNAL_FQDN_SUFFIX} -> ${KIND_INTERNAL_TARGET_IP}" >&2
    ;;
  *)
    echo "Usage: $0 {setup|config}" >&2
    exit 2
    ;;
esac
