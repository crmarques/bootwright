# Container clusters

Container cluster owns the installation of an OpenShift or OKD cluster from
declared intent, and the evidence that it completed. The
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
refuses before operation registration with one diagnostic naming the cluster:
`okd`, `disconnected` mode, a release pinned by image alone, an installation
proxy, `security.fips`, `security.diskEncryption`,
`install.servingCertificates`, `install.registries`, a multi-node cluster on
the `vsphere` or `external` platform (one on `baremetal`, on `none` or with no
declared platform is accepted), a node on a substrate this
executable does not realize, a node whose management controller would have
to be taught a new certificate, a virtual node whose provider host is not the
Machine the selected artifact server is placed on, because its emulated
controller fetches the private [boot image](#boot-media) without verifying the
server, a node that selects an install profile,
because managed OS and the cluster installer would both write its disk, a
node whose realized target is physical, because physical cluster installation
is not yet qualified ([B67](milestones/m3.md#b67) lifts that refusal after
in-tree tests and an emulated rehearsal, with real-hardware acceptance before
support is claimed), a node whose
`deviceName` the agent installer cannot name, which is anything but
`/dev/<name>` or `/dev/disk/by-path/<name>`, and a node
whose `minSizeGigabytes` exceeds 9007199254740991, the largest integer the
frozen input carries exactly. The placement, install-profile and physical-node
refusals and the two root-device refusals name the bound Machine in the
remediation the refusal carries, because it is what the operator changes. Node
`labels` and `taints` are accepted and reach no installer input, because they
are post-installation placement intent rather than install configuration.

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
node's own network configuration with its install address applied. The
selected NTP servers become additional time sources.

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

Two values are derived rather than authored, because the agent installer
refuses the alternatives. A single-node cluster renders `platform: none`
whatever platform the API derived, since the installer accepts no bare-metal or
vSphere platform for one control-plane node and no compute nodes. A cluster
whose endpoints are not all owned by the installer renders its platform's
load balancer as user-managed.

The frozen inputs carry no secret value. The pull secret, the cluster SSH
public key and each additional trust bundle are named as declarations in the
request and substituted into the input files by the attempt that writes them.
A cluster that selects additional trust bundles freezes an empty
`additionalTrustBundle` in `install-config.yaml`, and the attempt fills it with
the certificate part of each bundle's Secret, in the order the cluster selects
them, each trimmed of surrounding whitespace and joined to the next by one line
break, because the installer refuses a bundle holding anything after its last
certificate. No `additionalTrustBundlePolicy` is written, so the installer
applies its own default.

## Boot media

The block `cluster-media-<cluster>` produces the cluster's agent boot image. It
requires the managed `ArtifactServer` its `agent.redfishVirtualMedia` selection
names, because that is where the image is published, and nothing else: the name
and time services the cluster selects are baked into the image as data rather
than reached while it is built.

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
given. The attempt writes the frozen input files there, substitutes the bound
material into them, and invokes the installer once. The installer consumes its
inputs into its own asset state and writes beside the image the access the
installation later needs, whose admin client certificate is the cluster's
[identity](#installation), so the area outlives the attempt that created it, is
never enumerated in evidence, progress output or a log, and is discarded and
rebuilt rather than reused whenever the inputs it was built from are not the
inputs frozen now, unless an installation has booted from it.

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

That contract requires the fetching controller to verify the serving
certificate. A physical management controller does so under the trust its
Machine declares. An [emulated controller](substrates.md#machine-realization)
does not: it fetches over HTTPS without verifying the server's certificate, so
on that substrate confidentiality rests on the token and on the fetch never
leaving the provider host. Selection enforces the second: a virtual node whose
provider host is not the Machine the artifact server is placed on refuses before
registration. This is the one exception, it is recorded here rather than
implied, and it does not extend to any other consumer.

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
the installer work directory, and proves each absent. A removal's resolution
reads the observation for what the removal proves: neither the image nor the
work area present is its completion, the image this request describes is
positive no effect, and anything else of this block's own left is a positive
partial realization.

## Installation

The block `cluster-install-<cluster>` boots the nodes from that image and
watches the cluster install. It depends on the media block, and requires every
node `Machine` and the same name and time services.

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

**Boot.** Each node is booted in node-name order, through
[its own substrate's boot operation](substrates.md#identity-and-power-operations).
Before anything is inserted, each node the boot does not skip is proved through
its substrate's pre-boot proof, exactly as an OS installation proves its
target: a machine the substrate created by its controller answering with the
machine off, and a physical node against the declared NICs its install request
froze and the identity its Machine's
[bare-metal block pinned](substrates.md#physical-machine-realization) earlier in
the same operation. A node that fails the proof fails the block, and nothing is
inserted into it or booted. Then the published image is inserted as virtual
media, the substrate applies whatever boot selection its controller needs, and
the machine is powered on and polled to running. The media stays inserted: a
live agent image is still being read after the node answers on the network, and
removing it early corrupts the running installer.

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
host and what that state means, even when the budget stopped that attempt. A
give-up that proves a declared node never registered fails the block naming
that node, because the cluster waits for exactly the nodes the install
configuration declares. It does so even when the budget stops the attempt
re-invoked after that give-up: an attempt the budget stopped proves nothing
new, so when the last give-up the installer itself reported in that wait was a
stall, the block fails as that stall and adds that the budget stopped the
attempt after it. A wait is thus diagnosed as a host in error first, then as a
stall, then as the budget spent, and only then as the installer's own timeout.

**Budgets.** Each long phase of a cluster's two blocks is bounded in wall-clock
time by a budget its frozen request carries, never by a value the adapter
chooses: the media request's build budget of 1,800 seconds, and the install
request's boot budget of 300 seconds for each node it boots, never less than
900 seconds, so a cluster of up to three nodes freezes 900, and its bootstrap
and installation wait budgets of 5,400 seconds each. The image build runs
under `timeout` with the build budget and the same grace; a build the budget
stopped fails the media block as the budget spent, publishes nothing, and is
resumable, because the next apply discards the work area and builds again.
The boot budget bounds
booting every node together: its deadline is taken once, before the first
node, and each boot step, the substrate's own included, runs under
ansible-core's task timeout with only what is left of it, so a node already
running from this cluster's image, which the boot leaves alone, takes none of
it. A step still running at the deadline is stopped there, and a node reached
after it is not booted; either fails the block as the boot budget spent, and a
repeated apply boots only the nodes not already running from the image. The
task timeout ends the step but not the management-controller request the step
made, which ends within the adapter's own fixed bounds. Every read of the
cluster through its API passes oc's `--request-timeout` of 30 seconds, so each
request oc makes gives up rather than waiting on an API that accepted the
connection and never answers. Every run of either block is bounded by a
[deadline](architecture.md#the-adapter-result-protocol) derived from the
budgets its request froze: the build budget plus 30 minutes for the media
block, and for the installation its three budgets back to back plus 30 minutes
and 11 minutes for each node it reads and releases, which is 3 hours 56
minutes for a single node, 4 hours 18 minutes for three and 5 hours 54 minutes
for nine. A node's 11 minutes are exactly the
[bounds](substrates.md#identity-and-power-operations) of the calls the
installation makes to its controller outside the boot budget: its media read
in each of the two state reads, and the eject and the disk selection that
release its media. Those bounds count the requests of a call's polls as
answered at once. A cluster whose installation deadline would pass the
runner's 6-hour ceiling, more than 9 nodes with these budgets, refuses before
registration, naming that deadline and how many nodes fit, rather than being
cut short part way through its installation.

Not yet met: `plan` and `apply` report that refusal without its reason and
remedy, because the lifecycle's unsupported-shape refusal drops each object's
reason; tracked as [B32](milestones/m1.md#b32).

**The access it produces.** The installer writes the cluster administrator
kubeconfig and the initial administrator password into the work area when it
builds the image, beside the state it keeps there, and a completed installation
is what they then grant access to. That area is root-owned, `0700` and never
served. Once an apply's attempt proves the installation complete, or a
resolution of an apply's block reads it complete, including a destroy's
resolution of an incomplete apply, the role copies the kubeconfig into a
private output file of its run and the engine keeps it in the context's
[custody](secrets.md#produced-material), keyed by this block and `kubeconfig`,
before it records the block done; a copy of equal bytes publishes nothing. A
removal offers nothing. The custodied copy is withdrawn when the context's
removal completes, not with this block's inverse, so a destroy that stops part
way keeps the only access, and `cluster kubeconfig` reveals it until then
([administrator access export](cli.md#administrator-access-export)). The
initial administrator password stays in the work area. Every read of the
cluster, before and after completion is proved, uses the installer's own file,
which keeps its hazards: each rerun of `agent wait-for install-complete`
prepends the router CA to that file again, growing it toward the inspection's
64 KiB read bound, and a budget kill during the installer's in-place write of
it could truncate it. Neither copy is ever written to evidence, progress output or a
log.

**Releasing the media.** Once the installation has completed, and only then,
each node's virtual media is ejected and its controller is pointed at the
installed disk through the substrate's disk-boot entry point, which powers
nothing on, in node-name order.

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
certificate unchanged, so each rewrite leaves the identity unchanged while the
file stays within the 64 KiB the inspection reads. Each run grows the file by
the encoded bundle, about 3 KiB when it is the ingress operator's RSA-2048
wildcard certificate and CA, so the bound holds for roughly 18 rewrites. The
agent installer records no other identity. A work area without that kubeconfig
names none, and so does one whose kubeconfig exceeds that bound, is in neither
form the installer writes, lacks an embedded certificate authority, disables
verification or authenticates any other way. The cluster answers with the
identity only when its `ClusterVersion` is read through that kubeconfig with the
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
presenting media and which of them present this cluster's own image; it records
no credential and no path. A node presents its own image when the scheme, host
and path of the image its controller reports equal those of the address the
media block published, each compared exactly and never as a prefix, because a
controller may echo an image back without its default port; any other image,
including another attempt's under the same reserved path, is foreign.

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
physically installed operating system does.

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
