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
import tempfile
import urllib.parse

FORMAT = "bootwright.native-plan-v1"
MAX_PACKAGES = 512
MAX_INVENTORY = 32768
MAX_DATABASE = 512 << 20


def canonical(value):
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=True
    ).encode("ascii")


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def inventory_digest(value):
    return digest(sorted(value, key=canonical))


def identity(package):
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


def db_path(platform):
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
    if request.get("policy") != "ceph-packages-v2" or request["requirements"] != {"cephPackages": True}:
        raise ValueError("native root policy")
    versions = request["versions"]
    if (not isinstance(versions, dict) or not {"ceph", "ansible", "license"} <= set(versions)
            or set(versions) - {"ceph", "ansible", "license", "inheritedPolicy", "nfsGanesha"}):
        raise ValueError("native root selections")
    if any(not isinstance(value, str) or not 0 < len(value.encode("utf-8")) <= 1024
           or any(ord(char) < 32 or ord(char) == 127 for char in value) for value in versions.values()):
        raise ValueError("native root selection")
    roots = [("ceph", "cephadm", versions["ceph"]), ("ceph", "ceph-common", versions["ceph"]),
             ("ansible", "cephadm-ansible", versions["ansible"]), ("license", "ibm-storage-ceph-license", versions["license"])]
    roots.append(("broker", "openssl", "latest"))
    if "nfsGanesha" in versions:
        roots.append(("nfsGanesha", "nfs-ganesha", versions["nfsGanesha"]))
    if "inheritedPolicy" in versions:
        roots.extend(("inherited-policy", name, versions["inheritedPolicy"]) for name in ("crypto-policies", "crypto-policies-scripts"))
    return roots


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
    if request.get("policy") == "ceph-packages-v2":
        selected_roots(request)
        # The disposable snapshot contains RPMDB only, while DVD AppStream
        # modular metadata requires the selected RHEL platform explicitly.
        conf.module_platform_id = "platform:el" + request["platform"]["release"].split(".", 1)[0]
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


def choose(candidates, requested, compare):
    candidates = [
        pkg
        for pkg in candidates
        if identity(pkg)["architecture"] in ("x86_64", "noarch")
        and version_matches(identity(pkg), requested)
    ]
    if not candidates:
        raise ValueError("requested native release unavailable")
    candidates.sort(
        key=functools.cmp_to_key(
            lambda left, right: compare_identity(identity(left), identity(right), compare)
        )
    )
    selected = candidates[-1]
    selected_identity = identity(selected)
    peers = [
        pkg
        for pkg in candidates
        if compare_identity(identity(pkg), selected_identity, compare) == 0
    ]
    if any(identity(pkg) != selected_identity for pkg in peers):
        raise ValueError("ambiguous native architecture")
    # Equal identity from distinct repositories must still identify equal bytes.
    peers.sort(key=lambda pkg: pkg.repoid)
    return peers[0], peers


def package_record(package, request):
    value = identity(package)
    if request.get("plan") is not None:
        for known in request["plan"]["packages"]:
            if all(known[key] == expected for key, expected in value.items()):
                return known.copy()
        raise ValueError("local package is outside frozen plan")
    matches = [
        record for record in local_repositories(request) if record["id"] == package.repoid
    ]
    if len(matches) != 1:
        raise ValueError("native package repository")
    repo = matches[0]
    url = urllib.parse.urljoin(repo["baseURL"].rstrip("/") + "/", package.location)
    if (
        not url.startswith(repo["baseURL"].rstrip("/") + "/")
        or urllib.parse.urlsplit(url).query
        or urllib.parse.urlsplit(url).fragment
    ):
        raise ValueError("native package source")
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


def assemble(request, before, roots, inbound, outbound, compare, solver_version):
    packages, root_values = {}, []
    root_names = {}
    explicit_ceph_roots = set()
    for key, requested, package in roots:
        record = package_record(package, request)
        packages[record["source"]["id"]] = record
        root_values.append(
            {"key": key, "requested": requested, "package": identity(package)}
        )
        root_names[record["name"]] = requested
        if request.get("policy") == "ceph-packages-v2" and key in ("ceph", "ansible", "nfsGanesha"):
            explicit_ceph_roots.add(record["name"])
    before_names = {}
    for value in before:
        before_names.setdefault(value["name"], []).append(value)
    removed = {canonical(value): value for value in outbound}
    after = {canonical(value): value for value in before}
    actions = []
    for package in inbound:
        target = identity(package)
        if target["architecture"] not in ("x86_64", "noarch"):
            raise ValueError("native target architecture")
        matches = before_names.get(target["name"], [])
        if len(matches) > 1:
            raise ValueError("ambiguous installed package")
        previous = matches[0] if matches else None
        record = package_record(package, request)
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
            if order < 0 and target["name"] not in explicit_ceph_roots and root_names.get(target["name"], "latest") == "latest":
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
        "solver": "dnf4",
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


def authored_candidates(base, name, requested, *, unversioned=False):
    """The native resolver owns version parsing, availability and error text."""
    import dnf.subject

    subject = name if unversioned else name + "-" + requested
    base.install(subject, strict=True)
    return [package for package in dnf.subject.Subject(subject).get_best_query(base.sack, with_provides=False).available()
            if package.name == name]


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
        if local is None:
            # Let DNF interpret the exact authored subject, including malformed
            # versions. A fixed package name prevents option/command injection.
            candidates = authored_candidates(base, name, requested, unversioned=key in ("license", "inherited-policy", "broker"))
        else:
            candidates = [pkg for pkg in local if pkg.name == name]
        package, peers = choose(candidates, "latest", compare)
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
        compare,
        dnf.__version__,
    )
    return (plan, base, base.transaction) if return_transaction else plan


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


def inspect(request, scratch):
    root = request.get("snapshot") or snapshot_database(request["platform"], scratch)
    probe = {"platform": request["platform"], "snapshot": root}
    values = inventory4(setup_dnf4(probe, scratch, repositories=False))
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
