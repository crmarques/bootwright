# Internal Architecture Map

Reading this tree top-down follows Bootwright's pipeline: author desired
state, validate it, render tool inputs, converge, observe.

## Pipeline contexts

- `state/desired`: load YAML, strict-decode resources, normalize defaults, and
  validate ownership and references.
- `state/view`: pure point lookups over a loaded `v1alpha1.State` — the one
  place cross-kind joins and endpoint/network derivations live.
- `state/graph`: resolve shared host services, consumers, host placement,
  merge fields, and scoped-apply conflicts.
- `state/scaffold`: generate per-substrate example workspaces.
- `state/advice`: non-blocking best-practice advisories over loaded state
  (Ceph topology and provider warnings). Never fails a command.
- `render`: deterministically project desired state into installer inputs,
  Ansible inventory and vars, manifests, and locks. The root package owns all
  file writes and is the only importable surface; the emission families
  `render/installer`, `render/inventory`, and `render/ceph` are
  filesystem-free and one-way (`inventory -> installer`, `inventory -> ceph`).
- `converge`: the apply/destroy application service — scope and phase model,
  plan building, dry-run reports, run execution. Subpackages:
  `converge/workflow` (cross-cluster DAG tasks, leases, ledgers, resource
  locks, install records), `converge/ansible` (execute rendered playbooks),
  `converge/bundle` (embedded Ansible bundle), `converge/bastion` (controller
  tool planning). Subpackages never import the converge root.
- `preflight`: environmental-readiness rules (tools, secret material, SSH
  trust, entitlements). Returns plain check data; cli renders it.
- `status`: observe-stage analysis — report model, freshness, ledger
  summaries, structured normal hints, validated exact-ledger retry quoting,
  state-check classification. Returns data.
- `clusteraccess`: selection-by-name validation, kubeconfig generation, and
  access summaries for installed clusters.

## Shared components (single owners)

- `workspace`: context registry and every Bootwright-owned path under
  `/var/lib/bootwright` and `~/.bootwright` (contexts, cache, venv, bundles).
- `secrets` (package `secret`): encrypted context store, crypto, resolver,
  materialization, consumption classification, and directory-permission
  policy. All secret reads and writes go through here.
- `sshtrust`: known-hosts store plus trust planning, evaluation, and TOFU
  decisions (interactive confirmation stays in cli as a callback).
- `ownership`: durable host-resource ownership records for destroy scoping.
- `roles`: the Ansible dispatch registry — the only place
  `bootwright.core.*` role and playbook names are spelled in Go.
- `infra/{artifacts,locality,media,proxy}`: shared-infrastructure resolvers
  over state (artifact-server selection, bastion locality, install media,
  effective proxy).
- `storage/{topology,cephprovider,cephstate,cephdiff,cephadopt}`: the Ceph
  domain leaves — desired topology resolution (nodes, pools, OSDs), product,
  registry and release derivation, live-state decoding, desired-vs-live
  comparison, and the `diff --adopt` write-back.
- `addons/{plan,render,oc,records,steps,inputs,nativecatalog}`: cluster add-on
  expansion and ordering, generated-resource rendering, in-cluster apply
  through the `oc` command boundary, per-cluster add-on records, add-on-shipped
  steps and outputs, binding-scoped input values, and the embedded built-in
  catalog plus its machine-local store.
- `entitlements`, `nmstate`: small leaves (RHSM entitlements, NMState
  template rendering).
- `host/*`: generic host-execution primitives — `safefs`, `ptyexec`,
  `callerio`, `execution`, `localroot`, `managedroot`, `become`. They import
  nothing outside `host/` and carry no domain knowledge.

## Presentation

- `cli`: cobra commands, flags, prompts, and output rendering only; only
  `cmd/` and tests may import it. All human text goes through `cli/output`,
  and only cli may import `cli/output`.

## Enforcement

`repo/checks` and `repo/bundlecheck` hold no production code. They are
repository-fitness tests that run as part of `go test ./...` and enforce this
document structurally: a total per-package import matrix
(`import_matrix_test.go`), host-primitive genericity, the cli/output
boundary, the roles-registry literal rule, the CLI-import boundary, stale
import paths, Ansible registry/role consistency, and embedded-collection lock
integrity.
