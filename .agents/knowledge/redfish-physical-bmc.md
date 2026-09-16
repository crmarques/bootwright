# Physical Redfish BMCs: what firmware actually does

Observed against real vendor controllers (Huawei/xFusion iBMC, iDRAC-style,
OpenBMC variants) during the pre-rewrite implementation of the bare-metal boot
flow, and re-read from that branch when
[substrates](../../specs/substrates.md#adapter-boundary) was written. The spec
owns required behavior; this page records the firmware behavior that shaped it,
so an implementation does not rediscover each item by failing against hardware.

The emulated controller M1h uses is one more shape in this list, not the
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
scheme, host and path while dropping a default port — so compare those three
parts rather than the whole string.

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
before the write and send the tag it returned; `*` is a last resort.

`VerifyCertificate` on a VirtualMedia member is read-only on some firmware,
which answers 501 (iBMC), 400 or 405. Tolerate exactly those. Do **not**
tolerate 401 and 403: they mean the account authenticated but its BMC role
lacks the privilege the write needs, which on iBMC the fixed Operator and
Common User roles do not hold. Surfacing that distinctly is the difference
between "this controller cannot do it" and "use an Administrator account".

When the per-resource property is refused, the manager's
`SecurityService.HttpsTransferCertVerification` is the equivalent control on
iBMC and is writable by an administrator.

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

`TransferProtocolType` must match the scheme of the image URL, and iBMC
rejects an `HTTP` value outright before it creates the insert task; with
Bootwright's HTTPS artifact endpoint the value is `HTTPS` and the BMC must
either trust that certificate or have verification disabled for the fetch.

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
