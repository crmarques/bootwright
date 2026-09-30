# Physical Redfish BMCs: what firmware actually does

Observed against real vendor controllers (Huawei/xFusion iBMC, iDRAC-style,
OpenBMC variants) during the pre-rewrite implementation of the bare-metal boot
flow, and re-read from that branch when
[substrates](../../specs/substrates.md#adapter-boundary) was written. The spec
owns required behavior; this page records the firmware behavior that shaped it,
so an implementation does not rediscover each item by failing against hardware.

The emulated controller [B72](../../specs/milestones/m4.md#b72) uses is one more shape in this list, not the
reference shape. Anything written against it alone will meet at least three of
the divergences below on the first real server.

## Where virtual media lives differs per vendor

`sushy-tools` exposes VirtualMedia under the **system**:
`/redfish/v1/Systems/<id>/VirtualMedia/Cd`. iBMC 404s that path and exposes it
under the **manager** reached through the system's `Links.ManagedBy`. iDRAC-style
controllers expose the *same* resources under both, so a client that collects
from both views must de-duplicate on the resolved URL or it probes and acts
twice.

Discover, never assume: collect the system view and every manager view, union
them, prefer a member whose `MediaTypes` contains `CD` or `DVD`, and fall back
to an id ending in `Cd`, `CD`, `DVD` or a digit. A client hard-coded to `/Cd`
under the system works only on the emulator.

A view that cannot be read is not a view without media. Bootwright's client
requires a declared collection, a linked manager and every probed member to
answer 200 with an object. A view that declares no `VirtualMedia` is probed at
`<resource>/VirtualMedia`, and only 404, 501 or another 4xx except 401 and 403
there reads as none offered; 401, 403, any other 5xx, no answer or a body that
is not an object is unreadable and fails the read. The reason is the eject: a
client that reads an unreadable view as empty proves a release against a device
it never found, which is how an agent-install removal could resolve released
with media still attached.

## A request is not an outcome

`ComputerSystem.Reset` returning 204 means the request was accepted. The
emulator's libvirt backend implements `On` as `domain.create()` and reports
success whenever that call does not raise, so a guest whose QEMU exits
milliseconds later (BIOS-level boot failure, `on_crash=destroy`, an SELinux
denial on freshly inserted media) leaves the caller believing it booted. Real
firmware has the same property for a different reason: chassis power applies
long before anything reads a boot device.

Poll the resource to the state that was asked for, within a bound, and prefer
`ForceOn` when the system advertises it in `ResetType@Redfish.AllowableValues`.

`InsertMedia` is worse: some controllers answer 202 with a `TaskMonitor`
header, and the Task resource is the only place that reports either success or
`TaskState=Exception` with `MessageId=ConnectionFailed`. Normalize
`/TaskService/TaskMonitors/<id>` and a trailing `/Monitor` to the Task resource,
poll to a terminal state, and then confirm against the VirtualMedia resource
itself. Some controllers normalize the reported `Image` afterwards — preserving
scheme, host and path while dropping a default port — so compare those parts
rather than the whole string, reading an absent port as the scheme's default
(443 for `https`, 80 for `http`) and nothing else: Bootwright serves installer
images on 8443, and an echo naming no port does not prove one of them.

The task rule the client applies: 202, any 5xx and no answer mean the task is
still running, as does a 200 whose `TaskState` is not terminal (openstack/sushy
at ecddf50 also reads 202 as still processing, in sushy/taskmonitor.py lines
78-119; not re-checked here). `Completed` succeeds whatever `TaskStatus` says,
because a `Warning` reports a condition beside the outcome and the device read
back afterwards decides; `Exception`, `Killed` and `Cancelled` fail, naming the
state and the task's `MessageId`. `Interrupted` and `Suspended` are still
running: the [DMTF Task schema](https://redfish.dmtf.org/schemas/v1/Task.v1_7_4.json)
defines each as a task expected to restart, and failing on one would release a
device the task may still attach to and send a second attach beside it. Any
other answer, a 404 from a monitor that has gone included, leaves the outcome
to the device read-back. A task that never settles within the poll bound fails
the attempt.

## `PowerState=On` is not "the installer booted"

This one silently installs nothing. The BMC reports `On` the instant chassis
power applies, minutes before UEFI reads the pending one-time
`BootSourceOverrideEnabled=Once` / `Target=Cd` during POST. An iBMC that reads
boot configuration live during POST will see a `Continuous`/`Hdd` override
written seconds after power-on and boot the existing disk instead — the node
comes up on its old operating system and the run fails much later, at an
authentication or ownership check, with nothing pointing back here.

Never write a disk boot override until positive evidence proves the installer
actually ran. Bootwright's Anaconda path ends in `poweroff`, so observing the
machine reach `Off` is that evidence, and the eject and `Hdd` selection happen
after it.

## PATCH preconditions and read-only properties

Some controllers reject a PATCH with HTTP 412 unless the request carries the
resource's current `@odata.etag` as `If-Match`. Fetch the resource immediately
before the write and send the tag it returned; `*` is a last resort. The tag
may arrive only as an `ETag` response header, whose name a server spells as it
likes (openstack/sushy at ecddf50 reads it without regard to case, in
sushy/resources/base.py lines 643-648; not re-checked here), so the client
lower-cases every header name and prefers the header over the body's tag.

An accepted boot PATCH is not a selection either. The client reads the system
back until `BootSourceOverrideTarget` is the target and
`BootSourceOverrideEnabled` is `Once`, or `Continuous` on a controller that
does not list `Once` in `BootSourceOverrideEnabled@Redfish.AllowableValues`:
the emulator reports every override as `Continuous` and advertises nothing, but
one that offers `Once` and reports `Continuous` did not apply what was asked.
The bound, 12 reads 5 s apart, is borrowed from the reference's power-state
poll below and has never been observed for a boot selection.

`VerifyCertificate` on a VirtualMedia member is read-only on some firmware,
which answers 501 (iBMC), 400 or 405. Bootwright's client tolerates exactly
those, and only when it **restores** verification after an eject: a controller
that cannot write the property leaves nothing to restore. Importing a
certificate and disabling verification tolerate none of them, because the
insert that follows would fetch under a trust nobody declared (see the next
section). Do **not** tolerate 401 and 403 anywhere: they mean the account
authenticated but its BMC role lacks the privilege the write needs, which on
iBMC the fixed Operator and Common User roles do not hold. Surfacing that
distinctly is the difference between "this controller cannot do it" and "use an
Administrator account".

When the per-resource property is refused, the manager's
`SecurityService.HttpsTransferCertVerification` is the equivalent control on
iBMC and is writable by an administrator. Bootwright does not write it.

## Virtual-media certificate trust

What the schema says, from the
[DMTF VirtualMedia schema](https://github.com/DMTF/Redfish-Publications/blob/main/json-schema/VirtualMedia.v1_6_3.json)
(both properties added in v1_4_0): the read-only `Certificates` link names a
`CertificateCollection` whose members a service compares with the image
server's handshake certificate when `VerifyCertificate` is `true`, and it does
not complete the media connection when that fails. `VerifyCertificate` is
writable, "should default to `false`", and is assumed `false` when a service
does not support it. Either way a service may add checks of its own from its
`SecurityPolicy` resource, so a certificate the collection holds is necessary
but not always sufficient. The
[DMTF CertificateCollection schema](https://github.com/DMTF/Redfish-Publications/blob/main/json-schema/CertificateCollection.json)
publishes that collection only beneath `Systems/{id}/VirtualMedia/{id}` (and
the ResourceBlocks copies), never beneath a manager, so a manager-scoped device
such as iBMC's may offer no collection at all; the client follows the link the
member itself carries and builds no path. The
[DMTF Certificate schema](https://github.com/DMTF/Redfish-Publications/blob/main/json-schema/Certificate.json)
defines `CertificateType` `PEM` as a single certificate and `PEMchain` as a
chain; the client posts `PEM` with the server's own certificate, the first
block of what it was given, and compares members by DER.

The rules Bootwright's client applies, set once before the first attach and
settled after a proved eject:

- `import-certificate` reads the collection in full (at most 8 members, failing
  closed beyond that), posts the server's certificate unless its DER is
  already there, turns `VerifyCertificate` on unless it reads `true`, and reads
  the member back.
- `disable-verification` writes `VerifyCertificate` `false` only when it reads
  `true`; a refused write fails, because verification stays on.
- There is **no fallback**. A member without `Certificates`, or a
  `VerifyCertificate` write answered 501, 400 or 405, fails the import naming
  the two exceptions an operator may declare instead, and a post refused while
  another certificate is present names removing that stale certificate.
- Restoring after an eject writes `true` unless it reads `true`, whatever the
  attempt itself wrote, and removal deletes only the member that is the given
  certificate.

**UNVERIFIED on real firmware.** None of this has been driven against a
physical controller. Whether iBMC, iDRAC-style or OpenBMC controllers expose
`Certificates` on their device, accept a single-certificate `PEM`, and apply
`VerifyCertificate` to the next insert rather than only to a new session is
unknown; qualify it on the first physical controller before relying on the
default.

## `no_log` hides the BMC's own explanation

A task that protects its credential with `no_log` and asserts on `status_code`
raises only `Status code was 403` with the response body censored, so the
controller's own message — which usually names the exact privilege or
parameter — never reaches the operator. Let the call record its result, then
assert in a separate step that interpolates status, body and the resource path
but never the credential.

Equally: `until` on a `no_log` task force-fails with a fully censored result on
retry exhaustion.

## Proxy and TLS

Ansible's `uri` and Python's `urllib` consult the ambient proxy environment by
default, and their bypass implementations do not treat a CIDR `no_proxy` entry
as matching a concrete BMC address. A proxy's own 403 then looks exactly like a
BMC refusing the credential. A client that must not use a proxy installs an
empty proxy handler explicitly rather than trusting the environment to be clean.

A controller's references are its own to choose, and nothing stops one from
naming another host. Bootwright's client follows a returned reference — a
member, an action target, an action's `@Redfish.ActionInfo`, a task or a
`Location` header — only when its scheme, its host compared without case and
its port (the scheme's default when absent) are the endpoint's and it carries
no user information. Anything else is refused before a request is built, so the
Basic credential never leaves the endpoint; a task reference refused this way
is simply not polled, and the device read-back decides. This is the
[security](../../specs/security.md#network-remote-systems-and-privilege) rule
applied to Redfish, not an observed firmware behavior.

`TransferProtocolType` must match the scheme of the image URL, and iBMC
rejects an `HTTP` value outright before it creates the insert task; with
Bootwright's HTTPS artifact endpoint the value is `HTTPS` and the BMC must
either trust that certificate or have verification disabled for the fetch.

The controller's own certificate is verified against the Machine's declared
`caBundle` alone when it declares one: Python's `ssl.create_default_context`
loads the `cadata` it is given and never the system store beside it, keeping
`CERT_REQUIRED` and the host-name check
([CPython v3.13.15 ssl.py](https://github.com/python/cpython/blob/v3.13.15/Lib/ssl.py)).
From Python 3.13 that context also sets `VERIFY_X509_PARTIAL_CHAIN` and
`VERIFY_X509_STRICT`; v3.12.11 sets neither. The controller-local runtime is
3.13, but an SSH placement runs the host's own interpreter, which may predate
it and then anchors only at a self-signed root, so the bundle holds the issuing
root rather than an intermediate alone. A refused
certificate is an answer, not silence: the client raises it at once rather than
reading it as status 0 and spending a poll's attempts on it.

## Multi-system chassis

A blade chassis or some OpenBMC controllers expose more than one
ComputerSystem. Defaulting to the first member can boot — and erase — the wrong
server. Bootwright requires the Machine's BMC address to name one exact
`/redfish/v1/Systems/<id>`, which is why
[admission](../../specs/api/machines.md#os-lifecycle-and-substrate-invariants)
refuses a bare-metal install address that does not.

## How to stay vendor-neutral

The reference implementation supported iBMC, iDRAC-style and OpenBMC
controllers with no vendor branch anywhere, and the technique is worth keeping
exactly: **discover capabilities from the controller's own metadata, and let
the metadata decide**. A vendor name never appears in a condition.

- **Actions are collected from two places.** A resource's `Actions.<name>` is
  the standard location; `Oem.<vendor>.Actions.<name>` is where a vendor puts
  its own. Walking both, and recording which one an action came from, finds
  xFusion's `#VirtualMedia.VmmControl` without knowing that xFusion exists.
- **An OEM action is used only when its `@Redfish.ActionInfo` proves it fits.**
  The reference accepted `VmmControl` only when its ActionInfo declared both an
  `Image` and a `VmmControlType` parameter, and the latter's `AllowableValues`
  contained `Connect` and `Disconnect` (an absent `AllowableValues` is treated
  as permissive). So the capability is proved, not assumed.
- **Standard wins.** The OEM path is taken only when no standard
  `InsertMedia` action is advertised at all, so a controller that implements
  the specification is driven by the specification.
- **Power likewise.** `ResetType@Redfish.AllowableValues`, on the action or in
  its ActionInfo, selects `ForceOn` before `On` before `PushPowerButton`
  rather than sending a fixed value and hoping.
- **Media members are unioned and de-duplicated** across the system and every
  manager view, keyed on the resolved URL, then narrowed by `MediaTypes` or an
  id suffix.
- **Ejecting through the extension** sends `VmmControlType` `Disconnect` to
  the proved `VmmControl`, adding the presented `Image` only when its
  ActionInfo marks `Image` as `Required`. The reference's Disconnect payload
  was not recorded, so this rule is **not observed on hardware**: qualify it on
  the first physical controller that only offers the extension.

The one place a fixed value was unavoidable is the iBMC certificate slot
(`RootCertId` 8, because that firmware admits only 5 through 8). That is data
beside its own method file, not a branch in the shared flow, and the method
itself is chosen by discovery.

## Prove by observation, not by response status

Every mutating call in the reference accepts a wide status set and then
confirms by reading the resource back. Eject accepts 200, 202, 204, 400, 404,
409 and even 500, then polls the VirtualMedia member until `Inserted` is false,
treating a 404 as ejected because some controllers remove the resource
entirely. Insert accepts its own status, then polls the asynchronous task, then
confirms `Inserted` with a matching image. The status of a request is never the
evidence; the state of the resource is.

Bootwright's [client](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/redfish_control.py),
which [redfish_boot](../../ansible/collections/ansible_collections/bootwright/core/plugins/modules/redfish_boot.py),
[redfish_system_read](../../ansible/collections/ansible_collections/bootwright/core/plugins/modules/redfish_system_read.py)
and [redfish_system_inspect](../../ansible/collections/ansible_collections/bootwright/core/plugins/modules/redfish_system_inspect.py)
drive, keeps this with one exception: 401 and 403 end any write at once,
because no retry or read-back changes a missing privilege. An attach nothing
answered is read back before anything else; a failed attach is retried only
after the device is proved empty. Every poll, the power poll and each
read-back, treats an answer that is not a readable resource as not yet the
state, so only exhaustion fails it. Its
[tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/modules/test_redfish_boot.py)
drive the emulator, a manager-scoped and a dual-view shape through urllib, and
the [discovery readings](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_redfish_discovery.py)
are tested as pure functions.

Bounds it settled on: 60 s per request, 3 insert attempts 10 s apart, 60 task
polls 2 s apart, 24 media probes 5 s apart, 12 power-state polls 5 s apart, and
1800 s for a physical host to reach TCP/22 after power-on, because a production
server's POST is minutes long.

## Proving the target before erasing it

The system's `EthernetInterfaces` collection is the live MAC inventory, and
comparing it with the Machine's declared NICs is what distinguishes the
intended server from another that answers at the same endpoint after a
re-cabling or a re-addressing. Treat anything less than a complete, readable,
non-empty collection as unknown: a member that 403s, 404s or returns no MAC
proves nothing, and `data-loss` authorization must never substitute for it.

The emulator implements this collection too (`/redfish/v1/Systems/<id>/EthernetInterfaces`,
from the domain's interface MACs), which is what makes the physical path
rehearsable without hardware.

The controller-side proof still closes before the machine boots. The Kickstart
therefore repeats it in a fail-closed `%pre` on the booted host, and the
remaining interval is recorded as a
[residual race](../../specs/state-reconciliation.md#mutation-safety) rather than
described as closed — the same limitation
[openshift-agent-disk-safety.md](openshift-agent-disk-safety.md) records for the
agent installer, which admits no equivalent hook at all.
