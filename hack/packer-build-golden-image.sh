#!/usr/bin/env bash
# packer-build-golden-image.sh - Run OCI golden image build through native Packer OCI builder.
#
# hack/oci-build-golden-image.sh is used here only in --sanitize-only mode:
# profile/env validation, free-tier checks, and OCI input resolution.

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
export OCI_CONFIG_PROFILE="$OCI_PROFILE"
export OCI_CONFIG_FILE="${OCI_CLI_CONFIG_FILE:-$HOME/.oci/config}"

OUTPUT_ENV_FILE="${OCI_OUTPUT_ENV_FILE:-$HOME/.config/imp/oci-golden.env}"
mkdir -p "$(dirname "$OUTPUT_ENV_FILE")"

TMP_SANITIZED_ENV="$(mktemp /tmp/imp-oci-sanitize-XXXXXX)"
TMP_PACKER_OCI_CFG="$(mktemp /tmp/imp-oci-packer-cfg-XXXXXX)"
trap 'rm -f "$TMP_SANITIZED_ENV" "$TMP_PACKER_OCI_CFG"' EXIT

"$REPO_ROOT/hack/oci-build-golden-image.sh" --sanitize-only \
  | awk '/^OCI_[A-Z0-9_]+=/{print}' > "$TMP_SANITIZED_ENV"
# shellcheck disable=SC1090
source "$TMP_SANITIZED_ENV"
if [[ -z "${OCI_COMPARTMENT_OCID:-}" || -z "${OCI_AVAILABILITY_DOMAIN:-}" || -z "${OCI_SUBNET_OCID:-}" || -z "${OCI_BASE_IMAGE_OCID:-}" || -z "${OCI_REQUIRED_GO:-}" ]]; then
  echo "sanitize step did not produce required OCI_* variables" >&2
  exit 1
fi

IMAGE_NAME_PREFIX="${OCI_IMAGE_NAME_PREFIX:-imp-fc-golden}"
IMAGE_NAME="${IMAGE_NAME_PREFIX}-$(date +%Y%m%d-%H%M%S)"
OCI_SHAPE="${OCI_SHAPE:-VM.Standard.E2.1.Micro}"
OCI_SSH_USER="${OCI_SSH_USER:-ubuntu}"
FIRECRACKER_VERSION="${FIRECRACKER_VERSION:-v1.9.0}"

# oracle-oci builder reads the DEFAULT profile from access_cfg_file.
# Create a temporary config with selected profile mapped to DEFAULT.
awk -v p="$OCI_PROFILE" '
  BEGIN { in_profile=0; printed=0; has=0 }
  $0 ~ "^\\[" p "\\]$" { in_profile=1; has=1; print "[DEFAULT]"; next }
  $0 ~ "^\\[" && in_profile { in_profile=0 }
  in_profile { print }
  END { if (has==0) exit 1 }
' "$OCI_CONFIG_FILE" > "$TMP_PACKER_OCI_CFG" || {
  echo "failed to extract OCI profile '$OCI_PROFILE' from $OCI_CONFIG_FILE" >&2
  exit 1
}

packer init "$PACKER_TEMPLATE"
PACKER_LOG="${PACKER_LOG:-1}" packer build \
  -var "compartment_ocid=${OCI_COMPARTMENT_OCID}" \
  -var "availability_domain=${OCI_AVAILABILITY_DOMAIN}" \
  -var "subnet_ocid=${OCI_SUBNET_OCID}" \
  -var "base_image_ocid=${OCI_BASE_IMAGE_OCID}" \
  -var "image_name=${IMAGE_NAME}" \
  -var "shape=${OCI_SHAPE}" \
  -var "ssh_username=${OCI_SSH_USER}" \
  -var "firecracker_version=${FIRECRACKER_VERSION}" \
  -var "required_go=${OCI_REQUIRED_GO}" \
  -var "access_cfg_file=${TMP_PACKER_OCI_CFG}" \
  "$PACKER_TEMPLATE"

IMAGE_OCID="$(
  oci compute image list \
    --compartment-id "${OCI_COMPARTMENT_OCID}" \
    --all \
    --output json \
    | jq -r --arg n "$IMAGE_NAME" '.data[] | select(."display-name"==$n) | .id' \
    | head -n1
)"
if [[ -z "${IMAGE_OCID:-}" || "$IMAGE_OCID" == "null" ]]; then
  echo "failed to resolve OCI image OCID for image name: $IMAGE_NAME" >&2
  exit 1
fi
IMAGE_SIZE_MBS="$(
  oci compute image get \
    --image-id "$IMAGE_OCID" \
    --query 'data."size-in-mbs"' \
    --raw-output
)"
if ! [[ "${IMAGE_SIZE_MBS:-}" =~ ^[0-9]+$ ]]; then
  echo "failed to resolve OCI image size for image OCID: $IMAGE_OCID" >&2
  exit 1
fi
IMAGE_SIZE_GB=$(( (IMAGE_SIZE_MBS + 1023) / 1024 ))

cat >"$OUTPUT_ENV_FILE" <<EOF
OCI_COMPARTMENT_OCID=$OCI_COMPARTMENT_OCID
OCI_AVAILABILITY_DOMAIN=$OCI_AVAILABILITY_DOMAIN
OCI_SUBNET_OCID=$OCI_SUBNET_OCID
OCI_IMAGE_OCID=$IMAGE_OCID
OCI_IMAGE_SIZE_GB=$IMAGE_SIZE_GB
EOF

echo "OCI image metadata file: $OUTPUT_ENV_FILE"
