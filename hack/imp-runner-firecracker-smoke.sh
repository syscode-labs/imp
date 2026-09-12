#!/usr/bin/env bash
# Compose an Imp runner rootfs from OCI layers, boot it with Firecracker, and
# exercise the guest's runner, network, DNS, CA, and Git HTTPS prerequisites.
set -euo pipefail

BASE_IMAGE=${BASE_IMAGE:?set BASE_IMAGE to the base OCI image}
RUNNER_IMAGE=${RUNNER_IMAGE:?set RUNNER_IMAGE to the runner-layer OCI image}
FIRECRACKER_KERNEL=${FIRECRACKER_KERNEL:?set FIRECRACKER_KERNEL to an uncompressed x86_64 kernel}
FIRECRACKER_BIN=${FIRECRACKER_BIN:-firecracker}
FIRECRACKER_VERSION=${FIRECRACKER_VERSION:-v1.15.0}
GUEST_IP=${GUEST_IP:-172.16.0.2}
HOST_IP=${HOST_IP:-172.16.0.1}
TAP_NAME=${TAP_NAME:-imp-fc-smoke0}

fail() { printf 'firecracker smoke failure: %s\n' "$*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"; }
for cmd in docker python3 mke2fs ip "$FIRECRACKER_BIN"; do require_cmd "$cmd"; done
[[ -r "$FIRECRACKER_KERNEL" ]] || fail "kernel is not readable: $FIRECRACKER_KERNEL"
[[ -e /dev/kvm ]] || fail '/dev/kvm is unavailable (run this job on a KVM-capable Linux runner)'
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || fail 'Firecracker smoke requires Linux x86_64'

workdir=$(mktemp -d)
tap_created=0
nat_rule_added=0
old_forward=''
firecracker_pid=''
# shellcheck disable=SC2317
cleanup() {
  set +e
  [[ -n "$firecracker_pid" ]] && kill "$firecracker_pid" 2>/dev/null
  if (( nat_rule_added )); then
    iptables -t nat -D POSTROUTING -s "$GUEST_IP/32" ! -o "$TAP_NAME" -j MASQUERADE 2>/dev/null
  fi
  [[ -n "$old_forward" ]] && sysctl -w net.ipv4.ip_forward="$old_forward" >/dev/null 2>&1
  if (( tap_created )); then
    ip link set "$TAP_NAME" down 2>/dev/null
    ip tuntap del dev "$TAP_NAME" mode tap 2>/dev/null
  fi
  rm -rf "$workdir"
}
trap cleanup EXIT

# docker save retains the OCI/Docker layer sequence. The Python extractor below
# applies each layer in order, including Docker whiteouts, rather than relying
# on a container export that would hide the composition under test.
extract_image() {
  local image=$1 output=$2 archive
  archive="$workdir/$(printf '%s' "$2" | tr '/:' '__').tar"
  docker image inspect "$image" >/dev/null || fail "image is not available locally: $image"
  docker save "$image" -o "$archive"
  python3 - "$archive" "$output" <<'PY'
import json, os, pathlib, shutil, sys, tarfile

archive, destination = sys.argv[1:]
root = pathlib.Path(destination)
root.mkdir(parents=True, exist_ok=True)
with tarfile.open(archive) as saved:
    manifest = json.loads(saved.extractfile('manifest.json').read())
    layers = manifest[0]['Layers']
    for layer_name in layers:
        with tarfile.open(fileobj=saved.extractfile(layer_name)) as layer:
            for member in layer:
                name = member.name.lstrip('./')
                if not name or name == '.':
                    continue
                target = root / name
                try:
                    target.relative_to(root)
                except ValueError:
                    raise RuntimeError(f'unsafe layer path: {member.name}')
                parent = target.parent
                parent.mkdir(parents=True, exist_ok=True)
                basename = target.name
                if basename == '.wh..wh..opq':
                    for child in parent.iterdir():
                        if child.is_dir() and not child.is_symlink():
                            shutil.rmtree(child)
                        else:
                            child.unlink()
                    continue
                if basename.startswith('.wh.'):
                    victim = parent / basename[4:]
                    if victim.is_dir() and not victim.is_symlink():
                        shutil.rmtree(victim)
                    else:
                        victim.unlink(missing_ok=True)
                    continue
                if target.exists() or target.is_symlink():
                    if target.is_dir() and not target.is_symlink():
                        shutil.rmtree(target)
                    else:
                        target.unlink()
                layer.extract(member, root)
PY
}

rootfs_dir="$workdir/rootfs"
extract_image "$BASE_IMAGE" "$rootfs_dir"
extract_image "$RUNNER_IMAGE" "$rootfs_dir"
[[ -x "$rootfs_dir/usr/local/bin/runner" ]] || fail 'composite rootfs lacks executable /usr/local/bin/runner'
[[ -x "$rootfs_dir/home/runner/actions-runner/run.sh" ]] || fail 'composite rootfs lacks actions runner run.sh'
[[ -x "$rootfs_dir/usr/bin/git" ]] || fail 'composite rootfs lacks /usr/bin/git'
[[ -r "$rootfs_dir/etc/ssl/certs/ca-certificates.crt" ]] || fail 'composite rootfs lacks CA bundle'

# Replace the image entrypoint only for this smoke. It runs as PID 1, so every
# assertion below is made from inside the Firecracker guest, not the host.
cat > "$rootfs_dir/sbin/init" <<'INIT'
#!/bin/sh
set -eu
mkdir -p /proc /sys /dev/pts /run
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devpts devpts /dev/pts
mount -t tmpfs tmpfs /run
ln -sf /proc/net/pnp /etc/resolv.conf
printf 'IMP_FIRECRACKER_SMOKE=booted\n'
test -x /usr/local/bin/runner
printf 'IMP_FIRECRACKER_SMOKE=runner-binary\n'
test -x /usr/bin/git
test -r /etc/ssl/certs/ca-certificates.crt
printf 'IMP_FIRECRACKER_SMOKE=ca\n'
getent hosts github.com
printf 'IMP_FIRECRACKER_SMOKE=dns\n'
git ls-remote https://github.com/actions/runner.git HEAD >/dev/null
printf 'IMP_FIRECRACKER_SMOKE=git-https\n'
# Keep PID 1 alive while the host observes all markers, then let the host stop FC.
while :; do sleep 1; done
INIT
chmod 0755 "$rootfs_dir/sbin/init"

rootfs="$workdir/imp-runner-rootfs.ext4"
size_mib=${ROOTFS_SIZE_MIB:-2048}
mke2fs -t ext4 -d "$rootfs_dir" -F "$rootfs" "${size_mib}m" >/dev/null

# The isolated tap and NAT are deliberately explicit. This is also the point
# where a hosted runner without CAP_NET_ADMIN fails with an actionable error.
ip tuntap add dev "$TAP_NAME" mode tap || fail "could not create tap $TAP_NAME (CAP_NET_ADMIN/root required)"
tap_created=1
ip addr add "$HOST_IP/24" dev "$TAP_NAME"
ip link set "$TAP_NAME" up
if command -v sysctl >/dev/null 2>&1; then
  old_forward="$(sysctl -n net.ipv4.ip_forward 2>/dev/null || true)"
  sysctl -w net.ipv4.ip_forward=1 >/dev/null || fail 'could not enable IPv4 forwarding'
fi
if command -v iptables >/dev/null 2>&1; then
  iptables -t nat -A POSTROUTING -s "$GUEST_IP/32" ! -o "$TAP_NAME" -j MASQUERADE
  nat_rule_added=1
else
  fail 'iptables is required for guest egress NAT'
fi

config="$workdir/firecracker.json"
log_fifo="$workdir/firecracker.log"
mkfifo "$log_fifo"
cat "$log_fifo" > "$workdir/firecracker.log.capture" &
logger_pid=$!
python3 - "$config" "$rootfs" "$FIRECRACKER_KERNEL" "$TAP_NAME" "$log_fifo" <<'PY'
import json, sys
config, rootfs, kernel, tap, log_fifo = sys.argv[1:]
with open(config, 'w', encoding='utf-8') as out:
    json.dump({
        'boot-source': {'kernel_image_path': kernel, 'boot_args': 'console=ttyS0 reboot=k panic=1 pci=off ip=172.16.0.2::172.16.0.1:255.255.255.0::eth0:off'},
        'drives': [{'drive_id': 'rootfs', 'path_on_host': rootfs, 'is_root_device': True, 'is_read_only': False}],
        'network-interfaces': [{'iface_id': 'eth0', 'guest_mac': '06:00:AC:10:00:02', 'host_dev_name': tap}],
        'machine-config': {'vcpu_count': 2, 'mem_size_mib': 1024, 'smt': False},
        'logger': {'log_fifo': log_fifo, 'level': 'Info', 'show_level': True, 'show_log_origin': True},
    }, out)
PY

"$FIRECRACKER_BIN" --no-api --config-file "$config" >/dev/null 2>&1 &
firecracker_pid=$!
for _ in $(seq 1 60); do
  if grep -q 'IMP_FIRECRACKER_SMOKE=git-https' "$workdir/firecracker.log.capture"; then
    printf 'firecracker smoke passed: composite rootfs, runner binary, boot, CA, DNS, and Git HTTPS\n'
    kill "$firecracker_pid" "$logger_pid" 2>/dev/null || true
    wait "$firecracker_pid" 2>/dev/null || true
    exit 0
  fi
  if ! kill -0 "$firecracker_pid" 2>/dev/null; then
    printf '%s\n' 'Firecracker exited before guest smoke completed:' >&2
    sed -n '/IMP_FIRECRACKER_SMOKE=/p' "$workdir/firecracker.log.capture" >&2
    exit 1
  fi
  sleep 1
done
printf '%s\n' 'timed out waiting for guest smoke markers; Firecracker log:' >&2
sed -n '/IMP_FIRECRACKER_SMOKE=/p' "$workdir/firecracker.log.capture" >&2
exit 1
