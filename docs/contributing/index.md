---
title: Contributing
description: Orientation for contributors changing Bootwright's code — where the normative contracts live, and the API and architecture deep-dives.
---

# Contributing

This section is for contributors changing Bootwright's **code** — adding a
substrate adapter, a managed service, a CLI verb, or working on the convergence
pipeline. It is not the operator guide. If you are authoring desired state to
stand up clusters, start with the
[desired-state model](../concepts/index.md) and
[Getting Started](../getting-started/index.md) instead.

!!! note "The docs teach, the specs bind"
    These pages explain how Bootwright is built and how to extend it, but they
    are not the contract. The normative source of truth lives under
    [`/specs`](https://github.com/crmarques/bootwright/tree/main/specs): the API
    schema, the CLI contract, validation rules, and the security boundaries. When
    a page here and a spec disagree, the spec wins — fix the page.

## In this section

- **[API](api.md)** — the desired-state API and every extension walkthrough:
  where the kinds and fields live, the strict-decode and no-backward-compat rule,
  where validation and normalization sit, and the step-by-step checklists for
  adding a substrate adapter, a managed service, or a CLI verb without breaking
  the contracts.
- **[Architecture](architecture.md)** — the execution internals: the render
  pipeline, execution identities, resource locks, cluster install scheduling, the
  four-outcome convergence classifier, the three apply modes, the rendering
  contract, the ownership-record cross-boundary contract, and the Ansible bundle.
- **[Building and testing](building-and-testing.md)** — how to compile, run the
  tests, and pass the check gates.
- **Sending a change** — get the gates green locally first
  ([Building and testing](building-and-testing.md)); CI re-runs them from
  `.github/workflows/checks.yml`. Commit as one Conventional Commits subject
  line (`type(scope): subject`, 72 characters max, no body) with no
  co-author or attribution trailer — the full convention is in
  [`AGENTS.md`](https://github.com/crmarques/bootwright/blob/main/AGENTS.md).
