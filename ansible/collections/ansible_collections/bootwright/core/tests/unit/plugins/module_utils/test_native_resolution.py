"""Test bounded native resolution policy without host metadata or package effects."""

import copy
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import (
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
            versions=dict(nmstate="latest"),
            requirements={},
            plan=dict(packages=[record], repositories=[]),
        )

    def assemble(self, request, root, before, inbound, outbound, requested="latest"):
        return native.assemble(
            request,
            before,
            [("nmstate", requested, root)],
            inbound,
            outbound,
            False,
            compare,
            "4.20.0",
        )

    def test_no_action_retains_root_integrity_and_equal_inventory(self):
        root = package("nmstate")
        request = self.request(root)
        plan = self.assemble(request, root, [native.identity(root)], [], [])
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
            root = package("nmstate")
            previous = native.identity(package("nmstate", old_version))
            plan = self.assemble(
                self.request(root), root, [previous], [root], [previous], requested
            )
            self.assertEqual(plan["actions"][0]["kind"], expected)
            self.assertNotEqual(plan["beforeSHA256"], plan["afterSHA256"])
        with self.assertRaises(ValueError):
            self.assemble(self.request(root), root, [previous], [root], [previous])

    def test_reinstall_erase_architecture_and_unpaired_replacement_refused(self):
        root = package("nmstate")
        request = self.request(root)
        for before, outbound in [
            ([native.identity(root)], [native.identity(root)]),
            (
                [native.identity(root), native.identity(package("unrelated"))],
                [native.identity(package("unrelated"))],
            ),
            (
                [native.identity(package("nmstate", "1", arch="noarch"))],
                [native.identity(package("nmstate", "1", arch="noarch"))],
            ),
            ([native.identity(package("nmstate", "1"))], []),
        ]:
            with self.assertRaises(ValueError):
                self.assemble(request, root, before, [root], outbound)

    def test_unrelated_dependency_downgrade_refused(self):
        root = package("nmstate")
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
        value = native.identity(package("nmstate", "2.1.3", "5.fc43"))
        for requested in ("latest", "2.1.3", "0:2.1.3-5.fc43"):
            self.assertTrue(native.version_matches(value, requested))
        for requested in ("2.1.4", "2.1.3-6.fc43", "1:2.1.3-5.fc43"):
            self.assertFalse(native.version_matches(value, requested))
        self.assertEqual(native.inventory_digest([value]), native.digest([value]))

    def test_present_reports_roots_by_name_without_verifying_files(self):
        roots = [
            {"key": key, "requested": "latest", "package": native.identity(root)}
            for key, root in (
                ("podman", package("podman", "5.8.4", "1.fc43")),
                ("nmstate", package("nmstate", "2.2.0", "1.fc43")),
            )
        ]
        content = {"format": native.FORMAT, "roots": roots}
        plan = dict(content, digest=native.digest(content))
        commands = []

        def run(command, **_):
            commands.append(command)
            if command[-1] == "podman":
                return SimpleNamespace(
                    returncode=0, stdout=b"podman\t(none)\t5.8.5\t1.fc43\tx86_64\n"
                )
            return SimpleNamespace(returncode=1, stdout=b"")

        request = {
            "platform": {"os": "fedora", "release": "43", "architecture": "amd64"},
            "snapshot": "/snapshot",
            "plan": plan,
        }
        with patch.object(native.subprocess, "run", run):
            result = native.present(request, "/scratch")
        self.assertFalse(result["rootsReady"])
        self.assertEqual(
            result["roots"][0]["installed"],
            native.identity(package("podman", "5.8.5", "1.fc43")),
        )
        self.assertIsNone(result["roots"][1]["installed"])
        # Presence queries the snapshot by name only: no file verification.
        self.assertEqual(len(commands), 2)
        for command in commands:
            self.assertEqual(command[0], "/usr/bin/rpm")
            self.assertIn("-q", command)
            self.assertNotIn("--verify", command)
            self.assertEqual(command[2], "/snapshot/usr/lib/sysimage/rpm")
        with self.assertRaises(ValueError):
            native.installed_identity(b"other\t(none)\t1\t1\tx86_64\n", "podman")
