"""The KubeVirt action's dispatch, its pushed upload and its published evidence."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_api as action
from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.modules import substrate_kubevirt_upload as kubevirt_upload
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import (
    VM, FakeAPI, client, context, proof_request, proved_host, request)

DIGEST = "a" * 64


def test_media_insert_uploads_once_through_the_verified_proxy_then_presents_the_volume(tmp_path):
    api = proved_host()
    arm.apply(arm.Host(client(api), "guests", 1e12), request(), context())
    image = tmp_path / "agent.iso"
    image.write_bytes(b"x" * 1024)
    uploads = []

    def upload(namespace, volume, path, proxy, authority, seconds):
        uploads.append((namespace, volume, path, proxy, authority.startswith("-----BEGIN CERTIFICATE-----")))
        api.objects["/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/" + volume]["status"]["phase"] = "Succeeded"

    arguments = {"endpoint": "kubevirt:guests/" + VM, "image_path": str(image), "virtctl": "/usr/bin/virtctl", "media_identity": "digest-1", "seconds": 1800}
    assert action.port("media_insert", client(api), arguments, upload) == {}
    assert uploads == [("guests", VM + "-media", str(image), "https://cdi-uploadproxy.apps.host.example.test", True)]
    assert action.port("media_insert", client(api), arguments, upload) == {} and len(uploads) == 1
    assert action.port("state_read", client(api), dict(arguments, media=True), upload)["media_state"] == "own"
    assert action.port("boot_disk", client(api), dict(arguments, power_on=False), upload) == {}
    assert action.port("boot_media", client(api), arguments, upload)["power"] == "On"


def test_a_block_publishes_its_evidence_and_a_refusal_names_no_credential():
    changed, evidence = action.block("proof", client(proved_host()), {"request": proof_request()})
    assert not changed and evidence["postcondition"]
    failed = action.failed(kapi.Answered(403, {"reason": "Forbidden"}))
    assert "rebind" in failed["msg"] and "token" not in failed["msg"].replace("rebind", "")
    assert action.failed(arm.PortRefused("machine-running", "running"))["refusal"] == "machine-running"
    assert action.failed(kapi.Unanswered())["msg"] == "the host cluster did not answer"


def test_the_protocol_publishes_only_the_fields_go_decodes():
    arguments = {"request": request(), "apply_operation": "op-1", "block": "machine-master-0",
                 "registered": "2026-10-28T10:00:00Z", "pinned": context()["pinned"]}
    ignored_changed, evidence = action.block("observe", client(FakeAPI()), arguments)
    published, failure = protocol.completion({"subject": "machine", "digest": DIGEST, "observation": evidence, "observed": True})
    assert failure is None and published["absent"] and published["request"] == DIGEST
    assert set(published) == {"absent", "answered", "foreign", "instance", "kubeSystemUID", "owned", "postcondition", "receipts",
                              "request", "server", "virtualMachine", "volumes"}
    ignored_changed, refused = protocol.completion({"subject": "machine", "digest": DIGEST, "observation": dict(evidence, postcondition=False)})
    assert refused
    released, failure = protocol.completion({"subject": "host", "digest": DIGEST, "released": True})
    assert failure is None and released["absent"] and released["postcondition"]
    ignored_changed, failure = protocol.completion({"subject": "host", "digest": DIGEST, "observation": {"failures": ["the namespace guests does not exist"]}})
    assert "does not exist" in failure


def test_the_upload_never_disables_verification_and_trusts_only_the_ingress_authority():
    params = {"virtctl": "/usr/local/bin/virtctl", "kubeconfig": "/run/k", "trust": "/run/ca.pem", "namespace": "guests",
              "volume": VM + "-media", "image": "/var/image.iso", "proxy": "https://cdi-uploadproxy.apps.host.example.test", "seconds": 1200}
    argv = kubevirt_upload.argv(params)
    assert "--insecure" not in argv and argv[argv.index("--uploadproxy-url") + 1] == params["proxy"] and "--no-create" in argv
    environment = kubevirt_upload.environment(params)
    assert environment["SSL_CERT_FILE"] == "/run/ca.pem" and not {"HTTPS_PROXY", "https_proxy", "HTTP_PROXY"} & set(environment)
    for bad in ({"proxy": "http://cdi.example.test"}, {"volume": "Bad_Name"}, {"image": "relative.iso"}):
        with pytest.raises(ValueError):
            kubevirt_upload.argv(dict(params, **bad))
