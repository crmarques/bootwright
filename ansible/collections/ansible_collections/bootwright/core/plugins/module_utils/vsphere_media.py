"""Stage installation media on the selected datastore. Receipts live on the owned VM."""

from __future__ import annotations

import base64
import hashlib
import http.client
import json
import os
import re
import stat
import urllib.parse
import uuid

from ansible_collections.bootwright.core.plugins.module_utils import private_files, vsphere_session as api
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils.vsphere_hardware import connected, device_change, order_options

GUEST = ("guestinfo.metadata", "guestinfo.userdata")
MAX_IMAGE = 32 << 30


def payload_hash(value):
    return hashlib.sha256(value.encode("ascii")).hexdigest()


def seed_record(config):
    options = api.options(config)
    raw = options.get(api.SEED)
    if not raw or any(options.get(k) not in (None, "", "---", " ") for k in ("guestinfo.vendordata", "guestinfo.vendordata.encoding")):
        return None
    receipt = json.loads(raw)
    if receipt.get("vm") != config.instanceUuid or receipt.get("owner") != options.get(api.OWNER):
        return None
    for key in GUEST:
        value, encoding = options.get(key), options.get(key + ".encoding")
        if (value, encoding) == ("---", " "):
            continue
        if not isinstance(value, str) or encoding != "base64" or receipt.get(key) != payload_hash(value):
            return None
    return receipt


def checked_seed(vm):
    return seed_record(vm.config)


def seed_state(config, identity):
    options = api.options(config)
    if options.get(api.SEED):
        receipt = seed_record(config)
        return "own" if receipt and receipt["identity"] == identity else "foreign"
    return "foreign" if any(options.get(k) not in (None, "", "---", " ") for k in (*GUEST, "guestinfo.vendordata")) else "none"


def seed(session, vm, args, operation):
    identity = args.get("seed_identity")
    api.require(isinstance(identity, str) and 0 < len(identity) <= 256, "the cloud-init seed needs a stable identity")
    config = vm.config
    state = seed_state(config, identity)
    if operation == "seed_read":
        return {"seed_state": state, "power": api.power(vm)}
    api.require(state != "foreign", "a foreign cloud-init seed is already present")
    if operation == "seed_eject":
        if state == "own":
            clear_seed(session, vm)
        api.require(seed_state(vm.config, identity) == "none", "vCenter did not clear the private guestinfo seed")
        return {}
    api.off(vm)
    settings = {}
    receipt = {"identity": identity, "vm": config.instanceUuid, "owner": api.options(config)[api.OWNER]}
    for key, path in zip(GUEST, ("metadata_path", "userdata_path")):
        settings[key] = base64.b64encode(private_files.read(args[path], 512 << 10)).decode("ascii")
        settings[key + ".encoding"] = "base64"
        receipt[key] = payload_hash(settings[key])
    settings[api.SEED] = canonical(receipt).decode()
    options = api.options(config)
    if state == "none":
        api.config_change(session, vm, settings=settings, revision=config.changeVersion)
        options = api.options(vm.config)
    api.require(all(options.get(k) == v for k, v in settings.items()), "the installed guestinfo seed differs from the private seed")
    return {}


def clear_seed(session, vm):
    config = vm.config
    api.require(seed_record(config), "guestinfo seed custody is unproved")
    settings = {api.SEED: ""}
    for key in GUEST:
        settings[key], settings[key + ".encoding"] = "---", " "
    api.config_change(session, vm, settings=settings, revision=config.changeVersion)


def safe_path(path):
    api.require(isinstance(path, str) and 0 < len(path) <= 1024 and all(
        re.fullmatch(r"[A-Za-z0-9_. -]+", p) and p not in (".", "..") for p in path.split("/")),
        "invalid staging folder or file name")
    return path


def read_record(vm, volume):
    raw = api.options(vm.config).get(api.STAGE)
    if not raw:
        return None
    record = json.loads(raw)
    api.require(record["volume"] == volume and record["vm"] == vm.config.instanceUuid and record["owner"] == api.options(vm.config).get(api.OWNER),
                "staging receipt belongs to another placement or VM")
    folder = safe_path(volume["folder"])
    name = safe_path(volume["name"])
    reservation = record["reservation"]
    api.require(re.fullmatch(r"[a-f0-9]{32}", reservation) and "/" not in name, "invalid staging reservation")
    base = folder + "/" + name + "/" + reservation
    api.require(record["path"] == base + "/image.iso" and record["upload"] == base + "/image.upload",
                "the staged paths escape their original datastore reservation")
    api.require(re.fullmatch(r"[a-f0-9]{64}", record["sha256"]) and api.integer(record["size"]) and 0 < record["size"] <= MAX_IMAGE,
                "staging receipt lacks a bounded image identity")
    return record


def resolve(session, volume):
    dc = session.lookup(volume["datacenter"], session.vim.Datacenter)
    ds = session.lookup(volume["datastore"], session.vim.Datastore)
    api.require(ds in dc.datastore and ds.summary.accessible, "the staging datastore is inaccessible in the selected datacenter")
    # The transfer endpoint addresses a datastore by name. Prove that name
    # uniquely resolves to the selected MoRef in this datacenter.
    api.require([s for s in dc.datastore if s.name == ds.name] == [ds], "the staging datastore name is ambiguous")
    return dc, ds


def transfer(session, volume, ds, path, method, body=None, size=0, prefix=None):
    query = urllib.parse.urlencode({"dcPath": volume["datacenter"].lstrip("/"), "dsName": ds.name})
    url = "/folder/" + urllib.parse.quote(safe_path(path), safe="/") + "?" + query
    conn = http.client.HTTPSConnection(session.url.hostname, session.url.port or 443, timeout=60, context=session.tls)
    headers = {"Cookie": session.service._stub.cookie.split(";", 1)[0]}
    try:
        if method == "PUT":
            headers.update({"Content-Length": str(size), "Content-Type": "application/octet-stream"})
        conn.request(method, url, body=body, headers=headers)
        response = conn.getresponse()
        if response.status == 404 and method in ("GET", "HEAD"):
            return None
        api.require(response.status in (200, 201, 204), "the selected vCenter refused the datastore transfer")
        if method == "GET":
            return hash_stream(response, prefix)
        return True
    finally:
        conn.close()


def hash_stream(source, prefix=None):
    digest, size = hashlib.sha256(), 0
    while True:
        chunk = source.read(1 << 20)
        if not chunk:
            return size, digest.hexdigest()
        size += len(chunk)
        api.require(size <= MAX_IMAGE, "the staged image exceeds the transfer limit")
        if prefix is not None:
            api.require(prefix.read(len(chunk)) == chunk, "a partial upload differs from the owned image; staging is occupied")
        digest.update(chunk)


def datastore_path(ds, path):
    return "[%s] %s" % (ds.name, safe_path(path))


def drive(session, vm):
    devices = [d for d in vm.config.hardware.device if isinstance(d, session.vim.vm.device.VirtualCdrom)]
    api.require(len(devices) == 1, "the Machine must have exactly one CD-ROM")
    return devices[0]


def attachment(session, vm):
    backing = drive(session, vm).backing
    if isinstance(backing, session.vim.vm.device.VirtualCdrom.IsoBackingInfo):
        return backing.fileName, backing.datastore
    api.require(isinstance(backing, session.vim.vm.device.VirtualCdrom.RemotePassthroughBackingInfo),
                "a foreign CD-ROM backing occupies the machine")
    return "", None


def remove_file(session, dc, ds, path):
    session.wait(session.content.fileManager.DeleteDatastoreFile_Task(name=datastore_path(ds, path), datacenter=dc))


def save_record(session, vm, record, previous=None):
    config = session.guard(vm)
    prior = api.options(config).get(api.STAGE) or ""
    expect = canonical(previous).decode() if previous else ""
    api.require(prior == expect, "another staging operation changed the Machine receipt")
    api.config_change(session, vm, settings={api.STAGE: canonical(record).decode()}, revision=config.changeVersion)


def reserve(session, vm, volume, identity, size, digest, dc, ds, previous):
    folder = safe_path(volume["folder"]) + "/" + safe_path(volume["name"])
    nonce = uuid.uuid4().hex
    directory = folder + "/" + nonce
    record = {"reservation": nonce, "volume": volume, "vm": vm.config.instanceUuid, "owner": api.options(vm.config)[api.OWNER],
              "identity": identity, "datastore": ds._moId, "path": directory + "/image.iso", "upload": directory + "/image.upload",
              "sha256": digest, "size": size, "phase": "intent", "move": ""}
    # Persist the nonce before the first effect. An interrupted intent can
    # never authorize access to an existing directory. Retry with a new nonce.
    save_record(session, vm, record, previous)
    manager = session.content.fileManager
    try:
        manager.MakeDirectory(name=datastore_path(ds, folder), datacenter=dc, createParentDirectories=True)
    except session.vim.fault.FileAlreadyExists:
        pass
    manager.MakeDirectory(name=datastore_path(ds, directory), datacenter=dc, createParentDirectories=False)
    reserved = dict(record, phase="reserved")
    save_record(session, vm, reserved, record)
    return reserved


def move_succeeded(session, record):
    ref = record["move"]
    api.require(isinstance(ref, str) and re.fullmatch(r"task-[0-9]+", ref), "the staged ISO lacks a native move task receipt")
    session.wait(session.vim.Task(ref, session.service._stub))


def upload_and_publish(session, vm, volume, record, image, dc, ds):
    expected = record["size"], record["sha256"]
    # Even identical bytes left by a third party carry no ownership. Only a
    # successful native no-replace Move task can authorize the final file.
    api.require(transfer(session, volume, ds, record["path"], "HEAD") is None,
                "the final ISO path is occupied without a successful recorded move")
    image.seek(0)
    partial = transfer(session, volume, ds, record["upload"], "GET", prefix=image)
    if record["phase"] == "reserved":
        api.require(partial is None, "the newly reserved upload path is occupied")
        uploading = dict(record, phase="uploading")
        save_record(session, vm, uploading, record)
        record = uploading
    if partial != expected:
        if partial is not None:
            remove_file(session, dc, ds, record["upload"])
        image.seek(0)
        session.guard(vm)
        api.off(vm)
        transfer(session, volume, ds, record["upload"], "PUT", image, expected[0])
    api.require(transfer(session, volume, ds, record["upload"], "GET") == expected, "the uploaded ISO differs from the source")
    session.guard(vm)
    task = session.content.fileManager.MoveDatastoreFile_Task(
        sourceName=datastore_path(ds, record["upload"]), sourceDatacenter=dc, destinationName=datastore_path(ds, record["path"]),
        destinationDatacenter=dc, force=False)
    moving = dict(record, phase="moving", move=task._moId)
    save_record(session, vm, moving, record)
    return moving


def stage(session, vm, volume, identity, image, size, digest, dc, ds):
    record = read_record(vm, volume)
    if record:
        api.require((record["size"], record["sha256"], record["identity"], record["datastore"]) == (size, digest, identity, ds._moId),
                    "a different staging receipt occupies this Machine")
    if not record or record["phase"] == "intent":
        record = reserve(session, vm, volume, identity, size, digest, dc, ds, record)
    if record["phase"] in ("reserved", "uploading"):
        record = upload_and_publish(session, vm, volume, record, image, dc, ds)
    if record["phase"] == "moving":
        move_succeeded(session, record)
        published = dict(record, phase="published")
        save_record(session, vm, published, record)
        record = published
    api.require(record["phase"] == "published" and transfer(session, volume, ds, record["path"], "GET") == (size, digest),
                "the published ISO differs from its confirmed native move receipt")
    return record


def insert(session, vm, args, volume):
    api.off(vm)
    dc, ds = resolve(session, volume)
    record = read_record(vm, volume)
    path, backing = attachment(session, vm)
    api.require(not path or record and (path, backing) == (datastore_path(ds, record["path"]), ds),
                "a foreign CD-ROM image is attached")
    identity = args.get("media_identity")
    api.require(isinstance(identity, str) and 0 < len(identity) <= 512, "the pushed ISO needs a stable media identity")
    with os.fdopen(os.open(args["image_path"], private_files.OPEN_READ), "rb") as image:
        info = os.fstat(image.fileno())
        api.require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= MAX_IMAGE, "the ISO source is not a bounded regular file")
        size, digest = hash_stream(image)
        api.require(size == info.st_size, "the ISO changed during staging")
        record = stage(session, vm, volume, identity, image, size, digest, dc, ds)
    config = session.guard(vm)
    api.off(vm)
    current, backing = attachment(session, vm)
    api.require(not current or (current, backing) == (datastore_path(ds, record["path"]), ds), "a foreign CD-ROM appeared during staging")
    cd = drive(session, vm)
    cd.backing = session.vim.vm.device.VirtualCdrom.IsoBackingInfo(fileName=datastore_path(ds, record["path"]), datastore=ds)
    cd.connectable = connected(session.vim)
    api.config_change(session, vm, [device_change(session.vim, cd, "edit")], revision=config.changeVersion)
    api.require(attachment(session, vm) == (datastore_path(ds, record["path"]), ds), "vCenter did not attach the owned ISO")


def eject(session, vm, volume):
    config = session.guard(vm)
    path, backing = attachment(session, vm)
    record = read_record(vm, volume)
    if not record:
        api.require(not path, "the attached file has no staging receipt")
        return
    dc, ds = resolve(session, volume)
    api.require(record["datastore"] == ds._moId, "the staging datastore identity changed")
    if path:
        api.require((path, backing) == (datastore_path(ds, record["path"]), ds), "the attached image is foreign")
        cd = drive(session, vm)
        cd.backing = session.vim.vm.device.VirtualCdrom.RemotePassthroughBackingInfo(deviceName="", useAutoDetect=False)
        cd.connectable = connected(session.vim, False)
        disks = json.loads(api.options(config)["bootwright.disks"])
        root = next(r for r in disks if r["name"] == "root")
        api.config_change(session, vm, [device_change(session.vim, cd, "edit")], revision=config.changeVersion,
                          settings={"bootwright.bootISO": ""}, boot=order_options(session.vim, root["key"]))
    if record["phase"] == "moving":
        move_succeeded(session, record)
    for key in ("path", "upload"):
        existing = transfer(session, volume, ds, record[key], "GET")
        if existing is not None:
            phases = ("published", "moving") if key == "path" else ("uploading",)
            api.require(record["phase"] in phases and existing == (record["size"], record["sha256"]),
                        "a staged file differs from its ownership receipt; deletion refused")
            session.guard(vm)
            remove_file(session, dc, ds, record[key])
        api.require(transfer(session, volume, ds, record[key], "HEAD") is None, "the staged file was not removed")
    prove_empty(session, ds, volume)
    config = session.guard(vm)
    api.require(api.options(config).get(api.STAGE) == canonical(record).decode(), "the staging receipt changed during eject")
    api.config_change(session, vm, settings={api.STAGE: ""}, revision=config.changeVersion)


def prove_empty(session, ds, volume):
    folder = safe_path(volume["folder"]) + "/" + safe_path(volume["name"])
    try:
        result = session.wait(ds.browser.SearchDatastoreSubFolders_Task(datastorePath=datastore_path(ds, folder),
                              searchSpec=session.vim.host.DatastoreBrowser.SearchSpec()))
    except session.vim.fault.FileNotFound:
        return
    # FileManager deletes directories recursively. Retain empty reservations
    # and inspect every directory for residual files without deleting by name.
    api.require(result and all(isinstance(entry, session.vim.host.DatastoreBrowser.FolderInfo)
                for listing in result for entry in listing.file), "the Machine staging directory still contains a file")
