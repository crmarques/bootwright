# ADR 0051: The Repository an IBM Fleet Already Subscribes To

## Status

Accepted

Extends the machine-scope registration split of
[ADR 0015](0015-machine-scope-rhsm-registration.md): the Ceph phase keeps the
distribution tail, but its package source becomes declarable. Follows the
declared-alternative rule of
[ADR 0034](0034-wiping-a-device-no-node-claims.md)'s spirit as applied
throughout: an explicit spec field, never inference from what a node happens
to serve.

## Context

The `ibm` distribution hardcoded its package acquisition: every storage apply
fetched `https://public.dhe.ibm.com/.../ibm-storage-ceph-<stream>-rhel-9.repo`
into `/etc/yum.repos.d/ibm-storage-ceph.repo` and installed from IBM's public
CDN. The fetch ran on re-applies too — `get_url` without a checksum re-contacts
the URL even when the file exists — so a converged production fleet stayed
hostage to that egress path (10-second module timeout, no retries) for the
life of the cluster. A degraded proxy leg to IBM turned a routine re-apply
into a seven-node failure.

Estates with a Satellite already host the IBM Storage Ceph packages as an
RHSM-served repository, and their machine profiles already declare it in
`customizations.repositories.subscription.enable[]`. That declaration was not
only ignored by the Ceph phase — it was actively undone. The Ceph deps phase
re-asserted the repository set with `rhsm_repository purge: true` over the
provider's repos alone (BaseOS + AppStream for `ibm`), disabling the
Satellite repository the machines phase had just enabled, then fetching the
public `.repo` anyway. The two declaration sites fought each other on every
apply pair; the later writer won.

The `redhat` distribution already proves the alternative shape: its Ceph
packages come purely from RHSM repositories (`rhceph-<stream>-tools-*`), no
repo file is ever written, and a Satellite serves them transparently under
the standard label. `ibm` was the only distribution with an unavoidable
public-internet package dependency and no override.

## Decision

`spec.ceph.ibm.packages` declares where IBM Storage Ceph RPMs come from:

- `source: vendor` (and the absent block) keeps today's behavior bit-for-bit —
  the public `.repo` fetch. `subscriptionRepos` must then be empty.
- `source: subscription` names RHSM repository ids in `subscriptionRepos`.
  The renderer folds them into `repository.redhatRepos` and omits
  `repository.ibmRepoURL`; the role's existing empty-URL gate then skips the
  fetch. The phase removes a vendor `.repo` file an earlier apply installed,
  so flipped fleets stop pointing dnf metadata refreshes at the public CDN.
  The list must be non-empty, and one `dnf repoquery` preflight — after the
  removal, so it cannot pass against stale public-repo metadata — disables
  every undeclared repository, enables only the rendered provider repository
  set, forces `skip_if_unavailable=False` globally and for each declared id,
  and queries `cephadm` plus `ibm-storage-ceph-license` together. It proves the
  declared repositories are readable and serve both packages before anything
  installs.

The preflight queries bare package names rather than the pinned spec, and has
dnf attribute every emitted line to its package while printing every spec form
it would itself accept for each available build. One command means both package
verdicts share one repository read. Separate asserts then distinguish a command
failure from a readable repository set that publishes no build of a particular
package, before a final assert tests the pin against the emitted `cephadm` spec
set. Querying the pinned spec directly would have collapsed those failures into
a single empty result — a declared repository that is unreachable, one that
carries no Ceph content, and one that is merely a z-stream behind the pin are
three different problems with three different remedies. The command-failure
message prints the exact node-side argv and every apply refusal carries the
controller-resolved invocation, while the pin assert reports the builds that
*are* published.

It fails the apply even when every node already carries the pinned build. A
pinned `dnf install` short-circuits on an already-installed package without
consulting the repository, so a fleet can run for months on a pin its
repositories cannot serve, and the break surfaces only when the next node is
installed or rebuilt. Converging on the repository rather than on the disk is
the point of the preflight.

No external-registration escape hatch is carved out here. The `ibm` arm never
renders `rhsmManagement: external`: its own entitlement type forbids an `rhsm`
block, and the OS-subscription fallback resolves managed registrations only.
An allowance keyed on it would be unreachable in every state that validates,
so `subscriptionRepos` is required whenever the source is `subscription`,
whatever shape the surrounding entitlements take.

Independently, the storage-phase purge keep-set becomes the union of the
provider repositories and the host's machine-profile
`subscription.enable[]` ids. The purge stays: undeclared repositories are
still swept. But the machine-level declaration is authoritative — the storage
phase now agrees with the earlier writer instead of stomping it, on every
subscription-backed distribution.

The block is a pointer with `omitempty`: absent, it serializes to nothing and
every existing fleet's hashes stand still. Authored, it moves the desired hash
(a real declared change) but is stripped from the storage structural hash and
the managed-OS structural hash, so a source flip is reconcilable drift — one
values edit, one storage apply, no rebuild pressure.

## Consequences

- A Satellite-backed IBM fleet needs no egress to `public.dhe.ibm.com` at
  all; the disconnected-egress table's IBM row gains its first skip condition.
- Rollback is symmetric: delete the block and the next storage apply
  re-fetches the vendor `.repo` and the old purge baseline returns.
- The union changes shared behavior once: the first storage apply after
  upgrade reports `changed` where a machine-declared repository gets
  re-enabled, then converges. A fleet that declared a repository at the
  machine phase while relying on the storage phase to disable it flips
  behavior; that contradiction was the bug this ADR removes.
- The preflight trusts dnf metadata freshness; a content view republished
  since the node's last refresh can need `dnf clean metadata`, which the
  failure message names.
- An unrelated enabled repository cannot fail this preflight, and a declared
  repository cannot be silently skipped. A failed read and missing package
  content therefore retain distinct, stable operator remedies.
- A provided arbiter on an isolated network now needs Satellite reachability
  for the enable and the preflight, like every other storage node.
- `subscriptionRepos` ids are opaque Satellite labels; validation is
  syntax-only and a typo surfaces at the storage phase as rhsm_repository's
  invalid-id error, the same failure point machine-phase repos have today.
