"""Dedicated erase-only Anaconda entrypoint; it never returns to installation."""

from __future__ import annotations

import base64
import inspect
import ipaddress
import json
import re
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_runtime
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity

RAM = "/run/bootwright-erase"
# The installer runtime keeps python3 but not coreutils' base64 (Lorax
# runtime-cleanup.tmpl), so the %pre decodes its payload with python3.
DECODE = "/usr/bin/python3 -I -c 'import base64,sys;sys.stdout.buffer.write(base64.b64decode(sys.stdin.buffer.read().strip(),validate=True))'"
INTERFACE = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}")
HOSTNAME = re.compile(r"[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?")
BOND_OPTION = re.compile(r"[a-z][a-z0-9_]{0,31}=[A-Za-z0-9_.-]{1,64}")
NETWORK_FLAGS = {"activate"}
NETWORK_FIELDS = {"bootproto", "device", "bondslaves", "bondopts", "vlanid", "interfacename", "ip", "netmask",
                  "hostname", "onboot", "gateway", "nameserver"}


def kickstart(network, request, private_key, authorized_key):
    files = {
        "physical_erase_worker.py": inspect.getsource(physical_erase_worker),
        "bounded_process.py": inspect.getsource(bounded_process),
        "physical_machine_identity.py": inspect.getsource(physical_machine_identity),
        "runtime.py": inspect.getsource(physical_erase_runtime),
        "request.json": json.dumps(request),
        "host-key": private_key,
        "authorized_keys": "restrict " + authorized_key.strip() + "\n",
    }
    lines = [
        network.rstrip(),
        "%pre --erroronfail --interpreter=/bin/bash",
        "umask 077",
        "fail_closed() { trap '' HUP INT TERM; while :; do sleep 60; done; }",
        "trap fail_closed EXIT HUP INT TERM",
        "set -euo pipefail",
        "test ! -e /run/bootwright-erase",
        "mkdir -m 0700 /run/bootwright-erase",
    ]
    for name, data in files.items():
        encoded = base64.b64encode(data.encode()).decode()
        lines.append(DECODE + " > " + RAM + "/" + name + " <<'BOOTWRIGHT_ERASE_DATA'")
        lines.extend((encoded, "BOOTWRIGHT_ERASE_DATA"))
    lines.extend(
        (
            "cat > /run/systemd/system/bootwright-erase-sshd.service <<'BOOTWRIGHT_ERASE_SSH_UNIT'",
            "[Unit]",
            "Description=Bootwright RAM receipt transport",
            "[Service]",
            "Type=simple",
            "ExecStart=/usr/sbin/sshd -D -f /run/bootwright-erase/sshd_config",
            "Restart=no",
            "UMask=0077",
            "StandardOutput=null",
            "StandardError=null",
            "BOOTWRIGHT_ERASE_SSH_UNIT",
            "cat > /run/systemd/system/bootwright-erase.service <<'BOOTWRIGHT_ERASE_UNIT'",
            "[Unit]",
            "Description=Bootwright authorized offline logical overwrite",
            "[Service]",
            "Type=simple",
            "ExecStart=/usr/bin/python3 /run/bootwright-erase/runtime.py",
            "Restart=no",
            "UMask=0077",
            "StandardOutput=null",
            "StandardError=null",
            "BOOTWRIGHT_ERASE_UNIT",
            "systemctl daemon-reload",
            "systemctl start --no-block bootwright-erase.service",
            "fail_closed",
            "%end",
            "",
        )
    )
    return "\n".join(lines)


def network_fields(network):
    """The options of the frozen install's one static Kickstart network line, or refuse."""
    lines = network.splitlines() if isinstance(network, str) else []
    words = lines[0].split() if len(lines) == 1 else []
    if not words or words[0] != "network":
        raise physical_erase_worker.Refusal("install-network")
    fields = {}
    for word in words[1:]:
        key, separator, value = word.partition("=")
        name = key[2:]
        known = NETWORK_FIELDS if separator else NETWORK_FLAGS
        if not key.startswith("--") or name not in known or name in fields or (separator and not value):
            raise physical_erase_worker.Refusal("install-network")
        fields[name] = value
    if fields.get("bootproto") != "static" or not {"device", "ip", "netmask", "hostname"} <= set(fields):
        raise physical_erase_worker.Refusal("install-network")
    return fields


def network_arguments(network):
    """The dracut arguments that raise the frozen install network before Anaconda starts.

    Anaconda applies Kickstart network only after %pre returns, and the erase
    %pre never returns; dracut's own Kickstart reader drops a VLAN. So the
    erase image carries the same interface, bond, VLAN and address as kernel
    arguments the initramfs network applies before the installer starts. The
    worker resolves no name, so the line's resolvers are not carried.
    """
    fields = network_fields(network)
    try:
        address = ipaddress.IPv4Address(fields["ip"])
        mask = ipaddress.IPv4Network("0.0.0.0/" + fields["netmask"]).netmask
        gateway = str(ipaddress.IPv4Address(fields["gateway"])) if "gateway" in fields else ""
    except ValueError:
        raise physical_erase_worker.Refusal("install-network") from None
    device, hostname = fields["device"], fields["hostname"]
    if not INTERFACE.fullmatch(device) or not HOSTNAME.fullmatch(hostname):
        raise physical_erase_worker.Refusal("install-network")
    arguments, interface = [], device
    if "bondslaves" in fields or "bondopts" in fields:
        ports, options = fields.get("bondslaves", "").split(","), fields.get("bondopts", "").split(",")
        if (len(set(ports)) != len(ports) or not all(INTERFACE.fullmatch(port) for port in ports)
                or not all(BOND_OPTION.fullmatch(option) for option in options) or not options[0].startswith("mode=")):
            raise physical_erase_worker.Refusal("install-network")
        arguments.append("bond=" + device + ":" + ",".join(ports) + ":" + ",".join(options))
    if "vlanid" in fields or "interfacename" in fields:
        vlan = fields.get("vlanid", "")
        if not INTERFACE.fullmatch(fields.get("interfacename", "")) or not re.fullmatch("[1-9][0-9]{0,3}", vlan) or int(vlan) > 4094:
            raise physical_erase_worker.Refusal("install-network")
        # The initramfs reads a VLAN's ID from its name's trailing number, never
        # from --vlanid, so the image names the VLAN <device>.<id>, or
        # vlan<id> when that is longer than an interface name may be.
        interface = device + "." + fields["vlanid"]
        if len(interface) > 15:
            interface = "vlan" + fields["vlanid"]
        arguments.append("vlan=" + interface + ":" + device)
    arguments.append("ip=" + ":".join((str(address), "", gateway, str(mask), hostname, interface, "none")))
    arguments.append("rd.neednet=1")
    return arguments
