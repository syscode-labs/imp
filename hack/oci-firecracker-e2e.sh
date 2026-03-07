#!/usr/bin/env bash
# oci-firecracker-e2e.sh - Provision one OCI VM and run an Imp Firecracker e2e smoke.
#
# Cleanup policy:
#   - If remote test exits 0: terminate OCI instance
#   - If remote test exits non-zero: keep OCI instance for debugging
#
# Required env:
#   OCI_SSH_PUBLIC_KEY_FILE  Path to SSH public key file
#   OCI_SSH_PRIVATE_KEY_FILE Path to SSH private key file
#
# Optional env:
#   IMP_OCI_PROFILE          Preferred profile selector (default: syscode-api)
#   OCI_PROFILE              OCI CLI profile to use (required unless OCI_CLI_PROFILE is set)
#   OCI_CLI_PROFILE          OCI CLI profile to use (alternative to OCI_PROFILE)
#   IMP_OCI_COMPARTMENT_NAME Preferred target compartment name (default: homelab)
#   IMP_OCI_DOMAIN_NAME      Preferred target domain name (default: homelab; metadata only)
#   OCI_COMPARTMENT_OCID     Compartment OCID for compute/network operations
#   OCI_AVAILABILITY_DOMAIN  Availability domain (auto-filled if golden-image builder is used)
#   OCI_SUBNET_OCID          Subnet OCID with public IP access (auto-filled if golden-image builder is used)
#   OCI_IMAGE_OCID           Image OCID for the VM (auto-built if missing)
#   OCI_SHAPE                Default: VM.Standard.E2.1.Micro
#   OCI_SSH_USER             Default: ubuntu
#   OCI_INSTANCE_NAME_PREFIX Default: imp-fc-e2e
#   OCI_ASSIGN_PUBLIC_IP     Default: true
#   OCI_WAIT_SECONDS         Default: 900
#   FIRECRACKER_VERSION      Default: v1.9.0
#   IMP_SOURCE_MODE          Default: local-tarball (only supported mode currently)
#   FREE_TIER_MAX_MICRO      Default: 2
#   ALLOW_PAID_SHAPE         Default: false (must be true to use non-free-tier shape)
#   OCI_AUTO_BUILD_GOLDEN_IMAGE Default: true (build minimal OCI custom image when OCI_IMAGE_OCID is missing)
#   OCI_GOLDEN_BUILD_DRIVER     Default: packer (packer|native-oci)
#
# Usage:
#   IMP_OCI_PROFILE=syscode-api IMP_OCI_COMPARTMENT_NAME=homelab IMP_OCI_DOMAIN_NAME=homelab \
#   OCI_COMPARTMENT_OCID=... OCI_AVAILABILITY_DOMAIN=... OCI_SUBNET_OCID=... \
#   OCI_IMAGE_OCID=... OCI_SSH_PUBLIC_KEY_FILE=~/.ssh/id_ed25519.pub \
#   OCI_SSH_PRIVATE_KEY_FILE=~/.ssh/id_ed25519 \
#   hack/oci-firecracker-e2e.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required env: $name" >&2
    exit 2
  fi
}

log() {
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*"
}

resolve_oci_profile() {
  OCI_PROFILE="${OCI_PROFILE:-${IMP_OCI_PROFILE:-}}"
  OCI_CLI_PROFILE="${OCI_CLI_PROFILE:-$OCI_PROFILE}"
  OCI_PROFILE="${OCI_PROFILE:-${OCI_CLI_PROFILE:-}}"
  if [[ -z "${OCI_PROFILE:-}" ]]; then
    echo "missing required OCI profile: set OCI_PROFILE or OCI_CLI_PROFILE" >&2
    exit 2
  fi
  export OCI_PROFILE
  export OCI_CLI_PROFILE="$OCI_PROFILE"
  export OCI_CLI_SUPPRESS_PROMPTS=true
}

resolve_oci_auth_mode() {
  local cfg profile_has_token
  if [[ -n "${OCI_CLI_AUTH:-}" ]]; then
    return 0
  fi
  cfg="${OCI_CLI_CONFIG_FILE:-$HOME/.oci/config}"
  profile_has_token="$(
    awk -v p="$OCI_PROFILE" '
      BEGIN { in_profile=0; has=0 }
      $0 ~ "^\\[" p "\\]$" { in_profile=1; next }
      $0 ~ "^\\[" && in_profile { in_profile=0 }
      in_profile && $0 ~ /^[[:space:]]*security_token_file[[:space:]]*=/ { has=1 }
      END { print has }
    ' "$cfg" 2>/dev/null || true
  )"
  if [[ "$profile_has_token" == "1" ]]; then
    export OCI_CLI_AUTH=security_token
  fi
}

resolve_compartment() {
  if [[ -n "${OCI_COMPARTMENT_OCID:-}" ]]; then
    return 0
  fi
  if [[ -z "${OCI_COMPARTMENT_NAME:-}" ]]; then
    return 0
  fi
  local by_name
  by_name="$(
    oci iam compartment list --all --compartment-id-in-subtree true --output json \
      | jq -r --arg n "$OCI_COMPARTMENT_NAME" '
        .data
        | map(select(."lifecycle-state"=="ACTIVE" and .name==$n))
        | .[0].id // empty
      '
  )"
  if [[ -z "${by_name:-}" ]]; then
    echo "could not resolve OCI_COMPARTMENT_OCID from OCI_COMPARTMENT_NAME=$OCI_COMPARTMENT_NAME" >&2
    exit 2
  fi
  OCI_COMPARTMENT_OCID="$by_name"
}

wait_for_running() {
  local instance_id="$1"
  local deadline="$2"

  while true; do
    local state
    state="$(oci compute instance get --instance-id "$instance_id" --query 'data."lifecycle-state"' --raw-output)"
    if [[ "$state" == "RUNNING" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for instance RUNNING (last state: $state)" >&2
      return 1
    fi
    sleep 5
  done
}

get_instance_public_ip() {
  local instance_id="$1"
  local vnic_attachment_id
  local vnic_id

  vnic_attachment_id="$(oci compute vnic-attachment list \
    --compartment-id "$OCI_COMPARTMENT_OCID" \
    --instance-id "$instance_id" \
    --query 'data[0].id' --raw-output)"

  vnic_id="$(oci compute vnic-attachment get \
    --vnic-attachment-id "$vnic_attachment_id" \
    --query 'data."vnic-id"' --raw-output)"

  oci network vnic get --vnic-id "$vnic_id" --query 'data."public-ip"' --raw-output
}

wait_for_ssh() {
  local host="$1"
  local deadline="$2"
  local tries=0

  while true; do
    if ssh -i "$OCI_SSH_PRIVATE_KEY_FILE" \
      -o BatchMode=yes \
      -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null \
      -o ConnectTimeout=5 \
      "${OCI_SSH_USER}@${host}" "echo ssh-ready" >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      echo "timed out waiting for SSH on $host" >&2
      return 1
    fi
    tries=$((tries + 1))
    if (( tries % 6 == 0 )); then
      log "Still waiting for SSH on $host"
    fi
    sleep 5
  done
}

terminate_instance() {
  local instance_id="$1"
  log "Terminating instance: $instance_id"
  oci compute instance terminate \
    --instance-id "$instance_id" \
    --force \
    --preserve-boot-volume false >/dev/null
}

ensure_image_ocid() {
  if [[ -n "${OCI_IMAGE_OCID:-}" ]]; then
    return 0
  fi
  if [[ "${OCI_AUTO_BUILD_GOLDEN_IMAGE:-true}" != "true" ]]; then
    echo "OCI_IMAGE_OCID is missing and OCI_AUTO_BUILD_GOLDEN_IMAGE=false" >&2
    exit 2
  fi
  local env_file
  env_file="$(mktemp /tmp/imp-golden-env-XXXXXX)"
  log "OCI_IMAGE_OCID missing; building minimal golden image first"
  local build_driver
  build_driver="${OCI_GOLDEN_BUILD_DRIVER:-packer}"

  case "$build_driver" in
    packer)
      OCI_COMPARTMENT_OCID="${OCI_COMPARTMENT_OCID:-}" \
      OCI_AVAILABILITY_DOMAIN="${OCI_AVAILABILITY_DOMAIN:-}" \
      OCI_SUBNET_OCID="${OCI_SUBNET_OCID:-}" \
      FIRECRACKER_VERSION="$FIRECRACKER_VERSION" \
      OCI_OUTPUT_ENV_FILE="$env_file" \
      "$REPO_ROOT/hack/packer-build-golden-image.sh"
      ;;
    native-oci)
      OCI_COMPARTMENT_OCID="${OCI_COMPARTMENT_OCID:-}" \
      OCI_AVAILABILITY_DOMAIN="${OCI_AVAILABILITY_DOMAIN:-}" \
      OCI_SUBNET_OCID="${OCI_SUBNET_OCID:-}" \
      OCI_SSH_PUBLIC_KEY_FILE="$OCI_SSH_PUBLIC_KEY_FILE" \
      OCI_SSH_PRIVATE_KEY_FILE="$OCI_SSH_PRIVATE_KEY_FILE" \
      FIRECRACKER_VERSION="$FIRECRACKER_VERSION" \
      OCI_OUTPUT_ENV_FILE="$env_file" \
      "$REPO_ROOT/hack/oci-build-golden-image.sh"
      ;;
    *)
      echo "unsupported OCI_GOLDEN_BUILD_DRIVER=$build_driver (expected: packer|native-oci)" >&2
      exit 2
      ;;
  esac

  # shellcheck disable=SC1090
  source "$env_file"
  rm -f "$env_file"

  if [[ -z "${OCI_IMAGE_OCID:-}" ]]; then
    echo "golden image build did not return OCI_IMAGE_OCID" >&2
    exit 2
  fi
  log "Using generated OCI_IMAGE_OCID=$OCI_IMAGE_OCID"
}

ensure_noninteractive_ssh_key() {
  if ssh-keygen -y -P "" -f "$OCI_SSH_PRIVATE_KEY_FILE" >/dev/null 2>&1; then
    return 0
  fi
  if [[ -n "${SSH_AUTH_SOCK:-}" ]]; then
    log "SSH private key appears passphrase-protected; relying on ssh-agent"
    return 0
  fi
  echo "SSH key requires passphrase and no ssh-agent is available." >&2
  echo "Use an unencrypted key for automation or load the key into ssh-agent first." >&2
  exit 2
}

enforce_free_tier_guardrails() {
  local current projected
  local max_micro="${FREE_TIER_MAX_MICRO:-2}"
  local allow_paid="${ALLOW_PAID_SHAPE:-false}"

  if [[ "$OCI_SHAPE" != "VM.Standard.E2.1.Micro" && "$allow_paid" != "true" ]]; then
    echo "Refusing non-free-tier shape ($OCI_SHAPE). Set ALLOW_PAID_SHAPE=true to override." >&2
    exit 2
  fi

  if [[ "$OCI_SHAPE" == "VM.Standard.E2.1.Micro" ]]; then
    current="$(
      oci compute instance list \
        --compartment-id "$OCI_COMPARTMENT_OCID" \
        --all \
        --query "length(data[?shape=='VM.Standard.E2.1.Micro' && \"lifecycle-state\"=='RUNNING'])" \
        --raw-output
    )"
    projected=$((current + 1))
    if (( projected > max_micro )); then
      echo "Free-tier guardrail: RUNNING micro instances would become $projected (limit $max_micro)." >&2
      echo "Stop/terminate another micro instance or raise FREE_TIER_MAX_MICRO explicitly." >&2
      exit 2
    fi
    log "Free-tier check: current micro RUNNING=$current, projected after launch=$projected, limit=$max_micro"
  fi
}

run_remote_smoke() {
  local host="$1"

  local ssh_opts=(
    -i "$OCI_SSH_PRIVATE_KEY_FILE"
    -o StrictHostKeyChecking=no
    -o UserKnownHostsFile=/dev/null
    -o ConnectTimeout=15
  )

  local bundle
  bundle="$(mktemp /tmp/imp-src-XXXXXX.tar.gz)"
  trap 'rm -f "${bundle:-}"' RETURN

  if [[ "${IMP_SOURCE_MODE:-local-tarball}" != "local-tarball" ]]; then
    echo "unsupported IMP_SOURCE_MODE=${IMP_SOURCE_MODE:-}; only local-tarball is implemented" >&2
    return 2
  fi

  log "Packing local repo"
  tar \
    --exclude='.git' \
    --exclude='bin' \
    --exclude='*.test' \
    -czf "$bundle" \
    -C "$REPO_ROOT" .

  log "Uploading source bundle"
  scp "${ssh_opts[@]}" "$bundle" "${OCI_SSH_USER}@${host}:/tmp/imp-src.tar.gz" >/dev/null

  log "Running remote Firecracker smoke"
  # shellcheck disable=SC2029
  ssh "${ssh_opts[@]}" "${OCI_SSH_USER}@${host}" \
    "FIRECRACKER_VERSION='${FIRECRACKER_VERSION}' bash -s" <<'REMOTE_EOF'
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

sudo apt-get update -y
sudo apt-get install -y curl ca-certificates jq git build-essential qemu-utils iptables tar

if ! command -v k3s >/dev/null 2>&1; then
  curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644
fi

export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Wait for at least one node object to exist and become Ready.
for _ in $(seq 1 120); do
  node_count="$(sudo k3s kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "$node_count" -ge 1 ]]; then
    if sudo k3s kubectl wait --for=condition=Ready node --all --timeout=60s >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 2
done
if ! sudo k3s kubectl wait --for=condition=Ready node --all --timeout=10s >/dev/null 2>&1; then
  echo "k3s node did not become Ready in time" >&2
  sudo k3s kubectl get nodes -o wide || true
  exit 20
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) FC_ARCH="x86_64"; KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin" ;;
  aarch64|arm64) FC_ARCH="aarch64"; KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/aarch64/kernels/vmlinux.bin" ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 3 ;;
esac

if ! command -v firecracker >/dev/null 2>&1; then
  TMPD="$(mktemp -d)"
  TARBALL_URL="https://github.com/firecracker-microvm/firecracker/releases/download/${FIRECRACKER_VERSION}/firecracker-${FIRECRACKER_VERSION}-${FC_ARCH}.tgz"
  curl -fL "$TARBALL_URL" -o "$TMPD/fc.tgz"
  tar -xzf "$TMPD/fc.tgz" -C "$TMPD"
  FC_BIN_PATH="$(find "$TMPD" -type f -name 'firecracker*' | head -n1)"
  sudo install -m 0755 "$FC_BIN_PATH" /usr/local/bin/firecracker
  rm -rf "$TMPD"
fi

sudo mkdir -p /var/lib/imp /run/imp/sockets /var/lib/imp/images /opt/imp
if [[ ! -f /var/lib/imp/vmlinux ]]; then
  curl -fL "$KERNEL_URL" -o /tmp/vmlinux.bin
  sudo mv /tmp/vmlinux.bin /var/lib/imp/vmlinux
  sudo chmod 0644 /var/lib/imp/vmlinux
fi

mkdir -p /tmp/imp-src
rm -rf /tmp/imp-src/*
tar -xzf /tmp/imp-src.tar.gz -C /tmp/imp-src
cd /tmp/imp-src

required_go="$(awk '/^go /{print $2; exit}' go.mod)"
if [[ -z "${required_go:-}" ]]; then
  echo "could not parse required Go version from go.mod" >&2
  exit 21
fi

need_go_install="true"
if command -v go >/dev/null 2>&1; then
  current_go="$(go version | awk '{print $3}' | sed 's/^go//')"
  if [[ "$current_go" == "$required_go" ]]; then
    need_go_install="false"
  fi
fi

if [[ "$need_go_install" == "true" ]]; then
  case "$(uname -m)" in
    x86_64) GOARCH_DL="amd64" ;;
    aarch64|arm64) GOARCH_DL="arm64" ;;
    *) echo "unsupported arch for Go install: $(uname -m)" >&2; exit 22 ;;
  esac
  go_tgz="go${required_go}.linux-${GOARCH_DL}.tar.gz"
  curl -fL "https://go.dev/dl/${go_tgz}" -o "/tmp/${go_tgz}"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "/tmp/${go_tgz}"
fi
export PATH="/usr/local/go/bin:${PATH}"

go build -o ./bin/operator ./cmd/operator
go build -o ./bin/agent ./cmd/agent
go build -o ./bin/guest-agent ./cmd/guest-agent
sudo install -m 0755 ./bin/guest-agent /opt/imp/guest-agent

sudo k3s kubectl apply -f ./config/crd/bases

NODE_NAME="$(sudo k3s kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"

# Restart fresh processes if rerun.
sudo pkill -f '/tmp/imp-src/bin/operator' || true
sudo pkill -f '/tmp/imp-src/bin/agent' || true

sudo -E env KUBECONFIG="$KUBECONFIG" /tmp/imp-src/bin/operator >/tmp/imp-operator.log 2>&1 &
echo $! | sudo tee /tmp/imp-operator.pid >/dev/null

sudo -E env \
  KUBECONFIG="$KUBECONFIG" \
  NODE_NAME="$NODE_NAME" \
  FC_BIN="$(command -v firecracker)" \
  FC_KERNEL="/var/lib/imp/vmlinux" \
  FC_SOCK_DIR="/run/imp/sockets" \
  IMP_IMAGE_CACHE="/var/lib/imp/images" \
  /tmp/imp-src/bin/agent >/tmp/imp-agent.log 2>&1 &
echo $! | sudo tee /tmp/imp-agent.pid >/dev/null

sudo k3s kubectl create namespace imp-smoke --dry-run=client -o yaml | sudo k3s kubectl apply -f -

cat <<'YAML' | sudo k3s kubectl apply -f -
apiVersion: imp.dev/v1alpha1
kind: ImpVMClass
metadata:
  name: oci-smoke-class
spec:
  vcpu: 1
  memoryMiB: 256
  diskGiB: 2
---
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: oci-smoke-vm
  namespace: imp-smoke
spec:
  classRef:
    name: oci-smoke-class
  image: nginx:alpine
  lifecycle: persistent
YAML

end=$((SECONDS + 300))
phase=""
while (( SECONDS < end )); do
  phase="$(sudo k3s kubectl -n imp-smoke get impvm oci-smoke-vm -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  if [[ "$phase" == "Running" ]]; then
    break
  fi
  sleep 5
done

if [[ "$phase" != "Running" ]]; then
  echo "ImpVM did not reach Running; phase=$phase" >&2
  sudo k3s kubectl -n imp-smoke get impvm oci-smoke-vm -o yaml || true
  sudo tail -n 200 /tmp/imp-operator.log || true
  sudo tail -n 200 /tmp/imp-agent.log || true
  exit 10
fi

pid="$(sudo k3s kubectl -n imp-smoke get impvm oci-smoke-vm -o jsonpath='{.status.runtimePID}')"
if [[ -z "$pid" || "$pid" == "0" ]]; then
  echo "runtimePID missing or zero" >&2
  exit 11
fi

if ! sudo test -d "/proc/$pid"; then
  echo "runtimePID process not found: $pid" >&2
  exit 12
fi

if ! pgrep -f firecracker >/dev/null 2>&1; then
  echo "firecracker process not found" >&2
  exit 13
fi

sudo k3s kubectl -n imp-smoke delete impvm oci-smoke-vm --wait=true --timeout=180s

if sudo k3s kubectl -n imp-smoke get impvm oci-smoke-vm >/dev/null 2>&1; then
  echo "ImpVM still present after delete" >&2
  exit 14
fi

echo "Remote Firecracker e2e smoke passed"
REMOTE_EOF
}

main() {
  require_cmd oci
  require_cmd ssh
  require_cmd scp
  require_cmd tar
  require_cmd jq
  resolve_oci_profile
  resolve_oci_auth_mode

  require_env OCI_SSH_PUBLIC_KEY_FILE
  require_env OCI_SSH_PRIVATE_KEY_FILE

  OCI_SHAPE="${OCI_SHAPE:-VM.Standard.E2.1.Micro}"
  OCI_SSH_USER="${OCI_SSH_USER:-ubuntu}"
  OCI_INSTANCE_NAME_PREFIX="${OCI_INSTANCE_NAME_PREFIX:-imp-fc-e2e}"
  OCI_ASSIGN_PUBLIC_IP="${OCI_ASSIGN_PUBLIC_IP:-true}"
  OCI_WAIT_SECONDS="${OCI_WAIT_SECONDS:-900}"
  FIRECRACKER_VERSION="${FIRECRACKER_VERSION:-v1.9.0}"
  OCI_COMPARTMENT_NAME="${OCI_COMPARTMENT_NAME:-${IMP_OCI_COMPARTMENT_NAME:-homelab}}"
  OCI_DOMAIN_NAME="${OCI_DOMAIN_NAME:-${IMP_OCI_DOMAIN_NAME:-homelab}}"

  ensure_image_ocid
  resolve_compartment
  require_env OCI_COMPARTMENT_OCID
  require_env OCI_AVAILABILITY_DOMAIN
  require_env OCI_SUBNET_OCID
  require_env OCI_IMAGE_OCID
  log "Using target domain metadata: OCI_DOMAIN_NAME=$OCI_DOMAIN_NAME"

  if [[ ! -f "$OCI_SSH_PUBLIC_KEY_FILE" ]]; then
    echo "SSH public key file not found: $OCI_SSH_PUBLIC_KEY_FILE" >&2
    exit 2
  fi
  if [[ ! -f "$OCI_SSH_PRIVATE_KEY_FILE" ]]; then
    echo "SSH private key file not found: $OCI_SSH_PRIVATE_KEY_FILE" >&2
    exit 2
  fi

  ensure_noninteractive_ssh_key
  enforce_free_tier_guardrails

  local instance_name instance_id public_ip test_rc=1
  local launch_deadline

  instance_name="${OCI_INSTANCE_NAME_PREFIX}-$(date +%Y%m%d-%H%M%S)"
  launch_deadline=$(( $(date +%s) + OCI_WAIT_SECONDS ))

  local ssh_key
  ssh_key="$(tr -d '\n' < "$OCI_SSH_PUBLIC_KEY_FILE")"

  log "Launching OCI instance: $instance_name"
  instance_id="$(oci compute instance launch \
    --compartment-id "$OCI_COMPARTMENT_OCID" \
    --availability-domain "$OCI_AVAILABILITY_DOMAIN" \
    --shape "$OCI_SHAPE" \
    --subnet-id "$OCI_SUBNET_OCID" \
    --assign-public-ip "$OCI_ASSIGN_PUBLIC_IP" \
    --display-name "$instance_name" \
    --image-id "$OCI_IMAGE_OCID" \
    --metadata "{\"ssh_authorized_keys\":\"$ssh_key\"}" \
    --query 'data.id' --raw-output)"

  log "Instance OCID: $instance_id"

  cleanup() {
    if [[ -n "${instance_id:-}" ]]; then
      if [[ "$test_rc" -eq 0 ]]; then
        terminate_instance "$instance_id" || true
      else
        log "Keeping instance for debugging: $instance_id"
        if [[ -n "${public_ip:-}" ]]; then
          log "Debug SSH: ssh -i '$OCI_SSH_PRIVATE_KEY_FILE' ${OCI_SSH_USER}@${public_ip}"
        fi
      fi
    fi
  }
  trap cleanup EXIT

  wait_for_running "$instance_id" "$launch_deadline"

  public_ip="$(get_instance_public_ip "$instance_id")"
  if [[ -z "$public_ip" || "$public_ip" == "null" ]]; then
    echo "could not determine public IP" >&2
    exit 4
  fi
  log "Public IP: $public_ip"

  wait_for_ssh "$public_ip" "$launch_deadline"

  set +e
  run_remote_smoke "$public_ip"
  test_rc=$?
  set -e

  if [[ "$test_rc" -eq 0 ]]; then
    log "OCI Firecracker e2e smoke passed"
  else
    log "OCI Firecracker e2e smoke failed with code $test_rc"
  fi

  return "$test_rc"
}

main "$@"
