# ADR 0040: One Word for Every Token a Verb Accepts

## Status

Accepted

Extends [ADR 0030](0030-one-intent-flag-and-named-authorizations.md) with one
token, `all`, and states the single exception to that ADR's "each token unblocks
exactly one refusal" rule.

## Context

ADR 0030 replaced `--force` and eight sibling booleans with named tokens so that
an operator authorizing one risk could not silently authorize four others. That
decision holds and is not reopened here. What it did not settle is the operator
who wants to authorize all of them, knowingly, in one word.

Two workflows produce that operator, and neither is careless.

The first is a teardown that surfaces refusals one at a time. A `destroy` reads
its gates in order — stale input, unreadable records, shared infra, an installed
cluster node, protection, unreachable nodes, data loss — and each refusal ends
the run. A lab whose state has drifted far enough to trip several of them costs
one full run per refusal to discover the next, with the operator re-typing a
lengthening `--authorize a,b,c,d` list that is pure transcription. Nothing is
learned after the second round: the operator has already decided to flatten the
context, and the remaining rounds only reveal which order the gates run in.

The second is the disposable environment — a lab, a CI fixture, an e2e teardown
— where the state has no value by construction and the authorization ceremony
protects nothing. Scripts there ended up carrying the full token list inline,
which is worse than a blanket token in two ways: the list is copied between
scripts and goes stale when the vocabulary changes, and a reader cannot tell
whether it was assembled deliberately or pasted.

The token vocabulary is also a moving target by design — ADR 0032, ADR 0034 and
ADR 0038 each added one. Every enumerated list an operator maintains outside the
product is a list the product will outgrow.

## Decision

**Add one token, `all`, registered for both verbs, defined as the union of the
tokens its own verb accepts.**

`all` is not a new gate and not an override. It is resolved per verb, at the
point each gate consults the authorization set: a gate asking whether token *T*
is authorized is answered yes if *T* was named, or if `all` was named and *T* is
a registered token of the verb being run. Three properties follow from that
definition rather than from extra rules.

It cannot cross verbs. `apply --authorize all` answers for `data-loss`,
`unowned-devices` and `foreign-daemons` and for nothing else, because those are
the tokens `apply` accepts; the destroy-only tokens are not in its set, and
`destroy --authorize all` likewise leaves `foreign-daemons` out. It does not
answer for `protected`, which `apply` has no gate for and which ADR 0031 already
makes a usage error to pass there — a blanket token that granted it would be
granting a gate that does not exist.

It cannot reach a refusal that has no token. The scope-conflict refusals, the
KubeVirt tenant gate, a mounted or in-use device, a probe that failed, a
`protectedKinds` object under `apply --mode rebuild` — none of these is
authorizable by any token, so none is authorizable by the union of tokens. The
"no token widens `--clusters`" clause needs no exception written for `all`; it
is already true of it.

It does not answer a confirmation prompt. `--yes` and `--authorize` stay the two
things ADR 0030 made them, and `all` lives entirely on the authorization axis: a
`destroy --authorize all` with no `--yes` still stops at the confirmation, and
`--yes` alone still authorizes no named risk.

**A run that used `all` says what it stood in for.** The unused-token warning of
ADR 0030 exists so an operator who authorized the wrong risk learns it instead
of believing a gate was cleared; a blanket token needs the same disclosure
pointed the other way. A real run prints the tokens `all` answered for and that
the command line did not name, so the blast radius appears in the run's own
output rather than only in the reader's head. A token named alongside `all` is
credited to its own name, so `all` reports having had no effect when every gate
the run consulted was named explicitly — the same honesty the per-token warning
already provides.

## Consequences

"Each token unblocks exactly one refusal" is now true of every token but one,
and is restated that way in the three places the vocabulary is published. The
existing contract test keeps those tables and the registry in agreement, and the
scenario matrix exercises `all` on both verbs — that it clears what the named
token clears, that its expansion reaches the per-token extra vars, that it
discloses what it stood in for, and that it still does not widen a selection or
clear a tokenless refusal.

The blast radius is the point, and it is larger than any other token's: on
`destroy` it is every refusal the product can be argued out of, including
walking away from unreadable records and leaving a cluster half-destroyed by
skipping unreachable nodes. The documentation says so where the token is
published, and names the two workflows it is for rather than presenting it as a
convenience.

`all` also makes the vocabulary's growth invisible to the operator who wants
everything: a token added by a future ADR is inside `all` on the day it lands,
with no script to update. That is the intended behavior, and it is the reason
the disclosure is not optional — the set `all` expanded to is only knowable from
the run that expanded it.

## Alternatives Rejected

**Let `--yes` imply every token.** This is `--force` returning under a new
spelling, and it is exactly what ADR 0030 removed: `--yes` would again mean
opposite things on the two verbs, and every existing `--yes` script would
silently gain authorizations it never asked for. The two axes stay orthogonal.

**A separate `--authorize-all` boolean.** A second flag on the authorization
axis reintroduces the coupling ADR 0030 collapsed — operators would then have to
learn how the flag and the token list interact when both are given. A token
composes with the existing list by construction and needs no interaction rule.

**Accept it silently, with no disclosure.** The unused-token warning is the
product's answer to "did the gate I think I cleared actually clear"; a blanket
token without the mirrored answer would be the one authorization whose effect
the operator cannot read back. It would also make the growth property above a
hazard rather than a feature.

**Refuse `all` combined with named tokens as a usage error.** Strictness with no
payoff: the combination is unambiguous, and refusing it would punish the
operator who assembled a list and then decided to widen it. The crediting rule
above already reports the combination honestly.
