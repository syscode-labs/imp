#!/usr/bin/env bash
# packer-build-golden-image.sh - Run OCI golden image build through Packer.
#
# This wrapper intentionally delegates to hack/oci-build-golden-image.sh so all
# OCI profile enforcement and free-tier guardrails remain in one place.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKER_TEMPLATE="$REPO_ROOT/hack/packer-oci-golden-image.pkr.hcl"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

require_cmd packer

# Session defaults; explicit values still win.
IMP_OCI_PROFILE="${IMP_OCI_PROFILE:-syscode-api}"
IMP_OCI_COMPARTMENT_NAME="${IMP_OCI_COMPARTMENT_NAME:-homelab}"
IMP_OCI_DOMAIN_NAME="${IMP_OCI_DOMAIN_NAME:-homelab}"
export IMP_OCI_PROFILE IMP_OCI_COMPARTMENT_NAME IMP_OCI_DOMAIN_NAME

OCI_PROFILE="${OCI_PROFILE:-${OCI_CLI_PROFILE:-$IMP_OCI_PROFILE}}"
export OCI_PROFILE
export OCI_CLI_PROFILE="$OCI_PROFILE"

OUTPUT_ENV_FILE="${OCI_OUTPUT_ENV_FILE:-$HOME/.config/imp/oci-golden.env}"
mkdir -p "$(dirname "$OUTPUT_ENV_FILE")"

packer init "$PACKER_TEMPLATE"
packer build -var "output_env_file=${OUTPUT_ENV_FILE}" "$PACKER_TEMPLATE"

echo "OCI image metadata file: $OUTPUT_ENV_FILE"
