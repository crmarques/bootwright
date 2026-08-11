# Container-cluster Ansible flow semantics

**Semantics (agent teardown is controller-side):** In
`task_container_cluster_agent_destroy.yml`, `bootwright_ocp_hosts` resolves to
localhost (see `internal/render/inventory` `ocpReferencedHosts`). The
OpenShift agent install state — the generated ISO, manifests, and per-cluster
artifacts — lives on the controller, not on the cluster nodes, so there is no
managed host to reach and no device to wipe. A never-provisioned cluster has
no controller state to remove and its destroy is a clean no-op. There is no
node reachability to classify on this layer, so `--authorize unreachable-nodes` has no
effect here; the play still plumbs the gate var through only for uniformity
with the storage/machine teardown plays that do classify real nodes.

**Semantics (media cleanup dispatch is a registry, not a branch):** After
install, media cleanup in
`container_cluster_agent_install/tasks/actions/wait_install.yml` dispatches on
the rendered `cleanupMediaRole` of each cluster component
(`selectattr('cleanupMediaRole', 'defined')`), invoked with
`bootwright_vmedia_action: cleanup_media`. The renderer sets
`cleanupMediaRole` only for boot roles that own a `cleanup_media` action
(Redfish, vSphere) and leaves it unset otherwise — KubeVirt deletes its
agent-ISO DataVolume during boot, and `none` is a no-op. Adding a
media-bearing boot backend means adding a registry entry, never a new branch
in this play.

**Constraint (cluster nodes always receive an installer-provided OS):** Every
Machine referenced by `ContainerCluster.spec.nodes` must set
`spec.os.provided: false`; the agent installer writes RHCOS to that node. The
desired-state validator rejects a provided-OS binding before planning, and the
shared `boot_machine.yml` action independently asserts `osManaged: true` and
`osProvided: false` before dispatching any provider boot role. Both projected
values fail closed when absent, so a renderer regression or a future boot
driver cannot turn an incomplete component into permission to overwrite an
operator-provided OS. A powered-off check is not a substitute because a
provided-OS machine can be safely powered off while its disk remains foreign
to the installer. The closure is pinned by
`TestValidateNodesRejectsProvidedOSMachines`,
`TestContainerClusterRejectsProvidedOSNode`, and
`TestInstallAgentRefusesProvidedOSNodeBeforeBootDriverDispatch`.

**Constraint (FIPS clusters need the FIPS installer binary):** The stock
`openshift-install` refuses to build a FIPS cluster's agent ISO with
`use the FIPS-capable installer binary for RHEL 9`. The agent-install role
selects the binary per cluster:
`bootwright_openshift_install: openshift-install-fips` when
`bootwright_current_cluster.fips` is true (projected by the renderer), else
`openshift-install`. The `controller_openshift_tools` role installs both
binaries whenever any cluster enables FIPS.

**Constraint (effective installer inputs stay in place on the controller):**
Before Ansible runs, Go replaces and writes `install-config.yaml`,
`agent-config.yaml`, and the optional `openshift/` extra-manifest tree directly
under the cluster's controller-local `runtime/installer` directory. Go owns
stale-manifest removal; an empty rendered set deliberately leaves no
`openshift/` directory. The agent-install play runs on localhost and its local
and work directory defaults name that same path, so Ansible must consume these
files in place and must neither remove nor copy them as a staging step. The
retired bastion flow used distinct filesystems; retaining its remove-then-copy
sequence after moving installation to localhost deletes the copy source and
fails at `Stage installer extra manifests on controller` with `Could not find
or access .../runtime/installer/openshift/`.

**Constraint (no `end_play` in multi-host diagnostics):**
`diagnostic_cluster_endpoint_dns` runs when a cluster's endpoints use
`source.type: external` (the operator owns the LB and DNS records) and reports
hostname resolution from the provider-host vantage point without failing the
apply. It deliberately has no `meta: end_play` short-circuit: the role runs
inside a multi-host play, where `end_play` would abort the
play for **all** hosts, not just the current one. Its probe/report tasks
already no-op when `bootwright_external_endpoints` is empty (their loops
iterate an empty list). The same rule applies to any role added to a
multi-host play.
