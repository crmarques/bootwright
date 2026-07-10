# Bare-metal first-install and BMC-identity safety guards

Guards that keep a first apply from disk-wiping the wrong physical host. The
general ownership/override model is in docs/advanced/ownership-and-safety.md;
this records the bare-metal-specific mechanisms.

**Confirm-time disk-wipe warning:** `BareMetalFirstInstallClusters`
(internal/converge/workflow/apply_baremetal_boot.go) names, at confirm time,
the clusters whose planned nodeBoot (Redfish virtual-media) task has no
convergence-safety record — the first-apply case whose boot drives
coreos-installer to disk-wipe a physical machine (see
cluster-install-record-gates.md for the warning text and exclusions). nodeBoot
tasks are emitted only for Redfish/bare-metal clusters; KubeVirt and vSphere
boot a VM, not a physical host.

**Pre-boot occupancy probe (fail-open):** for first-install clusters,
`container_cluster_boot_redfish/tasks/validation/occupancy.yml` queries the
Redfish system before powering the host into the agent media, and refuses when
the BMC reports the host already running an OS. Best-effort and FAIL-OPEN: only
a definite `PowerState=On` plus `BootProgress.LastState=OSRunning` blocks; a
BMC that does not populate BootProgress (older Redfish or an OEM value), a
non-200 response, or any query error proceeds exactly as before. Owned clusters
(legitimate re-provisions) are never in the first-install list and never reach
this task.

**Duplicate BMC endpoints fail validation:** two Machines pointing at the same
BMC endpoint would drive — and could disk-wipe — the wrong physical host, so
`validateUniqueBMCAddresses` (internal/state/desired) fails closed. It
normalizes equivalent Redfish spellings before comparing: `redfish+https://`,
`redfish-virtualmedia+https://`, plain `redfish://` vs `https://`, and a
trailing `/redfish/v1/Systems/<id>` suffix all resolve to the same endpoint and
collide. Genuinely different hosts — distinct System IDs on the same
controller — still pass; the guard normalizes without over-collapsing. VM
machines with no BMC are ignored.

**BMC reachability preflight:** the external-reachability check
(`check_external_reachability/tasks/bmc.yml`) issues an authenticated GET
against `/redfish/v1/Systems` — the entry point every Redfish service must
expose — on the renderer-normalized base URL. A 200 with a JSON body proves
both reachability AND that the supplied credentials authenticate, which is what
the install layer needs. The renderer strips any Ironic-style transport hint
and trailing `/Systems/<id>` before projection (the suffix becomes the rendered
`systemId`), so no task parses BMC URLs. Failure here is fatal: the play aborts
before any infra convergence runs.

**Bare-metal substrate is record-keeping only:** `machine_substrate_baremetal`
performs no provisioning work on any provider machine — it records the
per-cluster manifest and validates the BMC `credentialsRef` exists locally.
All BMC interaction happens in `container_cluster_boot_redfish`, driven from
the controller during the install layer. The destroy side is local state
cleanup only (never contacts the host, never fails closed — documented in
docs/advanced/operations.md, "Managed bare-metal is torn down locally"); the
shared per-cluster `_source` ISO cache is removed with the cluster so the
source DVD is never left orphaned.
