"""Test bounded native resolution policy without host metadata or package effects."""

import copy
import io
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

    def test_inspect_reports_readiness_by_presence_without_verifying_files(self):
        root = package("libvirt-daemon-driver-qemu", "11.6.0", "3.fc43")
        installed = native.identity(root)
        content = {
            "format": native.FORMAT,
            "packages": [dict(installed, source={"id": "root-source"})],
        }
        plan = dict(content, digest=native.digest(content))
        request = {
            "platform": {"os": "fedora", "release": "43", "architecture": "amd64"},
            "snapshot": "/snapshot",
            "plan": plan,
        }
        commands = []

        def run(command, **_):
            commands.append(command)
            return SimpleNamespace(returncode=0)

        def inspect(inventory):
            with patch.object(native, "setup_dnf5", lambda *_, **__: None):
                with patch.object(native, "inventory5", lambda _: inventory):
                    with patch.object(native.subprocess, "run", run):
                        return native.inspect(request, "/scratch")

        result = inspect([installed])
        self.assertTrue(result["rootsReady"])
        self.assertEqual(result["inventory"], [installed])
        # Readiness after a transaction is presence: the closure is never read
        # back and probed twice for one effect.
        self.assertEqual(commands, [])
        # A closure package the host does not carry is still not ready.
        other = native.identity(package("libvirt-daemon-driver-qemu", "11.7.0"))
        self.assertFalse(inspect([other])["rootsReady"])

    def test_verified_files_covers_installed_files_and_no_host_owned_path(self):
        installed = native.identity(package("swtpm", "0.10.2", "1.fc43"))
        platform = {"os": "fedora", "release": "43", "architecture": "amd64"}
        commands = []

        def verify(returncode):
            def run(command, **_):
                commands.append(command)
                return SimpleNamespace(returncode=returncode)

            return run

        with patch.object(native.subprocess, "run", verify(0)):
            self.assertTrue(
                native.verified_files(platform, "/snapshot", [installed], "/scratch")
            )
        self.assertEqual(len(commands), 1)
        self.assertEqual(commands[0][0], "/usr/bin/rpm")
        self.assertEqual(commands[0][2], "/snapshot/usr/lib/sysimage/rpm")
        # A %config file is operator-owned and a %ghost entry runtime-owned, so
        # neither is read as a defect in what the transaction installed.
        for option in ("--verify", "--noconfig", "--noghost", "--noscripts"):
            self.assertIn(option, commands[0])
        self.assertEqual(commands[0][-1], "swtpm-0:0.10.2-1.fc43.x86_64")
        with patch.object(native.subprocess, "run", verify(1)):
            self.assertFalse(
                native.verified_files(platform, "/snapshot", [installed], "/scratch")
            )
        # Nothing installed is nothing to prove.
        with patch.object(native.subprocess, "run", verify(1)):
            self.assertTrue(
                native.verified_files(platform, "/snapshot", [], "/scratch")
            )


def package5(name, version="2", release="1", arch="x86_64"):
    return SimpleNamespace(
        get_name=lambda: name,
        get_epoch=lambda: 0,
        get_version=lambda: version,
        get_release=lambda: release,
        get_arch=lambda: arch,
    )


class Dnf5Transaction(unittest.TestCase):
    """solve5 reads libdnf5's transaction: a root the host already holds comes
    back as a reason change, which moves no file, so it is neither inbound nor
    outbound; any other action outside install, upgrade, downgrade, replaced
    and remove still refuses."""

    ACTIONS = {
        "Install": 1,
        "Upgrade": 2,
        "Downgrade": 3,
        "Reinstall": 4,
        "Remove": 5,
        "Replaced": 6,
        "Reason Change": 7,
        "Switch": 8,
    }

    def solve(self, entries):
        actions = self.ACTIONS
        names = {code: name for name, code in actions.items()}
        root = package5("podman")

        class Transaction:
            def get_problems(self):
                return 0

            def get_transaction_packages(self):
                return [
                    SimpleNamespace(
                        get_action=lambda code=actions[kind]: code,
                        get_package=lambda pkg=pkg: pkg,
                    )
                    for kind, pkg in entries
                ]

        class Goal:
            def __init__(self, base):
                pass

            def add_rpm_install(self, pkg):
                pass

            def resolve(self):
                return Transaction()

        class Query:
            def __init__(self, base):
                pass

            def filter_available(self):
                pass

            def filter_name(self, names):
                pass

            def __iter__(self):
                return iter([root])

        library = SimpleNamespace(
            base=SimpleNamespace(Goal=Goal, GoalProblem_NO_PROBLEM=0),
            rpm=SimpleNamespace(PackageQuery=Query, rpmvercmp=compare),
            transaction=SimpleNamespace(
                transaction_item_action_to_string=lambda code: names[code]
            ),
            conf=SimpleNamespace(
                get_library_version=lambda: SimpleNamespace(major=5, minor=4, micro=6)
            ),
        )
        seen = {}

        def assemble(request, before, roots, inbound, outbound, five, cmp, version):
            seen.update(roots=roots, inbound=inbound, outbound=outbound)
            return {"assembled": True}

        request = dict(
            platform=dict(os="fedora", release="44", architecture="amd64"),
            versions=dict(podman="latest"),
            requirements=dict(
                containerRuntime=True, libvirtClient=False, hypervisor=False, installerMedia=False
            ),
            repositories=[],
        )
        with patch.dict("sys.modules", libdnf5=library), patch.object(
            native, "setup_dnf5", return_value=object()
        ), patch.object(native, "inventory5", return_value=[]), patch.object(
            native, "selected_roots", return_value=[("podman", "podman", "latest")]
        ), patch.object(native, "choose", return_value=(root, [root])), patch.object(
            native, "package_record", return_value={"source": {"sha256": "c" * 64}}
        ), patch.object(native, "assemble", side_effect=assemble):
            self.assertEqual(native.solve5(request, "/scratch"), {"assembled": True})
        return root, seen

    def test_an_installed_root_asked_for_again_moves_no_package(self):
        root, seen = self.solve([("Reason Change", package5("podman"))])
        self.assertEqual(seen["inbound"], [])
        self.assertEqual(seen["outbound"], [])
        self.assertEqual(seen["roots"], [("podman", "latest", root)])

    def test_moves_are_inbound_and_replacements_outbound(self):
        new, up, old = package5("lorax"), package5("podman", version="3"), package5("podman")
        _, seen = self.solve(
            [("Install", new), ("Upgrade", up), ("Replaced", old), ("Reason Change", package5("nmstate"))]
        )
        self.assertEqual(seen["inbound"], [new, up])
        self.assertEqual(seen["outbound"], [native.identity(old, True)])

    def test_any_other_action_refuses(self):
        for kind in ("Reinstall", "Switch"):
            with self.subTest(kind), self.assertRaises(ValueError) as refused:
                self.solve([(kind, package5("podman"))])
            self.assertEqual(str(refused.exception), "unrequested native transaction action")


class Entrypoint(unittest.TestCase):
    """A refused operation writes exactly two lines and exits 1: its closed
    class, then its type and the first line of its text, bounded."""

    def entry(self, error):
        stderr = io.StringIO()
        with patch.object(native, "main", side_effect=error), patch.object(
            native.sys, "stderr", stderr
        ):
            with self.assertRaises(SystemExit) as exited:
                native.entry()
        self.assertEqual(exited.exception.code, 1)
        return stderr.getvalue()

    def test_the_entrypoint_writes_one_classified_reason(self):
        self.assertEqual(
            self.entry(ValueError("native dependency solve")),
            "refused solver-conflict\nValueError: native dependency solve\n",
        )

    def test_each_refusal_has_its_class(self):
        depsolve = type("DepsolveError", (RuntimeError,), {})
        marking = type("MarkingError", (RuntimeError,), {})
        for error, expected in (
            (ValueError("requested native release unavailable"), "missing-candidate"),
            (ValueError("native root unresolved"), "missing-candidate"),
            (marking("no match"), "missing-candidate"),
            (depsolve("conflict"), "solver-conflict"),
            (ValueError("native package signature"), "signature"),
            (ValueError("native source integrity"), "signature"),
            (ValueError("native database lock"), "database"),
            (ValueError("native database snapshot"), "database"),
            (ValueError("native database changed"), "database"),
            (ValueError("native transaction failed"), "transaction"),
            (ValueError("native vendor hook failed"), "transaction"),
            (ValueError("native transaction postcondition"), "postcondition"),
            (ValueError("native inventory changed before transaction"), "postcondition"),
            (ValueError("native transaction changed"), "postcondition"),
            (ImportError("No module named dnf"), "foundation"),
            (KeyError("operation"), "internal"),
            (ValueError("native request limit"), "internal"),
        ):
            with self.subTest(repr(error)):
                self.assertEqual(self.entry(error).split("\n")[0], "refused " + expected)

    def test_the_detail_is_one_bounded_line_without_control_characters(self):
        lines = self.entry(RuntimeError("\x1b[31m" + "x" * 400 + "\nsecond line")).split("\n")
        self.assertEqual(len(lines), 3)
        self.assertEqual(lines[2], "")
        self.assertTrue(lines[1].startswith("RuntimeError: [31m"))
        self.assertEqual(len(lines[1].encode("utf-8")), 200)
