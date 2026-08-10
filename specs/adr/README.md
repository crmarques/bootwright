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
| [0007](0007-apply-destroy-safety-model.md) | Apply/Destroy Safety Model | teardown ordering revised by 0023; gate surface revised by 0030; refined by 0031, 0054 |
| [0008](0008-ceph-declarative-cephadm-compat.md) | Declarative Ceph API on cephadm Native Concepts | |
| [0009](0009-renderer-owns-listening-surface.md) | The Renderer Owns the Listening Surface and Dispatch | |
| [0010](0010-cli-gate-and-flag-conventions.md) | CLI Gate and Flag Conventions | destructive-gate flags revised by 0030 |
| [0011](0011-bmc-vmedia-boot-flow.md) | Redfish BMC Virtual-Media Boot Flow | revised by 0050: the occupancy probe became the power-off gate |
| [0012](0012-proxy-fanout-per-directive.md) | Proxy Fan-Out, Per-Directive Bypass, and TLS-Inspection Trust | |
| [0013](0013-addon-catalog-and-hooks.md) | Add-on Catalog, Step Lifecycle, and OLM Readiness Gating | |
| [0014](0014-api-grammar.md) | Public API Grammar — References, Unions, Collections, Enablement | |
| [0015](0015-machine-scope-rhsm-registration.md) | Machine-Scope RHSM Registration and External Management | |
| [0016](0016-secret-first-class-kind.md) | Secret as a First-Class Kind | |
| [0017](0017-machine-fqdn-node-identity.md) | Machine fqdn Address and Independent Node Identity | revised by 0018, 0025; enforced by 0035; name-resolution group extended by 0046 |
| [0018](0018-environment-domain-model.md) | Environment Domain Model | refines 0017 |
| [0019](0019-node-root-posture-and-orchestration-identity.md) | Node Root Posture and Ceph Orchestration Identity | revised by 0024, 0027 |
| [0020](0020-cluster-captured-secrets-encrypted-at-rest.md) | Cluster-Captured Secrets Are Encrypted at Rest | |
| [0021](0021-external-playbook-content.md) | External Playbook Content | revises 0005 |
| [0022](0022-cluster-wait-bootstrap-boundary.md) | Cluster Install Wait Splits at the Bootstrap Boundary | |
| [0023](0023-teardown-is-the-inverse-of-buildup.md) | Teardown Is the Inverse of Build-Up | revises 0007; managed-storage proof edges revised by 0058 |
| [0024](0024-machine-access-union-and-cluster-owned-node-login.md) | Machine Access Is a Union, and a Ceph Cluster Owns Its Node Login | revises 0019; revised by 0027 |
| [0025](0025-composed-names-are-labels-plus-explicit-overrides.md) | Composed Names Are Labels Plus Explicit Overrides | refines 0017; `fqdn` override repaired by 0035 |
| [0026](0026-custom-playbook-kind-name.md) | The Operator-Supplied Playbook Kind Is `CustomPlaybook` | renames the kind of 0005, 0021 |
| [0027](0027-bootwright-owns-the-login-it-installs.md) | Bootwright Owns the Login on the Machines It Installs | revises 0019, 0024; refined by 0033 |
| [0028](0028-terminal-for-the-borrowed-identity.md) | A Terminal for the Borrowed Identity, Never for the Created One | extended by 0029 |
| [0029](0029-answering-a-sudo-password-for-the-borrowed-identity.md) | Answering a `sudo` Password for the Borrowed Identity | extends 0028 |
| [0030](0030-one-intent-flag-and-named-authorizations.md) | One Intent Flag and Named Authorizations | revises 0007, 0010; refined by 0031, 0039, 0054; extended by 0032, 0034, 0038, 0040, 0042 |
| [0031](0031-data-loss-follows-the-data-and-policy-is-not-drift.md) | Data-Loss Authorization Follows the Data, and Policy Is Not Drift | refines 0007, 0030 |
| [0032](0032-tearing-down-input-a-newer-build-cannot-read.md) | Tearing Down Input a Newer Build Cannot Read | extends 0030 with the `stale-input` token |
| [0033](0033-the-login-follows-the-scope-and-carries-its-credential.md) | The Login Follows the Command's Scope, and Carries Its Credential | refines 0019, 0024, 0027 |
| [0034](0034-wiping-a-device-no-node-claims.md) | Wiping a Device No Node Claims | extends 0030 with the `unowned-devices` token; refined by 0054 |
| [0035](0035-a-storage-node-answers-to-the-name-cephadm-registers.md) | A Storage Node Answers to the Name cephadm Registers | enforces 0017; repairs the `fqdn` override of 0025; revised by 0036 |
| [0036](0036-bootwright-writes-the-name-a-storage-node-answers-to.md) | Bootwright Writes the Name a Storage Node Answers To | revises 0035 |
| [0037](0037-a-tpm-holds-the-key-a-passphrase-holds-the-machine.md) | A TPM Holds the Key, a Passphrase Holds the Machine | follows the enablement grammar of 0014 |
| [0038](0038-removing-the-cluster-a-node-was-left-running.md) | Removing the Cluster a Node Was Left Running | extends 0030 with the `foreign-daemons` token; follows 0034 |
| [0039](0039-the-node-a-teardown-left-serving-the-cluster.md) | The Node a Teardown Left Serving the Cluster | refines the `unreachable-nodes` token of 0030; closes the teardown side of 0038 |
| [0040](0040-one-word-for-every-token-a-verb-accepts.md) | One Word for Every Token a Verb Accepts | extends 0030 with the `all` token |
| [0041](0041-the-gate-runs-what-the-change-can-break.md) | The Gate Runs What the Change Can Break | replaces the single `check-fast` gate; retires `make check` for `make check-full` |
| [0042](0042-moving-the-vote-that-breaks-the-tie.md) | Moving the Vote That Breaks the Tie | adds `storage-cluster replace-arbiter`; extends 0030 with the `same-site-arbiter` and `degraded-quorum` tokens and a third authorization verb; widens the `unreachable-nodes` token of 0030/0039 |
| [0043](0043-one-cluster-one-address-family.md) | One Cluster, One Address Family | extended by 0044 |
| [0044](0044-the-endpoint-a-single-node-cluster-answers-at.md) | The Endpoint a Single-Node Cluster Answers At | extends 0043; follows the source union of 0014 |
| [0045](0045-installing-the-os-a-golden-image-already-carries.md) | Installing the OS a Golden Image Already Carries | follows the second-backend dispatch rule of 0002; follows the presence-union grammar of 0014 |
| [0046](0046-the-hosts-preflight-cannot-reach-yet.md) | The Hosts Preflight Cannot Reach Yet | extends the name-resolution group of 0017; closes the coverage gap 0035 recorded |
| [0047](0047-the-certificate-a-vendor-gateway-never-settles-on.md) | The Certificate a Vendor Gateway Never Settles On | revised by 0049: the vendor ssl pin becomes the declared exposure field |
| [0048](0048-the-site-a-machine-stands-in.md) | The Site a Machine Stands In | extends the arbiter move of 0042; the site registry backs the placement grammar of 0018 |
| [0049](0049-the-scheme-a-gateway-declares-out-loud.md) | The Scheme a Gateway Declares Out Loud | revises the implicit vendor ssl pin of 0047; its repair machinery remains; extended by 0052 for the port the scheme implies |
| [0050](0050-the-machine-an-installer-boot-finds-powered-on.md) | The Machine an Installer Boot Finds Powered On | revises the occupancy probe of 0011; follows the physical-half rule of 0034 and the one-token-per-refusal grammar of 0030 |
| [0051](0051-the-repository-an-ibm-fleet-already-subscribes-to.md) | The Repository an IBM Fleet Already Subscribes To | extends the distribution tail of 0015; ends the storage-phase purge of machine-declared repositories |
| [0052](0052-the-port-a-scheme-arrives-on.md) | The Port a Scheme Arrives On | extends the declared scheme of 0049; separates the classic dashboard's TLS port from the gateway's |
| [0053](0053-the-flight-plan-a-run-publishes.md) | The Flight Plan a Run Publishes | extends the wait split of 0022; makes the run-tree strata a lowered value, nests logs per cluster, narrows the OLM and playbook edges |
| [0054](0054-a-filter-is-not-permission-to-wipe-a-device.md) | A Filter Is Not Permission to Wipe a Device | refines 0007, 0030, 0034; limits OSD auto-reclaim to an effective unbounded managed data selection |
| [0055](0055-the-controller-resolves-before-the-machine-it-contacts.md) | The Controller Resolves Before the Machine It Contacts | refines 0017, 0018, 0023, 0046 |
| [0056](0056-the-support-report-a-storage-node-can-produce.md) | The Support Report a Storage Node Can Produce | follows 0002 and 0007; gives storage support tooling the owned package lifecycle |
| [0057](0057-the-dag-decides-how-many-clusters-fly.md) | The DAG Decides How Many Clusters Fly | extends 0053; follows the value-carrying flag convention of 0010 |
| [0058](0058-storage-destroy-completion-is-positive-proof.md) | Storage Destroy Completion Is Positive Proof | revises managed-storage A2 and fail-closed edges of 0023; follows 0007 and 0039 |
