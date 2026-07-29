# Redfish virtual media quirks

**Symptom:** Redfish boot fails with `Base.1.0.GeneralError`, connection refused during `InsertMedia`, HTTPS errors against the local emulator or artifact server such as `ssl.SSLError`, the VM boots only from an empty disk after media insertion, managed OS install appears stuck waiting for SSH after virtual-media restore tasks, an install reboots into the agent ISO again after writing the disk, or lab-emulated Redfish leaves multi-GB ISO-shaped `tmp*` files in `/tmp`.

## Root causes

**Real BMC fetch:** real BMCs fetch remote ISOs by URL and may require HTTPS,
byte-range requests, `TransferProtocolType` matching the ISO URL scheme, a
minimal `InsertMedia` body, the manager-scoped virtual media URI, or
virtual-media certificate verification disabled for a self-signed artifact
endpoint. Bootwright's managed artifact publisher is HTTPS; using an `http://`
ISO URL against that service can reset the connection and make xFusion/iBMC
reject `TransferProtocolType=HTTP` before it creates an InsertMedia task.

**OEM/iBMC attach:** some xFusion/iBMC resources advertise standard
`#VirtualMedia.InsertMedia` but expose the working attach control as nested
`Oem.xFusion.Actions.#VirtualMedia.VmmControl`; the standard task can report
`iBMC.1.0.ConnectionFailed`. Some BMCs expose CD/DVD attach through the standard
manager-scoped `#VirtualMedia.InsertMedia` action and/or an OEM
`#VirtualMedia.VmmControl` action whose body shape must be confirmed with
`@Redfish.ActionInfo` before use. Some BMCs reject Redfish PATCH requests with
HTTP 412 unless the request uses the current resource ETag as its `If-Match`
precondition. Some BMCs return 501 for per-VirtualMedia `VerifyCertificate` but
expose manager `SecurityService.HttpsTransferCertVerification`; when it remains
enabled, self-signed HTTPS artifact endpoints can fail as
`iBMC.1.0.ConnectionFailed`.

**Async InsertMedia evidence:** some services return a TaskMonitor URL for
asynchronous `InsertMedia`; the Task resource may be the only place that reports
either `TaskState=Exception` and `MessageId=ConnectionFailed` or a successful
virtual-media mount when the VirtualMedia resource does not immediately reflect
the attachment. A retry loop that schedules included Ansible tasks ahead of the
fact set inside a successful attempt can eject a successfully mounted ISO and
overwrite the accepted task evidence with a later failed task. Some BMCs
normalize the reported virtual-media `Image` after a successful mount, for
example preserving scheme, host, and path while omitting a non-default port.

**Reachability is the BMC's, not the workstation's:** a URL that downloads
successfully from an operator workstation can still fail virtual media insertion
if the artifact server only supports full-file GETs, if the BMC cannot resolve
and reach the route itself, if the containerized static-file worker cannot
traverse the root-owned token directory and ISO file, if a hard-linked staged ISO
retains a non-container SELinux label, or if firmware aborts a TLS stream in a way
that Python's default `http.server` reports as `ssl.SSLError`.

**sushy-tools emulation:** lab-emulated Redfish exposes virtual media under the
ComputerSystem resource, fetches the `InsertMedia` image from a local URL into
Python's default temp directory before uploading it into the configured libvirt
vmedia pool, rewrites libvirt domain XML for the CD-ROM, and ignores
`BootSourceOverrideEnabled=Once` in its libvirt backend.

**Managed-OS ordering:** the direct libvirt media helper needs the staged ISO
path after the per-machine publish token has replaced the placeholder, the
install boot order must be disk-first with CD fallback so a newly written disk
wins after Kickstart reboot, and the attach ISO must live in a provider-native
published media path instead of Bootwright's private provider-state tree.

## Fixes

**Artifact serving:** serve staged ISOs through a byte-range-capable HTTPS unit
with a generated self-signed certificate whose worker can read Bootwright's
root-owned staged publish tree, align the staged directory and ISO SELinux labels
with the publish root before probing, close firmware-facing artifact responses
explicitly, tolerate TLS peer disconnects while streaming, reject non-HTTPS
managed ISO URLs before InsertMedia, and probe the staged fetch URL plus a
one-byte range request from the artifact host after copying the ISO.

**Attach discovery and call:** drive Redfish calls with `ansible.builtin.uri`,
prefer manager-scoped virtual media when present, discover
`#VirtualMedia.VmmControl` from top-level and direct OEM `Actions` blocks, fetch
advertised `@Redfish.ActionInfo`, and use the standard advertised `InsertMedia`
action when present; fall back to the VMM action only when there is no standard
action and metadata accepts `Image` plus `VmmControlType`. Send the standard
`InsertMedia` action with only `Image`, `Inserted`, and `TransferProtocolType`,
best-effort disable `VerifyCertificate` for HTTPS media fetches with a freshly
fetched ETag in `If-Match`, and disable advertised manager
`HttpsTransferCertVerification` before remote HTTPS file fetches.

**Async evidence and retries:** normalize TaskMonitor URLs and
`/Tasks/<id>/Monitor` locations to Task resources before accepting asynchronous
`InsertMedia`, retry the full attach operation when a terminal task reports a
transient connection failure, apply the attachment guard to every included retry
task so later attempts cannot run after success, keep skipped Ansible probe tasks
from overwriting the latest real VirtualMedia probe, accept a completed OK
asynchronous `InsertMedia` task as attachment evidence when the resource does not
reflect the mount and does not report a conflicting inserted image, accept
BMC-normalized reported image URLs that still match the requested scheme, host,
and path, fall back to PATCH-style virtual media attach when a BMC accepts
standard `InsertMedia` but does not reflect it, quote `ResetType: "On"` because
YAML 1.1 treats bare `On` as a boolean, and keep virtual media attached.

**libvirt-backed emulated Redfish:** keep the OpenShift agent domain disk-first
with CD fallback, attach the already staged ISO directly through libvirt before
Redfish boot, mark the Redfish virtual-media insert step satisfied so Bootwright
does not make sushy download another ISO, set the sushy service temp environment
to the provider state tree as a fallback, and skip the Redfish CD boot override.

**Managed-OS install ordering:** resolve the tokenized boot component before
provider media preparation, build the install ISO directly into the rendered
published media path on the provider host, keep mkksiso temporary state in
private provider-state, restore SELinux labels for the published path, boot
libvirt install media disk-first with CD fallback, clear persistent libvirt media
config immediately after the installer boots while leaving live media attached
for Anaconda, let Kickstart reboot normally, wait for SSH after the reboot, then
detach the installer optical drive entirely so the provisioned guest is left with
no leftover `/dev/sr0`.

**Per-provider cdrom cleanup:** that final detach is per-provider. For libvirt,
detach the cdrom device (not just eject the medium) from both the live domain
(best-effort — a running guest may refuse to hot-unplug a SATA optical drive,
which then clears on the next reboot) and the persistent definition
(authoritative, must end with no cdrom). For vSphere, remove the CD device
(`state: absent`, falling back to a `type: none` disconnect when live removal is
rejected). For bare metal — which carries no `mediaPrepareRole`, so the
mediaPrepareRole cleanup is skipped — eject the BMC virtual media through the
boot role's `cleanup_media` action. KubeVirt is not a managed-OS Anaconda path:
its cdrom is the OpenShift agent-ISO DataVolume that `boot_kubevirt` deletes.

If a real BMC still reports `Inserted=False`, verify reachability from the BMC
network, not only from the controller or operator workstation.
