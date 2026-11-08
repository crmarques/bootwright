#!/bin/bash
#
# Attach app-01 to the three Ceph stores and start the sample server.
# Run as root from the bundle directory the runbook copies to app-01:
#
#     sudo -n bash e2e-sample/app-setup.sh
#
# The bundle holds sample.env, ceph.conf (the cluster's minimal
# configuration), cephfs-path, app-block.keyring, app-files.keyring, s3.json,
# rgw-ca.pem and sample-server.py. It maps the RBD image and mounts the CephFS
# subvolume with the kernel clients, so no Ceph package is installed here, and
# every key reaches the kernel from a root-only file, never from an argument.
# A repeated run keeps what is already attached.
set -euo pipefail
umask 077

bundle=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=/dev/null
. "$bundle/sample.env"
: "${USER_ADDRESS:?}" "${PORT:?}" "${USER_CIDR:?}" "${RBD_POOL:?}" "${RBD_IMAGE:?}"
: "${BLOCK_CLIENT:?}" "${FILE_CLIENT:?}" "${FS_NAME:?}"
: "${S3_VIP:?}" "${S3_HOST:?}" "${S3_BUCKET:?}" "${S3_REGION:?}"

etc=/etc/e2e-sample
lib=/usr/local/libexec/e2e-sample
block_dir=/srv/ceph-block
file_dir=/srv/ceph-file
unit=e2e-sample-server

install -d -m 0700 "$etc"
install -d -m 0755 "$lib" "$block_dir" "$file_dir"
install -m 0644 "$bundle/sample-server.py" "$lib/sample-server.py"
install -m 0600 "$bundle/s3.json" "$etc/s3.json"
install -m 0644 "$bundle/rgw-ca.pem" "$etc/rgw-ca.pem"

# The base64 key of a keyring file, or the file's text when it holds a bare key.
for client in "$BLOCK_CLIENT" "$FILE_CLIENT"; do
    /usr/bin/python3 -I -c '
import re, sys
text = open(sys.argv[1]).read()
match = re.search(r"^\s*key\s*=\s*(\S+)", text, re.M)
sys.stdout.write(match.group(1) if match else text.strip())
' "$bundle/$client.keyring" > "$etc/$client.secret"
    test -s "$etc/$client.secret"
done

# The MONs' msgr v1 addresses (ip:port,...), the kernel clients' default protocol.
monitors=$(/usr/bin/python3 -I -c '
import re, sys
text = open(sys.argv[1]).read()
line = re.search(r"^\s*mon_host\s*=\s*(.+)$", text, re.M).group(1)
pairs = re.findall(r"v1:([0-9.]+):([0-9]+)", line)
if not pairs:
    pairs = [(ip, "6789") for ip in re.findall(r"\b([0-9]+(?:\.[0-9]+){3})\b", line)]
print(",".join(ip + ":" + port for ip, port in pairs))
' "$bundle/ceph.conf")
test -n "$monitors"
printf 'MON addresses: %s\n' "$monitors"

modprobe rbd
modprobe ceph

mapped_device() {
    local device
    for device in /sys/bus/rbd/devices/*; do
        [ -e "$device/name" ] || continue
        if [ "$(cat "$device/pool")" = "$RBD_POOL" ] && [ "$(cat "$device/name")" = "$RBD_IMAGE" ]; then
            printf '/dev/rbd%s\n' "${device##*/}"
            return 0
        fi
    done
    return 1
}

if ! device=$(mapped_device); then
    add=/sys/bus/rbd/add_single_major
    [ -e "$add" ] || add=/sys/bus/rbd/add
    printf '%s name=%s,secret=%s %s %s\n' "$monitors" "$BLOCK_CLIENT" "$(cat "$etc/$BLOCK_CLIENT.secret")" \
        "$RBD_POOL" "$RBD_IMAGE" > "$add"
    udevadm settle
    device=$(mapped_device)
fi
printf 'RBD image %s/%s mapped at %s\n' "$RBD_POOL" "$RBD_IMAGE" "$device"

if ! mountpoint -q "$block_dir"; then
    fstype=$(blkid -p -o value -s TYPE "$device" || true)
    if [ -z "$fstype" ]; then
        mkfs.xfs -q "$device"
    elif [ "$fstype" != xfs ]; then
        printf 'refusing to format %s: it already holds %s\n' "$device" "$fstype" >&2
        exit 1
    fi
    mount -t xfs "$device" "$block_dir"
fi

# CephFS through mount(2) directly, so the key never appears in a process
# argument and no mount.ceph helper is needed.
if ! mountpoint -q "$file_dir"; then
    /usr/bin/python3 -I -c '
import ctypes, os, sys
monitors, path, client, secret_file, fs_name, target = sys.argv[1:]
secret = open(secret_file).read().strip()
options = "name=%s,secret=%s,mds_namespace=%s" % (client, secret, fs_name)
libc = ctypes.CDLL("libc.so.6", use_errno=True)
if libc.mount((monitors + ":" + path).encode(), target.encode(), b"ceph", 0, options.encode()) != 0:
    error = ctypes.get_errno()
    sys.exit("CephFS mount failed: %s" % os.strerror(error))
' "$monitors" "$(cat "$bundle/cephfs-path")" "$FILE_CLIENT" "$etc/$FILE_CLIENT.secret" "$FS_NAME" "$file_dir"
fi
findmnt -n -o SOURCE,FSTYPE,TARGET "$block_dir"
findmnt -n -o SOURCE,FSTYPE,TARGET "$file_dir"

/usr/bin/python3 -I -c '
import json, sys
monitors = [pair.split(":")[0] for pair in sys.argv[1].split(",")]
config = {
    "listen": sys.argv[2], "port": int(sys.argv[3]),
    "blockDir": sys.argv[4], "fileDir": sys.argv[5], "monitors": monitors,
    "s3": {"vip": sys.argv[6], "host": sys.argv[7], "bucket": sys.argv[8], "region": sys.argv[9],
           "caFile": sys.argv[10] + "/rgw-ca.pem", "credentialsFile": sys.argv[10] + "/s3.json"},
}
with open(sys.argv[10] + "/config.json", "w") as handle:
    json.dump(config, handle, indent=2, sort_keys=True)
' "$monitors" "$USER_ADDRESS" "$PORT" "$block_dir" "$file_dir" "$S3_VIP" "$S3_HOST" "$S3_BUCKET" "$S3_REGION" "$etc"

# Admit the server port from the user network only, until the next reload.
rule="rule family=ipv4 source address=$USER_CIDR port port=$PORT protocol=tcp accept"
firewall-cmd --query-rich-rule="$rule" >/dev/null || firewall-cmd --add-rich-rule="$rule"

systemctl stop "$unit" 2>/dev/null || true
systemctl reset-failed "$unit" 2>/dev/null || true
systemd-run --unit "$unit" --property Restart=on-failure --property RestartSec=2 \
    /usr/bin/python3 -I "$lib/sample-server.py" "$etc/config.json"

for _ in $(seq 1 30); do
    if curl -fsS --noproxy '*' "http://$USER_ADDRESS:$PORT/healthz"; then
        exit 0
    fi
    sleep 2
done
journalctl -u "$unit" -n 50 --no-pager >&2
exit 1
