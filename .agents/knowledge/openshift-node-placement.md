# Labelling and tainting OpenShift nodes after installation

What the Kubernetes API does when the installation block places its nodes:
who may write a node's role labels and taints, and why the write is a pinned
JSON patch rather than a server-side apply. [Node placement](../../specs/openshift-clusters.md#node-placement)
owns the required behavior and [D199](../../specs/milestones/backlog.md#d199)
the decision; [B716](../../specs/milestones/m3.md#b716) is the item.

Provenance: each statement names the source it was read from on 2026-10-28.
Release labels name where a fact was read; they are not supported or required
versions (D163).

## Who may write node roles and taints

- The NodeRestriction admission plugin limits a kubelet (credentials in
  `system:nodes`, user `system:node:<nodeName>`) to its own Node object, and
  "kubelets are not allowed to update or remove taints from their `Node` API
  object". It forbids kubelets to modify labels with the
  `node-restriction.kubernetes.io/` prefix and labels with the
  `node-role.kubernetes.io/` prefix, "restricted to prevent unprivileged nodes
  from self-declaring cluster roles"
  ([admission controllers, NodeRestriction](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#noderestriction)).
  So `node-role.kubernetes.io/infra` and every authored taint are written
  through the API by an administrator, never by kubelet `--node-labels`; the
  agent installer has no input for either.

## Why not server-side apply

- `NodeSpec.Taints` is `+listType=atomic`
  ([k8s.io/api v0.36.2 `core/v1/types.go`](https://github.com/kubernetes/api/blob/v0.36.2/core/v1/types.go),
  the `Taints []Taint` field of `NodeSpec`), so an apply that sets it owns the
  whole list: without force it conflicts with the manager that set any other
  taint, and with force it would replace that manager's taints.
- An apply creates the object when it is absent, so an apply to a Node
  deleted between read and write would register a node object no kubelet
  backs. A JSON patch of an absent object fails NotFound.
- Labels are a granular map under apply, and an apply removes a key its own
  manager applied before and omits now; a pinned JSON patch that only adds
  keys never removes one, whoever owns it.
- A JSON patch `test` operation fails the whole patch when the value at its
  path differs, which is how the installation approves an InstallPlan
  (`cluster_objects_native.approve`) and how placement pins a node's uid, label
  map and spec as read. kubectl's and oc's `patch` take `--field-manager`, and
  the managed fields then record that manager with `operation: Update` for the
  keys it added ([kubernetes-object-ownership-with-oc.md](kubernetes-object-ownership-with-oc.md)
  says a manager entry proves only that some write used the name).

## Ingress placement and infra nodes

- With compute nodes present, the default IngressController places its router
  pods by `node-role.kubernetes.io/worker` and carries no toleration for an
  infra taint, so tainting every compute node `NoSchedule` leaves ingress
  unschedulable (OpenShift documentation on infrastructure machine sets and
  moving the router; not read in this session, re-verified by the infra row of
  lab-anysize). Infra nodes install through the worker pool and keep that
  label, which is why no taint is synthesized for the `infra` role.
