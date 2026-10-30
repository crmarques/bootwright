# Single-node OpenShift with ordered add-ons

[lab-sno](../../openshift-clusters/lab-sno/) plus two `ClusterAddonBinding`
objects on the same cluster: [add-ons/nmstate.yaml](add-ons/nmstate.yaml)
selects `nmstate`, and [add-ons/metallb.yaml](add-ons/metallb.yaml) selects
`metallb` with `afterRefs` naming the first. The topology, Secrets and
controller prerequisites are lab-sno's; the only additions are the Environment
name and the two bindings. All addresses and identities are synthetic.
Operator acceptance is pending.

## Worth noting

- Neither package requires the other, so the order comes only from the authored
  `afterRefs` entry; its rule is
  [`spec.afterRefs`](../../../specs/api/addons.md#clusteraddonbinding), and
  [instances and blocks](../../../specs/add-ons.md#instances-and-blocks) gives
  the block order it produces.
- Placeholder channel and CSV: both bindings declare `example-channel` and a
  `.v0.0.0` CSV. They are illustrative, not defaults or a qualified pairing;
  replace each pair with the operator's channel and its exact CSV. On a cluster
  whose catalog serves the package, read `status.channels[].name` and
  `status.channels[].currentCSV` from the lookup that
  `bootwright add-ons info --name <package>` prints.
- The `services` pool holds eight machine-network addresses
  (`198.51.100.128-198.51.100.135`), separate from the node, its endpoints and
  the managed network's host address. Keep it outside any DHCP range on the
  network, and adjust it with the subnet.
- To stop after the first instance, run `bootwright apply --until
  sno.nmstate`; a later `apply` continues with `metallb`
  ([stopping after an add-on instance](../../../specs/state-reconciliation.md#stopping-after-an-add-on-instance)).

Run it with the e2e [`lab-sno`](../../../e2e/lab-sno/runbook.md) case.
