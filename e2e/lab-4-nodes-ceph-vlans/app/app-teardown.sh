#!/bin/bash
#
# Stop the sample server and detach app-01 from the Ceph stores, keeping the
# data in Ceph. Run as root from the bundle directory:
#
#     sudo -n bash e2e-sample/app-teardown.sh
#
# app-setup.sh attaches the same stores again. A repeated run is harmless.
set -euo pipefail

bundle=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=/dev/null
. "$bundle/sample.env"
: "${PORT:?}" "${USER_CIDR:?}" "${RBD_POOL:?}" "${RBD_IMAGE:?}"

unit=e2e-sample-server
systemctl stop "$unit" 2>/dev/null || true
systemctl reset-failed "$unit" 2>/dev/null || true

rule="rule family=ipv4 source address=$USER_CIDR port port=$PORT protocol=tcp accept"
if firewall-cmd --query-rich-rule="$rule" >/dev/null; then
    firewall-cmd --remove-rich-rule="$rule"
fi

for target in /srv/ceph-file /srv/ceph-block; do
    if mountpoint -q "$target"; then
        umount "$target"
    fi
done

remove=/sys/bus/rbd/remove_single_major
[ -e "$remove" ] || remove=/sys/bus/rbd/remove
for device in /sys/bus/rbd/devices/*; do
    [ -e "$device/name" ] || continue
    if [ "$(cat "$device/pool")" = "$RBD_POOL" ] && [ "$(cat "$device/name")" = "$RBD_IMAGE" ]; then
        printf '%s\n' "${device##*/}" > "$remove"
    fi
done

rm -rf -- /etc/e2e-sample /usr/local/libexec/e2e-sample
printf 'app-01 detached; the data stays in Ceph\n'
