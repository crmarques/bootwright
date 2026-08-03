# Replacing the stretch arbiter: the supported order, and why each step exists

**Constraint:** `ceph mon set_new_tiebreaker <mon>` refuses unless every one of
these holds, and the refusals are the reason the procedure has the shape it has
(`src/mon/MonmapMonitor.cc`):

- stretch mode is on — *"Stretch mode is not enabled, so there is no
  tiebreaker"*;
- the named mon is in the monmap — *"mon.\<name\> does not exist"*;
- it carries a monmap location — *"mon.\<name\> does not have a location
  specified"*;
- that location names the stretch dividing bucket — *"has a specificed location,
  but not a \<bucket\>, which is the stretch divider"*;
- and the location matches no **non-tiebreaker** mon — *"has location X, which
  matches mons Y on the \<bucket\> dividing bucket for stretch mode. Pass
  --yes-i-really-mean-it if you're sure..."*.

The last one is what makes the *replacement* case legal at all: the mon being
retired is the current tiebreaker, so its location does not count against the
newcomer, and both arbiters may sit in the third site at the same time.

**Constraint:** `ceph mon set_new_tiebreaker` does **not** remove the previous
tiebreaker. Red Hat and IBM both state it explicitly ("This command WILL NOT
remove the previous tiebreaker monitor"), so retirement is a separate,
explicitly ordered step — never assumed.

**Constraint:** with stretch mode already enabled, a mon added without a
location is rejected (*"We are in stretch mode and new monitors must have a
location..."*). A mon cephadm deploys only receives `--set-crush-location` when
the mon **service spec** carries `spec.crush_locations: {<host>: [<domain>=<site>]}`,
and upstream calls that "the recommended way of replacing tiebreaker mon
daemons, as they require having a location set when they are added". Bootwright
therefore renders `crush_locations` on the mon service for every sited mon of a
stretch cluster. This is NOT the host-spec `location` that
[ceph-stretch-mode-constraints.md](ceph-stretch-mode-constraints.md) forbids on
the tiebreaker: the host spec creates a CRUSH failure-domain bucket (and a third
one breaks `enable_stretch_mode`), while `crush_locations` sets the mon's
*monmap* location, which is exactly what the tiebreaker needs.

**Order (RH/IBM "Replacing the tiebreaker with a new monitor", adapted to a
desired-state run):**

1. enrol the replacement host — `ceph orch host add <node> <addr> --labels=mon`;
2. apply a mon spec whose placement holds **both** arbiters, with
   `crush_locations` for every sited mon;
3. poll `ceph mon dump` until the new mon is in the monmap **and in quorum**
   **and** carries `<domain>=<site>` — all three of Ceph's preconditions, proved
   before the swap rather than discovered as an EINVAL half-way through;
4. `ceph mon set_new_tiebreaker <new-mon>`, then re-read `mon dump` and assert
   the swap took;
5. apply the final mon spec (placement without the retired host), then
   `ceph orch daemon rm mon.<old> --force`, `ceph mon rm <old>` if the monmap
   still carries it, `ceph orch host drain <old>` and `ceph orch host rm <old>`.

Steps 1–3 add capacity and remove nothing, so every failure before step 4 leaves
the original arbiter holding the tiebreaker and the quorum intact. That is why
the readiness gate sits between 3 and 4 rather than after the swap.

**Behavior:** the procedure is resumable by construction. Step 3's poll and the
`set_new_tiebreaker` guard both read live state, so a re-run after a failure at
any step picks up where it stopped.

**The no-op predicate is not `tiebreaker_mon`.** "Resume" and "nothing to do"
must not read the same variable. `tiebreaker_mon == desired` becomes true at
step 3, while the retirement in step 4 is still outstanding, so keying the no-op
on it makes every re-run after a post-swap failure exit 0 with the replaced mon
still voting and its host still enrolled — and `apply` then refuses on the
tiebreaker drift the interrupted run created and routes back to a command that
reports nothing to do. Settled therefore means the tiebreaker matches **and**
there is no retirement residue: no mon in the monmap that the authored topology
declares no node for, and no such host still running a mon daemon. The residue
also names what to retire, so the resumed run knows its target without the
promotion that first identified it. Two or more undeclared mons is ambiguous and
refuses rather than guessing which one an interrupted run left behind.

**A mon daemon name is not a cephadm host name.** `mon rm` takes the daemon
name; `orch host drain`/`orch host rm` take the hostname cephadm registered,
which is the FQDN the node identity wrote (ADR 0035/0036). Resolve the host from
`orch host ls` and keep the two names apart in the rendered vars — deriving the
host from the monmap works only while short name and registered hostname
coincide, and `orch host rm` treats `Unknown host` as success, so a wrong name
retires nothing and reports success. `verify.yml` asserts against `orch host ls`
for that reason, not against the monmap alone.

**The plan must be recomputed after the input is re-authored.**
`--new-arbiter-machine` writes the bare machine name into the input, but the
loader composes node names into FQDNs from `Environment.spec.domains`. A plan
built from the in-memory promotion therefore carries a bare name that
`ceph orch host add` fails `check-host` on in any domain-bearing context. Reload
the state after the rewrite, recompute the plan from it, and re-run the
authorization gates against the recomputed plan — the loader stays the single
owner of node-name normalization, and no gate is skipped because the plan
changed under it.

**Refreshing the converge records must not rebaseline what the run never
converged.** The arbiter playbook converges the tiebreaker only, so re-stamping
every `storageCluster` record with the current desired hash also marks any
unrelated edit in the same commit as reconciled, destroying the drift refusal
that would have caught it. Refresh a record only when it still matches the
desired state as it stood *before* the rewrite; otherwise leave it drifted and
name the `bootwright apply` that converges the rest.

**When it bites — the disaster variants:**

- **The old arbiter host is gone.** `ceph orch host drain` needs the host; skip
  it and use `ceph orch host rm --offline --force`, plus an explicit
  `ceph mon rm` because the daemon path cannot reach the daemon. Gated, never
  inferred: a host that answers and *refuses an identity* is not an absent host
  (same rule as `unreachable-nodes` on destroy). The token alone is not the
  proof, and this lane cannot borrow destroy's proof — the replaced arbiter left
  the topology, so it is in no play's inventory and `ignore_unreachable` never
  sees it. The orchestrator's own view is the evidence: take the offline path
  only when `ceph orch host ls` reports that host offline. The token widens what
  the run *may* do, so a supplied token against a host that answers degrades to
  the normal drain-and-remove and says so — it must not refuse, or
  `--authorize all` would abort every healthy retirement after the tiebreaker
  has already moved. The reverse case (host offline, token absent) is the one
  that fails closed, and the refusal names the token.
- **Mons outside quorum.** `set_new_tiebreaker` needs a quorum to commit, and
  swapping the arbiter while a data site is down removes the vote holding the
  remaining quorum together. Refuse by default.
- **No third site available.** Promoting an in-quorum data-site mon is RH/IBM's
  separate "Replacing the tiebreaker with a monitor in quorum" procedure — it
  needs `--yes-i-really-mean-it` and leaves the cluster without a real arbiter
  (lose that site and two votes go at once). It is a deliberate emergency
  fallback, so it is gated on its own token rather than shared with the healthy
  path.

**Sources:** Red Hat Ceph Storage 7/8 *Troubleshooting Guide* — "Troubleshooting
clusters in stretch mode"; IBM Storage Ceph 7.1 — "Replacing a tiebreaker with a
new monitor" and "Replacing a tiebreaker with a monitor in quorum"; upstream
`doc/rados/operations/stretch-mode.rst` and `doc/cephadm/services/mon.rst`.
