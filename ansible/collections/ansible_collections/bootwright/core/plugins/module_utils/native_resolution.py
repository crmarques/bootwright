"""Bounded DNF resolution on explicit metadata and disposable RPMDB snapshots."""

from __future__ import annotations

import fcntl
import functools
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import urllib.parse

FORMAT = "bootwright.native-plan-v1"
MAX_PACKAGES = 512
MAX_INVENTORY = 32768
MAX_DATABASE = 512 << 20
# Each requirement names the packages it installs as roots, and the version
# intent those roots share. A requirement may name more than one package: the
# hypervisor closure is one decision an operator makes, not eight.
ROOTS = {
    "podman": ("podman",),
    "openssh": ("openssh-clients",),
    "nmstate": ("nmstate",),
    "libvirt": ("libvirt-client",),
    "hypervisor": (
        "libvirt-daemon",
        "libvirt-daemon-driver-network",
        "libvirt-daemon-driver-qemu",
        "libvirt-daemon-driver-storage-core",
        "qemu-img",
        "qemu-kvm",
        "swtpm",
        "swtpm-tools",
    ),
    "installer-media": ("lorax", "xorriso"),
}

# The hypervisor runs the same libvirt release its client speaks to, so they
# share one version intent rather than drifting apart.
ROOT_VERSIONS = {
    "podman": "podman",
    "openssh": "openssh",
    "nmstate": "nmstate",
    "libvirt": "libvirt",
    "hypervisor": "libvirt",
    "installer-media": "installerMedia",
}


def canonical(value):
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=True
    ).encode("ascii")


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def inventory_digest(value):
    return digest(sorted(value, key=canonical))


def identity(package, five=False):
    if five:
        result = {
            "name": package.get_name(),
            "epoch": int(package.get_epoch()),
            "version": package.get_version(),
            "release": package.get_release(),
            "architecture": package.get_arch(),
        }
    else:
        result = {
            "name": package.name,
            "epoch": int(package.epoch),
            "version": package.version,
            "release": package.release,
            "architecture": package.arch,
        }
    if any(
        not isinstance(result[key], str) or len(result[key]) > 128
        for key in ("name", "version", "release", "architecture")
    ):
        raise ValueError("package identity")
    return result


def inventory4(base):
    result = [identity(package) for package in base.sack.query().installed()]
    if len(result) > MAX_INVENTORY:
        raise ValueError("inventory limit")
    return sorted(result, key=canonical)


def inventory5(base):
    import libdnf5

    query = libdnf5.rpm.PackageQuery(base)
    query.filter_installed()
    result = [identity(package, True) for package in query]
    if len(result) > MAX_INVENTORY:
        raise ValueError("inventory limit")
    return sorted(result, key=canonical)


def db_path(platform):
    if platform["os"] == "fedora":
        return "/usr/lib/sysimage/rpm"
    if platform["os"] == "rhel":
        return "/var/lib/rpm"
    raise ValueError("platform")


def snapshot_database(platform, scratch):
    """Copy a stable SQLite RPMDB while retaining a read lock; never open it live."""
    directory = db_path(platform)
    lock = os.open(directory + "/.rpm.lock", os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(lock)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("native database lock")
        fcntl.fcntl(
            lock, 37, struct.pack("hhqqi4x", fcntl.F_RDLCK, os.SEEK_SET, 0, 0, 0)
        )
        root = Path(tempfile.mkdtemp(prefix="snapshot-", dir=scratch))
        target = root / directory.lstrip("/")
        target.mkdir(parents=True, mode=0o700)
        total = 0
        for name in ("rpmdb.sqlite", "rpmdb.sqlite-wal"):
            try:
                descriptor = os.open(
                    directory + "/" + name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC
                )
            except FileNotFoundError:
                if name.endswith("-wal"):
                    continue
                raise
            with os.fdopen(descriptor, "rb") as source:
                before = os.fstat(source.fileno())
                total += before.st_size
                if (
                    not stat.S_ISREG(before.st_mode)
                    or before.st_uid != 0
                    or before.st_mode & 0o022
                    or total > MAX_DATABASE
                ):
                    raise ValueError("native database snapshot")
                with (target / name).open("xb") as output:
                    shutil.copyfileobj(source, output, 1 << 20)
                after = os.fstat(source.fileno())
                if any(
                    getattr(before, key) != getattr(after, key)
                    for key in (
                        "st_dev",
                        "st_ino",
                        "st_size",
                        "st_mode",
                        "st_uid",
                        "st_gid",
                        "st_nlink",
                        "st_mtime_ns",
                        "st_ctime_ns",
                    )
                ):
                    raise ValueError("native database changed")
        return str(root)
    finally:
        os.close(lock)


def selected_roots(request):
    keys = ["openssh", "nmstate"]
    if request["requirements"]["containerRuntime"]:
        keys.append("podman")
    if request["requirements"]["libvirtClient"]:
        keys.append("libvirt")
    if request["requirements"]["hypervisor"]:
        keys.append("hypervisor")
    if request["requirements"]["installerMedia"]:
        keys.append("installer-media")
    return [
        (key, name, request["versions"][ROOT_VERSIONS[key]])
        for key in sorted(keys)
        for name in ROOTS[key]
    ]


def local_repositories(request):
    repositories = request.get("repositories", [])
    if len(repositories) > 8:
        raise ValueError("repositories")
    return repositories


def scratch_paths(scratch):
    result = {}
    for name in ("cache", "logs", "persist", "empty-repos", "empty-vars", "state"):
        target = Path(scratch) / name
        target.mkdir(mode=0o700, exist_ok=True)
        result[name] = str(target)
    return result


def setup_dnf4(request, scratch, repositories=True, installroot=None):
    import dnf
    import dnf.conf

    paths = scratch_paths(scratch)
    root = (
        installroot
        or request.get("snapshot")
        or snapshot_database(request["platform"], scratch)
    )
    conf = dnf.conf.Conf()
    conf.installroot = root
    conf.cachedir = paths["cache"]
    conf.logdir = paths["logs"]
    conf.persistdir = paths["persist"] if root != "/" else "/var/lib/dnf"
    conf.reposdir = [paths["empty-repos"]]
    conf.varsdir = [paths["empty-vars"]]
    conf.plugins = False
    conf.zchunk = False
    conf.best = True
    conf.install_weak_deps = False
    conf.allow_vendor_change = False
    conf.obsoletes = False
    conf.clean_requirements_on_remove = False
    conf.gpgcheck = True
    conf.localpkg_gpgcheck = True
    conf.assumeyes = False
    conf.substitutions["releasever"] = request["platform"]["release"]
    conf.substitutions["basearch"] = "x86_64"
    base = dnf.Base(conf)
    if repositories:
        for record in local_repositories(request):
            repo = dnf.repo.Repo(record["id"], conf)
            repo.baseurl = [Path(record["localPath"]).as_uri()]
            repo.gpgcheck = True
            repo.repo_gpgcheck = False
            repo.metadata_expire = -1
            repo.skip_if_unavailable = False
            repo.enable()
            base.repos.add(repo)
    base.fill_sack(load_system_repo=True, load_available_repos=repositories)
    return base


def setup_dnf5(request, scratch, repositories=True, installroot=None):
    import libdnf5

    paths = scratch_paths(scratch)
    root = (
        installroot
        or request.get("snapshot")
        or snapshot_database(request["platform"], scratch)
    )
    base = libdnf5.base.Base()
    conf = base.get_config()
    options = {
        "installroot": root,
        "cachedir": paths["cache"],
        "system_cachedir": paths["cache"],
        "logdir": paths["logs"],
        "system_state_dir": (
            paths["state"] if root != "/" else "/usr/lib/sysimage/libdnf5"
        ),
        "reposdir": [paths["empty-repos"]],
        "varsdir": [paths["empty-vars"]],
        "plugins": False,
        "zchunk": False,
        "best": True,
        "install_weak_deps": False,
        "allow_vendor_change": False,
        "allow_downgrade": True,
        "clean_requirements_on_remove": False,
        "localpkg_gpgcheck": True,
        "pkg_gpgcheck": True,
        "optional_metadata_types": ["filelists"],
    }
    for name, value in options.items():
        getattr(conf, "get_" + name + "_option")().set(value)
    base.get_vars().set("releasever", request["platform"]["release"])
    base.get_vars().set("basearch", "x86_64")
    base.setup()
    sack = base.get_repo_sack()
    if repositories:
        for record in local_repositories(request):
            repo = sack.create_repo(record["id"])
            config = repo.get_config()
            config.get_baseurl_option().set([Path(record["localPath"]).as_uri()])
            config.get_pkg_gpgcheck_option().set(True)
            config.get_repo_gpgcheck_option().set(False)
            config.get_metadata_expire_option().set(-1)
            config.get_skip_if_unavailable_option().set(False)
        sack.load_repos()
    else:
        sack.load_repos(libdnf5.repo.Repo.Type_SYSTEM)
    return base


def version_matches(value, requested):
    if requested == "latest":
        return True
    epoch, version = None, requested
    if ":" in version:
        epoch, version = version.split(":", 1)
    release = None
    if "-" in version:
        version, release = version.split("-", 1)
    return (
        value["version"] == version
        and (epoch is None or value["epoch"] == int(epoch))
        and (release is None or value["release"] == release)
    )


def compare_identity(left, right, compare):
    if left["epoch"] != right["epoch"]:
        return 1 if left["epoch"] > right["epoch"] else -1
    return compare(left["version"], right["version"]) or compare(
        left["release"], right["release"]
    )


def choose(candidates, requested, five, compare):
    candidates = [
        pkg
        for pkg in candidates
        if identity(pkg, five)["architecture"] in ("x86_64", "noarch")
        and version_matches(identity(pkg, five), requested)
    ]
    if not candidates:
        raise ValueError("requested native release unavailable")
    candidates.sort(
        key=functools.cmp_to_key(
            lambda left, right: compare_identity(
                identity(left, five), identity(right, five), compare
            )
        )
    )
    selected = candidates[-1]
    selected_identity = identity(selected, five)
    peers = [
        pkg
        for pkg in candidates
        if compare_identity(identity(pkg, five), selected_identity, compare) == 0
    ]
    if any(identity(pkg, five) != selected_identity for pkg in peers):
        raise ValueError("ambiguous native architecture")
    # Equal identity from distinct repositories must still identify equal bytes.
    peers.sort(key=lambda pkg: pkg.get_repo_id() if five else pkg.repoid)
    return peers[0], peers


def package_record(package, request, five=False):
    value = identity(package, five)
    if request.get("plan") is not None:
        for known in request["plan"]["packages"]:
            if all(known[key] == expected for key, expected in value.items()):
                return known.copy()
        raise ValueError("local package is outside frozen plan")
    repo_id = package.get_repo_id() if five else package.repoid
    matches = [
        record for record in local_repositories(request) if record["id"] == repo_id
    ]
    if len(matches) != 1:
        raise ValueError("native package repository")
    repo = matches[0]
    location = package.get_location() if five else package.location
    url = urllib.parse.urljoin(repo["baseURL"].rstrip("/") + "/", location)
    if (
        not url.startswith(repo["baseURL"].rstrip("/") + "/")
        or urllib.parse.urlsplit(url).query
        or urllib.parse.urlsplit(url).fragment
    ):
        raise ValueError("native package source")
    if five:
        checksum = package.get_checksum()
        algorithm, checksum = checksum.get_type_str().lower(), checksum.get_checksum()
        size = package.get_download_size()
    else:
        import hawkey

        algorithm, raw = package.chksum
        algorithm, checksum = hawkey.chksum_name(algorithm).lower(), bytes(raw).hex()
        size = package.downloadsize
    if (
        algorithm != "sha256"
        or re.fullmatch("[0-9a-f]{64}", checksum) is None
        or not 0 < size <= 256 << 20
    ):
        raise ValueError("native source integrity")
    value["signer"] = repo["signer"]
    source = {"url": url, "sha256": checksum, "bytes": size}
    source["id"] = "native-" + digest({"package": value, "source": source})[:32]
    value["source"] = source
    return value


def assemble(request, before, roots, inbound, outbound, five, compare, solver_version):
    packages, root_values = {}, []
    root_names = {}
    for key, requested, package in roots:
        record = package_record(package, request, five)
        packages[record["source"]["id"]] = record
        root_values.append(
            {"key": key, "requested": requested, "package": identity(package, five)}
        )
        root_names[record["name"]] = requested
    before_names = {}
    for value in before:
        before_names.setdefault(value["name"], []).append(value)
    removed = {canonical(value): value for value in outbound}
    after = {canonical(value): value for value in before}
    actions = []
    for package in inbound:
        target = identity(package, five)
        if target["architecture"] not in ("x86_64", "noarch"):
            raise ValueError("native target architecture")
        matches = before_names.get(target["name"], [])
        if len(matches) > 1:
            raise ValueError("ambiguous installed package")
        previous = matches[0] if matches else None
        record = package_record(package, request, five)
        packages[record["source"]["id"]] = record
        action = {
            "kind": "install",
            "after": target,
            "sourceID": record["source"]["id"],
            "reason": "root" if target["name"] in root_names else "dependency",
        }
        if previous is not None:
            if (
                previous["architecture"] != target["architecture"]
                or canonical(previous) not in removed
            ):
                raise ValueError("unplanned native replacement")
            order = compare_identity(target, previous, compare)
            if order == 0:
                raise ValueError("unrequested native reinstall")
            action["kind"] = "upgrade" if order > 0 else "downgrade"
            action["before"] = previous
            if order < 0 and root_names.get(target["name"], "latest") == "latest":
                raise ValueError("unrelated native downgrade")
            removed.pop(canonical(previous))
            after.pop(canonical(previous))
        after[canonical(target)] = target
        actions.append(action)
    if removed:
        raise ValueError("unrelated native erase or obsoletes")
    for root in root_values:
        if canonical(root["package"]) not in after:
            raise ValueError("native root unresolved")
    if len(packages) > MAX_PACKAGES or len(actions) > MAX_PACKAGES:
        raise ValueError("native closure limit")
    repositories = (
        request["plan"]["repositories"]
        if request.get("plan")
        else [
            {key: repo[key] for key in ("id", "baseURL", "metadataSHA256")}
            for repo in local_repositories(request)
        ]
    )
    plan = {
        "format": FORMAT,
        "platform": request["platform"],
        "solver": "dnf5" if five else "dnf4",
        "solverVersion": solver_version,
        "requests": request["versions"],
        "requirements": request["requirements"],
        "roots": sorted(root_values, key=lambda value: value["key"]),
        "repositories": sorted(repositories, key=lambda value: value["id"]),
        "packages": sorted(packages.values(), key=lambda value: value["source"]["id"]),
        "actions": sorted(actions, key=lambda value: value["sourceID"]),
        "beforeSHA256": inventory_digest(before),
        "afterSHA256": inventory_digest(list(after.values())),
    }
    plan["digest"] = digest(plan)
    return plan


def solve4(request, scratch, local_sources=None, return_transaction=False):
    import dnf
    import rpm

    base = setup_dnf4(
        request,
        scratch,
        repositories=local_sources is None,
        installroot="/" if return_transaction else None,
    )
    before = inventory4(base)
    local = (
        base.add_remote_rpms(
            (
                list(local_sources.values())
                if isinstance(local_sources, dict)
                else local_sources
            ),
            strict=True,
        )
        if local_sources is not None
        else None
    )
    roots = []

    def compare(left, right):
        return rpm.labelCompare(("0", left, ""), ("0", right, ""))

    for key, name, requested in selected_roots(request):
        candidates = (
            [pkg for pkg in local if pkg.name == name]
            if local is not None
            else list(base.sack.query().available().filter(name=name))
        )
        package, peers = choose(
            candidates, requested if local is None else "latest", False, compare
        )
        records = [package_record(peer, request) for peer in peers]
        if any(
            record["source"]["sha256"] != records[0]["source"]["sha256"]
            for record in records
        ):
            raise ValueError("conflicting native repository identity")
        roots.append((key, requested, package))
        base.package_install(package, strict=True)
    if local is not None:
        for package in local:
            base.package_install(package, strict=True)
    base.resolve(allow_erasing=False)
    plan = assemble(
        request,
        before,
        roots,
        list(base.transaction.install_set),
        [identity(pkg) for pkg in base.transaction.remove_set],
        False,
        compare,
        dnf.__version__,
    )
    return (plan, base, base.transaction) if return_transaction else plan


def solve5(request, scratch, local_sources=None, return_transaction=False):
    import libdnf5

    base = setup_dnf5(
        request,
        scratch,
        repositories=local_sources is None,
        installroot="/" if return_transaction else None,
    )
    before = inventory5(base)
    goal = libdnf5.base.Goal(base)
    local = []
    if local_sources is not None:
        sources = (
            list(local_sources.values())
            if isinstance(local_sources, dict)
            else local_sources
        )
        base.get_repo_sack().add_cmdline_packages(sources)
        query = libdnf5.rpm.PackageQuery(base)
        query.filter_available()
        query.filter_repo_id(["@commandline"])
        local = list(query)
    roots = []
    for key, name, requested in selected_roots(request):
        if local_sources is None:
            query = libdnf5.rpm.PackageQuery(base)
            query.filter_available()
            query.filter_name([name])
            candidates = list(query)
        else:
            candidates = [pkg for pkg in local if pkg.get_name() == name]
        package, peers = choose(
            candidates,
            requested if local_sources is None else "latest",
            True,
            libdnf5.rpm.rpmvercmp,
        )
        records = [package_record(peer, request, True) for peer in peers]
        if any(
            record["source"]["sha256"] != records[0]["source"]["sha256"]
            for record in records
        ):
            raise ValueError("conflicting native repository identity")
        roots.append((key, requested, package))
        goal.add_rpm_install(package)
    for package in local:
        goal.add_rpm_install(package)
    transaction = goal.resolve()
    if transaction.get_problems() != libdnf5.base.GoalProblem_NO_PROBLEM:
        raise ValueError("native dependency solve")
    inbound, outbound = [], []
    for entry in transaction.get_transaction_packages():
        action = libdnf5.transaction.transaction_item_action_to_string(
            entry.get_action()
        ).lower()
        if action in ("install", "upgrade", "downgrade"):
            inbound.append(entry.get_package())
        elif action in ("replaced", "remove"):
            outbound.append(identity(entry.get_package(), True))
        else:
            raise ValueError("unrequested native transaction action")
    version = libdnf5.conf.get_library_version()
    plan = assemble(
        request,
        before,
        roots,
        inbound,
        outbound,
        True,
        libdnf5.rpm.rpmvercmp,
        ".".join(str(value) for value in (version.major, version.minor, version.micro)),
    )
    return (plan, base, transaction) if return_transaction else plan


def fresh_inventory(platform, scratch):
    request = {"platform": platform, "snapshot": snapshot_database(platform, scratch)}
    if platform["os"] == "fedora":
        return inventory5(setup_dnf5(request, scratch, repositories=False))
    return inventory4(setup_dnf4(request, scratch, repositories=False))


def validate_plan(plan):
    if not isinstance(plan, dict) or plan.get("format") != FORMAT:
        raise ValueError("native plan")
    content = plan.copy()
    actual = content.pop("digest", None)
    if (
        actual != digest(content)
        or len(plan.get("actions", [])) > MAX_PACKAGES
        or len(plan.get("packages", [])) > MAX_PACKAGES
    ):
        raise ValueError("native plan integrity")
    return plan


def installed_identity(output, name):
    lines = output.decode("ascii", "replace").splitlines()
    fields = lines[0].split("\t") if lines else []
    if (
        len(fields) != 5
        or fields[0] != name
        or any(not field or len(field) > 128 for field in fields)
    ):
        raise ValueError("installed package identity")
    return {
        "name": fields[0],
        "epoch": 0 if fields[1] == "(none)" else int(fields[1]),
        "version": fields[2],
        "release": fields[3],
        "architecture": fields[4],
    }


def present(request, scratch):
    """Report which selected roots are installed by name; verify no files."""
    root = request.get("snapshot") or snapshot_database(request["platform"], scratch)
    plan = validate_plan(request["plan"])
    dbpath = str(Path(root) / db_path(request["platform"]).lstrip("/"))
    roots = []
    for entry in plan["roots"]:
        name = entry["package"]["name"]
        # Standalone supplied-platform query; no remote module execution.
        # pylint: disable-next=ansible-bad-function
        result = subprocess.run(
            [
                "/usr/bin/rpm",
                "--dbpath",
                dbpath,
                "-q",
                "--queryformat",
                "%{NAME}\t%{EPOCH}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\n",
                "--",
                name,
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            check=False,
            timeout=30,
            env={
                "PATH": "/usr/sbin:/usr/bin",
                "LANG": "C",
                "LC_ALL": "C",
                "HOME": str(Path(scratch) / "home"),
            },
        )
        installed = None
        if result.returncode == 0:
            installed = installed_identity(result.stdout, name)
        roots.append({"key": entry["key"], "name": name, "installed": installed})
    return {
        "roots": roots,
        "rootsReady": all(root["installed"] is not None for root in roots),
    }


def inspect(request, scratch):
    root = request.get("snapshot") or snapshot_database(request["platform"], scratch)
    probe = {"platform": request["platform"], "snapshot": root}
    values = (
        inventory5(setup_dnf5(probe, scratch, repositories=False))
        if request["platform"]["os"] == "fedora"
        else inventory4(setup_dnf4(probe, scratch, repositories=False))
    )
    plan = request.get("plan")
    ready = True
    if plan is not None:
        validate_plan(plan)
        observed = {canonical(value) for value in values}
        expected = [
            {
                key: package[key]
                for key in ("name", "epoch", "version", "release", "architecture")
            }
            for package in plan["packages"]
        ]
        ready = all(canonical(value) in observed for value in expected)
    return {
        "inventory": values,
        "inventorySHA256": inventory_digest(values),
        "rootsReady": ready,
    }


def verified_files(platform, root, identities, scratch):
    """Prove the files one transaction installed, and nothing the host owns.

    A %config file is operator-owned and a %ghost entry is runtime-owned, so a
    daemon that creates its own runtime directory is not a dependency defect.
    """
    for value in identities:
        nevra = "{name}-{epoch}:{version}-{release}.{architecture}".format(**value)
        # Standalone supplied-platform probe; no remote module execution.
        # pylint: disable-next=ansible-bad-function
        result = subprocess.run(
            [
                "/usr/bin/rpm",
                "--dbpath",
                str(Path(root) / db_path(platform).lstrip("/")),
                "--verify",
                "--noconfig",
                "--noghost",
                "--noscripts",
                "--",
                nevra,
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
            timeout=30,
            env={
                "PATH": "/usr/sbin:/usr/bin",
                "LANG": "C",
                "LC_ALL": "C",
                "HOME": str(Path(scratch) / "home"),
            },
        )
        if result.returncode != 0:
            return False
    return True


def main():
    if len(sys.argv) != 2 or not os.path.isabs(sys.argv[1]):
        raise ValueError("native resolver launch")
    scratch = sys.argv[1]
    data = sys.stdin.buffer.read((4 << 20) + 1)
    if len(data) > 4 << 20:
        raise ValueError("native request limit")
    request = json.loads(data)
    private_home = Path(scratch) / "home"
    private_home.mkdir(mode=0o700, exist_ok=True)
    os.environ["HOME"] = str(private_home)
    operation = request.get("operation")
    output = os.dup(sys.stdout.fileno())
    with open(os.devnull, "wb") as sink:
        os.dup2(sink.fileno(), sys.stdout.fileno())
    if operation in ("inspect", "inventory"):
        result = inspect(request, scratch)
    elif operation == "resolve":
        result = (
            solve5(request, scratch)
            if request["platform"]["os"] == "fedora"
            else solve4(request, scratch)
        )
    elif operation == "present":
        result = present(request, scratch)
    elif operation == "apply":
        sys.path.insert(0, str(Path(__file__).resolve().parents[5]))
        from ansible_collections.bootwright.core.plugins.module_utils import (
            native_apply,
        )

        result = native_apply.apply(request, scratch)
    else:
        raise ValueError("native operation")
    encoded = canonical(result)
    if len(encoded) > 16 << 20:
        raise ValueError("native output limit")
    with os.fdopen(output, "wb") as destination:
        destination.write(encoded)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("native dependency operation refused\n")
        # Standalone bounded entrypoint uses its exit status as the protocol result.
        # pylint: disable-next=ansible-bad-function
        sys.exit(1)
