#!/usr/bin/env bash
set -euo pipefail

# Bootstrap a libvirt VM and run edge-node-agents `cluster-agent` on it.
#
# Goal: provide a VEN-like external edge node (VM) that can register with the
# cluster-orch southbound API (intel-infra-provider-grpc) and then allow
# cluster-tests to create a K3s cluster against the registered GUID.
#
# This script is designed to be used as:
#   EDGE_NODE_PROVIDER=ven VEN_BOOTSTRAP_CMD=./scripts/ven/bootstrap_vm_cluster_agent.sh make test
#
# It will create `.ven.env` with at least:
#   - NODEGUID
#   - VEN_SSH_HOST / VEN_SSH_USER / VEN_SSH_KEY (for debugging)
#
# NOTE: This is a best-effort dev bootstrap. Depending on your environment, you may
# need to ensure libvirt/KVM is available and that the VM can reach the host.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

if [[ "${EDGE_NODE_PROVIDER:-}" != "ven" ]]; then
  echo "ERROR: expects EDGE_NODE_PROVIDER=ven" >&2
  exit 2
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: missing required command: $1" >&2
    exit 2
  fi
}

need_cmd kubectl
need_cmd jq
need_cmd ssh
need_cmd scp
need_cmd virsh
need_cmd virt-install
need_cmd cloud-localds
need_cmd qemu-img
need_cmd uuidgen

# Ports on the host that the VM will use to reach kind services (via port-forward).
HOST_SB_GRPC_PORT="${VEN_SB_LOCAL_PORT:-150020}"
HOST_IP_FOR_VM="${VEN_HOST_IP_FOR_VM:-192.168.122.1}"

# VM parameters
VEN_VM_NAME="${VEN_VM_NAME:-cluster-tests-ven}"
VEN_VM_CPU="${VEN_VM_CPU:-4}"
VEN_VM_MEM_MB="${VEN_VM_MEM_MB:-8192}"
VEN_VM_DISK_GB="${VEN_VM_DISK_GB:-40}"
VEN_VM_IMAGE_DIR="${VEN_VM_IMAGE_DIR:-/var/lib/libvirt/images}"
VEN_VM_NET="${VEN_VM_NET:-default}"

# SSH parameters
SSH_USER="${VEN_SSH_USER:-ven}"
SSH_PORT="${VEN_SSH_PORT:-22}"
SSH_KEY_DIR="${VEN_SSH_KEY_DIR:-/tmp/cluster-tests-ven-ssh}"
SSH_PRIV="$SSH_KEY_DIR/id_ed25519"
SSH_PUB="$SSH_KEY_DIR/id_ed25519.pub"

# Choose a GUID we will register with cluster-orch.
NODEGUID="${NODEGUID:-${VEN_NODEGUID:-}}"
if [[ -z "$NODEGUID" ]]; then
  NODEGUID="$(uuidgen)"
fi

# Start required port-forwards for the VM to reach the southbound API.
./scripts/ven/portforward_kind_services.sh start

# Ensure intel-infra-provider southbound allows missing auth clients for cluster-agent.
# This mirrors edge-node-agents/enic/add_env_var.sh but is required in vEN mode as we skip the in-kind cluster-agent component.
if kubectl -n default get deploy intel-infra-provider-southbound >/dev/null 2>&1; then
  kubectl get deployment intel-infra-provider-southbound -n default -o json | \
    jq --arg name "ALLOW_MISSING_AUTH_CLIENTS" --arg value "cluster-agent" '
      if (.spec.template.spec.containers[].env // [] | map(select(.name == $name)) | length) == 0 then
        .spec.template.spec.containers[].env += [{"name": $name, "value": $value}]
      else
        .
      end' | kubectl apply -f - >/dev/null
fi

mkdir -p "$SSH_KEY_DIR"
if [[ ! -f "$SSH_PRIV" ]]; then
  ssh-keygen -t ed25519 -N "" -f "$SSH_PRIV" >/dev/null
fi

mkdir -p "$VEN_VM_IMAGE_DIR"
base_img="$VEN_VM_IMAGE_DIR/${VEN_VM_NAME}-base.qcow2"
vm_img="$VEN_VM_IMAGE_DIR/${VEN_VM_NAME}.qcow2"
seed_img="$VEN_VM_IMAGE_DIR/${VEN_VM_NAME}-seed.img"

# Download Ubuntu cloud image if missing
UBUNTU_IMG_URL="${VEN_UBUNTU_IMG_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"
if [[ ! -f "$base_img" ]]; then
  echo "Downloading Ubuntu cloud image..." >&2
  curl -L "$UBUNTU_IMG_URL" -o "$base_img" >&2
fi

# Recreate VM disk
rm -f "$vm_img" "$seed_img"
qemu-img create -f qcow2 -F qcow2 -b "$base_img" "$vm_img" "$VEN_VM_DISK_GB"G >/dev/null

user_data="/tmp/${VEN_VM_NAME}-user-data.yaml"
meta_data="/tmp/${VEN_VM_NAME}-meta-data.yaml"

cat >"$user_data" <<EOF
#cloud-config
users:
  - default
  - name: ${SSH_USER}
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: sudo
    shell: /bin/bash
    ssh_authorized_keys:
      - $(cat "$SSH_PUB")
package_update: true
packages:
  - qemu-guest-agent
  - curl
  - jq
runcmd:
  - systemctl enable --now qemu-guest-agent
EOF

cat >"$meta_data" <<EOF
instance-id: ${VEN_VM_NAME}
local-hostname: ${VEN_VM_NAME}
EOF

cloud-localds "$seed_img" "$user_data" "$meta_data" >/dev/null

# Destroy any old VM
virsh destroy "$VEN_VM_NAME" >/dev/null 2>&1 || true
virsh undefine "$VEN_VM_NAME" --remove-all-storage >/dev/null 2>&1 || true

# Create the VM
virt-install \
  --name "$VEN_VM_NAME" \
  --memory "$VEN_VM_MEM_MB" \
  --vcpus "$VEN_VM_CPU" \
  --import \
  --disk path="$vm_img",format=qcow2 \
  --disk path="$seed_img",device=cdrom \
  --network network="$VEN_VM_NET" \
  --os-variant ubuntu24.04 \
  --noautoconsole \
  --uuid "$NODEGUID" \
  >/dev/null

# Discover VM IP (via guest agent, then DHCP leases)
VEN_SSH_HOST=""
deadline=$((SECONDS + 180))
while [[ $SECONDS -lt $deadline ]]; do
  ip="$(virsh domifaddr "$VEN_VM_NAME" --source agent --interface --full 2>/dev/null | grep -oP '(\d{1,3}\.){3}\d{1,3}' | head -n1 || true)"
  if [[ -n "$ip" ]]; then
    VEN_SSH_HOST="$ip"
    break
  fi
  ip="$(virsh net-dhcp-leases "$VEN_VM_NET" 2>/dev/null | grep "$VEN_VM_NAME" | awk '{print $5}' | head -n1 | cut -d/ -f1 || true)"
  if [[ -n "$ip" ]]; then
    VEN_SSH_HOST="$ip"
    break
  fi
  sleep 2
done

if [[ -z "$VEN_SSH_HOST" ]]; then
  echo "ERROR: failed to discover VM IP for SSH" >&2
  exit 2
fi

echo "VM is up: $VEN_VM_NAME @ $VEN_SSH_HOST" >&2

ssh_opts=(
  -i "$SSH_PRIV"
  -p "$SSH_PORT"
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=10
)

# Build cluster-agent binary locally
pushd _workspace/cluster-agent/cluster-agent >/dev/null
make cabuild >/dev/null
CA_BIN="$(pwd)/build/artifacts/cluster-agent"
popd >/dev/null

if [[ ! -f "$CA_BIN" ]]; then
  echo "ERROR: cluster-agent binary not found at $CA_BIN" >&2
  exit 2
fi

# Install cluster-agent on the VM and start it
scp "${ssh_opts[@]}" "$CA_BIN" "${SSH_USER}@${VEN_SSH_HOST}:/tmp/cluster-agent" >/dev/null

ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" -- bash -lc "
  set -euo pipefail
  sudo install -m 0755 /tmp/cluster-agent /usr/local/bin/cluster-agent
  sudo mkdir -p /etc/enic
  echo 'dummy-token' | sudo tee /etc/enic/access_token >/dev/null
  cat | sudo tee /etc/enic/cluster-agent.yaml >/dev/null <<CFG
version: 'v0.4.0'
GUID: '${NODEGUID}'
logLevel: 'debug'
metricsInterval: 10s
clusterOrchestratorURL: '${HOST_IP_FOR_VM}:${HOST_SB_GRPC_PORT}'
heartbeat: '10s'
jwt:
  accessTokenPath: '/etc/enic/access_token'
CFG

  cat | sudo tee /etc/systemd/system/cluster-agent.service >/dev/null <<UNIT
[Unit]
Description=cluster-agent (cluster-tests vEN bootstrap)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cluster-agent -config /etc/enic/cluster-agent.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

  sudo systemctl daemon-reload
  sudo systemctl enable --now cluster-agent
  sudo systemctl --no-pager status cluster-agent || true
" >/dev/null

# Write .ven.env for the test run
{
  echo "# Generated by scripts/ven/bootstrap_vm_cluster_agent.sh"
  echo "export NODEGUID=\"${NODEGUID}\""
  echo "export VEN_SSH_HOST=\"${VEN_SSH_HOST}\""
  echo "export VEN_SSH_USER=\"${SSH_USER}\""
  echo "export VEN_SSH_PORT=\"${SSH_PORT}\""
  echo "export VEN_SSH_KEY=\"${SSH_PRIV}\""
  echo "export VEN_VM_NAME=\"${VEN_VM_NAME}\""
  echo "export VEN_HOST_IP_FOR_VM=\"${HOST_IP_FOR_VM}\""
  echo "export VEN_SB_LOCAL_PORT=\"${HOST_SB_GRPC_PORT}\""
} > .ven.env
chmod 0600 .ven.env

echo "Wrote $(pwd)/.ven.env" >&2
