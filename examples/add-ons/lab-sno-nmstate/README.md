# Single-node OpenShift with Kubernetes NMState

[lab-sno](../../openshift-clusters/lab-sno/) plus one `ClusterAddonBinding`
selecting `nmstate`, as the input for [B735](../../../specs/milestones/m4.md#b735).
The topology, Secrets and controller prerequisites are lab-sno's; the only
additions are the Environment name and [add-ons/binding.yaml](add-ons/binding.yaml).
All addresses and identities are synthetic. Operator acceptance is pending.

## Worth noting

- Placeholder channel and CSV: `add-ons/binding.yaml` declares
  `example-channel` and `kubernetes-nmstate-operator.v0.0.0`. They are
  illustrative, not defaults or a qualified pairing; replace both with the
  operator's channel and its exact CSV. On a cluster whose catalog serves the
  package, read `status.channels[].name` and `status.channels[].currentCSV`
  from:

  `oc get packagemanifest kubernetes-nmstate-operator -n openshift-marketplace -o json`

- Bootwright approves a Manual InstallPlan only when it resolves exactly the
  declared CSV from the declared catalog.
- The add-on installs after the cluster. A settled apply contacts no cluster,
  and destroy retires the add-on with the cluster without deleting its
  Kubernetes objects through the cluster API.

Run it with the e2e [`lab-sno`](../../../e2e/lab-sno/runbook.md) case.
