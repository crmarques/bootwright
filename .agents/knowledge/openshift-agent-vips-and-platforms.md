# OpenShift agent VIPs, platforms and load balancers

Which `platform` the agent installer accepts for a topology, how it treats the
API and ingress virtual addresses, and what an external load balancer needs.
[Container clusters](../../specs/openshift-clusters.md) owns the required
behavior; this page records native facts for
[B707](../../specs/milestones/delivered.md#b707),
[B708](../../specs/milestones/delivered.md#b708) and
[B709](../../specs/milestones/m3.md#b709).

Provenance: `R<n>:C<m>` and `R5:R5-<n>` are claim ids of the M3 research, `RV`
marks a claim the review re-verified, and "observed by the v1 implementation
(main-v1)" marks a fact to re-verify on this tree. Release labels name where a
fact was read; they are not supported or required versions (D163). The ids name
the M3 definition's research and review notes of 2026-10-20, which this
repository does not track; each statement here carries its fact, and an id only
records where it was read.
Assisted-service, assisted-installer and assisted-installer-agent behavior was
read at master, not per release.

## Platforms and virtual addresses

- A single-node cluster (one control-plane node, no compute) is accepted only
  with platform `none` or `external` and the OVNKubernetes network type
  (R1:C5). The installer's message is `Only platform none and external supports
  1 ControlPlane and 0 Compute nodes`.
- On `baremetal`, `apiVIPs` and `ingressVIPs` are required. Each list holds at
  most two addresses inside a machine network, and the API address may not
  equal the ingress address unless `loadBalancer.type` is `UserManaged`
  (R1:C15).
- The agent installer in the 4.20 to 4.22 branches never forwards
  `platform.baremetal.loadBalancer` (RV correction 2; R1:C16): `loadBalancer.type:
  UserManaged` is dropped and keepalived is deployed anyway. The installer's
  development branch (`main`) does forward it (RV R1:C16). Through the
  installers read, an external load balancer therefore means platform `none`;
  modelling it only as `none` is Bootwright's choice, not a claim about every
  release.

## Platform none and external

- `none` and `external` are user-managed networking. They need external DNS,
  reverse (PTR) records and a layer-4 load balancer on 6443, 22623, 443 and 80,
  and the assisted DNS validations run for them (R1:C13).

## Keepalived and the free-address check

- Installer-owned keepalived uses unicast peers, a `virtual_router_id` and
  `advert_int 1` (R5:R5-33).
- The assisted agent verifies each virtual address is free with
  `nmap -sn -n -oX - <vip>` (R5:R5-35).
- A load-balancer-held address needs an interface that answers ARP for it:
  binding HAProxy to the address alone left it unreachable (observed by the v1
  implementation (main-v1); re-verify on this tree). An installer-owned address
  must not be assigned on the host. v1's attach role is not a requirement.
