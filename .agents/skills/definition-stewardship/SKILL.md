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

Run the repository fast check before finishing. It includes the maintained
stale-term check:

```text
make check-fast
```

Report any failure only when it is intentionally deferred to the next
code-focused round.
