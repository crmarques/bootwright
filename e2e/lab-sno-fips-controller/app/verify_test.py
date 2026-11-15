"""Acceptance must not turn broken probes into isolation evidence."""

from __future__ import annotations

import contextlib
import copy
import errno
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import urllib.error

import verify


def configuration():
    envs = {}
    for name, subnet, vlan, bridge in (
            ("staging", "192.0.2", 10, "br-stg"),
            ("production", "203.0.113", 20, "br-prd")):
        envs[name] = {"cidr": subnet + ".0/24", "prefix": 24,
                      "hostAddress": subnet + ".1",
                      "nodeAddress": subnet + ".31",
                      "appAddress": subnet + ".50",
                      "egressAddress": subnet + ".60",
                      "pool": subnet + ".200/29", "vlan": vlan,
                      "bridge": bridge}
    return {"nodeName": "master-0", "nodeAddress": "198.51.100.31",
            "domain": "e2e.example.test", "environments": envs,
            "trunk": {"bridge": "br-e2e-trunk", "interface": "enp2s0"},
            "image": "unused-by-checker"}


class ResponseTests(unittest.TestCase):
    def test_http_error_proves_connectivity(self):
        for status in (200, 301, 403, 404, 503):
            with self.subTest(status=status), self.assertRaises(verify.Failure):
                verify.assert_response({"kind": "http", "status": status}, False)

    def test_only_explicit_network_failure_counts_as_blocked(self):
        verify.assert_response({"kind": "blocked", "reason": "timed out"}, False)
        for result in ({}, [], None, {"kind": "error", "reason": "bad DNS"}):
            with self.subTest(result=result), self.assertRaises(verify.Failure):
                verify.assert_response(result, False)

    def test_wrong_source_and_wrong_environment_fail(self):
        result = {"kind": "http", "status": 200, "body": json.dumps({
            "environment": "staging", "source": "192.0.2.60"})}
        verify.assert_response(result, True, "staging", "192.0.2.60")
        for environment, source in (("production", "192.0.2.60"),
                                    ("staging", "192.0.2.31")):
            with self.subTest(environment=environment), self.assertRaises(verify.Failure):
                verify.assert_response(result, True, environment, source)

    def test_invalid_application_json_fails(self):
        for body in ("not json", "null", "[]"):
            with self.subTest(body=body), self.assertRaises(verify.Failure):
                verify.assert_response({"kind": "http", "status": 200,
                                        "body": body}, True, "staging")

    def test_execution_failure_is_never_a_blocked_connection(self):
        def broken_runner(argv):
            raise verify.Failure("oc exec: container not running")

        with self.assertRaisesRegex(verify.Failure, "container not running"):
            verify.check(["/usr/bin/oc", "exec"], "http://192.0.2.1/",
                         False, runner=broken_runner)


class RemoteProbeTests(unittest.TestCase):
    def probe_with_error(self, error):
        output = io.StringIO()
        opener = mock.Mock()
        opener.open.side_effect = error
        with mock.patch("urllib.request.build_opener", return_value=opener), \
                mock.patch("sys.argv", ["probe", "http://192.0.2.1/", ""]), \
                contextlib.redirect_stdout(output):
            exec(verify.PROBE, {})
        return json.loads(output.getvalue())

    def test_http_403_is_not_a_network_block(self):
        result = self.probe_with_error(urllib.error.HTTPError(
            "http://192.0.2.1/", 403, "Forbidden", {}, None))
        self.assertEqual(result["kind"], "http")
        self.assertEqual(result["status"], 403)

    def test_redirect_handler_refuses_to_follow_redirect(self):
        output = io.StringIO()
        opener = mock.Mock()
        opener.open.side_effect = TimeoutError()
        with mock.patch("urllib.request.build_opener", return_value=opener) as build, \
                mock.patch("sys.argv", ["probe", "http://192.0.2.1/", ""]), \
                contextlib.redirect_stdout(output):
            exec(verify.PROBE, {})
        handler = build.call_args.args[1]
        self.assertIsNone(handler.redirect_request(
            None, None, 302, "Found", {}, "http://203.0.113.1/"))

    def test_dns_and_unknown_os_errors_fail(self):
        for error in (OSError(errno.EIO, "I/O error"),
                      urllib.error.URLError("name resolution failed")):
            self.assertEqual(self.probe_with_error(error)["kind"], "error")

    def test_connection_refused_and_timeout_are_network_blocks(self):
        for error in (TimeoutError(), urllib.error.URLError(
                OSError(errno.ECONNREFUSED, "refused"))):
            self.assertEqual(self.probe_with_error(error)["kind"], "blocked")


class CommandTests(unittest.TestCase):
    def test_nonzero_exit_and_timeout_fail(self):
        with mock.patch("subprocess.run", return_value=mock.Mock(returncode=1)):
            with self.assertRaises(verify.Failure):
                verify.run(["/usr/bin/oc", "exec"])
        with mock.patch("subprocess.run", side_effect=subprocess.TimeoutExpired("oc", 30)):
            with self.assertRaises(verify.Failure):
                verify.run(["/usr/bin/oc", "exec"])

    def test_successful_process_with_invalid_json_fails(self):
        def malformed(argv, **kwargs):
            kwargs["stdout"].write(b"not JSON")
            return mock.Mock(returncode=0)

        with mock.patch("subprocess.run", side_effect=malformed):
            with self.assertRaisesRegex(verify.Failure, "invalid JSON"):
                verify.run(["/usr/bin/oc", "exec"])

    def test_output_ceiling_fails(self):
        def oversized(argv, **kwargs):
            kwargs["stdout"].write(b" " * 1048577)
            return mock.Mock(returncode=0)

        with mock.patch("subprocess.run", side_effect=oversized):
            with self.assertRaisesRegex(verify.Failure, "exceeded limit"):
                verify.run(["/usr/bin/oc", "exec"])


class EvidenceTests(unittest.TestCase):
    def test_secondary_attachment_fails_even_if_ready(self):
        pod = {"metadata": {"annotations": {
            "k8s.v1.cni.cncf.io/network-status": json.dumps([
                {"name": "default", "default": True},
                {"name": "staging/staging"}])}}, "spec": {},
            "status": {"podIP": "10.128.0.5", "conditions": [
                {"type": "Ready", "status": "True"}]}}
        with self.assertRaisesRegex(verify.Failure, "secondary-network"):
            verify.ready_pod(pod)

    def test_snapshot_written_atomically_with_private_mode(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "snapshot.json"
            verify.write_snapshot(path, {"test": True}, {"result": True})
            self.assertEqual(json.loads(path.read_text())["config"], {"test": True})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual([p.name for p in path.parent.iterdir()], ["snapshot.json"])

    def test_snapshot_invalidated_before_a_failing_pod_run(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "snapshot.json"
            config = Path(directory) / "config.json"
            config.write_text(json.dumps(configuration()))
            verify.write_snapshot(path, configuration(), {})
            with mock.patch("sys.argv", ["verify", "--config", str(config),
                                          "--mode", "pods", "--snapshot", str(path)]), \
                    mock.patch.object(verify, "executable", side_effect=verify.Failure("missing oc")), \
                    contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(verify.main(), 1)
            self.assertFalse(path.exists())

    def test_snapshot_symlink_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "target"
            target.write_text("keep")
            link = Path(directory) / "snapshot.json"
            link.symlink_to(target)
            with self.assertRaises(verify.Failure):
                verify.invalidate_snapshot(link)
            self.assertEqual(target.read_text(), "keep")


class ConfigurationTests(unittest.TestCase):
    def test_parent_chart_configuration_passes_with_extra_fields(self):
        verify.validate_config(configuration())

    def test_address_collision_pool_overlap_and_vlan_reuse_fail(self):
        config = configuration()
        changes = ({"egressAddress": "192.0.2.200"},
                   {"egressAddress": "192.0.2.31"},
                   {"pool": "198.51.100.200/29"},
                   {"vlan": 20}, {"vlan": True},
                   {"prefix": 25}, {"bridge": "br;evil"})
        for change in changes:
            changed = copy.deepcopy(config)
            changed["environments"]["staging"].update(change)
            with self.subTest(change=change), self.assertRaises(verify.Failure):
                verify.validate_config(changed)

    def test_derived_vlan_interface_must_fit_linux_name_limit(self):
        config = configuration()
        config["trunk"]["interface"] = "abcdefghijklmn"
        with self.assertRaisesRegex(verify.Failure, "15 characters"):
            verify.validate_config(config)


class RouteProofTests(unittest.TestCase):
    def setUp(self):
        self.config = configuration()
        self.observed = {"production": {"routerLB": "203.0.113.200"}}

    def prove(self, route):
        verify.prove_routes(self.config, self.observed, "staging", 123,
                            "/usr/bin/nsenter", "/usr/sbin/ip",
                            lambda argv: [route])

    def test_gateway_and_source_are_both_required(self):
        self.prove({"gateway": "192.0.2.31", "prefsrc": "192.0.2.50"})
        for route in ({"prefsrc": "192.0.2.50"},
                      {"gateway": "192.0.2.1", "prefsrc": "192.0.2.50"},
                      {"gateway": "192.0.2.31", "prefsrc": "192.0.2.51"},
                      {"gateway": "192.0.2.31", "prefsrc": "192.0.2.50",
                       "type": "unreachable"}):
            with self.subTest(route=route), self.assertRaises(verify.Failure):
                self.prove(route)

    def test_nonrunning_container_fails(self):
        for pid in (0, -1, True, "123", None):
            with self.subTest(pid=pid), self.assertRaises(verify.Failure):
                verify.container_pid("/usr/bin/podman", "staging", lambda argv: pid)


if __name__ == "__main__":
    unittest.main()
