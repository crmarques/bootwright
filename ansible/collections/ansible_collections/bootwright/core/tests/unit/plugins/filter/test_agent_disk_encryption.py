"""Exercise the structured edit against actual native cluster-manifests output."""
from pathlib import Path

import pytest
import yaml
from ansible.errors import AnsibleFilterError
from ansible_collections.bootwright.core.plugins.filter.agent_disk_encryption import agent_disk_encryption

FIXTURE = Path(__file__).resolve().parents[2] / "fixtures/agent_cluster_manifests/agent-cluster-install.golden"


@pytest.mark.parametrize("selector", ["all", "masters", "workers"])
def test_native_manifest_keeps_every_unrelated_field(selector):
    original = yaml.safe_load(FIXTURE.read_text())
    encryption = {"enableOn": selector, "mode": "tpmv2"}
    result = agent_disk_encryption(original, encryption, "sno")
    assert result["spec"]["diskEncryption"] == encryption
    del result["spec"]["diskEncryption"]
    assert result == original
    assert "diskEncryption" not in original["spec"]


def test_another_cluster_is_refused():
    with pytest.raises(AnsibleFilterError):
        agent_disk_encryption(yaml.safe_load(FIXTURE.read_text()), {"enableOn": "all", "mode": "tpmv2"}, "another")
