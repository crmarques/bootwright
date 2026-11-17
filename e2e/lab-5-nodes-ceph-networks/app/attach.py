"""Parsing, mounting and configuration steps of app-setup.sh.

Run by app-setup.sh as `python3 -I attach.py STEP ARGUMENT...` on RHEL 9
(Python 3.9, standard library only):

    keyring FILE
        print the base64 key of a keyring file, or the file's text when it
        holds a bare key
    monitors CEPH_CONF
        print the MONs' msgr v1 addresses as ip:port,...
    mount MONITORS PATH CLIENT SECRET_FILE FS_NAME TARGET
        mount the CephFS subvolume with mount(2), so the key never appears
        in a process argument and no mount.ceph helper is needed
    config MONITORS LISTEN PORT BLOCK_DIR FILE_DIR S3_VIP S3_HOST S3_BUCKET
           S3_REGION ETC
        write ETC/config.json for sample-server.py
"""

import ctypes
import json
import os
import re
import sys

STEPS = {"keyring": 1, "monitors": 1, "mount": 6, "config": 10}


def keyring(path):
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    match = re.search(r"^\s*key\s*=\s*(\S+)", text, re.M)
    return match.group(1) if match else text.strip()


def monitors(path):
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    found = re.search(r"^\s*mon_host\s*=\s*(.+)$", text, re.M)
    if not found:
        raise SystemExit("%s names no mon_host" % path)
    line = found.group(1)
    pairs = re.findall(r"v1:([0-9.]+):([0-9]+)", line)
    if not pairs:
        pairs = [(ip, "6789") for ip in re.findall(r"\b([0-9]+(?:\.[0-9]+){3})\b", line)]
    return ",".join(ip + ":" + port for ip, port in pairs)


def mount(monitor_list, path, client, secret_file, fs_name, target):
    with open(secret_file, encoding="utf-8") as handle:
        secret = handle.read().strip()
    options = "name=%s,secret=%s,mds_namespace=%s" % (client, secret, fs_name)
    libc = ctypes.CDLL("libc.so.6", use_errno=True)
    if libc.mount((monitor_list + ":" + path).encode(), target.encode(), b"ceph", 0, options.encode()) != 0:
        raise SystemExit("CephFS mount failed: %s" % os.strerror(ctypes.get_errno()))


def config(monitor_list, listen, port, block_dir, file_dir, vip, host, bucket, region, etc):
    document = {
        "listen": listen,
        "port": int(port),
        "blockDir": block_dir,
        "fileDir": file_dir,
        "monitors": [pair.split(":")[0] for pair in monitor_list.split(",")],
        "s3": {
            "vip": vip,
            "host": host,
            "bucket": bucket,
            "region": region,
            "caFile": etc + "/rgw-ca.pem",
            "credentialsFile": etc + "/s3.json",
        },
    }
    with open(etc + "/config.json", "w", encoding="utf-8") as handle:
        json.dump(document, handle, indent=2, sort_keys=True)


def main(argv):
    if len(argv) < 2 or argv[1] not in STEPS or len(argv) != STEPS[argv[1]] + 2:
        sys.exit("usage: attach.py keyring FILE | monitors CEPH_CONF | mount MONITORS PATH CLIENT SECRET_FILE FS_NAME TARGET | config MONITORS LISTEN PORT BLOCK_DIR FILE_DIR S3_VIP S3_HOST S3_BUCKET S3_REGION ETC")
    step, arguments = argv[1], argv[2:]
    if step == "keyring":
        sys.stdout.write(keyring(*arguments))
    elif step == "monitors":
        print(monitors(*arguments))
    elif step == "mount":
        mount(*arguments)
    else:
        config(*arguments)


if __name__ == "__main__":
    main(sys.argv)
