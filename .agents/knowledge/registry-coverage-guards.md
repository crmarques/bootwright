# Coverage-enforced registries: what to update when adding things

**New authored kind = four coordinated edits:** the `State` field, the
`Kind*` constant, one `KindAccessor` entry in `AuthoredKindAccessors()`
(`api/v1alpha1/kinds.go`), and the `loadFile` decode case in
`internal/state/desired`. Guards fail on any subset: `kinds_test.go` proves
the accessor table covers every State field exactly once with unique kinds
(and that each accessor reads its OWN State field, catching copy-paste
accessors reading a sibling list), and the loader probe test proves every
kind round-trips through Load.

**InfraComponent union arms:** `InfraComponentSpec.SetSlots()`
(`api/v1alpha1/infracomponent.go`) is the single enumeration of the arm
union; the exactly-one-of validator counts its length. Adding an arm field
without adding it to `SetSlots` makes the validator silently under-count.

**Installer-owned fields:** `OwnedInstallerFields`/`OwnedFields()`
(`api/v1alpha1/owned_installer_fields.go`) registers every field Bootwright
writes into generated `install-config.yaml` and `agent-config.yaml`. It is a
test-time drift guard (`render/owned_test.go` compares it against rendered
output), NOT a runtime input. Add a field when the renderer learns to write
it; remove when it stops owning one; treat returned slices as read-only.

**Provisioning stages:** the five `ProvisioningStage*` values (fabric,
machines, deps, base, add-ons) must match the CLI `--stage` sub-phase names.
`ProvisioningStages()` (`api/v1alpha1/types.go`) is the ordered single source
of truth; `internal/converge` pins `SubPhaseStageNames()` to it via a guard
test. converge imports this leaf api package — never the reverse.

**Machine-service var builders:** `machineServiceVarBuilders`
(`internal/render/inventory/vars_machine_services.go`) must have an entry for
every service-graph identity kind the support registry can produce, else its
rendered vars are silently dropped — `TestMachineServiceVarBuildersCoverRegistry`
enforces this against `roles.ServiceEntries()`. `servicePinGates` keys must
match `roles.PinnableServiceKeys()` (`TestServicePinGatesCoverPinnableServices`)
so a new image-bearing registry entry cannot ship without a component pin.

**Native add-on catalog:** `add-ons/` doubles as authored content AND Go
package (`package catalog`) so committed catalog files embed with no sync
step; `internal/addons/nativecatalog` is the only consumer. Adding a catalog
entry means adding its directory name to the `//go:embed` pattern list in
`add-ons/embed.go` (currently: `catalog.yaml openshift-data-foundation
fusion-data-foundation`). `nativecatalog.Entries` cross-checks the embedded
`catalog.yaml` index against the embedded content tree (every declared
entry/version must have `<name>/<version>/add-on.yaml`, versions must be safe
path segments, `defaultVersion` must be declared) so a forgotten embed
pattern or index row fails loudly; `catalog_test` checks the reverse — every
embedded top-level directory must be indexed.

**Scaffold substrates:** `internal/state/scaffold` materialises example YAML
from one template set plus a per-substrate `Substrates` map entry, so a new
substrate is one map entry plus the schema/validator/render dispatch the spec
already requires. `TestWorkspacePassesValidator` is the load-bearing guard:
every substrate's scaffolded output must round-trip LoadNormalizeValidate;
file names are pinned so `init.go`'s directory layout doesn't drift.
`Substrate` fields are exported only for `text/template`; fragments may embed
`{{.ProviderID}}`/`{{.NetworkID}}`, resolved by a pre-pass.

**Credentials filter contracts:** in the `bootwright.core` filter plugins,
the `label` argument must appear in every `AnsibleFilterError` message —
consuming tasks are `no_log`, so the label is the operator's only signal for
which of several `credentialsRefs` is malformed. The `FilterModule`
registration names (`bootwright_parse_credential`,
`bootwright_proxy_userinfo`) are pinned by tests because name drift breaks
every consuming playbook at parse time. The tests are stdlib `unittest`, stub
`sys.modules['ansible.errors']`, and load the module by file path so they run
without an Ansible runtime; each test covers one validation branch because a
regression would otherwise surface only mid-playbook inside a no_log task.
