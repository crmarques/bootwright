"""Run the shipped Ceph image release through ansible-core, then replay it.

`playbooks/storage/image_destroy.yml` runs the Ceph image role and its
resolution module over the real runner protocol with the request Go freezes
(`tests/unit/goldens/ceph_request_goldens.json`, `image-trust-bundle`). A
release reads nothing remote and retains nothing, so it publishes an
unchanged `completed` record proving the resolution absent, and a replay must
publish the same. It is the one Ceph entry point that runs without a cluster:
every other one reaches `cephadm`, `ceph` or a registry.
"""

import json
from pathlib import Path
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "setup_entry_point"))
import entry_point  # noqa: E402  pylint: disable=wrong-import-position

DIGEST = "e" * 64
GOLDENS = entry_point.collection() / "tests/unit/goldens/ceph_request_goldens.json"
VARIABLES = {
    "bootwright_ceph_image_request": json.loads(GOLDENS.read_text(encoding="utf-8"))["requests"]["image-trust-bundle"],
    "bootwright_ceph_image_digest": DIGEST,
    "bootwright_ceph_image_material": {},
}


def check(record):
    evidence = record["evidence"]
    if evidence.get("request") != DIGEST or evidence.get("absent") is not True:
        raise entry_point.Failure("the release did not prove the resolution absent")
    return evidence


with tempfile.TemporaryDirectory(prefix="bootwright-ceph-image-") as work:
    entry_point.replay(entry_point.EntryPoint(work, "storage/image_destroy.yml"), VARIABLES, check, first_outcome="unchanged")
print("PASS Ceph image release proved the resolution absent and a replay changed nothing")
