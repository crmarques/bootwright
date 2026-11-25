# Custom playbook detail

The parked item [B102](../milestones/backlog.md#b102) revives this detail when
it replaces the reserved `CustomPlaybook` shape with one typed, invertible
executable journey. Until then the
[reserved-shape contract](../api/custom-playbooks.md) owns what admission and
lifecycle enforce, including the lexical checks, and the closed schema in
`api/v1alpha1/customplaybooks.go` is authoritative for field names, types and
defaults; this page keeps only the ordering the replacement defines.

## Ordering

The replacement that [B102](../milestones/backlog.md#b102) defines orders a
playbook by its dependencies on blocks or API objects, never on a stage anchor.
A `gates` declaration would complete before its anchor; `follows` would
complete after it. Enabled playbooks at the same anchor would order by `order`
and their `provides`/`requires` capability graph. Duplicate providers, missing
requirements, and cycles are invalid for enabled declarations. Disabled
declarations enter no ordering graph.
