# `KubeVirtHostKubeconfigPaths` nil vs empty is dry-run vs live

**Symptom:** A live run hands Ansible the durable encrypted host-cluster
kubeconfig path (`{{ bootwright_clusters_dir }}/<host>/secrets/kubeconfig`)
instead of the short-lived materialized copy, and the KubeVirt provider tasks
fail to read it — or a `--dry-run`/`plan` stops emitting that logical path and
renders no `kubevirt.kubeconfig` at all. Both regressions come from the same
one-word change.

**Root cause:** `render.PathOptions.KubeVirtHostKubeconfigPaths` is a
three-state signal, not a lookup table with a harmless zero value
(`internal/render/inventory/vars_components_machine.go`):

- **nil** — dry run. No secret is materialized, so the renderer emits the
  *logical durable* path as documentation of where the kubeconfig lives.
- **non-nil, even when empty** — live run. The durable encrypted path must
  never be handed to Ansible; a missing key means "no kubeconfig for this host
  cluster", and the field is simply left unset.

A live short-circuit that returns early for the no-KubeVirt case (no host
clusters to materialize) therefore MUST pass an explicit `map[string]string{}`.
Passing `nil` because "there is nothing to put in it" reclassifies the live run
as a dry run and leaks the durable path into the rendered vars.

**Fix / rule:** in `internal/converge/workflow/run.go`, only the `opts.DryRun`
branch passes `nil`; every live path — including the
`len(hostClusters) == 0` short-circuit — passes an explicit empty map. Both arms
(nil vs `map[string]string{}`) are covered by
`TestKubeVirtChildExampleRendersVarsGeneratedMACAndNonSecretState`; keep both
assertions when touching this seam.
