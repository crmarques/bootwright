# Substrate ownership markers: apply guards, destroy verification

Substrate VM names (`<cluster>-<machine>`) carry no context component, so a
second Bootwright context or a foreign owner can hold a same-named object.
Every destructive touch verifies a live ownership marker first.

**Apply-side guard (no --authorize unowned-vms escape):** before the override reset
destroys/undefines a libvirt domain, or a plain apply redefines it, the apply
path requires the Bootwright ownership marker for THIS context/cluster/machine
in the domain XML (`machine_substrate_libvirt/tasks/machine.yml`; vSphere has
the identical guard on its override reset via the VM annotation in
`machine_substrate_vsphere/tasks/probe.yml`). An absent domain fails the probe
and skips the guard, so first apply still creates. Apply fails closed with NO
`--authorize unowned-vms` escape — that flag exists only on destroy.

**Destroy-side verification per substrate:** libvirt reads the domain-XML
marker; vSphere asserts the vCenter annotation (`bootwright:context=`,
`bootwright:cluster=`, `bootwright:machine=`) before "Delete vSphere VM";
KubeVirt re-verifies the `bootwright.io/managed-by` label on the
VirtualMachine and each DataVolume (the namespace may host foreign workloads).
Assert and delete are gated on object presence so destroy stays idempotent; a
missing/mismatched marker fails closed and `bootwright_destroy_authorize_unowned_vms`
is the explicit recovery relaxation (per-VM refusals only — never Ceph
ownership or device data-safety gates).

**vSphere identity recorded before rename:** the apply path records a
`vsphere-machine` ownership record carrying `vmName:`, `moid:`, `uuid:` and ISO
staging attributes (`isoDatastore:`, `isoFolder:`) at CREATE time; destroy
reads them back, so the VM and its uploaded media are found even after a
post-create rename or move (reconfigure falls back to live moid/uuid). Every
uploaded ISO gets a `vsphere-vmedia` record BEFORE attach so datastore media
never outlives the machine. The boot_manager module addresses the VM by moid —
it has no folder scoping and `name_match=first` could hit a same-named foreign
VM. vSphere uses disk-first boot order (a blank disk has no bootable EFI entry
so firmware falls through to the install CD; first post-install boot comes from
disk with media still attached — no mid-install eject).

**Libvirt context sweep never trusts disk paths:** a foreign VM can park a disk
under `/var/lib/libvirt/images/bootwright/<context>/`. The sweep
(`provider_host_libvirt/tasks/destroy_context.yml`) undefines only domains
whose XML carries the marker for this context; when a foreign domain co-resides
under the context root, the blanket root removal is skipped
(`bootwright_libvirt_context_foreign_storage` non-empty) and only the
Bootwright-owned per-machine subtrees (`virsh domblklist --details`-derived)
are removed, leaving orphan subtrees for a warned manual sweep. The no-foreign
case removes the whole root, which also reclaims orphaned disks. Every listed
domain's block-device and XML probes must succeed or prove the exact domain
absent, and the two observations must agree. Any unreadable or inconsistent
probe preserves all domains, disks, and ownership. Stop/undefine failures abort
before evidence deletion, and each selected domain must then be proven absent.
The per-machine destroy and managed-OS rebuild paths impose the same ordering:
domain removal must succeed or prove absence before disk, state, or record
deletion.

**Shared DVD cache must not join per-machine records:** the multi-GB source DVD
staged once per cluster (throttle: 1) must never appear in a per-machine
ownership record — the first machine's destroy would delete it mid-loop and
force every surviving machine to re-stage the full DVD after a partial
teardown. It is removed only once this machine's install dir is gone AND no
other machine's install dir remains. Same pattern in
machine_substrate_{libvirt,baremetal,vsphere}/tasks/destroy.yml.

**Managed-OS machine dispatch:** managed-OS machines live in the
`bootwright_managed_os_install_groups` inventory groups, not
`bootwright_clusters`, and API-native substrates (vSphere) are unreachable
through the recorded-resource sweep — `task_machine_infra_destroy.yml`
dispatches each machine's `bootwright_managed_os_component.substrateDestroyRole`
(`tasks_from: destroy.yml`) per machine, honoring
`bootwright_destroy_cluster_scope`. Role destroys are idempotent, so substrates
also covered by recorded resources (libvirt) tolerate the double pass.

**A cluster-scoped gate inside a per-machine role never fires:** the libvirt
network `bw-<cluster>` is one object shared by every machine of the cluster, but
`machine_substrate_libvirt/tasks/destroy.yml` is dispatched once per machine-task
host. While the probe, ownership refusal, removal, and record deletion lived
there, whichever host won removed the network first; every later host's
`virsh net-dumpxml` then returned "Network not found", which the conclusive-probe
assert legitimately ACCEPTS, so `bootwright_libvirt_network_present` was false and
the ownership refusal was SKIPPED rather than evaluated — a fail-closed gate that
never ran on the pass that actually removed the network. The probe and both
asserts now run once per cluster in
`playbooks/tasks/machine_infra/prepare_destroy_cluster.yml` (pre-substrate,
`strategy: linear`, so a refusal fails the host before any teardown), and only
that gate may append to `bootwright_libvirt_network_removals`, which the
POST-substrate records play consumes. The removal cannot move into the
preparation play: the domains are still defined and attached at that point.
Guarded by TestLibvirtNetworkDestroyGateIsClusterScoped and
TestLibvirtNetworkRemovalRunsAfterMachineSubstrateTeardown. Because the
preparation play only iterates clusters the host holds machines for, its
component/cluster resolution must span `bootwright_clusters` AND
`bootwright_managed_os_install_groups` or a libvirt-hosted storage cluster loses
the gate entirely.

**Bastion containers: ownership by live labels, not per-context records:** the
bastion runs ONE global podman store shared by every context while ownership
dirs are per-context, so the infra-component apply-mode gate
(`ownership_record/tasks/infra_component_container_gate.yml`) derives
`bootwright_gate_owned` ONLY from the live container's provenance labels
(`bootwright.provider`, `bootwright.name`) — a per-context record stat
misreports a shared container created by another context as foreign and blocks
the second context's apply. `bootwright_gate_exists` = label probe OR
exact-name probe (`name=^{{ bootwright_gate_container_name }}$`); every
container-backed role must pass `bootwright_gate_container_name` or the
foreign-squatter probe is disarmed. Container names must live in role DEFAULTS,
not a tasks set_fact — the gate runs before the role's set_fact block. A
missing podman binary or no matching container reads as "does not exist".

**Conventions:** destroy paths tolerate firewalld errors when closing ports (a
best-effort close never aborts a teardown). `support_ssh_readiness` is the
shared post-boot SSH probe and is contractually read-only (`changed_when:
false` everywhere; `bootwright_ssh_ready_address: ''` skips all probes,
`bootwright_ssh_ready_key_path: ''` skips auth verification).
