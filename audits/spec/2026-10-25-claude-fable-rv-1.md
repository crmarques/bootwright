# Bootwright specification, architecture and agent-skill audit

Date: 2026-10-25. Reviewer: Claude Fable 5.1 (orchestrating, arbitrating and authoring), with Claude Opus 5.5 and Claude Sonnet 5.5 agents under its direction. Repository: `bootwright` at commit `17acb63d9` on `main`. Scope: specifications, concept model, desired-state API, architecture at the package and port level, reconciliation rules, agent guidance and skills. Out of scope: line-level code review and any command with effects. This file is the only repository change the audit makes.

## How to read this report

The report is long because the owner asked for complete coverage of every spec and skill file and for verified findings rather than impressions. It is organized so that each reader can stop early.

| If you want | Read |
| --- | --- |
| The verdict and the five actions that matter most | Section 2 |
| What to do, in what order, and which steps need an owner decision | Section 14, then the High and Medium entries of section 11 |
| The operator-facing API, kind by kind, with recommended shapes | Section 5, with the vocabulary of section 4 |
| Whether a new substrate, distribution, storage or service implementation can be added with low code impact | Section 6, then section 7 for the language rules |
| Every reconciliation rule with a verdict | Section 8 (parts A and B) |
| How to make the spec corpus and the skills cheaper and safer for coding agents | Sections 9 and 10 |
| Whether a specific file was examined and what the assessor concluded | Section 12 |
| How this audit relates to the sibling Codex audit of the same date | Section 13 |
| How the findings were produced and verified, and the limits of the method | Section 15 |

Finding identifiers (for example `F10-01` or `R2-02-01`) refer to the catalog in section 11; the prefix names the lens that raised the finding. Severity is Blocking, High, Medium or Low on the scale the brief defines, calibrated downward by verification where the two verifiers disagreed. Every finding cites the file and line range it rests on, as of the audited commit.

## Contents

1. [The audit request](#1-the-audit-request)
2. [Executive assessment](#2-executive-assessment)
3. [Objectives, scope and domain](#3-objectives-scope-and-domain)
4. [Concept model and nomenclature](#4-concept-model-and-nomenclature)
5. [Desired-state API: grammar, kinds and recommended shapes](#5-desired-state-api-grammar-kinds-and-recommended-shapes)
6. [Architecture: generic flow, specialization and code materialization](#6-architecture-generic-flow-specialization-and-code-materialization)
7. [Language responsibility matrix: Go, Ansible, Python, shell](#7-language-responsibility-matrix-go-ansible-python-shell)
8. Reconciliation rule audit: [part A, mutation safety, ownership, trust and secrets](#8-reconciliation-rule-audit-part-a-mutation-safety-ownership-trust-and-secrets) and [part B, lifecycle, continuation, comparison and durability](#8-reconciliation-rule-audit-part-b-lifecycle-continuation-comparison-and-durability)
9. [The spec corpus as an agent harness](#9-the-spec-corpus-as-an-agent-harness-structure-duplication-and-retrieval-cost)
10. [Skills and agent harness](#10-skills-and-agent-harness-current-state-proposed-portfolio-runtime-contract-and-token-budget)
11. [Findings catalog](#11-findings-catalog)
12. [Per-file coverage ledger](#12-per-file-coverage-ledger)
13. [Cross-check against the sibling audit](#13-cross-check-against-the-sibling-audit)
14. [Ordered remediation roadmap](#14-ordered-remediation-roadmap)
15. [Method and verification record](#15-method-and-verification-record)

## 1. The audit request

This is the prompt the audit ran under. It was written before any finding work started, after a scouting pass that sized the corpus, and every agent of the audit received it verbatim as its first instruction.

Date: 2026-10-25. Repository: `/home/crmarques/projects/gitops-workspace/bootwright` at commit `17acb63d9` (branch `main`). Requested by the repository owner. Deliverable: one report at `audits/spec/2026-10-25-claude-fable-rv-1.md`; no other repository change.

### Role

Act as a principal architect who has shipped declarative infrastructure products (Kubernetes-style APIs, installers, reconcilers), designed plugin and adapter architectures for multi-vendor platforms, reviewed safety-critical automation against physical hardware, and operates AI coding agents (Claude Code and OpenAI Codex) on large repositories. You audit the written contracts of this repository, not its line-level code. You judge by evidence, state the standard you judge against, and separate confirmed defects from improvements and from matters of taste.

### What Bootwright is

Bootwright is a declarative day-0 orchestrator: an operator authors a set of Kubernetes-style YAML objects (`apiVersion: bootwright.io/v1alpha1`, 33 kinds) describing one complete environment (substrate providers and machines, operating systems, infrastructure services, container clusters, storage clusters, cluster add-ons). Bootwright admits and freezes that input into a context, compiles it, plans an immutable operation of blocks, drives qualified native tools (openshift-install, cephadm, Redfish, libvirt, oc, OLM) through Ansible adapters, verifies each outcome and records durable evidence, until GitOps can take over. It is not a continuously reconciling controller and has no day-2 scope.

The owner's immediate support portfolio is: libvirt with sushy-tools emulated BMCs; bare metal through vendor BMCs (first xFusion); OpenShift Virtualization (KubeVirt) machines; IBM Storage Ceph; IBM Fusion Data Foundation as a cluster add-on; NTP and proxy services either container-provisioned by Bootwright or external; networking that already exists (provided, not managed). The owner intends later additions with low code impact: other substrates (VMware, AWS, GCP, Azure), other cluster types (vanilla Kubernetes, Rancher, EKS, AKS), other storage (open-source and Red Hat Ceph, NetApp, Pure Storage) and other infrastructure component types. The specs need not name these; they must guarantee that the general code flow is generic, that specifics live behind interfaces, adapters or registries selected by the operator's input, and that general code never knows which implementation runs.

### Material in scope

Every file under `specs/` (core specs, `specs/api/`, `specs/cli/`, `specs/contexts/`, `specs/deferred/`, `specs/milestones/` including `delivered.md` and `backlog.md`), every skill file (`.agents/skills/**`, the `.claude/skills/**` wrappers, `.agents/skills/index.md`), the agent entrypoints and rules (`AGENTS.md`, `CLAUDE.md`, `.claude/rules/*.md`, the hooks in `.claude/settings.json`), the knowledge index and its conventions (`.agents/knowledge/index.md`; sample pages as evidence of the convention), `docs/*.md`, `README.md`, `examples/README.md` and the tracked example inputs under `examples/` (excluding `examples/wip/`). The Go package map (`internal/**` directories, `contracts.go` files, `cmd/bootwright/wiring_*.go`, `test/architecture/*.go`), the Ansible collection layout (`ansible/collections/ansible_collections/bootwright/core/{roles,plugins,playbooks}`) and `api/v1alpha1/*.go` are in scope only as evidence of how concepts materialize at the package, port, registry and role level.

Out of scope: low- and medium-level code review (function bodies, tests, error handling), running any Bootwright command with effects, and any change to the repository other than the report.

### Questions the audit must answer

1. **Objectives, scope and domain.** Are purpose, scope, non-goals and domain boundaries stated once, consistently and testably? Is the immediate portfolio distinguishable from the intended family scope, and does the gap distort any design? Is the day-0/GitOps-handoff boundary coherent with everything the specs promise?
2. **Concept model and nomenclature.** Are the core concepts (environment, context, controller, operation, attempt, block, stage, wave, slice, substrate, provider, machine, realized target, reservation, implementation, variant, specialization, adapter, driver, package, profile, binding, export, custody, evidence, receipt, refusal) defined once, named consistently across API kinds, CLI nouns and verbs, specs and code, and free of overloads? Propose a normative vocabulary, a rename table where names should change, and naming rules for future kinds and fields.
3. **Desired-state API as an operator language.** Judge the authoring grammar (envelope, references, unions, collections, enablement, defaults, effective state, native fields, the environment directory and selection) and each kind's field design against the standard of the best declarative platform APIs: intuitive for a platform operator, minimal, composable, unambiguous on omission/null/empty, stable, provider-neutral where intent is shared and explicitly provider-shaped where it is not, and truthful about what is supported. Identify where the API leaks implementation, where it forces operators to repeat facts, where kinds should merge or split, and where a field name misleads. Give concrete recommended shapes.
4. **Concept interaction and architecture.** Are the interactions between concepts (environment → machines → OS → services → clusters → storage → add-ons; context ↔ operation ↔ evidence; implementation selection ↔ frozen request ↔ continuation) specified as complete contracts? Does the architecture spec (bounded contexts, ports, dependency direction, composition root, registries, version specialization) guarantee a generic general flow with specialization only behind ports and registries chosen by input? What exactly would adding a new substrate, cluster type, storage type or infrastructure component touch today, and is that the minimum? Evaluate how the spec's promises materialize in the package map, ports, wiring, Ansible roles and action-plugin protocols, at the architectural level only.
5. **Reconciliation rules: security, safety, reliability.** Review every rule governing apply, destroy, plan, continuation, attempts and unknown outcomes, convergence, block transitions, state precedence, dependency safety during recovery, desired-versus-real comparison, GitOps readiness, mutation safety (quiescence, physical disk safety, opt-in overwrite, controller-host protection), confirmation and authorization, ownership and identity proofs, locking and reservations, secrets custody and the trust model. For each rule judge: sound, ambiguous, contradicted elsewhere, unsafe, or unimplementable as written; name the failure scenario and the smallest correction.
6. **Language responsibility.** Is it clear which logic belongs in Go, Ansible, Python and shell, and why? Where is the boundary violated or unspecified (Python action plugins and module utilities, shell wrappers, Makefile, completion scripts)? Propose a decision matrix with rules and prohibited uses.
7. **Spec corpus as an agent harness.** Is the one-owner-per-fact model, the index, the milestone/ID scheme, the "Not yet met" convention and the knowledge split effective for coding agents? Quantify the retrieval path a typical task forces an agent through (tokens) and identify duplication, drift between pages, pages that are too large or badly placed, and conventions that cost tokens without buying precision. Propose restructuring that preserves precision.
8. **Skills and agent instructions.** Review every skill, reference, wrapper, rule file, hook and entrypoint for correctness, discoverability (Claude Code and Codex), contradiction, duplication, procedural completeness, placement of vendor- or model-specific guidance, and total accepted-task token cost. Propose an explicit keep / modify / remove / add portfolio with the purpose, trigger description, body outline and size budget of each skill, a common contract that works in both runtimes, and the measurements that would show the portfolio works.

### Method and evidence standard

- Read the owning sections for each question fully; do not sample a page you make a claim about. Record every file you read.
- Every finding carries: a stable id, a title, the area, a severity (Blocking: an unsafe or contradictory rule, or a design that blocks the stated goals; High: an operator- or agent-visible defect of the contract; Medium: a gap or ambiguity that will produce divergent implementations; Low: clarity, naming or cost), the kind (contradiction, gap, ambiguity, unsafe-rule, design, naming, duplication, process, cost), evidence as `path:line-range` plus a quote of at most 25 words per cited passage, the consequence (a concrete failure scenario or cost), the smallest recommendation, acceptance criteria that a reviewer can check, and the result of checking the recommendation against recorded owner decisions (`specs/milestones/backlog.md` decisions D1 to D173) and stated project principles.
- Prefer the finding that changes what the owner will do. Do not report style. Do not restate a "Not yet met" line as a defect unless the rule itself is wrong. A specified capability that is not implemented is not a defect; a specified rule that cannot be implemented safely is.
- Each finding is verified independently before it enters the report: one verifier tries to refute it from the cited text, one checks impact, severity and the recommendation against decisions and principles. A split vote is arbitrated. Refuted findings are not reported as findings; a refuted candidate that an operator would plausibly also raise may be listed as "considered and refuted".
- Treat `AGENTS.override.md` as a local-only override file: never quote or paraphrase its content, and never place environment-specific facts (host names, addresses, credentials, organization names, private branch names) or anything from `examples/wip/` in any output. Refer to that file only as "the local-only override file".
- Do not read `audits/` during the finding phases; a separate cross-check against the sibling audit happens after verification and is reported in its own section.
- Never modify, create or delete a file in the repository. Write intermediate material only under the scratch directory named in your instructions.

### Report structure

1. The audit request (this text).
2. Executive assessment: what is good, what needs improvement, what is bad; a scorecard by dimension; the five actions that matter most.
3. Objectives, scope and domain.
4. Concept model and nomenclature: normative vocabulary, rename table, naming rules.
5. Desired-state API: grammar verdict, per-kind verdicts, recommended shapes.
6. Architecture: generic flow and specialization; extension scenarios with the exact impact of adding a substrate, cluster type, storage type and infrastructure component; code materialization map; the immediate portfolio.
7. Language responsibility matrix.
8. Reconciliation rule audit: rule-by-rule table and findings.
9. Spec corpus structure and maintenance for agent efficiency.
10. Skills and agent harness: current state, proposed portfolio with drafts, Claude Code and Codex contract, token budget model.
11. Findings catalog, most severe first, with verification verdicts.
12. Per-file coverage ledger for every spec, skill, wrapper, rule, hook and entrypoint file.
13. Cross-check against the sibling audit.
14. Ordered remediation roadmap.
15. Method and verification record.

## 2. Executive assessment

### 2.1 Verdict

Bootwright's foundations are stronger than those of most pre-1.0 declarative platforms, and the owner should protect them. The authoring grammar refuses every YAML ambiguity that Kubernetes, Helm and Kustomize tolerate. The lifecycle engine runs an immutable plan with write-ahead records at every boundary, treats an unknown outcome as unknown, re-proves every decision under the exclusive lock, and never knows which implementation it drives. Implementation identity is frozen per block, selected from a static allowlist bound at one composition root, with release differences held inside version specializations. The spec corpus states one owner per fact and holds its tables to code with tests. These are the right bones.

The specs do not yet deliver three of the things the owner asked this audit to confirm.

1. **The generic-flow guarantee holds at the engine and fails above it.** Substrate arms, Ceph field paths and implementation identities leak into consumers and general-flow packages, two bounded contexts are defined as their first product, and no fitness test holds the rule that policy never branches on implementation [F06-01] [F06-03] [F18-03] [F18-04] [F07-03]. Adding a substrate today edits every consumer; adding a cluster distribution or a non-Ceph storage type has no specified home. The intent is right, and the owner's "low code impact" requirement is a stated goal rather than a property the architecture enforces.
2. **The operator language has a sound grammar and three product-shaped surfaces.** `ContainerCluster` is OpenShift under a neutral name, the `Storage*` child kinds are Ceph objects under neutral names with no rule binding them to a Ceph cluster, and `Machine` scatters substrate facts across conditional rules and mixes host prerequisites with product-named roles [F18-02] [F05-03] [F04-01] [F04-02]. Four union idioms answer one question, and operators repeat facts that admission could derive [F03-01] [F03-03].
3. **Nine High findings, six of them reconciliation safety defects, must close before any new destructive path or implementation.** A physical managed-OS install erases whatever disk a kernel device name points at [F10-01]. Name-derived ownership on libvirt contradicts the page's own rule [F10-03]. Storage retirement can delete the backing storage of a cluster that keeps running [R2-02-01]. A rotated BMC credential strands every removal [R2-04-01]. Two contexts can share a managed bridge without a claim [R2-06-01]. A removal observation reads an unreadable target as no effect, the one claim both audits rate High [CX-R5].

Everything else is improvable at low cost, and much of it is cheap because the pre-1.0 rule removes converters and aliases from every rename (D142). The corpus and the skills are small and well shaped; their cost problem is one mandated sentence that sends agents through whole milestone pages, and their safety problem is two sentences in the wrong files and one hook.

### 2.2 Scorecard

| Dimension | Assessment | Main action | Lead findings |
| --- | --- | --- | --- |
| Objectives, scope, domain | Good, with one structural gap | Split "Product scope" into a committed portfolio and a design envelope; state the day-2 disposition of owned objects | [F01-01] [F01-02] |
| Concept model and nomenclature | Needs improvement | Define the overloaded terms once, sweep bare "controller", adopt the naming rules and the before-1.0 renames | [F02-01] [F02-07] [F02-09] [F05-05] |
| Desired-state grammar | Good, above mainstream | One union rule, derive instead of repeat-and-compare, omission classes stated once, schema discoverability | [F03-01] [F03-03] [F03-06] [F03-05] |
| Kind design | Needs improvement; three kinds bad | A distribution arm on `ContainerCluster`, Ceph-scoped child kinds or a type binding, a per-substrate arm on `Machine` | [F18-02] [F05-03] [F04-01] [F04-02] |
| Architecture: generic flow and specialization | Good engine; guarantee not enforced above it | Adopt the specialization contract (section 6.2) with its fitness tests; land the substrate dispatcher before lifting any arm refusal | [F06-01] [F06-03] [F18-03] [F18-04] |
| Language responsibility | Good for Go and Ansible; Python unowned | Widen the boundary section into a four-language matrix with a placement question and a test | [F08-04] [F08-07] [F08-01] |
| Reconciliation: safety and ownership | Strong rules; six High defects | Phase 0 of the roadmap: disk identity, name-derived ownership, consumer quiescence, credential rebind, shared bridges, unreadable targets | [F10-01] [F10-03] [R2-02-01] [R2-04-01] [R2-06-01] [CX-R5] |
| Reconciliation: reliability and consistency | Strong core; dead ends and drift | Name an exit for each dead end, correct the stale contexts page, make removal compare effect identity across builds | [F09-01] [F09-02] [F13-01] [R2-03-01] |
| Security and trust | Sound host model; remote principals unmodelled | Add the remote-principals paragraph, tool authenticity per tool, fingerprint-taking enrollment | [F11-01] [F11-02] [F11-03] |
| Spec corpus as a harness | Right model; costly path | Two sentences on the mandatory path, milestone cards, section-level index rows, budgets that follow the second-opened pages | [F15-01] [F17-03] [F15-07] |
| Skills and agent harness | Good portfolio; misplaced safety | Move the effect prohibition to AGENTS.md, replace the Stop hook, align integration with D168, add two skills | [F14-01] [F14-02] [F14-03] |

Verification lowered 116 of 201 severities from the finders' proposals and takes the lower of two calibrations, so the scorecard understates rather than overstates. No candidate survived as Blocking; the one proposed Blocking, the disk-identity rule, carries High because its fix is small and the agent-installer path is already safe.

### 2.3 What is good and must be kept

- **The decoding and refusal contract.** Strict YAML, closed envelopes, provenance kept out of data, effective output never accepted back as input, and refusals that name object, path, expectation and next step (section 5.1). This should be the house standard for every future kind.
- **The engine contract.** Immutable plans, one operation per context, write-ahead records, unknown treated as unknown, the compare-and-swap re-proof under the exclusive lock, bounded retries, crash-safe produced-material custody, and the content digest as a contract identity that lets an automation-only fix continue a live operation (section 8.B1).
- **The best destructive contract in the corpus.** The opt-in physical logical overwrite carries WWN, serial and size per target, re-checks identity during write and readback, and binds its receipt to the original apply. Other destructive paths should copy it (section 8.3).
- **Closed implementation selection.** No ambient plugin path, a static resolver at one composition root, frozen implementation identity, fixed Ansible entry points that authored data can never select, and the version-specialization model with its refusal of version pairing rules (D162, D163; section 6.1).
- **The controller-host security model.** The attacker model is precise, the process boundary forbids a shell and allowlists argv and environment, the runtime lock pins every wheel by hash, and capability URLs are minted per attempt and never recorded (section 8.5).
- **The corpus model and its gates.** One owner per fact, a compact index, byte budgets that only shrink, spec tables held to code, the "Not yet met" convention, the structural milestone checker, and the knowledge split (section 9.2).
- **The harness shape.** One tracked entrypoint for both runtimes, canonical skills with thin wrappers, small skills that link rather than restate, measured verification tiers, and "rules already broken once" lists (section 10.1).

### 2.4 What needs improvement

- **Scope tiers and non-goals.** No page distinguishes the committed portfolio from the family envelope, so milestone completion gates on arms nobody committed to, and the networking boundary, the managed-OS scope and the day-2 disposition of owned objects are unstated [F01-01] [F01-05] [F01-06] [F01-02].
- **Vocabulary.** "Controller" names the host and a BMC in safety text, "implementation", "package", "capability", "binding" and "profile" each carry several meanings, the kind count is stated three ways, and no rule fixes enum casing or the `Ref` suffix [F02-01] [F02-02] [F02-04] [F02-09].
- **Grammar consistency.** Unions, omission classes, reference classes and single-valued required fields need one rule each; discoverability needs an `explain`, a schema export and an origins view [F03-01] [F03-06] [F03-07] [F03-04] [F03-05].
- **Selection closure and plan edges.** The authoritative closure table omits four storage kinds and a replication peer, standalone Machines drop silently when a cluster list is authored, and no catalog maps a reference field to a plan edge [R2-01-04] [R2-01-05] [R2-01-01].
- **Reliability exits.** One unreachable target abandons a whole removal, an apply can spend its removal's reserve, a lost block record under a completed apply has no exit, a controller identity change strands every context, and a pending setup intent blocks the host [F09-01] [F09-02] [F09-03] [F13-06] [F13-04].
- **Trust beyond the host.** Managed hosts, BMCs, registries and cluster APIs are not modelled as principals, downloaded tools have no authenticity source beyond their first digest, and scripted enrollment pins an unconfirmed host key [F11-01] [F11-02] [F11-03].
- **Corpus cost.** Whole milestone pages of 63 to 141 KB sit on the mandatory path, owning sections reach 40 to 101 KB with no section-level routing, and the gates bound only the entry files [F15-01] [F15-07] [F15-08].

### 2.5 What is bad

- **A kernel device name selects the disk a physical install erases** while the install path refuses the durable identity hints every other destructive path requires [F10-01].
- **Name-derived ownership on the libvirt arm** lets a recreated same-name context adopt and overwrite an abandoned guest, against the page's own rule [F10-03].
- **Storage retirement with virtual hosts takes their stop proof as consumer quiescence**, so a surviving physical cluster loses its storage under a `data-loss` token that never named it [R2-02-01] [F10-04].
- **A rotated access credential has no rebind**, so every continuation and destroy authenticates with the old secret and the only exits abandon live effects [R2-04-01].
- **An external libvirt attachment claims nothing**, so two contexts share a managed bridge and one destroy cuts the other's uplink [R2-06-01].
- **`ContainerCluster` cannot take a second distribution** without a reshape or a new kind that touches every general-flow kind list [F18-02] [F18-04].
- **The owning page for contexts is stale after setup v2**, so an agent following the index writes the wrong registry version or wraps setup in a host-wide lock [F13-01].
- **Milestone scope rules cannot be decided from the tables an agent is told to read** [F17-03], and the storage coverage pages gate delivery on vendor support that three owner decisions forbid [F17-01].
- **The agent harness's only effect prohibition sits in an optional reference**, and the Stop hook runs the quick gate on the wrong tree and can block every turn end on an unrelated failure, which this session observed directly [F14-01] [F14-02].

### 2.6 The five actions that matter most

1. **Close the safety Highs before any new destructive path or implementation.** Durable root-disk identity on physical installs, the B568 exception and bridge claim on shared libvirt hosts, consumer quiescence for export retirement, an access-credential rebind, and the unreadable-target rule. Roadmap Phase 0 steps P0-C to P0-F plus CX-R5; each is a spec clause, a test name and a skill bullet.
2. **Make extension additive before extending.** Adopt the twelve specialization-contract rules (section 6.2) with their fitness tests, starting with selection from frozen input, no identity branching outside the owning domain, and consumer dispatch through the owning domain; land the substrate dispatcher role and capability facts on the realized target before B87 or B99 lift their refusals. Roadmap P1-A to P1-D.
3. **Take the three API shape decisions now, while they cost a same-version edit.** A distribution arm on `ContainerCluster`, Ceph-scoped child kinds or an explicit type binding, and a per-substrate arm on `Machine` with the role tokens dropped; adopt the one union rule and the derivation rule in the same decision. Roadmap P1-G.
4. **Fix the harness with four small edits.** The effect prohibition into AGENTS.md with widened ask patterns, a guarded advisory Stop hook, the mandated read changed from "the milestone page" to "the item's row and its section", and an Active slices table on the milestones page. These take Task A's mandatory reading from about 45k to about 18k tokens before any page is split. Roadmap P0-A, P0-B and the two-sentence fix in section 9.8.
5. **Make the corpus truthful and single-owned, then smaller.** Correct the contexts page, remove the vendor-support clauses, give the storage family one field-record home, then split the pages above budget along the owner boundaries they already name, with budgets that follow the second-opened pages. Roadmap P0-B, P2-A to P2-C.

### 2.7 The immediate portfolio

Every portfolio item is specified, and every unrealized one refuses truthfully. The libvirt arm and the bare-metal arm are realized; the bare-metal contract is the strongest in the corpus, with one exact system proof, a full MAC set, a UUID pin and a re-proof before media insertion. OpenShift Virtualization machines are refused before registration under B87 with a written contract whose deletion proof matches libvirt's but whose identity, lost-reply and reservation rules are weaker [R2-02-03], and whose day-2 stop is unreachable because power commands are wired to Redfish only [F18-07]. IBM Storage Ceph is realized for its milestone scope, with its coverage gated on vendor support the owner has forbidden [F17-01]. Fusion Data Foundation is specified end to end except for its removal order, which is the one High defect on the portfolio path [R2-02-01]. Managed and external NTP and proxy are realized and are the positive control for additive extension: an external service needs no code. Provided networking has no stated boundary [F01-06]. xFusion's BMC variation has no specialization model and sits in trust code [R2-02-04].

### 2.8 Confidence

The audit read every spec, skill, wrapper, rule, hook and entrypoint file (section 12), ran eighteen first-round and six second-round lenses, and sent each of 235 candidates through a refuter and an impact calibrator with arbitration on splits; 230 survived and 29 merged (section 15). The sibling audit of the same date agrees on the structural verdict and on the first defects to fix; its ten additional confirmed claims are carried in section 11.4 and scheduled in section 14.8. The limits are those of a textual audit: no function bodies or tests were reviewed, no command with effects ran, and a rule judged sound may still be implemented wrongly. Medium and Low entries are well-evidenced claims to check at the cited lines, not settled facts.

## 3. Objectives, scope and domain

This section answers brief question 1. The standard is a product-intent page that lets a newcomer or an agent decide, without reading behavior specs, whether a capability is committed, deferred or excluded. Each statement should appear once, agree with every other page, and be testable: a reviewer can name the spec row, refusal or test that breaks if it is violated.

### 3.1 What is good and should be kept

`specs/project.md` is short (105 lines, about 1,300 tokens) and correctly layered. It separates purpose, product scope, lifecycle and handoff, the implementation model, and priorities and non-goals. It delegates behavior by link instead of restating it (`specs/project.md:41-53`).

| Strength | Evidence | Why it meets the standard |
| --- | --- | --- |
| One-sentence purpose with a clear end state | `specs/project.md:3-5` | Day-0 "until it is ready for GitOps tools to take over"; README repeats it verbatim |
| Bounded lifecycle unit | `specs/project.md:36-44` | One frozen environment, positive verification or fail-closed, exact continuation; "Edited intent is never treated as continuation" |
| Handoff is a predicate, not a feeling | `specs/project.md:46-49` | Links a gate requiring completion, readiness, access and ownership evidence, surfaced in `status` |
| Safety non-goals are checkable | `specs/project.md:71-75, 90-96` | No ambient plugin path, `CustomPlaybook` has no execution authority, no read from an external location after admission, no pre-1.0 conversion |
| Specified is not supported | `specs/index.md:6`; `specs/substrates.md:45-51` | Undelivered arms refuse before registration with a Not-yet-met line naming the item |
| Delivery state lives elsewhere | `specs/project.md:31-32`; `README.md:12` | Scope never claims availability; milestones own it |
| Boundaries by exclusion | `specs/architecture.md:42-58` "Does not own" column | The right tool for domain boundaries |

The day-0 stance is honored consistently across the state contract, the coverage pages and the milestone pages, which link the boundary instead of restating it. Keep all of the above.

### 3.2 Where the four documents drift

The four documents are `specs/project.md`, `README.md`, `specs/architecture.md` and the milestone pages. Drift is small in wording but material in scope.

| Statement | project.md | README | architecture | milestones | Finding |
| --- | --- | --- | --- | --- | --- |
| Committed portfolio vs family | one undifferentiated list, 19-32 | absent | contexts named by product | every item gates its milestone | [F01-01], [F17-02] |
| Managed OS installation | absent | absent | Managed OS context, 52 | M1 outcome, m1.md:5 | [F01-05] |
| Networking boundary | absent | absent | Substrate owns attachments, 50 | M1c manages libvirt networks | [F01-06] |
| Day-2 non-goal | 98-102, undefined | absent | absent | substrates.md says "Day-2 power commands" | [F01-03] |
| Early per-cluster handoff | settled, 50-53 | absent | absent | pending Q60 under D173 | [F01-04] |
| Container cluster and Storage domains | family list | n/a | "OpenShift and OKD", "Ceph" | n/a | [F06-05] (also [F01-07]) |
| GitOps package, M7 link | "Argo CD"; extension text at 77-79 | n/a | n/a | m7.md links the wrong anchor | [F01-11] |
| "State reconciliation" context name | "not a continuously reconciling controller" | n/a | context name | n/a | [F01-10] |

Session-taken scope readings ("pending the owner's review") already define milestone contents. Section 9 covers that process gap [F17-04].

### 3.3 Is the immediate portfolio distinguishable from the family scope?

No. `specs/project.md:19-32` lists one set of "target platform families" and says they "bound Bootwright's intended scope". Nothing marks which entries are committed now [F01-01]. The owner's portfolio cannot be read from any page. xFusion, the first vendor BMC, appears only as a wire-reference URL (`specs/substrates.md:911`).

The gap distorts design in two ways.

1. **Completion is tied to family scope.** `specs/milestones.md:60-62` says every item gates its milestone. So M1 needs vSphere (M1f) and a managed Registry (B68). M3's exit tier needs a vSphere run, justified only by the scope list (`specs/milestones/m3.md:152`). M7 waits for all of M4, including GitLab [F01-01]. Milestone Requires encode priority, not dependency, and each new program needs another waiver [F17-02]. Portfolio work can finish and never reach "done".
2. **Domains are named by their first product.** The Container cluster and Storage contexts own "OpenShift and OKD" and "Ceph" (`specs/architecture.md:54-55`). Adding vanilla Kubernetes or NetApp then forces a choice the specs never made: widen the product contract or invent a context [F06-05].

The owner's own decisions also widen scope: D18 and D173 place ACM, OpenShift GitOps and GitLab in M4. The fix is therefore classification by the owner, not removal. The verified recommendation is to split "Product scope" into two labelled lists: a committed portfolio whose entries each link the milestone that delivers it, and a design envelope that general code must not preclude and no milestone gates on until promoted [F01-01]. Moving M1f or M3f out, or adding a portfolio/family tag per submilestone, supersedes D158's "All M1 items still gate M1's eventual completion". That is an owner decision, not a defect fix [F17-02].

A refuted candidate is worth noting. The claim that OKD appears in family scope while admission refuses it was considered and refuted (F01-09 in the refuted list); a refusal of a family member is the intended pattern.

### 3.4 Immediate portfolio: end-to-end status

The status column uses three values. Realized means the arm is specified and planned, with delivery or operator gates possibly still open. Refused means planning refuses it before registration. Not yet met means the spec carries that marker for part of the path.

| Immediate portfolio item | Where specified | Current status | Gaps |
| --- | --- | --- | --- |
| libvirt with sushy-tools BMCs | `specs/api/machines.md:73-101`; `specs/substrates.md:19-100, 320-412` | Realized; M1c virtual baseline accepted; X55/X60 operator gates open | none in scope |
| Bare metal via vendor BMC (xFusion first) | `specs/api/machines.md:40-71`; `specs/substrates.md:526-563, 895-913` | Realized in contract; M1e physical qualification open | [R2-02-04], [R2-02-05] |
| OpenShift Virtualization machines | `specs/substrates.md:639-698`; `specs/deferred/machine-arms.md` | Refused before registration (`substrates.md:50-51`), Not yet met, B87 | [R2-02-03], [F18-07] |
| IBM Storage Ceph | `specs/storage.md`, `storage-resources.md`, `storage-coverage.md` | Realized on libvirt (M2b done); M2c-M2e in progress | [F17-01], [R2-02-01] |
| IBM Fusion Data Foundation add-on | `specs/api/addons.md:354-390`; `specs/add-ons.md:121-379`; `storage-resources.md:1124-1255` | Specified end to end; M4d in-tree, operator exit B761 pending | [R2-02-01] |
| NTP and proxy, managed or external | `specs/api/infrastructure-services.md`; `specs/infrastructure-services.md:465-561` | Realized; M1d shared services delivered | [R2-02-07] |
| Pre-existing networking (provided) | no single owner; per-arm rules in `substrates.md:126-165` | Partly managed (libvirt networks, firewalld units); boundary unstated | [F01-06] |

Two adjacent family items leak into the portfolio. A managed Registry and LoadBalancer are refused by the engine (`specs/state-reconciliation.md:939`), but their API page shows supported-looking defaults with no marker [R2-02-06].

The strongest portfolio contract is the physical bare-metal arm. It proves one exact ComputerSystem and the full MAC set, pins identity, and repeats the proof before media insertion and before every power request. KubeVirt is weaker. It lacks a lost-reply rule, a reservation key and defined label values [R2-02-03]. Its quiescence depends on `machine stop`, which is hard-wired to Redfish [F18-07]. The only High-severity defect on the portfolio path is unsafe. A context with virtual Ceph and a physical cluster consuming it through FDF can destroy the backing storage of a cluster that keeps running [R2-02-01].

### 3.5 Is the day-0 and GitOps-handoff boundary coherent?

It is coherent as written but terminal. Ownership never transfers: "Handoff does not transfer, adopt or reclaim ownership" (`specs/project.md:49-50`). GitOps must exclude every owned object, and changed input over a completed apply refuses until destroy (`specs/state-reconciliation.md:27-29`). The day-2 surface stays parked as B106. Bootwright-owned Subscriptions with Manual approval, MetalLB pools, Data Foundation objects and Ceph pools therefore have no sanctioned owner after handoff [F01-02].

That is a defensible stance, but the specs do not say it. The verified fix keeps the stance and makes it explicit. Define "downstream day-2 resources", state that owned objects are frozen after handoff and change only by destroy-and-apply under D153, and state that upgrades and InstallPlan approvals of owned Subscriptions are out of scope until B106 [F01-02]. A relinquish verb would amend the ownership sentence and needs an owner decision.

Two smaller incoherences remain:

- The day-2 non-goal contradicts owner-requested utilities: `machine start|stop|restart`, `machine exec` over lists, and `diff --observe` (D150, D153). Name them as admitted utilities that register no operation, publish no ownership and never repair drift [F01-03].
- The early per-cluster handoff reads as settled in `specs/project.md:50-53` but is pending Q60 elsewhere. Record one status on every page [F01-04].

The "State reconciliation" context name also contradicts "There is no reconciliation" (`specs/state-reconciliation.md:25`); section 4 owns that rename [F01-10].

### 3.6 Non-goals the specs should add

Gather all non-goals into one list in `specs/project.md`. Today they are split across 98-102 and 104-105 and mixed with priorities. Add these:

| Proposed non-goal or boundary | Source |
| --- | --- |
| Physical fabric (switches, VLAN trunking, routing), external bridges and the host firewall are provided and verified, never changed; Bootwright owns only managed provider-host virtual networks and host network configuration it installs | [F01-06] |
| Bootwright-owned objects are frozen after handoff; GitOps controls only resources absent from owned-object evidence; a change to an owned object means destroy-and-apply | [F01-02] |
| Day-2 lifecycle is excluded except named operator utilities that register no operation and never repair drift | [F01-03] |
| No vendor support status, release catalog or compatibility rule; a coverage row means "Bootwright realizes this", not "the vendor supports this" (D157, D159, D163) | [F17-01] |
| No operator-authored code runs inside a lifecycle operation until a typed `CustomPlaybook` replacement exists (B102) | [F01-03] |
| Envelope entries gate no milestone until the owner promotes them | [F01-01] |

Add the missing positive scope line as well: Bootwright-installed machine operating systems with installer media and entitlement use, first RHEL through Anaconda, stated without versions [F01-05]. Add one README line linking project.md's scope and non-goals [F01-11].

A draft of the split scope section, for the owner to classify:

```markdown
## Product scope

Committed portfolio (each entry links the milestone that delivers it):
- libvirt with sushy-tools BMCs: M1c
- bare metal through vendor BMCs, first xFusion: M1e
- OpenShift Virtualization machines: M1g, M3e
- Bootwright-installed operating systems, first RHEL: M1
- managed or external NTP and proxy: M1d
- OpenShift clusters: M3
- IBM Storage Ceph: M2
- IBM Fusion Data Foundation add-on: M4d

Design envelope (general code must not preclude; gates no milestone until promoted):
- other substrates, cluster distributions, storage distributions
  and infrastructure-service kinds, including those named in owner decisions.
```

Where the owner has already promoted an envelope item by decision (D18, D173 for ACM, GitOps and GitLab; D158 for vSphere in M1), the committed list should name it with that decision.

## 4. Concept model and nomenclature

This section answers brief question 2. It judges whether each core concept is defined once, spelled one way across API kinds, CLI nouns, stage tokens, Go packages and Ansible roles, and free of overloads. It then proposes a normative vocabulary, a rename table, naming rules and a CLI alignment table.

The standard is the one a mature declarative platform meets. Kubernetes API conventions are the reference: every core term has one definition on one owner page. One word never names two concepts in operator-facing or safety-relevant text. One concept has one spelling at every layer. A written rule lets an author name a new kind or field without hunting for a precedent.

### 4.1 Verdict

The grammar layer is above the norm for a `v1alpha1` API. The vocabulary layer above it is not governed. The defects are cheap to fix now, because the pre-1.0 rule removes the compatibility cost of renaming.

#### What is good and should be kept

| Practice | Evidence | Why it is right |
| --- | --- | --- |
| Reference suffix rule with an enumerated exception list | specs/api.md:197-215 | One spelling for every object reference, and exceptions are named rather than tolerated |
| Reserved words: `type` only discriminates, `management: managed\|external` is the one who-runs-it axis, `direct: {}` replaces `none` | specs/api.md:253-258 | Removes three common sources of field ambiguity |
| Retired spellings are refused, never aliased | specs/api.md:31-37 | Keeps one live name per concept and makes renames honest |
| Kind names are nouns grouped by prefix (`Machine*`, `Storage*`, `ClusterAddon*`) with a fixed canonical order | api/v1alpha1/object.go:12-49 | Sorting, documentation and diagnostics follow one order |
| The glossary already separates the three meanings of "context" | specs/index.md:45-47 | It fixed the worst historical trap |
| The context table names the CLI noun beside each bounded context | specs/architecture.md:46-47 | Links the operator surface to the owning domain |
| Package names under `internal/` map almost one-to-one to bounded contexts | `internal/substrate`, `internal/machine`, `internal/storage`, `internal/addons` | An agent finds the code owner from the concept name |
| Block states and effects are crisp and closed | specs/state-reconciliation.md:742-756 | The core state vocabulary has no synonyms |

The D70 precedent matters for this section. The owner renamed `address` to `contact` and `ips` to `addresses` in two JSON outputs "before any consumer exists" (specs/milestones/backlog.md:3124). D142 then removed backward compatibility before 1.x (specs/milestones/backlog.md:3295). Together they make every rename below a single coordinated edit with no alias, converter or migration.

#### What needs improvement

The glossary defines six entries (specs/index.md:43-60), while the corpus relies on undefined, overloaded words [F02-07]. Binding, profile, capability, evidence and receipt each carry two to four meanings; receipt alone names the four-line lifecycle receipt and the setup receipt. "Capability" names a Machine eligibility token, an add-on ordering token and a Go lifecycle module [F02-12]. "Implementation" names a union arm, the managed-service product field and the frozen registry identity [F02-02]. "Add-on" and "package" are defined differently on two pages, and "driver" lives only in the deferred add-on design [F02-06]. The shared-service layer has several spellings: "infrastructure services", "infrastructure components", the `infra-components` stage token and `preflight infra` [F02-05]. The plan's unit is a "block" in the contract but a "step" in plan output, and the JSON result carries an undefined `steps` member beside `blocks` [F02-03]. "Drift" carries three consequences on three pages without one definition [F12-05]. The state-reconciliation context is named after the behavior its own contract disclaims [F01-10]. Enum casing and the `Ref` suffix have no written rule, and the outliers show it [F02-09].

#### What is bad

Two defects reach operator-visible or safety-relevant text.

- **Bare "controller" names both the Bootwright host and a BMC** [F02-01]. The glossary reserves bare "controller" for the host (specs/index.md:48-49). Yet specs/substrates.md uses bare "controller" 88 times and "management controller" 7 times. specs/api/machines.md:65-71 uses one word for both ends of the controller-to-BMC TLS leg. A reader cannot tell which party a trust bundle, verification check or remedy applies to.
- **The kind count is stated three ways** [F02-04]. specs/api.md:183 and :262 say 33. specs/api/environment.md:240-241 says the `spec.defaults` key set comes from a "27-kind catalog". The code registers 34 (api/v1alpha1/object.go:12-47), and specs/api/storage.md omits `StorageReplication`. The closed key set of kind defaults is therefore ambiguous in the contract.

Three API design findings also have a naming face. Neutral `Storage*` kind names cover Ceph-only semantics [F05-03]. `ContainerCluster` is OpenShift-shaped under a neutral name [F18-02]. `Machine.capabilities` mixes host prerequisites with product-named cluster roles [F04-02]. Section 5 owns their shapes; this section owns their names.

#### Findings in this section

| Id | Severity | Subject |
| --- | --- | --- |
| [F02-01] | Medium | Bare "controller" for host and BMC |
| [F02-04] | Medium | Kind count 33, 27 and 34 |
| [F01-10] | Low | "State reconciliation" disclaims reconciliation |
| [F02-02] | Low | "Implementation" has three meanings |
| [F02-03] | Low | Block versus step in plan output |
| [F02-05] | Low | Five spellings of the service layer |
| [F02-06] | Low | Add-on, package and driver |
| [F02-07] | Low | Glossary omits overloaded terms |
| [F02-09] | Low | No rule for enum casing and `Ref` |
| [F02-12] | Low | "Capability" has three meanings |
| [F05-05] | Low | `StorageExport` is not an export |
| [F12-05] | Low | Equal, settled and drift |
| [F16-07] | Low | CLI selector flags differ per noun |
| [F16-08] | Low | Three vocabularies for "next" |

### 4.2 Normative vocabulary

The table is the proposed norm. Each term has one meaning and one owner page; "forbids" lists the spellings that should disappear from contract text. Verification of [F02-07] judged that the always-loaded glossary in specs/index.md should stay short. The recommended split is therefore: the overloaded terms (binding, profile, capability, evidence, receipt, controller, implementation) enter specs/index.md#glossary with their qualified forms and owner links, about ten lines. The full table below lives in the audit and may later move to a non-index page.

| Term | Definition | Owner page | Replaces or forbids |
| --- | --- | --- | --- |
| Environment | The one `Environment` object that selects the complete desired state of one lifecycle unit | specs/api/environment.md | "env file" for the object |
| Environment directory | The directory holding the selecting Environment file; paths resolve against it | specs/api.md#environment-directory-and-selected-state | "input root" |
| Context | A named, durable, root-owned workspace holding frozen input, secrets and lifecycle state | specs/contexts.md | Go `context.Context`; bounded context |
| Controller | The host that runs Bootwright; in desired state, the Machine that `spec.controller.machineRef` names | specs/controller.md | bare "controller" for a BMC [F02-01] |
| Management controller (BMC) | A Machine's out-of-band Redfish endpoint, physical or emulated; written "BMC" or "management controller" | specs/substrates.md | bare "controller" |
| Operation | One registered apply or destroy over a frozen plan | specs/state-reconciliation.md | "run", "job" |
| Plan | The immutable dependency DAG of blocks that an operation freezes | specs/state-reconciliation.md#plan-and-execution | "step list" |
| Block | One plan node with identity, stage, dependencies, implementation and request | specs/state-reconciliation.md#plan-and-execution | "step" for a plan node [F02-03] |
| Step | A unit of work inside one block, or one add-on `spec.steps` entry; never a plan node | specs/cli/output.md | "step" for a block |
| Attempt | One execution of a block's effect, with its own record | specs/state-reconciliation.md#attempt-outcomes | "retry" as a noun for the record |
| Resolution | A read-only observation that settles an unproved block | specs/state-reconciliation.md | "resolve" as a verb the CLI lacks [F16-08] |
| Stage | The kind of platform work a block performs; a gate for `--stage`, not an order | specs/state-reconciliation.md#stages-and-the-pause-boundary | "phase" for this meaning |
| Wave | One past the deepest dependency; a freezing and presentation order, never a barrier | specs/state-reconciliation.md:749-755 | "batch", "tier" |
| Slice | A delivery unit of a milestone; never used in product contract sentences | specs/milestones.md | product use of "slice" |
| Substrate | A kind of infrastructure that realizes Machines (`baremetal`, `libvirt`, `vsphere`, `kubevirt`) | specs/substrates.md | "infra" for substrates [F02-05] |
| Provider | One declared `InfraProvider` object of one substrate | specs/api/machines.md | "substrate" for the object |
| InfraProvider arm | The presence-union member of `InfraProvider.spec` that names its substrate | specs/api.md#unions | "implementation arm" [F02-02] |
| Provider host | The Machine a hosted provider runs on | specs/substrates.md#provider-host-realization | "hypervisor host" as a term |
| Machine | One host Bootwright knows, physical or virtual, provided or installed | specs/api/machines.md | "node" outside a cluster topology |
| Realized target | What a substrate freezes for a Machine: arm, BMC, identity channel, physicality, realizing block | specs/substrates.md#selection-and-refusal | "target" unqualified |
| Reservation | A host-wide claim that refuses a conflicting context | specs/infrastructure-services.md#host-reservations | "lock" for this meaning |
| Infrastructure service | A shared service kind (`Proxy`, `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, `LoadBalancer`), managed or external | specs/infrastructure-services.md | "infrastructure component", `infra-components` [F02-05] |
| Implementation | The registry entry a block freezes: versioned identity, content digest, adapter entry point | specs/architecture.md#implementations-and-version-variation | union arm; product choice [F02-02] |
| Arm | One member of a presence or discriminated union in the API | specs/api.md#unions | "implementation arm" |
| Variant | The domain alternative an arm or discriminator selects; API meaning only | specs/api.md#unions | "variant" for specialization alternatives |
| Version specialization | A registry record selecting one code alternative ("case") at a declared seam from artifact facts | specs/architecture.md#implementations-and-version-variation | "variant" for a case |
| Adapter | The Ansible side of a block: a fixed playbook and role under the adapter result protocol | specs/architecture.md | "driver" in normative pages [F02-06] |
| Driver | Deferred add-on term only; normative pages link to deferred/add-ons-design.md or say "adapter" | specs/deferred/add-ons-design.md:19 | undefined use in specs/security.md:625 |
| Add-on | What a `ClusterAddonBinding` attaches to a ContainerCluster: a package or an authored add-on | specs/add-ons.md | conflicting definitions [F02-06] |
| Package | A built-in add-on compiled into the executable, selected under `spec.packages` | specs/add-ons.md | unqualified OLM, RPM or Go "package" |
| Authored add-on | A `ClusterAddon` object | specs/api/addons.md | "add-on package" |
| Profile | Always qualified: machine profile, install profile (`MachineInstallProfile`), add-on profile (`ClusterAddonProfile`), acceptance profile | owning pages | bare "profile" [F02-07] |
| Binding | Always qualified: add-on binding, Secret binding, host binding, interface binding | specs/secrets.md and owning pages | bare "binding"; the merged "Runtime and binding" entry |
| Export | An NFS export only (`StorageNFSExport`); disclosure in the CLI is "reveal" | specs/storage-resources.md | `StorageExport` [F05-05] |
| Custody | Who holds confidential or produced material, and under which rules | specs/secrets.md; specs/state-reconciliation.md | "ownership" for material |
| Evidence | Always qualified: context mutation evidence, block evidence, completion evidence | specs/state-reconciliation.md | bare "evidence" [F02-07] |
| Receipt | Always qualified: lifecycle receipt (four final lines), setup receipt | specs/cli.md:575-580; specs/controller.md:166 | bare "receipt" |
| Refusal | A diagnostic that stops a verb before or instead of an effect, naming object, reason and next action | specs/cli/output.md:191-192 | "error" for a deliberate refusal |
| Capability | Always qualified: Machine capability, add-on capability token, lifecycle capability | specs/api/machines.md; specs/api/addons.md; specs/architecture.md | bare "capability" [F02-12] |
| Drift | An owned object's observed real side differs from its recorded applied side; `diff --observe` reports it | specs/state-reconciliation.md | undefined use on storage pages [F12-05] |
| Settled | A verb's records are complete over unchanged input; no host is read | specs/state-reconciliation.md | "already realized" [F12-05] |

### 4.3 Rename table

Every row cites the finding that judged the current name misleading. "Before 1.0 only" marks renames that the version-authority rule (specs/api.md:31-37) and D142 make cheap now and costly after 1.0. Impact columns: API (schema, examples, goldens), CLI (commands, flags, help goldens), Spec (prose), Code (Go identifiers, roles, block ids). Rows marked "keep" record a name the findings questioned where verification judged that a definition is cheaper than a rename.

#### Kinds and fields

| Current | Proposed | Why | Impact | Findings |
| --- | --- | --- | --- | --- |
| `StorageExport`, `fdf.storageExportRef`, `storageExportAttachment`, block `ceph-export-` | `StorageAttachment` (or `StorageConsumer`) and matching field names; before 1.0 only | It is "the Ceph side of one Data Foundation consumer" (specs/storage-resources.md:1126), not an export, and collides with `StorageNFSExport` | API, Spec, Code | [F05-05] |
| `Storage*` children (`StoragePool`, `StorageClient`, `StoragePlacementPolicy`, `StorageRBDImage` and the rest) | Keep the names; state on specs/api/storage.md that they are the Ceph arm's resource kinds, valid only under a `ceph` StorageCluster. Option, before 1.0 only: `Ceph*`, which is an owner decision revisiting D133 | The neutral prefix promises neutrality that CephX, CRUSH and RBD semantics do not give | Spec now; API and Code only if renamed | [F05-03], [F06-05] |
| `StorageCluster` | Keep | It is the right neutral envelope; its union shape is section 5's question | none | [F03-01] |
| `InfraProvider` | Keep; add glossary entries for substrate, provider and infrastructure service. `SubstrateProvider` is optional and not recommended | "Infra" also prefixes the service layer, but a rename touches every example for little gain | Spec | [F02-05] |
| "implementation arm" (InfraProvider, ClusterAddon) | "arm" (or "substrate arm", "add-on arm") | specs/api/machines.md:20-21 says the arm is "not an adapter selection" | Spec | [F02-02] |
| Managed-service `spec.implementation` (squid, dnsmasq, chrony) | Keep the field; verification dropped the `spec.software` rename. The glossary reserves "implementation" for the registry entry | Rename churn exceeds the demonstrated harm | none | [F02-02], [F03-01] |
| `ClusterAddon` | Keep; define it as "authored add-on" | Name is accurate; the defect is the page definitions | Spec | [F02-06], [F05-04] |
| `ClusterAddonProfile` | Keep; always written "add-on profile" in prose | "Profile" has four meanings | Spec | [F02-07] |
| `ClusterAddonBinding` | Keep; always written "add-on binding" | "Binding" has four meanings | Spec | [F02-07] |
| `Machine.spec.capabilities` | Drop the role values `openshift-node`, `ceph-node`, `ceph-arbiter`; derive roles from topology membership. Rename the remainder `hostPrerequisites` if any value survives; before 1.0 only | It mixes host prerequisites with product-named roles, and "capability" already has three meanings | API, Spec, Code | [F04-02], [F02-12] |
| ContainerCluster node `role: master` and endpoint `source.type: openshift` | `control-plane` (projected to the installer's native `master`) and `installer`, if the owner chooses a distribution-neutral envelope; before 1.0 only | Product words in a neutral kind | API, Spec, Code | [F18-02] |
| `bootstrap.credentials` (gitops package) | `bootstrap.credentialsRef`; before 1.0 only | Names a Secret but breaks the `Ref` suffix rule | API, Spec | [F02-09] |
| StorageNFSExport `serviceID` | Keep and list it as a native-registry ID, or rename to `serviceRef` with the full `nfs.<id>` value | A bare rename would give `serviceRef` two value forms | API, Spec | [F03-07] |
| Ceph `bootstrap.node` | `bootstrap.nodeRef` (optional); document that it resolves to a topology node | Nested reference without the suffix | API, Spec | [F03-07] |
| Lowercase-camel enum outliers or kebab outliers (`redfishVirtualMedia`, `loadBalancer`, `logicalOverwrite` versus `container-runtime`, `read-only`) | One casing per vocabulary, chosen by the owner by smaller rename count; before 1.0 only | Operators must memorize casing per field | API, Spec, Code | [F02-09] |
| Add-on capability tokens `dataFoundation`, `certManager` | One casing with the rest of the token set (optional) | `metallb`, `nmstate`, `kubevirt` are lowercase | API, Code | [F02-12], [F02-09] |

#### Stage tokens, contexts and output words

| Current | Proposed | Why | Impact | Findings |
| --- | --- | --- | --- | --- |
| Stage `infra-components` | `infrastructure-services` (or `services`); before 1.0 only | One layer, five spellings | CLI, Spec, Code (plan goldens, completion) | [F02-05] |
| "infrastructure components" (milestones) | "infrastructure services" | Same layer | Spec | [F02-05] |
| "State reconciliation" (context row and spec H1) | "Lifecycle operations and state"; keep the file path; add `diff` to the verb list | The contract says "There is no reconciliation" (specs/state-reconciliation.md:25) | Spec | [F01-10] |
| Plan output "steps" and JSON `steps` | Define `steps` (frozen plan nodes) and `blocks` (per-block state) in a member table; renaming presentation "step" to "block" is optional | One unit, two names in one output line | CLI, Spec (goldens if renamed) | [F02-03] |
| Receipt `next:` tokens (`continue-apply`, `resolve`) | Keep the tokens; state beside the receipt that they are action tokens and map each to its command | `bootwright resolve` does not exist | Spec | [F16-08] |
| Bare "controller" for a BMC | "BMC" or "management controller", including refusal and diagnostic text and their goldens | Safety-relevant ambiguity | Spec, Code (messages) | [F02-01] |
| Literal kind counts (33, 27) | "every kind in the catalog", linked; add the `StorageReplication` row to specs/api/storage.md | Three counts, none correct | Spec | [F02-04] |
| Bare "driver" in specs/security.md:625 and specs/architecture.md:56 | Link to the deferred definition or say "adapter" | Undefined in normative pages | Spec | [F02-06] |
| Role and plugin names (`substrate_machine_protocol`, `managedos_install_anaconda`) | `<domain>_<implementation>_<capability>` for new names; rename `substrate_machine_protocol` to `substrate_libvirt_machine_protocol` | Three naming schemes coexist | Code | [F07-08] |
| `machine_power_redfish` in the generic power path | An arm-neutral name such as `machine_power` | The generic flow names one protocol | Code | [F18-07] |

### 4.4 Naming rules for future names

These rules belong in specs/api.md "Collections, enablement, and reserved words" and, for the CLI rules, in specs/cli/commands.md. Each rule is checkable by docs-check or a schema test, as [F02-09] and [F16-07] ask.

#### Kinds

1. A kind name is a PascalCase noun that says what one object is. Abbreviations are limited to established acronyms (DNS, NTP, RBD, NFS, SMB).
2. A neutral family prefix (`Storage`, `Container`) is used only when the kind's fields are neutral. A provider-shaped kind either carries the provider in its name or sits under a provider arm of a neutral kind [F05-03], [F18-02].
3. A new kind enters the catalog table of specs/api.md in the same change. No page states a literal kind count [F02-04].

#### Fields and references

4. Fields and union arms are lowerCamel.
5. A field whose value names a Bootwright object ends in `Ref`, and a list in `Refs`. A field holding a native identifier never ends in `Ref`; its row says "native ID" [F02-09], [F03-07].
6. `type` only discriminates a domain variant without its own configuration block. An implementation choice with configuration is a presence arm. Never an enum whose value equals a sibling arm's name [F03-01].
7. `management: managed|external` is the only who-runs-it axis. `enabled` appears only where omission differs from `false` (already specs/api.md:248-258).

#### Enum values and tokens: one casing per vocabulary

| Vocabulary | Casing | Status |
| --- | --- | --- |
| API field names and arms | lowerCamel | Already the rule in practice |
| Bootwright-owned API enum values | One casing, chosen by the owner by smaller rename count; verification noted lowerCamel likely wins [F02-09] | Decision needed |
| Native values mirrored from a product API | The native spelling, marked "native value" in the field row | Exempt from the rule |
| Add-on capability tokens | One casing for the defined set | Optional [F02-12] |
| CLI commands, flags, stage tokens, `--authorize` tokens, receipt tokens | kebab-case | Already the rule in practice |
| Diagnostic codes | dotted namespace with kebab segments (`preflight.unknown`) | Already the rule in practice |
| Go identifiers, Ansible roles | Go and Ansible conventions; roles `<domain>_<implementation>_<capability>` [F07-08] | Spec correction needed |

#### Terms

8. One concept has one spelling across kind, field, stage token, CLI noun, Go package and role prefix. A new term enters the glossary with its owner link in the same change.
9. Reserved single-meaning words: controller (host), BMC, block, stage, wave, attempt, implementation, arm, variant, package. Overloaded words (binding, profile, capability, evidence, receipt) appear only in qualified form where senses meet [F02-07].
10. Delivery vocabulary (slice, item, milestone) never appears in product contract sentences.

#### CLI nouns and verbs

11. A CLI noun is singular and names a kind, a domain or a durable concept (`machine`, `cluster`, `secret`, `context`, `media`).
12. `--name <list>` selects instances of the command's own noun. A plural `--<noun>s <list>` filters by a related noun. One names-only flag, `--short`, serves every command [F16-07].
13. Lifecycle verbs (`plan`, `apply`, `destroy`, `diff`, `status`) stay nounless and whole-context. A token that is not a command is never presented in a field read as a command [F16-08].

### 4.5 CLI noun and verb alignment

The catalog in specs/cli/commands.md:44-128 is mostly aligned with the kinds. The table lists every noun and the change, if any, that the findings support.

| CLI noun or verb | Maps to | Alignment | Proposed change | Findings |
| --- | --- | --- | --- | --- |
| `context` | Workspace (Go package `workspace`) | Good; glossary separates meanings | none | none |
| `setup`, `preflight controller` | Controller | Good | none | none |
| `secret` | `Secret` kind | Good | none | none |
| `media` | Host-wide media store, no kind | Good; a durable concept | none | none |
| `machine` | `Machine` | Selectors differ: `trust --machines`, `list --silent` | `machine trust --name`, keeping its empty-list rule; `machine list --short`; before 1.0 only | [F16-07] |
| `cluster` | `ContainerCluster` and `StorageCluster` | Acceptable: `cluster list` shows both kinds by name | none | none |
| `cluster keyring --client` | `StorageClient` | Acceptable | none | none |
| `add-ons list` | Built-in packages, not `ClusterAddon` objects | Misleading: the listing shows packages; plural noun | Title the result "packages", or `add-ons packages`; singular `add-on` optional | [F02-06], [F16-07] |
| `preflight infra` | Undefined scope | Misleading: "infra" means services and substrates | State the kinds it checks, or split into `preflight services` and `preflight substrates` | [F02-05] |
| `--stage infra-components` | Infrastructure services | One of five spellings | `infrastructure-services`; before 1.0 only | [F02-05] |
| `plan`, `apply`, `destroy`, `status` | Lifecycle operations | Good; nounless and whole-context | none | [F01-10] (context name only) |
| `diff` | Desired-versus-realized comparison | Good; missing from the context's verb list | Add to specs/architecture.md:58 | [F01-10] |
| receipt `next: resolve` | `bootwright apply` | Misleading: names a verb that does not exist | Map tokens to commands beside the receipt | [F16-08] |
| `render`, `render installer`, `render storage`, `preflight all` | Artifacts and readiness | Stubs with full flag contracts | Availability marker in the catalog | [F16-01] |

### 4.6 Order of work

1. Sweep bare "controller" in safety-relevant pages and messages, and fix the kind count and the `StorageReplication` row [F02-01], [F02-04]. These are the two Medium defects.
2. Add the ten-line glossary block for the overloaded terms and the four arm, implementation, variant and specialization entries [F02-07], [F02-02], [F02-12].
3. Write the naming rules into specs/api.md and specs/cli/commands.md with their docs-check or schema tests [F02-09], [F16-07].
4. Apply the before-1.0 renames in one coordinated change per surface: `StorageExport`, `bootstrap.credentialsRef`, the stage token, the two CLI flags and the chosen enum casing [F05-05], [F02-09], [F02-05], [F16-07].
5. Take the owner decisions that change kind shapes: `Storage*` statement or `Ceph*` rename, the ContainerCluster envelope, and `Machine.capabilities` [F05-03], [F18-02], [F04-02].

### Unverified observations

- Not verified: specs/architecture.md:47 labels the Controller context "CLI noun `controller`", but specs/cli/commands.md lists no `bootwright controller` command, only `setup` and `preflight controller`.

## 5. Desired-state API: grammar, kinds and recommended shapes

This section judges the authoring language an operator writes: the common grammar owned by `specs/api.md`, the kind pages under `specs/api/`, and the tracked examples that teach them. The standard is the Kubernetes API conventions (envelope, `*Ref` naming, discriminated unions, omission semantics, `kubectl explain` and OpenAPI discoverability). It is also measured against the authoring experience of Cluster API, Crossplane, Metal3, Rook and Terraform providers. Bootwright is also measured against its own stated goal. Shared intent should be provider-neutral, provider mechanics should sit in a named arm, and adding a substrate, cluster type or storage type should touch an arm and an adapter, not the general grammar.

No finding in this section is Blocking or High. The API is a strong base with a small number of structural choices that become expensive once more implementations exist. Pre-1.0, under D142 (`specs/milestones/backlog.md:3295`), now is the cheapest time to settle them.

### 5.1 Grammar verdict

#### What is better than mainstream and must be kept

| Property | Evidence | Why it is better |
| --- | --- | --- |
| Strict YAML decoding | `specs/api.md:55-59`, `:91-93`, `:99-110` | Aliases, anchors, merge keys, custom tags, duplicate keys and explicit `null` are refused; file and document order never choose a value. |
| YAML 1.1 traps closed | `specs/api.md:61-67` | Leading-zero integers and quoted booleans are type errors, so `0644` and `"true"` cannot change meaning. |
| Refusal contract | `specs/api.md:112-130` | Every refusal names object, path, expectation and next step, offers a bounded rename, and never echoes an authored scalar. |
| Effective state | `specs/api.md:348-357`, `:374-397` | One canonical, diffable rendering, kept distinct from authored input and never accepted back as input. |
| Provenance kept apart | `specs/api.md:328-329` | Source origin is retained for diagnostics but is never data, so it cannot leak into effective output. |
| Explicit omission where stated | `specs/api/secrets.md:27-34`; `specs/api/environment.md:278-293` | Omitted source, `source: {}` and `contextStore: {}` are distinct and preserved; an explicit empty value wins over inheritance. |
| Kind defaults | `specs/api/environment.md:238-307` | One closed, kind-keyed map with whole-value replacement and no arm introduction beats overlays and values files. |
| Who-runs-it axis | `specs/api.md:256-257`; `specs/api/infrastructure-services.md:11-16` | `management: managed|external` is one flat axis on every service, with no wrapper. |
| No implicit selection | `specs/api/infrastructure-services.md:179-185` | A sole service, a name `default` or declaration order never selects anything. |
| Native fields stay native | `specs/api.md:402-423` | Native documents pass through with JSON-pointer Secret bindings, and a native upgrade never reinterprets authored state. |
| Compiler boundary | `specs/api.md:21-29` | `validate` performs no I/O, network or effect, so admission is safe to run anywhere. |
| Secret descriptors | `specs/api/secrets.md:11-52`, `:104-134` | Declaration and material are separate; the type is explicit; generation is type-scoped and never fabricates bytes. |

The decoding layer and refusal contract are stricter than Kubernetes, Helm or Kustomize, which all let `null`, anchors or order leak into meaning. They should become the house standard for every future kind and should never regress.

#### What needs improvement

| Topic | Problem | Ids |
| --- | --- | --- |
| Unions | Four idioms express one question, "which implementation realizes this object", and the deciding criterion "meaningful domain variant" (`specs/api.md:219-221`) is not testable. | [F03-01] |
| Omission, empty, enablement | Empty lists mean invalid, clear or default depending on the field; enablement is spelled by presence, boolean and `enabled|disabled` string. | [F03-06] |
| References | Native-registry references, nested `addressRef`, endpoint selections and un-suffixed references (`cephadm.bootstrap.node`) are outside the catalogued classes. | [F03-07] |
| Single-valued fields | Fields with one legal value are required or reserved (`implementation`, `install.method`, `boot.method`, `bmcEmulationDefaults.enabled`). | [F03-04], [F04-06] |
| Discoverability | 34 kinds over eight pages, no `explain`, no schema export, and effective output hides value origin. | [F03-05] |
| Retired fields | "The former X is unknown" sentences recur per page instead of one retired-path table. | [F03-09] |
| Scoping | The source directory, `spec.resources` and the cluster lists all narrow scope. | [F03-08] |
| Name limits | Six or more per-kind name limits derive from internal block-identity prefixes. | [F04-11] |
| Diagnostic indexes | ClusterAddonBinding indexes refer to the sorted list, not the authored position. | [R2-05-09] |
| Reserved directory | A `secrets/` directory is skipped silently, so descriptors placed there surface as unresolved references. | [F03-02] |
| Kind count | The envelope and catalog say 33 kinds; `api/v1alpha1/object.go:13-49` registers 34. | [F02-04] |

#### What is bad

| Topic | Problem | Ids |
| --- | --- | --- |
| Repeat, then compare | Operators author a value that has one valid derivation, and admission checks that the copies agree: service bind addresses, certificate SANs, `mdsServiceRef`, SMB `cluster_id`. | [F03-03], [F05-07] |
| Truthfulness split | Some unrealized declarations refuse at `validate`, others pass and refuse at plan or installation; ContainerCluster admits inert fields that plan ignores silently. | [R2-05-01], [F05-06] |
| Neutral names, product shapes | ContainerCluster is OpenShift-shaped with no distribution arm; Storage child kinds are Ceph-only under neutral names; Machine `capabilities` carries `openshift-node` and `ceph-node`. | [F18-02], [F05-03], [F04-02] |
| Selection closure | The authoritative closure table omits four storage kinds and the StorageReplication peer, and silently drops standalone Machines once a cluster list is authored. | [R2-01-04], [R2-01-05] |
| References do not declare edges | No spec maps a reference field to a plan edge; the one explicit cluster-install list omits the managed Proxy that code waits for. | [R2-01-01] |

"Bad" here means a design choice that will multiply cost as implementations are added, not an unsafe rule. Every item above is Medium or Low.

Considered and refuted: the wildcard default of a managed service `bindAddress` is not a defect. It is a recorded owner decision (D114) with a parked item (B333) that binds the endpoint address by default. The recommendation below therefore schedules B333 rather than reopening D114.

#### Environment selection as part of the grammar

The selection closure is grammar, because it decides which objects exist for every later check. Three rules are missing. First, "every storage child whose `clusterRef` names it" should replace the enumerated list in `specs/api/environment.md:204` [R2-01-04]. Second, selecting either end of a StorageReplication should retain the relationship and both clusters as full roots [R2-01-04]. Third, Bootwright-installed Machines that are not nodes of an excluded cluster should be retained, or at least warned per object [R2-01-05]. The unresolved-reference rule at `specs/api/environment.md:215-217` should cover every retained referrer, not only always-retained objects [R2-01-03]. An attachment that retains a cluster root missing from an authored list should warn and name the list to extend [R2-01-06].

Each reference class should also state whether it creates a plan edge. One catalog row per reference field class (edge or not, stage pair, retention) removes the need to read capability code to learn the graph [R2-01-01]. The nested KubeVirt chain shows the cost when this is missing: the hub VMs depend on a storage class and networks that only external strings name [R2-01-02].

### 5.2 Per-kind verdicts

All 34 registered kinds, in `kindOrder` (`api/v1alpha1/object.go:49`). "Owner page" is the field-level owner under `specs/api/`.

| Kind | Owner page | Verdict | Main issue | Ids |
| --- | --- | --- | --- | --- |
| Environment | environment.md | needs-improvement | Closure table incomplete; three scoping mechanisms overlap | R2-01-04, R2-01-05, F03-08, R2-05-10 |
| Entitlement | secrets.md | good | Per-type arm set open; Satellite admitted, refused later | R2-05-05, R2-05-01, R2-05-11 |
| Machine | machines.md | needs-improvement | Substrate facts scattered; roles duplicated in `capabilities` | F04-01, F04-02, F04-04, F04-05 |
| MachineImage | machines.md | good | Thin kind; `local-media:` prefix carries no information | F03-04 |
| MachineInstallProfile | machines.md | good | Proxy bypass never derived for Machine-side choices | F04-08 |
| NetworkConfig | machines.md | good | Native NMState kept; documented subset only | none |
| InfraProvider | machines.md | needs-improvement | libvirt arm authors one-value mechanics; vSphere arm OpenShift-shaped | F04-06, F04-09, F18-06 |
| Proxy | infrastructure-services.md | good | Required `implementation` has one legal value | F04-06 |
| DNSServer | infrastructure-services.md | good | Bind address repeats the Machine address (B333) | F03-03 |
| NTPServer | infrastructure-services.md | good | Local-stratum fallback defined only in a template | R2-02-07, F03-03 |
| ArtifactServer | infrastructure-services.md | needs-improvement | External form admitted but no consumer accepts it | F04-07, F04-10 |
| Registry | infrastructure-services.md | needs-improvement | Managed form refused at plan without a marker | R2-02-06 |
| LoadBalancer | infrastructure-services.md | needs-improvement | External `bindAddresses` binds nothing; managed form refused | F04-07, R2-02-06 |
| ContainerCluster | container-clusters.md | bad | OpenShift-shaped under neutral name; inert fields accepted silently | F18-02, F05-06 |
| StorageCluster | storage.md | good | `type: ceph` duplicates the `ceph` arm | F03-01 |
| StoragePlacementPolicy | storage.md | needs-improvement | CRUSH rule under a neutral name | F05-03, F05-10 |
| StoragePool | storage.md | needs-improvement | Owning page omits `nativeName` and realized mirroring | F05-02, F05-03 |
| StorageRBDImage | storage.md | good | Ceph-only under a neutral name | F05-03 |
| StorageClient | storage.md | needs-improvement | CephX principal named neutrally; closure row omits it | F05-03, R2-01-04 |
| StorageFilesystem | storage.md | needs-improvement | `mdsServiceRef` admits exactly one derivable value | F05-07, F03-07 |
| StorageSubvolume | storage.md | good | Ceph-only under a neutral name | F05-03 |
| StorageObjectGateway | storage.md | good | Clear native ownership | F05-03 |
| StorageObjectUser | storage.md | good | Closure row omits it | R2-01-04 |
| StorageObjectBucket | storage.md | good | Closure row omits it | R2-01-04 |
| StorageNFSExport | storage.md | good | `serviceID` is an un-suffixed reference | F03-07 |
| StorageSMBResource | storage.md | needs-improvement | Native IDs repeat Bootwright references | F05-07 |
| StorageNVMeoFSubsystem | storage.md | good | Admitted and truthfully refused at plan | none |
| StorageReplication | storage.md | needs-improvement | Missing from owning page; peer not closed by selection | F05-02, R2-01-04 |
| StorageExport | storage.md | needs-improvement | Per-consumer FDF attachment named like an export | F05-05 |
| ClusterAddon | addons.md | bad | Second, refused add-on language; divergent approval default | F05-04 |
| ClusterAddonProfile | addons.md | bad | Same parallel language as ClusterAddon | F05-04 |
| ClusterAddonBinding | addons.md | good | Diagnostic indexes follow the sorted list | R2-05-09 |
| CustomPlaybook | custom-playbooks.md | needs-improvement | Validated reserved shape B102 replaces; stage-like anchors | F05-09, R2-01-07 |
| Secret | secrets.md | good | No types table; retired arm prose; `opaque` overloaded | R2-05-07, F03-02 |

The tally is 17 good, 14 needs-improvement and 3 bad. The good kinds share three traits: references only to first-class objects, a closed shape, and a refusal table that names the field to change. The bad kinds share one trait: a neutral-sounding surface whose content is a single product's vocabulary, or a second language that every binding refuses.

Three observations from the table deserve emphasis.

1. ContainerCluster is the one kind whose shape blocks the stated extension goal. A kubeadm, RKE2 or EKS cluster cannot be added as an arm; it needs a reshape or a new kind that touches every general-flow kind list [F18-02], [F18-04].
2. The Storage family is correctly granular. Fifteen kinds map one-to-one to native identities with their own create, observe and remove semantics. The defect is naming and the missing type-binding rule, not kind count [F05-03].
3. ClusterAddonBinding with typed `spec.packages.<name>` is the real add-on language and is a model diagnostic contract. ClusterAddon and ClusterAddonProfile should be marked reserved, as CustomPlaybook is, until B90 [F05-04].

### 5.3 Recommended shapes

Each shape below is minimal, provider-neutral in intent and provider-shaped in mechanics. Field names not shown keep their current spelling. Values use documentation ranges.

#### One union rule

Adopt one rule in `specs/api.md#unions` before the next implementation lands [F03-01]:

1. An implementation or engine choice is a presence union. The arm name is the implementation, and there is no `type` field.
2. `type` is an enum only for a domain variant that carries no implementation-specific block, such as Secret types or Entitlement products.
3. Never both an enum and a same-named arm.
4. An arm with no configuration may be omitted while exactly one implementation is registered. Its name then appears in effective output, and it becomes required when a second implementation registers.

Rule 4 answers the verifier note on [F04-06]: an implicit default is safe only while one choice exists, and effective output keeps it visible.

#### Machine: a per-substrate arm

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: metal-01
spec:
  substrate:
    providerRef: lab-metal
    baremetal:
      bmc:
        address: https://bmc-01.example.test/redfish/v1/Systems/1
        credentialsRef: metal-01-bmc
      nics:
        - name: eno1
          macAddress: 52:54:00:9a:1b:01
  hardware:
    rootDevice:
      wwn: "0x5000c500a1b2c3d4"
  os:
    bootwright:
      profileRef: rhel-9-8
      hostKeyRef: metal-01-host-key
  network:
    configRef: lab-metal
    addresses:
      - name: ip
        address: 198.51.100.11/24
        interface: eno1
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: rhel-01
spec:
  substrate:
    providerRef: lab-libvirt
    libvirt:
      profileRef: rhel
  hardware:
    rootDevice:
      deviceName: /dev/vda
  os:
    bootwright:
      profileRef: rhel-9-8
  network:
    configRef: lab-guests
    addresses:
      - name: ip
        address: 198.51.100.21/24
        interface: enp1s0
```

The substrate arm mirrors the InfraProvider arm, and admission requires them to match. BMC, MACs, `profileRef` and future `failureDomainRef` live only where they mean something, so a cloud substrate adds an arm instead of conditional rows across the Machine table [F04-01]. Per the verifier adjustment, the root-device hint stays outside the arm as `hardware.rootDevice`, because libvirt and installers both consume it [F04-05]. Virtual arms carry no NIC MACs, which gives every virtual substrate one MAC rule [F04-04]. The OS lifecycle becomes an explicit union, `provided: {}`, `bootwright: {...}` or `clusterInstaller: {}`, so forgetting `installProfileRef` can no longer change the lifecycle silently [F04-05]. Roles such as `openshift-node` and `ceph-node` disappear: membership in a cluster or storage topology is the role [F04-02].

#### InfraProvider: keep the arm, trim the mechanics

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-libvirt
spec:
  libvirt:
    hostRef: controller
    bmcEmulation:
      bindAddress: 127.0.0.1
      port: 8000
      certificateRef: lab-bmc-tls
      auth:
        credentialsRef: lab-bmc-credentials
    machineProfiles:
      - name: rhel
        cpu: 4
        memoryMiB: 8192
        diskGiB: 60
        tpm: {}
  networkAttachments:
    - name: lab-guests
      libvirt:
        bridge: virbr-lab
        management: managed
        address: 198.51.100.1/24
        forward: nat
```

InfraProvider already follows the union rule and should not change shape. `uri`, `enabled`, `protocol` and `emulator` leave the authored schema and appear only in effective output, and the required "Defaults" block gets a name that is not a default [F04-06]. Compare `examples/machines/lab-rhel/infra/providers/lab-libvirt.yaml:13-22`. The vSphere arm should hold realization facts and leave optional region and zone tags to B99's open questions [F18-06].

#### ContainerCluster: a distribution arm

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: compact
spec:
  nodes:
    - name: cp-0
      role: control-plane
      machineRef: compact-01
  endpoints:
    api:
      address: 198.51.100.10
      source:
        installer: {}
    ingress:
      address: 198.51.100.11
      source:
        installer: {}
  services:
    proxy:
      direct: {}
    ntp:
      - serverRef: lab-ntp
        endpointRef: ip
    trustBundleRefs:
      - rgw-issuer
  openshift:
    release:
      version: 4.21.15
    install:
      mode: connected
      agent:
        redfishVirtualMedia:
          artifactServerEndpoint:
            serverRef: lab-artifacts
            endpointRef: ip-https
    pullSecretRef: openshift-pull-secret
    security:
      fips:
        enabled: false
```

The envelope keeps intent every distribution shares: nodes with neutral roles, the API and ingress endpoints, and service selections. Everything OpenShift-specific moves under `openshift`: release, agent install, pull secret, FIPS, disk encryption, OVN settings, `api-int` and the `infra` role [F18-02]. A later `kubeadm:` or `rke2:` arm then adds one arm, one installer-input capability and refusal rows. `install.method: agent` folds into the presence of `install.agent` [F03-01]. The verifier asked that the owner decide between this envelope and "one kind per distribution family". Either answer is acceptable if recorded, but the envelope is cheaper for every general-flow kind list [F18-04]. Until then, the agent path should refuse authored non-default endpoint `port`, `scheme`, `prefixLength` and `interfaceNetworks` [F05-06].

#### StorageCluster and its child kinds

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-lab
spec:
  management: managed
  ceph:
    distribution: ibm
    release: 9.9.2.0
    entitlementRef: ibm-ceph
    cephadm:
      bootstrap:
        nodeRef: rhel-01
        addressRef: ip
---
apiVersion: bootwright.io/v1alpha1
kind: CephPool
metadata:
  name: rbd-data
spec:
  clusterRef: ceph-lab
  role: rbd
  replicated: {}
---
apiVersion: bootwright.io/v1alpha1
kind: CephFilesystem
metadata:
  name: files
spec:
  clusterRef: ceph-lab
  metadataPoolRef: cephfs-metadata
  dataPoolRefs:
    - cephfs-data
```

StorageCluster keeps the neutral name and the `management` axis, and drops `type: ceph` because the arm already discriminates [F03-01]. An external cluster also carries the `ceph` arm, holding only the access facts B107 will define, so an engine is always named. The children take the Ceph-scoped naming (`CephPool`, `CephClient`, `CephFilesystem`, `CephPlacementPolicy` and so on), since every one encodes CRUSH, CephX, MDS or RGW semantics [F05-03]. A NetApp or Pure cluster then adds its own arm and its own provider-named kinds, and no neutral `StoragePool` is stretched to cover an aggregate. If the owner prefers to keep the names, the minimum is one admission rule: a child's `clusterRef` must name a cluster with the `ceph` arm. `mdsServiceRef` is derived as `mds.<metadata.name>` [F05-07]. `bootstrap.node` becomes `bootstrap.nodeRef` [F03-07]. `StoragePool` follows rule 2: replicated and erasure become arms, because erasure carries configuration.

#### Fusion Data Foundation attachment

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageAttachment
metadata:
  name: compact-storage
spec:
  clusterRef: ceph-lab
  dataFoundation:
    rbdPoolRef: rbd-data
    filesystemRef: files
    objectGatewayRef: objects
---
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: compact-data-foundation
spec:
  clusterRef: compact
  packages:
    fdf:
      storageAttachmentRef: compact-storage
      subscription:
        channel: stable-4.20
        startingCSV: odf-operator.v4.20.0
        source: isf-data-foundation-catalog
```

The decomposition is right and must be kept: the storage side mints per-consumer principals, and the package only consumes produced material by reference. Only the name misleads, because "export" already means an NFS export and the object is one consumer of one cluster [F05-05]. Renaming the kind and the `fdf.storageExportRef` field, and dropping the redundant `type: dataFoundation`, aligns it with the union rule. Compare `examples/add-ons/lab-compact-df/storage/export.yaml:1-13`. Moving the external-cluster details to StorageCluster stays an option for when B107 is promoted.

#### Infrastructure services, managed or external

```yaml
apiVersion: bootwright.io/v1alpha1
kind: NTPServer
metadata:
  name: site-ntp
spec:
  management: external
  address: ntp.example.test
---
apiVersion: bootwright.io/v1alpha1
kind: Proxy
metadata:
  name: corp-proxy
spec:
  management: external
  connection:
    httpsProxy: http://proxy.example.test:3128
    trustBundleRef: corp-proxy-ca
---
apiVersion: bootwright.io/v1alpha1
kind: NTPServer
metadata:
  name: lab-ntp
spec:
  management: managed
  machineRef: controller
  chrony: {}
  endpoints:
    - name: ip
      addressRef: ip
  upstreamSources:
    - 192.0.2.123
---
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab-external-services
spec:
  defaults:
    MachineInstallProfile:
      proxy:
        proxyRef: corp-proxy
        noProxy:
          - .lab.example.test
      ntp:
        - serverRef: site-ntp
```

The flat `management` axis is kept unchanged. The external forms already have exactly the access fields the immediate portfolio needs (`specs/api/infrastructure-services.md:107-114`). A managed service names its implementation as an optional arm under rule 4 (`chrony: {}`). Implementation-neutral settings such as `upstreamSources` stay at the top level. The managed listener binds its endpoint addresses by default, which is B333; D142 removes the digest-migration reason for parking it [F03-03]. The managed NTPServer page should state the local-stratum fallback [R2-02-07]. The Environment block shows the missing teaching example: today external NTP, proxy and kind defaults appear only in the refused full-platform reference [R2-05-03]. External ArtifactServer should get a named validate refusal, and external LoadBalancer entries should be described as frontend addresses [F04-07]. Registry and LoadBalancer managed forms need a Not-yet-met line naming B68 and B100 [R2-02-06].

#### Secret descriptors

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: artifact-server-tls
spec:
  type: tlsCertificate
  source:
    generated:
      issuerRef: lab-ca
      commonName: controller.lab.example.test
      subjectFrom:
        consumers: true
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: openshift-pull-secret
spec:
  type: dockerConfigJson
```

The descriptor model is kept as is: explicit type, closed source union, omission selecting `contextStore`. The one change is derivation: a generated certificate takes its SANs from the endpoints of the services that consume it, instead of the operator restating the controller address (compare `examples/machines/lab-rhel/secret-descriptors/artifact-server-tls.yaml:9-19`) [F03-03]. Authored `dnsNames` and `ipAddresses` remain as an override. `specs/api/secrets.md` should hold one table of type, parts, `secret set` flags, generation support and typical consumers. The retired `file` arm then shrinks to one refusal row that links to it [R2-05-07]. The reserved `secrets/` directory should warn by name when it holds YAML [F03-02].

### 5.4 Omission, null, empty and enablement: rules to state once

`specs/api.md` should state these rules once, and every field table should name its class [F03-06]:

| Rule | Statement |
| --- | --- |
| Null | Explicit `null` is always `api.type`. Keep as is (`specs/api.md:91-93`). |
| Omission classes | Each optional field belongs to one class: absent selects a built-in default; absent inherits then defaults; absent leaves the native default; absent means off. |
| Empty classes | An explicit empty value either clears inherited values or is invalid; it never silently means the default. |
| Presence enablement | A feature with configuration is enabled by block presence (`tpm: {}`). |
| Booleans | A boolean is used only for a two-state fact with no configuration. A three-state field states what omission means. |
| Strings for state | No `enabled|disabled` strings unless the native vocabulary passes through verbatim. |
| Single value | A field enters `v1alpha1` only with two legal values or with information the operator must supply [F03-04]. |
| Derivation | A value with exactly one valid derivation is derived, not authored and compared [F03-03], [F05-07]. |
| Closed arm sets | A set-of-arms discriminator lists the permitted arms per type; any other arm refuses [R2-05-05]. |
| Truthfulness | A declaration no build realizes either refuses at admission or emits a deterministic `validate` warning naming its tracking item; one table in `specs/api.md` owns the list [R2-05-01]. |
| Diagnostic indexes | A path's list index is the authored position [R2-05-09]. |
| Names | One owning section states every kind's name limit [F04-11]. |

The Go `Field` should carry the omission class, so the same fact drives validation, `explain` and the docs-check [F03-05].

### 5.5 Schema discoverability and refusal ergonomics

The refusal contract is already better than mainstream. The gap is finding the right field before the refusal [F03-05]. In order of cost and value:

1. A docs-check test that compares each spec field table and kind table with the registered schema. It would have caught the StorageReplication and `nativeName` omissions [F05-02] and the 33-kind count [F02-04].
2. `render effective --origins`, annotating each leaf as authored, `defaults.<Kind>`, built-in or derived. It matters most where kind-default rule 6 suppresses or selects an arm.
3. A one-sentence `Doc` and the omission class on `Shape` and `Field`, then `bootwright explain <Kind>[.<path>]`.
4. `bootwright schema --format json-schema`, for editor completion through yaml-language-server.
5. One retired-path table in `specs/api.md`, backed by data in `api/v1alpha1`, that powers a tailored remedy for every removed field and replaces the per-page "former X is unknown" sentences [F03-09].
6. `validate` output listing retained and excluded roots, with a remedy that names the selection field to change [F03-08], [R2-01-05], [R2-01-06].
7. A reference-class table in `specs/api.md#references` covering native-registry and nested references [F03-07].

The CLI catalog should also mark unavailable commands, so `explain` and `schema` do not join the stub commands that look available [F16-01].

### 5.6 Migration cost under the pre-1.0 rule

D142 says "before 1.x no backward compatibility is wanted" (`specs/milestones/backlog.md:3295`), and `specs/api.md:33-37` keeps authorized changes in `v1alpha1` with no conversion. Every shape above is therefore a same-version edit. The cost is in-repository work, not operator migration tooling.

| Change | Touches | Relative cost |
| --- | --- | --- |
| Union rule, drop `type` beside arms, fold `install.method` | `specs/api.md`, three kind pages, schema, 31 scenarios and goldens | Low |
| Derivation rules (B333, SANs, `mdsServiceRef`, SMB IDs) | Normalization, examples, goldens | Low to medium |
| Machine substrate arm and OS lifecycle union | `specs/api/machines.md`, admission, every Machine example | Medium |
| Drop `capabilities` roles | Machine, cluster and storage admission; `internal/storage/topology.go` | Medium |
| ContainerCluster distribution arm | Container-cluster pages, admission, renderer input, add-on facts | High |
| Ceph-scoped child kinds, StorageAttachment | Kind registry, storage pages, examples, CLI labels | Medium to high |

Two costs reach beyond the repository. First, any change that moves a frozen request or block identity stops D142 continuation, so an affected live operation must be completed or destroyed with the build that started it. Batch the renames into one slice, and land them while few live contexts exist. Second, every rename leaves operators with unknown-field refusals. The retired-path table in 5.5 turns each of these into a refusal that names the replacement, which is the cheapest substitute for a converter the project has declined.

The order that minimises rework is: union rule and derivation rules first, since they shape every later arm. The ContainerCluster decision comes next, because it is the one that blocks a second distribution. Storage naming and the Machine arm follow, together with the discoverability tests that keep the pages and schema aligned.

## 6. Architecture: generic flow, specialization and code materialization

This section answers question 4 of the request. It judges whether the architecture guarantees one generic flow, with every product, vendor and substrate specific behind a port, an adapter or an allowlisted registry selected from the operator's frozen input. It then states the rules that would make that guarantee executable, maps the rules onto the code, and tests them against four extension scenarios and the immediate portfolio.

The standard is a ports-and-adapters platform of the kind that serves several vendors at once. Such a platform meets four conditions:

1. One engine runs admission, compilation, planning, execution, evidence and continuation without naming an implementation.
2. A consumer-owned port reaches every implementation, and a declared, deterministic function of validated input selects it.
3. A new implementation of an existing capability touches only its own package, its registry row, its composition binding, its automation and its tests.
4. An executable fitness test, not a reviewer's memory, holds each of these properties.

### 6.1 Verdict on the central requirement

**Verdict.** The engine boundary meets the standard. Above the engine, the guarantee holds only partly. The lifecycle engine never knows which implementation runs. It resolves capabilities by kind and implementation identity and freezes that identity, so continuation cannot drift. Below the engine, the guarantee breaks in three places. First, implementation selection is private code inside each capability [F06-01]. Second, substrate arms, Ceph field paths and implementation identities leak into consumers and into general-flow packages [F18-03, F18-04, F07-03]. Third, two bounded contexts are defined as their first product, so a second product has no home [F06-05, F18-02, F05-03]. Nothing mechanical holds the promise that "policy never branches on implementation" [F06-03]. The owner's requirement is therefore a stated intent. The architecture does not yet guarantee it.

#### What is right and should be kept

| Strength | Where it lives | Why it meets the standard |
| --- | --- | --- |
| One lifecycle port | `internal/reconciliation/lifecycle/contracts.go`; `specs/architecture.md:536-560` | Plan, apply, observe, quiescence, destroy and removal observation sit behind one `Capability` interface. |
| Dependencies as API objects | `specs/architecture.md:554-557`; `specs/state-reconciliation.md:781-787` | A capability never learns another capability's block identities. |
| Composition root as the only binder | `specs/architecture.md:336-342`; `TestApplicationImplementationsAreBoundAtComposition` | Constructors are bound in `cmd/bootwright` only. The test is exact and shrink-only. |
| Static, immutable resolver | `cmd/bootwright/wiring_reconciliation.go:94-122` | `Resolve(kind, implementation)` answers exactly one binding. No runtime plugin path exists. |
| Frozen implementation identity | `specs/architecture.md:952-959` | Each block freezes its identity and content digest. Continuation refuses another implementation. |
| Fixed Ansible entrypoints | `specs/architecture.md:739-748` | Go freezes variant dispatch, and authored data never selects code. |
| Uniform refusal mechanics | `Test<X>RefusalTableMatchesUnsupported` per capability | Each capability's refusals are held row by row against its spec table. |
| Version specializations | `specs/architecture.md:912-938`; `TestDocsVersionSpecializationCatalogMatchesRegistries` | One general flow per component. Facts come from frozen artifact bytes, and an unknown fact selects the general flow. |
| Secrets as the model seam | `specs/secrets.md:44-50` | An injected immutable catalog with exact resolution. "A second test implementation qualifies this extension seam." |
| Realized target | `internal/substrate/target.go` | Substrate derives controller, identity channel, interfaces and delivery once. Consumers read the frozen value. |
| Infrastructure-service axis | `management: managed` or `external` | An external DNS or NTP server needs no code at all. |
| Add-on built-in packages | `internal/addons/packages/` | Reviewed Go, a closed schema and a pure renderer. A CSI driver package is additive. |

The version-specialization model deserves separate praise. It keeps release differences inside one implementation, forbids seams in trust and credential code, keeps specialization identities out of content digests, and refuses version pairing rules (D162, D163). The knowledge catalog's "Decided general" table (`.agents/knowledge/version-specializations.md`) stops specialization creep. These rules should stay as written, with the three precision fixes in rule SC-10 below.

#### Where the guarantee fails

| Failure | Consequence for an extension | Ids |
| --- | --- | --- |
| Selection is a property, not a mechanism. Every capability plans over the whole state and claims its own objects. The engine checks only per kind for unclaimed objects. | A second implementation of an existing kind can claim the same object silently. The existing capability's filters must be edited. | [F06-01] |
| Consumers dispatch on the substrate arm. The managed-OS and cluster-install roles hold per-arm tables. Go consumers in Machine, Managed OS, Container cluster and Storage compare arm constants. | A new arm edits every consumer. This contradicts the B99 exit promise that "no consumer of a realized Machine changes". | [F18-03] |
| General-flow packages hard-code kind lists, implementation identities and Ceph field paths. Examples are environment selection, compilation, lifecycle support and CLI labels. | A new kind edits at least six general lists. A new storage type edits retention and membership code. | [F18-04, F07-03] |
| The "never branch on implementation" rule needs intent judgement and has no fitness test. | Leakage accumulates silently while import-direction tests pass. | [F06-03] |
| Container cluster and Storage contexts are their first product: "OpenShift and OKD release" and "Ceph cluster topology" (`specs/architecture.md:54-55`). | A kubeadm distribution or a NetApp array has no specified home. | [F06-05, F18-02, F05-03, F07-03] |
| Composition binds Ceph's retirement policy and bare-metal block identity into Managed OS. Neither is a named exception. | A second host-based storage or physical substrate edits these bindings. | [F06-07] |
| The engine's unclaimed-object refusal reads `management` and `os.provided` itself. | A new management mode needs an engine edit. | [F06-08] |
| Machine power is wired to the Redfish role, not to the substrate's `power` entry point. | KubeVirt stop is unreachable. A third arm needs a third power path. | [F18-07] |
| Managed services ignore `spec.implementation`. The shared role is keyed by kind. | A second DNS or NTP implementation would plan every object twice. This is a forward seam, not a live defect. | [F18-09] |
| The DNS server reads storage and cluster field paths directly. | A new export arm or storage type edits `dnsserver`. | [F18-10] |
| Preflight and capacity projections are libvirt-only and sit in composition. | Capacity for a new arm edits composition logic. | [F07-07] |
| No procedure exists for adding an implementation of an existing capability. | Agents discover the touch set by trial. Reviewers cannot tell necessary edits from overreach. | [F06-04] |
| "Abstractions only for real consumers" (`specs/project.md:88`) is unqualified. | A family's first implementation may hard-wire product facts. | [F06-06] |

#### Which extensions are low-impact today

| Extension | Additive today? |
| --- | --- |
| A new release of an existing component | Yes, through a version specialization |
| An external DNS, NTP or proxy | Yes, no code |
| A new add-on package, including a CSI driver | Yes |
| A new realization arm in Go, its own package only | Largely yes |
| That arm's consumers | No [F18-03] |
| A second implementation of an existing kind | No [F06-01, F18-09] |
| A new infrastructure kind | No [F18-04] |
| A new cluster distribution | No [F18-02] |
| A non-Ceph storage type | No [F05-03, F07-03] |

The pre-1.x version authority (`specs/api.md:31-37`) keeps every schema change inside `v1alpha1` with no conversion code. The vSphere and KubeVirt arms are still refused. The reshapes below therefore cost least now. Each later example, golden and milestone slice raises their price.

### 6.2 The specialization contract

The architecture spec should state the following rules once, in "Implementations and version variation" (`specs/architecture.md:901-969`). Each rule names the fitness test that would enforce it. The rules respect recorded decisions. D1 keeps the machine port as substrate role entry points. D162 and D163 keep version specializations narrow. D145 keeps the content digest a contract identity, not a hash of automation bytes. The pre-1.x rule allows reshaping without converters.

| # | Rule | Fitness test | Closes |
| --- | --- | --- | --- |
| SC-1 | **Family and implementation.** A context that may host more than one product is a family. It holds a product-neutral core (intent, selector, ports, evidence vocabulary) plus implementations. Product-shaped fields live under a product arm of the family kind. | Docs test: each context row names its family and lists its current implementations. | [F06-05, F18-02, F05-03, F07-03] |
| SC-2 | **Port per capability, seam at the first implementation.** A family's first implementation ships its selector, implementation identity, consumer-owned port and unbound-value refusal. A synthetic second implementation runs the shared port suite, as Secrets does. Speculative frameworks remain forbidden. | Contract-suite test per family port, run against a test double. | [F06-06] |
| SC-3 | **Registry shape.** Each implementation declares identity, kinds, selector values, block families, supported shape and content identity. The static composition list is the allowlisted registry, and the spec names it with the code's function name. | Test: every bound identity has a declaration row. Names follow one scheme. | [F07-10, F07-08] |
| SC-4 | **Selection from frozen input.** Each domain declares its selector, for example the InfraProvider arm, `distribution.type`, the StorageCluster type and `management`, or the service kind plus `spec.implementation`. Before registration, the engine computes the claims (object, block family). It refuses any claim held by two bindings and any required family held by none. | Engine unit test for overlap and gap. Golden of the claims over the example corpus. | [F06-01, F18-09, F06-12] |
| SC-5 | **Frozen identity covers removal.** Every value that fixes an effect's identity, place, mark, claimed resource or proof travels in the frozen request. A removal compares the effect-identity part of the digest. "Effect pin" is defined once. | Contract guard extended to mark-format ids and `bootwright.io` label keys. A removal-under-another-build test. | [R2-03-01, R2-03-02, R2-03-06] |
| SC-6 | **No identity branching outside the owning domain.** Implementation-identity constants, arm constants and arm field paths appear only in their owning package and in composition, apart from a shrinking allowlist that states its reasons. | Architecture test scanning `internal/reconciliation`, `internal/desiredstate`, `internal/environment`, `internal/cli` and consumer domains. | [F06-03, F18-04] |
| SC-7 | **Consumers dispatch through the owning domain.** Ansible consumers include one substrate-owned dispatcher role. Go consumers read capability facts on `substrate.Target`, not arm names. Cross-domain facts come from pure domain functions bound in composition, for example `MemberMachines`, `IsEffectBearing` and name contributions. | Test: consumer roles reference no arm role. Go consumers import no arm constant. | [F18-03, F18-07, F06-07, F06-08, F18-10, F07-07] |
| SC-8 | **Kind facts from the schema.** Effect-bearing status, managed-service membership, normalization rank and reference fields are derived from the `api/v1alpha1` registry, not hand lists. | Exhaustiveness test: every kind has a derived row. | [F18-04] |
| SC-9 | **Reference-edge catalog.** Each reference field class states its edge, stage pair, destroy inversion and selection retention in one table. | Test comparing emitted requirements over the examples with the catalog. | [R2-01-01, R2-01-04] |
| SC-10 | **Version specialization boundary.** Keep D162 as written. Add three clauses: a refusal seam may add a named pre-effect refusal; a difference beyond the excluded set is a new implementation identity; admission never keys on a release label. Runtime capability discovery over a controller is adapter mechanics, not a specialization. | Registry discovery by marker, not by path. | [F06-09, F06-10, F06-11, R2-02-04] |
| SC-11 | **Additive extension procedure.** Add "Adding an API kind" and "Adding an implementation of an existing capability" with a touch-point budget. An edit outside that budget is a missing seam, and the seam lands first. | Review checklist plus the SC-6 and SC-7 tests. | [F06-04] |
| SC-12 | **Support truth.** A coverage row states a capability Bootwright realizes. It never states a vendor's support status. | Docs lint refusing vendor-support phrases outside history. | [F17-01, F17-06] |

The SC-11 procedure would read roughly as follows in `specs/architecture.md`:

```markdown
### Adding an implementation of an existing capability

1. Create `internal/<domain>/<implementation>/` implementing the family port;
   declare identity `<domain>-<implementation>-<capability>-v1`, selector
   values, block families and supported shape (SC-3).
2. Add its row to the domain's Selection table and refusal table.
3. Bind it in `buildCapabilitiesWith` and `operationPlaybook`.
4. Add role `<domain>_<implementation>_<capability>` with the family's entry
   points, its playbooks and protocol plugin.
5. Run the family's shared port suite against it.
6. Touch nothing else. A needed edit in another domain or in general flow
   means a seam is missing: add the seam first, in its own change.
```

### 6.3 Code materialization map

The map shows where each context's promises live at commit `17acb63d9`. Port files are the `contracts.go` files under each package. The "Leaks" notes cite the findings that break the generic-flow promise at that layer.

| Bounded context | Capability packages | Ports (contracts.go) | Adapters | Wiring rows | Ansible roles; action protocols | Fitness tests |
| --- | --- | --- | --- | --- | --- | --- |
| Reconciliation (engine) | `reconciliation/lifecycle`, `operationstore`, `contextguard` | `Capability`, `CapabilityResolver{Bindings, Resolve}` | `reconciliation/ansiblerunner` | `capabilityResolver`; `operationPlaybook` | all playbooks via one runner | composition, boundaries, `TestBoundPlaybooksCoverExactlyTheOfferedCapabilities`. Leak: kind lists [F18-04, F06-08] |
| Substrate | `substrate/libvirt`, `substrate/baremetal`, `substrate/capacity` | per-arm `Runner`; capacity checker | Ansible arm roles | `boundCapability` rows; `realizationOf`; `libvirtCapacityAdmission` | `substrate_libvirt_host`, `substrate_libvirt_machine`, `substrate_baremetal_machine`; `substrate_host_protocol`, `substrate_machine_protocol`, `substrate_physical_protocol` | `TestSubstrateRefusalTableMatchesUnsupported`. Leaks: `Realized()` list, generic plugin name [F06-12, F07-08] |
| Machine | `machine/power`, `machine/inventory`, `machine/access` | consumer-owned `Runner`, realization, identities | `machine/sshlocal` | `wiring_machine.go` translators | `machine_power_redfish`, `machine_power_read_redfish`; `machine_power_protocol` | boundaries. Leak: Redfish-only power [F18-07] |
| Managed OS | `managedos/installation` (install, tree, erase), `managedos/media` | `Runner`, `Identities`, `MediaRecords` | `managedos/medialocal` | rows with `WithClaims(baremetal.BlockID)` and Ceph retirement | `managedos_install_anaconda`, `managedos_package_tree`, `physical_erase`; matching protocols | request goldens. Leaks: arm dispatch tables, Ceph fan-in [F18-03, F06-07] |
| Infrastructure services | `managedservice` with `dnsserver`, `ntpserver`, `proxy` definitions; `artifactserver` | `Runner`; `managedservice.Definition` | Ansible roles | loop over Definitions | `infra_managed_service`, `infra_artifact_server_nginx`; `infra_service_protocol`, `artifact_server_protocol` | managed-service refusal table. Leaks: kind-keyed role, DNS pulls fields [F18-09, F18-10] |
| Container cluster | `agentinstall` (media, install), `networking`, `access`, `preflight` | `Runner`, `Identities` | `containercluster/preflightlocal` | rows; install access | `containercluster_media_agent`, `containercluster_install_agent`, `containercluster_network`; matching protocols | topology table test. Leaks: OpenShift-shaped context, arm literals [F18-02, F06-03, F06-09] |
| Storage | `storage/ceph` (core, image, host, export, SMB, replication, physical retirement), `storage/preflight` | `Runner`, `DependencyProofs` | `storage/preflightlocal`, `storage/rendering` | loop binding 12 kinds to Ceph core; `cephDependencyProofs` | `storage_ceph_core`, `_host`, `_image`, `_export`, `_smb`, `_replication`; `ceph_*_protocol` | per-kind refusal tables. Leaks: Ceph is the context; general code reads `spec.ceph.topology.nodes` [F07-03, F05-03] |
| Add-ons | `addons/instance`, `addons/clusterobjects/driver`, `addons/packages` | `Runner`; package registry | `cluster_objects` plugins | rows; `packages.Builtin()` | `addons_instance`, `cluster_objects`; `cluster_objects_protocol`, `_read`, `_write` | package goldens, registry tests. Additive for new packages |
| Secrets | `secrets/secretstore`, `custody`, `material`, `encryption` | `ImplementationResolver{Types, Select, Reopen}` | `secrets/localkeyring` | `wireSecrets` | none | contract suite with a second test implementation. The model to copy |
| Controller | `controller/clients`, `prerequisites`, `firewall`, `privilege` | tool, setup and firewall ports | `*local` packages, `hostdnf`, `hostlinux` | controller rows | `controller_prerequisites`; `controller_protocol`, `controller_tool` | runtime-lock tests. Unversioned identity [F07-08] |
| Environment and desired state (general flow) | `environment`, `environment/inspection`, `desiredstate/compilation` | `inspection.CephEvidence` | none | `wiring_inspection.go` | none | none for implementation leakage. Leaks: Ceph ports and arm paths [F07-03, F18-04] |

Three observations follow from the map.

1. **The engine row is clean.** The F07 finder found no implementation-identity literal in `internal/reconciliation` beyond one retired-fixture string. The failures sit in the domain and general-flow rows.
2. **Fitness tests hold layering, construction and effects, not the generic-flow promise.** No test catches an arm path, an implementation literal or a dispatch table that misses an arm [F06-03]. Rules SC-6 and SC-7 add those tests.
3. **Names follow three schemes.** Roles mix `<domain>_<implementation>_<capability>` (`storage_ceph_core`) with `<domain>_<capability>_<implementation>` (`managedos_install_anaconda`), and some roles have no domain (`physical_erase`). The spec states a third order (`specs/architecture.md:681`). `substrate_machine_protocol` has a generic name but uses libvirt's domain-state vocabulary [F07-08].

### 6.4 Extension scenarios

Each table lists what an implementer would touch today and after rules SC-1 to SC-12. The "General flow" row answers the owner's test: does code outside the new implementation and composition change?

#### (a) vSphere as a substrate

| Layer | Today | After the rules |
| --- | --- | --- |
| API | `InfraProvider.spec.vsphere` and its arms are admitted and refused. The provider copies OpenShift's install-config platform shape [F18-06]. | Lift the refusal. Keep the provider to realization facts, and give `nodeNetworking` one owner (B99 question). |
| Go, own | New `internal/substrate/vsphere`. | Same. |
| Go, elsewhere | `substrate/target.go` (`Realized`, `TargetFor`, Target fields); machine admission; managed-OS `install_interface.go`; agent-install push delivery; `machine/power` [F18-03, F18-07]. | One `TargetFor` case, plus capability facts on `Target`. No consumer edit. |
| Wiring | `buildCapabilitiesWith` and three `operationPlaybook` rows; `realizationOf`; `cephDependencyProofs`; capacity projection [F07-07]. | Registry rows only. Capacity is an optional per-arm projector. |
| Ansible | New role with eight entry points. Also edit about twelve consumer dispatch tables and the identity channel include [F18-03]. | New arm role only, reached through the substrate dispatcher role. |
| Fitness | packageRoles, applicationDependencies, refusal table, playbook bindings, goldens. | Same, plus the SC-4 claims golden and the shared port suite. |
| General flow changes? | **Yes:** consumers in Managed OS, Container cluster and Machine, plus an optional CLI label. | **No.** |

#### (b) Vanilla Kubernetes through kubeadm

| Layer | Today | After the rules |
| --- | --- | --- |
| API | `distribution.type` gains a value, but top-level fields are OpenShift rules: `install.method: agent`, `pullSecretRef`, `api-int`, OVN settings, `master`/`infra` roles, FIPS and `openshift-node` [F18-02]. In practice this means a new kind or a breaking reshape. | `ContainerCluster` is a neutral envelope (nodes, roles, endpoints, networks, service selections) plus a `spec.openshift` arm. kubeadm adds a `spec.kubeadm` arm (SC-1). |
| Go, own | New `containercluster/kubeadminstall`. | Same. |
| Go, elsewhere | Context-root admission, endpoints, topology and VIPs branch per distribution. `agentinstall` selection must skip kubeadm clusters [F06-01]. `addons.ClusterFacts` carries an OVN gateway. | The family core stays. The selector routes by distribution arm, and the engine checks claims. |
| Wiring | Capability and playbook rows, plus the controller tool closure. | Same. |
| Ansible | New install role and protocol. Node OS comes through Managed OS. | Same. |
| Specs | `container-clusters.md` is entirely agent-installer. The coverage page is OpenShift-named [F17-06]. | A family page plus an OpenShift page and a kubeadm page. |
| General flow changes? | **Yes:** every hard-coded kind list if a new kind is chosen [F18-04], and the cluster context root either way. | **No**, once the envelope reshape lands. |

#### (c) A non-Ceph software-defined or array storage

| Layer | Today | After the rules |
| --- | --- | --- |
| API | `StorageCluster.spec.type` is `ceph` only. The thirteen `Storage*` children carry Ceph semantics [F05-03]. `StorageExport` has only Data Foundation arms. | The StorageCluster type arm is the selector. Child kinds declare which types they serve (SC-1). |
| Go, own | New `internal/storage/<array>`, or only a CSI package in `addons/packages` (additive). | Same. |
| Go, elsewhere | Ceph capability must skip non-Ceph clusters [F06-01]. Composition binds every storage kind to Ceph. Environment inspection, selection, admission and compilation read Ceph paths [F07-03]. Managed OS retirement is bound to Ceph [F06-07]. DNS reads export paths [F18-10]. | Storage publishes `MemberMachines` and name contributions. Retirement policies fan in. The engine claim check covers the rest. |
| Specs | `storage.md` is "the target runtime contract for managed Ceph". Coverage rows are conditioned on IBM support [F17-01]. | A family page plus a Ceph page. Coverage states what Bootwright realizes. |
| General flow changes? | **Yes:** Environment and compilation, and composition fan-ins. | **No.** |

#### (d) External-only DNS or NTP, a second managed implementation, and a DHCP component type

| Case | Today | After the rules | General flow changes? |
| --- | --- | --- | --- |
| External DNS or NTP | Nothing changes. `management: external` with an address is admitted and adds no edge. This is the positive control. | Same. | No |
| Second managed DNS implementation (for example CoreDNS) | A new `Definition`. Selection ignores `spec.implementation`, and the role is keyed by kind [F18-09]. | Selection filters on `spec.implementation` through SC-4. The role is keyed by identity. | No after; small Go edit today |
| New `DHCPServer` kind | Schema, kind catalog and specs, plus a new Definition. Also edit `infrastructureservices.IsService`, `environment/selection.go`, `compilation/phases.go`, `lifecycle/support.go`, `endpoint_ownership.go`, `prerequisites/reservations.go` and a CLI label [F18-04]. Fold DNS records and reservations through new pulls [F18-10]. | Schema, Definition, role row, templates and playbooks. Kind facts are derived (SC-8). Domains contribute reservations and names (SC-7). | **Yes** today (at least six lists); **No** after |

### 6.5 Immediate portfolio

The matrix records the status of each portfolio item at this commit. "Realized" means a bound implementation plans and applies it. "Refused" means admission or planning refuses it with a named diagnostic. "Not yet met" means a spec marks the gap.

| Item | Implementation identity | Status | Gaps |
| --- | --- | --- | --- |
| libvirt with sushy-tools BMCs | `substrate-host-libvirt-v1`, `machine-libvirt-v1` | Realized | External attachments claim nothing [R2-06-01]. An emulated BMC claims no key [R2-06-04]. Arm leaks into consumers [F18-03]. |
| Bare metal through vendor BMCs (first xFusion) | `machine-baremetal-v1`; `physical-erase-v1`; `machine-power-redfish-v2` | Realized, with physical operator gates open | Vendor variation has no stated model [R2-02-04]. No substrate coverage page [R2-02-05]. Day-2 power on unproved targets [F10-02]. BMC rotation strands removal [R2-04-01]. |
| KubeVirt Machines | none bound; arm `kubevirt` | Refused, Not yet met (B87) | Ownership proof weaker than libvirt's [R2-02-03]. Stop is unreachable [F18-07]. Nested host chain unordered [R2-01-02, R2-01-03]. |
| IBM Storage Ceph | `storage-ceph-core-v3` and image, host, SMB, replication, physical-retirement identities | Realized for M2 scope | Coverage gates on vendor support that D157, D159 and D163 forbid [F17-01]. Context is Ceph-shaped [F07-03]. Closure omits four kinds [R2-01-04]. |
| IBM Fusion Data Foundation add-on | `addon-instance-v1` with the `fdf` built-in package; `ceph-export-v1` | Realized; B760 and B761 open | Export retirement can destroy a surviving consumer's storage [R2-02-01, F10-04]. |
| NTP, managed or external | `ntp-server-chrony-v1`, or external | Realized | Served time source defined only in a template [R2-02-07]. Implementation field unused [F18-09]. |
| Proxy, managed or external | `proxy-squid-v1`, or external | Realized | Cluster-install Proxy edge is in code, not in the spec [R2-01-01]. |
| Existing networking (provided) | no block | Realized as input | None in this lens. |
| Managed Registry and LoadBalancer | none bound | Refused by the engine row | The API page carries no Not-yet-met marker [R2-02-06]. |

The bare-metal contract is the strongest part of the portfolio. It proves one exact ComputerSystem and its full MAC set, pins UUID and serial, re-proves before media insertion, and names its residual race honestly (`specs/state-reconciliation.md:1916-1928`). KubeVirt is marked truthfully: every page that describes it carries a Not-yet-met line naming B87. The one High defect in this lens is safety, not specialization. A context with virtual Ceph hosts and a physical consumer cluster can retire the export that the surviving cluster still uses [R2-02-01].

### 6.6 Cross-domain interaction chain and selection closure

The engine contract for interactions is sound and generic. A capability names its dependencies as API objects, and the engine resolves them to blocks before freezing (`specs/state-reconciliation.md:781-787`). Stages are not strata, so a `substrates` block may wait on an `add-ons` block (`specs/state-reconciliation.md:820-832`). Destroy inverts every edge by one uniform rule (`specs/state-reconciliation.md:1534-1540`). The intra-storage chain table and the add-on instance requirement row (`specs/add-ons.md:131`) are the models to copy.

The gaps lie in what feeds that engine.

| Gap | Effect | Id |
| --- | --- | --- |
| No reference-to-edge catalog. Each domain lists its edges in prose, and the cluster-install list omits the managed Proxy the code waits for. | The DAG cannot be derived from the specs. A new distribution may drop the edge and race the proxy. | [R2-01-01] |
| The nested KubeVirt chain depends on a storage class, networks and a capability token named only by external strings. | Hub VMs could start before their storage class exists, and removal order would be wrong. This is input to B732. | [R2-01-02] |
| Selecting a nested cluster keeps its KubeVirt provider but not the host cluster. | Validation reports a declared cluster as missing. | [R2-01-03] |
| The StorageCluster closure row omits four kinds and StorageReplication's peer. | Spec and code closures diverge. Selecting the primary leaves the peer reference unresolved. | [R2-01-04] |
| Authoring a cluster list silently drops standalone Machines. | Bastion installations leave the plan with no warning. | [R2-01-05] |
| A cluster root omitted from an authored list is still installed when an attachment retains it. | The context owns a partial Ceph cluster without warning. | [R2-01-06] |
| CustomPlaybook orders against anchors that name no stage. | B102 inherits a model the engine cannot express. | [R2-01-07] |

Rule SC-9 closes the first gap. One catalog row per reference field class would state the edge, stage pair, destroy inversion and retention. A test would compare each capability's emitted requirements with that catalog over the example corpus. The closure rows should then come from the same catalog, not a separate list.

### 6.7 Frozen identity across builds

Frozen implementation identity is the architecture's continuation anchor, and most of it is sound. The execution closure is cut precisely (`specs/state-reconciliation.md:1048-1056`). The content digest is a contract identity, so an `ansible/`-only fix continues a live operation (D145). The apply proof of "same blocks" re-plans and compares every frozen member under both locks. The contract guard in `cmd/bootwright/contract_versions_test.go` ties schemas and quoted literals to each digest.

Three gaps weaken the anchor for removal.

- A removal copies the digest from the frozen plan and never compares it. If a build moves a place, prefix or mark outside the request, the new build's removal looks in the wrong place, records absence and releases reservations [R2-03-01].
- Preserving marks and proofs across builds rests on author judgement. The guard cannot see unquoted or dynamically built marks [R2-03-02].
- "Effect pin" is undefined, so one implementation hashes pinned values and another hashes a hand-moved tag [R2-03-06].

Related process and shape gaps follow. No skill or commit check asks an agent to move a digest [R2-03-03]. Optional record members are added without a version move [R2-03-05]. The verification also considered and refuted a claim that "same blocks" lacked a per-verb definition (R2-03-04): the definition exists at `specs/state-reconciliation.md:1068-1080`. Rule SC-5 states the fix: carry effect-identity values in the request, and make removal compare them.

### 6.8 What to do, in order

1. Adopt rules SC-1 to SC-12 in `specs/architecture.md`, with SC-4, SC-6 and SC-7 first, because their fitness tests stop further leakage.
2. Decide the `ContainerCluster` distribution envelope and the `StorageCluster` type binding now, while the pre-1.x rule makes the reshape cheap [F18-02, F05-03].
3. Land the substrate dispatcher role and capability facts on `Target` before B99 or B87 lift their refusals [F18-03, F18-07].
4. Replace general-flow hand lists with schema-derived kind facts and domain functions [F18-04, F07-03].
5. Fix the export retirement rule [R2-02-01] and the coverage support wording [F17-01] before more Ceph and FDF work builds on them.

## 7. Language responsibility matrix: Go, Ansible, Python, shell

Standard judged against: a multi-language product states once which kind of logic each language may hold. It names where each runs and on which interpreter, what each must never do, and who owns its tests. A mechanical check keeps placement honest. An agent should answer one placement question per language instead of reading four specs.

Verdict: the Go and Ansible boundary is among the best passages in the corpus and should be kept. Python is the largest adapter language and has no stated row of its own. Shell and repository tooling are governed by habit, not by rule. Nine verified findings fall in this area: four Medium and five Low. None is Blocking, because the code is mostly safer than the text that governs it.

### 7.1 What the specs rule today, and what is good

#### The Go and Ansible boundary

`specs/architecture.md:629-649` lists what Go owns: "desired-state semantics, normalization, validation, and graph closure", implementation selection, "immutable planning, ordering, authorization, operation state", continuation and user-facing results. Ansible owns target-tool acquisition and "Bootwright-controlled interaction with managed remote components". The passage then says what Ansible must not do: it "does not infer desired state, choose product workflow, order cross-domain work, grant authorization, own operation state, or format product output" (`architecture.md:647-649`).

The controller's own dependencies are explicitly Go's: the runtime venv (`internal/controller/venvlocal`) and host packages through the host's dnf (`internal/controller/hostdnf`), with "no Ansible role installs a host package" (`architecture.md:654-664`). The one exception, Machine's direct SSH sessions, is named and linked. Keep all of this unchanged.

#### Playbook, role and plugin rules

Playbooks are thin entrypoints that "do not choose workflows or providers from authored data or ambient facts" (`architecture.md:694-700`). Substrate dispatch ends in a terminal fail for an unknown value (`architecture.md:745-748`). Plugins validate a frozen request, call one API and normalize its result (`architecture.md:752-757`). The collection's structural tests hold every role to rules a play cannot check: argv-only commands from an exact allowlist, "never through a shell", `no_log` on material, `loaded` first, poll semantics and template goldens (`architecture.md:1013-1039`).

#### Cross-language contracts

The adapter result protocol (`architecture.md:762-900`) is an excellent cross-language contract. One Go decoder and one runner core live in `internal/adapterprotocol`. One Python publisher, `plugins/module_utils/adapter_protocol.py`, owns phase dispatch, digest checks and refusal shape. Records are canonical JSON on descriptor 3 with closed phases and bounds. The durability guard `TestEveryImplementationMovesItsContentDigestWithItsContract` fails the build when a Python format moves without its digest. The version-specialization rule is deliberately language-neutral, "Whatever the language" (`architecture.md:913-920`), and a test holds its catalog. Completion scripts under `internal/cli/completion/` are thin shims that ask the binary, and `scripts/go` is a short pin. These are the patterns the matrix should generalize.

### 7.2 Violations and silences

| Id | Sev. | Defect |
| --- | --- | --- |
| [F08-01] | Medium | Remote fixed-worker Python sends a shell string over SSH, an unwritten exception to "Invocation uses no shell" (`specs/security.md:169`). |
| [F08-04] | Medium | Python has no reference, formatting rule or skill route; the skill loads only formatting, Go and Ansible guidance. |
| [F08-06] | Medium | At least seven Python copies of canonical JSON and five bounded process runners, where Go has one of each. |
| [F08-07] | Medium | No placement question or fitness test decides which language new logic belongs in. |
| [F08-02] | Low | Postcondition ownership: D11 says Go; `architecture.md:890-892` gives it to the plugin. |
| [F08-03] | Low | "Plugins contain no workflow" contradicts the storage, add-on and managed-OS specs that assign fixed procedures to Python. |
| [F08-05] | Low | Python execution sites, the 3.9 managed-host floor and the stdlib-only rule for shipped workers are not spec clauses. |
| [F08-08] | Low | The Go network criterion is split across three overlapping axes. |
| [F08-09] | Low | `architecture.md:772` says the controller runner serves `setup`; `architecture.md:867` says "Setup runs no Ansible." |
| [F08-10] | Low | Tooling runs host `python3`, and `scripts/quick-test` is a 156-line selection engine with no rule. |

The scale explains the urgency of [F08-04]. The collection holds 140 plugin files with 36,682 lines of Python, against 14,811 lines of Ansible YAML. Yet `specs/project.md:58-59` calls the whole far side "Ansible". Python runs in four places with different interpreters: controller action plugins in the locked venv, modules on managed hosts under a 3.9 floor, fixed workers shipped as source and run through `sudo -n /usr/bin/python3 -I -c`, and scripts inside the Ceph image. Only a test and delivered history name these sites [F08-05].

The two contradictions share one cause: the spec forbids what the product needs. Ceph rounds, cluster object ordering and network convergence filters are block-internal procedures. A blanket prohibition cannot be applied consistently [F08-03]. Likewise, the plugin must define its postcondition fields, while Go alone judges them [F08-02].

### 7.3 The decision matrix

| Language | Owns | Must not | Typical artifacts | Tests owned by | Placement question |
| --- | --- | --- | --- | --- | --- |
| Go | Admission, semantics, typed models, compilation, planning, selection, cross-block ordering, authorization, lifecycle, operation state, continuation, postcondition judgment, results, local runners, controller dependencies, content digests | Contact a managed component (except named SSH sessions); interpret native output beyond adapter evidence | `internal/**`, `api/v1alpha1`, `cmd/bootwright` | Package tests, port contract suites, `test/architecture` | Does this decide what happens, in what order, whether it is allowed, or what a result means? |
| Ansible YAML | One capability's binding: argument-spec validation, host targeting, privilege scope, fixed module calls, progress groups, bounded remote effects and observation under a frozen request | Choose workflow or variant from data or facts; call across domains; use shell, raw or script | `playbooks/<domain>/`, `roles/<domain>_<capability>/` | Structural role tests, syntax and lint; Go capability contract suite | Is this binding one fixed capability to hosts, privilege and modules? |
| Python (collection) | Action plugins and module utilities: protocol publication, native transport, bounded native-API clients, result normalization, evidence fields, a fixed spec-owned procedure inside one frozen request, shipped workers | Product policy or workflow; dispatch on authored data; cross-block ordering; authorization; operation state; ad hoc runners or encoders | `plugins/action`, `plugins/modules`, `plugins/module_utils`, `plugins/filter` | Collection units on controller and floor interpreters; Go digest guard | Is this how one already-selected effect is reached and proved on its target? |
| Shell | Pinned tool wrappers and completion shims | Selection logic, parsing repository content, product effects | `scripts/go`, `scripts/ansible-check`, `internal/cli/completion/*.sh` | Completion tests; tooling ratchet | Does this only fix an environment and exec a pinned tool? |
| Repository tooling | Gate dispatch in `Makefile`; stdlib-only launchers in `scripts/tools/` | Product behaviour; checks run outside a pinned wrapper | `Makefile`, `scripts/tools/*.py` | `make docs-check`, tool self-tests | Does this only choose which pinned check to run? |

### 7.4 Prohibited uses per language

- **Go** must not reach a managed remote component, except Machine's named SSH sessions. It may reach a non-managed endpoint only read-only, bounded, through the admitted egress route, for runtime wheels, tool metadata and media import [F08-08].
- **Ansible YAML** must not select a workflow, provider or variant from authored data or gathered facts, order work across domains, or run a command through a shell (`architecture.md:694-700`, `architecture.md:1020-1022`).
- **Python** must not choose product workflow, order work across blocks, authorize, or write operation state [F08-03]. It must not judge a postcondition; it defines the evidence fields and may refuse when they are missing [F08-02]. It must not start a process outside its site's one runner, or encode canonical JSON outside its site's one module [F08-06]. A shipped worker or container entrypoint must not import beyond the target's standard library [F08-05]. A remote command string must not contain a token that is neither literal nor closed and validated [F08-01].
- **Shell** must not branch on repository content or hold selection logic; `scripts/quick-test` is either moved under the rule or named as the exception [F08-10].

### 7.5 Where the matrix lives, and how a test enforces it

Following the arbiter on [F08-07], widen the existing "Go and Ansible responsibility boundary" section in place. Do not add a parallel section that restates `architecture.md:629-668`. Concretely:

1. Retitle the section "Language responsibility" and add the table in 7.3, with one placement question and the prohibited uses per row. `references/go.md` and `references/ansible.md` link to it instead of restating "Lifecycle policy remains in Go" (`references/ansible.md:8`).
2. Requalify `architecture.md:755` with one sentence: an adapter may sequence the fixed, spec-owned native steps of its one frozen request [F08-03].
3. Reword `architecture.md:890-892`: the plugin owns the evidence fields of its declared postcondition; Go alone decides whether evidence proves it (D11, D99) [F08-02].
4. Name the remote fixed-worker exception in `security.md#process-boundary`, scoped to every SSH remote command string [F08-01].
5. Add the Python execution sites, the stdlib-only rule and a link to the floor's owner near `specs/controller.md:1027` [F08-05].
6. Add `references/python.md`, or a Python section in `references/ansible.md`, and name Python and shell in `formatting.md` [F08-04].

Enforcement can reuse existing gates. A small `test/architecture` test, scoped to `plugins/` and `scripts/`, maps each tracked source path to a matrix row and fails on an unmapped file [F08-07]; the arbiter made it optional. The existing pylint `ansible-bad-function` rule, with an allowlist of files allowed to disable it, gates `subprocess` use [F08-06]. One collection test asserts that SSH remote commands are built only through the shared quoting function [F08-01]. The existing Go effect allowlist already fails on an unnamed `net/http` importer; docs-check should cross-reference it with the spec's named consumers [F08-08].

### 7.6 Migration of today's violations

| Order | Change | Ids | Cost |
| --- | --- | --- | --- |
| 1 | Fix the `setup` parenthetical at `architecture.md:772` | [F08-09] | One line |
| 2 | Requalify the plugin workflow rule and the postcondition sentence | [F08-03], [F08-02] | Two sentences |
| 3 | Widen the boundary section with the matrix; link from go.md and ansible.md | [F08-07], [F08-08] | One section |
| 4 | Write the Python reference and formatting rule; route the skill by extension | [F08-04], [F08-05] | One page |
| 5 | Name the remote-worker exception; add the quoting-function test | [F08-01] | Spec clause, one test |
| 6 | Consolidate canonical JSON and process runners to one module per site, with shared golden vectors where Go compares digests; extend B461 | [F08-06] | Code refactor |
| 7 | Add the tooling rule and derive docs-check test selection from a convention | [F08-10] | Small |

Steps 1 to 4 are text only and unblock every later adapter: a new storage or substrate type then has a rule for where each step goes. Step 6 is the only code refactor. It should land before a second SSH-based storage vendor copies the pattern again.

## 8. Reconciliation rule audit (part A): mutation safety, ownership, trust and secrets

This part covers the rules that decide what Bootwright may destroy, whom it trusts, and how secret material travels. Part B covers apply, destroy, continuation, attempts, block transitions and GitOps readiness. The general rules live in `specs/state-reconciliation.md` "Mutation safety" through "Confirmation and authorization" (1809-2084) and in `specs/security.md`. Every domain page was compared against them.

Thirty-seven verified findings fall in this part: five High, fifteen Medium and seventeen Low. None is Blocking after verification. [F10-01] was proposed Blocking; both verifiers kept it at High because the fix is small and the agent installer path is already safe.

### 8.1 Standard judged against

A destructive or ownership-changing effect is sound when it meets seven conditions:

1. It positively identifies the exact target immediately before the effect.
2. It proves ownership by durable, non-forgeable identity, never by name.
3. It proves quiescence separately from the loss acknowledgement.
4. It requires explicit opt-in before logically overwriting a physical disk.
5. It protects the controller host.
6. It takes an authorization that a stale or copied acknowledgement cannot satisfy.
7. It never reads an unknown or ambiguous probe as absence.

For security, the standard is a fail-closed orchestrator that runs as root on one controller host and drives remote root effects. Its threat model names every principal, the facts each principal may assert and the material each one receives. A disclosure sink (argv, sudo's log, retained output, records, served trees) is closed by a mechanism, not by convention.

The specs already state most of this standard themselves. "A name or controller record may locate a target but never proves ownership or identity" (`specs/state-reconciliation.md:1816-1817`). "Failed, forbidden, unsupported, empty, malformed, or contradictory probes are unknown, never absence" (`specs/state-reconciliation.md:1821-1822`). The defects below are places where a domain page or a CLI rule does not live up to these sentences.

### 8.2 Rule-by-rule table

Verdicts: **sound** (keep as written), **ambiguous** (two readable implementations), **contradicted** (another page says otherwise), **unsafe** (a concrete wrong-target or wrong-owner result), **unimplementable** (no defined proving step).

#### General mutation safety

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Validate and plan the whole context before the first effect | state-reconciliation.md:1811 | sound | none |
| One mutator per context through a durable lease | state-reconciliation.md:1812 | sound | none |
| Revalidate identity, ownership, quiescence and absence before every effect | state-reconciliation.md:1813-1815 | sound as a rule; not honoured by day-2 power or by the managed-OS disk check | [F10-01], [F10-02] |
| A name never proves ownership | state-reconciliation.md:1816-1817 | contradicted by the same page | [F10-03] |
| Unknown is never absence | state-reconciliation.md:1821-1822; security.md:33-35 | sound; missing only on the vSphere arm | [F10-07] |
| Foreign, unowned and unknown targets refuse without bypass | state-reconciliation.md:1823 | sound | none |
| Every view derives from one immutable plan | state-reconciliation.md:1826-1827 | sound | none |
| Transition re-proof under the exclusive lock | state-reconciliation.md:1558-1580 | sound | none |

#### Quiescence before removal

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Online and pre-stop orderings; pause instead of force | state-reconciliation.md:1852-1892 | sound | none |
| Data-loss acknowledges; quiescence proves separately | state-reconciliation.md:1894-1897 | sound | none |
| Stopping is never a side effect of removal | state-reconciliation.md:1894 | sound | none |
| Storage retired with its virtual hosts takes their stop proof as quiescence | state-reconciliation.md:1836-1839 | unsafe when a consumer survives | [R2-02-01], [F10-04] |
| Ceph removal while a host survives refuses (B491) | state-reconciliation.md:1831-1834 | sound | none |
| Managed libvirt network quiescence derived from the owner's own Machines | substrates.md:230-233 | unsafe | [R2-06-01] |

#### Physical disk safety and opt-in overwrite

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Claim serializes use; never ownership or permission to destroy | state-reconciliation.md:1903-1911; substrates.md:584-587 | sound | none |
| Complete live MAC set matches the Machine | state-reconciliation.md:1906-1907 | ambiguous (equality here, subset in substrates.md:539-540) | [F10-05] |
| Select one whole root disk from the authored hint | state-reconciliation.md:1907-1908; managed-os.md:1268-1280 | unsafe for managed-OS physical install | [F10-01] |
| Residual race stated honestly; nothing claims to close it | state-reconciliation.md:1915-1924 | sound | none |
| Opt-in logical overwrite (B70): WWN, serial, size, RAM worker, receipt bound to the original apply | state-reconciliation.md:1930-2014 | sound; the template for other destructive paths | none |
| Cancellation never means erase finished | state-reconciliation.md:2001-2014 | sound | none |

#### Controller-host protection

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Fresh removal re-proves the host binding before plan and under the lock | state-reconciliation.md:2033-2040 | sound | none |
| Apply or destroy preserves controller OS, power, runtime and state | state-reconciliation.md:2042-2044 | sound but scoped too narrowly (not day-2 power) | [F10-02] |
| Managed networks avoid the host's own routes and links | substrates.md:141-144 | gap: only Bootwright attachments compared | [R2-06-05] |
| Controller services run under host reservations | state-reconciliation.md:2025-2031 | sound | none |

#### Identity and ownership per substrate arm

| Arm | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| libvirt domain | substrates.md:320-321, 367-369, 408 | contradicted: UUID derived from names | [F10-03] |
| Emulated BMC | substrates.md:408-409 | gap: no `bmc:` key | [R2-06-04] |
| Bare metal target proof and pin | substrates.md:526-563, 612-618 | sound except MAC predicate and learned pin | [F10-05] |
| Bare metal `bmc:` claim | substrates.md:584-586; infrastructure-services.md:112 | ambiguous: keyed by address spelling | [R2-06-03] |
| KubeVirt VirtualMachine | substrates.md:660-666, 688-696 | sound deletion; gaps in labels, lost reply, reservation | [R2-02-03] |
| vSphere | substrates.md:718-730 | gap: no marker fields, no unknown rule | [F10-07] |
| Add-on cluster objects | add-ons.md:180-200 | ambiguous: computable mark accepted as ownership | [F10-08] |

#### Destructive paths

| Path | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| OS install over a physical disk (managed OS, `clearpart`) | managed-os.md:1268-1280 | unsafe | [F10-01] |
| OS install over a physical disk (agent installer) | container-clusters.md:200-201 | sound, residual race stated | none |
| Logical erase on removal | state-reconciliation.md:1930-2014 | sound | none |
| Power actions on physical Machines | substrates.md:826-831; cli.md:1353-1378 | unsafe when unpinned; controller not excluded | [F10-02] |
| libvirt VM deletion | substrates.md:367-369 | contradicted (name-derived ownership) | [F10-03] |
| KubeVirt VM deletion | substrates.md:688-696 | sound (UID precondition, positive NotFound) | none |
| vSphere VM deletion | substrates.md:728-730 | gap | [F10-07] |
| Ceph device use | storage.md:702-761 | sound (WWN by-id, live exclusions) | none |
| Ceph cluster removal, hosts survive | storage-resources.md:1252-1255 | sound (refuses, B491) | none |
| Ceph retirement with virtual hosts | storage-resources.md:1252-1255 | unsafe with a surviving consumer | [R2-02-01], [F10-04] |
| Add-on retirement | add-ons.md:389-391 | sound (no remote effect); impacts omit survivors | [F10-04] |
| Context deletion with `--allow-orphans` | contexts.md:816-838 | sound locally; releases keys early | [R2-06-02], [F10-03] |

#### Confirmation and authorization

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| `data-loss` the only token; inapplicable tokens refuse | state-reconciliation.md:2066-2072 | sound | none |
| `--yes` never authorizes loss or selects a target | cli/commands.md:298-306 | sound | none |
| Repeated `--yes` "never does more than the plan the operator reviewed" | cli.md:255-260 | unimplementable as written: nothing binds it | [F10-06] |
| Host-key first use needs interactive confirmation | security.md:509-512 | contradicted by `machine trust --yes` | [F11-03] |
| `--force` never overrides proofs | security.md:670-672 | sound as stated; false for unpinned Machines | [F10-02] |

#### Trust, privilege, process and network

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Controller-host attacker model | security.md:37-49 | sound but incomplete: remote principals unmodelled | [F11-01] |
| Untrusted input includes state "not written and authenticated by Bootwright" | security.md:12-13 | ambiguous | [F11-11] |
| Per-context authority among sudo-admitted accounts | security.md:104-105 | ambiguous; no attribution | [F11-05] |
| Process boundary: no shell, allowlisted argv and env, data-only Ansible | security.md:164-227 | sound | none |
| Only listed children receive a proxy route | security.md:173, 315, 362-364 | contradicted three ways | [F11-12] |
| Runner sweep bound | security.md:263-271 | unsafe (local denial of service) | [F11-07] |
| sudoers rule for the procfs re-execution | cli.md:282-284 | design: narrow rules are ALL-equivalent | [F11-04] |
| File modes 0700/0600 with one exception | security.md:150-152 | contradicted by the served tree (0640) | [F11-10] |
| Listener exposure table and firewall admission | security.md:468-499 | sound; emulated BMC bind is Not yet met (B334) | none |
| Capability URLs minted per attempt, never in records | infrastructure-services.md:400-411 | sound | none |
| Pinned host keys as transport boundary (D179) | security.md:502-544 | sound for sessions; enrollment gap | [F11-03] |
| Pin and verify publisher authenticity; no floating tags | security.md:558-564 | contradicted for target tools | [F11-02] |
| Controller runtime lock with hashes | controller.md:87-160 | sound, exemplary | none |

#### Secrets custody, binding, reveal and rotation

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Single opener of operator files (`secret set`), stdin rules, D118 | secrets.md:52-214 | sound | none |
| Secret values never on argv | security.md:79-81 | contradicted by `--username` | [F11-08] |
| Whole-version immutable binding; lent views | secrets.md:265-277; state-reconciliation.md:1015-1027 | sound | none |
| Removal presents the apply-bound version | state-reconciliation.md:1482-1486, 1521-1524 | unsafe for rotated access credentials | [R2-04-01] |
| Produced-material custody and D124 hand-over | state-reconciliation.md:605-653 | sound | none |
| Produced inputs read from custody each attempt | state-reconciliation.md:658-661 | gap: no identity-stability rule | [R2-04-08] |
| "Unproved" custody mark | state-reconciliation.md:617-620 | contradicted by cli.md:1167-1169 | [R2-04-04] |
| Which commands reveal produced material | secrets.md:353-357 | contradicted (object-user keys) | [R2-04-02] |
| Native-side plaintext retention files | storage.md:1050-1051, 1559-1560 | gap: no owner | [R2-04-03] |
| Adapter "material values" channel | security.md:112-120, 183-184 | gap | [R2-04-05] |
| Module-argument payloads on remote hosts | security.md:105-107 | gap: unlisted hop | [R2-04-07] |
| Installer work areas | container-clusters.md:357-359, 784 | gap: plaintext outside custody | [R2-04-06] |
| Keyring encryption threat | secrets.md:518-520 | sound mechanics; no named threat | [F11-09] |

#### Host reservations and locking across contexts

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| Claims published atomically with host identity and bindings | contexts/controller-record.md:13-18 | sound | none |
| Typed keys; wildcard socket and prefix overlap relations | infrastructure-services.md:102-115 | sound | none |
| Refusal names holder and field | infrastructure-services.md:119-148 | sound | none |
| Interrupted-registration release | state-reconciliation.md:128-140 | sound | none |
| External libvirt attachment claims nothing | substrates.md:135-137, 236-240 | unsafe | [R2-06-01] |
| Abandoning deletion releases exclusive keys | contexts.md:833-838; state-reconciliation.md:270-274 | contradicted ("only a completed destroy releases") | [R2-06-02], [R2-06-07] |
| `bmc:` key identity | substrates.md:584-586 | ambiguous | [R2-06-03] |
| SSH-placed effects | infrastructure-services.md:89-91 | design gap (B247) | [R2-06-06] |
| 256-entry host bound | controller-record.md:89-91 | gap: no refusal or remedy | [R2-06-08] |

### 8.3 What is good and should be kept

The general rule set is strong. The seven Mutation safety bullets (`specs/state-reconciliation.md:1811-1827`) are close to the standard in 8.1 word for word. Most domain pages honour "unknown is never absence" explicitly: libvirt observations, KubeVirt removal, the physical claim, Redfish reads and the delivered-key channel all treat silence as unknown.

The opt-in physical logical overwrite (B70) is the best destructive contract in the corpus. Each target carries `deviceName`, `wwn`, `serialNumber` and exact `sizeBytes` (`specs/state-reconciliation.md:1933-1934`). The worker opens every target exclusively, rechecks identity during write and readback, and binds its receipt to the original apply identity "so reuse of a context name cannot adopt an old receipt" (`specs/state-reconciliation.md:1981-1982`). Cancellation never reads as completion. Other destructive paths should copy this shape.

The physical-disk contract is honest about its limit. It calls the gap between the last controller proof and the installer's first write "a residual race, not an ownership proof" (`specs/state-reconciliation.md:1915-1916`), and refuses to let any token or claim stand in for a proof. Quiescence is separated from authorization, and stopping is never a side effect of removal.

KubeVirt deletion with `preconditions.uid` and a positive `NotFound` is a model ownership proof (`specs/substrates.md:688-696`). Ceph OSD selection by `/dev/disk/by-id/wwn-0x` only, with live exclusion of root, mounted, swap and foreign devices, is sound (`specs/storage.md:702-761`).

The transition re-proof is the strongest reliability rule in this part. Every transition re-proves, under the exclusive lock, the same operation, record, plan digest and block records it planned from (`specs/state-reconciliation.md:1558-1580`). This closes the race between prompt and lease.

On security, the process boundary (`specs/security.md:164-227`) forbids a shell, allowlists argv and environment, passes Ansible variables as data only, and is backed by collection-wide structural tests. The controller runtime lock pins every wheel by URL, size and SHA-256 with `--require-hashes` (`specs/controller.md:87-160`). Capability URLs are minted per attempt and never appear in a request, evidence or log (`specs/infrastructure-services.md:400-411`). The listener table states clients, transport, privilege and client restriction per listener (`specs/security.md:479-485`), and firewall admission refuses before registration.

Secret ingress and binding are precise. `secret set` is the only opener of an operator path, standard input is refused under sudo I/O logging, bindings pin whole versions, and lent views copy nothing (`specs/secrets.md:52-214, 276-277`). Produced-material custody captures before `done`, records a failed publication as `unknown`, and orders the kubeconfig hand-over so it is never in neither place (D124, `specs/state-reconciliation.md:605-653`). The cephadm registry broker, the RHSM stdin route and the KubeVirt kubeconfig restrictions are exemplary hops.

The reservation model is sound at its core. Claims live in the same atomic controller record as host identity and bindings, keys are typed, and two relations (wildcard socket, prefix overlap) carry real semantics. Refusals name the holder and the field to change.

### 8.4 Safety lens

#### High: the managed-OS installer erases a disk chosen by kernel name

The managed-OS physical install confines erasure to "the disk `rootDeviceHints.deviceName` names" and refuses any other hint beside it (`specs/managed-os.md:1268-1273`). The `%pre` check proves only that the device "must resolve to a whole block device" (`specs/managed-os.md:1277-1280`). `/dev/sda` is admitted [F10-01].

Scenario: kernel names follow driver probe order and move with firmware, hot-added disks or a BMC virtual USB device. A Machine installed through `/dev/sda` is re-applied; `/dev/sda` is now a data disk holding a surviving OSD. Every controller proof passes, `clearpart` wipes the wrong disk, and `data-loss` acknowledged the root disk only. The operator cannot add `wwn` or `serialNumber` as a cross-check, because this path refuses them, while OSDs, the erase worker and the agent installer all use durable identity. The security-safety skill repeats the rule ("A root disk is named by a device path", `.agents/skills/security-safety/SKILL.md:19-21`).

Smallest correction: on baremetal managed-OS installs, require `deviceName` under `/dev/disk/by-id/` in the `wwn-0x` form that `specs/storage.md:702-703` already uses, and have `%pre` refuse anything else. The stronger follow-up admits `wwn` and `serialNumber` beside `deviceName` and compares them in `%pre`, failing closed on mismatch or an unreadable attribute. Update the skill bullet and name a negative test for a `/dev/sdX` value. D177 already uses this identity for erase targets.

#### High: name-derived ownership on the libvirt arm

The Mutation safety page says a name never proves ownership. Sixty lines earlier it says an object "an abandoned context of the same name left on a host reads as this context's" (`specs/state-reconciliation.md:1755-1756`). The libvirt domain UUID is derived from context and Machine names (`specs/substrates.md:320-321`) [F10-03].

Scenario: an operator runs `context delete --purge --allow-orphans`, then `context init` with the same name. Alternatively, two controllers with same-named contexts share one libvirt host. The new context's apply finds the abandoned domain "owned", its virtual installation (no `data-loss` on apply) runs `clearpart` over the abandoned guest's disk, and its destroy deletes the other controller's VMs under its own token. D155 orders the B568 interim orphan warning first; no spec page names B568.

Smallest correction, spec-only now: add a known-exception line under Mutation safety naming libvirt, emulated BMC, managed-service units and artifact servers, linked to B568. Cite B568 at 1755-1756, `specs/substrates.md:367-369` and `specs/infrastructure-services.md:323-324`. Add D155's hazard note to `specs/contexts.md` permanent deletion. Changing what the orphan confirmation prints is a CLI contract change and belongs to the named B568 slice.

#### High: a managed bridge is shared without a claim

An `external` libvirt attachment "is never defined, changed or removed" and publishes no reservation (`specs/substrates.md:135-137, 236-240`). The owner of a managed network derives its quiescence from its own Machines (`specs/substrates.md:230-233`) [R2-06-01].

Scenario: context B declares `management: external` on context A's managed bridge and takes addresses in A's prefix. Nothing refuses it. A's destroy stops and undefines the network. B's running guests lose their uplink at once, its shut-off guests can no longer start, and B's settled apply still reads done. `specs/api/container-clusters.md:253-255` also asserts that "no address in it is shared across contexts", which is false.

Smallest correction: add a Not-yet-met line linking B567 at `specs/substrates.md:230-233`, and narrow the container-clusters sentence now. Record a shared `bridge-use:<name>` key, modelled on the `media:` shared class, as B567's reservation-layer fix: a removal releasing `bridge:<name>` refuses `controller.conflict` while another context holds `bridge-use:<name>`. This also covers shut-off guests that B567's live-domain probe misses. D155 already requires the guard before further shared-host destroy.

#### High: storage retirement can destroy a surviving consumer's storage

A StorageExport "retires with its cluster when every host is a Bootwright-owned virtual Machine the same destroy removes" (`specs/storage-resources.md:1252-1255`). Its quiescence "derives from their stop proof" (`specs/state-reconciliation.md:1838-1839`). A cluster on physical nodes "keeps running after its context is destroyed" (`specs/container-clusters.md:879-881`) [R2-02-01] [F10-04].

Scenario: one context holds Ceph on libvirt Machines and OpenShift on xFusion bare metal; the fdf add-on package consumes the Ceph StorageExport. Destroy retires the add-on with no effect, the physical nodes need no stop proof, and Ceph's VMs are stopped and deleted under `data-loss`. Running workloads on the surviving cluster now have PVCs pointing at a deleted cluster. The plan's impacts never named them. This breaks the page's own rule that "quiescence independently proves nothing is using the data" (`specs/state-reconciliation.md:1896-1897`).

Smallest correction: add a row to the existing refusal table in `specs/storage-resources.md`. Retirement-with-hosts applies only when every consumer of the cluster's exports is retired in the same removal on owned virtual Machines. Otherwise the export's removal refuses before registration, naming the consumer cluster and a remedy. The RBD-replication rule at `specs/storage-resources.md:279-281` is the precedent. The fdf row in `specs/add-on-coverage.md:44` changes with it. See 8.8 for the cost of this refusal.

#### High: rotating an access credential strands removal

"A fresh removal uses the secret material its apply bound" (`specs/state-reconciliation.md:1482-1483`), and no re-binding exists (1521-1524). Bounded power commands read the current version (`specs/secrets.md:295-299`) [R2-04-01].

Scenario: a BMC password rotates under a 90-day policy. The operator runs `secret set`. `machine stop` now works because it reads the current version, but every continuation and destroy authenticates with the old bound password and fails. The named exits are restoring an old keyring, which does not help, or an orphan-acknowledged delete, which abandons VMs, disks and reservations. The same applies to an expired KubeVirt token and a changed vCenter password.

Smallest correction: split Secret consumption into two classes in `specs/secrets.md`. Effect material (placed or derived into the effect) keeps today's rule. Access credentials (presented to an operator-owned endpoint whose ownership proof does not depend on the credential) may be rebound to the current version under an explicit, authorized and recorded step. Ownership proofs (UIDs, pins, host keys) stay unchanged. Each consumer field declares its class once. This adds an exit and leaves D37 intact.

#### Medium safety items

- **Day-2 power on unproved targets.** "A Machine with no pin is compared with nothing" (`specs/substrates.md:830-831`). Before the first apply or after a destroy, `machine stop --force` cuts power to whatever ComputerSystem answers at the address. Nothing refuses a power command on the environment's controller Machine [F10-02]. Correction: without a pin, run the full target proof; refuse a Machine that declares no MACs; refuse power on the controller Machine or on a reported UUID equal to the host's own; extend controller-host protection beyond apply and destroy.
- **MAC predicate.** "Complete live MAC set matches" is equality in the general rule and erase contract, subset in the target proof and `%pre` [F10-05]. Define one predicate, state which Redfish members count (exclude host-interface NICs), and confirm erase compares against the recorded original set.
- **`bmc:` keyed by spelling.** A DNS name and an IP literal produce two keys for one server; both proofs pass on the same UUID [R2-06-03]. Fill the default port per scheme in the key and add `bmc-system:<UUID>` published with the proof, before media insertion.
- **Host routes.** A managed prefix or bridge name is never checked against the controller host's own routes and links [R2-06-05]. Libvirt refuses some IPv4 collisions late; IPv6 overlap is not covered. Add one probe sentence before net-define, refusing `lifecycle.state` with the field to change.
- **SSH-placed effects.** "Two contexts targeting one SSH host are not coordinated" (`specs/infrastructure-services.md:89-91`), although one store holds both contexts [R2-06-06]. Qualify every SSH-placed key by the existing host token and decide B247 or a new parked item.
- **KubeVirt parity.** See 8.8 [R2-02-03].

#### Low safety items

[F10-06] `--authorize data-loss --yes` copied into a runbook authorizes any later plan that needs data-loss; bind the token to a digest of the consuming steps when `--yes` suppresses review, or correct the cli.md promise. [F10-07] vSphere names no marker fields; fold the vCenter and VM `instanceUuid` into B99 and link the general rule. [F10-08] The add-on mark is computable from public inputs; record how ownership was established (created, uid-matched, mark-recovered). [R2-06-02] Abandoning deletion frees exclusive keys while objects keep running; make the orphan confirmation name the keys it frees and warn that a powered-off abandoned machine can be reinstalled; a tombstone holder is an owner option that would amend D38. [R2-06-04] The emulated BMC publishes no `bmc:` key, so two contexts can drive one guest. [R2-06-07] Release paths are listed four ways. [R2-06-08] The 256-entry host bound has no refusal.

### 8.5 Security lens

The attacker model is precise for the controller host: an unprivileged local account and a network adversary toward publishers, with root trusted (`specs/security.md:39-42`, D152). The findings below are attacker actions the written rules do not stop.

| Attacker action | What happens | Finding | Smallest correction |
| --- | --- | --- | --- |
| Compromised BMC or managed host reports positive absence | Destroy releases binding, reservations and custody while the resource lives | [F11-01] | One "Remote principals" paragraph: each is trusted only for its own resources over the verified channel; link existing lent-material clauses |
| MITM at enrollment while a script runs `machine trust --yes` | Attacker key pinned; fleet identity and registration material flow to it | [F11-03] | Name `--yes` enrollment as accepted TOFU in security.md, or take `<machine>=SHA256:<fp>` under `--yes` and `--replace` |
| Compromised mirror at first tool download | Its digest becomes the pin for `oc`, `openshift-install` and others run as root | [F11-02] | Name per tool the publisher checksum metadata and whether a signature is verified; record TLS-only cases as a named widening |
| Local account creates 257 matching names in `/var/tmp` | Every lifecycle run's sweep refuses; apply and destroy denied | [F11-07] | Count only root-owned private directories; carry the move under a root 0700 parent in B389 |
| Local account reads `/proc/<pid>/cmdline` or sudo's log | Learns the Secret username and any presigned `--from-url` query | [F11-08] | Declare the username non-secret or read it from a file; take a signed URL off argv or name the exception |
| Second sudo-admitted operator | Reveals or abandons another team's context; nothing attributes it | [F11-05] | Define "context-scoped authority" as the keyring, not an access control; optionally record the invoker in `operation.json` |
| Operator wants a narrow sudoers rule | Procfs re-execution makes it ALL-equivalent | [F11-04] | Re-execute by the proved root-owned absolute path; keep procfs only for the invoking account's build output |

Three Low items are wording that misleads a reviewer. The served private tree uses 0640 files although security.md claims one mode exception [F11-10]. "Authenticated" persisted state is undefined for unMACed records [F11-11]. The proxy route to children is stated three ways [F11-12]. The keyring encryption names no threat it defends, since keys sit beside the ciphertext [F11-09]; one sentence naming the partial-copy scenario fixes it.

Listener exposure was considered: the default wildcard `bindAddress` for managed services was raised and refuted as a recorded owner decision with a tracked backlog item (F04-03, considered and refuted). The emulated BMC's missing loopback refusal is Not yet met under B334 and is not a defect of the rule.

### 8.6 Secrets custody beyond binding

The ingress and binding hops are sound. The weaknesses sit at both ends: adapter delivery and native-side retention.

- **Two meanings of custody.** The encrypted keyring is owned by `specs/secrets.md`. Plaintext 0600 retention files on Ceph hosts and the controller are re-specified per feature, with no shared removal or orphan rule [R2-04-03]. Add one "native-side retention" section to secrets.md and link the storage pages to it.
- **Reveal list.** `specs/storage-resources.md:1321-1322` says no command reveals object-user keys; `specs/cli.md:1173-1175` says `cluster info --secrets` does; secrets.md lists neither [R2-04-02]. Make secrets.md name every reveal command. D143 does not cover the object-user reveal, so the owner confirms it.
- **"Unproved".** The custody mark (D124) and dashboard reachability share one word and one warning code [R2-04-04]. Emit `access.unproved` only for the custody mark.
- **Adapter channel.** "Material values" are undefined; only RHSM is forbidden from Ansible variables, and the shipped filter example uses `set_fact` on a pull secret [R2-04-05]. Module payloads carrying passwords land in remote temp files [R2-04-07]. One sentence each in security.md fixes the contract: material values are non-secret, secret parts arrive only as lent files, and remote tasks receiving material use pipelining.
- **Installer work areas** keep substituted credentials and the kubeadmin password at a path a recreated same-name context reaches [R2-04-06]. Scope secrets.md:331-332 to the keyring and refuse a work area the current incarnation did not create.
- **Produced inputs** are re-read from custody each attempt [R2-04-08]. Generalize the existing storage rule: a recapture keeps the effect's identity or refuses as drift.

### 8.7 Reservations and locking across contexts

The model coordinates claims that contexts publish. It fails where a real sharing publishes no claim: an external bridge [R2-06-01], an emulated BMC [R2-06-04], a second BMC spelling [R2-06-03], the host's own routes [R2-06-05] and every SSH-placed effect [R2-06-06]. It also releases claims while the claimant's objects still run [R2-06-02].

The release paths are stated differently on four pages, and `specs/contexts/controller-record.md:81` says Workspace compares keys "without interpreting a key's meaning", while code applies overlap relations [R2-06-07]. One release table in `specs/infrastructure-services.md#host-reservations` and one sentence saying the prerequisites port owns the conflict relation would let an agent add a key class correctly.

### 8.8 Immediate portfolio: KubeVirt parity and the Fusion Data Foundation removal order

#### KubeVirt safety parity

KubeVirt is truthfully refused today (`specs/substrates.md:50-51, 698-699`, B87). The question is whether its written contract reaches the same proof class as libvirt and bare metal before B87 lands.

| Proof | libvirt | Bare metal | KubeVirt (written) |
| --- | --- | --- | --- |
| Exact target before effect | metadata and UUID | UUID pin, MACs, off | recorded UID; positive `NotFound` of the instance |
| Ownership marker content | stated | claim only, by design | "ownership labels", content unstated [R2-02-03] |
| Lost create reply | n/a | n/a | no rule [R2-02-03] |
| Cross-context reservation | `libvirt-domain:` | `bmc:` | none [R2-02-03] |
| Deletion | stop first, data-loss | erase opt-in | `preconditions.uid`, data-loss: sound |
| Day-2 stop for quiescence | defined | defined | unreachable: power commands are Redfish-only [F18-07] |
| Credential rotation | n/a | strands removal | strands removal [R2-04-01] |

Deletion is at parity. Identity before creation is not. The smallest correction adds B87 open question 11: the owner label carries the add-on operation-and-block mark, a lost-reply object is owned only when its mark matches and its `creationTimestamp` is not before registration, and a reservation key `kubevirt-vm:<server>/<namespace>/<name>` serializes contexts of one controller. Pinning the host cluster's identity is optional; the kubeconfig already pins server and CA. The day-2 stop path [F18-07] and the rotation rule [R2-04-01] must be settled in the same slice, or KubeVirt removal cannot reach quiescence or authenticate after a token expires.

#### Fusion Data Foundation removal order

The fdf path from StorageExport to package is close to end to end: one consumer per export, keys in custody before the native add, details delivered only through an owned Secret, and stated readiness and handoff evidence. The removal order is the gap.

Today's written order for one context:

```text
1. retire the fdf ClusterAddon instance      (no remote effect)
2. consumer ContainerCluster                  (physical: keeps running)
3. StorageExport retires with Ceph            (quiescence = Ceph VM stop proof)
4. Ceph host Machines stopped and deleted     (data-loss)
```

Step 3 accepts the storage hosts' stop as proof that nothing uses the data, while step 2 left the consumer running [R2-02-01] [F10-04]. The order is safe only when the consumer cluster is itself on owned virtual Machines removed in the same destroy.

Recommended rule, placed in the existing refusal table at `specs/storage-resources.md:1172`:

```text
A StorageExport retires with its Ceph cluster only when every ClusterAddon
instance consuming it runs on a ContainerCluster whose nodes are owned
virtual Machines the same removal stops and deletes. Otherwise the removal
refuses before registration, naming the consuming cluster and the export,
with remedy: remove the consumer first, or wait for B491 consumer quiescence.
```

The cost is real: a mixed context (virtual Ceph, physical OpenShift) has no full destroy until B491 defines consumer quiescence. The calibrator's alternative is a named impact plus a distinct acknowledgement naming the consumer. The owner should choose between refusal and acknowledged impact. Either choice should be flagged at plan time, so the operator learns the topology is not destroyable before the first apply, not at destroy.

### 8.9 Corrections in order

| Order | Change | Findings | Kind of change |
| --- | --- | --- | --- |
| 1 | Durable root-disk identity on baremetal managed-OS installs | [F10-01] | spec, skill, test |
| 2 | Export retirement refuses with a surviving consumer, or names it as an acknowledged impact | [R2-02-01], [F10-04] | spec; owner choice |
| 3 | Link B567 and B568; narrow the false sharing claims | [R2-06-01], [F10-03] | spec only |
| 4 | Access-credential class with an authorized rebind | [R2-04-01] | spec, then slice |
| 5 | Power commands: proof when unpinned, controller excluded | [F10-02] | spec, test |
| 6 | Remote principals paragraph; enrollment TOFU stated; tool authenticity per tool | [F11-01], [F11-03], [F11-02] | spec |
| 7 | One MAC predicate; `bmc-system:<UUID>` key | [F10-05], [R2-06-03] | spec, test |
| 8 | Secrets: native retention owner, reveal list, adapter channel | [R2-04-03], [R2-04-02], [R2-04-05] | spec |
| 9 | KubeVirt question 11 in B87 | [R2-02-03], [F18-07] | backlog |
| 10 | Sweep bound counts only owned entries | [F11-07] | spec, code, test |

## 8. Reconciliation rule audit (part B): lifecycle, continuation, comparison and durability

This part audits the rules that move an operation through its life: the lifecycle unit, the state machine, attempts and resolutions, stages, continuation under the same and another build, removal, the desired-versus-real comparison, GitOps readiness, durable formats, locking, setup and the controller runtime, and permanent deletion. The owning pages are `specs/state-reconciliation.md` (lines 1-1809 read in full), `specs/contexts.md` and `specs/controller.md`, with `specs/contexts/controller-record.md` for the setup subtree. Part A covers mutation safety, confirmation, authorization, secrets custody and the trust model.

### 8.B1 Standard and overall verdict

The standard is a crash-consistent, fail-closed engine for one-shot infrastructure bootstrap, judged as one would judge a package manager's transaction database or Terraform's locked local state. Every durable write must precede the effect it permits. Every state must have a named exit that does not needlessly abandon live effects. One event must get one answer across pages. Waits and retries must be bounded. Nothing may depend on information a crash or a reboot can erase. Two implementers reading the text must derive the same transition code.

**What is good and should be kept.** The core of this contract is unusually strong, and the owner should protect it from erosion.

- The frozen-plan executor. Plans are immutable, one operation is current per context, and destroy inverts the apply's frozen plan rather than re-deriving it from desired state (state-reconciliation.md:1329-1337). This removes the whole class of "the reconciler forgot what it created" failures.
- Write-ahead discipline at every boundary checked. `running` is durable before the first side effect (state-reconciliation.md:1119-1120). The attempt record is created exclusively before the block record counts it (:1131-1136). Resolution identity and log exist before any observation (:1194-1204). Mutation evidence is raised before a binding, claim or registration (:169-187).
- Unknown is treated as unknown. Nothing moves an unproved block except a read-only, capability-owned observation; confirmation, authorization, elapsed time and a dead executor cannot (:1223-1227). A lost executor's `running` block reads exactly as `unknown` (:708-714).
- The exclusive-lock re-proof of every member the shared-lock decision read is a correct compare-and-swap (:1558-1580). It closes the decide-then-act race without long exclusive holds.
- Bounded retries. Each block is retried at most once per invocation, and never one the invocation failed itself (:1312-1316, :900-901). This prevents tight retry loops while leaving recovery in the operator's hands.
- Produced-material custody is crash-safe in both directions. Capture precedes `done`, a failed publication records `unknown` rather than `failed`, and an unproved copy is published before the inverse that deletes the installer's copy (:605-653).
- The cross-build continuation design. The execution closure is exactly the runtime lock digest plus the Python ABI (:1042-1056), and the content digest is a contract identity rather than a hash of automation bytes (:1084-1088). This is what lets an `ansible/`-only fix reach a live operation, which the owner's live runs showed is worth the complexity.
- Spec-as-test. `TestTransitionTablesMatchSpec` drives the transition code with every row of the state tables (:500-502), which is a strong anti-drift device.
- Crash-safe publication on the store side: stage beside the target, prove the destination, no-replace rename, sync the parent (contexts.md:436-445), with stage collection tied to the lock that excludes live writers.
- The comparison model is honest. It has three named sides, says "no host was contacted" for the records-only reading, and states "unknown is never absent" as a fold rule (state-reconciliation.md:1722-1727).
- Permanent deletion demands positive disposal proof and an explicit orphan acknowledgement, with a named exit for each kind of unverifiable entry (contexts.md:760-857).

**What needs improvement.** Several rules give two answers to one event, and the state-machine table itself disagrees with the decision it encodes [F09-05]. The owning page for the registry contradicts itself and the code [F13-01]. The readiness predicate, the `diff` Input line and `status` do not agree on when an environment is ready or what the next command is [F12-01] [F12-07] [F12-09]. The operation state is computed from in-memory block states, which manufactures legal record shapes that every reader must special-case [F09-06].

**What is bad.** Five rules leave an operator at a dead end, or with only an exit that abandons every live effect of the context: one unreachable target [F09-01], a removal reserve the apply may spend [F09-02], a lost block record under a completed apply [F09-03], a controller identity change [F13-06], and a pending setup intent that blocks the whole host [F13-04]. A removal under another build never checks the content digest, so a moved place or mark can make it record positive absence over effects that still exist [R2-03-01]. The production store root may be tmpfs, which would erase ownership evidence on reboot [F13-07].

### 8.B2 Rule-by-rule table

Abbreviations: SR is `specs/state-reconciliation.md`, CX is `specs/contexts.md`, CT is `specs/controller.md`, CR is `specs/contexts/controller-record.md`. "Sound" means the rule is correct, consistent and implementable as written.

| Rule | Owner page:lines | Verdict | Findings |
| --- | --- | --- | --- |
| **Lifecycle unit** | | | |
| Whole-unit operations; stage selection gates, never narrows | SR:6-14 | sound | none |
| Three modes; no reconciliation, adoption or force path | SR:16-29 | sound; makes one exit too coarse | [F09-01] |
| Changed input over a completed apply refuses, names destroy | SR:28-34; CX:155-162 | sound rule; undo trap until B598 | [F12-06] |
| Settled verbs are no-ops proved by records | SR:35-57 | sound; "nothing left to do" overclaims | [F12-05] |
| Completed destroy holding a non-done block: deletion exit named | SR:57-70 | sound | none |
| Finalization of all-done operations; own-verb rule | SR:72-112 | sound core; legacy lag cases | [F09-06] [F09-08] |
| State no index accounts for: deletion exit named | SR:114-126 | sound | none |
| Interrupted registration released and restored | SR:128-144, 189-220 | sound | none |
| **Mutation evidence and identities** | | | |
| Evidence raised before binding, claim, registration; directory re-proof | SR:150-187 | sound | none |
| Reclaim of claims that hold nothing | SR:222-252 | sound | none |
| Guard and orphan acknowledgement | SR:254-274 | sound | none |
| Identity allocation, 16 bounded retries, number exhaustion refuses | SR:313-346 | sound | none |
| **Operation records** | | | |
| Closed, canonical, bounded records; exclusive creation | SR:348-452 | sound | none |
| Optional `build` and `replaces` members with no version move | SR:388-414 | ambiguous | [R2-03-05] |
| **State machine rows** | | | |
| No operation or completed destroy; unaccounted state; completed destroy with non-done block | SR:458-460 | sound | none |
| apply running, failed, paused | SR:461-464 | sound | none |
| apply failed, every block done | SR:463 | sound; exists for legacy records | [F09-06] [F09-08] |
| apply unknown; destroy unknown | SR:465, 469 | contradicted by SR:557-562 | [F09-05] |
| apply done | SR:466 | sound; dead end with a lost record | [F09-03] |
| destroy running; destroy failed | SR:467-468 | sound | none |
| **Attempts, resolutions, transitions** | | | |
| Attempt outcomes table | SR:504-527 | sound | none |
| Resolution outcomes; apply observation then removal check (D119, D123) | SR:529-603 | sound | none |
| Diagnostic: superseding removal "refuses until the target is restored" | SR:1254-1256 | contradicted by SR:579-582 | [F09-04] |
| Produced material custody | SR:605-666 | sound | none |
| Block transitions | SR:668-714 | sound; start-failure exception | [F09-06] |
| Operation state precedence | SR:716-727 | ambiguous (held vs durable states) | [F09-06] |
| Admission after failure, cancellation, unknown outcome | SR:892-907, 1223, 1320-1327 | ambiguous for unknown | [F09-07] |
| **Plan, stages and pause** | | | |
| Waves, plan bounds, resources not shared | SR:729-804 | sound | none |
| Stages; engine-owned controller edge | SR:806-843 | sound | none |
| Concurrency bound belongs to the executable, never frozen | SR:852-857 | sound | none |
| Pause boundary; pause is never cancellation | SR:885-890 | sound | none |
| Plan previews exactly the verb's decision | SR:970-1003 | sound | none |
| Implementation resolved once; "drifted implementation refuses" | SR:1014-1027 | ambiguous for removals | [R2-03-01] |
| **Continuation and content digests** | | | |
| Execution closure = lock digest + ABI | SR:1042-1056 | sound | none |
| Closure refusal "names the build ... whose runtime this host keeps" | SR:1058-1063; CT:814-817 | contradicted by CT:870-888 | [F13-05] |
| Apply continuation under another build compares blocks incl. digest | SR:1064-1071 | sound | none |
| Destroy continuation and fresh removal under the build in hand | SR:496-498, 1031-1035, 1071-1076 | unsafe where identity values live outside the request | [R2-03-01] |
| Content digest from the declared contract; "effect pins" | SR:1084-1088 | ambiguous (term undefined) | [R2-03-06] |
| Digest moves when meaning changes; mechanical guard | SR:1466-1480 | sound guard, partial coverage; no procedural owner | [R2-03-02] [R2-03-03] |
| Request versioning, no conversion | SR:1452-1464 | sound | none |
| **Unknown outcomes and recovery** | | | |
| Attempt start publication and narrow adoption | SR:1131-1154 | sound | none |
| Converging an effect | SR:1162-1179 | sound | none |
| Outcome written under an uncancellable boundary; resolution identity first | SR:1181-1227 | sound | none |
| Log fault sticky; restoration proved | SR:1284-1308 | sound | none |
| Continuation retries each failed block once per invocation | SR:1312-1327 | sound | none |
| **Removal and dependency safety** | | | |
| Ownership = every started block; removal from the frozen plan | SR:1329-1356 | sound | none |
| Removal resolves first; refuses whole if any block stays unproved | SR:586-588, 1229-1243 | unsafe exit | [F09-01] |
| Completed apply with a non-done block refuses removal | SR:1358-1368, 1431-1434 | unsafe exit (none named) | [F09-03] |
| Contradiction rules for an incomplete apply | SR:1370-1429 | sound core; legacy carve-outs | [F09-06] [F09-08] |
| Removal reopens the apply's binding; lost-binding exits | SR:1482-1532 | sound | none |
| Inverted dependency graph | SR:1534-1540 | sound | none |
| Running or unknown removal continued; failed one superseded | SR:1542-1556 | sound | none |
| Exclusive-lock re-proof before every transition | SR:1558-1580 | sound | none |
| **Comparison, plan and diff** | | | |
| Comparison is a read; three sides; canonical form | SR:1585-1645 | sound | none |
| Records-only reading under a removal | SR:1616-1629; cli/output.md:686-703 | contradicted by SR:1726-1727 | [F12-02] |
| Equal, settled and drift vocabulary | SR:35-40, 1631-1645; cli.md:510-517 | ambiguous ("drift" undefined) | [F12-05] |
| Live observation: boundary, material, re-proof, fold | SR:1647-1727 | sound | none |
| Projection and observable fields; add-on drift | SR:1729-1766; add-ons.md:369-379 | ambiguous (own but unequal) | [F12-03] |
| `diff` Input line names `apply` continues or settles | cli.md:732-734 | contradicted by cli.md:661-665 | [F12-09] |
| **GitOps readiness** | | | |
| Readiness predicate over the apply's records | SR:1768-1786 | gap: ignores the selected revision | [F12-01] |
| Predicate bullets vs status reasons | SR:1771-1778, 1799-1807 | ambiguous | [F12-07] |
| Per-cluster bootstrap object "after every block that reaches its cluster" | SR:1788-1794; add-ons.md:73-76, 131 | ambiguous (stage vs relation) | [F12-04] |
| **Durable formats** | | | |
| Registry format, record table, descriptor owner | CX:186-205, 302-310 | contradicted (says 5; code writes 6) | [F13-01] |
| Pre-1.0 rule: refuse, never convert, name the remedy | project.md:95-96; CX:742-749 | sound rule; one remedy not actionable | [F13-03] |
| Supported filesystems for the store root | CX:502-508 | unsafe for the production root | [F13-07] |
| Retained-operation and operation-area bounds | CX:560-633 | unsafe (apply may spend the removal reserve) | [F09-02] |
| **Locking** | | | |
| Two advisory locks, never waited for or taken over | CX:318-356 | sound for long mutators; gap mid-operation | [F13-02] |
| Context update and delete prompt with locks held | CX:877-882 | unsafe | [F13-02] |
| Media and secret commands confirm first, re-prove under lock | CX:346-351 | sound | none |
| **Setup and controller runtime** | | | |
| `setup/lock`; launch holds runtime `.lock` shared; non-blocking retirement | CT:799-802, 870-880 | sound | none |
| Pending setup intent blocks every consumer | CT:835-849 | unsafe (host-wide availability) | [F13-04] |
| Retention keeps current and previous runtime | CT:870-888 | sound default; refusal wording contradicted | [F13-05] |
| Host identity tuple; never silently rebind | CT:759-792; CX:84-109 | sound detection; only an abandoning exit | [F13-06] |
| Tool-source identities append-only, 4096 bound | CR:89-105, 158-161 | gap: refusal names no remedy | [F13-08] |
| **Permanent deletion** | | | |
| Disposal proof, orphan acknowledgement, named exits | CX:760-857; SR:259-274 | sound | none |
| Name is identity; a recreated name is a new context | CX:16-23 | accepted risk, pending B568 | none |

Two candidates were considered and refuted. "Freezes the same blocks" meaning different things per verb [R2-03-04] is already defined per verb at SR:1068-1076; the real gap is the digest one in [R2-03-01]. "Authored desired state is the source of truth" conflicting with GitOps control after handoff [F12-10] was refuted on arbitration. The predicate already says it transfers no ownership and authorizes no GitOps mutation of Bootwright-owned resources (SR:1782-1784).

### 8.B3 Reliability: dead ends, waits and crash boundaries

#### Dead ends

**One unreachable target abandons the whole context [F09-01].** A removal resolves every unproved block before it registers, and refuses whole when one stays `unknown` (SR:586-588, 1240-1243). The only named exit is deletion with `--allow-orphans`, which abandons every effect of the context (SR:1279-1282). A dead BMC or a decommissioned hypervisor host is an ordinary event in the owner's bare-metal portfolio. One such block then leaves VMs on healthy hosts, DNS, NTP and Ceph running unmanaged. This reverses D38, so it is a decision request, not a defect fix.

Smallest correction, for the owner to decide: a fresh destroy may take an explicit, confirmed `--abandon <block-id>` for a block that stays `unknown`. The removal records that block abandoned (never `done`), keeps every ancestor of it in the plan DAG and its host reservations, and removes the rest in inverted order. The abandoned block's ownership markers must stay recorded so the B568 same-name guard covers them, so this waits for B568.

**The apply may spend the reserve its removal needs [F09-02].** contexts.md reserves free entries and bytes so the removal of the last admitted apply can register. It then accepts that "that removal therefore refuses at its registration only once the apply's own later attempts used them up" (CX:575-578). A block retried across many continuations draws down that reserve; the destroy then refuses with every effect live. Smallest correction, folded into B572's fix: reserve the removal's first pass, its registration bytes and its restoration write for the removal alone. An apply attempt, retry or resolution refuses `lifecycle.state` before its effect when its write set would cut into that reserve, naming `bootwright destroy --context <name>`. Delete the accepting sentence and rename `TestADestroyTheAreaCannotHoldRefusesAtItsRegistration`.

**A completed apply with a lost block record [F09-03].** Both verbs refuse while any block of a completed apply is not `done` (SR:1358-1364). The refusal "points at `bootwright status`", which offers no next step for this state (cli.md:634-660). Smallest correction: add the completed-apply case to the deletion-exit exception at SR:1431-1434, to row SR:466 and to cli.md's completed-operation step, naming `context delete --purge --allow-orphans` and what it abandons. Removing the whole frozen plan regardless of records is a larger owner decision; if adopted, a lost record of a completed apply implies `done`, not unproved.

**Controller identity change [F13-06].** The identity tuple is machine-id, DMI product UUID and root filesystem UUID (CT:762-764). Recreating the bastion VM on another hypervisor or replacing a mainboard changes it, and every verb of every context refuses. The only remedy that the spec names, other than returning to the original host, is the orphan-acknowledged deletion. Smallest correction: the mismatch refusal lists, in order, returning to the original host or restoring its identity values, destroying from the original host before a planned change, and the orphan-acknowledged purge with what it abandons. Park an explicit, root-confirmed "rebind controller identity" item with its own reason, separate from B103.

**Edited input over a completed apply [F12-06].** A `context update` with changed input only warns (CX:160-162). Restoring the original files creates a new revision that still refuses until B598 lands. After GitOps handoff the only named way on is a full destroy. Smallest correction: the warning states that restoring the original files does not restore settlement until B598, and links B598.

**Tool-source identities [F13-08].** Source identities are append-only with a 4096 bound, and the refusal says no command frees one (CR:103-105). Smallest correction: the refusal names the fresh-install exit. Retiring sources with their last area is an optional change that needs a security review, because append-only is what detects "same ID, different bytes".

#### Waits

The spec deliberately has no unbounded lock wait: "no lock or lease is ever waited for or taken over" (CX:349-351). That choice is sound for long mutators, and it should stay. It leaves two gaps.

**A prompt under the root lock [F13-02].** Context update and delete prompt "while locks remain held" (CX:881-882), and both hold the exclusive root lock and lease (CX:334). An unattended prompt makes every `status`, `plan` and `context list` on the host refuse `lifecycle.lease`, and refuses the short root transactions of every other context's running apply. The page never says what that refusal does to a block mid-operation. Smallest correction: context update and delete confirm before the exclusive lock and re-prove under it, as media and secret commands already do (CX:346-351, D118). State the block outcome when an in-operation root transaction is refused: failed if no effect ran, unknown otherwise, produced material kept by the D124 rule. Naming the lock holder in `lifecycle.lease` is a separate, parked improvement.

**A pending setup intent blocks the host [F13-04].** "An unknown outcome blocks every consumer" (CT:842). A setup that fails while fetching wheels offline, before any dnf transaction, leaves the intent pending. Every apply, destroy and bounded run of every context then refuses, including continuations whose previous runtime is intact. The remedy `run bootwright setup` fails the same way offline. Smallest correction: a setup that fails at a named step before any dnf transaction began, or after dnf completed and presence was re-read, clears its intent in the failure path and records the failure in its run. Only a kill, a crash or a failure during dnf keeps the host-wide block. The refusal names both exits: this build's setup with `--wheel-mirror`, or the setup of the receipt's build.

#### Crash boundaries

The write-ahead order is right everywhere checked (8.B1). Three boundaries are weaker than the rest.

**Operation state from held states [F09-06].** The operation records "the state its blocks give it, as the invocation holds them" (SR:702-703). A start whose publication lands and then errors leaves the operation `failed` beside a `running` block (SR:695-699). Every reader then needs carve-outs: the attempt-count exception in the contradiction rules (SR:1381-1388) and the lag cases in finalization (SR:86-98). Smallest correction, folded into B569 and B575: state once that at each write the operation state is the precedence over its durable block records, or `running`. Keep one generic lag rule ("all blocks done, finalize under either verb") and delete the commit-anchored clauses.

**Tmpfs as the production root [F13-07].** The supported filesystems include tmpfs and overlayfs (CX:506-507). On a live-booted bastion, a reboot erases the registry, evidence, keyrings and custodied kubeconfigs while the clusters they own keep running. Smallest correction: refuse tmpfs and overlayfs for `/var/lib/bootwright` with `context.state` naming the filesystem type, and keep them for injected test roots only. Statfs reports only the overlay type, so "overlay on a persistent upper layer" cannot be checked and overlay should be refused outright. The setup dry run's state-root check reports the same.

**A removal under another build [R2-03-01].** A destroy's proof copies each block's content digest from the frozen plan (SR:1071-1074), and "a removal keeps the one its apply froze" (SR:496-498). So a removal never compares the digest with the build in hand. Where a value that fixes a target's place, name or ownership mark lives in a Go constant or a role literal outside the frozen request, the new build's removal looks in the new place. It records positive absence, completes, and releases reservations and the Secret binding while the old effects remain. The libvirt implementation already freezes its names and paths in the request, so the risk is in implementations that do not. Smallest correction: one clause in "Dependency safety during recovery" stating that every value fixing a removal's target identity, place, ownership mark or host claim is frozen in the apply's request and read from it. Back it with a contract-guard or architecture test that removal automation reads no such constant except through the request. Splitting the digest into an effect-identity part that removals compare is an owner option amending D145(4).

### 8.B4 Consistency: two pages, two answers

| Event | Page one says | Page two says | Smallest correction |
| --- | --- | --- | --- |
| Registry format this build writes [F13-01] | "writes private format version 5" (CX:186) and refuses version 5 (CX:192-195) | `const RegistryVersion = 6` (internal/workspace/contexts/contracts.go:24); D152 clause 9 | CX:186-189, 250, 304 say version 6; descriptor added by the first binding |
| Who publishes the controller descriptor [F13-01] | "confirmed setup adds" (CX:189) | "Setup itself writes no controller record" (CR:32) | Link CR's descriptor section |
| Setup's lock [F13-01] | `setup` under the exclusive root lock (CX:332) | "Setup's own subtree has its own lock, `setup/lock`" (CT:799-801) | Setup holds only `setup/lock`, plus one exclusive root transaction to create its subtree |
| Unknown-operation rows [F09-05] | "resolve the exact unknown block ... start no other effect or retry" (SR:465, 469) | the continuation retries what resolution leaves failed (SR:559-561, D146) | Reword both rows to D146; change the literal strings in `transitions_spec_test.go` |
| Drifted owned target under a superseding removal [F09-04] | "removed rather than refused" (SR:582) | "refuses the same way until the target is restored" (SR:1254-1256) | Delete SR:1254-1256 and substrates.md:480-483; name the removal check's reason |
| Kept runtimes [F13-05] | the host "keeps" the registering build's runtime (SR:1061-1062; CT:816-817) | every runtime but current and previous is retired (CT:876) | Say the registering build's setup restores its runtime, fetching pruned wheels |
| Next step after `diff` [F12-09] | "which bootwright apply --context <name> continues" (cli.md:732) | status offers no continuation when another build plans differently (cli.md:661-665) | Input line states the relation only and refers to `bootwright status` |
| Partly removed object [F12-02] | any removal block proved gone makes the object `pending`, read absent (cli/output.md:700-702; SR:1624) | "Unknown is never absent" (SR:1726-1727) | Absent only when every block the removal names is gone; otherwise unknown naming both sets |
| Readiness predicate vs status reasons [F12-07] | four bullets incl. "live access" and "other durable operation fault" (SR:1775-1778) | five ordered reasons, no "other fault" (SR:1799-1802) | Each condition maps to one reason; narrow to the `logFault` latch; drop "live" |
| Who the GitOps object waits for [F12-04] | "every other block that reaches its cluster" (SR:1789-1790) | add-ons-stage blocks only (add-ons.md:131) | Narrow the prose and name the excluded cross-stage blocks, pending Q60 |
| Record evolution [R2-03-05] | optional members move no record version (SR:409-414) | "adding or renaming one field" is a new request version (SR:1454-1456) | One paragraph on optional record members, citing D145(3) |
| "Effect pin" [R2-03-06] | used once (SR:1086) | defined only in a knowledge page; implementations hash different inputs | Define once: a Go-owned value outside the request that fixes what an effect is, hashed directly |
| "Drift" [F12-05] | refuse (storage.md:515), not ready (storage-resources.md:367), reported (SR:38-40) | no definition | One sentence under "Comparing desired and real state"; link the storage pages |

[F13-01] is the most consequential row, and the sibling defect [F16-02] reports the same registry version drift. contexts.md is the page `specs/index.md` routes registry questions to. An agent following it would write the wrong version, or wrap setup's dnf install in an exclusive root lock that makes every other command on the host refuse for minutes. `make docs-check` compares the bounds table with the code but not the record-version table, which is how the drift went unseen. Extending that test to each record-version cell is a sound follow-up but not part of the fix.

The duplication behind several rows is itself a finding [F13-09]. Retention appears in CT:870-890 and CR:208-212. The version-5 cut-over refusal appears on five pages. Make CR the single owner of the setup subtree's layout, publication and retention, and state the cut-over refusal once, with every other mention a link.

### 8.B5 Operability: what an operator does when a rule refuses

The standard here is the project's own rule that every refusal names an exact safe next action (CT:1010). Most refusals in these pages meet it. The retained-operation bound names a distinct exit for each evidence state (CX:612-633), and the lost-binding refusal names its only two exits (SR:1511-1513). The gaps are specific.

**Handoff readiness ignores the selected input [F12-01].** The predicate holds when the apply and every block are `done` (SR:1771-1774); no reason covers changed input. After `context update` with an edited input, `status` still reports ready while `desired` shows a revision no operation realized. A pipeline gated on `.handoff.ready` hands over an environment that does not match the selected desired state. Smallest correction, owner's choice: add the reason "the selected input revision is not the revision apply froze", after "the apply not done", naming `bootwright diff`. Alternatively, state in project.md:46-48 that readiness covers the scope the apply froze and print the frozen revision beside `ready`.

**Format refusals name a build the records do not store [F13-03].** The pre-1.0 rule is sound and consistently applied: refuse, never convert. But the corrupt-keyring remedy is only "a new context" (secrets.md:510-511). That omits that the existing context's objects are now abandoned and that the delete must be orphan-acknowledged. Smallest correction: the `secret.store.corrupt` remedy names D37's exits (restore the keyring from a complete backup, or `bootwright status` then `bootwright context delete --name <name> --purge --allow-orphans`, then init). It says the objects are abandoned and states once whether this refusal precedes the lost-binding reopen. A docs table mapping each format version to its last writing release is an optional Low improvement.

**Offline setup [F13-04]** and **identity change [F13-06]** are covered in 8.B3; in both, the refusal's remedy is one the operator often cannot carry out.

**A lock refusal names no holder [F13-02].** `lifecycle.lease` carries "a retry remedy" (CX:350). With two operators on one bastion, the second cannot tell which command holds the lock. This is the parked half of [F13-02].

**The digest duty has no procedural owner [R2-03-03].** The rule that an automation change altering what an effect is must move the content digest lives in delivered history: "the reviewer checks that on every `ansible/` change" (specs/milestones/delivered.md:4341-4342). The code-implementation skill's Ansible reference does not mention it. An agent making an `ansible/` fix is never asked. Smallest correction: one checklist line in `references/ansible.md`, and in `go.md` for capability Observe and ownership code, linking SR:1466-1480. A `Contract:` line in the commit check is optional. This is the operational half of [R2-03-02], whose own smallest correction is to restate the "backstop" honestly. Removal's ownership proof fails closed on a changed mark, so the consequence is a stuck or partial destroy. A change that loosens the proof is unguarded and must move the digest. Each transient classification in the contract golden needs a recorded reason, and the literal scan should extend to quoted `bootwright.io` label and annotation keys.

**Add-on drift [F12-03].** Pages promise that `diff --observe` reports changed add-on fields (add-ons.md:432-433), but only `machine-libvirt-v1` has a projection row (SR:1765-1766). An acceptance gate of "no drift" can pass while a field drifted. Smallest correction: classify own-but-unequal as a partial realization whose note names the differing object and first field. Park an add-on projector as a sibling of B600-B605.

### 8.B6 Writing style of the normative text

The rules are precise, and the tests prove most of them. The text that carries them is harder to use than it needs to be, and that is a reliability risk: two implementers converge only through the tests, not through the prose.

Measured facts:

- SR:1-1809 holds about 18,400 words. A rough split on sentence punctuation gives a median of 27 words per sentence and 29 passages over 60 words. Single sentences often carry three to five conditions, for example SR:42-53 and SR:1058-1068.
- Normative text cites builds by commit or era: "an executable before `4513e123`" (SR:93), "only an executable before `4513e123` left" (SR:1410-1411), "an executable before `83dcbebe` interrupted" (SR:1417), "the version 1 record a build before the execution closure was frozen wrote" (SR:441).
- The setup v2 cut-over refusal, a one-time migration fact, is restated as live contract on five pages (CX:192-201, CX:283-291, CT:899-906, CR:53-56, SR:442-444) [F13-09].
- Test names sit inline in rules: 26 in contexts.md, 11 in controller.md and 5 in state-reconciliation.md by a simple pattern count.
- Record evolution is stated member by member (`replaces` at SR:389-390, `build` at SR:409-414) rather than once [R2-03-05].

What to move out, and where:

| Material | Today | Move to | Ids |
| --- | --- | --- | --- |
| Behaviour of executables before named commits | SR:91-96, 1406-1417 | Delete under D142; the generic finalization rule at SR:72-80 and SR:471-473 already covers the state | [F09-08] |
| The "apply failed, every block done" row and lag cases | SR:463, 86-98 | One generic rule, once [F09-06] is adopted | [F09-06] [F09-08] |
| Cut-over refusal restatements | five pages | One anchor in CX's format section; links elsewhere | [F13-09] |
| Setup retention and publication | CT and CR both | CR owns; CT links | [F13-09] |
| Per-member evolution clauses | SR:389-390, 409-414 | One "optional record members" paragraph | [R2-03-05] |
| Digest-move procedure | delivered.md:4340-4343 and a knowledge page | The code-implementation skill references | [R2-03-03] |
| Meaning of "effect pin" | a knowledge page | One definition in SR, the knowledge page linking it | [R2-03-06] |

Keep the table-level test anchors such as `TestTransitionTablesMatchSpec` (SR:500-502). They make a table and its code move together. Per-sentence test names are a different matter. They cost tokens on every read and help only the reviewer who checks coverage. A reasonable rule is to keep a test name where it pins a table or a refusal golden, and move the rest to a coverage list per section. That is a recommendation of this audit, not a verified finding.

### 8.B7 Findings in this part

| Id | Severity | Title |
| --- | --- | --- |
| [F13-01] | High | contexts.md stale after setup v2: registry version, descriptor, lock owner (also [F16-02]) |
| [F09-01] | Medium | One unreachable target blocks a whole removal |
| [F09-02] | Medium | Apply retries may consume the removal's reserve |
| [F09-03] | Medium | Completed apply with a lost block record is a dead end |
| [F09-04] | Medium | Superseding removal waits for target restore, contradicting D119 |
| [F12-01] | Medium | Handoff predicate ignores the selected input revision |
| [F12-02] | Medium | Records-only diff calls a partly removed object absent |
| [F12-03] | Medium | Add-on observation leaves "owned but changed" unclassified |
| [F13-02] | Medium | Context update and delete prompt under the root lock |
| [F13-03] | Medium | Format refusals name a writing build records do not store |
| [F13-04] | Medium | A pending setup intent blocks every launch on the host |
| [F13-07] | Medium | Store root may be tmpfs or overlayfs |
| [R2-03-01] | Medium | A removal under another build never checks the content digest |
| [R2-03-02] | Medium | Cross-build preservation of marks and proofs rests on author judgement |
| [R2-03-03] | Medium | No skill or check owns moving a content digest |
| [F09-05] | Low | Unknown-operation rows forbid the retry D146 requires |
| [F09-06] | Low | Operation state recorded from in-memory block states |
| [F09-07] | Low | Admission after an unknown outcome stated only by implication |
| [F09-08] | Low | Normative rules justified by records of old executables |
| [F12-04] | Low | Last-package GitOps requirement narrower than the early-handoff promise |
| [F12-05] | Low | "Equal", "settled" and "drift" lack one definition |
| [F12-06] | Low | Edited input over a completed apply cannot be undone short of destroy |
| [F12-07] | Low | Readiness bullets and status reasons diverge |
| [F12-09] | Low | `diff` Input line promises a step status may refuse |
| [F13-05] | Low | Retention keeps one previous runtime; the refusal promises more |
| [F13-06] | Low | Controller identity change leaves only an abandoning exit |
| [F13-08] | Low | Tool-source identities append-only with an unfreeable bound |
| [F13-09] | Low | Setup-subtree rules and the cut-over restated on five pages |
| [R2-03-05] | Low | Optional record members blur the record version |
| [R2-03-06] | Low | "Effect pin" is undefined |

Four of the Medium findings ask the owner to revisit or extend a recorded decision rather than correct a slip: [F09-01] reopens D38, [R2-03-01] and [R2-03-02] amend D145, which is still pending the owner's review, and [F09-02] extends B572's recorded fix. The rest are wording, a single clause or one table row each.

## 9. The spec corpus as an agent harness: structure, duplication and retrieval cost

This section judges `specs/`, the knowledge index and the delivery pages as the material a coding agent must load before it changes anything. The standard is the one a well-run agent harness meets: an agent reaches the one normative clause its task needs through a bounded, predictable path. History, rationale and delivery state stay reachable but never mandatory. Every fact has one owner, and a mechanical gate keeps that true. Token figures are bytes divided by four at commit `17acb63d9`.

### 9.1 Verdict in brief

The ownership model is right and its gates are stronger than in most repositories. The cost problem is concentrated in three places. The mandatory path names whole milestone pages of 63 to 141 KB, mostly history [F15-01]. The index routes to pages, not to sections that reach 40 to 101 KB [F15-07]. The gates bound only the entry files [F15-08]. Duplication is real but local: the storage family, the setup subtree and milestone item details restate rules owned elsewhere [F05-08] [F13-09] [F15-09] [F17-05]. None of this needs a new model. It needs the existing model applied to the pages an agent opens second, and gates that hold it there.

### 9.2 What is good and must be kept

| Mechanism | Evidence | Why it should stay |
| --- | --- | --- |
| One owner per fact, stated as a rule | `specs/index.md:34-41`, `AGENTS.md:15-17` | It is the precondition for every cost saving below. |
| A compact routing index | `specs/index.md` is 4,627 bytes, about 1.2k tokens | A single-table router is the right shape; it only needs finer rows. |
| Short entrypoint that routes, not restates | `AGENTS.md` is 42 lines; `CLAUDE.md` is one import | Both runtimes read one tracked entrypoint. |
| Byte budgets that ratchet down | `test/architecture/docs_test.go:20-35` | A budget more than a tenth above the file fails, so budgets follow files down. |
| Spec tables checked against code | bounds, diagnostics and package-table tests (notes F15) | The cheapest way to keep a table true. |
| "Not yet met" convention with a gate | `specs/index.md:34-37`; about 37 such lines exist | Cheap, explicit and machine-checked. |
| Structural milestone checker | `test/architecture/milestones_test.go:72-446` | Status, one row per item and delivered-item departure are checked. |
| Knowledge split from specs | `specs/index.md:38-39`; symptom and error-text index | Lessons stay out of normative text; sampled pages carry date, symptom, cause and rule. |
| Clean core normative pages | `api.md`, `api/environment.md`, `cli/*.md`, `architecture.md` carry 0 to 6 delivery tokens each (notes F15) | Present-tense target rules without delivery noise. |
| Explicit authority boundary | `specs/milestones.md:36-39` | "A spec, milestone page, item or decision authorizes no implementation or effect." |
| Coverage-page skeleton | evidence, paths, matrix, budgeted profiles, "Left out" list (notes F17) | The right shape for a support matrix; the storage refusal table is a model executable table. |

### 9.3 Corpus shape

| Measure | Value |
| --- | --- |
| `specs/**/*.md` total | 3,139,710 bytes, about 785k tokens |
| Milestones and history (`delivered.md`, `backlog.md`, `m1`..`m7`, `milestones.md`) | about 1.40 MB, 44 percent |
| `delivered.md` / `backlog.md` | 534 KB (134k tokens) / 378 KB (95k tokens) |
| Milestone pages m2, m3, m4 | 141 KB, 129 KB, 141 KB (32k to 35k tokens each) |
| Storage family (eight pages) | about 413 KB, about 103k tokens, 13 percent of the corpus |
| Largest `##` sections | managed-os "Installation" 101 KB; state-reconciliation "Plan and execution" 59 KB; four more above 40 KB |
| Ledger verdicts over 80 assessed files | 17 good, 62 needs improvement, 1 bad |

The one "bad" file is a knowledge page, `.agents/knowledge/api-alignment.md`, which the ledger recommends deleting because it carries a wrong kind count.

### 9.4 Retrieval cost today for four task classes

The mandated reads come from `AGENTS.md:8-10`, `specs/index.md:3-5`, the code-implementation skill and the path-scoped rule. "As written" means an agent obeys "the milestone page" literally. "Targeted" means it reads the opening, the slice table, the item row and one `### B<n>` section.

#### Task A: a Go fix inside one capability (an M3 substrate item)

| Read | Bytes | Tokens | Class |
| --- | --- | --- | --- |
| `AGENTS.md`, `CLAUDE.md`, `.claude/rules/go.md` | 2,914 | 730 | process |
| `specs/index.md` | 4,627 | 1,160 | routing |
| `specs/milestones.md` | 8,182 | 2,050 | process and delivery |
| `specs/milestones/m3.md`, whole page | 128,633 | 32,160 | delivery and history |
| code-implementation skill and Go, formatting, verification references | 19,920 | 4,980 | procedure |
| knowledge index, searched | ~2,000 | ~500 | lessons |
| `substrates.md` "Machine realization" | 14,413 | 3,600 | rule |
| **Total** | **~180,700** | **~45,200** | rule 8 percent; delivery and history 76 percent |

Targeted, the total is about 18k tokens, with delivery state still about 40 percent.

#### Task B: add an API field to one kind (Machine)

| Read | Bytes | Tokens | Class |
| --- | --- | --- | --- |
| Entry files, index, `milestones.md`, rule | 15,700 | 3,900 | routing and process |
| code-implementation, ux-and-apis, references | 21,800 | 5,450 | procedure |
| `api.md` grammar, defaults, native fields | 24,562 | 6,140 | rule |
| `api/machines.md` "Machine" | 28,438 | 7,110 | rule |
| semantic owner section in `substrates.md` | 14,413 | 3,600 | rule |
| `m3.md`, whole page | 128,633 | 32,160 | delivery and history |
| **Total** | **~233,500** | **~58,400** | rule 29 percent; delivery and history 59 percent |

#### Task C: an Ansible role fix (managed-OS installation)

| Read | Bytes | Tokens | Class |
| --- | --- | --- | --- |
| Entry files, index, `milestones.md`, `rules/ansible.md` | 15,600 | 3,900 | routing and process |
| code-implementation, Ansible references, security-safety | 19,700 | 4,930 | procedure |
| `architecture.md` Go/Ansible boundary and collection structure | 19,700 | 4,930 | rule |
| `managed-os.md` "Installation" preamble and one subsection | 50,300 | 12,580 | rule |
| `m1.md`, whole page | 63,100 | 15,780 | delivery and history |
| **Total** | **~168,400** | **~42,100** | rule 41 percent; delivery and history 42 percent |

#### Task D: a spec clarification (state reconciliation, no milestone item)

| Read | Bytes | Tokens | Class |
| --- | --- | --- | --- |
| Entry files, index, `milestones.md`, `rules/guidance.md` | 15,600 | 3,900 | routing and process |
| architecture-best-practices, code-implementation, references | 14,600 | 3,650 | procedure |
| `state-reconciliation.md` "Plan and execution" | 58,644 | 14,660 | rule; one subsection needed |
| duplicate search across specs and the decisions log | ~8,000 | ~2,000 | rule and decisions |
| **Total** | **~97,000** | **~24,200** | the needed subsection is about 10 percent |

The fixed routing cost is lean, about 4k tokens. The variance comes from two sources: whole milestone pages [F15-01] and unsplit owning sections [F15-07]. Task D shows the second source alone: an index row that points at a 136 KB page leaves the agent to find a 6 KB answer inside a 59 KB section.

### 9.5 Findings in this area

| Id | Severity | Defect |
| --- | --- | --- |
| [F17-03] | High | Scope rules cannot be decided from the tables: no slice Delivery column, and the "Next" lines are stale. |
| [F15-01] | Medium | Whole milestone pages, mostly history, dominate every slice task's reading. |
| [F15-08] | Medium | Docs gates bound entry files only, not the pages agents read next. |
| [F17-04] | Medium | Session-taken waivers and scope readings act before the owner confirms them. |
| [F05-08] | Medium | The storage contract spans five to eight pages; the deferred page contradicts itself. |
| [F15-02] | Low | The six-host Ceph cap is written as a target rule, not as "Not yet met". |
| [F15-03] | Low | A stale, unindexed M2a review page sits among normative specs. |
| [F15-04] | Low | "Not yet met" lines carry acceptance and delivery state. |
| [F15-06] | Low | Q and OD identifiers are used hundreds of times but are undefined. |
| [F15-07] | Low | The index routes to whole pages while owning sections reach 40 to 101 KB. |
| [F15-09] | Low | Milestone item details restate spec rules. |
| [F15-10] | Low | The decision log is a 132 KB tail of `backlog.md` mixing pending clauses. |
| [F13-09] | Low | Setup-subtree rules and the version-5 cut-over are restated on five pages. |
| [F17-05] | Low | Ceph host-preparation and adoption safety rules have three owners. |
| [F17-08] | Low | Dated per-release native facts live in specs instead of knowledge. |
| [F16-03] | Low | The CLI help content contract exists only in delivered history. |
| [F07-10] | Low | Spec navigation cites a test-only function as the capability registry. |
| [F17-07] | Low | The acceptance ledger has no row template for cluster or add-on gates. |
| [R2-02-05] | Low | No substrate or service qualification section; qualification lives in milestone rows. |

[F17-03] is the one High finding here because it touches authorization, not cost. `specs/milestones.md:40-41` defines "active" through a slice's Delivery, yet the Planned slices tables have no Delivery column. `specs/milestones.md:29-30` still names M2c as next, which D168 paused. An agent told to "resume" cannot decide what is active from the page it is told to read.

### 9.6 Duplication and drift

| Claim | Page A | Page B (and others) | Ids |
| --- | --- | --- | --- |
| StorageClient has exactly one same-cluster scope | `api/storage.md:61-63` | `api/storage.md:262`, `storage-resources.md:401-404` | [F05-08] |
| Retired helpers still normalized (`mgmtGateway.dnsLabel`) | `deferred/storage-api.md:93-95` retires them | `deferred/storage-api.md:309-315` materializes them | [F05-08] |
| Six admitted storage kinds are "target only" | `storage-api-review.md:16-18, 227-228` | `api/storage.md:74-90` admits them | [F15-03] |
| Ceph cluster size target | `storage.md:564-566` caps at six hosts | `milestones/m2.md:10-11`, D130 | [F15-02] |
| Seventh-host refusal and concurrent rounds | `storage.md:567-569` | `milestones/m2.md` B688 | [F15-09] |
| Pool and RBD-image adoption needs an attempt record | `storage.md:795-796` | `storage-coverage.md:129-131`, `storage-resources.md:361-363` | [F17-05] |
| Setup runtime publication and retention | `controller.md:863-890` | `contexts/controller-record.md:191-212` | [F13-09] |
| Version-5 registry refusal | `contexts.md:192-201` | four restatements, including `contexts.md:283-291`, `controller.md:899-906` | [F13-09] |
| CLI help content | `milestones/delivered.md:3807-3810` | no owner in `cli/commands.md` | [F16-03] |
| Capability registry entry point | `architecture.md:391` cites `buildCapabilities` | production `wiring_reconciliation.go:199` is `buildCapabilitiesWith` | [F07-10] |
| Acceptance state | milestone pages | coverage "Not yet met" lines, e.g. `container-cluster-coverage.md:14, 201` | [F15-04] |
| Dated release pairings | knowledge pages | `add-on-coverage.md:146-148, 195-197`, `container-cluster-coverage.md:51-53` | [F17-08] |

Two patterns explain most rows. The storage contract grew by review rounds and kept each round's page. Milestone and coverage pages paraphrase owning rules instead of linking them, so they become second owners. Related drift reported in other sections, such as [F13-01] (stale `contexts.md` after setup v2) and [F05-02], is the result of the same pattern.

### 9.7 Per-page actions

Rows come from the ledger, condensed. Skills, rules, wrappers and entrypoints are covered in section 10. Tokens are approximate.

| File | Lines | ~Tokens | Verdict | Action |
| --- | --- | --- | --- | --- |
| `specs/index.md` | 60 | 1.2k | needs-improvement | Rewrite: add section-level rows for large domains, index the review page or remove it, glossary by pointer. |
| `specs/project.md` | 105 | 1.3k | needs-improvement | Rewrite scope and extension sections; one link per restated topic. |
| `specs/api.md` | 423 | 6.1k | needs-improvement | Keep as grammar owner; fix kind count; rewrite suppression paragraph as rule plus table. |
| `specs/api/input.md` | 192 | 2.9k | needs-improvement | Rewrite discovery and ceilings; YAML dialect rule into `api.md`. |
| `specs/api/environment.md` | 503 | 7.5k | needs-improvement | Split: defaults algorithm into `api.md`; controller material to controller pages. |
| `specs/api/machines.md` | 826 | 14.9k | needs-improvement | Split by kind; move milestone ids and test names out. |
| `specs/api/secrets.md` | 211 | 3.2k | needs-improvement | Rewrite "File source" to a refusal rule plus link. |
| `specs/api/infrastructure-services.md` | 258 | 3.9k | needs-improvement | Rewrite: per-kind support status for Registry and LoadBalancer. |
| `specs/api/container-clusters.md` | 475 | 6.9k | needs-improvement | Rewrite platform and topology sections; evidence to knowledge. |
| `specs/api/storage.md` | 615 | 10.0k | needs-improvement | Rewrite opening as a per-kind admission table; state `ceph.distribution`. |
| `specs/api/addons.md` | 518 | 9.8k | needs-improvement | Split: package configuration records to a sibling page. |
| `specs/api/custom-playbooks.md` | 38 | 0.5k | needs-improvement | Rewrite: become the single owner of admission; merge the deferred lexical rules in. |
| `specs/architecture.md` | 1119 | 19.5k | needs-improvement | Split: adapter protocol to its own page; gate-selection rules to the verification reference. |
| `specs/cli.md` | 1404 | 22.9k | needs-improvement | Split: access, sessions, trust and power to `cli/access.md`; privilege to security. |
| `specs/cli/commands.md` | 421 | 9.3k | needs-improvement | Rewrite: closed Effects vocabulary; add "Help content" [F16-03]. |
| `specs/cli/output.md` | 1090 | 19.8k | needs-improvement | Split: per-command result shapes to a results page. |
| `specs/controller.md` | 1032 | 17.1k | needs-improvement | Split: runtime mechanics into `contexts/controller-record.md` [F13-09]. |
| `specs/contexts.md` | 902 | 15.4k | needs-improvement | Rewrite stale registry text; split media acquisition and admission arithmetic to child pages. |
| `specs/contexts/controller-record.md` | 245 | 3.8k | needs-improvement | Split setup subtree into its own indexed page. |
| `specs/secrets.md` | 565 | 9.4k | needs-improvement | Split: keyring format to `secrets/local-keyring-v4.md` with stable anchor. |
| `specs/security.md` | 698 | 14.3k | needs-improvement | Rewrite "Process boundary": invariants only, mechanics linked. |
| `specs/state-reconciliation.md` | 2084 | 34.1k | needs-improvement | Split by consumer: records page, diff to CLI owner; move delivery ids out. |
| `specs/infrastructure-services.md` | 572 | 10.1k | needs-improvement | Split: host reservations to its own page. |
| `specs/substrates.md` | 1039 | 17.5k | needs-improvement | Split by arm into a directory; keep a 300-line contract core. |
| `specs/managed-os.md` | 1462 | 27.8k | needs-improvement | Split: media store out; installation core of about 300 lines plus feature pages [F15-07]. |
| `specs/container-clusters.md` | 1013 | 17.6k | needs-improvement | Split: media and installation block contracts to a second page; move history out. |
| `specs/container-cluster-coverage.md` | 220 | 7.0k | needs-improvement | Rewrite: drop delivery columns; dated facts to knowledge [F17-08]. |
| `specs/storage.md` | 1616 | 28.5k | needs-improvement | Split: shared lifecycle core plus one page per realization protocol. |
| `specs/storage-native-api.md` | 989 | 16.2k | needs-improvement | Split: tunnel and recovery protocol to the lifecycle core. |
| `specs/storage-resources.md` | 1498 | 23.6k | needs-improvement | Split by kind family; delete superseded "Target rule" tables. |
| `specs/storage-coverage.md` | 628 | 13.5k | needs-improvement | Rewrite: safety rules move to their owner [F17-05]; delivery state out. |
| `specs/storage-api-review.md` | 428 | 6.5k | needs-improvement | Move history out to `specs/milestones/` [F15-03]. |
| `specs/add-ons.md` | 463 | 7.1k | needs-improvement | Split: object program and receipts to a Kubernetes-object page. |
| `specs/add-on-coverage.md` | 435 | 8.0k | needs-improvement | Rewrite: field defaults only in `api/addons.md`; dated evidence to knowledge. |
| `specs/deferred/storage-api.md` | 337 | 4.9k | needs-improvement | Rewrite: live kinds to owners; delete retired entries [F05-08]. |
| `specs/deferred/add-ons-design.md` | 281 | 4.1k | needs-improvement | Split: Steps into `api/addons.md`; keep the rest as design. |
| `specs/deferred/custom-playbooks.md` | 103 | 1.4k | needs-improvement | Merge enforced rules into `api/custom-playbooks.md`. |
| `specs/deferred/machine-arms.md` | 106 | 1.7k | needs-improvement | Merge into `api/machines.md` refused arms, or link one way. |
| `specs/deferred/cli-access-and-rendering.md` | 146 | 1.9k | needs-improvement | Split per revival item; delete stubs owned live. |
| `specs/deferred/reconciliation-scenarios.md` | 193 | 2.8k | needs-improvement | Move history out; keep seam, acceptance rules, prohibitions. |
| `specs/milestones.md` | 131 | 2.0k | needs-improvement | Rewrite Status: an Active slices table [F17-03]; define Q and OD [F15-06]. |
| `specs/milestones/m1.md` | 965 | 15.8k | needs-improvement | Move history out (lines 749-965). |
| `specs/milestones/m2.md` | 1988 | 35.4k | needs-improvement | Move history out; card plus per-item detail. |
| `specs/milestones/m3.md` | 1880 | 32.2k | needs-improvement | Move history out; card under about 600 lines. |
| `specs/milestones/m4.md` | 1912 | 35.1k | needs-improvement | Move history out; Q26-Q61 options to the record. |
| `specs/milestones/m5.md`, `m6.md` | 9, 12 | 0.3k | good | Merge into `milestones.md` as a retired-milestones table. |
| `specs/milestones/m7.md` | 126 | 2.0k | needs-improvement | Rewrite Items with defined Delivery values. |
| `specs/milestones/backlog.md` | 4096 | 94.5k | needs-improvement | Split: decisions to their own page with status tokens [F15-10], owner agreement needed. |
| `specs/milestones/delivered.md` | 8437 | 133.6k | needs-improvement | Split: 150-line index plus per-era record pages. |
| `.agents/knowledge/index.md` | 86 | 7.3k | needs-improvement | Rewrite: domain headings, one row per page, about 40 words a row. |
| `.agents/knowledge/version-specializations.md` | 176 | 6.7k | needs-improvement | Split: compact catalog here, evidence on record pages. |
| `.agents/knowledge/reconciliation-audit-2026-10.md` | 357 | 5.3k | needs-improvement | Move history out; keep "How apply and destroy decide". |
| `.agents/knowledge/api-alignment.md` | 60 | 0.9k | bad | Delete with its index row. |
| `docs/development.md` | 244 | 4.0k | needs-improvement | Split: lock refresh and image qualification out. |
| `docs/acceptance.md` | 128 | 3.1k | needs-improvement | Rewrite row format; add cluster and add-on templates [F17-07]. |
| `docs/operator-guide.md` | 641 | 9.2k | needs-improvement | Split by task; delete the M1 closing section. |
| `examples/README.md` | 103 | 2.5k | needs-improvement | Rewrite: strip status text; contributing rules to a reference. |
| `README.md` | 41 | 0.4k | good | Keep; add one day-0 scope sentence. |

The table's shape is clear. Almost every core page above 60 KB should split along an owner boundary it already names. The four milestone pages, `delivered.md` and `backlog.md` should move history out rather than be rewritten.

### 9.8 Restructuring proposal

Each step below keeps every normative sentence. Where a verifier narrowed a finding, the smallest fix comes first and the larger restructuring is marked as the owner's choice.

#### Layering: normative core, detail, history

| Layer | Holds | Location | On the mandatory path? |
| --- | --- | --- | --- |
| Normative core | Present-tense rules, state and refusal tables, links to detail | one page per domain under `specs/` | Only the owning section |
| Detail | Field records, protocols, per-implementation coverage | sub-pages beside the core, linked by anchor | Only when the core section links it |
| History | Delivered records, reviews, decisions, waivers, dated evidence | `specs/milestones/` and knowledge | Never; reached by an id |

The review page [F15-03], the waiver narratives [F15-01] and the dated release facts [F17-08] are history in the wrong layer. Item details that restate rules [F15-09] are detail with a second owner.

#### Size budgets

| Unit | Budget | Rationale |
| --- | --- | --- |
| Core spec page | at most 48 KB (12k tokens) | No core page should cost more than a task's whole rule budget. |
| `##` section | at most 16 KB | One section per index row stays cheap. |
| Leaf section | at most 8 KB | The unit an agent actually needs. |
| Milestone card | at most 16 KB | Scope, Requires, exit, decisions, slices, items. |
| Open item section | no normative sentence without a link | The verifier preferred a checkable rule to a byte cap [F15-09]. |

Budgets should use the existing ratchet: record today's size, fail growth, and let each budget follow its page down. The verifier on [F15-07] treated a per-section ratchet as optional; the managed-os "Installation" split is the required first step.

#### Index granularity

Keep one row per domain, and add anchor-level rows only for the six largest domains: managed OS, state reconciliation, storage, CLI, contexts and controller. The index must stay within its own budget (`test/architecture/docs_test.go:28`, 4,656 bytes). If it cannot, each split domain gets a heading map at the top of its core page, and the index points there. Every `specs/**/*.md` outside `milestones/` and `deferred/` must be reachable from the index; `storage-api-review.md` is not today [F15-03].

#### The mandatory path

The smallest fix the verifiers endorsed for [F15-01] changes two sentences, not the corpus.

```markdown
<!-- AGENTS.md, replacing the milestone bullet -->
- specs/milestones.md: read the Active slices table and the Scope rules. Then
  read, on the named milestone page, the opening, the Planned slices row, the
  item's Items row and its `### B<n>` section. Read nothing else there unless
  that section links it.
```

`specs/index.md:3-5` should say the same. With that sentence alone, Task A falls from about 45k to about 18k tokens. The card restructure then removes the rest of the history from the card read.

```markdown
# M3: Full OpenShift clusters            <!-- card, at most 16 KB -->
Scope: one paragraph. Exit tier: one line.
## Requires
| Requires | State | Waiver (D<n>, date, owner request quoted) |
## Open decisions
| ID | Question | Status | Answered by |
## Planned slices
| Slice | Kind | Items in order | Requires | Delivery |
## Items
| ID | Alias | Kind | Owner | Outcome | Requires | Definition | Delivery |
## Item detail
### B714
Outcome (one sentence). Owning rule: link. Exit evidence. Deferral reason.
```

#### Milestone pages and the B, X, D scheme on the agent path

| Identifier | Role on the agent path | Proposal |
| --- | --- | --- |
| B | The handle of open work; commit `Refs:` lines need it | Keep. Allowed in normative pages only inside "Not yet met ...; tracked as B<n>". |
| X | Names a delivered slice | Keep in commits and records; ban from normative pages. |
| D | Owner decisions, checked before changes | Keep the numbers; add a leading status token and numeric order [F15-10]. |
| Q, OD | Open questions, used hundreds of times | Define in `milestones.md` Identifiers with one allocation rule; one table header with Status [F15-06]. |
| P, L | Local row labels and lane suffixes | Leave local; the verifier dropped them from scope. |
| Session clauses | Waivers taken "pending the owner's review" | One scope rule: a waiver quotes the owner's request; an overturned clause reopens its work [F17-04]. |

`specs/milestones.md:114-115` fixes the decision log's location, so moving it to its own page needs the owner's agreement. The status token and ordering check do not. For [F17-03], the calibrator preferred one Active slices table on `milestones.md` over a Delivery column on every slice table; either decides "active" from a table.

`m5.md` and `m6.md` become rows of a retired-milestones table. `delivered.md` keeps a short index and per-era pages; commit chronology stays in Git.

#### Machine-checkable ownership

Ownership today is a convention checked by review. A small registry would let a gate check it.

```yaml
# specs/owners.yaml (draft; anchors illustrative): topic key -> owning anchor
storage.client.scope: storage-resources.md#storageclient
storage.adoption.attempt-record: storage.md#adoption
setup.runtime.retention: contexts/controller-record.md#the-setup-subtree
cli.help.content: cli/commands.md#help-content
ceph.cluster.execution-budget: storage.md#cluster-execution-budget
```

The gate checks that each anchor resolves and that no key names two anchors. Restating pages carry a link to the anchor instead of text. This catches the drift class in 9.6 when a page gains a link, not when it diverges. It does not detect a silent paraphrase, so it complements review rather than replacing it.

#### What the docs gates should enforce

| Gate | Finding | Priority |
| --- | --- | --- |
| Byte budget for each milestone card, or for the sections the mandatory path names | [F15-01], [F15-08] | First |
| Every `specs/**/*.md` outside `milestones/` and `deferred/` reachable from the index | [F15-03], [F15-08] | First |
| No `delivered.md` link inside a "Not yet met" paragraph | [F15-04] | First |
| No X id or `delivered.md` link in normative pages outside "Not yet met", with an allowlist | [F15-08] | Second |
| Per-section byte budgets with a ratchet for core pages | [F15-07] | Second |
| Q and OD tables share one header with Status; answered rows name a D | [F15-06] | Optional |
| Decisions in numeric order, each with a status token | [F15-10] | Optional |
| Every function a spec's "Finding code" section cites exists in a non-test file | [F07-10] | Optional |

Keep every existing gate. The point is to move the budget boundary from the entry files to the pages the entry files send an agent to.

### 9.9 Expected token effect per task class

Estimates assume the mandatory-path rewrite, milestone cards, the managed-os and state-reconciliation splits, and a one-line decision index. Skill and reference costs are held at today's values; section 10 treats them.

| Task class | Today, as written | Today, targeted | After restructuring | Change vs as written | Rule share after |
| --- | --- | --- | --- | --- | --- |
| A: Go fix, one capability | ~45k | ~18k | ~15k | about -67 percent | about 25 percent |
| B: API field on one kind | ~58k | ~30k | ~28k | about -52 percent | about 60 percent |
| C: Ansible role fix | ~42k | ~31k | ~20k | about -53 percent | about 45 percent |
| D: spec clarification | ~24k | ~24k | ~11k | about -56 percent | about 30 percent |

The basis for the "after" column:

- A and B: a milestone read of about 2.5k tokens (card head, item row, one `### B<n>`) replaces 32k.
- C: the m1 card read replaces 15.8k, and a 2k-token installation leaf plus a short core intro replaces 12.6k.
- D: one leaf of at most 8 KB plus its parent summary, about 3k, replaces 14.7k; a decision lookup costs about 0.5k instead of 2k.

Most of the gain comes from one cheap change: two sentences in `AGENTS.md` and `specs/index.md`. That change takes Tasks A and B to their targeted figures without moving a byte of spec text. The splits deliver the rest and, more importantly, stop the cost from growing back.

### 9.10 Unverified observations

- Not verified: a grep at `17acb63d9` finds about 40 lines in ten specs outside `milestones/` and `deferred/` that link `delivered.md` without "Not yet met" on the same line, 16 of them in `container-cluster-coverage.md`; some may be legitimate pointers.

## 10. Skills and agent harness: current state, proposed portfolio, runtime contract and token budget

This section judges the agent harness: the entrypoints, the four skills and their references, the Claude Code wrappers, the path-scoped rules, the settings hooks and permissions, and the knowledge index as a retrieval surface. It then proposes a portfolio, drafts the changed and new skills, states one contract for Claude Code and Codex, models the token cost per task class, and gives a plan to measure the result.

Standards applied: the repository's own rule that skills own procedure and every fact has one owner (AGENTS.md:15-17); the discovery contracts of both runtimes as of 2026-10; the owner's recorded decisions (D139, D142, D145, D162, D168); and the practice visible in delivered records. Token figures are bytes divided by four at commit 17acb63d9.

### 10.1 Current state

#### What is good and should be kept

The harness is small, routed and mechanically guarded. These properties are rare and worth protecting.

- **One tracked entrypoint for both runtimes.** CLAUDE.md is one line, `@AGENTS.md` (CLAUDE.md:1), so Claude Code and Codex read the same 2.4 KB of root guidance. AGENTS.md routes to owners and says "A spec or backlog entry is not authorization" (AGENTS.md:21-22).
- **Canonical skills with thin wrappers.** Skills live once under `.agents/skills`, where Codex discovers them. `.claude/skills` holds seven-line wrappers whose frontmatter a docs test holds identical (test/architecture/docs_test.go:451-475).
- **Byte budgets that only shrink.** The always-read files carry budgets that fail when a file grows or when a budget sits more than a tenth above its file (docs_test.go:20-34). Few repositories measure entry cost at all.
- **Small skills that link instead of restating.** The four SKILL.md files are 1.9 to 2.9 KB each, about 470 to 730 tokens.
- **Rules already broken once.** security-safety and ux-and-apis each list rules the repository has broken before. That converts incident history into a short, checkable guard list (.agents/skills/security-safety/SKILL.md:14-34, .agents/skills/ux-and-apis/SKILL.md:15-26).
- **Measured verification tiers.** Probe, focus, quick and gate carry measured costs (.agents/skills/code-implementation/references/verification.md:7-19). `make focus` derives its checks from the branch diff.
- **Path-scoped rules are tiny pointers.** The three `.claude/rules` pages are 432 to 544 bytes and load only when matching files are read.
- **A symptom-keyed knowledge index.** Rows keyed by error text and symbol are the right retrieval shape for agents. The closing conventions (one page per lesson, link the owner, retire stale findings) are followed by the sampled pages.

A considered and refuted candidate: that Codex lacks the per-area guidance the Claude-only rules give. Verification found every rule's content reachable through AGENTS.md, code-implementation and the skill index, so the rules are redundancy for one runtime, not a gap (refuted.md, F14-10).

#### What needs improvement

- The canonical skill makes a CI-gated pull request the unconditional default, while delivered practice and D168 integrate locally and wait to push [F14-03].
- The skill description promises "change-class verification" that the body never defines, and delivered records integrate on gate subsets [F14-06].
- The safety class, which buys `make race` and an independent review, omits storage, secrets custody, privilege, infrastructure services and add-ons [F14-05].
- No procedure names the contract guard tests, the version-literal classification or `scripts/check-commits` [F14-07].
- Nothing procedural owns moving a content digest when automation changes what an effect is; that duty sits only in a delivered record and a knowledge page [R2-03-03].
- Tier names are restated in about seven places with conflicting "inner loop" labels [F14-11].
- The wrapper parity test skips a canonical skill with no wrapper, so a new skill can be invisible to Claude Code while the gate passes [F14-12].
- The mandatory path routes slice work through whole milestone pages of 63 to 141 KB [F15-01]; this is the largest single cost and is owned by section 9.

#### What is bad

- **The effect prohibition is in the wrong place.** The only sentence that forbids agents to run `apply`, `destroy` or playbooks against hosts sits in an optional reference (.agents/skills/code-implementation/references/agent-operation.md:100-102). The Claude ask list matches exact prefixes that `bin/bootwright apply`, `setup` and `machine exec` evade, and Codex has no tracked counterpart [F14-01].
- **The Stop hook can trap a session.** It runs `make quick` in the session directory whenever anything is dirty or untracked, ignores `stop_hook_active`, and make's exit status 2 is Claude Code's blocking code (.claude/settings.json:41-54) [F14-02].
- **Vendor model routing is tracked.** Model tiers, one vendor's behavior remarks and frozen model IDs live in a skill reference (agent-operation.md:34-91), duplicating the owner's user-level rules [F14-09].
- **The local-only override replaces AGENTS.md for Codex in the primary checkout only.** Claude Code sessions and Codex sessions in task worktrees therefore start from different root rules [F14-04].

#### Current-state table

| File | Role | Verdict | Action |
| --- | --- | --- | --- |
| AGENTS.md | Root routing, authorization, commands | Good | Add effect prohibition and override line [F14-01] [F14-04] |
| CLAUDE.md | Imports AGENTS.md | Good | Keep |
| local-only override file | Codex replacement of AGENTS.md | Needs improvement | Defer to tracked file or move to user level [F14-04] |
| .agents/skills/index.md | Skill routing | Good | Add rows for new skills; name runtimes' local mechanisms |
| code-implementation/SKILL.md | Verification, commits, integration | Needs improvement | Rewrite (draft below) [F14-03] [F14-05] [F14-06] |
| references/verification.md | Tier definitions and costs | Needs improvement | Own tier names; document the Stop hook [F14-11] [F14-02] |
| references/git-integration.md | Worktrees, variants, cleanup | Good | Replace "Local, no remote" with local fast-forward [F14-03] |
| references/go.md | Go conventions, goldens | Needs improvement | Add digest guard lines [F14-07] [R2-03-03] |
| references/ansible.md | Ansible conventions | Good | Add contract checklist item [R2-03-03] |
| references/formatting.md | Whitespace, Go, YAML, Ansible | Needs improvement | Optional split by language (ledger) |
| references/agent-operation.md | Model routing, reviews, unattended runs | Needs improvement | Strip vendor tables; move prohibition out [F14-09] [F14-01] |
| architecture-best-practices/SKILL.md | Spec and boundary design | Needs improvement | Rewrite (draft below) |
| security-safety/SKILL.md | Trust and effect review | Good | Small edits: owner links, residual-race wording (ledger) |
| ux-and-apis/SKILL.md | CLI, schema, output design | Good | Add an API-change checklist (draft below) [F14-07] |
| .claude/skills/*/SKILL.md (4) | Discovery wrappers | Good | Keep; make parity test strict [F14-12] |
| .claude/rules/{go,ansible,guidance}.md | Path-scoped pointers | Good | Drop restated tiers [F14-11] |
| .claude/settings.json | Permissions and Stop hook | Needs improvement | Widen ask list; replace hook [F14-01] [F14-02] [F14-06] |
| .agents/knowledge/index.md | Lesson retrieval | Needs improvement | Group by domain; keep symptom keys (ledger) |
| docs/development.md | Contributor guide | Needs improvement | Fix tier labels [F14-11] |

### 10.2 Proposed portfolio

#### Decisions on the candidate changes

The audit tested six candidate changes against the findings and their verification record. Each decision follows the evidence, not the candidate list.

| Candidate | Decision | Evidence and reason |
| --- | --- | --- |
| Change-class verification contract replacing the "make check once" gate | **Decline the table; fix the promise.** Keep `make check` as the single integration gate. Define the safety class once. | The arbiter on [F14-06] ruled against a change-class gate table and for rewording the description and widening the allow list. verification.md:47-56 states focus never proves what only the gate runs; D139 lists the Ansible suite among exit gates. |
| Implementation-extension skill | **Add** `extend-implementation`. | No procedure covers a new arm, distribution or second implementation [F06-04]; a new substrate arm edits every consumer [F18-03]; the "policy never branches on implementation" rule is unenforced [F06-03]; the architecture skill never explains how to choose a typed variant, an implementation or a version specialization (ledger). |
| Spec and contract-change skill | **Split.** Spec placement goes into a rewritten architecture-best-practices; the content-digest duty goes into the Go and Ansible references. No new skill. | The arbiter on [F14-07] declined a separate change-class reference and asked for lines in go.md; [R2-03-03] verifiers asked for one checklist item in ansible.md and go.md. The "Not yet met" form is paraphrased in the skill (ledger) and misused in coverage pages [F15-04]. |
| Milestone and knowledge bookkeeping skill | **Add** `milestones-and-knowledge`. | Item details restate spec rules [F15-09]; "Not yet met" carries acceptance state [F15-04]; identifier schemes are undefined [F15-06]; the decision log is unordered [F15-10]; agents read whole pages [F15-01]. Every slice ends with this bookkeeping, and no skill owns it. |
| API-change skill | **Fold into ux-and-apis** as a checklist section. | [F14-07] names API kind and field work as uncovered, but the arbiter preferred additions to existing files over new process files. ux-and-apis already owns schema design; a second skill would split one decision. |
| Relocate model routing | **Do it.** Keep vendor-neutral rules tracked; move tiers, model names and IDs to user-level files. | [F14-09]. The effect prohibition moves to AGENTS.md [F14-01]. |

#### Portfolio table

Size budgets are tokens of the SKILL.md body; references are budgeted separately. "Loads" says when the body enters context; the description is always in each runtime's skill listing.

| Status | Skill | Trigger (frontmatter description) | Purpose | Body outline | Budget | Loads | Runtime |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Modify | code-implementation | Deliver any authorized Bootwright tracked change, including specs, skills, guidance, code and tests: tiered verification, the safety class, commits and local or pull-request integration. | Single owner of verify, commit, integrate | Before editing; safety class; verify; commit; integrate; report | 850 | Every tracked edit | Both |
| Modify | architecture-best-practices | Design or review Bootwright specs and guidance: owning page, Not yet met lines, component ownership, ports, the choice between typed variant, implementation and version specialization, and durable state. | Spec placement and boundary review | Place the fact; spec edit steps; choose the variation mechanism; review boundaries; report | 750 | Spec, guidance or boundary work; guidance rule | Both |
| Keep (small edit) | security-safety | unchanged | Trust and effect review | unchanged; owner link per rule; residual-race wording | 700 | Trust, secret, privilege, destructive work | Both |
| Modify | ux-and-apis | Design or review Bootwright public schemas, API kinds and fields, CLI journeys, help, diagnostics, output, examples and compatibility. | Public contract design and API change checklist | Owners; broken-once rules; design; changing a kind or field | 650 | Schema, CLI, output, example work | Both |
| Add | extend-implementation | Add or change a Bootwright implementation behind an existing capability: a substrate arm, distribution, storage or service implementation, add-on driver, or version specialization. | Route extension work through the specialization contract with the minimum touch set | Choose the mechanism; touch set; contract and digest; prove generality; report the touch set | 800 | On trigger | Both |
| Add | milestones-and-knowledge | Record Bootwright delivery state and lessons: add, move, split or close a milestone item, write a delivered record or decision, add a Not yet met line, or add or retire a knowledge page. | Bookkeeping without second owners | Targeted reads; items; Not yet met; decisions; delivered records; knowledge pages; checks | 750 | On trigger; linked from code-implementation | Both |
| Modify (reference) | references/agent-operation.md | n/a | Vendor-neutral orchestration rules | Acceptance before routing; explicit model and effort; stronger independent review for safety; unattended checklist | 500 | Orchestrated runs | Both |
| Remove (content) | model tiers, vendor IDs, vendor behavior remarks | n/a | Move to user-level configuration | n/a | 0 | n/a | n/a |

No skill is removed. Four domain skills plus one process skill was the right shape; the two additions cover the two task classes with the highest omission risk and no current owner.

### 10.3 Drafts

The drafts are written for both runtimes: plain Markdown, relative links, only `name` and `description` in frontmatter, no tool names or runtime commands. Each new skill gets a seven-line `.claude/skills` wrapper with identical frontmatter. Links to architecture sections that do not yet exist ([F06-04]'s "Adding an implementation") are marked as such.

#### code-implementation (modified)

```markdown
---
name: code-implementation
description: Deliver any authorized Bootwright tracked change, including specs, skills, guidance, code and tests, through tiered verification, the safety class, the commit convention and local or pull-request integration.
---

# Code Implementation

The single owner of verification, commits and integration for tracked changes.
It never authorizes a push, a release, product scope, or any command with
effects (AGENTS.md).

## Before editing

1. Confirm the authorized slice in AGENTS.md. Read the item's row and its
   `### B<n>` section, not the whole milestone page.
2. Work in a task worktree, never in the destination checkout; a cloud
   session uses the harness branch. Inspect status and preserve unrelated work.
3. Load the domain skills the change touches ([index](../index.md)) and only
   the references it needs: [Go](references/go.md),
   [Ansible](references/ansible.md), [formatting](references/formatting.md).
   Implementation work also loads [extend-implementation](../extend-implementation/SKILL.md);
   milestone or knowledge records load
   [milestones-and-knowledge](../milestones-and-knowledge/SKILL.md).

## Safety class

A change is safety class when its item has Kind `safety`, or when it changes a
destructive effect, a privilege boundary, secret custody, an ownership mark or
proof, or a durable store transition, in any capability: substrates, managed
OS, container clusters, storage, infrastructure services and add-ons included.
A safety change adds `make race` and an independent review of the diff, and
loads [security-safety](../security-safety/SKILL.md). `make race` runs a fixed
package set; add a race run for a changed concurrent package it omits.

## Implement and verify

- Keep implementation, specs, examples and tests of one observable change
  together. Add a regression test for a defect, and watch it fail first.
- Verify in [tiers](references/verification.md): a probe after each edit,
  `make focus` before each intermediate commit or handoff, and `make check`
  once, before the commit that integrates into main, plus the safety additions.
- A change that alters what an effect is moves its implementation's content
  digest; the Go and Ansible references carry the check.
- Failed, skipped, flaky, unavailable and unrun gates are not passes.
- Review the complete diff and run `git diff --cached --check`.

## Commit and integrate

- One commit per coherent change: a Conventional Commit subject
  `type(domain): subject`; a body naming what changed, why, and every gate
  with its result; a `Refs:` line naming the slice and items.
  `scripts/check-commits BASE HEAD` is the local form of the CI check.
- Integrate by the variant in [git integration](references/git-integration.md):
  local fast-forward when pushing is not authorized, a pull request when the
  owner or harness asks for one. Push only when the owner or harness requires it.
- Report the outcome, commits, gates with results, limitations and follow-ups.
```

#### architecture-best-practices (modified)

```markdown
---
name: architecture-best-practices
description: Design or review Bootwright specs and guidance: owning page, Not yet met lines, component ownership, ports, the choice between typed variant, implementation and version specialization, and durable state.
---

# Architecture Best Practices

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[Architecture](../../../specs/architecture.md) owns boundaries and dependency
policy; [state reconciliation](../../../specs/state-reconciliation.md) owns
lifecycle and durable state. The [spec index](../../../specs/index.md) and its
[conventions](../../../specs/index.md#conventions) own placement.

## Edit a spec

1. Find the owning section through the index; read that section, not the page.
2. Search for the rule's other statements; replace each copy with a link.
3. State the target rule in the present tense. When code does not meet it yet,
   add one line in the exact form the conventions give, linking an open item.
   A Not yet met line never holds acceptance state or a delivered link.
4. Keep inventories in code; hold a prose table to code with a test.
5. Record a deliberate contract change in the same commit; delivery narrative
   goes to milestones, lessons to knowledge.

## Choose the variation mechanism

Read [implementations and version variation](../../../specs/architecture.md#implementations-and-version-variation).

- Authored intent or a business invariant differs: a typed variant in the
  owning domain.
- Vendor, substrate, transport or release mechanics differ behind one
  capability: an implementation behind the port, selected by the registry.
- One implementation's release mechanics differ: a version specialization
  record at a declared seam, never in trust, credential or ownership code.
Then follow [extend-implementation](../extend-implementation/SKILL.md).

## Review boundaries

- Split components by actor, invariant, volatility and security boundary
  ([mandatory boundaries](../../../specs/architecture.md#mandatory-component-boundaries)).
- Keep Go policy and Ansible effects on their sides of the
  [boundary](../../../specs/architecture.md#go-and-ansible-responsibility-boundary).
- Define a port's request, result, typed failures, cancellation and replay
  before implementing it; share one implementation of a shared protocol.
- Give mutable state one owner. For a durable-format or identity change,
  resolve canonical form, crash boundary, and continuation or safe refusal first.
- Add an interface at its first concrete consumer; follow the
  [dependency rule](../../../specs/architecture.md#dependency-selection-and-reuse).

## Report

Name the violated contract, evidence, impact, smallest correction and its
verification ([architecture verification](../../../specs/architecture.md#architecture-verification)).
Separate confirmed defects from optional improvements.
```

#### ux-and-apis (modified: new final section only)

The existing body stays. Append this section and change the description as in the portfolio table.

```markdown
## Changing a kind or field

1. Read the [grammar](../../../specs/api.md#public-authoring-grammar),
   [defaults and effective state](../../../specs/api.md#defaults-normalization-and-effective-state)
   and the owning kind page section; then the semantic owner section it links.
2. Resolve omission, null, empty and zero, the default and its precedence, and
   whether the field is shared intent or implementation-shaped
   ([native fields](../../../specs/api.md#native-and-implementation-shaped-fields)).
3. Change the type in `api/v1alpha1`, the kind page and its semantic owner
   together, and decide whether adding the field moves any frozen request.
4. Update the effective-output goldens golden-first, and every tracked example
   that the change makes invalid or newly expressible; keep runbooks runnable.
5. A field that reaches a frozen request follows the contract check in the
   [Go reference](../code-implementation/references/go.md).
6. Name downstream input charts the milestone item lists in the report; never
   edit another repository in this change.
```

#### extend-implementation (new)

```markdown
---
name: extend-implementation
description: Add or change a Bootwright implementation behind an existing capability: a substrate arm, distribution, storage or service implementation, add-on driver, or version specialization.
---

# Extend An Implementation

Use [code-implementation](../code-implementation/SKILL.md) for the change and
[architecture-best-practices](../architecture-best-practices/SKILL.md) for the
mechanism choice. The general flow must not learn which implementation runs.

## 1. Choose the mechanism

Read [implementations and version variation](../../../specs/architecture.md#implementations-and-version-variation).
A release difference in one implementation is a version specialization; a
different vendor, substrate or product is an implementation; different authored
intent is a typed variant in the owning domain. State the choice and why.

## 2. Find the touch set before editing

- A new capability follows
  [adding a lifecycle capability](../../../specs/architecture.md#adding-a-lifecycle-capability).
- A new implementation of an existing capability touches its package, its
  registry row, composition and playbook bindings, its role, the domain's
  support statement and its contract suite. (Owning spec section pending, per
  the audit's extension finding.)
- List every other file the change needs. A change in another domain's
  general code means a seam is missing: add the seam as its own reviewed step
  first, in the same slice, and say so in the commit body.

## 3. Keep specifics behind the seam

- Consumers dispatch through the capability's port or entry points, never on
  an arm or implementation name. Do not add an arm literal to application code.
- A version specialization is a record with id, seam, variant and condition
  over facts from frozen artifacts; add its catalog row so
  `TestDocsVersionSpecializationCatalogMatchesRegistries` passes. Never key on
  a release label, never encode a version pairing (D163), never place a seam in
  trust, host-key, signature, redaction or credential code.
- Unsupported arms refuse at admission with a named diagnostic.

## 4. Contract and digest

Declare the implementation's identity, content identity and execution
dependencies. If the change alters what an effect is (objects, names, places,
ownership mark, claimed host resources, observation proof), move the content
digest and the implementation version; the
[content digest clause](../../../specs/state-reconciliation.md#continuation-and-removal)
owns the rule and `TestEveryImplementationMovesItsContentDigestWithItsContract`
guards part of it.

## 5. Prove generality

- Run the family's shared port or contract suite against the new implementation.
- Add a test that a second implementation or arm needs no edit to the general
  flow, where the family has one.
- Update the observable-field table if the implementation projects state.

## Report

List the touch set, any seam added, the digest decision and the gates run.
```

#### milestones-and-knowledge (new)

```markdown
---
name: milestones-and-knowledge
description: Record Bootwright delivery state and lessons: add, move, split or close a milestone item, write a delivered record or decision, add a Not yet met line, or add or retire a knowledge page.
---

# Milestones And Knowledge

Use [code-implementation](../code-implementation/SKILL.md) for the commit.
[Milestones](../../../specs/milestones.md) owns status, scope rules and
identifiers; the [knowledge index](../../knowledge/index.md) owns its
conventions in its closing paragraphs. Recording is a tracked edit: in a
review-only task, report the proposed record instead of writing it.

## Read narrowly

Read the Status table, the item's row and its `### B<n>` section. Search
`delivered.md` and `backlog.md` by ID; never read them whole.

## Items

- A new item takes one above the largest B under `specs/milestones/`, on the
  earliest milestone not done whose scope it blocks, or parked; fill every
  column the Items table header names.
- Item detail holds the outcome, a link to the owning spec section, exit
  evidence and any deferral reason. Never restate a spec rule in an item.
- Move, split, drop and complete items as the Identifiers section says; keep
  IDs and add dated alias notes.

## Not yet met

A spec line in the exact conventions form, linking one open item. It states
what code does not yet do. It never links a delivered record and never holds
acceptance or baseline state; that belongs on the milestone page.

## Decisions and delivered records

- A decision is appended only when the owner decides; a clause a session
  takes is marked pending review. Keep numeric order.
- A delivered record names the slice, items, commits, gates with results and
  any limitation; it links the spec clause instead of restating the rule.

## Knowledge pages

One page per durable lesson: observation, decision it informs, applicable
implementation or version, links to code and evidence, link to the owning
spec. Index it with searchable symptom, symbol or error text. Retire a page
when its basis changes.

## Check

`make docs-check` holds milestone structure, Not yet met links and index links.
```

#### Supporting drafts

AGENTS.md additions (about 90 tokens, within the byte budget headroom after removing the duplicated sentences in code-implementation):

```markdown
Agents never run a Bootwright command with effects (`apply`, `destroy`, `setup`,
`machine` or `cluster exec`, `media add`, `secret reveal`) or `ansible-playbook`
against a host unless the owner asks for it in the conversation.

A personal override defers to this file: use your runtime's user-level or
local file, never a replacement of this one.
```

`.claude/settings.json` ask additions [F14-01] and allow additions [F14-06]:

```json
"allow": ["Bash(make focus)", "Bash(make check)", "Bash(make race)"],
"ask": [
  "Bash(*bootwright apply*)", "Bash(*bootwright destroy*)",
  "Bash(*bootwright setup*)", "Bash(*bootwright machine exec*)",
  "Bash(*bootwright machine rsh*)", "Bash(*bootwright cluster exec*)",
  "Bash(*bootwright media add*)", "Bash(*ansible-playbook*)"
]
```

Ask rules are a backstop; a bypass permission mode skips them, which is why the prohibition must also be in AGENTS.md.

### 10.4 Common Claude Code and Codex contract

#### What each runtime loads at session start

| Layer | Claude Code | Codex | Effect here |
| --- | --- | --- | --- |
| User instructions | `~/.claude/CLAUDE.md` | `~/.codex/AGENTS.md` (or its override) | Model routing belongs here [F14-09] |
| Project instructions | CLAUDE.md, which imports AGENTS.md; CLAUDE.local.md appended | First non-empty of the override or AGENTS.md, per directory, root to cwd | Override replaces AGENTS.md in the primary checkout [F14-04] |
| Skills | Names and descriptions from `.claude/skills`; body on use | Names and descriptions from `.agents/skills`; body on use | Wrappers bridge the two |
| Path rules | `.claude/rules/*.md` with `paths:` load when matching files are read | None; content reaches Codex through skills | Redundant by design (F14-10 refuted) |
| Hooks and approvals | `.claude/settings.json`, tracked | `.codex/` hooks and rules, used only in a trusted project | `.codex/` is gitignored (.gitignore:14) |
| Memory | Per-user auto memory | None tracked | Never a repository fact's only owner |

#### The contract

1. **Directory layout.** Canonical skills in `.agents/skills/<name>/SKILL.md` with `references/` beside them. One wrapper per skill in `.claude/skills/<name>/SKILL.md`, frontmatter identical, body a one-line pointer. No runtime-specific text in a canonical skill.
2. **Wrapper parity test.** TestDocsSkillsAreDiscoverable fails when a canonical skill lacks a wrapper and when a wrapper lacks a canonical skill [F14-12]. Replacing wrappers with a symlinked directory is untested in both runtimes; do not do it without a measured check.
3. **Rules files versus AGENTS.md.** Anything an agent must obey in every session goes in AGENTS.md: authorization, the effect prohibition, pinned commands. `.claude/rules` hold only pointers to skills and references, never a rule stated nowhere else. Tier names live once, in verification.md; rules and AGENTS.md name commands only [F14-11].
4. **Hooks.** A hook is advisory unless the procedure it enforces is in the skill. Replace the inline Stop command with a tracked script that both runtimes can call [F14-02]:

```sh
#!/bin/sh
# scripts/hooks/stop-check: advisory end-of-turn check.
input=$(cat)
case "$input" in *'"stop_hook_active":true'*) exit 0 ;; esac
test "$(git rev-parse --git-dir)" = "$(git rev-parse --git-common-dir)" && exit 0
git diff --quiet HEAD -- && test -z "$(git ls-files --others --exclude-standard)" && exit 0
make focus >&2 || exit 2
```

   It skips the primary checkout, blocks at most once, and runs the focus tier the skill names for handoffs. verification.md documents its command and timeout. A Codex project hook may call the same script; that requires un-ignoring a narrow `.codex/hooks.json`, which is an owner decision.

5. **Approvals.** The effect prohibition is in AGENTS.md for both runtimes. Claude Code's ask list matches any path form of the binary. The skill index states that a Codex session needs the same commands under its own approval policy.
6. **Local-only overrides.** A tracked line says a local override must defer to the tracked AGENTS.md. The Claude Code equivalent is CLAUDE.local.md or `~/.claude/CLAUDE.md`; for Codex it is `~/.codex/AGENTS.md`. The local-only override file's content stays local and is not a subject of this report [F14-04].
7. **Vendor-specific guidance.** Model names, tiers, effort settings and IDs live in user-level files. The tracked reference keeps only vendor-neutral rules: acceptance before routing, explicit model and effort per worker, a stronger independent review for safety-class work, the unattended-run checklist [F14-09].

### 10.5 Token budget model

The base path today for any tracked edit is AGENTS.md, specs/index.md, specs/milestones.md, the skill index, code-implementation and its wrapper, and the skill listing: about 5.1k tokens (notes F14). The "today" column follows the path as written, including the whole milestone page AGENTS.md:8-9 mandates [F15-01]. The "after" column assumes the portfolio above plus section 9's milestone cards and section rows [F15-01] [F15-07]. Figures are estimates from bytes; section 10.6 measures them.

| Task class | Files loaded today (beyond base) | Tokens today | Files after (beyond base) | Tokens after |
| --- | --- | --- | --- | --- |
| Go fix in one capability | rules/go.md, go, formatting, verification refs, whole m3 page (32k), owning section | ~45k | rules/go.md, go ref, item section, owning section | ~16k |
| Ansible role fix | rules/ansible.md, ansible, formatting, verification refs, security-safety, architecture boundary sections, unsplit managed-os section, whole m1 page | ~42k | ansible ref (with digest item), security-safety, item section, one leaf section | ~20k |
| API field on one kind | ux-and-apis, go, formatting, verification refs, api.md, kind page section, semantic owner, whole m3 page | ~58k | ux-and-apis (with checklist), go ref, api.md sections named, kind section, item section | ~25k |
| Spec clarification | architecture skill, guidance rule, formatting, verification, whole 59 KB section | ~24k | architecture skill (rewritten), leaf section by index row | ~11k |
| Safety change | Go or Ansible set, security-safety, security.md proof, reconciliation sections, agent-operation.md | 60k+ | Go or Ansible set, security-safety, linked proof sections, no routing reference | ~30k |
| New implementation or arm | no procedure: search across architecture, substrates, coverage pages, knowledge | 50-70k (unbounded) | extend-implementation, architecture variation section, domain section, item section | ~25k |
| Milestone bookkeeping | milestone page, backlog and delivered by search or whole | 40k+ | milestones-and-knowledge, card, item section, ID search | ~9k |
| Knowledge page | full knowledge index, architecture 215-262, guidance rule | ~13k | milestones-and-knowledge, grouped index section | ~6k |
| Integration and commit | git-integration, verification, agent-operation | ~8k | git-integration, verification | ~6k |

The skill changes alone save little: the fixed cost grows by about 150 tokens of AGENTS.md text and the two new skills load only on trigger. The large savings come from the mandated read changing from whole page to item section, which this section's code-implementation draft encodes in step 1 and section 9 enables. The new skills buy precision, not tokens: they cover the two classes whose failures cost a full gate round or a safety defect.

### 10.6 Measurement plan

#### Task set

Seven tasks, each with an owner-written acceptance check, run against a frozen commit in disposable task worktrees. None runs a command with effects.

| # | Task | Class | Acceptance check |
| --- | --- | --- | --- |
| 1 | Fix a seeded defect in one capability's Go package | Go fix | Regression test fails before, passes after; `make check` green |
| 2 | Change a role so its effect's name prefix moves | Ansible, contract | Implementation version and digest moved; guard test green |
| 3 | Add an optional field to Machine | API change | Type, kind page, goldens and examples consistent; docs-check green |
| 4 | Clarify one reconciliation rule with a Not yet met line | Spec | One owner; exact form; docs-check green |
| 5 | Record a discovered defect as a parked item | Bookkeeping | Correct B number, columns, no rule restated |
| 6 | Design (not implement) a second storage implementation's touch set | Extension | Touch set matches a reviewer's list; no general-code edit |
| 7 | Classify three diffs (OSD erase, secret custody, help text) | Safety class | Two safety, one not, with the skill's reason |

#### Metrics

| Metric | Source | Target after |
| --- | --- | --- |
| Accepted-task tokens: input, cached input, output and reasoning, retries included | Claude Code transcript usage records; Codex session logs | At least 40 percent lower median on tasks 1, 3, 5 |
| Gate passes: `make focus` and `make check` green on the first integration attempt | Command log | No drop; task 2 passes the guard first time |
| Scope violations: files changed outside the task's allowed paths; effectful commands attempted | Diff against an allowlist; tool-call log | Zero |
| Re-reads: the same file read more than once; files read beyond the mandated set | Tool-call log | Re-reads halved; no whole milestone page read |
| Skill selection: expected skill loaded, unrelated skill not loaded | Tool-call log | Correct on every task |

#### How to run it

1. Freeze two commits: the baseline (17acb63d9) and a branch with the portfolio and section 9's restructuring.
2. Run each task three times per runtime per commit, with the model and effort set explicitly and recorded, from a fresh session in a fresh worktree, with the Stop hook disabled for the baseline to avoid its loop.
3. For Claude Code, read usage from the session transcript and tool calls from the same file. For Codex, read the session log. Normalize both to one CSV row per run.
4. A reviewer judges acceptance blind to the commit. A run that fails acceptance counts its tokens toward the next accepted run of the same task.
5. Report medians and ranges per task and runtime, and keep the CSV under the scratch area, not in the repository.
6. Repeat after any skill change that the docs byte budgets do not cover.

### Unverified observations

These are not verified findings.

1. The `.claude/rules/guidance.md` paths omit `.claude/**`, so editing a rule, wrapper or settings file does not trigger the guidance reminder (ledger note; not verified).
2. AGENTS.md:19-25 forbids tracked edits without an active slice yet asks agents to record discovered work as a milestone item, which is itself a tracked edit (ledger note; not verified).
3. Codex runs project-local hooks and rules only when the project's `.codex/` layer is trusted, so even a tracked Codex hook would not guarantee parity (runtime documentation; not verified against this repository).

## 11. Findings catalog

Every entry below survived two independent verifications (a refuter who tried to disprove it from the cited text, and a calibrator who checked impact, severity and the recommendation against recorded owner decisions), with a Fable arbitration where the two disagreed. The final severity is the lower of the two verifiers' calibrations unless an arbiter decided it, so the catalog errs toward understating severity. Finding ids keep the finder that raised them (F01 to F18 for the first round, R2-01 to R2-06 for the completeness round); duplicates found by other lenses are merged and named. High and Medium entries carry their full record; Low entries are tabled with their files and recommendation.

### 11.1 Distribution

| Section | High | Medium | Low | Total |
| --- | --- | --- | --- | --- |
| S3 Objectives, scope and domain | 0 | 3 | 5 | 8 |
| S4 Concept model and nomenclature | 0 | 2 | 12 | 14 |
| S5 Desired-state API | 0 | 12 | 28 | 40 |
| S6 Architecture and specialization | 1 | 13 | 15 | 29 |
| S7 Language responsibility | 0 | 4 | 6 | 10 |
| S8 Reconciliation rules | 6 | 28 | 36 | 70 |
| S9 Spec corpus | 1 | 4 | 14 | 19 |
| S10 Skills and harness | 1 | 4 | 6 | 11 |
| **All** | 9 | 70 | 122 | 201 |

By kind: gap 49, design 38, contradiction 31, ambiguity 22, naming 15, unsafe-rule 14, duplication 14, cost 10, process 8.

Arbitrated entries: 46. Entries whose severity verification lowered from the finder's proposal: 116.

### 11.2 High and Medium findings

#### S3: Objectives, scope and domain

##### F01-01 · Medium · Portfolio and family scope are one list; milestone completion needs family arms

Kind: design; Area: Objectives, scope and domain; Status: confirmed; Proposed severity: High.

Files: `specs/project.md:17-32`; `specs/milestones/m3.md:8-14, 41-46, 152`; `specs/milestones/m1.md:23-28, 44`; `specs/milestones/backlog.md:3394-3398, 3873-3875`; `specs/milestones/m7.md:11`; `specs/milestones.md:58-62`

**Evidence.** specs/project.md:19-32 lists 'bare metal through BMCs, libvirt, VMware vSphere and KubeVirt', 'load balancing, proxying, DNS ... registries', 'OpenShift and OKD', 'open-source, Red Hat and IBM Ceph' and then says 'These platform families bound Bootwright's intended scope' with no tier marking what is committed now. specs/milestones/m3.md:152 (Q1) justifies scheduling M3f with 'vSphere, the one product-scope substrate no other submilestone covers, gets a cluster path'. specs/milestones/m3.md:44-45 makes 'a run on a vSphere environment for M3f' part of M3's exit tier. specs/milestones/backlog.md:3398 (D158): 'All M1 items still gate M1's eventual completion', and M1f (vSphere, m1.md:44) and B68 Registry are M1 items. specs/milestones.md:60-62: 'Every item on a milestone page gates that milestone, whatever its Definition.' specs/milestones/m7.md:11: M7 'Requires: M4, so standalone rendering is the final milestone'.

**Consequence.** No document lets an agent or reviewer tell the committed portfolio (libvirt+sushy, vendor-BMC bare metal, OpenShift Virtualization machines, IBM Ceph, Fusion Data Foundation, NTP/proxy, provided networking) from the design envelope. Family members are therefore pulled into milestones on the strength of the scope list alone (Q1's rationale), and because every item gates its milestone, M1 cannot be done without vSphere and a managed Registry, M3 cannot be done without a vSphere run, and M7 (standalone rendering of already-qualified OpenShift and Ceph projections) waits for every M4 submilestone including GitLab. Portfolio work can be finished and still never reach 'done'.

**Recommendation.** Split project.md 'Product scope' into (a) a committed portfolio, each entry naming the milestone that delivers it, and (b) an extension envelope that design must not preclude but that no milestone gates on until the owner promotes it. Move family-only arms (M1f vSphere substrate, M3f vSphere clusters, and any service implementation outside the portfolio such as the managed Registry, DNS and load balancer if the owner agrees) to their own milestone or to parked items, so M1, M3 and M7 can complete on the portfolio.

Verifier notes:

- [refuter] Keep the project.md split (committed portfolio linking milestones vs extension envelope) as the smallest change; drop or make owner-optional the move of M1f/M4f, since D158 and D173 are owner decisions; instead require that a session-taken decision cannot add an envelope-only arm (as Q1 does) without owner promotion.
- [calibrator] Keep the smallest change: split project.md Product scope into a committed portfolio (each entry linking its milestone) and a design envelope that general code must not preclude. Present moving M1f out of M1 as an owner option that would amend D158 (owner-accepted), not as a defect fix; M3f relocation can ride Q1, which is still open. Drop the M7/GitLab claim or frame it as a D173 consequence. Acceptance…

**Acceptance.** project.md carries two labelled lists; every committed entry links a milestone; no milestone page outside an 'envelope' milestone holds an item whose only scope justification is an envelope entry; M3's exit tier and M1's item table no longer require vSphere unless the owner places vSphere in the committed list.

**Decision check.** D158 (backlog.md:3365-3400) already says vSphere and OpenShift Virtualization 'do not gate libvirt/bare-metal delivery' but keeps them as M1 items that gate M1's completion, so the recommendation extends D158's own intent and needs an owner decision to move M1f. D172 clause (1)/Q1 (vSphere read for M3f) is pending owner review, so moving M3f does not overturn an accepted decision. D18 and D173 (owner) put ACM, GitOps and GitLab into scope and into M4e/M4f; the recommendation does not remove them, it only asks the owner to classify them committed or envelope. Consistent with the project principle 'Introduce abstractions and extension points only for real consumers' (project.md:88).

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F01-02 · Medium · GitOps handoff leaves Bootwright-owned resources with no day-2 owner or exit

Kind: gap; Area: Day-0 / GitOps handoff boundary; Status: confirmed-by-arbitration; Proposed severity: High.

Files: `specs/project.md:46-53, 98-102`; `specs/state-reconciliation.md:25-30`; `specs/add-ons.md:398-402`; `specs/add-on-coverage.md:46`; `specs/milestones/m4.md:19-23`; `specs/milestones/backlog.md:403-407`

**Evidence.** specs/project.md:48-50: 'GitOps may then control downstream day-2 resources, without co-managing Bootwright-owned resources. Handoff does not transfer, adopt or reclaim ownership.' specs/state-reconciliation.md:27-29: 'A completed apply must be destroyed before an apply of changed desired state can start'. specs/add-ons.md:401-402: 'a GitOps source must exclude what the evidence lists'. specs/add-on-coverage.md:46: the gitops Subscription is 'Manual, declared startingCSV'. specs/milestones/m4.md:21-22: 'removing one add-on from a running cluster is parked as B769'. backlog.md:404-405: B106 day-2 surface 'stays parked until a product-scope change admits it'.

**Consequence.** After handoff, the operators, Subscriptions with Manual approval, MetalLB pools, Data Foundation objects, Ceph pools and cluster objects Bootwright created stay Bootwright-owned forever: GitOps must exclude them, Bootwright refuses any changed input over a completed apply, and the only way to end ownership without destroying the whole environment is the context-deletion orphan acknowledgement. A routine change such as approving an operator upgrade or widening a MetalLB pool has no sanctioned owner, so operators will either destroy a production environment or mutate Bootwright-owned objects behind its back, which 'bootwright diff --observe' then reports as drift indefinitely. The boundary is coherent as written but is not a handoff of the platform, only of 'downstream' resources the specs never define.

**Recommendation.** State in project.md the disposition of Bootwright-owned resources after handoff, as an owner decision with options: (a) an explicit per-cluster or per-environment relinquish verb that records ownership release (no deletion, destroy afterwards refuses for released blocks) so GitOps may adopt; (b) declare owned day-0 objects frozen and name who may change them (for example, operator-approved InstallPlans are outside Bootwright's ownership claims); (c) keep the current stance but define 'downstream day-2 resources' and state explicitly that any change to an owned object requires destroy and re-apply. Link the choice from the handoff gate.

Verifier notes:

- [refuter] Drop as a High design finding; at most report Low ambiguity: define 'downstream day-2 resources' in project.md:48 (e.g. resources not listed in any owned-object evidence) and link the destroy / --allow-orphans paths from the handoff paragraph. Post-handoff ownership change belongs to B106 under a product-scope decision.
- [calibrator] Adopt option (c) only: define 'downstream day-2 resources' in project.md, state that Bootwright-owned objects are frozen after handoff (changed only by destroy-and-apply per D153) and that operator upgrades/approvals of owned Subscriptions are out of scope until B106. Options (a) relinquish and (b) named post-handoff owner are product-scope changes that belong as a note on B106, not this fix; (a) also s…

**Acceptance.** project.md 'Lifecycle and handoff' defines 'downstream' resources and names the owner of every Bootwright-owned resource class after handoff; state-reconciliation.md and add-ons.md link that rule; either a parked or milestone item exists for the release mechanism, or the non-goal text names destroy-and-reapply as the only change path. Arbiter acceptance: project.md defines the term, states the frozen stance and links D153 and B106; add-ons.md:398-402 and state-reconciliation.md readiness section link to it.

**Decision check.** Checked D153 (backlog.md:3306; apply admits three scenarios, changed input refuses, further scenarios one per named slice), B106 (parked day-2 surface) and project.md's 'Handoff does not transfer, adopt or reclaim ownership'. Option (c) is fully consistent with them; options (a) and (b) would amend the project.md ownership sentence and need an owner decision recorded as a new D. No recorded decision addresses post-handoff ownership of owned objects, so this is an open decision rather than a contradiction of one.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### F17-02 · Medium · Milestone Requires encode priority and tie portfolio completion to family scope

Kind: design; Area: Milestone structure; Status: confirmed-by-arbitration; Proposed severity: High.

Files: `specs/milestones.md:13-21`; `specs/milestones/m1.md:23-28, 37-45`; `specs/milestones/m3.md:27-42, 95-102`; `specs/milestones/m4.md:25-37, 73-80`; `specs/milestones/m7.md:11-12`; `specs/milestones/backlog.md:3386-3398`

**Evidence.** milestones.md:17 M3 Requires "M2; applicable M1c to M1g; B18/B23/B265"; milestones.md:18 M4 Requires "M2, M3c"; m7.md:11 "Requires: M4, so standalone rendering is the final milestone". m3.md:102 "M3f | Any-size OpenShift on VMware vSphere, pending Q1". m1.md:44 "M1f | vSphere substrate handling | Not started: B99 needs definition". backlog.md:3398 (D158) "All M1 items still gate M1's eventual completion." m3.md:38-42 "M2 is waived for the implementation of M3b to M3d's slices only". m4.md:32-34 "M2 and M3c are waived for the implementation of M4b to M4f's slices only".

**Consequence.** No milestone covering the owner's immediate portfolio (libvirt+sushy, xFusion bare metal, KubeVirt, IBM Ceph, FDF, NTP/proxy) can be declared done: M1 waits on vSphere (M1f), M3 on vSphere (M3f) and on all of M2 including physical stretch, M4 (NMState, MetalLB) on all of M2 and on ACM, GitOps and GitLab. Every new program needs another waiver (D134, D143, D159, D172, D173), and M3's Status row omits the M4 items (B83, B84, B735, B758) its own M3d depends on.

**Recommendation.** State Requires per submilestone, by real dependency (M3b needs M1c and M1d; M3e needs M1g; M4d needs M2b and M3c; M4b needs M3b), and drop milestone-to-milestone Requires that are only ordering. Either move vSphere (M1f, M3f) and other family-scope arms (M4e/M4f if the owner agrees) into a later milestone, or add a per-submilestone tag portfolio/family and let a milestone be declared done for its portfolio submilestones. Show cross-milestone item dependencies in the Status Requires cell.

Verifier notes:

- [refuter] Drop as a High design finding; at most list as considered-and-refuted or a Low note: add M3d's cross-milestone M4 items (X136/X138/X134/X143) to the milestones.md Status Requires cell, and optionally offer the owner a portfolio/family tag as a proposal superseding D158, not as a defect.
- [calibrator] Lead with the smallest change: per-submilestone Requires cells on m1-m4 and milestones.md Status Requires that list true prerequisites, including M3d's dependency on M4b/M4c items. Present moving M1f/M3f (and M4e/M4f) or a portfolio tag as an owner decision superseding D158's 'All M1 items still gate M1', not as part of the fix. Correct the waiver citation to D174. Acceptance: drop 'no current program n…

**Acceptance.** Each submilestone row on m1-m4 has its own Requires cell; milestones.md Status Requires lists only true prerequisites including cross-milestone items; no current program needs a recorded Requires waiver to start its in-tree slices; the portfolio submilestones can reach done without vSphere, stretch or GitLab.

**Decision check.** Conflicts with D158 (M3 retains M2 as prerequisite; M1f and M1g belong to M1; all M1 items gate M1) and with D172/D173 groupings; D158 itself already says vSphere and OpenShift Virtualization do not gate libvirt or bare-metal delivery and consumers require only the foundations they use, so the recommendation generalizes that intent. Requires an explicit owner decision superseding the D158 clauses; Q1 (M3f read as vSphere) is still open. Arbiter decision check: Does not conflict with D158 when limited to per-row Requires cells; the restructuring option would supersede D158's 'All M1 items still gate M1's eventual completion' and needs an explicit owner decision; Q1 remains open.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

#### S4: Concept model and nomenclature

##### F02-01 · Medium · Bare 'controller' means both the Bootwright host and a machine's BMC

Kind: ambiguity; Area: Concept model and nomenclature; Status: confirmed; Proposed severity: High.

Files: `specs/index.md:48-49`; `specs/substrates.md:56-65`; `specs/api/machines.md:59-71`; `specs/api/machines.md:292-304`; `specs/cli/commands.md:110`

**Evidence.** specs/index.md:48-49 "**Controller**: the host that runs Bootwright. A **management controller** is a server's out-of-band Redfish endpoint". specs/substrates.md:59-65 "the controller arm when it is the Environment controller ... its Machines are reached at the controllers they declare, always from the controller Machine." specs/api/machines.md:65 "the one anchor of the controller-to-BMC leg" versus machines.md:69-71 "a controller presenting a self-signed certificate that is not a CA cannot be anchored by one" (here the BMC). machines.md:301-303 "such an installation's insert first reads whether the controller verifies the artifact server" (the BMC). specs/substrates.md uses "controller" 88 times, "management controller" 7 times.

**Consequence.** The glossary's reserved meaning is not followed, so the reader must infer from context which party a TLS, trust-bundle, reservation or destructive-request rule applies to. An implementer or agent can attach the trust bundle, the verification check or the reservation to the wrong leg (host versus BMC); an operator reading a refusal or remedy that says "controller" cannot tell whether to fix the Bootwright host or the BMC.

**Recommendation.** Apply the glossary rule mechanically: bare "controller" means only the Bootwright host (the Environment controller Machine); every BMC reference says "BMC" or "management controller". Sweep specs/substrates.md, specs/api/machines.md, specs/managed-os.md and specs/cli*.md, and add a docs-check that flags bare "controller" in sentences that also name Redfish, BMC, virtual media or TrustedModules.

Verifier notes:

- [refuter] Keep the wording sweep; also cover the emulated-BMC passage substrates.md:386-499 and refusal message text. Make the docs-check optional/heuristic, since hardware 'controller' (virtio-SCSI, controller-0) is legitimate.
- [calibrator] Lower to Medium. Keep the mechanical sweep, and extend it to refusal and diagnostic message strings (for example substrates.md:91) and their code goldens. Drop the docs-check, or make it optional and advisory, because a sentence-level check that matches BMC or Redfish would flag correct uses such as 'controller-to-BMC leg'. Acceptance: every bare 'controller' in the swept pages and refusal strings refer…

**Acceptance.** In the swept pages every occurrence of bare "controller" refers to the Bootwright host; grep for "controller" in specs/substrates.md shows BMC meanings only as "BMC" or "management controller"; docs-check fails on a reintroduced bare use in a Redfish sentence.

**Decision check.** Checked D7 (placement hosts connect as root), D103/D120 (verified controller leg, emulated controller serves TLS): D120 itself says "emulated controller" for the BMC, so the recommendation changes wording only, not any decided behavior. Consistent with the glossary already in specs/index.md and with the one-owner-per-fact principle.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F02-04 · Medium · Kind catalog size is stated as 33 and 27 while the code registers 34

Kind: contradiction; Area: Concept model and nomenclature; Status: confirmed; Also raised as: F03-10, R2-05-06.

Files: `specs/api.md:183`; `specs/api.md:262-275`; `specs/api/environment.md:240-242`; `specs/api/storage.md:256-273`; `api/v1alpha1/object.go:12-47`; `.agents/knowledge/api-alignment.md:12`; `specs/api.md:183,262`; `internal/desiredstate/compilation/decoding.go:279-284`; `specs/api.md:262`; `.agents/knowledge/index.md:10`

**Evidence.** specs/api.md:183 "One of the 33 registered kinds"; api.md:262 "The `v1alpha1` catalog contains exactly 33 authored kinds" while its own table lists 34 names including `StorageReplication`. specs/api/environment.md:240-241 "`spec.defaults` is a closed map keyed by exact, case-sensitive names from the 27-kind catalog." api/v1alpha1/object.go:12-47 declares 34 `Kind` constants. specs/api/storage.md:258-273 Kinds table has no `StorageReplication` row; it is owned only by specs/storage-resources.md:238. Also reported as F03-10: environment.md:240-241 "`spec.defaults` is a closed map keyed by exact, case-sensitive names from the 27-kind catalog." api.md:262 "The `v1alpha1` catalog contains exactly 33 authored kinds". decoding.go:279-283 accepts any key for which `api.Schema(api.Kind(key))` is registered. Also reported as R2-05-06: environment.md:240-241 "spec.defaults is a closed map keyed by exact, case-sensitive names from the 27-kind catalog". api.md:262 "The v1alpha1 catalog contains exactly 33 authored kinds". knowledge index:10 "27-kind catalog with StorageClient".

**Consequence.** The closed key set of `spec.defaults` is contractually ambiguous (27 or 34 kinds); an agent implementing or reviewing kind defaults, or counting coverage, trusts a wrong number; the index routes "one kind's fields" to api/storage.md, which does not list one admitted kind. No test pins the count, so the drift recurs with each new kind.

**Recommendation.** Replace the literal counts with "every kind in the catalog below" (or keep one number in api.md and have docs-check compare it with `len(v1alpha1.Kinds())`); make environment.md link to the catalog instead of restating a count; add the StorageReplication row to the api/storage.md Kinds table with its admission rules or link; refresh the knowledge page.

Verifier notes:

- [refuter] Keep; for api/storage.md either add the StorageReplication row or a pointer to storage-resources.md#storagereplication, since its field contract already lives there.
- [calibrator] Drop literal counts (preferred over a docs-check count pin) and link environment.md:241 to specs/api.md#kind-catalog. Have the StorageReplication row in api/storage.md link to storage-resources.md#storagereplication rather than restate fields. Acceptance: a docs-check that every Kind constant appears in the api.md catalog table.

**Acceptance.** No spec states a kind count other than the one checked against code; docs-check fails when a kind is added without a catalog row; api/storage.md lists every Storage* kind in object.go.

**Decision check.** No recorded decision fixes the count. The recommendation follows the one-owner-per-fact convention of specs/index.md:34-37 and the docs-check principle that spec tables are checked against code (AGENTS.md command list).

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

#### S5: Desired-state API

##### F03-01 · Medium · Four union idioms select an implementation, chosen by an untestable criterion

Kind: design; Area: Desired-state grammar: unions; Status: confirmed; Proposed severity: High.

Files: `specs/api.md:219-235`; `specs/api/storage.md:260`; `specs/api/machines.md:19-24`; `specs/api/container-clusters.md:57,76`; `specs/api/infrastructure-services.md:22,72-77`; `examples/add-ons/lab-compact-df/storage/cluster.yaml:8-11`

**Evidence.** specs/api.md:219-221 "A discriminated union retains `type` when it identifies a meaningful domain variant, including `StorageCluster`, `StoragePool`, `StorageExport`, and container installation platforms." specs/api.md:228-230 "Exactly one implementation arm selects `InfraProvider` and `ClusterAddon`; their former `type` fields are unknown." specs/api/storage.md:260 "`spec.type` is exactly `ceph`; ... `spec.ceph` is required for `managed`". specs/api/machines.md:21-22 "The arm name selects the substrate; `spec.type` is not accepted." specs/api/container-clusters.md:76 "`spec.install.method` ... `agent` is the only accepted method." alongside a `spec.install.agent` arm (line 80). specs/api/infrastructure-services.md:22 "`spec.implementation` | string | ... Required implementation listed below".

**Consequence.** The same semantic choice is spelled as a presence arm (InfraProvider), a `type` plus same-named arm (StorageCluster `type: ceph` + `ceph:`), an enum plus sibling arm (`install.method: agent` + `agent:`) and a bare enum (`implementation: squid`). 'Meaningful domain variant' cannot be checked, so the next storage type (NetApp, Pure), cluster type (vanilla Kubernetes) or service implementation will pick an idiom by taste; operators learn four refusal shapes; kind defaults rule 6 must handle each idiom; StorageCluster forces a redundant `type` that must agree with the arm.

**Recommendation.** State one rule in api.md#unions: (1) implementation or engine choice is a presence union whose arm name is the implementation and which carries no `type`; (2) `type` is an enum only for a domain variant that has no implementation-specific configuration block (StoragePool replicated|erasure, Secret types, Entitlement product); (3) never both an enum and a same-named arm. Apply it: drop `StorageCluster.spec.type`, fold `install.method` into the `install.agent` arm presence, express service `implementation` as an arm (`squid: {}`) or default it. Add an architecture test that rejects a schema shape carrying both a discriminator and an arm with the discriminator's value.

Verifier notes:

- [refuter] Keep the testable-criterion rule in api.md#unions, but do not simply drop StorageCluster.spec.type: external management has no arm, so either keep type for engine identity with arm-optional semantics, or introduce an engine arm usable for external (e.g. ceph: {external...}). Same for install.method when the agent arm is empty. Route as substantial API change to the owner (D133).
- [calibrator] Keep the testable rule (never an enum whose value equals a sibling arm name; implementation choice with config is a presence arm). For StorageCluster, replace type+ceph with an engine arm that exists for both managed and external (e.g. ceph: {management: external, ...}) rather than just dropping type. Leave service `implementation` as an enum per rule (2), or default it; do not require `squid: {}`. Fold…

**Acceptance.** api.md#unions states the rule with a testable criterion; no registered schema has a `type`/`method`/`implementation` enum whose value equals a sibling arm name; StorageCluster examples carry no `type: ceph`; an architecture test enforces the rule over api/v1alpha1 schemas.

**Decision check.** Checked D132/D133 (Ceph API review; native service documents approved; substantial API changes need owner approval with current/proposed examples): the recommendation is a substantial change and must go to the owner as such, but it keeps D133's native documents untouched. D142 (no backward compatibility before 1.x) means removing `type: ceph` needs no alias. D94 precedent removes dead fields. No decision records why StorageCluster keeps `type` while InfraProvider forbids it (grep of backlog and storage-api-review found none).

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F03-02 · Medium · Reserved directory name 'secrets' silently skips Secret YAML with no consumer

Kind: gap; Area: Desired-state input: directory layout; Status: confirmed; Proposed severity: High.

Files: `specs/api/input.md:30-35,72-78`; `specs/api/secrets.md:54-60`; `internal/desiredstate/input_rules.go:9`; `examples/README.md:86-89`

**Evidence.** specs/api/input.md:31-35 "discovery does not descend into ... a non-root directory whose basename is exactly `vendor`, `node_modules`, `playbooks`, `roles`, `collections`, `manifests`, or `secrets`." specs/api/input.md:76-77 "`validate` warns deterministically for excluded files declaring Bootwright objects and for excluded cluster roots." (only resource-excluded files). specs/api/secrets.md:56-58 "The `file` arm is retired ... desired state names no path that a lifecycle operation reads after admission." Both examples place descriptors in `secret-descriptors/`.

**Consequence.** An operator who writes Secret descriptors into the most natural directory, `secrets/`, gets no warning; validation reports every consumer's `api.reference` to an unknown Secret, whose remedy cannot point at the real cause. The reservation existed for co-located Secret payload files, which the retired `file` arm no longer reads, so the trap buys nothing.

**Recommendation.** Remove `secrets` from the reserved payload basenames (it has no remaining input consumer), or, if a consumer is planned, have `validate` emit a deterministic warning naming each skipped reserved directory that contains `.yaml`/`.yml` files (names only, no read), and make an unresolved-reference remedy mention skipped reserved directories when any exist.

Verifier notes:

- [refuter] Prefer the warning option over removal, because the skip still shields plaintext secret YAML kept beside the input. Fix the stale input.md:81-83 rationale so it names the purpose that remains (keeping operator secret files out of discovery). Have validate warn, using names only, when a skipped reserved root holds .yaml/.yml files, and make the unknown-Secret remedy mention it. Severity Medium.
- [calibrator] Prefer the warning option. Keep `secrets` skipped. Have validate emit a deterministic warning naming each skipped reserved directory that holds .yaml/.yml files (names only, no read). Have the unresolved-Secret-reference remedy mention those directories. Remove the stale 'co-located Secret' clause in input.md:73-74, and restate why `secrets` stays reserved: it keeps raw secret material from being read d…

**Acceptance.** Either `secrets` no longer appears in input.md#discovery and `skippedDirectoryNames`, and a fixture with descriptors under `secrets/` validates; or a fixture with YAML under a skipped `secrets/` produces the warning and the reference remedy names the directory.

**Decision check.** Checked the copied-input rule (project.md design priorities; memory: Secret file-source exception removed) and D142: the retirement of `source.file` removed the only spec consumer of the `secrets` input root (grep of specs for a `secrets/` input consumer finds only the context-store subtree, specs/secrets.md:386). Security discovery rules (specs/security.md:137-140) reference the list generically and are unaffected by shrinking it.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F03-03 · Medium · The grammar makes operators repeat facts and then checks the copies agree

Kind: design; Area: Desired-state grammar: defaults and derivation; Status: confirmed; Proposed severity: High.

Files: `specs/api/infrastructure-services.md:72-97`; `specs/api/machines.md:82-85,481-488`; `specs/storage-native-api.md:259-262`; `specs/api/environment.md:112-123`; `examples/machines/lab-rhel/infra/components/dns.yaml:6-15`; `examples/machines/lab-rhel/secret-descriptors/artifact-server-tls.yaml:9-19`; `examples/add-ons/lab-compact-df/storage/cluster.yaml:119-140`

**Evidence.** infrastructure-services.md:79-83 "A wildcard default binds every address, so it collides with any other listener on that port ... and the examples declare an explicit bindAddress." :90-91 "Endpoint addresses are derived values, not another authored copy of Machine addresses." :94-96 "When `bindAddress` is not a wildcard ..., every endpoint whose address is an IP equals it". machines.md:85 "its SANs cover `bindAddress`, and admission refuses a generated one whose `ipAddresses` do not." storage-native-api.md:259 "`StorageFilesystem.spec.mdsServiceRef` is exactly `mds.<filesystem>`".

**Consequence.** In lab-rhel the controller IP 192.0.2.1 is written six times (Machine address, four service `bindAddress` values, a certificate SAN) and checked for agreement; the documented default (`0.0.0.0`) is one the spec itself says collides, so every operator must override it. `mdsServiceRef` admits exactly one derivable value. Storage nodes restate `name`, `fqdn` and `machineRef` for the same host because node FQDN composition differs from Machine FQDN composition. Editing one copy produces a refusal rather than a consistent change; kind defaults cannot help because each service kind needs its own entry.

**Recommendation.** Derive instead of comparing: (1) replace `bindAddress` with `bind: {addressRef: <machine address>}` defaulting to the single endpoint's address, with wildcard only as an explicit choice; (2) let a generated certificate take SANs from its consumers' endpoints (`subjectFrom: {consumers: true}`) or default them from the referencing service; (3) drop `mdsServiceRef` (derive `mds.<metadata.name>`); (4) default a storage or container node's `fqdn` to its Machine's `fqdn` address when the node binds a Machine; (5) stop authoring derived `fqdn` addresses in examples. Record in api.md the principle: a value with exactly one valid derivation is derived, not authored and compared.

Verifier notes:

- [refuter] Cite D114 and B333: point (1) is already parked as B333 with a stated digest-migration reason, so recommend scheduling B333 (pre-1.x no-compat principle weakens that reason) rather than presenting it as new. Keep (2)-(5) and the derivation principle for api.md; lower severity to Medium.
- [calibrator] Acknowledge D114 and fold (1) into B333, noting that the pre-1.x no-compatibility stance removes the digest-window reason for deferring it. Reword (4): fqdn is already optional; drop it from examples rather than change the schema. Keep (2), (3) and the api.md derivation principle. Narrow acceptance criterion 2 to the named fields (bindAddress, generated SANs, mdsServiceRef).

**Acceptance.** The lab-rhel example states the controller IP once; no schema field admits exactly one value derivable from other fields; api.md#defaults-normalization-and-effective-state states the derivation principle; effective output still shows the derived values.

**Decision check.** Checked D133 (native service documents keep their native shape): recommendation (3) removes a Bootwright-side reference, not a native field, so D133 holds. Checked D64 (session SSH address defaults derived from install address/fqdn): the same derive-don't-repeat pattern is already accepted there. The Ceph hostname contract (memory: nodes[].fqdn override repaired; ADR 0035/0036) must be reread before (4); recommendation keeps an explicit `fqdn` override. Memory principle 'MTU = capability, not use: NEVER derive a guest MTU' is unaffected (no MTU derivation proposed).

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F04-01 · Medium · Machine holds substrate facts as scattered conditional rules, not an arm

Kind: design; Area: Desired-state API: Machine / InfraProvider; Status: confirmed; Proposed severity: High.

Files: `specs/api/machines.md:180-198`; `specs/api/machines.md:217-219`; `specs/api/machines.md:249-257`; `specs/api/machines.md:313-320`; `specs/deferred/machine-arms.md:64-65, 83-85`

**Evidence.** machines.md:185 profileRef: "required for a virtual install machine and forbidden for bare metal"; machines.md:188 macAddress: "required on every bare-metal install NIC; refused on a libvirt provider's Machine"; machines.md:189 bmc: "Required for bare-metal install; refused on a libvirt provider's Machine"; machines.md:314-316: "On a Machine whose provider is `libvirt`, each of `wwn`, `hctl` and `serialNumber` refuses"; deferred/machine-arms.md:83-85: "A vSphere-authored MAC is in the manual assignment range"; deferred/machine-arms.md:64-65 puts vSphere-only `template` and `failureDomainRef` in the common profile shape.

**Consequence.** The provider-neutral/provider-shaped split is explicit on InfraProvider but implicit on Machine. Each new substrate (vSphere, KubeVirt, later AWS/GCP/Azure) adds more 'on a <substrate> provider, field X refuses/requires' rows across Machine's table and admission code, instead of one arm. An operator can author fields that the chosen substrate silently cannot use until admission refuses them one by one. A cloud substrate with no BMC, NICs or root-device hints would add a further layer of conditionals. This contradicts the brief's goal that specifics live behind arms selected by input.

**Recommendation.** Add a Machine-side substrate arm that mirrors the provider arm: `spec.substrate: {providerRef, baremetal: {bmc, nics, rootDevice}} | {libvirt: {profileRef}} | {kubevirt: {profileRef}} | {vsphere: {profileRef, failureDomainRef}}`. Admission requires the arm to match the provider's arm. Move vSphere-only profile fields into the vSphere arm's profile shape. Keep provider-neutral intent (os, network, access, proxy, placement) at the top level.

Verifier notes:

- [refuter] Keep NIC names and root-device hints provider-neutral at top level (libvirt uses them); put only substrate-specific facts (bmc, macAddress, wwn/hctl/serial hint admissibility, profileRef, failureDomainRef, interfaceAttachments) in per-substrate arms, or alternatively keep the flat shape but derive the per-substrate admissibility from one substrate-arm registry table rather than per-row prose. Downgrade to…
- [calibrator] Keep NIC names and the substrate-neutral root hints (deviceName, model, vendor, minSizeGigabytes, rotational) at top level. Move only the truly substrate-owned facts into the arm: baremetal {bmc, nics[].macAddress, wwn/hctl/serialNumber hints}, libvirt {profileRef}, vsphere {profileRef, failureDomainRef, template, MAC}, kubevirt {profileRef, interfaceAttachments}. Relax acceptance to: no top-level Machi…

**Acceptance.** No row of the Machine field table in specs/api/machines.md names a substrate in its rule. A test proves that a Machine arm differing from its provider's arm refuses at `spec.substrate`. Adding a substrate is documented as touching only the InfraProvider arm, the Machine substrate arm, the attachment arm and the substrate port entry points.

**Decision check.** D94 (dead InfraProvider/Machine fields removed; libvirt MACs and BMC refuse) and D44 (wwn/hctl/serial refused on libvirt) are kept: the fields simply do not exist in the libvirt arm. The pre-1.x no-backward-compatibility principle makes the move cheap. B87/B99 revive the KubeVirt/vSphere arms, so the restructure is cheapest before that revival. No conflicting decision was found.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F04-02 · Medium · Machine.capabilities mixes host prerequisites with product-named roles

Kind: design; Area: Desired-state API: Machine capabilities; Status: confirmed; Proposed severity: High; Also raised as: F18-11.

Files: `specs/api/machines.md:77, 182`; `specs/api/container-clusters.md:436`; `specs/api/infrastructure-services.md:47-50`; `specs/api/environment.md:369-370, 471`; `internal/storage/topology.go:177-178`; `examples/machines/lab-rhel/infra/machines/controller.yaml:7-9`; `specs/api/machines.md:182`

**Evidence.** machines.md:182: "Unique values from `openshift-node`, `ceph-node`, `ceph-arbiter`, `container-runtime`, and `libvirt`; `ceph-arbiter` requires `ceph-node`." container-clusters.md:436: "Every `machineRef` requires capability `openshift-node`". machines.md:77: "the machine has capability `libvirt`". environment.md:471: "controller stage prepares only the container-runtime and libvirt capabilities". topology.go:177-178 enforces ceph-node in code, but no storage spec states it. `ceph-arbiter` has no consumer in specs/ or internal/ beyond the requires-ceph-node check. Also reported as F18-11: api/machines.md:182 capabilities are "Unique values from openshift-node, ceph-node, ceph-arbiter, container-runtime, and libvirt". api/container-clusters.md:436 "Every machineRef requires capability openshift-node".

**Consequence.** The operator states every role twice: once by referencing the Machine from a ContainerCluster, StorageCluster or InfraProvider, and again in `capabilities`. Forgetting either half is a refusal. `openshift-node` hard-codes one cluster product into Machine, so vanilla Kubernetes, Rancher or EKS nodes would need new enum values in a core kind. That contradicts the low-code-impact goal. A rule enforced only in code (`ceph-node`) and a value nothing reads (`ceph-arbiter`) make the contract untruthful.

**Recommendation.** Drop the role values (`openshift-node`, `ceph-node`, `ceph-arbiter`): membership in a cluster or storage topology is the role. Derive host prerequisites from placements: `container-runtime` from a managed-service `machineRef` or controller selection, and `libvirt` from an InfraProvider host reference. Remove the field, or keep it only for prerequisites no placement implies, renamed `hostPrerequisites`.

Verifier notes:

- [refuter] Keep the drop of role values (openshift-node, ceph-node, ceph-arbiter) and the statement of placement-derived roles; for ceph-arbiter, note arbiter selection belongs to StorageCluster topology. For container-runtime/libvirt, derive from placements where a placement exists, but keep (or rename hostPrerequisites) an authored path for the controller Machine and update controller.md:24 and environment.md:471 a…
- [calibrator] Stage it. First, the smallest truthful fix: state the ceph-node rule in the storage API spec or drop it, and drop ceph-arbiter, since StorageCluster topology owns the arbiter. Second, the design change: derive container-runtime and libvirt from placements (managed-service machineRef, controller selection, libvirt InfraProvider machineRef) and drop the role values. Specify the derived controller-stage in…

**Acceptance.** specs/api/machines.md has no role enumeration. No consumer spec requires a capability for a relationship it already references. The controller-stage install set is specified as derived from placements, with a test. ceph-node enforcement is either removed or stated in the owning storage spec.

**Decision check.** The memory principle 'Bootwright owns host packages' (setup and the controller stage install them) is preserved, because the prerequisite set is still computed, only no longer authored. The arbiter switch is a StorageCluster day-1 decision recorded in the storage milestone, so a Machine tag adds nothing. D163 (no version rules) does not apply. No recorded decision mandates authored role tags.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### F05-02 · Medium · Storage admission page omits StorageReplication and nativeName, says mirroring refuses

Kind: contradiction; Area: Desired-state API: Storage admission; Status: confirmed; Proposed severity: High.

Files: `specs/api/storage.md:1-9, 90-93, 124-130, 256-300, 387-395`; `specs/storage-resources.md:9-16, 238-262`; `specs/api.md:273`; `specs/milestones/m2.md:1262-1267`

**Evidence.** specs/api/storage.md:3 "This page owns storage admission." At :393-394 it says "`mirroring` refuses at plan naming [B483]", and at :93 "Other P2 resource additions remain target contracts, not admitted kinds." Its Kinds table (:258-273) lists 14 kinds and no StorageReplication, and its References table has no `peerClusterRef`, `peerPoolRef` or `imageRefs`. Against this, specs/storage-resources.md:13-15 says "`StorageReplication` for one-way journal RBD mirroring... are admitted and realized", and :261 says "Both pools must explicitly author the same `mirroring.mode`". specs/api.md:273 lists StorageReplication among the Storage kinds. specs/milestones/m2.md:1262 says "Delivered in-tree (X59, RBD relationship): StorageReplication and StoragePool.nativeName".

**Consequence.** An operator or agent reading the owning admission page concludes that RBD mirroring is refused and that the kind does not exist. Two outcomes follow. They author no replication, or they implement a refusal that contradicts delivered code. Worse, an agent that "fixes" the code to match the owning page removes a delivered feature. The References table also leaves three reference fields without a namespace owner.

**Recommendation.** On specs/api/storage.md, do four things. Add a StorageReplication row to Kinds. Add `peerClusterRef`, `peerPoolRef`, `imageRefs` and `nativeName` to References. Replace the :393-394 sentence with the realized rule: `mirroring.mode` is admitted only with a StorageReplication owner, and otherwise refuses, naming that owner. Add replication to the "Ordinary apply realizes" list at :124-130. Then add a docs-check test that compares the Kinds table with the registered Storage kinds in api/v1alpha1/object.go.

Verifier notes:

- [refuter] Keep the recommendation; the docs-check test comparing the Kinds table with api/v1alpha1 kindOrder is optional hardening. Also correct :93 and add nativeName (StoragePool) to References.
- [calibrator] Smallest fix: add StorageReplication to Kinds with a short admission paragraph near :85-93 linking the resource record; add peerClusterRef/peerPoolRef (StoragePool), peerClusterRef (StorageCluster) and imageRefs (StorageRBDImage) to References. nativeName is not a reference: mention it as a StoragePool field/identity line, not in the References table. Replace :393-394 with 'mirroring.mode is admitted on…

**Acceptance.** Both api/storage.md tables list all 15 Storage kinds and their reference fields. Nothing on the page says pool mirroring refuses unconditionally. A repocheck test fails when a registered Storage kind is missing from the table.

**Decision check.** Checked D160 (OD18 chose (A): "StorageReplication, `StoragePool.nativeName` and a secondary gateway reference", recorded at milestones/m2.md:324) and D133 (P2 resource graph). The recommendation documents what those decisions approved, and conflicts with none.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### F05-03 · Medium · Storage* kinds are named neutrally but are Ceph-only, with no type-binding rule

Kind: naming; Area: Desired-state API: Storage kinds; Status: confirmed; Proposed severity: High; Also raised as: F02-11, F18-05.

Files: `specs/api/storage.md:256-273, 302-307, 339-360`; `specs/storage-api-review.md:31-33`; `specs/project.md:17-30`; `specs/api/storage.md:256-273`; `specs/api/storage.md:302-307`; `specs/architecture.md:55`; `specs/api/storage.md:256-273, 302-307`; `specs/storage.md:3`; `cmd/bootwright/wiring_reconciliation.go:232-234`

**Evidence.** specs/api/storage.md:302-303 "Storage children select their owner through `spec.clusterRef`; they do not repeat the cluster's `ceph` implementation arm." Every child is Ceph-specific. At :262 `StorageClient` is "One CephX principal". At :261 `StoragePlacementPolicy` is "A reusable CRUSH placement policy". At :355-357 pool roles derive the application "(`rbd`, `cephfs`... `rgw`)". At :265 a filesystem binds "the native MDS service". At :267 a gateway has "its realm, zone group and zone". specs/storage-api-review.md:32-33 reads: "No evidence justifies renaming these merely for uniformity." Also reported as F02-11: api/storage.md:262 "`StorageClient` | One CephX principal"; :261 "`StoragePlacementPolicy` | A reusable CRUSH placement policy"; :264 "`StorageRBDImage` | One RBD image"; :302-303 "Storage children select their owner through `spec.clusterRef`; they do not repeat the cluster's `ceph` implementation arm." Also reported as F18-05: specs/architecture.md:55 Storage owns "Ceph cluster topology, placement, pools, filesystems, gateways, NFS services, exports". api/storage.md:260 StorageCluster "spec.type is exactly ceph". :262 StorageClient is "One CephX principal". :302-303 "Storage children select their owner through spec.clusterRef; they do not repeat the cluster's ceph implementation arm". storage.md:3 "This page owns the target runtime contract for managed Ceph."

**Consequence.** Suppose a NetApp ONTAP or Pure cluster is added as `StorageCluster.spec.type: ontap`. Then either `StoragePool` and `StorageClient` take on a second meaning, so CRUSH roles and CephX caps must branch on the cluster type in shared validation. Or ONTAP gets its own kinds, and the neutral-sounding names permanently mislead: `StoragePool` that can only point at a Ceph cluster. Nothing in the spec says a child whose `clusterRef` names a non-Ceph cluster refuses, so the first new SDS has to discover and retrofit that rule.

**Recommendation.** Pick one of two options and state it on api/storage.md. (a) Preferred, pre-1.x: rename the Ceph-specific children to `Ceph*` (CephPool, CephPlacementPolicy, CephClient, CephRBDImage, CephFilesystem, CephSubvolume, CephObjectGateway/User/Bucket, CephNFSExport, CephSMBResource, CephNVMeoFSubsystem, CephReplication). Keep `StorageCluster` as the neutral union and keep a neutral consumer-attachment kind. (b) Keep the names, but add one sentence and an admission rule: every child requires a `clusterRef` whose `spec.type` is `ceph`, and another provider adds its own kinds. Either way, add a short "adding a storage type" paragraph.

Verifier notes:

- [refuter] Downgrade to Medium (Low defensible). Prefer option (b): one sentence on api/storage.md that the Storage* children are the Ceph arm's resource kinds and a future storage type brings its own kinds, plus a plan-time refusal when clusterRef's type differs. Option (a) rename requires an explicit owner decision revisiting D133; frame it as a decision item, not a defect fix.
- [calibrator] Lead with option (b) as the recommendation: one sentence on api/storage.md that child kinds are Ceph-specific and require a clusterRef whose spec.type is ceph, plus an 'adding a storage type' note that a new type adds its own kinds. Present (a) only as an owner-decision alternative revisiting D133's naming baseline. Note the admission test is only meaningful once a second spec.type exists; until then ac…

**Acceptance.** api/storage.md states which kinds are provider-neutral and which are Ceph-specific. Admission refuses a Ceph child whose cluster is of another type, and a test covers it. Under option (a), no kind named `Storage*` carries Ceph-only vocabulary.

**Decision check.** Checked D133. It approved P1 and P2 and kept the envelope, and the review text "No evidence justifies renaming these merely for uniformity" argues against renaming for uniformity. Provider neutrality is a different reason that the review did not weigh, and this lens supplies the evidence it asked for. Option (b) conflicts with no decision. Option (a) needs an explicit owner decision because it revisits D133's naming baseline. The pre-1.x no-compatibility principle makes (a) cheap now.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F05-06 · Medium · ContainerCluster admits eight inert fields that plan accepts silently

Kind: unsafe-rule; Area: Desired-state API: ContainerCluster truthfulness; Status: confirmed.

Files: `specs/container-clusters.md:45-63`; `specs/api/container-clusters.md:191-198`; `specs/api.md:21-29`

**Evidence.** specs/container-clusters.md:51-52 "The following admitted fields are preserved in effective state without changing an agent-install plan". The table that follows lists `release.channel`, endpoint `dnsName`, `port`, `scheme`, `prefixLength`, `interfaceNetworks`, and node `labels` and `taints`. At :58 `port` is "Inert; the installer uses its native endpoint ports." Node labels and taints are "inert" (:46-49) pending B716. specs/api.md:28-29: "Accepting a declaration claims no executable support for its effectful consumer."

**Consequence.** An operator writes `endpoints.api.port: 8443`, `scheme: http`, or `taints: [{key: infra, effect: NoSchedule}]`. Validate and plan succeed and apply completes, but the cluster has port 6443, https and no taint. Nothing reports the gap except a spec table. This is the silent-drop behaviour that storage-native-api.md:889-890 forbids for storage ("it never silently drops or overrides authored data").

**Recommendation.** On the agent path, refuse at plan any authored inert field whose value differs from what the installer will do. Add a refusal-table row per field (for example "this executable does not install endpoint port"), keeping the fields in the schema for later consumers. Alternatively, remove `port`, `scheme`, `prefixLength` and `interfaceNetworks` from the endpoint shape until a consumer exists. Keep `channel` as informational only if `render effective` labels it so.

Verifier notes:

- [refuter] Scope the refusal to authored non-default endpoint port, scheme, prefixLength and interfaceNetworks, or remove those fields until a consumer exists. Leave labels and taints to B716/Q11, and at most note that Q11 should decide whether they refuse in the meantime. Keep channel informational.
- [calibrator] Smallest change: emit the existing api.deferred warning (cli/output.md:804) at each authored inert field path, so validate, plan and apply report it. Escalate to a plan refusal only where the authored value contradicts the installer (port other than the native port, scheme other than https). Keep labels/taints as warnings pending Q11/B716 rather than refusing them. Removing fields from the schema is opt…

**Acceptance.** Every field in the container-clusters.md:51-63 table either has a refusal row or is removed from the schema. An authored, non-default inert value makes `plan` fail with a diagnostic at that field, and a test covers it.

**Decision check.** Checked the api.md compiler boundary (it limits what validate claims, but does not say plan may ignore effectful fields), B716 (the open node-label and taint decision; refusing until it is decided pre-empts nothing) and D153 (predictable and safe apply). The storage pages' "no silent drop" principle supports the change, and nothing recorded conflicts with it.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F05-07 · Medium · Several storage references accept only the value Bootwright could derive

Kind: duplication; Area: Desired-state API: Storage references; Status: confirmed.

Files: `specs/api/storage.md:450-453`; `specs/storage-resources.md:838-862`; `specs/storage-api-review.md:319-327`; `examples/storage-clusters/libvirt-6-nodes-ceph-services/storage/smb-archive-share.yaml:6-21`; `examples/storage-clusters/libvirt-6-nodes-ceph-services/storage/smb-cluster.yaml:9-20`

**Evidence.** specs/api/storage.md:452-453 refuses "`mdsServiceRef` other than `mds.<filesystem>`", so the only legal value is derived from the filesystem's name. specs/storage-resources.md:852 makes the share's native `cluster_id` required ("Required `cluster_id` and `share_id`") although `smbClusterRef` already names the cluster. The examples author `smbClusterRef: files-smb` beside `cluster_id: files`, and `resourceRefs: [files-users]` beside `user_group_settings: [{ref: files}]`. specs/storage-api-review.md:321-322: "no repeated value is required just to satisfy the wrapper."

**Consequence.** Every CephFS filesystem and SMB share makes the operator write the same fact twice in two spellings: a Bootwright object name and a native ID. Each duplicate is another admission refusal to hit, and a rename of one object needs coordinated edits in several files. This contradicts the review rule that the resource contract claims to follow.

**Recommendation.** Make `mdsServiceRef` optional and derived as `mds.<metadata.name>`, refusing only an authored disagreement, or drop it. Make the share's `cluster_id` optional and derived from `smbClusterRef`. Derive each `user_group_settings[].ref` and `join_sources[].ref` from `resourceRefs`, or the reverse, so each relationship has one authored home. Apply the same rule as the NFS `fs_name`: when the operator authors the native field, it must agree.

Verifier notes:

- [refuter] Cite the in-family precedents (NFS cluster_id refused at storage-resources.md:771; share cephfs leaves supplied by binding) as the model. For resourceRefs vs native ref, note they are different identities (object name vs native id); pick one authored home and derive the other. Consider Low-to-Medium: agreement checks prevent divergence.
- [calibrator] Make mdsServiceRef optional instead of dropping it, to stay symmetric with StorageObjectGateway.serviceRef, whose value is not fully derivable. Update storage-resources.md:108-110 and the M2 admission text. For the SMB auth links, keep the native refs (D133 native format) and make resourceRefs optional, derived by native ID, since IDs are unique per StorageCluster (916-919). Acceptance should also cover…

**Acceptance.** The 6-node services example validates with `mdsServiceRef`, the share `cluster_id` and either `resourceRefs` or the native `ref`s removed. A disagreeing authored value still refuses at its field.

**Decision check.** Checked D133 (NFS exports and SMB resources keep their native formats). Deriving an optional native leaf keeps the native format, and the review text attached to D133 explicitly endorses it. No recorded decision requires authoring these leaves.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F18-02 · Medium · ContainerCluster is OpenShift-shaped under a neutral name, with no distribution union

Kind: design; Area: Desired-state API: ContainerCluster; bounded contexts; Status: confirmed-by-arbitration; Proposed severity: High; Also raised as: F05-01, F02-10.

Files: `specs/architecture.md:54`; `specs/api/container-clusters.md:3-4, 57, 76, 85, 341, 389, 422, 436`; `specs/api.md:217-221`; `internal/addons/packages.go:55-67`; `specs/api/container-clusters.md:3-8, 55-59, 74-89, 185-201, 334-342, 387-392, 418-436`; `specs/api/storage.md:258-260`; `specs/project.md:17-30`; `specs/api/container-clusters.md:3-8`; `specs/api/container-clusters.md:193-203`; `specs/api/container-clusters.md:227-236`; `specs/api/container-clusters.md:418-425`

**Evidence.** specs/architecture.md:54 Container cluster owns "OpenShift and OKD release, topology, installer intent, node binding, cluster access, and installation evidence". api/container-clusters.md:3 "ContainerCluster owns OpenShift/OKD install intent". :57 distribution.type "openshift or okd". :76 "agent is the only accepted method". :436 "Every machineRef requires capability openshift-node and os.provided: false". :341 ovnKubernetesConfig sits at the top level. :389 FIPS "true is OpenShift-only". api.md:219-221 keeps type unions for container installation platforms, not distributions. addons/packages.go:63 ClusterFacts carries an OVNGateway. Also reported as F05-01: specs/api/container-clusters.md:3 "`ContainerCluster` owns OpenShift/OKD install intent". The rest of that page uses OpenShift vocabulary throughout: :422 role "`master`, `worker`, or `infra`"; :199 the endpoint source defaults to "`openshift`"; :85 `pullSecretRef` is "OpenShift"; :341 `ovnKubernetesConfig`; :389 `fips` "`true` is OpenShift-only"; :436 "Every `machineRef` requires capability `openshift-node`". The neutral parts (nodes, endpoints, cluster and service networks, proxy, NTP, registries) and the OpenShift parts sit side by side at the same level. Compare specs/api/storage.md:260: "`spec.type` is exactly `ceph`... `spec.ceph` is required for `managed`". That is a discriminated arm that ContainerCluster lacks. Also reported as F02-10: container-clusters.md:3 "`ContainerCluster` owns OpenShift/OKD install intent". container-clusters.md:199 "`source.type` ... `openshift`, `external`, `loadBalancer`, or `node`". container-clusters.md:227-229 "the installer owns all three endpoint slots as virtual IPs ... Each takes the `openshift` source". container-clusters.md:422 "`spec.nodes[].role` ... `master`, `worker`, or `infra`".

**Consequence.** A kubeadm, RKE2, EKS or AKS distribution cannot be added as an arm. Many top-level fields are meaningless or wrong for it: pullSecretRef, api-int and VIP endpoint semantics, roles mapped to machine-config pools, the openshift-node capability, and the installer-provisioned OS invariant, where kubeadm needs OS-ready nodes. The choice is a new kind, which touches every general-flow kind list (F18-04), or a breaking reshape that grows costlier with each example, golden and M3/M4 slice built on the flat shape.

**Recommendation.** Ask the owner to decide now, while the change costs least: make ContainerCluster a distribution-neutral envelope (nodes with neutral roles control-plane/worker, endpoints api and ingress, cluster and service networks, proxy/NTP/registry selections, trust) plus a presence union of distribution arms, starting with spec.openshift. That arm would hold release, install.agent, platform, pullSecretRef, nodeSSH, servingCertificates, FIPS, diskEncryption, OVN settings, api-int and the infra role. Alternatively, record that each distribution family is its own kind and list the generic touch points that implies. Rename the bounded context row to 'container cluster installation' with distribution adapters.

Verifier notes:

- [refuter] Downgrade or drop. If kept, reframe it as: project.md's product scope omits the owner's intended cluster families. Ask the owner to either add them, which would justify a distribution-neutral envelope with arms, or confirm OpenShift/OKD only. Do not prescribe the reshape before that scope decision.
- [calibrator] Lower the severity to Medium. Cite project.md:24 and :88 and B748/D172. Make the primary recommendation a recorded owner decision on how a non-OpenShift distribution enters: an envelope with distribution arms, or one kind per family with a list of the generic touch points each kind implies. The schema reshape should follow that decision and is not the immediate fix. Drop the bounded-context rename unles…

**Acceptance.** api/container-clusters.md shows an envelope plus an spec.openshift arm, or a recorded decision that distributions are separate kinds. No top-level field carries an 'OpenShift-only' rule. Node binding reads a distribution-declared node capability. architecture.md:54 names distributions as adapters. Arbiter acceptance: One decision entry in specs/milestones/backlog.md, linked from api/container-clusters.md, naming the extension path, how node binding gets a distribution-declared node capability, and B748's alignment.

**Decision check.** Checked api.md Version authority (31-37: schema changes stay in v1alpha1 with no conversion, so a reshape is permitted once authorized and costs no migration code), D162 clause (c) (topology rules are general plus specializations: they move into the openshift arm unchanged), D163 (no version rules: unaffected) and D172/D173 (M3/M4 scope builds on the flat shape: that raises the cost of waiting). No decision fixes the flat shape. This needs an owner decision because it is a public schema move.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### R2-05-01 · Medium · No single rule decides whether an unrealized declaration refuses at admission or later

Kind: gap; Area: Desired-state API: truthfulness (Environment, Entitlement, examples); Status: confirmed-by-arbitration; Proposed severity: High.

Files: `specs/api/environment.md:453-455`; `specs/api/environment.md:489-496`; `specs/api/environment.md:40-42`; `specs/api/secrets.md:188-193`; `specs/api.md:21-29`; `examples/container-clusters/lab-kubevirt/README.md:9-12`; `cmd/bootwright/admission_acceptance_test.go:108-112`

**Evidence.** environment.md:453-455 "Admission never refuses a shape only this executable cannot realize: those are the controller's, and refuse before registration". environment.md:491-494 "No rescue journey exists yet, so admission refuses a present rescue with api.invariant". admission_acceptance_test.go:108-112 refusedKinds "admission refuses until their later slices (storage family refusals)". secrets.md:192-193 "satellite and connectToInsights: true refuse at installation". lab-kubevirt/README.md:9-11 "The API validates this profile ... Runtime planning refuses that unqualified provider". environment.md:42 libvirt "the grammar admits a distribution package version ... which the controller stage refuses".

**Consequence.** An operator copies lab-kubevirt or declares a Satellite Entitlement, gets a clean `validate`, runs `context init`, binds Secrets and runs setup, and only `plan` or installation refuses; the same operator declaring `lifecycle.rescue` is stopped at `validate`. Each new future arm is placed by precedent, so implementers diverge on where to refuse and `validate` cannot be trusted as a support check.

**Recommendation.** In api.md#compiler-boundary state one classification: (a) a declaration no build realizes yet (reserved schema: rescue, StorageExport families, Satellite, Insights, NEVRA libvirt pins, the KubeVirt provider until B87) and (b) a declaration this build realizes but not on this host, target or state. Keep a single 'Admitted, not realized' table (field, refusal point, code, tracking item) owned by api.md, and have `validate` emit a deterministic warning for every class-(a) declaration it admits, so goldens stay green while operators learn before context import. Replace environment.md:453-455 with a pointer to that rule.

Verifier notes:

- [refuter] Drop the contradiction framing. At most, file a Low or Medium design item: name in api.md#compiler-boundary the admission-time exceptions (rescue per D29, StorageExport) as departures from the rule in state-reconciliation.md:909-919. A validate warning for admitted-but-unrealized shapes is optional, and the owner should decide whether to add one.
- [calibrator] Downgrade to Medium, kind gap. Smallest fix: in api.md#compiler-boundary link to state-reconciliation's plan-time lifecycle.unsupported rule as the default and state the one criterion for an admission-time exception (no build has any consumer, e.g. D29 rescue, StorageExport), with each exception named by its owning spec; make secrets.md:192-193 name the refusal point (plan before registration via lifecy…

**Acceptance.** api.md holds the classification and one table listing every admitted-but-unrealized field with its refusal point; environment.md, secrets.md and the lab-kubevirt README cite it instead of restating policy; `validate` of a lab-kubevirt copy prints a warning naming B87; a test holds the table to the set of lifecycle.unsupported refusals. Arbiter acceptance: api.md states the default and the exception criterion with links; secrets.md names the concrete refusal rows and code; each admission-time refusal of an unrealized field cites the criterion.

**Decision check.** D29 (rescue refused at admission) is preserved as class (a) at admission. D77 (controller egress: pure grammar at admission, executable limits at selection) is exactly class (b) and is preserved. B676 already tracks the libvirt pin field; the recommendation does not duplicate it, only lists it. The warning approach keeps the golden contract (every example compiles) intact. No decision fixes Satellite or KubeVirt refusal points. Pre-1.x no-compat principle permits moving refusals. Arbiter decision check: D29 preserved as a named exception; D77 is the plan-time default. No decision fixes a refusal point for Satellite, Insights or KubeVirt, so the clarification is unconstrained.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### R2-05-04 · Medium · Tracked lab-kubevirt is the profile coverage calls lab-kubevirt-external

Kind: contradiction; Area: Examples vs container-cluster acceptance profiles; Status: confirmed.

Files: `specs/container-cluster-coverage.md:164`; `specs/milestones/m3.md:170`; `examples/container-clusters/lab-kubevirt/clusters/kubevirt/cluster.yaml:17-56`; `examples/container-clusters/lab-kubevirt/README.md:3-12`; `examples/README.md:34`

**Evidence.** container-cluster-coverage.md:164 lab-kubevirt: "3 masters and 2 workers, each 8 vCPU ... authored platform.type: baremetal with installer VIPs"; "Variant lab-kubevirt-external: derived platform.type: none, external endpoints". cluster.yaml declares three masters, no workers and `source: type: external` endpoints. examples/README.md:34 says only "runtime qualification remains open".

**Consequence.** The B731 run labelled lab-kubevirt would rehearse the variant Q19's recommendation ranks optional, and the slice that creates the first-ranked profile collides with an existing directory of the same name. The index row hides that planning refuses the provider outright, which the scenario README states.

**Recommendation.** Rename the scenario to lab-kubevirt-external (and its golden), or change the coverage row to match it; add 'planning refuses the KubeVirt provider until B87' to the index row.

Verifier notes:

- [refuter] Prefer renaming the example to lab-kubevirt-external (matching its shape) over editing the coverage row, since Q19 (a) ranks the baremetal-VIP profile first under the name lab-kubevirt; also fix m3.md B731's in-tree preparation note to name the variant. Adding the planning refusal to the index row is a Low add-on.
- [calibrator] Prefer rename to lab-kubevirt-external (matches Q19 (a) and keeps 'lab-kubevirt' free for the first-ranked baremetal-VIP profile); also correct the B731 in-tree-preparation note (m3.md:1700-1704) to say the prepared example is the external variant, and reconcile the 6 vs 8 vCPU sizing in the README or the coverage row. Acceptance: directory name, README title/sizing, coverage row and B731 note agree; do…

**Acceptance.** The example directory name, its README title and the coverage-matrix row describe the same node shape and platform; the index row names the planning refusal; the golden test passes under the new name.

**Decision check.** Q19 is open with recommendation (a) (baremetal with VIPs first, derived none in goldens only); renaming aligns the example with that recommendation and pre-empts no answer.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

#### S6: Architecture and specialization

##### F17-01 · High · Storage coverage and M2 gate on vendor support that D157, D159, D163 forbid

Kind: contradiction; Area: Coverage contracts / version policy; Status: confirmed.

Files: `specs/storage-coverage.md:5-7, 150-158, 258-262, 419-422`; `specs/storage-resources.md:87-88`; `specs/milestones/m2.md:6-14, 124, 1277-1290`; `specs/storage-api-review.md:358`; `specs/milestones/backlog.md:3473-3478, 3612-3622`

**Evidence.** storage-coverage.md:5-7 "Each row below is required when supported by the frozen IBM release; an unqualified feature is refused before effects". storage-coverage.md:150-151 "the release record names ... support status (supported, preview, deprecated or unsupported)". storage-coverage.md:420-422 "The release-specific flash, failure-domain, network/latency and capacity prerequisites are admission and live-preflight gates." storage-coverage.md:261-262 "An AMD CPU with AVX-512 is not accepted until B480 records IBM's position." storage-resources.md:87 "Release support, numeric/native limits and graph capacity are checked before effects." m2.md:10-11 "M2 supports all IBM-supported cluster sizes for the qualified release". storage-api-review.md:358 "iSCSI remains conditional on IBM support." Against: backlog.md:3474 (D159 clause 15) "claims or cites a vendor support status or a release"; backlog.md:3612-3616 (D163) "registers and enforces no component version rule"; storage-coverage.md:186 "Bootwright records no vendor support status for it".

**Consequence.** An agent implementing B472, B484 or a new family reads the owning coverage page and builds a support-status record, release-conditional admission gates or a CPU-vendor refusal, re-introducing the catalog the owner removed; B484's exit ("no unrun supported row") cannot be decided because Bootwright may not keep the vendor table that defines "supported". Adding Red Hat or open-source Ceph inherits IBM-conditional wording.

**Recommendation.** Redefine a coverage row as "a capability Bootwright realizes" and replace every "IBM-supported", "provider-supported", "release-qualified" and "support status" clause in storage-coverage.md, storage-resources.md, storage-native-api.md, storage.md and m2.md with that meaning; delete the support-status record clause (150-158) and the AMD gate; restate stretch prerequisites as native-tool judgement reported through nativeError; drop "Release support" from storage-resources.md:87; fix the iSCSI line per D176. Add a docs-check lint that refuses those phrases in specs outside backlog.md and delivered.md.

Verifier notes:

- [refuter] Keep the recommendation; note B472 (m2.md:752-756) already states the intent, so the fix is aligning storage-coverage.md/m2.md/storage-resources.md/storage-api-review.md:38,358 to it. Distinguish native-tool-reported limits (keep, reported via native error) from vendor-table gates (remove). The lint is optional; the AMD clause needs an owner call only if a non-vendor-table basis exists.
- [calibrator] Acknowledge D129/D130 wording ('IBM supports') and state that D157/D163 supersede it, or route that reconciliation to the owner. Keep the rewrite but make the acceptance grep allow negative statements ('records no vendor support status') and decision-history rows (m2.md OD table), e.g. grep for affirmative gate phrases only. Make the docs-check lint optional follow-up (record as a backlog item) rather t…

**Acceptance.** grep -rniE 'IBM-supported|provider-supported|release-qualified|support status|Release support' specs --include=*.md, excluding milestones/backlog.md and milestones/delivered.md, returns nothing; B484's exit names a finite list of Bootwright-realized rows; make docs-check fails on a reintroduced phrase.

**Decision check.** Checked D157 (no release catalog or approval), D159 clause 15 (no vendor support status; vendor-table-only restrictions removed), D162 (specializations never approve), D163 (no pairing or compatibility rule), D164 OD29 (tested services are evidence, never an allowlist) and D176 (iSCSI closes with no support-status claim). The recommendation implements them; no decision keeps a vendor-support gate. U03's IBM colocation rule is a separate owner call because D159 clause 15 removes only restrictions whose sole basis is a vendor support table.

Verification: refuter real, high confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### F06-01 · Medium · Implementation selection is per-kind self-claim, not a per-object registry

Kind: gap; Area: Architecture: generic flow and specialization; Status: confirmed; Proposed severity: High; Also raised as: F18-01, F07-05.

Files: `specs/architecture.md:336-342, 539-544, 940-950`; `specs/state-reconciliation.md:1014-1027`; `specs/substrates.md:16-17`; `internal/reconciliation/lifecycle/planning.go:75-100`; `cmd/bootwright/wiring_reconciliation.go:96-122`; `specs/architecture.md:539-548, 942-950`; `internal/reconciliation/lifecycle/planning.go:75-98`; `internal/reconciliation/lifecycle/support.go:72-91`; `internal/storage/ceph/capability.go:247-258`; `internal/infrastructureservices/managedservice/selection.go:129-138`; `cmd/bootwright/wiring_reconciliation.go:232-234`; `specs/architecture.md:940-951`; `internal/reconciliation/lifecycle/support.go:72-97`; `internal/reconciliation/lifecycle/planning.go:381-397`; `internal/substrate/target.go:22-28`; `internal/substrate/libvirt/selection.go:21-25`; `cmd/bootwright/wiring_reconciliation.go:94-96`

**Evidence.** specs/architecture.md:942-945: 'An allowlisted registry resolves from explicit validated state: resolution is deterministic and produces exactly one implementation; no match or more than one match fails'. specs/state-reconciliation.md:1015-1016: 'Go deterministically resolves exactly one matching implementation and freezes its identity'. Neither page states the selection key. specs/architecture.md:540-541 keys the resolver 'by the API kind and the resolved implementation identity', i.e. after selection. internal/reconciliation/lifecycle/planning.go:91-95 iterates every binding and calls 'capability.Plan(ctx, input)' on the whole state, so each capability decides which objects it claims in its own selection.go. specs/substrates.md:16-17: 'one implementation per substrate arm, so a block resolves by kind and implementation together' is the only per-domain statement of a key. Also reported as F18-01: specs/architecture.md:944-945 "resolution is deterministic and produces exactly one implementation; no match or more than one match fails before output publication or effects". internal/reconciliation/lifecycle/planning.go:75 "planFrom asks every resolvable capability for its blocks." support.go:78 skips a kind once any binding claims it: "if slices.Contains(claimed, string(kind)) { continue }". internal/storage/ceph/capability.go:252-258 iterates every StorageCluster and reads cluster.Spec().Get("ceph") without checking spec.type. managedservice/selection.go:133 filters on management only, ignoring the authored spec.implementation. wiring_reconciliation.go:232-234 binds every storage kind to ceph.Implementation. Also reported as F07-05: specs/architecture.md:944-945: "resolution is deterministic and produces exactly one implementation; no match or more than one match fails before output publication or effects". lifecycle/support.go:77-78 skips any kind that has a binding: "for _, kind := range effectKinds() { if slices.Contains(claimed, string(kind)) { continue". target.go:28: "func Realized() []string { return []string{ArmBaremetal, ArmLibvirt} }". libvirt/selection.go:23: "for _, unrealized := range substrate.Unrealizable(catalog)". wiring_reconciliation.go:95: "There is no registry, discovery or runtime plugin path."

**Consequence.** Determinism and 'exactly one' rest on private selection code in each capability. A second implementation of an existing kind (vSphere machine beside libvirt; a second cluster distribution beside agentinstall) can silently claim the same object, or leave a shape claimed by none that only the kind-level unclaimed refusal catches if no implementation of the kind exists. Overlap is detected, if at all, only by an incidental block-identity collision, which is not a specified rule. Agents adding an implementation have no declared table to extend.

**Recommendation.** Specify selection as a pure, declared function per domain: each capability exposes a 'Claims(state) -> [(object, block family)]' over a documented selector (for example InfraProvider arm, ContainerCluster distribution.type plus install.method, StorageCluster type plus management, service kind plus management), and the engine refuses before registration any (object, block family) claimed by two bindings or by none among an effect-bearing object's required families. Add a 'Selection' row table to each domain spec naming selector values to implementation identities.

Verifier notes:

- [refuter] Keep the declared selector + overlap/gap refusal in architecture.md; per-domain tables can extend existing 'Selection and refusal' sections rather than new tables; move 'every other declaration refuses' into the shared selector so unsupported-shape refusal is computed once across implementations of a kind.
- [calibrator] Keep it at the spec level, not a new Claims() Go API: architecture.md names that selection keys come from each domain's supported-shape section, and adds one rule: every selected object of a block family is claimed by exactly one bound implementation or refused by that domain's refusal table; a double claim refuses before registration naming both implementations. Do not add a separate engine 'claimed by…

**Acceptance.** architecture.md names the selection key and the overlap/gap refusal; each domain spec has a selector-to-implementation table; a test drives every admitted example and a synthetic overlapping double binding and sees the overlap refused with a lifecycle diagnostic naming both implementations.

**Decision check.** Checked D1 (substrate owns machine port), D153 clause (3) (seams only with a present consumer: the claim function has a present consumer, the engine) and D162/D163 (unaffected: selection of implementation, not of versions). No decision conflicts; architecture.md:942-950 already demands the property.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F06-03 · Medium · 'Policy never branches on implementation' is untestable and unenforced

Kind: gap; Area: Architecture verification; Status: confirmed-by-arbitration; Proposed severity: High; Also raised as: F07-06.

Files: `specs/architecture.md:339-342, 543-544, 949-950, 989-1009`; `test/architecture/version_specializations_test.go:12-18`; `internal/containercluster/agentinstall/pushed.go:15`; `specs/architecture.md:993-996,949-951`; `test/architecture/boundaries_test.go:19-28,112-185`; `test/architecture/composition_rules_test.go:47-117`; `internal/cli/lifecycle_actions.go:17-56`

**Evidence.** specs/architecture.md:949-950: 'application and domain policy never branch on concrete implementation identity to reproduce adapter mechanics.' The qualifier 'to reproduce adapter mechanics' needs intent judgment. specs/architecture.md:543-544: 'application policy never switches on a concrete implementation' (no domain clause). The verification list (architecture.md:994-1009) checks imports, concrete types and registry content, but names no check on identity or arm literals. internal/containercluster/agentinstall/pushed.go:15 compares node arms with 'substrate.ArmKubeVirt' inside an application package. Also reported as F07-06: specs/architecture.md:995: "no direct adapter-to-adapter calls or domain vendor/I/O leaks." :949-950: "application and domain policy never branch on concrete implementation identity". boundaries_test.go checks package roles, imports and effect grants. composition_rules_test.go explicitly allows "pure function injection" and "value constructor", so `libvirt.MachineImplementation` and `baremetal.PinnedIdentity` pass. internal/cli/lifecycle_actions.go:35: "case \"machine-libvirt-v1\":" is an unchecked literal.

**Consequence.** The core guarantee the owner asks for (general code never knows which implementation runs) is review-only. Import-direction tests pass while application and domain packages compare implementation or arm identities, so leakage accumulates silently with each new arm or distribution.

**Recommendation.** Restate as a testable rule: outside its owning implementation package and cmd/bootwright, no production package references an implementation identity constant, a substrate arm constant, or their string literals, except through a declared, shrinking allowlist naming why (for example admission of an authored union arm). Add the matching fitness test to the verification list, plus a 'second synthetic implementation' contract test per family port (as secrets.md:48 does).

Verifier notes:

- [refuter] Drop or reframe: 'containercluster admission and media selection key on substrate arm literals (pushed.go:15, pushed_admission.go:14,35, selection.go:116) rather than a substrate-declared delivery capability; a new push-only arm must edit containercluster.' Medium design finding; the fix is a substrate capability query, not a ban on arm constants.
- [calibrator] Downgrade to Medium. Restate the rule as: outside the owning implementation or substrate package and composition, production code reaches implementation or arm specifics only through a capability the owner publishes (for example, the substrate declares a node's media delivery mode); the owning domain's support-matrix admission refusals are the only named exception. Add one fitness test that rejects Arm*…

**Acceptance.** architecture.md carries the unqualified rule and names the test; the test fails on a fixture comparing an implementation identity in an application package; the allowlist exists, is exact and only shrinks. Arbiter acceptance: architecture.md names the test; the test fails on a fixture comparing substrate.ArmKubeVirt in a non-allowlisted containercluster file; the allowlist is exact and only shrinks.

**Decision check.** Consistent with architecture.md:1042-1045 (exact allowlists that only shrink) and secrets.md:48-50 precedent. No D-decision exempts domains from this rule. D153 (3) satisfied: the test has a present consumer.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### F06-04 · Medium · No extension procedure for a new kind, arm or second implementation

Kind: gap; Area: Architecture: extension procedure; Status: confirmed; Proposed severity: High; Also raised as: F18-08.

Files: `specs/architecture.md:395-425, 940-950`; `specs/project.md:57-62`; `specs/architecture.md:380-425`; `specs/api.md:260-275`

**Evidence.** specs/architecture.md:404-422 'Adding a lifecycle capability' lists: create the package, packageRoles, applicationValues, 'add its boundCapability to buildCapabilities', playbooks, optional Projector. Nothing addresses a new arm, distribution or provider of an existing kind: refusal tables naming unsupported arms (substrates.md:89-90), consumer dispatch, selection rows, observable-field rows, examples. A repository-wide search finds no 'adding an implementation' procedure; the only hint is substrates.md:37-38 'a substrate added later supplies those entry points to every consumer'. Also reported as F18-08: specs/architecture.md:394-425 defines only "Adding a command" and "Adding a lifecycle capability". api.md:262 "The v1alpha1 catalog contains exactly 33 authored kinds". A search of specs/ and .agents/skills for 'new kind', 'adding an arm' or 'added later' finds only substrates.md:37.

**Consequence.** Adding vSphere, vanilla Kubernetes, NetApp or a second NTP implementation has no defined touch set, no budget and no test that the touch set is minimal; agents will discover the scattered edits by trial, and reviewers cannot tell an over-reaching change from a necessary one.

**Recommendation.** Add 'Adding an implementation of an existing capability' to architecture.md with the minimum touch set: the implementation package, its selection row, its composition binding and playbook binding, its role, its row in the domain's support statement, and its contract-suite run; state that a change touching any other domain's package is a design defect unless the seam is missing, in which case the seam is added first in its own change.

Verifier notes:

- [refuter] Keep the recommendation but anchor it in the existing 'Implementations and version variation' section next to the registry rules and reference the existing shared port suites (architecture.md:1005-1008) as the contract-suite step; avoid prescribing selector names for domains that have no second implementation yet (project.md 'abstractions only for real consumers').
- [calibrator] Lower to Medium. Replace 'the seam lands first in its own change' with 'the seam lands in the same slice as its first consumer, as a separate reviewed step' to honor project.md:87-88. Keep acceptance to: section exists beside 'Adding a lifecycle capability', lists the touch set with the guarding tests per step; drop the requirement that every family domain names its selector and contract suite (broader…

**Acceptance.** The section exists with a touch list; each family domain (substrate, container cluster, storage, infrastructure service) names its selector and contract suite; a review can check a new-implementation diff against the list.

**Decision check.** D153 (3) ('builds only seams with a present consumer') and project.md:87 are compatible if the seam is added with its first consumer (see F06-06). D162/D163 do not apply. No conflicting decision found.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F06-07 · Medium · Composition injects Ceph policy and bare-metal identity into Managed OS

Kind: contradiction; Area: Architecture: cross-context communication; Status: confirmed.

Files: `specs/architecture.md:126-162, 553-557`; `cmd/bootwright/wiring_reconciliation.go:214-222, 238-239`; `specs/storage.md:726-733`

**Evidence.** specs/architecture.md:556-557: 'Reconciliation resolves the former into block dependencies ... so no capability learns another's block identities.' specs/architecture.md:128: 'Each is deliberate and bounded; another like it is a defect.' cmd/bootwright/wiring_reconciliation.go:218 binds 'installation.NewErase(runner).WithClaims(baremetal.BlockID)...WithRetirementPolicies(ceph.PhysicalRetirementPolicies)'; line 221 binds the same Ceph policy into installation.New. Neither binding is a named exception, and no spec names the port Managed OS uses to learn Storage's retirement policy.

**Consequence.** Managed OS installation depends at composition on one storage implementation's function and on another capability's block-identity grammar. A second host-based storage implementation, or a second physical substrate, requires editing these bindings and the single-function port, and the spec gives no aggregation rule.

**Recommendation.** Either declare these as named exceptions with their bound, or (preferred) specify a fan-in port: the consumer owns 'RetirementPolicies' as a list contributed by every bound storage implementation, aggregated in composition, and replace the block-ID reader with a requirement-based dependency the engine resolves as architecture.md:553-557 states.

Verifier notes:

- [refuter] Prefer the second option: express the claim and retirement dependencies as requirements (kind+object) resolved by Reconciliation per state-reconciliation.md:781-787, which removes both Managed OS bindings and Ceph's baremetal.BlockID binding; do not add a named exception that contradicts that clause. Acceptance should include the Ceph physical-retirement WithClaims binding.
- [calibrator] Drop the 'declare as named exceptions' option for the retirement policy: a consumer-owned function port is already sanctioned (architecture.md:100-101); instead specify the fan-in rule (Managed OS owns the retirement-policy port; composition aggregates every bound storage implementation's contribution). For block identities, choose one: route the claim through Reconciliation's API-object dependency reso…

**Acceptance.** architecture.md either lists both bindings under named exceptions or defines the fan-in port rule; storage.md and managed-os.md name the port; a composition test fails when a capability receives another package's BlockID function.

**Decision check.** D124 (keeper custody) and D1 checked: neither authorizes cross-capability block-identity knowledge. The recommendation preserves architecture.md:553-557 as written.

Verification: refuter real, high confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F06-09 · Medium · Topology registry can key per-minor records only on the forbidden release label

Kind: contradiction; Area: Version specialization; Status: confirmed.

Files: `specs/architecture.md:919-927`; `.agents/knowledge/version-specializations.md:85-93`; `internal/containercluster/topology.go:41-60`; `test/architecture/version_specializations_test.go:29-30`; `specs/milestones/backlog.md:3593-3598`

**Evidence.** specs/architecture.md:924-925: 'A release label or a tag never keys a specialization.' Facts come from 'the bytes of the operation's frozen, authenticated artifacts' (921-922). D162(c), backlog.md:3593-3596: 'per-minor specializations only where an installer differs'. internal/containercluster/topology.go:41 'func topologyFor(version string)' runs in admission on the authored version; the fitness test expects 'var topologyOverrides = map[string]releaseTopology{' keyed by string.

**Consequence.** The first per-minor installer difference will be encoded as a release-label key in admission, either violating the architecture rule or forcing an ad hoc exception; admission has no frozen artifact from which a compliant fact could be derived.

**Recommendation.** State in architecture.md that a Go-side specialization in admission is not allowed; a per-minor installer difference is either absorbed by the general rule (the installer judges) or detected at plan/apply from the frozen installer binary. Remove the label-keyed override hook from the test expectation.

Verifier notes:

- [refuter] Also amend specs/api/container-clusters.md:65-67 and 451-463 (per-minor specialization 'names its major and minor'), not just architecture.md and the test; state whether a topology difference is detected from the frozen installer at plan time or absorbed by the general rule.
- [calibrator] Add specs/api/container-clusters.md:65-67 and 459-463 as evidence and fix sites: replace 'names its major and minor' with the architecture fact rule. State that a per-minor topology difference is detected at plan from the frozen installer artifact (or left to the installer), and note this moves any such refusal from admission api.invariant to plan. Acceptance: grep finds no clause keying a specializatio…

**Acceptance.** architecture.md names where Go-side specializations may evaluate facts; the topology registry has no string-keyed override shape; the catalog row says how a per-minor difference would be detected.

**Decision check.** D162(c) requires per-minor specializations 'only where an installer differs' but D162(a) requires artifact-byte facts; the recommendation satisfies both. D163 (no version rules) is reinforced.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F06-10 · Medium · Refusal seams live only in knowledge while specs say specializations never refuse

Kind: gap; Area: Version specialization; Status: confirmed-by-arbitration.

Files: `.agents/knowledge/version-specializations.md:95-109, 56-65`; `specs/architecture.md:912-938`; `specs/milestones/backlog.md:3580-3583`

**Evidence.** .agents/knowledge/version-specializations.md:99-101: 'A refusal seam is one whose variants change which intents the adapter admits; a record may add a pre-effect refusal there'. Line 63: '`enabled` refuses `call-home-unqualified`'. D162(a), backlog.md:3581-3582: 'It never approves, screens, refuses, selects or substitutes a version or an artifact.' specs/architecture.md:912-938 never mentions refusal seams; it says only that a specialization 'never removes a trust refusal'.

**Consequence.** A behavioral rule (when a specialization may refuse operator intent) lives in a knowledge page that by AGENTS.md owns lessons, not behavior; agents may add refusals per build believing them allowed, or reject legitimate ones believing D162 forbids them.

**Recommendation.** Move the refusal-seam rule into architecture.md: a variant may refuse, before any effect, an authored intent whose native mechanism a positive fact proves absent from the selected artifacts, naming the fact and the intent; it never refuses a version as such. Leave the knowledge page as the catalog.

Verifier notes:

- [refuter] If kept, downgrade to Low kind gap: add one sentence to architecture.md 'Implementations and version variation' that a seam may be declared a refusal seam whose variant adds a pre-effect refusal of an authored intent a positive fact proves unrealizable, never removes a trust refusal and never refuses a version as such; knowledge page links to it.
- [calibrator] Reword to: a variant may add a named refusal of an authored intent that the selected build cannot realize as declared. The refusal comes before the block's own claim or effect. It never refuses a version or artifact as such, never removes a trust refusal, and an unknown fact selects the general variant. Do not say 'before any effect'. Do not require a mechanism proved absent. Change kind to gap and cite…

**Acceptance.** architecture.md defines refusal seams; the knowledge page links to it instead of stating the rule; the test still checks the seam's refusal flag. Arbiter acceptance: architecture.md states the refusal-seam permission and its limits; the knowledge page links to it; TestDocsVersionSpecializationCatalogMatchesRegistries still checks the seam's refusal flag.

**Decision check.** D162(b) explicitly contemplates refusing a Call Home intent per build, so the clarification reconciles D162(a) and (b). D163 unaffected (no version rule).

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### F07-03 · Medium · Storage and cluster contexts are single-implementation; general code names Ceph

Kind: design; Area: Architecture / bounded contexts and general packages; Status: confirmed; Proposed severity: High.

Files: `specs/architecture.md:54-55`; `internal/environment/inspection/contracts.go:16-49`; `internal/environment/selection.go:196-209`; `internal/environment/admission.go:118-126`; `internal/desiredstate/compilation/selection.go:190-210`; `cmd/bootwright/wiring_inspection.go:34-37,87-164`

**Evidence.** specs/architecture.md:55: "| Storage | Ceph cluster topology, placement, pools, filesystems, gateways, NFS services, exports"; :54: "| Container cluster | OpenShift and OKD release, topology". The general Environment context declares environment/inspection/contracts.go:45-46: "type CephEvidence interface { ManagedCeph(ctx context.Context, contextName string, catalog api.Catalog, cluster api.Object) (CephProof, error)". environment/selection.go:207: "spec.Get(\"ceph\", \"topology\", \"nodes\")"; compilation/selection.go:208 does the same. internal/storage holds 27 non-test files, and Ceph vocabulary dominates (topology.go alone has 90 matches).

**Consequence.** A second storage implementation (NetApp, Pure, open-source Ceph under a different shape) has no domain home. internal/storage would have to be split under load. Environment inspection, environment access, admission, graph selection and compilation would each need a new branch for the new node-membership path and evidence. That is the same pattern of general code knowing the implementation that the brief says must not happen.

**Recommendation.** Split now while there is one implementation. Keep internal/storage for product-neutral StorageCluster rules (membership, endpoints, completion evidence), and move Ceph policy to a Ceph domain package. Have each cluster-kind domain export a pure `MemberMachines(api.Object) []string` that environment selection, admission and compilation call instead of reading `spec.ceph.topology.nodes`. Rename the inspection ports to product-neutral `StorageEvidence`/`StorageProof`, with a Ceph adapter bound in composition. Restate the context table rows as product-neutral, with Ceph and OpenShift as the current implementations.

Verifier notes:

- [refuter] Drop the product-scope part (restating context rows) and the split of internal/storage; keep the smallest fix: a pure storage-owned member-machines function consumed by environment selection, admission and compilation, and a product-neutral inspection port name with the Ceph adapter bound in composition. Schedule as a milestone item.
- [calibrator] Smallest change: (1) add a per-kind pure member-machines accessor (registry keyed by StorageCluster spec.type, ContainerCluster nodes) used by environment selection, admission and compilation, and a Field path from the same accessor; (2) restate the Container cluster and Storage Owns cells product-neutral with current implementations named in a separate column or note; (3) put inspection evidence behind…

**Acceptance.** `grep -rn '"ceph"' internal/environment internal/desiredstate` returns nothing. The bounded-context table names no product in the Owns column. A test with a second fake StorageCluster arm passes environment selection and admission with no edit outside its own package.

**Decision check.** D162/D163 (one general flow; no version pairing rules): compatible. D56 (owner's bar: architecture following best practices): supports it. The owner's stated portfolio intent (brief) supports it. No recorded decision fixes Storage as Ceph-only. Pre-1.x no-compat principle allows the package move. The split should be scheduled as a milestone item rather than done ad hoc (AGENTS.md scope rule).

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F18-03 · Medium · A new substrate arm edits every Go and Ansible consumer that dispatches on it

Kind: contradiction; Area: Architecture: substrate machine port and its consumers; Status: confirmed-by-arbitration; Proposed severity: High; Also raised as: F06-02, F07-01, F07-02, F07-04.

Files: `specs/substrates.md:31-38`; `specs/milestones/m1.md:525-527`; `internal/substrate/target.go:114-115`; `ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/defaults/main.yml:261-299`; `ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/identity.yml:8-45`; `ansible/collections/ansible_collections/bootwright/core/roles/containercluster_install_agent/defaults/main.yml:113-129`; `internal/managedos/installation/install_interface.go:38, 173, 223-230`; `specs/substrates.md:30-38, 963-977`; `specs/architecture.md:739-748`; `specs/container-clusters.md:12-18`; `specs/managed-os.md:1427-1440`; `ansible/collections/ansible_collections/bootwright/core/roles/containercluster_install_agent/defaults/main.yml:113-125`; `internal/storage/devices.go:62-65`; `internal/containercluster/admission.go:645-660`; `specs/milestones/m1.md:465-468`; `internal/substrate/target.go:113-118`; `cmd/bootwright/wiring_machine.go:317-341`; `cmd/bootwright/wiring_reconciliation.go:214-221,329-354`; `specs/architecture.md:293-298`; `ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/identity.yml:1-35`; `ansible/collections/ansible_collections/bootwright/core/roles/containercluster_install_agent/defaults/main.yml:112-118`; `ansible/collections/ansible_collections/bootwright/core/roles/machine_power_redfish/defaults/main.yml:30-40`; `specs/architecture.md:115-121,733-740`; `specs/substrates.md:960-976`; `internal/machine/admission.go:490-496,870-873`; `internal/machine/placement.go:129-132`; `internal/controller/selection.go:145-190`; `internal/containercluster/endpoint_ownership.go:40-47`; `internal/containercluster/agentinstall/encryption.go:60-66`; `internal/machine/power/batch.go:79-83`; `specs/architecture.md:51,54`

**Evidence.** specs/milestones/m1.md:525-527 "An arm is a capability package plus one case of the target derivation, and no consumer of a realized Machine changes". target.go:114 "Consumers read this and name no substrate, so an arm added later reaches every one of them". In practice managedos_install_anaconda/defaults/main.yml:261-299 holds eight tables keyed libvirt/baremetal. identity.yml passes arm-specific variables such as substrate_libvirt_machine_identity_uri. install_interface.go:38 sets "Libvirt: derived.Substrate == substrate.ArmLibvirt" and :229 reads the provider's libvirt attachment. Also reported as F06-02: specs/substrates.md:973-975: 'A consumer selects among them by the frozen arm through one dispatch table per entry point ... and a substrate supplies them'. specs/substrates.md:37-38: 'a substrate added later supplies those entry points to every consumer that dispatches on it'. specs/architecture.md:744-746: 'A consumer of a realized Machine dispatches on the substrate arm or identity channel Go froze into its request'. The managed-OS role defaults carry per-entry-point maps 'libvirt: bootwright.core.substrate_libvirt_machine' / 'baremetal: ...' (main.yml:261-299); the cluster install role repeats them (main.yml:113-125). Go consumers switch on arm names: internal/storage/devices.go:65 'for _, variant := range []string{"libvirt", "vsphere", "kubevirt"}', containercluster/admission.go:645 'case "baremetal", "libvirt":'; 22 substrate.Arm* uses sit outside internal/substrate. Also reported as F07-01: specs/milestones/m1.md:466-468: "An arm is a capability package plus one case of the target derivation, and no consumer of a realized Machine changes." internal/substrate/target.go:114: "Consumers read this and name no substrate, so an arm added later reaches every one of them". The composition does otherwise. cmd/bootwright/wiring_machine.go:332-337: "realizationOf picks the libvirt machine block ... if block.Implementation == libvirt.MachineImplementation". cmd/bootwright/wiring_reconciliation.go:336: "return libvirt.ProvedDataDisk(block, evidence, path, serial, size, rotational)"; :352: "baremetal.ProvedPresence(block, evidence)"; :218: "installation.NewErase(runner).WithClaims(baremetal.BlockID)". The identities translators (wiring_machine.go:374-399) decode only baremetal.Kind. Also reported as F07-02: specs/architecture.md:118-119: "callers depend on the capability contract, not private tasks, variables, or implementation-specific result shapes." :737: "bound by the selected entrypoint, rather than discovered by a consumer role." But managedos_install_anaconda/defaults/main.yml:274-276: "managedos_install_anaconda_port_result: libvirt: substrate_libvirt_machine_port_result baremetal: substrate_baremetal_machine_port_result". identity.yml:10-14: "name: bootwright.core.substrate_libvirt_machine ... substrate_libvirt_machine_identity_uri:". specs/substrates.md:973: "A consumer selects among them by the frozen arm through one dispatch table per entry point". Also reported as F07-04: specs/architecture.md:51 says the Machine context does not own "Provider mechanics", and :54 says Container cluster does not own "Machine, substrate". Yet internal/machine/admission.go:492: "attachment.Get(\"libvirt\", \"management\").Text() != \"managed\""; containercluster/agentinstall/encryption.go:65: "provider.Spec().Get(\"libvirt\", \"machineProfiles\").Items()"; machine/power/batch.go:82: "arm = \"baremetal\"". A grep for arm reads outside internal/substrate lists 20 files across machine, controller, containercluster, managedos, storage and environment.

**Consequence.** Adding vSphere or KubeVirt edits about 12 dispatch tables in two consumer roles, adds an identity channel branch, and changes Go consumer rules in managed OS and agent install. Every future consumer, such as kubeadm or Ceph host preparation, multiplies this N consumers x M arms. The milestone exit criteria for B99/B87 promise something the design does not deliver, so estimates and reviews will be wrong.

**Recommendation.** Give Substrate one dispatcher role, for example substrate_machine_port, with entry points pre_boot, boot_media, boot_disk, identity_read, state_read, media_insert, media_eject and power. Its single argument spec takes the frozen target object and material paths and dispatches internally on the frozen arm to the arm roles. Consumers include only this role. On the Go side, expose capability facts on substrate.Target, such as qualified bond modes or whether a managed attachment gives the host an untagged address, in place of arm names. Correct substrates.md:31-38 and B99/B87 to state the real touch list until this lands.

Verifier notes:

- [refuter] If kept, reframe it as a Low/Medium design finding. Each consumer role repeats one row per arm per entry point (substrates.md:963-975), so a later Bootwright-installed arm (for example B88) edits every consumer table. A substrate-owned dispatcher role is optional. Optionally, have B87/B99 say 'no consumer changes for installer-provisioned Machines; a consumer that installs on the arm adds one row per entry…
- [calibrator] Split the fix. (1) Smallest correction: reword m1.md B87/B99 and the target.go comment to list the real touch set: one row per consumer dispatch table, an identity-channel branch, and the Go qualification rules keyed on the arm. Clarify substrates.md:36-38 rather than treating it as false. (2) As a separate design item: a substrate-owned dispatcher role and capability facts on substrate.Target (qualifie…

**Acceptance.** No role outside substrate_* names substrate_<arm>_machine. No package outside internal/substrate and admission references substrate.Arm* or reads a provider's arm fields (a fitness test). Adding a fake arm in a test touches only internal/substrate and the dispatcher's table. Arbiter acceptance: B87/B99 and target.go text match substrates.md:973-975; the dispatcher item is tracked with the fitness test as its acceptance.

**Decision check.** Checked D1 (the substrate owns the machine port as role entry points): a substrate-owned dispatcher keeps ownership with Substrate, so it is consistent. It is also consistent with architecture.md:735-738, since the dispatch stays an allowlisted binding Go freezes and authored data cannot select code. D143 is unaffected.

Verification: refuter not real, medium confidence, severity Low; calibrator real, high confidence, severity High; arbiter real, severity Medium.

##### F18-04 · Medium · General-flow code hard-codes kind lists, implementation ids and field paths

Kind: design; Area: Architecture: generic flow (desired state, environment, reconciliation, CLI); Status: confirmed; Also raised as: F07-09.

Files: `internal/desiredstate/compilation/phases.go:165-173`; `internal/desiredstate/compilation/selection.go:196-210`; `internal/environment/selection.go:73, 193-208`; `internal/environment/admission.go:117-125`; `internal/reconciliation/lifecycle/support.go:14-22, 82-87`; `internal/cli/lifecycle_actions.go:20-56`; `specs/architecture.md:949-950`; `internal/cli/lifecycle_actions.go:14-63`

**Evidence.** specs/architecture.md:949-950 "application and domain policy never branch on concrete implementation identity to reproduce adapter mechanics". cli/lifecycle_actions.go:35 switches on "case \"machine-libvirt-v1\":" and :50 on "storage-ceph-image-v1". environment/selection.go:201 "r.keep(api.Machine, spec.Get(\"libvirt\", \"machineRef\").Text())" and :207 reads ceph topology nodes. compilation/selection.go:208 returns Get("ceph", "topology", "nodes"). lifecycle/support.go:14-22 enumerates effectKinds and imports infrastructureservices.IsService. Also reported as F07-09: internal/cli/lifecycle_actions.go:35-39: "case \"machine-libvirt-v1\": apply, destroy = \"Create virtual machine and controller\" ... case \"machine-baremetal-v1\":". The values are copied literals, not the packages' constants. Storage core, export, host, SMB, replication, the libvirt host and add-on implementations have no row and fall back to the generic label.

**Consequence.** Every new kind (for example DHCPServer) edits at least six general-flow lists. Every new arm or storage type that binds Machines through a different path edits retention, node-uniqueness and controller-membership code. A forgotten list silently drops retention or refusals: an unlisted kind is neither normalized in dependency order nor refused as unclaimed. The brief's requirement that general code never knows which implementation runs fails at these points.

**Recommendation.** Derive kind facts from the api/v1alpha1 schema registry (effect-bearing, managed-service, normalization rank, reference fields). Have owning domains publish a pure MachineBindings(object) function that environment and compilation call through the composition root. Take CLI action labels from the capability's BlockDefinition description, which already exists. Add a fitness test that forbids implementation-identity literals and arm field paths in internal/reconciliation, internal/desiredstate, internal/environment and internal/cli.

Verifier notes:

- [refuter] Drop the claim that an unlisted kind is silently not normalized. Acceptance should not require a new spec clause (architecture.md:543-548 and 949-950 already state it); require the fitness test plus exhaustiveness tests for effectKinds and retention lists, and CLI labels sourced from the frozen plan or BlockDefinition per architecture.md:353.
- [calibrator] Drop the acceptance clause asking for a new architecture.md rule: architecture.md:543-548 and 949-950 already state it; acceptance should instead cite those clauses and require the fitness test to enforce them. Smallest fix order: (1) fitness test forbidding implementation-identity literals and arm field paths (ceph/libvirt) outside owning domains, (2) domain-published MachineBindings/cluster-node acces…

**Acceptance.** Adding a test-only kind with schema metadata needs no edit in those packages. The new fitness test fails on a literal such as "machine-libvirt-v1" or Get("ceph", ...) there. A spec clause in architecture.md states the rule.

**Decision check.** Checked architecture.md:543-548 and 949-950 (stated principles: the recommendation enforces them), D143 (no runtime discovery: schema metadata is compiled in) and D162 (one general flow per component: consistent). No decision accepts these lists.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-01-01 · Medium · Plan edges are not derived from references; capability edge lists disagree with code

Kind: gap; Area: Concept interaction: reference-to-dependency contract; Status: confirmed; Proposed severity: High.

Files: `specs/api/infrastructure-services.md:247-253`; `specs/state-reconciliation.md:781-787`; `specs/architecture.md:554-557`; `specs/container-clusters.md:484-488`; `internal/containercluster/agentinstall/selection.go:26-34`; `internal/containercluster/agentinstall/install.go:93-107`; `specs/storage.md:203-206`

**Evidence.** infrastructure-services.md:247 "Reference resolution is not a readiness edge." state-reconciliation.md:782-783 "a definition may carry *requirements*, each the kind and name of an API object whose realization must be done first". container-clusters.md:485-487 "requires every node `Machine` and the managed name and time services the cluster and its nodes select." selection.go:28-34 Requirements holds "ArtifactServers ... DNSServers ... Machines ... NTPServers ... Proxies"; install.go:104-106 emits a `Proxy` requirement. storage.md:203-204 "The graph includes the following dependencies where the desired graph needs them".

**Consequence.** No spec maps a reference field to an edge, so each capability's Go code is the only source of truth for the DAG. The one explicit cluster-install list omits the managed Proxy that the code waits for. An implementer who follows the spec, or adds a distribution or substrate, drops that edge. Under parallel admission (bound eight), the install then starts before the managed proxy listens, and nodes fail to pull the release payload. This is a race that ordinary fixture runs may not show. Reviewers also cannot check a plan's edges against the input, and every new reference field quietly decides whether it orders anything.

**Recommendation.** Add one normative reference-edge catalog to state-reconciliation.md#plan-and-execution and link to it from each domain page. It has one row per reference field class with these columns: target realized by a block (managed or external), requiring block, produced material carried, stage pair, and selection retention. Destroy inversion stays uniform. Default rule: a reference to an object that some block realizes is a requirement unless the row states why not. Add the managed Proxy to container-clusters.md:485-487. Add a fitness test that compares each capability's emitted requirements over the example corpus against the catalog.

Verifier notes:

- [refuter] Smallest fix: in container-clusters.md, name the managed Proxy requirement for both cluster-install (485-487) and cluster-media (333-337, replace 'nothing else'), and generalize api/infrastructure-services.md:247-249 to 'a managed service it selects'. A full reference-edge catalog with a fitness test across every Ref field is optional, larger-scope work. If kept, record it as a backlog item, not as the fix…
- [calibrator] Downgrade to Medium. Smallest fix: (1) add a default rule to state-reconciliation.md#plan-and-execution: a reference to an object some block of the plan realizes is a requirement unless the owning domain page states an exemption (an external service is the standing one); (2) add the managed Proxy to container-clusters.md:485-487 and to infrastructure-services.md:247-249. Make the catalog table and the c…

**Acceptance.** The catalog has a row for every Ref/Refs field in api/v1alpha1. container-clusters.md names the managed Proxy requirement. A test (for example TestEveryRequirementMatchesTheEdgeCatalog) fails when a capability emits a requirement that is not in the catalog or omits one that the catalog requires.

**Decision check.** D144 and D127 (parallel by default) make every unstated edge a race, which supports the change. architecture.md:554-557 (capabilities name dependencies as API objects, and the engine resolves them) is kept: the catalog documents and tests it and does not centralize planning. The external-service exemption (no block realizes it) stays as a catalog row. Grep of backlog decisions found no decision that rejects a reference-to-edge rule.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-01-02 · Medium · Nested KubeVirt host chain rests on objects named only by external strings

Kind: gap; Area: Concept interaction: one cluster hosts another cluster's Machines (KubeVirt on a hub); Status: confirmed; Proposed severity: High.

Files: `examples/full-platforms/multidc-platform/infra/providers/kubevirt-ocp-01.yaml:7-10,29-36`; `examples/full-platforms/multidc-platform/add-ons/virtual-cluster-network/add-on.yaml:14-19`; `examples/full-platforms/multidc-platform/clusters/ocp-01/add-on-binding.yaml:9-16`; `specs/milestones/m3.md:1743-1753`; `specs/deferred/machine-arms.md:47-50`; `specs/substrates.md:651-663`; `specs/state-reconciliation.md:820-823`

**Evidence.** kubevirt-ocp-01.yaml:8-10 `hostClusterRef: ocp-multidc-01` ... `storageClassRef: ocs-external-storagecluster-ceph-rbd`; :33-36 networkRef `ClusterUserDefinedNetwork` `multidc-virtual-cluster-machine`. virtual-cluster-network/add-on.yaml:14-18 manifestSet creating those CUDNs. machine-arms.md:50 "`spec.kubevirt.storageClassRef` ... External Kubernetes storage-class name." m3.md:1752-1753 "each KubeVirt Machine block requires the host's `cluster-install-<host>` block and the add-on instance providing `kubevirt`".

**Consequence.** On ocp-multidc-01, the `fdf` instance creates the storage class, and only after the Ceph export is realized. The authored network add-on creates the CUDNs. Neither the provider nor the Machines reference either one. With only B732's two edges, the hub VM blocks start once HyperConverged is Available. Their DataVolumes wait Pending on a class that does not exist yet, and the block fails at its budget. On destroy, the inverted graph lets the ocp-01 `fdf` retirement and the Ceph export removal (which revokes CephX principals) run before the hub VMs are deleted. PVC deletion then hangs, and the KubeVirt removal reads unknown. The `kubevirt` edge itself is a capability-token match that the provider never declares.

**Recommendation.** Answer B732 open question 1 with a declared rule: a KubeVirt provider that names hostClusterRef requires every block that reaches the host cluster's API in the add-ons stage. That is every add-on instance on the host plus its cluster network block, the same set the last-package rule uses. Removal inverts it. State the rule in substrates.md and as a catalog row (R2-01-01). Typing storageClassRef and networkRef as references to package instances is the larger alternative.

Verifier notes:

- [refuter] Keep the recommendation, framed as input to B732 Q1 with severity Medium. Also cover the B726 secondary-network path that B732 Q3 already names: the host's typed secondary-network block, not only an authored add-on, should be in the required set. Prefer the general rule (all host add-on and network blocks) over typing storageClassRef as a reference.
- [calibrator] Keep the host-wide add-on edge as the default. Add a planning rule: a cycle between it and a host add-on that depends on the child refuses with a named reason, or that add-on is excluded. Alternatively, prefer the narrower rule, which adds edges only to the add-on instances whose readiness creates the named storage class or networkRef objects. Acceptance should also include a plan test showing that no c…

**Acceptance.** B732 question 1 is resolved and names the full host set. The nested fixture plans requirements from each hub VM machine block to the fdf, nmstate and virtual-cluster-network instances on the host. The removal plan orders VM deletion before those instances' retirement and before the export's removal.

**Decision check.** Q21 (where hostClusterRef lives, owner answer (a)) is unaffected, because this only fixes the edge set. The v1 lesson in m3.md:1781-1784 (child waits for the parent install and the parent's kubevirt add-on) is narrower, and no decision forbids the wider edge. D144 parallelism makes the missing edge a race. F12-04 (last package) is a different defect: it concerns blocks that the handoff ignores, not hosting edges.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-01-04 · Medium · Selection closure omits four storage kinds and StorageReplication's peer

Kind: gap; Area: Environment selection closure: storage; Status: confirmed; Also raised as: R2-05-02.

Files: `specs/api/environment.md:204`; `internal/environment/selection.go:187-189,217-219,230-232`; `specs/storage-resources.md:247-258,269-272`; `specs/api/environment.md:198-218`; `specs/api/storage.md:256-273`; `specs/api.md:5-15`; `specs/api.md:262-275`; `specs/storage-resources.md:238-260`; `internal/environment/selection.go:187-189`

**Evidence.** environment.md:204 retains "every `StoragePlacementPolicy`, `StoragePool`, `StorageRBDImage`, `StorageFilesystem`, `StorageSubvolume`, `StorageObjectGateway`, `StorageNFSExport`, `StorageSMBResource`, `StorageNVMeoFSubsystem`, and `StorageExport` naming it". This omits StorageClient, StorageObjectUser, StorageObjectBucket and StorageReplication. selection.go:230-231 retains any `Storage*` child by name prefix. selection.go:217 follows references for a kind list without StorageReplication. storage-resources.md:248-249 "Both clusters must be declared managed clusters in the same context". Also reported as R2-05-02: environment.md:198 "The cluster-selection closure is authoritative and independent of file layout"; its StorageCluster row (204) lists StoragePlacementPolicy ... StorageNVMeoFSubsystem and StorageExport, omitting StorageClient, StorageObjectUser, StorageObjectBucket and StorageReplication, all in the 33-kind catalog (api.md:273). selection.go:187 retains any storageChild(kind) with the cluster's clusterRef. storage-resources.md:255 peerClusterRef "Required distinct receiving managed StorageCluster". api/storage.md Kinds table (258-273) has no StorageReplication row.

**Consequence.** `storageClusters: [primary]` retains the StorageReplication but not its peerClusterRef or peerPoolRef, so admission fails with unresolved references to declared objects. `storageClusters: [peer]` silently drops the relationship but keeps the peer's rbd-mirror service and the empty receiving pool. An implementer who follows the row drops clients, object users and buckets, while the code keeps them, so two closures diverge.

**Recommendation.** Rewrite the row as "every storage child whose clusterRef names it". Add a rule: a retained StorageReplication retains both its clusters as full roots, because the relationship requires both in one context.

Verifier notes:

- [refuter] Keep recommendation. Note the peer-only case refuses at admission (mirroring pool without owner) rather than silently dropping; the rule 'a retained StorageReplication retains both clusters as full roots' should also say selecting either cluster retains the replication, so peer-only selection is consistent.
- [calibrator] Keep the row rewrite. For the replication rule, state the direction in both cases: selecting either end retains the relationship and both clusters as full roots. Also add StorageReplication to the dependency switch so its peer references are followed. The acceptance test should cover selecting the primary and selecting the peer, and should assert the selection passes admission.

**Acceptance.** The row text matches the code's prefix rule. A test that selects either end of a replication retains both clusters and the relationship.

**Decision check.** F02-04 and F05-02 note StorageReplication missing from other pages; this closure row is a separate owner. The rule is consistent with the same-context requirement in storage-resources.md and with D142 (pre-1.x changes need no migration).

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-01-05 · Medium · Authoring a cluster list silently drops Machines that are not nodes or hosts

Kind: gap; Area: Environment selection closure: Machines; Status: confirmed.

Files: `specs/api/environment.md:202,214-215`; `specs/api/input.md:75-78`; `internal/environment/selection.go:71-78`

**Evidence.** environment.md:214-215 "A `Machine` or `InfraProvider` not reached by the rules above is excluded." input.md:76-77 "`validate` warns deterministically for excluded files declaring Bootwright objects and for excluded cluster roots." selection.go:73 lists the always-retained kinds, which do not include Machine.

**Consequence.** Take an Environment that holds managed-OS RHEL Machines (the examples/machines/lab-rhel shape, such as bastions) beside one or more clusters. If it authors containerClusters, for example to defer a second cluster, the RHEL installations leave the plan with no warning. No selection field can retain them. A CustomPlaybook that targets them stops resolving. The operator finds out only from what the plan lacks.

**Recommendation.** Retain Bootwright-installed Machines (and their provider closure) that are not node Machines of an excluded cluster as roots, as the closure already does for services. As a minimum, emit one validate warning per excluded Machine and InfraProvider that states no field retains it.

Verifier notes:

- [refuter] Drop the CustomPlaybook claim from the consequence (it already errors). Keep the minimum fix: one deterministic validate warning per excluded Machine/InfraProvider, or define a retention rule for Machines that are not node Machines of an excluded cluster.
- [calibrator] Prefer retaining Machines (with provider/provider-host closure) that are not node Machines of any declared cluster, matching the 'cluster-owned graph' wording at environment.md:190-193; the warning is the fallback. Note the CustomPlaybook case already errors rather than going silent. Acceptance should include a golden validate case.

**Acceptance.** environment.md states the retention or the warning. A validate test over a cluster plus a standalone managed Machine with an authored containerClusters list shows the Machine retained or warned.

**Decision check.** This is compatible with either option in F03-08 (ergonomics); it is a closure correctness point that F03-08 does not cover. No backlog decision covers Machine retention under selection.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

#### S7: Language responsibility

##### F08-01 · Medium · Remote fixed-worker Python is an unwritten exception to the no-shell process boundary

Kind: unsafe-rule; Area: Language responsibility / security process boundary; Status: confirmed; Proposed severity: High.

Files: `specs/security.md:166-172`; `specs/managed-os.md:1052-1056`; `specs/architecture.md:1020-1022`; `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py:385-399`; `ansible/collections/ansible_collections/bootwright/core/plugins/modules/managedos_registration.py:320`

**Evidence.** security.md:169 "Invocation uses no shell, a fixed safe working directory, a minimal allowlisted environment". managed-os.md:1055 "runs one fixed worker as root through `sudo -n /usr/bin/python3 -I -c`". ceph_core.py:393 "SSH remote execution requires one string; quote only fixed worker code." ceph_core.py:399 builds `shlex.join(python)` as the ssh remote command; ceph_core.py:388 composes "exec(compile(" of shipped host source. architecture.md:1020-1022 holds YAML tasks to "runs from `argv` an executable its exact allowlist names ... and never through a shell"; no equivalent clause or test covers Python launches.

**Consequence.** The remote login shell of the target account interprets every SSH worker command and its rc files run first, contrary to the stated rule; reviewers cannot tell a compliant worker from a non-compliant one. A future adapter (another storage vendor over SSH) copies the pattern with an authored token in the string, and no fitness test catches it because the process-boundary checks only read role YAML.

**Recommendation.** Add a named exception 'Remote fixed worker' to security.md#process-boundary: embedded source only, data only on stdin, absolute remote interpreter, `sudo -n`, every command-string token a literal or a closed validated token joined by one shared quoting function, worker output read only through the bounded protocol. Add a collection fitness test that every ssh argv is built by that one function and no `subprocess` call outside the per-site runner passes `shell=True` or a string.

Verifier notes:

- [refuter] Keep the named 'remote fixed worker' exception in security.md#process-boundary (or the remote-systems section) with the stated constraints; scope the fitness test to ssh remote-command construction sites and extend the existing shell=True AST check to all plugins rather than claiming none exists. Severity Medium.
- [calibrator] Reclassify as kind=gap, severity Medium. Scope the exception to every remote command string (SSH exec carries a string by protocol), not only Python workers: fixed embedded code or validated closed tokens, one shared quoting function, data on stdin only, absolute interpreter, sudo -n. The fitness test should assert shlex.join/quote is called only inside that one function and that ssh argv is built only…

**Acceptance.** security.md names the remote fixed-worker exception with its constraints; one Python function builds every SSH remote command; a unit test lists every `subprocess`/ssh construction site in plugins/ and fails on one outside the allowlist.

**Decision check.** Checked D1, D11, D99, D143, D152, D162 and the security principle 'Authored values never select the executable, form shell text' (security.md:168). No decision authorizes or forbids remote Python workers; D143 keeps one binary, compatible. The recommendation documents existing behavior rather than changing it.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F08-04 · Medium · Python, the largest adapter language, has no spec owner, reference or formatting rule

Kind: gap; Area: Language responsibility / agent guidance; Status: confirmed; Proposed severity: High.

Files: `specs/architecture.md:629-649, 680-683`; `specs/project.md:57-62`; `.agents/skills/code-implementation/SKILL.md:21-23`; `.agents/skills/code-implementation/references/formatting.md:3-4`; `.agents/skills/code-implementation/references/ansible.md:51-54`

**Evidence.** project.md:58-59 "Ansible implements bounded remote observation and effects through driven adapters." SKILL.md:22-23 loads only "[formatting], [Go], [Ansible]". formatting.md:3 "owns layout for tracked, human-authored Go, YAML, Ansible, YAML front matter". ansible.md:51-54 gives plugins one bullet. Measured: 140 plugin .py files, 36,682 lines; 188 Python test files, 61,976 lines; Ansible YAML 14,811 lines; ceph_native.py 4,002 lines.

**Consequence.** Agents write two thirds of the adapter code under guidance written for YAML tasks; Python idioms the product depends on (exact type checks, stdlib-only shipped workers, bounded subprocess, no ambient proxy, canonical JSON) are rediscovered per file, which is how the duplications in F08-06 arose. Review of Python changes has no stated standard.

**Recommendation.** Rename the architecture section to 'Language responsibility' and give Python its own row and execution sites; add `.agents/skills/code-implementation/references/python.md` (sites, imports, process runner, canonical module, exact-type validation, tests through ansible-test on controller and floor) and a Python layout section in formatting.md; SKILL.md step 3 routes by changed file extension.

Verifier notes:

- [refuter] Drop 'no spec owner' from the title; acknowledge ansible.md and architecture.md plugins row own plugins. Smallest fix: a short python.md (or a Python section in ansible.md) plus Python in formatting.md and a Python line in architecture.md's boundary section; renaming the section is optional.
- [calibrator] Downgrade to Medium. Drop the architecture.md rename/row. Smallest fix: add references/python.md (or a Python section of ansible.md) owning plugin/module_utils idioms and tests, add Python to formatting.md, link it from SKILL.md step 3. Acceptance: reference exists and is linked; formatting.md names Python; docs-check passes.

**Acceptance.** A python.md reference exists and is linked from SKILL.md; formatting.md names Python and shell; architecture.md names Python explicitly with what it may and may not hold; docs-check links resolve.

**Decision check.** Checked D10 (ansible-core pin), D41 (frozen Python/Ansible closure), D152 (controller runtime) and D162. None assigns or forbids a Python reference; the project principle 'one owner per fact' (AGENTS.md) favors one Python owner page.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F08-06 · Medium · Python repeats process runners and canonical JSON that Go implements once

Kind: duplication; Area: Language responsibility / duplication; Status: confirmed.

Files: `specs/architecture.md:767-776`; `test/architecture/canonical_json_test.go:76`; `test/architecture/adapter_protocol_test.go:42`; `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_resolution.py:25-28`; `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:353`; `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/physical_erase_process.py:19-25`

**Evidence.** architecture.md:774-775 "its one runner core starts, supervises, cancels and drains every adapter process". Go tests TestCanonicalJSONProofsLiveInOnePackage and TestOnlyTheAdapterProtocolSupervisesAdapters enforce single implementations. Python: `def canonical` in controller_channel.py, ceph_tunnel_custody.py, native_resolution.py, controller_files.py, ceph_host_route_probe.py, ceph_tunnel_staging.py, ceph_tunnel.py; separate bounded process functions in ceph_native.py:353, physical_erase_process.py:19, managedos_guest_ssh.py:134, cluster_objects_native.py:83, ceph_host_runtime.py:436.

**Consequence.** Each copy carries its own bounds, kill and reap semantics and its own canonical encoding; a fix to one (a descendant left running, a non-ASCII escape) misses the others. Digests computed by a divergent copy refuse fail-closed, so the cost is debugging time on live runs rather than unsafety, but cancellation and output-bound defects would not be.

**Recommendation.** Specify one canonical module and one bounded process runner per Python execution site (controller, managed host, shipped worker, where shipped workers embed the shared source at build time), with cross-language golden vectors the Go canonical package also passes, and a collection AST test that forbids `subprocess` and `json.dumps(sort_keys=True` outside those modules.

Verifier notes:

- [refuter] State the rule in architecture.md next to the adapter_protocol.py clause (884-889): one canonical-encoding module and one bounded-runner module, each with a declared exception list for encodings that differ on purpose (for example, request signing in ceph_native_object.py). Relate the work to B461 instead of opening a parallel item.
- [calibrator] Reframe evidence: cite absence of a Python execution/encoding owner (architecture.md#language responsibility area) instead of the Go runner clause. Smallest change: spec one canonical module and one bounded runner per execution site, and reuse the existing pylint `ansible-bad-function` rule as the gate (an allowlist of files permitted to disable it) instead of a new AST test. Golden vectors shared with…

**Acceptance.** At most one canonical and one runner module per site; a Python test fails on a new copy; a shared vector file is read by both a Go and a Python test.

**Decision check.** Checked D99 (one shared plugin base carries per-capability evidence functions) which supports consolidation; D162 untouched; the dependency-reuse principle (architecture.md#dependency-selection-and-reuse) favors one implementation.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F08-07 · Medium · No placement question or fitness test decides which language new logic belongs in

Kind: gap; Area: Language responsibility / architecture verification; Status: confirmed-by-arbitration.

Files: `specs/architecture.md:629-668, 989-1039`; `.agents/skills/code-implementation/references/go.md:1-6`; `.agents/skills/code-implementation/references/ansible.md:1-8`

**Evidence.** architecture.md:994-995 "Check every production Go package for inward dependencies, declared context ownership"; the Ansible bullets (1010-1039) check role YAML. ansible.md:8 "Lifecycle policy remains in Go." go.md:3-6 defers to architecture. No clause states a criterion for Python versus YAML versus Go for a new step, and no test classifies sources by language site. Arbiter-cited evidence: specs/architecture.md:629-668 "Go and Ansible responsibility boundary"; specs/architecture.md:752-757 "Plugins contain no workflow or authorization policy"; specs/architecture.md:209 "never product policy"; ansible/collections/ansible_collections/bootwright/core/plugins/filter/managedos_network.py:1-4 "name its next step"; .agents/skills/code-implementation/references/ansible.md:8 "Lifecycle policy remains in Go."

**Consequence.** Placement is decided per change by precedent, which already produced Python filters that choose steps (managedos_network) and a native file editor in a filter (agent_disk_encryption); every new substrate or storage type re-litigates where its mechanics go, and the owner cannot review a placement against a rule.

**Recommendation.** Adopt the four-language matrix (notes/F08.md): per language what belongs, what is prohibited, the test owner and one placement question; place it as the 'Language responsibility' section of architecture.md, and add a `test/architecture` test that maps every tracked source path to a matrix site and fails on an unmapped file, plus the Python AST checks of F08-01 and F08-06.

Verifier notes:

- [refuter] If kept, reduce it to Low: add one sentence in the Roles or plugins row about when to use a YAML task and when to use a Python plugin (a plugin only for effect-free translation/normalization or protocol publication; effects through allowlisted modules). Optionally add a fitness test that fails on a collection file outside the declared table paths. Do not add a new four-language matrix section that would re…
- [calibrator] Acknowledge architecture.md:750-757. Widen the existing 'Go and Ansible responsibility boundary' section in place rather than adding a parallel section: one placement question and a prohibited-use list for each of YAML tasks, Python plugins and module_utils, shell wrappers and Makefile. Make the path-to-site fitness test optional, or limit it to plugins/ and scripts/, introduced with the next boundary c…

**Acceptance.** architecture.md carries the matrix with placement questions; go.md, ansible.md and python.md link to it instead of restating; a fitness test fails on a source file outside every declared site. Arbiter acceptance: architecture.md 629-668 states the placement question and prohibited uses per language site; go.md and ansible.md link rather than restate; no python.md required.

**Decision check.** Checked D1, D11, D99, D143, D162, D163 and the principle 'introduce abstractions only for real consumers' (project.md:88): the matrix codifies existing practice and adds no runtime abstraction.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

#### S8: Reconciliation rules

##### F10-01 · High · Physical OS install erases whatever disk a device path names, refusing hints

Kind: unsafe-rule; Area: Mutation safety / physical disk safety (managed-OS installation); Status: confirmed; Proposed severity: Blocking.

Files: `specs/managed-os.md:1268-1273`; `specs/managed-os.md:1275-1284`; `specs/managed-os.md:167-170`; `specs/managed-os.md:1392-1393`; `specs/api/machines.md:306-314`; `specs/state-reconciliation.md:1813-1815`; `specs/state-reconciliation.md:1906-1909`; `specs/state-reconciliation.md:1932-1935`; `specs/storage.md:702-705`; `specs/container-clusters.md:199-201`; `.agents/skills/security-safety/SKILL.md:18-21`

**Evidence.** managed-os.md:1268-1273 "Installation erasure is confined to the disk rootDeviceHints.deviceName names ... One declaring any other hint beside deviceName refuses before registration". managed-os.md:1277-1280 %pre checks only that "the declared root device must resolve to a whole block device". api/machines.md:308 "deviceName is a device path: a clean absolute path beneath /dev/" (so /dev/sda is admitted). Contrast storage.md:702-703 OSD paths "restricted here to /dev/disk/by-id/wwn-0x plus 16 or 32 lowercase hexadecimal digits"; state-reconciliation.md:1933-1934 erase targets require "deviceName, wwn, serialNumber and exact sizeBytes"; container-clusters.md:200-201 agent-config carries "every root-device hint the Machine declares". The skill entrenches it: SKILL.md:19-21 "A root disk is named by a device path; an empty or wwn-only selector refuses."

**Consequence.** On a multi-disk server, kernel names (/dev/sda, /dev/nvme0n1) follow driver probe order and change with controller firmware, hot-added disks or a BMC virtual USB device. A Machine authored with deviceName /dev/sda installs; on the next fresh apply (or on a sibling server of the same model) /dev/sda is a data disk holding a surviving Ceph OSD or operator data. Every controller-side proof (UUID, MACs, power-off) passes, %pre sees a whole block device, `clearpart` wipes the wrong disk. data-loss was consumed, but it acknowledges loss of the root disk, not of the data disk. The operator cannot even add serialNumber or wwn as a cross-check, because the managed-OS path refuses them, while the agent path for OpenShift nodes carries them. This violates the general rule to revalidate exact target identity immediately before every effect.

**Recommendation.** For a physical (baremetal) Bootwright-installed Machine, require the root disk to carry a durable identity and check it inside %pre before any storage directive: either restrict deviceName to /dev/disk/by-id/wwn-0x<hex> (the OSD rule) or require wwn plus serialNumber beside deviceName and have %pre resolve deviceName and compare the device's WWN/serial (and size when the erase policy lists it) with the frozen values, refusing on mismatch or an unreadable attribute. Stop refusing wwn/serialNumber beside deviceName on baremetal; refuse only predicate hints. Update the security-safety skill bullet to match.

Verifier notes:

- [refuter] Keep the recommendation but prefer the smaller variant: require deviceName under /dev/disk/by-id/ (wwn-0x form) on baremetal, or admit wwn/serialNumber beside deviceName and compare them in %pre with fail-closed on mismatch/unreadable. Drop the 'sibling server' consequence; MAC/UUID proof already binds the machine.
- [calibrator] Prefer the smallest option: on baremetal managed-OS installs require deviceName under /dev/disk/by-id/ (wwn-0x form as storage.md), and have %pre refuse unless the path is that form and resolves to a whole disk; the wwn/serial comparison is the stronger follow-up. Also reconcile api/machines.md:313-314 with the managed-OS wwn-only refusal. Acceptance: name a negative test for a /dev/sdX deviceName on ba…

**Acceptance.** managed-os.md Physical installation states that %pre compares the resolved root device's WWN and serial (or that deviceName is a wwn by-id path) with the frozen request and stops before storage on mismatch or unknown; the refusal table no longer rejects wwn/serialNumber beside deviceName on baremetal; api/machines.md states the baremetal deviceName form; SKILL.md bullet updated; a negative test name is cited for a %pre identity mismatch.

**Decision check.** D177 (B70) already requires deviceName+wwn+serialNumber+sizeBytes for erase targets, so reusing that identity at install is consistent. D44 concerns libvirt only (refuses wwn/hctl/serial there) and is unaffected because the change is baremetal-only. D153's principle 'target environment can never be broken with bootwright apply' supports the change. No decision accepts deviceName-only selection as sufficient identity.

Verification: refuter real, high confidence, severity High; calibrator real, high confidence, severity Blocking; no arbitration needed.

##### F10-03 · High · Name-derived ownership breaks mutation safety; B568 and D155 warning unlinked

Kind: contradiction; Area: Ownership proofs / context deletion and reuse; Status: confirmed.

Files: `specs/state-reconciliation.md:1816-1820`; `specs/state-reconciliation.md:1755-1756`; `specs/substrates.md:320-328`; `specs/substrates.md:367-369`; `specs/contexts.md:16-23`; `specs/contexts.md:816-821`; `specs/contexts.md:832-839`; `specs/container-clusters.md:503-505`; `specs/milestones/backlog.md:153`; `specs/milestones/backlog.md:3325-3329`

**Evidence.** state-reconciliation.md:1816-1817 "A name or controller record may locate a target but never proves ownership or identity." Same page 1755-1756: "An object an abandoned context of the same name left on a host reads as this context's, because no native identity yet distinguishes the two." substrates.md:320-321 domain UUID is "derived from the context and Machine names". contexts.md:816-821 the acknowledged deletion "abandons the objects rather than removing them ... it has no remote resource effect" and names no warning against re-initializing the name. backlog.md:3326-3329 (D155): "the B567 guard and B568 interim orphan warning come first". grep finds B568 in no spec page.

**Consequence.** After `context delete --purge --allow-orphans` and `context init` of the same name (or two controllers whose contexts share a name on one libvirt host), the new context's fresh apply finds the abandoned domain 'owned', reports it completed, and its virtual installation (which consumes no data-loss on apply) runs clearpart over the abandoned guest's disk; its destroy deletes the other controller's VMs under its own data-loss token. The general rule promises the opposite, so an implementer or reviewer reading Mutation safety cannot see the exception.

**Recommendation.** Spec-only now: (1) in Mutation safety, after the 'name never proves ownership' bullet, add a one-line known exception naming libvirt, emulated BMC, managed-service units and artifact servers, linking B568; (2) add D155's interim wording to contexts.md Permanent deletion and to the orphan confirmation: remove the abandoned objects by hand before initializing the same name again, and never reuse a context name across controllers that share a provider host; (3) move 1755-1756 to cite B568. The incarnation identity itself stays in B568's named slice.

Verifier notes:

- [refuter] Also correct infrastructure-services.md:323-324 (the 'cannot occur without a reservation conflict' claim) to name the abandoned same-name exception and link B568; add the B568 link at substrates.md:367-369 domain ownership.
- [calibrator] Split the change. Linking B568 as a known exception under Mutation safety, citing it at 1755-1756, and adding a hazard note to contexts.md are spec-only clarifications. Changing what the orphan confirmation prints is a CLI contract change: it is D155's interim B568 step and needs a named slice, with a test cited in the spec. Acceptance should cover both parts.

**Acceptance.** grep finds B568 in state-reconciliation.md Mutation safety and in contexts.md Permanent deletion; the orphan confirmation text in contexts.md names the by-hand removal and the same-name hazard; make docs-check passes.

**Decision check.** D155 accepts persistent random incarnation identity as direction and explicitly orders the interim orphan warning first; this recommendation is exactly that interim step and authorizes no implementation. Consistent with the memory-recorded rule that context name is identity (no allocator), since incarnation is an ownership marker, not a selection identity.

Verification: refuter real, high confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### F13-01 · High · contexts.md is stale after setup v2: registry version, descriptor and lock owner

Kind: contradiction; Area: Contexts / durable formats; Status: confirmed; Also raised as: F16-02.

Files: `specs/contexts.md:186-205`; `specs/contexts.md:250`; `specs/contexts.md:302-310`; `specs/contexts.md:329-335`; `specs/contexts.md:447-458`; `specs/contexts/controller-record.md:24-35`; `specs/controller.md:794-802`; `internal/workspace/contexts/contracts.go:24`; `internal/workspace/contextfs/setup_linux_amd64.go:124-155`; `specs/contexts.md:186-204`; `specs/milestones/backlog.md:3305`

**Evidence.** specs/contexts.md:186 "The registry writes private format version 5." and contexts.md:192-195 "A `registry.json` of version 5 ... refuses every access with `context.state`"; record table contexts.md:304 "Registry | `version` (5), `contexts`, then `controller` when setup has published it"; code: internal/workspace/contexts/contracts.go:24 "const RegistryVersion = 6"; D152 clause 9 "the registry from 5 to 6". contexts.md:189 "The Controller descriptor is an optional trailing member that confirmed setup adds" vs controller-record.md:32 "Setup itself writes no controller record." contexts.md:332 lists "`setup` (which scopes no context)" under the exclusive root lock and contexts.md:452-453 lists `setup` among commands that open a registry transaction, while controller.md:799-801 "Setup's own subtree has its own lock, `setup/lock`" and setup_linux_amd64.go:131-144 opens the root without a root lock and takes only setup/lock. Also reported as F16-02: contexts.md:186 says "The registry writes private format version 5." contexts.md:191-194 says "A `registry.json` of version 5, which every build before the setup v2 cut-over wrote, refuses every access". contracts.go:24 has `const RegistryVersion = 6`. D152 clause 9 says "the registry from 5 to 6".

**Consequence.** The owning page of the registry format states, in one section, that this build writes version 5 and refuses version 5. An agent implementing or reviewing from contexts.md (the page specs/index.md routes registry questions to) would write the wrong version, re-attach the descriptor to setup, or add an exclusive root lock around setup's dnf install and wheel downloads, which would make every other command on the host refuse lifecycle.lease for minutes. make docs-check checks the bounds table against code but not the record-version table, so the drift went unseen.

**Recommendation.** In contexts.md, change lines 186-189, 250 and 304 to registry version 6 with the descriptor added by the first binding (link controller-record.md#descriptor); remove `setup` from the exclusive-root-lock row (332) and from the registry-transaction list (452), and state that setup takes only `setup/lock` and an exclusive root transaction only to create the setup subtree once. Extend the docs-check table test to compare each record-version cell (Registry, Reservation, Manifest, User selection, Media record, Controller record) with its Go constant, as TestDocumentedBoundsMatchCode already does for bounds.

Verifier notes:

- [refuter] Keep as stated; note that the one-time subtree creation does take an exclusive root transaction, so the lock row should say so rather than drop setup entirely. The docs-check extension is a sensible but optional addition.
- [calibrator] Keep the spec edits. In the setup locking sentence, say that setup's run holds no exclusive root lock, takes a single exclusive root transaction only to create the missing setup subtree, then holds only setup/lock, and link controller.md for the details. The docs-check test for record versions is a sound but separate hardening step; it is not needed to fix the defect.

**Acceptance.** grep finds no 'version 5' describing what this build writes; the Registry row reads `version` (6); no sentence says setup publishes the descriptor or holds the root lock during its run; a docs-check test fails if a record-version cell differs from its constant.

**Decision check.** D152 clause 9 (registry 5 to 6, controller record 2 to 3, setup writes no controller record) supports the correction; D63 (store-wide verification) unaffected. Project principle 'one owner per fact' supports making contexts.md defer setup locking to controller-record.md.

Verification: refuter real, high confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### R2-02-01 · High · StorageExport retirement can destroy a surviving consumer cluster's storage

Kind: unsafe-rule; Area: Reconciliation rules / Storage and add-ons (FDF removal order); Status: confirmed.

Files: `specs/storage-resources.md:1252-1255`; `specs/state-reconciliation.md:1836-1842`; `specs/state-reconciliation.md:1895-1898`; `specs/container-clusters.md:879-881`; `specs/add-on-coverage.md:44`; `specs/storage.md:1459-1462`

**Evidence.** storage-resources.md:1252-1255 "The export block retires with its cluster when every host is a Bootwright-owned virtual Machine the same destroy removes". state-reconciliation.md:1836-1839 "A storage effect whose every host Machine is a Bootwright-owned virtual Machine ... its quiescence derives from their stop proof". state-reconciliation.md:1896-1897 "`data-loss` acknowledges destruction; quiescence independently proves nothing is using the data." container-clusters.md:879-881 "A cluster whose nodes are physical keeps running after its context is destroyed". add-on-coverage.md:44 fdf removal: "retired with its cluster; the export's principals leave with the Ceph cluster's owned virtual hosts". storage.md:1459 target removal first must "prove storage clients quiescent".

**Consequence.** A context with a virtual Ceph cluster on libvirt and an OpenShift cluster on physical (xFusion) nodes consuming it through fdf passes destroy: Ceph VMs are stopped and deleted under data-loss, the physical nodes need no stop proof (the claim block is always quiescent), and the surviving cluster keeps running workloads whose PVCs point at a deleted Ceph cluster. Consumer quiescence was never proved, contradicting the rule that data-loss does not substitute for quiescence.

**Recommendation.** Add to the export block's retirement condition (storage-resources.md StorageExport and the fdf coverage row) that every cluster consuming the export through an fdf instance must also be retired with owned virtual Machines stopped by the same removal; otherwise the export's removal refuses before registration with the consuming cluster named, joining the B491 refusal until consumer quiescence for surviving clusters is defined.

Verifier notes:

- [refuter] Keep recommendation; cite the RBD-replication precedent (storage-resources.md:279-281) as the model: export retires only when every consuming cluster's fdf instance is retired with owned virtual Machines in the same removal, else refuse under B491 naming the consumer cluster. Also update the fdf removal description in add-on-coverage.md.
- [calibrator] Put the new row in the existing refusal table at storage-resources.md:1172, not a new table. The refusal must name a remedy, because otherwise the context has no destroy path until B491 lands: for example, destroy with the consumer's nodes owned virtual Machines, or wait for B491/B769. Also have planning or apply flag this topology as not destroyable when the operator declares it, so the operator does n…

**Acceptance.** storage-resources.md states the consumer-cluster condition for export retirement; the export refusal table gains a row 'consumer cluster survives the removal'; a planner test refuses destroy of a context with virtual Ceph and a physical fdf consumer, naming the cluster.

**Decision check.** Checked D173 and its Q39 answer (B86 adds no revocation; per-export revocation while the cluster stays goes to B491): the fix adds a refusal and no revocation, consistent. D177 (erase opt-in) and D153 (destroy then fresh apply) unaffected. Consistent with the fail-closed and 'quiescence independently proves' principle in state-reconciliation.md:1895-1898. No decision accepts losing consumer storage.

Verification: refuter real, medium confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### R2-04-01 · High · Removal needs the apply-bound credential, so rotating a BMC secret strands it

Kind: unsafe-rule; Area: Reconciliation rules: secret binding across rotation; Status: confirmed.

Files: `specs/state-reconciliation.md:1482-1486`; `specs/state-reconciliation.md:1511-1524`; `specs/secrets.md:295-299`; `specs/substrates.md:641-649`; `specs/managed-os.md:943-947`

**Evidence.** state-reconciliation.md:1482-1486 "A fresh removal uses the secret material its apply bound ... a declaration edited or rotated afterwards never silently changes which version a removal presents." state-reconciliation.md:1521-1524 "No re-binding of the current declarations, substitution of other material, removal without the binding ... exists". secrets.md:295-299 "Bounded consumers bind nothing: machine start, stop and restart ... read the current version of every Secret they name". substrates.md:648-649 "The kubeconfig is lent to a run as an operation-scoped file". managed-os.md:945-946 "a remedy that takes the context back with destroy before replacing the Secret, since a continued apply reads the bound version".

**Consequence.** The rule is right for material Bootwright placed (a LUKS passphrase, a host key, a generated certificate) but it is applied equally to credentials Bootwright only presents to authenticate to a system the operator owns. A physical BMC password rotated by a 90-day policy, a KubeVirt kubeconfig whose token expired, or a changed vCenter password makes every continuation and every destroy authenticate with the old bound version and fail. The operator updates the keyring with secret set; machine stop now works (current version) but destroy still fails (bound version). The only exits the spec names are restoring an old keyring (which does not help, the old password is what fails) or context delete --purge --allow-orphans, abandoning VMs, disks and reservations. No rule states which material is right when bounded and lifecycle consumers disagree.

**Recommendation.** Split Secret consumption into two classes in secrets.md#immutable-binding-and-contexts: effect material (placed or derived into the effect; removal must present the bound version, as today) and access credentials (presented to an operator-owned endpoint whose ownership proof does not depend on the credential: BMC/Redfish auth, KubeVirt kubeconfigRef, vCenter, registry login). For access credentials, let a continuation or removal rebind the current version only under an explicit flag (for example destroy --rebind-access <Secret>) recorded in the operation, keeping ownership proofs (UIDs, pinned identities, host keys) unchanged. Each consumer field's class is declared once in the API page that defines it.

Verifier notes:

- [refuter] Lead with BMC/Redfish (realized) as the failure scenario. Cite m1.md:510-511 as the existing KubeVirt open question and fold it into the access-credential rule. Also state the diagnostic and exit that a held but externally rejected access credential gets, which no spec names today.
- [calibrator] Cite m1.md:510-511 and resolve it under the same rule. Name the remedy so it does not collide with B181's bounded-run 'rebind'. Gate it with the existing --authorize token model and record it as a new binding version in the operation's evidence, instead of adding a free-standing destroy flag. Declare the class per Secret-consuming field. Do not split the Secret type. Add to acceptance a test showing tha…

**Acceptance.** secrets.md names the two classes and the rebind rule; every *Ref field of a Secret in specs/api lists its class; state-reconciliation.md#continuation-and-removal names the rebind exit beside D37's two exits; a test rotates a bound BMC credential and proves destroy succeeds only with the flag and that the ownership proof still runs.

**Decision check.** D37 (lost binding exits: restore keyring or orphan delete) covers a binding the keyring no longer holds, not a held binding whose credential the external system no longer accepts; the recommendation adds an exit and leaves D37 intact. D85 (bounded power, rsh and exec read current material in one keyring session and publish no binding) is kept. It conflicts with the sentence at state-reconciliation.md:1521-1524, which is the point: that rationale (the material that created an effect proves it gone) does not hold for access credentials. Consistent with the project principle that identity comes from desired state and ownership proofs, not from the credential.

Verification: refuter real, medium confidence, severity High; calibrator real, medium confidence, severity High; no arbitration needed.

##### R2-06-01 · High · External libvirt attachments claim nothing, so contexts share a managed bridge

Kind: unsafe-rule; Area: Reconciliation rules / host reservations and substrates; Status: confirmed.

Files: `specs/substrates.md:135-137, 211-215, 230-245`; `specs/api/machines.md:158`; `specs/api/container-clusters.md:253-256`; `specs/milestones/backlog.md:1298-1343, 3317-3329`

**Evidence.** substrates.md:135-137 "An `external` attachment is proved present as a bridge device ... and is never defined, changed or removed." substrates.md:236-240 reservations: "`bridge:<name>` for every managed attachment ... `prefix:<masked prefix>` for every managed attachment" (external claims none). substrates.md:230-233 "its quiescence is derived from theirs under the removal gate rather than observed on the host." substrates.md:211-212 "Stopping a network disconnects every domain plugged into it". api/container-clusters.md:253-255 "A managed libvirt network's prefix belongs to one context through its exclusive reservation, so no address in it is shared across contexts". api/machines.md:158 admission's bridge rule is per host within one context's catalog. backlog.md:1300-1301 D155 "requires definition and delivery of this guard ... before further `destroy` on a shared libvirt host"; backlog.md:1336-1338 a `bridge-use:` reservation "is a separate item to record when this one is promoted". No spec page outside milestones links B567.

**Consequence.** Context B declares `management: external, bridge: <A's managed bridge>` (the default management value) and takes addresses in A's prefix; nothing refuses it. A's `destroy` derives quiescence from A's own Machines, stops and undefines the network, and B's guests lose their uplink (running ones at once, shut-off ones can no longer start). B's settled apply still reads done. The container-cluster rule that no address in a managed prefix is shared across contexts is false, so address-uniqueness reasoning built on it is unsound. B567's planned guard checks only domains that are not shut off, so B's stopped guests are still stranded.

**Recommendation.** Add a shared reservation class `bridge-use:<name>` that every controller-arm external attachment publishes, and state one cross-class rule in the key table: a removal that would release an exclusive `bridge:<name>` refuses `controller.conflict` while another context holds `bridge-use:<name>`, naming that context and `bootwright destroy --context <user>`. Keep B567's live-domain probe for domains outside Bootwright. Replace substrates.md:230-233 with an observed-quiescence rule plus a Not-yet-met line linking B567, and narrow api/container-clusters.md:253-255 to 'no other context's managed attachment'.

Verifier notes:

- [refuter] Note that the defect is already recorded as B567 under D155; frame the fix as (1) adding a Not-yet-met link to B567 at substrates.md:230-233 and narrowing container-clusters.md:253-255 now, and (2) recording the bridge-use: shared key (modelled on the existing media: shared-claim row) as its own backlog item tied to B567, rather than a standalone spec change implemented outside the named safety slice. Exte…
- [calibrator] State that B567 already owns the destroy-guard defect and that this finding extends it (shut-off guests, address claim); D155 marks it accepted direction, not authorization, so the spec change should be paired with promoting B567 and recording the bridge-use item. Consider the simpler alternative of making bridge-use conflict with another context's bridge: at acquisition (refuse at apply), which also ma…

**Acceptance.** The key table lists `bridge-use:` as shared with its release-blocking relation; substrates.md names it among a libvirt provider's reservations for external attachments; a test such as TestARemovalOfAManagedBridgeAnotherContextUsesRefuses is named; substrates.md:230-233 no longer asserts derived quiescence without a Not-yet-met link to B567; api/container-clusters.md:253-255 matches the rule.

**Decision check.** D155 (backlog.md:3317-3329) requires the B567 guard before shared-host destroy and B567 itself proposes `bridge-use:` as a follow-up: the recommendation implements that direction and adds no version or compatibility rule (D163). D95 covers only same-catalog bridge conflicts and is unaffected. Not a repeat of F10-03 (same-name adoption).

Verification: refuter real, high confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### F09-01 · Medium · One unreachable target blocks a whole removal; the only exit abandons all effects

Kind: design; Area: State reconciliation: unknown outcomes and removal; Status: confirmed; Proposed severity: High.

Files: `specs/state-reconciliation.md:25-26`; `specs/state-reconciliation.md:586-588`; `specs/state-reconciliation.md:1240-1243`; `specs/state-reconciliation.md:1275-1282`

**Evidence.** specs/state-reconciliation.md:25-26 "There is no reconciliation, partial planning, adoption, reclaim of ownership, or force path." :586-588 "A target neither observation resolves stays `unknown`, and the removal refuses, registering nothing." :1240-1243 "A removal that still cannot prove an effect registers nothing and refuses, naming each block it could not prove". :1279-1282 "Deleting the context with `--allow-orphans` instead abandons whatever the block may own, and releases the context's host reservations".

**Consequence.** A bare-metal node whose BMC died, or a libvirt host that was reinstalled, leaves one apply block `unknown` that no observation can ever resolve. Every other block of a 20-block environment (VMs on healthy hosts, DNS, NTP, load balancers, Ceph on other nodes) can then only be abandoned whole by `context delete --purge --allow-orphans`, which leaves them running unmanaged and, until B568 lands, lets a re-initialized same-name context adopt and erase them. Physical hardware failure is an ordinary day-0 event for the owner's bare-metal portfolio.

**Recommendation.** Add one scoped, acknowledged exit: a fresh destroy may take an explicit per-block abandonment acknowledgement (for example `--abandon <block-id>`, repeatable, confirmed) for a block that stays `unknown`; the removal then records that block abandoned (a closed effect state on the apply's block, never `done`), removes every other owned block in inverted order (dropping edges to the abandoned block only where its dependents are proved gone), and keeps the abandoned block's host reservations until context deletion. Everything else (no force path for unproved effects without acknowledgement, observation first) stays.

Verifier notes:

- [refuter] Frame this as a decision brief to reopen D38, not a defect. Drop the reinstalled-host example and keep permanently unreadable targets (dead BMC, decommissioned host). A scoped `--abandon <block>` must also keep that block's ownership markers out of reach of same-name adoption, so it depends on B568 landing first.
- [calibrator] Frame as a decision request to reopen D38 (per D153 clause 3 recipe: decision, spec row, stateMachineRows case, named slice). Scoped abandonment must also retain every block the abandoned block depends on (its ancestors in the plan DAG), not only its host reservations, or require each to be named too; abandoned blocks must keep ownership markers recorded so B568's same-name adoption guard covers them; a…

**Acceptance.** The state machine has a row for a removal over an unresolvable unknown block that names the scoped abandonment; the removal plan and `status` list abandoned blocks; a test drives a removal with one unreachable block to completion of every other block; the orphan-acknowledged deletion is no longer the only named exit in :1279-1282.

**Decision check.** Conflicts with D38 (an unresolvable unknown block keeps its refusal; the orphan-acknowledged delete is the exit) and with the 'no force path' clause; requires the owner to reopen D38. Consistent with D88/D89 (acknowledged abandonment as a principle, here scoped to one block) and with the safety priority of B568 under D155, since it shrinks what the orphaning delete abandons.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F09-02 · Medium · Apply retries may consume the reserve removal needs, leaving live effects

Kind: unsafe-rule; Area: Contexts storage bounds and state reconciliation; Status: confirmed; Proposed severity: High.

Files: `specs/contexts.md:566-578`; `specs/contexts.md:589-596`; `specs/contexts.md:612-613`; `specs/state-reconciliation.md:1284-1302`

**Evidence.** specs/contexts.md:575-578 "That removal therefore refuses at its registration only once the apply's own later attempts used them up, and it then leaves the apply's evidence, binding and effects as they were". :612 "No command prunes the operations a context retains". specs/state-reconciliation.md:1296-1299 restoration "first reopens that operation's operation log under the exclusive lock and durably writes its opening record".

**Consequence.** A persistently failing block retried across many applies (or several blocks over tens of continuations) draws down the 1024 free entries and 16 MiB the fresh apply reserved for its removal. Once used up, `destroy` refuses at registration; if the bound is hit mid-attempt, the log fault latches, the block stays `running`, and restoration itself needs a write. The only exit is the orphan-acknowledged deletion with every effect live. B572 fixes the entry counting but not the rule that lets the apply spend the removal's reserve.

**Recommendation.** Invert the priority in contexts.md: the removal's first pass plus one entry and its registration bytes are reserved for the removal alone; an apply attempt, retry or resolution start refuses `lifecycle.state` before its effect when its write set would cut into that reserve, naming `bootwright destroy --context <name>`. Delete the sentence at :575-578 that accepts the removal's refusal.

Verifier notes:

- [refuter] Fold into B572 rather than a separate item: amend B572's fix so the removal's first pass plus one entry and registration bytes are reserved exclusively, and apply attempts/retries/resolution starts refuse lifecycle.state before effect when they would cut into it. Severity Medium.
- [calibrator] Fold this into B572 as the rule half of its fix rather than a separate item. B572's whole-write-set admission is a precondition, because without it an apply cannot refuse before its effect. Reserve the removal's first pass, its registration bytes and its restoration write (the opening record of the replaced operation's log). The removal's own retries and resolutions also need a bound: when they exhaust…

**Acceptance.** contexts.md states that only the removal may write into its reserve; a test exhausts the area by repeated apply retries and shows the next apply refusing before an effect while the destroy registers and completes; no remaining sentence admits a removal refusing at registration because of the apply's own writes.

**Decision check.** Consistent with D105 (B243, B267 parked, not rejected) and with B572's recorded smallest fix, which it extends; no decision accepts stranding a removal. Consistent with the principle that bounds never authorize evidence deletion (contexts.md:659-660), because nothing is pruned.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity High; no arbitration needed.

##### F09-03 · Medium · A completed apply with a lost block record is a dead end for every verb

Kind: gap; Area: State reconciliation: continuation and removal; CLI status next steps; Status: confirmed.

Files: `specs/state-reconciliation.md:1358-1366`; `specs/state-reconciliation.md:1431-1434`; `specs/cli.md:634-660`

**Evidence.** specs/state-reconciliation.md:1358-1360 "A completed apply owns its whole frozen plan, so the removal of one refuses `lifecycle.state` while any block of that plan is not `done`". :1431-1434 refusals point at status "except the two whose only exit is deleting the context, beside no operation and over a completed destroy". specs/cli.md:634-660 lists next steps for no operation, incomplete apply and incomplete destroy only.

**Consequence.** A block directory lost to disk damage under a completed apply leaves `apply` and `destroy` both refusing, a refusal that names only `bootwright status`, and a `status` that names the contradiction but offers no command. The operator has no named exit, although the frozen plan alone already proves what the apply owns, so a safe removal exists.

**Recommendation.** Use the ownership fact the spec already states: a removal of a completed apply covers its whole frozen plan regardless of block records, treating each block whose record is not `done` as unproved and resolving it by its apply observation and removal check before registering (as for an incomplete apply). If the owner prefers to keep the refusal, name the deletion exit in it as for a completed destroy and add the case to cli.md's next-step list.

Verifier notes:

- [refuter] Prefer the second option as the smallest fix: name `context delete --purge` (with --allow-orphans unless the evidence is pristine) in the refusal and in cli.md's completed-operation clause, the same way as for a completed destroy. Resolving lost records by observation is a larger design change and needs an owner decision. Extend the cli.md citation to 679-697.
- [calibrator] Make option 2 the primary fix because it is the smallest. Add the completed-apply case to the deletion-exit exception (:1431-1434), to state-machine row :465 and to cli.md's 'over a completed operation' step. Name 'context delete --purge --allow-orphans' and warn that it abandons the apply's host effects. Option 1, removing the whole frozen plan, reverses the rationale stated at :1360-1362. Offer it onl…

**Acceptance.** Either the state machine admits a destroy over a completed apply with non-done block records (resolving them first) and a test proves every frozen block is removed, or the refusal and `status` name `context delete --purge --allow-orphans` for this state; in both cases no lifecycle state lacks a named exit.

**Decision check.** No decision found accepting a nameless dead end; B581 (one normalization of lost records) is related but does not address the exit. Consistent with D38-style naming of exits and with fail-closed resolution before removal (D119, D123).

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F09-04 · Medium · Superseding removal waits for target restore, contradicting D119 drift removal

Kind: contradiction; Area: State reconciliation: unknown outcomes and resolution; Status: confirmed.

Files: `specs/state-reconciliation.md:576-583`; `specs/state-reconciliation.md:1245-1256`

**Evidence.** specs/state-reconciliation.md:581-583 "so a target that drifted from that request yet reads as this context's own is removed rather than refused." :1254-1256 "A removal that supersedes an apply resolves the apply's blocks by the apply's reading, so it refuses the same way until the target is restored."

**Consequence.** One implementer makes a fresh destroy refuse over a drifted owned target until the operator restores it (the operator is told to rebuild what they want removed); another removes it per D119. When both observations fail, the diagnostic names the apply's first difference and 'restore the target', not why the removal check could not read the target as this context's own, so the operator fixes the wrong thing.

**Recommendation.** Rewrite :1254-1256: a superseding removal reports, for a block neither observation resolves, the removal check's reason (what keeps the target from reading as this context's own) after the apply reading's, and its remedy is the removal's; it never refuses a target the removal check reads as owned.

Verifier notes:

- [refuter] Fix substrates.md:480-483 together with state-reconciliation.md:1254-1256. If the intent is only diagnostic ordering in status, tie it to B460 rather than to refusal semantics.
- [calibrator] Delete the sentence at state-reconciliation.md:1254-1256 and the parallel clause at substrates.md:480-483, replacing each with: a superseding removal first applies the apply observation, then the removal's check (link to the D119 section); only a block neither resolves refuses, naming the removal check's reason and remedy. Acceptance: grep for 'by the apply's reading' across specs/ returns nothing; one…

**Acceptance.** No sentence says a superseding removal refuses by the apply's reading alone; the diagnostic order for a removal over an unknown apply block is stated once and a golden test shows both reasons.

**Decision check.** Aligns with D119 and D123 (apply observation first, then the removal's own check); no decision supports the contrary sentence.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### F10-02 · Medium · Day-2 power commands act on unproved physical targets, including the controller host

Kind: unsafe-rule; Area: Identity and power operations / controller-host protection; Status: confirmed; Proposed severity: High.

Files: `specs/substrates.md:815-831`; `specs/cli.md:1346-1354`; `specs/cli.md:1376-1384`; `specs/state-reconciliation.md:1813-1816`; `specs/state-reconciliation.md:2042-2047`; `specs/api/environment.md:369-374`; `specs/api/machines.md:189`; `specs/security.md:667-672`

**Evidence.** substrates.md:830-831 "a value the proof recorded empty is not compared, and a Machine with no pin is compared with nothing." cli.md:1353-1354 "A Machine that authors its own controller is reachable whenever that controller answers." cli.md:1377-1378 "--force cuts the power instead". The pin check compares UUID/serial only; no MAC proof (substrates.md:826-829). state-reconciliation.md:2042-2043 protects controller power only for "An apply or destroy". api/environment.md:369-374 sets controller requirements but does not forbid hardware.management.bmc on it, and no power rule excludes the controller Machine.

**Consequence.** (a) Before the first apply, after a destroy ended the pin, or for an OS-provided Machine that authors a BMC, `machine stop --force --name m1 --yes` sends a power cut to whatever ComputerSystem answers at the address: after a re-addressing that is another production server. (b) If the controller Machine authors its BMC (nothing forbids it) or another Machine's BMC address is a copy-paste of the controller's, `machine stop`/`restart` powers off the host running Bootwright, its runtime and the evidence of in-flight operations of other contexts. The mutation-safety rule ('immediately before every effect, revalidate exact target identity') is not applied to these effects.

**Recommendation.** Apply the physical target proof to every power request on an authored controller: require a current pin, or else run the full target proof (complete inventory, every declared MAC) before the request, and refuse `machine stop|restart` with no pin and no MAC set. Refuse any power command naming the environment's controller Machine, and refuse a power request whose reported system UUID or MAC set intersects the controller host's own (read locally from DMI and interfaces). Extend controller-host protection's first sentence from 'apply or destroy' to every Bootwright effect.

Verifier notes:

- [refuter] Keep the controller-Machine refusal (refuse power commands on spec.controller.machineRef, or refuse a BMC on it at admission) and extend controller-host protection to bounded commands. Soften the unpinned rule: rather than refusing all unpinned power, require a fresh target proof (complete inventory with every declared MAC) when no pin exists, or state explicitly that unpinned power is operator-trusted and…
- [calibrator] Without a pin, require the full realization proof (complete inventory, a non-empty set of declared MACs, all reported). Refuse when the Machine declares no MACs instead of comparing nothing. Refuse power on the environment's controller Machine, and refuse when the reported UUID equals the controller host's own. Add tests for each refusal.

**Acceptance.** substrates.md Identity and power operations states that an unpinned physical Machine is proved by UUID-free full MAC proof before any power request, or refused; cli.md Machine power operations lists the controller-Machine refusal and the self-identity refusal; state-reconciliation.md Controller-host protection covers bounded commands; tests named for each refusal.

**Decision check.** D150 (batched power by list) and D85/B337 (power reads material in one keyring session) do not decide identity proof and are compatible. security.md:670-672 already says --force does not override proofs, which this makes true for unpinned Machines. No decision accepts unproved power requests.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### F10-04 · Medium · Storage retirement substitutes host stop for quiescence of a live consumer

Kind: contradiction; Area: Quiescence before removal / storage and add-on retirement; Status: confirmed; Proposed severity: High.

Files: `specs/state-reconciliation.md:1836-1840`; `specs/state-reconciliation.md:1894-1899`; `specs/storage.md:1459-1463`; `specs/storage.md:1467-1473`; `specs/add-ons.md:78-84`; `specs/add-ons.md:383-391`

**Evidence.** state-reconciliation.md:1838-1839 a retired storage effect's "quiescence derives from their stop proof". Same page 1896-1897: "data-loss acknowledges destruction; quiescence independently proves nothing is using the data." storage.md:1459-1460 target removal must "prove storage clients quiescent". add-ons.md:78-82 an add-on instance may consume a StorageExport of another cluster. add-ons.md:389-391 "a physical cluster keeps running after its context is destroyed, with its add-on objects ... which the removal's description states and its impacts omit."

**Consequence.** A context holds a Ceph cluster on libvirt Machines and a bare-metal OpenShift cluster whose Fusion Data Foundation instance consumes that Ceph's StorageExport. Destroy retires the add-on (no effect), leaves the physical OpenShift running, and retires Ceph with its VMs after the operator stops them, deleting every OSD disk. Live PVs on the surviving cluster now point at a deleted cluster; the plan's impacts never named them, and the stop proof of the storage hosts was accepted as quiescence of a consumer that is still running.

**Recommendation.** Retirement-with-hosts applies only when every consumer of the cluster's exports is removed or retired in the same removal by a block whose own target is destroyed (a virtual ContainerCluster). If a consumer survives the removal (physical cluster, external client), refuse before registration naming the consumer, or require the storage-native consumer quiescence proof of B491. List the surviving consumer as an impact.

Verifier notes:

- [refuter] Reclassify kind as gap, severity Medium. Prefer the smallest correction: when any consumer of a retired cluster's export is a package instance on a cluster that keeps running, list it as an impact naming the cluster and export (and state it in add-ons Retirement and the fdf row); an optional refusal is the owner's choice, since refusing blocks the only full-context destroy until B491.
- [calibrator] Prefer the smaller change. List each surviving consumer cluster as a named impact of the retired storage block, and make the removal description say that the consumer's PVs and Secrets point at deleted storage. Require explicit acknowledgment of that impact, for example a distinct authorization or confirmation naming the consumer, before registration. Keep the outright refusal only as the alternative if…

**Acceptance.** state-reconciliation.md Quiescence before removal and storage.md Removal and concurrency state the surviving-consumer condition and its refusal; add-ons.md Retirement says a consumed export's surviving consumer is a named impact; a planning test name for the refusal is cited.

**Decision check.** D143 clause 6 (B515) authorized retirement with hosts for 'a managed cluster whose hosts are all Bootwright-owned virtual Machines that the same destroy removes'; it did not consider cross-cluster consumers, so narrowing it keeps its intent. B491 already owns the online consumer-quiescence proof. D153 'target environment can never be broken' supports refusal.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity High; no arbitration needed.

##### F10-05 · Medium · 'Complete live MAC set matches' is equality in one rule and subset in another

Kind: contradiction; Area: Physical machine identity proof; Status: confirmed.

Files: `specs/state-reconciliation.md:1906-1908`; `specs/state-reconciliation.md:1949-1950`; `specs/substrates.md:539-545`; `specs/substrates.md:605-613`; `specs/managed-os.md:1277-1279`

**Evidence.** state-reconciliation.md:1906-1907 "prove the complete live MAC set matches the immutable Machine"; 1949-1950 "exact complete physical MAC set ... must agree". substrates.md:539-540 "requires every MAC the Machine declares to appear in it" (subset). managed-os.md:1278-1279 "every declared MAC must be present on the machine". substrates.md:612-613 "the next apply pins what its own proof finds" (UUID/serial are learned, never authored).

**Consequence.** An operator declares only the install NIC. That PCIe NIC is moved into another chassis behind the same BMC address after a re-addressing; a fresh apply's subset proof passes, the new chassis' UUID becomes the pin, and its disk is erased. Under the general rule's equality reading the extra onboard NICs would have refused. Implementers of the target proof, %pre, the erase worker and the storage host re-proof will pick different readings.

**Recommendation.** Define one predicate in Physical disk safety: the declared MAC set must equal the set of physical (non-virtual) MACs the system reports, or explicitly state subset semantics and require at least every onboard NIC to be declared. Optionally admit an authored `hardware.systemUUID` (or serial) expectation compared at every proof, so the first pin is not learned from whatever answers.

Verifier notes:

- [refuter] Reclassify as ambiguity/gap. Besides one MAC predicate, require the target proof to record the complete reported physical MAC set (or define what the erase rule's 'original complete MAC set' refers to). The authored systemUUID anchor is optional design, not required to fix the defect.
- [calibrator] Under strict equality, Redfish EthernetInterfaces may report host-interface/USB-virtual NICs, BMC-shared ports or disabled ports, so operators would have to declare ports they do not use. The defining sentence should say which reported members count, for example by excluding Redfish host-interface members, or choose subset semantics explicitly with the anchor. Keep the authored systemUUID/serial expecta…

**Acceptance.** One sentence in state-reconciliation.md Physical disk safety defines the MAC predicate; substrates.md target proof and managed-os.md %pre cite it; the erase contract uses the same word; a test name pins the extra-MAC case.

**Decision check.** D177 says the RAM worker proves 'the machine and every target' and the erase text uses 'exact complete' MAC set, supporting equality. No decision chose subset semantics.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F11-01 · Medium · Attacker model covers only the controller; BMCs, hosts and APIs are unmodelled

Kind: gap; Area: Security / trust model; Status: confirmed; Proposed severity: High.

Files: `specs/security.md:9-13, 37-49, 459-462`; `specs/state-reconciliation.md:1813-1822`

**Evidence.** specs/security.md:39-40 "On the controller host the attacker is an unprivileged local account, and any network adversary between the host and the publishers it acquires from." specs/security.md:11-13 treats "tool streams, remote facts, network responses" as untrusted. Yet specs/security.md:461-462 says "ownership, power state, and claimed absence are positively proved where required", and specs/state-reconciliation.md:1818-1820 says "requires positive absence or exact agreement between recorded and live manager, context, resource". Those proofs can only come from the remote principal itself. A grep of specs/ for compromis|malicious|attacker|adversar finds no other threat model.

**Consequence.** No rule says whether a managed host's root, a BMC, a KubeVirt or vCenter API, a registry or a cluster API server is trusted for facts about itself, or what each one receives. Attacker action: a compromised managed host or BMC reports positive absence of a Bootwright-created resource. Destroy then releases the binding, reservations and produced material (for example withdrawing the custodied kubeconfig) while the resource still exists, or apply reports done and hands off to GitOps on false readiness. Implementers will diverge on whether to treat a remote report as proof, and reviewers have no standard for which material each target may receive. The fleet key, pull secret and registration keys are each lent to remote hosts.

**Recommendation.** Add a 'Remote principals' subsection beside the controller-host model. Give one row per principal (managed host root, management controller, provider host or API, registry or mirror, cluster API, native tool output, other sudo-admitted operators) with three columns: what Bootwright trusts it to assert, what material it receives, and the stated effect of its compromise. State that self-reported absence and identity are trusted only for that principal's own resources, and that a lie can at most strand the principal's own resources and leak the material lent to it.

Verifier notes:

- [refuter] Keep it small. Add one paragraph beside the controller-host model: a remote principal reached over a verified channel (pinned host key, verified TLS, declared CA) is trusted for facts about its own resources, and its compromise is out of scope. Link the existing lent-material clauses (86-90, 418-420). A full principal table is optional.
- [calibrator] Acknowledge the existing per-principal transport and material clauses (security.md:349-417, 86-89) and link to them instead of restating. Smallest change: one short 'Remote principals' paragraph or compact table stating each remote principal is trusted only for assertions about its own resources reached over the verified channel, with the residual effect of its compromise. Drop the acceptance clause req…

**Acceptance.** security.md names each remote principal with its trusted assertions, lent material and compromise effect. Each positive-proof rule in state-reconciliation.md links to the row whose report it relies on.

**Decision check.** D152 defines only the controller-host model and does not exclude remote principals. D179 keeps pinned host keys as the transport boundary, which is consistent. D157/D163 are unaffected. No decision records remote principals as out of scope.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F11-02 · Medium · Downloaded tools lack an authenticity source; first digest pins, virtctl floats

Kind: gap; Area: Security / supply chain; Status: confirmed-by-arbitration; Proposed severity: High.

Files: `specs/security.md:558-564`; `specs/controller.md:381-386, 458-461`; `specs/contexts/controller-record.md:67-68`

**Evidence.** specs/security.md:558-564 pins "executables, and downloaded artifacts by an immutable version and content digest. Verify integrity and required publisher authenticity from separately trusted metadata" and forbids "floating tags during execution". specs/controller.md:381-384: "`virtctl` uses upstream latest unless explicitly overridden ... A tool's `latest` intent is resolved by the attempt that installs it". specs/controller.md:458-459 says only "Target archives are verified before safe extraction", against nothing named. specs/contexts/controller-record.md:67 binds a source to "its original credential-free URL, SHA-256 and exact byte count", recorded after acquisition.

**Consequence.** oc, kubectl, openshift-install, helm, govc and virtctl are executed as root against clusters, yet their only integrity anchor is TLS to whatever host the operator-supplied mirror names (downloads.openshiftClientsMirror). Attacker action: a compromised mirror, or a malicious upstream release at first acquisition, is recorded as the trusted digest and reused from then on. The 'required publisher authenticity' clause is unimplementable as written because no field, authority or step defines it.

**Recommendation.** Define per tool kind the authority each source is verified against: the publisher's signed checksum list where one exists (for example the OpenShift mirror's sha256sum.txt and its signature), otherwise an operator-declared `sha256` in dependencyVersions, as `media add --from-url` already requires. Record which authority verified the source. Where neither exists, state the trust-on-first-use widening in security.md as W5 and refuse `latest` without a declared digest, or name it as the explicit exception.

Verifier notes:

- [refuter] Drop as High contradiction. If kept, restate as Low gap: controller.md should cite environment.md:351 for target-archive verification and state per tool whether publisher checksum metadata is accepted on TLS alone or requires a publisher signature, naming any TLS-only case as an explicit widening in security.md.
- [calibrator] Acknowledge environment.md:351. Smallest fix: in controller.md, name for each target tool the publisher metadata its checksum comes from, and whether a publisher signature exists and is verified. In security.md, state that publisher authenticity for target tools means TLS to the publisher plus that publisher's checksum metadata, and record it as a named widening where no signature is verified. Do not re…

**Acceptance.** controller.md names the verification authority for each target tool. security.md lists any TOFU exception as a named widening. A test refuses a source whose digest disagrees with that authority. Arbiter acceptance: controller.md carries a tool-to-checksum-source-to-signature table; security.md lists the widening; a test refuses a digest mismatch against publisher metadata.

**Decision check.** D157 and D163 forbid version and compatibility rules, but D157 states "Independent authenticity, transport trust ... remain". The recommendation adds authenticity, not version policy, so it is compatible. D152's W2 shows the precedent of naming a widening explicitly.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### F11-03 · Medium · machine trust --yes records unconfirmed host keys; --replace takes no fingerprint

Kind: contradiction; Area: Security / SSH identity; Status: confirmed; Proposed severity: High.

Files: `specs/security.md:509-512`; `specs/cli.md:976-977, 1257-1258, 1264-1268, 1284-1296`; `specs/cli/commands.md:114`

**Evidence.** specs/security.md:509-512: "a first-use record requires an explicit interactive confirmation of the displayed fingerprint ... A host-key observation ... is never the source of a record." specs/cli.md:1257: "The confirmed (or `--yes`) write records it". specs/cli.md:1288: "The plan is the same observation the command records." specs/cli.md:1294-1295: structured formats "[require] one of them" (`--yes` or `--dry-run`). specs/cli.md:1268: re-trust "after its new fingerprint is verified out of band". `--replace <list>` takes Machine names only (commands.md:114).

**Consequence.** Attacker action: a network MITM present when a script, or JSON automation, runs `machine trust --yes` gets its key pinned as context trust with no human comparison. The same happens on `--replace` after a legitimate key change, because the out-of-band verification is never compared with anything. Every later session and lifecycle block then offers the fleet identity and lends registration and cluster material to the attacker host. The sessions' non-terminal refusal points operators to exactly this command (cli.md:976-977).

**Recommendation.** Make `--replace` take `<machine>=SHA256:<fingerprint>`, and add `--expect <machine>=SHA256:<fp>` for `add`. Under `--yes` or a structured format, refuse any add or replace whose observed fingerprint is not named. Alternatively, change security.md to state that `--yes` is an accepted TOFU exception. Do not leave the two pages contradicting each other.

Verifier notes:

- [refuter] Reframe this as a gap plus a conflict with the --yes rule at commands.md:298-306, not with security.md's session rule. Add a security.md clause on machine trust's trust model. Either name --yes enrollment as an accepted TOFU exception to commands.md:304, or require an expected fingerprint (--replace <machine>=SHA256:..., --expect for add) under --yes or structured output.
- [calibrator] Acknowledge C27. The smallest fix is to reword security.md so that machine trust, after interactive confirmation of the displayed plan or under --yes, is the named enrollment path, and --yes is an explicit operator-accepted TOFU. Then confine the 'never the source of a record' clause to the session client. Make the --replace <machine>=SHA256:<fp> or --expect pinning optional hardening, recorded as a bac…

**Acceptance.** Either security.md and cli.md agree that `--yes` is a named TOFU exception, or `machine trust --yes` refuses to record a key whose fingerprint the invocation did not name. A test covers a substituted key under `--yes`.

**Decision check.** D117 and D65 cover record replacement rows, not verification. D179 calls the pinned host key the transport boundary, which supports closing the gap. No decision accepts unconfirmed TOFU.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F11-07 · Medium · Runner sweep bound counts /var/tmp entries, so any account can block every run

Kind: unsafe-rule; Area: Security / filesystem and availability; Status: confirmed.

Files: `specs/security.md:229-231, 262-272`; `internal/reconciliation/ansiblerunner/jobs_linux_amd64.go:36-40, 674-692`

**Evidence.** specs/security.md:229-231: scratch "`bootwright-run-scratch-<n>-<m>` beneath `/var/tmp`". specs/security.md:263-264: "Every run first removes each job on the host whose lock is free". The sweep "fails closed ... when a parent holds more run directories ... than its bounds allow" (270-271). Code: `matching` counts names before any owner check, `if count++; count > maxRunDirectories` with maxRunDirectories = 256.

**Consequence.** Attacker action: the declared attacker, an unprivileged local account, creates 257 entries named `bootwright-run-scratch-1-1`, `-1-2` and so on in sticky /var/tmp. Every later lifecycle run's sweep then refuses before any job exists. Apply, destroy and continuation are denied on that controller until root removes the entries. The spec says the sweep considers only root-owned private directories (267-268), but the bound does not follow that rule.

**Recommendation.** Place scratch under a root-owned 0700 parent, for example /var/tmp/bootwright, as D98 did for the managed-OS work area. Alternatively, count toward the bound only entries proved to be root-owned private directories, and skip foreign ones.

Verifier notes:

- [refuter] Link the fix to parked B389 (move job and scratch under the root-only staging parent) and add the finding's foreign-entries acceptance test to B389's exit evidence; alternatively count only root-owned private directories toward the bound and state that in security.md:270-271.
- [calibrator] Name B389 as the carrier of the move: put scratch under a root-owned 0700 parent (the staging port), and add this DoS to B389 exit evidence. As an interim fix, or as well, count only entries that pass the owner/private check, and make security.md say the bound covers only root-owned private run directories. The acceptance test stands: more than 256 foreign-owned matching entries in the scratch parent, a…

**Acceptance.** A test creates more than 256 foreign-owned matching entries in the scratch parent, and a lifecycle run still starts.

**Decision check.** D98 moved the managed-OS work area under a root-only 0700 parent for the same class of risk. D152's attacker model includes unprivileged local accounts. Consistent.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F11-08 · Medium · A Secret part travels on argv; capability URLs are banned only with userinfo

Kind: contradiction; Area: Security / disclosure; Status: confirmed.

Files: `specs/security.md:79-82, 351-353`; `specs/secrets.md:64-66, 149-151, 163-166`; `specs/cli.md:260-263`; `specs/cli/commands.md:75`

**Evidence.** specs/security.md:79-81: "Secret values ... must never enter ... command-line arguments, environment variables". specs/secrets.md:65: usernamePassword takes "`--username` and one of `--password-file`", and username is a revealable part (secrets.md:150). specs/cli.md:261-263 treats "a Secret's `--username` or a `--from-url`" as values no output repeats. specs/security.md:353 bans only "URLs containing `userinfo`".

**Consequence.** Attacker action: the declared unprivileged local account reads `/proc/<pid>/cmdline` of the supervisor and the elevated child, and sudo records the full command in its log (COMMAND=). Either way it obtains the Secret username, and any presigned query token in `media add --from-url`. Bootwright's own output hides both, so the protection is illusory.

**Recommendation.** Either declare the username non-secret in security.md, or accept it through `--username-file`/stdin like the other parts. Define credential-bearing URLs to include query and fragment tokens, and refuse a query in `--from-url` as the mirror fields already do (environment.md:342, controller.md:705).

Verifier notes:

- [refuter] Split the finding. URL: either refuse a query in --from-url, or move the URL off argv (stdin or file) and state in security.md that a signed query is a credential. Username: state in security.md that a Secret's username part is non-secret metadata, and drop the --username placeholder in cli.md:261-263, or carry it through a file or stdin. Do not claim 'protection is illusory' for the username.
- [calibrator] Username: either declare the username a non-secret identity part in security.md (and stop treating it as hidden in cli.md:262), or add a --username-file / stdin input. Presigned URLs: do not refuse queries, because managed-os.md:38-40 intends signed URLs. Instead take the URL through a file or stdin (for example --from-url-file), or record in security.md a named exception for a query-signed media URL on…

**Acceptance.** security.md and secrets.md agree on whether a username is secret. `media add --from-url` with a query string refuses, or the spec states why it is safe.

**Decision check.** D73 and D72 cover the stdin and file rules for values and keep usernames on the flag, which is not a decided exception to the argv ban. D157/D163 are not affected.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F12-01 · Medium · Handoff predicate ignores whether the input is still the revision applied

Kind: gap; Area: GitOps readiness / status; Status: confirmed; Proposed severity: High.

Files: `specs/state-reconciliation.md:1768-1807`; `specs/project.md:46-48`; `specs/contexts.md:155-162`; `specs/cli/output.md:679`

**Evidence.** state-reconciliation.md:1771-1774 "An apply is ready for handoff exactly when: the apply operation is `done` and every block in its immutable plan is `done`"; reasons list (1799-1802) has no input reason: "no apply recorded or a destroy as the latest operation, the apply not `done`, each block not `done` ... latched log fault". project.md:46-48 "requires durable completion, readiness, access and ownership evidence for the entire selected bootstrap scope". contexts.md:160-162 "Changed input over a completed apply warns ... that apply refuses it". output.md:679 "A completed apply with a latched log fault offers the settled apply in `nextSteps`".

**Consequence.** After `context update --input-dir` with an edited input (for example, a cluster removed or a StoragePool added), `status` keeps reporting `handoff: ready` while `desired` shows a revision no operation realized. A GitOps pipeline gated on `status --output json .handoff.ready` hands over an environment that does not match the selected desired state. One implementer reads 'selected scope' as the current selection and reports not ready; another reads the predicate literally and reports ready. When a log fault is latched and the input has changed, `nextSteps` offers a settled `apply` that refuses, which breaks the rule that status offers only commands that pass, and the handoff can never become ready short of destroy.

**Recommendation.** Add one reason to the predicate and to status's ordered reasons, after 'the apply not done': `the selected input revision <rev> is not the revision <frozen> that apply <op> froze; bootwright diff --context <name> shows what changed`, making the apply not ready. Restrict the latched-log-fault next step in output.md:679 to unchanged input. Alternatively, if the owner prefers readiness to mean the frozen revision, say so in project.md:46-48 ('the scope the apply froze') and have status print the frozen revision beside `ready`.

Verifier notes:

- [refuter] Drop the output.md:679 change and the nextSteps acceptance criterion (already covered). Keep the smallest fix: either add a reason 'the selected input revision <rev> is not the revision <frozen> that apply <op> froze' to the predicate and status reasons, or state in project.md:46-48 and the predicate that readiness covers the scope the apply froze and have status show the frozen revision beside ready. Owne…
- [calibrator] Pick one reading, owner's choice; preferred smallest fix: add the input-revision reason to the predicate and status reason order (after 'the apply not done'), mirroring the settlement scope at state-reconciliation.md:38. For output.md:679 a two-word qualifier 'over unchanged input' suffices; no separate rule needed since output.md:677 governs. Acceptance as stated is checkable.

**Acceptance.** state-reconciliation.md's predicate and status reason list both name the input condition (or project.md names the frozen scope explicitly). A status golden over a completed apply followed by a changed-input context update shows the chosen readiness. nextSteps never offers `apply` over changed input.

**Decision check.** D153 (1) and (4) and D155: settlement stays records-only with no host check; this recommendation adds no host check and changes no admission, only the readiness reporting over an input already compared locally. Consistent with project.md 'edited intent is never treated as continuation'. No decision says readiness ignores the selected input.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F12-02 · Medium · Records-only diff calls an object absent once one block is removed

Kind: contradiction; Area: Desired vs real comparison; Status: confirmed.

Files: `specs/state-reconciliation.md:1616-1629`; `specs/state-reconciliation.md:1722-1727`; `specs/cli/output.md:686-703`; `specs/state-reconciliation.md:1863-1872`

**Evidence.** state-reconciliation.md:1624 "Its blocks are pending under a destroy, which took them back | absent | `destroy <op> took it back`"; 1610 reads 'the realization `status` reports'. output.md:700-702 "Otherwise any removal block proved gone makes the object `pending`". state-reconciliation.md:1726-1727 "Unknown is never absent: an observation that proved nothing never reads as an object taken back."

**Consequence.** Take a destroy paused at `Stop after online cleanup`. A Machine's pre-stop unregistration block is proved gone, but its machine block has not run, so its domain and disks still exist. `diff` without `--observe` shows the Machine `---/dev/null` 'destroy took it back', and status says `pending ... the next apply realizes it`. An operator can conclude that the VM is gone. One implementer applies the any-block rule; another reads 'its blocks are pending' as all blocks. Each gives different diff output.

**Recommendation.** In the records-only table, read absent under a removal only when every block of the object that the removal names is proved gone (or was released by an earlier attempt). When some but not all are gone, read unknown with the reason `destroy <op> has removed <blocks> and not yet <blocks>`. Name the all-blocks rule explicitly instead of deferring to status's realization vocabulary.

Verifier notes:

- [refuter] Reclassify the kind as design (truthfulness of the records-only reading), not contradiction. Drop the divergent-implementation argument. Keep the fix: under a removal, read absent only when every block of the object the removal names is proved gone or was released earlier. Read unknown, naming the removed and remaining blocks, when only some are gone. Optionally split status's 'pending' for a partly remove…
- [calibrator] Change the shared realization rule in specs/cli/output.md, not only the diff table: an object with some but not all removal blocks proved gone reads `unknown` (or a new partial value) under status, and diff follows it. Reclassify the kind as ambiguity/design. Replace the Machine example with one whose object owns several removal blocks, or confirm that the installation block belongs to the Machine. Add…

**Acceptance.** The comparison table has a 'some blocks taken back' row. A diff golden over a removal paused at the online-cleanup boundary shows the Machine as unknown, not absent.

**Decision check.** D153 (2): diff reads records and reports, never repairs; unchanged. Matches the page's own 'unknown is never absent' principle. Status realization vocabulary is outside F12's lens; changing it is optional.

Verification: refuter real, high confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F12-03 · Medium · Add-on observation leaves 'owned but changed' unclassified despite drift promises

Kind: gap; Area: Observation / observable fields; Status: confirmed.

Files: `specs/add-ons.md:369-379`; `specs/add-ons.md:426-433`; `specs/container-clusters.md:974-980`; `specs/state-reconciliation.md:1749-1766`

**Evidence.** add-ons.md:372-375 "every object own and equal with every check holding is completion; every object absent is no effect; some own objects with the rest absent or not ready is a partial". add-ons.md:432-433 "A declared field Argo CD changes is reported by `bootwright diff --observe` and never repaired." state-reconciliation.md:1765-1766 lists only `machine-libvirt-v1` rows; 1752-1753 'renders no field, so the fields shown are what apply <op> applied'.

**Consequence.** If Argo CD or an operator edits a field of an owned add-on object, the observation can classify it as partial, unknown or (if checks still hold) completion, and implementers will pick differently. Even when it reads partial, diff shows the applied fields with a note, not the changed field. The promise in add-ons.md and container-clusters.md misleads the operator acceptance gate in add-on-coverage.md:362 ('diff --observe reporting no drift'), which can pass while fields have drifted.

**Recommendation.** Classify 'own and not equal' as a positive partial realization in add-ons.md:372-375, as managed-os.md:816-824 does for network drift. Replace the two promises with 'reported as a partial realization of its block; field values are shown only for implementations listed under observable fields'. Add a Not-yet-met line naming the add-on projector item (a sibling of B600-B605), or park one.

Verifier notes:

- [refuter] Primary fix: name own-but-unequal in add-ons.md observation (and m4.md:676-682) as partial or unknown with a drift note; drop the claim of field-level values rather than adding a Not-yet-met line unless the owner wants an add-on projector (park as a B600-style candidate).
- [calibrator] Keep. Also state that the partial note for drift names the differing object and first differing field (reuse the program's comparison output, add-ons.md:232-233) so diff shows what drifted even without a projector; park the add-on projector as a B600-B605 sibling candidate rather than a Not-yet-met line if no milestone owns it.

**Acceptance.** add-ons.md's observation paragraph names the own-but-unequal case. No spec claims field-level drift reporting for an implementation absent from the observable-fields table without a Not-yet-met line.

**Decision check.** D153 (2)/(4): drift reported, never repaired, unchanged. Scenario design rule 3 keeps projection outside proof evidence; the recommendation adds no evidence widening. B600-B605 (candidate projectors) already establish the per-implementation row convention.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F13-02 · Medium · Context update and delete prompt under the root lock; busy-lock rule missing

Kind: unsafe-rule; Area: Locking and concurrency; Status: confirmed; Proposed severity: High.

Files: `specs/contexts.md:318-323`; `specs/contexts.md:329-356`; `specs/contexts.md:377-397`; `specs/contexts.md:877-892`; `specs/contexts.md:686-692`; `specs/contexts/controller-record.md:137-143`; `specs/cli.md:540-542`

**Evidence.** contexts.md:881-882 "The prompt follows admission and safeguards while locks remain held, and a refused confirmation publishes nothing." contexts.md:334 puts `context update` that imports input and `context delete` under exclusive root lock plus lease. contexts.md:318 "Two advisory locks guard the store, and neither is ever waited for"; contexts.md:349-351 "refuses with `lifecycle.lease` and a retry remedy; no lock or lease is ever waited for or taken over." contexts.md:380-382 "short root transactions refresh the registry and shared controller state before reservation, binding, secret or tool publication." Media avoids this: contexts.md:688-689 "ask for confirmation with no root lock held".

**Consequence.** Operator A starts `context update --name lab-a` and leaves the prompt open. On the same host an apply of context lab-b reaches the end of a one-hour cluster installation and needs a short exclusive root transaction to capture the produced administrator kubeconfig, seal or retire a tool area, or publish a reservation. It cannot take the lock, refuses lifecycle.lease, and no page says whether that leaves the block failed or unknown or whether the produced material was captured. Meanwhile every `status`, `plan`, `preflight` and `context list` on the host also refuses. One unattended prompt degrades every context on the host, and nothing tells the other operators which command holds the lock.

**Recommendation.** (1) Context update and delete confirm before the exclusive lock and re-prove under it, refusing with no write if the store changed, as media add/delete and D118's secret mutations already do. (2) State that a short root transaction inside a registered operation waits for the root lock with a bounded, cancellable deadline (for example 60 s), and give the outcome when that deadline passes: failed when no effect of the attempt ran, unknown otherwise, with produced material kept by the D124 rule. (3) Optionally let shared reads wait the same bounded time. (4) Have a lifecycle.lease refusal name the command that holds the lock, read from a lock-holder note.

Verifier notes:

- [refuter] Keep recommendation (1): context update/delete confirm before the exclusive lock and re-prove under it. Optionally keep (4): name the lock holder. Drop the claim that block outcomes are undefined. A bounded wait (2)-(3) is an optional improvement, not a correction, because the generic failure rules already give the outcome. Acceptance: no lock row holds the exclusive root lock across a prompt.
- [calibrator] Keep (1) as the core fix. For (2), state the block outcome when a short root transaction is refused or times out (failed if no effect ran, unknown otherwise, produced material kept), and rewrite contexts.md:318 and 350 so the 'never waited for' rule makes an explicit exception for in-operation root transactions. Move (3) and (4) into a separate parked item. Acceptance should also check that a test cover…

**Acceptance.** No lock row in contexts.md holds the exclusive root lock across a confirmation prompt. The state-reconciliation or contexts spec states the wait bound and the block outcome for a root transaction that cannot be taken during execution, and a test (for example a held-root-lock test during produced-material capture) cites it.

**Decision check.** D118 (confirm before the lease, revalidate under it) and D76 (destroy's exclusive-lock proofs follow the prompt) set the confirm-then-re-prove pattern this extends to context commands. No recorded decision requires a lock never to be waited for: the no-wait rule appears only in contexts.md, and controller.md:192 already lets the cache-only rebuild wait for setup/lock, so a bounded wait is consistent with existing practice. D124's rule on kept kubeconfigs is preserved.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### F13-03 · Medium · Format refusals name a writing build that records do not store

Kind: gap; Area: Durable formats / pre-1.0 refusal rule; Status: confirmed-by-arbitration.

Files: `specs/project.md:95-96`; `specs/contexts.md:192-205`; `specs/contexts.md:742-749`; `specs/contexts.md:302-310`; `specs/secrets.md:507-516`; `specs/contexts/controller-record.md:48-56`; `specs/state-reconciliation.md:405-412`; `specs/milestones/backlog.md:3079, 3091`

**Evidence.** project.md:95-96 "a record in an earlier format is refused, and the refusal names the remedy." contexts.md:195-197 remedy: "with the build that wrote it, destroy each environment". secrets.md:514-516 "destroy the context's effects with the Bootwright build that created it". secrets.md:510-511 corrupt store "refuses with `secret.store.corrupt` ... and the remedy is a new context." The Registry, Reservation and Manifest rows (contexts.md:304-308) and the store.json envelope (secrets.md:409-410) carry no build field. Only operation records do: state-reconciliation.md:406-408 "`build`: ... the executable's version and commit". D25: "an earlier keyring refuses and names the build that reads it."

**Consequence.** Pre-1.0, every format move strands a store or keyring whose remedy needs an earlier build, and none of these refusals can say which build. An operator who upgraded twice, or an agent asked to recover a host, has to bisect releases to find a build that reads the store; if it guesses wrong, the next build's refusal is the same. 'A new context' for a corrupt keyring leaves out that the existing context's realized objects can no longer be destroyed (their bound Secrets cannot be reopened), that the name stays reserved until a delete completes, and that the delete must be `--purge --allow-orphans` after exporting nothing. D25 promises naming the build, and the spec cannot deliver it.

**Recommendation.** Smallest: add a 'format to build' table in docs (each registry, controller record, setup state, keyring and operation record version, with the last release or commit that wrote it), and have each format refusal name the version it read and link that table. At the next format change, which is a fresh install anyway under the pre-1.0 rule, add an optional `writer` member (version and commit) to the registry, setup state and store.json envelope, and print it in the refusal. Rewrite the secret.store.corrupt remedy as D37's exits: restore the keyring from a complete backup, or `bootwright status --context <name>` then `bootwright context delete --name <name> --purge --allow-orphans`, then init.

Verifier notes:

- [refuter] If it is kept at all, keep it as a Low clarity item: have the secret.store.corrupt clause (secrets.md:510-511) link the existing exits (state-reconciliation.md lost binding; contexts.md deletion walk with --allow-orphans) and not just say 'a new context'. Drop the writer member and the format-to-build table: the format version already identifies the build range.
- [calibrator] Lead with the fix to the secret.store.corrupt remedy. Have it name D37's exits: restore the keyring from a complete backup, or run `bootwright status --context <name>` and then `bootwright context delete --name <name> --purge --allow-orphans`, then init again. It should say that the context's objects are abandoned. Acceptance: secrets.md:510-511 names those commands and is consistent with state-reconcil…

**Acceptance.** Every format refusal in contexts.md, controller-record.md and secrets.md names the version it read and a place where the operator can map it to a build. The corrupt-keyring remedy names concrete commands, the orphan exit included, consistent with D37. Arbiter acceptance: secrets.md:510-511 names the concrete commands and the abandoned-objects consequence, consistent with state-reconciliation.md:1512-1515 and contexts.md:786-796; the ordering between the before-session refusal and the reopen path is stated once.

**Decision check.** D25 (keyring refusal names the build that reads it) and D37 (lost binding exits: restore the keyring or an orphan-acknowledged delete) support this. Pre-1.0 rule (project.md:95-96) and the owner rule against converters or older readers are respected: nothing is converted, and only the remedy becomes actionable. A writer stamp waits for a format change, so it adds no extra format move.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### F13-04 · Medium · A pending setup intent blocks every launch on the host, destroy included

Kind: design; Area: Interrupted setup / controller runtime; Status: confirmed.

Files: `specs/controller.md:161-162`; `specs/controller.md:164-170`; `specs/controller.md:835-849`; `specs/controller.md:887-888`; `specs/contexts/controller-record.md:174-189`

**Evidence.** controller.md:842-845 "An unknown outcome blocks every consumer. A launch that finds the pending intent refuses `controller.state` ... with the remedy `run bootwright setup`." controller.md:838-840 "Interrupted or failed setup may leave approved packages installed". The pending intent is cleared only by "the receipt written last" (controller.md:837-838). controller.md:887-888 "The previous runtime keeps a live operation registered with the earlier lock continuable offline."

**Consequence.** On a host without access to files.pythonhosted.org, an operator runs a new build's `setup` without `--wheel-mirror`. The wheel fetch fails after the pending intent was written. Every apply, destroy, bounded run and controller stage of every context, including continuations under the previous build whose runtime is intact, now refuses 'setup was interrupted'. The remedy 'run bootwright setup' with the new build fails the same way offline. Only running the previous build's setup, which the message does not suggest, clears the state. A failure that changed nothing on the host (fetch, verification, or a dnf preview refusal) takes the whole host out of service, which undoes the offline-continuation guarantee D152 clause 8 bought.

**Recommendation.** Record the pending intent's actions separately. A setup whose failure leaves no host effect (no dnf install began, no runtime published) clears its pending intent in its failure path and records the failure in its run, so it blocks nothing. Keep the host-wide block only while a dnf install may have run, and even then let a launch whose own runtime and receipt packages still verify proceed, because dnf effects are idempotent and presence is re-read. Have the refusal name both exits: this build's `setup` (with `--wheel-mirror` when offline) or the setup of the build whose runtime the receipt names.

Verifier notes:

- [refuter] Narrowest fix: clear pending on a failure path that began no dnf install and published nothing, and add --wheel-mirror to the refusal remedy. Letting launches proceed while a dnf install may have run is a larger change that needs an owner decision.
- [calibrator] Drop the dnf-preview example from the consequence and soften 'impossible offline' to 'needs a wheel mirror or the previous build'. Smallest fix: distinguish a known failure from an interruption. A setup that fails with a named step clears its pending intent in the failure path when no dnf transaction began or the dnf step completed and presence was re-read, recording the failure in its run; the intent s…

**Acceptance.** controller.md states which setup failures leave a pending intent and which do not. A test covers a failed wheel fetch followed by a launch of the receipt's existing runtime, which succeeds. The pending-intent refusal names the alternative exit.

**Decision check.** D152 clauses 8 and 9 (previous runtime kept so live operations continue offline; the setup subtree heals itself) support narrowing the block. 'Interrupted outcomes are unknown' is respected for dnf, the only non-idempotent effect. No decision requires a failed fetch to block the host.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F13-07 · Medium · Store root may be tmpfs or overlayfs, so a reboot erases ownership evidence

Kind: unsafe-rule; Area: Durable storage; Status: confirmed.

Files: `specs/contexts.md:502-508`; `specs/contexts.md:264-269`; `specs/controller.md:306`

**Evidence.** contexts.md:506-508 "Supported local filesystems are ext4, XFS, Btrfs, tmpfs and overlayfs; Linux must provide `openat2`. Unsupported containment or durability primitives fail closed." contexts.md:267-269 "No environment variable selects another production root. Isolated test storage is injected at composition."

**Consequence.** On a live-booted or image-based bastion where /var/lib is tmpfs, or an overlay whose upper layer is tmpfs, setup and apply admit the root. A reboot removes registry.json, mutation evidence, operation records, keyrings and the produced administrator kubeconfigs, while the clusters, VMs and services they own keep running. The next apply on the empty root starts fresh and may collide with or re-create what the lost context owned. Ownership evidence is supposed to be durable, and its loss would go unnoticed.

**Recommendation.** Qualify tmpfs, and overlayfs whose upper layer is tmpfs, only for injected test roots. For the production root `/var/lib/bootwright`, refuse them with `context.state` naming the filesystem type and the remedy (a persistent local filesystem). The setup dry run's `state-root` check reports the same.

Verifier notes:

- [refuter] Keep the recommendation; optionally allow tmpfs/overlayfs-on-tmpfs only for injected test roots and add a single sentence that the production root must be persistent across reboot, reported by the setup dry run state-root check.
- [calibrator] Statfs reports only the overlay magic, not the upper layer's type, so 'overlayfs whose upper layer is tmpfs' cannot be checked reliably. Refuse tmpfs, and overlayfs too (or require proof of a persistent upper layer), for the production root. Also make delivered.md:63 and the contexts.md list agree on which filesystems are qualified. Acceptance: one test refuses a tmpfs root and one refuses an overlayfs…

**Acceptance.** contexts.md separates the filesystems qualified for the production root from those qualified for test roots. A test refuses a tmpfs production root before the first publication.

**Decision check.** No recorded decision qualifies tmpfs for production. The project principles 'continuation evidence durable' (project.md:83-85) and fail-closed effects support the refusal.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### R2-02-03 · Medium · KubeVirt ownership proof is weaker than libvirt's and leaves key rules unstated

Kind: gap; Area: Substrate KubeVirt arm / Mutation safety; Status: confirmed.

Files: `specs/substrates.md:664-667`; `specs/substrates.md:366-369`; `specs/substrates.md:408-412`; `specs/add-ons.md:193-203`; `specs/state-reconciliation.md:1816-1819`; `specs/milestones/m1.md:490-524`

**Evidence.** substrates.md:664-666 "Every object carries the context's and Machine's ownership labels, and the block records each API-returned UID; a same-named object without them, or with another UID, is foreign". libvirt substrates.md:408 "**Reservations.** `libvirt-domain:<name>`, `unit:` for the BMC unit". add-ons.md:196-199 ownership by mark when the block "holds no receipt for it" and creationTimestamp "not earlier than the operation's registration". state-reconciliation.md:1816-1818 "exact agreement between recorded and live manager, context, resource, and immutable provider identity".

**Consequence.** A VirtualMachine create whose reply was lost leaves no recorded UID; the rule neither adopts nor refuses it by a stated test, so one implementation adopts any labelled object (including one another controller's same-named context created in the shared namespace) and another refuses its own object forever. With no reservation key, two contexts on one controller targeting one namespace are coordinated only by name collision. B87's questions cover naming and namespace ownership but not this receipt rule.

**Recommendation.** State the KubeVirt ownership proof by reference to the cluster-object receipt rule: the owner label or annotation carries the operation-and-block mark of add-ons.md, a recorded UID proves ownership outright, a lost-reply object is own only when its mark matches and its creationTimestamp is not before registration; declare a reservation key per VirtualMachine (server, namespace, name) like libvirt-domain; and pin the host cluster's identity (for example the kube-system namespace UID) in the realized target, compared before every write.

Verifier notes:

- [refuter] Keep. The host-cluster identity pin is an addition beyond the core gap; host reservations are controller-local, so a reservation key alone serializes only contexts on one controller; say so. Acceptance can cite storage-resources lost-reply clauses as the pattern.
- [calibrator] Smallest change: add B87 open question 11 (lost-reply test via the add-ons mark/creationTimestamp rule, reservation key kubevirt-vm:<server>/<namespace>/<name>) and settle it in B87's definition rather than editing the KubeVirt section ahead of an authorized slice. Make the host-cluster identity pin (kube-system UID) optional/secondary: the kubeconfig already pins the server and CA; it is an improvement…

**Acceptance.** substrates.md KubeVirt section names label values, the lost-reply test, the reservation key and the host-cluster pin; B87's definition records them as closed; a fake-API test covers lost-reply adoption and a foreign same-labelled object.

**Decision check.** Checked D172/Q18 (native API route) and B87's open questions 2 and 4 (naming, provider proof): complementary, not contradicted. Reusing the add-ons receipt rule follows the one-owner-per-fact principle. The 'context name is the identity' principle is respected because the mark adds the operation identity rather than replacing the context label.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-03-01 · Medium · A removal under another build never checks the content digest

Kind: gap; Area: Reconciliation: continuation and removal across builds; Status: confirmed-by-arbitration; Proposed severity: Blocking.

Files: `specs/state-reconciliation.md:1071-1076`; `specs/state-reconciliation.md:1025-1027`; `specs/state-reconciliation.md:493-498`; `specs/state-reconciliation.md:570-588`; `specs/milestones/backlog.md:3298`; `.agents/knowledge/continuation-after-a-fix.md:65-70`; `internal/substrate/libvirt/catalog.go:68-79`

**Evidence.** state-reconciliation.md:1071-1074 'For a destroy, the proof derives the removal again from the operation's own frozen plan, which copies each block's request, implementation, content digest'. state-reconciliation.md:1025-1027 'A missing, ambiguous, unsupported, or drifted implementation refuses before the affected side effect'. state-reconciliation.md:496-498 'a removal keeps the one its apply froze, and a continuation compares it only as part of proving that another build freezes the same blocks.' backlog.md:3298 (D145(4)) 'it cannot compare content digests, because the removal copies them from the frozen plan.' continuation-after-a-fix.md:65-66 'A destroy is needed when the fix moves a version or a pin, which moves the content digest'. state-reconciliation.md:579-580 the removal's check 'proves that the object is this context's own and can be taken back, or that it is absent'. libvirt/catalog.go:71-74 HostContentDigest hashes substrate.ImagePrefix and substrate.Prefix, Go constants outside the request.

**Consequence.** A build changes a host path, name prefix or ownership mark and moves the digest. The apply continuation refuses and the documented remedy is a destroy, which runs under the build in hand: its proof copies the old digest and passes, and its removal and its resolution of unknown apply blocks use the new build's location and mark. It finds nothing at the new place, records positive absence, completes, releases reservations and the Secret binding and leaves the context pristine. The old domains, volumes or Ceph state leak, and the released host claims let another context reuse those resources. A widened mark could instead match objects this context never created.

**Recommendation.** (1) Require every value that fixes an effect's identity, place, ownership mark, claimed host resource or proof to travel in the frozen request, sourced from Go, so a removal reads the apply's values and never the build in hand's. (2) Split the content digest into an effect-identity part (marks, places, claims, proof semantics) and a mechanism part (tool and image pins). An apply continuation compares both. A removal, fresh or continued, compares the effect-identity part with this build's implementation and refuses `lifecycle.state` before resolution or registration on a mismatch, naming the registering build and the deletion exit. (3) Reword 1025-1027 and 496-498 to say what each verb compares.

Verifier notes:

- [refuter] Drop as Blocking. If kept, reduce to Low: ask the owner, when reviewing D145(4), to require that every ownership mark and place a removal checks is carried in the frozen request, and to add a contract-guard or architecture test that removal automation reads no host name, path or mark except from the request. The digest split and refusal are unnecessary.
- [calibrator] Downgrade to Medium, kind gap. Fix the consequence: libvirt already freezes names and paths, so state the risk as a constant read by removal automation outside the request (e.g. an ownership-mark literal in a role or a Ceph path) rather than the libvirt prefix. Keep only part (1) as the smallest change: one clause in Dependency safety during recovery stating that every value fixing a removal's target id…

**Acceptance.** The spec states the per-verb digest rule in Dependency safety during recovery. A lifecycle test plans a removal from a frozen plan whose block effect-identity digest differs from the offered implementation's and asserts refusal before any resolution, quiescence probe or release. The contract goldens list which values each request carries for identity and mark. No Go constant that names a host path, prefix or mark is read by removal automation except through the frozen request.

**Decision check.** D145(4), taken by the session and pending the owner's review, and X69's constraints accept this gap knowingly. The recommendation amends D145(4), so the owner must decide. D142 (no converter or older reader before 1.x) is respected: the refusal sends the operator to the registering build or the deletion exit, and carrying values in the request is not a converter. D145(3) makes builds from before X74 unable to read a worked operation, so a refusal alone could strand one. Part (1) is the request-carrying half, and it removes most mismatches before any refusal. The architecture.md:932-934 rule that specializations never change ownership proofs is unaffected.

Verification: refuter not real, high confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### R2-03-02 · Medium · Cross-build preservation of marks and proofs rests on author judgement alone

Kind: design; Area: Reconciliation: content digest semantics; Status: confirmed-by-arbitration; Proposed severity: High.

Files: `specs/state-reconciliation.md:1466-1480`; `specs/state-reconciliation.md:1084-1088`; `cmd/bootwright/contract_versions_test.go:79-86`; `cmd/bootwright/contract_versions_test.go:275-284`; `specs/milestones/delivered.md:4340-4343`; `specs/milestones/delivered.md:4806-4813`; `specs/add-ons.md:141-144`

**Evidence.** state-reconciliation.md:1466-1469 'content digest moves when its automation changes what a block's effect is ... the mark that proves them this context's own, the host resources it claims'. state-reconciliation.md:1478-1480 'what a record or mark means beyond its shape and declared version, stay the author's to move.' state-reconciliation.md:1086-1088 'a fix confined to automation, or to Go code that moves no version or pin, continues'. contract_versions_test.go:281-282 'An unquoted, dynamically built or unversioned format is outside what it sees.' delivered.md:4341-4343 'the reviewer checks that on every `ansible/` change, and removal proves ownership and refuses a foreign object as the backstop.' add-ons.md:141-142 'The content digest covers the implementation, request, evidence and program versions and the ownership and approval rule tags'.

**Consequence.** An author changes an unversioned label key or value, a dynamically built marker name, or the Go ownership check in a capability's Observe, and moves no version. The guard passes. A continuation then mixes effects that carry the old mark with effects that carry the new one, and a later removal recognises only one set. Whether a literal is classified transient, so that it is exempt from the guard, is also the author's call. Removal's ownership proof is called the backstop, but it is the very code that changed, so it detects nothing.

**Recommendation.** Make the ownership mark and the observation proof explicit, versioned contract inputs for every implementation, as add-ons already do with ownership and approval rule tags. Each implementation declares a mark format id and a proof id in Go, hashes both into its content digest and passes the mark to automation through the request. Extend the literal scan to unversioned bootwright-namespaced keys and labels. Classifying a literal as transient must carry a recorded reason in the golden. Delete the 'backstop' claim, or restate it as valid only when the removal runs with the apply's mark.

Verifier notes:

- [refuter] If kept, reframe it as a Low/Medium design improvement: optionally add a declared mark-format id to each contract golden and extend the literal scan to bootwright.io label keys. Drop the 'backstop detects nothing' claim. Instead, state that mark drift fails closed (unknown or refusal) and leaves effects stranded, which an operator must recover by hand.
- [calibrator] Smallest first step: (1) restate the backstop claim: removal's proof fails closed on a changed mark, so the consequence is a stuck or partial destroy, and no change may loosen the proof without moving the digest; (2) require a recorded reason for each transient classification in the golden; (3) extend TestEveryAutomationVersionLiteralIsClassified to quoted bootwright.io label and annotation keys. Make m…

**Acceptance.** Each contract golden has mark-format and proof-id sections, and changing either without moving the digest fails TestEveryImplementationMovesItsContentDigestWithItsContract. An unclassified bootwright.io label or key literal in the collection fails TestEveryAutomationVersionLiteralIsClassified. state-reconciliation.md:1478-1480 lists only what genuinely remains a judgement call.

**Decision check.** D145(2) replaced the human rule with a guard 'where a tool can check it' and left meaning to the author. This recommendation extends the guard in the same direction, with no conflict. The add-on precedent (B750, add-ons.md:141-142) shows the pattern already works. It agrees with architecture.md:932-934 (specializations never change ownership proofs) and with D163, because no version rule is introduced. Arbiter decision check: Consistent with D145(1)-(2), D142, D163 and architecture.md:932-934; introduces no version rule and no conflict with the add-on precedent at add-ons.md:141-142.

Verification: refuter not real, medium confidence, severity Low; calibrator real, medium confidence, severity Medium; arbiter real, severity Medium.

##### R2-04-02 · Medium · Produced-material reveal paths contradict each other on object-user S3 keys

Kind: contradiction; Area: Secrets: produced material disclosure; Status: confirmed; Proposed severity: High.

Files: `specs/storage-resources.md:1314-1322`; `specs/cli.md:1173-1182`; `specs/secrets.md:352-357`; `specs/storage.md:1424-1430`

**Evidence.** storage-resources.md:1321-1322 "No command reveals the pair yet; B489 owns access handoff." cli.md:1173-1175 "The same explicit command also reveals the selected cluster's declared StorageObjectUsers in objectUsers ... accessKey and secretKey". secrets.md:353-357 "cluster kubeconfig reveals an installation's entry ... and cluster keyring reveals one StorageClient's entry in its native keyring form".

**Consequence.** Three pages each state part of the disclosure boundary for produced material and disagree. An agent implementing from storage-resources.md removes or refuses the objectUsers reveal; one implementing from cli.md ships it; secrets.md, which claims to own disclosure (secrets.md:3-4), omits both the dashboard password and the object-user reveal. A reviewer cannot tell from the owner page which commands may print custody bytes, which is the single most security-relevant list in the custody contract.

**Recommendation.** Make secrets.md#produced-material the one owner of a closed table: produced output (block kind, output name), the only command that reveals it, its output field, and its unproved behaviour (cluster kubeconfig, cluster keyring, cluster info --secrets dashboard, cluster info --secrets objectUsers). Replace the sentences in storage-resources.md:1321-1322, storage.md:1426-1430 and cli.md with links to that table, and decide whether the objectUsers reveal is current or B489's.

Verifier notes:

- [refuter] Treat as stale-spec repair: update storage-resources.md and storage.md to link the cli.md reveal, and add the dashboard and objectUsers reveals to secrets.md#produced-material; owner choice on B489 only if the reveal was not intended. Severity Medium.
- [calibrator] Drop 'decide whether current or B489': it is current, delivered under B489's in-tree handoff (m2.md:1403-1408). Smallest fix: replace the stale sentences in storage-resources.md:1321-1322 and storage.md:1431-1437 with a link to cli.md#storage-dashboard-access, and extend secrets.md:352-357 to name the dashboard password and objectUsers reveals (a short list or table there). The docs-check rule is option…

**Acceptance.** grep for 'reveals' across specs finds each produced-material reveal listed once in secrets.md; storage-resources.md no longer says no command reveals the pair if cli.md reveals it (or cli.md marks it Not yet met under B489); a docs-check rule ties the table to the commands that read custody.

**Decision check.** D143 clause 7 authorizes the dashboard password reveal by cluster info --secrets and says client export stays B489; it does not authorize the object-user reveal, so the owner must choose. D4 (administrator access via cluster kubeconfig) is kept. No decision conflicts with consolidating the list in secrets.md.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### R2-04-03 · Medium · Plaintext native-side custody stores have no owner in the Secrets contract

Kind: gap; Area: Secrets: custody ownership; Status: confirmed.

Files: `specs/secrets.md:3-4`; `specs/secrets.md:336-345`; `specs/storage.md:1048-1063`; `specs/storage.md:1558-1566`; `specs/storage-resources.md:1207-1213`; `specs/storage-native-api.md:117-118`

**Evidence.** secrets.md:3 "Secrets owns runtime custody, materialization, generation, immutable binding and disclosure." storage.md:1050-1051 "The pair user info reports is retained in the protected custody area (a reserved root-only 0600 file and its identity record)". storage.md:1559-1560 "The source retains the ASCII token in descriptor-bound root-only 0600 custody." storage-resources.md:1207-1208 "Each CephX key is generated at the declared type and retained under client custody before its native add".

**Consequence.** The word custody names two different things: the encrypted context keyring, which secrets.md owns, withdraws on removal and deletes with the context, and per-domain plaintext 0600 files on the Ceph bootstrap host or controller, which retain keys for crash recovery. Nothing states where those files live, when they are removed (the object user 'retires with the cluster's hosts', so physical hosts keep the file after the context is destroyed or orphan-deleted), whether keyring rotation or context delete --purge covers them, or how a new storage implementation must build one. Each Ceph feature has re-specified its own, and a NetApp or Pure adapter will add a fifth.

**Recommendation.** Add to secrets.md a 'native-side retention' section owning one contract for pre-capture retention files: location under the block's reserved path, 0600/0700 and descriptor-bound, contents never in records, removal by the owning block's inverse or with the hosts, explicit statement that they are outside keyring encryption and rotation, and what an orphan-acknowledged delete leaves. Rename it (for example 'native retention') so 'custody' means only the keyring. Domain pages link to it instead of restating it.

Verifier notes:

- [refuter] The new section should cite security.md:150-151 for modes instead of restating them, and should absorb the generic sentence at storage.md:1419-1421. Include erase-receipt custody (state-reconciliation.md:1980-1982) in scope. Renaming is optional; a defined term with a link would satisfy the acceptance.
- [calibrator] Keep the new secrets.md section, but describe location per owning block (the bootstrap host or the controller) rather than requiring a single reserved path. Drop the corpus-wide rule that 'custody' means only the keyring. Instead, define 'native-side custody' (or 'native retention') once in that section and link to it from the storage pages. Acceptance: (1) the section exists. (2) The four storage passa…

**Acceptance.** secrets.md has the section; storage.md, storage-resources.md and storage-native-api.md link to it and no longer define their own retention; the word custody in specs refers only to the keyring or links to the new section; context deletion's confirmation names native retention it abandons.

**Decision check.** D25 (produced material in local-keyring-v4) and D26 (withdrawal at removal) are kept; the recommendation only gives the pre-capture files one owner. D143 clause 7 (dashboard never rotated) is unaffected. Consistent with the one-owner-per-fact principle in AGENTS.md.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-04-04 · Medium · The 'unproved' custody mark is defined for removal only but used for apply

Kind: contradiction; Area: Secrets: produced material custody; Status: confirmed.

Files: `specs/state-reconciliation.md:615-620`; `specs/secrets.md:347-350`; `specs/cli.md:1167-1169`; `specs/cli/output.md:506`; `specs/storage.md:1390-1392`

**Evidence.** state-reconciliation.md:617-620 "a capability offers them only from a proved completion, except the copy a capability's read-only observation hands over before a removal, which is published unproved". cli.md:1167-1169 "A copy that custody holds from an apply that never proved the dashboard reachable is revealed with exactly one access.unproved warning". storage.md:1391-1392 "an unreachable port reports the dashboard unproved".

**Consequence.** Two meanings share one word and one warning code: the custody record's unproved mark (an effect never proved complete, D124) and a dashboard reachability status that a completed block reports. Read literally, cli.md requires custody to hold an entry the custody rule forbids an apply to publish, or requires the warning to be derived from evidence rather than from the custody mark. Implementations will diverge on whether access.unproved is read from the store record or from the block's evidence, and an agent may add an unproved-capture path to applies.

**Recommendation.** Keep 'unproved' for the custody mark only. Rename the dashboard reachability state (for example status 'unreachable' with a reason) in cli/output.md and storage.md, and have cluster info --secrets emit a distinct warning code, or none, for a proved capture whose endpoint was not reached.

Verifier notes:

- [refuter] Narrow: keep access.unproved for custody-marked copies only; drop it (or use a distinct warning) from cluster info --secrets dashboard reveal, and state in storage.md whether an unreachable TLS proof still completes the dashboard phase. Renaming the endpoint/dashboard status enum is optional, since endpoints[].status also uses unproved consistently as an evidence status.
- [calibrator] Keep dashboard.status unproved (matches endpoints[].status). Fix only the warning: either drop access.unproved for the dashboard case (status+reason already convey it) or give it a distinct code (e.g. access.unreachable) defined in cli/output.md; reword cli.md:1167 so 'unproved' custody refers solely to the D124 mark. Acceptance: access.unproved is emitted only where the custody record carries the unpro…

**Acceptance.** grep 'unproved' in cli.md and cli/output.md finds the custody meaning only; the dashboard status enum no longer contains unproved; access.unproved's definition (cli/output.md:853) matches every use.

**Decision check.** D124 defines the unproved mark for the removal-kept kubeconfig; D143 clause 7 defines dashboard reveal. Neither defines an apply-time unproved custody entry, so the rename aligns the text with both.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-06-03 · Medium · The bmc: claim is keyed by address spelling and store-local, not by server

Kind: design; Area: Reconciliation rules / physical machine claim; Status: confirmed; Proposed severity: High.

Files: `specs/infrastructure-services.md:112`; `specs/substrates.md:584-591`; `specs/api/machines.md:280-292`; `specs/infrastructure-services.md:89-91`

**Evidence.** infrastructure-services.md:112 "named by its normalized management-controller endpoint and exact ComputerSystem, so two contexts never drive one server." substrates.md:584-586 "claims `bmc:<host>:<port>/<system>` for the normalized endpoint, so two contexts cannot both drive one physical server." api/machines.md:284-286 the host is "a lowercase DNS name ... or an IP literal", port "when present". substrates.md:536-541 the proof records UUID and SerialNumber, but no key is derived from them.

**Consequence.** Context A names a server's BMC as a DNS name, context B as its IP literal (or as `https://h/...` versus `https://h:443/...`, or through the BMC's second management NIC). The keys differ, both claims publish, both proofs pass on the same UUID and MAC set, and B's installation erases the disk of a node A's cluster runs on. Two Bootwright stores (two controllers) are never coordinated at all, which the absolute wording hides.

**Recommendation.** Define 'normalized' (default port filled per scheme, or port excluded from the key). Add a second exclusive key `bmc-system:<UUID>` that the physical block publishes in the same atomic controller record when its proof records the UUID, before any media insert, refusing `controller.conflict` when another context holds it. Reword the promise to 'two contexts of one controller store'.

Verifier notes:

- [refuter] Keep: define normalization (fill default port per scheme or drop port from the key) and add an identity key (e.g. bmc-system:<UUID>) published with the proof before media insertion. Downgrade the scope rewording to a wording fix citing the existing one-store statements. Restate the consequence as erasure of a powered-off node or a concurrent install, not a running node.
- [calibrator] Resolve the 'normalized' vs 'nothing rewrites it' conflict explicitly: derive the key with the default port per scheme filled in (key derivation only, the address stays unrewritten). The bmc-system:<UUID> key is the substantive fix but must say what happens when UUID is absent or non-unique (then no identity key, and the wording must not promise more); publishing it after the proof read is acceptable si…

**Acceptance.** The key table defines the normalization and lists the identity key; substrates.md states when the identity key is published and that the pre-boot proof re-checks it; a test proves two spellings of one BMC refuse the second context before media insertion; the wording names the one-store scope.

**Decision check.** Consistent with the physical-identity direction already in substrates.md (UUID/serial pins) and D103/D104 (verified BMC leg); adds no compatibility rule (D163). Overlaps F10-02 only in using the pin; F10-02 concerns day-2 power, not cross-context claims.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### R2-06-05 · Medium · Managed networks are defined without probing host routes, exposing the uplink

Kind: gap; Area: Reconciliation rules / controller-host protection; Status: confirmed.

Files: `specs/substrates.md:138-146`; `specs/infrastructure-services.md:190-206`; `specs/state-reconciliation.md:2042-2046`

**Evidence.** substrates.md:141-144 "no two managed attachments on one host ... declare overlapping prefixes ... because the host routes each managed prefix to its own bridge" (only Bootwright attachments compared). infrastructure-services.md:190-191 "A socket held outside the contexts this host coordinates is no reservation, so the apply proves it before its first effect." state-reconciliation.md:2042-2043 "An apply or destroy must preserve the controller OS, power, Bootwright runtime, Workspace state".

**Consequence.** A managed attachment whose prefix overlaps the controller's management subnet, a VPN route, or a pre-existing non-libvirt bridge's name is admitted and reserved. Starting the network installs a competing connected route (libvirt's own collision check covers only some IPv4 forward modes), cutting the controller's path to BMCs, SSH hosts or the operator, mid-operation and on every boot via autostart.

**Recommendation.** State a foreign-network probe parallel to the socket probe: before defining a managed network, the provider-host block reads the host's addresses, routes (both families) and link names, and refuses `lifecycle.state` naming the overlapping route or existing link and the field that chooses it; an unanswered query proves nothing and refuses.

Verifier notes:

- [refuter] Keep the probe but scope it: refuse an existing non-libvirt link of the bridge name and a host route (either family) that overlaps the managed prefix, identical or nested, before net-define; note libvirt's own IPv4 check is partial. Drop the autostart-every-boot claim for the bridge case.
- [calibrator] Correct the consequence: libvirt already refuses an IPv4 route collision and an existing bridge at net-start. The gap is IPv6 overlap, plus refusing late through a native error instead of a named refusal before any definition. Smallest fix: one sentence in substrates.md Managed networks requires the provider-host block to read the host's addresses and routes (both families) and link names before net-def…

**Acceptance.** substrates.md Managed networks states the probe, its refusal and remedy; a collection unit test refuses an overlapping route and an existing bridge of the same name before `net-define`.

**Decision check.** Consistent with the project principle that MTU and network are capability not use (memory) and D153 ('target environment can never be broken with bootwright apply'). No decision covers it.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### R2-06-06 · Medium · Libvirt providers and services on an SSH host lack cross-context coordination

Kind: design; Area: Reconciliation rules / multi-context coexistence; Status: confirmed.

Files: `specs/infrastructure-services.md:89-91, 168-181`; `specs/substrates.md:56-62`; `specs/milestones/backlog.md:54, 512-514`

**Evidence.** infrastructure-services.md:89-91 "Two contexts targeting one SSH host are not coordinated. Only the controller arm publishes the host reservations". substrates.md:61-62 "the SSH arm is coordinated by the context lease alone". backlog.md:54 B247: "publishing SSH claims needs an owner decision".

**Consequence.** Two contexts of one controller store placing libvirt providers on one remote KVM host can each claim the same bridge, overlapping prefixes and the same emulated-BMC socket; the second apply's effects collide on the host (or one context's destroy removes what the other uses, as in R2-06-01) although the store already holds both contexts' state and could refuse before registration. The planner already computes the SSH host token for intra-context comparison.

**Recommendation.** Publish SSH-placed claims in the same controller record qualified by the existing host token (`ssh/<address>:<port>/<key>`), compared by the same conflict rule; keep the statement that contexts in different stores are not coordinated. Decide B247 together with this.

Verifier notes:

- [refuter] Cite delivered.md:150-153 as the recorded constraint and frame as lifting a documented limitation; note B247 is narrower (controller-recognized SSH only), so either widen B247 or add a new parked item rather than 'decide B247 together'. Qualify every host-scoped key (bridge, libvirt-*, prefix, socket, unit, path), not only bridge.
- [calibrator] State that host qualification inherits the token's limits (one host reached by name and by address, B247) so coordination is best-effort per declared address; extend the key set to every SSH-placed claim (socket, unit, path, bridge, prefix, libvirt-network/pool, emulated BMC socket), not only bridge; specify release on destroy as for controller keys; note the controller-record schema change (pre-1.x, no…

**Acceptance.** The key table states host qualification; infrastructure-services.md:89-91 narrows to different stores; a test refuses a second context's conflicting `bridge:` on the same SSH host token.

**Decision check.** D105 keeps B247 parked pending an owner decision; this recommendation is that decision's input, not a contradiction. No other decision covers SSH coordination.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

#### S9: Spec corpus

##### F17-03 · High · Milestone scope rules are undecidable from tables: no Delivery, stale Next lines

Kind: ambiguity; Area: Milestone scope rules / agent harness; Status: confirmed.

Files: `specs/milestones.md:15, 29-32, 40-48`; `specs/milestones/m2.md:243-246, 445-446`; `specs/milestones/m3.md:455-456`; `specs/milestones/m4.md:502-503`; `specs/milestones/backlog.md:3768-3776`

**Evidence.** milestones.md:40-41 "A slice is active only while its Delivery is in progress, and only active slices count toward the WIP limit of two". m2.md:445 header "| Slice | Kind | Items in order | Owner decisions | Requires |" (same on m1.md:147, m3.md:455, m4.md:502: no Delivery). milestones.md:29-30 "Next for agents: M2c lanes and profiles, integrated at their checkpoints (D168)". backlog.md:3770 (D168) "Under that order M2c is paused". milestones.md:31 "Next for operator: review D159 to D173" while D174 to D181 exist.

**Consequence.** An agent told to "resume" cannot determine which slices are active or whether a slice is in sequence; it may resume paused M2c lanes per the Next line, or treat D159's authorization as still live, while the owner's later requests (D174, D180, D181) target M3 and M4. The WIP limit, excepted for X55-X61, X111-X127 and the M4b-M4f slices, constrains nothing.

**Recommendation.** Add a Delivery column to every Planned slices table, and one Active slices table on milestones.md (slice, authorizing decision, date, state including paused) that agents read instead of Next prose. Add `paused` to the delivery vocabulary or record M2c as not active. Replace the standing WIP exceptions with a rule the current practice satisfies (for example, a limit per program) or retire the limit.

Verifier notes:

- [refuter] Keep the slice Delivery column and the Next-line refresh. Record M2c as paused, either as a vocabulary value or in the Next line plus the M2c State, so it agrees with D168. Treat the WIP-limit change as an owner question (D158 keeps the limit), not part of the defect fix.
- [calibrator] Smallest fix: one Active slices table on milestones.md (slice, authorizing decision, date, state incl. paused) plus corrected Status/Next rows; do not also add a per-slice Delivery column to every page, which duplicates item Delivery and the active table (one owner per fact) - derive slice state from items or from the active table. Add 'paused' to the vocabulary as not active. Leave the WIP limit wordin…

**Acceptance.** Every Planned slices row carries a Delivery value; milestones.md lists active slices with their authorizing decision; the Next lines agree with the latest decision; a docs-check test fails when an Items row names a slice whose Delivery disagrees with the slice row.

**Decision check.** Checked D159 clause 2, D172 clause 5, D173 clause 6 (WIP exceptions), D168 (merge on checkpoint; M2c paused), D174, D180, D181 (owner requests for M3/M4). The recommendation records these decisions' effect rather than changing them; retiring the WIP limit needs the owner because D158 says the WIP limit remains.

Verification: refuter real, medium confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### F05-08 · Medium · Storage contract spans five to eight pages; deferred page contradicts itself

Kind: duplication; Area: Spec structure: storage API ownership; Status: confirmed; Also raised as: F15-05.

Files: `specs/api/storage.md:1-9, 256-273`; `specs/deferred/storage-api.md:1-18, 83-97, 304-337`; `specs/storage.md:40-41`; `api/v1alpha1/storage.go:74`; `specs/api/storage.md:61-68`; `specs/api/storage.md:262`; `specs/storage-resources.md:387-405`; `specs/deferred/storage-api.md:1-18`; `specs/deferred/storage-api.md:99-105`

**Evidence.** specs/api/storage.md:3-7 "This page owns storage admission. The closed schemas... own accepted fields", and it defers field records to storage-resources.md and storage-native-api.md. specs/deferred/storage-api.md:93-95 retires "Legacy `monitoring`, `mgmtGateway`... are retired and refused", yet its normalization list (:309-315) still materializes "`mgmtGateway.dnsLabel: mgr`" and "`spec.ceph.distribution: oss`". The owning page never states the `distribution` field or its `oss` default (schema api/v1alpha1/storage.go:74). specs/storage.md:40-41: "Admission of other distributions... does not select an M2 implementation." Also reported as F15-05: api/storage.md:61-63 "StorageClient is admitted with required clusterRef and access ... and exactly one same-cluster scope"; :262 restates it in the Kinds table; storage-resources.md:401-404 "a StorageClient declares exactly one of poolRef, filesystemRef and subvolumeRef. Admission refuses two or three"; deferred/storage-api.md:101-103 "exactly one same-cluster scope, poolRef, filesystemRef or subvolumeRef, declare one principal". deferred/storage-api.md:13-14 "later resource family tables stay historical".

**Consequence.** No single page gives an operator the fields, defaults and enums of one storage kind. The normalization rules that are written down include retired helpers. An operator who omits `ceph.distribution` gets `oss`, which the IBM-only lifecycle does not realize, and the owning page never says so. The same drift produced F05-02.

**Recommendation.** Give each storage kind one field-record home (storage-resources.md already holds most), with fields, defaults, enums, normalization and refusals. Reduce api/storage.md to the kinds table, the references table and cross-kind invariants. Move the live normalization list onto the owning record and delete the stale entries from the deferred page. State `ceph.distribution` (`oss|redhat|ibm`, default `oss`) and which values the current lifecycle realizes. Alternatively, make `distribution` required so that its omission does not select an unrealized arm.

Verifier notes:

- [refuter] Narrow to two fixes: remove the retired mgmtGateway.dnsLabel entry (and mark or move the live normalization entries to their owning records), and state ceph.distribution enum, default and realization status on api/storage.md. The full field-table consolidation is optional and must respect D133.
- [calibrator] Smallest fix: (1) delete the stale `mgmtGateway.dnsLabel` entry from deferred/storage-api.md normalization; (2) in api/storage.md, state `ceph.distribution` (oss|redhat|ibm, default oss) and that the current lifecycle realizes only ibm; (3) add the lifecycle.unsupported distribution refusal to storage.md's refusal table. The full one-home-per-kind consolidation is optional follow-up. Making distributi…

**Acceptance.** For every storage kind, `grep -l` finds its field table on exactly one page. The deferred page contains no normalization entry for a retired field. api/storage.md or the StorageCluster record states the `distribution` enum, its default and its realization status.

**Decision check.** Checked the one-owner-per-fact principle (AGENTS.md), D133 (owner split between the native and resource specs, which is kept) and D163 (no version rules, unaffected; this concerns distribution enum documentation, not versions). There is no conflict. Making `distribution` required is an API choice for the owner and is offered as an alternative.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F15-01 · Medium · Whole milestone pages, mostly history, dominate every slice task's reading

Kind: cost; Area: Spec corpus / agent retrieval path; Status: confirmed; Proposed severity: High; Also raised as: F14-08, F17-09.

Files: `AGENTS.md:8-10`; `specs/index.md:3-6`; `specs/milestones/m2.md:18-107`; `specs/milestones/m3.md:1-84`; `AGENTS.md:8-11`; `test/architecture/docs_test.go:20-34`; `specs/milestones/m1.md:78-110, 749-965`; `specs/milestones/m2.md:57-107, 475-489`; `specs/milestones/m3.md:1-6, 50-84`; `specs/milestones/m4.md:520-575`; `specs/milestones.md:119-124`; `AGENTS.md:9-12`

**Evidence.** AGENTS.md:8-10 mandates "its scope rules and the milestone page of the slice or item your task names"; specs/index.md:3-5 "Read the milestones status and the milestone page of the authorized slice or item". Pages are 63-141 KB (m2 141,425 B, m3 128,633 B, m4 140,582 B). m2.md:18-56 Requires paragraph is waiver history: "D134 (2026-10-11) accepted M2a's definition baseline ... Taken by the session, pending the owner's review"; m2.md:57-107 lists delivered slices: "[X71] delivered B523, which retries independent failed blocks together (D144)." Measured: m2 opening 7 KB, Submilestones 26 KB, Item detail 96 KB. Also reported as F14-08: AGENTS.md:8-9 "read its status, its scope rules and the milestone page of the slice or item your task names". Page sizes: m1.md 63 KB, m3.md 129 KB, m4.md 141 KB, m2.md 141 KB. docs_test.go budgets cover only AGENTS.md, CLAUDE.md, index, milestones.md, the code-implementation SKILL and rules. Also reported as F17-09: Sizes: m1.md 63 KB, m2.md 141 KB, m3.md 129 KB, m4.md 141 KB. m1.md:749-752 "The following records the pre-D158 scope and completion requirements." m4.md:564-567 "Routing: X136, X138, X139, X142, X145, X146, X147 ... are complex (Opus planning and review, Sonnet implementation". milestones.md:119-120 "A milestone page opens with its scope, Requires, Exit tier and Open decisions" while m3.md:3 opens "X121's produced-input lending, node diagnostics and plan bounds are delivered in-tree".

**Consequence.** A Go fix on an M3 item costs about 45k mandatory tokens of which about 76 percent is the milestone page; an API-field task about 58k tokens, 59 percent delivery state (tables in notes/F15.md). Agents either pay this on every task or skip it and miss the one clause (Requires, open decision) they needed; waiver clauses buried in prose are easy to misread as authorization.

**Recommendation.** Make each milestone page a card of at most 16 KB: scope, Requires (current rule only), exit tier, open-decision table, Planned slices, Items. Move waiver history into the decisions it came from and delivery narrative into delivered records; keep item detail as on-demand `### B<n>` anchors (or one file per open item). Change AGENTS.md and index.md to mandate "the Status row, the item's row and its ### B<n> section" rather than the page, and add the card pages to docsByteBudgets.

Verifier notes:

- [refuter] Keep it, but downgrade to Medium. Smallest fix: change AGENTS.md and index.md to name the milestone page's opening, Planned slices, the item's row and its ### B<n> section, and move delivery narrative out of the page openings per milestones.md:130-131. A card-size byte budget is optional.
- [calibrator] Smallest change first: reword AGENTS.md and specs/index.md to mandate the Status row, the page opening (scope, Requires, Exit tier, Open decisions) and the named item's row and ### B<n> section, not the whole page. Then budget only the opening section (before ## Submilestones) e.g. 8 KB per page, and move dated waiver narrative out of Requires into the D entries. Keep Submilestones on the page (D172/D17…

**Acceptance.** Each specs/milestones/m<n>.md (or its card) is within a byte budget enforced by TestDocsStayWithinByteBudgets; no Requires paragraph contains a dated waiver narrative; AGENTS.md and specs/index.md name the row and section, not the page; Task A's mandatory path re-measured below 20k tokens.

**Decision check.** Checked D128, D131, D158, D172, D173 (submilestone definitions and their pages own mappings) and D134/D143/D159/D174 waivers: the recommendation keeps every ID, submilestone and waiver, only relocating prose to the decision log the identifiers already name; milestones.md scope rules (one page per milestone, items on exactly one page) are preserved. No decision fixes the page layout. Consistent.

Verification: refuter real, medium confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### F15-08 · Medium · Docs gates bound entry files only, not the specs and milestones agents read

Kind: process; Area: Docs gates; Status: confirmed.

Files: `test/architecture/docs_test.go:20-35`; `test/architecture/docs_test.go:648-660`; `test/architecture/milestones_test.go:72-446`

**Evidence.** docs_test.go:20-22 "docsByteBudgets bounds the files an agent reads at the start of every task" and lists only AGENTS.md, CLAUDE.md, specs/index.md, specs/milestones.md, the code-implementation skill and rules. notYetMetViolations skips "specs/milestones/" and "specs/deferred/" (:656-657). No test checks index reachability or delivery IDs in normative pages.

**Consequence.** Growth moves from budgeted entry files into unbudgeted milestone and spec pages (the budget comment even directs it to 'an on-demand page'), so mandatory cost grows unobserved; drift such as F15-03 and F15-04 passes every gate.

**Recommendation.** Add: budgets for milestone cards and per-section spec budgets; index reachability for specs/*.md; a ban on delivered.md links and X ids in normative pages outside Not yet met lines; an identifier-prefix registry check; ordering and status-token checks for decisions. Keep the existing structural and spec-to-code table tests.

Verifier notes:

- [refuter] Narrow it to the smallest effective set: (1) a byte budget per milestone item card or section, or splitting milestone pages so the page a task names stays under a budget; (2) a check that specs/index.md reaches every specs/*.md. Make the delivered-link/X-id ban, the identifier-prefix registry and the decision-ordering checks optional follow-ups.
- [calibrator] Split by priority: (1) gate that enforces the existing guidance rule (no delivered.md links or X ids in specs outside milestones/deferred except Not-yet-met lines), with an allowlist ratchet for current hits; (2) index reachability for specs/*.md; (3) a byte budget for the milestone status card/section an agent must read, not whole pages. Defer per-section spec budgets, prefix registry and decision orde…

**Acceptance.** Each new rule has a fixture test that fails on a seeded violation (as TestDocsNotYetMetMatcherRejectsBadParagraphs does) and passes on main after the restructuring.

**Decision check.** Project principle 'failure injection must assert it fired' and the existing ratchet design checked; recommendation follows both. No decision forbids additional docs gates.

Verification: refuter real, high confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F17-04 · Medium · Session-taken waivers and scope readings act before the owner confirms them

Kind: process; Area: Milestone scope rules / process; Status: confirmed; Also raised as: F01-08.

Files: `specs/milestones.md:49-51`; `specs/milestones/m2.md:24-37`; `specs/milestones/m3.md:37-42`; `specs/milestones/m1.md:78-84`; `specs/milestones.md:30-33`; `specs/milestones/m3.md:36-48`; `specs/milestones/backlog.md:3870-3875, 3968-3975`

**Evidence.** milestones.md:49-51 "An unmet Requires needs a waiver, which needs an explicit user request and is recorded in the item with its date." m2.md:25-27 "Taken by the session, pending the owner's review: D134 is the dated waiver of this Requires". m3.md:37-38 "Taken by the session, pending the owner's review ([D172], Q2): M2 is waived". The phrase "pending the owner's review" occurs 38 times across milestone pages, backlog and delivered. Also reported as F01-08: specs/milestones.md:32: 'Next for operator: review D159 to D173.' m3.md:37-38: 'Taken by the session, pending the owner's review (D172, Q2)'. backlog.md:3873-3874: 'the session reads M3f as OpenShift over VMware vSphere'. backlog.md:3973-3974: '"acm/gitops" read as Red Hat Advanced Cluster Management and Red Hat OpenShift GitOps'. m2.md:28-29: D143 'the session's reading of the owner's request, pending the owner's review'.

**Consequence.** Agents cannot tell whether a pending session clause binds them; an overturned clause silently invalidates work already landed under it, and the decision log accumulates unreviewed interpretations that read like owner decisions.

**Recommendation.** Make pending session clauses inert until confirmed, or keep them in one owner review queue on milestones.md with the clause, the work it enables and its date; on confirmation, record a dated owner decision; on expiry or rejection, open the item each overturned clause reopens.

Verifier notes:

- [refuter] Drop the 'make clauses inert' option (conflicts with owner practice in D168/D174). Smallest fix: one scope rule in milestones.md stating a waiver must quote the owner's explicit request for the waived slices, that pending session clauses bind until overturned, and that each overturned clause reopens its work (generalizing D159 clause 16); optionally a single open-clauses list. Correct the count to 43.
- [calibrator] Smallest fix: add one scope rule to milestones.md defining a session clause (binding pending review, or not), that it must quote the owner request it reads, and lifting D159's reopen-on-overturn rule to general scope; keep one open-clause queue. Replace acceptance criterion 2 with: milestones.md states whether a pending clause binds and the reopen rule; criterion 2 as written applies only if the owner c…

**Acceptance.** milestones.md holds a single list of open session clauses; no milestone page applies a waiver whose only basis is a pending clause; grep of the phrase in milestone pages returns only entries of that list.

**Decision check.** Checked D134, D143, D159, D172, D173 (session clauses pending review) and D174 (owner accepted M4a and applied D173's waiver). The owner's model already says each overturned clause reopens the work it shaped (D159 clause 16), so a review queue is consistent; making clauses inert would change practice and needs the owner.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

#### S10: Skills and harness

##### F14-02 · High · Stop hook runs make quick on the wrong tree and blocks stopping on unrelated failures

Kind: design; Area: hooks; Status: confirmed.

Files: `.claude/settings.json:41-54`; `.agents/skills/code-implementation/SKILL.md:17-20`; `.agents/knowledge/inner-loop-check-cost.md:52-55`; `.agents/skills/code-implementation/references/verification.md:7-12`

**Evidence.** settings.json:47 "git diff --quiet HEAD -- 2>/dev/null && test -z \"$(git ls-files --others --exclude-standard)\" || make quick >&2". SKILL.md:17-18 "Work on a task branch in its own worktree, never in the destination checkout". inner-loop-check-cost.md:52-55 "`./...` in the primary checkout fails with `permission denied`... vet, quick-test ... fail there". verification.md:10-11 focus is the per-handoff tier; quick only for widely imported Go changes.

**Consequence.** The hook checks the session directory (normally the primary checkout), not the task worktree, so it misses the agent's changes and fires on unrelated untracked files (40-110 s every turn end). When make quick fails there, make exits 2, which Claude Code treats as a blocking Stop: the agent is forced to keep working on an unrelated failure, and the hook ignores stop_hook_active, so the loop repeats. Codex has no counterpart, so the two runtimes enforce different end-of-turn checks.

**Recommendation.** Replace the inline command with a tracked script that reads the hook input, exits 0 when stop_hook_active is true or when the cwd is the primary checkout, and otherwise runs `make focus` in the hook's cwd; return exit 2 only for a focus failure caused by tracked changes, and document the hook in code-implementation as advisory, not a gate.

Verifier notes:

- [refuter] Keep it. Simplest version: drop the Stop hook, or make it advisory (exit 0, stderr only) and have it guard on stop_hook_active. Running focus is optional. Document the hook in verification.md, or record its removal there.
- [calibrator] Simplify. Drop the hard-to-verify condition 'exit 2 only for a focus failure caused by tracked changes': run make focus and block on failure, but never when stop_hook_active is true. Define 'primary checkout' checkably: git rev-parse --git-dir equals --git-common-dir. Make the Codex parity point a note, not an acceptance criterion. Name the owning page for the hook's documentation (verification.md) once…

**Acceptance.** With an untracked file in the primary checkout, ending a turn runs no check; in a task worktree with a failing focus lane, Stop blocks once and not after stop_hook_active; the hook's command and timeout are named in verification.md.

**Decision check.** Consistent with the verification tiers (focus while iterating, make check once before integrating) recorded in verification.md and the 10-18 tier change; no D-decision covers hooks.

Verification: refuter real, high confidence, severity High; calibrator real, high confidence, severity High; no arbitration needed.

##### F14-01 · Medium · Ban on agent-run apply/destroy lives only in an optional reference and ask list

Kind: unsafe-rule; Area: agent entrypoints / effect safety; Status: confirmed; Proposed severity: High.

Files: `.agents/skills/code-implementation/references/agent-operation.md:100-102`; `.claude/settings.json:23-33`; `AGENTS.md:31-32`; `specs/controller.md:16-24`

**Evidence.** agent-operation.md:100-102 "Agents never run `bootwright apply` or `destroy`, or Ansible playbooks against hosts, outside tests; the committed Claude Code settings request approval". settings.json:27-30 ask only "Bash(./bin/bootwright apply *)" and "Bash(bootwright apply *)" (same for destroy). AGENTS.md:31-32 "Repository guidance never authorizes a push, release, or additional product scope" (no effect clause). controller.md:23-24 setup and `apply --stage controller` install host packages. .codex/ is gitignored, so no tracked Codex approval policy exists.

**Consequence.** A Codex session (no tracked approval rules) or a Claude Code session in an auto/accept mode running `bin/bootwright apply --stage ...`, an absolute-path binary, `bootwright setup`, `machine exec` or `cluster exec` is not stopped by the ask list, and the prohibition is only seen if the agent happens to load the model-routing reference; on a host with a live context this can power-cycle machines, re-image disks or install host packages.

**Recommendation.** Move one sentence into AGENTS.md: agents never run Bootwright commands with effects (apply, destroy, setup, machine/cluster exec, media add, secret reveal) or ansible-playbook against hosts without the owner's explicit request in the conversation. Widen the ask patterns to `Bash(*bootwright apply*)`, `Bash(*bootwright destroy*)`, `Bash(*bootwright setup*)`, `Bash(*bootwright machine *)`, `Bash(*bootwright cluster exec*)`, and record in the skill index that Codex needs the same rule in its own approval policy.

Verifier notes:

- [refuter] Keep the one-sentence move into AGENTS.md and widen ask patterns; drop the D138 claim from decision_check; the docs-test acceptance clause is optional rather than required.
- [calibrator] Keep the one sentence in AGENTS.md and drop the duplicate from agent-operation.md (one owner per fact). Limit the ask patterns to effectful verbs only, e.g. *bootwright apply*, *bootwright destroy*, *bootwright setup*, *bootwright machine exec*, *bootwright machine rsh*, *bootwright cluster exec*, *bootwright media add*, not 'machine *'. Say that ask rules are a backstop that bypass mode skips. Make the…

**Acceptance.** AGENTS.md carries the effect prohibition within its byte budget; agent-operation.md no longer owns it; settings.json ask rules match any path form of the binary for each effectful verb; a docs test or review checklist names each effectful command from the catalog against an ask rule.

**Decision check.** D138 states the owner does not authorize agent-run privileged or entitled tests; consistent. AGENTS.md scope rule (guidance never authorizes effects) is extended, not changed. No decision opposes it.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity High; no arbitration needed.

##### F14-03 · Medium · code-implementation defaults to a pull request, contradicting D168

Kind: contradiction; Area: code-implementation skill / integration; Status: confirmed; Proposed severity: High.

Files: `.agents/skills/code-implementation/SKILL.md:1-4, 42-52`; `.agents/skills/code-implementation/references/git-integration.md:21-31`; `specs/milestones/backlog.md:3723-3731`; `AGENTS.md:31-32`

**Evidence.** SKILL.md:47 "Integrate through a pull request gated by CI." git-integration.md:23 "**Pull request (default).** Push the task branch when the user or harness asks". backlog.md:3727-3730 D168: "the session merges a checkpoint-passed, feature-complete slice to local main as soon as it is done, without asking, and pushing still waits".

**Consequence.** An agent that follows the canonical skill either stops to request a push it is not authorized to make, or opens a PR, while the owner's decided process is a local fast-forward; only a private per-user memory carries the real rule, so Codex sessions and other accounts follow the wrong default. Recent main history (17acb63d9, 77033e8f5) shows local integration is what actually happens.

**Recommendation.** Make 'Local fast-forward (default, D168)' the first variant in git-integration.md with D168's conditions (checkpoint passed, feature complete, make check green, push waits for the owner); keep pull request as the variant used when the owner or harness asks; change SKILL.md:47 and the description's 'branch or pull-request integration' accordingly.

Verifier notes:

- [refuter] Add a 'Local main (D168)' variant: rebase, gates, ff local main for checkpoint-passed, feature-complete slices, no push; PR when the owner or harness asks. Rename 'Local, no remote' to cover remotes without push authorization. Cite D168 and its scope rather than declaring a universal default.
- [calibrator] Do not cite D168 as a general default without an owner decision that extends it. Smallest fix: replace 'Local, no remote' with 'Local fast-forward (push waits for the owner)', which covers a repo with a remote. In SKILL.md:47 and the description, stop calling PR the unconditional default and point to the variants. If the owner wants local fast-forward as the repo-wide default, record that as a new D ent…

**Acceptance.** SKILL.md and git-integration.md name local fast-forward as default and cite D168; no sentence calls PR the default; the wrapper frontmatter matches (TestDocsSkillsAreDiscoverable).

**Decision check.** D168 (2026-10-20, owner) directly supports the change; AGENTS.md 'never authorizes a push' is preserved.

Verification: refuter real, medium confidence, severity Medium; calibrator real, medium confidence, severity Medium; no arbitration needed.

##### F14-05 · Medium · Safety change class omits storage, secrets, privilege and add-ons

Kind: gap; Area: code-implementation skill / verification; Status: confirmed.

Files: `.agents/skills/code-implementation/SKILL.md:32-37`; `.agents/skills/code-implementation/references/verification.md:12`; `.agents/skills/code-implementation/references/agent-operation.md:52-56`; `specs/milestones.md:119-124`

**Evidence.** SKILL.md:35-37 "for a safety change (substrate, managed OS, container cluster, lifecycle and contextfs code or their roles), `make race` and an independent review". agent-operation.md:54-56 'complex' includes "a safety refusal, the privilege boundary, durable store transitions". milestones.md:123-124 Kind includes `safety`.

**Consequence.** A change to Ceph OSD erase or disk selection, secret custody, controller privilege, infrastructure-service host reservations or add-on cluster-object deletion integrates without the independent review and race run the skill reserves for safety, although those paths destroy data or disclose secrets; three pages define 'safety' differently.

**Recommendation.** Define a safety change once in SKILL.md as any change to a refusal, proof, privilege, secret custody, durable store or destructive effect path in any capability (storage, infrastructure services and add-ons included), or any item of Kind `safety`; reference that definition from verification.md and agent-operation.md.

Verifier notes:

- [refuter] Keep the single definition in SKILL.md, tie it to Kind safety items and to refusal/proof/privilege/custody/durable-store/destructive paths in any capability; also reconcile the make race package list (Makefile:67-68) with that definition or state that race covers only concurrent packages. Soften the 'three definitions' wording.
- [calibrator] Narrow the proposed definition. 'Any change to a refusal' would cover nearly every diff and make the gate cost meaningless. Define the class as an item of Kind `safety`, or a change to a destructive effect, a privilege boundary, secret custody or a durable store transition, in any capability, storage, infrastructure services and add-ons included. Say that `make race` runs its fixed package set, and that…

**Acceptance.** One definition exists; verification.md and agent-operation.md link it; storage, secrets and privilege paths are covered.

**Decision check.** Consistent with milestones.md scope rule that narrowing any safety refusal needs a named slice and with D2/D123-style safety decisions; no decision narrows review to the listed domains.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

##### R2-03-03 · Medium · No skill or check owns moving a content digest when automation changes

Kind: process; Area: Agent harness: procedure for automation changes; Status: confirmed; Proposed severity: High.

Files: `specs/milestones/delivered.md:4340-4343`; `.agents/skills/code-implementation/references/ansible.md:1-8`; `.agents/knowledge/continuation-after-a-fix.md:65-70`; `specs/state-reconciliation.md:1466-1471`

**Evidence.** delivered.md:4340-4342 'an automation change that alters what an effect is must move its capability's content digest; the reviewer checks that on every `ansible/` change'. ansible.md:6-8 'Before editing a managed operation, resolve its frozen request, result/evidence, implementation identity, targets/versions, permitted effects, replay' and says nothing of the digest. continuation-after-a-fix.md:69-70 'it cannot see a change of meaning under an unmoved version, so a fix that changes what an effect is must move the implementation's version.' A grep of .agents/skills, .claude/rules and .claude/skills for 'content digest' finds no procedural mention.

**Consequence.** An agent making an `ansible/` fix follows code-implementation and its Ansible reference, which never ask whether the change alters objects, names, places, marks, claims or proofs. The knowledge page is found only by symptom search after a failure. The one defence for meaning changes the guard cannot see is therefore never triggered, and AGENTS.md's split ('skills own procedure') is broken because the rule sits in delivery history.

**Recommendation.** Add one checklist item to code-implementation's Ansible reference and to its Go reference, the latter for capability Observe and ownership code: state whether the change alters an effect's objects, names, places, mark, claimed host resources or observation proof, and if it does, move the implementation version. Have the commit check require a 'Contract:' line, either 'unchanged' or the moved implementation, on any commit touching the collection's roles or plugins or a capability package. Replace the delivered.md constraint text with a link to the spec clause.

Verifier notes:

- [refuter] Keep the smallest fix: one checklist line in references/ansible.md (and go.md for capability Observe/ownership code) linking to the state-reconciliation clause, plus one line in .claude/rules/ansible.md. Make the commit-check 'Contract:' line optional/secondary (adds hook cost); do not rewrite delivered.md history, which records constraints at delivery time.
- [calibrator] Lower to Medium. Required: one checklist item in references/ansible.md, and in go.md for capability Observe and ownership code, that links to state-reconciliation.md's content-digest clause: if the change alters objects, names, places, mark, claimed resources or observation proof, move the implementation version. Optional: a 'Contract:' line in check-commits. Keep delivered.md as history and do not rewr…

**Acceptance.** The skill reference carries the item. check-commits refuses an `ansible/` commit without the Contract line. make docs-check passes, and a dry-run commit touching a role without the line is refused.

**Decision check.** AGENTS.md says skills own procedure and milestones own history. D145(2) left the meaning-level remainder to a human, so that remainder needs a procedural owner. No recorded decision conflicts, and the commit-message convention already requires a body with gates, so one more line is cheap.

Verification: refuter real, high confidence, severity Medium; calibrator real, high confidence, severity Medium; no arbitration needed.

### 11.3 Low findings

Low findings are clarity, naming, duplication and cost defects, or gaps whose consequence verification judged minor. Each still names a concrete correction; the recommendation column gives its first sentence and the files column its evidence.

#### S3: Objectives, scope and domain

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F01-03 | ambiguity | Day-2 and arbitrary-code non-goals are contradicted by specified day-2 utilities | `specs/project.md:98-105`; `specs/substrates.md:815-820`; `specs/cli/commands.md:112-113, 121-122`; `specs/state-reconciliation.md:37-40` | Rewrite the non-goal as two testable rules: (1) no command changes realized desired state outside a registered lifecycle operation over unchanged or fresh input (drift is reported, never repaired); (2) operator utilities that register no operation and publish… | R✗C✓ A✓ |
| F01-04 | contradiction | project.md states the early per-cluster handoff as settled while it is pending | `specs/project.md:50-53`; `specs/state-reconciliation.md:1787-1794`; `specs/milestones/backlog.md:4028-4033`; `specs/milestones/m4.md:170`; `specs/project.md:4… | Either record the owner's Q60 answer as a decision, or mark the sentence in project.md as pending Q60 in the same form state-reconciliation.md uses (or move it out of project.md until accepted). | R✓C✓ |
| F01-05 | gap | Product scope omits managed operating-system installation | `specs/project.md:7-11, 19-29`; `README.md:3-10`; `specs/architecture.md:52`; `specs/milestones/m1.md:3-7` | Add operating-system installation and its entitlement use to project.md's scope list (with its family envelope, e.g. | R✓C✓ |
| F01-06 | gap | No networking boundary: what Bootwright owns versus what must already exist is unstated | `specs/project.md:17-32, 81-105`; `specs/architecture.md:50`; `specs/substrates.md:126-128, 161-165` | Add one paragraph to project.md: Bootwright manages only provider-host virtual networks it declares as managed and host network configuration it installs; physical fabric, VLANs, routing, DHCP outside managed services and external DNS/firewall are provided an… | R✗C✓ A✓ |
| F01-11 | duplication | Minor drift between project.md, README and milestone openings | `README.md:3-10`; `specs/milestones/m7.md:5-6`; `specs/project.md:28-29, 77-79`; `specs/milestones/m4.md:10-11` | Replace README's purpose paragraph with one sentence plus a link to project.md (including its non-goals); fix M7's link to project.md#implementation-and-extension-model; name the package 'Red Hat OpenShift GitOps (Argo CD)' once in project.md. | R✓C✓ |

#### S4: Concept model and nomenclature

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F01-10 | naming | 'State reconciliation' names a context whose contract says there is no reconciliation | `specs/architecture.md:58`; `specs/state-reconciliation.md:1, 25`; `specs/project.md:104-105` | Rename the context and spec to 'Lifecycle operations' (or 'Operations'), keep 'reconciliation' only for the D153 scenario seam, and add 'diff' to the context's verb list. | R✓C✓ |
| F02-02 | naming | 'Implementation' names a union arm, a server product field and a registry id | `specs/api.md:228-235`; `specs/api/machines.md:20-24`; `specs/api/infrastructure-services.md:18-25`; `specs/api/infrastructure-services.md:39-45`; `specs/archi… | Reserve "implementation" for the registry entry a block freezes. | R✓C✓ |
| F02-03 | naming | The plan's unit is a block in the contract but a step in plan output | `specs/state-reconciliation.md:742-756`; `specs/cli/output.md:598-623`; `specs/cli/output.md:238-239`; `specs/architecture.md:929-936`; `specs/api/addons.md:35` | Use "block" for every plan node in presentation; reserve "step" for in-block progress sub-units and add-on `spec.steps`. | R✓C✓ |
| F02-05 | naming | The shared-service layer has five spellings; 'Infra' in InfraProvider means substrate | `specs/api/infrastructure-services.md:1-5`; `specs/state-reconciliation.md:811-818`; `specs/cli/commands.md:90`; `specs/cli/commands.md:317-318`; `specs/milest… | Adopt "infrastructure service" as the one term. | R✓C✓ |
| F02-06 | ambiguity | 'Add-on' and 'package' are defined inconsistently, and 'package' carries five meanings | `specs/add-ons.md:3-8`; `specs/add-ons.md:12-13`; `specs/api/addons.md:4-8`; `specs/cli/commands.md:64`; `specs/security.md:624-626`; `specs/architecture.md:56` | Define in the glossary: add-on (umbrella of what a ClusterAddonBinding attaches), package (built-in add-on compiled into the executable), authored add-on (ClusterAddon; never "add-on package"), instance (one package on one cluster); always qualify OLM, RPM an… | R✓C✓ |
| F02-07 | gap | The glossary defines seven terms but not the most overloaded core terms | `specs/index.md:43-60`; `specs/state-reconciliation.md:8-10`; `specs/cli/output.md:108`; `specs/controller.md:166-168` | Replace the glossary with the normative vocabulary in the F02 notes (about 40 one-line entries, each with its owner link and "allowed synonyms: none"), with qualified forms mandatory for binding, profile, capability, evidence and receipt; add a docs-check tha… | R✓C✓ |
| F02-09 | naming | No naming rule for enum values and reference fields; casings and Ref suffix vary | `specs/api.md:245-258`; `specs/api/machines.md:44`; `specs/api/machines.md:193`; `specs/api/container-clusters.md:199`; `specs/api/addons.md:41-44`; `specs/api… | Add the naming rules in the F02 notes to specs/api.md "Collections, enablement, and reserved words": lowerCamel fields and arms, `Ref`/`Refs` exactly for Bootwright object names, kebab-case Bootwright-owned enum values, native spelling for mirrored native val… | R✓C✓ |
| F02-12 | naming | 'Capability' names three unrelated concepts and its tokens use two casings | `specs/api/machines.md:182`; `specs/api/addons.md:29-48`; `specs/substrates.md:11-17`; `specs/architecture.md:50` | Always qualify: Machine capability, add-on capability, lifecycle capability (glossary entries); normalize add-on tokens to lowercase kebab (`data-foundation`, `cert-manager`). | R✓C✓ |
| F05-05 | naming | StorageExport is a per-consumer Data Foundation attachment named like an export | `specs/storage-resources.md:1124-1155`; `specs/api/storage.md:105-107, 270, 273`; `specs/api/addons.md:365-378`; `examples/add-ons/lab-compact-df/storage/expor… | Keep the decomposition: the storage side mints principals and the package consumes them by reference. | R✓C✓ |
| F12-05 | ambiguity | 'Equal', 'settled' and 'drift' have several meanings and no single definition | `specs/state-reconciliation.md:35-40`; `specs/state-reconciliation.md:1631-1645`; `specs/cli.md:510-517`; `specs/cli.md:768-772`; `specs/storage.md:513-515`; `… | Add a five-line definitions block at the head of 'Comparing desired and real state': unchanged input; same plan; equal object; drift (an owned object's observed real side differs from its applied side; diff reports it; inside an attempt each capability's refu… | R✗C✓ A✓ |
| F16-07 | naming | CLI selector and output flags use different spellings for the same role across nouns | `specs/cli/commands.md:62, 64, 110-117, 119-122` | Adopt three rules. | R✓C✓ |
| F16-08 | naming | Three vocabularies for 'next': receipt tokens name verbs the CLI does not expose | `specs/cli.md:575-580, 619-626`; `specs/cli/output.md:603-607` | Keep the receipt tokens for automation and document the token-to-command mapping in one table beside the receipt. | R✓C✓ |

#### S5: Desired-state API

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F03-04 | cost | Single-valued and always-refused fields reserve extension points needlessly | `specs/api/infrastructure-services.md:22,72-77`; `specs/api/container-clusters.md:76`; `specs/api/machines.md:44,79-81,566`; `specs/api/environment.md:478-496`… | Under D142, remove fields whose only legal value is their default (`bmcEmulationDefaults.enabled`, `.protocol`, `.emulator`, `install.method`, `baremetal.boot.method`) and make managed-service `implementation` optional with its sole value materialized in effe… | R✗C✓ A✓ |
| F03-05 | gap | No schema discoverability: no explain, export, field docs or value origins | `api/v1alpha1/schema.go:10-33`; `specs/cli/commands.md:100`; `specs/api.md:17-19,324-329` | Add a `Doc` (one sentence) and omission-meaning attribute to `Shape`/`Field`; specify `bootwright explain <Kind>[.<path>]` (fields, types, defaults, omission meaning, reference targets, accepted Secret types) and `bootwright schema --format json-schema` for y… | R✓C✓ |
| F03-06 | ambiguity | Omission, empty and enablement semantics are per field and spelled three ways | `specs/api.md:233-235,253-255`; `specs/api/input.md:72-74`; `specs/api/container-clusters.md:83,87`; `specs/api/environment.md:278-287`; `specs/api/machines.md… | Define a closed set of omission classes in api.md (absent=built-in default; absent=inherit then default; empty=clear; empty=invalid; presence=enable) and require every field table to name its class; carry the class in the Go `Field` so explain (F03-05) prints… | R✗C✓ A✓ |
| F03-07 | gap | Reference classes beyond *Ref names are uncatalogued; some are untyped | `specs/api.md:195-215`; `specs/api/storage.md:276-296`; `api/v1alpha1/storage.go:76`; `specs/api/infrastructure-services.md:86-90,156-158`; `examples/add-ons/l… | Add a reference-class table to api.md#references: object, nested (owner-scoped), endpoint selection `{serverRef, endpointRef}`, native-registry `<service_type>.<service_id>`, and external object (KubeVirt `networkRef`). | R✓C✓ |
| F03-08 | cost | Environment scope is set by three overlapping mechanisms | `specs/api/input.md:8-28,53-78`; `specs/api/environment.md:138-221`; `examples/README.md:3-6` | Decide one primary scoping mechanism: the Environment directory is the scope (drop `spec.resources`), and cluster lists remain the only narrowing, documented with an example; or keep `resources` and drop cluster lists. | R✗C✓ A✓ |
| F03-09 | duplication | Breaking changes stay in v1alpha1 with no schema identity | `specs/api.md:31-37,109-110,228-230`; `specs/api/environment.md:366-367,435-438,500-503`; `specs/api/secrets.md:26-27,54-103` | Keep v1alpha1-in-place (it fits D142) but add (1) a schema fingerprint reported by `bootwright version` and recorded with each context's frozen input, named by any admission refusal of frozen input; (2) one retired-field registry (table in api.md, data in api… | R✗C✓ A✓ |
| F04-04 | contradiction | Virtual substrates disagree on who owns a NIC's MAC and bindings | `specs/api/machines.md:121-123, 188, 334, 425-426`; `specs/deferred/machine-arms.md:87-94`; `specs/substrates.md:658-660, 708-711`; `examples/container-cluster… | State one rule for every virtual arm: the provider derives each interface MAC from (context, Machine, interface); `hardware.nics` and `interfaceBinding` are absent on virtual arms (they belong in the bare-metal arm per F04-01). | R✓C✓ |
| F04-05 | design | OS lifecycle is a boolean plus reference presence; os.install over-reaches | `specs/api/machines.md:190-195, 202-215`; `examples/container-clusters/lab-physical/infra/machines/physical-01.yaml:10-14` | Make the lifecycle an explicit union: `os: {provided: {}} \| {bootwright: {profileRef, ntp, hostKeyRef}} \| {clusterInstaller: {}}`. | R✓C✓ |
| F04-06 | design | The libvirt arm and managed services make operators author one-value mechanics | `specs/api/machines.md:44, 78-88`; `specs/api/infrastructure-services.md:22, 70-77, 166`; `examples/machines/lab-rhel/infra/providers/lab-libvirt.yaml:13-19` | Rename `bmcEmulationDefaults` to `managementEmulation` (or `bmc`). | R✓C✓ |
| F04-07 | design | External ArtifactServer has no consumer; external LoadBalancer binds nothing | `specs/api/infrastructure-services.md:152-160, 164-177` | Remove `management: external` from ArtifactServer (make it managed-only, with no `management` field) until a read-only consumer exists. | R✓C✓ |
| F04-08 | ambiguity | Proxy bypass is consumer-local and never derived, so operators repeat it | `specs/api/infrastructure-services.md:120-121, 187-210`; `specs/container-clusters.md:312-316`; `specs/api/environment.md:286-288` | Define one derived bypass set for every proxy consumer: the Environment domains, the consumed machine networks, and the selected service endpoint hosts. | R✗C✓ A✓ |
| F04-09 | duplication | The bare-metal network attachment arm configures nothing yet examples use it | `specs/api/machines.md:157, 333, 352-357`; `examples/machines/lab-baremetal/infra/providers/lab-metal.yaml:23-25`; `examples/machines/lab-baremetal/infra/machi… | Remove the bare-metal attachment arm, or document it as a label only. | R✓C✓ |
| F04-10 | naming | Managed-service image.local and image.public have no defined meaning | `specs/api/infrastructure-services.md:23-25`; `specs/infrastructure-services.md:233-235` | Replace the pair with one `image` digest reference. | R✓C✓ |
| F04-11 | naming | Object name limits differ by kind, derived from internal block-id prefixes | `specs/api/machines.md:26-30, 172-178`; `specs/api/infrastructure-services.md:27-37` | Adopt one public name limit for all kinds (for example 47 bytes, which covers the longest prefix), or derive block identities by hashing long names, so the API states a single rule. | R✓C✓ |
| F05-04 | design | ClusterAddon and ClusterAddonProfile are a second, refused add-on language | `specs/add-ons.md:64-91`; `specs/api/addons.md:25-48, 67-84, 105-112, 267-283, 365-370`; `specs/milestones/backlog.md:249`; `examples/full-platforms/multidc-pl… | Mark both kinds reserved on api/addons.md, as custom-playbooks.md does: one sentence saying the shape is held for B90 and every selecting binding refuses. | R✓C✓ |
| F05-09 | cost | CustomPlaybook reserves a fully validated shape that B102 must replace | `specs/api/custom-playbooks.md:3-38`; `specs/milestones/backlog.md:360-368`; `specs/api/addons.md:162-172` | Keep the kind name registered, but reduce the reserved contract to a single admission refusal: "CustomPlaybook is reserved for B102", with that remedy. | R✓C✓ |
| F05-10 | cost | Storage examples repeat composed facts and declare unused objects | `examples/storage-clusters/libvirt-3-nodes-ceph/storage/cluster.yaml:87-108`; `examples/storage-clusters/libvirt-3-nodes-ceph/storage/placement-policy.yaml:1-1… | In the runnable examples, omit the authored values that normalization composes (topology `fqdn`, the defaulted `pullSecretRef` and `nodeSSH`), or comment each as a deliberate override. | R✓C✓ |
| F16-01 | contradiction | The CLI catalog presents about nine stub commands with full flag contracts | `specs/cli/commands.md:90-102`; `specs/cli/commands.md:351-354`; `specs/deferred/cli-access-and-rendering.md:3-9, 77-86, 138-146`; `specs/cli/output.md:833`; `… | Add an `Availability` column (available / unavailable, with the backlog item) to each catalog table. | R✓C✓ |
| F18-06 | design | The vSphere provider arm copies OpenShift's install-config platform shape | `specs/deferred/machine-arms.md:17-41`; `specs/api/container-clusters.md:144`; `specs/substrates.md:701-734` | Before B99 lifts the refusal, keep InfraProvider.spec.vsphere to realization facts: vcenters with trustBundleRef, plus placements with datacenter, computeCluster, resourcePool, datastore, folder and networks. | R✓C✓ |
| R2-02-06 | gap | Managed Registry and LoadBalancer lack a marker though the engine refuses them | `specs/api/infrastructure-services.md:22`; `specs/api/infrastructure-services.md:33-38`; `specs/api/infrastructure-services.md:70-77`; `specs/api/infrastructur… | Add one Not-yet-met line to api/infrastructure-services.md naming B68 (Registry) and B100 (LoadBalancer, parked), with the remedy 'declare management: external'; reserve the registry-<name> and loadbalancer-<name> identities and limits in the name-limit table… | R✓C✓ |
| R2-02-07 | gap | Managed NTPServer's served time source is defined only in an Ansible template | `specs/infrastructure-services.md:512`; `specs/infrastructure-services.md:520-521`; `specs/infrastructure-services.md:546-548`; `ansible/collections/ansible_co… | State in the NTPServer row of 'Derived configuration' that the daemon serves its host's clock at local stratum 10 when no upstream source is reachable or declared, and that readiness proves an answer, not synchronization. | R✓C✓ |
| R2-05-03 | gap | External NTP, proxy and kind defaults are taught only by a refused example | `examples/full-platforms/multidc-platform/environment.yaml:21-53`; `examples/full-platforms/multidc-platform/README.md:1-10`; `cmd/bootwright/admission_accepta… | Add one runnable scenario (for example infra-components/lab-external-services, or a variant of machines/lab-rhel) with external NTPServer, DNSServer and Proxy consumed by a MachineInstallProfile and the controller Machine's proxy choice, using `spec.defaults`… | R✓C✓ |
| R2-05-05 | ambiguity | Entitlement arm set is open: unused arms on a type are admitted without meaning | `specs/api.md:237-243`; `specs/api/secrets.md:142-162`; `specs/api/secrets.md:195-211` | Make each Entitlement type's arm set closed: redhat-rhel permits only rhsm; redhat-ceph permits rhsm and registry; ibm-storage-ceph permits registry and license. | R✓C✓ |
| R2-05-07 | duplication | Secret types, parts and set flags have no table, and 'opaque' is overloaded | `specs/api/secrets.md:15-22`; `specs/api/secrets.md:54-103`; `specs/secrets.md:149-152`; `examples/container-clusters/lab-kubevirt/secret-descriptors/host-kube… | Add one 'Secret types' table to api/secrets.md: type, parts, `secret set` flags, generated support, typical consumers. | R✗C✓ A✓ |
| R2-05-08 | process | Examples index has no status column and carries stale prose | `examples/README.md:8-49`; `examples/README.md:99-103` | Add a Status column with three fixed values (admission golden; plan-tested; accepted, linking the ledger row) and a test that each row has one; delete the stale sentence. | R✓C✓ |
| R2-05-09 | design | ClusterAddonBinding diagnostic indexes refer to sorted lists, not authored positions | `specs/api/addons.md:222-226`; `specs/api.md:112-131` | State in api.md which order diagnostic indexes use for every set-valued list (preferably authored order) and make addons.md follow it. | R✗C✓ A✓ |
| R2-05-10 | naming | Several examples reuse another example's Environment name | `specs/api/environment.md:3-5`; `examples/add-ons/lab-sno-metallb/environment.yaml:1-5`; `examples/storage-clusters/libvirt-4-nodes-ceph-observability/environm… | Either drop the identity sentence from environment.md or state what consumes the name, and require each example's Environment name to equal its directory name with a test. | R✓C✓ |
| R2-05-11 | unsafe-rule | Tracked examples pre-accept the IBM license for whoever copies them | `specs/api/secrets.md:207-211`; `examples/add-ons/lab-compact-df/storage/entitlement.yaml:1-15`; `examples/README.md:59-65` | Have each Ceph scenario README's customization list name license acceptance as the operator's explicit act, linking the license terms, and keep the example value so goldens still compile; optionally make `plan` echo the accepted license per Entitlement. | R✓C✓ |

#### S6: Architecture and specialization

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F06-05 | naming | Cluster and Storage contexts are defined as their first product | `specs/architecture.md:40-58, 907-910`; `specs/storage.md:1-8, 17-41`; `specs/container-clusters.md:1-18`; `specs/api/storage.md:256-273`; `specs/architecture.… | Restate the Container cluster and Storage rows as family contexts owning provider-neutral intent, selection, ports and evidence vocabulary, with OpenShift/OKD agent install and managed Ceph as their first implementations (packages already exist: containerclus… | R✗C✓ A✓ |
| F06-06 | ambiguity | 'Abstractions only for real consumers' conflicts with low-impact extension | `specs/project.md:82-87`; `specs/architecture.md:36-38, 625-627`; `specs/secrets.md:35-50` | Reconcile in architecture.md: a family capability's first implementation must ship its seam (selector, implementation identity, consumer-owned port, generic refusal for unbound selector values) and a synthetic second implementation in tests, as Secrets does;… | R✗C✓ A✓ |
| F06-08 | design | The generic unclaimed-object refusal embeds service domain knowledge | `specs/state-reconciliation.md:909-931`; `internal/reconciliation/lifecycle/support.go:72-97` | Move 'is this object effect-bearing' to each domain root as a pure predicate the compiler composes (like admission rules), injected into the engine; the engine refuses an effect-bearing object no binding claims, without naming kinds or fields. | R✓C✓ |
| F06-11 | gap | Specialization model lacks a promotion bound and registry discovery | `specs/architecture.md:912-938`; `test/architecture/version_specializations_test.go:12-18`; `.agents/knowledge/version-specializations.md:12-15` | Require a registry marker (a declared type or file name convention) that the test discovers rather than fixed paths; add a promotion rule: a release difference that changes the request, evidence shape, block plan or more than a small fixed number of seams bec… | R✓C✓ |
| F06-12 | design | Domain selection sections share no template and enumerate unsupported arms | `specs/substrates.md:19-28, 79-92`; `specs/container-clusters.md:20-37`; `specs/infrastructure-services.md:13-47`; `specs/storage.md:364-372`; `specs/managed-o… | Mandate one template per domain: Selector, Implementations (identity, block families), Supported shape, Refusal table, Test. | R✗C✓ A✓ |
| F07-07 | design | Preflight and capacity ports are libvirt-only; cluster preflight imports storage | `cmd/bootwright/wiring_preflight.go:22-25,27-55,121-122`; `cmd/bootwright/wiring_reconciliation.go:275`; `internal/storage/preflight/contracts.go:11-25`; `inte… | Let each substrate capability package export an optional allocation projector. | R✗C✓ A✓ |
| F07-08 | naming | Role, implementation-id and plugin names follow three schemes | `specs/architecture.md:681`; `ansible/collections/ansible_collections/bootwright/core/plugins/action/substrate_machine_protocol.py:1-30`; `internal/substrate/l… | Fix one scheme. | R✓C✓ |
| F17-06 | duplication | Coverage pages are generically named but single-implementation | `specs/container-cluster-coverage.md:1-12, 168-199`; `specs/storage-coverage.md:1-7, 555-564`; `specs/add-on-coverage.md:38-48, 345-398`; `specs/project.md:19-… | Create specs/coverage/ with one file per implementation (openshift.md, ibm-ceph.md, one file per built-in package) from a common template (paths, feature matrix, profiles, left out), and state the qualification rules once in milestones.md completion rules or… | R✗C✓ A✓ |
| F18-07 | duplication | machine power commands are hard-wired to Redfish, so KubeVirt stop is unreachable | `ansible/collections/ansible_collections/bootwright/core/playbooks/machine/power.yml:1-12`; `specs/substrates.md:736-750, 962-972`; `internal/machine/power/sel… | Have the machine power playbook compose the substrate machine-port dispatcher's power and state_read entry points (F18-03), driven by the frozen realized target. | R✗C✓ A✓ |
| F18-09 | design | Managed services ignore spec.implementation and the shared role is keyed by kind | `specs/infrastructure-services.md:467-479`; `specs/api/infrastructure-services.md:19, 62-67`; `ansible/collections/ansible_collections/bootwright/core/roles/in… | Key the role's fixed table and the Go selection by the frozen implementation identity, not the kind, and have managedservice plan only objects whose spec.implementation matches its Definition, through the F18-01 claim step. | R✓C✓ |
| F18-10 | design | The DNS server pulls records from storage and cluster fields | `internal/infrastructureservices/dnsserver/catalog.go:33-60`; `specs/infrastructure-services.md:535-539`; `internal/storage/addon_facts.go:10-27` | Define a pure 'name contribution' function per owning domain (Machine, Container cluster, Storage), bound in composition, that returns records for its own objects. | R✓C✓ |
| R2-01-03 | gap | Selecting a nested cluster keeps its KubeVirt provider but not the host cluster | `specs/api/environment.md:203,214-217`; `internal/environment/selection.go:198-201`; `specs/deferred/machine-arms.md:47-55`; `internal/desiredstate/compilation… | Add to the ContainerCluster closure row: "each KubeVirt provider's hostClusterRef ContainerCluster, retained as a root with its own row". | R✓C✓ |
| R2-01-06 | design | A cluster root omitted from the list is still installed when retained, unwarned | `specs/api/environment.md:207-214`; `specs/architecture.md:45`; `specs/infrastructure-services.md:89-91`; `specs/state-reconciliation.md:25-29` | Make an attachment that reaches a cluster root omitted from an authored list a validate error that names the list to extend, instead of silently retaining a partial cluster. | R✓C✓ |
| R2-01-07 | gap | CustomPlaybook orders against non-stage anchors, against block-only dependency | `specs/deferred/custom-playbooks.md:35-42`; `api/v1alpha1/customplaybooks.go:14-15`; `specs/state-reconciliation.md:820-823`; `specs/api/custom-playbooks.md:17… | Replace `gates`/`follows` with object requirements (API kind and name, as capability requirements already are), or make the two fields unknown until B102 defines them. | R✗C✓ A✓ |
| R2-02-04 | ambiguity | Vendor-BMC variation has no specialization model and its differences sit in trust code | `specs/substrates.md:1022-1027`; `specs/substrates.md:895-913`; `specs/architecture.md:907-916`; `specs/architecture.md:931-933` | State in substrates.md 'Adapter boundary' that controller variation is capability discovery over Redfish resources, never keyed on vendor or model: list the closed set of discovered variants (media location, ETag requirement, synchronous versus task insert, c… | R✗C✓ A✓ |

#### S7: Language responsibility

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F08-02 | ambiguity | Postcondition ownership: D11 says Go; architecture and code say Python | `specs/milestones/backlog.md:3051, 3153`; `specs/architecture.md:889-894`; `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/adapte… | Amend architecture.md:889-894 to say: the plugin owns the evidence projection and may refuse a mutation whose evidence lacks its declared postcondition fields as defence in depth; Go alone decides whether evidence proves the postcondition (D11). | R✗C✓ A✓ |
| F08-03 | contradiction | 'Plugins contain no workflow' contradicts spec-mandated Python procedures | `specs/architecture.md:647-649, 752-756`; `specs/storage.md:583-612`; `specs/add-ons.md:205-233`; `ansible/collections/ansible_collections/bootwright/core/plug… | Replace the prohibition with a defined term: a block-internal procedure is a fixed, spec-owned sequence of native steps inside one frozen block (rounds, retries, batches, per-target markers, ownership checks against frozen marks) that an adapter may own; it m… | R✓C✓ |
| F08-05 | gap | Python execution sites and their interpreter floors are not spec clauses | `specs/controller.md:112-114, 155-156`; `ansible/collections/ansible_collections/bootwright/core/tests/unit/test_remote_python_floor.py:1-8, 33`; `specs/milest… | Add an 'Execution sites' table under the language section of architecture.md: controller action (locked venv), managed-host module (floor 3.9, ansible-test target list), shipped worker (target `/usr/bin/python3`, stdlib only, floor), container entrypoint (ima… | R✗C✓ A✓ |
| F08-08 | ambiguity | The Go/Ansible network criterion is split across three axes with no single rule | `specs/architecture.md:642-662`; `specs/controller.md:551-555`; `internal/controller/bundlelocal/tools_transport.go:36`; `ansible/collections/ansible_collectio… | State one rule: Go may reach only non-managed endpoints, read-only or for controller-local publication, through its egress port; every contact with a managed component and every installation of a target tool runs in an adapter; name publisher-metadata resolut… | R✓C✓ |
| F08-09 | contradiction | Architecture says the controller runner serves setup, then says setup runs no Ansible | `specs/architecture.md:771-773, 867`; `specs/controller.md:206-218` | Edit architecture.md:772 to read "for the controller stage" only. | R✓C✓ |
| F08-10 | gap | Shell and tooling run unpinned interpreters and carry unruled selection logic | `AGENTS.md:34`; `Makefile:54-60`; `scripts/quick-test:1-16`; `scripts/runtime-lock:10`; `scripts/ansible-check:12-22` | Add a shell rule to formatting.md or python.md: shell scripts only pin and exec; logic goes to `scripts/tools/*.py` run by one pinned interpreter wrapper (the ansible-check venv), which `make focus` and `scripts/runtime-lock` also use; derive docs-check's tes… | R✗C✓ A✓ |

#### S8: Reconciliation rules

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F09-05 | contradiction | Unknown-operation state rows forbid the same-continuation retry D146 requires | `specs/state-reconciliation.md:465-469`; `specs/state-reconciliation.md:557-562`; `specs/state-reconciliation.md:1088-1090`; `specs/state-reconciliation.md:500… | Change both rows to: 'continue it: resolve every unproved block first, then retry what resolution leaves failed and start what that admits (D146); start nothing while any block stays unproved', keeping the fresh-destroy alternative on the apply row. | R✓C✓ |
| F09-06 | design | Operation state is recorded from in-memory block states, not durable records | `specs/state-reconciliation.md:695-704`; `specs/state-reconciliation.md:718-727`; `specs/state-reconciliation.md:84-96`; `specs/state-reconciliation.md:1380-13… | Make the durable records the single source: after any start or resolution whose publication reports failure, the invocation re-reads that block record and records the operation from the precedence of the block records it re-read; a removal superseding a faile… | R✓C✓ |
| F09-07 | ambiguity | What an invocation admits after an attempt ends unknown is stated only by implication | `specs/state-reconciliation.md:1320-1327`; `specs/state-reconciliation.md:1223-1225`; `specs/state-reconciliation.md:684-687` | Add one sentence beside :1320: 'An attempt that ends `unknown` admits nothing further, first attempts and retries alike, and waits for what is in flight; the invocation never observes it.' | R✗C✓ A✓ |
| F09-08 | cost | Normative recovery rules are justified only by records of old executables | `specs/state-reconciliation.md:91-96`; `specs/state-reconciliation.md:1406-1417` | State the generic rule only ('an operation whose blocks are all `done` is finalized by its own verb whatever state it records') and move the commit-specific explanations to a knowledge page; drop the dedicated state-machine row if F09-06 is adopted. | R✓C✓ |
| F10-06 | design | Data-loss authorization and repeated --yes are not bound to the reviewed plan or targets | `specs/state-reconciliation.md:2066-2072`; `specs/cli/commands.md:324-331`; `specs/cli.md:256-260`; `specs/cli.md:519-525`; `specs/cli.md:252-260, 519-525`; `s… | Bind the token to the frozen plan: print a short plan digest with the consuming steps, and require `--authorize data-loss=<plan-digest-prefix>` whenever `--yes` or non-interactive input is used; refuse a digest that does not match with lifecycle.authorization. | R✓C✓ |
| F10-07 | gap | vSphere removal names no ownership re-proof or unknown-is-not-absence rule | `specs/substrates.md:715-719`; `specs/substrates.md:724-731`; `specs/substrates.md:664-667`; `specs/substrates.md:688-696`; `specs/state-reconciliation.md:1818… | State the marker (vCenter instance UUID + VM instanceUuid + context/Machine marker), require it compared immediately before power, media and destroy calls, define a mismatch as foreign (refuse) and an unreadable vCenter as unknown, never absent, mirroring the… | R✗C✓ A✓ |
| F10-08 | ambiguity | Add-on ownership fallback accepts a computable annotation as proof | `specs/add-ons.md:183-203`; `specs/state-reconciliation.md:1816-1820` | State that the mark fallback is a recovery heuristic, not an ownership proof, and record such an object as `found` with reduced trust; or key the mark with a per-operation secret held in custody (HMAC) so it cannot be computed outside Bootwright. | R✗C✓ A✓ |
| F11-04 | design | Procfs re-execution makes narrow sudoers rules ALL-equivalent | `specs/cli.md:279-284, 393-402`; `docs/operator-guide.md:199-206`; `specs/controller.md:730-735`; `specs/security.md:39-42, 327-335` | When the supervisor has proved the resolved binary and every ancestor root-owned (it already does so for its refusal), re-execute by that absolute path so that a sudoers rule naming `/usr/local/bin/bootwright` matches. | R✓C✓ |
| F11-05 | gap | No per-context authority or attribution among sudo-admitted accounts | `specs/security.md:104-105, 336-339`; `specs/state-reconciliation.md:384-390`; `specs/secrets.md:149-159` | State in the attacker model that every account the sudo rule admits is fully trusted for every context, and that 'context-scoped authority' means the selected context's keyring, not an access control. | R✓C✓ |
| F11-09 | cost | Keyring encryption names no threat it defends: keys sit beside the ciphertext | `specs/secrets.md:450-460, 518-523`; `specs/security.md:39-42` | Add one sentence that names the protected scenario, for example 'a copy of secrets/ without keys/, as a backup that excludes keys', and state that every other in-model threat is defended by root-only modes. | R✓C✓ |
| F11-10 | contradiction | Consumer publication files are 0640 root:root, a mode security.md omits | `specs/security.md:150-153`; `specs/infrastructure-services.md:406-411` | Name the served-private-tree exception in security.md beside the tool-area exception, with its rationale (the application user's group) and a requirement that no interactive account is in GID 0. | R✓C✓ |
| F11-11 | ambiguity | 'Authenticated' persisted state is undefined for unMACed records | `specs/security.md:11-13`; `specs/state-reconciliation.md:430-436`; `specs/secrets.md:450-458` | Define 'authenticated' as 'read from the verified root-only store through held handles', and state that a restored store is trusted only as far as its backup medium is. | R✗C✓ A✓ |
| F11-12 | contradiction | The proxy route to children is stated three contradictory ways | `specs/security.md:173-179, 315-318, 362-364` | Replace line 315 with a closed list of the children that receive an admitted route as request-derived environment (dnf, the image-build installer, and named adapters), all under the rule at 174-177. | R✓C✓ |
| F12-04 | ambiguity | The last-package GitOps bootstrap requirement is narrower than the early-handoff promise | `specs/add-ons.md:73-76`; `specs/add-ons.md:131`; `specs/add-ons.md:433-437`; `specs/state-reconciliation.md:820-823`; `specs/project.md:50-53` | Define 'reaches a cluster' once as a capability-declared relation, a block whose frozen request targets that cluster's API, and make the last-package requirement every other block with that relation, whatever its stage. | R✓C✓ |
| F12-06 | unsafe-rule | Edited input over a completed apply cannot be undone short of a full destroy | `specs/state-reconciliation.md:28-37`; `specs/contexts.md:155-162`; `specs/cli.md:732`; `specs/milestones/backlog.md:1844-1851` | Until B598's slice lands: (a) make the context-update warning say that restoring the original files will not restore settlement and that the only way on is `destroy` of everything the apply owns; (b) when the refusal detects bytes that 'repeat the bytes that… | R✓C✓ |
| F12-07 | ambiguity | Readiness bullets and status reasons diverge; some evidence is undefined | `specs/state-reconciliation.md:1771-1786`; `specs/state-reconciliation.md:1799-1807` | State the predicate as exactly the ordered status reason list. | R✓C✓ |
| F12-09 | contradiction | diff's Input line promises a continuation or settle that status and the verb may refuse | `specs/cli.md:732-734`; `specs/cli.md:661-671`; `specs/state-reconciliation.md:54-57` | Make the Input line's command clause conditional on the same decision status uses (cli.md:635-694). | R✓C✓ |
| F13-05 | contradiction | Runtime retention keeps one previous build; the closure refusal promises more | `specs/controller.md:810-817`; `specs/controller.md:870-890`; `specs/state-reconciliation.md:1059-1063`; `specs/state-reconciliation.md:1094-1101`; `specs/cont… | Smallest: change the refusal wording to name the registering build and say that its setup restores its runtime, fetching wheels the cache no longer holds, and list the superseding removal first when one exists. | R✓C✓ |
| F13-06 | design | A controller identity change strands every context with only an abandoning exit | `specs/controller.md:761-783`; `specs/controller.md:332-337`; `specs/contexts/controller-record.md:13-18`; `specs/contexts.md:84-109`; `specs/milestones/backlo… | (1) Make the host-mismatch refusal name every exit: the original host; otherwise a destroy run from that host before the change; otherwise the orphan-acknowledged deletion, naming what it abandons. | R✓C✓ |
| F13-08 | gap | Retained tool-source identities are append-only with a 4096 bound that nothing frees | `specs/contexts/controller-record.md:67-68`; `specs/contexts/controller-record.md:89-105`; `specs/contexts/controller-record.md:158-161`; `specs/controller.md:… | Retire a source identity in the same root transaction that retires the last area whose closure names it, unless a reserved or attributed area still names it. | R✓C✓ |
| F16-04 | duplication | status next steps duplicate each verb's decision table by hand | `specs/cli.md:606-700`; `specs/cli/output.md:677-680` | Make the principle normative and the mechanism explicit. | R✓C✓ |
| F16-05 | ambiguity | status omits secret, media and controller readiness; preflight is a stub | `specs/cli/commands.md:95, 97`; `specs/cli.md:638-642`; `specs/cli/output.md:671, 676`; `specs/secrets.md:557-560`; `examples/machines/lab-rhel/README.md:128-1… | Make `status` report three read-only local readiness rows it can compute without host contact: declared Secrets missing or stale (as `secret check` computes), declared media missing from the store, and whether the context's controller stage is complete. | R✗C✓ A✓ |
| F16-06 | design | 'Every refusal names an exact safe next action' is absent from the diagnostic schema | `specs/cli/output.md:174-201`; `specs/cli/output.md:349-361`; `specs/cli.md:1085` | Add an optional `next` member to the diagnostic object, holding the exact command string (with `--context`) when the remedy is a Bootwright command, beside prose `remediation`. | R✗C✓ A✓ |
| F16-09 | process | The lab journey reviews the full plan only after the first mutating apply | `examples/machines/lab-rhel/README.md:132-145, 172-178`; `examples/README.md:67-76`; `specs/cli.md:503-505`; `specs/contexts.md:153-163` | In the lab runbooks and examples/README, run `validate` and a full `plan` (no `--stage`) before the first `apply --stage`, with one sentence that input cannot change once any stage has run, short of destroy. | R✓C✓ |
| F16-10 | design | Sensitive reveals have three shapes; an S3 handoff dumps every user's keys | `specs/cli.md:802-811, 1154-1156, 1173-1195`; `specs/milestones/backlog.md:3296` | Keep the D143-mandated dashboard path. | R✓C✓ |
| F16-11 | design | diff always exits 0, so automation cannot detect drift or pending input without parsing | `specs/cli.md:718-724`; `specs/milestones/backlog.md:3306` | Add an opt-in `--exit-code` flag: `0` when no object differs, `3` when any object differs or is unknown, and the default unchanged. | R✓C✓ |
| R2-03-05 | design | Optional record members without a version move blur the record version | `specs/state-reconciliation.md:384-414`; `specs/state-reconciliation.md:430-436`; `specs/state-reconciliation.md:1452-1460`; `specs/milestones/backlog.md:3298` | State one record-evolution rule in Operation records. | R✓C✓ |
| R2-03-06 | ambiguity | 'Effect pin' is undefined, so content digests hash different inputs | `specs/state-reconciliation.md:1084-1088`; `.agents/knowledge/continuation-after-a-fix.md:20-24`; `internal/storage/ceph/contracts.go:83-86`; `internal/substra… | Define 'effect pin' normatively in state-reconciliation.md: every Go value that fixes a native artifact, package set, host path, name prefix or mark that an effect depends on. | R✗C✓ A✓ |
| R2-04-05 | gap | The adapter 'material values' channel and in-play handling are undefined | `specs/security.md:79-82`; `specs/security.md:107-122`; `specs/security.md:181-185`; `specs/architecture.md:1013-1020`; `ansible/collections/ansible_collection… | In security.md#sensitive-material state once: material values carry only non-secret parts (public key, certificate, fingerprint) named in a per-type part classification owned by secrets.md; every secret part is lent only as a file; a role reads a secret file… | R✓C✓ |
| R2-04-06 | gap | Installer work areas keep cluster secrets in plaintext outside custody | `specs/container-clusters.md:355-366`; `specs/container-clusters.md:733-737`; `specs/container-clusters.md:784-785`; `specs/secrets.md:328-332`; `specs/context… | State in secrets.md (or the native-retention section of R2-04-03) that native tool work areas holding substituted material are confidential retention with an owner, list them in the orphan-acknowledgement confirmation, and make the path include a per-context-… | R✓C✓ |
| R2-04-07 | gap | Secret module arguments reach AnsiballZ temp files, an unlisted hop | `specs/security.md:104-107`; `specs/security.md:260-265`; `specs/state-reconciliation.md:876-878`; `ansible/ansible.cfg:25-29`; `ansible/collections/ansible_co… | Add module-argument transport to security.md#sensitive-material: either require pipelining (payload on the connection's stdin) for every task that receives material, or state that the payload lands only in the run's private scratch and name who removes a remo… | R✓C✓ |
| R2-04-08 | gap | Produced inputs are re-read from custody each attempt, not frozen | `specs/state-reconciliation.md:655-663`; `specs/secrets.md:346-348`; `specs/secrets.md:265-271` | State in state-reconciliation.md#produced-material-custody that a producer's recapture within the life of its effect must present the same identity, and that a consumer re-proves the identity (kubeconfig authority, CephX principal) it recorded at its first at… | R✗C✓ A✓ |
| R2-06-02 | contradiction | Abandoning deletion releases host keys while abandoned objects hold them | `specs/contexts.md:832-839`; `specs/state-reconciliation.md:262-274`; `specs/infrastructure-services.md:138-148`; `specs/substrates.md:584-591, 612-614`; `spec… | Keep D38's purpose (no key stranded with no context to release it) but change the mechanism: an abandoning deletion converts the context's exclusive entries into a host-level tombstone holder `abandoned/<name>` in the same atomic controller record. | R✗C✓ A✓ |
| R2-06-04 | gap | An emulated BMC claims no bmc: key, so two contexts can drive one VM | `specs/substrates.md:408-412, 584-591`; `examples/README.md:12-13`; `examples/machines/lab-baremetal/README.md:264-276` | Have the libvirt machine block publish `bmc:<bindAddress>:<port>/<uuid>` as a shared key, and state that a removal releasing a shared `bmc:` whose endpoint another context holds exclusively refuses `controller.conflict` naming that context; a context's own ma… | R✓C✓ |
| R2-06-07 | contradiction | Reservation release paths and the conflict relation are stated differently on four pages | `specs/infrastructure-services.md:138-160`; `specs/contexts/controller-record.md:75-85`; `specs/contexts.md:832-839`; `specs/state-reconciliation.md:128-140` | Add one 'Release' table to infrastructure-services.md#host-reservations listing every release path (completed destroy finalization, settling destroy over an interrupted registration, own fresh apply replacement, permanent deletion, abandoning deletion) with i… | R✓C✓ |
| R2-06-08 | gap | The 256-entry host reservation bound has no refusal, remedy or share | `specs/contexts/controller-record.md:89-91`; `specs/infrastructure-services.md:119-136` | State the refusal: an apply whose entries would pass 256 refuses before registration with `controller.conflict`, naming the bound, the count it needs and the contexts holding the most entries, with `bootwright destroy --context <name>` as remedy; or move the… | R✓C✓ |

#### S9: Spec corpus

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F07-10 | ambiguity | Spec navigation cites a test-only function as the capability registry | `specs/architecture.md:391-392,416-418`; `cmd/bootwright/wiring_reconciliation.go:94-96,196-199`; `cmd/bootwright/playbook_bindings_test.go:17` | Cite `buildCapabilitiesWith` (or rename the production function back to `buildCapabilities`), and reword the comment to 'this static list is the allowlisted registry; there is no discovery or runtime plugin path'. | R✓C✓ |
| F13-09 | duplication | Setup-subtree rules and the version-5 cut-over are restated on five pages | `specs/controller.md:825-913`; `specs/contexts/controller-record.md:163-227`; `specs/contexts.md:192-205`; `specs/contexts.md:283-291`; `specs/contexts/control… | Make controller-record.md the single owner of the setup subtree's layout, publication, retention and refusals, and have controller.md link to it. | R✓C✓ |
| F15-02 | ambiguity | Six-host Ceph cap is written as a target rule, contradicting D130 | `specs/storage.md:562-572`; `specs/milestones/m2.md:10-12`; `specs/milestones/backlog.md:3199-3205`; `specs/index.md:34-37` | State the target (every size D130 admits, under the bounded execution budget formula) and keep the six-host refusal as the current behaviour behind "Not yet met: clusters above six hosts refuse; tracked as [B79](milestones/m2.md#b79)". | R✗C✓ A✓ |
| F15-03 | contradiction | Stale, unindexed M2a storage API review page sits among normative specs | `specs/storage-api-review.md:1-20`; `specs/storage-api-review.md:225-229`; `specs/api/storage.md:73-93`; `specs/index.md:25`; `specs/storage-api-review.md:1-20… | Move the review to specs/milestones/ history (as the M2a definition record D133 approved) or delete it, retargeting its inbound links to the owning api/storage.md and storage-resources.md sections; add a gate that every specs/*.md outside milestones/ and defe… | R✓C✓ |
| F15-04 | process | Not yet met lines carry acceptance and delivery state outside the convention's purpose | `specs/index.md:34-38`; `specs/container-cluster-coverage.md:14-15`; `specs/container-cluster-coverage.md:201-202`; `specs/add-on-coverage.md:400-401`; `specs/… | Restrict Not yet met to code-unmet rules; move acceptance/baseline state to the milestone pages and ledger; let coverage rows name their item in one column. | R✓C✓ |
| F15-06 | ambiguity | Q, OD, P and L identifier schemes are used hundreds of times but undefined | `specs/milestones.md:98-131`; `specs/milestones/m2.md:302-308`; `specs/milestones/m3.md:148-152`; `specs/milestones/m4.md:132-135` | Define Q and OD (open questions, with one allocation rule across pages) and P/L where used, keeping existing numbers, and give every open-decision table one header with Status and Answered-by (D<n>); extend milestoneCheck to validate that header and that ever… | R✓C✓ |
| F15-07 | cost | Index routes to whole pages while owning sections reach 40-101 KB | `specs/index.md:8-30`; `specs/managed-os.md:111-600`; `specs/state-reconciliation.md:729-1584` | Split oversized sections into leaf subsections (target at most 16 KB per ## and 8 KB per leaf), add section-level index rows for the six largest domains, and enforce per-section budgets in the docs gate with a ratchet like docsByteBudgets. | R✓C✓ |
| F15-09 | duplication | Milestone item details restate spec rules, creating a second owner | `specs/milestones/m2.md:1798-1840`; `specs/storage.md:562-600` | Item detail holds the outcome, a link to the owning section, exit evidence and deferral reason only (as milestones.md:119-124 already prescribes); move design narrative to the delivered record when the item completes. | R✓C✓ |
| F15-10 | cost | The decision log is a 132 KB tail of backlog.md mixing pending clauses | `specs/milestones/backlog.md:3024-4100`; `specs/milestones.md:114-115` | Move decisions to specs/milestones/decisions.md with one line per decision (date, status token owner\|session-pending\|superseded-by D<n>, one-sentence rule) and detail below; gate numeric order and status tokens. | R✓C✓ |
| F16-03 | gap | The CLI help content contract exists only in delivered history, not in the CLI spec | `specs/cli/commands.md:128, 166-192`; `specs/cli/output.md:228`; `specs/milestones/delivered.md:3807-3819` | Add a `Help content` subsection to specs/cli/commands.md. | R✓C✓ |
| F17-05 | duplication | Ceph host-prep and adoption safety rules are restated on two lifecycle pages | `specs/storage-coverage.md:75-148`; `specs/storage.md:794-799`; `specs/storage-resources.md:361-366`; `specs/index.md:25` | Move storage-coverage.md:75-148 into storage.md (host preparation, adoption) and the resource-specific clauses into storage-resources.md, leaving the coverage page with release evidence, the matrix, profiles and the unsupported-combination table, and a link. | R✓C✓ |
| F17-07 | gap | The acceptance ledger has no row template for container-cluster or add-on operator gates | `docs/acceptance.md:9-38, 40, 59, 81`; `specs/container-cluster-coverage.md:175-180`; `specs/add-on-coverage.md:357-363`; `specs/milestones/backlog.md:3688-369… | Add "Container cluster row" and "Add-on row" templates to docs/acceptance.md naming which required fields appear publicly (release minor, profile, drills passed) and which are retained locally, and link them from both coverage pages' Rows rule. | R✗C✓ A✓ |
| F17-08 | design | Dated per-release native facts and pairings live in specs instead of knowledge | `specs/add-on-coverage.md:145-149, 195-203, 230-233, 326-329`; `specs/container-cluster-coverage.md:50-54`; `specs/storage-coverage.md:245-262, 411-414` | Move every "Release evidence" and "Evidence by release" paragraph and the dated catalog observations into the per-component knowledge pages (version-specializations or package pages) and leave in the spec only the rule that the operator chooses versions and t… | R✓C✓ |
| R2-02-05 | process | No substrate or service coverage page; qualification lives in milestone rows | `specs/index.md:24-28`; `specs/milestones/m1.md:268-272`; `specs/state-reconciliation.md:1926-1928`; `specs/project.md:21-22` | Add specs/substrate-coverage.md (or a section in substrates.md) on the add-on-coverage pattern: one row per arm and per qualified controller family or firmware line, with its item, the operator gate and the accepted ledger row as evidence only, sanitized of e… | R✓C✓ |

#### S10: Skills and harness

| Id | Kind | Title | Files | Recommendation (first sentence) | Votes |
| --- | --- | --- | --- | --- | --- |
| F14-04 | design | The local-only override replaces AGENTS.md in Codex, primary checkout only | `AGENTS.md:1-42`; `CLAUDE.md:1`; `.agents/skills/index.md:5-8`; `.agents/skills/code-implementation/references/git-integration.md:8-11` | Keep personal overrides out of the replacement slot: move them to the user-level files of each runtime (~/.codex/AGENTS.md, ~/.claude/CLAUDE.md or CLAUDE.local.md), or require the local-only file to begin with the tracked AGENTS.md text; document the per-runt… | R✓C✓ |
| F14-06 | cost | Integration gate rule differs from delivered practice and change classes | `.agents/skills/code-implementation/SKILL.md:2-4, 32-39`; `.agents/skills/code-implementation/references/verification.md:58-62`; `specs/milestones/delivered.md… | Either keep make check mandatory and add `make focus`, `make check` and `make race` to the allow list, or add a short change-class gate table to verification.md (docs-only, Go-only, Ansible, safety) stating which gates each may omit and why, and have delivere… | R✗C✓ A✓ |
| F14-07 | gap | No checklist for contract-version, API and commit-check work | `specs/state-reconciliation.md:1466-1480`; `.agents/skills/architecture-best-practices/SKILL.md:36-37`; `.agents/skills/ux-and-apis/SKILL.md:28-37`; `.agents/s… | Add one references/change-classes.md (under 3 KB) with a five-line checklist per class: spec, Go, Ansible, durable format/contract version, API kind or field, example, knowledge page, milestone bookkeeping; each line names the owning spec anchor, guard tests… | R✗C✓ A✓ |
| F14-09 | design | Vendor- and model-specific routing tables live in a tracked skill reference | `.agents/skills/code-implementation/references/agent-operation.md:34-91`; `.agents/skills/code-implementation/SKILL.md:49-50` | Keep in the repository only vendor-neutral rules (acceptance before routing, explicit model/effort per worker, a stronger independent review for safety-class work, unattended-run checklist); move model names, tiers and OpenAI candidates to user-level configur… | R✓C✓ |
| F14-11 | duplication | Verification tiers are restated in about seven places with conflicting labels | `docs/development.md:85-86`; `AGENTS.md:36-42`; `.agents/skills/code-implementation/references/verification.md:7-12`; `specs/architecture.md:1145-1147` | Let verification.md own tier names and when each runs; correct development.md's labels and reduce its table rows to command descriptions linking the tiers; keep AGENTS.md's command list as names only. | R✓C✓ |
| F14-12 | process | The wrapper parity test ignores a missing or orphan wrapper | `test/architecture/docs_test.go:451-475`; `.agents/skills/index.md:5-8`; `.claude/skills/code-implementation/SKILL.md:6-7` | Fail when a canonical skill lacks a wrapper or a wrapper lacks a canonical skill; optionally make each wrapper body a relative symlink-free one-line pointer as now, or test whether a symlinked .claude/skills directory is discovered and drop the wrappers. | R✓C✓ |

### 11.4 Sibling-audit claims confirmed by this audit's verification

The cross-check (section 13) sent every sibling claim our catalog did not cover through the same refuter and calibrator pair. The ones below were confirmed; they are not duplicated in sections 11.2 and 11.3 but carry the same weight in the roadmap. The claim text is the sibling's, condensed; the files and the verification are ours.

| Id | Severity (ours) | Status | Claim (sibling, condensed) | Files | Votes (refuter; calibrator; arbiter) |
| --- | --- | --- | --- | --- | --- |
| CX-X1 | Medium | confirmed | Recovery can reinstall an already installed machine when identity is unavailable: managed-os.md treats a private installation that powered off for a network restart as an installation that never began (publication gone, machine off, identity read cannot answer) and labels it positive no effect, so the next apply installs again; the interruption table repeats it. This contradicts security.md's rule that ambiguous probes stay unknown and turns a transport/power ambiguity into permission to repeat a… | `specs/managed-os.md:563-569, 788, 399-414, 1251-1284`; `specs/security.md:33-35` | real=True, medium, Medium; real=True, medium, Medium; - |
| CX-X3 | Low | confirmed | Service responsiveness and useful service readiness need separate meanings: NTP readiness accepts an unsynchronized server-mode reply and proxy readiness proves only a bounded protocol response; downstream readiness can be read more strongly than the evidence supports. Distinguish responding, local reference, upstream synchronized and unknown quality with a consumer-required level. | `specs/infrastructure-services.md:520-521, 540-549` | real=True, medium, Low; real=True, medium, Low; - |
| CX-X6 | Medium | confirmed-by-arbitration | Media cleanup authority is broader than media ownership: container-clusters.md specifies ejecting whatever media is currently presented, including foreign media, while substrates.md separates a physical-machine claim from ownership and destructive authority. Media ownership identity is also stated two ways (per-insert publication identity vs exact scheme/host/path with port normalization). An inverse may act on media another actor inserted. | `specs/container-clusters.md:622-625, 878-882, 905-935`; `specs/substrates.md:598-602` | real=False, medium, Medium; real=True, medium, Medium; real=True, Medium |
| CX-R4 | Medium | confirmed | Backup is repeatedly offered as a remedy without a supported restore contract: contexts.md, state-reconciliation.md and secrets.md offer whole-store or keyring backup restoration as the remedy for several damages, yet contexts.md rejects copy restoration via device/inode binding, has no restore command and defers rebinding and new keys after counter rollback. An operator with a complete backup has no supported path to regain authority. | `specs/contexts.md:354-371, 734-741`; `specs/state-reconciliation.md:1504-1537`; `specs/secrets.md:518-523` | real=True, high, Medium; real=True, high, High; - |
| CX-R5 | High | confirmed | Removal observation text converts an unreadable target into no-effect: state-reconciliation.md says a removal observation that cannot distinguish the postcondition from an unreadable target is read as no-effect so a retry proves absence, while the next table keeps failed/forbidden/malformed observations unknown, retry is forbidden while unknown, and security.md forbids treating ambiguous probes as absence. Implementers can derive incompatible retry authority. | `specs/state-reconciliation.md:545-548, 550-555, 1228-1232`; `specs/security.md:33-35` | real=True, medium, High; real=True, medium, High; - |
| CX-R6 | Medium | confirmed-by-arbitration | The read-only effect budget has contradictory owners: controller.md rebuilds/publishes a missing runtime before diff and bounded runs (disclosed in commands.md), while state-reconciliation.md says reads never create or repair and live comparison runs no effect, and security.md forbids cache/temp files and any process/network/secret lookup for read-only commands, contradicting live diff and preflight. Needs one command effect-class matrix. | `specs/controller.md:184-204`; `specs/cli/commands.md:98`; `specs/state-reconciliation.md:297-306, 1654-1657`; `specs/security.md:134-135, 696` | real=False, medium, Low; real=True, medium, Medium; real=True, Medium |
| CX-R9 | Medium | confirmed | Universal fixed concurrency is contradicted by unbounded multi-SSH sessions: security.md requires fixed target concurrency and process/resource bounds, but cli.md starts every selected SSH session concurrently with no bound, carried into cluster exec; power batches already use the engine bound. A large selection can exhaust controller processes, descriptors or remote authentication capacity. | `specs/security.md:20-31`; `specs/cli.md:1037-1040, 1109-1117, 1324-1332` | real=True, high, Medium; real=True, high, Medium; - |
| CX-API02 | Medium | confirmed | Explicit Environment selection has an incomplete storage closure: environment.md calls its closure authoritative but the StorageCluster row omits StorageClient, StorageObjectUser, StorageObjectBucket and StorageReplication, admitted elsewhere; omitting both lists bypasses filtering. It still calls the catalog 27 kinds versus api.md's expanded catalog. Selecting a cluster explicitly can drop access or replication intent. | `specs/api/environment.md:198-218, 204, 240-241`; `specs/api/storage.md:61-81`; `specs/storage-resources.md:9-19, 238-279`; `specs/api.md:262-275` | real=True, high, Medium; real=True, high, Medium; - |
| CX-API04 | Medium | confirmed-by-arbitration | Interrupted Ceph deployment remains outside the general continuation promise: storage.md refuses partial-cluster recovery after a non-bootstrap host carries a daemon while the cluster is not ready (pending B659), plus a partial DB/WAL boundary, while broader text promises exact journaled continuation. The primary multi-host physical Ceph journey can reach a safe refusal with no declared forward recovery. | `specs/storage.md:650-662, 687-689, 1355-1360`; `specs/storage-resources.md:1462-1477` | real=False, medium, Medium; real=True, medium, Medium; real=True, Medium |
| CX-G4 | Low | confirmed | 'An executable rule's prose goes' confuses normative authority with its verification: architecture-best-practices says specs own requirements, then that a rule a tool can check becomes a test and its prose goes; AGENTS.md assigns required behavior to specs and the UX skill requires specs and tests to change together. An agent can delete a safety or API requirement once a test checks it. | `.agents/skills/architecture-best-practices/SKILL.md:16-23`; `AGENTS.md:14-16`; `.agents/skills/ux-and-apis/SKILL.md:34-37` | real=True, medium, Low; real=True, medium, Medium; - |
| CX-G5 | Low | confirmed-by-arbitration | Scope discipline needs explicit delivery modes and a definition of inseparable repairs: AGENTS.md and code-implementation require backlog registration, isolated worktree, commits/integration and forbid same-change pre-existing fixes, conflicting with read-only/report-only/no-commit requests; a blanket ban can exclude a minimum prerequisite needed for the authorized outcome. B665 proposes a request reference form the skill lacks. Add review-only, edit-without-integration and normal delivery modes. | `AGENTS.md:18-26`; `.agents/skills/code-implementation/SKILL.md:13-20, 42-51`; `specs/milestones.md:39-64`; `specs/milestones/backlog.md:2225-2240` | real=False, medium, Low; real=True, medium, Low; real=True, Low |
| CX-RULEplanbytes | Medium | confirmed | Aggregate request bytes exceed the plan-record bound: Additional contract correction: state-reconciliation.md anticipates total request bytes up to 4 MiB while it bounds the complete plan record at 1 MiB; B715 marks future larger planning bounds. Prove aggregate encoded-size fit before promoting them. | `specs/state-reconciliation.md:420-423, 758-769` | real=True, high, Medium; real=True, high, Medium; - |
| CX-RULEcredentialrevocation | Medium | confirmed | Revoked credentials can strand continuation: Rule-table row (credential binding and revocation): immutable secret versions bind an operation, so a revoked credential can strand continuation; the limitation is unspecified, and any future rebind needs separate authority and proof, never implicit latest-secret substitution. |  | real=True, medium, Medium; real=True, medium, Medium; - |

## 12. Per-file coverage ledger

One Sonnet assessor read each file group fully (the six largest milestone and history pages structurally, by headings, tables and sampled sections) and recorded purpose, verdict, strengths, issues, overlap and a recommendation. Token figures are bytes divided by four. This ledger is the coverage proof for the brief's requirement that every spec, skill, wrapper, rule, hook and entrypoint file was examined; the verdict is the assessor's and the findings catalog carries the verified defects.

Files assessed: 80. Verdicts: good 17, needs improvement 62, bad 1. The full assessments (purpose, every strength and issue, naming notes, overlaps and token-cost notes) are longer than this report can carry; the table keeps one strength, up to three issues and the recommendation per file.

### 12.1 Specifications

| File | Lines | ~Tokens | Verdict | A strength | Main issues (line range: issue) | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `specs/project.md` | 105 | 1,313 | needs improvement | Short (105 lines) and mostly links to owners: lifecycle, handoff, dependency rule and add-ons are routed to state-reconciliation.md, architecture.md and add-ons.md inste… | (1) 19-32: The platform-family list is declared to 'bound' intended scope, yet it omits the family growth the owner intends (other cloud substrates, cluster types beyond OpenShift/OKD, non-Ceph storag… (2) 50-53: The early GitOps bootstrap-object handoff is stated as settled product behavior, but state-reconciliation.md marks it as pending the owner's decision Q60 under D173. The scope page therefor… (3) 64-71: One paragraph mixes add-on trust design (reviewed Go, closed object program, one adapter), a delivery-state link to B90, and the no-plugin rule. The add-on part duplicates add-ons.md and th… | Rewrite sections 'Product scope' and 'Implementation and extension model': state the family scope and the extension rule (new families via registries, no change to general flow), drop the B90 link and the add-on and plugin restatements in favor of one link ea… |
| `specs/index.md` | 60 | 1,156 | needs improvement | The routing table is dense and mostly one row per question, so an agent finds an owner with one load of about 1.2k tokens. | (1) 8-30: specs/storage-api-review.md is not routed from the index, although storage.md, storage-native-api.md, storage-resources.md and milestones/m2.md link to it. A page absent from the router is… (2) 43-60: The glossary defines only 7 terms. The lifecycle terms the audit and the specs depend on (operation, attempt, block, stage, resolution, evidence, receipt, refusal, implementation, variant,… (3) 48-49: Controller is defined as the host that runs Bootwright and, in the same bullet, management controller as a BMC endpoint. Specs also use controller record, controller runtime, controller sta… | Rewrite the glossary section (43-60) to cover the lifecycle and extension terms by pointer to their owner, split the bundled rows, add storage-api-review.md, and shorten Conventions to a link to AGENTS.md. |
| `specs/milestones.md` | 131 | 2,045 | needs improvement | States the authority rule once and firmly: a spec, page, item or decision authorizes no implementation (milestones.md:36-39), and AGENTS.md only points here. | (1) 67-68, 90-91, 19-20: Item/slice Delivery values end in `completed`, but a milestone is declared by setting its Delivery to `done`; no value exists for a retired milestone, so M5/M6 are forced to stay `not start… (2) 11-21, 29-32: A volatile status dashboard (Next cells, 'Next for agents/operator', 'review D159 to D173') lives in the page every agent loads for rules. The decision range is already stale: backlog.md re… (3) 40-48, 52-61: The WIP limit, 'in sequence' and waiver rules are not testable from this page because each milestone page and decision grants its own exceptions (m1.md:80, m2.md:37, m3.md:40, m4.md:169). '… | Rewrite sections Status and Completion: split the volatile status dashboard from the rules, add a retired Delivery value and one milestone terminal state name, drop dated decision ranges, and move the identifier-exception history into backlog decisions. |
| `specs/milestones/m5.md` | 9 | 111 | good | Nine lines hold the date, decision, destination anchors and the no-reuse rule (m5.md:3-7). | (1) 5-7: States that the Status row 'stays `not started`' for a retired milestone, which only exists because milestones.md has no retired value. The page keeps a state that is wrong by design (see m… | merge into specs/milestones.md as one 'Retired milestones' table (M5 to M3e, M6 to M4e/M4f) once a retired Delivery value exists, and delete m5.md and m6.md; keep the numbers reserved. |
| `specs/milestones/m6.md` | 12 | 151 | good | Maps every former item to its destination or retirement with a dated decision (m6.md:3-8). | (1) 6-9: One run-on sentence routes five items (B90, B91, B92, B93, B89) to four destinations, which is hard to verify; a four-row table would be checkable. The 'stays `not started`' state is the sa… (2) 7-8: B89 is 'retired, folded into B752 and B754', but milestones.md:111-131 says only open work receives a B and a dropped item is retired in the backlog Retired table. The page does not say whe… | merge into specs/milestones.md as a 'Retired milestones' table with one row per moved item, then delete m5.md and m6.md. |
| `specs/milestones/m7.md` | 126 | 1,959 | needs improvement | Follows the page template exactly: scope, Requires, Exit tier, Open decisions, Planned slices, Items table, one B detail section each (m7.md:1-37, milestones.md:119-124). | (1) 14-19, 25-34: Nine of ten items are 'Needs definition' with no planned slice, yet the milestone Requires M4 and every item gates it (milestones.md:60-61). Item outcomes are nouns ('The rendered-file mani… (2) 7-9: The rule that independent execution creates no operation, lease, ownership or continuation state is behavior stated in a milestone page; specs/project.md:79-80 already owns it in shorter fo… (3) 11-13: The Requires paragraph mixes a dependency with rationale ('so standalone rendering is the final milestone') and history ('M6 retired under D173'). It names 'legacy M1b/M1c/M1e' although M1… | rewrite section Items and the Requires line: use defined Delivery values in Requires, give B51 to B60 present-tense testable outcomes or collapse them into one 'needs definition' item until a slice is planned, merge B57 with B58, and replace the independence… |
| `specs/architecture.md` | 1119 | 19,451 | needs improvement | The core rules are precise and testable: consumer-owned ports, composition-only binding, closed registries, no runtime plugin path, no branching on implementation identi… | (1) 762-899: The adapter result protocol (descriptors 3 and 4, phase table, 64 KiB record bound, drain times, deadlines, exit-versus-record precedence) is a wire-level spec of about 140 lines. No other… (2) 989-1115: The verification section embeds gate procedure: the make quick and make focus selection algorithm (1082-1115) and the working-tree-versus-tracked-file rules of docs checks (1042-1081, one 3… (3) 3-8, 195-211, 244-245: The header says named paths are a placement contract, not a claim that code exists. The body then names many concrete existing files, functions and tests (wiring_machine.go, machineOwnershi… | Split. Keep the structural contract (boundaries, contexts, dependency direction, package kinds, lifecycle ports, implementations and specialization) in architecture.md. Move the adapter result protocol into its own spec such as adapter-protocol.md. Move the m… |
| `specs/api.md` | 423 | 6,140 | needs improvement | Owns one coherent subject (the closed authoring grammar) and routes kind-specific fields, input ceilings and kind defaults to named pages instead of restating them (api.… | (1) 183, 262: The page says the catalog has 33 kinds twice, but its own table lists 34 and api/v1alpha1 registers 34 kind names. Quote: "contains exactly 33 authored kinds"; the count and the canonical o… (2) 262-275, 376-385, 321-322: The 34 kinds are listed three times in two different orders (the catalog table puts Secret and Entitlement second, the canonical output order puts Secret last). "The catalog's canonical kin… (3) 31-37: Version authority is an agent-process rule ("requires an explicit user request", refactoring never authorizes a version) placed in the API contract, with no other owner in specs or AGENTS.m… | Keep as the single owner of the authoring grammar but fix the kind count (34) and make one list own kind order; move Version authority out to the policy owner; and rewrite the diagnostic-suppression paragraph (api.md:155-174) as one rule plus a table. Move di… |
| `specs/api/input.md` | 192 | 2,892 | needs improvement | Single, well-scoped owner for discovery, ceilings and parser boundary; opening paragraph (lines 3-6) names what it owns and delegates grammar to api.md. | (1) 36-39: States in the present tense that a hard-linked candidate or add-on marker refuses with input.symlink. environment.md:177-179 records the same behavior for markers as 'Not yet met' (B771), d… (2) 163-167: The only statement of the accepted YAML dialect (no directive or %YAML 1.1 accepted, %YAML 1.2 rejected) sits in the parser boundary. The grammar page api.md:46-80 (strict scalar rules, rej… (3) 152, 169-184: Syntax diagnostic content (parser reason, 'start of its enclosing block when the parser names one', line-1 fallback) encodes behavior of the chosen YAML v3 library without naming it precise… | Rewrite Discovery, ceilings and parser boundary sections: resolve the hard-link contradiction with environment.md/B771 by marking it 'Not yet met' in one place, move the YAML dialect rule into api.md#yaml-streams-and-decoding, define one term for the excluded… |
| `specs/api/environment.md` | 503 | 7,514 | needs improvement | Field tables give type, required, default and rule for each Environment field, and the closed shapes (domains, sites, downloads, lifecycle) are stated exactly (lines 9-2… | (1) 238-242: States 'the 27-kind catalog' but api.md:262 says the catalog 'contains exactly 33 authored kinds'. An implementer checking `defaults` keys against the wrong count, or a drift check built on… (2) 202-204: The retention closure enumerates kinds by hand, and the StorageCluster row omits `StorageClient` (which has a required `clusterRef`, storage.md:61) and `StorageReplication`, `StorageObjectU… (3) 257-307: The defaults algorithm is a generic, all-kinds processing contract, yet it is split between this page, api.md:314-345 and api.md:150-157 (and input.md:60). Three pages state the order and t… | Split: keep Environment fields, domains, sites, selection and closure here; move the generic defaults algorithm into api.md's defaults section as its single owner; move dependency versions, the controller Machine and the refusal table to controller-facing pag… |
| `specs/api/secrets.md` | 211 | 3,236 | needs improvement | One coherent scope: two kinds, declaration only, with custody, storage and materialization delegated by link to specs/secrets.md and security.md (lines 1-10, 129-133). | (1) 54-103: About 50 lines (a quarter of the page) specify the diagnostic text and remedy wording for a retired `source.file` arm, including how `<context>` is chosen per command and the placeholder ru… (2) 27-34: Three states (omitted, authored `{}`, authored `contextStore: {}`) are distinguished, with two different canonical serializations for the same semantic, and the text is a single dense parag… (3) 142-158: The arm-set table does not say whether `registry` or `license` is allowed, ignored or refused for `redhat-rhel`, nor whether `license.accept` is allowed for `redhat-ceph`; only `rhsm` is st… | Rewrite section 'File source' (lines 54-103) to a short refusal rule plus a link to the diagnostics owner, add the missing registry/license permission rows for redhat-rhel and redhat-ceph to the arm-set table, and replace consumer behaviour in the registry an… |
| `specs/api/machines.md` | 826 | 14,862 | needs improvement | Field tables give type, required, default and rule per field, and name the refusal path (for example api.invariant at a given JSON path), so admission behaviour is testa… | (1) 121-123 vs 334 vs deferred/machine-…: Three statements disagree on network.interfaceAttachments: line 121-123 says libvirt does not yet read it and it refuses on every realized substrate, line 334 says it maps interfaces 'on li… (2) 1-826 (whole page): One page owns five kinds plus lifecycle, access, proxy inheritance, Kickstart customization grammar and NMState interpretation, about 15k tokens. A task touching one kind (for example Netwo… (3) 158: The libvirt attachment row packs about ten independent rules (bridge grammar, management modes, address, forward, per-host uniqueness, overlap, 20-attachment bound, a test name) into one ta… | Split by kind or concern (InfraProvider/profiles, Machine, MachineInstallProfile with customizations, NetworkConfig with NMState subset), or at least add a section map; first fix the interfaceAttachments contradiction, move milestone ids and the test name out… |
| `specs/api/infrastructure-services.md` | 258 | 3,897 | needs improvement | One coherent subject with a clean split from specs/infrastructure-services.md: this page declares, the runtime page refuses and deploys, and cross-links point both ways. | (1) 164-177, 68-77, 27-37: Managed Registry (mirror-registry) and managed LoadBalancer (haproxy) are specified as valid declarations, but the block-identity table covers only four kinds and the runtime page has no ro… (2) 99-101, 142: The 61-endpoint-address and 62-socket bounds are restated here and again in specs/infrastructure-services.md (refusal rows near lines 58-59 and prose near 124-127). Two pages carry the same… (3) 85-98, 112-113: The bindAddress-equals-endpoint rule and the wildcard-requires-an-endpoint rule are stated inside the DNSServer paragraph; the Proxy/NTP equivalents are only implied by line 99. NTP address… | Rewrite sections Management and placement and Proxy/DNS/NTP/registry deployment: add per-kind support status for Registry and LoadBalancer with block identity and a refusal pointer, make the endpoint/bindAddress consistency rule per kind, mark additionalIngre… |
| `specs/api/container-clusters.md` | 475 | 6,919 | needs improvement | Field tables state type, required, default and rule per field, so most omission and default behaviour is testable (lines 55-88, 189-201, 369-380). | (1) 148-172: Platform derivation switches on substrate provider type (libvirt, bare metal, vSphere, KubeVirt) inside the generic API page, so adding a substrate or cluster type means editing this prose… (2) 164-181: The text specifies vcenterCredentials as a working feature (keying, required topology.folder, datacenters rule), then 'Not yet met' says the arm admits none. A reader cannot tell whether a… (3) 227-239: A 80-word sentence mixes the normative rule (baremetal/vsphere take the openshift source only) with installer-version rationale ('4.20 to 4.22', 'development branch', loadBalancer ignored o… | Rewrite sections 'Platform union and derivation' (move substrate-type derivation behind the realized-target owner and state the vcenterCredentials admission outcome in the present tense) and 'Topology' (move line-pinned evidence to the version-specialization… |
| `specs/api/storage.md` | 615 | 9,998 | needs improvement | Admission refusals are stated as closed, testable lists with a named code (api.invariant, api.field, api.required) and a 'before any effect' timing (e.g. 449-469, 490-49… | (1) 277-278 vs 437, 456-457: The page says every reference is a plain scalar string, but StorageFilesystem.dataPoolRefs carries a default flag and api.md:208 documents a {name, default} record exception. The page's own… (2) 10-59: The first paragraph is a single ~50-line run-on that mixes native-service admission, which services plan or refuse, serviceBindings routing, mgrModules and retired fields. An agent cannot f… (3) 124-130: 'Ordinary apply realizes ...' is a lifecycle capability list in an admission page, duplicating storage.md, and its list is garbled ('an object user's buckets and StorageClients of an RBD po… | Rewrite the opening 'current admission' prose (10-130) into a per-kind admission table plus short clauses, move request/evidence version continuation (213-223) to storage.md or the state pages, and move the listener list (526-567) into a table that storage-re… |
| `specs/api/addons.md` | 518 | 9,818 | needs improvement | Tables give type, required, default and rule per field, and omission, empty and explicit-false are mostly stated (lines 114-117, 149-151, 515-516). | (1) 3-8, 25-247, 249-497: The page joins two subjects with different lifecycles: authored ClusterAddon/Profile arms that every plan and apply refuses (admitted, inert), and the built-in packages that actually execut… (2) 354-363, 380-389, 281: Rationale and unverified history sit in normative text: v1 NooBaa x509 observation, the unverified redhat-operators package claim with B761, the catalog-priority reasoning. This is knowledg… (3) 455-497, 452-453: The full gitlab field table and the certmgr package are specified although both packages do not exist; the page does not say which package rows are in force. The spec becomes a design brief… | split: keep the authored kinds, binding grammar, shared diagnostics and normalization in addons.md; move the built-in package configuration records (one section per package) to a sibling page, with package-only unmet items marked once. Move rationale in lines… |
| `specs/api/custom-playbooks.md` | 38 | 488 | needs improvement | Short, present tense, and clearly separates admission (lexical only) from lifecycle (enabled objects refused before content access) at lines 13-34. | (1) 3-8: The page defers its own field list and lexical rules to the page under specs/deferred/, so the 'API' owner is incomplete and the detail page points back at it as the owner of enforcement. A… (2) 23-26, 30-34: Enabled defaults to true, enabled declarations are 'valid order graph' members, yet every enabled object is refused at plan and apply. The page never says an operator can only get a plannab… (3) 13-21: The disabled-declaration behavior on omission, null and empty is not stated here: target-list emptiness, null extraVars and null tags are covered only in the deferred page or the Go schema,… | rewrite section 'Reserved-shape contract' so this page owns the full admission rules and enabled-semantics (including omission and null), and reduce the other four restatements of the refusal to links. |
| `specs/deferred/custom-playbooks.md` | 103 | 1,427 | needs improvement | Precise, testable lexical rules: tag regex, forbidden controller host identities, and source-arm unions with exactly-one semantics (lines 14-33, 45-58, 85-100). | (1) 3-10: The page lives under specs/deferred/ and says it is revived by B102, but also says its rules are 'the lexical checks admission enforces as written'. Currently enforced rules therefore sit i… (2) 14-31: The schema table repeats field names, types and defaults that api/v1alpha1/customplaybooks.go declares authoritative (line 8-9), a second copy that can drift from the code and the schema co… (3) 35-43: Ordering text mixes 'would complete' with 'are invalid', so it is unclear whether the duplicate-provider, missing-requirement and cycle checks are enforced today. It does not define the fai… | move the currently enforced lexical rules into specs/api/custom-playbooks.md as the single owner, drop the schema table duplicated by the Go schema, and leave in the deferred page only the reserved ordering graph and execution prerequisites that B102 revives. |
| `specs/cli.md` | 1404 | 22,931 | needs improvement | Precedence order for invocation handling is an explicit numbered list (cli.md:46-54), so help versus validation versus unavailable-command outcomes are decidable. | (1) 1-1404: The page is about 23k tokens and mixes seven independent contracts (parsing, privilege, lifecycle receipt/status, diff, cluster/secret access, SSH sessions, trust/power). A task on any one… (2) 1209-1212: Keyring table says an empty `--name` or `--client` fails `access.target` with exit 1, but both flags are required and the parsing rules make an empty required or scalar value a usage error… (3) 113-188: The list-flag rules are a long chain of per-command bullets with mixed 'and'/';' connectors and repeated 'must likewise resolve to at least one member' clauses; the same per-command comma b… | Split by contract: keep cli.md as parsing, precedence, confirmation and lifecycle behavior; move privilege to security.md or cli/privilege.md, the access/session/trust/power commands to cli/access.md with one shared refusal-table preamble, and the diff text l… |
| `specs/cli/commands.md` | 421 | 9,274 | needs improvement | Single declared owner of the command and flag catalog; cli.md:27-31 defers to it as the sole list of command paths, and the grep across specs/ found no second catalog. | (1) 49-52, 56-132: The page defines Local, Observe and Mutate as effect classes but the Effects column never uses them as a classification; it is free text such as 'none', 'read local state' or 'complete plan… (2) 96-104, 118-122: Several table cells (plan, diff, apply, destroy, setup, cluster info) carry multi-clause normative prose, including flag constraints for setup (--download-only requires --dest-dir, conflict… (3) 298-308, 381-388: The --yes rule restates the ordinary-confirmation text of cli.md:239-266, including the sentence about proofs that follow the prompt, and the stop/restart and machine trust rules restate th… | Rewrite sections: give the Effects column a closed class vocabulary and move long cell prose into the safeguards section; collapse the --yes and --output restatements into links to cli.md and output.md; name the trust-on-first-use acceptance per preflight fam… |
| `specs/cli/output.md` | 1090 | 19,806 | needs improvement | Stream/exit-status matrix (lines 10-23) states every mode in one table, and the sensitive-bytes and write-failure rules (36-56) are explicit and testable. | (1) 492-523: The managed Ceph cluster-info entry (cephCommand, endpoint URL derivation with frontend_port and rgw_frontend_port defaults, IPv6 bracketing, scope strings per storage kind) is storage doma… (2) 238, 239: The plan and apply/destroy result tables list field names (bound, startable, deferred, stops, stopsAfterOnline, settled, recovered) with no type or meaning. Grep of specs/ finds no other de… (3) 336-341, 746, 752-755, 313-317: The general omission rule says an unavailable optional scalar or object is omitted, but diff uses empty strings for absence (operation values '' with no operation, plan/blocks/realization '… | Split: keep streams/exit status, human layout, envelope, omission rules, diagnostic registry and compatibility in output.md; move per-command result shapes (machine, media, cluster/Ceph, lifecycle/status, diff) to a sibling results page, move the managed Ceph… |
| `specs/controller.md` | 1032 | 17,120 | needs improvement | Clear top-level split of host prerequisites (setup, context-free) from context prerequisites (controller stage), with a two-row ownership table at lines 21-25. | (1) 21-25, 313, 426, 548, 761-784: The table assigns 'the binding between this context and this host' to the controller stage, but the stage 'never ... publishes a binding' and the first apply publishes it. The stage also wr… (2) 21-25, 227-250, 366-390, 927-937: The predicates that select each context closure (libvirt client, hypervisor, installer media, nmstate, iputils) are written four times: ownership table, host-package table, stage paragraph… (3) 448-575: The stage section embeds adapter internals: the 93-byte oc marker, streaming and memory rules, per-tool deadlines, closed refusal classes and 14 test names. Normative contract, adapter impl… | Split by owner: keep controller.md as scope, ownership table, supported hosts, the setup flow and the controller-stage guarantees; move the runtime build/launch/retention mechanics to a controller-runtime page (or into contexts/controller-record.md), move lis… |
| `specs/contexts.md` | 902 | 15,421 | needs improvement | Identity rule is crisp and states its own consequence (a recreated name silently aliases old references), lines 11-23. | (1) 186-205, 304: The page says the registry writes format version 5 and the record table lists Registry version (5), yet the same section says a version 5 registry (written by builds before the setup v2 cut… (2) 184-205, 283-291, 635-659: The earlier-build, fresh-install refusal is stated four times with partly repeated message text and remedy (fresh-install quote, 'another build may manage live environments', 'never moved a… (3) 553-633: About 80 lines derive retained-operation admission arithmetic (entries per first pass, 16 MiB free, last-removal reservation) and cite a dozen tests, while the record layout it counts is ow… | Rewrite the registry-version statements (lines 186-205 and 304) to the current format, keep the fresh-install refusal text in one place and link to it, and then split: move the media acquisition protocol (662-740) and the retained-operation admission arithmet… |
| `specs/contexts/controller-record.md` | 245 | 3,810 | needs improvement | Crash-safety is specified step by step (reservation, attribution, seal, retiring) with the durable-intent ordering stated, which makes the tool-area protocol implementab… | (1) 26-35: The page says the first apply that binds a context publishes the Controller descriptor and that setup writes no controller record. contexts.md:189 and contexts.md:250 say confirmed setup ad… (2) 19-23, 205: The only stated executable-file exception is a tool area's published executables (matching security.md:150-152), yet runtime contents keep the ordinary modes a Python virtual environment cr… (3) 142-145, 94-103: Retirement frees only sealed areas no binding names, and reserved or attributed areas are never retired by any command; the at-16 refusal remedy (context delete --purge) retires only the ar… | Split: move the setup subtree (163-227) into its own page (for example specs/contexts/setup-store.md) and index it, keep this page as record, descriptor and tool areas; then move the staged-write rules into Tool areas, list every record member name in a schem… |
| `specs/secrets.md` | 565 | 9,423 | needs improvement | Precise safety rules with remedies per refusal: strict input-file table with one condition per refusal (specs/secrets.md:195-214), never-material-in-evidence rule (565),… | (1) 52-124: The set subsection mixes flag grammar, a lock/lease protocol, sudo I/O-log detection, terminal prompting and remedies in a few very long paragraphs of nested clauses, so single requirements… (2) 60-69; api/secrets.md:82-90; cli/co…: The per-type set flag matrix is stated three times (this table, the api remedy table, the command table) with different content: api lists only --value-file and omits --value-stdin and the… (3) 113, 193, 226, 248, 319, 339: Owner decisions are quoted inline as D-ids (D71, D72, D73, D113, D124, D175) and some are rationale-in-quotes inside normative text, e.g. 'D124, below'. Decision provenance belongs in miles… | Split by consumer and trim: keep secrets.md as custody, commands and binding contract; move the local-keyring-v4 format (lines 384-523) to specs/secrets/local-keyring-v4.md with the anchor kept stable for cli.md and contexts.md links; move the sudo stdin prot… |
| `specs/state-reconciliation.md` | 2084 | 34,063 | needs improvement | The safety core is precise and fail-closed: unknown is never absence (1821-1822), positive evidence is required for done (1123-1126), and resolution precedes any retry o… | (1) 1-148: The Lifecycle unit section mixes the operation modes (6-23) with finalization, settling, orphan-state and lost-index mechanics. Single sentences run past 100 words (72-112, 114-126), and th… (2) 150-252: Context mutation evidence is about 100 lines of claim, re-proof, restoration and reclaim mechanics stated as long chained prose, while its own vocabulary (pending, applied) differs from the… (3) 456-469: The state-machine table has no paused-destroy row although a removal can pause on live or unproved Machines (1857-1861), and the file claims the table is exact (500-502). The backlog alread… | Split by consumer and move history out: keep the lifecycle model, state, outcome and precedence tables, mutation safety and readiness here; move records, evidence, registration and reclaim mechanics to a records page; move the diff comparison to the cli/diff… |
| `specs/infrastructure-services.md` | 572 | 10,132 | needs improvement | Failure semantics are explicit and testable: definite failure vs unknown vs positive partial realization are distinguished for readiness, apply replay and removal resolu… | (1) 93-221: The host reservation key registry (socket, unit, path, bridge, libvirt-*, prefix, bmc, media) and its conflict, release and SSH-host comparison rules are cross-capability contracts used by… (2) 58-59, 121-136: The 64-key bound and its 61/62 arithmetic is stated three times in this page (refusal table, prose, tests) and again in api/infrastructure-services.md and substrates.md (which restates a 63… (3) 552-561, 244-260, 490-501: The shared lifecycle (owned host state, normal-form content root, replay, inverse, unknown resolution) is specified inside the ArtifactServer section and the network services are defined by… | split: move 'Host reservations' (lines 93-221) into its own page (for example specs/host-reservations.md) as the single owner of the key registry, bounds and conflict contract, and restructure the rest into one shared 'managed service lifecycle' section with… |
| `specs/substrates.md` | 1039 | 17,475 | needs improvement | Per-block structure (Realization, Reservations, Evidence, Inverse, Quiescence) is uniform for libvirt host, libvirt machine and physical machine, so a reader can compare… | (1) 332: The spec names request versions `substrate-host-libvirt-v4` and `machine-libvirt-v7`, but the code constants are `substrate-host-libvirt-v6` and `machine-libvirt-v8` (internal/substrate/lib… (2) 22-54, 639-699, 701-734: Selection says KubeVirt and vSphere providers refuse before registration (also the refusal table, line 89), yet lines 40-48 and two whole sections specify how those arms 'realize' Machines… (3) 40-48: A Bootwright-installed Machine on KubeVirt or vSphere 'keeps refusing until its arm defines an identity channel', while lines 946-949 say a substrate lacking both channels must define its o… | Split by arm and concern while keeping this page as the contract owner: keep selection, refusal table, identity and power operations and the adapter boundary here (about 300 lines); move libvirt host and machine realization, physical realization and the media… |
| `specs/managed-os.md` | 1462 | 27,797 | needs improvement | Fail-closed, evidence-first semantics are stated concretely: proof of the exact target immediately before the effect, marker byte-match completion, positive/unknown/no-e… | (1) 1-1462: The page carries at least six separable owners (media store 11-109, installation 111-598, network convergence 600-838, FIPS/encryption/registration 840-1152, package tree 1154-1217, refusal… (2) 307-315, 324, 604-605, 816, 836-838…: Delivery state and history sit inside the normative text: B326, B70, B73, B78, B485, D153, D159, OD24, 'unverified until the owner's run', 'retire live contexts with the build that applied… (3) 1355-1364, 105-109, 510-513, 635-64…: The 'Refusal table' lists only the plan-time Unsupported rows. Named apply-time refusals (foreign-installation, media-changed, media-changed-tree, tree-source-drift, fips-not-enabled, root-… | split: move the media store (11-109) to its own page or into contexts.md, move network convergence, FIPS, root-disk encryption and registration into per-feature pages (or a specs/managed-os/ directory) leaving a ~300-line installation core; move every B/D/OD… |
| `specs/container-clusters.md` | 1013 | 17,617 | needs improvement | Block-level contracts (media, installation, network) are detailed enough to implement and test: reservations, evidence, replay, inverse and unknown resolution are each s… | (1) 25-37 vs 500-509: Selection says a physical node 'installs through its management controller' and states its private-delivery rules, but the Supported shape says all nodes are virtual, and Installation says… (2) 153-184: The section says all-KubeVirt and all-vSphere clusters are 'a shape the product admits' and describes pushed delivery in present tense, while the same section and the Supported shape (25-31… (3) 1-1013: One page mixes requirements with history, observation and rationale: 'observed by v1; re-verify', installer-release trivia (4.20 to 4.22, 'development branch does forward one'), the 'about… | split and move history out: keep this page as the selection/refusal/inputs contract, move the media and installation block contracts and their budgets into a second spec (or a clearly anchored sub-page), consolidate the kubeconfig keep/restore rules into one… |
| `specs/container-cluster-coverage.md` | 220 | 7,025 | needs improvement | The opening paragraph names its own scope and links the owner of lifecycle, public intent, machine realization and delivery (lines 3-9), which matches the one-owner-per-… | (1) 41-64, 81-138, 154-166: Delivery state is embedded in a spec page: the Item, Submilestone and Qualification columns, 'Needs definition (Q11)', 'Implemented in-tree; lab-fips acceptance remains', 'X126's build' and… (2) 51-54, 66-69, 19-28: Normative platform and size rules are restated here: the installer-VIP and external-load-balancer rationale repeats container-clusters.md:218-228, the size bound and 6-hour ceiling repeat c… (3) 140-142: 'Every row covers create, observe, continuation ... and removal' is a blanket claim that does not fit rows such as 'Endpoint address uniqueness' (a graph rule with no native mechanism) or '… | Rewrite in place: keep Release evidence (as a link to the owner plus the two page-specific sentences), Provider paths, the matrix reduced to feature, mechanism, contract owner and API, and the qualification tiers. Move the Item, Submilestone and Qualification… |
| `specs/storage.md` | 1616 | 28,494 | needs improvement | Opening (lines 3-8) names the owners of admission, operation state and delivery, and states that an accepted declaration claims no lifecycle support; the ownership split… | (1) 1-1616: The page is a transcript of implemented worker behavior (native command lines, per-block deadlines, exact probe windows, readback field lists) rather than a contract, so the requirement set… (2) 10-15, 669-670, 1475-1482, 1599-1616: Delivery state and target contract are interleaved: line 3 says the page owns the target contract, line 10 says candidate plans are implemented, and the body then states per-family behavior… (3) 565-566: 'Fewer than three hosts keep their existing refusal' points at no refusal on this page and the refusal table (374-382) has no such row; grepping specs/ finds no owner. The lower bound of th… | split: keep storage.md as the shared lifecycle core (ownership, frozen release and trust, plan and DAG, identity and continuation, readiness, custody, removal and concurrency, refusal registry), and move each realization protocol (service units, object gatewa… |
| `specs/storage-native-api.md` | 989 | 16,198 | needs improvement | The core idea is stated cleanly and once: complete native ServiceSpec mappings under spec.ceph.services, keyed by qualified native service name, with Bootwright snake/ca… | (1) 1-15, 166-178, 957-964: The page declares itself the 'M2 target' and 'approved target requirements' while other passages say 'The implemented nvmeof service' and describe current admission. A reader cannot tell wh… (2) 47-191: About 145 lines of transport and bootstrap-recovery protocol (file names, include-closure proof, eight-link bounds, Call Home fences, license-call specialization, code symbols 'ceph_core.py… (3) 27-31, 373-375, 563-566, 1-14: The schema authority is ambiguous: the selected IBM release's schema is authoritative, yet every closed record and table is fixed to python3-ceph-common 20.2.1-405 and described as 'the sou… | split: keep service identity, admitted-member tables, placement, binding grammar and defaults here as the input model; move the verified-tunnel and recovery protocol (47-191) and the daemon-type/delegated/host-layout rules (264-348) to storage.md or a storage… |
| `specs/storage-resources.md` | 1498 | 23,597 | needs improvement | Shared resource contract (lines 32-88) states reference scoping, native-name uniqueness, omission versus explicit empty, set ordering and presence-sensitive zero in one… | (1) 9-30, 296, 403, 602, 1492-1493: The page opens with 'Current:' stating kinds are admitted and realized, yet three tables are headed 'Target rule', and the closing gate says 'New kinds other than minimal StorageClient rema… (2) 240-245: StoragePool.spec.nativeName is defined under the StorageReplication heading and appears in no other spec (grep across specs/ finds only this page and milestones); the StoragePool field owne… (3) 32-132, 90-124: The page mixes a retained-owner table, migration/B474 notes and current rules for StorageObjectGateway before its field record, and the gateway rules are split across 97, 108-124, 134-236.… | Split by kind family (shared contract plus block/file system resources, object resources, access resources such as client/NFS/SMB/NVMe-oF, export and replication), move nativeName into the StoragePool owner, delete the superseded 'Target rule' tables and the… |
| `specs/storage-coverage.md` | 628 | 13,525 | needs improvement | Matrix rows each name an owning item, and the unsupported-combination table states that the diagnostic owns its text (lines 535-553), which avoids restating refusal reas… | (1) 75-114: A long normative block (host-preparation probe hardening: closed environment, read-only root, empty capabilities, Podman inspect quirks, route probe, ceph_host_runtime.py) has no counterpar… (2) 23-31, 150-158: The page says Bootwright keeps no release catalog or per-release profile and B472 records do not gate selections, yet line 150 requires 'the release record' per matrix row with support stat… (3) 128-148: Attempt-record names and refusal codes (pool-identity, foreign-rbd-image, ceph-subvolume-attempt-v1, ...) restate ownership rules that storage.md and storage-resources.md also own (e.g. sto… | Rewrite and split: move the host-probe hardening block (75-114) and attempt-record adoption rules (128-148) to their runtime owners (storage.md or a host-preparation section), move the profile catalog and example narratives into docs or the example READMEs wi… |
| `specs/storage-api-review.md` | 428 | 6,461 | needs improvement | Declares at lines 3-9 and 11-20 which pages own accepted input, lifecycle and qualification, and which fields are admitted versus target only. | (1) 41-64: The table describes helpers (daemon helper shapes, role-derived placement, combined NFS service) as 'current' while the sibling field page and API admission now state they are retired and r… (2) 43-44: Calls the deferred field detail 'the current field inventory', but that page labels the ObjectGateway and NFS tables historical (deferred/storage-api.md:221-224, 260-264) and says the close… (3) 100-223: P1 mixes normative target (frozen planning facts, graph edges, identity as type plus ID) with before/after YAML and migration prose in the present-tense 'proposed' voice. Clauses such as 'N… | Move history out: keep a short decision record (D133 outcome, alternatives considered, native-evidence exceptions, resource relationship table owner mapping) and move the before/after YAML and the baseline table to milestones/m2.md or delivered history, linki… |
| `specs/deferred/storage-api.md` | 337 | 4,917 | needs improvement | The shared lexical-values table (lines 22-39) states testable grammars with units, case sensitivity and bounds. | (1) 1-18: The page sits under deferred/ and its header says it is revived by B472 and its later tables are historical, yet it also owns present-tense rules for the realized pool, policy, client and f… (2) 41-81: StoragePlacement and StorageIngress are specified in the present tense, including 'Placement never accepts a label, host pattern, count' (55-56) and role-derived defaults, but the native in… (3) 83-97: States that mgmtGateway, monitoring and roles are retired and refused, while the Normalization list still materializes 'mgmtGateway.dnsLabel: mgr' and role-derived placement (315-318, 336).… | Rewrite section structure: move the current kinds (cluster, client, policy, pool, filesystem, lexical table, live normalization entries) into the owning api/storage.md or storage-resources.md field records, and delete the retired StoragePlacement, StorageIngr… |
| `specs/security.md` | 698 | 14,277 | needs improvement | Opens with a crisp fail-closed invariant and explicit attacker model for the controller host (lines 3-7, 37-49), with widening of trust enumerated once as W1-W4. | (1) 229-299: A single ~5 KB paragraph specifies the lifecycle job-directory, lock, holder and sweep protocol (shared vs exclusive locks, scratch dirs, record publication order, bounded reads) with test… (2) 203-228: Parent-death signal, subreaper, SIGUSR2 and drain mechanics are given as an implementation narrative with constants (5-second and 60-second drains). An agent implementing a different adapte… (3) 256-258, 280-283, 286-288: Rules describe behavior 'across an upgrade' for 'a build before this rule', i.e. compatibility with older builds. This contradicts the recorded pre-1.x no-backward-compatibility principle a… | Rewrite section Process boundary: keep the invariant statements here, move the lifecycle job/lock/sweep and parent-death mechanics to the state-reconciliation (or a lifecycle-adapter) page and link them, delete older-build upgrade clauses, collapse the proxy… |
| `specs/add-ons.md` | 463 | 7,089 | needs improvement | Present-tense, mostly testable clauses with exact bounds (nine-byte names, 64/32/96 program limits, 30 s request bound, 256-byte native strings, 5h50m budget cap) and cl… | (1) 152-247, 249-307: One page mixes five concerns: the package model, a generic Kubernetes object port (program, ownership marks, adapter read/create/compare semantics), OLM approval logic, readiness, and a Git… (2) 89-97, 266-269, 421-422, 394, 463: Delivery state and backlog IDs (B90, B735, B727, B755, B766, B769) sit inside normative sentences, and an unverified assumption (ResolutionFailed never transient) is stated as a rule with a… (3) 373-377, 388-392, 181-182: The remedy for an unknown observation (foreign object, failed read, other installed CSV) is 'destroy and apply again'. On a physical cluster destroy leaves the add-on objects in place and u… | split: move the cluster object program, ownership receipts and adapter (152-247) to their own page owned by the Kubernetes object port (also serving the cluster network block), move the GitOps bootstrap object to the gitops row or API page, move B-IDs and the… |
| `specs/add-on-coverage.md` | 435 | 8,028 | needs improvement | Clear ownership statement and cross-links at the top (lines 3-13) naming add-ons.md, api/addons.md and m4.md as the other owners. | (1) 42-48; 63-329: The same facts (native object names, readiness conditions, defaults, required Secrets) are stated in the table, in the per-package prose, in api/addons.md#built-in-packages and again in the… (2) 3-6; 42; 44; 48: The page says add-on API owns each package's fields, yet the Typed configuration column restates field lists and defaults (timeout 30m/1h/45m/20m) that api/addons.md also tabulates. Two own… (3) 63-329: Normative package contract is interleaved with dated research evidence and rationale (for example the 2026-10-20 catalog patch comparison, ACM channel mapping and GitOps OpenShift support t… | rewrite sections 'Built-in packages' and per-package prose: keep one owner per fact (table row for objects/readiness/removal; fields and defaults only in api/addons.md), move dated evidence into the knowledge pages, move profile sizing to m4.md, and replace t… |
| `specs/deferred/add-ons-design.md` | 281 | 4,121 | needs improvement | Opening paragraph states status and ownership plainly: nothing implemented except Steps, and names add-ons.md and api/addons.md as the live owners (lines 3-8). | (1) 212-281; api/addons.md:162-172: The Steps section is the normative admission contract that the API page defers to ('records it'), yet it lives in a page whose status is 'deferred, not implemented'. An implemented rule own… (2) 239-243; 219-237: 'A step declares playbook, manifests, or both' does not say what happens when neither is present, and the Required column says 'conditional' for playbook, target, gates and follows without… (3) 225-226; 279-281: 'gates' is typed string with rule 'only apply', and 'follows' is a string of two values, with no statement of what a gate names or what ordering results, because the section says step seman… | split: move the Steps section into api/addons.md#steps as live contract with the neither-playbook-nor-manifests case defined, and keep the remainder as a custom-package design page with deferred obligations marked and security/conformance text reduced to link… |
| `specs/deferred/cli-access-and-rendering.md` | 146 | 1,877 | needs improvement | The node-selector tiers (name, then FQDN, then role-ordinal) are testable, with explicit no-fall-through and access.target failure on ambiguity (lines 50-68). | (1) 1-12, 70-86, 88-96: The page bundles four topics with four different revival owners (B101, B52/B55, an unnamed preflight slice, 'No slice currently revives this detail'). A slice that revives one topic loads a… (2) 70-75, 77-86: Rendering and Preflight are thin restatements of live text: cli/commands.md:99 and :361 already own the path flags and placeholder rule, and cli/output.md:282 already states that '--trust-o… (3) 138-146: The page reserves preflight.unknown as 'Required readiness could not be positively determined', but cli/output.md:833 already lists preflight.unknown as 'Readiness includes unknown or pendi… | split: move each section to the milestone item that revives it (B101 access, B52/B55 render, the preflight family, group results) or one page per revival owner; delete the Rendering and Preflight stubs where live pages already own them, and fix the preflight.… |
| `specs/deferred/machine-arms.md` | 106 | 1,665 | needs improvement | Field tables give type, required, default and rule per field, so each requirement is testable and the file says admission enforces them 'as written' (lines 5-9). | (1) 89-94: The page calls interfaceAttachments a 'KubeVirt-only' set, but api/machines.md:334 says it maps interfaces to attachments 'on libvirt as on KubeVirt', and container-cluster-coverage.md:106… (2) 1-13; api/machines.md:113-123: One InfraProvider union is split across two pages: bare-metal and libvirt arms are in api/machines.md, vsphere and kubevirt arms are here, and the profile shape is split again (api/machines… (3) 64-69, 96-106: Cross-object rules do not say what refuses and where: 'template' is required when a consuming install profile uses templateClone, and 'a consuming machine uses a vSphere provider profile wh… | rewrite section Machine fields (remove the KubeVirt-only claim and the duplicated MAC range), and merge the arm field tables into api/machines.md under the existing Refused arms heading, or keep this file and make api/machines.md only link to it. |
| `specs/deferred/reconciliation-scenarios.md` | 193 | 2,780 | needs improvement | States the default firmly: every changed input over a completed apply refuses, and a new scenario edits one branch, one row, one guard and one port (lines 3-12, 29-67). | (1) 130-137: Rule 1 is titled 'Identity is lineage-stable' but its body says identity comes from the owning apply's id today and moves to a lineage id only later. The heading states the opposite of the… (2) 158-162: Rule 7 says the comparison lock 'narrows only with B18', but B18 is delivered (milestones/delivered.md:8400 narrows the root lock). The context lease still refuses diff for the whole run pe… (3) 13-27, 158-166: The admitted-scenarios table and rule 7 restate state-reconciliation.md owners (state machine, lock refusal, settled-proves-nothing) that those pages already own; the same claim appears aga… | move history out: shrink to the seam, the acceptance rules and the prohibitions; fix rule 1 title and rule 7, and move the prerequisite list to a backlog item that depends on B575. |
| `specs/milestones/m1.md` | 965 | 15,775 | needs improvement | Opening (lines 1-30) states scope, owning specs, Requires, Exit tier and open decisions, so an agent can orient in under 40 lines. | (1) 749-965: About 217 lines (22%) are pre-D158 hardening history and controller setup v2 design/plan, which duplicate delivered X87-X90 and X98 records and are fenced only by a heading. An agent resolv… (2) 759-897: The baseline carries a long decision-ID chain (D56, D116, D117 to D154) and a multi-paragraph per-slice digest and request-version window narrative (X39-X96). That is delivered history and… (3) 213-291: B73 detail is about 80 lines mixing acceptance baseline, supported shape, capabilities, constraints and rehearsal. Much of it restates managed-os.md, substrates.md and infrastructure-servic… | Move history out: relocate lines 749-965 (accepted hardening baseline and controller setup v2 plan) to delivered.md or a specs/milestones/m1-history.md with a one-paragraph pointer, and trim the B73/B74 details to links into the owning specs, leaving m1.md as… |
| `specs/milestones/m2.md` | 1988 | 35,356 | needs improvement | Stable anchors and a clear spine: Submilestones table (109-124), Planned slices table (443-460) and Items table (489-522) give a navigable skeleton with Requires and Def… | (1) 489-522; 560-668; 1-110; 443-460: The Items table and the slice table lag the page's own narrative. B79, B80, B316, B472, B474, B475, B490, B491, B494 still read 'Needs definition / not started, X52-X54' (493-516) while the… (2) 13-60; 174-206; 453-474: Decision-history prose (D134, D143, D157, D158, D159, D165 waivers, WIP exceptions, 'taken by the session, pending the owner's review') is repeated in the opening, M2b Requires, M2c Require… (3) 45-107: About 60 lines of the opening are an X-ID to B-ID delivery roll-call (X71 to X82, X101, X102, X52, X157, X158) that duplicates delivered.md. It pushes the status an agent needs (what M2 sti… | Move history out and split: keep m2.md as a short status page (scope, submilestone table, slice table, current Items table with corrected status) and move the delivery narrative, waiver chain and per-item detail into per-item pages under specs/milestones/m2/… |
| `specs/milestones/m3.md` | 1880 | 32,158 | needs improvement | Stable anchors: every item is a '### Bnnn' heading and the 8-column Items table (lines 488-523) gives alias, owner, Requires, Definition and Delivery per item, so an age… | (1) 1-85: The page opens with dated changelog paragraphs ('M3c implementation, 2026-10-20', 'Independent completion work, 2026-10-23 (D181)') and a hand-written list of remaining gates per submilesto… (2) 20-46: The 'Requires' paragraph is one run-on block mixing prerequisites, the D172 waiver, the WIP-limit exception, exit tier and open decisions. An agent deciding whether a slice may start must p… (3) 490-523: Status is free text in the Delivery column ('in progress, X130', 'awaiting operator acceptance, X124', 'not started, X127') with no closed vocabulary or definition of what in-tree delivery… | Move history out and split: keep m3.md as a short status page (opening, submilestone table, slice table, Items table, one status legend, open decisions with a status column, under about 600 lines), move the 80 KB of item detail to per-submilestone or per-slic… |
| `specs/milestones/m4.md` | 1912 | 35,145 | needs improvement | The opening scope paragraph, submilestone table (m4.md:64-80) and Items table (m4.md:576-600) give a navigable map: ID, owner, requires, definition state and delivery st… | (1) 22-56: The Requires, waiver (D174/Q59), WIP-limit exception, exit tier and delivery checkpoint are in two dense paragraphs of about 35 lines; the current delivery state ('M4a accepted; in-tree imp… (2) 211,529: The page says the M3 definition 'is on the unmerged branch m3, on which m4 is stacked'. No local m3 or m4 branch exists and the checkpoint at m4.md:46-56 says in-tree work has landed on mai… (3) 576-600,46-56: Status semantics are inconsistent: Definition column uses 'Specified'/'Needs definition' while Delivery mixes 'awaiting operator acceptance, X138', 'in progress, X143' and 'not started, X14… | Move history out and restructure: keep a 150-line page (status, submilestone table, Items table with an enumerated Delivery column, open decisions) and move Q26-Q61 options, planned-slice routing prose and delivery-complete item details to delivered.md or per… |
| `specs/milestones/backlog.md` | 4096 | 94,547 | needs improvement | Three sections with disjoint jobs (Parked 12-2968, Retired 2969-3023, Decisions 3024-4096), each with a one-paragraph contract in its opening lines. | (1) 3024-4096: The Decisions log is about 130 KB (about 33k tokens, one third of the file) and a decision has no status field. Whether D140, D141, D143 or D152 is owner-accepted, session-taken pending rev… (2) 3296, 3305: D143 and D152 are single bullet lines of several thousand characters each. They hold eleven to thirteen numbered clauses, pinned release tags and package versions, option flags and error te… (3) 3024-4096: Decisions are list bullets, not headings, so no D id has an anchor; every cross-reference links only to '#decisions' (milestones.md:114, backlog.md:2999 and 3060s). A reader is sent to a 10… | Split: move the Decisions log to its own page (specs/milestones/decisions.md) with one heading per D id and a Status line (owner-accepted, pending review, superseded by Dn). Keep Parked and Retired here, and move spec-sized clauses of D143 and D152 into their… |
| `specs/milestones/delivered.md` | 8437 | 133,627 | needs improvement | Every entry names owners, outcome, guard tests and constraints left behind in a fixed field order (delivered.md:22-264, 7422-7494), so an agent can verify a delivered cl… | (1) 1-18: The opening gives no table of slices, no status vocabulary (in-tree delivered, operator-accepted, awaiting operator evidence) and no era map; the ID rules are prose ('X IDs, distinct from t… (2) 8380-8437: Headings such as 'M3d implementation awaiting operator evidence' and 'Shared lifecycle prerequisites' sit in a page titled Delivered. Open or conditional state is mixed with delivered histo… (3) 7961, 8353: Two different slices share the heading 'X121' (Produced-input lending portion; Node diagnostics and plan capacity). The second anchor gets a numeric suffix, so links by X id are ambiguous;… | Move history out and split. Keep a short delivered index (about 150 lines: ID, title, milestone, status vocabulary, owning spec link) and move the slice bodies into per-milestone or per-era pages under specs/milestones/delivered/, each in ID order with unique… |

### 12.2 Agent guidance: entrypoints, rules, hooks, skills, references, indexes and docs

| File | Lines | ~Tokens | Verdict | A strength | Main issues (line range: issue) | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `/home/crmarques/projects/gitops-workspace/bootwright/AGENTS.md` | 42 | 589 | good | Route-to-owner model with explicit 'do not bulk-read' (AGENTS.md:3-17); every pointer resolves to an existing file and anchor. | (1) 19-25: The rule says with no active slice only an explicit request authorizes a tracked edit, but also says to record discovered work as a milestone item. That record is itself a tracked edit unde… (2) 27-32: Slice authorization and 'never authorizes a push or product scope' are restated in code-implementation (SKILL.md:9, 13-16, 51). Three copies must be edited together, contradicting the page'… (3) 34-42: The command list omits `make race` and the independent-review duty for safety changes, which only code-implementation (SKILL.md:34-37) states. An agent that skims the entrypoint learns the… | Keep as is; resolve the record-versus-edit ambiguity in lines 19-25, link `make race` to the safety-change rule, and cite the Status and Scope rules headings of milestones.md instead of the whole page. |
| `/home/crmarques/projects/gitops-workspace/bootwright/CLAUDE.md` | 1 | 2 | good | One-line import (CLAUDE.md:1) keeps a single source of truth with no drift surface. |  | Keep as is. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/rules/guidance.md` | 12 | 109 | good | Small, with a valid `paths:` frontmatter that loads only when matching files are touched. | (1) 8-12: Restates AGENTS.md:15-17 (one owner, state in milestones, lessons in knowledge) and the skill-index rule for loading architecture-best-practices. Codex never reads .claude/rules, so the onl… (2) 2-7: The paths do not cover .claude/** (rules, settings, skill wrappers), although scripts/tools/focus.py treats .claude/ as a docs-check path (focus.py:25). Editing a rule or wrapper does not t… (3) 8: Names `make docs-check` as the finishing command while go.md and ansible.md name `make focus`. `make focus` already runs docs-check for guidance (focus.py:189-190), so three tiers are named… | Keep as is; add `.claude/**` to paths and say 'make focus (which includes docs-check)' so the tier is not ambiguous. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/rules/go.md` | 11 | 136 | good | Loads only for Go sources and module files. | (1) 7-8: Restates code-implementation's tier rule (SKILL.md:32-37) with less: it omits `make race` and review for safety-class Go code. The rule is the only prompt a Claude agent sees when editing G… (2) 7-11: Unlike ansible.md:9, it does not point destructive or effectful Go paths (substrate, lifecycle, contextfs, privilege) at security-safety. The safety class is defined in the skill but the Go… | Rewrite lines 7-8 to a bare pointer ('follow code-implementation and its Go reference') plus one security-safety sentence for safety-class code; drop the restated tiers. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/rules/ansible.md` | 9 | 108 | good | States the policy/effects split in one sentence with a resolving anchor. | (1) 5-6: Repeats the focus and check tiers already owned by code-implementation and verification.md, and omits the lint/units suite choice that makes the Ansible gate cost 3 to 5 minutes. Two copies… (2) 2-3: The path glob does not cover ansible-check inputs outside ansible/ (scripts/ansible-check, lock files), so those edits get no Ansible reminder. | Keep, but drop the restated tiers and keep only the three pointers. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/settings.json` | 55 | 349 | needs improvement | Allow list is confined to pinned wrappers and read-only git commands. | (1) 41-53: The Stop hook runs `make quick` on every stop with any diff or untracked file, but verification.md:8-14 limits quick to 'a Go change many packages import, before the gate' and measures it a… (2) 47: A failing `make quick` exits 2 (make's error status), which Claude Code treats as a blocking Stop that feeds stderr back to the agent. The command has no guard for a repeated stop and canno… (3) 47: Any untracked file, such as a scratch directory, triggers the full run. `make quick` skips Markdown readers and docs-check (quick-test header), so a guidance-only edit pays for vet and the… | Rewrite the hook section: run `make focus` (or only on a transition to a clean handoff), add an input guard against repeated stops, document it in code-implementation, add `make focus` to allow, and extend the ask list to lifecycle-affecting commands with glo… |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/skills/code-implementation/SKILL.md` | 7 | 108 | good | Frontmatter name and description are identical to the canonical file, and a test enforces this (docs_test.go:450-475). | (1) 6-7: The wrapper makes Claude load a stub then Read the canonical file, one extra tool call per invocation, and AGENTS.md:27-30 already links the canonical path directly. The description is stor… | Keep as is; optionally replace .claude/skills with a symlink to ../.agents/skills so the wrapper, its test and its hop disappear. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/skills/architecture-best-practices/SKILL.md` | 7 | 103 | good | Frontmatter is identical to the canonical file and gated. | (1) 6-7: Same stub-then-read hop as the other wrappers. The body is not checked by the gate, so a wrapper could later grow its own rules unnoticed. | Keep as is; same optional symlink replacement as the other wrappers. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/skills/security-safety/SKILL.md` | 7 | 102 | good | Frontmatter is identical to the canonical file and gated. | (1) 6-7: Same extra hop. Nothing in the always-on text tells an agent editing destructive Go code to load this skill; only ansible.md:9 does, so discovery depends on description matching. | Keep as is; add a pointer to this skill in go.md for safety-class code. |
| `/home/crmarques/projects/gitops-workspace/bootwright/.claude/skills/ux-and-apis/SKILL.md` | 7 | 86 | good | Frontmatter is identical to the canonical file and gated. | (1) 6-7: Same extra hop. No path-scoped rule points api/ or CLI changes at this skill, unlike Go and Ansible, so loading depends on the description and on skills/index.md. | Keep as is; same optional symlink replacement. |
| `.agents/skills/index.md` | 15 | 243 | good | Short routing table keyed by the kind of work, not by skill name. | (1) 3-8: The wrapper-sync guarantee is enforced in test/architecture/docs_test.go, not by a named gate, so the index gives no command that reproduces it. The index is reached only through AGENTS.md:… (2) 10-15: The code-implementation row says 'Verification ... commits, and integration' while the skill's description also claims specs, guidance and skills; a reader choosing by row could miss that s… | Keep as is; name the check (make docs-check) next to the wrapper claim at line 8. |
| `.agents/skills/code-implementation/SKILL.md` | 52 | 726 | needs improvement | Compact (about 730 tokens) and delegates detail to six references loaded on demand. | (1) 47-51: The body says 'Integrate through a pull request gated by CI' as an unconditional rule, while references/git-integration.md:22-32 makes the pull request only the default and allows local fas… (2) 13-20: Steps 1 and 2 restate AGENTS.md slice authorization and worktree/status rules (AGENTS.md:19-31), and a mandatory separate worktree is imposed even on a one-line guidance edit while git-inte… (3) 34-37: The safety-change class is defined only here as a parenthetical list ('contextfs code', 'lifecycle'), and verification.md:12 refers to 'a safety change' without a link. The list is not tied… | Rewrite the Commit and integrate section to say the pull request is the default and link git-integration.md for the variants, link the safety-change definition to one owner, and drop the duplicated step 1 text in favor of the AGENTS.md pointer. |
| `.agents/skills/architecture-best-practices/SKILL.md` | 42 | 584 | needs improvement | Short (42 lines, about 580 tokens) with correct relative links; the spec index, state-reconciliation page, milestones page, knowledge index and the architecture.md#depen… | (1) 3-3, 14-27, 29-42: The description promises 'variants', but the body never says how to choose among a typed variant, an adapter or renderer behind a port, and a version specialization (D162). That decision is… (2) 8-12, 29-34: The skill never points at the architecture sections that own what it asks reviewers to judge: the Go and Ansible responsibility boundary (specs/architecture.md:629), implementations and ver… (3) 14-18: The first bullet restates the artifact-store split already stated in specs/index.md:34-40 (Conventions), AGENTS.md and .claude/rules/guidance.md. Four copies of 'one owner per fact, deliver… | Rewrite sections: keep the file at or near its size, add one 'Choose the variation mechanism' bullet linking specs/architecture.md#implementations-and-version-variation (typed variant vs adapter vs version specialization), add one line linking the Go/Ansible… |
| `.agents/skills/security-safety/SKILL.md` | 45 | 662 | good | Small (45 lines) and routes to the owners (specs/security.md, state-reconciliation.md, code-implementation) instead of restating them; all linked paths and the #required… | (1) 18-21: The rule says the exact target is proved "again inside the installer", but the linked knowledge page says the agent installer offers no supported hook before erasure (a residual race), and… (2) 14-15, 17-34: The header says each rule "is, or must become, an executable guard" but no bullet says which guard exists, which is still missing, or where; security.md's required-proof table (674-698) is… (3) 28-34: Bullets on stage-and-rename, shared-to-exclusive lock re-proof and open-once-without-following-links restate facts owned by contexts.md, secrets.md and security.md, and a grep finds no sing… | Keep as is structurally; rewrite the first rule (line 18-21) to state the residual race instead of an in-installer proof, add owner links to bullets 3-7, and add a short section-routing line into specs/security.md. |
| `.agents/skills/ux-and-apis/SKILL.md` | 37 | 472 | good | Short, link-first, and states which spec owns each fact (lines 8-13) instead of restating CLI, output or API behavior. | (1) 20-24: Three of the six regression rules (validate vs the only implementation, 'no default always refuses at plan time', declared flag read or refused) have no owning spec section or gate cited; g… (2) 17-18, 21-22: The taxonomy rule duplicates specs/cli/output.md:762-770, which already names TestDiagnosticCodesMatchOutputSpec as the registry gate; the skill names a different test for the command catal… (3) 34-37: The compatibility sentence points to no anchor although specs/cli/output.md:1078 'Compatibility' lists the public contracts and the review trigger. 'Update specs, examples and tests in the… | Keep as is, with a small edit to the regression list: link each rule to its owning spec section or gate (add owners for the three ownerless rules in specs/cli.md or specs/api.md) and link output.md#compatibility. |
| `.agents/skills/code-implementation/references/agent-operation.md` | 102 | 1,536 | needs improvement | Quality-first rule is explicit: a cheaper model only where the result is equivalent, never a silently weaker substitute (lines 11-22). | (1) 66-91: The page says to revalidate rather than 'freezing price numbers or availability in repository guidance', yet freezes model IDs (gpt-6-luna, gpt-6.1-sol, gpt-6-astra) and six external URLs.… (2) 26-31 vs 45-50: The text says to run on a model's default effort, but the table prescribes Sonnet high, Opus medium, Fable medium and Haiku low without saying whether those are runtime defaults. An agent c… (3) 100-102: The guard against agents running apply/destroy rests on committed Claude Code settings. The ask rules match only the prefixes './bin/bootwright' and 'bootwright', while verification.md itse… | rewrite section 'OpenAI runtime': drop frozen IDs and URLs for a rule to read the live catalog; state that the table efforts are the explicit settings; widen the apply/destroy guard note to say it is Claude Code only and name the Codex equivalent; consider sp… |
| `.agents/skills/code-implementation/references/verification.md` | 62 | 858 | needs improvement | States plainly what make focus never proves (dependents, second vet platform, serial rerun caveat), so an agent does not mistake it for a gate (lines 43-56). | (1) 7-12 vs Makefile:57-64: This page defines Probe as a hand-chosen per-edit check and Focus as make focus. The Makefile comment and docs/development.md:85 call make focus 'the probe tier' and quick 'the inner-loop t… (2) 12; SKILL.md:35-37: The gate row requires make race for a safety change in substrate, managed OS or container-cluster code, but the race target covers only reconciliation, privilege and cmd packages (Makefile:… (3) 7-12, 58-62: The committed Stop hook runs make quick on any dirty tree at every stop (up to 300 s), but this page says quick runs only 'before the gate' for broad Go changes. The hook's cost and its eff… | rewrite section 'tier table and Probe': pick one tier vocabulary and apply it here, in the Makefile comment and in docs/development.md; move the measured timings out to the knowledge page that already covers inner-loop cost; add the Stop hook and scripts/chec… |
| `.agents/skills/code-implementation/references/git-integration.md` | 42 | 561 | good | Short and single-purpose; explicitly defers when to commit and what to verify to SKILL.md. | (1) 8-10, 28-31: No worktree location, naming or creation command is given, and the local variant never says where the fast-forward runs when the destination branch is checked out in the primary tree. With… (2) 23-26 vs SKILL.md:44-46: Pull request is the default variant and commits must pass check-commits in CI, but this page never names scripts/check-commits or the commit-range check before push. The squash step ('minim… (3) 21-26: Variants are selected implicitly; there is no rule for which applies when the harness gives no branch and a remote exists but the owner works locally. Agents may open a pull request where l… | keep as is, with two additions: how the local fast-forward is performed against a dirty primary tree, and a one-line pointer to scripts/check-commits before integration. |
| `.agents/skills/code-implementation/references/go.md` | 113 | 1,887 | needs improvement | Opens by naming the owners (architecture, security, formatting) and keeps the Implementation list as short imperative rules (go.md:3-6, 8-40). | (1) 52-78: A 27-line single paragraph of golden-file helper mechanics (helper names, byte rules, flag names) sits on the mandatory path of every Go edit although only a change to a persisted/frozen fo… (2) 94-113: The inventory of contract suites, doubles, harness files and 'ledgers only shrink' restates what specs/architecture.md owns under 'Architecture verification' (architecture.md:989) and names… (3) 20-23: 'Document any intentionally ignored error' can be read as asking for a comment on every ignored error, whereas the code clarity contract says 'Minimize comments' and keeps only rationale co… | split: keep Implementation and a short Tests-and-gates section in go.md; move the golden-file paragraph (52-78) to a testing/golden reference loaded only for format changes; replace 94-113 with one sentence linking architecture.md#architecture-verification, a… |
| `.agents/skills/code-implementation/references/ansible.md` | 74 | 1,202 | good | Tight, imperative and checkable (FQCNs, argv commands, expand_argument_vars, native Boolean conditions, no_log plus diff false), with prohibited uses named (shell, raw,… | (1) 46-50: no_log, diff false, trace disclosure and cleanup restate security.md rules (security.md:583-613 govern no_log and redaction) without a link. The reference is secondary to security.md but gi… (2) 1-10: Several required procedure steps (the pre-edit resolution list in 5-8) are one run-on sentence of eight nouns with no pointer to where each fact is found (frozen request, evidence, implemen… (3) 58-61: 'Use pinned repository entrypoints' never names ./scripts/ansible-check or its suites (units, lint), which verification.md already lists. A reader of this file alone cannot run the gates, a… | keep as is, with two small edits: link security.md for the no_log/diff bullet and name ./scripts/ansible-check suites in the Tests section. |
| `.agents/skills/code-implementation/references/formatting.md` | 119 | 1,507 | needs improvement | Clear ownership statement and exclusions (generated, vendored, locked, byte-exact content; effective YAML has its own rules) at formatting.md:3-8. | (1) 36-67: The YAML blank-line grouping rules are dense and several overlap (scalar blocks, non-empty mapping blocks, sequence records, envelope) and rely on 'manual review' because no tool checks the… (2) 3-5: The file governs YAML in specs, examples and ansible/ but is reached only through SKILL.md step 3 and ansible.md/go.md links; no path-scoped Claude rule matches *.yaml or specs/**/*.md. A C… (3) 93-102: The Ansible section restates and extends the YAML rules (play and task key order, block/rescue/always order) that belong with the Ansible reference's conventions; the layout rule 'name firs… | split by language: keep a short shared-whitespace and Go page, move the YAML grouping rules and example into a YAML section loaded for YAML/Ansible/example edits, and add a path-scoped Claude rule (yaml, examples, specs front matter) so the reference is route… |
| `.agents/knowledge/index.md` | 86 | 7,337 | needs improvement | Keyed by symptom, symbol and error text, which matches how an agent searches; all 61 pages are linked and every link resolves (checked). | (1) 9-72: Rows average about 300 characters in the first column and reach 1166; the table is about 29 KB, roughly 7.3k tokens, and AGENTS.md sends every agent to it. An agent that reads it whole inst… (2) 57-58, 63-64, 71-74: The same page is indexed in 2 to 4 separate rows (metallb-on-openshift, kubernetes-object-ownership-with-oc, four rows for gitlab-operator-on-openshift). The extra rows add table overhead b… (3) 10: The row advertises a '27-kind catalog' for api-alignment.md, but specs/api.md:262 says the catalog contains exactly 33 kinds. A grep for 'kind catalog' lands on a stale count that contradic… | rewrite section: group rows under domain headings, merge the duplicate rows per page, cap each row at about 40 words of the most distinctive terms, mark retired or history pages, and remove milestone ids and renamed symbols from keywords. |
| `.agents/knowledge/version-specializations.md` | 176 | 6,680 | needs improvement | Registry and page are held equal by TestDocsVersionSpecializationCatalogMatchesRegistries (test/architecture/version_specializations_test.go:183), so the table cannot si… | (1) 31-37: The active table has 12 columns; five rows hold 2.5 to 3.5 KB each, mostly the evidence cell (sha256 values, zipapp line numbers, SRPM hashes) and the test-name list. The rows cannot be rea… (2) 1-176: One page holds the catalog, the seam table, 31 'decided general' rows spanning SSH, storage readbacks, installer rules and milestone pointers, and the procedure. All of it loads when an age… (3) 117-148: Many 'Decided general' rows are not version cases but storage readback and API behaviour (S3 key format, bucket IDs, NFS readback, SMB) restated from specs/storage.md and storage-resources.… | split: keep a compact catalog (id, seam, variant, condition, matched builds, general-flow outcome, link) on this page; move evidence cells and the per-record notes to the existing ibm-ceph-version-constraints.md or one page per record, and move non-version 'D… |
| `.agents/knowledge/reconciliation-audit-2026-10.md` | 357 | 5,278 | needs improvement | States plainly that line citations drift and that symbol names are the stable reference (lines 7-8). | (1) 120-169, 173-303: The findings section restates the backlog entries for B567, B568, B572, B526, B248, B576 and about 30 others, including their decisions and wording, while specs/milestones/backlog.md rows 1… (2) 10-78: Most file:line citations are already stale: mutate is still at execution.go:132, but decideOver is now at :395 (page says :367) and settleApply at :542 (page says :480). The page disclaims… (3) 89-93, 122-145: The page says 'Destroy can break another tenant's environment today (B567)' and 'fresh apply ... only while real state is literally absent, which nothing checks'. This is the only place the… | move history out: keep 'How apply and destroy decide' and the load-bearing assumptions as a short durable page named by topic; replace the findings and decision text with links to the backlog items, keep refuted candidates, and strip line numbers in favour of… |
| `.agents/knowledge/api-alignment.md` | 60 | 936 | bad | Declares itself non-normative and names the API spec as authoritative (lines 3-7). | (1) 11-12: States 'The current desired-state catalog contains 27 kinds', but specs/api.md:183 and :262 say exactly 33 registered kinds. The page contradicts the owning spec on a headline number and is… (2) 36-60: The table lists retired shapes (InfraComponent, RedHatSubscription, HTTPProxy, CephCluster, DataFoundationStorageExport) as 'earlier proposed' and 'then-aligned'. It records a rejected prop… (3) 9-34: The 'current infrastructure-service shape' section describes live shape (explicit controller Machine, direct consumer references, StorageClient admission) as 'superseded' history. Current-s… | delete: remove the page and its index row (any rationale worth keeping is a few lines in delivered.md); if kept, fix the count to 33 or drop the current-state section. |
| `docs/development.md` | 244 | 4,008 | needs improvement | The verification table (lines 82-98) is a complete, command-keyed map of make targets and is the only place listing check-offline, race, vulncheck and completion-test. | (1) 11-58: Dependency versions are restated although go.mod and scripts/tools/go.mod own them, and only the ansible-core and Python statements are test-guarded. The guide already drifted: it says Char… (2) 11-43: The Toolchain section carries selection rationale and history ("explicitly selected for this skeleton", parser-qualification budgets) that duplicates .agents/knowledge/desired-state-admissi… (3) 87: The make docs-check cell is a single 190-word sentence that restates the milestone-page rules enforced by TestDocsMilestonePagesAgreeWithTheirStatus. specs/milestones.md owns those rules, s… | Split: keep the toolchain pointer, the verification table and the harness list (about 120 lines). Move version pins and rationale to go.mod and knowledge pages. Move the lock-refresh and image-qualification sections to a lock-and-images page, or to knowledge.… |
| `docs/acceptance.md` | 128 | 3,074 | needs improvement | States who may accept a row and the baseline-descent plus clean-source matching rule once (lines 22-28), and specs/milestones.md links to it instead of restating. | (1) 11-20 vs 28-34: The Row format table says Example, Commands, Outcome and Log digest carry full content, but D165 immediately below makes the public row a summary and allows 'Retained locally'. A writer fol… (2) 40-57: The setup v2 template targets B555, which the ledger itself shows accepted on 2026-10-15 (line 123); this is a template for a closed gate. It is a 1,000-character single-line table row that… (3) 59-106: Ceph matrix and physical-qualification templates and their locally retained field lists are per-item procedure for B484, B78, B70 and B485. These belong on the milestone item pages or the p… | Rewrite section 'Row format' to the D165 summary form. Move the per-item templates (lines 40-106) to the owning milestone items or runbooks, and keep this file to the policy, the row format and the append-only table. |
| `docs/operator-guide.md` | 641 | 9,186 | needs improvement | Procedures are concrete and cite the owning spec section for each behaviour (controller, cli, contexts, security), so rules are not re-specified in detail. | (1) 542-617: 'M1 closing validation' is a runbook for a gate the same section says is accepted and 'a re-qualification, not an open M1 requirement' (lines 556-557, 615-617). It carries X48, B339, B608,… (2) 271-316: The fresh-install section opens with an X96 libvirt-v5 caveat, then says 'the following store-reset procedure applies to the earlier setup v2 cut-over'. It ends with a D109 real-hardware se… (3) 450-540: 'Changing the build between runs' restates continuation, destroy-supersede, secret-binding loss and keyring-format rules (X74, X20, X21) that specs/state-reconciliation.md and .agents/knowl… | Split by task: host preparation (lines 10-316), network exposure (318-373), lab runs (375-448), build change and recovery (450-540) and record a run (619-641). Delete the M1 closing section and the legacy media cleanup, move the migration material to knowledg… |
| `README.md` | 41 | 436 | good | Short and routes by link: status to specs/milestones.md, behavior to specs/index.md, rules to specs/architecture.md; no delivery state restated. | (1) 3-10: The scope paragraph is a third restatement of purpose (with specs and AGENTS.md) and omits 'day-0 only / not a continuous controller / no day-2', the boundary the audit brief treats as cent… (2) 18-22: Quickstart runs 'make build' then validate without naming the pinned toolchain or setup prerequisite, while AGENTS.md mandates pinned wrappers and docs/development.md owns toolchain; a firs… (3) 28-34: 'Finding code' is useful to agents but README.md is not on the AGENTS.md mandatory route; the same map is partly owned by specs/architecture.md, so it can drift without a docs-check tying i… | Keep as is; add one sentence stating day-0 scope and non-goals by linking the owning spec section, and one line pointing the quickstart at the toolchain page. |
| `examples/README.md` | 103 | 2,490 | needs improvement | Directory-per-environment rule (validate one directory, not a category) is stated up front and matches specs/api/input.md discovery. | (1) 42-49: Stale instruction: 'Each name becomes a table row when its profile lands', yet all seven named profiles already are rows (lines 17-18, 19, 24-27). It also repeats the profile list that spec… (2) 16-19, 24-27, 34: Table cells carry delivery state ('live acceptance unrun', 'P4 acceptance remains unrun', 'qualification unrun', 'runtime qualification remains open') although line 56-57 says milestones ow… (3) 67-77: Lifecycle rules (context import freezes input, replace via destroy then context update, unfinished apply continues with same stage selection, do not delete evidence) restate CLI and context… | Rewrite sections: delete the stale profile paragraph, strip status text from table cells, shorten lifecycle text to links, and split contributing rules into a short skill or reference reachable from the skill index. |

## 13. Cross-check against the sibling audit

The sibling audit reviewed the same commit under a near-identical request. We read it only after our own findings were verified. Its 42 defect claims are 30 numbered findings (X1 to X6, R1 to R10, API-01 to API-08, G1 to G6) and 12 rule-table or prose corrections without a severity. Each claim was either mapped to a catalog finding of ours, or sent through the same refuter and calibrator pair we used for our own candidates. A split vote went to an arbiter.

### 13.1 Disposition of every sibling finding

"Unrated" marks a rule-table or prose item that carries no sibling severity. "Confirmed at X" is the severity our verification assigned, not the sibling's.

| Sibling id | Title (shortened) | Its severity | Disposition |
|---|---|---|---|
| X1 | Recovery can reinstall an installed machine when identity is unavailable | High | Confirmed independently at Medium |
| X2 | Substrate extension contract disagrees where bindings change | Medium | Covered by [F18-03] |
| X3 | Service responsiveness vs useful readiness | Medium | Confirmed independently at Low; residue matches our [R2-02-07] |
| X4 | Lost-response ownership recovery needs a remote-writer trust model | Medium | Covered by [F10-08] |
| X5 | Mechanical prose/comment reduction is not a quality metric | Medium | Refuted: the shrink-only comment ratchet is owner decision D101 and keeps rationale comments |
| X6 | Media cleanup authority broader than media ownership | High | Arbitrated: authority half refuted by D27; confirmed at Medium as "own-image identity defined two ways" |
| R1 | Name reuse and local claims do not prove remote ownership | High | Covered by [F10-03] |
| R2 | Recovery dependency retention follows setup history | High | Covered by [F13-05] |
| R3 | Storage bounds can consume capacity removal needs | High | Covered by [F09-02] |
| R4 | Backup offered as remedy without a restore contract | High | Confirmed independently at Medium (calibrator voted High) |
| R5 | Removal observation turns an unreadable target into no effect | High | Confirmed independently at High |
| R6 | Read-only effect budget has contradictory owners | Medium | Arbitrated: owner conflict refuted; confirmed at Medium as an undefined "read-only command" |
| R7 | Durable-format statements reject the published format | High | Covered by [F13-01] |
| R8 | Readiness and inspection blur evidence, access and inventory | Medium | Covered by [F12-07] |
| R9 | Fixed concurrency contradicted by unbounded multi-SSH sessions | Medium | Confirmed independently at Medium |
| R10 | Duplicated lifecycle clauses disagree on recovery and retries | Medium | Covered by [F09-04], [F09-05] |
| API-01 | Authoritative storage pages disagree about what executes | High | Covered by [F05-02], [F05-08], [F15-03] |
| API-02 | Environment selection has an incomplete storage closure | High | Confirmed independently at Medium; same defect as our [R2-01-04] |
| API-03 | Accepted fields and unusable defaults | High | Covered by [F05-06], [F03-04], [F05-09] |
| API-04 | Interrupted Ceph deployment outside the continuation promise | High | Arbitrated: confirmed at Medium; recovery itself is tracked as B659 and B662 |
| API-05 | Schema/runtime compatibility needs a survival contract | High | Covered by [F03-09] |
| API-06 | Separate operator version choice from native prerequisites | High | Covered by [F06-10], [F17-01], [F17-08] |
| API-07 | Public names inherit hidden length constraints | Medium | Covered by [F04-11] |
| API-08 | Initial focus needs a complete cross-domain profile | High | Covered by [F01-01], [F10-04] |
| G1 | Vendor-specific model routing as mandatory policy | Medium | Covered by [F14-09] |
| G2 | Dirty-tree Stop hook undermines the inner loop | Medium | Covered by [F14-02] |
| G3 | Verification is temporal/path-based, not change-class | Medium | Covered by [F14-05], [F14-06], [F14-11] |
| G4 | "An executable rule's prose goes" confuses authority and verification | Medium | Confirmed independently at Low |
| G5 | Scope discipline needs delivery modes and inseparable repairs | Medium | Arbitrated: confirmed at Low, narrowed to the ban on fixing a blocking prerequisite |
| G6 | Milestone/history retrieval is expensive | Medium | Covered by [F15-01], [F17-03], [F15-10] |
| RULE plan bytes | Aggregate request bytes exceed the plan-record bound | Unrated | Confirmed independently at Medium, as stale 1 MiB prose, not a future risk |
| RULE root lock | Broad root lock limits concurrent status and contexts | Unrated | Refuted: the root lock is released after the snapshot (specs/contexts.md:377-389) |
| RULE cancellation | Cancellation claims must state what stopped | Unrated | Refuted: cancellation already yields durable unknown; the remote-worker window is tracked as B573 |
| RULE secret disclosure | Secret-info selection and disclosure wording disagree | Unrated | Covered by [F16-10] |
| RULE deferred parent proof | Deferred reconciliation treats parent observation as optional | Unrated | Arbitrated and refuted: every effect revalidates identity (specs/state-reconciliation.md:1812-1814) |
| RULE credential revocation | Revoked credentials can strand continuation | Unrated | Confirmed independently at Medium; overlaps our [R2-04-01] |
| RULE cross-context placement | Host-local claims; uncoordinated SSH placement | Unrated | Covered by [F10-03] |
| RULE handoff freshness | Handoff freshness; per-cluster vs whole-environment | Unrated | Covered by [F12-01], [F12-04], [F01-04] |
| RULE disk selector | Device path is not immutable disk identity | Unrated | Covered by [F10-01] |
| RULE add-on residual grants | Retirement leaves effects and grants on retained clusters | Unrated | Covered by [F10-04] |
| RULE managed-service sharing | Remote-host sharing and socket-release gaps | Unrated | Refuted: both gaps are stated and tracked (B247, B323, D102) |
| RULE inverse retained | Every inverse must state what it retains | Unrated | Arbitrated and refuted: the general rule exists (specs/state-reconciliation.md:774-779) |

Totals: 23 sibling claims map to findings we had already verified. Three more were confirmed and turn out to be the same defect as a round-two finding of ours that the mapping missed. Ten were confirmed and are absent from our catalog. Six were refuted, two of them by arbitration.

### 13.2 Where the two audits agree

The audits agree on the overall verdict: keep the architecture and the safety principles, and fix contradictory contracts before adding providers. Both name the same structural defects. Name-derived ownership is not proof of remote ownership [F10-03]. Consumers dispatch on substrate arms, so a new arm is not low-impact [F18-03]. Storage is described on several pages that disagree [F05-02] [F05-08]. Fields are admitted and then ignored [F05-06]. The contexts page went stale after setup v2 [F13-01]. Vendor-specific support claims enter the specs against D157, D159 and D163 [F17-01]. The Stop hook runs on the wrong tree [F14-02], and milestone pages cost too much to read [F15-01]. They also agree on what is good: strict decoding, closed implementation selection with no ambient plugins, custody kept apart from the request, positive physical identity proofs and explicit irreversible authorization. Agreement between two independent passes on the same text raises confidence that these are the defects to fix first.

### 13.3 Where they disagree, and why

Six sibling claims were refuted, mostly because the text already says what the claim asks for. X5 treats the shrink-only comment ratchet as a bad metric, but the owner accepted that ratchet in D101 (specs/milestones/backlog.md:3155). The root-lock item describes a superseded design: an operation now snapshots under the root lock, then "Readers and other contexts may proceed" (specs/contexts.md:379). Cancellation already resolves to a durable unknown; its one open window is tracked as B573. Managed-service sharing and socket release are stated limitations, tracked as B247 and B323 under D102. The deferred parent-proof item misreads an optional pre-admission read as the identity proof; every effect still revalidates identity. The inverse-retained item asks for a rule that specs/state-reconciliation.md:774-779 already states.

Severity is the second axis. The sibling rates higher where our verification found a bounded consequence. X1 repeats only this request's installation on the same Machine, behind the boot-time identity proof, so it lands at Medium. For X6, D27 decides that a removal ejects foreign media from a declared node, which leaves only the identity contradiction. API-02 reaches the operator less severely than claimed, so it rates Medium. API-04 restates parked items B659 and B662. For covered items, the sibling rates R2, R3, API-03, API-05 and API-08 one or two levels above our [F13-05], [F09-02], [F05-06], [F03-09] and [F01-01]. We rate three items higher than the sibling: the Stop hook [F14-02], undecidable milestone scope [F17-03], and device-path erase [F10-01], which the sibling lists only as an unrated rule note. R5 is the one independent sibling claim both passes rate High. Its rule reads an unreadable removal target "as no effect" (specs/state-reconciliation.md:544-548), against the unknown rule in specs/security.md:33-35.

### 13.4 What each found that the other did not

The sibling contributes ten verified findings our catalog lacks: X1 and R5 on reading missing evidence as no effect, R4 on backup remedies with no restore contract, X6 on own-image identity, R6 on the undefined read-only class, R9 on unbounded exec fan-out, API-04 on the continuation promise for Ceph, the stale plan-record bound, and G4 and G5 in the agent guidance. One is High, seven Medium and two Low. The section 14 roadmap should carry them with the same weight as catalog findings. R5 belongs with the reconciliation fixes in section 8.

Our catalog contributes 163 of its 201 findings that the sibling did not raise: 2 High, 57 Medium and 104 Low. Both High findings are reconciliation safety defects. StorageExport retirement can destroy a surviving consumer's storage [R2-02-01]. External libvirt attachments claim nothing, so two contexts can share a managed bridge [R2-06-01]. The Medium and Low findings sit mostly in reconciliation rules (56), the desired-state API (33) and architecture (25). They reflect the per-kind field review and the per-file ledger that the sibling covers more briefly. The sibling, in turn, contributes a compact observation model (complete, absent, owned incomplete, foreign, unknown) that our section 8 can adopt as a summary of rules the specs already hold.

## 14. Ordered remediation roadmap

This roadmap orders the 201 verified findings into four phases. It does not authorize work: under AGENTS.md, each step still needs an explicit owner request or an active slice, and each step below names the recorded decisions it touches so the owner can make that request. Steps carry a code (P0-A and so on) so milestone items can cite them.

### 14.1 Ordering principles

The order follows three observations from the catalog.

1. The catalog has no Blocking entry, but nine High entries and fourteen unsafe-rule entries cluster on destructive paths, shared hosts and the agent harness. These are the only findings whose cost grows with each new effect, so they come first.
2. The architecture contract is strong at the engine seam and weak at implementation selection. The F06 assessment shows that adding a substrate today edits every consumer's dispatch table [F18-03], so the specialization and selection contract must precede any new family member.
3. The corpus restructuring is cheap to defer but expensive to do twice. It follows the contract edits of Phases 0 and 1, which rewrite many of the same pages.

What is good and should be kept through every phase: the lifecycle Capability port, the frozen-plan identity of each block, the per-capability refusal tables held by tests, the physical-erase contract (the template for every other destructive path), the "unknown is never absence" rule, the docs gates with byte ratchets, and the one-owner-per-fact model. No step below weakens any of them.

The sibling-audit claims that our verification confirmed (section 11.4) are not catalog entries; section 14.8 schedules them separately. Two of them overlap catalog entries and travel with them: the sibling's credential-revocation item with [R2-04-01], and its storage-closure item with [R2-01-04].

### 14.2 Phase 0: before new implementation or destructive-path extension

Exit for the whole phase: every High finding is closed or has an owner-accepted carrier item, and no unsafe-rule finding is open without a refusal in its place.

| Step | Findings | Owner decision | Size | Depends on |
| --- | --- | --- | --- | --- |
| P0-A Agent effect safety | [F14-01] [F14-02] | No; consistent with D138 | S | none |
| P0-B Owning pages tell the truth | [F13-01] [F17-01] [F17-03] [F15-02] | Yes for the WIP limit (D158) | M | P0-A |
| P0-C Physical target identity | [F10-01] [F10-05] [F10-02] [R2-06-03] | No; reuses D177 identity | M | P0-B |
| P0-D Shared-host ownership | [F10-03] [R2-06-01] [R2-06-04] [R2-06-02] | Yes: D38 amendment for [R2-06-02]; D155 direction | M | P0-B |
| P0-E Consumer quiescence | [R2-02-01] [F10-04] | Confirm D143 clause 6 reading | S | none |
| P0-F Access-credential rebind | [R2-04-01] | Yes: new exit beside D37 | M | none |
| P0-G Store durability and locks | [F09-02] [F13-07] [F11-07] [F13-02] | No; carriers B572, B389 | M | P0-B |
| P0-H Truthful admission and boundaries | [F05-06] [F08-01] [F12-06] [R2-05-11] | No | S | none |

#### P0-A Agent effect safety

Move the ban on agent-run effectful commands into AGENTS.md and widen the ask patterns in `.claude/settings.json` to any path form of the binary [F14-01]. Replace the Stop hook with a guarded, advisory script that honours `stop_hook_active` and skips the primary checkout [F14-02]. This step is first because every later step is executed by agents under these rules.

Exit: AGENTS.md carries the sentence; agent-operation.md no longer owns it; ending a turn in the primary checkout with an untracked file runs no check.

#### P0-B Owning pages tell the truth

Correct contexts.md to registry version 6 and remove setup from the exclusive-lock rows [F13-01]. Remove every vendor-support clause from the storage coverage family, implementing D157, D159 clause 15, D163 and D176 [F17-01]. Add one Active slices table to milestones.md and record M2c as paused per D168 [F17-03]. Reclassify the six-host cap as an unmet target under D130 [F15-02].

Exit: the [F17-01] grep acceptance returns nothing; a docs-check test compares record-version cells with Go constants; milestones.md names each active slice with its authorizing decision. Retiring or reshaping the WIP limit stays with the owner.

#### P0-C Physical target identity

Require a durable disk identity at physical managed-OS installation and compare it inside `%pre` [F10-01]. Define one MAC-set predicate, equality, used by target proof, `%pre` and erase [F10-05]. Refuse power commands on the controller Machine and on unpinned targets without a fresh full proof [F10-02]. Add the `bmc-system:<UUID>` key beside the address-keyed claim [R2-06-03].

Exit: the security-safety skill bullet matches; managed-os.md states the `%pre` comparison; tests are named for the identity mismatch, the controller-Machine refusal and the self-identity refusal.

#### P0-D Shared-host ownership

Spec-only first: link B568 as a known exception under Mutation safety and add D155's interim orphan warning to contexts.md [F10-03]. Then the `bridge-use:` shared key, as B567 proposes [R2-06-01], the emulated-BMC claim [R2-06-04], and key release on abandoning deletion [R2-06-02], which needs the owner to amend D38.

Exit: grep finds B568 in both pages; the key table lists `bridge-use:` with its release-blocking rule; a named test refuses removing a bridge another context uses.

#### P0-E Consumer quiescence

Make export and cluster retirement conditional on every consumer being retired in the same removal, otherwise refuse under B491 naming the consumer [R2-02-01] [F10-04]. The RBD-replication rule at storage-resources.md is the model.

Exit: the export refusal table has the "consumer cluster survives" row and a planner test cites it.

#### P0-F Access-credential rebind

Split Secret consumption into effect material and access credentials, and give access credentials an explicit, recorded rebind exit [R2-04-01]. This adds an exit beside D37 and keeps D85, so the owner decides the flag.

Exit: every `*Ref` to a Secret in specs/api lists its class; a test rotates a BMC credential and proves destroy succeeds only with the flag while the ownership proof still runs.

#### P0-G Store durability and locks

Reserve the removal's budget for the removal alone, folded into B572 [F09-02]. Refuse tmpfs and overlay production roots [F13-07]. Move scratch under a root-owned parent through B389 [F11-07]. Confirm context update and delete before the exclusive lock and re-prove under it, as D118 and D76 do for other commands [F13-02].

Exit: the acceptance test of each finding exists and is cited from its spec clause.

#### P0-H Truthful admission and boundaries

Refuse authored non-default inert endpoint fields at plan or remove them [F05-06]. Name the remote fixed-worker exception in security.md with one quoting function [F08-01]. Say in the context-update warning that restoring files does not restore settlement [F12-06]. Add license acceptance to the Ceph example READMEs [R2-05-11].

Exit: each finding's acceptance clause holds; all four are page-local edits plus small tests.

### 14.3 Phase 1: additive-extension contracts and owner shape decisions

Phase 1 makes "add a substrate, distribution, storage provider or service" an additive change. Steps P1-A to P1-D are architecture; P1-E to P1-H need owner decisions before an agent can start.

| Step | Findings | Owner decision | Size | Depends on |
| --- | --- | --- | --- | --- |
| P1-A Selection and extension contract | [F06-01] [F06-03] [F06-04] [F06-06] [F06-12] [F06-08] | No; D153 clause 3 compatible | M | Phase 0 |
| P1-B Substrate single dispatch | [F18-03] [F18-07] [F07-07] [F04-04] | No; D1 kept | L | P1-A |
| P1-C Family-neutral cores | [F07-03] [F06-05] [F06-07] [F18-04] [F18-09] [F18-10] [F17-06] | No | L | P1-A |
| P1-D Specialization contract | [F06-09] [F06-10] [F06-11] [R2-02-04] [F17-08] | No; reconciles D162(a) and (b) | M | P1-A |
| P1-E Plan edges and selection closure | [R2-01-01] [R2-01-02] [R2-01-04] [R2-01-05] [R2-01-03] [R2-01-06] [R2-01-07] [F03-08] | Yes for [F03-08] with [R2-01-03] | M | none |
| P1-F Cross-build continuation | [R2-03-01] [R2-03-02] [R2-03-03] [R2-03-06] [R2-03-05] | Yes: amends D145(4) | M | P0-B |
| P1-G API shape decisions | [F18-02] [F05-03] [F03-01] [R2-05-01] [F04-01] [F04-02] [F03-03] [F05-07] [F05-02] [R2-05-04] | Yes: D132, D133 | L | P1-A |
| P1-H Scope and milestone dependency | [F01-01] [F17-02] [F01-02] [F17-04] [F01-04] | Yes: D158, D172, D153 | S | P0-B |
| P1-I Reconciliation rule completion | [F09-01] [F09-03] [F09-04] [F12-01] [F12-02] [F12-03] [F13-03] [F13-04] [R2-02-03] [R2-06-05] [R2-06-06] | Yes for [F09-01] (D38) and [R2-06-06] (B247, D105) | M | Phase 0 |
| P1-J Trust model and custody | [F11-01] [F11-02] [F11-03] [F11-08] [R2-04-02] [R2-04-03] [R2-04-04] | Yes for [R2-04-02] (D143 clause 7) | M | P0-F |
| P1-K Language responsibility | [F08-04] [F08-06] [F08-07] [F08-02] [F08-03] [F08-05] [F08-08] [F08-09] [F08-10] | No; codifies D11, D99, D170 | M | none |

Exit criteria a reviewer can check:

- **P1-A.** architecture.md has "Adding an implementation of an existing capability" with a touch list. An engine claim check refuses overlapping or missing claims. A `test/architecture` rule forbids implementation and arm constants outside their owning package and composition, with an allowlist that only shrinks [F06-03].
- **P1-B.** One substrate-owned dispatcher role replaces the per-consumer arm tables. `machine stop` reaches KubeVirt through the substrate port. A synthetic second arm compiles with no consumer edit.
- **P1-C.** Storage and cluster contexts each have a neutral core and an implementation package. Composition carries no Ceph policy into Managed OS except through a fan-in port. Coverage pages are named for their implementation.
- **P1-D.** The refusal-seam rule moves from knowledge into architecture.md. No registry is keyed by a release label. Registry discovery uses a marker, not two hard-coded paths.
- **P1-E.** state-reconciliation.md holds one reference-edge catalog, and each domain page links to it. The closure row reads "every storage child whose clusterRef names it".
- **P1-F.** Identity-fixing values travel in the frozen request. Each implementation declares a mark format id and a proof id. The Ansible and Go references carry the digest checklist item.
- **P1-G.** One recorded owner decision covers the ContainerCluster distribution union, `Storage*` versus `Ceph*` names, the union idiom rule and the admission classification. A decision before B87 and B99 revive arms costs least.
- **P1-H.** project.md separates the committed portfolio from the extension envelope. Requires lines state real dependencies only, and session clauses sit in one review queue.
- **P1-I.** Each rule finding's acceptance holds. [F09-01] stays open until the owner reopens D38.
- **P1-J.** security.md has a remote-principals table. Downloaded tools name an authenticity source. `--yes` refuses an unnamed host-key fingerprint.
- **P1-K.** architecture.md has a "Language responsibility" section with a Python row. A python.md reference exists. A placement test maps every tracked source file to a language row.

The P1-G decision is the most time-sensitive owner input in the roadmap. M3 and M4 work keeps building on the flat ContainerCluster shape, and pre-1.x the reshape costs no converter (api.md Version authority, D142) [F18-02].

### 14.4 Phase 2: corpus and skill restructuring

| Step | Findings | Owner decision | Size | Depends on |
| --- | --- | --- | --- | --- |
| P2-A Milestone cards | [F15-01] [F15-09] [F15-10] [F15-06] [F15-04] | Yes: log location (milestones.md) | M | P0-B, P1-H |
| P2-B Section routing and gates | [F15-07] [F15-08] [F15-03] [R2-02-05] [F16-03] [F17-07] | No | M | P2-A |
| P2-C One storage field-record home | [F05-08] [F17-05] [F13-09] | No; keeps D133 split | M | P0-B, P1-G |
| P2-D Skill portfolio | [F14-03] [F14-05] [F14-06] [F14-07] [F14-09] [F14-11] [F14-12] [F14-04] | No; F14-03 implements D168 | M | P0-A, P1-F, P1-K |

Exit criteria:

- **P2-A.** Each milestone page is at most 16 KB; waiver history sits in decisions and delivered records; AGENTS.md mandates the item row and its section, not the page. On the F15 measurement, that read is 60 to 76 percent of a slice task's mandatory tokens today.
- **P2-B.** specs/index.md has section rows for the large domains. Docs gates enforce section budgets, index reachability and an identifier-prefix registry. The stale storage review page is indexed as history or removed.
- **P2-C.** Each storage kind has one field-record page; api/storage.md keeps the kinds, references and cross-kind invariants.
- **P2-D.** git-integration.md leads with local fast-forward per D168. One safety change class covers every capability. Verification tiers have one owner. The wrapper parity test fails on a missing or orphan wrapper. Model-routing tables leave the tracked reference.

### 14.5 Phase 3: batched Low clean-ups by owning page

Each batch is one change to one owning page or page pair, run by an implementation-tier worker with one reviewer. None needs an owner decision unless marked.

| Batch | Owning page | Findings | Size |
| --- | --- | --- | --- |
| L1 Scope prose | project.md, README.md | [F01-03] [F01-05] [F01-06] [F01-10] [F01-11] | S |
| L2 Glossary and naming rules | specs/index.md glossary, api.md | [F02-01] [F02-02] [F02-03] [F02-04] [F02-05] [F02-06] [F02-07] [F02-09] [F02-12] [F12-05] [F05-05] | M |
| L3 CLI vocabulary and output | cli.md, cli/output.md, cli/commands.md | [F16-01] [F16-04] [F16-05] [F16-06] [F16-07] [F16-08] [F16-11] [F12-07] [F12-09] | M |
| L4 API grammar | api.md, api/input.md | [F03-02] (Medium) [F03-04] [F03-05] [F03-06] [F03-07] [F03-09] [R2-05-05] [R2-05-09] | S |
| L5 Machines and services | api/machines.md, api/infrastructure-services.md | [F04-05] [F04-06] [F04-07] [F04-08] [F04-09] [F04-10] [F04-11] [F18-06] [R2-02-06] [R2-02-07] | M |
| L6 Add-ons and playbooks | api/addons.md, api/custom-playbooks.md | [F05-04] [F05-09] | S |
| L7 Secrets and custody detail | secrets.md, api/secrets.md | [R2-05-07] [F11-09] [F11-11] [R2-04-05] [R2-04-06] [R2-04-07] [R2-04-08] [F16-10] | M |
| L8 Recovery wording | state-reconciliation.md | [F09-05] [F09-06] [F09-07] [F09-08] [F10-06] [F10-07] [F10-08] [F12-04] | M |
| L9 Host security | security.md | [F11-04] [F11-05] [F11-10] [F11-12] | S |
| L10 Controller records | contexts.md, contexts/controller-record.md | [F13-05] [F13-06] [F13-08] [R2-06-07] [R2-06-08] | S |
| L11 Examples | examples/README.md and scenario READMEs | [F05-10] [R2-05-03] [R2-05-08] [R2-05-10] [F16-09] | S |
| L12 Corpus leftovers | specs/index.md, coverage pages | [F07-10] [F07-08] | S |

L8 should follow P1-I, and L7 should follow P1-J, because both rewrite the same sections. [F16-11] adds a public flag and needs owner acceptance under D153 clause 2.

### 14.6 Do not do yet

| Work | Wait for | Why |
| --- | --- | --- |
| A new substrate arm (vSphere, AWS, GCP, Azure) or reviving KubeVirt and vSphere arms (B87, B99) | P1-A, P1-B, P1-G | Each consumer's dispatch table would change [F18-03]; the Machine arm shape is undecided [F04-01] [F04-04] |
| A second cluster distribution | P1-G decision on [F18-02], P1-C | ContainerCluster and its context are OpenShift-shaped [F18-02] [F07-03] |
| A second storage provider, including Red Hat or open-source Ceph | P0-B, P1-C, P1-G | Coverage carries vendor-conditional wording [F17-01]; kinds are Ceph-only under neutral names [F05-03] |
| Shared-host destroy or any new destructive path | P0-C, P0-D, P0-E | Same-name adoption and bridge sharing are unguarded [F10-03] [R2-06-01]; D155 orders the guard first |
| Rename the `Storage*` kinds to `Ceph*` | Owner decision under D133 | D133's review argued against renaming for uniformity [F05-03] |
| A `--abandon <block>` exit | Owner reopens D38 | Conflicts with D38 as recorded [F09-01] |
| Changing the WIP limit or session-clause force | Owner decision | D158 keeps the limit [F17-03] [F17-04] |
| Any support-status, release or pairing table | Never | D157, D159, D163 forbid it [F17-01] |
| Milestone-page restructuring | P0-B and P1-H | Both rewrite the same pages first [F17-03] [F01-01] |

### 14.7 Quick wins

These are single-page edits that close or unblock more than one finding.

| Edit | Closes | Size |
| --- | --- | --- |
| `.claude/settings.json`: guarded Stop hook plus widened ask patterns | [F14-02] [F14-01] (with one AGENTS.md sentence) | S |
| contexts.md: registry version 6, setup lock rows, D155 orphan warning, settlement warning | [F13-01] [F13-09] [F10-03] (part 2) [F12-06] | S |
| milestones.md: Active slices table, M2c paused, identifier schemes defined | [F17-03] [F15-06] | S |
| storage-coverage.md family: delete support-status clauses, reclassify the six-host cap | [F17-01] [F15-02] | S |
| api/storage.md: StorageReplication row, references, mirroring sentence; api/environment.md closure row | [F05-02] [R2-01-04] [F02-04] (count) | S |
| security.md: remote fixed-worker exception, consumer-file mode, one proxy-route statement | [F08-01] [F11-10] [F11-12] | S |
| git-integration.md and code-implementation SKILL.md: D168 default, one safety class, digest checklist | [F14-03] [F14-05] [R2-03-03] | S |
| Ceph example READMEs: license acceptance line | [R2-05-11] | S |

The first three rows remove the facts most likely to mislead an agent on its next task, at a total size of three small edits.

### 14.8 Sibling-confirmed items

Section 11.4 lists the sibling audit's claims that our own refuter and calibrator confirmed. They carry the same weight as catalog entries and slot into the phases above as follows.

| Item | Severity (ours) | Phase and step | Why there |
| --- | --- | --- | --- |
| CX-R5: a removal observation that cannot tell the postcondition from an unreadable target reads as no effect | High | Phase 0, with P0-C and P0-D | It is the one sibling claim both audits rate High, and it violates the "unknown is never absence" rule every Phase 0 step relies on; the correction is a two-sentence change in the removal observation clause and a named negative test. |
| CX-X1: recovery can reinstall an installed Machine when its identity channel cannot answer after a power-off | Medium | Phase 0, with P0-C | Same target-identity family as [F10-01]; bounded to the same request's installation behind the boot-time proof, which is why it is Medium. |
| CX-X6: own-image identity is defined two ways between the cluster and substrate pages | Medium | Phase 1, P1-B | The single-dispatch rework rewrites the media entry points where the definition must be stated once. |
| CX-R4: backup is offered as a remedy without a restore contract | Medium | Phase 1, P1-I | Belongs with the operability corrections of the recovery rules. |
| CX-R6: the read-only command class is undefined, so the effect budget has two owners | Medium | Phase 1, P1-I | Fixed by one definition in the state contract that the CLI owner links. |
| CX-R9: multi-session SSH fan-out is unbounded against the fixed-concurrency rule | Medium | Phase 1, P1-J | A bound in the CLI session rule, stated beside the listener and process bounds. |
| CX-API02: explicit Environment selection has an incomplete storage closure | Medium | Phase 1, P1-E | The same defect as [R2-01-04]. |
| CX-API04: an interrupted Ceph deployment sits outside the general continuation promise | Medium | Phase 1, P1-I | Tracked as B659 and B662; the step records the exception in the continuation clause until those land. |
| CX-RULE plan bytes: the stale aggregate plan-record bound | Medium | Phase 3, L8 | A wording fix on the record bound. |
| CX-RULE credential revocation | Medium | Phase 0, P0-F | The same defect as [R2-04-01]. |
| CX-X3: service responsiveness versus useful readiness | Low | Phase 3, L5 | Joins [R2-02-07] on the managed-service readiness wording. |
| CX-G4: "an executable rule's prose goes" conflates authority with verification | Low | Phase 2, P2-D | One sentence in the architecture-best-practices skill. |
| CX-G5: scope discipline lacks a definition of an inseparable repair | Low | Phase 2, P2-D | One sentence in the code-implementation skill, narrowed to the ban on fixing a blocking prerequisite in the same change. |

## 15. Method and verification record

### 15.1 Process

The audit ran as two orchestrated multi-agent workflows under a Claude Fable 5.1 session that wrote the brief, designed the orchestration, arbitrated split verdicts, wrote the executive assessment and this record, and assembled and reviewed the report. Models were routed by task class as the repository's own agent-operation reference prescribes: Sonnet for bounded per-file assessment, Opus for finding, verifying, consolidating and writing, Fable only for arbitration and final judgment.

| Stage | Agents | Model and effort | What each did |
| --- | --- | --- | --- |
| Per-file ledger | 52 | Claude Sonnet 5.5, high | Read one file group fully (the six largest milestone and history pages structurally) and recorded purpose, verdict, strengths, issues, overlap and a recommendation for each of 80 files. |
| Lens finders, round one | 18 | Claude Opus 5.5, medium | Each read the owning sections of one audit question (scope, nomenclature, grammar, two kind groups, architecture contract, code materialization, language matrix, five reconciliation lenses, skills, corpus structure, CLI journey, coverage and milestones, extension scenarios) and reported at most twelve findings plus an assessment narrative. |
| Refuting verifier | 254 | Claude Opus 5.5, medium | One per candidate: opened every cited range, searched the specs for a resolving clause, checked for restated tracked items and deliberate decisions, and defaulted to refuted when the evidence did not support the claim. |
| Impact-and-remedy verifier | 254 | Claude Opus 5.5, medium | One per candidate: judged the consequence, calibrated severity on the brief's scale, checked the recommendation against the recorded decisions D1 to D177 and the project principles, and required checkable acceptance criteria. |
| Arbiter | 58 | Claude Fable 5.1, medium | Decided every split vote after reading the finding, both verdicts and the cited text. |
| Completeness critic | 1 | Claude Opus 5.5, medium | Compared the verified findings and the ledger against the brief and named six gaps. |
| Lens finders, round two | 6 | Claude Opus 5.5, medium | Covered the critic's gaps: the cross-domain interaction chain and selection closure, the immediate portfolio end to end, cross-build continuation and content digests, the secret path into native tools, unjudged kinds and examples, host reservations across contexts. |
| Sibling cross-check | 1 + 38 | Claude Opus 5.5, medium | Extracted the 42 findings of the sibling audit, mapped them to ours, and verified the 19 ours did not cover with the same two verifier lenses (6 of them arbitrated by Fable). |
| Consolidation and catalog | 2 | Opus (plan), Sonnet (render) | Grouped duplicates, assigned report sections, and rendered the catalog, the refuted list and the cross-check table. |
| Section writers | 11 | Claude Opus 5.5, medium | Wrote sections 3 to 10, 13 and 14 from the catalog, the finders' narratives and the ledger. |

Totals for the finding workflow: 646 agents, 5,413 tool calls, about 38.7 million tokens, 60 minutes of wall-clock time at a concurrency of 14. The section workflow added 11 agents. Every agent read the brief first, worked read-only against the repository at commit 17acb63d9, wrote intermediate material only to a scratch directory, and was barred from the `audits/` directory except the cross-check stage.

### 15.2 Verification outcome

| Measure | Value |
| --- | --- |
| Candidate findings (rounds one and two) | 235 |
| Confirmed by both verifiers | 183 |
| Split votes arbitrated | 52, of which 46 confirmed and 6 refuted |
| Refuted | 5 (listed below) |
| Duplicates merged after verification | 29 |
| Catalog entries | 201: 9 High, 70 Medium, 122 Low |
| Entries whose severity verification lowered from the finder's proposal | 116 |
| Sibling findings: covered by ours, confirmed independently, refuted | 23, 13, 6 |

Final severity is the lower of the two verifiers' calibrations unless an arbiter decided it, so the catalog errs toward understating severity: the one Blocking proposal (F10-01, the physical root-disk identity rule) carries High. Both verifiers reported medium or high confidence on every surviving entry; neither reported low confidence anywhere.

Considered and refuted: the claim that OKD is admitted nowhere while the scope lists it (F01-09, the premise was wrong); that managed services repeat the host address in `bindAddress` with a wildcard default (F04-03, a recorded decision and a tracked item); that the source-of-truth principle conflicts with day-2 GitOps control (F12-10, resolved by D153); that Codex receives no per-area guidance because path-scoped rules are Claude-only (F14-10, the consequence was wrong); and that "freezes the same blocks" means different things per verb (R2-03-04, the per-verb definition exists once).

### 15.3 Limits

- The audit judged written contracts and the architectural shape of the code (packages, ports, wiring, roles, plugins, fitness tests). It did not review function bodies, tests or Ansible task logic, and it executed no Bootwright command with effects. A rule judged sound here may still be implemented wrongly, and a rule judged unsafe may be guarded by code the specs do not mention.
- Verification was textual: verifiers read the cited passages and searched the specs; they did not run the test suites that hold some tables to code. Where a spec names such a test, the finding reports the spec's claim, not the test's result.
- The confirmation rate (230 of 235) is high. The refuters were instructed to default to refuted when unsupported, and 22 percent of candidates still needed arbitration, which argues against uncritical agreement; it remains possible that a verifier anchored on a well-cited finding. The owner should treat Medium and Low entries as well-evidenced claims to check at the cited lines, not as settled facts.
- Knowledge pages were read as observed facts, never as requirements, and only the pages the brief or a finder named were opened. Tracked examples outside `examples/wip/` were read for the API verdicts; the local-only override file and everything under `examples/wip/` were excluded from every output by rule.
- The sibling audit was read only by the cross-check stage after our verification had finished, so the two audits' first-round findings are independent; the cross-check confirmations are ours by verification, not by adoption.
- One harness observation was made directly by the orchestrating session rather than by an agent: on every turn end, the repository's Stop hook ran `make quick` because the tree held the pre-existing untracked `audits/` directory, and the gate failed at `fmt-check` on a root-owned directory under `examples/wip/` that the Go pattern `./...` cannot open. That is the behavior finding F14-02 describes, observed on an unchanged tree.
