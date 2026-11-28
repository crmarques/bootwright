"""Controller-only vCenter session and exact inventory lookups."""

from __future__ import annotations

import contextlib
import re
import ssl
import time
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import private_files, tls_trust


class Refused(ValueError):
    """A fixed, public diagnosis. Native faults and their arguments remain private."""

    def __init__(self, message, code=""):
        super().__init__(message)
        self.code = code


def require(condition, message, code=""):
    if not condition:
        raise Refused(message, code)


def integer(value):
    return isinstance(value, int) and not isinstance(value, bool)


def origin(value):
    url = urllib.parse.urlsplit(value)
    require(url.scheme == "https" and url.hostname and url.username is None and not url.password
            and not url.path.strip("/") and not url.query and not url.fragment and not re.search(r"[\s\\]", value),
            "the vCenter endpoint must be a verified HTTPS origin")
    return url


def private_text(path, limit=65536):
    return private_files.read(path, limit).decode("utf-8")


@contextlib.contextmanager
def connect(endpoint, user, password, ca, verify):
    # The SDK belongs only to the controller runtime. Module utilities also
    # undergo import checks under the managed-host Python floor.
    from pyVim.connect import Connect, Disconnect
    from pyVmomi import vim, vmodl

    require(verify is True, "vCenter management and media require verified TLS")
    url = origin(endpoint)
    tls = tls_trust.context(private_text(ca, 1 << 20)) if ca else ssl.create_default_context()
    service = Connect(host=url.hostname, port=url.port or 443, user=private_text(user), pwd=private_text(password),
                      sslContext=tls, httpConnectionTimeout=30, version="vim.version.v8_0_3_0")
    try:
        yield Session(service, vim, vmodl, url, tls)
    finally:
        Disconnect(service)


class Session:
    def __init__(self, service, vim, vmodl, url, tls):
        self.pin = None
        self.check_hardware = None
        self.service, self.vim, self.vmodl = service, vim, vmodl
        self.url, self.tls = url, tls
        self.content = service.RetrieveContent()
        self.uuid = self.content.about.instanceUuid
        require(self.content.about.apiType == "VirtualCenter" and self.uuid,
                "the endpoint did not identify a vCenter instance")

    def wait(self, task, seconds=1500):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            info = task.info
            if info.state == "success":
                return info.result
            if info.state == "error":
                raise info.error
            time.sleep(1)
        raise Refused("the vCenter task did not complete within its bound; observe before retry")

    def lookup(self, path, kind):
        require(isinstance(path, str) and path.startswith("/") and all(
            part not in ("", ".", "..") for part in path[1:].split("/")), "an absolute inventory path is required")
        item = self.content.searchIndex.FindByInventoryPath(inventoryPath=path)
        require(isinstance(item, kind), "a required inventory path is missing or identifies the wrong type")
        return item

    def pinned_vm(self, pin):
        require(pin.get("vcenterUUID") == self.uuid and pin.get("moRef") and pin.get("instanceUUID") and pin.get("owner"),
                "the recorded vCenter or Machine identity is missing or different", "identity-mismatch")
        vm = self.vim.VirtualMachine(pin["moRef"], self.service._stub)
        try:
            config = vm.config
        except self.vmodl.fault.ManagedObjectNotFound as fault:
            require(fault.obj == vm, "vCenter did not positively identify the missing VM")
            return None
        require(config and config.instanceUuid, "vCenter did not return the VM configuration")
        require(config.instanceUuid == pin["instanceUUID"] and options(config).get(OWNER) == pin["owner"] and not config.template,
                "the VM identity or owner differs from its receipt", "identity-mismatch")
        self.pin = pin
        return vm

    def guard(self, vm):
        require(self.pin and self.pinned_vm(self.pin) == vm, "vCenter no longer proves the original VM", "identity-mismatch")
        config = vm.config
        require(config and config.instanceUuid == self.pin["instanceUUID"] and options(config).get(OWNER) == self.pin["owner"],
                "the original VM identity is no longer present", "identity-mismatch")
        if self.check_hardware:
            self.check_hardware(config)
        return config


OWNER = "bootwright.owner"
OPERATION = "bootwright.operation"
STAGE = "bootwright.media"
SEED = "bootwright.seed"


def options(config):
    return {item.key: item.value for item in config.extraConfig or []}


def power(vm):
    state = vm.runtime.powerState
    require(state in ("poweredOn", "poweredOff"), "the VM has no actionable power state")
    return "On" if state == "poweredOn" else "Off"


def off(vm):
    require(power(vm) == "Off", "the Machine must be stopped before this operation", "machine-running")


def config_change(session, vm, changes=(), settings=None, revision=None, boot=None):
    config = session.guard(vm)
    require(config and config.changeVersion, "the VM configuration has no concurrency token")
    require(revision is None or config.changeVersion == revision, "the VM configuration changed after the hardware proof")
    spec = session.vim.vm.ConfigSpec(changeVersion=revision or config.changeVersion, deviceChange=list(changes))
    if boot is not None:
        spec.bootOptions = boot
    if settings:
        spec.extraConfig = [session.vim.option.OptionValue(key=k, value=v) for k, v in settings.items()]
    session.wait(vm.ReconfigVM_Task(spec=spec))


def placement(session, req):
    vim = session.vim
    dc = session.lookup(req["datacenter"], vim.Datacenter)
    cluster = session.lookup(req["computeCluster"], vim.ClusterComputeResource)
    pool = session.lookup(req["resourcePool"], vim.ResourcePool) if req.get("resourcePool") else cluster.resourcePool
    folder = session.lookup(req["folder"], vim.Folder)
    store = session.lookup(req["datastore"], vim.Datastore)
    require(pool.owner == cluster and store in cluster.datastore and store in dc.datastore
            and descendant(cluster, dc.hostFolder) and descendant(folder, dc.vmFolder),
            "the folder, resource pool, cluster and datastore must belong to the selected placement")
    return dc, cluster, pool, folder, store


def descendant(item, ancestor):
    for _depth in range(128):
        if item == ancestor:
            return True
        if item is None:
            break
        item = item.parent
    return False
