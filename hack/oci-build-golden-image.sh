#!/usr/bin/env bash
# oci-build-golden-image.sh - Build a small OCI custom image for imp e2e.
#
# Required env:
#   OCI_SSH_PUBLIC_KEY_FILE
#   OCI_SSH_PRIVATE_KEY_FILE
#
# Optional env:
#   IMP_OCI_PROFILE          Preferred profile selector (default: syscode-api)
#   OCI_PROFILE              OCI CLI profile to use (required unless OCI_CLI_PROFILE is set)
#   OCI_CLI_PROFILE          OCI CLI profile to use (alternative to OCI_PROFILE)
#   IMP_OCI_COMPARTMENT_NAME Preferred target compartment name (default: homelab)
#   IMP_OCI_DOMAIN_NAME      Preferred target domain name (default: homelab; metadata only)
#   OCI_TENANCY_OCID         Tenancy OCID override (auto-detected from OCI profile if omitted)
#   OCI_COMPARTMENT_OCID      Auto-detected if omitted
#   OCI_AVAILABILITY_DOMAIN   Auto-detected if omitted
#   OCI_SUBNET_OCID           Auto-detected/created if omitted
#   OCI_SHAPE                 Default: VM.Standard.E2.1.Micro
#   OCI_ASSIGN_PUBLIC_IP      Default: true
#   OCI_SSH_USER              Default: ubuntu
#   OCI_WAIT_SECONDS          Default: 1200
#   OCI_IMAGE_NAME_PREFIX     Default: imp-fc-golden
#   OCI_BOOT_VOLUME_GB        Default: 50
#   OCI_GOLDEN_MAX_GB         Default: 50
#   OCI_BASE_OS               Default: Canonical Ubuntu
#   OCI_BASE_OS_VERSION       Default: 24.04
#   FIRECRACKER_VERSION       Default: v1.9.0
#   OCI_OUTPUT_ENV_FILE       If set, write OCI_IMAGE_OCID and OCI_IMAGE_SIZE_GB to this file
#   OCI_DELETE_OVERSIZE_IMAGE Default: true (delete image if above OCI_GOLDEN_MAX_GB)
#   KEEP_BUILDER_ON_FAILURE   Default: false
#   FREE_TIER_MAX_MICRO       Default: 2
#   OCI_GOLDEN_STRIP          Default: true (remove caches/docs/logs before image capture)
#   OCI_GOLDEN_ZERO_FILL      Default: false (zero free space before image capture)
#   OCI_NETWORK_PREFIX        Default: imp-e2e-autonet
#   OCI_VCN_CIDR              Default: 10.0.0.0/16
#   OCI_SUBNET_CIDR           Default: 10.0.1.0/24
#   ALLOW_SSH_AGENT           Default: false (set true to allow passphrase key via ssh-agent)
#   OCI_AUTO_PRUNE_CUSTOM_IMAGES Default: true (delete oldest prefixed images if custom-image quota is full)
#   OCI_AUTO_PRUNE_ALL_CUSTOM_IMAGES Default: false (if no prefixed images are available, prune oldest custom image)
#
# Usage:
#   IMP_OCI_PROFILE=syscode-api IMP_OCI_COMPARTMENT_NAME=homelab IMP_OCI_DOMAIN_NAME=homelab \
#   OCI_COMPARTMENT_OCID=... OCI_AVAILABILITY_DOMAIN=... OCI_SUBNET_OCID=... \
#   OCI_SSH_PUBLIC_KEY_FILE=~/.ssh/id_ed25519.pub OCI_SSH_PRIVATE_KEY_FILE=~/.ssh/id_ed25519 \
#   hack/oci-build-golden-image.sh
#
#   # Preflight/sanitize mode for packer wrapper
#   hack/oci-build-golden-image.sh --sanitize-only

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

  if [[ -n "${OCI_COMPARTMENT_NAME:-}" ]]; then
    local by_name
    by_name="$(
      oci iam compartment list --all --compartment-id-in-subtree true --output json \
        | jq -r --arg n "$OCI_COMPARTMENT_NAME" '
          .data
          | map(select(."lifecycle-state"=="ACTIVE" and .name==$n))
          | .[0].id // empty
        '
    )"
    if [[ -n "${by_name:-}" ]]; then
      OCI_COMPARTMENT_OCID="$by_name"
      log "Resolved OCI_COMPARTMENT_OCID from OCI_COMPARTMENT_NAME=$OCI_COMPARTMENT_NAME: $OCI_COMPARTMENT_OCID"
      return 0
    fi
  fi

  local chosen
  chosen="$(
    oci iam compartment list --all --compartment-id-in-subtree true --output json \
      | jq -r '
        .data
        | map(select(."lifecycle-state"=="ACTIVE"))
        | (
            [ .[] | select(.name=="freetier-compartment") ]
            + [ .[] | select(.name!="ManagedCompartmentForPaaS") ]
          )
        | .[0].id // empty
      '
  )"
  if [[ -z "${chosen:-}" ]]; then
    chosen="$(
      awk -v p="$OCI_PROFILE" '
        BEGIN { in_profile=0 }
        $0 ~ "^\\[" p "\\]$" { in_profile=1; next }
        $0 ~ "^\\[" && in_profile { in_profile=0 }
        in_profile && $0 ~ /^[[:space:]]*tenancy[[:space:]]*=/ {
          split($0, a, "=")
          gsub(/[[:space:]]/, "", a[2])
          print a[2]
          exit
        }
      ' "${OCI_CLI_CONFIG_FILE:-$HOME/.oci/config}"
    )"
    if [[ -z "${chosen:-}" ]]; then
      echo "could not auto-detect OCI_COMPARTMENT_OCID; set it explicitly" >&2
      exit 2
    fi
  fi
  OCI_COMPARTMENT_OCID="$chosen"
  log "Auto-detected OCI_COMPARTMENT_OCID=$OCI_COMPARTMENT_OCID"
}

resolve_tenancy_ocid() {
  if [[ -n "${OCI_TENANCY_OCID:-}" ]]; then
    return 0
  fi
  OCI_TENANCY_OCID="$(
    awk -v p="$OCI_PROFILE" '
      BEGIN { in_profile=0 }
      $0 ~ "^\\[" p "\\]$" { in_profile=1; next }
      $0 ~ "^\\[" && in_profile { in_profile=0 }
      in_profile && $0 ~ /^[[:space:]]*tenancy[[:space:]]*=/ {
        split($0, a, "=")
        gsub(/[[:space:]]/, "", a[2])
        print a[2]
        exit
      }
    ' "${OCI_CLI_CONFIG_FILE:-$HOME/.oci/config}"
  )"
  if [[ -z "${OCI_TENANCY_OCID:-}" ]]; then
    echo "could not auto-detect OCI_TENANCY_OCID from OCI profile/config" >&2
    exit 2
  fi
}

resolve_availability_domain() {
  if [[ -n "${OCI_AVAILABILITY_DOMAIN:-}" ]]; then
    return 0
  fi

  local tenancy_ocid ad_name
  if [[ "${OCI_SHAPE:-VM.Standard.E2.1.Micro}" == "VM.Standard.E2.1.Micro" ]]; then
    local limits_json
    limits_json="$(oci limits value list --all --service-name compute --compartment-id "$OCI_TENANCY_OCID" --output json 2>/dev/null || echo '{"data":[]}')"
    ad_name="$(
      printf '%s' "$limits_json" | jq -r '
          .data
          | map(select(.name=="vm-standard-e2-1-micro-count" and ((.value // 0) > 0) and ."availability-domain" != null))
          | sort_by(.value)
          | reverse
          | .[0]."availability-domain" // empty
        '
    )"
    if [[ -n "${ad_name:-}" ]]; then
      OCI_AVAILABILITY_DOMAIN="$ad_name"
      log "Auto-detected OCI_AVAILABILITY_DOMAIN from limits: $OCI_AVAILABILITY_DOMAIN"
      return 0
    fi
  fi

  tenancy_ocid="$(
    awk -F= '
      /^[[:space:]]*tenancy[[:space:]]*=/ {
        gsub(/[[:space:]]/, "", $2);
        print $2;
        exit
      }' "${HOME}/.oci/config"
  )"
  if [[ -z "${tenancy_ocid:-}" ]]; then
    echo "could not auto-detect tenancy OCID from ~/.oci/config; set OCI_AVAILABILITY_DOMAIN explicitly" >&2
    exit 2
  fi

  ad_name="$(
    oci iam availability-domain list --compartment-id "$tenancy_ocid" --output json \
      | jq -r '.data[0].name // empty'
  )"
  if [[ -z "${ad_name:-}" ]]; then
    echo "could not auto-detect OCI_AVAILABILITY_DOMAIN; set it explicitly" >&2
    exit 2
  fi
  OCI_AVAILABILITY_DOMAIN="$ad_name"
  log "Auto-detected OCI_AVAILABILITY_DOMAIN=$OCI_AVAILABILITY_DOMAIN"
}

ensure_subnet() {
  if [[ -n "${OCI_SUBNET_OCID:-}" ]]; then
    return 0
  fi

  local existing_subnet
  existing_subnet="$(
    oci network subnet list --all --compartment-id "$OCI_COMPARTMENT_OCID" --output json \
      | jq -r '
        .data
        | map(select(."lifecycle-state"=="AVAILABLE" and ."prohibit-public-ip-on-vnic"==false))
        | .[0].id // empty
      '
  )"
  if [[ -n "${existing_subnet:-}" ]]; then
    OCI_SUBNET_OCID="$existing_subnet"
    log "Reusing existing public subnet: $OCI_SUBNET_OCID"
    return 0
  fi

  local prefix vcn_name igw_name rt_name sl_name subnet_name
  local vcn_cidr subnet_cidr
  local vcn_id dhcp_id igw_id rt_id sl_id

  prefix="${OCI_NETWORK_PREFIX:-imp-e2e-autonet}"
  vcn_name="${prefix}-vcn"
  igw_name="${prefix}-igw"
  rt_name="${prefix}-rt"
  sl_name="${prefix}-sl"
  subnet_name="${prefix}-subnet"
  vcn_cidr="${OCI_VCN_CIDR:-10.0.0.0/16}"
  subnet_cidr="${OCI_SUBNET_CIDR:-10.0.1.0/24}"

  vcn_id="$(
    oci network vcn list --all --compartment-id "$OCI_COMPARTMENT_OCID" --output json \
      | jq -r --arg n "$vcn_name" '
        .data
        | map(select(."lifecycle-state"=="AVAILABLE" and ."display-name"==$n))
        | .[0].id // empty
      '
  )"
  if [[ -z "${vcn_id:-}" ]]; then
    log "Creating VCN: $vcn_name ($vcn_cidr)"
    vcn_id="$(
      oci network vcn create \
        --compartment-id "$OCI_COMPARTMENT_OCID" \
        --display-name "$vcn_name" \
        --cidr-block "$vcn_cidr" \
        --query 'data.id' \
        --raw-output
    )"
  else
    log "Reusing VCN: $vcn_id"
  fi

  dhcp_id="$(
    oci network vcn get --vcn-id "$vcn_id" --output json \
      | jq -r '.data."default-dhcp-options-id"'
  )"

  igw_id="$(
    oci network internet-gateway list --all --compartment-id "$OCI_COMPARTMENT_OCID" --output json \
      | jq -r --arg v "$vcn_id" --arg n "$igw_name" '
        .data
        | map(select(."lifecycle-state"=="AVAILABLE" and ."vcn-id"==$v and ."display-name"==$n))
        | .[0].id // empty
      '
  )"
  if [[ -z "${igw_id:-}" ]]; then
    log "Creating Internet Gateway: $igw_name"
    igw_id="$(
      oci network internet-gateway create \
        --compartment-id "$OCI_COMPARTMENT_OCID" \
        --vcn-id "$vcn_id" \
        --display-name "$igw_name" \
        --is-enabled true \
        --query 'data.id' \
        --raw-output
    )"
  fi

  rt_id="$(
    oci network route-table list --all --compartment-id "$OCI_COMPARTMENT_OCID" --output json \
      | jq -r --arg v "$vcn_id" --arg n "$rt_name" '
        .data
        | map(select(."lifecycle-state"=="AVAILABLE" and ."vcn-id"==$v and ."display-name"==$n))
        | .[0].id // empty
      '
  )"
  if [[ -z "${rt_id:-}" ]]; then
    log "Creating Route Table: $rt_name"
    rt_id="$(
      oci network route-table create \
        --compartment-id "$OCI_COMPARTMENT_OCID" \
        --vcn-id "$vcn_id" \
        --display-name "$rt_name" \
        --route-rules "[{\"cidrBlock\":\"0.0.0.0/0\",\"networkEntityId\":\"${igw_id}\"}]" \
        --query 'data.id' \
        --raw-output
    )"
  else
    oci network route-table update \
      --rt-id "$rt_id" \
      --force \
      --route-rules "[{\"cidrBlock\":\"0.0.0.0/0\",\"networkEntityId\":\"${igw_id}\"}]" >/dev/null
  fi

  sl_id="$(
    oci network security-list list --all --compartment-id "$OCI_COMPARTMENT_OCID" --output json \
      | jq -r --arg v "$vcn_id" --arg n "$sl_name" '
        .data
        | map(select(."lifecycle-state"=="AVAILABLE" and ."vcn-id"==$v and ."display-name"==$n))
        | .[0].id // empty
      '
  )"
  if [[ -z "${sl_id:-}" ]]; then
    log "Creating Security List: $sl_name"
    sl_id="$(
      oci network security-list create \
        --compartment-id "$OCI_COMPARTMENT_OCID" \
        --vcn-id "$vcn_id" \
        --display-name "$sl_name" \
        --egress-security-rules '[{"destination":"0.0.0.0/0","destinationType":"CIDR_BLOCK","protocol":"all","isStateless":false}]' \
        --ingress-security-rules '[{"source":"0.0.0.0/0","sourceType":"CIDR_BLOCK","protocol":"6","isStateless":false,"tcpOptions":{"destinationPortRange":{"min":22,"max":22}}}]' \
        --query 'data.id' \
        --raw-output
    )"
  fi

  OCI_SUBNET_OCID="$(
    oci network subnet list --all --compartment-id "$OCI_COMPARTMENT_OCID" --output json \
      | jq -r --arg v "$vcn_id" --arg n "$subnet_name" '
        .data
        | map(select(."lifecycle-state"=="AVAILABLE" and ."vcn-id"==$v and ."display-name"==$n))
        | .[0].id // empty
      '
  )"
  if [[ -z "${OCI_SUBNET_OCID:-}" ]]; then
    log "Creating Subnet: $subnet_name ($subnet_cidr)"
    OCI_SUBNET_OCID="$(
      oci network subnet create \
        --compartment-id "$OCI_COMPARTMENT_OCID" \
        --vcn-id "$vcn_id" \
        --display-name "$subnet_name" \
        --cidr-block "$subnet_cidr" \
        --route-table-id "$rt_id" \
        --security-list-ids "[\"${sl_id}\"]" \
        --dhcp-options-id "$dhcp_id" \
        --prohibit-public-ip-on-vnic false \
        --query 'data.id' \
        --raw-output
    )"
  fi

  log "Using subnet: $OCI_SUBNET_OCID"
}

ensure_noninteractive_ssh_key() {
  if ssh-keygen -y -P "" -f "$OCI_SSH_PRIVATE_KEY_FILE" >/dev/null 2>&1; then
    return 0
  fi
  if [[ "${ALLOW_SSH_AGENT:-false}" == "true" && -n "${SSH_AUTH_SOCK:-}" ]]; then
    log "SSH private key appears passphrase-protected; relying on ssh-agent"
    return 0
  fi
  echo "SSH private key appears passphrase-protected." >&2
  echo "Use an unencrypted key for automation, or set ALLOW_SSH_AGENT=true with a loaded ssh-agent key." >&2
  exit 2
}

enforce_free_tier_guardrails() {
  local current projected limit_from_oci
  local max_micro="${FREE_TIER_MAX_MICRO:-2}"

  if [[ "$OCI_SHAPE" != "VM.Standard.E2.1.Micro" ]]; then
    echo "Refusing non-free-tier shape ($OCI_SHAPE) for golden-image build." >&2
    exit 2
  fi

  current="$(
    oci compute instance list \
      --compartment-id "$OCI_COMPARTMENT_OCID" \
      --all \
      --query "length(data[?shape=='VM.Standard.E2.1.Micro' && \"lifecycle-state\"=='RUNNING' && \"availability-domain\"=='${OCI_AVAILABILITY_DOMAIN}'])" \
      --raw-output
  )"
  if ! [[ "${current:-}" =~ ^[0-9]+$ ]]; then
    current=0
  fi
  local limits_json
  limits_json="$(oci limits value list --all --service-name compute --compartment-id "$OCI_TENANCY_OCID" --output json 2>/dev/null || echo '{"data":[]}')"
  limit_from_oci="$(
    printf '%s' "$limits_json" | jq -r --arg ad "$OCI_AVAILABILITY_DOMAIN" '
        .data
        | map(select(.name=="vm-standard-e2-1-micro-count" and ."availability-domain"==$ad))
        | .[0].value // empty
      '
  )"
  if [[ "${limit_from_oci:-}" =~ ^[0-9]+$ ]] && (( limit_from_oci > 0 )); then
    max_micro="$limit_from_oci"
  fi
  projected=$((current + 1))
  if (( projected > max_micro )); then
    echo "Free-tier guardrail: RUNNING micro instances in ${OCI_AVAILABILITY_DOMAIN} would become $projected (limit $max_micro)." >&2
    echo "Stop/terminate another micro instance in that AD or override FREE_TIER_MAX_MICRO explicitly." >&2
    exit 2
  fi
  log "Free-tier check ($OCI_AVAILABILITY_DOMAIN): current micro RUNNING=$current, projected=$projected, limit=$max_micro"
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
  log "Terminating builder instance: $instance_id"
  oci compute instance terminate \
    --instance-id "$instance_id" \
    --force \
    --preserve-boot-volume false >/dev/null
}

prune_custom_images_if_needed() {
  local limit current
  local prefix
  prefix="${OCI_IMAGE_NAME_PREFIX:-imp-fc-golden}"

  local limits_json
  limits_json="$(oci limits value list --all --service-name compute --compartment-id "$OCI_TENANCY_OCID" --output json 2>/dev/null || echo '{"data":[]}')"
  limit="$(
    printf '%s' "$limits_json" | jq -r '
        .data
        | map(select(.name=="custom-image-count"))
        | .[0].value // empty
      '
  )"
  if ! [[ "${limit:-}" =~ ^[0-9]+$ ]] || (( limit <= 0 )); then
    return 0
  fi

  local all_images_json
  all_images_json="$(
    {
      echo '['
      local first=1
      while IFS= read -r cid; do
        local rows
        rows="$(
          oci compute image list --all --compartment-id "$cid" --output json \
            | jq -c '.data[] | select(."created-by" != null and ."lifecycle-state"=="AVAILABLE")'
        )"
        if [[ -n "${rows:-}" ]]; then
          while IFS= read -r row; do
            if (( first == 0 )); then
              echo ','
            fi
            printf '%s' "$row"
            first=0
          done <<<"$rows"
        fi
      done < <(
        oci iam compartment list --all --compartment-id-in-subtree true --output json \
          | jq -r '.data[] | select(."lifecycle-state"=="ACTIVE") | .id'
      )
      echo ']'
    } | tr -d '\n'
  )"
  current="$(printf '%s' "$all_images_json" | jq -r 'length')"
  if ! [[ "${current:-}" =~ ^[0-9]+$ ]]; then
    current=0
  fi
  if (( current < limit )); then
    return 0
  fi

  if [[ "${OCI_AUTO_PRUNE_CUSTOM_IMAGES:-true}" != "true" ]]; then
    echo "custom image quota is full ($current/$limit); set OCI_AUTO_PRUNE_CUSTOM_IMAGES=true or delete old custom images" >&2
    exit 2
  fi

  log "Custom image quota full ($current/$limit), pruning oldest ${prefix}-* images"
  while (( current >= limit )); do
    local victim
    victim="$(printf '%s' "$all_images_json" | jq -r --arg p "${prefix}-" '
      map(select((."display-name" | startswith($p))))
      | sort_by(."time-created")
      | .[0].id // empty
    ')"
    if [[ -z "${victim:-}" && "${OCI_AUTO_PRUNE_ALL_CUSTOM_IMAGES:-false}" == "true" ]]; then
      victim="$(printf '%s' "$all_images_json" | jq -r '
        sort_by(."time-created")
        | .[0].id // empty
      ')"
    fi
    if [[ -z "${victim:-}" ]]; then
      echo "custom image quota is full ($current/$limit) and no prunable images were found under current policy" >&2
      echo "Set OCI_AUTO_PRUNE_ALL_CUSTOM_IMAGES=true to allow pruning oldest custom images globally." >&2
      exit 2
    fi
    log "Deleting old custom image: $victim"
    oci compute image delete --image-id "$victim" --force >/dev/null
    current=$((current - 1))
    all_images_json="$(printf '%s' "$all_images_json" | jq -c --arg id "$victim" 'map(select(.id != $id))')"
  done
}

resolve_required_go() {
  local required_go
  required_go="$(awk '/^go /{print $2; exit}' "$REPO_ROOT/go.mod")"
  if [[ -z "${required_go:-}" ]]; then
    echo "could not parse required Go version from go.mod" >&2
    exit 2
  fi
  echo "$required_go"
}

resolve_smallest_base_image() {
  local image_json
  image_json="$(oci compute image list \
    --compartment-id "$OCI_COMPARTMENT_OCID" \
    --all \
    --operating-system "$OCI_BASE_OS" \
    --operating-system-version "$OCI_BASE_OS_VERSION")"

  echo "$image_json" | jq -r '
    .data
    | map(select(."lifecycle-state" == "AVAILABLE"))
    | sort_by(."size-in-mbs")
    | (
      [ .[] | select((."display-name" | ascii_downcase | contains("minimal")) and (."display-name" | ascii_downcase | contains("aarch64") | not)) ]
      + [ .[] | select(."display-name" | ascii_downcase | contains("aarch64") | not) ]
    )
    | unique_by(.id)
    | .[0]
    | [.id, ."display-name", ((."size-in-mbs" / 1024) | floor)]
    | @tsv
  '
}

main() {
  local sanitize_only=false
  if [[ "${1:-}" == "--sanitize-only" ]]; then
    sanitize_only=true
  elif [[ $# -gt 0 ]]; then
    echo "unknown argument: $1" >&2
    exit 2
  fi

  require_cmd oci
  require_cmd jq
  if [[ "$sanitize_only" != "true" ]]; then
    require_cmd ssh
    require_cmd ssh-keygen
    require_cmd curl
  fi
  resolve_oci_profile
  resolve_oci_auth_mode
  if [[ "$sanitize_only" != "true" ]]; then
    require_env OCI_SSH_PUBLIC_KEY_FILE
    require_env OCI_SSH_PRIVATE_KEY_FILE
  fi

  OCI_SHAPE="${OCI_SHAPE:-VM.Standard.E2.1.Micro}"
  OCI_ASSIGN_PUBLIC_IP="${OCI_ASSIGN_PUBLIC_IP:-true}"
  OCI_SSH_USER="${OCI_SSH_USER:-ubuntu}"
  OCI_WAIT_SECONDS="${OCI_WAIT_SECONDS:-1200}"
  OCI_IMAGE_NAME_PREFIX="${OCI_IMAGE_NAME_PREFIX:-imp-fc-golden}"
  OCI_BOOT_VOLUME_GB="${OCI_BOOT_VOLUME_GB:-50}"
  OCI_GOLDEN_MAX_GB="${OCI_GOLDEN_MAX_GB:-50}"
  OCI_BASE_OS="${OCI_BASE_OS:-Canonical Ubuntu}"
  OCI_BASE_OS_VERSION="${OCI_BASE_OS_VERSION:-24.04}"
  FIRECRACKER_VERSION="${FIRECRACKER_VERSION:-v1.9.0}"
  OCI_DELETE_OVERSIZE_IMAGE="${OCI_DELETE_OVERSIZE_IMAGE:-true}"
  KEEP_BUILDER_ON_FAILURE="${KEEP_BUILDER_ON_FAILURE:-false}"
  OCI_GOLDEN_STRIP="${OCI_GOLDEN_STRIP:-true}"
  OCI_GOLDEN_ZERO_FILL="${OCI_GOLDEN_ZERO_FILL:-false}"
  OCI_COMPARTMENT_NAME="${OCI_COMPARTMENT_NAME:-${IMP_OCI_COMPARTMENT_NAME:-homelab}}"
  OCI_DOMAIN_NAME="${OCI_DOMAIN_NAME:-${IMP_OCI_DOMAIN_NAME:-homelab}}"

  if [[ "$sanitize_only" != "true" ]]; then
    if [[ ! -f "$OCI_SSH_PUBLIC_KEY_FILE" ]]; then
      echo "SSH public key file not found: $OCI_SSH_PUBLIC_KEY_FILE" >&2
      exit 2
    fi
    if [[ ! -f "$OCI_SSH_PRIVATE_KEY_FILE" ]]; then
      echo "SSH private key file not found: $OCI_SSH_PRIVATE_KEY_FILE" >&2
      exit 2
    fi
  fi
  if (( OCI_BOOT_VOLUME_GB < 50 )); then
    echo "OCI_BOOT_VOLUME_GB must be >= 50 for OCI boot volumes" >&2
    exit 2
  fi

  resolve_compartment
  resolve_tenancy_ocid
  resolve_availability_domain
  ensure_subnet
  log "Using target domain metadata: OCI_DOMAIN_NAME=$OCI_DOMAIN_NAME"

  require_env OCI_COMPARTMENT_OCID
  require_env OCI_AVAILABILITY_DOMAIN
  require_env OCI_SUBNET_OCID

  enforce_free_tier_guardrails
  if [[ "$sanitize_only" != "true" ]]; then
    ensure_noninteractive_ssh_key
    prune_custom_images_if_needed
  fi

  local base_id base_name base_size_gb
  local base_line
  base_line="$(resolve_smallest_base_image)"
  if [[ -z "${base_line:-}" || "$base_line" == "null" ]]; then
    echo "could not resolve a base image for $OCI_BASE_OS $OCI_BASE_OS_VERSION" >&2
    exit 2
  fi
  base_id="$(printf '%s\n' "$base_line" | cut -f1)"
  base_name="$(printf '%s\n' "$base_line" | cut -f2)"
  base_size_gb="$(printf '%s\n' "$base_line" | cut -f3)"

  log "Selected base image: $base_name"
  log "Base image OCID: $base_id"
  log "Base image size (reported): ${base_size_gb}GiB"
  log "Builder boot volume target: ${OCI_BOOT_VOLUME_GB}GiB"
  log "Golden image max allowed size: ${OCI_GOLDEN_MAX_GB}GiB"

  if [[ "$sanitize_only" == "true" ]]; then
    local required_go
    required_go="$(resolve_required_go)"
    cat <<EOF
OCI_COMPARTMENT_OCID=$OCI_COMPARTMENT_OCID
OCI_AVAILABILITY_DOMAIN=$OCI_AVAILABILITY_DOMAIN
OCI_SUBNET_OCID=$OCI_SUBNET_OCID
OCI_BASE_IMAGE_OCID=$base_id
OCI_BASE_IMAGE_NAME=$base_name
OCI_BASE_IMAGE_SIZE_GB=$base_size_gb
OCI_REQUIRED_GO=$required_go
EOF
    return 0
  fi

  local required_go
  required_go="$(resolve_required_go)"

  local launch_deadline
  launch_deadline=$(( $(date +%s) + OCI_WAIT_SECONDS ))

  local instance_name instance_id public_ip image_name image_id test_rc=1
  local ssh_key

  instance_name="${OCI_IMAGE_NAME_PREFIX}-builder-$(date +%Y%m%d-%H%M%S)"
  image_name="${OCI_IMAGE_NAME_PREFIX}-$(date +%Y%m%d-%H%M%S)"
  ssh_key="$(tr -d '\n' < "$OCI_SSH_PUBLIC_KEY_FILE")"

  log "Launching builder instance: $instance_name"
  instance_id="$(oci compute instance launch \
    --compartment-id "$OCI_COMPARTMENT_OCID" \
    --availability-domain "$OCI_AVAILABILITY_DOMAIN" \
    --shape "$OCI_SHAPE" \
    --subnet-id "$OCI_SUBNET_OCID" \
    --assign-public-ip "$OCI_ASSIGN_PUBLIC_IP" \
    --display-name "$instance_name" \
    --image-id "$base_id" \
    --boot-volume-size-in-gbs "$OCI_BOOT_VOLUME_GB" \
    --metadata "{\"ssh_authorized_keys\":\"$ssh_key\"}" \
    --query 'data.id' --raw-output)"

  cleanup() {
    if [[ -n "${instance_id:-}" ]]; then
      if [[ "$test_rc" -eq 0 || "$KEEP_BUILDER_ON_FAILURE" != "true" ]]; then
        terminate_instance "$instance_id" || true
      else
        log "Keeping builder instance for debugging: $instance_id"
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
    echo "could not determine builder public IP" >&2
    exit 4
  fi
  wait_for_ssh "$public_ip" "$launch_deadline"

  log "Provisioning builder host for imp e2e prerequisites"
  # shellcheck disable=SC2029
  ssh -i "$OCI_SSH_PRIVATE_KEY_FILE" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=15 \
    "${OCI_SSH_USER}@${public_ip}" \
    "REQUIRED_GO='${required_go}' FIRECRACKER_VERSION='${FIRECRACKER_VERSION}' OCI_GOLDEN_STRIP='${OCI_GOLDEN_STRIP}' OCI_GOLDEN_ZERO_FILL='${OCI_GOLDEN_ZERO_FILL}' bash -s" <<'REMOTE_EOF'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
sudo apt-get update -y
sudo apt-get install -y curl ca-certificates jq git build-essential qemu-utils iptables tar

if ! command -v k3s >/dev/null 2>&1; then
  curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) FC_ARCH="x86_64"; KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin"; GOARCH_DL="amd64" ;;
  aarch64|arm64) FC_ARCH="aarch64"; KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/aarch64/kernels/vmlinux.bin"; GOARCH_DL="arm64" ;;
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

if [[ "${OCI_GOLDEN_STRIP:-true}" == "true" ]]; then
  # Keep only runtime essentials before taking the custom image snapshot.
  sudo apt-get autoremove -y --purge || true
  sudo apt-get clean
  sudo rm -rf /var/lib/apt/lists/*
  sudo rm -rf /tmp/* /var/tmp/*
  sudo rm -rf /var/cache/* || true
  sudo rm -rf /root/.cache/* || true
  sudo rm -rf /home/ubuntu/.cache/* || true
  sudo find /var/log -type f -name '*.gz' -delete || true
  sudo find /var/log -type f -name '*.[0-9]' -delete || true
  sudo truncate -s 0 /var/log/wtmp /var/log/btmp /var/log/lastlog || true
  sudo journalctl --rotate || true
  sudo journalctl --vacuum-time=1s || true
  sudo cloud-init clean --logs || true
  # Optional removals; keep disabled by default for safety.
  if [[ "${OCI_GOLDEN_REMOVE_DOCS:-false}" == "true" ]]; then
    sudo rm -rf /usr/share/doc/* /usr/share/man/* /usr/share/info/*
  fi
  if [[ "${OCI_GOLDEN_REMOVE_LOCALES:-false}" == "true" ]]; then
    sudo find /usr/share/locale -mindepth 1 -maxdepth 1 ! -name 'en*' -exec rm -rf {} +
  fi
  # Hint the platform to reclaim free blocks before custom image capture.
  sudo fstrim -av || true
fi

if [[ "${OCI_GOLDEN_ZERO_FILL:-false}" == "true" ]]; then
  # Zero-fill free space so sparse/compressed image backends can store fewer blocks.
  sudo dd if=/dev/zero of=/EMPTY bs=16M status=progress || true
  sync
  sudo rm -f /EMPTY
  sync
  sudo fstrim -av || true
fi

echo "builder provisioning complete"
REMOTE_EOF

  log "Creating OCI custom image: $image_name"
  image_id="$(oci compute image create \
    --compartment-id "$OCI_COMPARTMENT_OCID" \
    --instance-id "$instance_id" \
    --display-name "$image_name" \
    --wait-for-state AVAILABLE \
    --max-wait-seconds 3600 \
    --query 'data.id' \
    --raw-output)"

  local image_size_mbs image_size_gb
  image_size_mbs="$(oci compute image get \
    --image-id "$image_id" \
    --query 'data."size-in-mbs"' \
    --raw-output)"
  if ! [[ "${image_size_mbs:-}" =~ ^[0-9]+$ ]]; then
    echo "could not parse image size in MiB for image: $image_id" >&2
    exit 6
  fi
  image_size_gb="$(( (image_size_mbs + 1023) / 1024 ))"

  log "Golden image OCID: $image_id"
  log "Golden image size (reported): ${image_size_gb}GiB"

  if (( image_size_gb > OCI_GOLDEN_MAX_GB )); then
    echo "golden image too large: ${image_size_gb}GiB > limit ${OCI_GOLDEN_MAX_GB}GiB" >&2
    if [[ "$OCI_DELETE_OVERSIZE_IMAGE" == "true" ]]; then
      log "Deleting oversize image to avoid storage cost"
      oci compute image delete --image-id "$image_id" --force >/dev/null || true
    fi
    exit 5
  fi

  if [[ -n "${OCI_OUTPUT_ENV_FILE:-}" ]]; then
    cat >"$OCI_OUTPUT_ENV_FILE" <<EOF
OCI_COMPARTMENT_OCID=$OCI_COMPARTMENT_OCID
OCI_AVAILABILITY_DOMAIN=$OCI_AVAILABILITY_DOMAIN
OCI_SUBNET_OCID=$OCI_SUBNET_OCID
OCI_IMAGE_OCID=$image_id
OCI_IMAGE_SIZE_GB=$image_size_gb
EOF
    log "Wrote image env file: $OCI_OUTPUT_ENV_FILE"
  fi

  test_rc=0
  cat <<EOF
OCI_COMPARTMENT_OCID=$OCI_COMPARTMENT_OCID
OCI_AVAILABILITY_DOMAIN=$OCI_AVAILABILITY_DOMAIN
OCI_SUBNET_OCID=$OCI_SUBNET_OCID
OCI_IMAGE_OCID=$image_id
OCI_IMAGE_SIZE_GB=$image_size_gb
EOF
}

main "$@"
