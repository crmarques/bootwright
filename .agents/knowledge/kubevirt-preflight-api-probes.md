# KubeVirt Preflight API Probes

KubeVirt host-cluster preflight probes address exact REST paths through
`kubectl get --raw`. Typed `kubectl get` commands initialize API discovery
before reading the target resource; when an endpoint or load balancer returns a
generic HTTP 404, client-go emits repeated `memcache.go` discovery errors that
obscure the actual failure.

The readiness probe uses the same
`/apis/subresources.kubevirt.io/v1/version` endpoint as version-matched
`virtctl` provisioning. When it fails, a separate Kubernetes `/version` probe
distinguishes a reachable host without OpenShift Virtualization from a
kubeconfig that does not reach the expected Kubernetes API surface. Once the
KubeVirt API prerequisite fails, dependent network probes must not run.

The durable managed-host path
`clusters/<host>/secrets/kubeconfig` contains an AES-GCM envelope, not
kubectl input. Passing that path directly to kubectl provides no usable
kubeconfig context and can surface the same misleading generic HTTP 404. Host
references decrypt through their per-cluster store; `kubeconfigRef` references
decrypt or copy through the declared-secret resolver.

Preflight creates a private per-invocation directory below the context runs
directory and uses it as the materialization parent. The materializer creates
a nested `0700` scratch directory and `0600` file, then removes that scratch on
every callback return, including errors. A target's KubeVirt readiness and
network probes share that one materialized file inside the callback. Human
evidence and remediation continue to name the durable source path. Deferred
cleanup is a normal-return guarantee, not a claim that `SIGKILL` can run
cleanup handlers.

KubeVirt execution has the same boundary. `workflow.Run` materializes each
managed host kubeconfig required by the current playbook or task under the
task's runtime secrets directory and keeps the selected set alive through one
Ansible invocation. The renderer projects those paths both into
machine-component KubeVirt variables and the host-to-kubeconfig map used by
controller `virtctl` provisioning. Provision, boot, and destroy roles never
receive the durable envelope path. This map covers `hostClusterRef` only:
Ansible `kubeconfigRef` paths continue through ordinary declared-secret
resolution, so context material uses the task runtime secret store and an
explicit file source in source mode remains the operator-owned source path.
Dry-run keeps the logical managed-host path and creates no plaintext runtime
material.

For a destroy graph, machine-infra tasks derive that set from their exact
machine keys, cluster substrate closure, and explicit destroy machine scope.
Records-only and unrelated tasks resolve an explicit empty set. The resulting
runtime path map is part of the task-overlay cache key, so an overlay rendered
for one host-cluster scope cannot be reused for another. Context secrets may be
shared once across the destroy graph, but captured host kubeconfigs retain this
per-task callback lifetime.

KubeVirt host readiness, missing captured kubeconfig, and runtime host-access
failures carry the same typed host-`ContainerCluster` reconcile action. The
backend message names only the child, host, durable path, and observed failure;
it never assembles apply argv. For a real apply, `internal/cli` formats the
exact cluster-stage host reconcile and then the unchanged original invocation,
including context, selection, confirmation, preview/output, SSH identity,
effects, and authorizations. For a standalone read-only preflight, there is no
original apply selection: the CLI formats only the interactive host reconcile
and tells the operator to rerun the preflight. Inferring a fleet-wide apply from
that diagnostic would be a state change the operator did not request.
