"""Run the shipped machine hardware claim through ansible-core, then replay it.

`playbooks/machine/hardware_apply.yml` runs the machine hardware role over the
real runner protocol. Its read changes nothing and runs no command: it lists
the kernel's network interfaces under /sys/class/net, which here are the gate
host's own, read-only, and asks each device-backed one for its permanent
address. The claim publishes an unchanged `completed` record, and a replay
must publish the same addresses and change nothing.
"""

from pathlib import Path
import re
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "setup_entry_point"))
import entry_point  # noqa: E402  pylint: disable=wrong-import-position

DIGEST = "c" * 64
ADDRESS = re.compile(r"[0-9a-f]{2}(:[0-9a-f]{2}){5}")
VARIABLES = {
    "bootwright_machine_hardware_request": {
        "version": "machine-hardware-v1",
        "identity": {"context": "example", "object": "target", "block": "hardware"},
        "placement": {"connection": "ssh", "machine": "target", "address": "192.0.2.10", "port": 22, "user": "root"},
    },
    "bootwright_machine_hardware_digest": DIGEST,
    "bootwright_machine_hardware_material": {},
}


def check(record):
    evidence = record["evidence"]
    if evidence["request"] != DIGEST or evidence["version"] != "machine-hardware-evidence-v1" or evidence["absent"]:
        raise entry_point.Failure("the completed evidence is not attributable to the claim")
    if not all(ADDRESS.fullmatch(address) for address in evidence["addresses"]):
        raise entry_point.Failure("the claim published an address that is no hardware address")
    return evidence


with tempfile.TemporaryDirectory(prefix="bootwright-machine-hardware-") as work:
    entry_point.replay(entry_point.EntryPoint(work, "machine/hardware_apply.yml"), VARIABLES, check, first_outcome="unchanged")
print("PASS machine hardware claim published its addresses and a replay changed nothing")
