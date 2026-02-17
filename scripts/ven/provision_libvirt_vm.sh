#!/usr/bin/env bash
set -euo pipefail

# Provision a libvirt-based Virtual Edge Node (vEN) VM and write `.ven.env`.
#
# This is intended to be used as:
#   EDGE_NODE_PROVIDER=ven VEN_BOOTSTRAP_CMD=./scripts/ven/provision_libvirt_vm.sh make test
#
# What it does:
#   1) Runs Terraform for the virtual-edge-node libvirt module (pico-vm-libvirt)
#   2) Extracts the VM SMBIOS UUID and writes it as NODEGUID into `.ven.env`
#   3) Optionally discovers the VM IP via `virsh domifaddr --source agent` and writes VEN_SSH_HOST
#
# What it does NOT do (yet):
#   - Onboard the vEN into cluster-orch / connect-gateway
#   - Guarantee connect-gateway reachability from the VM network

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

if [[ "${EDGE_NODE_PROVIDER:-}" != "ven" ]]; then
  echo "ERROR: scripts/ven/provision_libvirt_vm.sh expects EDGE_NODE_PROVIDER=ven" >&2
  exit 2
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: missing required command: $1" >&2
    exit 2
  fi
}

need_cmd terraform
need_cmd virsh
need_cmd jq
need_cmd rsync

# If user didn't specify a module dir, try a sensible default from edge-manage-test-automation.
VEN_MODULE_DIR="${VEN_MODULE_DIR:-}"
if [[ -z "$VEN_MODULE_DIR" ]]; then
  if [[ -d "/root/edge-manage-test-automation/repos/ven/pico/modules/pico-vm-libvirt" ]]; then
    VEN_MODULE_DIR="/root/edge-manage-test-automation/repos/ven/pico/modules/pico-vm-libvirt"
  fi
fi

if [[ -z "$VEN_MODULE_DIR" || ! -d "$VEN_MODULE_DIR" ]]; then
  cat >&2 <<'EOM'
ERROR: VEN_MODULE_DIR is not set (or is not a directory).

Point it at the virtual-edge-node Terraform module directory, e.g.:
  export VEN_MODULE_DIR=/root/edge-manage-test-automation/repos/ven/pico/modules/pico-vm-libvirt
EOM
  exit 2
fi

modules_dir="$(cd "$(dirname "$VEN_MODULE_DIR")" && pwd)"
if [[ ! -d "$modules_dir/common" ]]; then
  echo "ERROR: expected sibling module directory not found: $modules_dir/common" >&2
  exit 2
fi

# Terraform variables (mostly matching edge-manage-test-automation defaults)
VEN_VM_NAME="${VEN_VM_NAME:-pico-node-libvirt}"
VEN_CPU_CORES="${VEN_CPU_CORES:-8}"
VEN_MEMORY_MB="${VEN_MEMORY_MB:-8192}"
VEN_DISK_SIZE_GB="${VEN_DISK_SIZE_GB:-128}"
VEN_LIBVIRT_POOL_NAME="${VEN_LIBVIRT_POOL_NAME:-default}"
VEN_LIBVIRT_NETWORK_NAME="${VEN_LIBVIRT_NETWORK_NAME:-default}"
VEN_VM_CONSOLE="${VEN_VM_CONSOLE:-pty}"
VEN_TINKERBELL_HAPROXY_DOMAIN="${VEN_TINKERBELL_HAPROXY_DOMAIN:-}" # may be empty
VEN_SMBIOS_SERIAL="${VEN_SMBIOS_SERIAL:-}"                         # optional
VEN_SMBIOS_UUID="${VEN_SMBIOS_UUID:-}"                             # optional

workdir="${VEN_TERRAFORM_WORKDIR:-}"
if [[ -z "$workdir" ]]; then
  workdir="/tmp/cluster-tests-ven-tf-${VEN_VM_NAME}-$$"
fi

mkdir -p "$workdir/modules"

# Copy the module(s) into a temp dir so we don't dirty external checkouts.
rsync -a --delete "$modules_dir/common" "$workdir/modules/"
rsync -a --delete "$VEN_MODULE_DIR" "$workdir/modules/"

module_workdir="$workdir/modules/$(basename "$VEN_MODULE_DIR")"

cat >"$module_workdir/terraform.tfvars" <<EOF
vm_name               = "${VEN_VM_NAME}"
cpu_cores             = ${VEN_CPU_CORES}
memory                = ${VEN_MEMORY_MB}
disk_size             = ${VEN_DISK_SIZE_GB}
libvirt_pool_name      = "${VEN_LIBVIRT_POOL_NAME}"
libvirt_network_name   = "${VEN_LIBVIRT_NETWORK_NAME}"
vm_console             = "${VEN_VM_CONSOLE}"
tinkerbell_haproxy_domain = "${VEN_TINKERBELL_HAPROXY_DOMAIN}"
smbios_serial          = "${VEN_SMBIOS_SERIAL}"
smbios_uuid            = "${VEN_SMBIOS_UUID}"
EOF

echo "Terraform workdir: $module_workdir" >&2

# Basic libvirt sanity check
if ! virsh list --all >/dev/null 2>&1; then
  echo "ERROR: libvirt does not appear to be available (virsh list failed). Is libvirtd running and are you in the libvirt group?" >&2
  exit 2
fi

terraform -chdir="$module_workdir" init -upgrade >&2
terraform -chdir="$module_workdir" apply -auto-approve >&2

vm_json="$(terraform -chdir="$module_workdir" output -json vm_name_and_serial)"
vm_name="$(jq -r '.name' <<<"$vm_json")"
vm_uuid="$(jq -r '.uuid' <<<"$vm_json")"

if [[ -z "$vm_uuid" || "$vm_uuid" == "null" ]]; then
  echo "ERROR: failed to determine VM UUID from terraform output" >&2
  echo "$vm_json" >&2
  exit 2
fi

# Try to discover an IP address via qemu-guest-agent.
VEN_SSH_HOST=""
if [[ "${VEN_DISCOVER_IP:-true}" == "true" ]]; then
  echo "Attempting to discover VM IP via virsh domifaddr (requires qemu-guest-agent)..." >&2
  deadline=$((SECONDS + 120))
  while [[ $SECONDS -lt $deadline ]]; do
    ip="$(virsh domifaddr "$vm_name" --source agent --interface --full 2>/dev/null | grep -oP '(\d{1,3}\.){3}\d{1,3}' | head -n1 || true)"
    if [[ -n "$ip" ]]; then
      VEN_SSH_HOST="$ip"
      break
    fi
    sleep 2
  done

  if [[ -n "$VEN_SSH_HOST" ]]; then
    echo "Discovered VM IP: $VEN_SSH_HOST" >&2
  else
    echo "NOTE: could not discover VM IP (no domifaddr output). Continuing without VEN_SSH_HOST." >&2
  fi
fi

# Write .ven.env for Makefile targets to source.
{
  echo "# Generated by scripts/ven/provision_libvirt_vm.sh"
  echo "export NODEGUID=\"${vm_uuid}\""
  echo "export VEN_VM_NAME=\"${vm_name}\""
  if [[ -n "$VEN_SSH_HOST" ]]; then
    echo "export VEN_SSH_HOST=\"${VEN_SSH_HOST}\""
  fi
} > .ven.env
chmod 0600 .ven.env

echo "Wrote $(pwd)/.ven.env" >&2
echo "NODEGUID=$vm_uuid" >&2

if [[ "${VEN_TERRAFORM_KEEP:-false}" != "true" ]]; then
  # Keep state by default? For now, keep the workdir so users can `terraform destroy`.
  echo "NOTE: Terraform workdir retained at $workdir (set VEN_TERRAFORM_KEEP=false to silence; cleanup manually when done)" >&2
fi
