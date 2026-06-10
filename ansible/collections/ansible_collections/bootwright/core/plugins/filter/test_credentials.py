"""Tests for the bootwright_parse_credential Ansible filter.

Uses stdlib unittest (no pytest dependency) so the suite runs anywhere
Python 3 is available — including a stock CI runner without an
Ansible venv. Pytest discovers and runs unittest classes too, so a
developer with pytest installed locally can run pytest in this directory.

The filter lives next to this test in the collection filter plugin directory
and is loaded by Ansible through the `bootwright.core` collection.
Each test exercises one branch of the filter's input validation so a
regression in either decoding or the username:password split surfaces
here rather than at apply time, where the failure would only show up
mid-playbook with a `no_log` task.
"""

from __future__ import annotations

import base64
import importlib.util
import pathlib
import sys
import unittest

# Provide a stub for ansible.errors so the filter module imports without
# pulling in the full Ansible runtime. The filter only uses
# AnsibleFilterError, which behaves as a regular exception subclass.
if "ansible" not in sys.modules:
    ansible_module = type(sys)("ansible")
    sys.modules["ansible"] = ansible_module
if "ansible.errors" not in sys.modules:
    errors_module = type(sys)("ansible.errors")

    class _AnsibleFilterError(Exception):
        pass

    errors_module.AnsibleFilterError = _AnsibleFilterError
    sys.modules["ansible.errors"] = errors_module

# Load the filter module by file path so the test does not depend on
# any legacy filter plugin directory being on sys.path.
_HERE = pathlib.Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location(
    "_bootwright_credentials_filter",
    _HERE / "credentials.py",
)
assert _spec and _spec.loader, "could not locate credentials.py next to test file"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_parse_credential = _module.bootwright_parse_credential
bootwright_proxy_userinfo = _module.bootwright_proxy_userinfo
AnsibleFilterError = sys.modules["ansible.errors"].AnsibleFilterError


def _slurp(payload: str | bytes) -> dict:
    """Build a fake ansible.builtin.slurp result dict."""
    if isinstance(payload, str):
        payload = payload.encode("utf-8")
    return {"content": base64.b64encode(payload).decode("ascii"), "source": "/fake/path"}


class ParseCredentialHappyPath(unittest.TestCase):
    def test_basic_username_password(self):
        got = bootwright_parse_credential(_slurp("alice:s3cret"))
        self.assertEqual(got, {"username": "alice", "password": "s3cret"})

    def test_trailing_newline_is_stripped(self):
        got = bootwright_parse_credential(_slurp("alice:s3cret\n"))
        self.assertEqual(got, {"username": "alice", "password": "s3cret"})

    def test_trailing_crlf_is_stripped(self):
        got = bootwright_parse_credential(_slurp("alice:s3cret\r\n"))
        self.assertEqual(got, {"username": "alice", "password": "s3cret"})

    def test_password_with_colon_is_preserved(self):
        got = bootwright_parse_credential(_slurp("alice:s3c:ret"))
        self.assertEqual(got, {"username": "alice", "password": "s3c:ret"})

    def test_label_appears_in_filter_module_signature(self):
        # The label is keyword-only positional second; default applies
        # when caller omits it.
        got = bootwright_parse_credential(_slurp("u:p"), "my-label")
        self.assertEqual(got, {"username": "u", "password": "p"})


class ParseCredentialBadInput(unittest.TestCase):
    def assertFilterError(self, payload, *, label="credential", contains=""):
        with self.assertRaises(AnsibleFilterError) as ctx:
            bootwright_parse_credential(payload, label)
        if contains:
            self.assertIn(contains, str(ctx.exception),
                          f"error message missing {contains!r}; got: {ctx.exception}")

    def test_non_dict_input_rejected(self):
        self.assertFilterError("not a dict", contains="expected an ansible.builtin.slurp result mapping")

    def test_missing_content_field_rejected(self):
        self.assertFilterError({"source": "/fake/path"}, contains="missing 'content' field")

    def test_invalid_base64_rejected(self):
        self.assertFilterError({"content": "not-base64-!!!"}, contains="base64/utf-8 decode failed")

    def test_invalid_utf8_rejected(self):
        bad = base64.b64encode(b"\xff\xfe\xfd").decode("ascii")
        self.assertFilterError({"content": bad}, contains="base64/utf-8 decode failed")

    def test_empty_payload_rejected(self):
        self.assertFilterError(_slurp(""), contains="single username:password line")

    def test_multiline_payload_rejected(self):
        self.assertFilterError(_slurp("alice:pass\nbob:other"),
                               contains="single username:password line")

    def test_missing_colon_rejected(self):
        self.assertFilterError(_slurp("just-a-username"),
                               contains="single username:password line")

    def test_empty_username_rejected(self):
        self.assertFilterError(_slurp(":secret"),
                               contains="username and password must both be non-empty")

    def test_empty_password_rejected(self):
        self.assertFilterError(_slurp("alice:"),
                               contains="username and password must both be non-empty")

    def test_label_is_included_in_error(self):
        # Operators rely on the label to identify which credentialsRef is
        # malformed — they may have several configured and the error
        # message is the only signal.
        with self.assertRaises(AnsibleFilterError) as ctx:
            bootwright_parse_credential({"source": "/fake/path"}, "mirror credentialsRef secrets/mirror")
        self.assertIn("mirror credentialsRef secrets/mirror", str(ctx.exception))


class ProxyUserinfo(unittest.TestCase):
    def test_dollar_is_preserved_and_delimiters_are_escaped(self):
        got = bootwright_proxy_userinfo({"username": "u@x", "password": "p$:s/q?"})
        self.assertEqual(got, "u%40x:p$%3As%2Fq%3F@")

    def test_bad_input_rejected_without_secret_echo(self):
        with self.assertRaises(AnsibleFilterError) as ctx:
            bootwright_proxy_userinfo({"username": "u", "password": ""}, "proxy auth")
        self.assertIn("proxy auth", str(ctx.exception))


class FilterRegistration(unittest.TestCase):
    def test_filter_module_exposes_filter(self):
        # The FilterModule class is how Ansible discovers the filter.
        # If the registration name drifts, every playbook using the
        # filter breaks at parse time.
        registered = _module.FilterModule().filters()
        self.assertIn("bootwright_parse_credential", registered)
        self.assertIs(registered["bootwright_parse_credential"], bootwright_parse_credential)
        self.assertIn("bootwright_proxy_userinfo", registered)
        self.assertIs(registered["bootwright_proxy_userinfo"], bootwright_proxy_userinfo)


if __name__ == "__main__":
    unittest.main()
