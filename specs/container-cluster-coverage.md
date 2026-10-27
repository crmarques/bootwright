# OpenShift cluster coverage and qualification

This page owns M3's provider coverage: the provider paths, the topologies and
features each must cover, the item and qualification behind every row, and the
acceptance profiles. [Container clusters](container-clusters.md) owns lifecycle
behavior, [container-cluster admission](api/container-clusters.md) owns public
intent, [substrates](substrates.md) owns machine realization, and
[M3](milestones/m3.md) owns delivery and sequencing. Rows describe day-0
bootstrap, never an ongoing day-2 control plane
([B106](milestones/backlog.md#b106)). A row not yet delivered refuses before
any effect through the [refusal table](container-clusters.md#refusal-table),
never silently omitted.

## Release evidence

The operator authors an exact `distribution.release.version`, and the
installer of that release judges it (D157). Bootwright keeps no release
catalog, no approved version and no compatibility statement between OpenShift,
its installer, its clients or a substrate (D163). One general rule holds for
every release minor, as the [topology](api/container-clusters.md#topology)
rule does for control-plane counts; where a minor's native mechanics differ, a
registered version specialization adapts the mechanics with its evidence and a
parity test in the
[version-specialization catalog](../.agents/knowledge/version-specializations.md),
and never approves, refuses or recommends a release (D162).

Each acceptance run records the release it installed, the build commit, each
phase's duration and every native fact it observed. That record is the
evidence for the rows its profile covers, on that release; it certifies no
other release or feature. Hardware floors the agent image enforces, such as a
single-node cluster's CPU count, differ between minors and are evidence, never
a Bootwright rule. An agent image built for installation carries tokens that
do not expire, so no image-age rule exists; the 48-hour tokens belong to the
day-2 worker flow ([B739](milestones/backlog.md#b739)).

## Provider paths and topologies

| Path | Machines and controller | Media delivery | Platforms | Submilestones |
| --- | --- | --- | --- | --- |
| libvirt+sushy | libvirt guests, each behind its own emulated Redfish controller on the provider host | fetched from the controller's artifact server, a private token per insert | `none` for one node; `baremetal` with installer VIPs; `none` behind an external load balancer | M3b, M3c, M3d |
| Physical bare metal | physical servers through their BMCs | fetched over a verified controller leg, private delivery | `baremetal` with installer VIPs; `none` behind an external load balancer | M3d |
| OpenShift Virtualization | KubeVirt VirtualMachines on an existing host cluster named by `kubeconfigRef`, later one the context builds | pushed through the host's CDI upload proxy | derived `none` behind an external load balancer; authored `baremetal` with installer VIPs; `none` for one node | M3e |
| VMware vSphere | VMs Bootwright creates through vCenter | pushed to a staging datastore | `vsphere` with installer VIPs; `none` behind an operator load balancer; `none` for one node | M3f |

A cluster's nodes are all on one path; a mixed cluster is parked
([B747](milestones/backlog.md#b747)), and so is one virtual cluster across
several libvirt hosts ([B743](milestones/backlog.md#b743)). On every path a
VIP-bearing platform takes installer-owned endpoints only, because the agent
installer forwards no user-managed load balancer to the service that installs
the cluster; modelling an external load balancer only as `none` is
Bootwright's choice, not a claim about every release
([knowledge](../.agents/knowledge/openshift-agent-vips-and-platforms.md)).

| Topology | Shape | Item | Submilestone |
| --- | --- | --- | --- |
| Single node | one master, no compute, `platform: none` | [B61](milestones/m3.md#b61) | M3b |
| Compact | three schedulable masters | [B66](milestones/m3.md#b66) | M3c |
| Any size | three, four or five masters, with workers and infra nodes, up to the derived bound | [B714](milestones/m3.md#b714) | M3d |
| Two masters and an arbiter | `arbiter.replicas: 1` | [B717](milestones/m3.md#b717), Needs definition | M3d |
| Two nodes with fencing | `controlPlane.fencing` | [B737](milestones/backlog.md#b737), parked | none |
| Dual stack | two address families | [B738](milestones/backlog.md#b738), parked | none |
| OKD | another distribution | [B748](milestones/backlog.md#b748), parked | none |

A cluster's size is bounded by its installation deadline alone
([cluster size](container-clusters.md#cluster-size)), the derived deadline
under the 6-hour ceiling. A profile below samples that range; none is a
product limit.

Not yet met: a cluster of more than 9 nodes, a physical node, a KubeVirt node
and a multi-node vSphere cluster refuse; tracked as
[B714](milestones/m3.md#b714), [B67](milestones/m3.md#b67),
[B88](milestones/m3.md#b88) and [B733](milestones/m3.md#b733).

## Feature matrix

The Item column names the item that owns the implementation and its evidence.
Qualification names the tier and the run that proves the row.

| Feature | Native mechanism | Contract owner | API | Item | Submilestone | Qualification |
| --- | --- | --- | --- | --- | --- | --- |
| Single-node cluster | agent installer, rendezvous at the node | [container clusters](container-clusters.md) | one `master` node; `node` endpoint sources | [B61](milestones/m3.md#b61) | M3b | emulated rehearsal |
| Interruption, continuation, unknown resolution, destroy | `agent wait-for` re-run against the asset directory | [container clusters](container-clusters.md) | none | [B61](milestones/m3.md#b61), [B713](milestones/delivered.md#b713) | M3b, M3c | drills in each emulated rehearsal |
| Exact release pin, pre-releases included | the installer binary is the release pin | [container-cluster admission](api/container-clusters.md) | `spec.distribution.release.version` | [B307](milestones/delivered.md#b307), [B680](milestones/delivered.md#b680) | M3b | in-tree |
| Controller provides `nmstatectl` | the image build runs the controller's NMState generator per host | [controller](controller.md) | none | [B706](milestones/m3.md#b706) | M3b | in-tree; B61's run |
| Controller resolves cluster names | `getent ahosts` through the host's name-service configuration | [container clusters](container-clusters.md#preflight) | none (operator prerequisite) | [B705](milestones/m3.md#b705) | M3b | in-tree; B61's run |
| Libvirt capacity for cluster guests | libvirt and host capacity observation | [substrates](substrates.md) | `capacityReserve` | [B700](milestones/delivered.md#b700) | M3b (M1c item) | in-tree; B61's run |
| GitOps handoff readiness | engine records | [state reconciliation](state-reconciliation.md#bootstrap-completion-and-gitops-readiness) | `status` | [B71](milestones/delivered.md#b71) | M3b | in-tree |
| Compact cluster with installer VIPs | `platform.baremetal` `apiVIPs` and `ingressVIPs`, keepalived | [container clusters](container-clusters.md) | three masters; `openshift` sources | [B66](milestones/m3.md#b66), [B713](milestones/delivered.md#b713) | M3c | emulated rehearsal |
| External load balancer on a multi-node cluster | `platform: none`, user-managed networking | [platform union](api/container-clusters.md#platform-union-and-derivation) | `external` or `loadBalancer` sources | [B707](milestones/delivered.md#b707) | M3c | in-tree; live with the first profile that declares one |
| Endpoint address uniqueness | none (a graph rule) | [endpoints](api/container-clusters.md#endpoints) | `spec.install.endpoints` | [B708](milestones/delivered.md#b708) | M3c | in-tree |
| VIP free before boot | the assisted service's VIP check | [container clusters](container-clusters.md) | none | [B709](milestones/m3.md#b709) | M3c | in-tree; B66's run |
| Per-insert boot-image token | Redfish `InsertMedia` image URL | [container clusters](container-clusters.md) | none | [B314](milestones/delivered.md#b314) | M3c | in-tree; B66's run |
| Booted node answers | the live image's sshd and its assisted service on port 8090 | [container clusters](container-clusters.md) | none | [B710](milestones/m3.md#b710) | M3c | in-tree; B66's run |
| Unready host diagnosis | assisted-service host status and validations | [container clusters](container-clusters.md) | none | [B260](milestones/m3.md#b260) | M3c | in-tree; B66's drill |
| Completion before media release, stranded rendezvous host | assisted host `error` recovery | [container clusters](container-clusters.md) | none | [B711](milestones/delivered.md#b711) | M3c | in-tree |
| One installation at a time per context | an exclusive plan key | [container clusters](container-clusters.md) | none | [B712](milestones/delivered.md#b712) | M3c | in-tree |
| Workers, infra nodes, four and five masters | `compute[0].replicas`, `controlPlane.replicas`, host `role` | [container clusters](container-clusters.md) | `spec.nodes[].role` | [B714](milestones/m3.md#b714) | M3d | in-tree goldens; lab-anysize |
| Size beyond nine nodes | per-arm fan-out, a deadline by rounds, plan bounds | [cluster size](container-clusters.md#cluster-size), [plan bounds](state-reconciliation.md#plan-and-execution) | none | [B714](milestones/m3.md#b714), [B715](milestones/delivered.md#b715) | M3d | in-tree; lab-anysize |
| A node that never registers | the installation starts only when every declared host is known | [container clusters](container-clusters.md) | none | [B714](milestones/m3.md#b714), [B260](milestones/m3.md#b260) | M3d | emulated drill |
| Infra placement | none at install time; labels and taints through the API | [container clusters](container-clusters.md) | `spec.nodes[].labels`, `taints` | [B716](milestones/m3.md#b716) | M3d | Needs definition (Q11); lab-anysize infra row |
| Two masters and an arbiter | `arbiter.replicas: 1`, host `role: arbiter` | [topology](api/container-clusters.md#topology) | none yet | [B717](milestones/m3.md#b717) | M3d | Needs definition (Q17); lab-anysize arbiter variant |
| Physical nodes | Redfish virtual media, BMC trust, private delivery | [container clusters](container-clusters.md) | Machine BMC | [B67](milestones/m3.md#b67), [B251](milestones/m3.md#b251) | M3d | in-tree; emulated with a CA-issued leaf; real hardware [B730](milestones/m3.md#b730) |
| Bonds, VLANs, addressless NICs | agent `networkConfig` NMState, copied to the installed node | [container clusters](container-clusters.md) | NetworkConfig `spec.nmstate` | [B718](milestones/m3.md#b718) | M3d | in-tree goldens; lab-anysize; optional physical row |
| Second NIC on another libvirt network | one domain interface per NIC | [machines](api/machines.md) | `network.interfaceAttachments` | [B701](milestones/delivered.md#b701) | M1c, with M3d | in-tree; lab-anysize |
| Declared MTU | libvirt `<mtu>`, NMState `mtu` | [machines](api/machines.md) | attachment `mtu` | [B702](milestones/delivered.md#b702) | M1c, with M3d | in-tree; lab-anysize |
| Cluster network MTU and gateway mode | the `Network` `cluster` install-time manifest | [container clusters](container-clusters.md) | `spec.networking.ovnKubernetesConfig` | [B719](milestones/m3.md#b719) | M3d | in-tree goldens; lab-features |
| TPM2 disk encryption | `diskEncryption` in `agent-cluster-install.yaml` after `agent create cluster-manifests` | [container clusters](container-clusters.md) | `spec.security.diskEncryption` | [B720](milestones/m3.md#b720) | M3d | in-tree; lab-features |
| Installation proxy | install-config `proxy` | [container clusters](container-clusters.md) | `spec.install.proxy` | [B721](milestones/m3.md#b721) | M3d | in-tree; lab-features |
| FIPS | `fips: true`, the installer the release publishes as FIPS-capable, on a controller whose `fips_enabled` reads 1 | [container clusters](container-clusters.md) | `spec.security.fips` | [B722](milestones/m3.md#b722) | M3d | Implemented in-tree; lab-fips acceptance remains |
| Serving certificates | `APIServer`, `IngressController`, TLS Secrets | [container clusters](container-clusters.md) | `spec.install.servingCertificates` | [B723](milestones/m3.md#b723) | M3d | Needs definition (Q16); a probe run; lab-features serving-certificates row |
| Registry mirrors | `imageDigestSources`, a mirror pull-secret entry | [container clusters](container-clusters.md) | `spec.install.registries` | [B724](milestones/m3.md#b724) | M3d | in-tree; lab-features |
| Trust bundle policy | `additionalTrustBundlePolicy` | [installation trust](api/container-clusters.md#additional-installation-trust) | none yet | [B195](milestones/delivered.md#b195) | M3d | Needs definition (Q13) |
| Disconnected installation | mirrors and an offline build | [container clusters](container-clusters.md) | `spec.install.mode: disconnected` | [B69](milestones/m3.md#b69) | M3d | Needs definition; lab-disconnected, emulated with M1d's B68 |
| IPv6 single stack | IPv6 VIPs, networks and static NMState | [networking](api/container-clusters.md#networking) | `spec.networking` | [B725](milestones/m3.md#b725), [B703](milestones/m1.md#b703) | M3d | in-tree; lab-features IPv6 row |
| Localnet secondary networks | NNCP `ovn.bridge-mappings`, `ClusterUserDefinedNetwork` `Localnet` | [container clusters](container-clusters.md) | `spec.networking.secondaryNetworks` | [B726](milestones/m3.md#b726) | M3d | in-tree; lab-networks |
| Attachments for pods and VMs | the NetworkAttachmentDefinition a CUDN renders | [container clusters](container-clusters.md) | through secondary networks | [B726](milestones/m3.md#b726) | M3d | lab-networks pod and VM rows |
| NMState operator | OLM `kubernetes-nmstate-operator`, `NMState` | [add-ons](add-ons.md) | built-in package `nmstate` | [B735](milestones/m4.md#b735) | M4b, for M3d | package qualification; addons-sno; lab-networks |
| MetalLB operator | OLM `metallb-operator`, `MetalLB` | [add-ons](add-ons.md) | built-in package `metallb` | [B84](milestones/m4.md#b84) | M4c, for M3d | package qualification; lab-networks |
| MetalLB pools over L2 | `IPAddressPool`, `L2Advertisement` | [add-ons](add-ons.md) | the MetalLB package's configuration in its binding | [B84](milestones/m4.md#b84), [B727](milestones/m4.md#b727) | M4c, for M3d | in-tree; lab-networks |
| MetalLB on a secondary interface | host routing and the cluster's `ipForwarding: Global`; the documentation's per-release forwarding procedures are evidence | [add-ons](add-ons.md) | binding configuration `l2.interface`; cluster `routingViaHost` and `ipForwarding` | [B727](milestones/m4.md#b727), [B758](milestones/m4.md#b758) | M4c, for M3d | in-tree; lab-networks; a parity test and a further row only if a run registers a specialization |
| Lent administrator kubeconfig | a custody entry lent as a material file | [produced material custody](state-reconciliation.md#produced-material-custody) | none | [B64](milestones/delivered.md#b64) | M3d | in-tree journeys |
| Substrate state, media and power port | per-arm entry points over Redfish, the Kubernetes API or vCenter | [adapter boundary](substrates.md#adapter-boundary) | none | [B704](milestones/delivered.md#b704) | M1g, with M3e | conformance suite; the next lab-sno and lab-rhel runs |
| KubeVirt node VM | VirtualMachine `runStrategy: Manual`, root DataVolume through `spec.storage` | [substrates](substrates.md) | `spec.kubevirt`, profiles | [B87](milestones/m1.md#b87) | M1g | adapter suites; lab-kubevirt |
| KubeVirt image delivery | upload DataVolume, `virtctl image-upload --no-create`, SATA `cdrom` with `bootOrder: 2` | [substrates](substrates.md), [boot media](container-clusters.md#boot-media) | none | [B87](milestones/m1.md#b87), [B88](milestones/m3.md#b88) | M3e | fake-port tests; lab-kubevirt push drill |
| KubeVirt node network | `bridge` binding to a `Localnet` CUDN with IPAM disabled or a `bridge` NAD | [substrates](substrates.md) | `networkAttachments[].kubevirt.networkRef` | [B87](milestones/m1.md#b87) | M1g | preflight tests; lab-kubevirt |
| KubeVirt cluster on derived `none` | an external layer-4 load balancer and DNS | [container clusters](container-clusters.md) | `external` or `loadBalancer` sources | [B88](milestones/m3.md#b88) | M3e | goldens; optional row (Q19) |
| KubeVirt cluster on authored `baremetal` | keepalived on shared layer 2; a Localnet network with `ipam.mode: Disabled` filters by MAC alone (4.22), and a bridge NAD needs `macspoofchk` off | [container clusters](container-clusters.md) | `platform.type: baremetal`, `openshift` sources | [B88](milestones/m3.md#b88) | M3e | goldens; [B731](milestones/m3.md#b731) |
| Host cluster through `kubeconfigRef` | a materialized kubeconfig and a verified server | [substrates](substrates.md), [security](security.md) | `spec.kubevirt.kubeconfigRef` | [B87](milestones/m1.md#b87) | M1g | negative kubeconfig tests; lab-kubevirt |
| Host cluster the context builds | a lent kubeconfig and nested edges | [state reconciliation](state-reconciliation.md) | `spec.kubevirt.hostClusterRef` | [B732](milestones/m3.md#b732) | M3e | Needs definition; lab-kubevirt-nested |
| KubeVirt removal | VMI absent, delete with `preconditions.uid`, VM, DataVolumes and claims proved absent | [substrates](substrates.md) | none | [B87](milestones/m1.md#b87), [B88](milestones/m3.md#b88) | M3e | adapter suites; lab-kubevirt destroy |
| vSphere node VM | created powered off, EFI, `disk.EnableUUID` `TRUE`, the manual MAC range | [substrates](substrates.md) | `spec.vsphere`, profiles | [B99](milestones/m1.md#b99) | M1f | adapter suites; lab-vsphere |
| vSphere image delivery | datastore upload, CD-ROM insert, forced power cycle | [substrates](substrates.md), [boot media](container-clusters.md#boot-media) | `spec.vsphere.isoStaging` | [B99](milestones/m1.md#b99), [B733](milestones/m3.md#b733) | M3f | fake-port tests; lab-vsphere |
| vSphere platform with VIPs | `platform.vsphere` `apiVIPs` and `ingressVIPs` | [installer inputs](container-clusters.md#installer-inputs) | `platform.type: vsphere` | [B733](milestones/m3.md#b733) | M3f | goldens; lab-vsphere |
| vSphere in-cluster credentials | `vcenters` and `failureDomains` embedded in the image | [platform union](api/container-clusters.md#platform-union-and-derivation) | `platform.vsphere.vcenterCredentials` | [B733](milestones/m3.md#b733) | M3f | substitution and preflight tests; runs per Q22 |
| vSphere behind an operator load balancer | `platform: none` | [container clusters](container-clusters.md) | `platform.type: none` | [B733](milestones/m3.md#b733) | M3f | goldens; optional row |
| vSphere removal | destroy by managed-object reference; CSI volumes retained | [substrates](substrates.md) | none | [B99](milestones/m1.md#b99), [B733](milestones/m3.md#b733) | M3f | adapter suites; lab-vsphere destroy |

Every row covers create, observe, continuation of byte-identical input,
failed and ambiguous effects, readiness, access custody and removal. A changed
declaration over a completed apply is destroyed and applied again (D153).

Not yet met: every row whose item is open; the earliest are tracked as
[B61](milestones/m3.md#b61), [B66](milestones/m3.md#b66) and
[B714](milestones/m3.md#b714).

## Acceptance profiles

Profiles are created by the slices that implement them, beside
`examples/container-clusters/lab-sno`, and each names its node shape and the
controller or host budget an operator provides.

| Profile | Submilestone | Node shape | Budget | Item |
| --- | --- | --- | --- | --- |
| lab-sno ([example](../examples/container-clusters/lab-sno/README.md)) | M3b | one master guest of 8 vCPU, 16 GiB memory and a 120 GiB root disk on a NAT managed network, with the controller as provider host, artifact server and managed DNS, NTP and proxy host | the guest plus the provider's authored `capacityReserve`: the example authors a reserve of 1 CPU, 2 GiB and 10 GiB, so the gate needs at least 9 logical CPUs, 18 GiB available memory and 130 GiB free on the pool filesystem (the guest's 120 GiB root disk plus the reserve), beside about 4 GiB for the image's three copies (work area, publication, the emulator's volume) | [B61](milestones/m3.md#b61) |
| lab-compact ([example](../examples/container-clusters/lab-compact/README.md)) | M3c | three master guests of 6 vCPU, 16 GiB memory and a 120 GiB root disk (the enforced master floor is 4 CPU, 16 GiB and 100 GB) on one NAT managed network, an API VIP and an ingress VIP inside its prefix, emulated controllers on consecutive loopback ports, and the controller as provider host, artifact server and managed DNS and NTP host (Q7) | the three guests plus the authored reserve: with 4/8/40, at least 22 logical CPUs, 56 GiB available memory and 400 GiB free on the pool filesystem, beside about 7 GiB for five image copies | [B66](milestones/m3.md#b66) |
| lab-anysize | M3d | 3 masters of 8 vCPU, 16384 MiB, 120 GiB; 6 workers and 1 infra node of 4 vCPU, 8192 MiB, 120 GiB; each node a two-NIC `active-backup` bond plus an addressless NIC on a second managed network; variants of 5 masters with no compute, and 4 masters with 2 workers; an infra-labels row ([B716](milestones/m3.md#b716)) and a variant of 2 masters and 1 arbiter ([B717](milestones/m3.md#b717)) on X126's build | 52 vCPU, 104 GiB, 1.2 TiB sparse and about 16 GiB of media on one libvirt host | [B728](milestones/m3.md#b728) |
| lab-features | M3d | 3 schedulable masters of 8 vCPU, 16384 MiB, 120 GiB with `tpm: {}`, an installation proxy, an external mirror and serving certificates ([B723](milestones/m3.md#b723)'s row); an IPv6 single-stack variant | 24 vCPU, 48 GiB, 360 GiB | [B728](milestones/m3.md#b728) |
| lab-fips | M3d | lab-features' masters with `security.fips.enabled: true`, built on a controller whose `/proc/sys/crypto/fips_enabled` reads 1 | as lab-features | [B722](milestones/m3.md#b722) |
| lab-disconnected | M3d | 3 masters of 8 vCPU, 16384 MiB, 120 GiB on a managed network with no route to the internet, and a B68 Registry mirror | 24 vCPU, 48 GiB, 360 GiB, plus the registry's disk | [B69](milestones/m3.md#b69) |
| lab-networks | M3d | 3 masters and 2 workers of 8 vCPU, 16384 MiB, 120 GiB; one NIC on the machine network, one addressless NIC on a second managed network (the localnet uplink), and one NIC with a static address on a third managed network; one localnet secondary network; one MetalLB pool on the machine network and one advertised on the third NIC, the cluster declaring `routingViaHost: true` and `ipForwarding: Global`; one run per forwarding variant that a run proves differs | 40 vCPU, 80 GiB, 600 GiB, nested virtualization for the VM row | [B729](milestones/m3.md#b729) |
| lab-physical | M3d | at least 3 physical masters and 2 physical workers through their BMCs; an emulated rehearsal of the same path through sushy-tools serving B496's CA-issued leaf | the controller only, as artifact server, DNS and NTP host: 4 vCPU, 16 GiB, 100 GiB free | [B730](milestones/m3.md#b730) |
| lab-kubevirt | M3e | 3 masters and 2 workers, each 8 vCPU, 16384 MiB, 120 GiB root, no data disk, no TPM; one NIC per node on a `Localnet` CUDN (`ipam.mode: Disabled`) or a bridge NAD in the provider namespace, routed to the controller; authored `platform.type: baremetal` with installer VIPs (`openshift` sources). Variant lab-kubevirt-external: derived `platform.type: none`, `external` endpoints on an operator load balancer, DNS records for `api`, `api-int`, `*.apps` and each node | controller: one agent image (about 1.3 GiB) in the work area and five pushes through the CDI upload proxy; host: about 40 vCPU, about 82 GiB with virt-launcher overhead (0.002 x 16384 MiB + 218 MiB + 8 MiB x 8 per VM, plus 16 MiB per graphics device; approximate), 600 GiB of root claims plus five 4 GiB image claims (with CDI scratch); the variant adds the operator's layer-4 load balancer | [B731](milestones/m3.md#b731) |
| lab-kubevirt-nested | M3e | a 3-master host cluster the context builds on libvirt, with the OpenShift Virtualization add-on and nested virtualization, hosting a 3-master child cluster | the libvirt host carries the host cluster's masters and the child's nested VMs; [B732](milestones/m3.md#b732)'s definition fixes the figures | [B732](milestones/m3.md#b732) |
| lab-vsphere | M3f | 3 masters and 2 workers, each 8 vCPU, 16384 MiB, 120 GiB thin root, EFI, no TPM; one vCenter, one datacenter, one failure domain, one portgroup; `platform.type: vsphere` with installer VIPs; in-cluster credentials as Q22 decides. Variant lab-vsphere-external: authored `platform.type: none`, `external` endpoints on an operator load balancer | controller: one agent image and five datastore uploads (about 6.5 GiB on the staging datastore until release); vCenter: 40 vCPU, 80 GiB, 600 GiB on the failure domain's datastore, and the provisioning role of [B99](milestones/m1.md#b99); the variant adds the operator's load balancer and DNS | [B734](milestones/m3.md#b734) |

## Qualification rules

- **Tiers.** In-tree tests qualify contracts, refusals and recovery and never
  run a native installer. M3b and M3c exit on emulated rehearsals on
  libvirt+sushy, M3d on emulated rehearsals and a real-hardware run through
  BMCs, M3e on a run on an existing OpenShift Virtualization host cluster and
  M3f on a run on a vSphere environment.
- **Rows.** Each operator gate is an owner-accepted [ledger](../docs/acceptance.md)
  row from a build that contains its item's slices, recording the release
  installed, each phase's duration, its interruption drills, a settled apply
  that changes nothing, the exported administrator kubeconfig read with an
  independently installed client, and a destroy that proves what it owns
  absent. The public row summarizes these as the ledger defines it; the fields
  it does not carry stay in the private evidence archive.
- **Versions and evidence.** The shared
  [Versions and Evidence, not approval](../docs/acceptance.md#shared-qualification-rules)
  rules apply unchanged.
- **Unverified native facts.** A fact the research left uncertain, or one only
  the earlier implementation observed, is re-verified by the first run that
  meets it and recorded in knowledge: bridges in agent `networkConfig`, bond
  members by permanent MAC, interface renaming on the live image, serving
  certificates at bootstrap, the agent image under KubeVirt Secure Boot, VRRP
  between KubeVirt VMs on a localnet, `govc` datastore behavior, a blank
  disk falling through to the `cdrom` under the KubeVirt persistent boot order
  (an inference), the CDI upload proxy certificate being signed by the CA in
  `openshift-config-managed/default-ingress-cert`, a host in error stranding the
  rendezvous host, and a physical node's Redfish `TrustedModules` fields
  (`InterfaceType`, `Status.State`), read as observed by the earlier
  implementation.

## Left out

Designs M3 leaves out are parked items, each with its reason and the evidence
that would revive it: a Bootwright-owned controller resolver route
([B736](milestones/backlog.md#b736)), two-node fencing
([B737](milestones/backlog.md#b737)), dual stack
([B738](milestones/backlog.md#b738)), day-2 workers
([B739](milestones/backlog.md#b739)), install-time bridges and a custom
`br-ex` ([B740](milestones/backlog.md#b740)), bridge NADs and overlay UDNs
([B741](milestones/backlog.md#b741)), Tang encryption
([B742](milestones/backlog.md#b742)), one virtual cluster across libvirt hosts
([B743](milestones/backlog.md#b743)), MetalLB BGP
([B744](milestones/backlog.md#b744)), a Redfish facade over KubeVirt
([B745](milestones/backlog.md#b745)), one staged image per cluster
([B746](milestones/backlog.md#b746)), mixed-arm clusters
([B747](milestones/backlog.md#b747)) and OKD
([B748](milestones/backlog.md#b748)).
