# Creating, proving and deleting cluster objects with oc

What `oc` and the Kubernetes API do when Bootwright creates an object, decides
whether it owns it, waits on it and deletes it. The
[cluster object adapter](../../specs/add-ons.md#the-cluster-object-adapter)
owns the required behavior; the program and its ownership evidence are
[B750](../../specs/milestones/m4.md#b750), the adapter is
[B751](../../specs/milestones/m4.md#b751), and the first live proof is
[B755](../../specs/milestones/m4.md#b755). The OLM objects are in
[olm-v0-exact-install.md](olm-v0-exact-install.md).

Provenance: `R2:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (Kubernetes API mechanics) and `S<n>` its summary items; `RV` marks
a claim the adversarial review re-checked, with its verdict (confirmed,
corrected or uncertain). Release labels name where a fact was read; they are
not supported or required versions (D163). The ids name research notes this
repository does not track; each statement here carries its fact, and an id
only records where it was read. "Observed by the v1 implementation (main-v1);
re-verify on this tree" marks a v1 fact the research did not confirm.

## Absence, create and apply

- `kubectl get --ignore-not-found` ignores only API NotFound errors, so an
  absent object prints nothing and exits 0 while other errors still fail. The
  source says so; no research claim ran it, and
  [B755](../../specs/milestones/m4.md#b755) re-verifies it on the gate's `oc`.
- Apply of a custom resource before its CRD is served fails client-side, before
  any request: `resource mapping not found for name: "<n>" namespace: "<ns>"
  from "<file>": ... ensure CRDs are installed first`. A server-side dry run
  fails the same way, because the mapping is client-side (R2:C12, confirmed).
- A server-side apply over an existing object does not fail; it updates the
  object. A `POST` create over an existing object returns 409 `AlreadyExists`,
  which is the reliable "it was already there" signal (R2:C42, uncertain: a
  derivation, and whether an apply that creates answers 201 and an update 200
  is open).
- Apply needs a field manager (`fieldManager` is required; other writes take
  the manager from the `User-Agent`). `kubectl` uses the name `kubectl`; a
  client picks any name up to 128 printable characters (R2:C2, confirmed except
  the length limit).
- `oc create --field-manager` recording the manager on the created fields is
  confirmed by no research claim. B755 re-verifies it on the gate's `oc`.
- A conflict is a 409 when an apply would change a field another manager owns.
  Resolutions: force (`--force-conflicts`, which makes the applier the sole
  owner), omit the field, or set the live value to share ownership. Equal
  values share ownership, and a later change by either side conflicts
  (R2:C3 and C4, confirmed).
- An update (`PUT`, and non-apply patches such as `kubectl edit`) never
  conflicts and records `operation: Update`. An operator that writes a field
  with an update becomes its owner, and the next apply of another value from us
  returns 409 (R2:C5, confirmed; the consequence is derived).
- `kubectl apply --server-side --prune --applyset=...` is accepted. ApplySet is
  alpha (behind `KUBECTL_APPLYSET=1`) and label-based, with no uid pin; only the
  legacy label prune refuses server-side-applied objects. It is "not suitable"
  for a delete-only-what-we-created guarantee, not "absent" (RV-R2 correction
  2; C9 corrected).

## Proving "ours"

- Only a recorded `uid` proves an object is the one Bootwright created. Use the
  `uid` and `resourceVersion` of a real, non-dry-run create response. A dry-run
  response carries a uid that "will likely be different" and must not be stored
  (R2:C39, confirmed; S12).
- `metadata.managedFields` entries carry a free-string `manager`, an
  `operation` and times, with no authenticated user. Anyone who may update or
  patch the object can overwrite or reset them, so an entry naming our manager
  proves only that some write used the name (R2:C40 and C6, confirmed). An apply
  body cannot carry `managedFields`; a non-apply write of a single empty entry
  (`[{}]`) resets them, and an empty list does not.
- Labels and annotations are forgeable too. A key has a name segment of at most
  63 characters and an optional DNS-subdomain prefix of at most 253 characters;
  the prefixes `kubernetes.io/` and `k8s.io/` are reserved; the annotations of
  one object total at most 256 KiB (R2:C41, confirmed except the key-length
  rules).
- Stronger "same name, not ours" evidence is a conjunction: the `uid` equals the
  recorded one, `creationTimestamp` is not earlier than the recorded creation,
  our marker is present, and `managedFields` shows our manager with
  `operation: Apply` (R2:C42, uncertain: a derivation).

## Deleting

- `DeleteOptions.preconditions` carries `uid` and `resourceVersion`. When one
  is not met the request returns 409 `Precondition failed: UID in
  precondition: <want>, UID in object meta: <got>` (R2:C37, confirmed).
- `kubectl delete` and `oc delete` have no flag for a precondition. A raw
  `DELETE` with a `DeleteOptions` body, `kubernetes.core.k8s` with
  `delete_options.preconditions.uid` (ignored in check mode) and Go clients do
  (R2:C38 and S5, confirmed).
- The wait inside `kubectl delete` is uid-aware. A standalone
  `kubectl wait --for=delete` and the `kubernetes.core` `state: absent` wait
  match by name only (R2:C29 and C20, confirmed).
- A terminating namespace publishes why in `status.conditions` (five fixed types
  with fixed reasons; the content-failure reason is `ContentDeletionFailed`),
  and an unavailable aggregated APIService keeps it terminating. Reporting
  needs no finalizer removal (R2:C33 to C35, confirmed).
- Deleting an operator leaves its CRDs and custom resources (R2:C36,
  confirmed).

## CRDs and discovery

- `Established` stays true until the CRD is deleted. The other condition types
  are `NamesAccepted`, `NonStructuralSchema`, `Terminating` and
  `KubernetesAPIApprovalPolicyConformant` (R2:C13, confirmed).
- `kubectl` keeps a six-hour on-disk discovery cache. "Fresh" means every cache
  file used was written by the same process, not "within the TTL". A new process
  reading older files is never fresh, so a mapping miss resets and discovers
  live. A just-`Established` CRD is therefore found by a new `oc`, and a private
  `--cache-dir` is isolation, not a correctness need. The residual risk is lag
  between the three API servers right after `Established`, which a bounded retry
  on the "ensure CRDs are installed first" error covers (RV-R2 correction 1;
  C14 and S4 corrected).

## Output and limits

- Errors print `error: <text>` to stderr and exit 1. API-server warnings arrive
  in a `Warning` response header and print to stderr with a `Warning:` prefix
  on success. Object output goes to stdout (R2:C55, uncertain: not re-fetched).
  v1 read stdout only for this reason (observed by the v1 implementation
  (main-v1); re-verify on this tree; L2-A08).
- The API server rejects a request body over 3 MiB. etcd's default request limit
  of 1.5 MiB bounds a stored object; OpenShift's etcd flag was not read
  (R2:C64, confirmed).
- Client-side `kubectl apply` stores the whole object in the
  `last-applied-configuration` annotation, so a large CRD fails with
  "metadata.annotations: Too long" at the 256 KiB annotation cap. Server-side
  apply does not use the annotation (R2:C65, confirmed).
- `system:masters` is exempt from API priority and fairness upstream (OpenShift
  flow schemas not checked). `client-go` allows 5 requests per second with a
  burst of 10 (R2:C53 and C60, confirmed); `kubectl` raises only the discovery
  rate (R2:C68, confirmed for `kubectl`, not checked for `oc`).

## Waits and stale conditions

- `oc wait --for=condition=<Type>` ignores a condition whose own
  `observedGeneration`, else `status.observedGeneration`, is behind
  `metadata.generation`. When the object reports no `observedGeneration` the
  condition is trusted as fresh, which is the case for an OLM CSV and many
  custom resources. `--for=jsonpath` never checks it (R2:C23, C24 and C28,
  confirmed).
- A kind without `observedGeneration` has one stale-proof reading: a status
  field that embeds a spec-derived value, such as `installedCSV` equal to the
  CSV subscribed to (R2:C28).
- The wait exits 1 at once on NotFound unless `--for=create`; an empty selector
  prints "no matching resources found"; the default `--timeout` is 30s, zero
  checks once and negative waits a week; timeout, NotFound and a watch failure
  all exit 1. Repeated `--for` exists only from kubectl 1.36, so not in `oc`
  4.20 to 4.22, which carry v0.33 to v0.35 (R2:C25 to C27, confirmed).
- A `kubernetes.core` wait on a Job is satisfied by `Failed=True`, so a failed
  Job reads as done (RV-R2 correction 5; C19, confirmed). `wait_condition` has
  no generation check.
- A status condition can freeze while its operator keeps working. In the
  Data Foundation external-mode attach, the StorageCluster `Available=False`
  stayed unevaluated for nine hours while `ReconcileComplete` and `Progressing`
  kept heartbeating (observed by the v1 implementation (main-v1); re-verify on
  this tree; D-17, in
  [data-foundation-external-mode.md](data-foundation-external-mode.md)). v1
  only reported the lag, taking the newest heartbeat within one object as the
  clock and flagging conditions more than five minutes behind it (L1-09); no
  remedy was built. Two package readiness facts bear on a state that looks
  stuck: the ACM hub phase set has no `Waiting` on the 2.x line, and Argo CD
  `Available` also needs its Route admitted
  ([acm-hub-install.md](acm-hub-install.md),
  [openshift-gitops-and-argocd-handoff.md](openshift-gitops-and-argocd-handoff.md);
  RV-R4 corrections 1 and 2).
