"""Install-time manifests are privately staged before image creation."""
from pathlib import Path

import yaml


def test_media_stages_manifests_before_installer():
    role = Path(__file__).resolve().parents[2] / "roles/containercluster_media_agent"
    tasks = yaml.safe_load((role / "tasks/build.yml").read_text())[0]["block"]
    names = [task["name"] for task in tasks]
    directory = names.index("Create the install-time manifest directory")
    write = names.index("Write the frozen install-time manifests")
    build = names.index("Build the image this cluster's nodes boot")
    assert directory < write < build
    assert tasks[directory]["ansible.builtin.file"]["mode"] == "0700"
    copy = tasks[write]["ansible.builtin.copy"]
    assert copy["mode"] == "0600"
    assert "/openshift/" in copy["dest"]
    assert "to_installer_yaml" in copy["content"]
    assert tasks[write]["no_log"] is True
    assert tasks[write]["loop"] == "{{ bootwright_cluster_media_request.manifests }}"
