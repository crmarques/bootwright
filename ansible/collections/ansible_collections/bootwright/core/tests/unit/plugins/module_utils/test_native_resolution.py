"""Test bounded native resolution policy without host metadata or package effects."""

import copy
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import (
    bounded_process,
    native_resolution as native,
)


def package(name, version="2", release="1", arch="x86_64"):
    return SimpleNamespace(
        name=name, epoch=0, version=version, release=release, arch=arch
    )


def compare(left, right):
    return (left > right) - (left < right)


class NativeResolution(unittest.TestCase):
    def request(self, root):
        record = native.identity(root)
        record["source"] = dict(
            id="root-source",
            url="https://publisher.example.test/root.rpm",
            sha256="a" * 64,
            bytes=64,
        )
        record["signer"] = "b" * 40
        return dict(
            platform=dict(os="rhel", release="9.8", architecture="amd64"),
            versions=dict(ceph="latest"),
            requirements={},
            plan=dict(packages=[record], repositories=[]),
        )

    def assemble(self, request, root, before, inbound, outbound, requested="latest"):
        return native.assemble(
            request,
            before,
            [("ceph", requested, root)],
            inbound,
            outbound,
            compare,
            "4.20.0",
        )

    def test_no_action_retains_root_integrity_and_equal_inventory(self):
        root = package("cephadm")
        request = self.request(root)
        plan = self.assemble(request, root, [native.identity(root)], [], [])
        self.assertEqual(plan["solver"], "dnf4")
        self.assertEqual(plan["beforeSHA256"], plan["afterSHA256"])
        self.assertEqual(plan["packages"], request["plan"]["packages"])
        self.assertEqual(plan["actions"], [])
        native.validate_plan(plan)
        altered = copy.deepcopy(plan)
        altered["packages"][0]["source"]["sha256"] = "c" * 64
        with self.assertRaises(ValueError):
            native.validate_plan(altered)

    def test_latest_upgrade_and_explicit_root_downgrade(self):
        for old_version, requested, expected in [
            ("1", "latest", "upgrade"),
            ("3", "2", "downgrade"),
        ]:
            root = package("cephadm")
            previous = native.identity(package("cephadm", old_version))
            plan = self.assemble(
                self.request(root), root, [previous], [root], [previous], requested
            )
            self.assertEqual(plan["actions"][0]["kind"], expected)
            self.assertNotEqual(plan["beforeSHA256"], plan["afterSHA256"])
        with self.assertRaises(ValueError):
            self.assemble(self.request(root), root, [previous], [root], [previous])

    def test_reinstall_erase_architecture_and_unpaired_replacement_refused(self):
        root = package("cephadm")
        request = self.request(root)
        for before, outbound in [
            ([native.identity(root)], [native.identity(root)]),
            (
                [native.identity(root), native.identity(package("unrelated"))],
                [native.identity(package("unrelated"))],
            ),
            (
                [native.identity(package("cephadm", "1", arch="noarch"))],
                [native.identity(package("cephadm", "1", arch="noarch"))],
            ),
            ([native.identity(package("cephadm", "1"))], []),
        ]:
            with self.assertRaises(ValueError):
                self.assemble(request, root, before, [root], outbound)

    def test_unrelated_dependency_downgrade_refused(self):
        root = package("cephadm")
        dependency = package("library", "1")
        previous = native.identity(package("library", "2"))
        request = self.request(root)
        record = native.identity(dependency)
        record["source"] = dict(id="dependency-source")
        request["plan"]["packages"].append(record)
        with self.assertRaises(ValueError):
            self.assemble(
                request,
                root,
                [native.identity(root), previous],
                [dependency],
                [previous],
            )

    def test_version_only_and_exact_epoch_build_selection(self):
        value = native.identity(package("cephadm", "2.1.3", "5.el9cp"))
        for requested in ("latest", "2.1.3", "0:2.1.3-5.el9cp"):
            self.assertTrue(native.version_matches(value, requested))
        for requested in ("2.1.4", "2.1.3-6.el9cp", "1:2.1.3-5.el9cp"):
            self.assertFalse(native.version_matches(value, requested))
        self.assertEqual(native.inventory_digest([value]), native.digest([value]))

    def test_only_the_ceph_policy_selects_roots_and_only_on_rhel(self):
        with self.assertRaises(ValueError):
            native.selected_roots({"requirements": {}, "versions": {}})
        with self.assertRaises(ValueError):
            native.db_path({"os": "fedora"})
        self.assertEqual(native.db_path({"os": "rhel"}), "/var/lib/rpm")

    def test_inspect_reports_readiness_by_presence_without_verifying_files(self):
        root = package("ceph-common", "20.2.1", "405.el9cp")
        installed = native.identity(root)
        content = {
            "format": native.FORMAT,
            "packages": [dict(installed, source={"id": "root-source"})],
        }
        plan = dict(content, digest=native.digest(content))
        request = {
            "platform": {"os": "rhel", "release": "9.8", "architecture": "amd64"},
            "snapshot": "/snapshot",
            "plan": plan,
        }
        commands = []

        def run(command, **_):
            commands.append(command)
            return bounded_process.Result(0, b"", b"")

        def inspect(inventory):
            with patch.object(native, "setup_dnf4", lambda *_, **__: None):
                with patch.object(native, "inventory4", lambda _: inventory):
                    with patch.object(native.bounded_process, "run", run):
                        return native.inspect(request, "/scratch")

        result = inspect([installed])
        self.assertTrue(result["rootsReady"])
        self.assertEqual(result["inventory"], [installed])
        # Readiness after a transaction is presence: the closure is never read
        # back and probed twice for one effect.
        self.assertEqual(commands, [])
        # A closure package the host does not carry is still not ready.
        other = native.identity(package("ceph-common", "20.2.2"))
        self.assertFalse(inspect([other])["rootsReady"])

    def test_verified_files_covers_installed_files_and_no_host_owned_path(self):
        installed = native.identity(package("cephadm", "20.2.1", "405.el9cp"))
        platform = {"os": "rhel", "release": "9.8", "architecture": "amd64"}
        commands = []

        def verify(returncode):
            def run(command, **_):
                commands.append(command)
                return bounded_process.Result(returncode, b"", b"")

            return run

        with patch.object(native.bounded_process, "run", verify(0)):
            self.assertTrue(
                native.verified_files(platform, "/snapshot", [installed], "/scratch")
            )
        self.assertEqual(len(commands), 1)
        self.assertEqual(commands[0][0], "/usr/bin/rpm")
        self.assertEqual(commands[0][2], "/snapshot/var/lib/rpm")
        # A %config file is operator-owned and a %ghost entry runtime-owned, so
        # neither is read as a defect in what the transaction installed.
        for option in ("--verify", "--noconfig", "--noghost", "--noscripts"):
            self.assertIn(option, commands[0])
        self.assertEqual(commands[0][-1], "cephadm-0:20.2.1-405.el9cp.x86_64")
        with patch.object(native.bounded_process, "run", verify(1)):
            self.assertFalse(
                native.verified_files(platform, "/snapshot", [installed], "/scratch")
            )
        # Nothing installed is nothing to prove.
        with patch.object(native.bounded_process, "run", verify(1)):
            self.assertTrue(
                native.verified_files(platform, "/snapshot", [], "/scratch")
            )


class AuthoredNativeSelections(unittest.TestCase):
    def test_native_dnf_receives_authored_coordinate_without_bootwright_parsing(self):
        import sys
        import types

        for coordinate in ("x" * 1024, "latest", "83.7-42.vendor", "not a version", "--invalid", "2:1.0~candidate-7", "absent@version"):
            seen = []
            chosen = package("nfs-ganesha")
            query = SimpleNamespace(available=lambda: [chosen])
            subject = types.ModuleType("dnf.subject")
            subject.Subject = lambda value: seen.append(("subject", value)) or SimpleNamespace(get_best_query=lambda *args, **kwargs: query)
            dnf = types.ModuleType("dnf")
            dnf.subject = subject
            base = SimpleNamespace(sack=object(), install=lambda value, **kwargs: seen.append(("install", value)))
            with patch.dict(sys.modules, {"dnf": dnf, "dnf.subject": subject}):
                self.assertEqual(native.authored_candidates(base, "nfs-ganesha", coordinate), [chosen])
            self.assertEqual(seen, [("install", "nfs-ganesha-" + coordinate), ("subject", "nfs-ganesha-" + coordinate)])

    def test_each_selected_package_has_an_actual_native_root(self):
        request = {"policy": "ceph-packages-v2", "requirements": {"cephPackages": True},
                   "versions": {"ceph": "synthetic-ceph", "ansible": "synthetic-ansible", "nfsGanesha": "synthetic-nfs",
                                "license": "latest", "inheritedPolicy": "latest"}}
        roots = native.selected_roots(request)
        self.assertIn(("ceph", "cephadm", "synthetic-ceph"), roots)
        self.assertIn(("ansible", "cephadm-ansible", "synthetic-ansible"), roots)
        self.assertIn(("nfsGanesha", "nfs-ganesha", "synthetic-nfs"), roots)
