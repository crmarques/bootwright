# ADR 0054: A Filter Is Not Permission to Wipe a Device

## Status

Accepted

Refines [ADR 0007](0007-apply-destroy-safety-model.md) and
[ADR 0030](0030-one-intent-flag-and-named-authorizations.md): rebuild intent and
data-loss authorization do not waive an OSD device-selection gate. Refines
[ADR 0034](0034-wiping-a-device-no-node-claims.md): explicit-path reclaim remains
the only in-product wipe for a narrowing selector; automatic reclaim is limited
to an effectively unbounded managed data selection.

## Context

An explicit OSD path lets Bootwright probe the exact device before cephadm sees
the service spec. A dynamic selector uses `all`, `model`, `vendor`,
`rotational`, or `size` without `paths`/`pathSpecs`, optionally capped by
`limit`; it names a changing set that only host inventory can resolve.
Bootwright therefore left those selections to ceph-volume and detected a
rejected dirty disk only at the later OSD-readiness wait.

The automatic recovery for `dataDevices.all: true` made that gap destructive.
The consequence predicate tested only the `all` field, then the Ansible role
zapped every unavailable, unmounted, non-live-OSD disk on the host. Bootwright's
validation did not yet mirror cephadm's DeviceSelection grammar, so it accepted
`all` or explicit paths alongside narrowing predicates; `limit` could cap an
`all` selection; and `unmanaged: true` made the service consume nothing.
Collapsing all of those meanings into one boolean let
`--mode rebuild --authorize data-loss` wipe a disk the authored selection would
not consume, even when cephadm would later reject the ambiguous service spec.

The same missing gate affected dynamic DB and WAL selectors. Letting ceph-volume
refuse those devices was too late: the persistent OSD service had already been
applied, the failure did not name a safe recovery command, and a future inventory
change could make the service select a different disk.

## Decision

Desired-state validation mirrors cephadm's DeviceSelection grammar before any
runtime classification:

- `paths` and `pathSpecs` are mutually exclusive, and either explicit-path form
  is mutually exclusive with `all`, `model`, `vendor`, `rotational`, and `size`;
- `all` is allowed only for `dataDevices` and is mutually exclusive with
  `model`, `vendor`, `rotational`, and `size`;
- `limit` may cap another selector but is not a selector by itself.

Every managed dynamic data, DB, and WAL selector is rendered into the host
inventory with its role, effective `filterLogic`, and authored filter fields.
That rendered value is the one Go-to-Ansible contract for the device gate; the
role does not reconstruct desired selection from a service-spec file.

After cephadm has admitted the hosts, before any optional auto-reclaim and before
Bootwright applies the persistent OSD service, a read-only gate:

- requires complete device inventory for every dynamically selected host and
  readable live-OSD metadata;
- evaluates every unavailable, non-live-OSD device against the authored
  selector, treating a missing inventory field or an unevaluable filter as
  unknown rather than as a non-match;
- probes each matching candidate on its owning host, excluding mounted devices
  and refusing an unreadable mount or signature probe; and
- refuses every unmounted matching device that carries a signature, except that
  an effectively unbounded selection already authorized for auto-reclaim may
  pass that candidate to the reclaim step.

Unknown state fails closed before the OSD service is applied. No apply mode or
authorization token converts an unreadable probe into evidence.

`--mode rebuild` and `--authorize data-loss` do not bypass a narrowing selector's
refusal. To reclaim a refused disk in-product, the operator must first pin that
device with `paths` or `pathSpecs`, then run the exact-path reclaim named by the
refusal. That runnable command is controller-built from the resolved invocation,
not assembled from the task-local cluster. It preserves the operator's mode,
context, selected clusters or machines, range, prior reclaim paths,
authorizations, dry-run/output/confirmation, and SSH flags; unions
`data-loss,unowned-devices`; and places one sentinel as the entire
`--reclaim-devices` argv value. The role validates and comma-joins the prior
controller-resolved paths with the nonempty runtime path set, shell-quotes that
one operand, and replaces only the sentinel. An empty, unrepresentable, or
multi-sentinel value refuses before rendering a command.

Automatic reclaim remains available only when all of these are true:

- the OSD service is managed;
- the data selector sets `all: true`;
- it sets no `limit`.

That is an *effective unbounded managed data selection*, not merely an authored
`all` field. Validation already forbids combining `all` with narrowing filters.
Only `apply --mode rebuild --authorize data-loss` may auto-reclaim its unavailable,
unmounted, non-live-OSD candidates. The CLI warning, the
authorization consequence, the rendered `osdReclaimAll` value, the read-only
gate, and the reclaim task all consume that same classification.

`--reclaim-devices all` remains static-path shorthand, never dynamic wildcard
permission. When it resolves no static path, a typed evidence-only controller
error lets the CLI render the safe continuation. When every selected cluster is
effectively unbounded, the exact resolved invocation is changed to rebuild, with
the incompatible reclaim flag removed and `data-loss` unioned. A narrowing
selector or a mixed narrowing/unbounded selection requires the desired path edit
and then repeats the exact original invocation. Mixing `all` with paths is a
command-shape error and needs no runnable retry.

`limit` never reduces the safety gate to an arbitrary first N devices. A cephadm
OSD service persists after the run and may select a different matching device
when availability changes, so every dirty matching device remains a possible
future target and is refused.

## Consequences

- A dynamic filter now fails early with the host, path, reason, and exact safe
  remedy instead of failing late with zero OSDs.
- Every authorized auto-reclaim zap is asserted before the persistent service;
  a failed zap reports its process evidence and exact scoped retry instead of
  being rediscovered by an exhausted OSD-readiness wait.
- Rebuild remains useful for owned structural replacement without becoming a
  general disk-wipe override. `data-loss` authorizes a known destructive
  consequence; it does not choose which disk is in scope.
- A narrowing filter costs one desired-state edit before an intentional wipe.
  That friction is deliberate: the path becomes reviewable operator intent.
- The filter-field set and the effective-unbounded predicate are closed by tests
  across validation, renderer, filter evaluator, CLI consequence, and Ansible
  ordering, so a new selector field cannot silently inherit wipe authority.
- Runtime-path retry rendering is closed by the mutation-variable registry, the
  no-literal Ansible guard, and a hostile-path shell round trip that proves the
  sentinel replacement cannot add argv or drop an existing flag.
- The read-only evaluator mirrors `AND`/`OR` short-circuiting: a known false
  decides `AND`, a known true decides `OR`, and a missing fact refuses only when
  the remaining facts cannot determine whether cephadm could choose the disk.

## Alternatives Rejected

**Treat every `all: true` as permission to zap the host.** An `all` field is not
the effective managed selector. This is the bug the decision closes.

**Let rebuild plus data-loss waive the filter gate.** The flags express cluster
rebuild intent and acceptance of data loss; neither names a path. Combining them
must not manufacture device selection the operator did not author.

**Rely on ceph-volume and the readiness wait.** ceph-volume is the final
consumer, but its late refusal follows a persistent state change and cannot give
the operator Bootwright's exact safe recovery workflow.

**Apply `limit` to the current inventory and gate only those devices.** Cephadm's
persistent service can choose a different matching disk later, so a controller
snapshot cannot prove that the remaining matches are outside future scope.
