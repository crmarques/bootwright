# Controlled network supply

A named slice revives this design when the owner promotes the items
[D250](../milestones/backlog.md#d250) parked. Until then only what
[acquisition origins](../security.md#acquisition-origins) states exists in
code. The design lets a context run where no Machine may reach a public
origin: the controller and every Machine it builds reach their own package
repositories, the vendor origins and one mirror host, and that host alone
reaches the public origins.

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
release payload is published on `quay.io`, and the Data Foundation catalog and
operator images IBM publishes are on `icr.io`, the IBM registry that also
serves its cloud customers' own namespaces; both go through the mirror, while
`cp.icr.io`, the entitled registry, stays direct. A vendor host that accepts
uploads, such as a telemetry or support endpoint, is no acquisition origin, so
a firewall that serves this rule admits the content hosts alone. Time and name
service follow the rule through fields that already exist: a Machine or
cluster with no NTP selection keeps its operating system's public pool, so a
controlled network declares one. A cluster's selection is added beside its
nodes' default pool, which only the cluster's own configuration replaces.

## The mirror host

One Machine with `os.provided: true` holds a mirror `Registry` and an
`ArtifactServer`. It is the only Machine with a route to a public origin.

- **Curated, never a pass-through.** It holds exactly the artifacts a
  context's acquisition list names. A pull-through cache or a forward proxy
  would hand every Machine the whole public origin again, and the names it
  forwards would carry data out.
- **Trusted for nothing.** Every consumer verifies a digest it held before it
  asked: one the build pins, one the desired state declares, or the vendor's
  checksum for a vendor's own tool. A compromised mirror host can refuse to
  serve and nothing else ([remote principals](../security.md#remote-principals)).
  An image authored by tag, as a storage cluster's may be, takes its digest
  from the registry that serves it, so a controlled network authors digests.
- **No platform secret.** It holds its registry's push credential and, while
  it mirrors an OpenShift release, that release's pull secret; it holds no BMC
  credential, kubeconfig or fleet key.
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
| Controller stage tools | Three `spec.downloads` fields, and the publisher's metadata | Every public-origin tool from the artifact server, by its pinned digest |
| Managed service image | `spec.image.local` on each service | Derived, with the Registry's credential and trust |
| Emulated BMC image | The host's own container configuration | Derived |
| Cluster release images | Authored `imageDigestSources` | Derived for the release repositories |

With the declaration present no public origin is dialed: an artifact the
mirror does not hold refuses in preflight, naming it and the command that
fills the mirror. Vendor origins stay direct. A site with no route at all
extends the same declaration to them, which is the disconnected delivery
[B69](../milestones/m3.md#b69) and [B493](../milestones/m2.md#b493) own.

## Filling the mirror

The acquisition list is the one source: every artifact the admitted graph and
the build's pins select, with its kind, version, origin, class, size, digest
and the Machine that reads it. It is a read-only result, so it also gives a
firewall's owner each Machine's exact destinations.

Bootwright fills the mirror on the mirror host, because only that host reaches
the origins: images copied by digest, tools fetched and checked against their
pinned digests, and an OpenShift release mirrored by that release's own `oc`.
Each artifact is verified there before it is published, and a repeat copies
only what is missing.

The mirror's blocks run before the controller stage's tools. They need no
target client, because they run on the mirror host through the automation the
executable carries, so the
[engine's ordering exception](../state-reconciliation.md#stages-and-the-pause-boundary)
gains one clause: the controller block waits for the mirror's blocks, and
every other block still waits for the controller block.

## The build

- **Pinned tools.** A tool lock beside the
  [runtime lock](../controller.md#the-runtime-lock) holds the default release,
  size and SHA-256 of each client no vendor serves. `latest` no longer asks a
  publisher at apply, an explicit release carries its own SHA-256, and a
  pinned or declared digest needs no publisher metadata. The container build's
  Go image is pinned by digest.
- **One SBOM.** Each build writes and embeds one CycloneDX document: what the
  executable carries (Go modules, the toolchain, the wheels, the collection)
  and what it pins and fetches at use (tools and images), each with its
  version, digest, origin and license.
- **A vulnerability gate.** `make build` fails on a known vulnerability in a
  Go module or the standard library, a locked wheel or a pinned tool. A
  reviewed exception with its reason and expiry is the only way past, and it
  ships with the SBOM as a VEX statement. The executable carries the verdict
  and the database's date. Images are scanned on a schedule and in the mirror
  registry, because a base image always carries findings a build cannot fix.

## Delivery order

Each step leaves a working product.

1. The build: [B1242](../milestones/backlog.md#b1242),
   [B1243](../milestones/backlog.md#b1243) and
   [B1244](../milestones/backlog.md#b1244). After the first, a file server the
   operator fills already serves every tool.
2. Consuming a mirror the operator fills:
   [B1245](../milestones/backlog.md#b1245),
   [B1246](../milestones/backlog.md#b1246),
   [B1247](../milestones/backlog.md#b1247) and
   [B1248](../milestones/backlog.md#b1248), with the acceptance of
   [B724](../milestones/m3.md#b724).
3. Bootwright builds and fills the mirror host:
   [B1236](../milestones/backlog.md#b1236), [B68](../milestones/m1.md#b68)
   and [B1249](../milestones/backlog.md#b1249).
4. Proof: [B1250](../milestones/backlog.md#b1250), with
   [B1251](../milestones/backlog.md#b1251) as the lab's vendor-only gate.

## Left out

What an installed cluster contacts by itself, such as telemetry, the update
service or a community catalog, is cluster configuration its GitOps owns. RPM
mirroring stays with the enterprise's repositories. An add-on whose images
live on another public registry needs a mirror set of its own.
