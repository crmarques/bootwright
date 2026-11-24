"""The provided registration worker converges an existing consumer onto its Satellite and never registers one."""

from __future__ import annotations

import base64
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_registration as registration
from ansible_collections.bootwright.core.plugins.module_utils import managedos_rhsm_worker as worker
from ansible_collections.bootwright.core.plugins.module_utils.managedos_rhsm_worker import Refused

SM = worker.SUBSCRIPTION_MANAGER
DIGEST = "3" * 64
CONSUMER = "11111111-2222-4333-8444-555555555555"
BUNDLE = "-----BEGIN CERTIFICATE-----\nQUJDREVGRw==\n-----END CERTIFICATE-----\n"
OTHER_BUNDLE = "-----BEGIN CERTIFICATE-----\nSElKS0xNTg==\n-----END CERTIFICATE-----\n"
HOSTNAME = "satellite.example.test"
ROOT = "https://satellite.example.test/pulp/content"
OLD = {"server.hostname": "old.example.test", "server.port": "8443", "server.prefix": "/subscription", "server.insecure": "1",
       "rhsm.baseurl": "https://cdn.example.test", "rhsm.repo_ca_cert": "%(ca_cert_dir)sredhat-uep.pem"}


def declared(ca):
    return {"server.hostname": HOSTNAME, "server.port": "443", "server.prefix": "/rhsm", "server.insecure": "0",
            "rhsm.baseurl": ROOT, "rhsm.repo_ca_cert": ca}


def good_repo(root=ROOT):
    return ("[rhel-9-baseos]\nname = BaseOS\nbaseurl = %s/Org/Library/content/baseos\nenabled = 1\n\n"
            "[rhel-9-appstream]\nname = AppStream\nbaseurl = %s/Org/Library/content/appstream\nenabled = 1\n") % (root, root)


class Manager:
    """The files and commands of subscription-manager: config edits rhsm.conf, repos regenerates redhat.repo."""

    def __init__(self, tmp_path, settings=None, repo=None, identity=(0, None), repos=(0, None), config=0, ca=None, refresh=(0, "")):
        self.conf = str(tmp_path / "rhsm.conf")
        self.repo = str(tmp_path / "redhat.repo")
        self.cert = str(tmp_path / "cert.pem")
        self.ca_directory = str(tmp_path / "ca")
        os.makedirs(self.ca_directory, exist_ok=True)
        self.ca = os.path.join(self.ca_directory, "bootwright-satellite.pem")
        self.settings = dict(OLD if settings is None else settings)
        self.written = repo
        self.identity, self.repos, self.config, self.refresh = identity, repos, config, refresh
        self.directory = str(tmp_path / "bootwright")
        self.record = os.path.join(self.directory, "provided-registration.json")
        self.runs, self.captures = [], []
        self.flush()
        if repo is not None:
            self.write(self.repo, repo)
        if ca is not None:
            self.write(self.ca, ca)
            os.chmod(self.ca, 0o644)
        self.guest = worker.Guest(cert=self.cert, owner=os.getuid(), run=self.run, identity=lambda path: CONSUMER,
                                  ca=self.ca, ca_directory=self.ca_directory, group=os.getgid())
        self.host = registration.Host(self.guest, capture=self.capture, conf=self.conf, repo=self.repo,
                                      record=self.record, directory=self.directory)

    def write(self, path, text):
        with open(path, "w", encoding="ascii") as handle:
            handle.write(text)

    def read(self, path):
        with open(path, encoding="ascii") as handle:
            return handle.read()

    def flush(self):
        sections = {}
        for name, value in self.settings.items():
            section, key = name.split(".")
            sections.setdefault(section, []).append("%s = %s" % (key, value))
        self.write(self.conf, "".join("[%s]\n%s\n" % (section, "\n".join(lines)) for section, lines in sections.items()))

    def register(self):
        self.write(self.cert, CONSUMER)

    def run(self, argv, timeout):
        self.runs.append((list(argv), timeout))
        if self.config != 0:
            return self.config
        for item in argv[2:]:
            name, value = item[2:].split("=", 1) if "=" in item else (item[2:], "")
            if name == "remove":
                self.settings.pop(value, None)
            else:
                self.settings[name] = value
        self.flush()
        return 0

    def capture(self, argv, timeout):
        self.captures.append((list(argv), timeout))
        if argv[1] == "refresh":
            return self.refresh
        code, output = self.identity if argv[1] == "identity" else self.repos
        if argv[1] == "identity":
            return None if code is None else (code, output if output is not None else "system identity: %s\nname: host\norg name: Org\norg ID: 1\n" % CONSUMER)
        if code == 0 and self.written is None:
            self.write(self.repo, good_repo())
        return None if code is None else (code, output or "")

    def apply(self, bundle=BUNDLE):
        return registration.execute("apply", DIGEST, HOSTNAME, ROOT, bundle, self.host)

    def observe(self, bundle=BUNDLE):
        return registration.execute("observe", DIGEST, HOSTNAME, ROOT, bundle, self.host)

    def snapshot(self):
        files = [self.conf, self.repo, self.ca, self.record]
        return [self.read(path) if os.path.exists(path) else None for path in files]

    def recorded(self):
        with open(self.record, encoding="utf-8") as handle:
            return json.load(handle)


class Crash(BaseException):
    """A worker killed between subscription-manager config and the identity proof."""


def crash_after_config(manager):
    def capture(argv, timeout):
        if argv[1] == "identity":
            raise Crash()
        return Manager.capture(manager, argv, timeout)

    manager.host.capture = capture
    with pytest.raises(Crash):
        manager.apply()
    manager.host.capture = manager.capture


@pytest.fixture(autouse=True)
def root(monkeypatch):
    monkeypatch.setattr(os, "geteuid", lambda: 0)


def test_a_host_without_a_consumer_certificate_refuses_before_changing_anything(tmp_path):
    manager = Manager(tmp_path)
    before = manager.snapshot()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-absent"
    assert manager.runs == [] and manager.captures == [] and manager.snapshot() == before
    assert manager.observe() == {"changed": False, "consumer": "", "state": "absent", "postcondition": False}


def test_a_converged_host_runs_no_config_and_reports_unchanged(tmp_path):
    manager = Manager(tmp_path, repo=good_repo(), ca=BUNDLE)
    manager.register()
    manager.settings = declared(manager.ca)
    manager.flush()
    assert manager.apply() == {"changed": False, "consumer": CONSUMER, "state": "unchanged", "postcondition": True}
    assert manager.runs == []
    assert [call[0] for call in manager.captures] == [[SM, "identity"], [SM, "refresh"], [SM, "repos", "--list-enabled"]]
    assert manager.observe() == {"changed": False, "consumer": CONSUMER, "state": "converged", "postcondition": True}


def test_convergence_points_the_host_at_the_satellite_with_one_config_call(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    assert manager.apply() == {"changed": True, "consumer": CONSUMER, "state": "converged", "postcondition": True}
    assert manager.runs == [([SM, "config", "--server.hostname=" + HOSTNAME, "--server.port=443", "--server.prefix=/rhsm",
                              "--server.insecure=0", "--rhsm.baseurl=" + ROOT, "--rhsm.repo_ca_cert=" + manager.ca], worker.CONFIG_CEILING)]
    assert manager.read(manager.ca) == BUNDLE and oct(os.stat(manager.ca).st_mode & 0o777) == "0o644"
    assert [call[0] for call in manager.captures] == [[SM, "identity"], [SM, "refresh"], [SM, "repos", "--list-enabled"]]
    assert manager.settings == declared(manager.ca)
    assert manager.observe()["state"] == "converged"


def test_a_changed_bundle_reconfigures_even_when_rhsm_conf_already_names_it(tmp_path):
    manager = Manager(tmp_path, repo=good_repo(), ca=OTHER_BUNDLE)
    manager.register()
    manager.settings = declared(manager.ca)
    manager.flush()
    assert manager.observe()["state"] == "diverged"
    assert manager.apply()["state"] == "converged"
    assert len(manager.runs) == 1 and manager.read(manager.ca) == BUNDLE


@pytest.mark.parametrize("output, reason", [
    ((1, "Unable to verify server's identity: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed"), "registration-trust"),
    ((1, "ssl.SSLCertVerificationError"), "registration-trust"),
    ((1, "Consumer with id 1 could not be found."), "registration-foreign"),
    ((1, "Unit 1 has been deleted"), "registration-foreign"),
    ((1, "HTTP error (401 - Unauthorized)"), "registration-foreign"),
    ((1, "HTTP error (410)"), "registration-foreign"),
    ((1, "Unable to reach the server at satellite.example.test:443/rhsm"), "registration-unavailable"),
    (None, "registration-unavailable"),
    ((0, "system identity: 99999999-2222-4333-8444-555555555555\n"), "registration-unavailable"),
    ((0, "name: host\n"), "registration-unavailable"),
])
def test_a_failed_identity_proof_restores_the_recorded_settings_and_names_one_reason(tmp_path, output, reason):
    manager = Manager(tmp_path, identity=(output[0] if output else None, output[1] if output else None))
    manager.register()
    before = dict(manager.settings)
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == reason
    assert len(manager.runs) == 2 and manager.settings == before
    assert manager.runs[1] == ([SM, "config"] + ["--%s=%s" % item for item in OLD.items()], worker.CONFIG_CEILING)
    assert [call[0][1] for call in manager.captures] == ["identity"]


def test_an_unset_setting_is_restored_by_removing_it(tmp_path):
    settings = {name: value for name, value in OLD.items() if name != "server.prefix"}
    manager = Manager(tmp_path, settings=settings, identity=(1, "Consumer could not be found"))
    manager.register()
    with pytest.raises(Refused):
        manager.apply()
    restore = manager.runs[1][0]
    assert restore[:2] == [SM, "config"] and restore[-1] == "--remove=server.prefix"
    assert "--server.hostname=old.example.test" in restore and len(restore) == 2 + len(OLD)
    assert manager.settings == settings


def test_a_failed_proof_leaves_the_previous_bundle_in_place(tmp_path):
    manager = Manager(tmp_path, ca=OTHER_BUNDLE, identity=(1, "certificate verify failed"))
    manager.register()
    manager.settings = dict(OLD, **{"rhsm.repo_ca_cert": manager.ca})
    manager.flush()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-trust"
    assert manager.read(manager.ca) == OTHER_BUNDLE


def test_a_config_failure_restores_and_refuses_as_unavailable(tmp_path):
    manager = Manager(tmp_path, config=1)
    manager.register()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-unavailable" and error.value.detail == "settings-not-restored"
    assert len(manager.runs) == 2 and manager.captures == []


def test_an_unreadable_consumer_certificate_is_foreign(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    manager.guest.identity = lambda path: None
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-foreign" and manager.runs == []
    assert manager.observe() == {"changed": False, "consumer": "", "state": "diverged", "postcondition": False}


@pytest.mark.parametrize("refresh, repos, cause", [
    ((0, ""), (1, ""), "service"), ((0, ""), (None, ""), "timeout"), ((1, ""), (0, None), "service"), (None, (0, None), "timeout")])
def test_repositories_that_cannot_be_regenerated_refuse_unavailable_without_undoing_the_proved_configuration(tmp_path, refresh, repos, cause):
    manager = Manager(tmp_path, repos=repos, refresh=refresh)
    manager.register()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-unavailable" and error.value.cause == cause
    assert len(manager.runs) == 1 and manager.settings == declared(manager.ca)
    assert os.path.exists(manager.record) and manager.observe()["state"] == "diverged"


def test_refresh_precedes_the_regeneration_within_bounded_ceilings(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    manager.apply()
    assert manager.captures[1:] == [([SM, "refresh"], registration.REFRESH_CEILING), ([SM, "repos", "--list-enabled"], worker.REPOSITORIES_CEILING)]
    assert worker.CONFIG_CEILING + registration.IDENTITY_CEILING + registration.REFRESH_CEILING + worker.REPOSITORIES_CEILING < 540


@pytest.mark.parametrize("text", [
    "",
    "# nothing\n",
    good_repo("https://other.example.test/pulp/content"),
    good_repo() + "[extra]\nbaseurl = https://cdn.example.test/x\n",
    "[mixed]\nbaseurl = %s/a/b,\n    https://elsewhere.example.test/c\n" % ROOT,
    "[sibling]\nbaseurl = https://satellite.example.test/pulp/contentX/a\n",
    "[nobase]\nname = x\n",
])
def test_repositories_outside_the_content_root_refuse(tmp_path, text):
    manager = Manager(tmp_path, repo=text)
    manager.register()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-content-root"
    assert manager.observe()["state"] == "diverged"


def test_every_base_url_of_a_multi_line_value_is_checked(tmp_path):
    text = "[both]\nbaseurl = %s/a\n    %s/b, %s/c\n" % (ROOT, ROOT, ROOT)
    manager = Manager(tmp_path, repo=text)
    manager.register()
    assert manager.apply()["state"] == "converged"


def test_a_missing_repository_file_refuses_after_listing(tmp_path):
    manager = Manager(tmp_path, repos=(0, ""), repo=None)
    manager.register()
    manager.written = ""
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-content-root"


def test_an_observation_changes_nothing(tmp_path):
    manager = Manager(tmp_path, repo=good_repo("https://other.example.test/pulp/content"), ca=OTHER_BUNDLE)
    manager.register()
    before = manager.snapshot()
    assert manager.observe() == {"changed": False, "consumer": CONSUMER, "state": "diverged", "postcondition": False}
    assert manager.runs == [] and manager.captures == [] and manager.snapshot() == before


def test_an_observation_without_the_ca_directory_reads_as_diverged(tmp_path):
    manager = Manager(tmp_path, repo=good_repo())
    manager.register()
    manager.settings = declared(manager.ca)
    manager.flush()
    os.rmdir(manager.ca_directory)
    assert manager.observe()["state"] == "diverged"


@pytest.mark.parametrize("bundle", ["", "not a certificate", "-----BEGIN PRIVATE KEY-----\nQUJD\n-----END PRIVATE KEY-----\n"])
def test_a_malformed_bundle_refuses_as_trust_before_any_read(tmp_path, bundle):
    manager = Manager(tmp_path)
    manager.register()
    with pytest.raises(Refused) as error:
        manager.apply(bundle)
    assert error.value.reason == "registration-trust" and manager.runs == [] and manager.captures == []


@pytest.mark.parametrize("hostname, root_url", [("bad host", ROOT), (HOSTNAME, "http://satellite.example.test/pulp"), (HOSTNAME, "")])
def test_an_invalid_satellite_refuses_before_any_read(tmp_path, hostname, root_url):
    manager = Manager(tmp_path)
    manager.register()
    with pytest.raises(Refused) as error:
        registration.execute("apply", DIGEST, hostname, root_url, BUNDLE, manager.host)
    assert error.value.reason == "registration-unavailable" and manager.runs == []


def test_only_root_with_a_digest_and_a_known_operation_may_run(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    for operation, digest in (("register", DIGEST), ("apply", "short"), ("apply", None), ("clean", DIGEST)):
        with pytest.raises(Refused) as error:
            registration.execute(operation, digest, HOSTNAME, ROOT, BUNDLE, manager.host)
        assert error.value.reason == "registration-unavailable" and error.value.cause == "authority"
    monkeypatch.setattr(os, "geteuid", lambda: 1000)
    with pytest.raises(Refused):
        manager.apply()


def test_the_worker_never_runs_a_command_that_registers_or_cleans(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    manager.apply()
    forbidden = {"register", "unregister", "clean", "release", "attach", "remove"}
    seen = [call[0] for call in manager.runs + manager.captures]
    assert all(argv[0] == SM and argv[1] in ("config", "identity", "refresh", "repos") for argv in seen)
    assert not any(token in forbidden or token == "--force" or token.startswith("--enable") or token.startswith("--disable")
                   for argv in seen for token in argv[1:])


def test_the_merged_capture_reads_both_streams_and_maps_failures(monkeypatch):
    seen = {}

    def run(argv, **options):
        seen.update(options, argv=argv)
        return registration.bounded_process.Result(3, b"out", b"err")

    monkeypatch.setattr(registration.bounded_process, "run", run)
    assert registration.merged(["/bin/true"], 5) == (3, "out\nerr")
    assert seen["timeout"] == 5 and seen["cwd"] == "/" and seen["limit"] == registration.MAX_OUTPUT
    assert seen["environment"] == worker.ENVIRONMENT and seen["argv"] == ["/bin/true"]
    for kind, expected in (("timeout", None), ("command", "tooling"), ("output", "service")):
        def fail(*args, kind=kind, **options):
            raise registration.bounded_process.Failure(kind)

        monkeypatch.setattr(registration.bounded_process, "run", fail)
        if expected is None:
            assert registration.merged(["/bin/true"], 1) is None
            continue
        with pytest.raises(Refused) as error:
            registration.merged(["/bin/true"], 1)
        assert error.value.reason == "registration-unavailable" and error.value.cause == expected


def test_settings_are_read_raw_without_interpolation(tmp_path):
    path = tmp_path / "rhsm.conf"
    path.write_text("[server]\nhostname = a\n[rhsm]\nrepo_ca_cert = %(ca_cert_dir)sredhat-uep.pem\nbaseurl =\n")
    settings = registration.read_settings(str(path))
    assert settings["rhsm.repo_ca_cert"] == "%(ca_cert_dir)sredhat-uep.pem" and settings["server.port"] is None
    assert settings["rhsm.baseurl"] == "" and settings["server.hostname"] == "a"


def test_the_prior_settings_and_ca_are_recorded_root_only_before_any_change(tmp_path):
    manager = Manager(tmp_path, ca=OTHER_BUNDLE)
    manager.register()
    crash_after_config(manager)
    assert manager.settings == declared(manager.ca) and manager.read(manager.ca) == BUNDLE
    assert oct(os.stat(manager.record).st_mode & 0o777) == "0o600"
    assert manager.recorded() == {"request": DIGEST, "settings": OLD, "ca": base64.b64encode(OTHER_BUNDLE.encode()).decode()}
    assert not os.path.exists(os.path.join(manager.directory, ".provided-registration.json.tmp"))


def test_an_absent_setting_and_an_absent_ca_are_recorded_as_absent(tmp_path):
    settings = {name: value for name, value in OLD.items() if name != "server.prefix"}
    manager = Manager(tmp_path, settings=settings)
    manager.register()
    crash_after_config(manager)
    assert manager.recorded() == {"request": DIGEST, "settings": dict(settings, **{"server.prefix": None}), "ca": None}


def test_an_outstanding_record_makes_an_observation_partial(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    crash_after_config(manager)
    manager.write(manager.repo, good_repo())
    assert manager.observe() == {"changed": False, "consumer": CONSUMER, "state": "diverged", "postcondition": False}
    manager.guest.identity = lambda path: None
    assert manager.observe() == {"changed": False, "consumer": "", "state": "diverged", "postcondition": False}


def test_an_apply_after_a_crash_proves_the_consumer_and_then_removes_the_record(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    crash_after_config(manager)
    runs = len(manager.runs)
    assert manager.apply() == {"changed": True, "consumer": CONSUMER, "state": "converged", "postcondition": True}
    assert len(manager.runs) == runs and [call[0][1] for call in manager.captures] == ["identity", "refresh", "repos"]
    assert not os.path.exists(manager.record) and manager.observe()["state"] == "converged"


def test_an_apply_after_a_crash_restores_the_recorded_host_when_the_proof_fails(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    crash_after_config(manager)
    manager.identity = (1, "Consumer with id 1 could not be found.")
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-foreign" and error.value.detail == ""
    assert manager.settings == OLD and not os.path.exists(manager.ca) and not os.path.exists(manager.record)
    assert manager.runs[-1] == ([SM, "config"] + ["--%s=%s" % item for item in OLD.items()], worker.CONFIG_CEILING)


def test_an_outstanding_record_is_the_rollback_target_and_is_never_rerecorded(tmp_path):
    manager = Manager(tmp_path, ca=OTHER_BUNDLE)
    manager.register()
    crash_after_config(manager)
    first = manager.recorded()
    crash_after_config(manager)
    assert manager.recorded() == first
    manager.identity = (1, "certificate verify failed")
    with pytest.raises(Refused):
        manager.apply()
    assert manager.settings == OLD and manager.read(manager.ca) == OTHER_BUNDLE and not os.path.exists(manager.record)


def test_a_ca_this_run_created_is_unlinked_when_the_proof_fails(tmp_path):
    manager = Manager(tmp_path, identity=(1, "HTTP error (401 - Unauthorized)"))
    manager.register()
    with pytest.raises(Refused):
        manager.apply()
    assert not os.path.exists(manager.ca) and not os.path.exists(manager.record) and manager.settings == OLD


def test_a_failed_restore_keeps_the_record_for_the_next_apply(tmp_path):
    manager = Manager(tmp_path, config=1)
    manager.register()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.detail == "settings-not-restored" and manager.recorded()["settings"] == OLD
    assert manager.observe()["state"] == "diverged"


@pytest.mark.parametrize("content", [
    b"", b"not json", b"[]", json.dumps({"request": DIGEST, "settings": OLD}).encode(),
    json.dumps({"request": "short", "settings": OLD, "ca": None}).encode(),
    json.dumps({"request": DIGEST, "settings": {"server.hostname": "a"}, "ca": None}).encode(),
    json.dumps({"request": DIGEST, "settings": dict(OLD, **{"server.port": 443}), "ca": None}).encode(),
    json.dumps({"request": DIGEST, "settings": OLD, "ca": "not base64!"}).encode(),
])
def test_an_unreadable_record_refuses_with_no_change(tmp_path, content):
    manager = Manager(tmp_path)
    manager.register()
    os.makedirs(manager.directory)
    with open(manager.record, "wb") as handle:
        handle.write(content)
    os.chmod(manager.record, 0o600)
    before = manager.snapshot()
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-unavailable" and manager.runs == [] and manager.captures == []
    assert manager.snapshot() == before and manager.observe()["state"] == "diverged"


def test_a_record_others_can_read_refuses(tmp_path):
    manager = Manager(tmp_path)
    manager.register()
    crash_after_config(manager)
    os.chmod(manager.record, 0o644)
    runs = len(manager.runs)
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-unavailable" and len(manager.runs) == runs


@pytest.mark.parametrize("kind", ["link", "directory", "oversized"])
def test_a_prior_ca_that_cannot_be_recorded_refuses_before_any_change(tmp_path, kind):
    manager = Manager(tmp_path)
    manager.register()
    if kind == "link":
        manager.write(str(tmp_path / "elsewhere.pem"), BUNDLE)
        os.symlink(str(tmp_path / "elsewhere.pem"), manager.ca)
    elif kind == "directory":
        os.mkdir(manager.ca)
    else:
        manager.write(manager.ca, "x" * (worker.MAX_BUNDLE + 1))
    with pytest.raises(Refused) as error:
        manager.apply()
    assert error.value.reason == "registration-unavailable" and manager.runs == [] and manager.captures == []
    assert not os.path.exists(manager.record)


def test_a_prior_ca_with_another_mode_is_recorded_raw_and_restored_byte_for_byte(tmp_path):
    manager = Manager(tmp_path, ca=OTHER_BUNDLE, identity=(1, "certificate verify failed"))
    os.chmod(manager.ca, 0o600)
    manager.register()
    with pytest.raises(Refused):
        manager.apply()
    assert manager.read(manager.ca) == OTHER_BUNDLE and not os.path.exists(manager.record)
