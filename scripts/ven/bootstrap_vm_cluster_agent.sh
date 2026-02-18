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

# Some operations (writing to /var/lib/libvirt/images, enabling libvirtd) require root.
# Use sudo when needed to keep the script usable in CI runners where the user is not root.
SUDO=""
if [[ "$(id -u)" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "ERROR: this script requires root privileges (sudo not found and not running as root)" >&2
    exit 2
  fi
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: missing required command: $1" >&2
    exit 2
  fi
}

maybe_install() {
  # Best-effort install for Ubuntu runners.
  # This script is typically used in ephemeral CI runners where sudo is available.
  local pkgs=("$@")
  if ! command -v apt-get >/dev/null 2>&1; then
    return 0
  fi
  if [[ ${#pkgs[@]} -eq 0 ]]; then
    return 0
  fi
  echo "Installing packages: ${pkgs[*]}" >&2
  sudo apt-get update -y >&2
  sudo apt-get install -y --no-install-recommends "${pkgs[@]}" >&2
}

need_cmd kubectl
need_cmd jq
need_cmd ssh
need_cmd scp
need_cmd uuidgen

need_cmd go

# Optional proxy support (required in environments where the VM has no direct egress).
#
# How it works:
# - Read proxy vars from the current environment and/or a local env file.
# - Copy them to the VM as /etc/cluster-tests/proxy.env (0600).
# - Configure systemd drop-ins so k3s (and cluster-agent) inherit the proxy.
#
# IMPORTANT: do not print proxy values; CI should store them as masked secrets.
VEN_PROXY_ENV_FILE="${VEN_PROXY_ENV_FILE:-${PROXY_ENV_FILE:-}}"

read_env_file_var() {
  # Read KEY=value from a file without eval. Returns empty if not present.
  local file="$1" key="$2"
  [[ -n "$file" && -f "$file" ]] || return 0
  # Take the last match to allow overrides later in the file.
  sed -n -E "s/^[[:space:]]*${key}[[:space:]]*=[[:space:]]*(.*)[[:space:]]*$/\\1/p" "$file" | tail -n 1
}

append_no_proxy() {
  # Append an entry to NO_PROXY if it is not already present.
  local entry="$1"
  if [[ -z "${NO_PROXY:-}" ]]; then
    NO_PROXY="$entry"
    export NO_PROXY
    return 0
  fi

  # Delimiter-aware contains check.
  case ",${NO_PROXY}," in
    *",${entry},"*) return 0 ;;
    *) NO_PROXY="${NO_PROXY},${entry}"; export NO_PROXY ;;
  esac
}

maybe_prepare_proxy_env() {
  # Populate HTTP_PROXY/HTTPS_PROXY/NO_PROXY from VEN_PROXY_ENV_FILE if provided.
  if [[ -n "$VEN_PROXY_ENV_FILE" && -f "$VEN_PROXY_ENV_FILE" ]]; then
    : "${HTTP_PROXY:=$(read_env_file_var "$VEN_PROXY_ENV_FILE" HTTP_PROXY)}"
    : "${HTTPS_PROXY:=$(read_env_file_var "$VEN_PROXY_ENV_FILE" HTTPS_PROXY)}"
    : "${NO_PROXY:=$(read_env_file_var "$VEN_PROXY_ENV_FILE" NO_PROXY)}"
    export HTTP_PROXY HTTPS_PROXY NO_PROXY
  fi

  # If a proxy is configured, ensure we bypass it for the VM<->host/libvirt and k8s internals.
  if [[ -n "${HTTP_PROXY:-}" || -n "${HTTPS_PROXY:-}" ]]; then
    append_no_proxy "localhost"
    append_no_proxy "127.0.0.1"
    append_no_proxy "${HOST_IP_FOR_VM}"
    append_no_proxy "192.168.122.0/24"
    append_no_proxy "10.42.0.0/16"
    append_no_proxy "10.43.0.0/16"
    append_no_proxy ".svc"
    append_no_proxy ".svc.cluster.local"
    append_no_proxy "cluster.local"
  fi
}

if ! command -v virsh >/dev/null 2>&1 || ! command -v virt-install >/dev/null 2>&1 || ! command -v cloud-localds >/dev/null 2>&1 || ! command -v qemu-img >/dev/null 2>&1; then
  maybe_install qemu-kvm libvirt-daemon-system libvirt-clients virtinst cloud-image-utils qemu-utils dnsmasq-base dnsmasq curl
  # Ensure libvirt is running.
  sudo systemctl enable --now libvirtd >/dev/null 2>&1 || true
fi

need_cmd virsh
need_cmd virt-install
need_cmd cloud-localds
need_cmd qemu-img

# libvirt NAT networks (including the default network) require dnsmasq.
if [[ ! -x /usr/sbin/dnsmasq ]]; then
  maybe_install dnsmasq-base dnsmasq
fi

# Ports on the host that the VM will use to reach kind services (via port-forward).
# NOTE: Must be a valid TCP port (<= 65535). The in-cluster service listens on 50020; we default
# the host port-forward to 15020 to avoid clashing with common local ports.
HOST_SB_GRPC_PORT="${VEN_SB_LOCAL_PORT:-15020}"
HOST_IP_FOR_VM="${VEN_HOST_IP_FOR_VM:-192.168.122.1}"
HOST_GW_PORT_FOR_VM="${VEN_GW_LOCAL_PORT:-18081}"

maybe_prepare_proxy_env

# NOTE: The cluster-agent implementation used by cluster-tests lives under
#   _workspace/edge-node-agents/cluster-agent
# and (in this repo version) uses insecure gRPC credentials by default. This is
# required for the kind-based test environment where the southbound service is
# exposed without TLS.

# VM parameters
VEN_VM_NAME="${VEN_VM_NAME:-cluster-tests-ven}"
VEN_VM_CPU="${VEN_VM_CPU:-4}"
VEN_VM_MEM_MB="${VEN_VM_MEM_MB:-8192}"
VEN_VM_DISK_GB="${VEN_VM_DISK_GB:-40}"
VEN_VM_IMAGE_DIR="${VEN_VM_IMAGE_DIR:-/var/lib/libvirt/images}"
VEN_VM_NET="${VEN_VM_NET:-default}"
VEN_REUSE_VM="${VEN_REUSE_VM:-false}"

# SSH parameters
SSH_USER="${VEN_SSH_USER:-ubuntu}"
SSH_PORT="${VEN_SSH_PORT:-22}"
SSH_KEY_DIR="${VEN_SSH_KEY_DIR:-/tmp/cluster-tests-ven-ssh}"
SSH_PRIV="$SSH_KEY_DIR/id_ed25519"
SSH_PUB="$SSH_KEY_DIR/id_ed25519.pub"

# Choose a GUID we will register with cluster-orch.
# IMPORTANT: The default test environment (and inventory stub) expects a stable,
# well-known Node GUID.
# Keep vEN behavior aligned with the in-kind ENiC flow unless explicitly overridden.
DEFAULT_NODEGUID="12345678-1234-1234-1234-123456789012"
NODEGUID="${VEN_NODEGUID:-${NODEGUID:-$DEFAULT_NODEGUID}}"

# Guardrail: bad ports are very hard to debug from inside the VM.
if [[ ! "$HOST_SB_GRPC_PORT" =~ ^[0-9]+$ ]] || (( HOST_SB_GRPC_PORT < 1 || HOST_SB_GRPC_PORT > 65535 )); then
  echo "ERROR: VEN_SB_LOCAL_PORT/HOST_SB_GRPC_PORT must be a valid TCP port (1-65535); got: $HOST_SB_GRPC_PORT" >&2
  exit 2
fi

# Start required port-forwards for the VM to reach kind services via the host.
# This script is commonly invoked after the kind cluster is recreated, so ensure
# we do not keep stale port-forward processes.
./scripts/ven/portforward_kind_services.sh stop || true
./scripts/ven/portforward_kind_services.sh start

# The southbound handler enforces JWT auth/RBAC. In this test environment we do not
# deploy a real Keycloak, so provide a mock OIDC discovery + JWKS endpoint that
# matches the default issuer used by the chart:
#   http://platform-keycloak.orch-platform.svc/realms/master
ensure_oidc_mock() {
  # Create namespace and apply the generated manifest.
  kubectl create namespace orch-platform --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1 || true

  # Generate and apply the manifest (uses vendored deps).
  GOFLAGS=-mod=vendor go run ./scripts/oidc_mock_gen -mode manifest | kubectl apply -f - >/dev/null

  # Wait for the mock deployment to be ready (best-effort).
  kubectl wait --for=condition=Available --timeout=120s deployment.apps/oidc-mock -n default >/dev/null 2>&1 || true
}

ensure_oidc_mock

if ! ./scripts/ven/portforward_kind_services.sh status 2>/dev/null | grep -q '^southbound-grpc: running'; then
  echo "ERROR: southbound-grpc port-forward is not running" >&2
  echo "Hint: check logs under ${VEN_PORTFORWARD_DIR:-/tmp/cluster-tests-ven-portforwards}" >&2
  ./scripts/ven/portforward_kind_services.sh status || true
  tail -n 50 "${VEN_PORTFORWARD_DIR:-/tmp/cluster-tests-ven-portforwards}/southbound-grpc.log" 2>/dev/null || true
  exit 2
fi

# Ensure intel-infra-provider southbound allows missing auth clients for cluster-agent.
# This mirrors edge-node-agents/enic/add_env_var.sh but is required in vEN mode as we skip the in-kind cluster-agent component.
echo "Ensuring intel-infra-provider-southbound allows unauthenticated cluster-agent..." >&2
for i in {1..10}; do
  if kubectl -n default get deploy intel-infra-provider-southbound >/dev/null 2>&1; then
    kubectl get deployment intel-infra-provider-southbound -n default -o json | \
      jq --arg name "ALLOW_MISSING_AUTH_CLIENTS" --arg value "cluster-agent" '
        (.spec.template.spec.containers[0].env // []) as $env |
        if ($env | map(select(.name == $name)) | length) == 0 then
          .spec.template.spec.containers[0].env += [{"name": $name, "value": $value}]
        else
          .
        end' | kubectl apply -f -
    if [[ $? -eq 0 ]]; then
      echo "Successfully patched intel-infra-provider-southbound." >&2
      kubectl rollout status deployment/intel-infra-provider-southbound -n default --timeout=60s || true
      break
    fi
  fi
  echo "Waiting for intel-infra-provider-southbound deployment (attempt $i)..." >&2
  sleep 5
done

mkdir -p "$SSH_KEY_DIR"
if [[ ! -f "$SSH_PRIV" ]]; then
  ssh-keygen -t ed25519 -N "" -f "$SSH_PRIV" >/dev/null
fi

$SUDO mkdir -p "$VEN_VM_IMAGE_DIR"
base_img="$VEN_VM_IMAGE_DIR/${VEN_VM_NAME}-base.qcow2"
vm_img="$VEN_VM_IMAGE_DIR/${VEN_VM_NAME}.qcow2"
seed_img="$VEN_VM_IMAGE_DIR/${VEN_VM_NAME}-seed.img"

vm_exists=false
if $SUDO virsh dominfo "$VEN_VM_NAME" >/dev/null 2>&1; then
  vm_exists=true
fi

if [[ "$VEN_REUSE_VM" != "true" ]]; then
  # Destroy any old VM first (and remove its storage) before we create new disk files.
  $SUDO virsh destroy "$VEN_VM_NAME" >/dev/null 2>&1 || true
  $SUDO virsh undefine "$VEN_VM_NAME" --remove-all-storage >/dev/null 2>&1 || true
  vm_exists=false
fi

if [[ "$vm_exists" != "true" ]]; then
  # Download Ubuntu cloud image if missing
  UBUNTU_IMG_URL="${VEN_UBUNTU_IMG_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"
  if [[ ! -f "$base_img" ]]; then
    echo "Downloading Ubuntu cloud image..." >&2
    $SUDO curl -L "$UBUNTU_IMG_URL" -o "$base_img" >&2
  fi

  # Recreate VM disk
  $SUDO rm -f "$vm_img" "$seed_img"
  $SUDO qemu-img create -f qcow2 -F qcow2 -b "$base_img" "$vm_img" "$VEN_VM_DISK_GB"G >/dev/null

  user_data="/tmp/${VEN_VM_NAME}-user-data.yaml"
  meta_data="/tmp/${VEN_VM_NAME}-meta-data.yaml"

  cat >"$user_data" <<EOF
#cloud-config
# Keep cloud-init minimal and fast: avoid apt operations here.
users:
  - name: ${SSH_USER}
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: sudo
    shell: /bin/bash
    ssh_authorized_keys:
      - $(cat "$SSH_PUB")
ssh_pwauth: false
disable_root: true
EOF

  cat >"$meta_data" <<EOF
instance-id: ${VEN_VM_NAME}
local-hostname: ${VEN_VM_NAME}
EOF

  $SUDO cloud-localds "$seed_img" "$user_data" "$meta_data" >/dev/null
fi

# Create the VM
if ! $SUDO virsh net-info "$VEN_VM_NET" >/dev/null 2>&1; then
  echo "ERROR: libvirt network '$VEN_VM_NET' not found" >&2
  $SUDO virsh net-list --all >&2 || true
  exit 2
fi

if ! $SUDO virsh net-info "$VEN_VM_NET" 2>/dev/null | awk -F': +' '/^Active:/ {print $2}' | grep -qi '^yes$'; then
  echo "Starting libvirt network: $VEN_VM_NET" >&2
  if ! start_out="$($SUDO virsh net-start "$VEN_VM_NET" 2>&1)"; then
    if echo "$start_out" | grep -qi 'already active'; then
      echo "libvirt network '$VEN_VM_NET' already active" >&2
    else
      echo "ERROR: failed to start libvirt network '$VEN_VM_NET': $start_out" >&2
      exit 2
    fi
  fi
  $SUDO virsh net-autostart "$VEN_VM_NET" >/dev/null 2>&1 || true
fi

if [[ "$vm_exists" != "true" ]]; then
  $SUDO virt-install \
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
else
  # Optional: Align NODEGUID to the existing domain UUID.
  # Default is false because the test environment stubs are keyed on NODEGUID.
  if [[ "${VEN_NODEGUID_FROM_DOMAIN_UUID:-false}" == "true" ]]; then
    existing_uuid="$($SUDO virsh dominfo "$VEN_VM_NAME" 2>/dev/null | awk -F': +' '/^UUID:/ {print $2}' | tr -d '\r' || true)"
    if [[ -n "$existing_uuid" ]]; then
      NODEGUID="$existing_uuid"
    fi
  fi
fi

port_open() {
  # Bash /dev/tcp check to avoid requiring netcat.
  # Returns 0 if TCP connection succeeds.
  local host="$1" port="$2"
  (echo >/dev/tcp/"$host"/"$port") >/dev/null 2>&1
}

wait_for_vm_ready() {
  # Wait for a VM to be ready for SSH:
  #  - libvirt domain is running
  #  - has a NIC MAC
  #  - has an IPv4 address (from libvirt lease info)
  #  - TCP SSH port is open
  local vm_name="$1"
  local network="$2"
  local wait_seconds="${3:-300}"
  local deadline=$((SECONDS + wait_seconds))
  local attempt=0

  echo "Waiting for VM to be fully ready for SSH..." >&2

  while [[ $SECONDS -lt $deadline ]]; do
    attempt=$((attempt + 1))

    if ! $SUDO virsh domstate "$vm_name" 2>/dev/null | grep -qi "running"; then
      if (( attempt % 6 == 0 )); then
        echo "  VM not running yet..." >&2
      fi
      sleep 5
      continue
    fi

    # Prefer domiflist over XML parsing.
    mac="$($SUDO virsh domiflist "$vm_name" 2>/dev/null | awk 'NR>2 && $1 != "" {print $5}' | head -n1 || true)"
    if [[ -z "${mac:-}" ]]; then
      if (( attempt % 6 == 0 )); then
        echo "  VM network interface not ready yet..." >&2
      fi
      sleep 5
      continue
    fi

    ip=""

    # Method A: domifaddr --source lease (does not require guest agent)
    ip="$($SUDO virsh domifaddr "$vm_name" --source lease 2>/dev/null | awk 'NR>2 && /ipv4/ {print $4}' | cut -d'/' -f1 | head -n1 || true)"

    # Method B: net-dhcp-leases by MAC
    if [[ -z "$ip" ]]; then
      ip="$($SUDO virsh net-dhcp-leases "$network" 2>/dev/null | awk -v m="$mac" 'tolower($3)==tolower(m) {print $5}' | cut -d'/' -f1 | head -n1 || true)"
    fi

    if [[ -z "$ip" || "$ip" == "127.0.0.1" ]]; then
      if (( attempt % 6 == 0 )); then
        echo "  VM IP not assigned yet..." >&2
      fi
      sleep 5
      continue
    fi

    if ! port_open "$ip" "$SSH_PORT"; then
      if (( attempt % 6 == 0 )); then
        echo "  VM IP $ip assigned but SSH port not open yet..." >&2
      fi
      sleep 5
      continue
    fi

    echo "VM is ready: $ip" >&2
    echo "$ip"
    return 0
  done

  echo "ERROR: VM failed to become ready within ${wait_seconds}s" >&2
  return 1
}

VEN_SSH_HOST="$(wait_for_vm_ready "$VEN_VM_NAME" "$VEN_VM_NET" "${VEN_VM_READY_WAIT_SECONDS:-300}")"

if [[ -z "$VEN_SSH_HOST" ]]; then
  echo "ERROR: failed to discover VM IP for SSH" >&2
  echo "Hint: check 'virsh net-dhcp-leases $VEN_VM_NET' and ensure the VM has booted." >&2
  exit 2
fi

echo "VM is up: $VEN_VM_NAME @ $VEN_SSH_HOST" >&2

ssh_opts=(
  -i "$SSH_PRIV"
  -p "$SSH_PORT"
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=10
  -o BatchMode=yes
  -o ConnectionAttempts=1
)

scp_opts=(
  -i "$SSH_PRIV"
  -P "$SSH_PORT"
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=10
  -o BatchMode=yes
)

# Wait for SSH to come up (cloud-init may still be running).
echo "Waiting for SSH to become ready..." >&2
wait_seconds="${VEN_SSH_WAIT_SECONDS:-600}"
deadline=$((SECONDS + wait_seconds))
attempt=0
last_reason=""
while [[ $SECONDS -lt $deadline ]]; do
  attempt=$((attempt + 1))

  if ! port_open "$VEN_SSH_HOST" "$SSH_PORT"; then
    last_reason="tcp"
  elif ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" true >/dev/null 2>&1; then
    break
  else
    last_reason="ssh"
  fi
  if (( attempt % 10 == 0 )); then
    if [[ "$last_reason" == "tcp" ]]; then
      echo "  still waiting for SSH (port ${SSH_PORT} not open on ${VEN_SSH_HOST})..." >&2
    else
      echo "  still waiting for SSH (auth/handshake not ready for ${SSH_USER}@${VEN_SSH_HOST})..." >&2
    fi
  fi
  sleep 2
done

if ! ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" true >/dev/null 2>&1; then
  echo "ERROR: SSH did not become ready for ${SSH_USER}@${VEN_SSH_HOST}" >&2
  exit 2
fi

# Best-effort: wait for cloud-init to finish initial setup (should be quick with minimal config).
echo "Waiting for cloud-init to complete (best-effort)..." >&2
ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" 'bash -se' <<'EOSSH' >/dev/null
set -euo pipefail
if command -v cloud-init >/dev/null 2>&1; then
  cloud-init status --wait || true
fi
EOSSH

# If a proxy is configured on the host, stage it to the VM so that apt/curl (and
# later k3s/containerd image pulls) can reach external endpoints.
if [[ -n "${HTTP_PROXY:-}" || -n "${HTTPS_PROXY:-}" ]]; then
  proxy_tmp="$(mktemp /tmp/cluster-tests-ven-proxy.XXXXXX)"
  (
    umask 077
    cat >"$proxy_tmp" <<EOF
HTTP_PROXY=${HTTP_PROXY:-}
HTTPS_PROXY=${HTTPS_PROXY:-}
NO_PROXY=${NO_PROXY:-}
http_proxy=${HTTP_PROXY:-}
https_proxy=${HTTPS_PROXY:-}
no_proxy=${NO_PROXY:-}
EOF
  )

  # Copy to a temp path first; the VM bootstrap will install it under /etc/cluster-tests/.
  scp "${scp_opts[@]}" "$proxy_tmp" "${SSH_USER}@${VEN_SSH_HOST}:/tmp/cluster-tests-proxy.env" >/dev/null
  rm -f "$proxy_tmp"
fi

# Ensure basic tools on the VM (curl/jq) for potential debugging.
ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" 'bash -se' <<'EOSSH' >/dev/null
set -euo pipefail

# If a proxy was staged by the host, install it and export for this session.
if [[ -f /tmp/cluster-tests-proxy.env ]]; then
  sudo mkdir -p /etc/cluster-tests
  sudo install -m 0600 -o root -g root /tmp/cluster-tests-proxy.env /etc/cluster-tests/proxy.env

  # Make k3s inherit proxy settings (affects containerd image pulls).
  sudo mkdir -p /etc/systemd/system/k3s.service.d
  sudo tee /etc/systemd/system/k3s.service.d/10-proxy.conf >/dev/null <<'CFG'
[Service]
EnvironmentFile=/etc/cluster-tests/proxy.env
CFG
  sudo systemctl daemon-reload
fi

if [[ -f /etc/cluster-tests/proxy.env ]]; then
  # proxy.env is root-only by design; run networked bootstrap steps under sudo.
  sudo bash -se <<'EOROOT'
set -euo pipefail
set -a
# shellcheck disable=SC1091
. /etc/cluster-tests/proxy.env
set +a

apt-get update -y
apt-get install -y --no-install-recommends curl jq ca-certificates

# Pre-stage k3s installer (required by cluster-agent install_cmd)
curl -L -o /opt/install.sh https://get.k3s.io
chmod +x /opt/install.sh
EOROOT
else
  sudo apt-get update -y
  sudo apt-get install -y --no-install-recommends curl jq ca-certificates

  # Pre-stage k3s installer (required by cluster-agent install_cmd)
  sudo curl -L -o /opt/install.sh https://get.k3s.io
  sudo chmod +x /opt/install.sh
fi
EOSSH

# The baseline k3s template is configured as air-gapped and the install command
# explicitly sets INSTALL_K3S_SKIP_DOWNLOAD=true.
#
# That means the VM must already have:
#   - /usr/local/bin/k3s
#   - /var/lib/rancher/k3s/agent/images/k3s-airgap-images-<arch>.tar
#
# Otherwise cluster-agent will fail with:
#   "Executable k3s binary not found at /usr/local/bin/k3s"
K3S_VERSION_DEFAULT="$(jq -r '.kubernetesVersion // empty' "$repo_root/configs/baseline-cluster-template-k3s.json" 2>/dev/null || true)"
if [[ -z "$K3S_VERSION_DEFAULT" || "$K3S_VERSION_DEFAULT" == "null" ]]; then
  K3S_VERSION_DEFAULT="v1.32.4+k3s1"
fi
K3S_VERSION="${VEN_K3S_VERSION:-$K3S_VERSION_DEFAULT}"

VEN_ARCH_RAW="$(ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" 'uname -m' 2>/dev/null | tr -d '\r' || true)"
case "$VEN_ARCH_RAW" in
  x86_64|amd64)
    VEN_ARCH=amd64
    ;;
  aarch64|arm64)
    VEN_ARCH=arm64
    ;;
  *)
    echo "WARN: unknown VM arch '$VEN_ARCH_RAW'; defaulting to amd64 for k3s assets" >&2
    VEN_ARCH=amd64
    ;;
esac

k3s_cache_dir="/tmp/cluster-tests-k3s-cache/${K3S_VERSION}/${VEN_ARCH}"
mkdir -p "$k3s_cache_dir"

k3s_bin="$k3s_cache_dir/k3s"
k3s_images="$k3s_cache_dir/k3s-airgap-images-${VEN_ARCH}.tar"

if [[ ! -s "$k3s_bin" ]]; then
  curl -fsSL -o "$k3s_bin" "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s"
  chmod 0755 "$k3s_bin"
fi
if [[ ! -s "$k3s_images" ]]; then
  curl -fsSL -o "$k3s_images" "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s-airgap-images-${VEN_ARCH}.tar"
fi

scp "${scp_opts[@]}" "$k3s_bin" "${SSH_USER}@${VEN_SSH_HOST}:/tmp/k3s" >/dev/null
scp "${scp_opts[@]}" "$k3s_images" "${SSH_USER}@${VEN_SSH_HOST}:/tmp/k3s-airgap-images-${VEN_ARCH}.tar" >/dev/null

ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" 'bash -se' <<EOSSH >/dev/null
set -euo pipefail

sudo install -d -m 0755 /usr/local/bin
sudo install -m 0755 /tmp/k3s /usr/local/bin/k3s

sudo install -d -m 0755 /var/lib/rancher/k3s/agent/images
sudo install -m 0644 "/tmp/k3s-airgap-images-${VEN_ARCH}.tar" "/var/lib/rancher/k3s/agent/images/k3s-airgap-images-${VEN_ARCH}.tar"
EOSSH

# Option 1 (preferred for vEN): patch connect-agent to use a gateway URL that is
# reachable from the VM network (host port-forward), instead of a management
# cluster internal .svc DNS name.
#
# The connect-agent static manifest is written by k3s to:
#   /var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml
# after the cluster is created.
#
# We install a systemd path+service that:
# - rewrites --gateway-url to ws://$HOST_IP_FOR_VM:$HOST_GW_PORT_FOR_VM/connect
# - bounces the mirror pod so kubelet applies the updated manifest
#
# NOTE: This block intentionally quotes the outer heredoc delimiter to avoid the
# host shell expanding any $-expressions that are meant to be written literally
# into the VM-side patch script.
desired_gateway_url_for_vm="ws://${HOST_IP_FOR_VM}:${HOST_GW_PORT_FOR_VM}/connect"
ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" "CONNECT_AGENT_GATEWAY_URL=${desired_gateway_url_for_vm} bash -se" <<'EOSSH' >/dev/null
set -euo pipefail

sudo mkdir -p /etc/cluster-tests
sudo tee /usr/local/bin/cluster-tests-patch-connect-agent-gateway >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

target="/var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml"
desired_gateway_url="${1:?missing desired gateway url (e.g. ws://192.168.122.1:18081/connect)}"

if [[ ! -f "$target" ]]; then
  exit 0
fi

if ! grep -q -- "--gateway-url=" "$target"; then
  exit 0
fi

current="$(sed -n -E 's/^[[:space:]]*- "--gateway-url=([^"]+)"[[:space:]]*$/\1/p' "$target" | head -n1 || true)"
if [[ "$current" == "$desired_gateway_url" ]]; then
  exit 0
fi

backup_dir="/var/log/cluster-tests/static-pod-backups"
mkdir -p "$backup_dir"
cp -a "$target" "$backup_dir/connect-agent.yaml.$(date +%s).bak" || true

# Replace any existing ws://.../connect or wss://.../connect form.
sed -i -E "s#(--gateway-url=)(wss?://)[^\"]+/connect#\\1${desired_gateway_url}#g" "$target"

# Prefer bouncing just the mirror pod; fall back to restarting k3s.
if command -v k3s >/dev/null 2>&1; then
  # Mirror pod name is <staticPodName>-<nodeName>. Static pod name is connect-agent.
  node_name="$(hostname -s 2>/dev/null || hostname)"
  k3s kubectl -n kube-system delete pod "connect-agent-${node_name}" --ignore-not-found=true >/dev/null 2>&1 || true
else
  if command -v systemctl >/dev/null 2>&1; then
    systemctl restart k3s >/dev/null 2>&1 || true
  fi
fi
SCRIPT
sudo chmod 0755 /usr/local/bin/cluster-tests-patch-connect-agent-gateway

sudo tee /etc/cluster-tests/connect-agent-gateway.env >/dev/null <<EOF
CONNECT_AGENT_GATEWAY_URL=${CONNECT_AGENT_GATEWAY_URL}
EOF
sudo chmod 0600 /etc/cluster-tests/connect-agent-gateway.env

sudo tee /etc/systemd/system/cluster-tests-connect-agent-gateway-patch.service >/dev/null <<'UNIT'
[Unit]
Description=cluster-tests: patch connect-agent gateway-url (vEN)
# The connect-agent manifest may be written/rewritten multiple times in quick
# succession during bootstrap; disable start limiting so the patch remains
# effective (the patch script is idempotent).
StartLimitIntervalSec=0
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=/etc/cluster-tests/connect-agent-gateway.env
# systemd does not do shell expansion for ExecStart arguments. Use a shell so
# CONNECT_AGENT_GATEWAY_URL is expanded at runtime.
ExecStart=/usr/bin/env bash -lc '/usr/local/bin/cluster-tests-patch-connect-agent-gateway "${CONNECT_AGENT_GATEWAY_URL}"'
UNIT

sudo tee /etc/systemd/system/cluster-tests-connect-agent-gateway-patch.path >/dev/null <<'UNIT'
[Unit]
Description=cluster-tests: watch connect-agent static manifest (vEN)

[Path]
PathExists=/var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml
PathChanged=/var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml
Unit=cluster-tests-connect-agent-gateway-patch.service

[Install]
WantedBy=multi-user.target
UNIT

sudo systemctl daemon-reload
sudo systemctl enable --now cluster-tests-connect-agent-gateway-patch.path

# Run once immediately (best-effort).
sudo systemctl start cluster-tests-connect-agent-gateway-patch.service >/dev/null 2>&1 || true
EOSSH

# Build cluster-agent binary locally.
#
# vEN mode MUST use edge-node-agents `cluster-agent` sources (not the legacy in-repo copy).
# Source location:
#   _workspace/edge-node-agents/cluster-agent
CA_DIR="${CA_DIR:-}"
if [[ -z "$CA_DIR" ]]; then
  CA_DIR="$repo_root/_workspace/edge-node-agents/cluster-agent"
fi

LEGACY_CA_DIR="$repo_root/_workspace/cluster-agent/cluster-agent"
if [[ "$CA_DIR" == "$LEGACY_CA_DIR" ]]; then
  echo "ERROR: legacy cluster-agent is not supported in vEN mode." >&2
  echo "Refusing CA_DIR=$CA_DIR" >&2
  echo "Use: $repo_root/_workspace/edge-node-agents/cluster-agent" >&2
  exit 2
fi

if [[ ! -d "$CA_DIR" ]]; then
  echo "ERROR: cluster-agent sources not found at: $CA_DIR" >&2
  echo "Expected: $repo_root/_workspace/edge-node-agents/cluster-agent" >&2
  echo "Hint: ensure mage test:bootstrap populates _workspace/ (see .test-dependencies.yaml), or set CA_DIR explicitly." >&2
  exit 2
fi

# vEN test environment note:
# The kind-based southbound gRPC endpoint exposed via host port-forward is plaintext.
# Upstream edge-node-agents `cluster-agent` uses TLS transport credentials by default.
#
# To avoid carrying a forked `cluster-agent` binary (and to satisfy "no legacy cluster-agent"),
# we patch the cloned sources in-place before building to support a runtime switch.
#
# - When CLUSTER_AGENT_INSECURE_GRPC=true, the agent will use plaintext gRPC.
# - Otherwise it will keep using TLS as upstream intended.
VEN_CLUSTER_AGENT_INSECURE_GRPC="${VEN_CLUSTER_AGENT_INSECURE_GRPC:-true}"
if [[ "$VEN_CLUSTER_AGENT_INSECURE_GRPC" == "true" ]]; then
  comms_file="$CA_DIR/internal/comms/comms.go"
  if [[ -f "$comms_file" ]] && ! grep -q 'CLUSTER_AGENT_INSECURE_GRPC' "$comms_file"; then
    # Verify the line we expect to patch is present.
    needle='	cli.Transport = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))'
    if ! grep -qF "$needle" "$comms_file"; then
      echo "ERROR: did not find expected TLS transport line to patch in comms.go;" >&2
      echo "       edge-node-agents cluster-agent source may have changed" >&2
      exit 2
    fi

    # Add missing imports (idempotent): os, strings, grpc/credentials/insecure.
    grep -qF '"os"' "$comms_file" || \
      sed -i 's|"fmt"|"fmt"\n\t"os"|' "$comms_file"
    grep -qF '"strings"' "$comms_file" || \
      sed -i 's|"os"|"os"\n\t"strings"|' "$comms_file"
    grep -qF 'grpc/credentials/insecure' "$comms_file" || \
      sed -i 's|"google.golang.org/grpc/credentials"|"google.golang.org/grpc/credentials"\n\t"google.golang.org/grpc/credentials/insecure"|' "$comms_file"

    # Replace the single TLS transport line with an insecure/TLS runtime switch.
    sed -i 's|\tcli\.Transport = grpc\.WithTransportCredentials(credentials\.NewTLS(tlsConfig))|\tif strings.EqualFold(os.Getenv("CLUSTER_AGENT_INSECURE_GRPC"), "true") {\n\t\tcli.Transport = grpc.WithTransportCredentials(insecure.NewCredentials())\n\t} else {\n\t\tcli.Transport = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))\n\t}|' "$comms_file"

    echo "Patched $comms_file for CLUSTER_AGENT_INSECURE_GRPC runtime switch"
  fi
fi

pushd "$CA_DIR" >/dev/null
make cabuild >/dev/null
CA_BIN="$(pwd)/build/artifacts/cluster-agent"
CA_VERSION="$(cat VERSION 2>/dev/null || echo '0.0.0')"
popd >/dev/null

if [[ ! -f "$CA_BIN" ]]; then
  echo "ERROR: cluster-agent binary not found at $CA_BIN" >&2
  exit 2
fi

# Install cluster-agent on the VM and start it
# Generate a valid JWT signed by the same runtime keypair used by the OIDC mock.
# Do NOT print the token.
ACCESS_TOKEN_FILE="$(mktemp /tmp/cluster-tests-cluster-agent-token.XXXXXX)"
chmod 0600 "$ACCESS_TOKEN_FILE"
GOFLAGS=-mod=vendor go run ./scripts/oidc_mock_gen -mode token -subject cluster-agent >"$ACCESS_TOKEN_FILE"

scp "${scp_opts[@]}" "$CA_BIN" "${SSH_USER}@${VEN_SSH_HOST}:/tmp/cluster-agent" >/dev/null
scp "${scp_opts[@]}" "$ACCESS_TOKEN_FILE" "${SSH_USER}@${VEN_SSH_HOST}:/tmp/cluster-agent-access-token" >/dev/null
rm -f "$ACCESS_TOKEN_FILE"

ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" 'bash -se' <<EOSSH >/dev/null
set -euo pipefail

sudo install -m 0755 /tmp/cluster-agent /usr/local/bin/cluster-agent
sudo mkdir -p /etc/enic
# Install a JWT token for the southbound handler RBAC/auth.
# If the token is missing for any reason, fall back to an empty file.
if [[ -s /tmp/cluster-agent-access-token ]]; then
  sudo install -m 0600 /tmp/cluster-agent-access-token /etc/enic/access_token
else
  sudo truncate -s 0 /etc/enic/access_token
fi

sudo tee /etc/enic/cluster-agent.yaml >/dev/null <<CFG
version: 'v${CA_VERSION}'
GUID: '${NODEGUID}'
logLevel: 'debug'
metricsInterval: 10s
clusterOrchestratorURL: '${HOST_IP_FOR_VM}:${HOST_SB_GRPC_PORT}'
statusEndpoint: 'unix:///run/node-agent/node-agent.sock'
heartbeat: '10s'
jwt:
  accessTokenPath: '/etc/enic/access_token'
CFG

sudo tee /etc/systemd/system/cluster-agent.service >/dev/null <<UNIT
[Unit]
Description=cluster-agent (cluster-tests vEN bootstrap)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=CLUSTER_AGENT_INSECURE_GRPC=${VEN_CLUSTER_AGENT_INSECURE_GRPC}
ExecStart=/usr/local/bin/cluster-agent -config /etc/enic/cluster-agent.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

# If the host staged a proxy env file, ensure cluster-agent also inherits it.
# (NO_PROXY is augmented on the host to avoid proxying VM<->host traffic.)
if [[ -f /etc/cluster-tests/proxy.env ]]; then
  sudo mkdir -p /etc/systemd/system/cluster-agent.service.d
  sudo tee /etc/systemd/system/cluster-agent.service.d/10-proxy.conf >/dev/null <<'CFG'
[Service]
EnvironmentFile=/etc/cluster-tests/proxy.env
CFG
fi

sudo systemctl daemon-reload
sudo systemctl enable cluster-agent
sudo systemctl restart cluster-agent
sudo systemctl --no-pager status cluster-agent || true
EOSSH

# Quick sanity check that cluster-agent process is running.
if ! ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" sudo systemctl is-active cluster-agent >/dev/null 2>&1; then
  echo "ERROR: cluster-agent service is not active on the VM" >&2
  ssh "${ssh_opts[@]}" "${SSH_USER}@${VEN_SSH_HOST}" sudo systemctl --no-pager status cluster-agent || true
  exit 2
fi

# Write .ven.env for the test run
{
  echo "# Generated by scripts/ven/bootstrap_vm_cluster_agent.sh"
  # NOTE: The intel-infra-provider inventory stub is pre-seeded for the default test
  # tenant UUID used by cluster-tests. Using an arbitrary UUID can cause reconciliation
  # to stall with: "no stubbed response for key: Create:<tenant>:workload".
  # Keep vEN behavior aligned with the in-kind ENiC flow by exporting the default.
  echo "export NAMESPACE=\"53cd37b9-66b2-4cc8-b080-3722ed7af64a\""
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
