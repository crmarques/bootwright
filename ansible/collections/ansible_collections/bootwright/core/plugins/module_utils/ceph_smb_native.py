"""Fixed SMB manager resource worker, shipped beside the core worker as source.

One block applies or observes one SMB manager resource on the cluster's bootstrap host. `smb apply -i -` reads the one
resource as JSON on stdin, the bound passwords spliced in memory only, with `--password-filter-out=hidden`, because the
output filter defaults to the input one and the answer would otherwise echo them (src/pybind/mgr/smb/module.py:145-193).
An invalid resource is answered with its raw input, password included (src/pybind/mgr/smb/results.py:68-88), so no apply
answer reaches an error, a record or a log: an answer that is not one successful result for the resource refuses with its
reason alone. Every resource is read back with `smb show <name> --password-filter=hidden`, which answers one matched
resource itself and otherwise a `resources` list (module.py:389-412) with each password as sixteen asterisks
(src/pybind/mgr/smb/resources.py:37, :770-782). The worker records its attempt before the first apply and its completion
after the readback, never adopts a resource, never writes an image setting and never enables a manager module: the
cluster's settings block is their single writer. Like the core worker it imports only the target Python standard
library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import copy
import ipaddress
import json
import os
import re

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_nfs, ceph_native_resources
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, canonical, named, require, unique_object

VERSION = "ceph-smb-v1"
EVIDENCE_VERSION = "ceph-smb-evidence-v1"
# Each SMB manager resource type with the native member naming its identity.
TYPES = {"ceph.smb.cluster": "cluster_id", "ceph.smb.share": "share_id", "ceph.smb.join.auth": "auth_id", "ceph.smb.usersgroups": "users_groups_id"}
SERVED = ("ceph.smb.cluster", "ceph.smb.share")
REQUIRED = frozenset(("bindings", "cluster", "controller", "id", "machines", "native", "object", "resourceType", "scope", "version"))
OPTIONAL = frozenset(("clusterIDs", "images", "service"))
USER_PASSWORD = re.compile(r"/values/users/(?:0|[1-9][0-9]{0,3})/password\Z")
IMAGE = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}\Z")
PHASES = frozenset(("smb_apply", "smb_observe"))
MASK = "*" * 16
SMB_PORT = 445
PENDING = re.compile(r"(?:smb|vip)@smb\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,16}[a-zA-Z0-9])?\Z")
SAMBA_KEYS = (ceph_native_resources.IMAGE_KEY + "samba", ceph_native_resources.IMAGE_KEY + "samba_metrics")
MAX_SECRET = 1024
MAX_DAEMONS = 128
MAX_LISTED = 1024
DAEMON_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\Z")
# The members admission lets a CephSMBResource author, per type; the manager may print others, which are not compared.
ADMITTED = {
    "ceph.smb.cluster": ("resource_type", "cluster_id", "intent", "auth_mode", "domain_settings", "user_group_settings", "custom_dns", "placement",
                         "clustering", "public_addrs"),
    "ceph.smb.share": ("resource_type", "cluster_id", "share_id", "intent", "name", "readonly", "browseable", "cephfs", "restrict_access", "login_control"),
    "ceph.smb.join.auth": ("resource_type", "auth_id", "intent", "auth"),
    "ceph.smb.usersgroups": ("resource_type", "users_groups_id", "intent", "values"),
}
# The members admission refuses, per type: the manager may print each only unset.
REFUSED = {
    "ceph.smb.cluster": ("custom_smb_global_options", "client_compat", "custom_ports", "bind_addrs", "remote_control"),
    "ceph.smb.share": ("comment", "max_connections", "custom_smb_share_options"),
    "ceph.smb.join.auth": ("linked_to_cluster",),
    "ceph.smb.usersgroups": ("linked_to_cluster",),
}
# The manager's defaults for an omitted member (src/pybind/mgr/smb/resources.py:138-142, :180-183, :221-236, :298, :339).
SHARE_DEFAULTS = (("readonly", False), ("browseable", True), ("restrict_access", False))
CEPHFS_DEFAULTS = (("path", "/"), ("subvolumegroup", ""), ("subvolume", ""), ("provider", "samba-vfs"))
LOGIN_DEFAULTS = (("category", "user"), ("access", "read"))


def ascending(values):
    """A nonempty list of distinct names in byte order."""
    return isinstance(values, list) and bool(values) and all(named(value) for value in values) and values == sorted(set(values))


def validate_bindings(value):
    """The Secret references a join auth or usersgroups resource binds, and none on a served resource."""
    bindings = value["bindings"]
    require(isinstance(bindings, list))
    for binding in bindings:
        require(isinstance(binding, dict) and set(binding) == {"path", "secretRef"} and named(binding["secretRef"]))
        require(isinstance(binding["path"], str))
    paths = [binding["path"] for binding in bindings]
    if value["resourceType"] in SERVED:
        require(not bindings)
    elif value["resourceType"] == "ceph.smb.join.auth":
        require(paths == ["/auth/password"])
    else:
        require(bool(paths) and all(USER_PASSWORD.fullmatch(path) for path in paths) and paths == sorted(set(paths)))


def validate_service(value, scope):
    """A served resource's SMB service, the declared SMB cluster IDs and both samba images beneath the scope registry."""
    if value["resourceType"] not in SERVED:
        require(not OPTIONAL & set(value))
        return
    require(OPTIONAL <= set(value))
    service, identities, images = value["service"], value["clusterIDs"], value["images"]
    require(isinstance(service, dict) and set(service) == {"clustered", "hosts", "name"} and type(service["clustered"]) is bool)
    require(ascending(service["hosts"]) and ascending(identities))
    own = value["id"] if value["resourceType"] == "ceph.smb.cluster" else value["native"].get("cluster_id")
    require(service["name"] == "smb." + str(own) and own in identities)
    require(isinstance(images, list) and [entry.get("name") if isinstance(entry, dict) else None for entry in images] == ["samba", "samba_metrics"])
    for entry in images:
        require(set(entry) == {"image", "name"} and isinstance(entry["image"], str) and IMAGE.fullmatch(entry["image"]) is not None)
        require(entry["image"].startswith(scope["registry"] + "/"))


def validate(value):
    """A completed SMB block request: the frozen resource with its cluster scope; every failure is the request's."""
    require(isinstance(value, dict) and REQUIRED <= set(value) and not set(value) - REQUIRED - OPTIONAL)
    require(value["version"] == VERSION and value["resourceType"] in TYPES)
    require(all(named(value[key]) for key in ("cluster", "controller", "object")))
    native, identity = value["native"], value["id"]
    require(isinstance(native, dict) and native.get("resource_type") == value["resourceType"])
    require(isinstance(identity, str) and identity != "" and native.get(TYPES[value["resourceType"]]) == identity)
    require(ascending(value["machines"]))
    try:
        scope = ceph_native.validate_scope(value["scope"])
    except Refusal:
        raise Refusal("request") from None
    require(scope["cluster"] == value["cluster"])
    validate_bindings(value)
    validate_service(value, scope)
    return value


def material(request, private):
    """Exactly one password per binding, each one line of 1 to 1024 bytes of UTF-8."""
    names = {"smbSecret" + str(index) for index in range(len(request["bindings"]))}
    require(isinstance(private, dict) and set(private) == names, "material")
    for value in private.values():
        require(isinstance(value, str) and not any(char in value for char in "\x00\r\n"), "material")
        try:
            size = len(value.encode("utf-8"))
        except UnicodeError:
            raise Refusal("material") from None
        require(1 <= size <= MAX_SECRET, "material")
    return private


def dispatch(envelope):
    """Route one SMB phase to its worker on the cluster's bootstrap host."""
    require(isinstance(envelope, dict) and set(envelope) == {"request", "host", "phase", "private"})
    phase, private = envelope["phase"], envelope["private"]
    require(phase in PHASES and isinstance(private, dict))
    request = validate(envelope["request"])
    require(envelope["host"] == request["scope"]["bootstrap"])
    worker = SMBWorker(request, envelope["host"])
    worker.horizon = worker.clock() + ceph_native.MANAGER_HORIZON
    if phase == "smb_apply":
        require(ceph_native.supported_transport(worker.request))
        return worker.smb_apply(material(request, private))
    require(private == {}, "material")
    return worker.smb_observe()


def stripped(value):
    """The value without any mapping member that is None, at every depth."""
    if isinstance(value, dict):
        return {key: stripped(item) for key, item in value.items() if item is not None}
    if isinstance(value, list):
        return [stripped(item) for item in value]
    return value


def passwords(value, path=""):
    """Each JSON pointer of a `password` member, with its value."""
    found = {}
    if isinstance(value, dict):
        for key, item in value.items():
            if key == "password":
                found[path + "/" + key] = item
            else:
                found.update(passwords(item, path + "/" + str(key)))
    elif isinstance(value, list):
        for index, item in enumerate(value):
            found.update(passwords(item, path + "/" + str(index)))
    return found


def unmasked(value, resource_type):
    """The observed resource without its passwords: each masked, and only where its type keeps one; the value is never kept or quoted."""
    found = passwords(value)
    if resource_type == "ceph.smb.join.auth":
        require(set(found) <= {"/auth/password"}, "native-output")
    else:
        require(resource_type == "ceph.smb.usersgroups" or not found, "native-output")
        require(all(USER_PASSWORD.fullmatch(path) for path in found), "native-output")
    require(all(item == MASK for item in found.values()), "native-output")
    result = copy.deepcopy(value)
    for path in found:
        parts = path.strip("/").split("/")
        parent = result
        for part in parts[:-1]:
            parent = parent[int(part)] if isinstance(parent, list) else parent[part]
        del parent[parts[-1]]
    return result


def unrealized(value, resource_type):
    """Whether the observed resource sets a member admission refuses."""
    for key in REFUSED[resource_type]:
        item = value.get(key)
        if item not in (None, {}, []) and not (key == "client_compat" and item == "default"):
            return True
    return False


def placement(value):
    """A placement with its hosts as sorted names: a host object reduces to its hostname when it names nothing else."""
    require(isinstance(value, dict), "native-output")
    if "hosts" not in value:
        return value
    hosts = value["hosts"]
    require(isinstance(hosts, list), "native-output")
    names = []
    for host in hosts:
        if isinstance(host, dict):
            require(isinstance(host.get("hostname"), str) and set(host) <= {"hostname", "network", "name"}, "native-output")
            require(all(host.get(key) in (None, "") for key in ("network", "name")), "native-output")
            host = host["hostname"]
        require(isinstance(host, str), "native-output")
        names.append(host)
    return dict(value, hosts=sorted(names))


def defaults(value, resource_type):
    """Fill the manager's default for each omitted member."""
    if resource_type == "ceph.smb.share":
        if value.get("name") in (None, ""):
            value["name"] = value.get("share_id")
        for key, default in SHARE_DEFAULTS:
            value.setdefault(key, default)
        if isinstance(value.get("cephfs"), dict):
            for key, default in CEPHFS_DEFAULTS:
                value["cephfs"].setdefault(key, default)
        for entry in value.get("login_control") or []:
            if isinstance(entry, dict):
                for key, default in LOGIN_DEFAULTS:
                    entry.setdefault(key, default)
    if resource_type == "ceph.smb.cluster":
        sources = list(value.get("user_group_settings") or [])
        if isinstance(value.get("domain_settings"), dict):
            sources += value["domain_settings"].get("join_sources") or []
        for entry in sources:
            if isinstance(entry, dict):
                entry.setdefault("source_type", "resource")
        if "placement" in value:
            value["placement"] = placement(value["placement"])
        if isinstance(value.get("custom_dns"), list):
            value["custom_dns"] = sorted(value["custom_dns"], key=canonical)
    return value


def projection(value, resource_type):
    """The comparable resource: no unset member, no default intent, only admitted members, every manager default filled."""
    value = stripped(value)
    if value.get("intent") == "present":
        del value["intent"]
    value = {key: item for key, item in value.items() if key in ADMITTED[resource_type]}
    return defaults(value, resource_type)


class SMBWorker(ceph_native_resources.ResourceWorker):
    """The core worker bound to one cluster scope, with one SMB resource's apply and read-only observation."""

    admit = staticmethod(ceph_native.validate_scope)

    def __init__(self, request, host, run=ceph_native.bounded_command, markers=None):
        self.smb = validate(request)
        super().__init__(request["scope"], host, run, markers)

    def served(self):
        return self.smb["resourceType"] in SERVED

    def share(self):
        return self.smb["resourceType"] == "ceph.smb.share"

    def recovery_scope(self):
        """The cluster's bootstrap scope: every member this scope carries, and the SMB services its delegated list names."""
        unused, staging = ceph_native.tunnel_helpers()
        own = staging.recovery_scope(self.request)
        recorded = self.markers.read("bootstrap-scope.json")
        require(isinstance(recorded, dict) and isinstance(recorded.get("cluster"), dict), "ownership")
        cluster = dict(recorded["cluster"])
        delegated = cluster.pop("delegated", [])
        require(canonical(dict(recorded, cluster=cluster)) == canonical(own), "ownership")
        require(isinstance(delegated, list) and all(isinstance(entry, dict) and isinstance(entry.get("name"), str) for entry in delegated), "ownership")
        if self.served():
            service = self.smb["service"]
            require({"hosts": service["hosts"], "name": service["name"]} in delegated, "ownership")
            listed = {entry["name"] for entry in delegated if entry["name"].startswith("smb.")}
            require(listed == {"smb." + identity for identity in self.smb["clusterIDs"]}, "ownership")
        return recorded

    def ceph(self, *args, data=b"", json_result=True, recover=None):
        """The core worker's admin command; an apply alone passes `recover`, which keeps its exit code for its own refusal."""
        if recover is None:
            return super().ceph(*args, data=data, json_result=json_result)
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verifiedTunnel" else contextlib.nullcontext()
        with proof:
            return self.command(["/usr/bin/ceph", "--conf", os.path.join(self.markers.path, "ceph.conf"),
                                 "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"), "--format", "json", *args],
                                data=data, json_result=json_result, recover=recover)

    def name(self, resource_type=None, identity=None):
        """The manager's resource name: its type and ID, a share's prefixed by its SMB cluster ID."""
        resource_type = resource_type or self.smb["resourceType"]
        identity = identity or self.smb["id"]
        prefix = self.smb["native"]["cluster_id"] + "." if resource_type == "ceph.smb.share" else ""
        return resource_type + "." + prefix + identity

    def intent(self):
        return {"bindings": [binding["path"] for binding in self.smb["bindings"]], "fsid": self.request["fsid"], "native": self.smb["native"],
                "object": self.smb["object"], "owner": self.request["owner"], "resource": self.name()}

    def records(self):
        return "smb-attempt." + self.name() + ".json", "smb-applied." + self.name() + ".json"

    def identity(self, phrase):
        return NativeFailure("smb-identity", self.native_coordinate(), self.name() + " " + phrase)

    def foreign(self, cause):
        return NativeFailure("foreign-service", self.native_coordinate(), cause)

    def held(self):
        """This operation's attempt and completion records, each its own intent or absent."""
        intent = canonical(self.intent())
        attempt, applied = (self.markers.read(name) for name in self.records())
        for record in (attempt, applied):
            if record is not None and canonical(record) != intent:
                raise self.identity("is not the resource this operation attempted")
        return attempt, applied

    def proved(self):
        """The owned cluster of this FSID with the smb manager module enabled; for a served resource only declared SMB clusters."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        if "smb" not in self.manager_modules():
            raise Refusal("smb-module-disabled")
        if self.served():
            self.only_declared()

    def show(self, resource_type=None, identity=None):
        """One named resource as `smb show` reads it back, passwords masked; None when absent."""
        resource_type = resource_type or self.smb["resourceType"]
        identity = identity or self.smb["id"]
        value = self.manager_retry(self.ceph, "smb", "show", self.name(resource_type, identity), "--password-filter=hidden")
        if isinstance(value, dict) and "resources" in value and "resource_type" not in value:
            require(set(value) == {"resources"} and isinstance(value["resources"], list) and len(value["resources"]) <= 1, "native-output")
            if not value["resources"]:
                return None
            value = value["resources"][0]
        require(isinstance(value, dict) and value.get("resource_type") == resource_type and value.get(TYPES[resource_type]) == identity, "native-output")
        require(resource_type != "ceph.smb.share" or value.get("cluster_id") == self.smb["native"]["cluster_id"], "native-output")
        require(all(item == MASK for item in passwords(stripped(value)).values()), "native-output")
        return value

    def matches(self, observed):
        """The readback postcondition: every admitted member after the manager's defaults, no refused member set, every password masked."""
        resource_type = self.smb["resourceType"]
        observed = stripped(observed)
        if unrealized(observed, resource_type):
            raise self.identity("holds a field Bootwright does not realize")
        observed = unmasked(observed, resource_type)
        return canonical(projection(observed, resource_type)) == canonical(projection(self.smb["native"], resource_type))

    def only_declared(self):
        """Every SMB cluster and smb service on the cluster is one a CephSMBResource of this cluster declares."""
        identities = self.smb["clusterIDs"]
        listed = self.manager_retry(self.ceph, "smb", "cluster", "ls")
        require(isinstance(listed, list) and len(listed) <= MAX_LISTED and all(isinstance(item, str) for item in listed), "native-output")
        for identity in listed:
            if identity not in identities:
                raise self.foreign("SMB cluster " + identity)
        services = {"smb." + identity for identity in identities}
        for row in self.manager_retry(self.services_inventory):
            if row.get("service_type") == "smb":
                service = row.get("service_name")
                require(isinstance(service, str), "native-output")
                if service not in services:
                    raise self.foreign("service " + service)

    def images(self):
        """Both samba image settings held at the frozen digests; the settings block writes them, this block only reads them."""
        auxiliary = {entry["name"]: entry["image"] for entry in self.request.get("auxiliary", [])}
        require(all(auxiliary.get(entry["name"]) == entry["image"] for entry in self.smb["images"]))
        unused, held = self.image_settings()
        missing = [key for key in SAMBA_KEYS if key not in held]
        if missing:
            raise NativeFailure("foreign-image", self.native_coordinate(), ", ".join(missing))

    def document(self, private):
        """The apply stream: the frozen resource with each bound password spliced in memory."""
        document = copy.deepcopy(self.smb["native"])
        if self.smb["resourceType"] == "ceph.smb.join.auth":
            auth = document.get("auth")
            require(isinstance(auth, dict) and "password" not in auth)
            auth["password"] = private["smbSecret0"]
        elif self.smb["resourceType"] == "ceph.smb.usersgroups":
            values = document.get("values")
            users = values.get("users") if isinstance(values, dict) else None
            require(isinstance(users, list))
            for index, binding in enumerate(self.smb["bindings"]):
                position = int(binding["path"].split("/")[3])
                require(position < len(users) and isinstance(users[position], dict) and "password" not in users[position])
                users[position]["password"] = private["smbSecret" + str(index)]
        return canonical({"resources": [document]})

    def answered(self, output):
        """Whether the apply answer is one successful result for this resource; the answer itself is never kept."""
        resource_type = self.smb["resourceType"]
        try:
            value = json.loads(output, object_pairs_hook=unique_object)
        except (ValueError, UnicodeError, TypeError):
            return False
        if not (isinstance(value, dict) and value.get("success") is True and isinstance(value.get("results"), list) and len(value["results"]) == 1):
            return False
        result = value["results"][0]
        resource = result.get("resource") if isinstance(result, dict) else None
        return (isinstance(resource, dict) and result.get("success") is True and resource.get("resource_type") == resource_type
                and resource.get(TYPES[resource_type]) == self.smb["id"])

    def apply_resource(self, payload):
        """One apply of the resource on stdin, never retried; any answer but one successful result refuses without the manager's words."""
        seen = {}
        output = self.tunnel_effect("smb", "apply", "-i", "-", "--password-filter=none", "--password-filter-out=hidden", data=payload, json_result=False,
                                    recover=lambda code, unused: seen.setdefault("code", code) is not None)
        accepted = not seen and self.answered(output)
        del output
        if not accepted:
            raise Refusal("smb-result")

    def smb_apply(self, private):
        """Prove ownership, the module, the declared SMB clusters and the samba images before any effect; then apply or continue once."""
        self.proved()
        if self.served():
            self.images()
        attempt, applied = self.held()
        observed = self.show()
        attempt_name, applied_name = self.records()
        if applied is not None:
            if observed is None:
                raise self.identity("that this operation created is gone")
            if not self.matches(observed):
                raise self.identity("that this operation created changed")
            return {"changed": False}
        if observed is not None:
            if attempt is None:
                raise self.identity("has no creation record of this operation")
            if not self.matches(observed):
                raise self.identity("is not the resource this operation attempted")
            self.markers.write(applied_name, self.intent())
            return {"changed": True}
        payload = self.document(private)
        if attempt is None:
            self.markers.write(attempt_name, self.intent())
        self.apply_resource(payload)
        observed = self.show()
        if observed is None or not self.matches(observed):
            raise Refusal("smb-result")
        self.markers.write(applied_name, self.intent())
        return {"changed": True}

    def smb_observe(self):
        """Read-only: the resource this operation applied, and for a share the generated service's daemons and addresses."""
        self.proved()
        attempt, applied = self.held()
        observed = self.show()
        if observed is not None and attempt is None:
            raise self.identity("has no creation record of this operation")
        if observed is not None and unrealized(stripped(observed), self.smb["resourceType"]):
            raise self.identity("holds a field Bootwright does not realize")
        owned = attempt is not None
        exists = owned or observed is not None
        ready = applied is not None and observed is not None and self.matches(observed)
        daemons, blocking, directory = [], [], False
        if self.share() and exists:
            cluster = self.show("ceph.smb.cluster", self.smb["native"]["cluster_id"])
            directory = cluster is not None and cluster.get("auth_mode") == "active-directory"
            running, daemons, blocking = self.daemons_ready(cluster)
            ready = ready and running
        return {"version": EVIDENCE_VERSION, "owner": self.request["owner"], "fsid": self.request["fsid"], "owned": owned, "exists": exists,
                "ready": ready, "partial": exists and not ready, "removed": False, "resource": self.name() if observed is not None else "",
                "daemons": daemons, "blocking": blocking, "directory": directory}

    def daemon_rows(self):
        """The generated service's daemons: each of that service, on its hosts, on the frozen samba image or none yet; another declared
        SMB cluster's daemons are that cluster's own."""
        service, frozen = self.smb["service"], self.smb["images"][0]["image"]
        others = {"smb." + identity for identity in self.smb["clusterIDs"]} - {service["name"]}
        rows = self.manager_retry(self.ceph, "orch", "ps")
        require(isinstance(rows, list) and len(rows) <= MAX_LISTED and all(isinstance(row, dict) for row in rows), "native-output")
        daemons, running = [], 0
        for row in rows:
            if row.get("daemon_type") != "smb":
                continue
            identifier, host, name = row.get("daemon_id"), row.get("hostname"), row.get("service_name")
            require(isinstance(identifier, str) and DAEMON_ID.fullmatch(identifier) is not None and isinstance(host, str) and isinstance(name, str),
                    "native-output")
            if name in others:
                continue
            if name != service["name"] or host not in service["hosts"]:
                raise self.foreign("daemon smb." + identifier)
            images = row.get("container_image_digests") or []
            require(isinstance(images, list) and all(isinstance(image, str) for image in images), "native-output")
            require(not images or frozen in images, "artifact")
            running += type(row.get("status")) is int and row["status"] == 1 and frozen in images
            daemons.append({"host": host, "name": "smb." + identifier, "service": name})
        require(len(daemons) <= MAX_DAEMONS, "native-output")
        return sorted(daemons, key=lambda daemon: daemon["name"]), running

    def daemons_ready(self, cluster):
        """As many running daemons as the placement count, else one per placement host, and each public address answering TCP 445."""
        service = self.smb["service"]
        daemons, running = self.daemon_rows()
        blocking = []
        count = cluster.get("placement", {}).get("count") if cluster is not None and isinstance(cluster.get("placement"), dict) else None
        expected = count if type(count) is int and count >= 1 else len(service["hosts"])
        if cluster is None or running < expected:
            blocking.append("smb@" + service["name"])
        if cluster is not None and service["clustered"]:
            addresses = cluster.get("public_addrs") or []
            require(isinstance(addresses, list) and all(isinstance(entry, dict) and isinstance(entry.get("address"), str) for entry in addresses),
                    "native-output")
            for entry in addresses:
                try:
                    address = str(ipaddress.ip_address(entry["address"].split("/", 1)[0]))
                except ValueError:
                    raise Refusal("native-output") from None
                if not ceph_native_nfs.reachable(address, SMB_PORT):
                    blocking.append("vip@" + service["name"])
                    break
        return cluster is not None and not blocking, daemons, blocking
