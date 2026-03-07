#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
sudo apt-get update -y
sudo apt-get install -y curl ca-certificates jq git build-essential qemu-utils iptables tar

if ! command -v k3s >/dev/null 2>&1; then
  curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)
    FC_ARCH="x86_64"
    KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin"
    GOARCH_DL="amd64"
    ;;
  aarch64|arm64)
    FC_ARCH="aarch64"
    KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/aarch64/kernels/vmlinux.bin"
    GOARCH_DL="arm64"
    ;;
  *)
    echo "unsupported architecture: $ARCH" >&2
    exit 3
    ;;
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

sudo mkdir -p /var/lib/imp/images /run/imp/sockets /opt/imp
if [[ ! -f /var/lib/imp/vmlinux ]]; then
  curl -fL "$KERNEL_URL" -o /tmp/vmlinux.bin
  sudo mv /tmp/vmlinux.bin /var/lib/imp/vmlinux
  sudo chmod 0644 /var/lib/imp/vmlinux
fi

need_go_install="true"
if command -v go >/dev/null 2>&1; then
  current_go="$(go version | awk '{print $3}' | sed 's/^go//')"
  if [[ "$current_go" == "$REQUIRED_GO" ]]; then
    need_go_install="false"
  fi
fi

if [[ "$need_go_install" == "true" ]]; then
  go_tgz="go${REQUIRED_GO}.linux-${GOARCH_DL}.tar.gz"
  curl -fL "https://go.dev/dl/${go_tgz}" -o "/tmp/${go_tgz}"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "/tmp/${go_tgz}"
fi

sudo apt-get autoremove -y --purge || true
sudo apt-get clean
sudo rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/* /var/cache/* || true
sudo rm -rf /root/.cache/* /home/ubuntu/.cache/* || true
sudo find /var/log -type f -name '*.gz' -delete || true
sudo find /var/log -type f -name '*.[0-9]' -delete || true
sudo truncate -s 0 /var/log/wtmp /var/log/btmp /var/log/lastlog || true
sudo journalctl --rotate || true
sudo journalctl --vacuum-time=1s || true
sudo cloud-init clean --logs || true
sudo fstrim -av || true
