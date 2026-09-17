"""Exercise exact transaction grants and failure evidence without host writes."""

import copy
import hashlib
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import (
    native_apply,
    native_resolution,
    native_signatures,
)


class NativeApply(unittest.TestCase):
    def plan(self):
        identity = dict(
            name="fixture", epoch=0, version="2", release="1", architecture="x86_64"
        )
        return dict(
            solver="dnf5",
            solverVersion="5.4",
            beforeSHA256="a" * 64,
            afterSHA256="b" * 64,
            digest="c" * 64,
            platform=dict(os="fedora", release="43", architecture="amd64"),
            requirements={},
            requests={},
            packages=[],
            actions=[
                dict(
                    kind="install",
                    after=identity,
                    sourceID="fixture-rpm",
                    reason="root",
                )
            ],
        )

    def test_exact_plan_refuses_extra_replacement_and_wrong_inventory(self):
        expected = self.plan()
        native_apply.same_transaction(expected, copy.deepcopy(expected))
        for key in ("beforeSHA256", "afterSHA256", "solverVersion"):
            actual = copy.deepcopy(expected)
            actual[key] = "different"
            with self.assertRaises(ValueError):
                native_apply.same_transaction(expected, actual)
        actual = copy.deepcopy(expected)
        actual["actions"].append(dict(actual["actions"][0], sourceID="extra"))
        with self.assertRaises(ValueError):
            native_apply.same_transaction(expected, actual)
        actual = copy.deepcopy(expected)
        actual["actions"][0]["kind"] = "downgrade"
        with self.assertRaises(ValueError):
            native_apply.same_transaction(expected, actual)

    def test_reason_label_does_not_change_an_identical_effect(self):
        expected = self.plan()
        actual = copy.deepcopy(expected)
        actual["actions"][0]["reason"] = "dependency"
        native_apply.same_transaction(expected, actual)

    def test_payload_digest_owner_mode_and_symlink_are_checked_at_open(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "0.rpm")
            path.write_bytes(b"fixture")
            path.chmod(0o600)
            source = dict(
                id="rpm", bytes=7, sha256=hashlib.sha256(b"fixture").hexdigest()
            )
            plan = dict(packages=[dict(source=source)])
            payloads = {"rpm": str(path)}
            native_apply.verified_payloads(plan, payloads, directory)
            path.write_bytes(b"changed")
            with self.assertRaises(ValueError):
                native_apply.verified_payloads(plan, payloads, directory)
            path.unlink()
            path.symlink_to("missing")
            with self.assertRaises(OSError):
                native_apply.verified_payloads(plan, payloads, directory)

    def exercise(self, before="a" * 64, after="b" * 64, roots=True, files=True):
        plan = self.plan()
        request = dict(
            operation="apply",
            platform=plan["platform"],
            requirements={},
            versions={},
            plan=plan,
            payloads={},
        )
        options = SimpleNamespace(set=lambda value: None)
        base = SimpleNamespace(
            get_config=lambda: SimpleNamespace(
                get_pkg_gpgcheck_option=lambda: options,
                get_localpkg_gpgcheck_option=lambda: options,
            )
        )
        observations = iter(
            [
                dict(inventorySHA256=before),
                dict(inventorySHA256=after, rootsReady=roots),
            ]
        )
        self.verified = []

        def verify(platform, root, identities, scratch):
            self.verified.append(identities)
            return files

        helper = SimpleNamespace(
            validate_plan=lambda value: None,
            solve5=lambda *args, **kwargs: (copy.deepcopy(plan), base, "transaction"),
            inspect=lambda *args: next(observations),
        )
        with tempfile.TemporaryDirectory() as directory, patch.multiple(
            native_resolution,
            validate_plan=helper.validate_plan,
            solve5=helper.solve5,
            inspect=helper.inspect,
            snapshot_database=lambda *args: "/snapshot",
            verified_files=verify,
        ), patch.object(os, "geteuid", return_value=0), patch.object(
            native_apply, "run5"
        ) as run, patch.object(
            native_signatures, "verify"
        ):
            try:
                result = native_apply.apply(request, directory)
                return result, run.call_count, None
            except ValueError as error:
                return None, run.call_count, error

    def test_before_drift_refuses_native_effect(self):
        result, calls, error = self.exercise(before="d" * 64)
        self.assertIsNone(result)
        self.assertEqual(calls, 0)
        self.assertIsInstance(error, ValueError)

    def test_post_drift_and_failed_root_proof_do_not_claim_completion(self):
        for arguments in (dict(after="d" * 64), dict(roots=False), dict(files=False)):
            result, calls, error = self.exercise(**arguments)
            self.assertIsNone(result)
            self.assertEqual(calls, 1)
            self.assertIsInstance(error, ValueError)

    def test_integrity_is_proved_over_exactly_what_the_transaction_installed(self):
        result, calls, error = self.exercise()
        self.assertIsNone(error)
        self.assertIsNotNone(result)
        self.assertEqual(calls, 1)
        # Not the whole closure: the identities this transaction's own actions
        # put on disk, read back once beside the inventory.
        self.assertEqual(self.verified, [[self.plan()["actions"][0]["after"]]])

    def test_exact_transaction_returns_only_bounded_completion(self):
        result, calls, error = self.exercise()
        self.assertIsNone(error)
        self.assertEqual(calls, 1)
        self.assertEqual(
            result,
            dict(
                changed=True,
                planDigest="c" * 64,
                beforeSHA256="a" * 64,
                afterSHA256="b" * 64,
            ),
        )


if __name__ == "__main__":
    unittest.main()
