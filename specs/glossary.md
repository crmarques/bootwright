# Glossary

Each term has one meaning and one owner page. A word marked "qualified" takes
its modifier wherever two of its senses can meet; elsewhere context decides. A new term enters this page with its owner link in the same change.

## Core terms

- **Context**: a named, durable Bootwright workspace holding frozen input,
  secrets and lifecycle state ([contexts](contexts.md)). It is not a Go
  `context.Context` and not a bounded context of the architecture.
- **Controller**: the host that runs Bootwright, and in desired state the
  Machine that `spec.controller.machineRef` names ([controller](controller.md)).
  Bare "controller" never names anything else.
- **BMC** or **management controller**: a Machine's out-of-band Redfish
  endpoint, physical or emulated ([substrates](substrates.md)).
- **Realized target**: what a substrate derives for a Machine: the management
  controller it boots through, its identity channel, whether it is physical and
  the block that realizes it ([substrates](substrates.md#selection-and-refusal)).
- **Substrate**, **provider** and **infrastructure service**: a substrate is a
  kind of infrastructure that realizes Machines (`baremetal`, `libvirt`,
  `vsphere`, `kubevirt`); a provider is one declared `InfraProvider` of one
  substrate, so `Infra` in `InfraProvider` means substrate; an infrastructure
  service is a shared service kind, managed or external, such as `Proxy` or
  `DNSServer` ([infrastructure services](infrastructure-services.md)). The
  `infrastructure-services` stage token names the service layer.
- **Block** and **step**: a block is the plan node, one frozen unit of the
  plan ([state reconciliation](state-reconciliation.md#plan-and-execution)); a
  step is a block as a preview or result presents it, numbered by its place in
  plan order, and `steps` is the public JSON name of that list
  ([output](cli/output.md#lifecycle-and-status-results)). An add-on
  `spec.steps` entry is an add-on step ([add-ons](api/addons.md#steps)), never a
  block.
- **Reservation**: a host-wide claim, such as a socket, BMC, media or machine,
  that refuses a conflicting context
  ([host reservations](infrastructure-services.md#host-reservations)).
- **Runtime**: setup publishes the controller runtime a lock and interpreter
  name ([controller](controller.md#the-controller-runtime)).
- **Acceptance**: an operator-run gate recorded in the
  [ledger](../docs/acceptance.md); in-tree gates are tests.

## Qualified terms

- **Binding**: always a host binding, Secret binding, add-on binding or
  interface binding. A context binds the controller host it uses
  ([contexts](contexts.md#controller-relationship-and-host-binding)); an
  operation binds the exact secret material it froze
  ([secrets](secrets.md#immutable-binding-and-contexts)); a
  `ClusterAddonBinding` attaches add-ons to a cluster
  ([add-ons](api/addons.md#clusteraddonbinding)); a Machine's
  `network.interfaceBinding` binds each physical interface to a hardware NIC
  ([machines](api/machines.md#network-configuration)).
- **Profile**: always a machine profile, install profile
  ([`MachineInstallProfile`](api/machines.md#machineinstallprofile)), add-on
  profile ([`ClusterAddonProfile`](api/addons.md#clusteraddonprofile)) or
  acceptance profile (a row of a coverage page, such as the
  [Ceph profile catalog](ceph-coverage.md#profile-catalog)).
- **Capability**: always an add-on
  capability token (what an [add-on](api/addons.md#clusteraddon) provides or
  requires for ordering), a lifecycle capability (a module of one bounded
  context, [architecture](architecture.md#adding-a-lifecycle-capability)) or,
  in the architecture's dependency rules, a consumer-owned contract: a Go
  interface or function, or an Ansible named capability
  ([architecture](architecture.md#dependency-direction-and-communication)).
- **Implementation**: the registry entry a block freezes, with a versioned
  identity, a content digest and an adapter entry point
  ([architecture](architecture.md#implementations-and-version-variation)). A
  member of a union is an **arm** ([api](api.md#unions)), never an
  implementation. A **variant** is the domain distinction a union's `type`
  names ([unions](api.md#unions)), never a registry entry or a release
  difference.
- **Version specialization**: a record that varies one seam of a component's
  general flow by a release difference, with an id, the seam, one of the
  seam's declared cases and a condition over facts
  ([architecture](architecture.md#implementations-and-version-variation)).
  Its alternatives are cases, never variants.
- **Evidence**: always context mutation evidence
  ([state reconciliation](state-reconciliation.md#context-mutation-evidence)),
  block evidence ([transitions](state-reconciliation.md#block-transitions)) or
  completion evidence
  ([handoff](state-reconciliation.md#bootstrap-completion-and-gitops-readiness)).
- **Receipt**: always the lifecycle receipt, the four final lines of an
  operation ([cli](cli.md#lifecycle-receipt)), or the setup receipt, which
  names the interpreter setup prepared
  ([controller](controller.md#the-controller-runtime)).
