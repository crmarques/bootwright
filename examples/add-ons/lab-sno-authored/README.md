# Single-node OpenShift with an authored operator

[lab-sno](../../openshift-clusters/lab-sno/) plus one `ClusterAddon` of the
[executable OLM shape](../../../specs/api/addons.md#executable-olm-add-ons),
selected by a `ClusterAddonBinding` through `spec.addonRefs`. It shows how to
install an operator Bootwright does not ship. The topology, Secrets and
controller prerequisites are lab-sno's; the additions are the Environment name,
[add-ons/binding.yaml](add-ons/binding.yaml) and
[add-ons/widgets.yaml](add-ons/widgets.yaml).
All addresses and identities are synthetic, and the operator, its catalog and
its `Widget` resource are illustrative: no such operator exists, so the example
plans but is not run.

## Worth noting

- Illustrative values: the package `example-operator`, the channel
  `example-channel`, the CSV `example-operator.v0.0.0`, the catalog image digest
  and the `widgets.example.test` definition are placeholders, not defaults or a
  qualified pairing. Replace them with your operator's package, channel, exact
  CSV, digest-pinned catalog and definition.
- The `ClusterAddon` is named `widgets`, not after the operator: an authored
  add-on is named like a package, so its name fits `addons.MaxPackageNameBytes`
  and is never a built-in package's name.
- The add-on owns its namespace, its catalog (`priority` below `-100`, so it
  never outranks the default catalogs) and its subscription. Bootwright
  subscribes Manual and approves only the declared `startingCSV`.
- Each custom resource sits in the add-on's own namespace behind a gate that
  names its `CustomResourceDefinition`; the plural comes from you, because
  Bootwright cannot derive it from the kind. An operator whose custom resources
  live under a reserved API group, or are cluster-scoped, is refused.
- The catalog image and the CSV are code you choose and run with the
  [trust boundary](../../../specs/add-ons.md#trust-boundary) of a built-in
  package.

Run it with the e2e [`lab-sno`](../../../e2e/lab-sno/runbook.md) case's
topology after replacing the placeholders.
