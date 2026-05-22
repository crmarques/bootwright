# Definition Stewardship Skill

Use this skill when changing specs, ADRs, docs, examples, E2E fixture names,
or project-local agent guidance.

## Load First

- `/specs/README.md`
- `/specs/index.md`
- `/specs/domain.md`
- `/specs/architecture.md`
- `/specs/state-model.md`
- `/specs/security.md`

## Responsibilities

- Keep `/specs/` as the source of truth. Docs explain workflows and link to
  specs instead of copying long normative rule sets.
- Keep ADRs current. Remove superseded ADR files once the surviving decision
  and specs carry the needed context.
- Use meaningful names for examples, E2E cases, providers, hosts, clusters,
  secrets, and machine profiles.
- Use "managed cluster" in public docs and examples. Avoid legacy
  managed-cluster synonyms except when quoting an external product concept.
- Keep `v1alpha1` clean: no migrations, aliases, compatibility shims, or
  legacy schema examples.
- Keep examples safe to commit: no plaintext credentials, kubeconfigs, pull
  secrets, private keys, tokens, personal usernames, or private absolute paths.

## Required Checks

Run stale-term searches over definition files before finishing:

```text
rg -n 'connectivity[.](mode|connected|restricted|disconnected)|spec[.]connectivity|bootwright_''connectivity|local''Registry|bootwright pre''flight|bootwright pl''an|bootwright di''ff|sp''oke|examples/inf''ra|ADR 000''3' README.md docs specs examples test .agents
```

Confirm connected provider-swap examples keep `Environment` and `ContainerCluster`
files byte-identical:

```text
diff -u examples/libvirt-redfish-fleet/environment.yaml examples/baremetal-redfish-fleet/environment.yaml
diff -u examples/libvirt-redfish-fleet/container-cluster.yaml examples/baremetal-redfish-fleet/container-cluster.yaml
```

Report any remaining stale term only when it is intentionally deferred to the
next code-focused round.
