# Architecture Decision Records

Specs carry the current contract. ADRs record decisions that still explain why
the contract has its present shape. Scan this table before designing or
changing behavior in an area — an accepted decision may already fix the shape
of the change or record why the obvious alternative was rejected. Incident and
constraint knowledge lives in [`.agents/knowledge/`](../../.agents/knowledge/KNOWLEDGE.md).

A superseded ADR is deleted rather than kept with a Superseded status, and its
number is retired — so the gaps at 0001 and 0003 are intentional, not missing
files.

| ADR | Title | Status |
| --- | --- | --- |
| [0002](0002-ansible-provider-dispatch.md) | Ansible Collection Layout And Provider Dispatch | Accepted |
| [0004](0004-cross-cluster-substrate-dependencies.md) | Cross-Cluster Substrate Dependencies | Accepted |
| [0005](0005-provisioning-playbooks.md) | Operator-Supplied Playbooks | Accepted |
| [0006](0006-no-prose-comments-knowledge-catalog.md) | Source Knowledge Lives in the Indexed Catalog, Not Comments | Accepted |
| [0007](0007-apply-destroy-safety-model.md) | Apply/Destroy Safety Model | Accepted |
| [0008](0008-ceph-declarative-cephadm-compat.md) | Declarative Ceph API on cephadm Native Concepts | Accepted |
| [0009](0009-renderer-owns-listening-surface.md) | The Renderer Owns the Listening Surface and Dispatch | Accepted |
| [0010](0010-cli-gate-and-flag-conventions.md) | CLI Gate and Flag Conventions | Accepted |
| [0011](0011-bmc-vmedia-boot-flow.md) | Redfish BMC Virtual-Media Boot Flow | Accepted |
| [0012](0012-proxy-fanout-per-directive.md) | Proxy Fan-Out, Per-Directive Bypass, and TLS-Inspection Trust | Accepted |
| [0013](0013-addon-catalog-and-hooks.md) | Add-on Catalog, Hook Lifecycle, and OLM Readiness Gating | Accepted |
| [0014](0014-api-grammar.md) | Public API Grammar: References, Unions, Collections, Enablement | Accepted |
| [0015](0015-machine-scope-rhsm-registration.md) | Machine-Scope RHSM Registration and External Management | Accepted |
| [0016](0016-secret-first-class-kind.md) | Secret as a First-Class Kind | Accepted |
| [0017](0017-machine-fqdn-node-identity.md) | Machine fqdn Address and Independent Node Identity | Accepted |
| [0018](0018-environment-domain-model.md) | Environment Domain Model | Accepted |
| [0019](0019-node-root-posture-and-orchestration-identity.md) | Node Root Posture and Ceph Orchestration Identity | Accepted |
| [0020](0020-cluster-captured-secrets-encrypted-at-rest.md) | Cluster-Captured Secrets Are Encrypted at Rest | Accepted |
| [0021](0021-external-playbook-content.md) | External Playbook Content | Accepted |
| [0022](0022-cluster-wait-bootstrap-boundary.md) | Cluster Install Wait Splits at the Bootstrap Boundary | Accepted |
| [0023](0023-teardown-is-the-inverse-of-buildup.md) | Teardown Is the Inverse of Build-Up | Accepted |
