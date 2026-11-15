# SNO case API and setup review

The [strict profile](isolation/runbook.md) is the entry point for enforced
staging/production isolation. Provisioning uses Bootwright declarations;
application policy and adversarial traffic remain native fixtures. Start with
fresh contexts and the build containing this case's managed DNS forwarding.
A complete replay remains an operator gate; source checks cannot promise a
successful installation on every host.

## Current ownership

| Setup | Declarative owner |
| --- | --- |
| Controller and SNO VMs, TPM2, encrypted root, FIPS, boot media and emulated BMCs | Machine, InfraProvider, MachineInstallProfile and OpenShiftCluster. |
| Management network, trunk and environment VLAN legs | Managed Network objects. The trunk is unaddressed; host VLAN legs have reserved addresses, with NAT and inter-leg routing disabled. |
| Resolver, cluster DNS, time, artifacts and listener firewall grants | NetworkConfig DNS references, DNSServer including suffix forwarding, NTPServer, ArtifactServer and managed Machine firewall policy. |
| Node VLAN bridges and egress eligibility | OpenShiftCluster secondary networks and node labels. NMState realizes the node network; no manual label or resolver mutation. |
| MetalLB operator, pools and interface advertisements | ClusterAddonBinding packages with exact OLM CSV pins and per-environment pool selection. |
| Ingress shards, EgressIP objects, default-router exclusion, namespaces, policies and node filter | Native OpenShift/Kubernetes manifests. These are cluster configuration and application policy, outside core provisioning. |
| External peers, Podman attachments, deliberate bypass routes, reboots and assertions | E2E harness. They exercise the boundary independently of provisioning. |

Operator steps still establish SSH trust, import private credentials and media,
select release pins, and preserve evidence. They are explicit trust inputs and
test operations. The case uses Bootwright file-copy and access commands for
remote transport.

## Alternatives and improvements

The existing [GitOps package](../../specs/api/addons.md#package-gitops) can hand
native policy to an approved HTTPS repository, pinned revision and path, with
an explicitly selected ClusterRole. Prefer that for persistent deployments.
The local rehearsal does not assume a repository or require another operator
solely to deliver its fixtures.

[Authored add-ons](../../specs/api/addons.md#executable-olm-add-ons) execute a
bounded OLM shape with namespaced custom resources. They cannot install these
cluster-scoped objects or patch the default router. `manifestSet` and authored
steps are admitted but not executable; wrapping shell commands in them would
misrepresent current support. Reusable ingress/egress policy belongs in a
GitOps layer or a deliberately scoped future add-on, with ownership, readiness
and removal contracts; it does not require a new core workload API.

Fully addressless host VLAN legs remain an optional
[candidate](../../specs/milestones/backlog.md#b1409). They are not required by
this managed case. The resolver gap is covered by
[DNS suffix forwarding](../../specs/api/infrastructure-services.md#proxy-dns-ntp-and-registry-deployment),
without adding general mutation of provided-host networking.
