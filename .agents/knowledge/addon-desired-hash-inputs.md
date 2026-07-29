# Add-on desired hash: what it folds and what must stay in sync

**Constraint:** `render.DesiredHash` (internal/addons/render) folds three
things beyond the add-on spec itself, and each carries an invariant that
silently breaks re-apply behavior when violated.

**Canonical resource order:** `OLMResources` returns the ordered list —
shipped CatalogSource (if any), then the operator-install set (Namespace,
OperatorGroup, Subscription), then declared custom resources — and this exact
ordering is what `DesiredHash` folds into the add-on hash. Callers that need
the groups separately (the apply path gates on catalog READY before the
Subscription and on the CSV before custom resources) use `CatalogResources` +
`OperatorResources` + `CustomResources`, whose concatenation must remain
byte-identical to `OLMResources`, or recorded hashes drift from what applies.

**Binding inputs:** `ExtensionPlan.Inputs` (the binding-supplied values) are
part of desired state because hooks and effects resolve against them — editing
an input re-applies an otherwise-ready add-on. The `Inputs` field is json
`omitempty` in the hash payload specifically to keep previously recorded
hashes stable for add-ons whose bindings supply no inputs.

**Duplicate addonRef merge parity:** `plan.BindingPlans` appends duplicate
`addonRef` entries' inputs into one per-addon list expressly to mirror
`inputs.EffectiveBindingAddons` (which merges duplicate entries the same way):
the desired hash must see the exact input list the executor resolves, or a
binding with two entries for the same add-on would hash differently from what
runs. Pinned by the plan_test case that appends a second `virt` entry and
expects merged `[external-storage, tuning]` inputs.

**Traversal parity:** `inputs.EffectiveBindingAddons` intentionally mirrors
`plan.expandSet` — the same ProfileRefs-then-AddonRefs walk of the
ClusterAddonProfile DAG, breaking on a profile already on the current path.
Profile cycles are rejected upstream by state/desired (the cycle authority),
so the in-walk break is unreachable on valid state; the duplicated shape
exists to keep the two traversals from drifting apart.

**Hook content digest:** `hooks.ContentDigest` is a best-effort sha256 over a
hook's shipped content — the playbook file, the vendored `roles/` and
`collections/` trees, and every manifest template; missing files contribute
nothing (validation separately requires them for a real apply).
`render.DesiredHash` folds a per-add-on `hookContentDigest` (hook name +
`ContentDigest` per `spec.steps` entry) into the desired hash even though the
Extension field already serializes the hook specs, so editing a shipped
playbook without touching the add-on YAML still moves the hash and re-runs the
add-on. The same digest also feeds the per-hook record for `run: onChange`
skipping.
