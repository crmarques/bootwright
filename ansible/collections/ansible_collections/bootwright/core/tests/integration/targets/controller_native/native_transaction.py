"""Current-OS DNF signature refusal against an isolated installroot.

The actual maintained solver reads a newly built unsigned fixture package. The
production transaction wrapper must refuse it before any payload is installed.
This is module integration; it does not claim privileged host setup acceptance.
"""

import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile

candidates = [
    Path(value) / "ansible_collections/bootwright/core"
    for value in os.environ.get("ANSIBLE_COLLECTIONS_PATH", "").split(os.pathsep)
    if value
]
candidates.append(Path(__file__).resolve().parents[4])
collection = next(
    path
    for path in candidates
    if (path / "plugins/module_utils/native_apply.py").is_file()
)
sys.path.insert(0, str(collection.parents[2]))


def load(name):
    spec = importlib.util.spec_from_file_location(
        name, collection / "plugins/module_utils" / (name + ".py")
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


native, resolution = load("native_apply"), load("native_resolution")
release = {}
for line in Path("/etc/os-release").read_text().splitlines():
    if "=" in line:
        key, value = line.split("=", 1)
        release[key] = value.strip('"')
platform = dict(os=release["ID"], release=release["VERSION_ID"], architecture="amd64")
with tempfile.TemporaryDirectory(prefix="bootwright-dnf-integration-") as directory:
    work = Path(directory)
    for name in (
        "BUILD",
        "BUILDROOT",
        "RPMS",
        "SOURCES",
        "SPECS",
        "SRPMS",
        "root",
        "scratch",
    ):
        (work / name).mkdir(mode=0o700)
    spec = work / "SPECS/fixture.spec"
    spec.write_text("""Name: bootwright-native-fixture
Version: 1
Release: 1
Summary: Synthetic package transaction integration fixture
License: Public Domain
BuildArch: noarch
AutoReqProv: no
%description
Synthetic fixture with no host dependencies or package hooks.
%install
mkdir -p %{buildroot}/opt/bootwright-fixture
printf 'fixture' > %{buildroot}/opt/bootwright-fixture/value
%files
/opt/bootwright-fixture/value
""")
    subprocess.run(
        [
            "/usr/bin/rpmbuild",
            "-bb",
            "--define",
            "_topdir " + directory,
            "--define",
            "_build_id_links none",
            "--define",
            "_tmppath " + directory,
            "--define",
            "_buildhost example.invalid",
            "--define",
            "__os_install_post %{nil}",
            str(spec),
        ],
        check=True,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        timeout=60,
    )
    package = next((work / "RPMS").rglob("*.rpm"))
    request = dict(platform=platform)
    if platform["os"] == "fedora":
        import libdnf5

        base = resolution.setup_dnf5(
            request,
            str(work / "scratch"),
            repositories=False,
            installroot=str(work / "root"),
        )
        before = resolution.inventory5(base)
        goal = libdnf5.base.Goal(base)
        base.get_repo_sack().add_cmdline_packages([str(package)])
        available = libdnf5.rpm.PackageQuery(base)
        available.filter_available()
        available.filter_name(["bootwright-native-fixture"])
        goal.add_rpm_install(next(iter(available)))
        transaction = goal.resolve()
        if transaction.get_problems() != libdnf5.base.GoalProblem_NO_PROBLEM:
            raise RuntimeError("isolated fixture did not resolve")
        refused = False
        try:
            native.run5(transaction)
        except ValueError:
            refused = True
        after = resolution.inventory5(base)
    elif platform["os"] == "rhel":
        base = resolution.setup_dnf4(
            request,
            str(work / "scratch"),
            repositories=False,
            installroot=str(work / "root"),
        )
        before = resolution.inventory4(base)
        local = base.add_remote_rpms([str(package)], strict=True)
        base.package_install(local[0], strict=True)
        base.resolve(allow_erasing=False)
        refused = False
        try:
            native.run4(base)
        except ValueError:
            refused = True
        after = resolution.inventory4(base)
        base.close()
    else:
        raise RuntimeError("current OS does not provide a qualified native backend")
    if (
        not refused
        or before
        or after
        or (work / "root/opt/bootwright-fixture").exists()
    ):
        raise RuntimeError("untrusted package acquired transaction authority")
print("PASS current-OS DNF rejects unsigned local package in an isolated installroot")
