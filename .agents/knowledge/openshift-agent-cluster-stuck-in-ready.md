# One unregistered node stalls the cluster in "ready" and aborts the wait

**Symptom:** `openshift-install agent wait-for bootstrap-complete` exits
non-zero a few minutes in, with
`Bootstrap failed to complete: : bootstrap process returned error: failed to
progress after all hosts available`, immediately after the log reports
`Cluster is ready for install` and
`Cluster validation: All hosts in the cluster are ready to install.`
A trailing
`Attempted to gather ClusterOperator status after wait failure: ... dial tcp
<apiVIP>:6443: connect: no route to host` is a consequence, not the cause —
the API VIP is unowned because bootstrap never started.

**Root cause:** two different components count hosts, and only one of them is
strict. assisted-service marks the cluster `ready` once the registered hosts
validate and the control-plane count is met; it does not require the declared
compute count. The rendezvous node's `start-cluster-installation.sh` blocks in
`while [[ "${total_required_nodes}" != $(num_known_hosts) ]]`, where
`total_required_nodes` is `controlPlane.replicas + arbiter + compute.replicas`
from install-config and `num_known_hosts` counts hosts whose status is exactly
`known`. One declared node that never registers therefore leaves the cluster
`ready` forever with the install never triggered.

`openshift-install` gives up on that state fast: `IsClusterStuckInReady`
(`pkg/agent/cluster.go`) fails the wait once the cluster's previous polled
status was `ready` for more than **one minute**. The give-up is client-side —
the rendezvous node keeps waiting, so re-running the wait resumes monitoring
and succeeds if the missing host turns up.

Every host that did register appears in the wait's stderr with validation
lines. The node that never registered appears nowhere at all; diff that list
against the cluster's declared nodes to name it.

**Fix:** `wait_install.yml` retries both `wait-for bootstrap-complete` and
`wait-for install-complete` while stderr matches
`bootwright_install_stalled_wait_pattern` — the installer's two documented
give-ups, `failed to progress after all hosts available` and `failed to prepare
cluster installation`. Each attempt buys another minute of grace inside the
installer plus `bootwright_install_stalled_wait_delay_seconds`, bounded by
`bootwright_install_stalled_wait_retries` and by the wait budget deadline. The
installer's third client-side give-up, `bootstrap process timed out`, is
resumed on the same loop under its own pattern and hint — see
[openshift-agent-wait-installer-window.md](openshift-agent-wait-installer-window.md).
Any other failure is not retried.
The waits carry `failed_when: false` and report through a dedicated fail task,
so the give-up is named rather than dumped: the message states the exact
known-host count the rendezvous node is waiting for and lists every declared
node.

A give-up that survives the retries is a real missing node, not a slow one.

**Boot-side gate.** The node that never registers should never have reached
this wait. Every boot driver ends by handing its machine to
`support_ssh_readiness`, which waits for the node's `primaryIPAddress` to
answer TCP/22 and then to accept the cluster SSH key — proof the guest booted
the generated agent ISO, not merely that the hypervisor started it. The
KubeVirt driver used to stop at `kubectl wait vmi --for=condition=Ready`, which
only means the virt-launcher pod is up: one VM whose guest never booted the ISO
passed its boot activity clean and surfaced 9 tasks later as this opaque
installer give-up. It now runs the same gate as the Redfish and vSphere
drivers, so a guest that never comes up fails its own machine's boot activity,
by name.
