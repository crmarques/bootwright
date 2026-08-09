# A host in assisted-service status `error` strands the rendezvous node

**Symptom:** the agent install log records one host failing an installation
stage, immediately followed by a cluster-level notice, and bootstrap never
completes:

```text
level=warning msg="Host master-03.<cluster>.<domain>: updated status from
installing-in-progress to error (Host failed to install because its
installation stage Joined took longer than expected 1h0m0s)"
level=info msg="Cluster has hosts in error"
...
level=error msg="Bootstrap failed to complete: : bootstrap process timed out:
context deadline exceeded"
```

`oc --kubeconfig <work_dir>/auth/kubeconfig get nodes` is short exactly one
declared node, and the missing one is the **rendezvous** host — the first master
by sorted name, the same host `agentHosts` (`internal/render/installer/installer_agent.go:40-42`)
picked for `rendezvousIP`. The ClusterOperator dump that follows names
`authentication`, `ingress` (`0/2 of replicas are available`), `monitoring` and
`openshift-apiserver`; every one of those is downstream of the missing node.

**Root cause:** the rendezvous host runs assisted-service and the bootstrap
control plane, and the agent flow reboots it into its own role **last**, after
the other declared nodes have joined. When assisted-service moves any host to
status `error` it stops installing the cluster and diverts into recovery, and
that recovery does not include the final pivot. The rendezvous host therefore
keeps serving the bootstrap control plane indefinitely: its
`/etc/kubernetes/manifests` still holds `coredns.yaml`, `etcd-member-pod.yaml`
and `keepalived.yaml` rather than `etcd-pod.yaml`, `kube-apiserver-pod.yaml`,
`kube-controller-manager-pod.yaml` and `kube-scheduler-pod.yaml`, kubelet loops
on the bootstrap etcd member in `CrashLoopBackOff`, and `bootkube.service` is
`inactive (dead) … status=0/SUCCESS` — bootstrap finished cleanly and the host
simply stayed put. `oc get machines` shows its Machine parked in `Provisioned`
while its peers are `Running`.

Bootstrap can still *complete* from the surviving control-plane hosts, because
two masters suffice to hand the control plane off. That is what makes the state
so easy to misread: `bootstrap-complete` can return zero on a cluster that is
permanently one node short.

**What bootwright does with it:** `bootwright_install_host_error_pattern`
(`container_cluster_agent_install/defaults/main.yml`) matches `has hosts in
error` and `updated status from <stage> to error`, and both `until` expressions
in `actions/wait_install.yml` treat a match as **non-resumable**. It is
deliberately *not* folded into `bootwright_install_resumable_wait_pattern`: the
installer prints `bootstrap process timed out` on every bootstrap give-up, so
without the separate class a terminal failure is indistinguishable from the
slow-but-converging one that
[openshift-agent-wait-installer-window.md](openshift-agent-wait-installer-window.md)
describes, and bootwright spends another full installer window watching a state
that cannot change.

The classification is resolved from the wait's stderr *and* from the tail of
`.openshift_install.log`, because the host transition reaches stderr only at
`--log-level info` while the log keeps it unconditionally, and it is checked
ahead of every other class — the same stderr carries both the host error and
the installer's own timeout, so whichever branch is evaluated first wins.
`bootwright_install_rendezvous_node` re-derives the rendezvous host from the
declared roster and, when that host is the one missing from `oc get nodes`,
`bootwright_install_rendezvous_missing_hint` says so outright.

**Recovery:** the RHCOS master image was already written to the rendezvous
host's install disk, so the master ignition is on disk and a reboot **from
disk** pivots it. Confirm first that the BMC is no longer presenting the agent
ISO through virtual media, or it boots the ISO again, and check whether the host
still holds the API and ingress VIPs before rebooting it. When that is not worth
the hand-work, or the host that errored needs re-imaging anyway, rebuild:
`bootwright apply --stage clusters --clusters <cluster> --mode rebuild
--authorize data-loss --yes`.

**Not a fix:** re-running the apply. The wait resumes and observes the same
state, so it fails the same way after another window. Only fix the host that
errored — or rebuild — first.
