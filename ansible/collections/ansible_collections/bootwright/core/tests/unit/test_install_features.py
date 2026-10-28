"""Private material and the controller proxy stay scoped to the installer build."""
from pathlib import Path

import yaml


def test_media_scopes_the_controller_route_and_secret_writer():
    role = Path(__file__).resolve().parents[2] / "roles/containercluster_media_agent"
    tasks = yaml.safe_load((role / "tasks/build.yml").read_text())[0]["block"]
    routed = [task for task in tasks if "environment" in task]
    assert len(routed) == 2
    assert {task["name"] for task in routed} == {"Build the image this cluster's nodes boot", "Generate native cluster manifests for encrypted installation"}
    assert "request.buildEgress" in routed[0]["environment"]
    assert routed[0]["no_log"] is True
    writer = next(task for task in tasks if task["name"] == "Write the install configuration with its bound material")
    assert writer["no_log"] is True
    assert writer["diff"] is False
    assert "mode" not in writer["bootwright.core.private_file"]
    assert "registry_pull_secret" in writer["bootwright.core.private_file"]["content"]
    assert "installer_proxy_credentials" in writer["bootwright.core.private_file"]["content"]
