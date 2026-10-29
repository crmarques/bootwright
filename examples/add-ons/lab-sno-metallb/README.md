# Single-node OpenShift with MetalLB

[lab-sno](../../openshift-clusters/lab-sno/) plus one `ClusterAddonBinding`
selecting `metallb`, as the input for
[B727](../../../specs/milestones/m4.md#b727). The topology, Secrets and
controller prerequisites are lab-sno's; the only additions are the Environment
name and [add-ons/binding.yaml](add-ons/binding.yaml). All addresses and
identities are synthetic. Operator acceptance is pending.

## Worth noting

- Placeholder channel and CSV: `add-ons/binding.yaml` declares
  `example-channel` and `metallb-operator.v0.0.0`. They are illustrative, not
  defaults or a qualified pairing; replace both with the operator's channel
  and its exact CSV. On a cluster whose catalog serves the package, read
  `status.channels[].name` and `status.channels[].currentCSV` from:

  `oc get packagemanifest metallb-operator -n openshift-marketplace -o json`

- Bootwright approves a Manual InstallPlan only when it resolves exactly the
  declared CSV from the declared catalog.
- The `services` pool holds eight machine-network addresses
  (`198.51.100.128-198.51.100.135`), separate from the node, its endpoints and
  the managed network's host address. Keep it outside any DHCP range on the
  network, and adjust it with the subnet. MetalLB does not serve the API or
  default ingress endpoints. Bootwright creates no test workload; verifying a
  LoadBalancer Service is the operator's step.
- The add-on installs after the cluster. A settled apply contacts no cluster,
  and destroy retires the add-on with the cluster without deleting its
  Kubernetes objects through the cluster API.

Run it with the e2e [`lab-sno`](../../../e2e/lab-sno/runbook.md) case.
