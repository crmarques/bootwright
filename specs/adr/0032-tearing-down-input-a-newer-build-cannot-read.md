# ADR 0032: Tearing Down Input a Newer Build Cannot Read

## Status

Accepted

Extends [ADR 0030](0030-one-intent-flag-and-named-authorizations.md) with one
token. Supersedes nothing; the flag-day rule for `apply` stated in
`docs/contributing/api.md` stands unchanged.

## Context

`v1alpha1` carries no aliases and no migrations. A breaking schema change is a
flag-day: stale desired state is expected to fail strict decode, and the operator
re-authors it. That rule was written for `apply`, where it is unambiguously
right — building from input the binary cannot fully read would mean guessing.

`destroy` inherited the same rule by construction, and there it produces a trap.
Teardown is planned from the context's *stored* input plus the ownership records,
and both loads happen before a record is read or a host contacted: the locality
pre-check strict-decodes the whole input tree, and the state load additionally
runs full semantic validation. So the sequence

1. apply a lab,
2. rebuild Bootwright across a breaking change,
3. destroy the lab

fails at step 3 with a decode error, and every remedy is out of band: re-render
the entire input tree keeping every applied identity byte-identical, rebuild the
binary that applied it from git, or hand-edit files under a root-owned
`/var/lib/bootwright/contexts/<name>/input`. The refusal is also all-or-nothing —
one stale document blocks destroying every object in the context, including
objects unrelated to that document.

This is not a corner case for a tool whose own development ships breaking
changes as routine. It is the normal consequence of rebuilding `main` while a lab
is standing, and it converts a disposable lab into a manual cleanup job. The
only command that still functions is
`context delete --purge --abandon-resources`, which works *because* it loads no
input — and which abandons the running infrastructure and deletes the
kubeconfigs needed to clean it up by hand. The trap therefore pushes operators
toward the one action that guarantees residue.

Two further observations shaped the decision. First, the symmetric problem
already has an answer three lines away in the same function: an ownership record
that cannot be read does not abort the teardown, it refuses by default and is
cleared by `--authorize unreadable-records`, which discloses exactly what would
be left standing. Second, semantic validation protects the *coherence of a
build*; a teardown plan derives from the object graph and the ownership records,
so a dangling reference in an object the run will not touch has no bearing on
whether the teardown is safe.

## Decision

Add one token, `stale-input`, registered for the `destroy` verb only.

A tolerant load path collects per-document decode failures and continues instead
of aborting at the first one. It is used at both destroy load points. Nothing
that decodes cleanly is skipped, no validator is rewritten or relaxed, and the
tolerant entry points are reachable from the destroy path alone.

Without the token, `destroy` still exits non-zero on any skipped document — now
naming the offending documents and the `context update` remedy rather than
surfacing a bare decode error. `--dry-run` refuses too, so the whole-input
validation contract holds for every form of the command; the token is what makes
a dry run tolerant, giving the operator a preview of the blast radius before
authorizing a real run.

With the token, every skipped document is printed, and the ownership records
whose declaring object was dropped are reported in the existing "Owned but no
longer declared" section as left standing.

The token relaxes exactly one refusal. `data-loss`, `protected`,
`installed-cluster-node`, `unowned-vms`, `unowned-networks`,
`unreachable-nodes`, `unreadable-records`, and `shared-infra` are unchanged; so
are device data-safety checks, the Ceph seed ownership proof, the active-run
check, and the confirmation prompt. All of them are still evaluated against
whatever did decode.

Because the `--authorize` registry is per-verb, `apply`, `plan`, `diff`, and
`context init`/`update` reject the token by name with the guidance that resolves
the problem on those verbs. Nothing that creates state can build from input it
cannot fully read.

## Consequences

A lab can be torn down after a breaking schema change without re-authoring its
input first, and the operator is told precisely what the skipped documents cost
them. The `abandon-resources` path stops being the only one that works.

The cost is a second, deliberately narrow loader whose behaviour must stay
identical to the strict one when nothing is skipped — enforced by the strict
entry points delegating to the collecting ones with a nil collector, so there is
one implementation of the per-document decode.

An operator who authorizes `stale-input` on a context whose *cluster* documents
are the stale ones gets a teardown that leaves those clusters standing. The
disclosure is what makes that safe rather than silent, and the refusal remains
the default precisely so the token is a decision, not a fallback.
