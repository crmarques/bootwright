# Architecture Decision Records

Specs carry the current contract. ADRs record decisions that still explain why
the contract has its present shape. Scan this table before designing or
changing behavior in an area — an accepted decision may already fix the shape
of the change or record why the obvious alternative was rejected. Incident and
constraint knowledge lives in [`.agents/knowledge/`](../../.agents/knowledge/KNOWLEDGE.md).

`Accepted` is the only status: a decision that has not shipped belongs in the
backlog, not here. A wholly superseded ADR is deleted rather than kept with a
Superseded status, and its number is retired — so the gaps at 0001 and 0003
are intentional, not missing files. A decision revised only *in part* keeps its
file: its body is rewritten so it reads as the current decision, its own
`## Status` block names each clause that moved, and the revising ADR is listed
below.

| ADR | Title | Revised by / revises |
| --- | --- | --- |
| [0002](0002-ansible-provider-dispatch.md) | Ansible Collection Layout And Provider Dispatch | |
| [0004](0004-cross-cluster-substrate-dependencies.md) | Cross-Cluster Substrate Dependencies | |
| [0005](0005-provisioning-playbooks.md) | Operator-Supplied Provisioning Playbooks | delivery revised by 0021; kind renamed by 0026 |
| [0006](0006-no-prose-comments-knowledge-catalog.md) | Source Knowledge Lives in the Indexed Catalog, Not Comments | |
| [0007](0007-apply-destroy-safety-model.md) | Apply/Destroy Safety Model | teardown ordering revised by 0023 |
| [0008](0008-ceph-declarative-cephadm-compat.md) | Declarative Ceph API on cephadm Native Concepts | |
| [0009](0009-renderer-owns-listening-surface.md) | The Renderer Owns the Listening Surface and Dispatch | |
| [0010](0010-cli-gate-and-flag-conventions.md) | CLI Gate and Flag Conventions | |
| [0011](0011-bmc-vmedia-boot-flow.md) | Redfish BMC Virtual-Media Boot Flow | |
| [0012](0012-proxy-fanout-per-directive.md) | Proxy Fan-Out, Per-Directive Bypass, and TLS-Inspection Trust | |
| [0013](0013-addon-catalog-and-hooks.md) | Add-on Catalog, Step Lifecycle, and OLM Readiness Gating | |
| [0014](0014-api-grammar.md) | Public API Grammar — References, Unions, Collections, Enablement | |
| [0015](0015-machine-scope-rhsm-registration.md) | Machine-Scope RHSM Registration and External Management | |
| [0016](0016-secret-first-class-kind.md) | Secret as a First-Class Kind | |
| [0017](0017-machine-fqdn-node-identity.md) | Machine fqdn Address and Independent Node Identity | revised by 0018, 0025 |
| [0018](0018-environment-domain-model.md) | Environment Domain Model | refines 0017 |
| [0019](0019-node-root-posture-and-orchestration-identity.md) | Node Root Posture and Ceph Orchestration Identity | revised by 0024, 0027 |
| [0020](0020-cluster-captured-secrets-encrypted-at-rest.md) | Cluster-Captured Secrets Are Encrypted at Rest | |
| [0021](0021-external-playbook-content.md) | External Playbook Content | revises 0005 |
| [0022](0022-cluster-wait-bootstrap-boundary.md) | Cluster Install Wait Splits at the Bootstrap Boundary | |
| [0023](0023-teardown-is-the-inverse-of-buildup.md) | Teardown Is the Inverse of Build-Up | revises 0007 |
| [0024](0024-machine-access-union-and-cluster-owned-node-login.md) | Machine Access Is a Union, and a Ceph Cluster Owns Its Node Login | revises 0019; revised by 0027 |
| [0025](0025-composed-names-are-labels-plus-explicit-overrides.md) | Composed Names Are Labels Plus Explicit Overrides | refines 0017 |
| [0026](0026-custom-playbook-kind-name.md) | The Operator-Supplied Playbook Kind Is `CustomPlaybook` | renames the kind of 0005, 0021 |
| [0027](0027-bootwright-owns-the-login-it-installs.md) | Bootwright Owns the Login on the Machines It Installs | revises 0019, 0024 |
| [0028](0028-terminal-for-the-borrowed-identity.md) | A Terminal for the Borrowed Identity, Never for the Created One | extended by 0029 |
| [0029](0029-answering-a-sudo-password-for-the-borrowed-identity.md) | Answering a `sudo` Password for the Borrowed Identity | extends 0028 |
