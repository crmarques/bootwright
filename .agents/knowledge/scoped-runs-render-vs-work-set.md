# Scoped runs: render set vs work set

`clusteraccess.Selection` encodes the organizing rule: RENDER against the
render set, MUTATE only the work set. A managed StorageCluster pulled into a
`--clusters` scope only through a selected container cluster's data-foundation
attachment is a *render reference*: it renders (so the attachment renders) but
is provisioned and torn down by neither verb, and readiness checks never demand
its bootstrap secrets. Co-selecting it by name makes it a work object again.

**nil vs empty is load-bearing:** `applyTarget.StorageClusterNames` (and
destroy's `StorageWorkNames`) is tri-state — nil = no narrowing, non-nil empty
= act on NO storage clusters. A container-only selection must produce a NON-NIL
empty slice, or every render-reference cluster joins the provisioning set.

**Preflight secret scope:** `SecretScope{Machines, StorageClusters}` narrows
secret-material and storage-tool checks to acted-on objects; a render
reference must not require bootstrap secrets, a ceph entitlement, or cephadm
ssh/scp tooling. Each `secretRefRequirement` carries a `secretRefOwner` with at
most one field set; an empty owner is always in scope (environment-global
material, container install secrets, and data-foundation attachment SSH, which
the consuming cluster drives). A nil `*SecretScope` means no narrowing. Pinned
by TestPreflightSecretScopeDropsRenderReferenceStorage.

**Host trust only for connected machines:** `ApplyTaskConnectedMachines`
resolves every planned task's ansible `--limit` against that task's rendered
inventory and maps targeted hosts back to Machines; trust is required only for
those. Example: `apply --stage base --clusters <ocp>` pulls a managed
StorageCluster for rendering, but base only SSHes the cephadm seed — the
cluster's provided-OS arbiter (reached only by deps) must not block on missing
trust. Pseudo-hosts (localhost, agent-node hosts) resolve to no Machine. When
the scope empties the machine list, the managed known_hosts file check drops
too. Pinned by TestManagedHostTrustChecksScopeExcludesOutOfScopeMachine.

**diff --recorded mirrors apply exactly:** it threads the resolved scope name into
`clusteraccess.Resolve` (a container-only stage like add-ons rejects a
StorageCluster name with the same "unknown cluster" error apply raises — M11),
takes `StorageClusterNames` from `Selection.StorageWorkNames()` (the same
single source scoped apply uses), and classifies a StorageCluster's
sub-objects only when the selected graph plans that cluster's task — so a
render reference never reports spurious pool/export drift and a scoped
diff --recorded never exits 3 where the identically-scoped apply is a no-op.

**Orphan detection needs the FULL state:** undeclared-resource reporting must
compare ownership records against the full desired state captured before
`--clusters` scoping — a scoped state would report every other cluster's
still-declared resources as orphans.

**Whole-input validation:** the `--scoped-validation` flag was REMOVED. Every
scoped apply/destroy validates the WHOLE input, so a broken out-of-scope object
blocks a scoped run, naming the offender; do not reintroduce per-scope
validation. `LoadNormalizeInputFiles` (load+normalize without validation)
remains for pre-validation steps — e.g. `EnforceContextLocality` needs only
controller/bastion topology and must not fail on desired-state errors before
the explicit validation step. `LoadedInputFiles` returns the exact
post-selection file set that mutating runs snapshot.

**Scope filtering keeps service machines:** machines hosting services (DNS,
BMC, …) consumed by selected clusters stay in the filtered state even when no
provider `machineRef` pulls them in (API-native substrates have none), and
provider host machines backing selected guests stay so their SSH material
remains in scope.

**Artifact-server consumer edge:** a bare-metal managed-OS storage node
publishes its install ISO through the artifact server (Redfish virtual media),
so the artifact server must be registered as a machine service the
StorageCluster consumes (`storageArtifactServerConsumers`). Without it,
`apply --clusters <storage>` drops the artifact server's host machine, the ISO
stage path resolves empty, and the managed-OS role fails with an opaque
empty-path `No such file or directory`.

**Fleet selection gotcha:** when adding `Environment` selection lists, a
bare-metal storage cluster's bastion artifact-server Machine survives the
selection only via the cluster dependency graph — a selection that severs the
graph silently drops the machine the install depends on.

**--clusters availability:** `isClusterRootScopeTarget` names the scopes that
accept cluster-root narrowing (infra/clusters/all, sub-phases
fabric/machines/deps/base, and every synthetic `through-<phase>` scope);
single-kind targets validate against their own namespace; everything else gets
the shared "`--clusters` is not supported for <target>" error, deliberately
spelled in one function. The `all` scope accepts `--clusters` for apply and for
destroy's full-lifecycle task-chain path; render references remain outside the
work set in both directions.
