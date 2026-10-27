# MetalLB on OpenShift

How the MetalLB Operator installs, what makes its address pools answer, and
what it can never serve. The MetalLB package and its pool configuration are
[B727](../../specs/milestones/m4.md#b727) on M4, with
[B83](../../specs/milestones/m4.md#b83) and
[B84](../../specs/milestones/m4.md#b84), never the OpenShiftCluster's. The
cluster's endpoints are owned by
[container clusters](../../specs/openshift-clusters.md).

Provenance: `R4:C<n>` is a claim id of the M3 research (not the M4 research
that uses the same labels on the ACM and GitOps pages), `RV` marks a claim the
review re-verified. Release labels name where a fact was read; they are not
supported or required versions (D163). The ids name the M3 definition's
research and review notes of 2026-10-20, which this repository does not track;
each statement here carries its fact, and an id only records where it was read.
`M4 RV-R1` names the M4 definition's review of OLM and catalogs of the same
date, likewise untracked, and "inference" marks a fact the research derived and
did not read.

## Installation

- The Subscription uses package `metallb-operator`, channel `stable`, source
  `redhat-operators` and source namespace `openshift-marketplace`. The
  community bundle declares channel `alpha`, so the downstream channel names
  are read from the cluster's package manifest (R4:C2).
- The CLI install uses namespace `metallb-system`; the console install uses
  `openshift-operators`, and the MetalLB resource's namespace follows it
  (R4:C3).
- The OperatorGroup carries no `spec`: the operator's install modes allow only
  all namespaces, so `targetNamespaces` is unsupported (R4:C4). The documented
  Subscription sets no `installPlanApproval`, so OLM approves automatically
  (R4:C5); the MetalLB package declares `Manual` approval and a `startingCSV`
  instead ([B84](../../specs/milestones/m4.md#b84), exact approval in
  [olm-v0-exact-install.md](olm-v0-exact-install.md)).
- The operator supports a single `MetalLB` resource named `metallb`; any other
  name sets `Degraded` with reason `IncorrectMetalLBResourceName` and deploys
  nothing (R4:C6, read in code).
- The shipped CSV, read from `manifests/stable` on the operator's `release-4.21`
  branch, supports all namespaces only, declares `webhookdefinitions` and
  `features.operators.openshift.io/disconnected: "true"`, and suggests
  `metallb-system` as its namespace. The upstream `bundle/` CSV is a template
  that lacks the disconnected flag, so a table built from it is wrong (M4 RV-R1
  C50, corrected; correction 2). No `dependencies.yaml` was found, but that was a
  404, so [B757](../../specs/milestones/m4.md#b757) re-verifies that the
  InstallPlan holds a single CSV.

## Readiness

The `MetalLB` resource reports conditions `Available`, `Upgradeable`,
`Progressing` and `Degraded`. Healthy is `Available` `True` and `Upgradeable`
`True`. `Progressing` holds until the speaker DaemonSet and the controller
Deployment are ready, and `Available` follows the readiness of the `controller`
Deployment and the `speaker` DaemonSet with a 5-second requeue (R4:C8).
`Degraded` also reports transient sync failures (not researched; re-verified by
[B757](../../specs/milestones/m4.md#b757)), so it is reported and never fails
fast. The speaker is a host-network DaemonSet on every
Linux node and tolerates the control-plane taint, so single-node and compact
clusters get a speaker per node (R4:C22, RV). The operator performs no platform
check, so it installs on platform `none` (R4:C22, medium confidence).

## Address advertisement

- `L2Advertisement.interfaces` restricts the interfaces an address is advertised
  from. It does not steer which node announces: a chosen node that lacks the
  named interface does not announce. Every eligible speaker node needs the
  interface, or the speaker set is restricted by node selector (R4:C13).
- A pool on a secondary interface needs more cluster networking. Forwarding
  between interfaces is off by default, so the docs patch the cluster network
  operator with `gatewayConfig.routingViaHost: true` and then enable forwarding
  on the interface. The 4.20 and 4.21 docs do it with a MachineConfig sysctl
  (`net.ipv4.conf.<interface>.forwarding = 1`) or globally with
  `gatewayConfig.ipForwarding: Global`; the 4.22 docs do it with an NNCP
  `ipv4.forwarding: true`, which needs the NMState Operator (R4:C14, RV), and
  the 4.22 notes list per-interface NNCP forwarding as new (R3:C30). That is a
  documentation difference, not evidence that one mechanism fails on another
  release. B727's driver uses one general mechanism; a version specialization
  is registered only if a run shows it failing where a provable artifact or
  cluster fact discriminates (D162).
- A MetalLB address must not share a subnet or VLAN with an additional network
  on a source pod on the same node (R4:C14).
- A pool that no advertisement selects allocates addresses nobody announces
  (R4:C17, inference, medium). An `L2Advertisement` without `ipAddressPools`
  applies to every pool (R4:C12).
- OVN-Kubernetes moves the install interface into `br-ex`, so an advertisement
  that names the install interface does not match. Pools on the machine network
  therefore omit `interfaces` ([container clusters](../../specs/openshift-clusters.md),
  node networking; an inference that
  [B729](../../specs/milestones/m3.md#b729) re-verifies).
- An interface enslaved to an OVS bridge as a localnet uplink carries no host
  address and does not route replies, so it is never a MetalLB announcing
  interface (inference; B729 re-verifies the addressed-NIC path).

## Pool creation

MetalLB validates its configuration resources with webhooks served by its
controller, so a pool created before the controller serves may be refused
(R4:C18, medium). [B757](../../specs/milestones/m4.md#b757) re-verifies this
after `Available`.

## Platforms and API surface

- Supported platforms are bare metal, vSphere, IBM Z and LinuxONE (and RHEL
  KVM), and IBM Power; KubeVirt and `none` are not named. Anti-ARP-spoofing must
  be off where speakers run (R4:C19, RV; R4:C20). On a localnet with
  `ipam.mode: Disabled`, OVN port security enforces only the MAC, so a speaker's
  ARP reply from its own MAC passes (R5:R5-32, RV, M3 research).
- `IPAddressPool.status` (`assignedIPv4` and others) is documented from 4.21
  (R4:C11). `L2Advertisement.nodeSelectors` is Technology Preview in 4.20 to
  4.22 (R4:C12). 4.22 adds advertisement `serviceSelectors` and documents
  `ConfigurationState` (R4:C29, C36).

## What MetalLB never serves

- MetalLB cannot be the endpoint for `api`, `api-int` or the default ingress:
  it is deployed after the cluster, and those endpoints are needed first
  (R4:C31, RV; R4:C32, reasoned from sources). The default IngressController
  publishes through the host network on bare-metal and `none` platforms (R4:C33,
  RV for the default), and its strategy type cannot change after installation
  (medium confidence, derived from the documentation's list of updatable
  fields).
- Installer keepalived holds the API and ingress addresses, so a pool must not
  include them. No Red Hat document states the exclusion; it follows from two
  owners answering for one address (R4:C24, inference).
