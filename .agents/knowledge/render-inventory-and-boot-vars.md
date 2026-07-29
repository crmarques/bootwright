# Inventory group taxonomy and boot/provider render vars

**Inventory group taxonomy (ADR-0002).** `internal/render/inventory` places
hosts by role, not by machine: profile-backed substrate hosts land in
`bootwright_infra_hosts` (libvirt uses its provider host; KubeVirt/vSphere use
localhost because VM ops run through a kubeconfig or the vCenter API;
bare-metal is reached through its BMC); provider-setup/BMC-service hosts land
in `bootwright_provider_hosts`; managed InfraComponent service hosts (LB, DNS,
proxy, registry, artifacts, NTP) land in `bootwright_infra_component_hosts`; a
host may live in several groups; OCP-install and agent-node layers run on
localhost. The groups are deliberate so the machine-infra playbook targets
`bootwright_infra_hosts` directly instead of filtering hosts by `machineRef`
in its body.

**Controller-driven substrates render a local connection.** API-native KubeVirt
(kubeconfig), vSphere (vCenter API), and bare-metal-over-BMC (Redfish) run
machine tasks on the controller with no `Machine` object backing the ref. The
per-machine inventory entries still must be emitted with a local connection
rather than silently dropped, or the machine-task plays have no host to run on.
`MachineInventoryHosts` (`machine_inventory.go`) maps a `Machine` to its
post-provision inventory host names — the agent-node host per referencing
`ContainerCluster` and the storage-node host per referencing `StorageCluster`
topology host — which resolves a `CustomPlaybook` `target.machines` entry
to an ansible `--limit` token set; a machine referenced by no cluster
contributes none.

**boot_redfish is substrate-blind by contract.** `bootRole` names the
*protocol* the controller drives at install time, not the substrate: libvirt
and bare-metal machines both speak Redfish (libvirt through sushy-emulator,
bare-metal through a vendor BMC), so the renderer projects a substrate-blind
`boot.{redfish,agentIso}` tuple and `boot_redfish` consumes one shape for both,
never branching on `bmcRole` or looking up provider components at runtime. The
leak that prompted this was emulator-specific staging logic surfacing on the
bare-metal path, so `render_vars_boot_test.go` pins both substrate arms and
pins that `from.name` bare-metal machines carry no `bmcEmulated` block.
`bmcRole` keeps the substrate distinction only for the provider-host
`bmc_<role>` converger that stands up the endpoint.

**Emulated-BMC ports are renderer-owned.** The renderer is the single source of
truth for the emulated BMC's listen port, vmedia port, and bind address;
`boot.agentIso.fetchUrl` must use the same vmedia port the `bmc_emulated` role
stands up. Without the pinned projection, drift between the Go value and the
role's `default(8000)`-style fallbacks would surface only at apply time as a
"BMC reaches a different port than agentIso.fetchUrl" mismatch.

**vSphere boot/vars quirks.** The vSphere ISO is staged on the controller and
uploaded to the staging datastore, so `stageHost` is localhost and `fetchUrl`
is the datastore attach path `"[datastore] folder/<token>/<iso>"`, not an HTTP
URL — vSphere machines never join `agentIsoPublishTargets` (the publish/probe
contract requires an HTTP URL). Values authored in install-config inventory-path
form (`/dc1/host/cluster1`) are reduced to the object *name* for
`community.vmware` name-resolving parameters (computeCluster, datastore,
resourcePool) because vSphere object names cannot contain `/`; folders are
excluded (they resolve by path). vCenter accepts manually-assigned NIC MACs
only inside `00:50:56:00:00:00`–`00:50:56:3f:ff:ff`, so `vsphereMACAddress`
masks the first hashed byte into that range. The vSphere adapter feeds
`machineProfile` cpu/memoryMiB/diskGiB straight into `vmware_guest` and a
cloned template inherits nothing from omitted values, so validation requires
all three explicitly — omitting them renders an unbootable 0-cpu/0-memory/0-disk
VM that vCenter rejects deep in apply.

**Post-install media cleanup dispatches on `cleanupMediaRole`.** The
`wait_install.yml` and managed-OS `wait.yml` plays never enumerate boot
backends: the renderer sets `cleanupMediaRole` only for boot roles that own a
`cleanup_media` action (Redfish, vSphere) and leaves it unset otherwise
(KubeVirt deletes its agent-ISO DataVolume during boot; `none` is a no-op).
Adding a media-bearing boot backend is a renderer registry entry, not a new
branch in the play. The boot-component fact that gates cleanup exists only when
the run actually installed the machine — an already-ready machine attached no
media, and the include must not template an undefined var.

**Clients-mirror overrides stay renderer-owned.** `OpenShiftClientsMirrorBase`
is the canonical upstream base for oc/kubectl/openshift-install; an
`Environment` `defaults.clientsMirror` override replaces it (disconnected labs)
and is honored in both the `openshift-install` `ComponentPin` source and
`OpenShiftClientsReleaseURL`, so the bill of materials records the URL the
controller actually fetches from. `virtctl` has no upstream default base:
when `defaults.virtctlMirror` is empty the `controller_virtctl` role fetches
the version-matched binary from each KubeVirt host cluster's OpenShift
Virtualization `ConsoleCLIDownload`. `helm` takes the same renderer-owned
shape through `defaults.helmMirror`, but it does not track an OpenShift
release, so `HelmReleaseURL` appends the mirror's `latest` channel instead of a
version and the role verifies the binary against that channel's
`sha256sum.txt`. That is why `helm` gets no `ComponentPin`: a lock entry whose
version reads `latest` would record a version the run never resolved. The
`controller_openshift_tools` tmp working directory is therefore created
unconditionally — the helm checksum manifest is fetched on every run, even when
the release-pinned CLIs are already current.
