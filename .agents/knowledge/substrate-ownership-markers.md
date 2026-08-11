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
KubeVirt re-verifies the `bootwright.io/managed-by`, context, cluster, and node
labels on the VirtualMachine and the manager, context, cluster, node, and
volume-role labels on every DataVolume (the namespace may host foreign
workloads). A PVC must carry that same exact label set or have no ownership
labels and carry an exact ownerReference, including UID, to the already-verified
DataVolume. Bootwright stamps the inherited identity durably on that PVC before
deleting the DataVolume, so later PVC teardown never depends on vanished parent
evidence. Each API read must either return a successful `NotFound` or a complete
live object; a
forbidden, transport, parse, or empty result is unknown, not absence. Assert and
delete are gated on that conclusive classification so destroy stays idempotent;
a libvirt `failed to get domain/network` prefix alone is likewise ambiguous and
only an explicit `Domain/Network not found` or `no ... with matching name`
diagnostic proves absence. The repo guard rejects that broad prefix anywhere in
the Ansible collection. A missing/mismatched marker fails closed and
`bootwright_destroy_authorize_unowned_vms` is the explicit recovery relaxation
(per-VM refusals only — never failed probes, Ceph ownership, or device
data-safety gates).

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
(`provider_host_libvirt/tasks/destroy_context.yml`) undefines every domain
whose XML carries the marker for this context, including a domain whose disk was
moved outside the conventional storage root. Block-device paths decide only
which storage trees are safe to remove. When a foreign domain co-resides under
the context root, the blanket root removal is skipped
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

**Provider teardown retains its retry evidence:** the cluster-scoped libvirt
network remover, emulated-BMC runtime and vmedia-pool teardown, and vSphere
datastore-media remover all fail before deleting local state or ownership. Their
rescues name the failed external operation and the controller-rendered exact
mutating invocation. Firewall-port closure may be skipped only when firewalld
is conclusively absent or stopped: with the listener already removed, a stale
allow rule exposes no process and does not authorize deleting a live provider
resource. A cleanup attempted against running firewalld is a hard operation;
failure retains the teardown evidence for retry.

**A controller name locates; live identity authorizes:** provider objects can be
removed and recreated under the same global name between runs. Apply, declared
destroy, and record-only orphan sweep therefore re-read the live object and
require the exact Bootwright manager, context, provider/cluster, machine or
component, and role metadata before the first mutation. A stale record never
overrides a contradictory libvirt XML description, KubeVirt label set, vSphere
annotation, container label, or service claim. A suppressed probe is classified
from success-only response fields, never `.failed`; an unsupported or
inconclusive live classifier conservatively retains the resource and evidence.

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
Bootwright creates that work only for cluster-member managed-OS machines. A
standalone `Machine` that hosts no shared service or provider has no safe
apply/destroy task to dispatch, so machine selection refuses without inventing
a retry command; restore its intended declared relationship or decommission it
out of band.

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
the gate entirely. The record can establish the expected address but cannot
make a mismatched live XML owned: exact live context and cluster identity is
required, and a contradictory network is refused even when a stale record has
the same kind and name.

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

**Conclusive firewalld absence is not a teardown blocker:** shared
infra-component destroy accepts an absent `/usr/bin/firewall-cmd`, a running
daemon (`rc=0`, `running`), or firewalld's conclusive not-running result
(`rc=252`). Only the running case attempts firewall cleanup. A non-regular,
linked, or non-executable binary, an incomplete command result, or any other
return code remains unknown and fails before service mutation. A gate must not
also require `bootwright_firewalld_available` merely because its authoritative
port ledger is non-empty: that fact is intentionally false for both conclusive
unavailable states, and the extra condition strands every such teardown.
Skipping the close in those states is safe because removing the listener leaves
a stale allow rule with no process to expose. A cleanup attempted against a
running daemon remains hard and retains evidence on failure.

**Conventions:** `support_ssh_readiness` is the shared post-boot SSH probe and
is contractually read-only (`changed_when: false` everywhere;
`bootwright_ssh_ready_address: ''` skips all probes,
`bootwright_ssh_ready_key_path: ''` skips auth verification).
