from __future__ import annotations

import importlib.util
import pathlib
import unittest


_FILTER_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_virtctl_filter",
    _FILTER_DIR / "virtctl.py",
)
assert _spec and _spec.loader, "could not locate virtctl.py"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_virtctl_version = _module.bootwright_virtctl_version


class VirtctlVersion(unittest.TestCase):
    def test_extracts_red_hat_version_info(self):
        output = (
            'Client Version: version.Info{GitVersion:"v1.7.4", '
            'GitCommit:"2c30e3249a21ce68f4b9bd2e7f2dd6035fa3d68d", '
            'GitTreeState:"clean", BuildDate:"2026-07-07T02:06:03Z", '
            'GoVersion:"go1.24.13 (Red Hat 1.24.13-5.el9_6) '
            'X:strictfipsruntime", Compiler:"gc", Platform:"linux/amd64"}'
        )

        self.assertEqual(bootwright_virtctl_version(output), "v1.7.4")

    def test_returns_empty_for_unknown_output(self):
        self.assertEqual(bootwright_virtctl_version("Client Version: unknown"), "")
        self.assertEqual(bootwright_virtctl_version(None), "")


class FilterRegistration(unittest.TestCase):
    def test_filter_module_exposes_filter(self):
        filters = _module.FilterModule().filters()

        self.assertIs(filters["bootwright_virtctl_version"], bootwright_virtctl_version)


if __name__ == "__main__":
    unittest.main()
