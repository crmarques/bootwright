# Container clusters

Container cluster owns the installation of an OpenShift or OKD cluster from
declared intent, and the evidence that it completed. `spec.distribution.type`
selects the distribution and `spec.install.method` the installation method, and
the agent installation of OpenShift is the first implementation, the one this
page specifies
([implementation selection](architecture.md#implementation-selection)). The
[kind schema](api/container-clusters.md) owns `ContainerCluster`;
[substrates](substrates.md) own the Machines its nodes are bound to and the
management controllers they are booted through;
[infrastructure services](infrastructure-services.md#consumer-publication) own
the served root its boot media is published beneath; [Secrets](secrets.md) owns
the material it consumes.

One installation contract covers every substrate, as
[managed-OS installation](managed-os.md#installation) does. Each node's
[realized target](substrates.md#selection-and-refusal) supplies its substrate
arm, the management controller to boot through, the NICs the node reports, and
whether the machine is physical. The installation dispatches on the frozen arm
through the substrate's port entry points and fails closed on an arm it has
none for, before any media is inserted.

## Selection and refusal

Two blocks are planned for every selected `ContainerCluster`, and both belong
to the [`clusters` stage](state-reconciliation.md#stages-and-the-pause-boundary).

**Supported shape.** An OpenShift cluster declaring an exact
`distribution.release.version`, installed by the `agent` method in `connected`
mode through direct access, whose nodes are all virtual Machines on a realized
substrate with `os.provided: false` and no install profile. A single-node
cluster resolves its three endpoint slots from that node; a multi-node cluster
resolves them from authored or load-balancer addresses. Every other declaration
refuses before operation registration, in `plan` as in `apply`, with one
diagnostic whose object is the cluster and which carries why it is refused and
the remedy, as the [refusal table](#refusal-table) states row by row. A
multi-node cluster on `baremetal`, on `none` or with no declared platform is
accepted. A node whose realized target is physical installs through its
management controller under the trust its Machine declares, with the
[private delivery](#boot-media) rules below.

A virtual node whose provider host is not the Machine the selected artifact
server is placed on refuses because its emulated controller fetches the private
[boot image](#boot-media) without verifying the server. The placement, install-profile and physical-node refusals
and the two root-device refusals name the bound Machine in the remediation the
refusal carries, because it is what the operator changes. A node whose
[realized target](substrates.md#selection-and-refusal) does not derive, other
than on an unrealized substrate, refuses with the substrate's own reason and
remedy, which name the object to change. Node `labels` and `taints` reach no
installer input, because the agent installer has none for them; whether the
installation applies them once it completes is
[B716](milestones/m3.md#b716)'s open decision, and until then they are inert.

The following admitted fields are preserved in effective state and never change
an agent-install plan; each is inert or refuses at plan, as its row states:

| Field | Meaning for the agent installer |
| --- | --- |
| `spec.distribution.release.channel` | Informational, including a channel derived from the version; release selection uses the exact version or image. |
| `spec.install.endpoints.*.dnsName` | Inert; the installer derives every endpoint name from the cluster name and zone. Only `api-int`'s `dnsName` is read: admission refuses an API named certificate that carries it. |
| `spec.install.endpoints.*.port` | Refused at plan when authored with a value other than the native endpoint port the installer uses. |
| `spec.install.endpoints.*.scheme` | Refused at plan when authored with a value other than the native endpoint protocol the installer uses. |
| `spec.install.endpoints.*.prefixLength` | Refused at plan when authored; no installer input consumes it. |
| `spec.install.endpoints.*.interfaceNetworks` | Refused at plan when authored non-empty; no installer input consumes it. |
| `spec.nodes[].labels` | Inert until the node-label decision above is resolved. |
| `spec.nodes[].taints` | Inert until the node-taint decision above is resolved. |

Each authored field of this table except `channel` and the four endpoint
fields refused above makes `validate`, `plan` and `apply` report an
`api.deferred` warning at its path, so an operator learns that it changes
nothing. An authored endpoint `port`, `scheme`, `prefixLength` or
`interfaceNetworks` that the installer does not use refuses at `plan`, naming
the field, because the installation would not follow the declaration. Node
`labels` and `taints` stay warnings until [B716](milestones/m3.md#b716) decides
them.

Not yet met: no inert field reports a warning and an authored endpoint `port`,
`scheme`, `prefixLength` or `interfaceNetworks` plans without a refusal, so the
refusal table has no row for them yet; tracked as
[B807](milestones/backlog.md#b807).

A multi-node cluster's platform follows who answers its endpoints: on
`baremetal` the installer owns all three as virtual IPs its own VIP holders
answer, and on `none` an external or load-balancer address answers each, as
the [platform union](api/container-clusters.md#platform-union-and-derivation)
states.


### Refusal table

Each row is one refusal of both blocks' capabilities, and
`TestContainerClusterRefusalTableMatchesUnsupported` holds their `Unsupported`
to it. Its reason and remedy are the diagnostic's message and remediation, in
which `<cluster>` is the refused cluster, `<machine>` the Machine of the node
refused, `<provider>` and `<provider host>` that Machine's provider and the
Machine the provider runs on, and `<server>` and `<server host>` the selected
artifact server and its placement Machine.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| An agent OVN section | a node's composed `ovn` or `ovs-db` | `agent network configuration of <machine> contains an ovn or ovs-db section` | `declare OVN bridges through ContainerCluster secondaryNetworks` |
| An agent bridge interface | a node's bridge interface | `agent network configuration of <machine> contains an unqualified bridge interface <interface>` | `declare bridges through ContainerCluster secondaryNetworks` |
| An agent reserved interface | a node interface named `br-int` | `agent network configuration of <machine> uses reserved interface br-int` | `choose an interface name not reserved by OVN-Kubernetes` |
| An agent install interface off the default route | a node's install address and default route select different interfaces | `installation address of <machine> is not on its default-route interface` | `assign the installation address to the interface carrying its default route` |
| Ambiguous agent default-route interfaces | same-family default routes select different interfaces, regardless of metric or table | `installation address of <machine> has default routes on multiple interfaces` | `use one default-route interface for the installation address family` |
| A boot-image server off the controller | selected artifact server’s `spec.machineRef` differs from the controller Machine | `a cluster's boot image is built on the controller, so the server it is published through is placed there` | `set spec.machineRef on <server> to the controller Machine` |
| An OKD cluster | `spec.distribution.type: okd` | `this executable installs no OKD cluster` | `correct <cluster>` |
| A release pinned by image alone | `spec.distribution.release` without `version` | `a cluster pinned to a release image alone names no version for its installer to match` | `correct <cluster>` |
| A disconnected cluster | `spec.install.mode: disconnected` | `this executable installs no disconnected cluster` | `correct <cluster>` |
| Serving certificates | `spec.install.servingCertificates` | `this executable does not install install.servingCertificates` | `correct <cluster>` |
| Several nodes on another platform | `spec.install.platform.type: vsphere` or `external` with more than one of `spec.nodes` | `this executable installs no multi-node cluster on the declared platform` | `correct <cluster>` |
| More nodes than the run ceiling fits | more than 28 emulated `spec.nodes`, whose installation [deadline](#installation) passes the 6-hour ceiling | `installing <nodes> nodes needs a run deadline of <deadline>, past the 6h0m0s every adapter run is held to` | `declare at most 28 nodes on <cluster>` |
| A node with an install profile | `spec.os.installProfileRef` on a node's Machine | `a declared node selects an install profile, so two installations would write its disk` | `remove spec.os.installProfileRef from <machine> or drop it from <cluster>` |
| A node on an unrealized substrate | a node's Machine whose provider declares a substrate this executable does not realize | `a declared node is on a substrate this executable does not realize` | `correct <cluster>` |
| A KubeVirt node on a nested host | a node's KubeVirt provider names `hostClusterRef` | `a declared node's KubeVirt provider names hostClusterRef` | `select kubeconfigRef on <provider>` |
| A node whose emulated controller port does not allocate | a virtual node whose provider's `spec.libvirt.bmcEmulationDefaults.port` plus the node's position among that provider's Machines is past 65535 | `node <node> (<machine>): the Machine's emulated controller port does not allocate` | `correct spec.libvirt.bmcEmulationDefaults.port on <provider>` |
| A node's provider with no emulated controller credential | a virtual node whose provider declares no `spec.libvirt.bmcEmulationDefaults.auth.credentialsRef` | `node <node> (<machine>): the provider declares no emulated controller credential` | `set spec.libvirt.bmcEmulationDefaults.auth.credentialsRef on <provider>` |
| A physical node with an HTTP controller | a physical node's controller uses HTTP | `a physical node's private boot image requires an HTTPS management controller` | `set spec.hardware.management.bmc.address on <machine> to an HTTPS ComputerSystem endpoint` |
| A physical node with unverified controller TLS | a physical node disables controller TLS verification | `a physical node's private boot image requires a controller connection that verifies its certificate` | `enable spec.hardware.management.bmc.tls.verify on <machine> and declare its trust bundle when needed` |
| A physical node with unverified media TLS | a physical node selects disable-verification | `a physical node's private boot image requires virtual-media TLS verification` | `set spec.hardware.management.bmc.virtualMedia.tls.trust on <machine> to import-certificate or established` |
| A root device the installer cannot name | a node's `spec.os.install.rootDeviceHints.deviceName` other than `/dev/<name>` or `/dev/disk/by-path/<name>` | `the agent installer names a root device only as /dev/<name> or /dev/disk/by-path/<name>` | `set spec.os.install.rootDeviceHints.deviceName on <machine> to such a path` |
| A root device size the frozen input cannot carry | a node whose `minSizeGigabytes` exceeds 9007199254740991, the largest integer the frozen input carries exactly, at `spec.os.install.rootDeviceHints` on its Machine | `a node's minSizeGigabytes is larger than the frozen installer input carries exactly` | `declare spec.os.install.rootDeviceHints.minSizeGigabytes on <machine> as at most 9007199254740991` |
| A node off the artifact server's host | a virtual node whose provider's `spec.libvirt.machineRef` is not the selected server's `spec.machineRef` | `an emulated controller fetches the boot image without verifying its server, so the server is placed on the provider host that controller runs on` | `<machine> is booted through a controller on <provider host> and <server> is placed on <server host>; place <provider> and <server> on the same Machine` |

A row lifts with the code of the item that delivers what it refuses, and the
table and its test change in that commit:

- [B723](milestones/m3.md#b723) lifts the serving-certificate row.
- [B69](milestones/m3.md#b69) lifts the disconnected-cluster row.
- [B88](milestones/m3.md#b88) lifts the unrealized-substrate row for a node on
  a KubeVirt provider, and [B733](milestones/m3.md#b733) for a node on a
  vSphere provider and the multi-node `vsphere` platform row.
- [B743](milestones/backlog.md#b743) lifts the row for a node off the artifact
  server's host, and [B748](milestones/backlog.md#b748) the OKD row.

Each other row stays until an item names it.

A physical node's controller imports the artifact server's certificate under
`import-certificate`, its default, and the installation carries that trust
through the cluster role exactly as the managed-OS installation does: the
controller leg's bundle to the pre-boot, boot and disk-boot entry points, and
the virtual-media trust, the server's certificate part, private delivery and
the restore and removal settings to the insert and the eject. Because the image
is private, a physical node whose controller leg is `http` or does not verify,
or whose virtual-media trust is `disable-verification`, refuses before
registration, and an insert under `established` refuses where the manager's
deciding setting reads false.

### Cluster size

A cluster's size is bounded by its installation [deadline](#installation)
alone. Its nodes' per-node work runs at a fan-out frozen per provider kind, four
nodes at a time behind emulated controllers and sixteen behind physical ones,
and a cluster takes the smallest fan-out among its nodes. With the budgets
below a cluster fits the 6-hour ceiling with up to 28 nodes behind emulated
controllers, and with `16 × floor(9,000 / (1,800 + m))` physical nodes, where
`m` is the physical round margin, at least 32 while `m` is at most 45 minutes.
A larger cluster refuses before registration, naming its deadline and how many
nodes fit.

The agent installer starts installing only when every declared host has
registered and passed its validations, so one node that never comes up holds
the whole cluster: the boot fails naming a node whose image never reached the
network, the stall diagnosis names one that never registered, and the cluster
never starts with fewer nodes than it declares. Repairing the node and
repeating the apply boots only the nodes not already running this cluster's
image; dropping the node is a changed input, so the operation is destroyed and
applied again.

### Nodes on KubeVirt and vSphere

A cluster whose nodes are all Machines on KubeVirt providers that name their
host through `kubeconfigRef`, or all Machines on one vSphere provider, is a
shape the product admits. Its nodes take pushed delivery, so the [boot media](#boot-media)
is built and not published, and the [installation](#installation) boots, reads
and releases each node through its arm's entry points. A cluster mixing such
nodes with nodes of another arm refuses, and so does a KubeVirt provider that
names `hostClusterRef`.

Platform. Each arm's derivation, and the attachment rule of a KubeVirt cluster
that authors `baremetal`, are in the
[platform union](api/container-clusters.md#platform-union-and-derivation). The
installer's keepalived holds the VIPs on the nodes' shared layer 2, which a
`Localnet` network with `ipam.mode: Disabled` permits by filtering by MAC alone
(4.22); a bridge NetworkAttachmentDefinition needs `macspoofchk` off.

These rules are evidence of what the 4.20 to 4.22 agent installer accepts and
does, not a support statement
([knowledge](../.agents/knowledge/kubevirt-agent-node-boot.md)).

Not yet met: a node on a KubeVirt provider still refuses as a node on an
unrealized substrate; a nested host is refused by lifecycle selection. Admission checks the homogeneous arm,
attachment kinds and endpoint sources, and installer input projections have
offline goldens. Target derivation, native port integration and published
bounds still require [B87](milestones/m1.md#b87); tracked as
[B88](milestones/m3.md#b88).

Not yet met: a node on a vSphere provider refuses as a node on an unrealized
substrate, and a multi-node `vsphere` cluster refuses as several nodes on
another platform; tracked as [B733](milestones/m3.md#b733).

## Installer inputs

Planning derives the complete installer input set from effective state alone,
and freezes its canonical bytes with the plan, so the plan digest covers
exactly the cluster this operation would install.

`install-config.yaml` carries the base domain from
[the container-cluster zone](api/environment.md#domains), the cluster name, the
control-plane and compute replica counts the node roster implies, the
[networking](api/container-clusters.md#networking) the graph resolves, the
platform the API derived, the installation proxy choice, and the additional
trust bundles the cluster selects. `agent-config.yaml` carries the rendezvous
address — the install address of the first master in node-name order — and one
host record per node: its declared node name, its installer role, every NIC the
realized target reports by name and hardware address, every root-device hint
the Machine declares, under its own name and with its declared type, and the
node's own network configuration with its install address applied and the name
servers it selects as its resolvers. The selected NTP servers become additional
time sources. Each selected name or time server is written as a managed
server's endpoint address or as an external server's declared `address`. The
installation accepts a time source that answers, from an upstream source or
from its own clock ([time service readiness](infrastructure-services.md#managed-network-services)),
and requires no synchronized time of it.

The attempt writes `install-config.yaml`, with the bound material substituted,
and `agent-config.yaml` in JSON's syntax, which is YAML whose every string is
quoted, because the installer's YAML reader reads a plain `1e3`, `0o17` or
`0987654321` as a number and a plain `y` or `n` as a boolean, and passes the
installer different text, such as the name `1000` for a cluster named `1e3`.
Every character outside printable ASCII is escaped, a code point above U+FFFF
as the YAML escape `\UXXXXXXXX` rather than JSON's surrogate pair, because that
reader refuses a raw DEL or C1 control and any surrogate escape. Every value
therefore reaches the installer with the type and the text the request froze or
the bound material holds.

One value is derived rather than authored, because the agent installer refuses
the alternative: a single-node cluster renders `platform: none` whatever
platform the API derived, since the installer accepts no bare-metal or vSphere
platform for one control-plane node and no compute nodes. A multi-node
`baremetal` cluster renders its `api` address as `apiVIPs` and its `ingress`
address as `ingressVIPs`, and no installer input names a load-balancer type:
the 4.20 to 4.22 agent installers forward none to the service that installs the
cluster, which deploys its own VIP holders on that platform whatever the input
says (the installer's development branch does forward one). Modelling an
external load balancer only as platform `none` is Bootwright's choice, not a
claim about every release. A multi-node `none` cluster renders no virtual IP.

On `vsphere`, `platform.vsphere` carries `apiVIPs` and `ingressVIPs` from the
slots. When the cluster declares
[in-cluster vCenter credentials](api/container-clusters.md#platform-union-and-derivation),
it also carries `vcenters` and `failureDomains` from its nodes' provider: each
vCenter with `server`, its `port` when declared, `datacenters`, and `user` and
`password` frozen empty and substituted by the attempt like the pull secret;
each failure domain the nodes use with `name`, `region`, `zone`, `server` and a
`topology` of `datacenter`, `computeCluster`, `networks`, `datastore`, `folder`
and, when declared, `resourcePool`. Without them neither is written, and the
installer's own placeholders leave the cluster without vSphere integration,
which configuring later is day-2 and outside this product. `nodeNetworking`
reaches no input, because the agent installer forwards only the VIPs, vCenters
and failure domains of a `vsphere` platform.

Not yet met: no input carries `platform.vsphere`; tracked as
[B733](milestones/m3.md#b733).

The frozen inputs carry no secret value. The pull secret, the cluster SSH
public key and each additional trust bundle are named as declarations in the
request and substituted into the input files by the attempt that writes them.
A cluster that selects additional trust bundles freezes an empty
`additionalTrustBundle` in `install-config.yaml`, and the attempt fills it with
the certificate part of each bundle's Secret, in the order the cluster selects
them, each trimmed of surrounding whitespace and joined to the next by one line
break, because the installer refuses a bundle holding anything after its last
certificate. The optional `additionalTrustBundlePolicy` is written unchanged
when selected as `Always` or `Proxyonly`, and refuses without bundle references.
When omitted, the installer applies its own default.

### Node networking

A node's network configuration carries `ethernet`, `bond` and `vlan`
interfaces, static addresses, routes and resolvers into `agent-config.yaml`.
Every bond member carries its permanent hardware address with `identifier:
mac-address`, because a bond member reports the bond's address while it runs;
no bond or VLAN carries one. An `ethernet` interface with both families
disabled is an addressless NIC the substrate realizes and the image lists like
any other. Interface MTUs reach the installer as declared. A node whose network
configuration holds an OVS or Linux bridge, an `ovn` or `ovs-db` section, or an
interface named as OVN-Kubernetes reserves refuses, and so does a node whose
install address is not on its default-route interface, because the installed
node builds `br-ex` from that interface on every boot. Same-family default
routes on distinct interfaces refuse regardless of metric or routing table;
multiple routes on the same interface remain supported. The agent renames a
logical interface name to the device's kernel name, so a node's `ethernet`
names are its kernel names once it is installed.

The name servers a node selects through its network's `dns` field become
`dns-resolver.config.server` in that node's host record. Admission refuses a
node whose composed network configuration authors `dns-resolver.config.server`
and also selects name servers, with an `api.invariant` diagnostic at
`nodes[i].machineRef` that directs the operator to keep one of the two on the
Machine. A node that authors `dns-resolver.config.server` and selects no name
servers reaches the installer with its authored list unchanged. A
[managed-OS installation](managed-os.md#refusal-table) refuses the same
combination under its own wording.

### Install-time manifests

The media block writes into `<work>/openshift/`, before it builds the image, the
manifests its frozen request carries: each a core-kind object, named
`bootwright-<nn>-<purpose>.yaml`, rendered from typed intent, never authored.
The admitted kinds are `MachineConfig` and the operator `Network` named
`cluster`. No operator custom resource is one, because the bootstrap control
plane retries a manifest whose kind it cannot map for 60 minutes after
bootstrap and then fails it; such objects are applied after installation by the
[cluster network block](#post-installation-network-configuration). A manifest
holding secret material names its Secret, and the attempt substitutes it.

Not yet met: MachineConfig and Secret-part manifest projections have no typed
source; tracked as [B719](milestones/m3.md#b719).

### Installation features

**Disk encryption.** The agent installer has no install-config field for it, so
the media block of an encrypted cluster runs `agent create cluster-manifests`,
sets `spec.diskEncryption.enableOn` (`all`, `masters` or `workers`) and
`spec.diskEncryption.mode` (`tpmv2`) in
`cluster-manifests/agent-cluster-install.yaml`, and then builds the image. Every
node of an encrypted role has a TPM 2.0: a libvirt node's profile declares one,
and a physical node's pre-boot proof reads one enabled.

**Installation proxy.** `install-config` carries the selected Proxy as
`proxy.httpProxy`, `proxy.httpsProxy` and `proxy.noProxy`, its credentials
substituted by the attempt, the choice's `noProxy` followed by every consumed
machine network and the cluster's own domain. A proxy's trust bundle must be
one the cluster selects. The image build reaches the release payload through the
controller Machine's own proxy choice.

**Registry mirrors.** `install-config` carries `imageDigestSources` as
authored. The selected mirror's credentials join the install pull secret as one
entry for its host, and its trust bundle must be one the cluster selects.

**IPv6.** A cluster whose networks are all IPv6 installs with IPv6 VIPs and
rendezvous address, and each static IPv6 interface states `dhcp: false` and
`autoconf: false`.

## Boot media

ContainerCluster `metadata.name` is at most 47 bytes: the longest block
prefix, `cluster-install-`, consumes 16 of the block identity's 63 bytes.
Admission refuses a longer name at `metadata.name`.

The block `cluster-media-<cluster>` produces the cluster's agent boot image. It
requires the managed `ArtifactServer` its `agent.redfishVirtualMedia` selection
names, because that is where the image is published, and the managed `Proxy`
its installation proxy selects, whose address its install-config carries, and
nothing else: the name and time services the cluster selects are baked into the
image as data rather than reached while it is built.

The image is built where the installer is, which is the controller: the
[controller stage](controller.md#the-controller-stage) installs a context's
clients there and nowhere else. A cluster whose selected artifact server is
placed on another Machine therefore refuses before registration rather than
building where no installer exists.

**The installer binary is the release pin.** The agent image embeds the release
payload compiled into the `openshift-install` the
[controller stage](controller.md#the-controller-stage) installed for the
declared release, and no later step reports which release was used. The attempt
therefore locates that exact executable in the sealed client area its own
context selected, reads the version it reports, and refuses before building
anything when that version is not the declared `release.version`, naming the
command that supplies the matching one. A build whose client area holds no such
executable refuses the same way.

**The image is built once, from frozen inputs.** The block owns one work area
on the artifact server's placement Machine,
`/var/lib/bootwright-clusters/<context>/<cluster>/`, reserved by path and
created `0700` because the installer retains inside it the material it was
given, which makes the area [native-side
retention](secrets.md#native-side-retention). The attempt writes the frozen input files there, substitutes the bound
material into them, and invokes the installer once. The installer consumes its
inputs into its own asset state and writes beside the image the access the
installation later needs, whose admin client certificate is the cluster's
[identity](#installation), so the area outlives the attempt that created it, is
never enumerated in evidence, progress output or a log, and is discarded and
rebuilt rather than reused whenever the inputs it was built from are not the
inputs frozen now, unless an installation has booted from it. The build
receipt also records the context's incarnation: the device and inode the
registry holds for the context's directory with that directory's birth time,
or its inode generation where the filesystem reports no birth time, since a
filesystem reuses a freed inode; a context directory with neither refuses
`lifecycle.state`. A build or a removal over an area whose receipt records
another incarnation, as a recreated context of the same name finds one its
predecessor left, refuses before any effect, naming the area and keeping it,
and the hand-over of a kept kubeconfig to custody before a removal refuses
naming such an area rather than skipping it. A receipt an earlier build wrote
records no directory identity and names no other incarnation.

**A work area an installation booted from is never rebuilt.** The area also
holds the state the [installation](#installation) boots its nodes into and the
installer's own copy of the administrator access to the cluster they install,
the only one until the installation block proves the installation complete,
and a rebuild starts by discarding it. The installation block therefore marks
the area with the file `.bootwright-booted` before it hands any node the image, whether or not that
attempt then boots one, and this block's observation reports the mark. An
attempt that finds the area marked and would build again, because no image
built there is published, no receipt there records what it was built from, or
the inputs or installer version it records are not the ones frozen now, refuses
before any effect, naming each reason and directing the operator to destroy the
cluster's installation first. A marked area whose published image is this
request's is replayed as below, and an unmarked area is rebuilt as before. The
mark leaves only with the area itself, in this block's inverse, which a
removal starts only once the installation's own inverse has completed, because
a removal [waits on its dependents](state-reconciliation.md#continuation-and-removal).

**Publication is private.** The image embeds the pull secret and the cluster
SSH key in its own ignition, so it is confidential on every substrate and is
published beneath the served root under the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication),
at `private/clusters/<cluster>/<token>/`, with the token minted by the attempt
and appearing in no frozen request, evidence, progress output or log. The block
owns `private/clusters/<cluster>/` as its reserved path. Its request names the
selected server's serving certificate Secret among its bindings, and its apply
hands the adapter that Secret's certificate part alone, as the authority the
fetch proving the publication verifies the listener against.

The installation block reaches that reserved path by file descriptor and
follows no symbolic link from the artifact server's content root down, so a
link anywhere there refuses every proof, insert and retirement of its links.
The directories above the content root may be symbolic links, so a controller
can move its services root onto a larger disk, but every directory the walk to
the content root passes through, each link resolved, must be owned by root and
writable by neither group nor other; any other layout refuses as a publication
that could not be proved, in apply, observation and removal alike. The media
block proves the same layout, by the one rule both blocks share, before it
creates, mints, publishes, relabels or removes anything: it refuses a symbolic
link from the content root down to its published image or an untrusted
resolved ancestor, so a layout the installation would refuse is never
published. Because its later tasks resolve paths again, it also requires
every existing directory from the content root down to be owned by root and
writable by neither group nor other
(`test_the_apply_proves_the_layout_before_it_includes_the_build`). A missing
directory beneath the content root is accepted, because the publication
creates it; a missing content root refuses a publication and leaves a removal
nothing to remove. Either block's refusal names the directory to fix, never a
URL or a token
(`test_a_refusal_names_the_directory_and_no_url_or_token`).

That contract requires the fetching controller to verify the serving
certificate. A physical management controller does so under the trust its
Machine declares. An [emulated controller](substrates.md#machine-realization)
does not: it fetches over HTTPS without verifying the server's certificate, so
on that substrate confidentiality rests on the token and on the fetch never
leaving the provider host. Selection enforces the second: a virtual node whose
provider host is not the Machine the artifact server is placed on refuses before
registration. This is the one exception, and it is recorded here rather than
implied. The [managed-OS installation](managed-os.md#refusal-table) of a
virtual Machine shares it, bounded the same way: a virtual Machine whose
provider host is not its image server's placement Machine refuses before
registration. It extends to no other consumer.

**Pushed delivery.** A cluster whose every node takes pushed delivery needs no
artifact server: the block builds its image in the work area
`/var/lib/bootwright-clusters/<context>/<cluster>/` on the controller Machine,
publishes nothing, and its evidence is the receipt and the image in that area.
The installation freezes the image's path and its media identity, and each
node's `media_insert` transfers that file from the controller to where the node
reads it: the host cluster's CDI upload proxy or the vCenter's datastore, each
over the [verified connection](security.md#network-remote-systems-and-privilege)
security owns. Nothing fetches the image, so no placement
exception applies; what bounds its confidentiality is the destination's own
access control: whoever can read claims in the KubeVirt provider's namespace,
or browse the vSphere staging datastore, can read the pull secret the image
embeds, and on vSphere the vCenter password when the cluster declares one,
until that node's media is released. A push-only cluster that selects
`agent.redfishVirtualMedia` refuses at admission, naming the field to remove.

Not yet met: the media block always publishes through an artifact server;
tracked as [B88](milestones/m3.md#b88).

**Reservations.** `path:` for the work area and `path:` for the private
publication subtree, so a second context refuses rather than taking either.

**Evidence.** Completion requires an image published beneath the frozen path
and the receipt its build left in the work area naming the digest of the inputs
it was built from and the installer version that built it, both equal to the
ones frozen now. The evidence carries no digest of the image itself, so
completion claims which inputs and which installer produced the image, not its
bytes. Replay reports `completed` without rebuilding when the published image
was built from the same inputs by the same installer version. It publishes
nothing, yet it applies the published image's mode and label again and proves
the publication by the same fetch through the listener a build makes, as the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication)
requires, before it reports: an image an earlier executable published with a
mode the serving process cannot read is repaired and reported changed, and one
the listener still refuses fails the block with its cause. The inverse
removes the published image and the private directory that held it, discards
the installer work directory, and proves each absent. That directory holds the
installation's kept administrator kubeconfig, which is never deleted while
custody holds none for the cluster (D124): before the inverse runs, when
custody holds no entry for the installation block's `kubeconfig`, the engine
runs this block's read-only observation with that one output declared, and the
role copies the kept kubeconfig there, private to root, when it is a regular
file of one byte up to the inspection's 64 KiB bound, whatever the
installation proved. The engine keeps it in custody under the installation
block and `kubeconfig`, marked unproved, before the inverse deletes anything
([produced material custody](state-reconciliation.md#produced-material-custody)). A removal's resolution
reads the observation for what the removal proves: neither the image nor the
work area present is its completion, the image this request describes is
positive no effect, and anything else of this block's own left is a positive
partial realization.

## Installation

The block `cluster-install-<cluster>` boots the nodes from that image and
watches the cluster install. It depends on the media block, and requires every
node `Machine` and the managed name and time services the cluster and its
nodes select, and the managed `Proxy` its installation proxy selects. An
external one is used at its declared `address` and adds no requirement,
because no block of this product realizes it.

**One installation at a time.** Every installation block of a context holds the
exclusive key `container-cluster-install`, so a context installs one cluster at
a time while its image builds may overlap: nodes of two clusters pulling their
release payloads over one uplink converge slower, and the assisted service may
move a host it holds past a stage timeout into error, which Bootwright treats
as stranding the installation (observed by v1; re-verify, in
[knowledge](../.agents/knowledge/openshift-agent-install-waits.md)).
Installations of different contexts on one controller are not coordinated.


A node whose realized target is
[physical](substrates.md#physical-machine-realization) makes this block consume
`data-loss` on **apply**: the agent installer writes the release image to that
node's disk, and that is the moment its existing content is lost. A cluster of
virtual nodes consumes nothing here, because their disks are created by their
realization and removed by its inverse. A physical node refuses before
registration, as [selection](#selection-and-refusal)
states; an operation that froze one refuses its apply at execution, naming the
node and directing the operator to destroy the operation and plan again, while
its destroy and observation still run.

**Resolution before boot.** The installer polls the cluster API from the
controller, so before anything is booted the block resolves the name of each
API, internal API and applications endpoint slot the plan froze an address for
(`api`, `api-int` or `console-openshift-console.apps`, then the cluster name
and the zone) through the controller's own resolver, for every address family
the controller is configured for. It proves that each name answers with its own
slot's address and nothing else, because any other answer is one the installer
may poll instead of the cluster. When the applications slot froze an address,
its name is a wildcard, so one more name beneath it is held to that address
too: its label is `bootwright-` followed by the cluster name, cut to 63
characters and to no trailing hyphen, so it is deterministic and names nothing
secret. A name that does not resolve, that resolves elsewhere, or that resolves
to its frozen address and any other fails the block naming each such name, its
answers and the address it must answer with, and nothing is booted.

Bootwright configures no resolver on the controller: routing each cluster zone
to the name server that serves it is the operator's prerequisite, as for any
[managed network service](infrastructure-services.md#managed-network-services).
[Preflight](#preflight) proves the route before an apply with the same
resolution, and the block's proof stays the gate.

**Boot.** Nodes are booted in rounds of the cluster's frozen fan-out, in
node-name order, each through
[its own substrate's boot operation](substrates.md#identity-and-power-operations).
Before anything is presented, each node the boot does not skip is proved
through its substrate's pre-boot proof, exactly as an OS installation proves
its target: a machine the substrate created by its controller answering with
the machine off, and a physical node against the declared NICs its install
request froze and the identity its Machine's
[bare-metal block pinned](substrates.md#physical-machine-realization) earlier in
the same operation. A node that fails the proof is neither presented the image
nor booted. Then the image is presented through the node's `media_insert`, the
published image on a fetching arm and the image file on a pushing one, the
substrate applies whatever boot selection its machine needs, and the machine is
powered on and polled to running. The media stays inserted: a live agent image
is still being read after the node answers on the network, and removing it
early corrupts the running installer. A node that fails a step fails the block
naming it once the other nodes of its round have finished.

**Virtual IPs are free before boot.** When the boot is a fresh one, so no
declared node is powered on, the block first proves that no host answers
a virtual IP of a multi-node `baremetal` cluster, for each such address inside a
managed libvirt network whose provider host is the controller: it sends
duplicate-address ARP probes from that network's bridge, three within three
seconds, and any reply fails the block naming the address, its endpoint slot
and the hardware address that answered, before the work area is marked or
anything is inserted. The assisted service refuses a virtual IP another host
answers only after every node has registered, and the installer reports that
as a timeout naming no address. Bootwright never assigns a virtual IP to a host
interface. An IPv6 virtual IP and one on any other network is not probed, and
the evidence names the addresses that were not probed. Once any declared node is
powered on, whether it runs this cluster's image or belongs to an installed
cluster whose media was released, nothing is probed: the cluster's own nodes
may answer its virtual IPs, and a running node without this cluster's image
refuses at its pre-boot proof.


**Each insert has its own token.** A management controller holds the URL it is
handed until its media is ejected, and an emulated one writes it to its journal
when a fetch fails, so on a node whose arm fetches its media, no node is handed
the media block's own publication. Before each such insert the block mints a
token of at least 32 bytes from the operating system's cryptographic random
source, publishes beneath `private/clusters/<cluster>/<token>/` a hard link to
the image the media block published, proves that link by the
[publication fetch](infrastructure-services.md#private-consumer-publication),
and inserts its URL. After the eject that releases the node, in release or in
this block's inverse, it removes that token's directory and proves it absent.
A link no controller presents is retired too, because a controller may have
journaled it: an insert that fails has its link retired in the same attempt
once its controller, read without being changed, answers presenting something
else, and every insert link no controller presents is swept when an apply has
read its nodes and before it boots any, when a release has ejected every node,
and in this block's inverse after its ejects, which keeps only the media of a
node whose eject failed. A link is presented when its token is a component of
the path of an image some controller reports, whatever host and port that image
names, because a physical controller may echo the image without its
non-default port; a token is too random for a foreign image to name it. The
sweep removes an unpresented link only when it is
a directory holding nothing but a file that is, by device and inode, the media
block's published image, or an empty directory an interrupted publication
left; any other entry named as an insert link is left in place and refuses the
block, once every provable link is removed, so that tampering with the
reserved path is never hidden. The sweep reports only how many links it
removed, kept and refused, and a refusal names those counts to the operator.
Observation never retires a link.
The token appears in no request, evidence, progress output or log. A node
presents this cluster's own image when the scheme and host of the image its
controller reports are the media block's publication's, and its path names a
file beneath `private/clusters/<cluster>/` that is the same file, by device and
inode, as the media block's published image; any other image is foreign. The
block writes only beneath the media block's reserved path, and that block's
inverse, which a removal orders after this one, removes whatever is left there.


**Booted nodes are proved to answer.** Once a node is booted, within what is
left of the boot budget, the block proves that the node answers a TCP
connection on port 22 at its installation address, which the live agent image
opens once it has applied the network configuration it found for the node's
hardware address, and then that the rendezvous address answers on port 8090,
where that image's assisted service listens, when the rendezvous node was
booted by this attempt. A node that has not answered by the boot deadline fails
the block as the boot budget spent, leading with each such node and its
address: it did not boot this cluster's image, or the image found no network
configuration for the hardware address it booted with. A repeated apply leaves
such a node as it is rather than booting it again, so the failure names those
causes instead of offering another boot. Without this proof the
installer's own wait watches for an hour a cluster that cannot form.


**Waiting.** The block waits for bootstrap completion and then for installation
completion, through the same installer that built the image. Each wait is a
read-only observation that starts nothing: the installer gives up on its own
compiled deadlines while the cluster keeps converging, so a give-up it can
resume from is re-invoked while that wait's own budget lasts, 30 seconds after
the last and at most 40 times. Every attempt runs under `timeout` with only
what the budget has left when that attempt starts, so the wait returns within
its budget and a 30-second grace: `timeout` sends the installer SIGTERM once
the budget is spent, and kills it with its process group once the grace has
passed too. An attempt the budget stopped, which exits 124, or 137 as a shell
reports the kill after the grace and -9 as the adapter does, fails the block as
the budget spent, with a message of its own that no give-up of the installer's
shares. It is resumable: the cluster keeps converging after its wait is
stopped, so repeating the apply watches it again under a fresh budget. A
give-up that proves the cluster stopped installing — a declared host the
assisted service moved into error — is never re-invoked, because the next
window would watch a state that cannot change; it fails the block naming the
host, what that state means and that recovery is a destroy and a fresh apply,
which is the only recovery Bootwright performs, even when the budget stopped
that attempt (the service's recovery was observed by v1 never to move the
rendezvous host out of the bootstrap control plane; re-verify, in
[knowledge](../.agents/knowledge/openshift-agent-install-waits.md)). A
give-up that proves a declared node never registered fails the block naming
that node, because the cluster waits for exactly the nodes the install
configuration declares. It does so even when the budget stops the attempt
re-invoked after that give-up: an attempt the budget stopped proves nothing
new, so when the last give-up the installer itself reported in that wait was a
stall, the block fails as that stall and adds that the budget stopped the
attempt after it. A wait is thus diagnosed as a host in error first, then as a
stall, then as the budget spent, and only then as the installer's own timeout.

The two installer give-ups a stall is recognized by, a cluster that stayed
ready without starting to install and one that failed to prepare its
installation, name no host, and the installer reports the hosts' events only
at debug level. So a bootstrap wait diagnosed as a stall, as the installer's own
timeout or as its budget spent, and no other, then reads once which hosts
registered with the assisted service on the rendezvous host, the only place
they can be read from: one request for the service's cluster with its hosts,
under `http://<rendezvous>:8090/api/assisted-install/v2`, carrying the watcher
token the installer keeps in its asset state in the work area, which travels in
cleartext ([security](security.md#network-remote-systems-and-privilege)). The
rendezvous address is the one that asset state names, and the read reaches it
only when the frozen request declares a node at that address. It follows no
redirect, reads no proxy setting, ends within 10 seconds of its start and
drops an answer past 16 MiB; it runs under `no_log`, and the token never
leaves the inspection that reads and sends it. The block then fails leading
with each declared node that no registered host carries the name of, as its
requested hostname or as the hostname its inventory reports, and says that the
cluster reached ready without the rendezvous node starting to install only when
the wait was diagnosed as a stall: after the installer's own timeout or a spent
budget the cluster held short of a declared node need never have reached ready,
so the block says only that installing starts once every declared node reports
known. It then names each declared
node whose host the service holds `insufficient`, `disconnected`,
`pending-for-input`, `preparing-failed`, `installing-pending-user-action` or
`error`, with that status, its status message and the identifiers of its
failing validations; the image sets a node's hostname to its declared name when
the node carries the MAC address of its first declared interface. A read that
fails, an answer that is not exactly one cluster whose hosts each carry a name,
or one with no missing or bad-state declared node names no node, and the block
fails with that classification's own hint: the stall's own message after a
stall, the spent budget's or the installer timeout's otherwise. The read comes after the wait, so it is no
part of the wait's budget, and the installation's deadline covers it.


**Budgets.** Each long phase of a cluster's two blocks is bounded in wall-clock
time by a budget its frozen request carries, never by a value the adapter
chooses: the media request's build budget of 1,800 seconds, and the install
request's boot budget, the per-round boot allowance of its slowest arm for each
round of its fan-out, never less than 900 seconds, and its bootstrap and
installation wait budgets of 5,400 seconds each. A round's allowance is 600
seconds behind emulated controllers and 1,800 behind physical ones; the
KubeVirt and vSphere arms publish theirs. The image build runs
under `timeout` with the build budget and the same grace; a build the budget
stopped fails the media block as the budget spent, publishes nothing, and is
resumable, because the next apply discards the work area and builds again.
The boot budget bounds booting every node together and proving that each
answers: its deadline is taken once, before the first round, and each boot
step, the substrate's own included, runs under ansible-core's task timeout with
only what is left of it, so a node already running from this cluster's image,
which the boot leaves alone, takes none of it. A step still running at the deadline is stopped there, and a node reached
after it is not booted; either fails the block as the boot budget spent, and a
repeated apply boots only the nodes not already running from the image. Each
proof that a node answers waits for what is left of the same deadline, but for
no less than one second, so a node checked after an earlier one spent the
deadline still gets one connection attempt and is named only when it does not
answer. The read of a node's controller after an insert it did not complete
runs under the media read's own bound plus 30 seconds rather than what is left
of the boot budget, so an insert that failed near the deadline still has its
link retired or the read's own failure named, rather than a censored timeout;
the block then fails, so the node's media read after boot does not run in that
attempt. The
task timeout ends the step but not the management-controller request the step
made, which ends within the adapter's own fixed bounds. Every read of the
cluster through its API passes oc's `--request-timeout` of 30 seconds, so each
request oc makes gives up rather than waiting on an API that accepted the
connection and never answers. Every run of either block is bounded by a
[deadline](architecture.md#the-adapter-result-protocol) derived from the
budgets its request froze: the build budget plus 30 minutes for the media
block, and for the installation its three budgets back to back plus 30 minutes
and one round margin for each round, which is 3 hours 56 minutes for up to four
nodes behind emulated controllers. A round margin is exactly the
[bounds](substrates.md#identity-and-power-operations) its arm publishes for the
calls the installation makes to a node outside the boot budget: its media read
in each of the two state reads, and the eject and the disk selection that
release its media, 11 minutes behind an emulated controller; a physical
controller's margin is the one [B67](milestones/m3.md#b67) derives. Those
bounds count the requests of a call's polls as answered at once. The waits do
not grow with the nodes, which join concurrently. A cluster whose installation
deadline would pass the runner's 6-hour ceiling refuses before registration,
naming that deadline and how many nodes fit, rather than being cut short part
way through its installation; [cluster size](#cluster-size) states the bound.

**The access it produces.** The installer writes the cluster administrator
kubeconfig and the initial administrator password into the work area when it
builds the image, beside the state it keeps there, and a completed installation
is what they then grant access to. That area is root-owned, `0700` and never
served. The installer rewrites its own kubeconfig in place: each run of
`agent wait-for install-complete` prepends the router CA to it again (see
Completion), so it grows past any bound, and a budget kill during that write
can leave it cut short. The installation therefore keeps its own copy of that
file in the same work area, and every read of the cluster, before and after
completion is proved, goes through the kept copy, never the installer's file.
Only an apply keeps it: before any effect, and again after its waits, it
takes the installer's file as the kept copy, in one rename and private to
root, when that file is whole, within the inspection's 64 KiB read bound and
names an identity, and the kept copy is missing or names that same identity.
A file cut short, one grown past the bound, or one naming another identity is
never kept, so the kept copy stays at the last whole rewrite within the bound,
and a kept copy that names no identity is never replaced. An observation keeps
nothing. An apply that then holds no kept copy naming an identity refuses
before any effect, naming the destroy that discards the work area as the
remedy, rather than booting nodes no read could prove. The installer's own
waits never read the kept copy: each `agent wait-for` loads the installer's
file itself and exits when it cannot. So before each wait attempt runs the
installer, the apply restores that file from the kept copy, in one rename and
private to root, when the file is a regular file within the 64 KiB bound that
names no identity, as every file a kill during the installer's write cuts short
does, and the kept copy still names the identity the apply took from it before
its first effect; the completion evidence then records the restore. When the
kept copy names no identity or another one, the attempt refuses before it runs
the installer, naming the same destroy, and writes nothing. A file that names
an identity, whichever it names, is the installer's to read. A file past the
bound is judged by its last 8 KiB alone: it is whole when it ends with a line
break right after the `client-key-data` line the installer's writer puts last,
and is then the installer's to read; otherwise it was cut short, and is
restored like a cut file within the bound. A missing file or one that is not a
regular file is never read and left as it is. A wait that restored the file and
then fails says so in its failure. Once an apply's attempt
proves the installation complete, or a resolution of an apply's block reads it
complete, including a destroy's resolution of an incomplete apply, the role
copies the kept kubeconfig into a private output file of its run and the
engine keeps it in the context's
[custody](secrets.md#produced-material), keyed by this block and `kubeconfig`,
before it records the block done; a copy of equal bytes publishes nothing but
the clearing of an unproved mark. This block's removal offers nothing. When no
completion was ever proved, as for an installation whose custody publication
failed and whose nodes were powered off before its destroy, the kept copy
enters custody only from the media block's removal, under the same entry and
marked unproved, before that removal deletes the work area. The custodied copy
is withdrawn when the context's removal completes, not with this block's
inverse, so a destroy that stops part way keeps the only access, and `cluster
kubeconfig` reveals it until then, with a warning when it is unproved
([administrator access export](cli.md#administrator-access-export)). The
initial administrator password stays in the work area. No copy is ever
written to evidence, progress output or a log.

**Releasing the media.** Once the read that follows the installation wait
proves the installation complete, as Completion below defines it with every
declared node present, and only then, each node's virtual media is ejected and
its controller is pointed at the installed disk through the substrate's
disk-boot entry point, which powers nothing on, in node-name order. An
installation wait that returns while a declared node is missing releases
nothing and fails the block naming each missing node. When that node is the
rendezvous host, the failure says that it still runs the bootstrap control
plane from the live image, because the assisted service stopped installing
after another host's error and its recovery never pivots that host, and that
recovery is a destroy and a fresh apply. Only a read in which every request
was answered, the cluster with this build's identity, names a node missing or
the rendezvous host stranded. When a request of that read fails, or the
cluster does not answer with this build's identity, the block also releases
nothing, and the failure says the cluster could not be read, that no media was
released, and that the remedy is to repeat the apply; it offers no destroy.
Any other missing node or incomplete installation keeps the media attached
and asks for the cluster to be inspected before the apply is repeated.


**Completion.** Completion requires the cluster answering with this build's
identity, reporting the release it was installed for and its installation
completed at that release, holding every declared node, with no node's
controller still presenting the media it booted from. The cluster reports its
installation completed only when its `ClusterVersion` condition `Available` is
`True` and the newest entry of its update history, `status.history[0]`, is
`Completed` at the declared release. The cluster opens that entry `Partial` when
it starts and marks it `Completed` once the whole release is applied, so a
cluster still installing reports no completion, and neither does an installer
that exited or an answer that lacks either field. The
identity is a domain-separated SHA-256 of the client certificate in the
administrator kubeconfig. That certificate is signed by a signer minted when the
image was built, so it names the cluster that image installs and no other.
`agent wait-for install-complete` rewrites the kubeconfig once the cluster
initializes, and again on each later run, prepending the router CA bundle to its
certificate authority and adding `apiVersion` and `kind`, but leaves the client
certificate unchanged, so each rewrite leaves the identity unchanged. Each run
grows the file by the encoded bundle, about 3 KiB when it is the ingress
operator's RSA-2048 wildcard certificate and CA, so it passes the 64 KiB the
inspection reads after roughly 18 rewrites; the kept copy stops following it
there and keeps naming the identity. The agent installer records no other
identity. The identity is taken from the kept copy alone. An installation
without one names none, and so does a kept copy that exceeds that bound, is
not whole (it lacks its final newline, its client key or the end line of a
certificate or key it embeds), is in neither form the installer writes, lacks
an embedded certificate authority, disables verification or authenticates any
other way. The cluster answers with the identity only when its
`ClusterVersion` is read through the kept copy with the
serving certificate verified against its certificate authority and the request
authenticated by that client certificate. An API that answers but rejects that
kubeconfig, with a certificate the authority does not verify or a 401 or 403,
is a foreign answer, recorded as a fixed marker that never equals an identity,
and an API that does not answer is recorded as none.
The cluster is read through its own API with the
client the controller stage published, so completion is what the cluster says
about itself rather than what the installer said before it exited. The evidence
records the identity, what answered, the release, whether the cluster reported
its installation completed, the declared nodes still missing, the nodes still
presenting media, which of them present this cluster's own image, and whether
a wait of the apply restored the installer's kubeconfig; it records no
credential and no path. A node presents this cluster's own image as the rule
under [each insert has its own token](#installation) defines it; any other
image is foreign.

**Replay.** A cluster already answering with this operation's identity, at the
declared release, reporting its installation completed and holding every
declared node, reports `completed` with the same evidence, boots nothing and
waits for nothing. The only differences it converges are media it did not
finish releasing and each node's controller pointed at its installed disk:
release runs on every apply, so a settled apply selects each node's disk again.
The same cluster answering while it does not report its installation
completed, as an attempt interrupted during the installation wait leaves it, is
not installed: the next attempt waits for bootstrap and installation completion
again before it releases any media. An attempt interrupted during boot or the
bootstrap wait, before the API answers, leaves nodes running from this
cluster's own image. The next attempt boots only the nodes that are not both
running and presenting that image: it inserts no media into such a node, which
would swap the image its installer is still reading, and sets no boot override
on it, which its installer's own reboot would obey, and then waits for
bootstrap and installation completion. A node running without this cluster's
own image is not skipped, and refuses at its pre-boot proof: nothing is
inserted into a running machine. A foreign answer is not
converged: there is no reinstall path, and installing again requires this
cluster's nodes to be destroyed and applied again.

**Inverse.** Destroy ejects the media each node still presents and proves none
is left. The work area and the published image leave with the media block's own
inverse, which the plan orders after this one. The custodied administrator
access leaves when the removal completes, not with this inverse. The installed
cluster leaves with its nodes' disks, so this block removes nothing from a
node and consumes no authorization of its own on removal. A cluster whose nodes
are physical keeps running after its context is destroyed, exactly as a
physically installed operating system does. Its secondary networks stay with
it, because the cluster network block is retired with the cluster it
configured.

**Unknown resolution.** Observation is read-only against the frozen request. A
cluster answering with this operation's identity at the declared release,
reporting its installation completed, whole and with its media released, is
positive completion. Nothing answering, no node
running and no media inserted is positive no effect. That same cluster
answering while the completion is not yet true is a positive partial
realization the next attempt converges. So is nothing answering yet while this
operation recorded its identity, at least one node presents this cluster's own
image and no node presents any other, which an attempt interrupted during boot
or the bootstrap wait leaves. Anything else stays unknown, including a foreign
answer, a foreign image while nothing answers, and a node running while nothing
answers and no node presents this cluster's own image, because the first two
may belong to another installation and the last may be installing now. A
removal's resolution observes only the media each node's controller presents,
because that is what the removal takes back and a completed installation
already presents none, so it reads nothing of the cluster: no node presenting
media is its completion; any of this cluster's declared Machines presenting
media, this cluster's own image or any other, is a positive partial
realization, because the removal ejects whatever each node presents and
repeating it converges; and it stays unknown when a node's controller cannot be
read, which fails the observation, or when the evidence names a Machine that is
not one of the cluster's nodes.

**Quiescence and cancellation.** This block owns published boot media and
controller-side state that a running cluster does not read, so its quiescence
follows the Machines under the
[removal gate](state-reconciliation.md#quiescence-before-removal). An
installation already under way continues on the nodes after cancellation; the
attempt becomes unknown and is resolved by observing the cluster.

## Preflight

`bootwright preflight container-cluster` reports, for each selected
ContainerCluster, the checks an apply of its installation would otherwise meet
only after its image is built, with no effect on any host. Every check carries
`id`, `target`, `status`, `reason` and `next`, as storage preflight's do; its
status is `passed`, `failed`, `unknown` or `pending`, and only `passed` proves
readiness.

- `resolution.api`, `resolution.api-int`, `resolution.ingress` and
  `resolution.ingress-wildcard`: one check for each name
  [resolution before boot](#installation) proves, targeting that name:
  `api.<cluster>.<zone>`, `api-int.<cluster>.<zone>`,
  `console-openshift-console.apps.<cluster>.<zone>` and
  `<label>.apps.<cluster>.<zone>`, where `<label>` is `bootwright-` followed
  by the cluster name, cut to 63 characters and to no trailing hyphen. The
  applications name `apps.<cluster>.<zone>` itself is never resolved: a
  `*.apps` wildcard record does not answer it, and resolution before boot does
  not prove it. Each check resolves its name through the controller's own
  resolver exactly as the installation does, by running `getent ahosts` with
  exact arguments, because
  only the system's name-service configuration answers what the installer
  will poll. It passes when every answer is the slot's effective address, and
  fails naming each answer otherwise. Its `next` names the record and the
  route: when a managed DNSServer the cluster's nodes select serves the
  record, the controller's resolver routes the cluster zone to that server's
  endpoint address; otherwise the record is published in the resolver the
  controller uses. It is `pending` while that managed DNSServer's block is not
  done in the context's current operation, and its `next` is then the
  `infra-components` stage apply. A slot with no effective address has no
  check.
- `capacity.cpu`, `capacity.memory` and `capacity.disk`: the
  [libvirt capacity checks](substrates.md#capacity-admission) of each provider
  host the cluster's nodes are placed on, as the apply's gate computes them.

With `--dry-run` every check is `pending`, its `reason` names the record or the
quantity it would check, and nothing is resolved or observed. These controller
checks refuse `--trust-on-first-use` and borrowed SSH options.

## Post-installation network configuration

The block `cluster-network-<cluster>`, in the `add-ons` stage, configures what
only a running cluster accepts: the cluster's
[secondary networks](api/container-clusters.md#networking). It requires the
installation block and each add-on instance that provides what its intent
needs (`nmstate`), and reaches the cluster through the administrator kubeconfig
custody lends it. MetalLB address pools and advertisements are the MetalLB
package's configuration ([B727](milestones/m4.md#b727)), never the cluster's.

For each node it applies one `NodeNetworkConfigurationPolicy`,
`bootwright-<node>`, selecting that node by `kubernetes.io/hostname`, holding an
OVS bridge for each dedicated uplink (with `allow-extra-patch-ports: true`) and
one `ovn.bridge-mappings` entry per network, and waits for each to report
`Available`. It then applies one `ClusterUserDefinedNetwork` per network, of
topology `Localnet`, role `Secondary` and `physicalNetworkName` equal to the
network's name, and waits for `NetworkCreated`; OVN-Kubernetes renders a
`NetworkAttachmentDefinition` of the same name into every namespace the
network's selector matches, so pods and VMs attach by that name.

Every object is created through the
[cluster object program](add-ons.md#the-cluster-object-program) under its
[ownership receipts](add-ons.md#ownership-receipts), a foreign object of the
same name refuses, a continuation reports equal objects unchanged and fails
naming a difference, a settled apply reaches no cluster and
`bootwright diff --observe` reports drift as a partial realization
([observation](add-ons.md#observation-and-continuation), D153), and the block's removal
retires it with its cluster without contacting it: the cluster leaves with its
disks, or keeps running on physical nodes. A condition reporting `Degraded`, a
policy matching no node and a foreign attachment definition each fail the block
naming the object.

## Adapter boundary

Installation crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per block and operation, on the Machine the
artifact server is placed on, under
[the adapter result protocol](architecture.md#the-adapter-result-protocol) and
the [process](security.md#process-boundary) and
[Secret-material](security.md#sensitive-material) rules, composing each
substrate's pre-boot, boot and disk-boot entry points by fixed qualified name.
Go locates the exact installer executable; the adapter substitutes bound
material into the installer inputs and invokes the installer with exact
argument vectors. No pull secret, private key or captured credential enters an
argument, an environment variable, the evidence or a log.

Both frozen requests carry the controller arm alone: connection `local` and the
controller Machine's name, with no other placement field, the only placement
planning produces because [the image is built on the controller](#boot-media).
Either decoder refuses any other placement with `lifecycle.state`, so apply,
destroy, observation and removal planning each refuse such a block before an
adapter runs, and no run carries a placement's identity or host key.

Selected FIPS clusters project `fips: true` and use the exact release's
publisher-verified RHEL 9 `openshift-install-fips` executable. Controller
preflight, plan and apply admission require a positive kernel FIPS-mode
observation and otherwise refuse `controller.unsupported`. Media apply refuses
an `ssh-ed25519` administration key naming the bound Secret before running the
installer. These prerequisites make no claim about Bootwright's cryptography;
real FIPS-host qualification remains [B722](milestones/m3.md#b722).
