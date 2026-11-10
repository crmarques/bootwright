"""Tests of the runtime lock tool; run: python3 -I -B runtime_lock_test.py."""

from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

_specification = importlib.util.spec_from_file_location(
    "runtime_lock", Path(__file__).with_name("runtime_lock.py")
)
runtime_lock = importlib.util.module_from_spec(_specification)
_specification.loader.exec_module(runtime_lock)

ToolError = runtime_lock.ToolError
HOST = "https://files.pythonhosted.org"


def entry(name: str, version: str, filename: str, content: bytes) -> dict:
    """Return one pip report install entry for a file of the given content."""
    return {
        "download_info": {
            "url": f"{HOST}/packages/aa/bb/{filename}",
            "archive_info": {"hashes": {"sha256": hashlib.sha256(content).hexdigest()}},
        },
        "metadata": {"name": name, "version": version},
    }


class Mirror:
    """A directory serving the index and files the way the tool reads them."""

    def __init__(self, root: Path):
        self.root = root
        self.wheels: dict[str, bytes] = {}

    def add(self, name: str, version: str, filename: str, license_lines: tuple[str, ...] | None = None) -> dict:
        lines = ("License-Expression: MIT",) if license_lines is None else license_lines
        metadata = "\n".join(("Metadata-Version: 2.4", f"Name: {name}", f"Version: {version}", *lines)) + "\n\nbody\n"
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr(zipfile.ZipInfo(f"{name.lower()}-{version}.dist-info/METADATA", (2020, 1, 1, 0, 0, 0)), metadata)
        content = buffer.getvalue()
        self.wheels[filename] = content
        path = self.root / "files/packages/aa/bb" / filename
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        return entry(name, version, filename, content)

    def args(self) -> list[str]:
        return ["--pypi", f"file://{self.root}", "--files", f"file://{self.root}/files"]


def run(*argv: str) -> tuple[int, str, str]:
    """Run the tool's main and return its status and both streams."""
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        status = runtime_lock.main(list(argv))
    return status, out.getvalue(), err.getvalue()


class Helpers(unittest.TestCase):
    def test_normalize_follows_pep_503(self):
        self.assertEqual(runtime_lock.normalize("PyYAML"), "pyyaml")
        self.assertEqual(runtime_lock.normalize("ansible_core"), "ansible-core")
        self.assertEqual(runtime_lock.normalize("Zope.Interface"), "zope-interface")

    def test_release_key_takes_plain_releases_only(self):
        self.assertEqual(runtime_lock.release_key("2.21.4"), (2, 21, 4))
        for version in ("2.21.4rc1", "2.21.dev0", "1!2.0", "2.0+local", ""):
            self.assertIsNone(runtime_lock.release_key(version), version)

    def test_glibc_needed_takes_the_oldest_tag(self):
        needed = runtime_lock.glibc_needed
        self.assertIsNone(needed("jinja2-3.1.6-py3-none-any.whl"))
        self.assertEqual(
            needed("a-1-cp312-cp312-manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64.whl"),
            17,
        )
        self.assertEqual(needed("a-1-cp311-abi3-manylinux_2_34_x86_64.whl"), 34)
        self.assertEqual(needed("a-1-cp311-abi3-manylinux1_x86_64.whl"), 5)

    def test_glibc_needed_refuses_other_platforms(self):
        for filename in (
            "a-1-cp312-cp312-musllinux_1_2_x86_64.whl",
            "a-1-cp312-cp312-manylinux_2_28_aarch64.whl",
            "a-1-cp312-cp312-win_amd64.whl",
            "a-1.tar.gz",
            "a-1-2-cp312-cp312-manylinux_2_28_x86_64.whl",
        ):
            with self.assertRaises(ToolError, msg=filename):
                runtime_lock.glibc_needed(filename)

    def test_a_minor_line_becomes_a_range(self):
        self.assertEqual(runtime_lock.ansible_requirement("2.21.4"), "ansible-core==2.21.4")
        self.assertEqual(runtime_lock.ansible_requirement("2.21"), "ansible-core>=2.21,<2.22")
        for version in ("2", "2.21.4.1", "2.21rc1", "latest"):
            with self.assertRaises(ToolError, msg=version):
                runtime_lock.ansible_requirement(version)

    def test_abis_sort_newest_first_and_stay_distinct(self):
        self.assertEqual(runtime_lock.parse_abis("3.9,3.14,3.12"), ["3.14", "3.12", "3.9"])
        for value in ("3.12,3.12", "2.7", "3", "3.12,", "3.x"):
            with self.assertRaises(ToolError, msg=value):
                runtime_lock.parse_abis(value)

    def test_tested_releases_parse_and_refuse_repeats(self):
        self.assertEqual(
            runtime_lock.parse_tested(["fedora:43,44", "rhel:9"]),
            [{"id": "fedora", "versions": ["43", "44"]}, {"id": "rhel", "versions": ["9"]}],
        )
        for values in (["fedora"], ["fedora:"], ["Fedora:43"], ["fedora:43", "fedora:44"], ["fedora:4x"]):
            with self.assertRaises(ToolError, msg=values):
                runtime_lock.parse_tested(values)

    def test_a_mirror_root_replaces_only_a_leading_host(self):
        url = f"{HOST}/packages/aa/bb/x.whl"
        self.assertEqual(runtime_lock.rewritten(url, HOST, "file:///m/"), "file:///m/packages/aa/bb/x.whl")
        self.assertEqual(runtime_lock.rewritten(url, HOST, HOST), url)
        with self.assertRaises(ToolError):
            runtime_lock.rewritten("https://example.com/x.whl", HOST, "file:///m")

    def test_only_files_pythonhosted_packages_are_locked(self):
        runtime_lock.check_pythonhosted(f"{HOST}/packages/aa/bb/x.whl")
        for url in (
            "http://files.pythonhosted.org/packages/x.whl",
            "https://example.com/packages/x.whl",
            "https://files.pythonhosted.org:8443/packages/x.whl",
            "https://files.pythonhosted.org/simple/x.whl",
            "https://files.pythonhosted.org/packages/x.whl?a=1",
            "file:///packages/x.whl",
        ):
            with self.assertRaises(ToolError, msg=url):
                runtime_lock.check_pythonhosted(url)

    def test_the_newest_release_skips_yanked_and_other_lines(self):
        index = {
            "releases": {
                "2.21.4": [{}],
                "2.21.5": [{}],
                "2.21.6": [{"yanked": True}],
                "2.21.7rc1": [{}],
                "2.21.8": [],
                "2.22.0": [{}],
            }
        }
        self.assertEqual(runtime_lock.newest_release(index, (2, 21)), "2.21.5")
        self.assertEqual(runtime_lock.newest_release(index, None), "2.22.0")
        self.assertIsNone(runtime_lock.newest_release({"releases": {}}, None))

    def test_pip_runs_with_a_bare_environment(self):
        environment = runtime_lock.pip_environment(Path("/home-area"))
        self.assertEqual(environment["PIP_CONFIG_FILE"], "/dev/null")
        self.assertEqual(environment["HOME"], "/home-area")
        self.assertLessEqual(set(environment), {"PATH", "HOME", "LC_ALL", "PIP_CONFIG_FILE", *runtime_lock.PASSED_ENVIRONMENT})


class Report(unittest.TestCase):
    def test_a_report_gives_each_file_its_identity_and_hash(self):
        report = {
            "install": [
                entry("urllib3", "2.8.0", "urllib3-2.8.0-py3-none-any.whl", b"u"),
                entry("PyYAML", "6.0.3", "pyyaml-6.0.3-cp312-cp312-manylinux2014_x86_64.whl", b"y"),
                entry("ansible-core", "2.21.4", "ansible_core-2.21.4-py3-none-any.whl", b"a"),
            ]
        }
        files = runtime_lock.report_files(report, "3.12")
        self.assertEqual([file["name"] for file in files], ["ansible-core", "pyyaml", "urllib3"])
        self.assertEqual(files[1]["sha256"], hashlib.sha256(b"y").hexdigest())
        self.assertEqual(files[1]["filename"], "pyyaml-6.0.3-cp312-cp312-manylinux2014_x86_64.whl")

    def test_the_omitted_distributions_leave_the_lock(self):
        omitted = [
            entry("Cryptography", "50.0.2", "cryptography-50.0.2-cp311-abi3-manylinux2014_x86_64.whl", b"c"),
            entry("cffi", "2.1.1", "cffi-2.1.1-cp312-cp312-manylinux2014_x86_64.whl", b"f"),
            entry("pycparser", "3.11", "pycparser-3.11-py3-none-any.whl", b"p"),
            entry("resolvelib", "1.2.1", "resolvelib-1.2.1-py3-none-any.whl", b"r"),
        ]
        report = {
            "install": [
                entry("urllib3", "2.8.0", "urllib3-2.8.0-py3-none-any.whl", b"u"),
                *omitted,
                entry("ansible-core", "2.21.4", "ansible_core-2.21.4-py3-none-any.whl", b"a"),
            ]
        }
        files = runtime_lock.report_files(report, "3.12")
        self.assertEqual([file["name"] for file in files], ["ansible-core", "urllib3"])
        self.assertEqual(sorted(runtime_lock.normalize(e["metadata"]["name"]) for e in omitted), list(runtime_lock.OMITTED))

    def test_a_report_without_the_requested_packages_is_refused(self):
        report = {"install": [entry("urllib3", "2.8.0", "urllib3-2.8.0-py3-none-any.whl", b"u")]}
        with self.assertRaises(ToolError):
            runtime_lock.report_files(report, "3.12")

    def test_a_file_outside_pythonhosted_or_without_a_hash_is_refused(self):
        base = [
            entry("urllib3", "2.8.0", "urllib3-2.8.0-py3-none-any.whl", b"u"),
            entry("ansible-core", "2.21.4", "ansible_core-2.21.4-py3-none-any.whl", b"a"),
        ]
        elsewhere = json.loads(json.dumps(base))
        elsewhere[0]["download_info"]["url"] = "https://example.com/packages/u.whl"
        unhashed = json.loads(json.dumps(base))
        del unhashed[0]["download_info"]["archive_info"]
        for install in (elsewhere, unhashed):
            with self.assertRaises(ToolError):
                runtime_lock.report_files({"install": install}, "3.12")


class Licenses(unittest.TestCase):
    def setUp(self):
        self.area = tempfile.TemporaryDirectory()
        self.addCleanup(self.area.cleanup)
        self.mirror = Mirror(Path(self.area.name))

    def declared(self, *lines: str):
        self.mirror.add("Demo", "1.0", "demo-1.0-py3-none-any.whl", lines)
        path = self.mirror.root / "files/packages/aa/bb/demo-1.0-py3-none-any.whl"
        return runtime_lock.wheel_license(path)

    def test_a_license_expression_is_taken_as_declared(self):
        self.assertEqual(self.declared("License-Expression: Apache-2.0 OR BSD-2-Clause"), ("Apache-2.0 OR BSD-2-Clause", "License-Expression"))

    def test_an_unambiguous_classifier_names_its_license(self):
        self.assertEqual(self.declared("License: MIT", "Classifier: License :: OSI Approved :: MIT License"), ("MIT", "Classifier"))

    def test_an_ambiguous_conflicting_or_missing_declaration_names_none(self):
        self.assertIsNone(self.declared("Classifier: License :: OSI Approved :: BSD License"))
        self.assertIsNone(self.declared("Classifier: License :: OSI Approved :: MIT License", "Classifier: License :: OSI Approved :: Apache Software License"))
        self.assertIsNone(self.declared("License: Proprietary"))
        self.assertIsNone(self.declared())

    def test_an_expression_that_is_not_plain_spdx_is_refused(self):
        for value in ("(MIT OR Apache-2.0)", "MIT; rm", "MIT AND"):
            with self.assertRaises(ToolError, msg=value):
                self.declared(f"License-Expression: {value}")

    def test_a_file_that_is_not_a_wheel_is_refused(self):
        path = self.mirror.root / "not.whl"
        path.write_bytes(b"plain")
        with self.assertRaises(ToolError):
            runtime_lock.wheel_license(path)


class Commands(unittest.TestCase):
    def setUp(self):
        self.area = tempfile.TemporaryDirectory()
        self.addCleanup(self.area.cleanup)
        self.root = Path(self.area.name)
        self.mirror = Mirror(self.root / "mirror")
        pip = self.mirror.add("pip", "26.0.1", "pip-26.0.1-py3-none-any.whl")
        digest = pip["download_info"]["archive_info"]["hashes"]["sha256"]
        (self.mirror.root / "pypi/pip/26.0.1").mkdir(parents=True)
        (self.mirror.root / "pypi/pip/26.0.1/json").write_text(
            json.dumps(
                {
                    "urls": [
                        {
                            "filename": "pip-26.0.1-py3-none-any.whl",
                            "packagetype": "bdist_wheel",
                            "url": pip["download_info"]["url"],
                            "digests": {"sha256": digest},
                            "size": len(self.mirror.wheels["pip-26.0.1-py3-none-any.whl"]),
                        }
                    ]
                }
            )
        )
        reports = {
            abi: {
                "install": [
                    self.mirror.add("ansible-core", "2.21.4", "ansible_core-2.21.4-py3-none-any.whl"),
                    self.mirror.add("urllib3", "2.8.0", "urllib3-2.8.0-py3-none-any.whl"),
                    self.mirror.add("PyYAML", "6.0.3", f"pyyaml-6.0.3-cp3{abi[2:]}-cp3{abi[2:]}-manylinux_2_28_x86_64.whl"),
                ]
            }
            for abi in ("3.12", "3.13")
        }
        self.report = self.root / "report.json"
        self.report.write_text(json.dumps(reports))
        self.output = self.root / "lock.json"
        self.licenses = self.root / "licenses.json"

    def generate(self, *extra: str) -> tuple[int, str, str]:
        return run(
            "generate",
            "--ansible-core",
            "2.21",
            "--python",
            "3.12,3.13",
            "--installer",
            "pip==26.0.1",
            "--output",
            str(self.output),
            "--report",
            str(self.report),
            *self.mirror.args(),
            *extra,
        )

    def test_generate_writes_the_lock_in_the_order_go_reads_it(self):
        status, _, err = self.generate("--tested", "rhel:9,10")
        self.assertEqual(status, 0, err)
        lock = json.loads(self.output.read_text())
        self.assertEqual(
            list(lock),
            ["format", "ansibleCore", "pythonABIs", "glibcFloor", "tested", "installer", "abis"],
        )
        self.assertEqual(lock["pythonABIs"], ["3.13", "3.12"])
        self.assertEqual(lock["glibcFloor"], "2.28")
        self.assertEqual(lock["tested"], [{"id": "rhel", "versions": ["9", "10"]}])
        self.assertEqual(list(lock["abis"]), ["3.12", "3.13"])
        self.assertEqual(
            list(lock["abis"]["3.12"][0]),
            ["name", "version", "filename", "url", "sha256", "size"],
        )
        self.assertEqual(lock["abis"]["3.12"][0]["size"], len(self.mirror.wheels["ansible_core-2.21.4-py3-none-any.whl"]))
        self.assertEqual(self.output.read_text(), json.dumps(lock, indent=2) + "\n")

    def test_generate_writes_each_distributions_declared_license_beside_the_lock(self):
        self.assertEqual(self.generate()[0], 0)
        table = json.loads(self.licenses.read_text())
        self.assertEqual(table["format"], "bootwright.controller.runtime-licenses-v1")
        self.assertEqual(
            [(entry["name"], entry["version"]) for entry in table["distributions"]],
            [("ansible-core", "2.21.4"), ("pip", "26.0.1"), ("pyyaml", "6.0.3"), ("urllib3", "2.8.0")],
        )
        self.assertTrue(all(entry["license"] == "MIT" and entry["source"] == "License-Expression" for entry in table["distributions"]))
        self.assertEqual(self.licenses.read_text(), json.dumps(table, indent=2) + "\n")

    def test_a_wheel_that_declares_no_license_needs_a_reviewed_entry_and_keeps_it(self):
        self.report.write_text(json.dumps({abi: {"install": [
            self.mirror.add("ansible-core", "2.21.4", "ansible_core-2.21.4-py3-none-any.whl"),
            self.mirror.add("urllib3", "2.8.0", "urllib3-2.8.0-py3-none-any.whl"),
            self.mirror.add("Jinja2", "3.1.6", "jinja2-3.1.6-py3-none-any.whl", ("Classifier: License :: OSI Approved :: BSD License",)),
            self.mirror.add("PyYAML", "6.0.3", f"pyyaml-6.0.3-cp3{abi[2:]}-cp3{abi[2:]}-manylinux_2_28_x86_64.whl"),
        ]} for abi in ("3.12", "3.13")}))
        status, _, err = self.generate()
        self.assertEqual(status, 1)
        self.assertIn("jinja2-3.1.6-py3-none-any.whl declares no License-Expression", err)
        self.assertFalse(self.output.exists() or self.licenses.exists())
        entry = {"name": "jinja2", "version": "3.1.6", "license": "BSD-3-Clause", "source": "reviewed: LICENSE.txt is the BSD 3-clause text"}
        self.licenses.write_text(json.dumps({"format": "bootwright.controller.runtime-licenses-v1", "distributions": [entry]}))
        self.assertEqual(self.generate()[0], 0)
        table = json.loads(self.licenses.read_text())["distributions"]
        self.assertIn(entry, table)
        self.assertEqual(self.generate()[0], 0)
        self.assertEqual(json.loads(self.licenses.read_text())["distributions"], table)

    def test_a_rerun_carries_the_tested_releases_forward(self):
        self.assertEqual(self.generate("--tested", "rhel:9")[0], 0)
        first = self.output.read_text()
        self.assertEqual(self.generate()[0], 0)
        self.assertEqual(self.output.read_text(), first)

    def test_a_download_that_contradicts_the_report_keeps_the_old_lock(self):
        self.assertEqual(self.generate()[0], 0)
        first = self.output.read_text()
        (self.mirror.root / "files/packages/aa/bb/urllib3-2.8.0-py3-none-any.whl").write_bytes(b"other")
        status, _, err = self.generate()
        self.assertEqual(status, 1)
        self.assertIn("urllib3-2.8.0-py3-none-any.whl hashes to", err)
        self.assertEqual(self.output.read_text(), first)

    def test_a_report_missing_an_abi_is_refused(self):
        status, _, err = run(
            "generate", "--ansible-core", "2.21.4", "--python", "3.12,3.14", "--installer", "pip==26.0.1",
            "--output", str(self.output), "--report", str(self.report), *self.mirror.args(),
        )
        self.assertEqual(status, 1)
        self.assertIn("no report for Python 3.14", err)
        self.assertFalse(self.output.exists())

    def test_verify_passes_a_lock_and_names_a_changed_file(self):
        self.assertEqual(self.generate()[0], 0)
        status, out, _ = run("verify", "--lock", str(self.output), *self.mirror.args())
        self.assertEqual((status, out.strip()), (0, "verified 5 files"))
        (self.mirror.root / "files/packages/aa/bb/ansible_core-2.21.4-py3-none-any.whl").write_bytes(b"x")
        status, _, err = run("verify", "--lock", str(self.output), *self.mirror.args())
        self.assertEqual(status, 1)
        self.assertIn("ansible_core-2.21.4-py3-none-any.whl", err)

    def test_verify_holds_the_license_table_to_the_wheels(self):
        self.assertEqual(self.generate()[0], 0)
        good = self.licenses.read_text()
        table = json.loads(good)
        table["distributions"][0]["license"] = "GPL-3.0-or-later"
        table["distributions"].append({"name": "gone", "version": "1.0", "license": "MIT", "source": "License-Expression"})
        del table["distributions"][2]
        self.licenses.write_text(json.dumps(table))
        status, _, err = run("verify", "--lock", str(self.output), *self.mirror.args())
        self.assertEqual(status, 1)
        self.assertIn("ansible_core-2.21.4-py3-none-any.whl: declares MIT in License-Expression, the license table has GPL-3.0-or-later", err)
        self.assertIn("the license table has no entry for pyyaml 6.0.3", err)
        self.assertIn("gone 1.0 is in the license table and not in the lock", err)
        self.licenses.write_text(good)
        self.assertEqual(run("verify", "--lock", str(self.output), *self.mirror.args())[0], 0)

    def test_verify_refuses_a_lock_that_names_another_host(self):
        self.assertEqual(self.generate()[0], 0)
        lock = json.loads(self.output.read_text())
        lock["installer"]["url"] = "https://example.com/packages/pip.whl"
        self.output.write_text(json.dumps(lock))
        status, _, err = run("verify", "--lock", str(self.output), *self.mirror.args())
        self.assertEqual(status, 1)
        self.assertIn("pip-26.0.1-py3-none-any.whl", err)

    def test_a_missing_lock_is_one_line(self):
        status, _, err = run("verify", "--lock", str(self.root / "none.json"), *self.mirror.args())
        self.assertEqual(status, 1)
        self.assertTrue(err.startswith("runtime-lock: cannot read the lock"), err)

    def test_outdated_never_fails_on_what_it_cannot_check(self):
        self.assertEqual(self.generate()[0], 0)
        status, out, _ = run("outdated", "--lock", str(self.output), *self.mirror.args())
        self.assertEqual(status, 0)
        self.assertIn("could not check", out)


if __name__ == "__main__":
    unittest.main()
