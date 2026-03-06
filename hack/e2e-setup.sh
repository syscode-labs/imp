#!/usr/bin/env bash
# e2e-setup.sh — create Kind clusters for Imp e2e testing.
#
# Usage:
#   hack/e2e-setup.sh cilium    # create imp-cilium cluster + install Cilium
#   hack/e2e-setup.sh flannel   # create imp-flannel cluster (default CNI)
#   hack/e2e-setup.sh all       # create both clusters
#
# Prerequisites:
#   - kind (https://kind.sigs.k8s.io)
#   - kubectl
#   - cilium CLI (for cilium target): https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

setup_cilium() {
  echo "==> Creating imp-cilium cluster..."
  kind create cluster --name imp-cilium \
    --config "${REPO_ROOT}/test/e2e/kind-cilium.yaml"

  echo "==> Installing Cilium..."
  cilium install --wait

  echo "==> Cilium status:"
  cilium status --wait
  echo "==> imp-cilium cluster ready."
}

setup_flannel() {
  echo "==> Creating imp-flannel cluster..."
  kind create cluster --name imp-flannel \
    --config "${REPO_ROOT}/test/e2e/kind-flannel.yaml"
  echo "==> imp-flannel cluster ready."
}

case "${1:-all}" in
  cilium)  setup_cilium ;;
  flannel) setup_flannel ;;
  all)
    setup_cilium
    setup_flannel
    ;;
  *)
    echo "Usage: $0 [cilium|flannel|all]" >&2
    exit 1
    ;;
esac
