"""Native registry credential custody: the retained podman auth file, its preflight, preparation, proof and removal."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import os

from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    REGISTRY_FILE, REGISTRY_NAME, registry_authority, registry_directory, registry_exists, registry_metadata, registry_metadata_record, registry_read,
    registry_scope, require,
)


class Registry:
    """Native registry credential custody: the retained podman auth file, its preflight, preparation, proof and removal."""

    def __init__(self, worker):
        self.worker = worker

    def registry_intent(self):
        token = hashlib.sha256((self.worker.request["owner"] + "\x00" + self.worker.request["fsid"]).encode()).hexdigest()
        return {"version": "ceph-registry-v1", "owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"],
                "host": self.worker.host["name"], "registry": registry_authority(self.worker.request), "path": REGISTRY_FILE,
                "anchor": ".bootwright-registry-" + token}

    def registry_preflight(self, wanted):
        """No mutation or adoption before foreign account and file checks pass."""
        require(self.worker.markers.owned(), "ownership")
        registry_scope(self.worker.request, wanted)
        intent = self.worker.registry_intent()
        recorded = self.worker.markers.read("registry-intent.json")
        pending = self.worker.markers.read("registry-pending.json")
        ready = self.worker.markers.read("registry-file.json")
        require(recorded is None or recorded == intent, "registry-ownership")
        require(self.worker.markers.read("registry-removed.json") is None, "registry-ownership")
        for record, is_pending in ((pending, True), (ready, False)):
            require(record is None or isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent, "registry-ownership")
            if record is not None:
                registry_metadata_record(record["metadata"], pending=is_pending)
        require(pending is None or recorded == intent, "registry-ownership")
        require(ready is None or pending is not None, "registry-ownership")
        with registry_directory() as directory:
            if pending is None:
                # An unbound foreign name authorizes no credential read.
                require(not registry_exists(directory, REGISTRY_NAME) and not registry_exists(directory, intent["anchor"]), "registry-ownership")
                return intent, pending, ready
            if ready is not None:
                require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
                final = registry_read(directory, REGISTRY_NAME, ready["metadata"])
                anchor = None
            else:
                final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True, links=(1, 2))
                anchor = registry_read(directory, intent["anchor"], pending["metadata"], pending=True, links=(1, 2))
            if ready is not None:
                require(final is not None and anchor is None, "registry-ownership")
                require(registry_metadata(final[0]) == ready["metadata"] and registry_metadata(final[0], pending=True) == pending["metadata"],
                        "registry-ownership")
                require(final[1] == wanted, "registry-ownership")
            elif pending is not None:
                require(final is not None or anchor is not None, "registry-ownership")
                for item in (final, anchor):
                    if item is not None:
                        require(registry_metadata(item[0], pending=True) == pending["metadata"] and item[1] == wanted, "registry-ownership")
                require((final is None or anchor is None) and (final or anchor)[0].st_nlink == 1 or
                        final is not None and anchor is not None and final[0].st_nlink == anchor[0].st_nlink == 2, "registry-ownership")
        return intent, pending, ready

    def prepare_registry(self, wanted):
        """Publish the native authfile from stdin using a prebound private inode.

        Once the pending binding is durable, every interruption can be resumed
        only with that inode and exact protected bytes. No secret-derived hash
        enters any journal. An unbound private anchor is never adopted.
        """
        intent, pending, ready = self.worker.registry_preflight(wanted)
        if ready is not None:
            self.worker.prove_registry(wanted)
            return False
        self.worker.markers.write("registry-intent.json", intent)
        with registry_directory(create=True) as directory:
            if pending is None:
                fd = os.open(intent["anchor"], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
                created = os.fstat(fd)
                bound = False
                try:
                    with os.fdopen(fd, "wb") as handle:
                        os.fchmod(handle.fileno(), 0o600)
                        os.fchown(handle.fileno(), 0, 0)
                        handle.write(wanted)
                        handle.flush()
                        os.fsync(handle.fileno())
                        complete = registry_metadata(os.fstat(handle.fileno()))
                    os.fsync(directory)
                    item = registry_read(directory, intent["anchor"], complete)
                    require(item is not None and item[0].st_dev == created.st_dev and item[0].st_ino == created.st_ino and item[1] == wanted,
                            "registry-ownership")
                    pending = {"intent": intent, "metadata": registry_metadata(item[0], pending=True)}
                    self.worker.markers.write("registry-pending.json", pending)
                    # A journal write may fail after its durable publication.
                    # Re-read it before deciding whether cleanup may unlink.
                    require(self.worker.markers.read("registry-pending.json") == pending, "registry-ownership")
                    bound = True
                finally:
                    durable = self.worker.markers.read("registry-pending.json")
                    if not bound and durable is None:
                        observed = os.stat(intent["anchor"], dir_fd=directory, follow_symlinks=False)
                        require(observed.st_dev == created.st_dev and observed.st_ino == created.st_ino and observed.st_nlink == 1, "registry-ownership")
                        os.unlink(intent["anchor"], dir_fd=directory)
                        os.fsync(directory)
            # Reprove the exact reserved inode and bytes after its journal is
            # durable, before linking it to the provider's fixed global name.
            self.worker.registry_preflight(wanted)
            final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True, links=(1, 2))
            anchor = registry_read(directory, intent["anchor"], pending["metadata"], pending=True, links=(1, 2))
            if final is None:
                require(anchor is not None and registry_metadata(anchor[0], pending=True) == pending["metadata"], "registry-ownership")
                os.link(intent["anchor"], REGISTRY_NAME, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
                os.fsync(directory)
            if anchor is not None:
                self.worker.registry_preflight(wanted)
                os.unlink(intent["anchor"], dir_fd=directory)
                os.fsync(directory)
            final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True)
            require(final is not None and registry_metadata(final[0], pending=True) == pending["metadata"] and final[1] == wanted, "registry-ownership")
            self.worker.markers.write("registry-file.json", {"intent": intent, "metadata": registry_metadata(final[0])})
        self.worker.prove_registry(wanted)
        return True

    def prove_registry(self, wanted=None):
        require(self.worker.markers.owned(), "ownership")
        intent = self.worker.registry_intent()
        require(self.worker.markers.read("registry-intent.json") == intent and self.worker.markers.read("registry-removed.json") is None, "registry-ownership")
        record = self.worker.markers.read("registry-file.json")
        pending = self.worker.markers.read("registry-pending.json")
        require(isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent, "registry-ownership")
        require(isinstance(pending, dict) and set(pending) == {"intent", "metadata"} and pending["intent"] == intent, "registry-ownership")
        registry_metadata_record(record["metadata"])
        registry_metadata_record(pending["metadata"], pending=True)
        with registry_directory() as directory:
            require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
            item = registry_read(directory, REGISTRY_NAME, record["metadata"])
            require(item is not None, "registry-ownership")
            require(registry_metadata(item[0]) == record["metadata"] and registry_metadata(item[0], pending=True) == pending["metadata"], "registry-ownership")
            registry_scope(self.worker.request, item[1])
            require(wanted is None or item[1] == wanted, "registry-ownership")

    def provider_registry_absent(self):
        # Reading names avoids disclosing values. Any stored credential would
        # re-enable provider registry-login/password argv during remote pulls.
        keys = self.worker.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 4096 and all(isinstance(key, str) and len(key) <= 4096 for key in keys), "native-output")
        require(len(set(keys)) == len(keys) and "mgr/cephadm/registry_credentials" not in keys, "registry-credentials-present")

    def remove_registry(self):
        """Internal inverse remains unreachable from the closed destroy phase."""
        proof = {"version": "ceph-native-teardown-v1", "owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"],
                 "host": self.worker.host["name"], "removed": True}
        teardown = self.worker.markers.read("native-teardown.json")
        require(self.worker.markers.owned() and isinstance(teardown, dict) and teardown == proof and type(teardown.get("removed")) is bool
                and not self.worker.daemons(), "registry-teardown-unproved")
        record = self.worker.markers.read("registry-file.json")
        require(isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == self.worker.registry_intent(), "registry-ownership")
        registry_metadata_record(record["metadata"])
        pending = self.worker.markers.read("registry-pending.json")
        require(self.worker.markers.read("registry-intent.json") == record["intent"] and isinstance(pending, dict) and set(pending) == {"intent", "metadata"}
                and pending["intent"] == record["intent"], "registry-ownership")
        registry_metadata_record(pending["metadata"], pending=True)
        require(pending["metadata"] == {key: value for key, value in record["metadata"].items() if key != "ctimeNS"}, "registry-ownership")
        removing, removed = self.worker.markers.read("registry-removal.json"), self.worker.markers.read("registry-removed.json")
        for value in (removing, removed):
            require(value is None or isinstance(value, dict) and set(value) == {"intent", "metadata"} and value["intent"] == record["intent"],
                    "registry-ownership")
            if value is not None:
                registry_metadata_record(value["metadata"])
                require(value == record, "registry-ownership")
        require(removed is None or removed == record and removing == record, "registry-ownership")
        if removing is not None:
            with registry_directory() as directory:
                require(not registry_exists(directory, record["intent"]["anchor"]), "registry-ownership")
                if not registry_exists(directory, REGISTRY_NAME):
                    self.worker.markers.write("registry-removed.json", record)
                    return removed is None
            require(removed is None, "registry-ownership")
        self.worker.prove_registry()
        self.worker.markers.write("registry-removal.json", record)
        with registry_directory() as directory:
            item = registry_read(directory, REGISTRY_NAME, record["metadata"])
            require(item is not None and registry_metadata(item[0]) == record["metadata"], "registry-ownership")
            registry_scope(self.worker.request, item[1])
            os.unlink(REGISTRY_NAME, dir_fd=directory)
            os.fsync(directory)
        self.worker.markers.write("registry-removed.json", record)
        return True
