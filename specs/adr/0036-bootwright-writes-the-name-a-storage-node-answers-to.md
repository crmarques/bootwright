# ADR 0036: Bootwright Writes the Name a Storage Node Answers To

## Status

Accepted

Revises [ADR 0035](0035-a-storage-node-answers-to-the-name-cephadm-registers.md),
which enforced the hostname contract but left writing it to the operator for
`os.provided: true` machines.

## Context

ADR 0035 turned an unverified requirement into a gate: `apply` refuses a storage
node whose kernel hostname is not the name cephadm will register, before the run
touches the cluster. It deliberately stopped there. "Bootwright does not write
the hostname of a machine it did not install" — establishing the name stayed with
the installer for a managed OS and with the operator for a provided one, on the
grounds that writing an OS hostname onto a machine the operator's organization
owns carries a Satellite consumer-identity hazard.

That hazard does not survive inspection of what Bootwright actually does. A
provided machine is never registered and never re-registered: the registration
task narrows its host list to machines whose OS Bootwright installs, and the
registration itself passes only `state`, `org_id` and `activationkey` — no
consumer name, no forced re-registration. A Candlepin consumer is named at
registration and is not re-derived from the hostname afterwards, so renaming a
registered host cannot produce a duplicate consumer or trip the DMI-UUID
collision that a re-register would. What remains is a stale display name in
Satellite, which `subscription-manager facts --update` refreshes and which no
Bootwright behavior depends on.

Meanwhile the gate's remedy was expensive out of proportion to the fix. The
message named two ways out, and both cost the operator a decision that Bootwright
had all the information to make: log in to the machine and set a hostname the
run already knows, or restructure the input so the cluster adopts the machine's
existing name — which, for a stretch tiebreaker, means the arbiter's foreign
hostname appears in `orch host ls` beside six nodes named by the estate's own
convention.

The tool Bootwright drives already takes the other position. cephadm ships
`prepare-host --expect-hostname <name>`, whose entire purpose is to reconcile a
host whose name does not match what the cluster expects, by writing it. Refusing
to do what the underlying tool does on request is not conservatism; it is a gap
the operator fills by hand on every provided node, once per node, forever.

## Decision

**`apply` writes the name cephadm will register onto every storage node.** The
`node_identity` phase, after resolving the host's declared entry and before
anything else runs, sets the OS hostname to that node's `cephHostname`. The write
is idempotent — a node already holding the name reports no change, which is every
machine whose OS Bootwright installed — and it persists, so the name survives a
reboot rather than living until the next boot.

**The gate stays, as a post-condition.** Facts are re-read after the write and
the assert runs against the refreshed kernel nodename, unchanged in its
comparison: exact, no short-name fallback, no case folding. It can now only fire
when the write reached the machine and did not hold, which means something on the
machine owns the name — cloud-init without `preserve_hostname: true`, or a
configuration-management agent that stamps hostnames. The refusal says so, and
still names `nodes[].fqdn` as the way to keep the machine's existing name.

**`nodes[].fqdn` changes meaning from escape hatch to instruction.** It was the
way to tell Bootwright which foreign hostname a machine already had; it is now
the way to tell Bootwright which hostname to write. The mechanism is identical —
`Normalize` resolves it into `node.Name`, which is rendered into the host spec and
is what the phase writes — but the operator authoring it is now choosing the name
the machine will carry, not describing one it carries already.

**The preflight stops refusing what `apply` repairs.** `check_storage_preflight`
reports a hostname `apply` will rewrite and passes. A read-only check that fails
on a condition the next command fixes is a false blocker.

## Consequences

- A provided storage node needs no hostname preparation. Declaring it and running
  `apply` is sufficient, which is the same contract a Bootwright-installed node
  has always had.
- `node_identity` now requires privilege on every storage host, at the head of
  the role rather than in the phases that install packages. A node whose login
  cannot `become` fails there instead of later; the run needed that privilege
  either way.
- Bootwright now changes a property of a machine the operator's organization
  owns. That is the point of the decision, and it is bounded: the name written is
  the one the input already declares, and authoring `nodes[].fqdn` makes the
  machine's existing name the declared one.
- Satellite keeps the display name a renamed host registered under.
  `subscription-manager facts --update` refreshes it. Nothing in Bootwright reads
  it.
- ADR 0035's second decision is reversed. Its first — that the name is checked
  before Bootwright touches the cluster — holds unchanged, and its third, the
  `fqdn` token resolution, is what makes the new meaning of `fqdn` workable.
