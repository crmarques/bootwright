# ADR 0056: The Support Report a Storage Node Can Produce

## Status

Accepted

Follows the provider-owned dependency model of
[ADR 0002](0002-open-provider-dispatch.md) and the explicit lifecycle boundary
of [ADR 0007](0007-state-model-safety-and-convergence.md).

## Context

Bootwright installs the packages a managed Ceph node needs to run, but omitted
`sos`. The vendor's own storage readiness check includes `sos` in its required
infrastructure package set, so it reports a Bootwright-built node as incomplete
at the moment an operator needs to collect a support report. Minimal managed-OS
installs do not otherwise carry the package.

Installing diagnostic tooling only during an incident is weaker: it depends on
the same repository, subscription, proxy, and trust paths the incident may have
broken. Treating `sos` as an untracked convenience would also let destroy remove
an operator-owned package or leave a Bootwright-installed package without a
lifecycle record.

## Decision

The managed Ceph provider prerequisite set includes `sos` on every storage node.
Apply installs it through the shared package-ownership role with the same
`storage-cluster:<name>` requirement as the functional prerequisites. The
Red Hat-family fallback list carries the same entry.

The static managed-package release list also includes the bare `sos` package
name. Destroy releases only the selected storage cluster's requirement. The
ownership record preserves a package that predated Bootwright or is still
required by another owner; only a package Bootwright installed with no remaining
requirement is eligible for removal.

## Consequences

- Every Bootwright-managed Ceph node can produce a support report without a new
  package transaction during an incident.
- Adding the package is ordinary reconcilable dependency drift, not a rebuild or
  a new destructive authorization surface.
- Provided nodes keep a pre-existing `sos` installation after destroy.
- A future provider that changes the prerequisite set still has to project it
  through the provider table and retain matching apply/destroy ownership lists.
