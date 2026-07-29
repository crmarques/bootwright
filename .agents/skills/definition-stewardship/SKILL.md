# Definition Stewardship Skill

Use this skill when changing specs, ADRs, docs, examples, E2E fixture names,
or project-local agent guidance.

## Load First

- `/specs/README.md` and `/specs/index.md`, then the spec(s) the index maps to
  the definition you are changing. Load `state-model.md` only when the change
  touches a kind, a validation rule, or the CLI contract — bulk-loading every
  spec is forbidden for anything narrower than a repo-wide task
  (`specs/README.md`, "Agents load only the specs needed"; `/AGENTS.md` Load
  Order).

## Responsibilities

- Keep `/specs/` as the source of truth. Docs explain workflows and link to
  specs instead of copying long normative rule sets.
- Route content to its designated home (ADR 0006): implementation, incident,
  vendor-quirk and constraint knowledge → `.agents/knowledge/` with an index
  row in `KNOWLEDGE.md`; decisions with durable rationale → `specs/adr/`;
  authored-contract schema and field semantics → `specs/state-model.md` and
  `docs/concepts/`.
- Keep ADRs current. Remove superseded ADR files once the surviving decision
  and specs carry the needed context.
- A new or moved page under `docs/` gets its `mkdocs.yml` nav entry in the same
  change — a guard test fails `check-fast` otherwise (see
  `docs/contributing/building-and-testing.md`, "The guard-test regime").
- Use meaningful names for examples, E2E cases, providers, hosts, clusters,
  secrets, and machine profiles.
- Use "managed cluster" in public docs and examples. Do not import ACM
  vocabulary — "spoke", "hub cluster", "ClusterDeployment" — for a
  Bootwright-managed cluster, except when quoting an external product concept.
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
