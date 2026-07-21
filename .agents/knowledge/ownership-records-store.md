# Ownership record store invariants (Go + Ansible)

The per-context ownership store under `ownership/` is the safety net every
destroy, orphan report, and package-removal decision relies on. Both sides of
the Go/Ansible boundary write and read it; these invariants keep them honest.

**Hand-synced literals:** The owner stamp `bootwright` (`ownership.Owner`) and the
role literals `owner`/`reference` (`RoleOwner`/`RoleReference`, mirroring
`api/v1alpha1.ComponentRole*`) are written independently by the Ansible
`ownership_record` role (`tasks/resource.yml`: `owner: bootwright`,
`bootwright_ownership_role`) and read back by Go (orphan reporting, destroy
gating). Unlike the kind taxonomy, NO fitness test guards these literals or the
reference filename convention — keep Go, `api/v1alpha1`, and the role in sync by hand.

**Role semantics:** `RoleOwner` provisions the base and may destroy it;
`RoleReference` only contributes/consumes and is released — never torn down — by
another context's destroy. An empty/absent role reads as owner via
`EffectiveRole()` (pre-field and single-context records); `ValidateResource`
rejects any other value. The Ansible role stamps `role` only on reference
records so pre-existing owner records stay byte-identical.

**Reference filenames:** A reference record shares Kind+Name+Host with the owner
record, so `ResourcePath` names it `<name>@<context>.json` (Context required for
references) while the owner keeps `<name>.json` — single-owner is a filesystem
invariant and a reference save can never clobber the owner's file. Matching is
always by Kind+Name+Host, never by filename. `resource.yml`/`remove_resource.yml`
implement the identical naming. Guarded by TestReferenceRecordDoesNotOverwriteOwnerFile
and TestReferenceRecordPathRequiresContext.

**Load policy skips, never fails:** `LoadResources` skips a record it cannot
read, decode, or validate (truncated atomic-writer leftovers; role-written
records that never ran Go validation) — one corrupt file must never hide every
other record and block the sweep that would reclaim it. Only a directory-traversal
failure errors. `LoadResourcesWithWarnings`/`LoadContextWithWarnings` surface
per-record skip reasons so destroy preview and diff name the dropped
files. Guarded by TestLoadResourcesSkipsBadRecordWithoutDroppingGood.

**LoadContext is the single context-scoped entry point:** destroy planning, the
destroy run inventory, and diff orphan reporting all read through it, so
the records that gate a destroy, the records it executes against, and the
preview are always the same set. It drops records stamped with a different
context, keeps unstamped (pre-context-field) records, and applies no filter when
`contextName` is empty.

**Path safety:** `ResourceRecord.Paths` drive destructive file removal, so
`ValidateResource` rejects any owned path containing `..`, and Kind/Name are
restricted to `[A-Za-z0-9_.-]` (they become path segments of the record itself).
The Ansible mirror (`destroy_resource.yml`) allowlists deletion roots (system
prefixes plus the context state roots, so non-default roots like
`/srv/bootwright` still clean up), rejects `..`, and guards each root clause on
a non-empty value so an undefined var can never match every path.

**Sensitive-data scan is deliberately asymmetric:** key-name markers are broad
(password, token, private_key, bearer, authorization, client-secret,
kubeconfig, secret); value markers are narrow (`-----begin ` PEM,
private_key/privatekey, client-secret, `authorization:`, `bearer `) because
recorded values legitimately contain "token"/"kubeconfig" inside ssh
ProxyCommand args, UserKnownHostsFile paths, and FQDNs — a broad value match
would reject the record and strand the resource it exists to reclaim.

**Kind taxonomy single source:** `internal/ownership/kinds.go` registers every
kind; Ansible emits the literals via `bootwright_ownership_kind`; the fitness
test `internal/repo/checks/ownership_kinds_test.go` asserts every role-emitted
concrete literal is registered (Jinja-templated values exempt). A kind added on
one side only silently falls out of every destroy inventory group. `InventoryGroup`
names the destroy host-set; `GroupNone` marks kinds reclaimed by a dedicated
play (controller-name-resolver → container-cluster agent-destroy play).
`InventoryGroupForKind` returns `(GroupNone,false)` for unregistered kinds so
callers surface drift; the render-side fallback still routes an
unrecognized-but-Bootwright-owned record to the infra teardown group.

**Back-compat readers:** old package records may carry `requiredBy` as a bare
scalar (`coerce_required_by.yml` wraps it into a list); records written before
the vMedia attribute rename carry lowercase `vmediaUnit`/`vmediaPort`, and
`destroy_resource.yml` reads both spellings (`attrs.vMediaUnit | default(attrs.vmediaUnit)`)
— dropping the fallback silently leaves the recorded vmedia unit running and
its firewall port open on destroy.

**Package removal gate:** `package_remove_one.yml` removes a package only when
its record proves Bootwright introduced it — `preexisting` must be false and it
DEFAULTS TO TRUE (`preexisting | default(true)`) — and nothing else requires it
(`bootwright_ownership_package_remaining_required_by` empty). Inverting the
default or dropping the requiredBy gate silently removes operator-preexisting
packages such as chrony or podman.

**Byte-stable writes:** `resource.yml` reuses the prior `updatedAt` when nothing
substantive changed so unchanged records stay byte-identical and the copy task
reports no change; a new timestamp is stamped only on a real difference.

**Shared-component identity:** a bastion-shared infra-component's
context-independent identity is the (Kind, Name, Host) triple — Name encodes
`<provider>-<component>`. A same-Kind+Name record on a different Host is a
different bastion's service (TestOtherContextsWithRoleDiffersByHost). The
cross-context scan (`OtherContextsWithRole`) loads sibling stores with
`LoadResources`, NOT `LoadContext` — sibling records are stamped with the
sibling's context and `LoadContext` would drop them all, making every sibling
look like a non-referrer. An unreadable sibling store is a warning and the scan
continues: over-counting referrers fails safe toward keeping the shared service.
