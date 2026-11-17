"""The HTTPS source gets only its own bound public certificate contents."""

from __future__ import annotations

from ansible.template import Templar

from ansible_collections.bootwright.core.tests.unit import test_managedos_install_private as private
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER


def render_kickstart(kickstart, material):
    task = next(t for t in private.flattened("apply.yml") if t.get("name") == "Render the frozen kickstart with the values this attempt derived")
    assert task["no_log"] is True and task["diff"] is False
    values = private.scope(
        bootwright_os_install_request={"kickstart": kickstart},
        bootwright_os_install_material={"marker": "marker", "authorizedKey": "public-key", **material},
        managedos_install_anaconda_certificate="IMAGE-CERTIFICATE",
        managedos_install_anaconda_private_url="https://artifacts.example.test/private/example",
    )
    return Templar(loader=LOADER, variables=values).template(task["bootwright.core.private_file"]["content"])


def test_tree_cert_contents_reach_the_script_without_a_loan_path_or_image_cert(tmp_path):
    path = tmp_path / "bound-tree-certificate"
    pem = "-----BEGIN CERTIFICATE-----\nEXAMPLE-TREE\n-----END CERTIFICATE-----\n"
    path.write_text(pem)
    output = render_kickstart("@@BOOTWRIGHT_PACKAGE_TREE_CERTIFICATE@@", {"treeCertificate": str(path)})
    assert output == pem
    assert str(path) not in output and "IMAGE-CERTIFICATE" not in output


def test_http_kickstart_does_not_acquire_missing_tree_material():
    frozen = "url --url=http://artifacts.example.test/tree\ntimezone Etc/UTC --utc --ntpservers=192.0.2.123\n"
    assert render_kickstart(frozen, {}) == frozen
