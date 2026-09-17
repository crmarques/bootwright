# Container clusters

Container cluster owns the installation of an OpenShift or OKD cluster from
declared intent, and the evidence that it completed. The
[kind schema](api/container-clusters.md) owns `ContainerCluster`;
[substrates](substrates.md) own the Machines its nodes are bound to and the
management controllers they are booted through;
[infrastructure services](infrastructure-services.md#consumer-publication) own
the served root its boot media is published beneath; [Secrets](secrets.md)
owns the material it consumes and the credentials it captures; availability
follows [milestones](milestones.md).

One installation contract covers every substrate, exactly as
[managed-OS installation](managed-os.md#installation) does. It never asks which
substrate a node is on: the [realized target](substrates.md#selection-and-refusal)
supplies the management controller to boot through, the NICs the node reports,
and whether the machine is physical, and everything that differs between
substrates follows from those answers. A substrate added later inherits this
installation whole.

## Selection and refusal

Two blocks are planned for every selected `ContainerCluster`, and both belong
to the [`clusters` stage](state-reconciliation.md#stages-and-the-pause-boundary).

**Supported shape.** An OpenShift cluster declaring an exact
`distribution.release.version`, installed by the `agent` method in `connected`
mode, whose nodes are all Machines on a realized substrate with
`os.provided: false` and no install profile. A single-node cluster resolves its
three endpoint slots from that node; a multi-node cluster resolves them from
authored or load-balancer addresses. Every other declaration refuses before
operation registration with one diagnostic naming the cluster: `okd`,
`disconnected` mode, a release pinned by image alone, `security.fips`,
`security.diskEncryption`, `install.servingCertificates`,
`install.registries`, and a node on a substrate this executable does not
realize. Node `labels` and `taints` are accepted and reach no installer input,
because they are post-installation placement intent rather than install
configuration.

Selection is pure and reads no host, endpoint or Secret material.

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
realized target reports by name and hardware address, the root device the
Machine selects, and the node's own network configuration with its install
address applied. The selected NTP servers become additional time sources.

Two values are derived rather than authored, because the agent installer
refuses the alternatives. A single-node cluster renders `platform: none`
whatever platform the API derived, since the installer accepts no bare-metal or
vSphere platform for one control-plane node and no compute nodes. A cluster
whose endpoints are not all owned by the installer renders its platform's
load balancer as user-managed.

The frozen inputs carry no secret value. The pull secret, the cluster SSH
public key and each additional trust bundle are named as declarations in the
request and substituted into the input files by the attempt that writes them.

## Boot media

The block `cluster-media-<cluster>` produces the cluster's agent boot image. It
requires the managed `ArtifactServer` its `agent.redfishVirtualMedia` selection
names, and every managed `DNSServer` and `NTPServer` its nodes and its own
selections resolve to, because the installer resolves names while it runs.

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
given. The attempt writes the frozen input files there, substitutes the bound
material into them, and invokes the installer once. The installer consumes its
inputs into its own asset state and keeps there the cluster identity and the
access the installation later needs, so the area outlives the attempt that
created it, is never enumerated in evidence, progress output or a log, and is
discarded and rebuilt rather than reused whenever the inputs it was built from
are not the inputs frozen now.

**Publication is private.** The image embeds the pull secret and the cluster
SSH key in its own ignition, so it is confidential on every substrate and is
published beneath the served root under the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication),
at `private/clusters/<cluster>/<token>/`, with the token minted by the attempt
and appearing in no frozen request, evidence, progress output or log. The block
owns `private/clusters/<cluster>/` as its reserved path.

That contract requires the fetching controller to verify the serving
certificate. A physical management controller does so under the trust its
Machine declares. An [emulated controller](substrates.md#machine-realization)
does not: it fetches over plain HTTP from the host it runs on, so on that
substrate confidentiality rests on the token and on the fetch never leaving the
provider host. This is the one exception, it is recorded here rather than
implied, and it does not extend to any other consumer.

**Reservations.** `path:` for the work area and `path:` for the private
publication subtree, so a second context refuses rather than taking either.

**Evidence.** Completion requires the published image present at the frozen
path with the digest the attempt computed, the installer version it was built
with, and the digest of the inputs it was built from. Replay reports
`completed` without rebuilding when the published image was built from the same
inputs by the same installer version. The inverse removes the published image
and the private directory that held it, discards the installer work directory,
and proves each absent.

## Installation

The block `cluster-install-<cluster>` boots the nodes from that image and
watches the cluster install. It depends on the media block, and requires every
node `Machine` and the same name and time services.

A node whose realized target is [physical](substrates.md#physical-machine-realization)
makes this block consume `data-loss` on **apply**: the agent installer writes
the release image to that node's disk, and that is the moment its existing
content is lost. A cluster of virtual nodes consumes nothing here, because
their disks are created by their realization and removed by its inverse.

**Resolution before boot.** The installer polls the cluster API from the
controller, so before anything is booted the block proves that the controller
resolves the cluster's API, internal API and applications names to the
addresses the plan froze. A name that does not resolve fails the block naming
each missing answer, and nothing is booted.

**Boot.** Each node is booted in node-name order, through
[its own substrate's boot operation](substrates.md#identity-and-power-operations):
the target is proved exactly as an OS installation proves it, the published
image is inserted as virtual media, the substrate applies whatever boot
selection its controller needs, and the machine is powered on and polled to
running. The media stays inserted: a live agent image is still being read after
the node answers on the network, and removing it early corrupts the running
installer.

**Waiting.** The block waits for bootstrap completion and then for installation
completion, through the same installer that built the image. Each wait is a
read-only observation that starts nothing: the installer gives up on its own
compiled deadlines while the cluster keeps converging, so a give-up it can
resume from is re-invoked until this block's own wall-clock budget is spent,
and the budget bounds when a new wait may start rather than when the block
returns. A give-up that proves the cluster stopped installing — a declared host
the assisted service moved into error — is never re-invoked, because the next
window would watch a state that cannot change; it fails the block naming the
host and what that state means. A give-up that proves a declared node never
registered fails the block naming that node, because the cluster waits for
exactly the nodes the install configuration declares.

**Captured credentials.** A completed installation produces the cluster
administrator kubeconfig and the initial administrator password. Both are
[captured](secrets.md#captured-material) into this context's confidential
custody as soon as the installation completes, and the copies the installer
left in the work directory are removed. They are never written to evidence,
progress output or a log, and the `cluster kubeconfig` command is what reveals
one.

**Releasing the media.** Once the installation has completed, and only then,
each node's virtual media is ejected and its controller is pointed at the
installed disk, in node-name order.

**Completion.** Completion requires the installer reporting installation
complete, the captured credentials present in custody, the cluster answering
with the cluster identity this operation's own installer metadata records, its
cluster version reporting the declared release available, every declared node
present as a node, and every node's media ejected. The evidence records the
cluster identity, the release the cluster reports, the declared nodes it found
and the hosts the installer registered; it records no credential.

**Replay.** A cluster already answering with this operation's cluster identity
and reporting the declared release available reports `completed` with the same
evidence and boots nothing. The only differences it converges are credentials
it captured but has not yet removed from the work directory, and media it did
not finish ejecting. A cluster answering with another identity refuses: there
is no reinstall path, and installing again requires this cluster's nodes to be
destroyed and applied again.

**Inverse.** Destroy ejects each node's virtual media and releases the captured
credentials from custody, and proves each absent. The work area and the
published image leave with the media block's own inverse, which the plan orders
after this one. The installed cluster leaves with its nodes' disks, so this block
removes nothing from a node and consumes no authorization of its own on
removal. A cluster whose nodes are physical therefore keeps running after its
context is destroyed, exactly as a physically installed operating system does.

**Unknown resolution.** Observation is read-only against the frozen request. A
cluster answering with the frozen identity and the declared release, with its
credentials captured and its media ejected, is positive completion. Every node
powered off, no captured credential and no work directory is positive no
effect. This operation's own unfinished work — a cluster that answers while a
credential is still in the work directory, or media still inserted after a
completed installation — is a positive partial realization the next attempt
converges. Anything else stays unknown, including a cluster answering with
another identity and a node powered on while nothing answers, because the first
belongs to another installation and the second may be installing now.

**Quiescence.** This block owns published boot media and controller-side state
that a running cluster does not read, so it is quiescent whenever the
[removal gate](state-reconciliation.md#quiescence-before-removal) asks. A node
still running is probed by its own Machine block in the same removal.

**Cancellation.** Cancellation stops authorization of new effects and
terminates the owned process tree. An installation already under way continues
on the nodes; the attempt becomes unknown and is resolved by observing the
cluster.

## Adapter boundary

Installation crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per block and operation, on the Machine the
artifact server is placed on, composing each substrate's boot and proof task
files by fixed qualified name. Go freezes the request, locates the exact
installer executable, authorizes each phase and validates the returned evidence
strictly. The adapter substitutes bound material into the installer inputs,
invokes the installer with exact argument vectors, and returns bounded
structured evidence. No pull secret, private key or captured credential enters
an argument, an environment variable, the evidence or a log.
