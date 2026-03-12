#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EXAMPLE_DIR="${ROOT_DIR}/examples/tiny-smoke"
NS="${NS:-default}"
IMP_NS="${IMP_NS:-imp-system}"
SERVER_VM="${SERVER_VM:-tiny-server}"
CLIENT_VM="${CLIENT_VM:-tiny-client}"
LOCAL_AGENT_PORT="${LOCAL_AGENT_PORT:-19091}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require kubectl
require curl

echo "[1/6] apply tiny smoke manifests"
kubectl apply -f "${EXAMPLE_DIR}/impvmclass.yaml"
kubectl apply -f "${EXAMPLE_DIR}/impnetwork.yaml" -n "${NS}"
kubectl apply -f "${EXAMPLE_DIR}/vm-server.yaml" -n "${NS}"
kubectl apply -f "${EXAMPLE_DIR}/vm-client.yaml" -n "${NS}"

echo "[2/6] wait for both VMs to be Running"
kubectl wait --for=jsonpath='{.status.phase}'=Running "impvm/${SERVER_VM}" -n "${NS}" --timeout=8m
kubectl wait --for=jsonpath='{.status.phase}'=Running "impvm/${CLIENT_VM}" -n "${NS}" --timeout=8m

SERVER_IP="$(kubectl get impvm "${SERVER_VM}" -n "${NS}" -o jsonpath='{.status.ip}')"
CLIENT_NODE="$(kubectl get impvm "${CLIENT_VM}" -n "${NS}" -o jsonpath='{.spec.nodeName}')"

if [[ -z "${SERVER_IP}" || -z "${CLIENT_NODE}" ]]; then
  echo "failed to resolve server IP or client node" >&2
  exit 1
fi

echo "[3/6] locate imp-agent pod on client node ${CLIENT_NODE}"
AGENT_POD="$(
  kubectl get pods -n "${IMP_NS}" -l app.kubernetes.io/component=agent \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.nodeName}{"\n"}{end}' \
  | awk -v node="${CLIENT_NODE}" '$2==node {print $1; exit}'
)"

if [[ -z "${AGENT_POD}" ]]; then
  echo "no imp-agent pod found on node ${CLIENT_NODE} in namespace ${IMP_NS}" >&2
  exit 1
fi

cleanup() {
  if [[ -n "${PF_PID:-}" ]]; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "[4/6] port-forward agent API via pod/${AGENT_POD}"
kubectl -n "${IMP_NS}" port-forward "pod/${AGENT_POD}" "${LOCAL_AGENT_PORT}:9091" >/tmp/imp-tiny-smoke-portforward.log 2>&1 &
PF_PID=$!

for _ in $(seq 1 40); do
  if ss -lnt | awk '{print $4}' | grep -q ":${LOCAL_AGENT_PORT}$"; then
    break
  fi
  sleep 0.5
done

if ! ss -lnt | awk '{print $4}' | grep -q ":${LOCAL_AGENT_PORT}$"; then
  echo "agent API port-forward did not become ready" >&2
  cat /tmp/imp-tiny-smoke-portforward.log >&2 || true
  exit 1
fi

echo "[5/6] execute connectivity check from ${CLIENT_VM} -> ${SERVER_IP}"
REQ_BODY="$(
  cat <<EOF
{"command":["/bin/sh","-lc","ping -c1 -W2 ${SERVER_IP}"]}
EOF
)"
RESP="$(
  curl --max-time 60 -fsS -X POST \
    -H "Content-Type: application/json" \
    --data "${REQ_BODY}" \
    "http://127.0.0.1:${LOCAL_AGENT_PORT}/v1/exec/${NS}/${CLIENT_VM}"
)"

echo "${RESP}" | grep -q '"stream":"exit","code":0' || {
  echo "connectivity check failed; agent exec response:" >&2
  echo "${RESP}" >&2
  exit 1
}

echo "[6/6] smoke PASS: classRef boot + VM-to-VM HTTP connectivity validated"
