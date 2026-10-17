# Bootstrap daemons can appear as inferred unmanaged services

## Description and stored intent differ

The native orchestrator's
[service description implementation](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py)
describes discovered daemons even when its specification store has no entry
for their service. It synthesizes an unmanaged ServiceSpec for that case.
Enrollment can therefore make the bootstrap MON/MGR visible as services
without a service-apply attempt having created a stored specification.

The orchestrator's
[export command](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/orchestrator/module.py)
exports these descriptions' specifications alongside actual stored intent.
Comparing every exported value with the final desired ServiceSpec conflates
the two. A fixture that starts with an empty map and only echoes applied input
cannot reproduce discovery of those inferred entries.

The full
[ServiceDescription representation](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/orchestrator/_interface.py)
retains status metadata that export discards, including creation metadata for
stored specifications. That distinction must be combined with the exact
inferred shape and existing bootstrap ownership, daemon and transport proofs;
neither an absent timestamp nor an unmanaged flag alone proves ownership.
The [storage contract](../../specs/storage.md#bootstrap-identity-and-continuation)
owns the required admission and effect boundaries.

## Exercise discovery and continuation together

Keep the native description fixture independent of submitted input. Carry
bootstrap and enrollment state into service reconciliation with both inferred
entries present, then exercise normal desired apply, lost responses and
unchanged continuation. Include a stored unmanaged lookalike, foreign entries,
malformed metadata and completed-service drift as refusal cases. Do not erase
specifications or manufacture ownership markers to make the fixture pass.

An automation correction may continue the same owned partial operation when
its frozen contract and runtime are unchanged; see
[continuation after a fix](continuation-after-a-fix.md). That does not authorize
restarting an attempted bootstrap or ignoring a contradictory native read.
