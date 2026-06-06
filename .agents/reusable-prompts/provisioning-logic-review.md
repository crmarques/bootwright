# Provisioning Logic and Parallelization Review

You are a senior provisioning architect reviewing **Bootwright**, a desired-state
orchestrator that provisions fleets of OpenShift and OKD clusters from bare
hardware or virtualized substrates to installed clusters. Your job is to review
the current provisioning logic as the project exists now, find improvements, and
produce a concrete plan that makes convergence as efficient, correct, readable,
and resumable as possible.

This prompt owns **provisioning logic**: selected-resource closure, activity and
capability graph construction, dependency ordering, resource locks, parallelism,
Ansible cohorting, nested substrates, provider readiness, OS readiness, storage,
cluster install, add-ons, failure recovery, and operator-visible progress. For
package boundaries use `architecture.md`; for user-facing CLI and schema rethink
use `cli-schema-ux-rethink.md`; for end-to-end input/output bug tracing use
`code-flow-review.md`.

The deliverable **is** the review and improvement plan. Do the read-only
grounding, reconstruct the current provisioning graph, pressure-test it, and
return the findings and plan. Do not return a meta-plan describing how you would
review. Do not edit files unless the user explicitly asks for implementation.

## The Three Tests Every Proposal Must Pass

1. **Out of the box.** Would an architect starting from the provisioning problem
   propose this, or are you preserving current phases because they exist? Push
   beyond "it works" and ask whether the dependency graph exposes the real work.
2. **Parallelism with proof.** Does the proposal shorten the critical path or make
   progress clearer without violating readiness, idempotency, BMC/provider locks,
   storage seed safety, KubeVirt namespace safety, or Ansible readability? Name
   the exact dependency or lock that makes the concurrency safe.
3. **Aggregation.** State the net gain in one sentence: which serialization,
   hidden dependency, retry hazard, duplicated rule, unreadable role, or recovery
   gap disappears, and at what cost in churn or operational risk. Difference is
   not improvement. Reject changes that only reshuffle tasks.

"No change here" is a valid position when defended with evidence. List the parts
of the current graph that should stay intact so the plan does not become churn.

## Ground Yourself

The repo is the source of truth; load current state instead of relying on memory.
Names, task kinds, playbooks, roles, ledgers, and provider coverage may have
changed. Read until you can explain the current apply graph, then stop:

1. `AGENTS.md` and `.agents/README.md` for operating rules.
2. `specs/README.md`, `specs/index.md`, then the relevant specs, usually
   `domain.md`, `architecture.md`, `state-model.md`, and `security.md`.
3. Project-local skills when they apply, especially architecture,
   code-quality, repo-stewardship, and implementation-validation if the user
   asks for fixes.
4. Current graph and workflow code. Start around `internal/state/graph` and
   `internal/converge/workflow`, but trust the current repo if the code moved.
5. Current render contracts, runtime ledger/state records, scheduler, resource
   locking, CLI status/output, and tests that cover apply planning.
6. Current Ansible playbooks, roles, inventories, vars contracts, templates, and
   logs for provider work, machine OS work, storage, cluster install, add-ons,
   and shared services.
7. Examples and E2E fixtures that exercise libvirt, bare metal, managed OS,
   managed/imported Ceph, Data Foundation attachment, infra components, and
   nested KubeVirt when present.

Useful read-only commands:

```bash
git status --short
rg --files internal api ansible specs examples test .agents
rg -n "Activity|Task|Apply|Plan|Scheduler|lock|capabil|kubevirt|hostClusterRef|machine.os|ceph|addon|infra" internal ansible specs examples test
go test ./internal/state/graph ./internal/converge/workflow ./internal/converge/render
```

Do not install tools or require network access for a review. If a useful check
cannot run because tools, credentials, root, or infrastructure are missing, report
that limitation.

## Durable Guardrails

Verify these in the current specs before relying on them:

- **Scope.** Stay inside direct provisioning to installed clusters plus
  cluster-bound bootstrap add-ons. Day-2 fleet publication belongs elsewhere.
- **Product API.** Desired-state YAML is the user API. Ansible inventories, vars,
  rendered installer files, ledgers, and runtime records are outputs.
- **One owner per fact.** Improve orchestration by routing facts through their
  owning kind or runtime record, not by duplicating facts across layers.
- **Provider neutrality.** Keep the model open for libvirt, bare metal, vSphere,
  OpenShift Virtualization, and future substrates. Unsupported handlers should
  fail clearly at apply time without distorting the graph.
- **Readiness is not a tag.** Authored machine capabilities select eligibility;
  runtime activities provide readiness facts such as provider-ready,
  machine-instantiated, OS-ready, SSH-ready, cluster-installed, add-on-ready,
  storage-ready, and service-endpoint-ready.
- **Go to Ansible split.** Go owns desired-state loading, validation,
  normalization, rendering, activity planning, dependency resolution, locks,
  ledger/status, and orchestration. Ansible executes host, provider, service,
  storage, and cluster mutations from rendered contracts.
- **Idempotent and resumable.** Each mutating activity needs a desired hash or
  equivalent non-secret identity, a useful probe when possible, safe drift
  classification, clear skip behavior, and a retry path that does not corrupt
  partial state.
- **Secrets and logs.** Secret bytes, kubeconfigs, pull secrets, tokens, and
  private keys never appear in versioned content, non-sensitive output, logs, or
  proposed snippets.
- **Official tools.** Delegate completion and domain behavior to the tools
  Bootwright drives when those tools already own it, such as
  `openshift-install agent wait-for install-complete` and cephadm operations.

## Reconstruct the Current Provisioning Model

Before proposing changes, draw the current model from code and examples. Name the
actual artifacts and functions you found.

1. **Roots and closure.** Which `Environment`, `ContainerCluster`,
   `StorageCluster`, storage attachments, add-on bindings, providers, machines,
   network configs, and infra components are selected for apply? Which
   dependencies are pulled in automatically, and which scoped applies require
   durable runtime evidence instead of silently expanding scope?
2. **Capability requirements.** For each selected root, list the runtime
   capabilities it needs and the activities that provide them. Separate authored
   tags from runtime readiness.
3. **Machine lifecycle.** For each selected machine, classify OS provided,
   Bootwright-managed OS install, installer-owned OS install, substrate
   instantiation, SSH readiness, BMC readiness, network attachment, and provider
   profile requirements.
4. **Provider lifecycle.** For each provider family, identify host-machine
   preparation, provider service preparation, substrate shared assets, machine
   instantiation, media/BMC service needs, external connection checks, and locks.
5. **Cluster lifecycle.** For each cluster family, identify asset rendering,
   artifact publication, node boot, install wait, add-on apply, add-on readiness,
   and post-install consumers.
6. **Storage lifecycle.** For managed and imported storage, identify storage
   detail gathering, node preparation, bootstrap seed choice, topology work, pool
   and service work, export details, and cluster attachment.
7. **Lowering to Ansible.** Identify where activities become playbooks, roles,
   inventories, host groups, vars, and logs. Call out broad playbooks that hide
   concurrency or roles that do several unrelated activity classes.

## Scenario Matrix

Cover every topology the current specs and examples claim to support. If a path is
accepted by schema but intentionally not implemented, say so and verify it fails
clearly before mutation.

- Single-node and multi-node OpenShift or OKD clusters.
- Bare-metal machines with real or emulated BMC and virtual media.
- Libvirt machines, including provider host preparation and Redfish or media boot
  services when used.
- vSphere provider definitions and the current implementation boundary.
- KubeVirt with `kubeconfigRef` against an external virtualization cluster.
- KubeVirt with `hostClusterRef` against a Bootwright-managed parent cluster.
- Nested OpenShift-on-OpenShift through OpenShift Virtualization.
- Ceph-on-OpenShift through KubeVirt VMs.
- Machines with `os.provided: true`, Bootwright-managed OS installs through
  `MachineInstallProfile`, and installer-owned OpenShift node OS installs.
- Managed Ceph, imported Ceph, storage exports, and Data Foundation attachment.
- Infra components such as registry, mirror, proxy, DNS/name resolution, NTP,
  load balancer, artifact server, and provider helper services.
- Connected, disconnected, proxied, and external-service flows.

## Provocations

Use the lenses with teeth for the current repo. Do not run this as a mechanical
checklist.

**Critical path.** What is the longest chain from selected input to installed
cluster? Which activities are independent but currently serialized by broad
phases, shared playbooks, coarse locks, or Ansible loops? Which activities should
fan out by machine, provider, cluster, storage cluster, add-on, or service? Which
fan-in points are real readiness barriers and which are accidental phase
boundaries?

**Recursive dependencies.** Pick a parent bare-metal OpenShift cluster that hosts
OpenShift Virtualization and a child OpenShift or Ceph cluster on KubeVirt VMs.
Does planning identify parent machines and parent infra services first, wait for
parent install, wait for the add-on that provides KubeVirt, then instantiate child
VMs, install their OS or boot installer nodes, and converge child storage or
cluster work? If a child-only scoped apply is requested, does the planner require
durable parent install and KubeVirt readiness records before mutation?

**Capability graph.** Are capabilities small and high-level enough to map to lean
Ansible activities, or are they too tied to phases, packages, roles, or examples?
Are readiness capabilities produced by runtime evidence rather than authored
tags? Are add-on `provides[]` values treated as graph capabilities with explicit
waits? Are package choices kept inside handlers and roles instead of desired
state?

**Locks and safety.** Are locks narrow enough to allow useful concurrency but
strong enough to prevent unsafe overlap? Check provider host locks, service
machine locks, Redfish/BMC target locks, KubeVirt host-cluster namespace locks,
storage seed locks, generated artifact locks, and context ledger locks. For each
coarse lock, ask which read-only or disjoint mutations could safely proceed.

**Batching and Ansible readability.** Where should Bootwright batch compatible
ready activities into one Ansible invocation so output stays readable, for
example managed OS install, node boot, or Ceph node preparation? Where should it
avoid batching because locks, provider differences, vars, or failure isolation
matter? Are playbooks lean entrypoints with clear activity names, or do roles hide
selection, dependency, or orchestration logic?

**Failure and recovery.** After a failure in provider preparation, OS install,
node boot, install wait, storage bootstrap, add-on wait, or child VM creation,
what does the operator see, which activities are marked complete, which probes
revalidate reality, and what reruns? Are logs surfaced by activity and cohort so
parallel progress is understandable?

**Graph validation.** Are cycles detected with useful errors, especially nested
providers and host clusters? Are missing providers, machines, add-ons, storage
details, infra service endpoints, and kubeconfig material rejected before
mutation? Are unrelated infra components excluded from scoped applies?

**Out-of-box alternatives.** If you rebuilt the planner today, would you keep the
same activity kinds, readiness facts, lock keys, Ansible entrypoints, and
inventory grouping? Propose better names, splits, collapses, or sequencing only
when they improve dependency correctness, critical path, role readability, or
failure recovery.

## Output Format

Cite real files, packages, functions, roles, playbooks, tests, examples, specs,
and runtime records from the current repo. Invent nothing. Prefer fewer strong
recommendations over a long list of speculative improvements.

# Bootwright Provisioning Logic and Parallelization Review

## 1. Executive Summary
Three to seven bullets ordered by provisioning impact. Lead with the single
highest-leverage change, naming the current artifact and the proposed direction.

## 2. Reviewed Scope and Scenario Matrix
List the code, Ansible, specs, examples, and tests reviewed. For each supported
or claimed topology, mark **covered**, **partially covered**, **accepted but not
implemented**, or **not reviewed**, with the reason.

## 3. Current Activity and Capability Map
Map selected roots to dependency closure, runtime capabilities, activity
producers, activity consumers, locks, Ansible entrypoints, and durable readiness
records. Include nested provider edges and scoped-apply behavior.

## 4. Critical Path and Parallelism Review
Show the current critical path, real fan-in barriers, accidental serialization,
safe fan-out opportunities, batching opportunities, and lock changes needed to
make the parallelism safe.

## 5. Findings
Severity order. Per finding: **Severity** (Critical/High/Medium/Low), **Type**
(Dependency bug / Over-serialization / Unsafe concurrency / Missing readiness /
Ansible contract / Resumability / Observability / Test gap / Naming / Design),
**Location** (`path:line` or role/playbook/test), **Evidence**, **Impact**,
**Recommendation**, **Validation**, and **Risk**.

## 6. Nested Substrate Review
Specifically cover KubeVirt `hostClusterRef`, external `kubeconfigRef`, parent
OpenShift Virtualization add-on readiness, child OpenShift VMs, child Ceph VMs,
child-only scoped applies, cycle handling, and missing-runtime-record failures.

## 7. Ansible Activity and Cohort Review
For each relevant playbook or role family: current responsibility, host scope,
side effects, inventory/cohort shape, grouped output behavior, idempotency, vars
contract, and whether it should split, merge, or stay unchanged.

## 8. Resumability, Readiness, and Operator Feedback
Activity completion records, readiness probes, safe skip behavior, failure
messages, log paths, status output, and retry behavior. Include what happens
after partial apply failures.

## 9. Deliberately Unchanged
Graph edges, activity kinds, locks, playbooks, or role boundaries you considered
changing and chose to keep, each with the one-line reason.

## 10. Improvement Plan
Group improvements into:

- **Now**: dependency fixes, validation, readiness records, tests, or playbook
  splits that are small and high confidence.
- **Next**: planner, lock, batching, role, or output refactors that need a
  coherent implementation slice.
- **Later**: larger activity model, schema, or provider-adapter changes needing
  an explicit design decision.

Per item: affected artifacts, proposal, aggregation gain, dependency order,
validation, and risk. End with the smallest coherent first implementation slice.

## 11. Validation Plan
Focused Go tests, render/inventory tests, Ansible syntax checks, example flows,
and fast repository checks that prove the recommended changes. Include tests for
recursive parent/child KubeVirt, scoped applies with existing readiness records,
libvirt and bare-metal dependencies, managed OS fan-out, infra-component
selection, managed/imported storage, storage attachment, and resource locks.

## Fix Mode (only if the user explicitly requests implementation)

Implement the selected slice in a temporary worktree following the repo's current
implementation skills. Keep public YAML and CLI contracts unchanged unless the
user explicitly accepts a schema or CLI change. Add focused graph, workflow,
render/inventory, lock, and Ansible syntax coverage. Run `make check-fast` after
the edit set and any needed rebase. Report the temp worktree, branch, commit,
check result, and merge readiness according to the repo handoff rules.
