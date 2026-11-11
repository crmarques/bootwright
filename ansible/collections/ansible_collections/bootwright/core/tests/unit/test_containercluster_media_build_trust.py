"""The image build verifies the release's registry against the cluster's trust.

`openshift-install agent create image` reads the release payload through
`oc adm release info` and `oc image extract`, each with --insecure=true
(pkg/asset/agent/image/releaseextract.go in 4.20 and 4.21,
pkg/asset/rhcos/releaseextract.go in 4.22 and later, and
DetermineReleaseImageArch in pkg/asset/agent/common.go, openshift/installer),
and oc then hands
library-go's registry client a transport that skips certificate verification
(SecurityOptions.NewContext in pkg/cli/image/manifest/manifest.go, openshift/oc;
Context.Ping in pkg/image/registryclient/client.go, openshift/library-go). The
installer runs oc by name with its own environment (ExecuteOC in
pkg/asset/agent/oc.go), so the build leads its search path with a wrapper that
drops that flag and ends with --insecure=false, and names in SSL_CERT_DIR,
which Go's crypto/x509 reads on Linux beside the system bundle file, a private
directory holding the trust bundles the cluster selects. A base-image read
that fails only warns, and the installer then downloads the base image from
its compiled stream URL (retrieveBaseIso in pkg/asset/agent/image/baseiso.go
for 4.20 and 4.21, pkg/asset/rhcos/iso.go for 4.22), so the build refuses when
the installer's log records that fallback. These cases render the role's real
tasks: the build steps name the lent bundle, the wrapper drops only the flag
and leaves insecure reads off whatever follows, the lent file is written
private in private directories, the build's always section removes it whether
or not the build succeeded, and a logged fallback refuses the build.

Rendering the task files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import subprocess

import pytest
from ansible.module_utils.testing import patch_module_args
from ansible.modules import file as file_module
from ansible.modules import find as find_module
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.modules import private_file

MEDIA = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_media_agent"
LOADER = DataLoader()
WORK = "/var/lib/bootwright-hosted/lab/clusters/sno"
OC = "/var/lib/bootwright/tools/oc/4.21.15/oc"
ROUTED = ("Build the image this cluster's nodes boot", "Generate native cluster manifests for encrypted installation")
LEND = "Lend the cluster's selected trust bundles to the build"
WRAP = "Write the client wrapper the installer finds as oc"
CREATE = "Create the private directories the build's trust and client wrapper live in"
REMOVE = "Remove the trust and client wrapper lent to the build"


def build():
    loaded = LOADER.load_from_file(str(MEDIA / "tasks" / "build.yml"), trusted_as_template=True)
    blocks = [task for task in loaded if isinstance(task, dict) and "block" in task]
    assert len(blocks) == 1
    return blocks[0]


def task(section, name):
    matches = [entry for entry in build().get(section) or [] if isinstance(entry, dict) and entry.get("name") == name]
    assert len(matches) == 1, "build.yml has %d tasks named %r in its %s" % (len(matches), name, section)
    return matches[0]


def certificate(index):
    """A PEM block standing for one CA bundle; rendering never parses it."""
    return "-----BEGIN CERTIFICATE-----\nQ0EtYnVuZGxl%02d\n-----END CERTIFICATE-----" % index


def scope(work=WORK, bundles=(), **variables):
    values = dict(LOADER.load_from_file(str(MEDIA / "defaults" / "main.yml"), trusted_as_template=True))
    request = {"workRoot": work, "buildEgress": {"HTTPS_PROXY": "http://proxy.example.test:3128"}}
    material = {"oc": OC, "installer": "/installer"}
    if bundles:
        request["trustBundleRefs"] = ["ca-%d" % index for index in range(len(bundles))]
        material.update(("trustBundle%d" % index, str(path)) for index, path in enumerate(bundles))
    values.update(bootwright_cluster_media_request=request, bootwright_cluster_media_material=material)
    values.update(variables)
    return values


def render(value, variables):
    return Templar(loader=LOADER, variables=variables).template(value)


def lent(tmp_path, count):
    bundles = []
    for index in range(count):
        bundle = tmp_path / ("trust-%d" % index)
        bundle.write_text("\n" + certificate(index) + "\n\n")
        bundles.append(bundle)
    return bundles


def test_every_build_step_names_the_lent_bundle_and_leads_with_the_wrapper(tmp_path):
    variables = scope(bundles=lent(tmp_path, 2))
    for name in ROUTED:
        assert task("block", name)["environment"] == "{{ containercluster_media_agent_environment }}"
    environment = render(task("block", ROUTED[0])["environment"], variables)
    trust = WORK + "/.bootwright-build/trust"
    assert environment["SSL_CERT_DIR"].split(":") == [trust, "/etc/pki/tls/certs", "/etc/ssl/certs"]
    assert environment["PATH"].split(":") == [WORK + "/.bootwright-build/bin", os.path.dirname(OC), "/usr/bin", "/usr/sbin"]
    assert environment["HTTPS_PROXY"] == "http://proxy.example.test:3128"
    lend = task("block", LEND)
    destination = render(lend["bootwright.core.private_file"]["path"], variables)
    assert destination == trust + "/selected.pem"
    assert render(task("block", WRAP)["ansible.builtin.copy"]["dest"], variables) == WORK + "/.bootwright-build/bin/oc"


def test_the_lent_bundle_holds_the_selected_bundles_in_order(tmp_path):
    """Eleven bundles, so ordering them by variable name, which puts
    trustBundle10 before trustBundle2, differs from the order the cluster
    selects them in; the same order as the install configuration's
    additionalTrustBundle."""
    lend = task("block", LEND)
    variables = scope(bundles=lent(tmp_path, 11))
    variables.update(lend["vars"])
    assert Templar(loader=LOADER, variables=variables).evaluate_conditional(lend["when"]) is True
    content = render(lend["bootwright.core.private_file"]["content"], variables)
    assert content == "\n".join(certificate(index) for index in range(11)) + "\n"


def test_a_cluster_selecting_no_trust_bundle_lends_none():
    lend = task("block", LEND)
    variables = scope()
    assert Templar(loader=LOADER, variables=variables).evaluate_conditional(lend["when"]) is False


def run(module, arguments, capsys):
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        module.main()
    return json.loads(capsys.readouterr().out)


def test_the_lent_bundle_is_private_and_removed_after_the_build(tmp_path, capsys):
    work = tmp_path / "work"
    work.mkdir(mode=0o700)
    variables = scope(work=str(work), bundles=lent(tmp_path, 1))
    create = task("block", CREATE)
    arguments = render({key: value for key, value in create["ansible.builtin.file"].items() if key != "path"}, variables)
    assert (arguments.pop("owner"), arguments.pop("group"), arguments["mode"]) == ("root", "root", "0700")
    for item in render(create["loop"], variables):
        run(file_module, dict(arguments, path=item), capsys)
        assert stat.S_IMODE(os.stat(item).st_mode) == 0o700
    lend = task("block", LEND)
    variables.update(lend["vars"])
    written = render(lend["bootwright.core.private_file"], variables)
    assert "mode" not in written
    private_file.write(written["path"], written["content"], "0600", os.getuid(), os.getgid())
    assert stat.S_IMODE(os.stat(written["path"]).st_mode) == 0o600

    removal = render(task("always", REMOVE)["ansible.builtin.file"], variables)
    assert removal["state"] == "absent"
    assert pathlib.Path(written["path"]).is_relative_to(removal["path"])
    run(file_module, removal, capsys)
    assert not os.path.lexists(written["path"])
    assert sorted(os.listdir(work)) == []


def test_the_build_block_removes_what_it_lent_on_every_outcome():
    """The always section runs after a failed or stopped build too, and it is
    the block's only one, so nothing lent outlives the attempt."""
    block = build()
    assert [entry["name"] for entry in block["always"]] == [REMOVE]
    names = [entry["name"] for entry in block["block"]]
    for name in ROUTED:
        assert names.index(CREATE) < names.index(LEND) < names.index(WRAP) < names.index(name)


def wrapper(tmp_path):
    client = tmp_path / "client"
    client.write_text("#!/bin/sh\nfor argument do printf '%s\\n' \"$argument\"; done\n")
    client.chmod(0o700)
    content = render(task("block", WRAP)["ansible.builtin.copy"]["content"], scope(bundles=(), bootwright_cluster_media_material={"oc": str(client)}))
    script = tmp_path / "oc"
    script.write_text(content)
    script.chmod(0o700)
    return script


def test_the_wrapper_drops_the_installers_insecure_flag_and_keeps_every_other_argument(tmp_path):
    """The installer's own extraction call, as extractFileFromImage builds it."""
    script = wrapper(tmp_path)
    arguments = ["image", "extract", "--path=/coreos/coreos-x86_64.iso:/cache", "--filter-by-os=linux/amd64",
                 "--insecure=true", "--confirm", "--icsp-file=/tmp/icsp-file 1", "--insecure",
                 "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:" + "a" * 64, "--registry-config=/tmp/registry-config"]
    finished = subprocess.run([str(script)] + arguments, capture_output=True, text=True, check=True)
    kept = [argument for argument in arguments if argument not in ("--insecure=true", "--insecure")]
    assert finished.stdout.splitlines() == kept + ["--insecure=false"]


def insecure(arguments):
    """The value pflag gives a boolean flag: its last occurrence wins, a bare
    flag is true, and a value is read as strconv.ParseBool reads it."""
    value = False
    for argument in arguments:
        if argument == "--insecure":
            value = True
        elif argument.startswith("--insecure="):
            spelled = argument.split("=", 1)[1]
            assert spelled in ("1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False")
            value = spelled in ("1", "t", "T", "TRUE", "true", "True")
    return value


@pytest.mark.parametrize("spelling", ["--insecure=True", "--insecure=1", "--insecure=TRUE", "--insecure=t", "--insecure=true", "--insecure"])
def test_no_later_spelling_turns_insecure_reads_back_on(tmp_path, spelling):
    script = wrapper(tmp_path)
    arguments = ["adm", "release", "info", "--insecure=true", "quay.io/openshift-release-dev/ocp-release@sha256:" + "b" * 64, spelling]
    assert insecure(arguments) is True
    finished = subprocess.run([str(script)] + arguments, capture_output=True, text=True, check=True)
    assert insecure(finished.stdout.splitlines()) is False
    assert finished.stdout.splitlines()[-1] == "--insecure=false"


def test_an_explicit_insecure_false_passes_through(tmp_path):
    script = wrapper(tmp_path)
    arguments = ["image", "info", "--insecure=false", "registry.example.test/release@sha256:" + "c" * 64]
    finished = subprocess.run([str(script)] + arguments, capture_output=True, text=True, check=True)
    assert finished.stdout.splitlines() == arguments + ["--insecure=false"]


FALLBACK_READ = "Read whether the installer fell back from the release's base image"
FALLBACK_REFUSAL = "Refuse an image built from a downloaded base image"
EXTRACTED = 'level=info msg="Extracting base ISO from release payload"\nlevel=info msg="Generated ISO at agent.x86_64.iso"\n'
WARNED = 'level=warning msg="Failed to extract base ISO from release payload - check registry configuration"\n'
DOWNLOADED = 'level=info msg="Downloading base ISO"\n'


def fallback(tmp_path, capsys, log):
    """The read run by the real find module over one installer log, then
    whether the build refuses after it."""
    work = tmp_path / "work"
    work.mkdir()
    (work / ".openshift_install.log").write_text(log)
    (work / "install-config.yaml").write_text("Downloading base ISO\n")
    variables = scope(work=str(work))
    read = task("block", FALLBACK_READ)
    variables[read["register"]] = run(find_module, render(read["ansible.builtin.find"], variables), capsys)
    return Templar(loader=LOADER, variables=variables).evaluate_conditional(task("block", FALLBACK_REFUSAL)["when"])


@pytest.mark.parametrize("log", [EXTRACTED + WARNED + DOWNLOADED, EXTRACTED + DOWNLOADED, WARNED])
def test_a_logged_fallback_to_a_downloaded_base_image_refuses_the_build(tmp_path, capsys, log):
    assert fallback(tmp_path, capsys, log) is True


def test_a_base_image_read_from_the_release_builds(tmp_path, capsys):
    """Another file of the work area naming the message is not the log."""
    assert fallback(tmp_path, capsys, EXTRACTED) is False


def test_the_fallback_is_read_after_the_build_and_before_anything_is_published():
    names = [entry["name"] for entry in build()["block"]]
    assert names.index(ROUTED[0]) < names.index(FALLBACK_READ) < names.index(FALLBACK_REFUSAL) < names.index("Find the image the installer produced")
    refusal = task("block", FALLBACK_REFUSAL)["ansible.builtin.fail"]["msg"]
    assert "additionalTrustBundleRefs" in refusal and "mirror lacks the release" in refusal


def test_the_wrapper_runs_the_published_client_by_a_quoted_path(tmp_path):
    content = render(task("block", WRAP)["ansible.builtin.copy"]["content"],
                     scope(bootwright_cluster_media_material={"oc": "/var/lib/bootwright/tools/o c/oc"}))
    assert content.splitlines()[0] == "#!/bin/sh"
    assert content.splitlines()[-1] == "exec '/var/lib/bootwright/tools/o c/oc' \"$@\" --insecure=false"
    copy = task("block", WRAP)["ansible.builtin.copy"]
    assert (copy["owner"], copy["group"], copy["mode"]) == ("root", "root", "0700")
