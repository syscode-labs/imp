#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXAMPLE_DIR="${ROOT_DIR}/examples/tiny-smoke"

required_files=(
  "${EXAMPLE_DIR}/README.md"
  "${EXAMPLE_DIR}/impvmclass.yaml"
  "${EXAMPLE_DIR}/impnetwork.yaml"
  "${EXAMPLE_DIR}/vm-server.yaml"
  "${EXAMPLE_DIR}/vm-client.yaml"
  "${EXAMPLE_DIR}/run.sh"
)

for f in "${required_files[@]}"; do
  [[ -f "${f}" ]] || {
    echo "missing tiny-smoke asset: ${f}" >&2
    exit 1
  }
done

grep -q '^kind: ImpVMClass$' "${EXAMPLE_DIR}/impvmclass.yaml"
grep -q '^kind: ImpNetwork$' "${EXAMPLE_DIR}/impnetwork.yaml"
grep -q '^kind: ImpVM$' "${EXAMPLE_DIR}/vm-server.yaml"
grep -q '^kind: ImpVM$' "${EXAMPLE_DIR}/vm-client.yaml"
grep -q 'classRef:' "${EXAMPLE_DIR}/vm-server.yaml"
grep -q 'classRef:' "${EXAMPLE_DIR}/vm-client.yaml"
grep -q 'networkRef:' "${EXAMPLE_DIR}/vm-server.yaml"
grep -q 'networkRef:' "${EXAMPLE_DIR}/vm-client.yaml"

# Require pinned image tags (reject :latest) for this tiny smoke path.
if grep -R --line-number 'image: .*:latest' "${EXAMPLE_DIR}"/*.yaml; then
  echo "tiny-smoke manifests must not use :latest tags" >&2
  exit 1
fi

bash -n "${EXAMPLE_DIR}/run.sh"
echo "tiny-smoke assets verified"
