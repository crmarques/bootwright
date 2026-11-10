# Controlled network supply

A named slice revives this design when the owner promotes the items
[D250](../milestones/backlog.md#d250) parked. Until then only what
[acquisition origins](../security.md#acquisition-origins) states exists in
code. The design lets each estate choose how far its Machines reach: from
direct access to every origin, as today, to a network where the controller and
every Machine it builds reach only their own package repositories, the vendor
origins and one mirror host.

## The rule

The concern is the route, not only the content. A Machine that reaches a site
any account publishes on can fetch an attacker's code from it and send data to
it, whatever Bootwright itself acquires there. A digest protects the content;
only the absent route protects the Machine. In a controlled network:

- every RPM keeps coming from the host's own repositories;
- a vendor origin stays direct, from the controller and from every Machine;
- every public-origin artifact comes from the mirror host, and the context
  neither dials a public origin nor hands one to a Machine.

The class follows the serving host, never the content's author. OpenShift's
release payload is published on `quay.io`, on which any account publishes, so
it goes through the mirror. IBM's `icr.io`, where the Data Foundation catalog
and operator images live and where IBM's cloud customers also publish, stays a
direct vendor origin by the owner's choice, so that a cluster keeps taking
Data Foundation's later updates from it; a firewall that admits it admits that
whole host. A vendor host that accepts uploads, such as a telemetry or support
endpoint, is no acquisition origin, so a firewall that serves this rule admits
the content hosts alone. Time and name service follow the rule through fields
that already exist: a Machine or cluster with no NTP selection keeps its
operating system's public pool, so a controlled network declares one. A
cluster's selection is added beside its nodes' default pool, which only the
cluster's own configuration replaces.

## Deployment choices

The mirror is optional, and where it runs is the operator's choice. Each row
is one Environment's declaration of the same product:

| Choice | Declaration | Which Machines reach a public origin |
| --- | --- | --- |
| Direct | No mirror; each Machine's own proxy choice, direct or an external Proxy | Every Machine that acquires something, as today |
| Mirror on the controller | The mirror's `Registry` and `ArtifactServer` placed on the controller Machine | The controller alone; nodes and hosts read the controller |
| Mirror on its own Machine | The mirror's services placed on another Machine | That Machine alone |
| The enterprise's mirror | An external `Registry` and file server the operator fills | None |
| No route at all | The enterprise's mirror, with the vendor origins mirrored too | None, the vendors included ([B69](../milestones/m3.md#b69), [B493](../milestones/m2.md#b493)) |

The tool table, the bill of materials and the vulnerability gate hold in every
row: the build decides what is acquired, the declaration only where from.

## The mirror host

The mirror host is a Machine like any other: its operating system is provided
or installed by Bootwright, Bootwright reaches it over SSH, or locally when it
is the controller, and places on it a mirror `Registry` and an
`ArtifactServer` as managed services.

- **Curated, never a pass-through.** It holds exactly the artifacts a
  context's acquisition list names. A pull-through cache or a forward proxy
  would hand every Machine the whole public origin again, and the names it
  forwards would carry data out.
- **Trusted for nothing, with one exception.** Every consumer verifies a
  digest it held before it asked: one the build's table holds or one the
  desired state declares. A compromised mirror host can refuse to serve and
  nothing else ([remote principals](../security.md#remote-principals)). The
  exception is a tool release newer than the build's default, whose SHA-256
  the mirror host reads from the publisher and so is trusted for. An image
  authored by tag, as a storage cluster's may be, takes its digest from the
  registry that serves it, so a controlled network authors digests.
- **HTTPS only.** Both services serve TLS with a certificate Bootwright
  generates for the host's names and addresses, and every consumer trusts its
  authority: each Machine's container runtime, each cluster's trust bundle and
  the controller's acquisitions. No Machine is configured with an insecure
  registry.
- **No platform secret.** It holds its registry's push credential and, while
  it mirrors an OpenShift release, that release's pull secret; it holds no BMC
  credential, kubeconfig or fleet key, unless it is the controller.
- **Narrow both ways.** Its egress is the public origins of the acquisition
  list. Inbound, the platform networks read its two services and the
  controller alone administers it.

An enterprise's own registry and file server serve as well: the context
declares them external, and its operator fills them from the acquisition list.

## Declaration and refusal

The Environment names the mirror once, a `Registry` and an artifact server
endpoint, instead of one override on each consumer. Bootwright derives every
location from it under a fixed layout, `<registry>/<origin host>/<repository>`
for an image and `<endpoint>/<kind>/<version>/<file>` for a tool:

| Consumer | Today's route | With the declaration |
| --- | --- | --- |
| Controller stage tools | Three `spec.downloads` fields, and the publisher's metadata | Every public-origin tool from the artifact server |
| Managed service image | `spec.image.local` on each service | Derived, with the Registry's credential and trust |
| Emulated BMC image | The host's own container configuration | Derived |
| Cluster release images | Authored `imageDigestSources` | Derived for the release repositories |

With the declaration present no public origin is dialed: an artifact the
mirror does not hold refuses in preflight, naming it and the command that
fills the mirror. Vendor origins stay direct. Without it, every consumer keeps
its own route, as today.

## Filling the mirror

The acquisition list is the one source: every artifact the admitted graph and
the build's table select, with its kind, version, origin, class, size, digest
and the Machine that reads it. It is a read-only result, so it also gives a
firewall's owner each Machine's exact destinations.

Bootwright fills the mirror on the mirror host, because only that host reaches
the origins: images copied by digest, tools fetched and checked against their
SHA-256, and an OpenShift release mirrored by that release's own `oc`. Each
artifact is verified there before it is published, and a repeat copies only
what is missing.

The mirror's blocks run before the controller stage's tools. They need no
target client, because they run on the mirror host through the automation the
executable carries, so the
[engine's ordering exception](../state-reconciliation.md#stages-and-the-pause-boundary)
gains one clause: the controller block waits for the mirror's blocks, and
every other block still waits for the controller block. A mirror placed on the
controller needs the container runtime before the controller stage's tools, so
that stage's host packages and its tools become two blocks with the mirror
between them. A mirror on a Machine whose operating system Bootwright installs
waits for that installation, whose own media come from an artifact server
elsewhere, usually on the controller.

## The build

- **Minimum and default releases.** A tool table beside the
  [runtime lock](../controller.md#the-runtime-lock) holds, for Helm,
  `kubectl`, `virtctl` and `govc`, a minimum release and a default release
  with its size and SHA-256. An Environment that names no release gets the
  default, acquired by that digest with no publisher metadata. A declared
  release at or above the minimum is admitted, and its SHA-256 is read from
  its publisher by the Machine that downloads it, the controller or the mirror
  host; one below the minimum refuses at plan. When an advisory reaches the
  default or the minimum, a new build raises both, and the next apply replaces
  a retained release below the new minimum. `latest` goes. The OpenShift
  clients and installer follow the cluster's release; the release itself, the
  storage images, the carried wheels and the service images take no minimum,
  because the operator chooses a release
  ([D163](../milestones/backlog.md#d163)) and the rest are exact by digest.
  The container build's Go image is pinned by digest.
- **One SBOM.** Each build writes and embeds one CycloneDX document: what the
  executable carries (Go modules, the toolchain, the wheels, the collection)
  and what it pins and fetches at use (tools and images), each with its
  version, digest, origin and license.
- **A vulnerability gate.** `make build` fails on a known vulnerability in a
  Go module or the standard library, a locked wheel or a default tool. A
  reviewed exception is the only way past, and it ships with the SBOM as a VEX
  statement. The executable carries the verdict and the database's date.
  Images are scanned on a schedule and in the mirror registry, because a base
  image always carries findings a build cannot fix.

### Handling a finding

A coding agent that meets the gate handles the finding itself and continues
its task, as the
[code-implementation](../../.agents/skills/code-implementation/references/verification.md#vulnerability-findings)
skill says.

## Delivery order

Each step leaves a working product.

1. The build: [B1242](../milestones/delivered.md#x342--default-clients-pinned-by-the-build),
   [B1243](../milestones/backlog.md#b1243) and
   [B1244](../milestones/backlog.md#b1244). After the first, a file server the
   operator fills already serves every default tool.
2. Consuming a mirror the operator fills:
   [B1245](../milestones/backlog.md#b1245),
   [B1246](../milestones/backlog.md#b1246),
   [B1247](../milestones/delivered.md#x356--service-and-bmc-images-from-the-image-mirror) and
   [B1248](../milestones/delivered.md#x357--release-repositories-from-the-image-mirror), with the acceptance of
   [B724](../milestones/m3.md#b724).
3. Bootwright builds and fills the mirror host:
   [B1236](../milestones/delivered.md#x358--container-runtime-on-an-ssh-placement-host), [B68](../milestones/m1.md#b68)
   and [B1249](../milestones/backlog.md#b1249).
4. Proof: [B1250](../milestones/backlog.md#b1250), with
   [B1251](../milestones/backlog.md#b1251) as the lab's vendor-only gate.

## Left out

What an installed cluster contacts by itself, such as telemetry, the update
service or a community catalog, is cluster configuration its GitOps owns. RPM
mirroring stays with the enterprise's repositories. An add-on whose images
live on another public registry needs a mirror set of its own.
