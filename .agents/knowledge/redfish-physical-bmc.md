# Physical Redfish BMCs: what firmware actually does

The xFusion iBMC facts here are captured: the owner's iBMC captures of
2026-05-22, from a 2288H V7 at iBMC 3.08.05.85, are archived outside the
repository, and each fact names the capture it comes from;
[the xFusion iBMC as captured](#the-xfusion-ibmc-as-captured) gathers them. The
iDRAC-style and OpenBMC facts are still recalled from the pre-rewrite
implementation of the bare-metal boot flow, re-read from that branch when
[substrates](../../specs/substrates.md#adapter-boundary) was written. The spec
owns required behavior; this page records the firmware behavior that shaped it,
so an implementation does not rediscover each item by failing against hardware.

The emulated controller [B72](../../specs/milestones/delivered.md#x38--the-real-host-run-of-2026-10-05) uses is one more shape in this list, not the
reference shape. Anything written against it alone will meet at least three of
the divergences below on the first real server.

## Where virtual media lives differs per vendor

`sushy-tools` exposes VirtualMedia under the **system**:
`/redfish/v1/Systems/<id>/VirtualMedia/Cd`. Other firmware exposes it only
under the **manager** reached through the system's `Links.ManagedBy`, and
iDRAC-style controllers expose the *same* resources under both, so a client that
collects from both views must de-duplicate on the resolved URL or it probes and
acts twice.

The captured xFusion iBMC is a third case. Its system declares `VirtualMedia`,
and `/redfish/v1/Systems/1/VirtualMedia` and `/redfish/v1/Managers/1/VirtualMedia`
both answer 200, each listing CD, USBStick and iBMAUSBStick under its own URLs:
two URLs present one device (the owner's iBMC capture of 2026-05-22 15:55Z, both
VirtualMedia collections and all six members). URL de-duplication cannot merge
them, and the client takes the first optical candidate in sorted order, which
is the manager's CD.

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

`InsertMedia` is worse: the captured iBMC answers 202 with a Task body and a
`Location` of `/redfish/v1/TaskService/Tasks/<n>/Monitor`, and the Task resource
is the only place that reports the outcome. Its task reads `Running`, then
`TaskState` `Exception` with `TaskStatus` `Warning` and `Messages` as **one
object**, not a list: `iBMC.1.0.ConnectionFailed` when the BMC could not fetch
the image (the owner's iBMC capture of 2026-05-22 15:09Z, insert task poll 2),
`iBMC.1.0.ConnectionOccupied` when media was still connected (the capture of
15:55Z, insert task poll 2). The client reads either shape. A completed insert
task was never captured. Normalize `/TaskService/TaskMonitors/<id>` and a
trailing `/Monitor` to the Task resource, poll to a terminal state, and then
confirm against the VirtualMedia resource itself.

The read-back must allow for the echo. The captured iBMC reports a mounted
image **without its non-default port**: an image requested at
https://address:8443/name.iso reads back as https://address/name.iso (the
capture of 15:55Z, the device's first read). So the client compares scheme,
host without case, path and query, leaves the port uncompared when the echo
names none, and refuses an echo that names a port other than the requested one
(or the scheme's default when the request names none).

An eject answers 202 with a task too. The device read in the same second still
presented the image, an insert sent then ended in `ConnectionOccupied`, and the
next read found the device empty (the capture of 15:55Z, eject before insert,
insert task poll 2 and the device read after it). So the client releases other
media and proves the device empty before the first attach, rather than spend an
attempt on an insert over connected media.

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
poll. On the captured iBMC the `Once`/`Cd` PATCH answered 200 with the system
body already reporting `Once`/`Cd`, and the next read reported it, so the
read-back was satisfied on its first read (the capture of 15:55Z, the boot
override and the system read after it). That system reports
`BootSourceOverrideMode` `UEFI` and advertises no
`BootSourceOverrideEnabled@Redfish.AllowableValues`. Its system and device
answer with `ETag` headers; whether its PATCH needs `If-Match` was not
captured.

`VerifyCertificate` on a VirtualMedia member is read-only on some firmware,
which answers 501, 400 or 405. The captured iBMC answers its PATCH 501
`iBMC.1.0.PropertyModificationNotSupported` (the capture of 15:55Z, the
VerifyCertificate PATCH). Bootwright's client tolerates exactly those, and only
when it **restores** verification after an eject: a controller that cannot
write the property leaves nothing to restore. Importing a certificate and
disabling verification tolerate none of them, because the insert that follows
would fetch under a trust nobody declared (see the next section). The client
ends any write at once on 401 and 403, which mean the account authenticated
but lacks the privilege.

The captured iBMC does **not** report a missing privilege that way. A PATCH of
the manager's `HttpsTransferCertVerification` from an account without the
privilege answered **400** `iBMC.1.0.PropertyModificationNeedPrivilege`, not
401 or 403 (the capture of 15:09Z, the HTTPS transfer verification PATCH). The
client's restore reads a 400 as a read-only property, which is harmless there
because the device property answers 501 and the restore never writes the
manager; but [B319](../../specs/milestones/m4.md#b319), which takes the
manager's import, must read that 400 as a privilege refusal.

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

On the captured iBMC neither CD view links `Certificates`, so
`import-certificate` refuses there before any write; and `disable-verification`
writes nothing, because `VerifyCertificate` already reads `false` and the
manager's switch is what decides. Whether iDRAC-style or OpenBMC controllers
expose `Certificates` on their device, accept a single-certificate `PEM`, and
apply `VerifyCertificate` to the next insert rather than only to a new session
is still unknown.

Under `established`, an insert carrying private material reads, writing
nothing, the device's `VerifyCertificate` and the `HttpsTransferCertVerification`
of every security service a manager links, and refuses unless the device reads
`true` and every service that reports the setting reads `true`. On the captured
iBMC both read `false`, so the operator first turns HTTPS transfer verification
on and imports the artifact server's CA out of band, through the iBMC, with an
account holding the Security Configuration privilege.

**UNVERIFIED premise, with its check.** Whether the device's
`VerifyCertificate` reads `true` once `HttpsTransferCertVerification` is on was
never captured. After enabling it, GET
`/redfish/v1/Managers/1/VirtualMedia/CD` and record `VerifyCertificate`. If it
stays `false`, every private delivery to this firmware refuses, and the
decision behind the read-back must be revisited before the real-hardware run.

## The xFusion iBMC as captured

The owner's iBMC captures of 2026-05-22 come from an xFusion 2288H V7 at iBMC
3.08.05.85 (the capture of 15:55Z, the system and manager reads). They hold no
completed install, so they settle shapes, not an end-to-end run.

- **Media actions.** The CD device, on both views, advertises the standard
  `InsertMedia` and `EjectMedia`, each with an ActionInfo, beside an OEM
  `VmmControl`, which is never used because the standard action wins.
  `InsertMediaActionInfo` declares `TransferProtocolType` (required; `Nfs`,
  `Cifs`, `https`, `NFS`, `CIFS`, `HTTPS`), `Image`, `Password` and `UserName`;
  `EjectMediaActionInfo` declares none. The only insert body any capture shows
  accepted is `{Image, Inserted, TransferProtocolType}`, which is exactly what
  the client sends.
- **Power.** `ComputerSystem.Reset` advertises `On` (no `ForceOn`),
  `ForceOff`, `GracefulShutdown`, `ForceRestart`, `Nmi`, `ForcePowerCycle` and
  `PowerCycle` (its ActionInfo lists the same without `PowerCycle`), and
  answers 200 with an error-shaped body whose `MessageId` is
  `Base.1.0.Success`. The system reached `On` on the second power poll.
- **References.** Every reference in every captured body is relative.
- **Trust store.** The manager links its `SecurityService` only under
  `Oem.<vendor>`. It carries `HttpsTransferCertVerification`, `false` in every
  capture, and the actions `ImportRemoteHttpsServerRootCA` and
  `DeleteRemoteHttpsServerRootCA`; the import is
  [B319](../../specs/milestones/m4.md#b319)'s.

**Reachability is the BMC's.** The BMC fetches the image itself, over its own
network. The host-name image URL failed with `iBMC.1.0.ConnectionFailed` (the
capture of 15:09Z, insert task poll 2) while the IP image URL, under the same
trust settings, was presented as mounted by the capture of 15:55Z. Prefer an IP
endpoint for the BMC's fetch, and check route, DNS and firewall from the BMC
network, not from the controller.

**Serving-certificate premise.** The iBMC has fetched only from an RSA-2048
self-signed server (the capture of 15:55Z, the artifact endpoint's TLS probe),
while Bootwright's generated serving certificates are P-256
([secrets](../../specs/secrets.md)). The iBMC's own HTTPS server enables
ECDHE-ECDSA suites (its security service's `SSLCipherSuites`), but its
virtual-media ClientHello was never captured, so whether it accepts a P-256
server is unproved.

- The check: during one manual `InsertMedia` by IP, from this build's nginx
  serving the generated certificate, capture the ClientHello on the controller
  (for example a packet capture on the HTTPS listener port, filtered to the
  BMC's address) or observe whether the image mounts.
- The workaround, if it does not: declare the artifact server's
  `tlsCertificate` Secret as `contextStore` and store an RSA-2048 certificate
  with `bootwright secret set --context <context> --name <secret>
  --certificate-file <crt> --private-key-file <key>`. The certificate must be
  self-signed and not a CA (`basicConstraints` `CA:FALSE`), carry `serverAuth`
  and `digitalSignature` or `keyEncipherment`, and name the IP endpoint and
  the DNS alias as subject alternative names, which
  [the material check](../../internal/secrets/material/validation.go) and
  [the serving-certificate check](../../internal/infrastructureservices/artifactserver/tls.go)
  require. A plain `openssl req -x509` with a distribution's default
  configuration marks the certificate `CA:TRUE` (observed with OpenSSL 3.5 on
  Fedora), so set the extensions explicitly. The
  [lab-baremetal README's](../../examples/lab-baremetal/README.md) invocation
  sets `basicConstraints`, `keyUsage` and `extendedKeyUsage` with `-addext`;
  its output passed the material check with OpenSSL 3.5.8 on Fedora, and it
  has not been run on a RHEL 9 host.
- A `keyType` for generation follows only if the check fails.

**The fetch record.** The artifact server's unit journal holds one line per
completed request: remote address, time, TLS protocol and cipher, method,
status and bytes, never the path. A handshake the BMC refuses is logged below
`warn`, where the error log stops, so it needs a packet capture. A failed
attach names these causes, BMC-side route or DNS, certificate trust and TLS,
and that journal.

**Still UNVERIFIED on this firmware:** the `EthernetInterfaces` inventory
([below](#proving-the-target-before-erasing-it)); whether `VerifyCertificate`
follows the manager's switch
([above](#virtual-media-certificate-trust)); the virtual-media ClientHello
against an ECDSA server; a completed insert task's shape; `Once`/`Hdd` under
UEFI; and whether a PATCH requires `If-Match`.

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

`TransferProtocolType` must match the scheme of the image URL. The captured
iBMC answers an http image URL with `TransferProtocolType` `HTTP` with 400
`Base.1.0.ActionParameterValueFormatError` before it creates any task (the
owner's iBMC capture of 2026-05-22 09:26, insert attempt 1); with Bootwright's
HTTPS artifact endpoint the value is `HTTPS` and the BMC must either trust that
certificate or have verification off for the fetch.

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
  was not recorded, so this rule is **not observed on hardware**. The captured
  iBMC never needs it, because it advertises the standard `EjectMedia`.

The reference used one fixed value, the iBMC certificate slot (`RootCertId` 8);
which slot the manager's import takes is
[B319](../../specs/milestones/m4.md#b319)'s to settle.

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
drive four shapes through urllib, the emulator, a manager-scoped, a dual-view
and a redacted xfusion shape built from the captures, and the
[discovery readings](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_redfish_discovery.py)
are tested as pure functions.

Its bounds: 30 s per request, 300 s per attach, 3 attach attempts 10 s apart,
60 task polls 2 s apart, 24 media probes 5 s apart, 60 power polls 2 s apart
and 12 boot read-backs 5 s apart. How long an installation waits for the
installer, the identity channel and the fleet account are the managed-OS
budgets in
[catalog.go](../../internal/managedos/installation/catalog.go), not the
client's.

## Proving the target before erasing it

The system's `EthernetInterfaces` collection is the live MAC inventory, and
comparing it with the Machine's declared NICs is what distinguishes the
intended server from another that answers at the same endpoint after a
re-cabling or a re-addressing. Treat anything less than a complete, readable,
non-empty collection as unknown: a member that 403s, 404s or returns no MAC
proves nothing, and `data-loss` authorization must never substitute for it.

The emulator implements this collection too (`/redfish/v1/Systems/<id>/EthernetInterfaces`,
from the domain's interface MACs), which is what makes the physical path
rehearsable without hardware. A refusal names each declared NIC the inventory
lacks, by its declared name and address, and each member that proved nothing,
by its position, never a value the controller reported.

**UNVERIFIED on the xFusion iBMC.** Its system links `EthernetInterfaces`, but
no capture reads the collection. The check: with the server powered off, GET
`/redfish/v1/Systems/1/EthernetInterfaces` and each member, and record whether
every member reports `MACAddress` or `PermanentMACAddress`. A member reporting
neither refuses every physical installation on that server.

The controller-side proof still closes before the machine boots. The Kickstart
therefore repeats it in a fail-closed `%pre` on the booted host, and the
remaining interval is recorded as a
[residual race](../../specs/state-reconciliation.md#mutation-safety) rather than
described as closed — the same limitation
[openshift-agent-disk-safety.md](openshift-agent-disk-safety.md) records for the
agent installer, which admits no equivalent hook at all.
