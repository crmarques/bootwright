# ADR 0042: Moving the Vote That Breaks the Tie

## Status

Accepted

Adds the `storage-cluster replace-arbiter` verb, a third authorization verb, and
the `same-site-arbiter` and `degraded-quorum` tokens; extends
[ADR 0030](0030-one-intent-flag-and-named-authorizations.md) and widens the
`unreachable-nodes` token it defines.

## Context

A stretch Ceph cluster's tiebreaker is one mon in a third site whose only job is
to break a tie between the two data sites. It is also the one part of the
topology `apply` cannot move.

`apply` converges the authored topology, and it does so correctly for a cluster
being built: the mon service is applied, the mons join, and
`ceph mon enable_stretch_mode <tiebreaker> …` names the arbiter once. Once that
has run, the arbiter is a property of the live monmap, not of the mon service.
Re-authoring `spec.ceph.topology.stretch.tiebreaker` and re-applying changes the
rendered spec and nothing else: `enable_stretch_mode` is idempotency-guarded and
skips, `set_new_tiebreaker` is never issued, and the mon being replaced is never
retired. The cluster keeps the old arbiter and now disagrees with its input.

Operators need this move for ordinary reasons — a data centre going down for
maintenance, arbiter hardware being replaced — and for one disaster: the third
site is gone and the cluster is running on two.

Ceph is specific about what it will accept. `ceph mon set_new_tiebreaker` refuses
unless stretch mode is on, the named mon is in the monmap, it carries a location,
that location names the stretch dividing bucket, and the location matches no
non-tiebreaker mon. It also does not remove the mon it replaces. A mon deployed
into a cluster already in stretch mode is itself rejected without a location, and
cephadm only passes one when the mon *service spec* carries `crush_locations`.
So the operation is not one command; it is an ordered procedure with five
preconditions, and every one of them is a way to get a half-moved cluster.

Doing it by hand is what the Red Hat and IBM runbooks describe, and it is exactly
the kind of multi-step, precondition-heavy procedure that goes wrong at 03:00
during the outage that motivated it.

## Decision

**A verb, not a flag on `apply`.** `bootwright storage-cluster replace-arbiter
--name <cluster>` performs the move. It is not a new intent axis on `apply`:
`apply` converges desired state, this reconciles one live property that desired
state cannot reach on its own, and folding it into `apply` would put a
site-outage procedure behind the verb operators run routinely.

**Desired state stays the source of truth.** The verb reconciles the live
tiebreaker onto `spec.ceph.topology.stretch.tiebreaker`. `--new-arbiter-machine`
is a convenience that *authors* that intent — it rewrites the context input
through the one snapshot-then-write mutation component and then reconciles — so
the input is never left disagreeing with the cluster the run just changed.
Omitting the flag reconciles what the operator authored by hand. There is no mode
in which the command changes a cluster without the input saying so.

**Candidates are declared, not discovered.** The `ceph-arbiter` Machine
capability marks a machine eligible to hold a tiebreaker whether or not it holds
one today, which is what makes "move the arbiter to the standby" expressible at
all. It is the vocabulary's one forward-looking value; the rest restate a binding
or assert a present property. It requires `ceph-node`, and a candidate bound as a
node by another cluster is refused.

**Add before removing, and prove before swapping.** The run prepares and installs
the replacement machine and Ceph on it, deploys its mon with the stretch location,
and waits for that mon to be in the monmap *and* in quorum *and* located —
Ceph's own preconditions, checked by Bootwright rather than discovered as an
EINVAL — before `set_new_tiebreaker`. Only afterwards is the replaced mon
retired. Everything ahead of the swap adds capacity and removes nothing, so any
failure there leaves the original arbiter holding the tiebreaker with the
cluster's quorum intact. Every step reads live state, so a re-run resumes, and a
run whose desired arbiter already answers as `tiebreaker_mon` is a no-op.

**The mon service spec carries the location.** `spec.crush_locations` is rendered
for every sited mon of a stretch cluster. This is upstream's recommended
mechanism for replacing tiebreaker mons and is distinct from the host-spec
`location` that ADR-adjacent knowledge forbids on a tiebreaker: the host spec
creates a CRUSH failure-domain bucket, `crush_locations` sets the mon's monmap
location.

**Three refusals, three tokens.** `same-site-arbiter` authorizes promoting a mon
that shares a failure domain with the data-site mons — the emergency fallback
when the third site is gone, and Ceph's own `--yes-i-really-mean-it` path.
`degraded-quorum` authorizes moving the tiebreaker while declared mons sit
outside quorum. `unreachable-nodes`, already defined by ADR 0030 for `destroy`,
widens to this verb with the same evidentiary rule: absence must be *proved* by
the probe, and a node that answered and refused an identity is never treated as
gone.

**A third authorization verb.** `--authorize` was keyed to `apply` and `destroy`;
it is now keyed to a set, `all` covers this verb's tokens as it does theirs, and
every token that does not reach all three verbs carries the guidance printed when
a verb that cannot consume it is given it.

**Machine teardown stays a `destroy` decision.** The replaced machine keeps
running with its OS intact; only its Ceph membership is removed. Embedding a
substrate teardown here would duplicate the destroy safety model — ownership
records, protection, data-loss authorization, record resets — in a second place
where the two could disagree.

## Consequences

- One live property of a Ceph cluster is now reconciled by a verb rather than by
  `apply`. If a second such property appears, it belongs under `storage-cluster`
  beside this one, not as an `apply` flag.
- The mon service spec changes shape for every existing stretch cluster, so the
  first apply after this lands re-applies the mon service. cephadm reconciles it
  and already-deployed mons keep the locations they have; nothing restarts.
- `--machines` teardown of the replaced machine must happen while it is still a
  declared node. Once the input is re-authored the machine has no provisioning
  work and `destroy --machines` fails closed by its own rule. Teaching destroy to
  tear down a machine that has left desired state but still carries an ownership
  record is a separate change, deliberately not made here.
- The verb reaches the cluster before it can plan, so it fails closed where
  `apply --dry-run` would have planned offline. That is intended: there is no
  honest plan for "replace the arbiter" without knowing which mon holds it.
