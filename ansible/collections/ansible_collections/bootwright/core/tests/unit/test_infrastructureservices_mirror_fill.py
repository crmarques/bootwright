"""The artifact mirror fill publishes only what it verified, through its frozen route.

Each file is read from its publisher by ansible.builtin.get_url, which checks
the downloaded temporary file against the pinned SHA-256 before it moves it
into place and fetches nothing for a destination that already matches
(ansible/modules/get_url.py, main, ansible-core 2.21), so a replay copies
nothing and a file of another digest publishes nothing. The route is the frozen
egress alone: with no HTTPS proxy the download ignores every proxy variable,
and with one the task sets both spellings of it. The refusals the rescue names
are exactly those the protocol admits, and a removal deletes the Environment's
mirror subtree and nothing beside it. The role's defaults are evaluated here as
Ansible does, against a request of the frozen shape.
"""
from __future__ import annotations

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.action import infrastructureservices_mirror_files_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "infrastructureservices_mirror_fill"
DIGEST = "0123456789abcdef" * 4
REQUEST = {
    "egress": {"noProxy": []},
    "files": [
        {"bytes": 5, "path": "helm/v4.3.1/helm-v4.3.1-linux-amd64.tar.gz", "sha256": DIGEST, "source": "https://get.helm.sh/helm-v4.3.1-linux-amd64.tar.gz"},
        {"bytes": 7, "path": "kubectl/v1.37.2/kubectl", "sha256": DIGEST, "source": "https://dl.k8s.io/release/v1.37.2/bin/linux/amd64/kubectl"},
    ],
    "identity": {"context": "lab", "environment": "lab", "server": "mirror"},
    "placement": {"connection": "ssh", "machine": "mirror"},
    "root": "/var/lib/bootwright-hosted/lab/services/artifact-server/mirror/public/mirror/lab",
    "servedRoot": "/var/lib/bootwright-hosted/lab/services/artifact-server/mirror/public",
    "version": "mirror-files-v1",
}


LOADER = DataLoader()


def tasks(name):
    return LOADER.load_from_file(str(ROLE / "tasks" / (name + ".yml")), trusted_as_template=True)


def templar(request):
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(bootwright_mirror_files_request=request, bootwright_mirror_files_digest=DIGEST)
    return Templar(loader=LOADER, variables=variables)


def download():
    block = next(task for task in tasks("apply") if "block" in task)
    return block, block["block"][0]


def test_the_request_names_its_root_beneath_the_servers_served_root_and_every_parent_directory():
    values = templar(REQUEST)
    assert values.template(trust_as_template("{{ infrastructureservices_mirror_fill_root }}")) == REQUEST["root"]
    assert values.template(trust_as_template("{{ infrastructureservices_mirror_fill_served_root }}")) == REQUEST["servedRoot"]
    assert values.template(trust_as_template("{{ infrastructureservices_mirror_fill_directories }}")) == [
        REQUEST["servedRoot"] + "/mirror", REQUEST["root"], REQUEST["root"] + "/helm/v4.3.1", REQUEST["root"] + "/kubectl/v1.37.2"]
    for valid in ("paths", "digests", "sources", "sizes"):
        assert len(values.template(trust_as_template("{{ infrastructureservices_mirror_fill_valid_%s }}" % valid))) == 2


def test_a_file_that_could_leave_the_root_or_names_no_digest_is_not_valid():
    files = [dict(REQUEST["files"][0], path="../escape/file"), dict(REQUEST["files"][1], sha256="abc", source="http://dl.k8s.io/kubectl")]
    values = templar(dict(REQUEST, files=files))
    assert values.template(trust_as_template("{{ infrastructureservices_mirror_fill_valid_paths }}")) == [files[1]["path"]]
    assert values.template(trust_as_template("{{ infrastructureservices_mirror_fill_valid_digests }}")) == [DIGEST]
    assert values.template(trust_as_template("{{ infrastructureservices_mirror_fill_valid_sources }}")) == [REQUEST["files"][0]["source"]]


def test_each_file_is_published_only_at_its_pinned_digest_over_verified_tls():
    task = download()[1]
    arguments = task["ansible.builtin.get_url"]
    assert arguments["checksum"] == "sha256:{{ item.sha256 }}"
    assert arguments["validate_certs"] is True and arguments["mode"] == "0644" and arguments["owner"] == "root"
    assert arguments["dest"] == "{{ infrastructureservices_mirror_fill_root }}/{{ item.path }}"
    assert "force" not in arguments, "a forced download would fetch a file that already matches its pin"


def test_the_download_takes_its_route_from_the_frozen_egress_alone():
    task = download()[1]
    for egress, proxied in (({"noProxy": []}, False), ({"httpsProxy": "http://proxy.example.test:3128", "noProxy": [".lab"]}, True)):
        values = templar(dict(REQUEST, egress=egress))
        assert values.template(task["ansible.builtin.get_url"]["use_proxy"]) is proxied
        environment = values.template(task["environment"])
        if proxied:
            assert environment["https_proxy"] == environment["HTTPS_PROXY"] == egress["httpsProxy"]
            assert environment["http_proxy"] == environment["HTTP_PROXY"] == egress["httpsProxy"], "a redirect to HTTP would go direct"
            assert environment["no_proxy"] == environment["NO_PROXY"] == ".lab"
        else:
            assert environment == {}


def test_the_rescue_names_only_the_refusals_the_protocol_admits():
    block = download()[0]
    named = block["rescue"][0]["bootwright.core.infrastructureservices_mirror_files_protocol"]["reason"]
    for failure, reason in (("The checksum for /x did not match", "file-digest-mismatch"), ("Request failed: <urlopen error timed out>", "file-unreachable")):
        values = templar(REQUEST)
        values.available_variables = dict(values.available_variables, ansible_failed_result={"results": [{"msg": failure}]})
        assert values.template(named) == reason
        assert reason in protocol.REFUSALS
    assert block["rescue"][-1].get("ansible.builtin.fail"), "a refused file must still fail the run"
    label = block["always"][0]["ansible.builtin.command"]["argv"]
    assert label[:3] == ["/usr/bin/chcon", "--recursive", "--reference"], "a partial fill's published files must be labeled to be served"


def test_a_removal_deletes_the_environments_mirror_subtree_and_nothing_beside_it():
    removals = [task for task in tasks("destroy") if task.get("ansible.builtin.file", {}).get("state") == "absent"]
    assert [task["ansible.builtin.file"]["path"] for task in removals] == ["{{ infrastructureservices_mirror_fill_root }}"]
    emptied = [task for task in tasks("destroy") if "ansible.builtin.command" in task]
    assert [task["ansible.builtin.command"]["argv"][:2] for task in emptied] == [["/usr/bin/rmdir", "--ignore-fail-on-non-empty"]]
