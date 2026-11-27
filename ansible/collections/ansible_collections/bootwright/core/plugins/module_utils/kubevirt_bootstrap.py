"""A KubeVirt clone's NoCloud payload and authenticated first-boot reading."""

from __future__ import annotations

import base64
import hashlib
import ipaddress
import re
import tempfile
from urllib.parse import urlsplit

from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh as guest
from ansible_collections.bootwright.core.plugins.module_utils import private_files, ssh_key_policy
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Refused

FLEET_USER = "bootwright"
BOOT_DIR = "/run/bootwright-bootstrap"
MARKER = re.compile(r"[0-9a-f]{64}")
RELEASE = re.compile(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)")
MAC = re.compile(r"(?:[0-9a-f]{2}:){5}[0-9a-f]{2}")
HOST_TYPES = {"ssh-ed25519": "ed25519", "ecdsa-sha2-nistp256": "ecdsa", "ssh-rsa": "rsa"}
INTENT_FIELDS = {"address", "gateway", "growRootFilesystem", "hostname", "mac", "marker", "nameservers", "release"}
FETCH = """set -eu
umask 077
trap 'rm -f /run/bootwright-bootstrap/host.key /run/bootwright-bootstrap/fetch.conf' EXIT
/usr/bin/curl -q --config /run/bootwright-bootstrap/fetch.conf
test "$(ssh-keygen -y -f /run/bootwright-bootstrap/host.key | cut -d ' ' -f 1,2)" = "$(cat /run/bootwright-bootstrap/expected.pub)"
install -m 0600 /run/bootwright-bootstrap/host.key "/etc/ssh/ssh_host_${1}_key"
install -m 0644 /run/bootwright-bootstrap/expected.pub "/etc/ssh/ssh_host_${1}_key.pub"
restorecon "/etc/ssh/ssh_host_${1}_key" "/etc/ssh/ssh_host_${1}_key.pub"
install -d -m 0755 /var/lib/bootwright
install -m 0644 /run/bootwright-bootstrap/marker /var/lib/bootwright/template-bootstrap
systemctl restart sshd.service
"""

CHECK_CLOUD_INIT = "/usr/bin/cloud-init status --wait >/dev/null 2>&1\n"
VERIFY = """printf '%s\n' "$(cat /var/lib/bootwright/template-bootstrap)"
printf '%s\n' "$(uname -m)"
sed -n 's/^ID=//p' /etc/os-release | tr -d '"'
sed -n 's/^VERSION_ID=//p' /etc/os-release | tr -d '"'
/usr/bin/hostname
cat /etc/machine-id
/usr/sbin/ip -o -4 addr show scope global | awk '{print $4}'
"""


def checked_intent(intent):
    """Return the one supported bootstrap network and refuse before rendering."""
    if not isinstance(intent, dict) or set(intent) != INTENT_FIELDS:
        raise Refused("the template bootstrap intent has missing or unknown fields")
    try:
        address = ipaddress.IPv4Interface(intent["address"])
        gateway = ipaddress.IPv4Address(intent["gateway"])
        if (not isinstance(intent["nameservers"], list) or len(intent["nameservers"]) > 8
                or any(not isinstance(server, str) or str(ipaddress.IPv4Address(server)) != server for server in intent["nameservers"])):
            raise ValueError("name servers")
        if (not isinstance(intent["hostname"], str) or len(intent["hostname"]) > 253
                or not all(guest.LABEL.fullmatch(part) for part in intent["hostname"].split("."))
                or str(address) != intent["address"] or str(gateway) != intent["gateway"] or gateway not in address.network
                or gateway == address.ip or not MAC.fullmatch(intent["mac"]) or not MARKER.fullmatch(intent["marker"])
                or not RELEASE.fullmatch(intent["release"]) or not isinstance(intent["growRootFilesystem"], bool)):
            raise ValueError("bootstrap settings")
    except (TypeError, ValueError):
        raise Refused("the template bootstrap requires a pinned MAC, hostname, static IPv4 network, and exact release") from None
    return address


def public_key(line):
    if not isinstance(line, str) or not line.isascii() or not line.isprintable() or len(line) > 8192:
        raise Refused("the bootstrap public key is invalid")
    fields = line.split()
    try:
        ssh_key_policy.pinned_algorithms(line)
        wire = base64.b64decode(fields[1], validate=True)
        size = int.from_bytes(wire[:4], "big")
        if size == 0 or len(wire) <= 4 + size or wire[4:4 + size] != fields[0].encode("ascii"):
            raise ValueError("key type")
    except (IndexError, ValueError):
        raise Refused("the bootstrap public key is invalid") from None
    return " ".join(fields[:2])


def fetch_configuration(value):
    try:
        url = urlsplit(value)
        parts = url.path.split("/")
        if (not value.isascii() or not value.isprintable() or len(value) > 2048 or any(char in value for char in '\"\\ ')
                or url.scheme != "https" or not url.hostname or url.username is not None or url.password is not None
                or url.query or url.fragment or len(parts) < 4 or parts[1] != "private"
                or parts[-1] != "identity" or not MARKER.fullmatch(parts[-2])
                or any(part in ("", ".", "..") or not re.fullmatch(r"[a-zA-Z0-9._-]+", part) for part in parts[1:])):
            raise ValueError("publication URL")
    except (AttributeError, TypeError, ValueError):
        raise Refused("the template bootstrap requires a private HTTPS identity URL") from None
    return ('fail\nsilent\nproto = "=https"\nnoproxy = "*"\nmax-time = 30\nmax-filesize = 16384\n'
            'cacert = "/run/bootwright-bootstrap/ca.pem"\noutput = "/run/bootwright-bootstrap/host.key"\n'
            'url = "' + value + '"\n')


def seed(intent, fleet_key, host_key, certificate, fetch_url):
    """Emit only the NoCloud user and network data sent inside a Secret."""
    checked_intent(intent)
    fleet_key, host_key = public_key(fleet_key), public_key(host_key)
    host_type = HOST_TYPES.get(host_key.split()[0])
    if host_type is None:
        raise Refused("the template bootstrap host key algorithm is unsupported")
    if not isinstance(certificate, str) or len(certificate) > 16384 or not certificate.startswith("-----BEGIN CERTIFICATE-----\n"):
        raise Refused("the template bootstrap has no serving certificate")
    files = (
        ("ca.pem", certificate, "0644"), ("expected.pub", host_key + "\n", "0644"),
        ("marker", intent["marker"] + "\n", "0644"), ("fetch.conf", fetch_configuration(fetch_url), "0600"),
    )
    grow = intent["growRootFilesystem"]
    config = {
        "disable_root": True, "ssh_pwauth": False, "ssh_deletekeys": True, "ssh_genkeytypes": [], "allow_public_ssh_keys": False,
        "preserve_hostname": False, "fqdn": intent["hostname"], "prefer_fqdn_over_hostname": True, "manage_etc_hosts": True,
        "users": [{"name": FLEET_USER, "lock_passwd": True, "shell": "/bin/bash",
                   "sudo": "ALL=(ALL) NOPASSWD:ALL", "ssh_authorized_keys": [fleet_key]}],
        "write_files": [{"path": BOOT_DIR + "/" + name, "content": value, "permissions": mode} for name, value, mode in files],
        "runcmd": [["/usr/bin/sh", "-c", FETCH, "bootwright-bootstrap", host_type]],
        "growpart": {"mode": "auto" if grow else "off", "devices": ["/"]}, "resize_rootfs": grow,
    }
    network = {"version": 2, "ethernets": {"eth0": {
        "match": {"macaddress": intent["mac"]}, "dhcp4": False, "addresses": [intent["address"]],
        "routes": [{"to": "0.0.0.0/0", "via": intent["gateway"]}],
        "nameservers": {"addresses": intent["nameservers"]},
    }}}
    return {"userdata": base64.b64encode(b"#cloud-config\n" + canonical(config) + b"\n").decode("ascii"),
            "networkdata": base64.b64encode(canonical(network)).decode("ascii")}


def material_seed(intent, fleet_key, host_key, certificate_file, fetch_url_file):
    try:
        certificate = private_files.read(certificate_file, 16384).decode("ascii")
        url = private_files.read(fetch_url_file, 2048).decode("ascii").removesuffix("\n")
    except (OSError, UnicodeError, private_files.Refused):
        raise Refused("the private template bootstrap input could not be read") from None
    return seed(intent, fleet_key, host_key, certificate, url)


def verify_answer(answer, intent):
    """Only an exact pinned guest response can become readiness evidence."""
    if answer.host_key:
        raise Refused("the guest did not present its pinned template host key")
    if answer.code != 0 or answer.timed_out or answer.stdout is None:
        raise Refused("the pinned guest did not complete cloud-init")
    try:
        lines = answer.stdout.decode("ascii").splitlines()
    except UnicodeError:
        raise Refused("the first-boot proof has an invalid format") from None
    expected = [intent["marker"], "x86_64", "rhel", intent["release"], intent["hostname"]]
    if (len(lines) < 7 or lines[:5] != expected or not re.fullmatch(r"[0-9a-f]{32}", lines[5])
            or lines[5] == "0" * 32 or intent["address"] not in lines[6:]):
        raise Refused("the pinned guest did not prove the expected clone, OS release, hostname and address")
    return {"marker": intent["marker"], "architecture": "x86_64", "family": "rhel", "release": intent["release"],
            "address": intent["address"], "hostname": intent["hostname"], "postcondition": True,
            "guestIdentity": hashlib.sha256((intent["marker"] + lines[5]).encode("ascii")).hexdigest()}


def verify_guest(intent, scratch, identity, host_key,
                 policy=lambda: ssh_key_policy.effective_policy(ssh_key_policy.read_policy()), run=guest.run, *, seeded=True):
    address = str(checked_intent(intent).ip)
    try:
        with tempfile.TemporaryDirectory(prefix="clone-proof-", dir=scratch) as directory:
            session = guest.open_session(directory, address, public_key(host_key), policy())
            argv = guest.guest_argv(
                **session, identity=identity, user=FLEET_USER, address=address,
                remote_argv=("/usr/bin/sh", "-c", "set -eu\n" + (CHECK_CLOUD_INIT if seeded else "") + VERIFY),
            )
            return verify_answer(run(argv, None, 45), intent)
    except (OSError, guest.SessionError, ssh_key_policy.PolicyError):
        raise Refused("the pinned template SSH connection could not be established") from None
