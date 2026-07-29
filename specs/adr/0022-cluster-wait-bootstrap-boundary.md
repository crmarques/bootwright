# ADR 0022: Cluster Install Wait Splits at the Bootstrap Boundary

## Status

Accepted

## Context

A container cluster install ended in one graph activity, `wait.<cluster>`,
running `openshift-install agent wait-for install-complete`. That command
returns only when every ClusterOperator reports Available, which on a
production-shaped cluster is many minutes after the cluster API server is
usable. The captured `kubeconfig` and `kubeadmin-password` were published only
after that command returned, even though the agent installer writes both into
the asset directory when the ISO is built.

Everything downstream — add-ons, node config, custom playbooks anchored at the
`base` phase, and, on nested topologies, the entire child-cluster branch —
therefore waited for full cluster convergence. On the reference nested
topology the cost is paid twice, because the parent install gates the child.

`openshift-install agent wait-for bootstrap-complete` returns at the earlier
boundary, when the temporary bootstrap control plane has handed off to the
permanent one. No task in the repository ran it.

## Decision

`base`-phase planning emits two activities per container cluster instead of
one:

- `wait-bootstrap.<cluster>` (task kind `bootstrapWait`) runs `agent wait-for
  bootstrap-complete`, then publishes the `kubeconfig` and
  `kubeadmin-password` exactly as the install-complete path did, including the
  post-capture encryption boundary of ADR 0020.
- `wait.<cluster>` (task kind `installWait`, unchanged ID, kind, playbook and
  limit) runs `agent wait-for install-complete` and keeps every existing
  behaviour: cluster install record marking, `Available=True` reporting,
  virtual-media cleanup, generated-ISO cleanup, staged publish-target cleanup,
  and substrate-release consumption.

`wait.<cluster>` depends on `wait-bootstrap.<cluster>`; the bootstrap activity
inherits the node-boot (or ISO) dependency the install wait used to carry. Both
activities run the same playbook and role action, selected by the
`bootwright_install_wait_target` extra var (`bootstrap` or `install`); the role
default is `install`, so the monolithic `run` action is unchanged.

What bootstrap-complete guarantees: the permanent control plane serves the
cluster API, and the `auth/` credentials in the installer asset directory
address it. What it does not guarantee: that every node has joined, that
worker kubelets have registered, that the MCO has finished rolling any pool,
that the ingress or image-registry ClusterOperators are Available, or that any
operator catalog is served.

Only work that is safe under all of those absences may hang off the early gate.
Today nothing does. `wait.<cluster>` is the sole consumer of
`wait-bootstrap.<cluster>`, which makes this change a scheduling no-op with the
gate in place and the credential publication moved earlier.

These stay behind `wait.<cluster>` (install-complete), deliberately:

- Every add-on activity, in whole. Splitting an add-on so that only Namespace,
  OperatorGroup and Subscription creation runs early would require splitting
  one `clusterAddon` ledger entry into two across the add-on runner, the ledger
  taxonomy, the destroy kind map, and the run-phase model — and would split one
  converge-safety record into two on installed fleets.
- Node config apply. Its node-registration wait is a bounded poll (30 attempts
  at 10s), and the nodes it configures are the labelled, tainted and infra-role
  nodes, which are typically the last to join. Beyond the risk of exhausting
  that budget, creating the infra `MachineConfigPool` at bootstrap-complete
  starts a machine-config rollout — node reboots — while ClusterOperators are
  still converging, which perturbs the very install the run is waiting on.
- `boot.<cluster>`, every `infra.*`/`machine*` activity, and anything else that
  mutates hardware or substrate.
- KubeVirt child-cluster machine tasks. They require `cluster.installed` on the
  parent plus the parent add-ons that produce their `storageClassRef` and
  `networkRef`; the objects those refs name exist only after the add-on
  completes, never merely after its Subscription is created.

The bootstrap wait records its own completion in a marker file inside the
installer work directory, and skips the command when the marker is present. A
resumed apply whose bootstrap already succeeded therefore does not re-run
`wait-for bootstrap-complete`. The marker is removed with the rest of the
installer state before each ISO generation and with the runtime directory on
destroy.

## Consequences

- Captured cluster credentials are published, and encrypted per ADR 0020, at
  bootstrap-complete instead of install-complete. Operators can reach the
  cluster with `bootwright cluster kubeconfig` while the install finishes.
- `bootstrapWait` is a distinct task kind, so an already-installed cluster
  skips it before Ansible, a destroy removes its converge record, and it never
  moves the cluster install record — only install-complete may mark a cluster
  installed.
- On a fleet installed before this change the new activity has no
  converge-safety record, so the first `bootwright diff --recorded` reports it as
  missing under its own object key. The `ContainerCluster` object keeps its
  recorded classification, `apply` refuses nothing extra, and the record is
  stamped on the next apply, including the verified-installed skip path.
- Worst-case wall clock for a stuck install is now two waits rather than one;
  the bootstrap wait has its own timeout, defaulting to the install timeout.
- This is unvalidated without a hardware soak. Whether `agent wait-for
  bootstrap-complete` returns promptly on a cluster whose bootstrap completed
  under an earlier release (no marker file, assisted-service already gone) is
  not proven by any test in this repository; only a real install and a real
  resumed install exercise it.
