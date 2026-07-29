# ADR 0011: Redfish BMC Virtual-Media Boot Flow

## Status

Accepted

## Context

Bare-metal machines boot their install media — the OpenShift agent ISO or the
managed-OS Anaconda ISO — over Redfish BMC virtual media, driven from the
controller by `bootwright.core.container_cluster_boot_redfish`. The same role
serves real vendor BMCs (Huawei/xFusion iBMC, iDRAC-style controllers,
OpenBMC variants) and the lab sushy-tools emulator.

Firmware diverges on almost every step: which ComputerSystem to boot, where
VirtualMedia members live, which attach action works, whether properties such
as `VerifyCertificate` are writable, which trust-store mechanism exists,
whether `InsertMedia` is synchronous, and when `BootSourceOverrideEnabled=Once`
is consumed. Early optimistic implementations produced silent wrong outcomes:
`Reset(On)` returning 2xx while the host stayed down, a post-boot disk
override clobbering the pending one-time CD boot so the node booted its old
disk, vendor-specific action parameters breaking the very fetch they meant to
permit, and `no_log` hiding the BMC's own explanation of a refusal. Booting a
physical host is also irreversibly destructive: the first install disk-wipes
the machine.

## Decision

The Redfish boot flow is built on four rules.

**Discover capabilities before acting.** The role never assumes a firmware
shape. It resolves the target ComputerSystem (warning when a multi-system BMC
requires pinning via a `/redfish/v1/Systems/<id>` address suffix, rendered as
`systemId`), collects VirtualMedia members from both the Systems and Managers
views and de-duplicates them, discovers the working attach action (standard
`InsertMedia`, falling back to OEM `VmmControl` guided by
`@Redfish.ActionInfo`), and discovers the certificate trust-store mechanism —
the DMTF Certificates collection or the manager SecurityService OEM import
action. Discovered method names dispatch to per-method task files; adding a
vendor mechanism is a new sibling file plus data (for example the fixed iBMC
`RootCertId` slot), never a vendor branch in the shared flow.

**Poll state; never trust an accepted request.** A 2xx on
`ComputerSystem.Reset` is not evidence of power: the role polls `PowerState`
to the expected value within a bounded budget. Asynchronous `InsertMedia`
tasks are normalized to Task resources and polled to a terminal state, and the
attach is confirmed against the VirtualMedia resource (or accepted task
evidence). A retry must not censor the failure it is retrying, and refusals
hidden by `no_log` are re-surfaced through credential-safe asserts.
`PowerState=On` is not "the ISO booted": the
subsequent-boot disk override fires only behind an SSH readiness proof
(`readiness.type=ssh`) and only when Bootwright manages the boot source
(`setBootSource`).

**Trust is a two-leg model, restored on exit.** The controller→BMC leg
(`bmc.tls`) and the BMC→artifact-server leg (`bmc.virtualMedia.tls`) are
configured independently. For the second leg the role can import the artifact
server's live leaf certificate (read on the controller with `openssl
s_client`) into the discovered trust store, and/or best-effort disable
verification via the per-resource `VerifyCertificate` PATCH and the manager
`HttpsTransferCertVerification` PATCH — always out-of-band, never as an
`InsertMedia` action parameter. Original verification values are captured once
per boot, and the role's always-block removes the imported certificate (by its
recorded handle, 404-tolerant) and restores verification, warning loudly —
without failing or censoring — when a restore does not land.

**Stage media where the BMC can fetch it, behind safety gates.** Install ISOs
are built or staged directly into the publish root of the artifact server
endpoint selected for the BMC, with SELinux labels aligned to that root, and
the fetch URL is probed before `InsertMedia`. Because a first boot disk-wipes
the host, the flow is wrapped in identity and occupancy gates: BMC addresses
must be unique after normalizing equivalent Redfish spellings, first-install
clusters are named in a confirm-time disk-wipe warning, and a fail-open
occupancy probe refuses a first install onto a host whose BMC definitively
reports a running OS (`PowerState=On` + `BootProgress.LastState=OSRunning`).

## Consequences

- Vendor quirks land as data plus per-method task files; the shared flow stays
  vendor-free and the repo checks in `ansible_boot_redfish_test.go` pin the
  discovery/dispatch structure, canonical request bodies, and retry shape.
- Every boot spends extra Redfish GETs on discovery, polling, and ETag
  refresh, in exchange for eliminating silent wrong-state hand-offs.
- Failures carry the BMC's own message despite `no_log`, at the cost of the
  `failed_when: false` + assert pattern on each guarded write.
- The emulator and real BMCs share one role; emulator-only serialization is a
  renderer-projected flag (`vmediaColdInitRetry`), not a role branch.
- A failed restore can leave BMC certificate verification off; the flow
  surfaces a prominent warning rather than masking the original error, and the
  operator re-enables it manually.
- The firmware quirks and the retry/staging mechanics these rules exist for
  are cataloged in `.agents/knowledge/redfish-boot-flow-quirks.md`,
  `redfish-power-on-silent-noop.md`, `redfish-virtual-media.md`,
  `redfish-bmc-cert-import-mechanics.md`, `redfish-post-boot-disk-override.md`,
  `redfish-local-artifact-staging.md`, and `redfish-proxy-bypass.md`.
