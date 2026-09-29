"""The agent configuration reaches the installer with every value it froze.

The media request freezes each node's root-device hints with their declared
types (internal/containercluster/agentinstall, projection.go). The installer
reads agent-config.yaml with sigs.k8s.io/yaml, whose YAML 1.1 resolver reads a
plain 1e3, 0o17 or 0987654321 as a number and a plain y or n as a boolean and
hands the installer different text, while to_nice_yaml leaves exactly those
scalars plain (.agents/knowledge/installer-input-yaml-scalars.md). The role
therefore writes the file as JSON, which is YAML whose every string is quoted,
and this check renders the real task over such values and reads them back.
That reader also refuses the surrogate-pair escape JSON writes for a code point
above U+FFFF, so such a code point must reach the file as the eight-digit YAML
escape that reader accepts.

Rendering the role's task file needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins.
"""

from __future__ import annotations

import json
import pathlib
import re

import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

MEDIA = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_media_agent"
LOADER = DataLoader()

EVERY_HINT = {
    "hostname": "master-0",
    "role": "master",
    "rootDeviceHints": {
        "deviceName": "/dev/disk/by-path/pci-0000:00:04.0",
        "hctl": "1:0:0:0",
        "model": "1e3",
        "vendor": "0o17",
        "serialNumber": "0987654321",
        "wwn": "0x5000c500a1b2c3d4",
        "minSizeGigabytes": 0,
        "rotational": False,
    },
}

# Each value a YAML 1.1 reader takes for a boolean or a float.
READ_AS_ANOTHER_TYPE = {
    "hostname": "y",
    "role": "worker",
    "rootDeviceHints": {"model": "12E45", "serialNumber": "09876", "vendor": "Y"},
}


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


# Strings above U+FFFF, as a hint or in the node's own network configuration.
OUTSIDE_THE_BMP = {
    "hostname": "master-1",
    "role": "master",
    "rootDeviceHints": {"deviceName": "/dev/vda", "model": "NVMe \U0001D7D9"},
    "networkConfig": {"interfaces": [{"name": "eth0", "type": "ethernet", "state": "up",
                                      "description": "uplink \U0001F310"}]},
}

# A JSON surrogate-pair escape, which go.yaml.in/yaml/v2 refuses to read.
SURROGATE_ESCAPE = re.compile(r"\\u[dD][89a-fA-F][0-9a-fA-F]{2}")


def render(config):
    loaded = LOADER.load_from_file(str(MEDIA / "tasks" / "build.yml"), trusted_as_template=True)
    written = [task for task in walk([task for task in loaded if isinstance(task, dict)])
               if task.get("name") == "Write the agent configuration"]
    assert len(written) == 1, "build.yml has %d tasks writing the agent configuration" % len(written)
    templar = Templar(loader=LOADER, variables={"bootwright_cluster_media_request": {"agentConfig": config}})
    return templar.template(written[0]["ansible.builtin.copy"]["content"])


def test_the_agent_configuration_is_written_with_every_value_typed():
    config = {"apiVersion": "v1beta1", "kind": "AgentConfig", "hosts": [EVERY_HINT, READ_AS_ANOTHER_TYPE]}
    assert json.loads(render(config)) == config


def test_a_string_above_the_bmp_reaches_the_installer_as_a_yaml_escape():
    config = {"apiVersion": "v1beta1", "kind": "AgentConfig", "hosts": [OUTSIDE_THE_BMP]}
    rendered = render(config)
    assert rendered.isascii()
    assert not SURROGATE_ESCAPE.search(rendered), rendered
    assert "\\U0001d7d9" in rendered and "\\U0001f310" in rendered
    assert yaml.safe_load(rendered) == config
