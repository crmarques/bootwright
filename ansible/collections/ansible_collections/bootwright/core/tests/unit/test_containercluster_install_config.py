"""The install configuration reaches the installer with every value it froze.

The media request freezes the cluster's install configuration (installConfig in
internal/containercluster/agentinstall/projection.go), and the attempt
substitutes the pull secret, the cluster key and the additional trust bundles
the cluster selects into it. The installer reads
install-config.yaml with sigs.k8s.io/yaml, whose YAML 1.1 resolver reads a plain
1e3, 0o17 or 08 as a number and a plain y or n as a boolean and hands the
installer different text, while to_nice_yaml leaves exactly those scalars plain
(.agents/knowledge/installer-input-yaml-scalars.md). This check renders the real
task over cluster names that are such scalars, requires that no string is
written plain, and reads every value back.

Rendering the role's task file needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins.
"""

from __future__ import annotations

import json
import pathlib

import pytest
import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

MEDIA = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_media_agent"
LOADER = DataLoader()
STRING = "tag:yaml.org,2002:str"

# Each a cluster name the installer's YAML 1.1 reader takes for a number or a
# boolean when it is written plain.
READ_AS_ANOTHER_TYPE = ["1e3", "1E+3", "5E2", "12E45", "0o17", "08", "09876", "0987654321", "y", "Y", "n", "N"]

PULL_SECRET = '{"auths": {"registry.example.test": {"auth": "Ym9vdHdyaWdodDpwcm9iZQ=="}}}'
SSH_KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPbootwrightprobekey0000000000000000000000 core"


def install_config(name):
    """A single-node install configuration as the media request freezes it."""
    return {
        "apiVersion": "v1",
        "baseDomain": "lab.example.test",
        "compute": [{"name": "worker", "replicas": 0}],
        "controlPlane": {"name": "master", "replicas": 1},
        "metadata": {"name": name},
        "networking": {
            "clusterNetwork": [{"cidr": "10.128.0.0/14", "hostPrefix": 23}],
            "machineNetwork": [{"cidr": "198.51.100.0/24"}],
            "serviceNetwork": ["172.30.0.0/16"],
        },
        "platform": {"none": {}},
        "pullSecret": "",
        "sshKey": "",
    }


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def render(config, pull_secret, trust_bundles=()):
    """The task's content over a request selecting one trust bundle per path.

    The runner hands the adapter each bound trust bundle's path as
    trustBundle<index>, the index of its reference in the request
    (MediaCapability.run in internal/containercluster/agentinstall).
    """
    loaded = LOADER.load_from_file(str(MEDIA / "tasks" / "build.yml"), trusted_as_template=True)
    written = [task for task in walk([task for task in loaded if isinstance(task, dict)])
               if task.get("name") == "Write the install configuration with its bound material"]
    assert len(written) == 1, "build.yml has %d tasks writing the install configuration" % len(written)
    request = {"installConfig": config}
    material = {"pullSecret": str(pull_secret), "sshKey": SSH_KEY}
    if trust_bundles:
        request["trustBundleRefs"] = ["ca-%d" % index for index in range(len(trust_bundles))]
        material.update(("trustBundle%d" % index, str(path)) for index, path in enumerate(trust_bundles))
    variables = dict(written[0].get("vars") or {})
    variables.update(bootwright_cluster_media_request=request, bootwright_cluster_media_material=material)
    return Templar(loader=LOADER, variables=variables).template(written[0]["ansible.builtin.copy"]["content"])


def plain_strings(node):
    """Every string the document writes as a plain scalar."""
    if isinstance(node, yaml.ScalarNode):
        return [node.value] if node.style is None and node.tag == STRING else []
    if isinstance(node, yaml.MappingNode):
        return [value for pair in node.value for child in pair for value in plain_strings(child)]
    return [value for child in node.value for value in plain_strings(child)]


@pytest.mark.parametrize("name", READ_AS_ANOTHER_TYPE)
def test_the_install_configuration_is_written_with_every_value_typed(tmp_path, name):
    pull_secret = tmp_path / "pull-secret"
    pull_secret.write_text(PULL_SECRET + "\n")
    rendered = render(install_config(name), pull_secret)
    assert plain_strings(yaml.compose(rendered)) == [], rendered
    assert json.loads(rendered) == dict(install_config(name), pullSecret=PULL_SECRET, sshKey=SSH_KEY)


def certificate(index):
    """A PEM block standing for one CA bundle; rendering never parses it."""
    return "-----BEGIN CERTIFICATE-----\nQ0EtYnVuZGxl%02d\n-----END CERTIFICATE-----" % index


def test_the_selected_trust_bundles_reach_the_installer_as_one_bundle_in_order(tmp_path):
    """Eleven bundles, so ordering them by variable name, which puts
    trustBundle10 before trustBundle2, differs from the order the cluster
    selects them in. Each file carries blank lines around its certificate,
    and the installer's CABundle refuses a bundle with a blank line after its
    last one."""
    pull_secret = tmp_path / "pull-secret"
    pull_secret.write_text(PULL_SECRET + "\n")
    bundles = []
    for index in range(11):
        bundle = tmp_path / ("trust-%d" % index)
        bundle.write_text("\n" + certificate(index) + "\n\n")
        bundles.append(bundle)
    config = dict(install_config("ocp"), additionalTrustBundle="")
    rendered = render(config, pull_secret, bundles)
    assert plain_strings(yaml.compose(rendered)) == [], rendered
    joined = "\n".join(certificate(index) for index in range(11))
    assert json.loads(rendered) == dict(config, pullSecret=PULL_SECRET, sshKey=SSH_KEY, additionalTrustBundle=joined)


def test_a_cluster_selecting_no_trust_bundle_writes_none(tmp_path):
    pull_secret = tmp_path / "pull-secret"
    pull_secret.write_text(PULL_SECRET + "\n")
    assert "additionalTrustBundle" not in json.loads(render(install_config("ocp"), pull_secret))
