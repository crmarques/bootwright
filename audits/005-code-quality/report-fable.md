# Code quality audit 005: Claude Fable report

[Audit 005](README.md) · [Audit directory format](../README.md)

- **Auditor:** Claude Fable 5.2, orchestrating Claude Opus 5.5 reviewers and
  one Claude Sonnet 5.5 reviewer, each given an explicit model and effort.
- **Date:** 2026-11-16.
- **Audited commit:** `d8d5672ef`. Main moved while the audit ran;
  [section 9](#9-after-the-baseline) covers the four later commits, up to
  `e0940ce15`.
- **Scope:** all the code the repository tracks: the Go module (`api`, `cmd`,
  `internal`, `test`, `scripts/tools`), the embedded Ansible collection (its
  Python plugins, roles, playbooks and templates, and its tests), the build,
  release and CI files, the repository tools, the end-to-end cases and the
  examples. The specs, the skills and the engineering references were read as
  the yardstick, not reviewed.
- **Question:** is this code well built? The owner asked for a review of code
  quality, security, efficiency, clearness, domain-driven design,
  non-duplication, use of the right language (Go, Ansible, Python, shell) and
  test coverage, followed by a report and a plan.
- **Result:** 474 findings from 45 review lanes: 11 High,
  207 Medium and 256 Low. The orchestrator re-checked every High
  finding and a sample of the others at the source
  ([section 8](#8-method-verification-and-limits)).

This report changes no product code, specification, milestone or decision. It
authorizes no implementation and no command with effects. Every
recommendation in it, the plan included, is a proposal for the owner.

Audit 003 asked whether the code does what the specs say. This audit asks a
different question, so it does not repeat those findings; where a reviewer met
one, the lane record cites its id.

## How to read it

- [Section 1](#1-verdict) is the verdict: what is good, what needs work, and
  the ten actions that matter most.
- [Section 2](#2-measured-baseline) gives the numbers behind it: size, test
  coverage, complexity, duplication and what the gates run.
- [Section 3](#3-scorecard) scores each lane on each of the eight dimensions.
- [Section 4](#4-findings-by-dimension) takes one dimension at a time.
- [Section 5](#5-defects-to-fix-first) lists the confirmed defects.
- [Section 6](#6-improvement-plan) is the plan: ten workstreams, their order,
  what each needs and how to tell it is done.
- [Section 7](#7-decisions-for-the-owner) lists the decisions only the owner
  can take.
- [Section 8](#8-method-verification-and-limits) says how the audit ran, what
  was verified and what was not.
- [Section 10](#10-findings-catalog) lists every finding in one table per
  severity. Any id in the text, such as [G14-01](#g14-01), links to its full entry in
  [Appendix A](#appendix-a-lane-records).

## Contents

1. [Verdict](#1-verdict)
2. [Measured baseline](#2-measured-baseline)
3. [Scorecard](#3-scorecard)
4. [Findings by dimension](#4-findings-by-dimension)
5. [Defects to fix first](#5-defects-to-fix-first)
6. [Improvement plan](#6-improvement-plan)
7. [Decisions for the owner](#7-decisions-for-the-owner)
8. [Method, verification and limits](#8-method-verification-and-limits)
9. [After the baseline](#9-after-the-baseline)
10. [Findings catalog](#10-findings-catalog)

[Appendix A. Lane records](#appendix-a-lane-records)

## 1. Verdict

**This is a well-built codebase, and the discipline behind it is unusual.**
The review found one functional bug in a shipped command, a short list of
defects with a security or data consequence, and no sign of carelessness.
What it found most is the cost of growing fast without consolidating: the
same code and the same rules written several times, in several places and
languages, and starting to drift apart.

### 1.1 What is good and must be kept

- **It fails closed.** Strict bounded decoding, typed refusals with a remedy,
  an unknown outcome never read as absence, positive proof before every
  destructive step. Almost every lane lists this as its first strength.
- **Security is designed in, not added.** Descriptor-relative file access in
  both languages, no process started through a shell on the controller, one
  qualified executable identity,
  secrets passed as references and cleared after use, pinned TLS and SSH,
  every download pinned by digest. Security is the strongest column of the
  scorecard.
- **The architecture is enforced, not only described.** 187 fitness tests
  hold dependency direction, effect boundaries, composition-only binding and
  the planner and executor partition, with allowlists that only shrink.
- **The tests are serious.** 6,206 Go tests and 11,184 Python tests reach 89%
  of the statements in each language, assert behavior by diagnostic code,
  run port contract suites against adapters and doubles, and interrupt every
  durable write in two harnesses.
- **The code is clean at the small scale.** No `TODO`, no lint suppression in
  Go, seven third-party imports, median function length 11 lines, comments
  that give reasons.

### 1.2 What needs work

In order of importance:

1. **Duplication that has started to drift** (lowest scorecard column). The
   lifecycle capability skeleton is copied per capability; security-critical
   helpers (process runners, ownership checks, tree removal, atomic writes)
   exist in two to ten copies with different behavior; and the same rule is
   often written in Go, again in Python and again in Jinja, tied together by
   a comment and not by a test. Two of the copies already disagree on
   outcomes ([G17-01](#g17-01), [G14-02](#g14-02)), and one copy of a bounded writer has a real
   defect its siblings do not ([X01-01](#x01-01)).
2. **Logic in the wrong language at the Ansible edge.** Procedure and
   judgment live in Jinja (the install wait's resume policy, cluster state
   classification, wrong-target identity rules), and Python re-derives or
   decides things Go owns (capability grants, budgets, product rules).
   Because roles are tested by rendering expressions and not by running
   tasks, this is also where defects reach the owner's live runs ([PT1-01](#pt1-01)).
3. **Coverage holes exactly where the stakes are highest.** The 175 Go
   functions no test runs include the only secret rebind path, both
   administrator-credential flows and the erase-receipt proof ([T02-01](#t02-01)). The
   least covered packages are the adapters that touch the host and cross
   privilege. The one functional bug found sits in such a hole ([G14-01](#g14-01)).
4. **Gates that guard structure but not these qualities.** Nothing measures
   coverage, complexity or duplication; no analyzer runs beyond `go vet`; the
   race detector covers four package trees; the commit gate does not run on
   the path this project actually delivers by.
5. **Large units and packed complexity.** A 224-method service, a
   14,700-line store type, a 4,865-line Python module, and functions that
   meet the 100-line rule while carrying complexity of 40 to 86.
6. **Repeated and serial work.** Independent hosts are probed one at a time
   in about a dozen places, and one command compiles the same input four to
   six times. Nothing here is unsafe; it costs time, and the owner's rule is
   parallel by default.
7. **Domain boundaries that leak at a few known points.** The desired-state
   tree is read through 3,687 string paths, the composition root and the CLI
   hold some domain rules, and one misplaced value (the host reservation)
   accounts for three of the nine context pairs that depend on each other in
   both directions ([X04-02](#x04-02)).

### 1.3 The ten actions that matter most

1. Fix the confirmed defects in [section 5](#5-defects-to-fix-first), each
   with a test that fails first. Start with [G14-01](#g14-01), [X01-01](#x01-01), [P07-01](#p07-01) and
   [G04-02](#g04-02).
2. Make coverage measurable and hold it: repair the one test that breaks
   under coverage ([G07-02](#g07-02)), then add per-package floors that only rise. Test
   the never-run security flows first ([T02-01](#t02-01)).
3. Widen the gates: a pinned static analyzer with an exclusion list that
   only shrinks ([T01-06](#t01-06)), the race detector on every package with
   concurrency ([T01-07](#t01-07)), the commit gate on push ([B01-01](#b01-01)), and a list of the
   most complex functions that only shrinks.
4. Build one hardened kit for operating-system primitives in Go and one in
   Python, and move every copy onto it ([T02-05](#t02-05), [X01-02](#x01-02), [X01-05](#x01-05), [G07-05](#g07-05),
   [G07-04](#g07-04), [P05-03](#p05-03), [P01-02](#p01-02)).
5. Give every rule one owner. Where two languages must each hold a copy, tie
   them with shared fixtures, as the erase regions and the firewall corpus
   already are; feed the Go request goldens to the Python validators
   ([P01-11](#p01-11)); and have Go freeze derived values so that Python and YAML stop
   re-deriving them ([G18-04](#g18-04), [P02-05](#p02-05), [Y01-02](#y01-02), [Y04-02](#y04-02)).
6. Extract the lifecycle capability skeleton ([G15-01](#g15-01), [G21-03](#g21-03), [G21-02](#g21-02)) and
   fold the artifact server into the managed service, in Go and in the role
   ([G17-01](#g17-01), [Y04-01](#y04-01)).
7. Move procedure out of Jinja into tested Python ([Y03-01](#y03-01), [Y03-02](#y03-02), [Y01-01](#y01-01),
   [Y01-03](#y01-03)) and judgment out of Python into Go ([G18-04](#g18-04), [P06-05](#p06-05), [P02-05](#p02-05)).
8. Run each role family end to end in the gate, twice in a row, so a first
   execution and a replay are proved through ansible-core ([PT1-01](#pt1-01)).
9. Probe independent hosts in parallel through one shared fan-out helper
   ([G13-01](#g13-01), [G13-02](#g13-02)) and compile once per decision ([G12-02](#g12-02), [G01-01](#g01-01)).
10. Reduce the large units in steps: per-command facts on the catalog
    ([G03-01](#g03-01), [G04-01](#g04-01)), the lifecycle service and its ports ([G12-05](#g12-05), [G12-04](#g12-04)),
    the context-store kernel ([G08-04](#g08-04)), the Ceph worker's collaborators
    ([P01-03](#p01-03)); guard context-level dependencies with a fitness rule and move
    host reservations to their owner ([X04-01](#x04-01), [X04-02](#x04-02)).

[Section 6](#6-improvement-plan) turns these into ten workstreams with an
order, sizes and exit evidence.

## 2. Measured baseline

Every figure in this section was measured for this audit at `d8d5672ef`, with
small tools written for it and kept out of the repository
([section 8](#8-method-verification-and-limits) describes them). None of
these figures is a gate today.

### 2.1 Size

| Code | Lines | Files |
| --- | ---: | ---: |
| Go production (`api`, `cmd`, `internal`, `ansible/*.go`) | 168,918 | 918 |
| Go tests beside the packages | 255,353 | 1,248 |
| Go fitness and completion tests (`test/`) | 13,685 | 49 |
| Python plugins of the collection (production) | 55,831 | 221 |
| Python tests of the collection | 91,941 | 284 |
| Ansible role tasks, defaults and vars | 16,210 | 205 |
| Ansible role argument specifications | 4,269 | 35 |
| Ansible playbooks | 1,402 | 92 |
| Jinja templates | 526 | 14 |
| Repository tools: Python, with tests | 4,305 | 10 |
| Repository tools: Go, with tests | 3,285 | 17 |
| End-to-end case templates and scripts | 24,386 | 541 |
| Examples | 28,124 | 1,056 |
| Goldens and fixtures | 93,753 | 810 |

The Go tests are 1.5 times the size of the Go production code and the Python
tests 1.6 times the size of the plugins. There are 6,206 Go test functions,
13 fuzz targets, no benchmark, 187 fitness tests under `test/architecture`,
and 11,184 collected Python tests. Production Go imports seven third-party
packages in all: the YAML library, `golang.org/x/sys/unix`,
`golang.org/x/crypto` (bcrypt and ssh), cobra, pflag and one terminal-width
library.

### 2.2 Test coverage

Go was measured twice: once with each package's own tests (`go test -cover`),
and once with every test in the module counted for every package
(`-coverpkg`). Python was measured with a recorder built on `sys.monitoring`
under the plain pytest loop; it sees only code that runs in the test process,
so a worker that the tests start as a child interpreter counts as not run,
and the Python figures are a floor.

| Measure | Statements | Run | Share |
| --- | ---: | ---: | ---: |
| Go, by each package's own tests | 76,827 | 64,720 | 84.2% |
| Go, by any test in the module | 76,827 | 68,698 | 89.4% |
| Python plugins, in-process | 33,449 | 29,878 | 89.3% |

- **Go functions that no test in the module runs:** 175, holding 1,032
  statements. With own-package tests alone the count is 715.
- **Where the unrun Go statements are:** `controller/bundlelocal` 135,
  `controller/privilege` 85, `storage/ceph` 74 and
  `containercluster/agentinstall` 67 (both are the administrator-credential
  flows), `storage/preflightlocal` 66, `secrets/localkeyring` 64 (the whole
  of `session.Rebind`), `storage/cephcluster` 60 (dead code, [G20-01](#g20-01)),
  `secrets/custody` 49.
- **Python by plugin kind:** module utilities 91.4%, action plugins 91.3%,
  filters 92.7%, modules 67.4%, the output callback 0% of 16 statements. 211
  functions (1,570 statements) never run in-process.
- **One Go test fails whenever coverage is on.**
  `TestSudoColorHintDoesNotReachSubprocesses` compares the combined output of
  a re-executed test binary byte for byte, and an instrumented child adds a
  warning line ([G07-02](#g07-02), [T02-06](#t02-06)). Coverage cannot run clean until it is fixed.

The least covered Go packages with at least 100 statements, by any test:

| Package | Own tests | Any test | Statements |
| --- | ---: | ---: | ---: |
| `internal/controller/bundlelocal` | 61.5% | 63.5% | 509 |
| `internal/controller/firewalllocal` | 75.5% | 75.5% | 302 |
| `internal/workspace/selectionfs` | 76.8% | 76.8% | 272 |
| `internal/storage/preflightlocal` | 72.0% | 78.2% | 472 |
| `internal/controller/hostowned` | 78.9% | 79.4% | 209 |
| `internal/workspace/invokerfs` | 79.5% | 79.5% | 332 |
| `internal/secrets/localkeyring` | 78.0% | 79.9% | 1,787 |
| `internal/controller/venvlocal` | 80.1% | 80.1% | 412 |
| `internal/controller/stagelocal` | 81.4% | 81.4% | 307 |
| `internal/controller/hostlinux` | 81.2% | 81.5% | 384 |
| `internal/reconciliation/logview` | 82.0% | 82.4% | 550 |
| `internal/machine/diskselection` | 74.6% | 82.5% | 228 |
| `cmd/bootwright` | 83.3% | 83.1% | 1,889 |
| `internal/workspace/contextfs` | 82.6% | 83.1% | 7,770 |

The pattern is plain: the adapters that touch the real host (the `*local`,
`*fs` and `host*` packages) are the least covered, because their entry points
hard-wire the root directory, `/proc`, system executables or an HTTP client
and leave no seam for a test ([T02-04](#t02-04)).

Some packages are covered mostly from elsewhere, chiefly through the
capability contract suite in `cmd/bootwright`: `internal/secrets` 42.7% own
against 94.9% by any test, `internal/diagnostics` 38.2% against 98.5%,
`infrastructureservices/managedservice` 65.3% against 93.0%, `api/v1alpha1`
76.0% against 96.4%. That is a sound design for contracts, with one cost: a
change to such a package cannot be verified by its own tests alone (T02,
Patterns).

One run of the suites on this host took 152 s for `cmd/bootwright`, 114 s for
`workspace/contextfs`, 58 s for `reconciliation/lifecycle`, 35 s for
`reconciliation/ansiblerunner`, 32 s for `secrets/localkeyring` and 29 s for
`desiredstate/inputfs`; the Python plain loop took 320 s. `t.Parallel`
appears in 3 Go test files out of about 1,300.

### 2.3 Complexity and size

| Measure | Go production | Python production |
| --- | ---: | ---: |
| Functions | 8,100 | 3,072 |
| Cyclomatic complexity: median, 90th percentile, 99th, maximum | 4, 13, 30, 86 | 4, 14, 31, 73 |
| Functions with complexity above 15 | 518 (6.4%) | 218 (7.1%) |
| Functions with complexity 25 or more | 158 | 64 |
| Functions with complexity 40 or more | 28 | 14 |
| Functions of 95 to 100 lines (the enforced ceiling is 100) | 24, of which 9 are exactly 100 | 2 |
| Functions with 10 or more parameters | 10 (maximum 18) | 3 (maximum 12) |

The fitness suite holds every production function to 100 lines. It holds
neither cyclomatic complexity nor nesting, and several of the most complex
functions sit exactly at the line ceiling: the limit was met by packing, not
by splitting ([G20-08](#g20-08), [G13-04](#g13-04)).

The most complex functions:

| Complexity | Lines | Function |
| ---: | ---: | --- |
| 86 | 99 | `api/v1alpha1/lexical.go` `ValidLexical` |
| 73 | 88 | `plugins/module_utils/ceph_native.py` `dispatch` |
| 70 | 98 | `plugins/module_utils/ceph_native.py` `Worker.observe_cluster` |
| 67 | 86 | `internal/cli/output_additional.go` `writeAdditionalStructuredResult` |
| 66 | 74 | `plugins/module_utils/ceph_native.py` `validate_hosts` |
| 61 | 72 | `plugins/module_utils/cluster_objects_engine.py` `Engine.chain` |
| 57 | 81 | `internal/secrets/localkeyring/localkeyring.go` `validateIndex` |
| 56 | 48 | `internal/storage/ceph/evidence.go` `EvaluateEvidence` |
| 55 | 86 | `internal/secrets/material/generation.go` `validateGeneratedCertificate` |
| 54 | 90 | `plugins/module_utils/ceph_core.py` `execute` |
| 53 | 99 | `internal/cli/validation.go` `validateCommandFlags` |
| 53 | 82 | `internal/managedos/installation/erase_unstarted.go` `PhysicalEraseUnstarted` |

The largest files are `ceph_native.py` (4,865 lines), `ceph_core.py` (1,955),
`ceph_host_runtime.py` (1,576), `ceph_native_object.py` (1,482),
`contextfs/repository_linux_amd64.go` (1,473), `ceph_native_resources.py`
(1,457), `operationstore/store.go` (1,316), `controller_files.py` (1,266),
`contextfs/media_linux_amd64.go` (1,195), `machine/admission.go` (1,151) and
`managedos/installation/kickstart.go` (1,040). The largest packages are
`reconciliation/lifecycle` (16,038 production lines in 67 files),
`workspace/contextfs` (14,693 in 38), `internal/cli` (13,236 in 82),
`storage/ceph` (12,282 in 49) and `managedos/installation` (9,398 in 41).

### 2.4 Duplication

The detector hashes windows of consecutive normalized lines (comments,
imports and punctuation-only lines dropped). "Exact" compares the lines as
written; "shape" first replaces identifiers and literals by placeholders, so
it also finds a copy whose names were changed.

| Corpus | Window | Normalized lines | In duplicated regions | Share | Regions |
| --- | ---: | ---: | ---: | ---: | ---: |
| Go production, exact | 10 | 104,208 | 753 | 0.7% | 33 |
| Go production, shape | 18 | 104,208 | 1,432 | 1.4% | 66 |
| Go tests, exact | 12 | 171,092 | 2,642 | 1.5% | 370 |
| Python production, exact | 8 | 41,343 | 2,242 | 5.4% | 381 |
| Python production, shape | 14 | 41,343 | 2,321 | 5.6% | 215 |
| Python tests, exact | 10 | 65,984 | 730 | 1.1% | 43 |
| Role tasks and playbooks, exact | 8 | 14,380 | 3,554 | 24.7% | 269 |

Three cautions apply to these numbers. First, most of the Python module share
is the Ansible `DOCUMENTATION` block that every module must carry, not logic
(P07, Patterns). Second, the role figure is real: nearly every role repeats
its entry preamble in its apply, destroy and observe files ([Y04-09](#y04-09), [Y01-09](#y01-09)).
Third, and most important, a line-window detector understates the
duplication that matters here. What the reviewers found again and again is
the same rule written a second time with other names, in another package or
in another language, which no clone detector sees
([section 4.6](#46-non-duplication)).

A second signal is helper names defined in many places: 57 free-function
names are defined in three or more Go packages (`failure` in 21, `refusal` in
12, `ContentDigest` and `DecodeRequest` in 9 each), and 79 names in three or
more Python files (`completion` in 25, `require` in 16, `validate` in 13).

### 2.5 Signals

A regex scan of production code. A count here is a lead, not a finding; the
cross-cutting lanes judged each class.

| Signal | Go | Python | Role and playbook YAML |
| --- | ---: | ---: | ---: |
| `TODO`, `FIXME`, `XXX`, `HACK` | 0 | 0 | n/a |
| Lint suppressions (`nolint`, `noqa`, `pylint: disable`, `type: ignore`) | 0 | 34 | n/a |
| Shell invocation on the controller (`sh -c`, `shell=True`, `os.system`, shell/raw/script modules) | 0 | 0 | 0 |
| Fixed `/usr/bin/sh -c` programs sent to the installed machine over the pinned SSH session | n/a | n/a | 9 |
| Disabled certificate verification as a literal | 0 | 13 leads | 0 |
| Default HTTP client, `math/rand`, `pickle`, unsafe `yaml.load` | 0 | 0 | n/a |
| `panic` calls | 2 | n/a | n/a |
| `context.Background()` or `context.TODO()` | 79 | n/a | n/a |
| Explicitly ignored results (`_ = f()`) | 64 | n/a | n/a |
| `defer x.Close()` with no check | 171 | n/a | n/a |
| Broad `except Exception` or bare `except` | n/a | 88 | n/a |
| Goroutine starts, sync primitives | 14, 36 | n/a | n/a |
| String-path reads of the desired-state tree (`.Get("…")`, `.Has("…")`) | 3,687 | n/a | n/a |
| `command` tasks, all with `argv` | n/a | n/a | 85 |
| `no_log` | n/a | n/a | 676 |
| `failed_when: false` | n/a | n/a | 118 |
| `set_fact` | n/a | n/a | 161 |
| Jinja expressions, statement blocks | n/a | n/a | 2,684, 39 |

The role and playbook YAML holds 212,496 characters of Jinja in 18,138 lines.
56 single tasks hold more than 600 characters of Jinja each, and 13 hold more
than 1,200.

### 2.6 What the gates run today

- **Go:** gofmt, `go vet` on two platforms, every test, the 187 fitness
  tests, completion, module verification and tidiness, licenses, govulncheck
  and the vulnerability gate. `make race` covers four package trees. There is
  no analyzer beyond vet, no coverage measurement, no complexity or
  duplication check, and nothing runs the 13 fuzz targets with `-fuzz`
  ([T01-06](#t01-06), [T01-07](#t01-07), T02).
- **Collection:** playbook syntax, ansible-lint, the ansible-test sanity
  suite (whose 64 ignores are all one licence-header rule), the unit tests
  twice plus once under the Python 3.9 floor, and three integration targets.
  There is no type checking and no coverage measurement (T01, PT1).
- **Repository tools:** each has behavioral tests. The nested Go module under
  `scripts/tools` gets tests but neither vet nor gofmt ([B01-10](#b01-10)).
- **CI:** `make check` and `make race` on every push and pull request. The
  commit-message gate runs on pull requests only, and this project delivers
  by pushing main ([B01-01](#b01-01), [T01-08](#t01-08)).

## 3. Scorecard

Each lane reviewer scored its scope from 1 to 5 on each dimension: 5 is
exemplary, 4 good with minor issues, 3 adequate with notable issues, 2 weak,
1 poor. A score is one reviewer's judgment of one slice, so compare rows with
care; the columns and the means are the useful reading. Lane ids: G is Go, P
Python, Y Ansible YAML, T tests and gates, PT the collection's test design, B
build and tools, E end-to-end cases and examples, X a cross-cutting review of
the whole codebase.

| Lane | Scope | Qual | Sec | Eff | Clear | DDD | Dup | Lang | Test | High | Medium | Low |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| [G01](#lane-g01) | Desired state and public API (Go) | 4 | 4 | 3 | 3 | 3 | 3 | 5 | 4 | 0 | 5 | 9 |
| [G02](#lane-g02) | Environment, network, trust, native artifacts, adapter protocol (Go) | 4 | 4 | 4 | 3 | 4 | 3 | 4 | 4 | 0 | 1 | 10 |
| [G03](#lane-g03) | CLI catalog, commands, parsing, validation, dispatch (Go) | 4 | 5 | 4 | 3 | 3 | 3 | 5 | 5 | 0 | 3 | 7 |
| [G04](#lane-g04) | CLI output, rendering, progress and results (Go) | 3 | 3 | 4 | 3 | 3 | 3 | 5 | 4 | 1 | 4 | 7 |
| [G05](#lane-g05) | Composition root cmd/bootwright (Go) | 4 | 4 | 3 | 3 | 2 | 3 | 5 | 4 | 1 | 4 | 4 |
| [G06](#lane-g06) | Controller: prerequisites, host packages, host inspection, firewall (Go) | 4 | 4 | 3 | 3 | 2 | 2 | 5 | 3 | 0 | 5 | 7 |
| [G07](#lane-g07) | Controller: privilege, Python runtime, bundles, staging, local Ansible, clients (Go) | 4 | 4 | 4 | 3 | 4 | 3 | 4 | 3 | 0 | 5 | 4 |
| [G08](#lane-g08) | Workspace contextfs: repository, files, publication, media, operations (Go) | 4 | 4 | 3 | 3 | 3 | 3 | 5 | 4 | 0 | 5 | 5 |
| [G09](#lane-g09) | Workspace: contextfs secrets, logs, runs, controller and setup records; contexts, invokerfs, selectionfs (Go) | 4 | 4 | 3 | 3 | 4 | 3 | 5 | 3 | 0 | 4 | 8 |
| [G10](#lane-g10) | Secrets: custody, material, encryption, local keyring, secret store (Go) | 4 | 4 | 3 | 3 | 4 | 3 | 5 | 3 | 0 | 5 | 7 |
| [G11](#lane-g11) | Reconciliation core (port vocabulary, operation store, log view, Ansible runner) | 4 | 4 | 3 | 3 | 4 | 3 | 5 | 4 | 0 | 4 | 6 |
| [G12](#lane-g12) | Lifecycle: planning, registration, decision, requests, admin credentials (Go) | 3 | 4 | 3 | 3 | 3 | 3 | 5 | 4 | 0 | 5 | 6 |
| [G13](#lane-g13) | Lifecycle: attempts, execution, scheduler, removal, status, diff (Go) | 4 | 4 | 3 | 3 | 4 | 3 | 5 | 4 | 0 | 4 | 7 |
| [G14](#lane-g14) | Machine: admission, access, power, disk selection, hardware, inventory, direct SSH | 3 | 4 | 3 | 3 | 3 | 3 | 4 | 4 | 1 | 4 | 6 |
| [G15](#lane-g15) | Substrate: domain root and the bare-metal, libvirt and KubeVirt arms, capacity (Go) | 4 | 4 | 3 | 3 | 3 | 2 | 4 | 4 | 0 | 4 | 4 |
| [G16](#lane-g16) | Managed OS installation capability (Go) | 4 | 4 | 3 | 3 | 3 | 2 | 4 | 3 | 0 | 6 | 7 |
| [G17](#lane-g17) | Managed OS root and media store; infrastructure services (Go) | 4 | 4 | 4 | 3 | 3 | 2 | 4 | 4 | 1 | 4 | 7 |
| [G18](#lane-g18) | Storage Ceph capability: requests, evidence, resources, services, inspection (Go) | 4 | 4 | 3 | 3 | 3 | 3 | 3 | 4 | 1 | 6 | 5 |
| [G19](#lane-g19) | Storage Ceph capability: hosts, physical retirement, exports, gateways, replication, SMB (Go) | 4 | 4 | 4 | 3 | 3 | 3 | 5 | 3 | 0 | 3 | 7 |
| [G20](#lane-g20) | Storage: CephCluster kind admission and normalization, storage preflight (Go) | 3 | 4 | 3 | 3 | 4 | 2 | 5 | 4 | 0 | 6 | 5 |
| [G21](#lane-g21) | Container cluster (OpenShift admission, agent installation, networking, access, preflight) | 4 | 4 | 3 | 3 | 3 | 3 | 3 | 4 | 0 | 5 | 9 |
| [G22](#lane-g22) | Add-ons: admission, catalog, packages, instances, cluster object programs (Go) | 4 | 4 | 3 | 3 | 4 | 3 | 4 | 4 | 0 | 3 | 7 |
| [T01](#lane-t01) | The architecture fitness suite and what the gates enforce | 4 | 3 | 2 | 3 | n/a | 3 | 3 | 3 | 0 | 7 | 5 |
| [T02](#lane-t02) | Go test strategy: contract suites, harnesses, doubles, determinism, speed | 4 | n/a | 3 | 3 | n/a | 4 | n/a | 3 | 1 | 8 | 3 |
| [P01](#lane-p01) | Ceph native worker core (Python) | 4 | 4 | 3 | 2 | 3 | 2 | 3 | 3 | 0 | 8 | 3 |
| [P02](#lane-p02) | Ceph native services (Python) | 4 | 4 | 4 | 3 | 3 | 2 | 3 | 4 | 0 | 7 | 5 |
| [P03](#lane-p03) | Ceph host runtime, tunnel, packages, registry (Python) | 4 | 4 | 3 | 3 | 3 | 3 | 4 | 4 | 0 | 5 | 8 |
| [P04](#lane-p04) | Physical erase, Redfish, disk discovery, hardware (Python) | 4 | 4 | 4 | 3 | 4 | 3 | 3 | 3 | 0 | 5 | 6 |
| [P05](#lane-p05) | KubeVirt, libvirt, managed-OS workers and native resolution (Python) | 4 | 4 | 3 | 3 | 4 | 3 | 4 | 4 | 0 | 3 | 8 |
| [P06](#lane-p06) | Controller, protocol, cluster objects, shared utilities, action plugins, filters, callback (Python) | 4 | 4 | 3 | 3 | 3 | 2 | 3 | 4 | 0 | 6 | 5 |
| [P07](#lane-p07) | Ansible modules (Python) | 3 | 3 | 4 | 3 | 4 | 3 | 4 | 3 | 0 | 5 | 5 |
| [PT1](#lane-pt1) | The collection's test design | 4 | n/a | 3 | 4 | n/a | 2 | n/a | 3 | 1 | 5 | 2 |
| [Y01](#lane-y01) | Roles for the managed OS, physical erase and machine operations (Ansible YAML) | 4 | 4 | 4 | 3 | 3 | 2 | 2 | 4 | 1 | 7 | 4 |
| [Y02](#lane-y02) | Substrate roles (libvirt, KubeVirt, bare metal, dispatcher) | 3 | 4 | 3 | 3 | 4 | 2 | 3 | 3 | 0 | 5 | 7 |
| [Y03](#lane-y03) | Container cluster and add-on roles (Ansible YAML) | 4 | 4 | 3 | 2 | 3 | 2 | 2 | 4 | 1 | 3 | 5 |
| [Y04](#lane-y04) | Infrastructure service, mirror, storage and controller roles (Ansible YAML and templates) | 4 | 4 | 4 | 4 | 4 | 2 | 3 | 4 | 1 | 5 | 6 |
| [B01](#lane-b01) | Build, release, CI, repository tools and supply chain | 4 | 4 | 3 | 3 | n/a | 3 | 3 | 4 | 0 | 8 | 5 |
| [E01](#lane-e01) | End-to-end cases, examples and acceptance coverage | 3 | n/a | n/a | 3 | n/a | 2 | 3 | 3 | 0 | 7 | 8 |
| [X01](#lane-x01) | Cross-cutting security, Go side (process execution, filesystem safety, privilege, network clients) | 4 | 4 | n/a | n/a | n/a | 3 | n/a | 3 | 0 | 2 | 6 |
| [X02](#lane-x02) | Cross-cutting security: secret and credential flow from custody to target | 4 | 4 | n/a | n/a | n/a | n/a | 4 | 4 | 0 | 2 | 1 |
| [X03](#lane-x03) | Cross-cutting concurrency and efficiency (Go, Python, Ansible, tests) | 5 | n/a | 3 | 4 | n/a | n/a | n/a | 3 | 0 | 0 | 3 |
| [X04](#lane-x04) | Cross-cutting domain-driven design: bounded contexts, dependency direction, domain model | 4 | n/a | n/a | 3 | 3 | 4 | n/a | n/a | 1 | 4 | 5 |
| [X05](#lane-x05) | Cross-cutting duplication in Go and across languages | n/a | n/a | n/a | 3 | 3 | 2 | 3 | n/a | 0 | 5 | 4 |
| [X06](#lane-x06) | Cross-cutting duplication in Python and Ansible YAML | n/a | n/a | n/a | 3 | n/a | 3 | 4 | 3 | 0 | 2 | 5 |
| [X07](#lane-x07) | Cross-cutting language placement | n/a | n/a | n/a | 3 | 3 | n/a | 3 | 3 | 0 | 3 | 3 |

Mean score by dimension over the lanes that scored it: Quality 3.86; Security 3.95; Efficiency 3.26; Clearness 3.02; DDD 3.31; Non-duplication 2.69; Right language 3.90; Test coverage 3.60.

Read by column, the table says where the codebase is strong and where it is
not:

- **Strongest:** security (most lanes score 4), quality, and in the Go lanes
  the use of the right language (5 almost everywhere).
- **Weakest:** non-duplication is the lowest column by a clear margin, and
  clearness is second lowest. Efficiency and domain-driven design sit in the
  middle.
- **The Ansible YAML lanes** score lowest on the right language and on
  non-duplication: that is where procedure and judgment have leaked out of Go
  and Python.

## 4. Findings by dimension

Each subsection says what is done well, what needs work, and why. Ids link to
the full entries. A finding listed here was reported by one reviewer with the
evidence quoted in its entry; [section 8](#8-method-verification-and-limits)
says which ones the orchestrator re-checked.

### 4.1 Quality

**Done well.** The code fails closed almost everywhere. Decoding is strict
and bounded, an unknown outcome is never read as absence, refusals carry a
code and a remedy, cancellation is kept apart from failure, and resources are
released on every path. There is no `TODO`, no lint suppression in Go and no
panic on user input. Errors are classified with `errors.Is` and `errors.As`
(371 uses) and string matching on error text is almost absent in Go (one
instance, [G22-02](#g22-02)).

**Needs work.**

- **Errors lost or given the wrong name at a boundary.** A custody read
  failure is reported as "the original administrator credential is missing"
  ([G12-01](#g12-01)). A transient I/O error on the setup state reads as "another build
  wrote this" and leads to an offer to discard it ([G09-01](#g09-01)). Every `Quiescent`
  probe turns any error, cancellation included, into "unproved" with one
  fixed reason ([G19-01](#g19-01), [G15-07](#g15-07)). `diff` drops an error and answers "adds
  nothing" ([G13-09](#g13-09)). A log append loses the errno that would name a full disk
  ([G08-09](#g08-09)). Unreadable host evidence weakens a self-protection check instead
  of refusing ([G06-07](#g06-07)). A broken embedded asset surfaces as "not implemented"
  ([G05-06](#g05-06)). Several operator-facing refusals are plain `fmt.Errorf` values
  with no code or remedy ([G20-10](#g20-10), [G21-11](#g21-11), [G17-11](#g17-11), [G11-07](#g11-07)), and several
  adapters discard the cause of a failed probe ([G20-07](#g20-07), [G06-09](#g06-09), [P03-11](#p03-11),
  [P02-07](#p02-07), [P07-06](#p07-06), [P03-07](#p03-07)).
- **State left behind by a failure.** A failed input update leaves a partial
  revision that an applied context never collects ([G08-01](#g08-01)). The erase
  worker's atomic write leaves its stage file, after which every retry fails
  ([P04-01](#p04-01)). A cancellation between removing the old media image and renaming
  the new one loses both ([G08-10](#g08-10)).
- **Dead code.** The largest piece is a retired OSD vocabulary of about 120
  lines in CephCluster admission, with rules that look live ([G20-01](#g20-01)). The
  native Ceph SSH transport is refused by admission, yet its branches remain
  in the Python worker and 42 monkeypatches in 17 test files keep them
  running in tests ([P01-01](#p01-01), [P03-06](#p03-06)). Seven Go functions have no caller at all
  ([T02-03](#t02-03)). Smaller pieces: [G07-06](#g07-06), [G12-06](#g12-06), [G18-10](#g18-10), [G10-10](#g10-10), [G01-06](#g01-06), [G22-04](#g22-04),
  [G22-01](#g22-01), [P01-10](#p01-10), [P02-08](#p02-08), [P07-08](#p07-08), [G03-08](#g03-08), [G04-12](#g04-12), [G17-06](#g17-06), [Y02-07](#y02-07), [P04-10](#p04-10).
  `go vet` does not report unused unexported methods, so nothing catches
  these.
- **Optional behavior found by type assertion at run time.** The lifecycle
  upgrades its secret port with `s.binder.(ProducedBinder)` at five sites and
  silently skips an identity check when the assertion fails ([G12-04](#g12-04)); the
  Controller service reaches a mandatory port method the same way ([G06-06](#g06-06)). A
  wiring gap then shows up only when the command runs.
- **Process supervision gaps.** The dnf output writer breaks the writer
  contract, so a preview over its bound fails instead of being truncated
  ([X01-01](#x01-01); reproduced with a probe). One Go runner has no `WaitDelay` and
  discards stderr ([G07-01](#g07-01)). Only 3 of the 11 Go supervisors kill the child's
  process group ([X01-03](#x01-03)). The Python runner can replace a timeout with
  "could not start" and waits out its whole budget when a descendant keeps a
  pipe open ([P06-02](#p06-02), [P06-07](#p06-07)).
- **Lent secrets are not always read byte for byte.** 27 role lookups of a
  password file strip trailing whitespace, while 15 others read it exactly
  ([X02-01](#x02-01)).
- **Results that do not tell the truth.** Two modules always report
  `changed` on update ([P07-07](#p07-07), [Y04-10](#y04-10)); erase and discovery return an empty
  result on refusal, which the engine records as unknown and then has to
  resolve ([G16-12](#g16-12)).

### 4.2 Security

**Done well.** This is the strongest dimension. File access is
descriptor-relative with no-follow opens and identity proofs before and after
each effect, in Go and in Python. The sudo boundary, the pinned SSH client
and the Ceph tunnel are thorough. No process on the controller starts through
a shell: each one starts from an absolute executable and an argument vector.
The shell programs that exist are fixed texts the specs sanction: the
installer's Kickstart sections, and nine programs that the managed-OS role
sends to the installed machine as `/usr/bin/sh -c` with positional
parameters ([Y01-07](#y01-07)). Secrets travel as
references, are lent only at execution and are cleared afterwards; no secret
enters a frozen request or a Kickstart. Evidence is re-validated in Go and
bound to the request digest. Every download is pinned by digest, CI actions
are pinned by commit, and the vulnerability gate fails closed.

**Needs work.**

- **Defects and gaps with a security consequence.**
  `controller uninstall` streams raw dnf output to the terminal, where
  repository-controlled text can carry control sequences ([G04-02](#g04-02)). The check
  that keeps a remote firewalld start from locking out SSH misses any rich
  rule that contains a colon ([P07-01](#p07-01)). The type that guards secret bytes from
  being printed does not guard them inside an unexported struct field
  ([G10-02](#g10-02); reproduced with a probe). A managed-service unit name is checked
  only as "a string" before it builds a path that destroy deletes ([Y04-04](#y04-04)),
  and several template inputs reach dnsmasq and chrony lines unchecked
  ([Y04-05](#y04-05)). Eleven modules start processes through Ansible's own runner, with
  the inherited environment and no time bound, where the rule is the one
  bounded runner ([P07-02](#p07-02)). Mirror downloads enforce no size ([Y04-03](#y04-03)). On a
  mirror host reached over SSH, a registry credential file is removed only
  by a Python `finally`, so a killed run can leave it behind ([X02-02](#x02-02)). The
  sudo argument guard checks the executable only after a `--` ([G07-07](#g07-07)); a nil
  file opener under root opens files as root ([G09-06](#g09-06)). Smaller items: [G09-05](#g09-05),
  [G02-03](#g02-03), [G02-04](#g02-04), [G21-07](#g21-07), [G04-03](#g04-03), [G16-11](#g16-11), [G16-13](#g16-13), [Y03-06](#y03-06), [P04-06](#p04-06), [G08-08](#g08-08),
  [G20-09](#g20-09), [G22-08](#g22-08), [P01-04](#p01-04), [Y01-03](#y01-03), [G04-08](#g04-08), [X02-03](#x02-03).
- **The traced secret classes reach their targets safely.** The secret-flow
  lane followed a machine key, a pull secret, a BMC credential, a Ceph key,
  an entitlement credential and a registry credential from custody to the
  target. Each travels as a private file, on standard input or over a pinned
  or verified channel; none appears in an argument list, a log or a served
  artifact; all 93 tasks that fail silently under `no_log` are judged by a
  later task; every weakened-TLS site is either the explicit per-BMC policy
  or followed by a certificate pin before any credential is sent (X02,
  Strengths).
- **Security-critical helpers exist in several copies that have drifted.**
  This is the main structural risk in the dimension, because a hardening fix
  then reaches one copy only. The rule "root-owned and not writable by
  others" has at least four Go implementations that disagree about sticky
  directories ([G07-05](#g07-05), [G02-02](#g02-02)). The no-follow tree removal is copied between
  two packages line for line and written three more times in the context
  store ([G07-04](#g07-04), [G11-03](#g11-03), [G09-04](#g09-04)). The descriptor walk, identity comparison
  and bounded read are copied in three Go adapters, and only one compares
  owner and group ([G01-04](#g01-04)). About nine Go adapters carry their own bounded
  process runner ([T02-05](#t02-05), [G06-04](#g06-04)), and their ten bounded output writers
  follow three incompatible rules on overflow: five drain and flag, three
  return an error that kills the child, and two report a short write
  ([X01-02](#x01-02)). The no-follow walk from the root is hand-written five times in
  four Go packages ([X01-05](#x01-05)). Python has a second runner for the erase path
  ([P04-02](#p04-02)), five atomic-write helpers of which two sync the directory
  ([P05-03](#p05-03)), five fixed-path walks with different rules ([P01-02](#p01-02)) and two
  private-file readers ([P02-09](#p02-09)). The OpenSSH crypto-policy reducer exists
  three times with different treatment of unknown directives ([G14-02](#g14-02),
  [P03-03](#p03-03)), and the pinned SSH option list is spelled out in three role files
  beside its Python owner ([Y01-01](#y01-01)).
- **Release and build hygiene rests on denylists.** The release recipe
  removes four environment variables and checks the origin of four make
  variables; `GO_IMAGE`, `GO_LICENSES`, `GOAMD64` and a `go env -w` setting
  still pass ([B01-02](#b01-02)). The container build context is a denylist and on this
  clone would send 22 GB of local worktrees ([B01-05](#b01-05)).
- **The code that crosses a privilege boundary is the least tested.** The
  root-only branches of the two helper launchers run in no gate (G09,
  Coverage notes), the account-provenance fixture needs root and an
  environment variable that nothing sets ([T02-08](#t02-08)), and the sudo argument
  guard is never run ([G07-07](#g07-07)).
- **Blind spots in the guards.** The effect-boundary check sees package
  functions but not method calls on a held file ([T01-03](#t01-03)). The race detector
  runs on four package trees and misses, among others, the adapter process
  runner, the context store and parallel machine access ([T01-07](#t01-07)). The Python
  site scan does not count `module.run_command` as starting a process
  ([P07-02](#p07-02)), and Python kept in string constants is invisible to every Python
  gate ([P04-03](#p04-03), [P03-09](#p03-09), [G02-10](#g02-10)).

### 4.3 Efficiency

**Done well.** Everything is bounded: reads, outputs, counts, deadlines. The
scheduler runs independent blocks at once under a bound and a throttle, with
deterministic recording. The concurrency lane opened every goroutine start
and every lock in production and found no concurrency defect: each of the 14
goroutines has an owner, a way to stop and a join, and every file lock is
non-blocking or polled under a bound (X03, Patterns). Polling takes its
budgets from the frozen request. Nothing found here is a hazard; the cost is
time and repeated work.

**Needs work.**

- **Independent work done one item at a time.** The owner's standing rule is
  that any step that can run in parallel must. Quiescence probes before a
  destroy run one adapter process per Machine in sequence ([G13-01](#g13-01)). Guest
  prerequisite checks take up to 10 s per host in sequence ([G20-06](#g20-06)), capacity
  probes up to 60 s per host ([G15-04](#g15-04)), copy preflights and power surveys run
  serially ([G14-05](#g14-05)), as do publisher fetches ([G07-08](#g07-08)), node reads in destroy
  and observe ([Y03-03](#y03-03)) and power readings ([Y01-12](#y01-12)). Firewall inspection
  reruns the host-wide queries for every socket inside one shared 45 s budget
  ([G06-02](#g06-02)). Several adapters start one process per item where one call would
  do ([P05-01](#p05-01), [P05-02](#p05-02), [Y02-09](#y02-09)), and the controller's client tools download
  one after another ([X03-02](#x03-02)). The concurrency lane's record lists eleven
  such sites in one place.
- **The same result computed many times in one command.** One plan or apply
  over changed input compiles the same two revisions four to six times and
  opens the keyring twice ([G12-02](#g12-02)). Normalization walks every record's tree
  about 39 times ([G01-01](#g01-01)). CephCluster admission resolves the same service
  placement from scratch at 35 call sites ([G20-02](#g20-02)). Capabilities re-derive
  every request on every plan ([G16-10](#g16-10), [G21-08](#g21-08), [G18-08](#g18-08), [G19-09](#g19-09), [G22-03](#g22-03)), and
  evidence is decoded up to four times per decision ([G18-07](#g18-07)).
- **Durable stores pay per entry.** Removing a tree verifies the whole
  ancestor chain and syncs the directory for every file ([G08-02](#g08-02)). Every read
  decodes every context's manifest, and a commit does it again ([G08-03](#g08-03)).
  Each attempt re-walks the operation area under a mutex ([G11-01](#g11-01)); each
  burst of adapter output is one fsync ([G11-02](#g11-02)); a status call reads up to
  8 MiB to learn whether a log exists ([G11-05](#g11-05)). Each secret write rescans
  the secret tree ([G09-02](#g09-02)), and every keyring open re-reads every identity
  file in a directory that only grows and is capped at 32,768 entries, which
  gives a keyring a finite lifetime ([G10-01](#g10-01)).
- **Fixed cost per call.** Every native Ceph call ships about 1 MB of Python
  source and compiles it remotely; that is already half of the 2 MiB input
  cap and no test guards the headroom ([P03-01](#p03-01)).
- **The test suites.** The fitness suite re-lists and re-parses the module
  for each of its tests ([T01-01](#t01-01)). Three of about 1,300 Go test files use
  `t.Parallel`, and the slowest packages run their process-spawning tests
  serially ([G13-05](#g13-05), [G11-10](#g11-10)). The gate runs govulncheck twice ([B01-11](#b01-11)) and the
  CI cache key misses two pinned inputs ([B01-04](#b01-04)).

### 4.4 Clearness

**Done well.** Names are precise and domain-true, comments state rationale
and not narration, most functions are short (median 11 lines), and refusal
messages name the remedy.

**Needs work.** This is the second lowest column.

- **Complexity packed under the line limit.** The 100-line rule is met, but
  several functions carry complexity of 40 to 86 inside it, mostly as single
  boolean expressions of ten to twenty terms that prove an identity or a
  shape (G19, G16, G14 and G22 Patterns; [G10-05](#g10-05); [G22-10](#g22-10)). A reviewer cannot
  tell which term guards which rule, and a wrong edit silently widens what is
  accepted.
- **Dispatch on strings.** `ValidLexical` is one switch with complexity 86
  over rule names that are free strings ([G01-02](#g01-02)). The CLI routes rendering by
  command path in two switches of about 45 and 30 cases ([G04-01](#g04-01)), and keeps
  per-command facts in about ten separate tables although the spec says the
  catalog is the single list ([G03-01](#g03-01)). The Ceph request is one struct with an
  action string and twelve optional arms that eight functions re-dispatch
  ([G18-01](#g18-01)). The Python Ceph worker does the same ([P01-03](#p01-03), [P03-05](#p03-05)).
- **Large units.** One lifecycle `Service` has 224 methods and one contracts
  file declares 56 interfaces ([G12-05](#g12-05)). One context-store type implements six
  ports in 14,700 lines ([G08-04](#g08-04)). `ceph_native.py` is 4,865 lines with one
  `Worker` of about 200 methods ([P01-03](#p01-03)). The managed-OS installation package
  holds five capabilities ([G16-04](#g16-04)).
- **Long parameter lists and boolean traps.** `invoke` takes 18 parameters
  and three siblings take 13 to 16 ([G13-04](#g13-04)); `validateShape` takes 8 with two
  adjacent booleans ([G01-05](#g01-05)).
- **State passed sideways.** The adapter session's protocol state is nine
  booleans ([G02-07](#g02-07)), the cluster-object engine carries results between
  methods in nine mutable flags ([P06-06](#p06-06)), and the Ceph worker's `devices()`
  creates attributes that later calls depend on ([P01-09](#p01-09)).
- **Misleading names.** `selectedJSON` is also true for YAML at 37 call sites
  ([G03-06](#g03-06)); a checkpoint named for secrets is reported for media and
  operation writes ([G08-07](#g08-07)); a progress group reports "ok" before its check
  runs ([Y02-08](#y02-08)). One helper name carries different rules in different
  packages: `findNamed` means "the unique match" in one and "the first match"
  in three ([X05-03](#x05-03)); `validName` and `printable` each name four different
  rules ([X05-07](#x05-07)); in Python `canonical`, `refusal` and `require` each name
  two or three things ([X06-05](#x06-05)).
- **Words used against the glossary.** `Variant` returns the selected arm,
  which the glossary says a variant never is; "receipt" has four senses
  where the glossary allows two ([X04-08](#x04-08)). The 24 driven adapters follow five
  naming conventions, and two packages are both called `preflightlocal`
  ([X04-09](#x04-09)).
- **Jinja as a programming language.** The role lanes score clearness at 2 to
  3 because diagnostics and state machines are written as nested inline
  conditionals ([section 4.7](#47-right-language)).

### 4.5 Domain-driven design

**Done well.** The architecture is real, not aspirational: ports are owned by
their consumers, admission is pure, the composition root is the only place
that binds adapters, the planner and the executor are held apart by a
fitness test, and generic code names no vendor. API values are immutable.

**Needs work.**

- **The desired-state tree is the working model.** Contexts read the generic
  value tree through string paths at 3,687 production sites, 1,212 of them
  in storage, and 18% of production functions take an API object, catalog or
  value. A misspelled or renamed path reads as "absent" instead of failing,
  so an admission rule can silently stop applying ([G01-03](#g01-03)). The design lane's
  verdict is that this choice is defensible: it avoids a second type
  hierarchy and shared mutable state, and code does translate out of the
  tree at two points, the published value objects and each capability's
  frozen request. Its costs are the silent path, repeated derivation and no
  compile-time reach of a schema change ([X04-03](#x04-03)). The domain layer built on
  it is mostly free functions over that tree, not types that guard their own
  invariants: the OpenShift root has 98 free functions and no method, the
  CephCluster root 155 and one; context names travel as bare strings through
  285 functions ([X04-04](#x04-04)). Several vocabularies are strings where a type
  would hold them: lexical rule names
  ([G01-02](#g01-02)), controller modes ([G09-07](#g09-07)), check statuses (G06, Patterns),
  address family in three encodings ([G21-10](#g21-10)), installer documents carried as
  `map[string]any` with unchecked assertions ([G21-12](#g21-12)).
- **Policy in the wrong layer.** The composition root holds a whole domain,
  about 700 lines that decide what the mirror copies ([G05-01](#g05-01)), and further
  application work outside the named exceptions ([G05-05](#g05-05)). The CLI re-parses
  the lifecycle's `--authorize` grammar, re-implements the media-name rule
  and filters `info` results itself ([G03-02](#g03-02), [G03-04](#g03-04), [G03-03](#g03-03)), and computes
  the `diff` verdict and its exit status in the output layer ([G04-05](#g04-05)).
- **Context-level dependencies have no guard.** The fitness suite demands a
  stated reason for every edge between two application packages, but none
  for an import of another context's domain root: 36 domain-to-domain and 94
  application-to-domain edges cross a context with no reason recorded
  ([X04-01](#x04-01)). Nine pairs of contexts depend on each other in both directions.
  The design lane judged each pair by its package edges: three are sound
  layering, and three exist only because of one misplaced value, the host
  reservation ([X04-02](#x04-02)).
- **Policy in the wrong context.** The cross-context host-reservation model
  lives in Controller setup, which gives that package 23 importers, and its
  key grammar is spread over five packages ([G06-01](#g06-01), [X04-02](#x04-02)). The Controller
  root mixes published evidence with selection policy, which is why add-on
  and cluster packages depend on it ([X04-05](#x04-05)), and Controller selection reads
  the native fields of six other contexts ([G06-05](#g06-05)). OpenShift admission owns
  a catalog-wide address registry ([G21-05](#g21-05)). The arm-neutral substrate root holds libvirt-only admission, and
  the "capacity" package is libvirt's capacity under a generic name ([G15-03](#g15-03),
  [G15-05](#g15-05)). The machine root mixes the Machine model with context selection,
  ownership vocabulary and CLI operand parsing ([G14-04](#g14-04)). The shared managed
  service holds DNS and proxy derivations ([G17-05](#g17-05)).
- **One service and its ports are too wide.** See [G12-05](#g12-05) above; every path
  consumes a 27-method store although readers need seven ([G12-05](#g12-05)).
- **The Python side has contracts with no owner.** Marker-record file names
  are rebuilt by hand in several modules ([P02-04](#p02-04)), and Python computes a
  classification that Go never reads while rewriting an observation to signal
  "unknown" ([P06-05](#p06-05)).

### 4.6 Non-duplication

This is the weakest dimension, and its findings share one root cause: the
codebase grew capability by capability, each new one started from a copy of
its nearest sibling, and the common parts were never pulled out afterwards.
The copies have begun to drift, which is when duplication stops being a
style matter and becomes a defect source.

- **The lifecycle capability skeleton is copied per capability.** The rule
  that turns an observation into an effect state is hand-written in 10
  functions across 7 packages, in two styles ([X05-01](#x05-01)), and the adapter's
  outcome is a plain string at the port, so 8 switches map it back to a typed
  value that the decoder had already checked ([X05-02](#x05-02)). The artifact server is
  a near copy of the managed-service capability in Go and the two have
  already diverged on what a claimed postcondition without a fresh start
  means ([G17-01](#g17-01)); their roles' destroy files differ in 6 lines of 181 once
  the role, variable and module names are set aside ([Y04-01](#y04-01)). The same holds
  for the three mirror-fill capabilities ([G17-02](#g17-02)), the five substrate
  capabilities ([G15-01](#g15-01)), the agent-install
  capabilities ([G21-03](#g21-03)), the two untested administrator-credential flows
  ([G21-02](#g21-02)), the Ceph data services (G19, Patterns; [G19-02](#g19-02), [G19-03](#g19-03), [G19-04](#g19-04)),
  the Python Ceph service workers ([P02-01](#p02-01), [P02-03](#p02-03)), the three guest-worker
  modules ([P07-03](#p07-03)), the two protocol plugins and their module utilities
  ([P06-01](#p06-01), [P06-04](#p06-04)) and the two pull loops ([P03-02](#p03-02)).
- **One rule, several owners, often in several languages.** The grammar of a
  disk identity is defined in four or more Go places, once in emitted bash
  and twice in Python, and the readers disagree in detail ([G14-03](#g14-03), [G16-01](#g16-01),
  [G16-09](#g16-09), [P04-07](#p04-07)). The proof of a machine's identity is written twice in
  Python and twice in Jinja ([P04-04](#p04-04), [Y02-05](#y02-05), [Y01-05](#y01-05)). Go, the role's argument
  specification and the Python worker each validate the same request, and
  the Python side also repeats product rules ([P01-05](#p01-05)); no Python test loads a
  request that Go emits ([P01-11](#p01-11)). Budgets and timeouts are literals in Go
  and again in YAML or Python ([P02-05](#p02-05), [Y04-02](#y04-02), [Y01-06](#y01-06), [G22-06](#g22-06)). Further
  cases: [G18-04](#g18-04), [G18-05](#g18-05), [Y01-02](#y01-02), [G20-03](#g20-03), [G20-04](#g20-04), [G20-05](#g20-05), [G17-03](#g17-03), [G17-04](#g17-04),
  [G21-06](#g21-06), [G05-02](#g05-02), [G05-03](#g05-03), [G03-02](#g03-02), [G03-04](#g03-04), [B01-07](#b01-07), [B01-08](#b01-08), [G22-07](#g22-07), [G10-06](#g10-06),
  [G10-07](#g10-07).
- **Security primitives.** Listed in [section 4.2](#42-security): path
  ownership, tree removal, descriptor walks, process runners, atomic writes,
  the crypto-policy reducer.
- **Role YAML.** A quarter of role and playbook lines sit in duplicated
  regions, and none of it is documentation. 55% of it is the run, refuse,
  fail and complete body that 86 entry files in 29 roles repeat (X06,
  consolidated table). A structural test forces the first dozen lines by
  allowing only inline assertions before the handoff; the rest is habit, and
  one private task file per role would hold it ([Y01-09](#y01-09), [Y04-09](#y04-09)). Beyond the
  entry bodies: the pinned guest SSH session in 10 task files ([Y01-01](#y01-01)), the
  listener proof in 6 roles with 11 copies of its failure classifier, already
  drifted ([Y01-03](#y01-03), [X07-02](#x07-02)), the machine-port access map built 21 times in 12
  task files of 4 roles ([X07-01](#x07-01), [Y01-04](#y01-04), [Y03-04](#y03-04)), a Redfish credential
  preamble at 27 call sites and three task files copied between two
  substrate arms ([Y02-02](#y02-02)).
- **Python.** The detector's 5.4% is misleading in the other direction: 91%
  of the duplicated Python lines are Ansible documentation blocks, which a
  documentation fragment would hold once ([X06-02](#x06-02)), and one copied list has
  already drifted from its code ([X06-01](#x06-01)). The Python duplication that
  matters is the kind listed above, which the detector does not see.
- **Data.** 40% of example YAML lines and 24% of end-to-end template lines
  are byte-identical copies of another case's file, and one workload is
  described twice ([E01-04](#e01-04), [E01-05](#e01-05)). Since October, 191 commits touched these
  trees.
- **Small helpers.** Five converters from the API value to native data with
  different number handling (G22, Patterns), four "last address of a prefix"
  helpers, the 999-diagnostic cap as a literal in six packages (X05, family
  15), nine `ContentDigest` functions that spell out one mechanism ([X05-06](#x05-06)).
- **Tests.** 23 copies of the golden test helper in four signatures
  ([T02-11](#t02-11), deliberate but drifting), five Go test files that each emulate
  ansible-core's argument validation differently ([X05-04](#x05-04)), and about 18
  hand-written partial Ansible task interpreters in the role tests that
  disagree on block semantics ([PT1-02](#pt1-02)).

The Go duplication lane grouped every Go family into one table with its
copies, a proposed owner and what blocks a naive merge (X05, Duplication
families); the Python and YAML lane did the same for the collection (X06,
Consolidated family table). Both also list what is deliberate and should
stay: consumer-owned ports, the per-port contract suites, and the worker
preludes that the stand-alone rule forces.

### 4.7 Right language

**Done well.** Go owns policy, planning and judgment. No role uses the
shell, raw or script modules.
The language lane checked every place where Go opens a connection or runs a
remote command and found each one an allowed case; two of them are
sanctioned by other spec pages but missing from the architecture's list
([X07-06](#x07-06)). The Python `dispatch` functions route only on the phase and action
that Go chose, and version specialization follows the spec's record shape.
The filters are free of effects.

**Needs work.** The Go lanes score 5 here; the role lanes score 2 to 3. The
language lane classified the 25 single tasks that hold the most Jinja. By
the rows of its table, 17 are decisions or transformations that belong
elsewhere, 3 are copies of one failure classifier, and 5 are legitimate
refusals or evidence (X07, Heaviest Jinja tasks; the lane's own closing
sentence gives 18, 3 and 6, which is more than 25).

- **Procedure and judgment written in Jinja.** The installer wait's resume
  policy (retry, stall detection, hint selection) is Jinja over regexes of
  `openshift-install` stderr; testing it needs a 130-line hand-written model
  of ansible-core's executor ([Y03-01](#y03-01)). Each cluster read is four `oc` calls
  whose stderr text is classified in Jinja, while a bounded Python `oc`
  runner exists ([Y03-02](#y03-02)). Which proofs an installation requires is computed
  in Jinja in three places that mirror a Go function ([Y01-02](#y01-02)). The
  wrong-target rules for power and erase are Jinja expressions ([Y01-05](#y01-05),
  [Y02-05](#y02-05)). The registry's SELinux level is derived in Jinja from a hash
  ([Y04-06](#y04-06)). The registration worker's time budget is arithmetic in a task,
  while Go's bound for the same step is a separate constant with a stale
  comment ([X07-03](#x07-03), [Y01-06](#y01-06)). Secret-bearing install configuration is composed
  in a 35-line expression ([G21-04](#g21-04)). Roles classify failures by matching
  English tool output ([Y04-08](#y04-08), [Y02-04](#y02-04)).
- **Python deciding what Go should decide.** Python recomputes a client's
  capability grants, which is an authorization decision ([G18-04](#g18-04)), repeats
  Go's evidence scope rules and product rules ([G18-05](#g18-05), [P01-05](#p01-05)), recomputes a
  budget formula and a placement-group clamp ([P02-05](#p02-05)), holds the list of
  kinds that must compare exactly ([G22-09](#g22-09)), and computes Ceph readiness
  ([P01-06](#p01-06); the spec sanctions that one, so it is a decision for the owner).
- **One language inside another.** Shell for the installer lives in Go string
  slices; the spec allows it, but its helpers are repeated four times and
  nothing checks its syntax ([G16-05](#g16-05)). Guest shell programs live in role
  defaults ([Y01-07](#y01-07)). Python lives in strings in several places and is then
  invisible to lint, to the floor interpreter and mostly to tests: the erase
  RAM runtime of 140 lines, including the handler that authorizes power-off
  ([P04-03](#p04-03)), three probes ([P03-09](#p03-09)), a bcrypt program run through podman
  ([Y02-03](#y02-03)), four programs in a bash script ([E01-06](#e01-06)), the adapter bootstrap in
  a Go string ([G02-10](#g02-10)) and a scanner inside a Go test ([T01-12](#t01-12)). The discovery
  boot's `init` script is shell inside a Python string that no test runs
  ([X07-05](#x07-05)). The language lane's record lists every embedded piece with its
  size and how it is tested; the fitness test for language rows classifies a
  file by path and shebang only, so none of this is visible to it.
- **Shell with logic.** The release recipe is a 62-line shell program that
  parses the executable's JSON with sed ([B01-03](#b01-03), [T01-10](#t01-10)).
- **Outside its own gates.** The Go-embedded copy worker must run on Python
  3.9 but no Python gate sees it ([G14-08](#g14-08)).

### 4.8 Test coverage

**Done well.** Coverage is high in both languages (section 2.2), and the
tests assert behavior: refusals by diagnostic code, cancellation, replay.
The port contract suites run against the production adapters and the
doubles. The kill and checkpoint harnesses interrupt every durable write.
Tests are hermetic and clocks are injected. Fitness rules mostly prove
themselves on planted violations.

**Needs work.**

- **The flows with the highest stakes are among those no test runs.** The
  only production path that rebinds a secret, the erase-receipt check that
  Ceph physical retirement relies on, and both administrator-credential
  implementations run in no test in the module ([T02-01](#t02-01), [G10-04](#g10-04), [G16-02](#g16-02),
  [G21-01](#g21-01), [G18-03](#g18-03)). The same holds for the firewall SSH-admission success
  path ([P07-01](#p07-01)), the erase runtime's acknowledgement handler ([P04-03](#p04-03)), the
  wrong-target gate of disk discovery (P04, Coverage notes) and the binding
  from the privilege boundary to the user's files (G05, Coverage notes).
- **One confirmed bug sits in an untested common path.** Copying files to a
  single key-authenticated Machine always refuses; every copy test uses two
  or more Machines ([G14-01](#g14-01)).
- **The destructive bindings get the weakest contract coverage.** 21 of the
  42 rows of the capability contract suite, among them physical erase, disk
  discovery, the host firewall and physical retirement, get planning checks
  only ([T02-02](#t02-02)).
- **Some tests do not prove what their names say.** One compares a Python
  function with itself under a name that claims Go parity ([P05-08](#p05-08)). One
  classifies an error message that the tool never emits ([Y04-08](#y04-08)). One
  "material never appears" test checks two fields and no stored file (G10,
  Coverage notes). Seventeen test files drive a transport that production
  cannot reach ([P01-01](#p01-01)). Go goldens for the owner mark are read by no Python
  test, while Python is the live copy ([G22-01](#g22-01)). Several fitness rules have no
  planted violation ([T01-04](#t01-04)).
- **Rules written in two languages are rarely tied together by a test.**
  Good examples exist: the erase regions share test vectors and the firewall
  rules share one JSON corpus. Most pairs have none ([P01-11](#p01-11), [X05-05](#x05-05), [X05-09](#x05-09),
  [P02-05](#p02-05), [Y01-02](#y01-02), [Y01-06](#y01-06), [Y04-02](#y04-02), [G03-02](#g03-02), [G22-01](#g22-01)). Where parity is held, it
  is often by a Go test that scrapes Python source with a regular
  expression; goldens written by Go and read by both languages are the
  sturdier form and already exist for a few cases (X05, Patterns).
- **Roles are tested by rendering, not by running.** No role entry point
  runs past argument validation through ansible-core in the gate. Role
  behavior is exercised by test-local interpreters that template `when` and
  `set_fact` over hand-built scopes and fake every module result; the three
  integration targets run one module, one refusal path and a supervisor
  ([PT1-01](#pt1-01)). The roughly 18 interpreters disagree with each other and with
  Ansible on how a block's condition and its `rescue` and `always` sections
  behave ([PT1-02](#pt1-02)), and the rule "a variable is registered before it is read"
  scans top-level tasks only, so it passes vacuously for the 13% of
  registers that sit inside blocks ([PT1-04](#pt1-04)). A defect that depends on
  variable precedence, on the shape of a real registered result or on
  run-time types therefore reaches the owner's live run. Commit `21ccc6234`,
  made while this audit ran, fixed exactly such a defect: Ansible-tagged
  integers failed an exact type check once a module's logic moved into an
  action plugin.
- **Modules have no stubbed-run test.** Action plugins have one test that
  drives every `run()`; modules do not, and 48 of 77 module files never run
  in the test process. Most are thin, but some hold real logic, such as the
  completion-drift check of the erase observation ([PT1-03](#pt1-03), P07 Coverage
  notes).
- **The gate depends on its host.** Unit tests need `helm` and `python3`
  from the ambient path, unpinned ([T02-07](#t02-07), [E01-02](#e01-02)). The 13 fuzz targets never
  run with `-fuzz`, and the adapter record decoder, which parses adapter
  output, has no fuzz target (T01, Coverage notes). Several documented
  end-to-end switches are never rendered ([E01-03](#e01-03)).

## 5. Defects to fix first

These are findings that describe wrong behavior, not structure. The first
table holds the ones the orchestrator re-checked at the source after the
reviewer reported them; all of them held. "Probe" means the mechanism was
also reproduced with a small standalone program outside the repository.
None was exercised on a live host.

| Finding | What is wrong | Consequence | Checked by |
| --- | --- | --- | --- |
| [G14-01](#g14-01) | `machine cp` builds the single-Machine member without its key part, so the bound key Secret is asked for an empty part. | Copying to one key-authenticated Machine always refuses; only two or more Machines, or a password or operator identity, work. | Reading the path end to end |
| [X01-01](#x01-01) | The dnf output writer truncates and reports a short write. `os/exec` then closes the pipe. | An install or removal preview over 64 KiB fails instead of being shown truncated. | Probe: the child dies with a broken pipe |
| [P07-01](#p07-01) | The check that a firewalld start cannot cut SSH treats a rich rule that contains a colon as a field, and stops checking the rules after it. | A zone with such a rule passes the lockout gate. | Reading |
| [G04-02](#g04-02) | `controller uninstall` hands dnf the raw terminal writer. | Repository-controlled text reaches the terminal unescaped. | Reading |
| [G10-02](#g10-02) | The secret container hides its bytes only through methods that `fmt` cannot call on an unexported field. | Formatting any struct that holds material in an unexported field prints the secret. Latent: no such print was found. | Probe: `%v` prints the bytes, `%s` the text |
| [G12-01](#g12-01) | The "credential missing" check runs before the error check. | A failed or cancelled custody read is reported as a missing Ceph administrator credential, with the wrong remedy. | Reading |
| [G09-01](#g09-01) | A setup state that cannot be read is classified as written by another build. | An I/O error leads to an offer to discard recoverable setup state. Partly specified behavior: see [section 7](#7-decisions-for-the-owner). | Reading |
| [G08-01](#g08-01) | A failed input publication returns without removing the revision directory it created. | An applied context keeps the partial revision for good, and it counts toward the revision limit. | Reading |
| [P04-01](#p04-01) | The erase worker's atomic write leaves its stage file when the write fails. | Every later acknowledgement in that boot fails, so the worker never powers off. | Reading |
| [X02-01](#x02-01) | 27 role lookups read a lent password file with the default right-strip. | A password that ends in whitespace reaches the BMC or libvirt changed. | Count over all role files |
| [Y04-04](#y04-04) | A managed-service unit name is checked only as a string. | The path that destroy deletes rests on Go's derivation alone. | Reading |
| [G07-01](#g07-01) | The runtime build runner sets no wait bound after cancellation and discards stderr. | A timed-out pip or compile step can block, and every failure reaches the operator with no cause. | Reading |
| [G18-02](#g18-02) | The Ceph request identity check covers 7 of the 12 request arms. | A request carrying an extra arm is not refused there. | Reading |
| [G07-07](#g07-07), [G09-06](#g09-06) | The sudo argument guard checks the executable only after a `--`; a nil file opener under root opens files as root. | Both fail open if a future caller changes; neither is reachable today. | Reading |
| [G13-09](#g13-09), [G19-01](#g19-01) | `diff` drops an error and answers "adds nothing"; each Ceph quiescence probe turns any error into one fixed reason. | A confident wrong answer, or the wrong remedy. | Reading |
| [G02-03](#g02-03) | The rule against pinning one endpoint to two keys compares raw address text. | Two spellings of one address can hold different keys. | Reading |
| [G21-07](#g21-07) | The cluster handoff refuses `--show-token` but not `whoami -t`. | A descriptor can be issued for a token-revealing command. | Reading |
| [G01-09](#g01-09) | Effective YAML writes `yes`, `no`, `on`, `off` and similar strings unquoted. | A YAML 1.1 reader takes them as booleans. | Reading |
| [E01-01](#e01-01) | The end-to-end render script exits silently when the route probe fails. | A host with no default route cannot render any case and gets no message. | Reading |
| [T01-08](#t01-08), [B01-01](#b01-01) | CI runs the commit gate on pull requests only. | On the delivery path (push to main) the contract and breaking-change rules are procedure, not a gate. | Reading |

Reported by one reviewer with quoted evidence, not re-checked by the
orchestrator:

- **Quality and state:** [G08-10](#g08-10), [G08-08](#g08-08), [G08-09](#g08-09), [G04-03](#g04-03), [G04-04](#g04-04), [G04-06](#g04-06),
  [G04-07](#g04-07), [G06-07](#g06-07), [G06-08](#g06-08), [G16-12](#g16-12), [G15-07](#g15-07), [G17-07](#g17-07), [G22-02](#g22-02), [P06-02](#p06-02), [P06-07](#p06-07),
  [P02-07](#p02-07), [P03-07](#p03-07), [P07-06](#p07-06), [P07-07](#p07-07), [P07-10](#p07-10), [Y01-10](#y01-10), [Y02-01](#y02-01), [Y02-04](#y02-04), [Y02-06](#y02-06),
  [PT1-04](#pt1-04), [PT1-05](#pt1-05), [G05-06](#g05-06), [G05-07](#g05-07).
- **Security:** [G04-08](#g04-08), [G16-11](#g16-11), [G16-13](#g16-13), [G20-09](#g20-09), [G22-08](#g22-08), [G09-05](#g09-05), [G02-04](#g02-04),
  [P02-09](#p02-09), [P04-06](#p04-06), [P07-02](#p07-02), [Y01-03](#y01-03), [Y03-06](#y03-06), [Y04-03](#y04-03), [Y04-05](#y04-05), [X02-02](#x02-02), [X02-03](#x02-03),
  [X01-03](#x01-03), [X01-04](#x01-04), [B01-02](#b01-02), [B01-05](#b01-05), [G05-03](#g05-03), [P01-04](#p01-04).
- **Needs a run on an older kernel before it is called a defect:** [P06-03](#p06-03).
  The claim is that publishing a tool with `linkat` and `AT_EMPTY_PATH`
  needs a capability on kernels before 6.10. The mechanism is right, but
  the controller stage runs as root, which holds that capability, so the
  exposure is probably limited to unprivileged test harnesses on such
  kernels. The orchestrator narrowed this one; it did not settle it.

## 6. Improvement plan

This plan is a proposal. It creates no milestone item and authorizes no
change; the owner decides which parts become items and in which milestone.

### 6.1 Principles

- **Defects first, each with a test that fails before the fix.** Several of
  them sit in code that no test runs, so the test is half of the fix.
- **Gates before refactors.** The project's own style is "lists only
  shrink". Put coverage, analysis and complexity under that style first, so
  each later improvement holds once made.
- **Consolidate primitives before restructuring.** Moving ten copies of a
  helper onto one tested owner shrinks the code that the later structural
  work has to move.
- **One slice, one observable change, one commit,** as the delivery skill
  requires. Sizes below are S (hours, one slice), M (one to three slices) or
  L (a program of slices).
- **No version bumps.** Where a slice changes a frozen request or a content
  digest, it regenerates the contract golden and names the implementation in
  its `Contract:` line, as the development policy asks. Those slices are
  marked "contract" below; the owner's routing treats them as work for the
  stronger model.
- **Safety class.** Slices that touch a destructive effect, a privilege
  boundary, secret custody or a durable store are marked "safety": they take
  an independent review and the race run.

### 6.2 Workstreams

| # | Workstream | Goal | Size | Depends on |
| --- | --- | --- | --- | --- |
| W0 | Confirmed defects | Remove the wrong behavior listed in section 5 | 20 S slices, 4 M | none |
| W1 | Gates | Measure and hold coverage, analysis, races, complexity; fix CI and release hygiene | 12 S, 4 M | none; W1.1 needs [G07-02](#g07-02) |
| W2 | Primitives kit | One owner for each operating-system primitive, in Go and in Python | L | none |
| W3 | Single owners for rules | One owner per rule; shared fixtures across languages; Go freezes derived values | L | W2 for file helpers |
| W4 | Capability kit | One lifecycle skeleton; fold copied capabilities | L | W3 for frozen values |
| W5 | Right language | Procedure out of Jinja; judgment out of Python; strings into real files | L | W3, W4 |
| W6 | Role and collection tests | Run each role family through ansible-core; one shared test interpreter | M to L | W5 reduces its size |
| W7 | Performance | Parallel probes; compute once per decision; cheaper stores and suites | M to L | W4 for the fan-out helper |
| W8 | Structure | Smaller units; clean context boundaries | L | W2, W4 |
| W9 | Examples and end-to-end cases | Less copied data; every documented switch rendered | M | none |

**W0. Confirmed defects.** One slice per row of the first table in section
5, in that order, then the unverified lists after a quick confirmation of
each. Safety-class slices: [G14-01](#g14-01), [P07-01](#p07-01), [G10-02](#g10-02), [G12-01](#g12-01), [G09-01](#g09-01), [G08-01](#g08-01),
[P04-01](#p04-01), [G18-02](#g18-02), [G07-07](#g07-07), [G09-06](#g09-06), [Y04-04](#y04-04). Exit evidence: each slice adds a
regression test that failed before the fix; [G14-01](#g14-01)'s test copies to one
key-authenticated Machine; [X01-01](#x01-01)'s runs a real child process into the
writer with output over the bound.

**W1. Gates.**

1. *Coverage.* Fix [G07-02](#g07-02) (compare stdout only), add a `make cover` target
   that writes the per-package table of section 2.2, and add a fitness test
   that holds each package to a floor that only rises, seeded from today's
   figures. For Python, either pin a coverage tool in the check environment
   or adopt a `sys.monitoring` recorder like the one this audit used. Size M.
2. *Static analysis.* Pin one analyzer in the tools module and add it to
   `check-go` with an exact exclusion list that only shrinks, starting with
   unused code and ineffective assignments ([T01-06](#t01-06)). The first run is also
   the dead-code sweep: [G20-01](#g20-01), [T02-03](#t02-03), [G07-06](#g07-06), [G12-06](#g12-06), [G18-10](#g18-10), [G10-10](#g10-10),
   [G01-06](#g01-06), [G22-04](#g22-04), [G03-08](#g03-08), [G04-12](#g04-12). Add vet and gofmt for the tools module
   ([B01-10](#b01-10)). Size M.
3. *Races.* Run the race detector on every package that starts a goroutine
   or takes a lock, or on the whole module with a raised timeout ([T01-07](#t01-07),
   X03 Coverage notes). Size S.
4. *Complexity.* Add a known list of production functions with cyclomatic
   complexity of 40 or more (28 in Go, 14 in Python today) that only
   shrinks, beside the line-length rule. Size S.
5. *Fuzzing.* Add fuzz targets for the adapter record decoder, the canonical
   JSON decoder and the lexical rules, and run the seed corpora plus a short
   `-fuzz` budget in the scheduled workflow (T01, T02). Size S.
6. *CI.* Run the commit gate on push ([B01-01](#b01-01)), add the two missing inputs to
   the cache key ([B01-04](#b01-04)), add a concurrency group ([B01-13](#b01-13)). Size S.
7. *Release and build.* Make the release environment an allowlist and refuse
   any make variable not set by the Makefile ([B01-02](#b01-02)); switch the build
   context to an allowlist ([B01-05](#b01-05)); declare the `wheels` dependency on the
   targets that embed it ([B01-06](#b01-06)); spread the 89 exception expiries or warn
   two weeks ahead ([B01-12](#b01-12)). Size M.
8. *The fitness suite itself.* Memoize the source walk ([T01-01](#t01-01)); split docs
   and tooling tests into their own packages so `go test` runs them in
   parallel ([T01-02](#t01-02)); add planted violations for the dependency-direction,
   lifecycle-partition and deadline rules ([T01-04](#t01-04)); make the effect boundary
   type-aware ([T01-03](#t01-03)). Size M.
9. *Parity of copies the project keeps on purpose.* A fitness test that
   compares each per-package golden helper with the model ([T02-11](#t02-11)), and one
   that ties every documented refusal list to its code ([X06-01](#x06-01)). Size S.

Exit evidence: `make check` runs each new gate; each list has a test that
fails when an entry can be removed.

**W2. Primitives kit.** Safety class throughout.

1. *Go process runner.* Extend `internal/nativeexec` from "qualify an
   executable" to "run one": absolute executable, explicit environment,
   process-group kill, bounded output with one overflow contract, a wait
   bound, exit classification. Move the adapters onto it one per slice:
   firewalllocal, the two preflight resolvers, hostowned, hostdnf, venvlocal,
   comparisonlocal, selectionfs, sshlocal ([T02-05](#t02-05), [X01-02](#x01-02), [G06-04](#g06-04), [G07-01](#g07-01),
   [X01-03](#x01-03)). Size L, about ten S slices.
2. *Go descriptor file kit.* One package for the no-follow walk, the
   identity comparison, the bounded read, the atomic publish and the verified
   tree removal, on `openat2` as the context store already uses it. Move
   inputfs, the secret material reader, hostlinux, the storage preflight
   walkers, stagelocal and the Ansible runner onto it ([G01-04](#g01-04), [X01-05](#x01-05),
   [G07-04](#g07-04), [G11-03](#g11-03), [G09-04](#g09-04), [G02-08](#g02-08)), and replace the hand-coded trap numbers
   with the typed calls of the system library ([X01-06](#x01-06)). Size L.
3. *One ownership rule.* One walker with explicit options for the trusted
   owner and the sticky policy, used by the four copies ([G07-05](#g07-05), [G02-02](#g02-02)).
   Size M.
4. *One privilege-drop helper launcher* for invokerfs, selectionfs and the
   comparison helper, with an injectable launch seam so its root-only
   branches run in tests (G09 Patterns, [T02-04](#t02-04)). Size M.
5. *Python twins.* One held-directory helper ([P01-02](#p01-02)), one atomic publish
   with a directory sync ([P05-03](#p05-03)), one private read and write pair ([P02-09](#p02-09),
   [P07-04](#p07-04)), one bounded runner for every site including the modules that
   use Ansible's own ([P07-02](#p07-02), [P04-02](#p04-02), [P06-02](#p06-02), [P06-07](#p06-07)), one registry
   authentication helper ([P03-04](#p03-04)), one crypto-policy reducer owned by the
   module that must run alone ([P03-03](#p03-03)). Size L.

Exit evidence: one contract test suite per primitive; the clone tables of
section 2.4 show none of these families; the Python site scan counts
`run_command`.

**W3. Single owners for rules.**

1. *Shared fixtures where two languages must each hold a copy.* A corpus of
   sysfs trees with expected disk identities, run against the emitted bash
   and both Python readers ([G16-01](#g16-01), [P04-07](#p04-07)); a table of crypto-policy lines
   with expected verdicts for Go and Python ([G14-02](#g14-02)); the owner mark and
   field manager goldens read by a Python test ([G22-01](#g22-01)); every Go request
   golden fed to its Python validator ([P01-11](#p01-11), [X05-05](#x05-05), [X05-09](#x05-09)). Size M.
2. *Go freezes what others re-derive* (contract slices): client capability
   grants ([G18-04](#g18-04)), per-phase budgets and the derived placement-group count
   ([P02-05](#p02-05)), the proofs an installation requires ([Y01-02](#y01-02)), worker session
   timeouts ([Y01-06](#y01-06), [X07-03](#x07-03)), the image pull budget ([Y04-02](#y04-02)), the registry
   label level ([Y04-06](#y04-06)), the media volume name ([P05-10](#p05-10)), one machine-port
   access object per target in place of 21 hand-built ones ([X07-01](#x07-01)), the
   hosted roots that roles now recover by path arithmetic ([Y01-11](#y01-11)). Python
   then keeps shape,
   type and bound checks and drops the product rules ([P01-05](#p01-05)). Size L.
3. *One Go owner per rule.* Disk identity and size ([G14-03](#g14-03), [G16-09](#g16-09)), the
   lookup of a named entry ([X05-03](#x05-03)), the issue cap (X05 family 15), the
   endpoint resolver ([G17-04](#g17-04), [G21-06](#g21-06)), the hosted layout ([G17-03](#g17-03)), the image
   reference parser ([G05-02](#g05-02)), the content-digest mechanism ([X05-06](#x05-06)), the
   typed adapter outcome ([X05-02](#x05-02)), the argument-specification validator used
   by five test files ([X05-04](#x05-04)), the shape walker ([G20-03](#g20-03)), the value-to-native
   converter (G22 Patterns), the vulnerability-exception validator ([G05-03](#g05-03)),
   the verified download in the tools ([B01-07](#b01-07)), and one documentation
   fragment for the options that 41 module files repeat ([X06-02](#x06-02)). Size L,
   mostly S slices.
4. *The CLI stops restating domain rules:* the authorization grammar
   ([G03-02](#g03-02)), the media name ([G03-04](#g03-04)), `info` selection ([G03-03](#g03-03)), the diff
   verdict ([G04-05](#g04-05)). Size M.

Exit evidence: for each rule, a grep finds one definition; for each
cross-language pair, one fixture read by both sides.

**W4. Capability kit.**

1. One effect-reading helper and one mutate and observe skeleton in the
   lifecycle package, with hooks for the real differences ([X05-01](#x05-01), [G15-01](#g15-01),
   [G21-03](#g21-03)); move the substrate arms, agent install, the artifact server, the
   managed service, mirror fill and the package tree onto it one per slice.
2. One administrator-credential helper for the two untested copies, tested
   once ([G21-02](#g21-02), [G18-03](#g18-03)).
3. Fold the artifact server into the managed service as one more definition,
   in Go and in the role ([G17-01](#g17-01), [Y04-01](#y04-01), [P06-01](#p06-01), [P06-04](#p06-04)). Contract slices.
4. One generic fill for the three mirror-fill capabilities ([G17-02](#g17-02)).
5. An action table for the Ceph request and its Python dispatch ([G18-01](#g18-01),
   [P01-03](#p01-03), [P03-05](#p03-05)), and shared helpers for the Ceph data services ([G19-02](#g19-02),
   [G19-03](#g19-03), [G19-04](#g19-04), [P02-01](#p02-01), [P02-03](#p02-03)).
6. One bounded fan-out helper for read-only work, used by diff observation
   and by W7 ([G13-02](#g13-02)).

Exit evidence: the capability contract suite passes unchanged; the contract
goldens move only in the slices marked contract; the shape-clone table no
longer pairs capabilities.

**W5. Right language.**

1. *Jinja to Python:* the installer wait as one module with a typed outcome
   ([Y03-01](#y03-01), which also removes the hand-written executor model in its test);
   one cluster read through the existing `oc` runner ([Y03-02](#y03-02)); one guest
   command module on the Python SSH builder, replacing ten copies of the
   session ([Y01-01](#y01-01)); one listener proof with one failure classifier in place
   of six proofs and eleven classifier copies ([Y01-03](#y01-03), [X07-02](#x07-02)); one identity
   verdict used by power and erase ([Y01-05](#y01-05), [Y02-05](#y02-05)); the install
   configuration composed by a plugin ([G21-04](#g21-04)); diagnostics maps moved
   beside their codes with a totality test ([Y01-08](#y01-08)).
2. *Python to Go:* an explicit `unknown` evidence field in place of a
   rewritten observation and an unread classification ([P06-05](#p06-05)); the exact
   comparison list emitted by Go ([G22-09](#g22-09)); the readiness predicate, after
   the owner's decision ([P01-06](#p01-06)).
3. *Strings to files:* the erase runtime, the probes, the discovery boot
   scripts and the adapter bootstrap as real files under test and lint
   ([P04-03](#p04-03), [P03-09](#p03-09), [X07-05](#x07-05), [G02-10](#g02-10));
   the copy worker under the Python floor gate ([G14-08](#g14-08)); the release recipe
   as a Go tool run as one recipe line ([B01-03](#b01-03)); the emitted shell helpers
   written once ([G16-05](#g16-05)).

Exit evidence: the Jinja volume of section 2.5 drops, with no task above
1,200 characters; the language fitness test maps every moved file.

**W6. Role and collection tests.**

1. One integration target per role family that runs an entry point with a
   local connection against a temporary root and stubbed executables, twice,
   and asserts a completed record and no change on replay ([PT1-01](#pt1-01)).
2. One shared task walker and interpreter for the role tests, checked
   against ansible-core on a fixture play, replacing about 18 partial ones
   ([PT1-02](#pt1-02), [PT1-04](#pt1-04)).
3. A stubbed-run test for every module, as action plugins already have
   ([PT1-03](#pt1-03), P07 Coverage notes).
4. Run only the fork and signal tests serially ([PT1-06](#pt1-06)) and convert the one
   test that still signals its own process ([PT1-05](#pt1-05)).

**W7. Performance.**

1. *Parallel probes* on the W4 fan-out helper: quiescence ([G13-01](#g13-01)), storage
   guest checks ([G20-06](#g20-06)), capacity ([G15-04](#g15-04)), copy preflights and power
   surveys ([G14-05](#g14-05)), node reads ([Y03-03](#y03-03)), tool downloads ([X03-02](#x03-02)).
2. *Read once:* a firewall snapshot per check ([G06-02](#g06-02), [P05-01](#p05-01)), one profile
   query ([P05-02](#p05-02)), one package transaction ([Y02-09](#y02-09)).
3. *Compute once per decision:* compilation keyed by the sources digest
   ([G12-02](#g12-02)), admission before the rule loop ([G01-01](#g01-01)), placements per cluster
   ([G20-02](#g20-02)), derived requests per plan ([G16-10](#g16-10), [G21-08](#g21-08), [G18-08](#g18-08), [G22-03](#g22-03)), a
   dependency counter in the scheduler ([X03-01](#x03-01)).
4. *Stores:* verify and sync once per directory ([G08-02](#g08-02)), re-prove only the
   changed records on commit ([G08-03](#g08-03)), ask the area for its usage ([G11-01](#g11-01)),
   flush adapter output by size or timer ([G11-02](#g11-02)), test existence without a
   read ([G11-05](#g11-05)), keep the secret count across writes ([G09-02](#g09-02)), check
   identity reservations on publish only ([G10-01](#g10-01)).
5. *Suites:* `t.Parallel` in the lifecycle, runner and composition tests
   ([G13-05](#g13-05), [G11-10](#g11-10)), a compiled-example cache (X03 Coverage notes, [E01-11](#e01-11)),
   memoized Python source shipping with a headroom test ([P03-01](#p03-01)).

Exit evidence: the package timings of section 2.2, measured again.

**W8. Structure.**

1. A fitness rule for context-level dependencies, seeded from today's graph
   with a reason per edge ([X04-01](#x04-01)); and the two Go network contacts that
   other spec pages already sanction, named in the architecture's list
   ([X07-06](#x07-06)).
2. Host reservations moved to their owner with a typed key ([X04-02](#x04-02), [G06-01](#g06-01));
   controller evidence split from controller selection ([X04-05](#x04-05), [G06-05](#g06-05)).
3. Per-command facts on the catalog and renderers chosen by result type
   ([G03-01](#g03-01), [G04-01](#g04-01)).
4. The lifecycle service: an invocation value in place of 13 to 18
   parameters ([G13-04](#g13-04)), declared ports in place of run-time assertions
   ([G12-04](#g12-04)), then the administrator-credential service and the decision
   component split out ([G12-05](#g12-05)).
5. The context store: the file kernel as a sub-package (W2.2), then media
   and trust as their own stores, and one read prologue ([G08-04](#g08-04), [G08-05](#g08-05),
   [G09-03](#g09-03)).
6. One capability per package in managed-OS installation and in Ceph
   ([G16-04](#g16-04), X04 Patterns); the acquisition domain out of the composition
   root ([G05-01](#g05-01), [G05-05](#g05-05)); per-arm admission out of the substrate root
   ([G15-03](#g15-03)).
7. A fitness test that resolves every literal path read against the schema
   ([G01-03](#g01-03)), typed lexical rules ([G01-02](#g01-02)), typed arms, connections and
   context names ([X04-04](#x04-04)), then typed views for the two hottest kinds
   ([X04-03](#x04-03)).
8. The Ceph worker split into collaborators ([P01-03](#p01-03)) and the evidence
   schema and transport out of `ceph_core.py` ([P03-05](#p03-05)).

**W9. Examples and end-to-end cases.** A shared base for the end-to-end
templates that every case repeats ([E01-04](#e01-04)); one description of the isolation
workload ([E01-05](#e01-05)); every documented switch rendered in the gate ([E01-03](#e01-03));
pinned `helm` and `python3` for the tests that need them ([E01-02](#e01-02), [T02-07](#t02-07));
tests for the workloads that acceptance evidence trusts ([E01-07](#e01-07)).

### 6.3 Order

1. **First:** W0 and W1 together. They are independent, small, and W1 makes
   everything after it stick.
2. **Second:** W2 and W3, which remove the drift risk in the code that
   matters most and prepare W4 and W5.
3. **Third:** W4 and W5, then W6 once W5 has reduced what roles do.
4. **Alongside, as capacity allows:** W7 and W9, which touch little that the
   others move.
5. **Last and in steps:** W8, each step behind the gates of W1 and with the
   contract suite as its safety net.

## 7. Decisions for the owner

Each of these changes what a workstream does, and only the owner can take
it. The recommendation is the orchestrator's.

1. **A setup state that cannot be read ([G09-01](#g09-01)).** Today the code, following
   the controller-record spec, treats it as written by another build and
   offers to discard it. The alternative is to refuse with an I/O error and
   keep the state. *Recommendation:* refuse on I/O, bound and permission
   failures; keep "another build" for content that decodes with another
   format. The cost is one spec sentence.
2. **Who judges Ceph readiness ([P01-06](#p01-06)).** Today Python computes `ready`
   and Go checks that the flags are consistent; the Ceph spec places this in
   the adapter, the architecture says Python never judges a postcondition.
   Either Python publishes the raw checks and Go judges, or the exception is
   named. *Recommendation:* name the exception now and move the judgment
   when W4 reshapes the Ceph request, because it is a large change with
   little immediate risk.
3. **A coverage gate.** Adopt per-package floors that only rise, and choose
   the Python tool: a pinned coverage library in the check environment, or a
   small recorder on the standard library. *Recommendation:* adopt the
   floors; use the recorder to avoid a new dependency.
4. **A pinned static analyzer.** It is a new tool dependency, which the
   dependency rule asks to weigh. *Recommendation:* add it; it is the only
   way dead code and dropped errors are found without a reviewer.
5. **Golden helpers per package ([T02-11](#t02-11)).** Keep the 23 copies and add a
   drift check, or allow one shared test-support package. *Recommendation:*
   keep the policy and add the check.
6. **The role preamble ([Y01-09](#y01-09), [Y04-09](#y04-09)).** A structural test allows only
   inline assertions before the handoff, which forces each role to repeat
   its validation in three files. Allowing an include of an
   assertions-only file removes about a thousand lines. *Recommendation:*
   allow it.
7. **Identity reservations in the keyring ([G10-01](#g10-01)).** The spec keeps them
   for good; the directory is capped at 32,768 entries and every apply adds
   some. Decide whether to compact them into one authenticated record or to
   accept the limit. *Recommendation:* compact them; this needs a spec
   change and is safety class.
8. **The native Ceph transport ([P01-01](#p01-01)).** Admission refuses it and the code
   for it is unreachable. Delete it with its 42 test patches, or park it as
   an item. *Recommendation:* delete it; the project carries no code for
   futures it has not promoted.
9. **Examples and end-to-end data ([E01-04](#e01-04)).** Examples are self-contained by
   their own rule. A shared base is cheap only for the end-to-end cases.
   *Recommendation:* share the base for end-to-end cases; leave examples as
   they are.
10. **Where this plan is tracked.** An audit authorizes nothing. If the
    owner wants the plan carried out, the workstreams become items on a
    milestone page and a decision in the backlog, as audits 002 and 004 did.

## 8. Method, verification and limits

### 8.1 How the audit ran

- **Lanes.** 45 review lanes: 22 for the Go packages by bounded context, 2
  for the fitness suite and the Go test strategy, 7 for the collection's
  Python, 4 for the roles, 1 for the collection's test design, 1 for build,
  CI and tools, 1 for end-to-end cases and examples, and 7 cross-cutting
  ones (Go security primitives, secret flow, concurrency and efficiency,
  domain design, duplication in Go and across languages, duplication in
  Python and YAML, language placement).
- **Models.** Each lane was one Claude Opus 5.5 reviewer at medium effort,
  except the end-to-end and examples lane, which was Claude Sonnet 5.5 at
  high effort. Claude Fable 5.2 planned the lanes, gathered the measured
  data, re-checked findings and wrote sections 1 to 9. The cross-cutting
  lanes started after the package lanes and read their saved reports, so
  that they consolidate and do not repeat.
- **One brief for all.** Every reviewer received the same brief: the eight
  dimensions, the project's own yardsticks (the architecture and security
  specs, the Go, Ansible and Python references, the security skill), the
  deliberate policies that are not findings, the rule to grep audit 003 and
  the milestone pages before reporting, an evidence rule (a file and line
  and the decisive lines quoted, counter-evidence looked for and stated) and
  a fixed report format with a cap of 15 findings.
- **Measured data.** Reviewers received the tables of section 2 as files:
  coverage by package, file and function; complexity by function; clone
  pairs; signal hits; the import graph; Jinja volume by task. They were
  small tools written for this audit: a `sys.monitoring` recorder loaded as
  a pytest plugin, a clone detector, two metrics programs on the Go and
  Python syntax trees, and a regex signal scan. They were kept outside the
  repository and installed nothing into the pinned environments.
- **Commands run.** The Go suite twice with coverage (`./scripts/go test
  -cover`, then with `-coverpkg` over the module) and the collection's plain
  pytest loop once on a copy of the collection, as the gate's plain lane
  does. One Go test failed in both coverage runs, for the reason in [G07-02](#g07-02);
  11,184 Python tests passed and 1 was skipped. The audit ran no
  `ansible-playbook` against a host, no Bootwright command and nothing
  against a host. The collection's own unit tests start ansible-core against
  a throwaway local inventory, as they do in the gate.

### 8.2 What was verified, and how

- The orchestrator re-checked every finding in the first table of section 5
  and all 11 High findings against the source: it re-read the cited code,
  repeated the counts and comparisons they rest on (for [T02-01](#t02-01) in the
  coverage data, for [Y04-01](#y04-01) by comparing the two files with their names set
  aside), and reproduced two mechanisms with standalone programs ([G10-02](#g10-02),
  [X01-01](#x01-01)). Every finding it re-checked held. One was narrowed ([P06-03](#p06-03)) and
  one count was corrected: the two destroy files of [Y04-01](#y04-01) differ in six
  lines, not four.
- The cross-cutting lanes re-opened the code behind many package-lane
  findings while consolidating them, and cite the ids they confirmed.
- There was no second, independent refutation pass over all findings, as
  audits 003 and 004 ran. The remaining findings are single-reviewer
  findings with quoted evidence and a stated confidence. Treat a Low or
  medium-confidence finding as a lead to confirm when its slice starts.

### 8.3 Limits

- **Static review.** Nothing here was observed on a live system. A finding
  about behavior under load, on another kernel or on real hardware is an
  argument from the source.
- **Sampling.** Production code in each lane was read in full or nearly so;
  each lane record lists what was skimmed or not read. Tests were sampled:
  270,000 Go test lines and 92,000 Python test lines were not read line by
  line.
- **Coverage figures.** Statement coverage says a line ran, not that a test
  would notice if it were wrong. The Python figure counts only code run in
  the test process. Coverage of code reached only in a re-executed child
  process is not counted in either language.
- **Duplication figures.** The detector sees repeated lines, not repeated
  ideas, and it counts documentation blocks. Section 4.6 rests on the
  reviewers' reading more than on the detector.
- **Scores.** They are judgments by different reviewers of different
  slices.
- **Severity.** Reviewers assigned it. The plan's order is the
  orchestrator's and does not always follow it: a Medium defect in section
  5 comes before a High structural finding.
- **Scope.** The specs were the yardstick and were not audited; audit 004
  did that. Findings that a milestone item or audit 003 already records are
  cited in the lane records and not counted again.

## 9. After the baseline

Four commits landed on main while the audit ran. The findings stay on the
audited commit; this section says what the later commits change for them.
The figures of section 2 were not measured again.

### 9.1 Three small commits

- `21ccc6234` normalizes templated discovery arguments in one action plugin.
- `ed7bae2d6` shares one media identity check through disk discovery.
- `fe3ad28d3` admits parallel discovery media builds by capacity and adds
  `plugins/module_utils/discovery_capacity.py` (182 lines) with its tests.

They change three plugin files this audit read
(`plugins/action/managedos_disk_discovery.py`,
`plugins/module_utils/disk_discovery_controller.py`,
`plugins/module_utils/disk_discovery_media.py`), their tests, one knowledge
page and two spec pages. The orchestrator read the three diffs:

- No finding is resolved by them. [P04-04](#p04-04) (the identity proof written twice)
  and the P04 coverage notes still apply; line numbers in
  `disk_discovery_controller.py` moved by a few lines.
- The new module `discovery_capacity.py` was not reviewed.
- `21ccc6234` is worth reading beside [PT1-01](#pt1-01). It fixes a failure that
  appeared when logic moved from a module into an action plugin: values
  templated by Ansible carry Ansible's own types, and an exact type check
  refused them. The fix restores the JSON boundary by hand. That is the
  class of defect that only a run through ansible-core shows.

### 9.2 Disk discovery over SSH

`e0940ce15` replaces the HTTPS endpoint of the discovery RAM boot with a
restricted SSH server that allows one forced inventory command. It changes
42 files (1,172 lines added, 404 removed): eleven plugin files, three of
them new, the discovery role's messages, the Go discovery request (a frozen
`ssh.port`, default 22), the host packages of the installer-media
requirement, two contract goldens, tests, three spec pages, two milestone
pages and a knowledge page. Its message records that the full gates were
deferred during the owner's interactive end-to-end run.

The orchestrator read the diff of each changed production file that a
finding cites, and the new RAM-side module:

- **Partly resolved: [P04-10](#p04-10).** The finding has two parts. The report file
  that nothing read is now what the forced command returns
  (`disk_discovery_sshd.report`), so that part no longer holds. The check
  that can never fire is still there, now at `disk_discovery_worker.py:68`.
- **Still apply, at new lines.** [P04-04](#p04-04): `Controller.target` is unchanged,
  now at `disk_discovery_controller.py:326`. [P04-02](#p04-02): the RAM image ships one
  more shared module the same way. [X07-05](#x07-05): `INIT` and `DRIVERS` now start at
  `disk_discovery_boot.py:55` and `:80`; no test runs either script, and no
  test asserts `INIT`. The P04 coverage notes are unchanged in substance.
- **Still applies, and larger: [Y01-08](#y01-08).** The Python map from refusal text to
  code grew from 29 to 47 entries, and the role's message map gained the
  matching codes.
- **A step in the direction this report recommends: [Y01-01](#y01-01).** The new
  discovery client builds its argv with the shared Python builder, which
  gained a bounded `port` argument, and runs it under the bounded process
  runner. It does not write another session in YAML. The ten YAML copies of
  the session remain.
- **Untouched in substance:** the Go findings that cite `hostpackages.go`,
  `inspect.go`, `clients/requests.go` and `contract_identity_test.go`. The
  commit changes a package list, a digest recipe and one request field
  there.
- **Removed code that no finding cited:** the worker's HTTPS server, and the
  controller's certificate preparation and pull.
- **Not reviewed:** `disk_discovery_ssh.py` (164 lines),
  `disk_discovery_ssh_sources.py` (144 lines) and the two test files added
  with them. `disk_discovery_sshd.py` (87 lines) was read for this section
  but not reviewed. One note for the list of embedded languages in the X07
  record: the RAM boot's `sshd_config` is a string of about 40 lines in
  that module, the same form that [P04-03](#p04-03) cites in the erase runtime.

## 10. Findings catalog

Every finding, one row each, grouped by the severity its reviewer assigned.
The id links to the full entry in the lane record, which holds the location,
the quoted evidence, the impact, the proposed fix and what the reviewer
checked. "Where" is the first location the finding cites; paths under
`internal/` and under the collection are shortened. Effort is the reviewer's
estimate for the fix: S, M or L, or a range such as S-M; an empty cell means
the reviewer gave none.

### High findings (11)

| ID | Dimension | Where | Finding | Effort |
| --- | --- | --- | --- | --- |
| [G04-01](#g04-01) | Clearness/Non-duplication | `results.go:43-133` | Rendering is routed by literal command-path strings in two separate switches: about 45 paths in `writeResult` and about 30 in `writeAdditionalStructuredResult`. | M |
| [G05-01](#g05-01) | DDD | `cmd/bootwright/acquisitions.go:32-35` | The whole acquisition domain sits in package main: vendor hosts, release-signature stores, catalog index repositories, CDN companions, OCI reference parsing, and the managed mirror's fill sets. | M |
| [G14-01](#g14-01) | Quality | `machine/access/copy.go:69-76` | For a single Machine, `Copy` builds its `member` without `privateKeyPart`. | S |
| [G17-01](#g17-01) | Non-duplication | `infrastructureservices/artifactserver/capability.go:43-311` | `artifactserver.Capability` repeats `managedservice.Capability` almost line for line: Plan, the description/impacts/groups helpers, Removal, mutate, Observe and ObserveRemoval, the effect function, prepare and run, plus ValidateAbsence, ValidatePartial, Valid… | L |
| [G18-01](#g18-01) | DDD | `requests.go:265-312` | `Request` is one struct with an `Action string` and 12 optional arm pointers. | M |
| [T02-01](#t02-01) | Test coverage | `secrets/localkeyring/mutations.go:215` | The only production secret-rebind path, the erase-receipt check that Ceph physical retirement relies on before it releases a claim, and both admin-credential implementations are run by no test in the module. | M |
| [PT1-01](#pt1-01) | Test coverage | `tests/unit/test_playbook_entry_points.py:258-270` | No role entry point runs past argument validation through ansible-core in the gate. | L |
| [Y01-01](#y01-01) | Non-duplication | `roles/managedos_install_anaconda/tasks/guest.yml:11-118` | The pinned guest SSH session (policy read, known_hosts, ssh_config, host-key algorithms, cleanup) is written out in 10 task files. | M |
| [Y03-01](#y03-01) | Right language | `roles/containercluster_install_agent/tasks/wait_attempt.yml:86-104` | The installer wait's resume policy is written as Jinja over regexes of `openshift-install` stderr: whether to try again, whether a stall was seen, which hint to give, and whether to read the assisted service. | M |
| [Y04-01](#y04-01) | Non-duplication | `roles/infra_artifact_server_nginx/tasks/destroy.yml:1-181` | The artifact-server role repeats the managed-service lifecycle task for task: the podman probe, socket and bind refusals, the pinned pull, hosted directories, daemon-reload, the restart rule (start time compared with file mtimes), the reset-failed handling an… | L |
| [X04-02](#x04-02) | DDD | `controller/prerequisites/storage.go:96` | [G06-01](#g06-01) reports that the host-reservation value lives in Controller setup. | M |

### Medium findings (207)

| ID | Dimension | Where | Finding | Effort |
| --- | --- | --- | --- | --- |
| [G01-01](#g01-01) | Efficiency | `desiredstate/compilation/phases.go:233-253` | For every record, `normalizeRecord` runs all 13 rule sets. | S |
| [G01-02](#g01-02) | Quality/Clearness | `api/v1alpha1/lexical.go:57-155` | Lexical rules are free strings. | M |
| [G01-03](#g01-03) | DDD | `api/v1alpha1/value.go:72-84` | Contexts read the generic `api.Value` tree by string path: 1,487 `.Spec().Get(`/`.Spec().Has(` sites across storage, containercluster, substrate, machine and managedos. | M-L |
| [G01-04](#g01-04) | Non-duplication/Security | `desiredstate/inputfs/reader_linux_amd64.go:719-775` | The same descriptor-walk primitives are copied: `processFiles`, `denied`/`openDenied`/`deniedToRoot`, fstat, stable-identity comparison and bounded read. | M |
| [G01-05](#g01-05) | Clearness | `desiredstate/compilation/schema_validation.go:12` | `validateShape` takes 8 parameters, including two adjacent bools, and call sites pass bare `true, false` / `false, true`. | S |
| [G02-01](#g02-01) | Non-duplication | `environment/clusterpreflight/combine.go:52-70` | Three packages each count `capacity.Check` statuses into a Summary, and two of them also copy the "not proved" verdict (the error code chosen by Failed versus Unknown/Pending). | S |
| [G03-01](#g03-01) | Clearness/Non-duplication | `inherited_scope.go:16-30` | The spec says catalog.go is "the single list of commands ... | M |
| [G03-02](#g03-02) | DDD/Non-duplication | `validation.go:212-226` | The CLI re-parses the whole `--authorize` grammar: data-loss, a 16-hex digest, and `rebind=<name>` once each. | S |
| [G03-03](#g03-03) | DDD | `info_selection.go:14-76` | `machine info` and `secret info` fetch the whole list, filter it, and refuse unknown names with an `access.target` domain diagnostic, all inside the CLI. | M |
| [G04-02](#g04-02) | Security | `output_uninstall.go:51-56` | `RemovalOutput` hands the raw stdout writer to dnf. | S |
| [G04-03](#g04-03) | Security/Quality | `output_additional.go:180-207` | In structured mode, when a reveal result fails the structured guard, the function returns `(false, nil)`. | S |
| [G04-04](#g04-04) | Quality | `results.go:56-65 (compare results.go:66-71 and 184-188)` | Human `machine discover` never writes `Operation.Warnings` or the dry-run `lifecyclePlan.Warnings` to stderr. | S |
| [G04-05](#g04-05) | DDD | `output_diff.go:279-293` | The output layer computes the diff verdict. | M |
| [G05-02](#g05-02) | Non-duplication | `cmd/bootwright/acquisitions.go:362-364` | The rule that qualifies a short image name to docker.io is written twice, and the two copies differ. | S |
| [G05-03](#g05-03) | Security | `cmd/bootwright/billofmaterials.go:43-49` | The embedded vulnerability-exceptions document is decoded and judged twice, with different rules. | S |
| [G05-04](#g05-04) | Efficiency | `cmd/bootwright/wiring_firewall.go:172-184` | The transitive dependency closure of a plan is written twice: once as a recursive `visit` over a map, once as a BFS through `plan.Block`, which scans linearly and clones every block it returns (request bytes included). | S-M |
| [G05-05](#g05-05) | DDD | `cmd/bootwright/wiring_discovery.go:28-51` | Application work and decoding that the named exceptions do not list: - `machineDiscovery.Discover` orchestrates Prepare, gates on the receipt state and collects evidence per Machine. | M |
| [G06-01](#g06-01) | DDD | `controller/prerequisites/reservations.go:1-462` | The cross-context host-reservation model (HostReservation, Conflicts, UseConflicts, ConflictDiagnostic, the release guards) lives in the Controller setup package. | M |
| [G06-02](#g06-02) | Efficiency | `controller/firewall/check.go:40` | CheckSockets inspects each socket serially. | M |
| [G06-03](#g06-03) | Non-duplication | `controller/selection.go:20-32` | The same seven native closures (container runtime, libvirt client, hypervisor, installer media, image mirror, nmstate, iputils) appear as four parallel bool structs, with field-by-field converters and three switch or if ladders. | M |
| [G06-04](#g06-04) | Non-duplication / Security | `hostdnf/packages_linux_amd64.go:455-520` | There are three hand-written root-run process runners whose supervision differs, plus three bounded writers and two output sanitisers. | M |
| [G06-05](#g06-05) | DDD / Non-duplication | `controller/selection.go:173-190` | The root `controller` package imports addons, containercluster, infrastructureservices, machine and substrate. | M |
| [G07-01](#g07-01) | Quality | `controller/venvlocal/build_linux_amd64.go:277-294` | `runCommand` runs `python -m venv`, pip, `compileall -j0` and the probes through `exec.CommandContext`. | S |
| [G07-02](#g07-02) | Test coverage | `controller/privilege/terminal_hint_linux_amd64_test.go:80-84` | The test re-executes the test binary twice, with `Env` replaced by `PATH` and `COLORTERM`. | S |
| [G07-03](#g07-03) | Clearness | `controller/privilege/sudo.go:99-193` | Each of these functions packs several phases into one body. | M |
| [G07-04](#g07-04) | Non-duplication | `controller/stagelocal/staging_linux_amd64.go:368-504` | `emptyStage`, `emptyStageChild`, `stageEntries`, `directory` and `file` are the same code as `emptyTree`, `emptyChild`, `entries`, `runDirectories.dir` and `runDirectories.file`, down to the sort trick and the error strings. | M |
| [G07-05](#g07-05) | Non-duplication | `nativeexec/executable.go:17-43` | The rule "root-owned, no group or other write, up to /" is implemented at least four times in this lane, and the copies disagree. | M |
| [G08-01](#g08-01) | Quality | `workspace/contextfs/snapshots_linux_amd64.go:136-162` | `Publish` creates the revision directory itself, not a stage. | S |
| [G08-02](#g08-02) | Efficiency | `workspace/contextfs/deletion_linux_amd64.go:211-225` | Tree removal runs `unlinkVerified` (which verifies the parent) and then `syncDirectory` (which verifies again and fsyncs) for every entry. | M |
| [G08-03](#g08-03) | Efficiency | `workspace/contextfs/repository_linux_amd64.go:523-533` | Every read and every transaction fully reads and decodes every ready context's manifest (the second loop of `verifyMappingsExcept`). | M |
| [G08-04](#g08-04) | DDD / Clearness | `workspace/contextfs/repository_unsupported.go:26-114` | One `Store` implements the contexts Repository, lifecycle (Read, Run, Mutate), media, SSH trust, secrets, logs, controller and setup, about 14,700 lines and 25 port methods. | L |
| [G08-05](#g08-05) | Non-duplication | `workspace/contextfs/reconciliation_linux_amd64.go:188-230` | The prologue that opens the root, locks it, reads the registry, verifies the mappings, opens the context directory and checks its recorded inode is copied five times. | M |
| [G09-01](#g09-01) | Quality | `workspace/contextfs/setup_linux_amd64.go:106-110` | A transient read or listing failure of the setup subtree is classified as "another build's format", and `controller install` then plans `DiscardForeign`, which deletes the receipt, the runtimes and the wheel cache. | S |
| [G09-02](#g09-02) | Efficiency | `workspace/contextfs/secrets_linux_amd64.go:720-722` | `capacity` calls `scan`, which opens and stats every secret entry, on every `WriteExclusive`, `PublishExclusive`, `Replace` and `EnsureDirectory`. | M |
| [G09-03](#g09-03) | Non-duplication | `secrets_linux_amd64.go:126-176 and :178-242` | Three copies of the same block that lends a `secretArea`: build the area, defer its close and the secrets-directory close, open `secrets` while tolerating ENOENT, scan, then `safeError(callback(...))`. | S |
| [G09-04](#g09-04) | Non-duplication / Clearness | `controller_uninstall_linux_amd64.go:715 emptyRenamedTree` | Three recursive removers implement the same depth-bounded, same-device, identity-checked unlink walk and differ only in the predicate that admits an entry. | M |
| [G10-01](#g10-01) | Efficiency | `localkeyring/artifacts.go:122-158` | Each `refreshMetadata` call decrypts and checks every ledger and reads and decodes every `identities/*.json` file. | M |
| [G10-02](#g10-02) | Security | `secrets/values.go:27` | `Material` hides its bytes only through `String`, `GoString` and `MarshalJSON`. | S |
| [G10-03](#g10-03) | Non-duplication | `localkeyring/mutations.go:47-55` | The same "collect identity names into known IDs" prologue appears in four places. | M |
| [G10-04](#g10-04) | Test coverage | `localkeyring/mutations.go:215 (session.Rebind` | The only production path that rebinds an access credential has never run against the real keyring. | S |
| [G10-05](#g10-05) | Clearness | `material/generation.go:256-341 (cyclomatic complexity 55)` | Validating a generated certificate packs 15 to 20 conditions into single expressions. | S |
| [G11-01](#g11-01) | Efficiency | `reconciliation/operationstore/store.go:301-334` | `AdmitAttempt` runs for every attempt and resolution. | M |
| [G11-02](#g11-02) | Efficiency | `reconciliation/operationstore/logs.go:130-147` | Every `AdapterOutput.Write` publishes at once through `Area.Append`. | S |
| [G11-03](#g11-03) | Non-duplication | `reconciliation/ansiblerunner/jobs_linux_amd64.go:607-672` | `emptyTree`, `emptyChild` and `entries` copy `emptyStage`, `emptyStageChild` and `stageEntries` line for line. | M |
| [G11-10](#g11-10) | Test coverage (efficiency) | `reconciliation/ansiblerunner/process_linux_amd64_test.go:218-260` | This is why the suite takes about 35 seconds. | S |
| [G12-01](#g12-01) | Quality | `reconciliation/lifecycle/admin_credentials.go:189-196` | `adminProduced` returns `found=false` together with an error. | S |
| [G12-02](#g12-02) | Efficiency | `decision.go:91` | With no verb given (plan), `decideFrom` runs `s.additive(..., true)` only to choose the verb and drops its error; `decideBy` then runs it again. | M |
| [G12-03](#g12-03) | Non-duplication / Security | `admin_credentials.go:213-233` | The rule "is the pending update valid and already applied" is implemented twice, and the two copies differ: - The update path compares the password with `==` on a Go string; the read path uses `subtle.ConstantTimeCompare`. | S |
| [G12-04](#g12-04) | DDD | `contracts.go:129-138` | The `SecretBinder` port declares 8 methods. | S |
| [G12-05](#g12-05) | DDD / Clearness | `service.go:33-79` | `Service` has 224 methods over 66 production files, 10 fields, a 27-field `Options` and a 9-parameter `New`, and acts as the planner, registrar, executor, status reader and admin-credential use case at once. | L |
| [G13-01](#g13-01) | Efficiency | `reconciliation/lifecycle/removal_gates.go:242` | `proveQuiescent` probes every block one at a time inside one `withPython`. | M |
| [G13-02](#g13-02) | Non-duplication | `reconciliation/lifecycle/diff_observe.go:147-187` | `inspectAll` is a second hand-built bounded scheduler: its own `running` counter, `throttled` map, buffered channel and cancel drain. | M |
| [G13-04](#g13-04) | Clearness | `reconciliation/lifecycle/attempts.go:39` | Each invocation's context is passed positionally on every call: tx, store, approved, boundary, operation, plan and material. | M |
| [G13-05](#g13-05) | Test coverage (efficiency) | `reconciliation/lifecycle/*_test.go` | Across 731 `Test` functions there is no `t.Parallel()`. | M |
| [G14-02](#g14-02) | Non-duplication / Security | `machine/sshlocal/policy.go:87-92` | Three implementations reduce the same `/etc/crypto-policies/back-ends/openssh.config`, and they disagree. | M |
| [G14-03](#g14-03) | Non-duplication | `machine/erase_admission.go:49-59` | The disk-identity grammar is redefined everywhere. | S-M |
| [G14-04](#g14-04) | DDD | `machine/selection.go:13` | The machine root is the Machine admission model, but it also holds context selection (`SelectedContext`), the generic per-object ownership vocabulary (`OwnershipState`, `VerbApply`, `Block*`), CLI operand parsing with CLI wording (`ParseCopyPaths`), and SSH s… | M |
| [G14-05](#g14-05) | Efficiency | `machine/access/copy.go:125-133` | Work against different hosts runs one after another although each piece is independent. | S |
| [G15-01](#g15-01) | Non-duplication | `substrate/libvirt/capability.go:308-423 and :446-561` | Each of the five capabilities re-implements the same lifecycle scaffolding: - the Plan loop and `Removal`; - `Apply`/`Destroy` → `mutate` (prepare, run, usable outcome, then the presence or absence check); - `observe`; - the five-check effect classification;… | M |
| [G15-02](#g15-02) | Non-duplication | `substrate/libvirt/selection.go:170-175` | The libvirt machine request derives the emulated controller itself: port = base + ordinal, its own credential and certificate refusals (same text as `virtualTarget`), and the endpoint. | S |
| [G15-03](#g15-03) | DDD | `substrate/admission.go:202-284` | The arm-neutral root holds libvirt and vSphere admission branches, all of libvirt managed-Network admission, and libvirt readers (`HostMachine`, `NeedsHypervisorClient`, `AddressClaims`). |  |
| [G15-04](#g15-04) | Efficiency | `substrate/capacity/budget.go:67-73` | `Budget.Check` observes each host and pool group one after another, and `addWrites` probes each distinct host and path one after another. | S |
| [G16-01](#g16-01) | Non-duplication | `managedos/installation/kickstart.go:949-977` | The rule for reading a disk's serial number and WWN from sysfs is implemented three times. | M |
| [G16-02](#g16-02) | Test coverage / Quality | `managedos/installation/erase_request.go:251-272` | `PhysicalEraseProof` has 0% coverage (13 of 13 statements unrun). | S |
| [G16-03](#g16-03) | Non-duplication / Clearness | `discovery.go:244-258` | Whether a discovery report belongs to its request is decided three ways. | S |
| [G16-04](#g16-04) | DDD / Clearness | `capability.go:24` | One package holds five lifecycle capabilities (install, physical erase, disk discovery, package tree, host firewall). | L |
| [G16-05](#g16-05) | Clearness / Right language | `clear.go:161-281` | A large destructive shell program lives as Go string slices: the device-mapper holder walk, `dmsetup` release, mount and swap scans, and `dd` zero-and-readback. | M |
| [G16-06](#g16-06) | Test coverage | `testdata/request-*.golden (16 files` | Each request golden stores the whole Kickstart as one escaped JSON string. | S |
| [G17-02](#g17-02) | Non-duplication | `infrastructureservices/mirrorfill/capability.go:46-287` | the three fill capabilities (Files, ImageFill, ReleaseFill) each repeat Plan, run (identity check, runner nil-check, Canonical, PinInstalledHost, RunFor), mutate, Observe, Unresolved, SecretReferences, SecretClasses and Claimed. | M |
| [G17-03](#g17-03) | DDD / Non-duplication | `infrastructureservices/artifactserver/catalog.go:22-29` | the hosted layout is declared twice: `HostedRoot = "/var/lib/bootwright-hosted"`, `servicesArea`/`ServicesArea` and `unitPrefix`/`UnitPrefix` exist in both packages. | S |
| [G17-04](#g17-04) | Non-duplication | `infrastructureservices/mirror.go:122-160` | there are two `EndpointURL` functions for one rule (endpoint, listener, placement address, `protocol://host:port`). | S |
| [G17-05](#g17-05) | DDD | `infrastructureservices/managedservice/requests.go:32-56` | the shared package holds DNS-only derivations (MachineRecords, ClusterRecords, nodeAddress, routed), which pull `containercluster` and `network` into every capability. | M |
| [G18-02](#g18-02) | Quality | `capability.go:419-420` | identity's exact-arm check covers only 7 of the 12 arms. | S |
| [G18-03](#g18-03) | Quality | `admin_credentials.go:59-89` | adminCredentialRun calls `c.runner.Run` directly, bypassing `invoke`. | S |
| [G18-04](#g18-04) | Right language | `produced.go:102-135` | Go (clientCaps, cephFSClientCaps) and Python (ResourceWorker.caps) each compute the client's capability grants from the same request fields. | S-M |
| [G18-05](#g18-05) | Non-duplication | `ceph_core.py:183-300 versus evidence.go:125-206 and 490-540` | the Python publisher re-implements the closed evidence member set and per-action resource-scope rules (subvolume path, NFS pseudo path, object user and bucket IDs, resource ID patterns) that Go's decodeEvidence and validateScope own. | M |
| [G18-06](#g18-06) | Quality | `inspection.go:148-185 versus proofs.go:47-63 and image.go:388-417` | Inspect finds the cluster and image proofs with looser lookups than execution does. | S |
| [G18-07](#g18-07) | Efficiency | `evidence.go:411-424 and 703-717 (via requests.go:703)` | the request carries `Services` and `Documents` as `[]json.RawMessage`, which Go itself marshalled from typed structs. | M |
| [G19-01](#g19-01) | Quality | `storage/ceph/export_evidence.go:232-237` | Every Quiescent turns any error (a decode failure, a physical policy refusal from `pendingPhysicalClusters`, and for the consumer also context cancellation) into `Unproved` with the fixed reason "hosts outlive this destroy (B491)". | S |
| [G19-02](#g19-02) | Non-duplication / DDD | `host.go:504-510` | The fleet-key file, the known-hosts builder, the `maps.Clone(e.Material)` and the cleanup closure are assembled four times. | M |
| [G19-03](#g19-03) | Non-duplication / Clearness | `disk_proof.go:28` | The predicate that selects "a completed original apply of kind K, object O and implementation I with a self-consistent block" is spelled out by hand nine times. | S |
| [G20-01](#g20-01) | Quality | `storage/cephcluster/devices.go:26` | A whole retired camelCase OSD vocabulary is still in the package: - `admission.osd`, `admission.selector` and `declaredDataDevices` read `dataDevices`/`pathSpecs`/`dbDevices`. | S |
| [G20-02](#g20-02) | Efficiency / DDD | `storage/cephcluster/native.go:46-55` | Each placement is resolved by building a fresh `admission{object: cluster}` and re-running the whole `nativePlacement` validator. | M |
| [G20-03](#g20-03) | Non-duplication | `storage/cephcluster/native.go:216-299` | `nativeShape` is a second interpreter of `api.Shape`, separate from compilation's `validateShape`. | M |
| [G20-04](#g20-04) | Non-duplication | `storage/cephcluster/native.go:563-576 vs smb.go:300-313` | Two rules are copied by hand: - The secret-binding pointer check, including its overlap loop. | S |
| [G20-05](#g20-05) | Non-duplication | `storage/cephcluster/native_devices.go:94` | The rules "TPM2 requires encryption" and "data/DB/WAL paths must not overlap" each exist in two places: admission (native_devices.go:88-95) and the ceph request builder. | S |
| [G20-06](#g20-06) | Efficiency | `storage/preflight/service.go:299-306` | Guest prerequisite checks run one host at a time. | S |
| [G21-01](#g21-01) | Test coverage / Quality | `containercluster/agentinstall/admin_credentials.go:24-66` | The console-password preparation has security-relevant branches that no test in the module runs: guard decode, the password length bounds, the hash step and re-encoding. | S |
| [G21-02](#g21-02) | Non-duplication | `agentinstall/admin_credentials.go:24-114` | `PrepareAdminCredential`, `UpdateAdminCredential` and `adminCredentialRun` are line-for-line the same skeleton in both packages: run, double `ClearProduced`, the `AdminCredentialProved` check, the `"guard"` output, `prepare_credentials` vs `update_credentials… | M |
| [G21-03](#g21-03) | Non-duplication | `agentinstall/capability.go:165-194` | Apply/Destroy → `mutate` → `prepare` → `run` → the `"changed"/"unchanged"` outcome switch → presence or absence validation, plus `Removal` and `observe`, are cloned per capability. | M |
| [G21-04](#g21-04) | Right language | `ansible/.../roles/containercluster_media_agent/tasks/build.yml:59-94` | Go freezes install-config with placeholders (projection.go:60-67), but the role composes the bound material in a 35-line Jinja expression. | M |
| [G21-05](#g21-05) | DDD / Efficiency | `openshift/endpoint_ownership.go:20-89` | `addressClaims` is a catalog-wide registry of who holds which address, and it lives in the OpenShift admission package. | M |
| [G22-01](#g22-01) | Non-duplication | `addons/clusterobjects/evidence.go:130` | `EvaluateOwnership`, `EvaluateDependent`, `OwnerMark`, `FieldManager`, `Probe` and `DependentProbe` are Go copies of the Python ownership and owner-mark rules. | S |
| [G22-02](#g22-02) | Quality | `addons/instance/selection.go:80-86` | `unrenderable` chooses its refusal text by substring-matching `err.Error()` against literals that copy the validator's message text. | S |
| [G22-03](#g22-03) | Efficiency | `cmd/bootwright/wiring_desiredstate.go:55` | - Admission: compilation runs every rule's `Validate` for every object (phases.go:281-288). | M |
| [T01-01](#t01-01) | Efficiency | `test/architecture/boundaries_test.go:253-265` | `productionSources` re-walks and re-parses all 918 production files on every call, and 27 tests call it. | S |
| [T01-02](#t01-02) | Efficiency/Clearness | `test/architecture/ (49 files)` | about 5,800 of the 13,700 lines are not Go architecture checks. | M |
| [T01-03](#t01-03) | Security | `test/architecture/boundaries_test.go:591-606` | the effect boundary is checked only on package-qualified selectors (`pkg.Member`). | M |
| [T01-04](#t01-04) | Test coverage | `boundaries_test.go:220 (permitsDependency)` | the dependency-direction matrix, the lifecycle planner and executor reachability rule (an approximately 700-line hand-rolled resolver) and the deadline-application rule run only against the real tree. | S-M |
| [T01-05](#t01-05) | Non-duplication | `requests_test.go:242-320` | there are three independent analysis engines: a real go/types loader, a hand-rolled scope and type inference (`lifecycleTypes.typeOf`/`callResults`), and a hand-rolled reference follower. | L |
| [T01-06](#t01-06) | Quality (gate gap) | `Makefile:226-235 (vet` | no Go analysis beyond gofmt and vet runs anywhere: no staticcheck, golangci-lint, errcheck or gosec in code, tools or workflows. | M |
| [T01-07](#t01-07) | Security (race) | `Makefile:277-278` | `make race` covers a hand-picked set: `./internal/reconciliation/... | S |
| [T02-02](#t02-02) | Test coverage | `cmd/bootwright/capability_contract_test.go:544-547 (rows at 175-220)` | 21 of the 42 contract rows are `completedFrom`. | M-L |
| [T02-03](#t02-03) | Quality (dead code) | `storage/cephcluster/devices.go:26 (admission.osd)` | Seven 0% functions have no caller at all. | S |
| [T02-04](#t02-04) | Test coverage | `controller/bundlelocal/trust_linux_amd64.go:30-35` | Security-relevant adapters hard-code the root, /proc paths, system executables and their own HTTP client. | M |
| [T02-05](#t02-05) | Non-duplication | `controller/firewalllocal/native.go:16-42` | About nine Go adapters each re-implement the same bounded process envelope: a capped writer, `Qualify`, `Dir="/"`, a clean `Env` and `WaitDelay`. | M |
| [T02-06](#t02-06) | Quality (determinism) | `controller/privilege/terminal_hint_linux_amd64_test.go:82-83` | Tests re-execute the test binary and compare `CombinedOutput` byte for byte. | S |
| [T02-07](#t02-07) | Test coverage (hermeticity) | `cmd/bootwright/e2e_cases_linux_amd64_test.go:193-194` | Unit tests fail without helm or python3 on PATH, and they run `e2e/render.sh`. | S-M |
| [T02-08](#t02-08) | Test coverage | `controller/privilege/process_linux_amd64_test.go:556` | The sudo account-provenance fixture needs root plus `BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE=1`. | M |
| [T02-09](#t02-09) | Clearness, policy trade-off | `cmd/bootwright/contract_identity_test.go:108-174` | These tests restate every implementation's ContentDigest inputs by hand, plus about 270 classified automation literals, so every pin or literal change edits them. | M |
| [P01-01](#p01-01) | Quality | `C/plugins/module_utils/ceph_native.py:2924-2966` | Go admission refuses `clusterSSHTransport: native`, and `Worker.bootstrap` requires `supported_transport`, which is false for anything but `verifiedTunnel`. | M |
| [P01-02](#p01-02) | Non-duplication / Security | `ceph_native.py:379-402 (registry_directory)` | five separate implementations open each component of a fixed path without following links and check owner and mode. | M |
| [P01-03](#p01-03) | Clearness / DDD | `ceph_native.py:1519-4741` | one Worker class owns registry custody (2166-2339), SSH authorization and sshd policy (2689-2891), tunnel bootstrap, configuration and mount (2923-3573), Call Home and license (3173-3290, 3574-3761), OSD device clearing (2341-2687), services (3791-4001), the… | L |
| [P01-04](#p01-04) | Security / Quality | `ceph_native.py:1400-1404` | the generic `Custody` record, which holds the dashboard password and the dashboard TLS pair, persists `st_dev` and compares it before every read. | S |
| [P01-05](#p01-05) | Right language / Non-duplication | `ceph_native.py:799-815` | `validate_config`, `validate_services` and `validate_hosts` repeat Go's admission rules, not just shape bounds: - the `osd_memory_target` floor of 2 GiB (`2 * 1024 ** 3 <= int(target)`); - the exact pool-default pair of 2 and 2; - `count_per_host == 1`; - tpm… | M |
| [P01-06](#p01-06) | Right language | `ceph_native.py:4447-4470` | Python computes `evidence["ready"]`: which health checks are tolerated, which scoped checks count for the block's pools, the PG state, the license and the daemon counts. | L |
| [P01-07](#p01-07) | Non-duplication | `ceph_package_worker.py:107-195 versus ceph_native_worker.py:288-391` | the package worker is an older copy of the supervisor. | M |
| [P01-11](#p01-11) | Test coverage | `storage/ceph/testdata/request-cluster-*.golden` | Go keeps request goldens, but no Python test loads one. | S |
| [P02-01](#p02-01) | Non-duplication | `ceph_native_object.py:572` | `recovery_scope` is copied 9 times in two shapes: 6 copies that drop one member, 3 that pop `delegated`. | S |
| [P02-02](#p02-02) | Non-duplication | `ceph_native_object.py:117-127 vs ceph_native_nfs.py:59-69` | Identical helpers are copied between sibling modules: `port`/`networks` (object.py already imports nfs), the `fs subvolume getpath` reader with the same docstring, three image-settings checks, and the stretch 4/2 constants. | S |
| [P02-03](#p02-03) | Non-duplication | `ceph_native_resources.py:370` | One transport-hold expression is repeated 11 times. | S |
| [P02-04](#p02-04) | DDD | `ceph_export_native.py:151-163` | Marker-record file names (`"pool-" + name + ".json"`, `"pool-id-"`, `"filesystem-id." + name`) and the RBD attempt record are rebuilt by hand in other modules. | M |
| [P02-05](#p02-05) | Right language | `ceph_native_nvmeof.py:33-62` | The NVMe-oF phase and execution budget formula, including the 6 h cap, is written identically in Go and Python. | M |
| [P02-06](#p02-06) | Clearness | `ceph_native_object.py:968-1149` | Code is reused through inheritance between unrelated identities. | M-L |
| [P02-07](#p02-07) | Quality | `ceph_native_nvmeof.py:126-129` | `nvme()` turns every Refusal into a bare `Refusal("nvmeof-native")` with `from None`. | S |
| [P03-01](#p03-01) | Efficiency | `ceph_core.py:586-596` | every `Transport.call` rebuilds `native_sources()` (29 modules through `inspect.getsource`) and sends it inside the stdin payload. | S |
| [P03-02](#p03-02) | Non-duplication | `ceph_host_runtime.py:500-555` | `pull` and `pull_reference` are copies of the same journaled pull loop: attempt replay, inspect-and-adopt, broker, signature policy, retry. | M |
| [P03-03](#p03-03) | Non-duplication | `ceph_tunnel.py:32-45 and 74-86` | the crypto-policy reducer (the same regex and the same 13 directives) and the key-type to host-key-algorithm table exist twice in Python, plus `hostkeys.go`. | S |
| [P03-04](#p03-04) | Non-duplication | `ceph_registry_discovery.py:243-251` | the registry Bearer challenge parser, token validation and Basic credential construction are implemented twice with different rules. | S |
| [P03-05](#p03-05) | Clearness / DDD | `ceph_core.py:1224-1313 (and the module as a whole)` | `execute` has 11 parameters, cyclomatic complexity 54 and depth 5 (gometrics/pymetrics). | M |
| [P04-01](#p04-01) | Security | `plugins/module_utils/physical_erase_worker.py:543-553` | `atomic` creates `<path>.new` with `O_EXCL` and never removes it when the write or fsync fails. | S |
| [P04-02](#p04-02) | Non-duplication | `physical_erase_process.py:19-84` | `physical_erase_process.run` is a second bounded process runner beside `bounded_process.py`. | M |
| [P04-03](#p04-03) | Right language | `physical_erase_boot.py:13-154 (RUNTIME)` | The RAM erase runtime (isolation, receipt server, ack and poweroff) is a raw Python string. | M |
| [P04-04](#p04-04) | Non-duplication | `physical_erase_redfish.py:41-49` | The BMC proof of system identity (UUID plus the complete MAC set) is written twice, and the copies have drifted. | S |
| [P04-05](#p04-05) | Clearness | `redfish_control.py:546-583` | `Client.insert` (complexity 35) evaluates the same trust predicates again under different names. | S |
| [P05-01](#p05-01) | Efficiency | `managedos_firewall_worker.py:486-497` | The firewall worker gathers evidence by starting one `firewall-cmd` process for each (service, interface zone) twice, and for each (service, zone in permanent∪runtime) once more. | M |
| [P05-02](#p05-02) | Efficiency | `libvirt_nm_connections.py:546-551` | `foreign_devices` runs `nmcli connection show uuid <u>` once for every profile the provider does not own. | S-M |
| [P05-03](#p05-03) | Non-duplication | `libvirt_routing.py:307-340` | Five sibling module_utils each implement stage, fsync and rename differently. | M |
| [P06-01](#p06-01) | Non-duplication | `plugins/module_utils/artifact_server.py:10` | artifact_server.py imports infra_service and calls `infra_service.started_after`, yet it carries its own verbatim copies of `probe_failure`, `invoke`, `podman`, `unit_state`, `container_image` and `container_present`, plus the constants. | S |
| [P06-02](#p06-02) | Quality | `plugins/module_utils/bounded_process.py:119-120` | `stop()` runs in `run()`'s `finally` and raises `Failure("command")` when the kill-and-reap does not finish within 1 s. | S |
| [P06-03](#p06-03) | Quality | `plugins/module_utils/controller_files.py:680-694` | tools are published with `O_TMPFILE` and then `linkat(fd, "", parent, leaf, AT_EMPTY_PATH)` through ctypes. | S |
| [P06-04](#p06-04) | Non-duplication | `plugins/action/artifact_server_protocol.py:61-133` | `remaining`, `unproved`, `absence`, `refused` and the postcondition expression are copied between the two plugins. | S |
| [P06-05](#p06-05) | Clearness / DDD | `plugins/module_utils/cluster_objects_engine.py:585-595` | the engine computes `classification` and `absent`, but Go never reads `Classification` (no `.Classification` use under internal/addons); Go re-judges the evidence and uses only `Postcondition`. | M |
| [P06-06](#p06-06) | Clearness | `plugins/module_utils/cluster_objects_engine.py:274-343` | `subscription` (cc 55) and `chain` (cc 61) mix probing, OLM proof rules, pausing, fact assembly and observe/apply branching. | M |
| [P07-01](#p07-01) | Security | `plugins/modules/managedos_host_firewall.py:115-121` | `offline_ssh_shape` detects a rich rule only as a non-empty line with no colon right after `rich rules:`. | S |
| [P07-02](#p07-02) | Security | `plugins/modules/image_acquire.py:172-173` | 11 modules start processes through `module.run_command`. | M |
| [P07-03](#p07-03) | Non-duplication | `plugins/modules/managedos_update.py:205-322` | The three SSH fixed-worker modules each re-implement the same functions: `ArgumentError`, `worker_source`, `refusal`, the `parse_answer` framing, the `interpret` transport mapping, the `execute` call to `guest_argv` with seven keyword arguments, and the seven… | M |
| [P07-04](#p07-04) | Non-duplication | `write_private at image_acquire.py:118` | Security-relevant helpers are copied between modules instead of shared from module_utils. | M |
| [P07-05](#p07-05) | Clearness | `plugins/modules/infrastructureservices_mirror_release.py:159-551` | One 555-line module holds three unrelated modes (`read`, `prepare`, `clean`), each with its own `required_if`. | M |
| [PT1-02](#pt1-02) | Non-duplication | `test_managedos_install_network.py:510` | Each role test file rewrites its own task walker and interpreter, and they disagree on Ansible semantics: - Network checks a block's `when` once, then runs its children with no inherited condition, and ignores `rescue` and `always`. | M |
| [PT1-03](#pt1-03) | Test coverage | `plugins/modules/physical_erase.py:99-113` | Action plugins have a "drive every `run()` with what surrounds it stubbed" test; modules have none. | M |
| [PT1-04](#pt1-04) | Quality | `tests/unit/test_role_task_order.py:28-34` | The rule "every role variable is registered before the task that reads it" loads only each file's top-level tasks. | S |
| [PT1-05](#pt1-05) | Quality | `tests/unit/plugins/module_utils/test_ceph_registry_discovery.py:592-597` | The test sends itself a process-directed signal, and it runs under xdist in the gate's units lane. | S |
| [PT1-06](#pt1-06) | Efficiency | `scripts/tools/ansible_check.py:599-627` | The plain lane reruns the whole unit tree serially in process on every full gate. | M |
| [Y01-02](#y01-02) | Right language | `managedos_install_anaconda/tasks/apply.yml:888-892` | Which proofs a completion requires (`rootWWNRequired`, `fipsRequired`, `rootEncryptionRequired`, `registrationRequired`) is computed in Jinja. | M |
| [Y01-03](#y01-03) | Security | `physical_erase/tasks/boot.yml:48-67` | The "fetch the first byte through the listener" proof exists in six copies with diverging options. | M |
| [Y01-04](#y01-04) | Non-duplication | `managedos_install_anaconda/tasks/boot.yml:21-35` | The controller access map (endpoint, verify, ca, user, password, optional `verification`) is rebuilt inline five times, although `managedos_install_anaconda_port_access` already holds it and other callers use `port_access \| combine({...})`. | S |
| [Y01-05](#y01-05) | Right language | `machine_power/defaults/main.yml:22-53` | The wrong-target safety rules (identity pin UUID trim/lower and serial trim, controller-host UUID/MAC refusal, exact declared MAC set) are Jinja expressions, and the pin comparison is written a second time in the baremetal substrate. | S |
| [Y01-06](#y01-06) | DDD | `managedos_install_anaconda/tasks/register.yml:63-66` | Worker session timeouts (630/750 +60 +60, 1800/120, 330, 300) are chosen in role YAML, and Go's run deadlines are sized by comments that restate those numbers. | S |
| [Y01-07](#y01-07) | Right language | `managedos_install_anaconda/defaults/main.yml:135-240` | Guest-side shell programs (awk pipelines that decide crypt/tpm2, os-release parsing, nmstate checkpoint scraping) live as `sh -c` strings in YAML. | M |
| [Y01-08](#y01-08) | Clearness | `managedos_disk_discovery/tasks/result.yml:18-111` | About 90 operator diagnostics live in a YAML map, while the code vocabulary lives in Python and is partly generated (`"native-" + error.tool`, `"bmc-" + cause`, `media-http-NNN`). | S |
| [Y02-01](#y02-01) | Quality | `roles/substrate_machine/tasks/dispatch.yml:21-26` | After every entry point, the dispatcher's `always` republishes `<arm>_port_result`. | S |
| [Y02-02](#y02-02) | Non-duplication | `roles/substrate_baremetal_machine/tasks/{power` | After renaming the arm, the files are byte-identical except that bare metal adds `verification:` (and boot_disk makes ca_data conditional). | M |
| [Y02-03](#y02-03) | Right language / Non-duplication | `roles/substrate_libvirt_machine/tasks/apply.yml:265-294` | The keep-or-rehash logic for the BMC htpasswd is a Python program in a YAML string, run through `podman run ... | M |
| [Y02-04](#y02-04) | Quality | `roles/substrate_libvirt_machine/tasks/destroy.yml:151-153` | Seven virsh tasks decide "already done" by matching English stderr (`"failed to get domain"`, `"already active"`, `"not found"`, `"no storage pool"`) without `environment: LC_ALL: C`. | S |
| [Y02-05](#y02-05) | Right language / Non-duplication | `roles/substrate_baremetal_machine/tasks/target_proof.yml:58-66` | The rule "the reported addresses equal the declared set, there are no inventory failures, and a UUID or serial is present" is written in Jinja for pre-boot and in Python `presence()` for apply and observe. | M |
| [Y03-02](#y03-02) | Right language / DDD / Efficiency | `roles/containercluster_install_agent/tasks/cluster_state.yml:15-114` | Each cluster read makes four separate `oc` calls, three of them against the same ClusterVersion object. | M |
| [Y03-03](#y03-03) | Non-duplication / Efficiency | `tasks/state.yml:11-71 with state_node_serial.yml` | Node observation is implemented twice. | M |
| [Y03-04](#y03-04) | Non-duplication | `playbooks/containercluster/install_apply.yml:23-36 (repeated at 76-89` | The per-node credential bindings (`controllerUserNode`/`PasswordNode`/`CANode` with the `portPinned` blank) are spelled six times. | S |
| [Y04-02](#y04-02) | Non-duplication / Right language | `roles/storage_ceph_host/tasks/prepare.yml:21` | The host image-pull budget is a literal 4285 s in Go, YAML and three Python sites. | M |
| [Y04-03](#y04-03) | Security | `roles/infrastructureservices_mirror_fill/tasks/apply.yml:58-70` | The frozen request carries each file's `bytes`, and the role validates it, but the download never enforces it. | M |
| [Y04-04](#y04-04) | Security / Quality | `roles/infra_managed_service/tasks/apply.yml:7` | `unit` is checked only as `is string` at apply and not at all at destroy, yet it builds a path that destroy deletes and is passed to systemctl and podman. | S |
| [Y04-05](#y04-05) | Security | `roles/infra_managed_service/templates/dnsmasq.conf.j2:25` | The role guards destinations, localZones and forwardZones against directive injection before rendering, but `records[].name/addresses`, `forwarders`, `clients`, `sources` and `bindAddress` reach dnsmasq and chrony lines unchecked. | S |
| [Y04-06](#y04-06) | Right language / DDD | `roles/infra_managed_service/defaults/main.yml:17-22` | The registry's SELinux MCS level, a product isolation decision, is computed in Jinja from a sha256 of the unit name. | M |
| [B01-01](#b01-01) | Quality | `.github/workflows/check.yml:42-49` | CI runs `scripts/check-commits` only on `pull_request` events. | S |
| [B01-02](#b01-02) | Security | `Makefile:161` | `release` checks the origin of only `VULNDB GO STAMP VULNERABILITY_CHECK`, and the native build removes only `GOFLAGS GOEXPERIMENT GOOS GOARCH`. | S-M |
| [B01-03](#b01-03) | Right language | `Makefile:157-218` | `release` is a 62-line shell program in one recipe, and `container-build` is 33 lines. | M |
| [B01-04](#b01-04) | Efficiency | `.github/workflows/check.yml:22-25` | the `.cache` key hashes the Ansible pins and the tools' go.sum, but not `internal/controller/tooltable.json` or `internal/controller/runtimelock/lock.json`. | S |
| [B01-05](#b01-05) | Security / Efficiency | `.dockerignore:1-24` | this is still open as audit 003 BLD-01: the build context is a root-anchored denylist. | S |
| [B01-06](#b01-06) | Quality | `Makefile:294` | `test`, `vet`, `vulncheck` and `vulngate` compile `//go:embed all:wheels`, but only `build` and `execution-contracts-check` depend on `wheels`. | S |
| [B01-07](#b01-07) | Non-duplication | `scripts/tools/runtimewheels/main.go:191-236` | "download within a bound, check size and SHA-256, keep in a cache by digest, rename on match" is written five times. | M |
| [B01-08](#b01-08) | Non-duplication | `scripts/tools/clientassets/assets.go:13-24` | the publisher URL template of each default client is written once in the tools module, which the gate and tool-table verify, and again in the product, which the controller download uses. | S |
| [E01-01](#e01-01) | Quality | `e2e/render.sh:91` | The route probe runs on every render and, under `set -euo pipefail`, aborts the script with exit 2 and no message when `ip` fails, even when `host.address` is explicit. | S |
| [E01-02](#e01-02) | Test coverage | `cmd/bootwright/e2e_cases_linux_amd64_test.go:193` | The in-tree gate needs an ambient `helm` and `python3` (plus `git`, via `exampleDirectories`), none of them pinned, while development.md promises tests that are "unitary and host-independent". | S |
| [E01-03](#e01-03) | Test coverage | `e2e/README.md:20` | The README says the tests list "each service switch its runbook offers", but several runbook switches are never rendered, validated or planned. | M |
| [E01-04](#e01-04) | Non-duplication | `e2e/*/templates/ (487 files)` | Every case and example is a full copy with no shared base. | L |
| [E01-05](#e01-05) | Non-duplication | `e2e/lab-sno-fips-controller/workloads/` | One workload is described twice. | M |
| [E01-06](#e01-06) | Right language | `e2e/lab-5-nodes-ceph-networks/app/app-setup.sh:38` | Four `python3 -I -c '...'` programs (keyring parse, MON address parse, a ctypes `mount(2)` call, config.json writer), about 35 lines in total, are embedded as strings in the bash script and passed positionally through `sys.argv`. | M |
| [E01-07](#e01-07) | Test coverage | `e2e/lab-5-nodes-ceph-networks/app/sample-server.py` | The only tested workload is `verify.py` (22 unittest cases). | S |
| [X01-01](#x01-01) | Quality | `controller/hostdnf/packages_linux_amd64.go:461-468` | `capped.Write` truncates and then returns the shortened length with a nil error. | S |
| [X01-02](#x01-02) | Non-duplication | `hostowned/probe_linux_amd64.go:35` | The duplication itself is already reported ([T02-05](#t02-05), [G06-04](#g06-04), G21 patterns). | M |
| [X02-01](#x02-01) | Quality | `roles/substrate_baremetal_machine/tasks/boot_media.yml:17` | A password Secret part is admitted as "one nonempty UTF-8 line", so leading and trailing spaces are legal (material/service.go:395-399; secrets.md:172). | S |
| [X02-02](#x02-02) | Security | `plugins/modules/infrastructureservices_mirror_image.py:196-197 and 249-2` | On an SSH-placed mirror host, these modules write the registry `auth.json` (a base64 username:password) under `module.tmpdir`. | M |
| [X04-01](#x04-01) | DDD | `test/architecture/boundaries_test.go:222-223` | The fitness test guards cross-context coupling only between two application packages. | S-M |
| [X04-03](#x04-03) | DDD | `api/v1alpha1 (60 of 106 importers)` | Verdict: the immutable `api.Value` tree is the working domain model through admission, selection and request building. | L |
| [X04-04](#x04-04) | DDD | `substrate` | The domain layer is mostly transaction script with primitive obsession: free functions validating and deriving from the wire tree, with few behaviour-bearing types. | M |
| [X04-05](#x04-05) | DDD | `controller/tools.go:43-157` | The controller root plays two unrelated roles in one package: - Published controller evidence that others consume: `controller.State` (44 uses), `InstalledTool` (29), `InstalledHostIdentity` (23), `ToolRequest` (16). | S-M |
| [X05-01](#x05-01) | Non-duplication | `substrate/libvirt/capability.go:384` | The rule that turns an observation into an effect state (apply: presence, then absence, then partial; destroy: absence, then unremoved, then unfinished) is hand-written in 10 functions across 7 packages. | M |
| [X05-02](#x05-02) | DDD | `reconciliation/lifecycle/invocation.go:138` | `RunResult.Outcome` is a plain `string`. | S-M |
| [X05-03](#x05-03) | Clearness | `substrate/admission.go:410` | `findNamed` has two meanings. | S |
| [X05-04](#x05-04) | Non-duplication | `cmd/bootwright/role_admission_test.go:28` | Five Go test files each re-implement ansible-core argument-spec validation, each with its own option type, and they check different subsets. | M |
| [X05-05](#x05-05) | Test coverage | `ansible/.../plugins/module_utils/ceph_native.py:68-75` | The Ceph Python validator requires `SCOPE_MEMBERS` and forbids every key outside `SCOPE_OPTIONAL`/`RESOURCE_MEMBERS`/`delegated`. | S |
| [X06-01](#x06-01) | Test coverage / Quality | `plugins/modules/ceph_export_protocol.py:12` | The export protocol's DOCUMENTATION was copied from SMB and has drifted. | S |
| [X06-02](#x06-02) | Non-duplication | `plugins/modules/*_protocol.py (27 files` | The 41 module files repeat the same option docs (`phase`, `detail`, `native_error`, `outcome`, `digest`, `evidence`, `observed`, `removed`) and EXAMPLES/RETURN boilerplate. | M |
| [X07-01](#x07-01) | Right language / DDD | `managedos_install_anaconda/tasks/boot.yml:15` | Every role that calls a machine port builds the `bootwright_machine_port` access dict in Jinja from request and material. | M |
| [X07-02](#x07-02) | Right language / Non-duplication | `managedos_install_anaconda/tasks/apply.yml:371-397` | The classifier for a failed artifact-listener fetch is written in Jinja in 6 roles, with 11 copies of the regex. | S |
| [X07-03](#x07-03) | Right language / DDD | `managedos_install_anaconda/tasks/register.yml:63-66` | The registration worker's time budget is computed in Jinja: `(750 if 'update' ... | S |

### Low findings (256)

| ID | Dimension | Where | Finding | Effort |
| --- | --- | --- | --- | --- |
| [G01-06](#g01-06) | Quality | `desiredstate/compilation/selection.go:14-20` | `selectResources` marks every source file as selected, and `r.selected` is assigned but never read. | S |
| [G01-07](#g01-07) | Security (input bounds) | `desiredstate/inputfs/reader_linux_amd64.go:281-285` | Each skipped `secrets` directory gets a fresh 65,536-entry budget for its YAML probe. | S |
| [G01-08](#g01-08) | Efficiency | `desiredstate/compilation/selection.go:77-85` | `validateSourcePaths` recomputes the root list against every source file for every record. | S |
| [G01-09](#g01-09) | Quality | `desiredstate/encoding/encoding.go:252-258` | Effective YAML writes YAML-1.1-ambiguous strings plain: `yes`, `no`, `on`, `off`, `y`, `n`, sexagesimal `1:20`, dates. | S |
| [G01-10](#g01-10) | Clearness | `api/v1alpha1/object.go:143` | Two unrelated types named `FieldOrigin` live in the same compile path: one is normalizer provenance, the other is the render origin label. | S |
| [G01-11](#g01-11) | DDD/Non-duplication | `api/v1alpha1/addons.go (PackageSubscription` | Add-on package configs are defined twice in the API package: as JSON-tagged Go structs and as `Shape` schemas. | S |
| [G01-12](#g01-12) | Clearness | `api/v1alpha1/schema.go:127-145` | Schema registration is split: three kinds are in the map literal and the rest go through `init` and a panicking `register` across seven files. | S |
| [G01-13](#g01-13) | Clearness | `api/v1alpha1/storage.go:124 (1` | Whole schemas are written as single lines. | S |
| [G01-14](#g01-14) | Quality | `compilation/compiler.go:104` | On a parse failure, `Parse` already puts the failure into its returned diagnostics, and `Compile` appends `failures` again, relying on de-duplication. | S |
| [G02-02](#g02-02) | Security | `nativeartifacts/renderlocal/files.go:137` | One package has two rules for "a directory safe for private output". | S |
| [G02-03](#g02-03) | Security | `trust/records.go:117-125` | The divergent-pin rule keys endpoints by the raw `HostToken(address, port)`. | S |
| [G02-04](#g02-04) | Security | `trust/records.go:117-120` | A stored record is held to a weaker rule than parsed input. | S |
| [G02-05](#g02-05) | Quality | `nativeartifacts/comparisonlocal/artifacts.go:11` | `Resolve` takes no context because the `Files` port has none, so under root delegation it starts the account helper with `context.Background()`. | S |
| [G02-06](#g02-06) | Quality | `adapterprotocol/process_linux_amd64.go:270-283` | The comment explains why the single-process signal is safe once the adapter has been reaped. | S |
| [G02-07](#g02-07) | Clearness | `adapterprotocol/process_linux_amd64.go:156-182` | The session state machine is encoded in nine booleans (`ended`, `exited`, `undelivered`, `canceled`, `completed`, `stopping`, `groupKilled`, ...). | M |
| [G02-08](#g02-08) | Non-duplication | `nativeartifacts/renderlocal/files.go:26-39` | "Walk up to the nearest existing ancestor and collect the missing components" is written three times (`Resolve`, `treeWriter.open`, `absent`). | S |
| [G02-09](#g02-09) | DDD | `trust/enrollment/requests.go:26-52` | `enrollment.Report` and `HostReport` carry JSON tags, but nothing marshals them. | S |
| [G02-10](#g02-10) | Right language | `adapterprotocol/process_linux_amd64.go:29-35` | A Python bootstrap (an isolated-interpreter assertion, then exec of controller_supervisor.py) is embedded in Go as a `-c` string. | S |
| [G02-11](#g02-11) | Clearness | `environment/inspection/object_user.go:22` | `revealObjectUser` loops over a one-element slice. | S |
| [G03-04](#g03-04) | Non-duplication | `validation.go:375-384` | `mediaName` re-implements `managedos.ValidMediaName` with three regexes, and the `--sha256` check re-implements `managedos.NormalizeMediaDigest`. | S |
| [G03-05](#g03-05) | Efficiency/Idiom | `validation.go:118` | Four regexes are compiled on every call, even though the file already keeps others at package level (lines 19, 301). | S |
| [G03-06](#g03-06) | Clearness | `runner.go:415-417` | `selectedJSON` is an alias of `selectedStructured` and is true for YAML too. | S |
| [G03-07](#g03-07) | Quality | `runner.go:357-365` | Two branches that cannot change the outcome, and a swallowed error. | S |
| [G03-08](#g03-08) | Quality | `operation.go:11` | `implementedOperation` has no production caller; only catalog_dispatch_test.go:45 uses it. | S |
| [G03-09](#g03-09) | Clearness | `parsing.go:263` | Whether a short flag accepts an attached value (`-fvalue`) is hard-coded to the letters `f` and `o`, not derived from the flag's type. | S |
| [G03-10](#g03-10) | Clearness/DDD | `validation.go:121` | The "validate" functions also normalize flag values in place through `normalizeScalar`. | S |
| [G04-06](#g04-06) | Quality | `output_uninstall.go:43` | These values are escaped twice. | S |
| [G04-07](#g04-07) | Quality/Clearness | `output_machine_discovery.go:59-71` | The newest human renderer (disk inventory) bypasses `display` with raw `Fprintf`, and it applies `%q` to a value that is already escaped. | S |
| [G04-08](#g04-08) | Security/Quality | `output_handoff.go:28-41` | The handoff descriptor is the only DTO that skips `escapeDisplayLine`. | S |
| [G04-09](#g04-09) | Non-duplication | `22 hand-built commandEnvelope{SchemaVersion: "v1alpha1"` | Most structured writers rebuild the envelope by hand. | S |
| [G04-10](#g04-10) | Non-duplication | `output_contexts.go:132 and output_additional.go:151` | The `context.orphaned` warning text is written out twice, once in the human renderer and once in the structured renderer. | S |
| [G04-11](#g04-11) | DDD/Non-duplication | `output_controller.go:463-523 and output_controller_structured.go:10-77` | One command is served by two structured DTOs: `setupPlanResult` for dry run and failures, and `controllerPresentation` otherwise. | S |
| [G04-12](#g04-12) | Clearness | `runner.go:415-417` | `selectedJSON` is an alias for `selectedStructured`, so every `jsonMode` parameter really means "JSON or YAML". | S |
| [G05-06](#g05-06) | Quality | `cmd/bootwright/wiring_controller.go:67` | Construction and decode failures are dropped and turn into "unavailable" or empty state. | S |
| [G05-07](#g05-07) | Quality | `cmd/bootwright/wiring_inspection.go:252-255` | `HostKeyProved` turns a trust store that cannot be decoded into "not proved", with no error. | S |
| [G05-08](#g05-08) | Non-duplication | `cmd/bootwright/wiring_machine.go:65` | Adapter construction is repeated: - Two Ansible runners, and two KubeVirt lenders, are built per invocation. | S |
| [G05-09](#g05-09) | Clearness | `cmd/bootwright/wiring_machine.go:195` | String literals are used where typed constants exist: the kind `"Machine"` (while :317 of wiring_machine.go uses `api.Machine`), the verb `"apply"`, and the tool kind `"openshift-clients"`. | S |
| [G06-06](#g06-06) | DDD / Quality | `prerequisites/contracts.go:20-27` | Release is mandatory (available() refuses without it) but is reached through a type assertion. | S |
| [G06-07](#g06-07) | Security | `hostlinux/addresses_linux_amd64.go:14-26` | HardwareAddresses skips unreadable entries and reads through os.ReadDir and os.ReadFile, outside the package's held-fd discipline. | S |
| [G06-08](#g06-08) | Clearness | `hostlinux/inspector_linux_amd64.go:55` | LoaderPreload returns `(true, nil)` on any open error other than ENOENT. | S |
| [G06-09](#g06-09) | Quality | `firewalllocal/native.go:28` | Every failure collapses into fixed text. | S |
| [G06-10](#g06-10) | Clearness | `hostlinux/identity_linux_amd64.go:189-192` | canonicalUUID validates a UUID by building a whole InstalledHostIdentity around placeholder identifiers. | S |
| [G06-11](#g06-11) | Efficiency | `hostdnf/packages_linux_amd64.go:126-132` | `queries` starts one goroutine and one rpm process per requirement with no limit, up to 64 at once. | S |
| [G06-12](#g06-12) | Quality | `hostowned/probe.go:162-164` | `printable` truncates at `text[:512]`, which can split a multibyte rune (hostdnf's lastError uses ToValidUTF8 for the same job). | S |
| [G07-06](#g07-06) | Quality | `controller/bundlelocal/tools.go:61-68` | `Select` keeps the newest version among retained sources that share a prefix. | S |
| [G07-07](#g07-07) | Security | `controller/privilege/process_linux_amd64.go:252-272` | `ProcessExecutor.Run` verifies the re-execution path only after the first `--`. | S |
| [G07-08](#g07-08) | Efficiency | `controller/bundlelocal/tools_transport.go:44-48` | Every publisher request builds a new client: it re-reads and parses the CA bundle twice (once to check it, once to build the pool) and discards idle connections afterwards. | S |
| [G07-09](#g07-09) | Non-duplication | `controller/ansiblelocal/protocol.go:14-85` | `strictJSON` decodes three times (a case-folded uniqueness walk, a map pass, a closed struct pass). | S |
| [G08-06](#g08-06) | Clearness | `workspace/contextfs/repository_linux_amd64.go:1364` | `if next != prior` at line 1384 is always true, because line 1364 already continues the loop on equality. | S |
| [G08-07](#g08-07) | Clearness | `workspace/contextfs/files_linux_amd64.go:855-858` | `writeExclusiveAtomic` publishes media records, operation records and trust records, but reports the checkpoints `checkpointBeforeSecretImmutableRename` and `checkpointAfterSecretImmutableRename`. | S |
| [G08-08](#g08-08) | Security | `workspace/contextfs/operations_linux_amd64.go:724` | The operation log is opened `O_WRONLY\|O_APPEND` without `O_NONBLOCK`, and the type check comes only after the open. | S |
| [G08-09](#g08-09) | Quality | `workspace/contextfs/operations_linux_amd64.go:703-708` | `Append` turns the write and sync errors into plain state refusals and drops the errno. | S |
| [G08-10](#g08-10) | Quality | `workspace/contextfs/media_linux_amd64.go:1045-1055` | A replacement removes the old image and record, then checks for cancellation (the `before-media-rename` checkpoint), then renames. | S |
| [G09-05](#g09-05) | Security | `workspace/selectionfs/helper_linux_amd64.go:100-112` | Root decodes the helper's stdout with a non-strict `json.Unmarshal` and returns `response.Selection` without `validSelection`. | S |
| [G09-06](#g09-06) | Security | `workspace/invokerfs/session_linux_amd64.go:72-73` | A nil `*Opener` under euid 0 begins a `direct{root: true}` session, so operator-named paths are opened with root's credentials. | S |
| [G09-07](#g09-07) | DDD | `contexts/contracts.go:37` | `ControllerDescriptor.Mode` is a bare `string`, compared against the literals "initializing" and "ready" in five files, while `contexts.Mode` with its `Initializing` and `Ready` constants exists beside it. | S |
| [G09-08](#g09-08) | Clearness | `secrets_cleanup_linux_amd64.go:85` | The literal "store.json" appears eight times, although `secretstore.RecordPath` holds that value and the sibling file uses it (secrets_linux_amd64.go:858). | S |
| [G09-09](#g09-09) | Non-duplication | `setup_runs_linux_amd64.go:24` | `maxSetupRunOutput = 4 << 20` restates `operationstore.MaxAdapterOutputBytes`; its own comment says the two must be one bound. | S |
| [G09-10](#g09-10) | Quality | `workspace/contexts/service.go:468` | `updateUnder` writes to `reg.Contexts[findRecord(reg, record.Name)]` without checking for -1. | S |
| [G09-11](#g09-11) | Efficiency | `controller_linux_amd64.go:416-419` | `readControllerStored` (root-entry verification, controller listing, decode, bundle-reservation proofs) runs again for each query. | M |
| [G09-12](#g09-12) | Clearness / DDD | `workspace/contexts/service.go:31` | The 823-line Service combines the init, update, use, list, current and delete flows. | M |
| [G10-06](#g10-06) | Non-duplication | `material/generation.go:261-280 vs 455-476` | Certificate parameter handling is copied between generation and validation: the 3650-day default, the bound, the common-name check, and the DNS and IP loops. | S |
| [G10-07](#g10-07) | Non-duplication | `custody/declarations.go:75-80` | The declaration fingerprint rule (JSON-encode the declaration with an empty fingerprint, then hex SHA-256) is implemented twice. | S |
| [G10-08](#g10-08) | Security | `material/files_linux_amd64.go:581-598` | `readWithLimit` starts with a capacity of at most 32 KiB and then grows by `append`. | S |
| [G10-09](#g10-09) | Security | `material/generation.go:506-509` | A generated CA has no path-length constraint (`MaxPathLen` is unset, so the length is unlimited). | S |
| [G10-10](#g10-10) | Quality | `material/validation.go:35-46` | There is dead generality. | S |
| [G10-11](#g10-11) | Efficiency / Clearness | `localkeyring/mutations.go:355-362` | The `keys` directory is listed twice, once as "keyNames" and once as "ledgerNames", which suggests a separate ledger directory that does not exist. | S |
| [G10-12](#g10-12) | Clearness | `encryption/service.go:131` | The rotation refusals spell out `bootwright secret encryption ... | S |
| [G11-04](#g11-04) | Clearness / Non-duplication | `reconciliation/operationstore/store.go:836-856` | The grammar of attempt and resolution file names is built or parsed in about seven places, each slightly differently. | S |
| [G11-05](#g11-05) | Efficiency | `reconciliation/operationstore/logs.go:315` | `LogPaths` tests whether the operation log exists by reading it with maximum 0, and the area treats 0 as "up to 8 MiB" (operations_linux_amd64.go:411-413). | S |
| [G11-06](#g11-06) | Security | `reconciliation/ansiblerunner/process_linux_amd64.go:207-218` | The runner clears secret bytes on purpose, but two spots work against that. | S |
| [G11-07](#g11-07) | Quality | `reconciliation/preparation.go:24` | The preparation-scope refusals are plain `fmt.Errorf` values. | S |
| [G11-08](#g11-08) | Clearness | `reconciliation/ansiblerunner/process_linux_amd64.go:64-157` | `Runner.Run` has cyclomatic complexity 30 and 74 statements, and covers validation, staging, claiming, materializing, inventory, execution and reading outputs back. | M |
| [G11-09](#g11-09) | Non-duplication | `reconciliation/plan.go:573-579` | The check "64 lowercase hex digits, canonical" exists as `validDigest` (unexported) in the root package and again inline three times in records.go. | S |
| [G12-06](#g12-06) | Quality | `service.go:67` | The `Inputs` port is stored as `Service.inputs` and never read anywhere in the package. | S |
| [G12-07](#g12-07) | Security / Non-duplication | `admin_credentials.go:275-288 and :336-349 versus produced_inputs.go:97-1` | `adminProduced` duplicates `producedInTransaction`. | S |
| [G12-08](#g12-08) | Security | `admin_credentials.go:30` | `adminUpdate.Password` is a `string`. | M |
| [G12-09](#g12-09) | Quality | `planning.go:76-82` | Aliasing discipline is uneven at hand-off points: - `DependOnController` (exported) writes into the caller's backing array, while its siblings `dependOnStageTails` and `dependOnProducedInputs` clone first. | S |
| [G12-10](#g12-10) | Clearness | `admin_credentials.go:177` | Six complexity hotspots (gometrics-prod.tsv: cyclomatic complexity, nesting depth, statements): \| Function \| Complexity \| Depth \| Statements \| \|---\|---\|---\|---\| \| `updateAdminCredential` \| 31 \| 3 \| 76 \| \| `planFrom` \| 27 \| 3 \| 77 \| \| `decideBy` \| 27 \| 3 \| 58… | M |
| [G12-11](#g12-11) | Clearness | `handoff.go:27-40` | Three small clarity defects: - `handoff` reports an unknown block twice, and labels a *running* block "has an unresolved unknown outcome". | S |
| [G13-03](#g13-03) | Quality | `reconciliation/lifecycle/scheduler.go:202-207` | Resolutions are admitted without `c.free(block)`, so they ignore exclusive claims and group throttles. | S |
| [G13-06](#g13-06) | Non-duplication / DDD | `evidence.go:186-200` | The transitive dependency closure is coded twice as a BFS, and the reverse closure (dependents) once more as an O(n²·k) fixpoint. | S |
| [G13-07](#g13-07) | Non-duplication | `evidence.go:279-289` | The rule "latest attempt, superseded by its last resolution" is re-implemented in four places. | S |
| [G13-08](#g13-08) | Security / Non-duplication | `diff.go:292-297` | `firstMessage` and `contractMessage` are identical, and both fall back to `err.Error()`. | S |
| [G13-09](#g13-09) | Quality | `diff.go:173` | Read paths drop errors. | S |
| [G13-10](#g13-10) | Efficiency | `attempts.go:48` | Each attempt re-reads its lineage several times: `provedDependencies` calls `lineageOf`, `applyDependencies` calls `lineageOf` again, `applyPreparations` re-reads the operation, `populateRemovalExecution` calls `sourceLineage` twice, and `ownerID` runs again… | M |
| [G13-11](#g13-11) | Clearness | `diff.go:32-62` | `diffRead` is a 28-field accumulator. | M |
| [G14-06](#g14-06) | Efficiency | `machine/sshlocal/launcher_linux_amd64.go:133-148` | `ssh -G -F /dev/stdin localhost` runs once per `HostKeyAlgorithms` call and again in every `Run`, although its output depends only on the policy bytes and never on the session. | S |
| [G14-07](#g14-07) | Clearness / Quality | `machine/diskselection/selection.go:165` | `Select` (complexity 46) checks the exclusion twice with identical code. | S |
| [G14-08](#g14-08) | Right language / Test | `machine/sshlocal/copy_worker.py` | The shipped worker must run on Python 3.9 at both endpoints (machine-copy.md:34), but it lives outside the collection. | S |
| [G14-09](#g14-09) | Security / Robustness | `machine/sshlocal/copy_linux_amd64.go:183` | The remote worker reaches the host as one 419-line single-quoted word that the remote login shell parses. | S |
| [G14-10](#g14-10) | Clearness / Non-duplication | `machine/sshlocal/copy_linux_amd64.go:95-99` | The Go allowlist of worker error tokens is a hand copy of the Python reasons and has already drifted. | S |
| [G14-11](#g14-11) | Non-duplication | `machine/access/service.go:74-87 vs several.go:46-59 vs copy.go:46-64` | The open/several/copy preamble is cloned (this clone is what let [G14-01](#g14-01) diverge). | S |
| [G15-05](#g15-05) | DDD | `substrate/capacity/reserves.go:13` | The generic `capacity` package reads `spec.libvirt.capacityReserve` and its remediation text names libvirt. | S |
| [G15-06](#g15-06) | Quality | `substrate/kubevirt/capability.go:90` | The KubeVirt host block sets `Family: "substrate-host"` as a literal and the libvirt host block sets no family. | S |
| [G15-07](#g15-07) | Quality | `substrate/libvirt/capability.go:642-645` | `Quiescent` turns every run error, including a context cancelled mid-probe, into `Unproved, nil`. | S |
| [G15-08](#g15-08) | Clearness | `substrate/libvirt/requests.go:34-37` | Three local clarity problems: - **Misplaced doc comment:** "HostRequest is the complete frozen intent…" sits above `type Trunk`. | S |
| [G16-07](#g16-07) | Quality / DDD | `erase_request.go:82-86` | The erase request recovers its network declaration by scanning the rendered Kickstart text for `network ` lines. | S |
| [G16-08](#g16-08) | Non-duplication | `network.go:100-131` | `nativeDocument` is an exact copy of agentinstall's `nativeValue`, and the `Firmware`/`BIOSAttribute` types are duplicated between the two packages. | S |
| [G16-09](#g16-09) | Non-duplication | `kickstart.go:148` | The exact-disk-size predicate (at least 4096, at most 64 TiB, 4096-aligned) appears seven times. | S |
| [G16-10](#g16-10) | Efficiency | `discovery.go:82` | `DiscoveryCapability.requests` reads `MediaRecords` inside its per-machine loop. | M |
| [G16-11](#g16-11) | Security | `kickstart.go:550-553` | The delivered private host key is fetched by `curl --output` in a `%post` that sets no `umask`. | S |
| [G16-12](#g16-12) | Quality | `erase.go:154` | When Erase and Discovery refuse, they return a zero `lifecycle.Result{}`. | S |
| [G16-13](#g16-13) | Security | `firewall_request.go:142-155` | The firewalld service XML is built by string concatenation with no escaping. | S |
| [G17-06](#g17-06) | Quality | `infrastructureservices/artifactserver/requests.go:52-56` | `TLS.Fingerprint` is frozen into every artifact-server request but never set. | S |
| [G17-07](#g17-07) | Quality | `infrastructureservices/artifactserver/secret_references.go:23` | the return value of `classes.Add(...)` is thrown away. | S |
| [G17-08](#g17-08) | Clearness | `infrastructureservices/mirrorfill/requests.go:73-74` | `validate()` serves both plan time and decode time, but its only remedy is the decode-time one. | S |
| [G17-09](#g17-09) | Non-duplication | `managedos/admission.go:507` | four packages each parse the `"local-media:"` reference by hand. | S |
| [G17-10](#g17-10) | Non-duplication | `infrastructureservices/managedservice/credential.go:31-35` | the password-file grammar (no colon or control characters in the username, 1 to 72 bytes and no NUL in the password) is written twice. | S |
| [G17-11](#g17-11) | Quality | `infrastructureservices/dnsserver/catalog.go:73` | a gateway record conflict returns `fmt.Errorf("gateway DNS record %s conflicts ...")` with no diagnostic code, object or remedy, unlike every other refusal in the lane. | S |
| [G17-12](#g17-12) | Clearness | `managedos/media/service.go:141-143` | the doc comment above `readURL` belongs to `addition` (`// addition refuses what an add names before any store is read`). | S |
| [G18-08](#g18-08) | Efficiency | `resources.go:50-69` | every derived request rebuilds the whole cluster request. | M |
| [G18-09](#g18-09) | Clearness | `capability.go:77-88 with capability.go:248-257` | placement emits `Requires` refs that use native pool names (`request.RBD.Pool`, `c.Pool`, `Filesystem.MetadataPool`). | S |
| [G18-10](#g18-10) | Quality | `services.go:40-46` | two pieces of dead code. | S |
| [G18-11](#g18-11) | Non-duplication | `materials.go:80-98` | two functions build the same registry credential JSON (`url`, `username`, `password`) from the same Secret parts, with the same clearing. | S |
| [G18-12](#g18-12) | Clearness | `materials.go:366` | adapterRefusals builds about 75 diagnostics. | S |
| [G19-04](#g19-04) | Non-duplication | `host.go:437` | The same adapter boundary refusal list ("request", "artifact", "ownership", ..., "adapter-exception") with the same message template is written four times. | S |
| [G19-05](#g19-05) | Clearness | `host_trust.go:49-52` | `proveFIPS` skips provided hosts when `r.Cluster == ""`. | S |
| [G19-06](#g19-06) | Non-duplication | `retirement.go:230-234` | Each evidence decoder decodes the same bytes twice, once into the struct and once into `map[string]json.RawMessage`, only to check that required members are present and not null. | S |
| [G19-07](#g19-07) | Security | `export_evidence.go:81-86` | `validateExportProduced` clears the copied part bytes but decodes the CephX `userKey`, `accessKey` and `secretKey` into Go strings that cannot be cleared. | M |
| [G19-08](#g19-08) | Quality | `export_evidence.go:209-216` | Consumer retirement refuses `r.PhysicalHosts` before it calls `pendingPhysicalClusters`, so `PendingErase` can never be true. | S |
| [G19-09](#g19-09) | Efficiency | `object_bucket.go:39-43` | Each bucket, user and RGW NFS export calls `gatewayRequests`. | S |
| [G19-10](#g19-10) | Clearness | `host.go:538` | `run(..., removal ...bool)` uses a variadic bool as an optional flag, and `r.target()` is computed twice (host.go:550 and 559). | S |
| [G20-07](#g20-07) | Quality | `storage/preflight/service.go:401-403` | When a probe errors, both the error and the cause are thrown away. | S |
| [G20-08](#g20-08) | Clearness | `storage/cephcluster/normalization.go:57 (cx 44)` | These functions were squeezed under the 100-line ceiling, not split by meaning. | S |
| [G20-09](#g20-09) | Security | `storage/preflightlocal/reuse_linux.go:178-187` | `processStart` finds the end of the `/proc/<pid>/stat` comm field with the first `") "` instead of the last `)`. | S |
| [G20-10](#g20-10) | Quality | `storage/cephcluster/gateway_dns.go:33-90` | Exported derivations used by the reconciliation wiring return plain `fmt.Errorf` strings with no diagnostic code or remediation. | S |
| [G20-11](#g20-11) | Efficiency / Quality | `storage/cephcluster/native.go:262-268` | Three small issues: - nativeShape clones the whole sequence inside its own loop (`value.Items()[:i]`), which is O(n²) work, and reports one issue per earlier duplicate. | S |
| [G21-06](#g21-06) | Non-duplication | `preflight/service.go:161-190` | The managed-service endpoint address is resolved three ways. | S |
| [G21-07](#g21-07) | Security | `access/handoff.go:70-90` | The handoff argument denylist refuses `--show-token` but not oc's short form `whoami -t`. | S |
| [G21-08](#g21-08) | Efficiency | `agentinstall/capability.go:50` | `MediaCapability.Plan` and `InstallCapability.Plan` each call the full `Requests()` derivation and discard half of it. | M |
| [G21-09](#g21-09) | Clearness | `agentinstall/selection.go:161 vs :463` | The refusal pass calls `substrate.TargetFor(catalog, bound, cluster.Name(), controllerMachine)`, passing the cluster name as the context name. | S |
| [G21-10](#g21-10) | Clearness / DDD | `openshift/admission.go:837-850` | One package encodes address family three ways: `networkFamily` returns 4, 6 or -1; `familyBits` uses 32 or 128; `network.Family` returns "ipv4" or "ipv6". | S |
| [G21-11](#g21-11) | Quality | `networking/planning.go:32` | The networking capability returns bare `fmt.Errorf` for operator-facing refusals, with no diagnostic code or remediation. | S |
| [G21-12](#g21-12) | Clearness / DDD | `agentinstall/requests.go:168` | The installer documents are frozen as `map[string]any`. | M |
| [G21-13](#g21-13) | Quality | `agentinstall/install.go:466-479` | The user material for a hosted (KubeVirt) node is redirected to the lent host credential, but the password material still names `node.Controller.CredentialsRef`. | S |
| [G21-14](#g21-14) | Non-duplication | `agentinstall/projection.go:370-401` | `nativeValue` and `nativeDocument` are an exact 31-line clone that converts an `api.Value` to plain data. | S |
| [G22-04](#g22-04) | Quality | `addons/instance/render.go:188-207` | No package ever sets `Rendered.SubscriptionConfig`, so the `settings != nil` branch of `subscribe` and the types `co.SubscriptionConfig` and `EnvVar` are unreachable. | S |
| [G22-05](#g22-05) | Non-duplication | `addons/instance/render.go:115` | `instance.nativeObject` and `packages.object` are byte-for-byte the same. | S |
| [G22-06](#g22-06) | Non-duplication | `addons/instance/render.go:65-80` | The derivation of produced inputs and Secret names from placements is written twice, once to build the request and once to verify it. | S |
| [G22-07](#g22-07) | Non-duplication | `addons/admission_olm.go:174-186` | The rule "the gate holds a resourceExists check of a CRD named `<plural>.<group>`, with a non-empty, dot-free plural" is implemented twice: once over `api.Value` for admission and once over `co.Check` for rendering. | S |
| [G22-08](#g22-08) | Security | `addons/clusterobjects/validation.go:215` | `safeBody` refuses the keys `status`, `managedFields` and `bootwright.io/owner` at every depth, not just at the top level and under `metadata.annotations`. | S |
| [G22-09](#g22-09) | Right language | `plugins/module_utils/cluster_objects_model.py:108-113` | The list of kinds and paths that must compare exactly (Subscription `spec`; RBAC `subjects` and `rules`) is a product rule (add-ons.md:299-300), but it is hard-coded in the Python adapter. | M |
| [G22-10](#g22-10) | Clearness | `addons/clusterobjects/evidence_validation.go:31` | Validation is written as single boolean expressions with up to 10 terms (ValidateEvidence complexity 47, validateStep 39, validatePlacement 36). | S |
| [T01-08](#t01-08) | Quality (CI) | `.github/workflows/check.yml (commits job)` | the commit-message check runs only on `pull_request`. | S |
| [T01-09](#t01-09) | Clearness | `lifecycle_partition_test.go:20-40` | `lifecycleFileClasses` gives each of 66 file names a class (planner, executor, ...), but the only reader is TestLifecycleFilesAreClassified, which checks that the list is complete. | S |
| [T01-10](#t01-10) | Right language | `Makefile:157-218 (release)` | the 60-line release recipe parses the executable's JSON with sed and holds the decision logic. | M |
| [T01-11](#t01-11) | Quality | `container_build_test.go:178` | Makefile recipe tests pin exact substrings, their order and `strings.Count(recipe, "$(MAKE)") != 2`. | S |
| [T01-12](#t01-12) | Right language/Quality | `version_specializations_test.go:97-98` | Python registries are read with regexes, and the test demands a fixed layout: "keep every record in that layout". | M |
| [T02-10](#t02-10) | Quality | `cmd/bootwright/capability_contract_test.go:524-565` | The module-wide contract runs as one test function with no `t.Run`. | S |
| [T02-11](#t02-11) | Non-duplication, policy trade-off | `machine/hardware/golden_test.go:22` | 39 golden files contain 23 `matchesGolden` copies in four signatures. | S |
| [T02-12](#t02-12) | Quality | `secrets/secretstore/access_test.go:16-31` | `testArea` embeds a nil `Area`, ignores `ReadMutable`'s limit, returns `Entries` in map order, and does not run areacontract. | S |
| [P01-08](#p01-08) | Non-duplication | `ceph_native.py:2014-2045 versus :2349-2373 and :2448-2453` | the LVM report is read and its rows and tags parsed three times, although `logical_volumes()` exists. | S |
| [P01-09](#p01-09) | Clearness | `ceph_native.py:2582-2584` | `devices()` creates `observed_device_ids`, `osd_clears`, `osd_settled` and `osd_orphans` outside `__init__`, and `clear_partial_osds` and `osd_prepare` read them. | M |
| [P01-10](#p01-10) | Quality | `ceph_native.py:4847-4865` | `ceph_native.main` is dead. | S |
| [P02-08](#p02-08) | Quality | `ceph_admin_credentials_native.py:24-27` | `prepare` computes a bcrypt hash through the deprecated `crypt` module and puts it in the candidate. | S |
| [P02-09](#p02-09) | Security | `admin_credential_io.py:9-30` | There are two private-file readers with different rules. | S |
| [P02-10](#p02-10) | Clearness | `ceph_native_units.py:32-33` | The 12-field `UnitType` hook tuples are built positionally next to six no-op hooks whose bodies are only `del` statements. | S |
| [P02-11](#p02-11) | Quality | `ceph_export_native.py:77-78` | These are local inconsistencies: - The export validator calls `ipaddress.ip_address` without a guard, so bad input becomes `native-unknown` instead of `request`. | S |
| [P02-12](#p02-12) | Efficiency | `ceph_native_nvmeof.py:206-214` | `qualify_commands` runs `ceph --help` and does 10 regex scans on every `nvme_scope`, which means every apply and observe phase plus each unit check. | S |
| [P03-06](#p03-06) | Quality (dead code) | `ceph_core.py:1196-1221` | `apply_native_cluster` can never run in production. | S |
| [P03-07](#p03-07) | Quality (error classification) | `ceph_core.py:724` | `Transport.packages` decodes the package CA without the UnicodeError guard that every sibling material reader has. | S |
| [P03-08](#p03-08) | Non-duplication | `ceph_core.py:918-941 vs 944-964` | `wait_ready` and `wait_activation` are line-for-line copies apart from the `pending` flag. | S |
| [P03-09](#p03-09) | Right language / Test coverage | `ceph_host_runtime.py:99-211` | three Python programs live as string constants (PROBE, LABEL_PROBE, DEVICE_PROBE) and run inside the image. | M |
| [P03-10](#p03-10) | Non-duplication | `ceph_host_worker.py:65-66` | the launcher code-string template is assembled three times, and the budget formula four times. | S |
| [P03-11](#p03-11) | Quality (diagnostics) | `ceph_pull_broker.py:541-544` | `serve` swallows every BrokerError (refused delivery location, failed DNS resolution, header or size bounds). | S |
| [P03-12](#p03-12) | Quality | `ceph_package_sources.py:25-26` | `IBM_BASE` and `IBM_KEY` are hard-coded vendor URLs that no production code reads; only tests use them. | S |
| [P03-13](#p03-13) | Clearness | `ceph_host_runtime.py:888` | the `KERNEL_DISK` regex is re-inlined twice although the constant exists, the printable-serial check is repeated, and `cache_exists` duplicates `custody.exists`. | S |
| [P04-06](#p04-06) | Quality | `redfish_control.py:755-758` | `eject()` on a controller with no media device, with `restoreVerification` set, calls `_configure_media_verification("", "", True)`. | S |
| [P04-07](#p04-07) | Non-duplication | `disk_inventory.py:49-68` | The disk serial is read in two ways. | S |
| [P04-08](#p04-08) | Efficiency | `redfish_control.py:263` | Every `fetch` builds a new opener and SSL context and parses the CA bundle again. | S |
| [P04-09](#p04-09) | Efficiency | `redfish_root_volume.py:89` | `discover()` with `{"firmware": true}` walks the full Storage/Drives/Volumes inventory twice, which is up to 256 reads per walk. | S |
| [P04-10](#p04-10) | Clearness | `disk_discovery_worker.py:71` | `len(set(observed.values())) != len(observed)` runs on every loop iteration and can never fire: the values are distinct sysfs names, and a duplicate MAC has already collapsed in the dict comprehension. | S |
| [P04-11](#p04-11) | Clearness | `plugins/modules/physical_erase.py:126-128 (consumer of this lane's codes` | The worker and controller define closed, non-sensitive refusal codes (`foreign-target`, `disk-identity`, `controller-identity`, ...). | S |
| [P05-04](#p05-04) | Quality | `managedos_update_worker.py:137-168` | The spec's release-drift refusal is proved on the preview's resolve. | S |
| [P05-05](#p05-05) | Quality | `native_resolution.py:474-477` | `inspect()` builds a `dnf.Base` and never closes it. | S |
| [P05-06](#p05-06) | Clearness | `native_resolution.py:79-81` | The RPMDB read lock is a raw `fcntl(lock, 37, struct.pack("hhqqi4x", ...))`. | S |
| [P05-07](#p05-07) | Clearness | `substrate_libvirt.py:582-622` | `domain_disk_bindings` (CC 35, depth 4) mixes three proofs in one body: SCSI address uniqueness, per-disk file, bus and serial, and SSD/HDD presentation. | S |
| [P05-08](#p05-08) | Test coverage | `tests/unit/plugins/module_utils/test_libvirt_nm_connections.py:486-488` | A test named `test_the_connection_uuid_is_the_one_go_derives` compares Python with itself. | S |
| [P05-09](#p05-09) | Efficiency | `kubevirt_api.py:210-243` | Every request, including each 5-second poll in `Host.wait`, builds a new `SSLContext`, writes the client key and certificate to a fresh temporary directory, and opens a new TLS connection. | S |
| [P05-10](#p05-10) | Non-duplication | `kubevirt_machine.py:70-74` | Two problems. | S |
| [P05-11](#p05-11) | Quality | `kubevirt_machine.py:801` | Two reads use `.get(key, {})`, which fails on an explicit JSON `null`. | S |
| [P06-07](#p06-07) | Quality | `plugins/module_utils/bounded_process.py:80-104` | the exit status is observed only after both pipes reach EOF. | S |
| [P06-08](#p06-08) | Clearness | `plugins/action/cluster_objects_read.py:8-36` | the request copy, the secretSources→materials projection and the `read` closure are duplicated between the two plugins. | S |
| [P06-09](#p06-09) | Non-duplication | `plugins/module_utils/host_sockets.py:34-53` | `table_lines`, `hexadecimal` and `table_address` are copied verbatim into substrate_libvirt. | S |
| [P06-10](#p06-10) | Efficiency | `plugins/module_utils/cluster_objects_engine.py:233-244` | each `checks` poll re-reads every check, including ones already satisfied. | M |
| [P06-11](#p06-11) | Clearness | `plugins/module_utils/controller_channel.py:1-29` | it is named "controller" channel, but 27 action plugins import its `emit`, including every lifecycle capability, not only the controller stage. | S |
| [P07-06](#p07-06) | Quality | `plugins/modules/managedos_host_firewall.py:94` | Each `decode("ascii")` or `decode("utf-8")` can raise `UnicodeDecodeError`, which is a `ValueError`. | S |
| [P07-07](#p07-07) | Quality | `plugins/modules/ceph_admin_credentials.py:79` | `update` always reports `changed=True`. | S |
| [P07-08](#p07-08) | Quality | `plugins/modules/ceph_replication.py:73` | The argument spec admits `destroy`, but `admitted()` and module_utils/ceph_replication.py:41 refuse anything except `("apply", "observe")`. | S |
| [P07-09](#p07-09) | Non-duplication | `plugins/modules/artifact_server_inspect.py:104-166` | Both mains repeat the same flow: observe, then bind_address, then foreign, then a readiness retry loop with `time.sleep(RETRY_DELAY)`, then the failure sort. | S |
| [P07-10](#p07-10) | Efficiency | `plugins/modules/infrastructureservices_mirror_release.py:389` | The `oc` member (bounded only by `MAX_CLIENT`, 1 GiB) is read whole into memory before it is written. | S |
| [PT1-07](#pt1-07) | Quality | `plugins/module_utils/test_bounded_process.py:57-59` | Several tests assert wall-clock bounds or use fixed subprocess timeouts while the gate runs lanes side by side. | S |
| [PT1-08](#pt1-08) | Clearness | `scripts/tools/ansible_check.py:927-1053` | The launcher's `main` is 127 lines with complexity 17. | S |
| [Y01-09](#y01-09) | Non-duplication | `managedos_host_firewall/tasks/apply.yml:2-22 (and destroy/observe)` | Entry-point validation is copied verbatim for each operation, and the three disk-discovery entry points differ only in `operation:` and `observed:`. | S |
| [Y01-10](#y01-10) | Quality | `physical_erase/tasks/destroy.yml:2-18` | physical_erase hands `loaded` before its assert, and its observe validates nothing, unlike every other entry point. | S |
| [Y01-11](#y01-11) | Clearness | `managedos_package_tree/tasks/apply.yml:8-21` | Path structure is decoded by component index (`split('/')[5:7]`, `[:8]`, `length == 12`) and by `dirname \| dirname \| dirname` in three roles. | S |
| [Y01-12](#y01-12) | Efficiency | `machine_power_read/tasks/read.yml:35-40` | Power readings run one target at a time through a looped include, and the list is accumulated quadratically. | M |
| [Y02-06](#y02-06) | Quality | `roles/substrate_kubevirt_machine/tasks/boot_disk.yml:20-31` | The bare-metal and libvirt boot_disk entry points stop at their own refusal. | S |
| [Y02-07](#y02-07) | Quality | `roles/substrate_machine/defaults/main.yml` | `<arm>_port_refusal` is written in 10 places, but no production task reads it: the dispatcher republishes only `_refusal` and `_port_result`. | S |
| [Y02-08](#y02-08) | Clearness | `roles/substrate_baremetal_machine/tasks/apply.yml:68-80` | The "prove-identity" and "verify-absence" groups report `running` and then `ok` with no check in between. | S |
| [Y02-09](#y02-09) | Efficiency | `roles/substrate_libvirt_host/tasks/apply.yml:42-55` | Each package runs its own `dnf install` transaction (loop), each with its own metadata and solve. | S |
| [Y02-10](#y02-10) | Non-duplication | `roles/substrate_kubevirt_machine/tasks/*.yml (7 copies at lines ~9-19)` | The "Refuse port access this arm does not read" block is repeated in 7 files. | S |
| [Y02-11](#y02-11) | Clearness | `roles/substrate_baremetal_machine/defaults/main.yml:31-56` | Three alias sets (`_target_`, `_boot_`, `_disk_`) each re-map the same `bootwright_machine_port.endpoint/ca/user/password/verify`. | S |
| [Y02-12](#y02-12) | Quality | `roles/substrate_libvirt_machine/tasks/identity_agent.yml:33` | `when: substrate_libvirt_machine_marker_read.answered` has no default. | S |
| [Y03-05](#y03-05) | Non-duplication | `containercluster_media_agent/tasks/apply.yml:2-33 = destroy.yml:2-33` | Request validation and handoff are copied between media apply and destroy. | S |
| [Y03-06](#y03-06) | Security | `roles/containercluster_media_agent/tasks/publish.yml:93-125` | The image is renamed into the served subtree first, and only afterwards set to 0640 and labelled. | S |
| [Y03-07](#y03-07) | Quality / Right language | `containercluster_media_agent/tasks/publish.yml:34-44` | The publication segment is minted by spawning `openssl rand`, while insert tokens are minted in Python (`secrets.token_hex(32)`, plugins/modules/containercluster_media_token.py:252). | S |
| [Y03-08](#y03-08) | Quality | `roles/containercluster_media_agent/tasks/build.yml:216` | `lookup('ansible.builtin.file', ...)` reads on the Ansible controller. | S |
| [Y03-09](#y03-09) | Efficiency / Clearness | `roles/containercluster_install_agent/tasks/wait.yml:28-33` | Each wait always expands `wait_retries + 1` = 41 includes of a 6-task block, even after success. | S |
| [Y04-07](#y04-07) | Non-duplication / Right language | `roles/infrastructureservices_mirror_release/defaults/main.yml:47-50` | The "release held whole" rule (digest non-empty, missing == 0, components non-empty) is written three times in YAML: once for the first read and twice for the read-back, as the refusal and the fail. | S |
| [Y04-08](#y04-08) | Quality | `roles/infrastructureservices_mirror_fill/tasks/apply.yml:79` | The rescue tells a digest mismatch from an unreachable publisher by searching ansible-core's English error text for "checksum". | S |
| [Y04-09](#y04-09) | Non-duplication | `roles/storage_ceph_core/tasks/{apply` | Each Ceph entry point is the same 52-line run, refuse, fail, complete sequence. | M |
| [Y04-10](#y04-10) | Quality | `roles/storage_ceph_admin_credentials/tasks/prepare.yml:2-8` | Unlike its Ceph siblings, this role asserts neither the digest format nor `ansible_connection == 'local'`, and it does not name a module failure through the protocol's `refused` phase. | S |
| [Y04-11](#y04-11) | Quality | `roles/controller_prerequisites/tasks/main.yml:8` | The role checks only that `acquisition` and `tools` have the same length. | S |
| [Y04-12](#y04-12) | Efficiency | `roles/infrastructureservices_mirror_fill/tasks/apply.yml:87-96` | Every apply relabels the whole `public/mirror` tree with `chcon --recursive`, which includes other Environments' subtrees. | S |
| [B01-09](#b01-09) | Quality | `scripts/ansible-check:9` | the bootstrap runs only when `venv/bin/python` is missing. | S |
| [B01-10](#b01-10) | Quality | `Makefile:226-235` | `fmt-check` and `vet` cover only the root module (`go list ./...` and `vet ./...` do not descend into the nested scripts/tools module). | S |
| [B01-11](#b01-11) | Efficiency | `Makefile:248-249` | `check-go` runs govulncheck twice: `vulncheck` in source mode over `./...`, and `vulngate` over `./cmd/bootwright`. | S |
| [B01-12](#b01-12) | Security | `cmd/bootwright/vulnerability-exceptions.json` | 89 exceptions: virtctl 74, kubectl 9, helm 5, govc 1; 48 `not_affected` with a justification and 41 `no_fix`. | S |
| [B01-13](#b01-13) | Efficiency | `.github/workflows/check.yml:3-5` | `push` plus `pull_request`, with no `concurrency` group, runs the 90-minute check twice for a PR branch and never cancels superseded runs. | S |
| [E01-08](#e01-08) | Quality | `e2e/lab-sno-fips-controller/app/verify.py` | The "Case workload" row excludes a file that "is read by a render or a test", yet three Go tests execute `verify.py` and run `verify_test.py`. | S |
| [E01-09](#e01-09) | Quality | `e2e/lab-5-nodes-ceph-networks/app/sample-server.py:43-52` | `_health` guards `backend.ready()` with `except OSError`, but `source_address()` (a UDP `connect`) is called unguarded. | S |
| [E01-10](#e01-10) | Non-duplication | `e2e/lab-5-nodes-ceph-networks/app/sample-server.py:81` | SigV4 signing is implemented three times, twice standalone in e2e (nearly the same canonical-request code) and once in the collection. | S |
| [E01-11](#e01-11) | Efficiency | `cmd/bootwright/admission_acceptance_test.go:47` | `exampleDirectory(t, name)` calls `exampleDirectories(t)`, which execs `git ls-files` over all of examples/ on every call. | S |
| [E01-12](#e01-12) | Clearness | `e2e/lab-sno-fips-controller/app/verify.py:233` | `for phase in ("before", "after")` with a `break`: positive checks run, the negatives run, then the positives run again, but nothing changes between "before" and "after" and no rationale is given. | S |
| [E01-13](#e01-13) | Clearness | `e2e/lab-sno-fips-controller/api-review.md` | `api-review.md` is referenced from nowhere, and its 47 lines hold a point-in-time review inside a case directory. | S |
| [E01-14](#e01-14) | Clearness | `cmd/bootwright/admission_acceptance_test.go:128-160` | `admissibleExampleSources` derives the multidc projection with raw string surgery on YAML (`strings.Cut(body, " fdf:\n")`, `mustReplaceOnce`, hard-coded `dropped != 14`). | M |
| [E01-15](#e01-15) | Clearness | `docs/acceptance.md:150-173` | Ledger rows run up to ~3,200 characters inside one Markdown table cell (11 of 22 exceed 1,500), mixing outcome, deviations, numbered findings and digests. | M |
| [X01-03](#x01-03) | Security | `nativeartifacts/comparisonlocal/service.go:128-138` | Only 3 of the 11 supervisors put the child in its own process group and kill the group: adapterprotocol (:63), hostdnf (:508) and hostowned (:55). | S |
| [X01-04](#x01-04) | Quality | `controller/ansiblelocal/runner_linux_amd64.go:70` | The root controller Ansible run puts its scratch in the shared sticky `/var/tmp` and removes job and scratch with deferred, unchecked `os.RemoveAll`. | S-M |
| [X01-05](#x01-05) | Non-duplication | `storage/preflightlocal/observer_linux.go:157-176 openPathSafe` | Five hand-written "open each component with O_NOFOLLOW from /" walkers exist, two of them near-identical in one package (only `O_PATH` versus `O_RDONLY\|O_NONBLOCK` differ). | M |
| [X01-06](#x01-06) | Security, policy trade-off | `workspace/contextfs/files_linux_amd64.go:105-106` | contextfs hand-codes `renameat2` twice (`renameNoReplaceAt`, `renameNoReplaceIn`), `openat2`, `symlinkat` and `unlinkat` through `syscall.Syscall6` with `unsafe.Pointer` and amd64 trap numbers. | S |
| [X01-07](#x01-07) | Non-duplication | `acquisition/transport.go:26-56` | Two classifiers turn net/TLS/DNS errors into operator text. | S |
| [X01-08](#x01-08) | Quality | `cmd/bootwright/wiring_account.go:60` | `home()` and `uid()` resolve the account with `context.Background()`, because the port has no ctx. | S |
| [X02-03](#x02-03) | Security | `plugins/module_utils/ceph_host_route_probe.py:261-266` | `provider_source` admits the installed cephadm `ssh.py` and `utils.py` for `exec` after checking only "regular, not group- or world-writable, at most 1 MiB". | S |
| [X03-01](#x03-01) | Efficiency | `reconciliation/stage.go:158-167` | Every `scheduler.next()` call goes through `firstAdmission` to `Startable` to `Ready`. | S |
| [X03-02](#x03-02) | Efficiency | `roles/controller_prerequisites/tasks/main.yml:27-32` | The four target tools (govc, helm, kubectl, virtctl, per internal/controller/tooltable.json) are downloaded and verified one at a time through a looped `include_tasks`. | M |
| [X03-03](#x03-03) | Quality | `reconciliation/lifecycle/removal_gates.go:197-216` | `ReportProgress` computes `completed` under the mutex, then calls `p.report` after unlocking. | S |
| [X04-06](#x04-06) | DDD | `reconciliation/lifecycle/invocation.go:76` | The engine's `Execution` embeds Machine's `Placement` and `SSHOptions`. | M |
| [X04-07](#x04-07) | DDD | `containercluster/facts.go:4-24` | The container-cluster family root imports its one implementation and re-exports that implementation's types as the family vocabulary. | S |
| [X04-08](#x04-08) | Clearness | `substrate/admission.go:13` | The code departs from the glossary in several places: - `Variant(provider)` returns the present arm (`Variant(provider) != ArmKubeVirt`), but the glossary says a variant is never an arm. | S |
| [X04-09](#x04-09) | Clearness | `test/architecture/boundaries_test.go:42-68` | The 24 driven adapters use five naming conventions: - `*local` (11) - `*fs` (4) - `host*` (3) - `local*` (1: `localkeyring`) - a plain noun (5: privilege, material, encoding, yamlstream, ansiblerunner) Two packages are both named `preflightlocal`. | S |
| [X04-10](#x04-10) | Non-duplication | `addons/packages.go:36-41` | The `Approval` enum, with `Exact` and `OperatorManaged`, is declared twice and bridged with a conversion. | S |
| [X05-06](#x05-06) | Non-duplication | `storage/ceph/contracts.go:83-85` | Nine exported `ContentDigest` functions (same concept, inputs per capability) and three private `contentDigest(parts...)` helpers all re-implement "sha256 over NUL-joined parts, hex". | S |
| [X05-07](#x05-07) | Clearness | `validName: addons/clusterobjects/validation.go:31 (DNS subdomain)` | These are the same names for different rules. | S |
| [X05-08](#x05-08) | Non-duplication | `ansible/.../module_utils/controller_channel.py:12` | The 64 KiB record bound is pinned between Go `MaxLine` and Python `adapter_protocol.MAX_LINE`. | S |
| [X05-09](#x05-09) | Test coverage | `addons/clusterobjects/program_test.go:278 (vocabulary.golden)` | Go `Validate` holds the full program grammar. | S |
| [X06-03](#x06-03) | Non-duplication | `module_utils/libvirt_firewall_ready.py:13` | Four sibling utilities each declare `ENVIRONMENT`, `command(runner, arguments)` and `require(runner, arguments)`. | S |
| [X06-04](#x06-04) | Non-duplication | `roles/managedos_host_firewall/tasks/provided_apply.yml:8-44` | The three private files are identical apart from the task name, `operation`, `observed`/`removed`, and apply's extra package install with its `changed` term. | S |
| [X06-05](#x06-05) | Clearness | `module_utils/machine_hardware.py:41` | Verdict on the name collisions: - **`canonical`**: of 6 definitions, 1 is the owner (canonical_json.py:18). | S |
| [X06-06](#x06-06) | Quality / Non-duplication | `plugins/action/cluster_objects_read.py:33-36` | The read action defines a `write` closure it never uses: it passes `None` to `Engine`. | S |
| [X06-07](#x06-07) | Non-duplication | `plugins/action/artifact_server_protocol.py:57-117 and infra_service_prot` | B461 tracks the module_utils half of this pair. | S |
| [X07-04](#x07-04) | Right language / Non-duplication | `infrastructureservices_mirror_release/defaults/main.yml:47-50 (plus Y04-` | architecture.md:1106-1110 has the plugin refuse a mutation whose evidence is missing and has Go decide again. | M |
| [X07-05](#x07-05) | Right language / Test coverage | `plugins/module_utils/disk_discovery_boot.py:45-67 (INIT) and :70-92 (DRI` | The discovery initrd's PID-1 `init` and its driver probe are about 46 lines of shell held as Python strings. | S |
| [X07-06](#x07-06) | DDD / Clearness | `specs/architecture.md:760-766` | architecture.md lists exactly two allowed Go network contacts (bundlelocal publisher metadata and medialocal). | S |

## Appendix A. Lane records

The 45 lane reports as their reviewers returned them. Only the layout was
changed: headings were moved down two levels, each finding heading was
reduced to its id so that the id can be linked, and absolute paths were
shortened. `DATA/` names the measured data files of section 2, which stay
outside the repository. A lane's "Scope read" line says what it read in full, skimmed or
left out.

<a id="lane-g01"></a>

### Lane G01: Desired state and public API (Go)
Scope read: I read every production file in api/v1alpha1 fully, except openshift_clusters.go and addons_hub.go, which I skimmed (schema data). I read fully all of internal/desiredstate (root, compilation, customplaybooks, encoding, inputfs, yamlstream), internal/canonicaljson, internal/diagnostics and internal/availability. Skimmed: lexical_test.go, quoting_test.go, limits_regression_test.go, opener_linux_amd64_test.go, cmd/bootwright/wiring_desiredstate.go, and the comparison sides in secrets/material and workspace/contextfs. Not read: the other tests (about 60 files). I sampled them by name and size and used the coverage data instead.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Fail-closed, bounded, cancellable; some dead selection logic and stringly rule names |
| Security | 4 | inputfs is a model open boundary; one unshared enumeration budget |
| Efficiency | 3 | Normalization re-walks every tree and rebuilds schemas once per rule |
| Clearness | 3 | ValidLexical has cyclomatic complexity 86, boolean-trap parameter lists, schema lines of 1,000+ characters |
| DDD | 3 | Immutable wire tree is the de facto domain model, read through about 1,500 string paths |
| Non-duplication | 3 | openat/stat helpers copied across three adapters; about eight shape-guided walkers |
| Right language | 5 | All Go; nothing misplaced |
| Test coverage | 4 | 85-96% per package; limits and cancellation are pinned; a few refusal branches are thin |

#### Findings
##### G01-01

**Medium · Efficiency · confidence medium**

- **Where:** internal/desiredstate/compilation/phases.go:233-253, cmd/bootwright/wiring_desiredstate.go:42-66
- **What:** For every record, `normalizeRecord` runs all 13 rule sets. Each one re-walks the record's whole tree twice for the budget, diffs it for reordering and rebuilds its schema with `api.Schema`. This happens even when the rule ignores that kind and returns the object unchanged.
- **Evidence:** `previous := expansionBudget{}; if !previous.admit(record, r.ds)` … `if !budget.admit(record, r.ds)` … `record.recordReordering(previousSpec, object.Spec(), api.Schema(object.Kind()))`
- **Impact:** With up to 1,000,000 nodes that is about 39 full tree visits per node, and each visit clones its slice (`Value.Fields()`/`Items()`). That cost sits on every `validate`, `plan` and `render`.
- **Fix:** Admit once before the rule loop. Skip the re-admit and the reorder diff when the returned spec is unchanged (cheap identity check or `Equal`). Fetch the schema once per record. (effort S)
- **Checked:** limits_regression_test pins the ceilings but not timing. No benchmark exists, so the figure is derived, not measured.

##### G01-02

**Medium · Quality/Clearness · confidence high**

- **Where:** api/v1alpha1/lexical.go:57-155, internal/desiredstate/compilation/schema_validation.go:43
- **What:** Lexical rules are free strings. The schemas use `lexical("…")` and 51 production files call `ValidLexical("name", …)` directly. One switch with complexity 86 dispatches them. An unknown rule falls through to `return false`.
- **Evidence:** `func ValidLexical(rule, value string) bool {` … `}` / `return false`
- **Impact:** A misspelled rule silently refuses every value, or silently never accepts on an allow-branch, and no test enumerates which rules the schemas use.
- **Fix:** Add a `type LexicalRule string` with constants and a `map[LexicalRule]func(string) bool`. Add a test that walks every `Schema(kind)` and asserts each `Shape.Rule` is registered. (effort M)
- **Checked:** test/architecture has severity and diagnostic-code guards but nothing for rule names. lexical_test.go tests rules one by one.

##### G01-03

**Medium · DDD · confidence medium**

- **Where:** api/v1alpha1/value.go:72-84, api/v1alpha1/families.go:169
- **What:** Contexts read the generic `api.Value` tree by string path: 1,487 `.Spec().Get(`/`.Spec().Has(` sites across storage, containercluster, substrate, machine and managedos. A missing name returns `Value{}`, not an error. The values are immutable (private fields, cloned accessors), so the "shared mutable state" rule holds. But the translate-into-a-domain-model clause is rarely exercised.
- **Evidence:** `found := Value{}` … `v = found`; `{{"install", "nodeSSH", "keyPairRef"}, …}`
- **Impact:** A schema rename or path typo reads as absent instead of failing, so admission rules can silently stop applying.
- **Fix:** Add a fitness test that resolves every literal `Get`/`Has` path against `Schema(kind)`. Over time, give the hot kinds typed views. (effort M/L)
- **Checked:** no such test in test/architecture. The registry accessors in api.md:445-461 are deliberate, but they do not guard path spelling.

##### G01-04

**Medium · Non-duplication/Security · confidence high**

- **Where:** internal/desiredstate/inputfs/reader_linux_amd64.go:719-775, internal/secrets/material/files_linux_amd64.go:464-571, internal/workspace/contextfs/files_linux_amd64.go:137
- **What:** The same descriptor-walk primitives are copied: `processFiles`, `denied`/`openDenied`/`deniedToRoot`, fstat, stable-identity comparison and bounded read. The copies have already drifted. In secrets, identity includes `Uid`/`Gid` and the bounded read clears buffers and checks the read count; inputfs does neither.
- **Evidence:** inputfs `return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode`; secrets `… && left.Uid == right.Uid && left.Gid == right.Gid`
- **Impact:** Hardening applied to one open boundary does not reach the others, and reviewers must re-verify three walkers.
- **Fix:** Put the openat/identity/bounded-read core in one technical package, as was done with canonicaljson. Keep the consumer-owned `FileSession` ports. (effort M)
- **Checked:** the exact-clone data confirms 24 identical lines. Nothing is recorded in knowledge or backlog. The repeated ports are deliberate; the helper implementations are not ports.

##### G01-05

**Medium · Clearness · confidence high**

- **Where:** internal/desiredstate/compilation/schema_validation.go:12, phases.go:45, phases.go:278, defaults.go:12
- **What:** `validateShape` takes 8 parameters, including two adjacent bools, and call sites pass bare `true, false` / `false, true`. `inherit` takes 7, including `root bool`.
- **Evidence:** `validateShape(r.environment, r.defaults, …, "$.spec.defaults", true, false, api.Catalog{}, r.ds)`
- **Impact:** Swapping `partial` and `references` compiles and silently changes admission.
- **Fix:** Pass a small `walk{partial, references, catalog, sink}` value. (effort S)
- **Checked:** no named-constant wrapper exists.

##### G01-06

**Low · Quality · confidence high**

- **Where:** internal/desiredstate/compilation/selection.go:14-20, phases.go:68-74, yamlstream/parser.go:124-126
- **What:** `selectResources` marks every source file as selected, and `r.selected` is assigned but never read. The filter in `decodeSelected` therefore never drops a diagnostic, and the `addSyntax` comment refers to "excluded files" that cannot exist.
- **Evidence:** `for _, file := range sources.Files { selected[file.Path()] = true }`
- **Impact:** Dead logic suggests a file-level selection that the spec removed (api/input.md:60 "Select every discovered file").
- **Fix:** Delete the map, the field and the comment. (effort S)
- **Checked:** grep finds no other reader of `selected`.

##### G01-07

**Low · Security (input bounds) · confidence medium**

- **Where:** internal/desiredstate/inputfs/reader_linux_amd64.go:281-285, 328-341
- **What:** Each skipped `secrets` directory gets a fresh 65,536-entry budget for its YAML probe. That budget is not charged to `s.entries`.
- **Evidence:** `budget := desiredstate.MaxEntries` / `if s.holdsYAML(file, entry.name, depth+1, &budget)`
- **Impact:** Total enumeration can grow with the number of `secrets` directories, beyond the stated entry ceiling (api/input.md:78).
- **Fix:** Draw the probe from the remaining `MaxEntries - s.entries`. (effort S)
- **Checked:** no test covers several `secrets` directories.

##### G01-08

**Low · Efficiency · confidence high**

- **Where:** internal/desiredstate/compilation/selection.go:77-85, phases.go:130
- **What:** `validateSourcePaths` recomputes the root list against every source file for every record.
- **Evidence:** `for i, root := range roots { for _, file := range sources.Files {`
- **Impact:** O(records × files), up to about 33M comparisons per root, for a value that is constant across the compile.
- **Fix:** Compute the roots once in `run`. (effort S)
- **Checked:** the only caller is `validateAuthored`, which loops over records.

##### G01-09

**Low · Quality · confidence medium**

- **Where:** internal/desiredstate/encoding/encoding.go:252-258
- **What:** Effective YAML writes YAML-1.1-ambiguous strings plain: `yes`, `no`, `on`, `off`, `y`, `n`, sexagesimal `1:20`, dates. The project elsewhere refuses leading-zero integers for YAML 1.1 readers, and output.md:61 quotes YAML 1.1 booleans.
- **Evidence:** `coreSchemaScalar = regexp.MustCompile(`^(?:true|True|TRUE|false|…`
- **Impact:** A PyYAML or Ansible reader of `render effective` output reads such strings as booleans or integers.
- **Fix:** Treat the YAML 1.1 bool, base-60 and timestamp forms as non-plain, and add them to `quotingCorpus`. (effort S)
- **Checked:** yaml.v3 v3.0.5 node encoding force-quotes only when its own `resolve` differs (encode.go:449-455). Its `isOldBool` applies only to the Go string path, not to nodes.

##### G01-10

**Low · Clearness · confidence high**

- **Where:** api/v1alpha1/object.go:143, internal/desiredstate/compilation/origins.go:15
- **What:** Two unrelated types named `FieldOrigin` live in the same compile path: one is normalizer provenance, the other is the render origin label.
- **Impact:** Readers confuse them in `normalizeRecord` and `complete`.
- **Fix:** Rename one, for example `api.DerivedFrom`. (effort S)
- **Checked:** both are exported and used side by side.

##### G01-11

**Low · DDD/Non-duplication · confidence medium**

- **Where:** api/v1alpha1/addons.go (`PackageSubscription`, `MetalLBConfig`, `FDFConfig`), addons_hub.go:3-56
- **What:** Add-on package configs are defined twice in the API package: as JSON-tagged Go structs and as `Shape` schemas. Only internal/addons/packages decodes into the structs.
- **Impact:** A parallel type hierarchy in the wire package, which must be kept in step by hand.
- **Fix:** Move the structs to addons/packages. (effort S)
- **Checked:** `TestSchemaMaximalPackagesCompileDecodePlanAndCoverEveryMember` guards drift, so this is placement and duplication, not a defect.

##### G01-12

**Low · Clearness · confidence high**

- **Where:** api/v1alpha1/schema.go:127-145, schema_validation.go:177-182
- **What:** Schema registration is split: three kinds are in the map literal and the rest go through `init` and a panicking `register` across seven files. Separately, `Schema()` rebuilds the whole tree on every call. For example, `secretTypes()` rebuilds the Secret schema for every reference it checks.
- **Impact:** Registration order is harder to follow, and the rebuilds add to [G01-01](#g01-01).
- **Fix:** Use one literal map in schema.go, which removes the init functions and the panic. Cache the immutable enums such as the Secret types. (effort S) (policy trade-off: named exception)
- **Checked:** architecture.md:189-191 permits the registry. All factories are in one package, so a literal loses nothing.

##### G01-13

**Low · Clearness · confidence high**

- **Where:** api/v1alpha1/storage.go:124 (1,135 characters), :100 (859), storage_native.go:34 (671)
- **What:** Whole schemas are written as single lines.
- **Impact:** Diffs and reviews of a field change show the entire schema as changed.
- **Fix:** One field per line; gofmt keeps that layout. (effort S)
- **Checked:** formatting.md:23 sets no width rule, so this is readability only.

##### G01-14

**Low · Quality · confidence high**

- **Where:** compilation/compiler.go:104, yamlstream/parser.go:38-42, customplaybooks/validation.go:31-83
- **What:** On a parse failure, `Parse` already puts the failure into its returned diagnostics, and `Compile` appends `failures` again, relying on de-duplication. Separately, customplaybooks repeats the literal `999` 16 times instead of deriving it from `MaxDiagnostics`.
- **Fix:** Append the failure once, and use one named bound. (effort S)
- **Checked:** the sink's `seen` map hides the duplicate, so nothing user-visible is wrong.

#### Patterns
- Shape-guided traversal is re-implemented per concern: decoding.go:158, schema_validation.go:12, defaults.go:12 and :97, provenance.go:51, encoding.go:127 and :189, jsonschema.go:60. The three alternative selectors differ: `encoding.selectedShape` lacks the Integer-for-Number coercion that `shapeForValue` and `acceptsNode` apply. No schema has a Number alternative today, so there is no live effect.
- Field paths are string identities with copied prefix-walk loops: `sourceField`, `derivedOrigin`, `isInherited`, `location` (provenance.go:136, :32, diagnostics.go:234, :277). `valueAt` (diagnostics.go:399) and `referenceAt` (:185) parse the same grammar twice.
- `Value.Fields()`/`Items()` clone on every call, and recursive walkers (`admit`, `valueKey`, `fieldOrigins`/`originOf`, which rebuilds `authored.Value()` for every leaf) pay one slice copy per node.
- Several vocabularies are stringly typed: Severity, Code and lexical rules. They are held by fitness tests (diagnostic_severity_test.go) rather than by types.

#### Strengths
- inputfs: descriptor-relative `openat` with `O_NOFOLLOW` at every component, stat proof before and after, `Nlink==1`, `O_NONBLOCK`, bounded reads, a full re-walk of directories, and a virtual-session test double that proves no ambient open happens.
- The API values (`Object`, `Value`, `Catalog`) are immutable, with private fields and cloned accessors, so contexts cannot share writable state.
- The diagnostic sink is bounded, de-duplicated and provenance-aware, gives nearest-name suggestions and checks cancellation at every phase. The limits tests pin inclusive ceilings and "stops later callbacks".
- yamlstream walks nodes iteratively with per-document and aggregate node and depth ceilings, and translates away from yaml.v3 types at once.
- canonicaljson is small and exact: the size predictor refuses every shape whose size it cannot predict.

#### Coverage notes
- compilation/diagnostics.go:185 `referenceAt`, 69%: the branch that suppresses references to undecodable objects is thin, and it decides whether a diagnostic is hidden.
- compilation/schema_validation.go:327 `valueKey`, 60%: uniqueness of boolean, mapping and sequence set elements is untested.
- inputfs/reader_linux_amd64.go:55 `ReadFile`, 74%; `discoverThrough` (:182), 38%: the root-open denial and cancellation paths.
- compilation/selection.go:77 `validateSourcePaths`, 58%: the containment of external sources under input roots is a security-relevant refusal.
- internal/diagnostics own coverage is 38.2%: the `Compare` order, which fixes deterministic output, is tested only through consumers.
- No test asserts that every `Shape.Rule` a schema names is recognized ([G01-02](#g01-02)), and the encoding quoting corpus has no YAML 1.1 forms ([G01-09](#g01-09)).

#### Already recorded
- Audit 004 ARC-44 (report-fable.md:5704) and audit 003 report-claude.md:1147: the dead `storageOSD`, `storageMonitoringService`, `storageServiceSpec`, `storagePlacement` and `storageDeviceSelector` (api/v1alpha1/storage.go:23-58), measured at 0% by any test.
- Audit 003 API-01: `validImage` admits image references with no repository.
- The shape-clone lead (object.go:11-30 vs contextfs/checkpoints.go:8-27) is a false positive: both are plain constant enumerations.

<a id="lane-g02"></a>

### Lane G02: Environment, network, trust, native artifacts, adapter protocol (Go)
Scope read: I read every production file in internal/adapterprotocol, internal/trust (root and enrollment), internal/acquisition, internal/nativeartifacts (comparisonlocal, rendering, renderlocal), internal/nativeexec, internal/network, internal/environment (root: admission, selection, members, selections), environment/access, environment/clusterpreflight, environment/preflight and environment/inspection (service, object_user, console, keyring, result, contracts, container). Skimmed: adapterprotocol/process_linux_amd64_test.go, trust/records_test.go (names and the store-refusal table), comparisonlocal/process_linux_test.go, and the Test* listings of every lane test file. I did not read the remaining environment admission tests or most of enrollment/service_test.go (993 lines) or inspection/service_test.go, because coverage there is 93-99%. Outside the lane I opened only the clone counterparts and callers: containercluster/preflight/service.go, storage/preflight/contracts.go, cli/output_trust.go, controller/bundlelocal/acquisition.go, controller/ansiblelocal and reconciliation/ansiblerunner Run calls, secrets/values.go.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful lifetimes and fail-closed paths; a few ordering and context gaps |
| Security | 4 | Strong path, process and DNS-pinning work; the trust-store read is weaker than the parse boundary |
| Efficiency | 4 | Bounded concurrency; only a small fixpoint loop that redoes work |
| Clearness | 3 | The adapter session's state is spread over about 9 booleans, and shape/Enroll mix phases |
| DDD | 4 | Ports are consumer-owned and policy sits in services; the domain report carries dead wire tags |
| Non-duplication | 3 | Preflight tally copied three times; the walk-up loop three times; two directory-safety rules |
| Right language | 4 | Go owns decisions; one Python bootstrap is embedded as a Go string |
| Test coverage | 4 | Refusal-heavy behavioral tests; the root-delegation helper spawn never runs |

#### Findings
##### G02-01

**Medium · Non-duplication · confidence high**

- **Where:** internal/environment/clusterpreflight/combine.go:52-70, internal/containercluster/preflight/service.go:84-102, internal/storage/preflight/contracts.go:40-54
- **What:** Three packages each count `capacity.Check` statuses into a Summary, and two of them also copy the "not proved" verdict (the error code chosen by Failed versus Unknown/Pending). This is the measured 15-line exact clone.
- **Evidence:** `case capacity.Pending: result.Summary.Pending++ default: result.Summary.Unknown++` appears in all three; `code := "preflight.unknown"; if result.Summary.Failed > 0 { code = "preflight.failed" }` appears in two.
- **Impact:** A new check status, or a change to the verdict rule, has to be made in three places. `severity()` (combine.go:87) is a fourth encoding of the same ordering.
- **Fix:** Add `capacity.Summarize(checks)` and a `capacity.Verdict(summary)` (or one helper in clusterpreflight), and call them from all three (effort S).
- **Checked:** The copies are not deliberate ports: they are identical policy, not interfaces. Grepped the backlog and found no item.

##### G02-02

**Low · Security · confidence medium**

- **Where:** internal/nativeartifacts/renderlocal/files.go:137, internal/nativeartifacts/renderlocal/tree.go:148
- **What:** One package has two rules for "a directory safe for private output". `Files.unsafe` accepts a sticky directory that its group or others can write; `Files.foreign` refuses any group- or world-writable directory.
- **Evidence:** `if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0` versus `if uint32(stat.Mode)&0o022 != 0`
- **Impact:** `render --sensitive` accepts an output directory that `diff --output-dir` refuses. Nothing in the code or tests says why they differ.
- **Fix:** Use one predicate for both, or state why a sticky directory is enough for a single file renamed into place but not for a tree (effort S).
- **Checked:** Neither files_test.go nor tree_test.go pins the sticky case either way.

##### G02-03

**Low · Security · confidence medium**

- **Where:** internal/trust/records.go:117-125, internal/trust/hostkeys.go:136, 166-178
- **What:** The divergent-pin rule keys endpoints by the raw `HostToken(address, port)`. `ParseKnownHostsLine` compares through `hostTokenKey`, which lowercases names and unmaps IPv4-mapped addresses, as OpenSSH matching does. So two records whose addresses differ only in case or mapping, and that hold different keys, pass Validate.
- **Evidence:** `token := HostToken(record.Address, record.Port)` (records.go:125) versus `hostTokenKey(host) != hostTokenKey(HostToken(address, port))` (hostkeys.go:136)
- **Impact:** A multi-line known_hosts built from the store (storage/ceph/materials.go:73 writes one line per host) could pin one endpoint to "either key", which Validate's own comment says it prevents.
- **Fix:** Key `pinned`, `DropDivergentUnused` and `Supersede` by `hostTokenKey(HostToken(...))` (effort S).
- **Checked:** Machine `Normalize` (internal/machine/admission.go) canonicalizes MAC addresses but not SSH addresses. No test covers case or mapped variants.

##### G02-04

**Low · Security · confidence medium**

- **Where:** internal/trust/records.go:117-120, internal/trust/enrollment/service.go:159
- **What:** A stored record is held to a weaker rule than parsed input. Decode checks only that the key type is known and the key is non-empty. It does not run `qualify`, so it never checks that the blob agrees with the type, that `Fingerprint` matches the key, or that the port is in range. Enrollment then shows the stored `previous.Fingerprint` to the operator.
- **Evidence:** `case !known(record.KeyType) || record.PublicKey == "":` and `host.PreviousFingerprint = previous.Fingerprint`
- **Impact:** A damaged or hand-edited store shows a "changed from X" fingerprint that is not the pinned key's, and pins blobs the parse boundary would refuse.
- **Fix:** In `validate`, run `qualify(KeyType, PublicKey)` and require `Fingerprint == HostKey().Fingerprint()`, or derive the previous fingerprint from the key (effort S).
- **Checked:** `TestATrustStoreRefusesContentNoSessionCouldRead` has no case for a blob/type mismatch or a wrong fingerprint.

##### G02-05

**Low · Quality · confidence medium**

- **Where:** internal/nativeartifacts/comparisonlocal/artifacts.go:11, internal/nativeartifacts/rendering/contracts.go:33
- **What:** `Resolve` takes no context because the `Files` port has none, so under root delegation it starts the account helper with `context.Background()`. The helper deadline is one minute.
- **Evidence:** `ctx := context.Background()` before `s.perform(ctx, who, helperRequest{Action: actionResolve, ...})`
- **Impact:** An interrupt during `render` path resolution is not passed to the helper, which can hold the command for up to the helper deadline.
- **Fix:** Add a `ctx` parameter to `rendering.Files.Resolve` and pass the request context through (effort S).
- **Checked:** `Publish` and `WriteTrees` take ctx. Own-package coverage of `Service.Resolve` is 0%.

##### G02-06

**Low · Quality · confidence medium**

- **Where:** internal/adapterprotocol/process_linux_amd64.go:270-283, 296-297
- **What:** The comment explains why the single-process signal is safe once the adapter has been reaped. The group kill gets no such protection: `stop()` after reaping (a breach or cancel during the drain) and `reaped()` both send `kill(-pid)` on a PID that has already been waited for.
- **Evidence:** `// A reaped adapter refuses the signal, so it never reaches a reused process ID.` and then `_ = syscall.Kill(-s.command.Process.Pid, syscall.SIGKILL)`
- **Impact:** If no descendant still holds the group and the PID has been reused by a new group leader, an unrelated group gets SIGKILL. That needs a PID wrap inside the drain, so it is very unlikely.
- **Fix:** Kill the group only while the group is known to be alive (for example a pidfd-based check or `kill(-pid, 0)` before reap), or document why it is acceptable (effort S).
- **Checked:** B445 tracks the timing of the group kill, not this target question.

##### G02-07

**Low · Clearness · confidence high**

- **Where:** internal/adapterprotocol/process_linux_amd64.go:156-182
- **What:** The session state machine is encoded in nine booleans (`ended`, `exited`, `undelivered`, `canceled`, `completed`, `stopping`, `groupKilled`, ...). The channel field `cancelled` and the boolean `canceled` differ only by spelling.
- **Evidence:** `cancelled  <-chan struct{}` (163) and `canceled    bool` (177)
- **Impact:** `receive` (complexity 17) and `readEnded` turn on combinations such as `!s.ended || s.exited && !errors.Is(...)`, which are hard to check by reading. This is the one runner core.
- **Fix:** Rename the channel to `done`. Consider an explicit protocol-phase enum for the ended/exited/undelivered states (effort M).
- **Checked:** The runner tests (ansiblerunner, ansiblelocal) pin the behavior, so a rename is safe.

##### G02-08

**Low · Non-duplication · confidence high**

- **Where:** internal/nativeartifacts/renderlocal/files.go:26-39, internal/nativeartifacts/renderlocal/tree.go:77-90, internal/nativeartifacts/renderlocal/files.go:163-171
- **What:** "Walk up to the nearest existing ancestor and collect the missing components" is written three times (`Resolve`, `treeWriter.open`, `absent`).
- **Evidence:** `missing = append([]string{filepath.Base(existing)}, missing...)` appears in two of them.
- **Impact:** An edge-case fix (root, a non-ENOENT error) has to be applied three times.
- **Fix:** Add one `splitExisting(path) (existing string, missing []string, err error)` (effort S).
- **Checked:** The three behave the same apart from the error text.

##### G02-09

**Low · DDD · confidence high**

- **Where:** internal/trust/enrollment/requests.go:26-52, internal/cli/output_trust.go:160-169
- **What:** `enrollment.Report` and `HostReport` carry JSON tags, but nothing marshals them. The CLI copies the struct field for field into its own `trustHost`. This is the measured 10-line exact clone.
- **Evidence:** `Fingerprint string `json:"fingerprint,omitempty"`` exists on both types; the only marshaling in the package is `trust.Store`.
- **Impact:** The application type looks like a wire type, and someone editing it may think a tag change alters output.
- **Fix:** Remove the tags (and `json:"-"`) from the enrollment types and let the CLI own the wire shape (effort S).
- **Checked:** Grepped for json.Marshal/Encode in enrollment and the CLI: none marshal Report.

##### G02-10

**Low · Right language · confidence medium**

- **Where:** internal/adapterprotocol/process_linux_amd64.go:29-35
- **What:** A Python bootstrap (an isolated-interpreter assertion, then exec of controller_supervisor.py) is embedded in Go as a `-c` string.
- **Evidence:** `const entrypoint = `import sys\nassert sys.flags.isolated and ...``
- **Impact:** Python's tooling (pylint, the 3.9 floor, the unit suite) never sees it, and `assert` is skipped under `-O`.
- **Fix:** Ship it as a file in module_utils covered by test_python_sites, or replace `assert` with an explicit `if not ...: sys.exit(2)` (effort S). This is a policy trade-off if the bootstrap must stay outside the collection.
- **Checked:** `-O` is not passed today. No knowledge page explains the embedding.

##### G02-11

**Low · Clearness · confidence high**

- **Where:** internal/environment/inspection/object_user.go:22, internal/environment/inspection/service.go:124, 140-163
- **What:** `revealObjectUser` loops over a one-element slice. `revealDashboard` takes `StorageEntry` by value and works only because it mutates through the `Ceph` pointer, while its siblings take `*Entry`.
- **Evidence:** `for _, user := range []api.Object{user} {` and `func (s Service) revealDashboard(ctx context.Context, selected string, entry StorageEntry) error`
- **Impact:** Both read as mistakes. Revealed material reaching the result depends on an aliasing detail.
- **Fix:** Remove the loop, and pass `&result.Storage[0]` (effort S).
- **Checked:** The tests pass with either form, which is why the oddity survives.

#### Patterns
- The 999-issue cap is applied piece by piece and with literals: environment/admission.go:38, 67, 99 cap some appends, while rescue and mirror issues are appended uncapped. addons/admission.go:251 has `maxIssues`, and customplaybooks/validation.go repeats `999`. One api-level constant and one cap at the aggregation point would serve all of them.
- Consumer-owned port result types repeat a sibling's vocabulary: inspection/result.go:21-24 and storage/ceph/inspection.go:49-53 both define `DashboardReady`/`DashboardUnproved`/..., and the types Image/Monitor/DashboardURL match (an 18-line shape clone). This is acceptable as a port, but the status strings now have two owners.
- Identical CLI-facing helpers in sibling services: `canonicalNames` (access/nodes.go:20, inspection/service.go:185) and the "no cluster named" message (access/service.go:102-107, inspection/service.go:207-212).
- High complexity where phases mix: `adapterprotocol.shape` (complexity 39; four near-identical "optional member" blocks at record.go:281-292 could be one loop over the phase table), and `enrollment.Service.Enroll` (complexity 29, which reads, observes, evaluates, repairs, confirms and writes in one body).

#### Strengths
- The adapter decoder is bounded and canonical: line, record count, depth, case-folded duplicate members, lone surrogates. It is checked against the Python encoder's vectors and dispatch bounds (record_test.go:287, 411).
- renderlocal's tree writer works only through descriptors (openat with O_NOFOLLOW, O_EXCL). On failure it removes only what it created, matched by inode (tree.go:224-238); `Publish` uses os.Root with an atomic rename and fsync.
- Acquisition checks every resolved address before any dial and pins the dial to the checked answer, including an explicit proxy. Private-mirror permission does not carry over to another host (destination.go:78-114).
- known_hosts parsing refuses markers, hashed hosts, patterns and multiple entries, and requires the blob's algorithm to match the declared type (hostkeys.go:120-228).
- comparisonlocal drops privilege through a /proc/self/exe helper that re-checks uid and gid and refuses root, with a clean environment, Pdeathsig and a bounded response.

#### Coverage notes
- comparisonlocal/helper.go:87 `spawnHelper` and :249 `boundedBuffer.Write`: 0% by any test. The real root-delegation spawn and the response-overflow refusal never run; only the command's shape is asserted.
- acquisition/transport.go:26 `TransportFailure` and :59 `AnchoredDial`: 0% own (package own coverage 57.5%). The certificate-error rule that makes a failure non-routable is tested only indirectly through bundlelocal.
- adapterprotocol/process_linux_amd64.go:286 `drained` (0% own), :312 `readEnded` (33% own), :325 `receive` (50% own): the one runner core is tested mostly through the two runners. A table of read-error, exit and drain orderings in the package itself would pin the Ending precedence.
- trust/records_test.go:100: there is no case for a stored record whose blob contradicts its type, or whose fingerprint does not match its key (see [G02-04](#g02-04)).
- renderlocal/files.go:118-125: the branch where the rename succeeds but the directory sync fails is untested.
- nativeartifacts/comparisonlocal/artifacts.go:10 `Service.Resolve`: 0% own.

#### Already recorded
- audit 003 CLI-01: render under sudo publishes as root through `renderlocal.Files{}`. The current `comparisonlocal.Service.Publish`/`Resolve` look like its remedy; I did not re-verify the wiring.
- audit 003 PRJ-03: the plugin search paths are now pinned in `pluginEnvironment` (process_linux_amd64.go:115-132); it appears fixed.
- audit 003 entry citing inspection/service.go:108-112: ClearSecrets not run in the cancel window (the CLI side).
- tracked: B445 (the adapter group kill is delayed by the drain), which is related to [G02-06](#g02-06) but is not the same defect.

<a id="lane-g03"></a>

### Lane G03: CLI catalog, commands, parsing, validation, dispatch (Go)

Scope read: I read these files in full: catalog.go, all 19 commands_*.go, dispatch.go, dispatch_values.go, parsing.go, validation.go, validation_cluster.go, validation_diff.go, flags.go, invocation.go, invocation_progress.go, runner.go, completion.go, completion_scripts.go, confirmation.go, help.go, services.go, operation.go, unified.go, version.go, sbom.go, acquisitions.go, admin_credentials.go, info_selection.go, inherited_scope.go, lifecycle_actions.go and repetition.go. I also read validation_test.go and catalog_dispatch_test.go in full. I skimmed inherited_scope_test.go and the test lists of confirmation_test, runner_test, operation_test, invocation_test, dispatch_test, parsing_test and flags_empty_occurrence_test. Outside the lane I read lifecycle/authorization.go, lifecycle/rebind.go, lifecycle/execution.go:103, managedos/media.go:72-106 and inspection/service.go:261 only to settle claims. I did not read the output_*, display, presentation, progress or results files (another lane) or the embedded completion scripts.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Strict closed parser, fail-closed defaults; a few redundant branches, misleading names and dead code |
| Security | 5 | Argv bounds, 64-byte byte-wise prompt reads, cancellation checked at each step, escaped prompts, undisclosed-value placeholders |
| Efficiency | 4 | The command tree and catalog are rebuilt per lookup and up to three times per process; regexes compiled per call; trivial absolute cost |
| Clearness | 3 | Per-command behaviour is spread across a 53-branch validator and about ten tables keyed by path strings |
| DDD | 3 | The CLI re-implements lifecycle, media and info-selection rules that their bounded contexts own |
| Non-duplication | 3 | Three domain rules are copied, and routing goes through two switches per command; the measured clone pairs are false positives |
| Right language | 5 | Completion scripts are embedded files, not Go strings; no logic is pushed to shell |
| Test coverage | 5 | 94.2% own coverage; behavioural tables check refusals, cancellation, privilege and parity with the catalog |

#### Findings

##### G03-01

**Medium · Clearness/Non-duplication · confidence high**

- **Where:** inherited_scope.go:16-30, help.go:99-110, help.go:23-28, dispatch.go:103-155, validation.go:176-274, invocation.go:96-108, runner.go:244-267
- **What:** The spec says catalog.go is "the single list of commands ... so no second list of command paths exists". In practice, per-command facts live in many separate tables keyed by path strings:
- `contextFreeCommands` and `sshFlagsByCommand`
- the `helpExample` map
- the root help groups
- the dispatch switch, plus a second switch inside each `invoke*` function
- the 53-branch `validateCommandFlags`
- `contextFreeAcquisition` and `allowsEmpty`
- the `path ==` branches in `Runner.run`

- **Evidence:** architecture.md:388-390 says "`catalog.go` is the single list of commands; ... so no second list of command paths exists". gometrics gives `validateCommandFlags` CC 53, `Services.invoke` CC 27 and `resolveInvocation` CC 35.
- **Impact:** Adding or renaming a command means touching 6-10 places. Tests catch drift for dispatch and scope only (catalog_dispatch_test.go:13, inherited_scope_test.go:49); nothing catches it for examples or validators.
- **Fix:** Move the per-command facts onto `commandSpec`: `contextFree`, `sshFlags`, `example`, `validate func(*cobra.Command) string` and `invoke func(Services, ...)`. Then delete the side maps and the outer switches. (effort M)
- **Checked:** The consistency tests above exist, and the measured clone pairs in dispatch.go:34-80 are just the field list of the `commandResult` struct, not copy-paste. Neither audit 003 nor the backlog records this.

##### G03-02

**Medium · DDD/Non-duplication · confidence high**

- **Where:** validation.go:212-226, validation.go:469-479; reconciliation/lifecycle/rebind.go:18-34, lifecycle/authorization.go:107
- **What:** The CLI re-parses the whole `--authorize` grammar: data-loss, a 16-hex digest, and `rebind=<name>` once each. The lifecycle owns that grammar and parses it again (`splitRebinds` checks name validity and duplicates; `authorize` checks the digest). The CLI hard-codes the digest width that lifecycle derives.
- **Evidence:** `if len(value) != 16 {` (validation.go:470) against `return hex.EncodeToString(sum.Sum(nil))[:16]` (authorization.go:107). Also `if !dataLoss && (!rebind || !api.ValidLexical("name", secret)) || seen[name] {`.
- **Impact:** If lifecycle changes the digest or adds a token, the CLI refuses valid invocations with a usage error. The same rule now lives in two bounded contexts.
- **Fix:** Export one `lifecycle.ParseAuthorizations([]string) error` (or an `AuthorizationDigestLength` constant) and call it from validation, or let the service's `lifecycle.authorization` refusal stand alone. (effort S)
- **Checked:** flags_empty_occurrence_test.go:37-55 pins the CLI behaviour, but no test ties it to lifecycle's width.

##### G03-03

**Medium · DDD · confidence high**

- **Where:** info_selection.go:14-76, commands_machine.go:94-105, commands_secrets.go:73-84
- **What:** `machine info` and `secret info` fetch the whole list, filter it, and refuse unknown names with an `access.target` domain diagnostic, all inside the CLI. `cluster info` does the same thing in its service (inspection/service.go:261 `"the context "+selected+" selects no "+...`).
- **Evidence:** `if err := refuseUnknownInfo("Machine", result.Context, names, machineInfoNames(result)); err != nil {`
- **Impact:** Domain selection policy sits in the driving adapter, which architecture.md:395 forbids ("never ... sequences a cross-domain workflow" and "complete validated requests"). One spec rule (cli.md:191-198) is implemented two ways.
- **Fix:** Add `Names` to `inventory.ListRequest` and `custody.ListRequest` and move the filter and refusal into those services, as `inspection.ClusterInfoRequest.Names` already does. (effort M)
- **Checked:** The spec requires the behaviour, not its location. No B-item or audit-003 match.

##### G03-04

**Low · Non-duplication · confidence high**

- **Where:** validation.go:375-384, validation.go:391-398
- **What:** `mediaName` re-implements `managedos.ValidMediaName` with three regexes, and the `--sha256` check re-implements `managedos.NormalizeMediaDigest`.
- **Evidence:** `return !regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`).MatchString(stem)` against managedos/media.go:94 `reservedMediaStem`.
- **Impact:** Two copies of one rule. A parity test (validation_test.go:99 `TestTheCLIMediaNameIsTheStoreRule`) guards it, which shows the copy is redundant.
- **Fix:** Call `managedos.ValidMediaName` and `managedos.NormalizeMediaDigest` directly. (effort S)
- **Checked:** The parity test exists and the CLI may import managedos (the tests already do).

##### G03-05

**Low · Efficiency/Idiom · confidence high**

- **Where:** validation.go:118, :380, :383, :394; operation.go:17-24; validation.go:23-29; runner.go:166
- **What:** Four regexes are compiled on every call, even though the file already keeps others at package level (lines 19, 301). `commandCatalog()` rebuilds about 60 specs and their concatenated help text on every `specFor`, `SessionCommand` or `hasAvailableSubcommand` call. `validateInvocation` re-implements `specFor` inline. `helpExample` builds a 65-entry map for each of the roughly 60 commands while the tree is built. The tree itself is built by `ClassifyInvocation`, again by `Run`, and again by `repeatedCommand`.
- **Evidence:** `if !regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(user) {`
- **Impact:** Sub-millisecond per invocation, but it runs on every TAB completion. Mostly an idiom and consistency issue.
- **Fix:** Use package-level regex vars. Memoize the catalog once (it is immutable) as a path-indexed map and use `specFor` in validation. (effort S)
- **Checked:** Completion spawns one process per request, so the cost is per keystroke.

##### G03-06

**Low · Clearness · confidence high**

- **Where:** runner.go:415-417, invocation.go:16-17
- **What:** `selectedJSON` is an alias of `selectedStructured` and is true for YAML too. `InvocationClass.JSON` likewise "marks either structured format". There are 37 call sites under the wrong name.
- **Evidence:** `func selectedJSON(command *cobra.Command) bool { return selectedStructured(command) }`
- **Impact:** Readers misread the conditions, e.g. `ssh-ask-sudo-password conflicts with ... output` at validation.go:129.
- **Fix:** Delete the alias, use `selectedStructured`, and rename the field to `Structured`. (effort S)
- **Checked:** No call site differs in meaning.

##### G03-07

**Low · Quality · confidence high**

- **Where:** runner.go:357-365
- **What:** Two branches that cannot change the outcome, and a swallowed error.
- **Evidence:** `if presentErr != nil { return 1 }; return 1` and `if handled, _ := r.writeExecutedFailure(...); handled { return 1 }`
- **Impact:** The dead distinction suggests the write error matters when it does not. A later change could miss that `checkedWriter` is what really maps write failure to exit 1.
- **Fix:** Collapse each to `if handled { return 1 }` and name the reason in one line. (effort S)
- **Checked:** `Run` (runner.go:186) maps any writer error to 1 anyway.

##### G03-08

**Low · Quality · confidence high**

- **Where:** operation.go:11
- **What:** `implementedOperation` has no production caller; only catalog_dispatch_test.go:45 uses it. Its comment says the operation-cancellation boundary is gated on it, but runner.go:276 gates on `privilegedOperation`.
- **Evidence:** `func implementedOperation(path string) bool { return specFor(path).implemented }`
- **Impact:** The comment describes a gate that does not exist.
- **Fix:** Delete it, or use it at runner.go:276 if "implemented" was meant. In the test, check `specFor(path).implemented`. (effort S)
- **Checked:** Grepped the whole repo.

##### G03-09

**Low · Clearness · confidence medium**

- **Where:** parsing.go:263, parsing.go:224
- **What:** Whether a short flag accepts an attached value (`-fvalue`) is hard-coded to the letters `f` and `o`, not derived from the flag's type.
- **Evidence:** `if token[2] == '=' || short == "f" || short == "o" {`
- **Impact:** A new value-taking shorthand would be refused in attached form with "unknown flag", and nothing warns the author.
- **Fix:** Use `flag.Value.Type() != "bool"` (or `NoOptDefVal == ""`). (effort S)
- **Checked:** Today only `-f`, `-o` and `-h` exist, so there is no live defect.

##### G03-10

**Low · Clearness/DDD · confidence medium**

- **Where:** validation.go:121, :151, :172; validation_cluster.go:49, :57; dispatch_values.go:68-77
- **What:** The "validate" functions also normalize flag values in place through `normalizeScalar`. Dispatch silently depends on that: `values.names` splits on commas without trimming.
- **Evidence:** `normalizeScalar(flags, "name", strings.Join(names, ","))`
- **Impact:** The name hides a required side effect. Any path that dispatches without the matching validator gets untrimmed members. `machine rsh` already returns before normalizing (validation.go:162-166).
- **Fix:** Split into `normalizeFlags` and `validateFlags` steps in `Runner.run`, or have `requestValues.names` normalize. (effort S)
- **Checked:** `Runner.run` always validates before dispatch, so there is no live defect.

#### Patterns
- Double dispatch on the same string. `Services.invoke` switches on the path, then each `invoke<Domain>` switches again, with 18 default arms ("command has no application dispatch") that cannot be reached and show as uncovered in gocov-any-funcs (commands_machine.go:107, :132, :154; commands_secrets.go:100, :126). A path-to-function table would remove them.
- Command-specific policy inside the runner: `"controller install" || "controller check"` appears at runner.go:307, 308 and 372, and `version`, `schema`, `sbom` and completion branch at runner.go:244-267. These belong on the spec or the result type.
- Lexical grammars defined inline in the CLI instead of in `api.ValidLexical`: the `--ssh-user` POSIX regex (validation.go:118, which differs from api/v1alpha1/lexical.go:18 `userPattern`), the host-key fingerprint (validation.go:301), the `untilObject` regex (validation.go:19).
- Request wiring is inconsistent: `validate` bypasses `invokeResult` and returns `values.err` without the wrap (commands_desiredstate.go:47-53), and `invokeLogs` reads `stringValue(values.flags, "detail")` directly (commands_logs.go:44).

#### Strengths
- A closed, hand-rolled resolver (parsing.go:89-188) with raw argv bounds (parsing.go:18-33) and a fixed list of trusted reason strings (parsing.go:66-82), so unknown input never echoes raw parse errors.
- Confirmation (confirmation.go:217-242): one byte at a time with no read-ahead, a 64-byte cap, cancellation checked before and after each read, untrusted identities escaped in prompts, and remedies that repeat the exact invocation, with undisclosed values replaced by placeholders (repetition.go:44-45).
- Interrupt handling: an interrupt maps to `ErrInterrupted`, exit 130, and keeps the log list (runner.go:319-320). Sensitive results are cleared on every path through `defer result.clearSensitive()` (runner.go:289).
- The privilege classification (`ClassifyInvocation`) is purely syntactic and names stdin-secret and stdin-URL shapes for the sudo boundary (invocation.go:69-70).
- Tests derive consistency from behaviour: inherited-flag scope from the flags each command actually reads (inherited_scope_test.go:49), dispatch arms from the catalog, and CLI media-name parity with the store rule.

#### Coverage notes
- confirmation.go:83 `Confirmation.Interactive` is 0% covered by any test in the module. lifecycle/execution.go:103 uses it to force `unattended = true`, which makes digest-bound `--authorize data-loss` mandatory. It is a security-relevant branch with no test, though it fails closed.
- confirmation.go:48 `ConfirmHostKey`: 3 of 13 statements uncovered (77%). These are the not-configured and cancel refusals of the host-key prompt.
- runner.go:355 `writeServiceDiagnostics`: 4 of 22 statements uncovered (82%), including the structured controller-report failure branch at runner.go:372-379.
- commands_storage_access.go:28 `writeKeyring`: 80%. The rejection of an unsupported result before raw keyring bytes are written is the uncovered part worth a test.
- No test ties the CLI's 16-character authorization digest (validation.go:470) to `lifecycle.AuthorizationDigest`. A parity test like the media one would catch drift (see [G03-02](#g03-02)).

#### Already recorded
- None of the findings above matches an audit-003 ID or an open B-item; grep found no hits for these symbols in `specs/milestones/` or the audit-003 reports. The nearest audit-003 neighbours are spec mismatches, not this code-quality issue: the bare `render` privilege classification (invocation.go) and the `machine trust` structured-output text (validation.go).

<a id="lane-g04"></a>

### Lane G04: CLI output, rendering, progress and results (Go)

Scope read: I read these files fully: output.go, output_additional.go, results.go, display.go, presentation.go, presentation_resolution.go, progress.go, lifecycle_presentation.go, lifecycle_actions.go, and every output*.go file (cluster, container, secrets, controller, controller_structured, reconciliation, lifecycle_structured, status, status_live, diff, diff_text, diff_files, diff_trees, trust, machine, machine_power, machine_copy, machine_discovery, managedos, logs, effective, render, handoff, containercluster, contexts, context_plan, uninstall, addons, addon_info, storage_preflight). I skimmed progress_test.go (first 200 lines and the test list), lifecycle_presentation_test.go (test list), golden_test.go (structure), result_registry_test.go, controller_labels_test.go, output_uninstall_test.go and unified.go (head). I did not read the remaining roughly 70 CLI tests one by one; I used the measured coverage instead. Outside the lane I opened these only to confirm claims: hostdnf stream, inspection/object_user.go, containercluster/access/handoff.go, cmd/bootwright/run.go and wiring_discovery.go.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 3 | Some warnings are dropped, some values are escaped twice, and structured mode can fall through into the raw-byte writers |
| Security | 3 | Escaping is thorough almost everywhere, but `controller uninstall` streams raw dnf bytes to the terminal |
| Efficiency | 4 | Bounded, linear rendering; only small repeated work |
| Clearness | 3 | Path-string switches with complexity 67 and 46, two output-format sources, a misleading `selectedJSON` |
| DDD | 3 | The diff classification and its exit status are computed in the output layer; 33 `valid*` guards restate domain invariants |
| Non-duplication | 3 | 22 hand-built envelopes, a duplicated warning literal, a reveal guard written twice, two DTOs for one command |
| Right language | 5 | Pure Go, no embedded languages |
| Test coverage | 4 | 94.2% own coverage, a golden for every command, a manual clock; no real-goroutine progress test and no escaping tests for several renderers |

#### Findings

##### G04-01

**High · Clearness/Non-duplication · confidence high**

- **Where:** results.go:43-133, output_additional.go:130-208, specs/architecture.md:388-390
- **What:** Rendering is routed by literal command-path strings in two separate switches: about 45 paths in `writeResult` and about 30 in `writeAdditionalStructuredResult`. The structured rendering of a single command is split between them. For example, `controller install` without dry run goes through the additional switch, while `--dry-run` falls into `writeControllerOutcome`.
- **Evidence:** `case "secret set", "secret generate", "secret delete":` (results.go:79); `case (path == "secret set" || path == "secret generate" || path == "secret delete") && ...` (output_additional.go:153). The spec says "`catalog.go` is the single list of commands … so no second list of command paths exists."
- **Impact:** Every new command or result has to be added to the catalog, two switches and the registry test, and the measured complexity is 67 and 46.
- **Fix:** Let each catalog entry (or each result type) carry a renderer with a human and a structured method, so that `writeResult` dispatches by result type and not by path (effort M).
- **Checked:** I looked for a generated or table-driven route and found none. The only guard against drift is the registry test (result_registry_test.go:17), which repeats the list a third time.

##### G04-02

**Medium · Security · confidence medium-high**

- **Where:** output_uninstall.go:51-56, internal/controller/hostdnf/packages_linux_amd64.go:495-499, specs/security.md:970-972
- **What:** `RemovalOutput` hands the raw stdout writer to dnf. Its stdout and stderr bytes reach the terminal unescaped and unbounded, including package and repository text that a third party controls.
- **Evidence:** `return p.progress.out` and `_, _ = s.out.Write(p)`. The spec says: "Raw callback bytes, terminal control sequences … are forbidden. Attacker-controlled text is escaped."
- **Impact:** A malicious or compromised repository can inject terminal control sequences into the operator's terminal during `controller uninstall`.
- **Fix:** Wrap the writer in a line-buffered writer that applies `escapeDisplayLine` to each line, or discard the output, as the install path does with its setup run (effort S).
- **Checked:** JSON mode is safe, because run.go:172-175 swaps in io.Discard. No other raw stream path exists in the lane. Coverage for this function is 0%.

##### G04-03

**Medium · Security/Quality · confidence medium**

- **Where:** output_additional.go:180-207, results.go:121-122, output_secrets.go:279-290
- **What:** In structured mode, when a reveal result fails the structured guard, the function returns `(false, nil)`. `writeResult` then continues into the human path `writeReveal`, which writes the raw secret bytes to stdout if its own, separately written guard passes.
- **Evidence:** `if !present || len(value) > secrets.MaxPartBytes … { return false, nil }`, followed by `case "secret show", ...: return writeReveal(...)`.
- **Impact:** Today the two guards are identical, so nothing leaks. Loosening one of them later would print raw secret bytes into a JSON or YAML stream.
- **Fix:** For structured reveal paths, return a `resultFailure` instead of "unhandled", and share one validation function for the material (effort S).
- **Checked:** I compared the guards for secret show, kubeconfig and keyring. They match now, and `keyringValue` is already shared.

##### G04-04

**Medium · Quality · confidence high**

- **Where:** results.go:56-65 (compare results.go:66-71 and 184-188)
- **What:** Human `machine discover` never writes `Operation.Warnings` or the dry-run `lifecyclePlan.Warnings` to stderr. `apply`, `destroy` and `plan` do.
- **Evidence:** `if result.machineDiscovery != nil { return true, writeMachineDiscovery(...) }`, while `apply` first calls `writeHumanDiagnostics(errOut, displayDiagnostics(result.lifecycleOperation.Warnings))`.
- **Impact:** Warnings from `converge` and from removals (executor.go:217) are lost in text mode. Structured mode keeps them (output_machine_discovery.go:54).
- **Fix:** Write the warnings first, as the `apply`/`plan` branches do (effort S).
- **Checked:** The failure path (`writeExecutedLifecycle`) does print the warnings. No golden for discover covers warnings.

##### G04-05

**Medium · DDD · confidence medium**

- **Where:** output_diff.go:279-293, 302-324, 164-191; output_diff_trees.go:13-16
- **What:** The output layer computes the diff verdict. It classifies each object as changed, added, removed or unmanaged by comparing encoded bytes, rewrites the real side to unknown (`documented`), derives exit status 3, writes trees through a port and runs an external tool.
- **Evidence:** `case bytes.Equal(real, desired): return differenceNone` and `status = diffDiffersStatus`.
- **Impact:** Comparison policy that decides the exit code lives in the adapter, so no other caller can reuse it or test it without the CLI.
- **Fix:** Have the lifecycle or a comparison service return each object's difference and the summary; the CLI would only render (effort M).
- **Checked:** `lifecycle.ObjectDiff` carries no difference field. The output spec defines the values but does not say who computes them.

##### G04-06

**Low · Quality · confidence high**

- **Where:** output_uninstall.go:43, 79, 83, 87; output_reconciliation.go:260 together with :198
- **What:** These values are escaped twice. `displayLines` or `escapeDisplayLine` runs first, then `text.lines` or `field` escapes again through `r.paragraph`/`r.fields`.
- **Evidence:** `text.lines(displayLines(plan.Kept))` and `shown := displayUninstall(result)` followed by `text.lines(shown.Packages)`.
- **Impact:** A path containing `\` or a control character is shown as `\\\\` or `\\n`, which breaks the rule that values "are escaped exactly once before layout" (output.md:105).
- **Fix:** Pass the raw values to `display` (effort S).
- **Checked:** I grepped every `text.*(` call that receives an escaped value. These are the only hits apart from [G04-07](#g04-07).

##### G04-07

**Low · Quality/Clearness · confidence high**

- **Where:** output_machine_discovery.go:59-71; output_addons.go:33-38; output_addon_info.go:54-82
- **What:** The newest human renderer (disk inventory) bypasses `display` with raw `Fprintf`, and it applies `%q` to a value that is already escaped.
- **Evidence:** `fmt.Fprintf(out, "  %s  sizeBytes=%d  model=%q ...", ..., d.Model, ...)`
- **Impact:** `--width`, `--ascii` and colour are ignored, and the model string is escaped twice. This is the same class of defect as add-ons.
- **Fix:** Render through `text.resourceTable` (effort S).
- **Checked:** The add-ons part is already recorded as OUT-13; the discovery renderer is new.

##### G04-08

**Low · Security/Quality · confidence medium**

- **Where:** output_handoff.go:28-41, 42-46
- **What:** The handoff descriptor is the only DTO that skips `escapeDisplayLine`. It is also the only encoder that keeps Go's default HTML escaping.
- **Evidence:** `Context: result.Context, Cluster: result.Cluster, … Arguments: slices.Clone(result.Arguments)` and `encoder := json.NewEncoder(out)` with no `SetEscapeHTML(false)`.
- **Impact:** Operator arguments that contain C1 controls (which `safeText` allows) reach the terminal raw, and `<`, `>` and `&` come out as `<` and similar. This differs from the output.md:21 "escaped descriptor" and from line 451.
- **Fix:** Escape the fields and set `SetEscapeHTML(false)` (effort S).
- **Checked:** `safeText` (handoff.go:66) rejects only NUL, CR and LF. Because the input is self-supplied, the severity is Low.

##### G04-09

**Low · Non-duplication · confidence high**

- **Where:** 22 hand-built `commandEnvelope{SchemaVersion: "v1alpha1", ...}` literals (for example output_secrets.go:235, 246, 321, 562; output_machine.go:52, 128), compared with `writeStructuredResult` (output_additional.go:20-22)
- **What:** Most structured writers rebuild the envelope by hand. Some escape `Command` and some do not (output_storage_preflight.go:57, output_machine_copy.go:47).
- **Impact:** Envelope rules (the escaping, the logs, the `ok` flag) drift from writer to writer.
- **Fix:** Route every envelope through `writeStructuredResult` (effort S).
- **Checked:** The schema version is constant across all 22 sites.

##### G04-10

**Low · Non-duplication · confidence high**

- **Where:** output_contexts.go:132 and output_additional.go:151
- **What:** The `context.orphaned` warning text is written out twice, once in the human renderer and once in the structured renderer.
- **Impact:** A wording change made in one place gives the two modes different diagnostics.
- **Fix:** Use a single `orphanedWarning()` (effort S).
- **Checked:** There is no shared constant.

##### G04-11

**Low · DDD/Non-duplication · confidence medium**

- **Where:** output_controller.go:463-523 and output_controller_structured.go:10-77
- **What:** One command is served by two structured DTOs: `setupPlanResult` for dry run and failures, and `controllerPresentation` otherwise. The second one copies fields from the first.
- **Evidence:** `plan := displaySetupPlan(report)` and then `Actions: plan.Actions, ...`. The registry maps `controller install` to both types.
- **Impact:** Dry-run JSON leaves out the context, machine, route, preview and next fields that the live JSON carries. This conflicts with the rule of "one CLI-owned tagged DTO" per result.
- **Fix:** Emit `controllerPresentation` in both cases (effort S).
- **Checked:** output.md:287 lists a single field set for the command.

##### G04-12

**Low · Clearness · confidence high**

- **Where:** runner.go:415-417; results.go:38 versus :266; display.go:451
- **What:** `selectedJSON` is an alias for `selectedStructured`, so every `jsonMode` parameter really means "JSON or YAML". The format is read both from the flag and from `presentationFor(out)`. `displayRow` is dead code (0% coverage, no callers).
- **Impact:** Readers misjudge which modes a branch serves.
- **Fix:** Rename to `structured`, keep one source for the format, and delete `displayRow` (effort S).
- **Checked:** I grepped for callers.

#### Patterns
- Whether a string is already escaped is a convention and not a type. There are 428 `escapeDisplayLine` calls, and display text and raw text are both plain `string`. This is the root cause of [G04-06](#g04-06), [G04-07](#g04-07) and [G04-08](#g04-08), and of the unescaped object-user Name and Gateway at output_cluster.go:314. A `displayText` type, or escaping once at the encoder, would remove the whole class.
- Each reveal is decided twice, once per renderer: `d.Revealed` at output_cluster.go:281 and :505, and `console.Revealed` at output_container.go:87 and :172. Revealed passwords are escaped (`PasswordValue: escapeDisplayLine(...)`) while S3 keys are emitted raw (output_cluster.go:314), so two revealed credentials in one DTO follow different policies.
- 33 `valid*` guards restate the invariants of domain results (for example `validManagedCeph` has complexity 27 and `validDashboard` 24). Any mismatch surfaces only as a generic "application service returned an unsupported result" (runner.go:352). This is a policy trade-off: it fails closed, but the cause cannot be diagnosed.
- Styling and width calls reach the injected `Columns()` reader on every fragment (`colored()` at presentation.go:90, `terminal()` at progress.go:547). This costs many small reads per table, which is minor.

#### Strengths
- The progress presenter is well built. One mutex covers rows, heartbeat and redraw, and a generation counter discards stale timers. Clock and width are injected, so the tests are deterministic. Echo hold is released in `closeLine`, and the caller also restores echo with `defer preserveTerminalEcho` (run.go:98).
- One JSON encoding pass feeds YAML through `yaml.Node` with forced quoting and C1/U+FFFE escaping (output.go:30-88). This keeps JSON and YAML equivalent from a single DTO.
- The `jsonResult` marker interface stops domain types from being encoded as results. The registry test enforces it.
- Secret material is cleared after rendering (`defer clear(value)`), and byte-exact export goes through utf-8 or base64 `export.content`.
- Every available command has a golden, with an `-update` workflow, and `TestEveryControllerCheckHasALabel` checks that the label table is complete.

#### Coverage notes
- output_uninstall.go:51 `RemovalOutput`: 0%. This is the raw-stream path in [G04-02](#g04-02).
- progress.go: the tests fire timers synchronously (progress_test.go:50-63). No test runs `report` concurrently with the real `time.AfterFunc` heartbeat under `-race`.
- No test covers human `machine discover` warnings or dry-run plan warnings ([G04-04](#g04-04)).
- No test feeds `\` or control characters to the uninstall renderers, the `--authorize` text, the handoff or the disk inventory ([G04-06](#g04-06), [G04-07](#g04-07), [G04-08](#g04-08)).
- output_additional.go:127 `writeAdditionalStructuredResult`: 4 of 55 statements uncovered, including the structured reveal fall-through branches ([G04-03](#g04-03)).
- output_diff.go:145 `writeDiffStatus`: 87%. The output-dir and tool branches are only partly covered.

#### Already recorded
- OUT-13 (audit 003): add-ons human output bypasses the renderer. It is still present, and [G04-07](#g04-07) extends it to disk discovery.
- OUT-07 (audit 003) and Q100/D183: revealing `objectUsers.secretKey`. It is authorized; only the escaping inconsistency is new.
- OUT-11 and OUT-15 (audit 003): retained adapter output and the log sanitizer. They are related to [G04-02](#g04-02) but are not the same path.

<a id="lane-g05"></a>

### Lane G05: Composition root cmd/bootwright (Go)
Scope read: I read all 40 non-test files in cmd/bootwright, plus ansible/assets.go and ansible/variables.go. / Test files sampled (11): session_refusal_test, presentation_test, secret_stdin_boundary_test, interrupts_linux_amd64_test (bodies); contract_versions_test (manifest generator); run_test, main_test, wiring_machine_test, wiring_readiness_test, wiring_firewall_test, acquisitions_test (test lists and helpers). / I did not assess the capability contract suites; that belongs to lane T02.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | The process boundary is careful, but construction and decode errors are dropped in several places. |
| Security | 4 | Privilege crossing, signals and stdin are well built; the vulnerability-exception rules exist twice and disagree. |
| Efficiency | 3 | The account is re-resolved repeatedly (B348) and firewall admission recomputes plan closures. |
| Clearness | 3 | Wiring files mix binding with domain policy (228 functions); the most complex ones are firewall policy. |
| DDD | 2 | The acquisition and mirror-fill domain, firewall policy and proof decoding live in the composition root. |
| Non-duplication | 3 | Image-reference rules, the exception schema, plan closure and per-kind service facts are each restated. |
| Right language | 5 | It is all Go, the process facts go through syscall, and no other language is embedded. |
| Test coverage | 4 | 83.1% with strong real-process interrupt tests, but 0% on the account binding and several named-exception translations. |

#### Findings
##### G05-01

**High · DDD · confidence high**

- **Where:** cmd/bootwright/acquisitions.go:32-35, :194-247, :360-373; acquisitions_registries.go:156-168; acquisitions_operators.go:87-94; acquisitions_machines.go:10-13; mirror_probe.go:105; wiring_reconciliation.go:337, :344, :348
- **What:** The whole acquisition domain sits in package main: vendor hosts, release-signature stores, catalog index repositories, CDN companions, OCI reference parsing, and the managed mirror's fill sets. The fill sets are bound straight into the mirrorfill capabilities, so composition decides what the mirror copies.
- **Evidence:** `capability: mirrorfill.NewImages(runner, mirrorImageArtifacts)` (wiring_reconciliation.go:344); `var releaseSignatureStores = []string{"https://storage.googleapis.com/..."` (acquisitions.go:32); an `internal/acquisition` package already exists and is only imported for `OriginClass`.
- **Impact:** About 700 production lines and 13 domain tests (acquisitions_test.go) live where architecture.md:363-365 forbids domain decisions, and every new managed service or image means editing main.
- **Fix:** Move the list and the fill-set derivation into `internal/acquisition` (or mirrorfill) as pure catalog functions; composition then only binds `acquisition.List` and the fill functions. (effort M)
- **Checked:** audit 004 ARC-26 names controllerSockets, hostPortChecks, installProofs and the KubeVirt closures, but not acquisitions or mirror fill. No B item tracks the placement; B1300 and B1336 are content items.

##### G05-02

**Medium · Non-duplication · confidence high**

- **Where:** cmd/bootwright/acquisitions.go:362-364; internal/infrastructureservices/managedservice/mirror.go:51-56; acquisitions.go:205-209; billofmaterials.go:187-189
- **What:** The rule that qualifies a short image name to docker.io is written twice, and the two copies differ. A third and fourth hand-written reference split (tag and digest stripping) sit beside them.
- **Evidence:** acquisitions: `if first, _, _ := strings.Cut(repository, "/"); !strings.ContainsAny(first, ".:") && first != "localhost" { repository = "docker.io/" + repository`. mirror.go has the same rule and also `strings.CutPrefix(qualified, "registry-1.docker.io/")`, which acquisitions lacks.
- **Impact:** For an image named through `registry-1.docker.io`, the acquisition list and the mirror can name and class it differently.
- **Fix:** Write one OCI reference parser and qualifier in the acquisition or managedservice owner and use it at all four sites. (effort S)
- **Checked:** I searched for a shared reference parser and found none (`grep ^func .*Reference(`).

##### G05-03

**Medium · Security · confidence high**

- **Where:** cmd/bootwright/billofmaterials.go:43-49; scripts/tools/vulngate/main.go:183-202; billofmaterials_test.go:246
- **What:** The embedded vulnerability-exceptions document is decoded and judged twice, with different rules. vulngate refuses unknown fields, justifications outside its list, missing or expired dates and expiries more than 90 days out. The BOM decoder accepts unknown fields, any non-empty justification and no date check at all. The test adds a third rule (`len(Expires) != 10`).
- **Evidence:** billofmaterials.go:43 `json.Unmarshal(vulnerabilityExceptions, &document)` with no `DisallowUnknownFields`; :48 checks only the state and that the justification is non-empty.
- **Impact:** A build stamped unchecked (`VULNCHECK=skip`, Makefile:28) publishes in its BOM exceptions that the gate would refuse.
- **Fix:** Put the document type and its validator in one package that both vulngate and the BOM import; the BOM then reports exactly what the gate admits. (effort S)
- **Checked:** the Makefile's `build` depends on `vulngate`, but the skip path still embeds the file.

##### G05-04

**Medium · Efficiency · confidence medium**

- **Where:** cmd/bootwright/wiring_firewall.go:172-184; wiring_service_firewall.go:113-145, :151-155
- **What:** The transitive dependency closure of a plan is written twice: once as a recursive `visit` over a map, once as a BFS through `plan.Block`, which scans linearly and clones every block it returns (request bytes included). `firewallBlockAdmission` runs for every block attempt and calls `frozenHostFirewall` for each ancestor; that call recomputes a closure and decodes the firewall requests again. The cost is O(A²·N) block clones per attempt.
- **Evidence:** `if block, ok := plan.Block(next); ok {` (:140); `request, managed := frozenHostFirewall(plan, ancestor.ID)` (:155). A failed decode is skipped (`if err != nil || !request.Direct ... continue`, :120), so block admission checks nothing for that block (it fails open), while apply admission keeps checking it.
- **Impact:** The allocation churn grows with plan size on every attempt, and a closure helper that Reconciliation should own is restated.
- **Fix:** Export an indexed `Plan.Ancestors(id)` from reconciliation. Decode the firewall requests once per admission into a publisher-to-request map, and refuse a decode error. (effort S-M)
- **Checked:** reconciliation exports no ancestor or closure helper; lifecycle repeats its own loops (removal_phases.go:56,103,120).

##### G05-05

**Medium · DDD · confidence medium**

- **Where:** cmd/bootwright/wiring_discovery.go:28-51, :53-74; wiring_containercluster.go:52-87; wiring_inspection.go:56-99, :103-128; wiring_machine.go:194-221
- **What:** Application work and decoding that the named exceptions do not list:
- `machineDiscovery.Discover` orchestrates Prepare, gates on the receipt state and collects evidence per Machine.
- `discoveryPreparations` decides which implementations are prerequisites of discovery.
- `clusterClientMetadata.Client` decodes `agentinstall` install requests and picks the tool kind.
- `inspectionCustody` validates the shape of Ceph secret material.
- `cephInspection.ObjectUserProved` matches frozen blocks against proofs.
- `machineRealization` decides which implementation realizes a Machine.
- **Evidence:** `request, err := agentinstall.DecodeInstallRequest(record.Block.Request)` (wiring_containercluster.go:61); `if err != nil || operation.Receipt.State != string(reconciliation.OperationDone)` (wiring_discovery.go:34).
- **Impact:** Each of these is another case of what architecture.md:156 calls a defect, and several are untested (see Coverage notes).
- **Fix:** Move each into its owner (an installation discovery service, `agentinstall.InstalledClient`, ceph material decoders, and a lifecycle proof matcher) so composition only binds them. (effort M)
- **Checked:** these are related to ARC-26, ARC-29 and ARC-06 but none of them names these sites. The named exceptions cover only machineOwnership, machineHostKeys, machineIdentities and provedIdentities.

##### G05-06

**Low · Quality · confidence high**

- **Where:** cmd/bootwright/wiring_controller.go:67, :104; execution_contracts.go:23; ansible/assets.go:18
- **What:** Construction and decode failures are dropped and turn into "unavailable" or empty state. If the embedded runtime lock fails, `Python` and `Lock` stay nil and the Controller service reports `availability.ErrNotImplemented` (prerequisites/service.go:54-56). If the manifest decode fails, every block has no contract. A failed collection walk would yield a partial digest.
- **Evidence:** `if lock, err := runtimelock.Embedded(); err == nil {`; `if json.Unmarshal(executionManifest, &contracts) != nil { return nil }`; `_ = fs.WalkDir(embedded, ...`.
- **Impact:** A build defect reaches the operator as "not implemented" instead of an internal failure.
- **Fix:** Panic at init for embedded-asset decode failures (they are build invariants), or carry the error into the service's availability diagnostic. (effort S)
- **Checked:** TestEveryImplementationContractMatchesItsGolden pins the manifest bytes, so this is reachable only through a broken build. That is why it is Low.

##### G05-07

**Low · Quality · confidence medium**

- **Where:** cmd/bootwright/wiring_inspection.go:252-255
- **What:** `HostKeyProved` turns a trust store that cannot be decoded into "not proved", with no error.
- **Evidence:** `store, err := trust.Decode(data); if err != nil { return false, nil }`.
- **Impact:** A corrupt per-context trust store shows up as an unproved host key instead of a store failure that needs a remedy.
- **Fix:** Return the decode error, as the `ReadHostKeys` error two lines above is returned. (effort S)
- **Checked:** cluster_inspection_hostkey_test.go covers the found and absent cases; I saw no corrupt-store case.

##### G05-08

**Low · Non-duplication · confidence high**

- **Where:** cmd/bootwright/wiring_machine.go:65; wiring_reconciliation.go:388, :277, :279, :294; wiring_desiredstate.go:31-33; wiring_preflight.go:59-64, :78-80
- **What:** Adapter construction is repeated:
- Two Ansible runners, and two KubeVirt lenders, are built per invocation.
- The `machineaccess` service and its `sshlocal` client are built in three places; `remoteCommands.Run` rebuilds both on every command.
- `packages.Builtin()` builds the add-on registry seven times. The test seam `wireCompilerWithRegistry` injects a registry that the capabilities ignore, because they hard-code `packages.Builtin()`.
- **Evidence:** `runner := ansiblerunner.New(operationPlaybook(), deps.Stager).AdmitReadOnly(readOnlyOperations()...)` appears at both sites.
- **Impact:** Wiring drift: a test can compile against one registry and plan against another.
- **Fix:** Build the runner, the registry and the session factory once in `assembleServices` and pass them down. (effort S)
- **Checked:** ARC-05 covers the preflight logic itself, not the duplicate construction.

##### G05-09

**Low · Clearness · confidence high**

- **Where:** cmd/bootwright/wiring_machine.go:195, :202, :229; wiring_preflight.go:142; wiring_containercluster.go:78; acquisitions.go:235
- **What:** String literals are used where typed constants exist: the kind `"Machine"` (while :317 of wiring_machine.go uses `api.Machine`), the verb `"apply"`, and the tool kind `"openshift-clients"`.
- **Evidence:** `r.reconciler.Evidence(ctx, contextName, "Machine", name)`; `result.Lifecycle.Verb == "apply"`.
- **Impact:** Renaming a kind or verb compiles cleanly but breaks these sites.
- **Fix:** Use `string(api.Machine)`, `reconciliation.Apply`, and one exported tool-kind constant. (effort S)
- **Checked:** I grepped for exported constants; `api.Machine` and `reconciliation.Apply` exist.

#### Patterns
- **The composition root collects domain policy.** Its two most complex functions are firewall policy: `controllerSockets` (CC 25) and `serviceHostPorts` (CC 20), from gometrics. Its domain tests live in main: acquisitions_test.go, wiring_firewall_test.go and wiring_host_ports.
- **Per-kind managed-service facts are restated.** The DNS tcp+udp and NTP udp choice appears at wiring_firewall.go:36-41 and again at wiring_service_firewall.go:74-80. The service image lists appear at acquisitions.go:136-139 and billofmaterials.go:58-66. ARC-79 and ARC-80 already record these.
- **Errors fall back to defaults.** `err == nil` guards and `_ =` drop failures at wiring_controller.go:104, wiring_workspace.go:33-35 (`AddsTo` returns "") and wiring_readiness.go:39-44. The last one hides one store's error when the other store reports gaps, and its test pins that as deliberate.
- **Policy trade-off: a runtime file is generated by test code.** execution-contracts.json, which the binary consumes to prove recovery, is written by contract_versions_test.go `-update`. The digest recipes are restated in contract_identity_test.go:108. This is a strong guard, but the generator is invisible to anyone reading production code.

#### Strengths
- `privilegeBoundary` takes every process fact as an injected function. Its refusals keep their contract status (255 for a session refusal), and session_refusal_test and secret_stdin_boundary_test drive it.
- Signal handling has a single subscription, `privilege.Begin`. Second-signal escalation is gated, and real-process tests synchronise through a handshake rather than sleeps (interrupts_linux_amd64_test.go:286-400). A test holds the composition root to subscribing to no signal of its own.
- The stdin and terminal code is careful: cancellation by polling, a nonblocking read with the descriptor flags restored, termios restored on every return, and a SIGTTIN handoff from the background.
- completion_paths.go bounds what it reads and withholds names that are unsafe as shell words. ansible.ExtraVariables marks every string unsafe and refuses reserved keys.
- The BOM fails closed on an unlicensed module or wheel and on an image not pinned by digest.

#### Coverage notes
- wiring_account.go:24-130: every `invokingAccount` method (Read, Write, Clear, home, uid, FileIdentity, copyAccount, comparison) is at 0%. This is the production binding from the privilege boundary to the user's files, and tests always substitute their own Selection.
- wiring_machine.go:153 and :165: `machineOwnership.Ownership` and `Completed` are at 0%. This is a named-exception translation with a real rule (any block of the implementation that is not Done answers false).
- wiring_discovery.go:28: `machineDiscovery.Discover` is at 0% (16 statements), including its gate on the receipt state.
- wiring_reconciliation.go:488 and :505: `PackageTree` and `physicalCephEraseProof` are at 0%. They translate the proofs that gate Ceph physical retirement, which is close to data-loss territory.
- wiring_machine.go:260: `ControllerHost` is at 0%, and it builds `hostlinux.New()` inline instead of having it injected.
- run.go:72-73: the elevated-child branch reads `privilege.SupervisedChild()` and `os.Environ()` directly, so `boundaryUnderTest` cannot drive it.
- wiring_managedos.go:62: `mediaRoute` is at 25%; its fail-closed branch for a route it cannot honor is unexercised.

#### Already recorded
- tracked: B348 (the account is resolved repeatedly, getent runs three times per resolution, wiring_account.go)
- tracked: B1339 (`go test -race` on cmd/bootwright exceeds the timeout); B671 (plan block-set test)
- audit 003 ARC-02 (clocks not injected at the machineaccess and prerequisites sites), ARC-03 (invokerfs wrappers openerFiles, inputFiles, mediaFiles, materialFiles, sshFiles), ARC-05 (preflight decision logic in wiring_preflight.go), ARC-06 (kubevirt.PinnedIdentity outside the named exception)
- audit 004 ARC-26 (controllerSockets protocols, hostPortChecks remedies, installProofs, kubevirtHostRequirements and kubevirtAuthoredProviders), ARC-79 (per-kind transports), ARC-80 (managed-service definition list restated), ARC-29 (cephDependencyProofs)

<a id="lane-g06"></a>

### Lane G06: Controller: prerequisites, host packages, host inspection, firewall (Go)
Scope read: I read every production file fully: internal/controller/*.go, prerequisites/*.go, hostdnf/*.go, hostlinux/*.go, hostowned/*.go, firewall/*.go, firewalllocal/*.go. / I skimmed the tests: hostdnf/packages_linux_amd64_test.go (fakes and runner), the test lists of hostowned and firewalllocal, and the assertions in prerequisites/fips_test.go, rebind_test.go and service_test.go that bear on settleHost. Outside the lane I opened cmd/bootwright/wiring_machine.go:255-268, reconciliation/lifecycle/planning.go:365-379, clients/requests.go:55-145 and the import edges. / I did not read the remaining _test.go bodies (the coverage data stood in for them).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed and context-aware throughout; a few evidence reads fail open or lose their reason |
| Security | 4 | Qualified absolute executables, `--` before arguments, minimal environment, bounded output, held-fd path walk; small fail-open gaps |
| Efficiency | 3 | Firewall inspection repeats the global firewalld queries for every socket inside one 45 s budget |
| Clearness | 3 | inspectZone (complexity 38) and Inspect (33) are dense; status values are bare strings |
| DDD | 2 | Host-reservation policy lives in Controller setup; Controller selection reads the native fields of six other contexts |
| Non-duplication | 2 | The seven-closure flag set exists in four structs and about six switches; three local process runners |
| Right language | 5 | Go owns dnf, rpm, virsh, systemctl and podman; no logic is embedded as strings in another language |
| Test coverage | 3 | Adapters are hermetic and assert argv; the host-mismatch refusal, the FIPS branch and two real runners have no test |

#### Findings
##### G06-01

**Medium · DDD · confidence high**

- **Where:** internal/controller/prerequisites/reservations.go:1-462, prerequisites/storage.go:161-176
- **What:** The cross-context host-reservation model (HostReservation, Conflicts, UseConflicts, ConflictDiagnostic, the release guards) lives in the Controller setup package. Its own doc says "Workspace stores the keys and compares them through Conflicts ... Setup reserves nothing."
- **Evidence:** Seven of the 23 importers use this package only for HostReservation, MaxReservationKeys and Conflicts: artifactserver, managedservice, machine/hardware, managedos/installation, substrate/baremetal, kubevirt and libvirt. architecture.md:594-595 gives "Infrastructure services own local service effects, their host reservations".
- **Impact:** Substrate, Machine and Managed OS all compile against Controller setup vocabulary. Every reservation change touches the setup package, which inflates its fan-in.
- **Fix:** Move the reservation value and its conflict rules into a small neutral package that Workspace and the claimants share (or into infrastructureservices), and leave prerequisites to setup. (effort M)
- **Checked:** There is no named exception for this. B464 notes only a duplicated key constant.

##### G06-02

**Medium · Efficiency · confidence medium**

- **Where:** internal/controller/firewall/check.go:40,47-57, firewalllocal/inspect.go:25,80-103
- **What:** CheckSockets inspects each socket serially. Each Inspect reruns the host-wide queries: `--state`, `--query-panic`, `--get-active-zones`, `--get-active-policies`, `--info-policy` once per policy, and both `--direct` reads. It then runs the zone queries. Every socket draws on one shared 45 s deadline.
- **Evidence:** `ctx, cancel := context.WithTimeout(ctx, 45*time.Second)`. Per socket, Inspect starts about 10 or more `firewall-cmd` processes (firewall-cmd is a Python D-Bus client). Once the deadline passes, a socket gets `Reason: "read-only firewall inspection is unavailable"`.
- **Impact:** With many listeners, the later sockets come back `unknown` from budget exhaustion, and AdmitRequired then refuses preflight with a misleading reason.
- **Fix:** Read the host-wide state, policies and direct rules once per CheckSockets. Memoise the zone and service reads by name. Report budget exhaustion in its own words. (effort M)
- **Checked:** There is no cache anywhere. The spec (controller.md:1246-1262) requires the semantics, not a query per socket. I did not measure timing.

##### G06-03

**Medium · Non-duplication · confidence high**

- **Where:** controller/selection.go:20-32,36-42,51,134-139; prerequisites/closure.go (StageNative, StageNativeOf, StageRequirements, closureTable); prerequisites/effects.go:20-33 (NativeRequirements); prerequisites/hostpackages.go:60-84; prerequisites/inspect.go:127-147,596-616; controller/clients/requests.go:55-62,120-123,145
- **What:** The same seven native closures (container runtime, libvirt client, hypervisor, installer media, image mirror, nmstate, iputils) appear as four parallel bool structs, with field-by-field converters and three switch or if ladders.
- **Evidence:** `closureRequirements(id string) NativeRequirements { return NativeRequirements{ContainerRuntime: id == "container-runtime", ...` duplicates closureTable. The `declare` ladder repeats `if i.native.X { checks = append(...) }` seven times instead of ranging over closureTable.
- **Impact:** Adding a closure (as 51bc3c447 did for image-mirror) means editing roughly ten places, and a missed one silently drops a check or a package.
- **Fix:** Use one closure table holding id, label, package list and selector, and one closure-set type. Derive the rest from it. (effort M)
- **Checked:** This is not the deliberate repetition of consumer-owned ports. These are value types.

##### G06-04

**Medium · Non-duplication / Security · confidence high**

- **Where:** hostdnf/packages_linux_amd64.go:455-520, hostowned/probe_linux_amd64.go:29-69, firewalllocal/native.go:16-43; hostdnf:431-442 against hostowned/probe.go:154-166
- **What:** There are three hand-written root-run process runners whose supervision differs, plus three bounded writers and two output sanitisers.
- **Evidence:** hostdnf sets `Setpgid`, SIGTERM to the group and `WaitDelay = 30s`. hostowned sets `Setpgid`, SIGKILL and `HOME=/root`. firewalllocal sets no process group, uses the default kill, `PATH=/usr/bin:/bin` and `WaitDelay = time.Second`.
- **Impact:** The environment, the kill semantics and the bounds drift by package. Python, by contrast, has one runner by policy.
- **Fix:** Extract one runner into nativeexec beside Qualify (absolute path, fixed environment, process-group kill, bounded stdout and stderr, exit classification) and make the per-tool settings explicit parameters. (effort M)
- **Checked:** The executable qualifier was already unified (nativeexec.Qualify). The runners were not.

##### G06-05

**Medium · DDD / Non-duplication · confidence medium**

- **Where:** controller/selection.go:173-190, 480-561; controller/tools.go:54-106
- **What:** The root `controller` package imports addons, containercluster, infrastructureservices, machine and substrate. It reads their native fields, for example `profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint")`, and it judges mirror placement policy. managedMirrorShapes and managedImageMirrorShapes are near-identical: compare lines 486-492 with 534-540, 506-510 with 549-553, and 511-516 with 554-559.
- **Evidence:** The dependency runs both ways. controller imports containercluster and addons, while containercluster/agentinstall, addons/instance and the addons cluster-object driver import controller (for InstalledTool and similar). The Bounded contexts table says Controller "Does not own managed-machine provisioning".
- **Impact:** The installer, mirror and cluster rules of other contexts are re-derived inside Controller, so a change to their schema breaks Controller selection.
- **Fix:** Have Managed OS and Infrastructure services publish facts such as "builds installer media on Machine X" and "mirror placement shapes", as containercluster.FactsOf already does. Fold the two mirror-shape functions into one. (effort M)
- **Checked:** Where Controller uses published facts (FactsOf, ProviderTools, ImageMirrorHost), it does so correctly. The leak is the remaining raw field reads.

##### G06-06

**Low · DDD / Quality · confidence high**

- **Where:** prerequisites/contracts.go:20-27, service.go:53, inspect.go:60; hostlinux/inspector_linux_amd64.go:98-113,199-205
- **What:** Release is mandatory (available() refuses without it) but is reached through a type assertion. Meanwhile HostInspector.Platform is never called in production.
- **Evidence:** `release, err := s.host.(ReleaseInspector).Release(ctx)`. `grep '\.Platform(ctx'` finds no production call. Platform and parseOSRelease duplicate Release and osReleaseValues, and the test doubles (rebind_test.go:33, service_test.go:103) must still implement Platform.
- **Impact:** There is a dead port method, duplicated parsing, and a panic-shaped call that is safe only because of a guard in another file.
- **Fix:** Put Release into HostInspector, drop Platform, and derive Platform from HostRelease. (effort S)
- **Checked:** available() does prevent the panic today.

##### G06-07

**Low · Security · confidence medium**

- **Where:** hostlinux/addresses_linux_amd64.go:14-26, hostlinux/sshkeys_linux_amd64.go:15-27
- **What:** HardwareAddresses skips unreadable entries and reads through os.ReadDir and os.ReadFile, outside the package's held-fd discipline. SSHHostKeys returns `nil, ctx.Err()` when the root cannot be opened, and skips unreadable keys.
- **Evidence:** `if err != nil || len(data) > 64 { continue }`, and `fs, err := i.view.open(ctx); if err != nil { return nil, ctx.Err() }`.
- **Impact:** Both feed self-protection guards: the controller-host power identity at wiring_machine.go:266 and the controller key qualifiers at planning.go:369. Missing evidence silently weakens a "this is the controller" check instead of refusing.
- **Fix:** Return an inspection failure when the directory or the key root cannot be read, and decide per caller whether an absent key file is acceptable. (effort S)
- **Checked:** The package doc claims "fixed local Linux evidence". No test covers an unreadable entry.

##### G06-08

**Low · Clearness · confidence high**

- **Where:** hostlinux/inspector_linux_amd64.go:55; prerequisites/inspect.go:225-226
- **What:** LoaderPreload returns `(true, nil)` on any open error other than ENOENT. The warning then states the file "is not empty".
- **Evidence:** `return err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size != 0, nil`
- **Impact:** The warning misreports an uninspectable /etc/ld.so.preload as a populated one.
- **Fix:** Return a distinct state, or an error, for a file that cannot be inspected. (effort S)
- **Checked:** The check is only a warning, so it does not decide any outcome.

##### G06-09

**Low · Quality · confidence high**

- **Where:** firewalllocal/native.go:28,39
- **What:** Every failure collapses into fixed text. The exit code, a polkit denial, an output overflow and a timeout are all lost before Inspect maps them to "firewalld is unavailable".
- **Evidence:** `return nil, errors.New("read-only runtime firewall query did not complete")`
- **Impact:** An operator cannot tell a missing privilege from a stopped firewalld.
- **Fix:** Classify the cause (not qualified, timeout, overflow, exit status with bounded sanitised stderr) into Reason. (effort S)
- **Checked:** Redaction rules allow bounded native text in a reason (hostowned already does this).

##### G06-10

**Low · Clearness · confidence high**

- **Where:** hostlinux/identity_linux_amd64.go:189-192
- **What:** canonicalUUID validates a UUID by building a whole InstalledHostIdentity around placeholder identifiers.
- **Evidence:** `controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, "1234567890abcdef1234567890abcdef", "12345678-...", value)`
- **Impact:** It is obscure, and it couples the check to unrelated validation rules.
- **Fix:** Export a `ValidInstalledUUID` from controller/identity.go. (effort S)
- **Checked:** No other helper exists for this.

##### G06-11

**Low · Efficiency · confidence medium**

- **Where:** hostdnf/packages_linux_amd64.go:126-132; hostowned/probe.go:193-204
- **What:** `queries` starts one goroutine and one rpm process per requirement with no limit, up to 64 at once. The libvirt probe runs one serial `virsh dumpxml`, each with a 1-minute timeout, for every object without the Bootwright prefix, up to 4096 per kind.
- **Impact:** The rpm calls make bursty load. The virsh probe makes uninstall slow on hosts with many foreign libvirt objects.
- **Fix:** Bound the rpm concurrency (for example to 8). Consider running the dumpxml calls with bounded parallelism. (effort S)
- **Checked:** Neither is a correctness issue.

##### G06-12

**Low · Quality · confidence high**

- **Where:** hostowned/probe.go:162-164, firewalllocal/inspect.go:134-146
- **What:** `printable` truncates at `text[:512]`, which can split a multibyte rune (hostdnf's lastError uses ToValidUTF8 for the same job). inspectZone re-tests `hasTarget`, `hasPorts` and `hasServices` after the loop above has already guaranteed they are present, so those conditions are dead.
- **Fix:** Truncate on a rune boundary, and drop the redundant conditions. (effort S)

#### Patterns
- Optional capabilities are discovered by interface type assertion instead of being declared ports: service.go:53, uninstall.go:94-96, rebind.go:29, service.go:202, rebuild.go:115. A wiring gap only appears at run time, as "... is not configured".
- Check and observation status values are bare strings ("ready", "not-ready", "unverified", "open", "closed", "unknown"), and Check is built with positional literals (inspect.go:112-150, service.go:271-280, firewall/check.go:55-66).
- hostdnf has two failure constructor families, failure/unqualified and removalFailure/unqualifiedFor (packages:444-453, removal:176-183). They differ in error type (ScopedFailure against Failure) and in remediation casing ("Restore..." against "restore...").

#### Strengths
- The hostdnf adapter is strong: qualified absolute rpm and dnf, `--` before arguments, `LC_ALL=C` with a fixed PATH, separate deadlines for preview and install, bounded and sanitised output, and fake-runner tests that assert argv and environment.
- The hostlinux held-fd path walk (files_linux_amd64.go:52-118) admits only root-owned symlinks, refuses procfs magic links, rechecks stability with fstat, and follows reads through a context-aware reader.
- Firewall inspection fails closed: unknown keys, duplicates, rich rules, sources and direct rules all yield `unknown`, never `open`.
- Setup and uninstall re-inspect under the lock and refuse a host that moved (samePlan, same), and record pending intent before any effect.

#### Coverage notes
- prerequisites/inspect.go:310-313: the refusal "stored controller state belongs to a different installed host" (a wrong-target guard) is not asserted by any test in the repository (grep finds only the source line). The FIPS-required branch at 316-322 is also unrun (settleHost is 44%).
- hostowned/probe_linux_amd64.go:47 execute (0%): the process-group SIGKILL, the 16 MiB overflow and exit classification are untested, although hostdnf's execute shows a real-process test is feasible.
- firewalllocal/native.go:25 command and :45 localInterfaces (0%): the overflow-as-error path and the qualifiedNegative integration with real exit codes are untested.
- firewalllocal/inspect.go:107 inspectZone (75%): the target, protocol and forward-port refusals are partly unrun. These are the branches that keep `open` from being reported falsely.
- No test covers CheckSockets with enough sockets to exhaust the 45 s budget (see [G06-02](#g06-02)).
- hostlinux/files_linux_amd64.go:52 openPath (77%) and identity_linux_amd64.go:131 filesystemUUID (71%): the symlink-loop and duplicate-UUID refusals are partly unrun.

#### Already recorded
- audit 003 SEC-01 (firewall-cmd unqualified): now fixed, since native.go:26 calls nativeexec.Qualify. I did not re-report it.
- audit 003 CTH-10: the post-install presence check is duplicated between hostdnf.Install (packages:256-259) and setup.go:283-289.
- audit 003 CTH-07: recordReceipt's repoquery runs on setups that install nothing.
- B560: hostdnf preview fixtures are agent-written, not captured from real hosts.
- B464: the context store keeps a private reservation-key constant beside prerequisites' exported one (related to [G06-01](#g06-01)).

<a id="lane-g07"></a>

### Lane G07: Controller: privilege, Python runtime, bundles, staging, local Ansible, clients (Go)
Scope read: I read every production file in privilege/, bundlelocal/, venvlocal/ (except unsupported.go), runtimelock/, stagelocal/ (except staging_unsupported.go), ansiblelocal/ (except deadline.go and runner_unsupported.go) and clients/ in full. I also read terminal_hint_linux_amd64_test.go and the first 300 lines of process_linux_amd64_test.go. Skimmed: the test names in sudo_test.go, elevation_test.go and the bundlelocal tests, input_logging_test.go, tools_test.go:381-412, destinations_docs_test.go:140-156. Cross-checked: ansiblerunner/jobs_linux_amd64.go:560-680, adapterprotocol/record.go:160-260, canonicaljson.go, nativeexec/executable.go, acquisition AnchoredDial and GuardAcquisitionTransport. Not read: the other test files and the small unsupported stubs, which only hold refusals.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful fail-closed code; one subprocess runner has no WaitDelay and drops stderr, and some code is dead |
| Security | 4 | Strong sudo, environment and trust boundaries; audit SCR-05 is still open and the executor's argv guard is partial |
| Efficiency | 4 | Small costs: serial publisher fetches, and the HTTP client and CA pool rebuilt for every request |
| Clearness | 3 | Three state machines with complexity 40, 51 and 33 thread boolean flags through long switches |
| DDD | 4 | Ports are consumer-owned and adapters are thin; clients.ToolRequest mirrors controller.ToolRequest field by field |
| Non-duplication | 3 | A copied safe tree remover, four or more root-ownership walks, and a third strict-JSON decoder |
| Right language | 4 | Embedded Python probes are small and the spec owns them; no misplaced logic |
| Test coverage | 3 | The real entry points (Resolve, ProcessExecutor.Run, fetchToolMetadata, qualifiedTrustPEM) run in no test; one test breaks under `-cover` |

#### Findings
##### G07-01

**Medium · Quality · confidence high**

- **Where:** internal/controller/venvlocal/build_linux_amd64.go:277-294
- **What:** `runCommand` runs `python -m venv`, pip, `compileall -j0` and the probes through `exec.CommandContext`. It sets no `WaitDelay` and no process group, and it captures stderr into a buffer that nothing ever reads.
- **Evidence:** `process := exec.CommandContext(ctx, request.Path, request.Arguments...)` … `var stdout, stderr bounded` … `return nil, err`
- **Impact:** On a timeout, a worker that `compileall -j0` or pip started can keep the pipes open, so `Wait` blocks with no bound. Every failure then reaches the operator as a fixed sentence such as "pip could not install the locked wheels from the cache", with no cause.
- **Fix:** Set `WaitDelay`, as nss_linux_amd64.go:99 and privilege's runProcess already do. Fold a bounded tail of stderr into the failure. (S)
- **Checked:** Every other subprocess runner in this lane sets `WaitDelay`. The measured data shows `runCommand` is never run by any test.

##### G07-02

**Medium · Test coverage · confidence high**

- **Where:** internal/controller/privilege/terminal_hint_linux_amd64_test.go:80-84, process_linux_amd64_test.go:34-36
- **What:** The test re-executes the test binary twice, with `Env` replaced by `PATH` and `COLORTERM`. It then requires `CombinedOutput` to equal exactly `"hint absent\n"`. Under `-cover`, both re-executed binaries print the "GOCOVERDIR not set" warning on stderr, so the test fails.
- **Evidence:** `output, err := child.CombinedOutput()` / `if err != nil || string(output) != "hint absent\n"`
- **Impact:** A coverage run of the package is always red. The relay tests avoid this because they compare stdout only.
- **Fix:** Compare `child.Output()` (stdout only), or pass `GOCOVERDIR` through when it is set. (S)
- **Checked:** Confirmed in the file. The test is described at delivered.md:10111 and no backlog item tracks the problem.

##### G07-03

**Medium · Clearness · confidence high**

- **Where:** internal/controller/privilege/sudo.go:99-193; internal/controller/ansiblelocal/runner_linux_amd64.go:214-276; internal/controller/clients/capability.go:337-422
- **What:** Each of these functions packs several phases into one body. `Supervisor.Run` (complexity 40) does six things: wraps the writers, probes the policy, validates, decides the stdin-secret question, builds the argv, and runs the refresh goroutine and the child. `protocolRun.Judge` (complexity 51) threads `valid = valid && …` through five phases and performs side effects (`run.publish`, `run.report`) inside a "judge". `Capability.apply` (complexity 33) declares a tool-`missing` slice that shadows the host-package `missing` (line 371), and the later `len(missing)` checks at lines 386 and 405 mean the packages.
- **Evidence:** `validated = true` is set before `if err == nil && code == 0` (sudo.go:136-137); `missing := []controller.ToolRequest{}` (capability.go:371).
- **Impact:** This is the most security-relevant and protocol-relevant code in the lane, and every change to it means re-deriving the flag interactions by hand. One side effect: after a failed interactive `sudo -v`, `Validated` is still true, so the refusal drops its "run sudo -v" remedy.
- **Fix:** Extract `probePolicy`, `admitStdinSecret` and `childArguments` from Run; make one method per phase in Judge; rename the inner `missing`. (M)
- **Checked:** The behaviour is pinned by sudo_test.go and elevation_test.go, so extracting these pieces is low-risk.

##### G07-04

**Medium · Non-duplication · confidence high**

- **Where:** internal/controller/stagelocal/staging_linux_amd64.go:368-504; internal/reconciliation/ansiblerunner/jobs_linux_amd64.go:560-669
- **What:** `emptyStage`, `emptyStageChild`, `stageEntries`, `directory` and `file` are the same code as `emptyTree`, `emptyChild`, `entries`, `runDirectories.dir` and `runDirectories.file`, down to the sort trick and the error strings. The two packages also share the same lock and owner-marker claim protocol; the comment in staging says "exactly as an adapter job is claimed".
- **Evidence:** Both have `slices.SortStableFunc(names, func(a, b string) int { return slices.Index(last, a) - slices.Index(last, b) })` and `part, err := listing.Readdirnames(1024)`.
- **Impact:** This is a security-relevant traversal (no following links, no crossing filesystems, bounded entries). A fix to one copy will silently miss the other.
- **Fix:** Move a descriptor-relative "remove a verified private tree" helper into one shared internal package that both adapters import. (M)
- **Checked:** The measured data has this pair as both an exact clone (10 lines) and a shape clone (45 lines). This is not one of the named exceptions; those cover injecting the stager, not copying code.

##### G07-05

**Medium · Non-duplication · confidence high**

- **Where:** internal/nativeexec/executable.go:17-43; internal/controller/venvlocal/walk_linux_amd64.go:14-49; internal/controller/privilege/process_linux_amd64.go:90-111,142-156; internal/controller/bundlelocal/trust_linux_amd64.go:79-85
- **What:** The rule "root-owned, no group or other write, up to /" is implemented at least four times in this lane, and the copies disagree. `privilege.rootOwned` accepts a sticky world-writable directory, while `nativeexec.qualify` and `venvlocal.ownedDirectory` refuse one. venvlocal's comment points at a copy it does not call.
- **Evidence:** walk_linux_amd64.go:13 `as privilege.QualifiedSudo does for sudo`; process_linux_amd64.go:149 `... && !(info.IsDir() && info.Mode()&fs.ModeSticky != 0)`
- **Impact:** A newcomer cannot tell which variant a new boundary should use. Outside this lane, 11 more files match the same mode check.
- **Fix:** Give nativeexec one walker that takes options (trusted UID predicate, top directory, sticky policy) and call it from all four sites. (M)
- **Checked:** venvlocal's `trusted` and `top` fields are test seams, and an options-taking walker can keep them.

##### G07-06

**Low · Quality · confidence high**

- **Where:** internal/controller/bundlelocal/tools.go:61-68,196-232,326
- **What:** `Select` keeps the newest version among retained sources that share a prefix. But the prefix hashes the whole request, `Version` included, and `toolDefinition` refuses any `version != request.Version`. So every candidate has the same version, and `compareStableVersions`/`stableVersion` (21 statements) can only ever compare equal strings. `githubToolRelease.Prerelease` is decoded and never read, yet the error message says "unstable".
- **Evidence:** `data, _ := json.Marshal(request)` in toolSourcePrefix; `if !toolVersion(version) || version != request.Version {`
- **Impact:** The dead logic suggests a "newest release" selection that does not exist.
- **Fix:** Drop the comparison and select the single match; remove `Prerelease` and the word "unstable". (S)
- **Checked:** The measured data shows no test runs either function (0%). controller.ToolRequest at internal/controller/tools.go:18-25 includes Version.

##### G07-07

**Low · Security · confidence medium**

- **Where:** internal/controller/privilege/process_linux_amd64.go:252-272
- **What:** `ProcessExecutor.Run` verifies the re-execution path only after the first `--`. A sudo argv with no `--` is accepted whatever it contains, for example `-u #0 /bin/sh`.
- **Evidence:** `for i, arg := range args { if arg == "--" { … } }` with no refusal when there is no `--`.
- **Impact:** Today only the Supervisor builds argv, so this does not break the boundary. But the guard reads as a general rule ("never run anything but Bootwright as root") and does not enforce one. Its 14 statements run in no test.
- **Fix:** Admit exactly the three argv shapes the Supervisor uses (probe, validate, child) and refuse anything else. Inject `QualifiedSudo`/`ReexecutionPath` so the guard can be tested. (S)
- **Checked:** The Supervisor is the only caller (sudo.go:122,132,176,185).

##### G07-08

**Low · Efficiency · confidence high**

- **Where:** internal/controller/bundlelocal/tools_transport.go:44-48, acquisition.go:22-34, trust_linux_amd64.go:72-75, tools.go:128-138
- **What:** Every publisher request builds a new client: it re-reads and parses the CA bundle twice (once to check it, once to build the pool) and discards idle connections afterwards. `Resolve` also fetches each tool's metadata one after another, though the fetches are independent.
- **Evidence:** `client, closeIdle, err := acquisitionClient(...)` inside fetchToolMetadata; `for _, request := range requests { … c.resolve(bounded, request, egress)`
- **Impact:** About three TLS handshakes and two PEM parses per tool, run serially. This is minor, but it cuts against the project's "parallel by default" rule.
- **Fix:** Build one client in `Resolve` and pass it down; return the pool from `qualifiedTrustPEM`. Optionally resolve tools concurrently. (S)
- **Checked:** The 5-minute bound applies to the whole loop.

##### G07-09

**Low · Non-duplication · confidence high**

- **Where:** internal/controller/ansiblelocal/protocol.go:14-85
- **What:** `strictJSON` decodes three times (a case-folded uniqueness walk, a map pass, a closed struct pass). `adapterprotocol.Decode` has already walked the whole record, nested `preparation` and `evidence` included, with the same case-folded uniqueness check. `canonicaljson.DecodeClosed` already owns the closed decode.
- **Evidence:** protocol.go:58 `folded := strings.ToLower(name)` duplicates record.go:236.
- **Impact:** This is a third strict-JSON implementation; runtimelock/licenses.go:34-41 is a fourth, hand-rolled one.
- **Fix:** Use `canonicaljson.DecodeClosed` plus the member-presence check. (S)

#### Patterns
- Long boolean-flag state machines decide security outcomes: privilege/elevation.go:119 `conclude` (complexity 26), elevation.go:439 `startFilter.judge` (24), sudo.go:99, runner_linux_amd64.go:214.
- Parallel conversion code: clients/requests.go repeats the ToolRequest-to-ToolRequest field copy three times (lines 97, 131, 142), and bundlelocal/tools.go keeps the GitHub-repository rule twice (`resolveVersion` at 412-423 and `Contacts` at 520). Tests at tools_test.go:381 partly pin that pair.
- Real-system entry points are hard-wired (`os.OpenRoot("/")`, `/usr/bin/sudo`, `os.Getenv`), so the tested logic sits below an untested 0% shell: account_linux_amd64.go:17, trust_linux_amd64.go:30, process_linux_amd64.go:252.

#### Strengths
- The sudo boundary is thorough: a fixed environment, a vocabulary for assignments, a PDEATHSIG guard, the procfs re-execution proof, a policy probe before stdin secrets, and a single signal relay with escalation.
- File work is descriptor-relative with `os.Root`, refuses to cross devices or follow links, and bounds entry counts (stagelocal, bundlelocal trust).
- Wheel supply chain: the lock is canonical, hash-pinned, installed offline with `--require-hashes --only-binary`, checked for startup hooks and for unchecked-hash pyc files, and import-probed.
- The relay tests use real processes and check exit status, timing and output, not just that the code ran.

#### Coverage notes
- privilege/account_linux_amd64.go:17 `Resolve` and :70 `readAccountFile` run only under an opt-in root fixture, so TOCTOU-checked reads of /etc/passwd are unproven in CI.
- privilege/process_linux_amd64.go:252 `ProcessExecutor.Run` (the argv guard) runs in no test; see [G07-07](#g07-07).
- bundlelocal/tools_transport.go:36 `fetchToolMetadata`: the origin allowlist, HEAD size, status, Content-Encoding and read bound are never exercised, because tests inject the fetcher.
- bundlelocal/trust_linux_amd64.go:30 `qualifiedTrustPEM`: the ownership walk and the changed-while-read check are untested, because the root is hard-wired.
- bundlelocal/tools.go:82 `Present` is never run, and it is the postcondition check behind sealing a client area (capability.go:588).
- bundlelocal/mirror.go:36 `ProbeMirror` is 28% covered: the proxy and trust-anchor client path is untested.
- venvlocal/build_linux_amd64.go:277 `runCommand`: the timeout and overflow paths are untested; see [G07-01](#g07-01).

#### Already recorded
- Audit 003 SCR-05 (sudo.go:299-302: an unbalanced Defaults list is skipped, which fails open on `log_input`). It is still present at d8d5672ef, input_logging_test.go:43 still expects `""`, and I found no B item tracking it.
- Audit 003 SEC-06 (the venvlocal wheel-client redirect check, plus the conflict between the spec and the code on redirects): B1133 cites it, so not re-reported.

<a id="lane-g08"></a>

### Lane G08: Workspace contextfs: repository, files, publication, media, operations (Go)
Scope read: read in full: repository_linux_amd64.go, repository_unsupported.go, files_linux_amd64.go, store.go, registry.go, records.go, paths_linux_amd64.go, publication_linux_amd64.go, revisions_linux_amd64.go, snapshots_linux_amd64.go, incarnation_linux_amd64.go, initialization_linux_amd64.go, initialization_stages_linux_amd64.go, lifecycle_lock_linux_amd64.go, checkpoints.go, collection_linux_amd64.go, root_inspection_linux_amd64.go, deletion_linux_amd64.go, trust_linux_amd64.go, media_linux_amd64.go, operations_linux_amd64.go, reconciliation_linux_amd64.go / skimmed: checkpoint_harness (the scenario list, the trace shapes at 867-1038, the update and media scenarios), media_linux_amd64_test.go 640-718, the test-function lists of the durability, store_failures, publication, revisions and operations tests, specs/contexts.md 360-559 and 752-831, media/service.go 380-400 / not read: the secrets, logs, runs, controller_* and setup* files (another lane), and most other _test.go bodies (time).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Careful stage, prove and rename discipline; one failure path leaks a partial revision, plus small dead or lossy branches |
| Security | 4 | All I/O goes through openat2 BENEATH, NO_SYMLINKS and NO_XDEV with identity re-proof; one FIFO-blocking open path |
| Efficiency | 3 | Each verify walks again from `/`, tree removal syncs once per entry, and every read decodes every manifest |
| Clearness | 3 | Complexity of 41 or 30 in Commit, publishInitialRegistry and media Publish; misleading checkpoint names |
| DDD | 3 | One `Store` type implements six ports over a shared kernel; media and trust are separate stores in the same type |
| Non-duplication | 3 | The read prologue is copied five times and has already diverged; there are three 16-try candidate loops |
| Right language | 5 | Go syscalls used deliberately; no embedded language |
| Test coverage | 4 | The checkpoint harness pins every effect's trace; the non-pristine failure and lock-guard branches are thin |

#### Findings
##### G08-01

**Medium · Quality · confidence high**

- **Where:** internal/workspace/contextfs/snapshots_linux_amd64.go:136-162
- **What:** `Publish` creates the revision directory itself, not a stage. A failure after any blob is written returns without removing the directory or the blobs already written.
- **Evidence:** `if err = t.store.writeExclusive(ctx, fresh, blobName(index), ...); err != nil { break }` … `fresh.file.Close(); if err != nil { return "", safeError(err) }`
- **Impact:** Only pristine contexts collect unselected revisions (revisions_linux_amd64.go:18), so in any applied context every failed update leaves its bytes forever. ENOSPC is the likeliest cause, so the leak makes the capacity shortage worse, and the dead revisions count toward `maxRevisions`.
- **Fix:** On any error after `newDirectory`, remove `fresh` with the existing `walkContextTree(removeContextTree)` and `unlinkVerified` path under the held lease, then sync `revisions` (effort S).
- **Checked:** The spec says "A failed write removes its file unless retry relies on it" (contexts.md:495-496), and no retry relies on these files. The harness update scenario uses a pristine fixture (checkpoint_harness:1287-1311), so its retry hides the leak. `TestInputCleanupPreservesProtectedRevisions` confirms protected contexts keep all revisions.

##### G08-02

**Medium · Efficiency · confidence high**

- **Where:** internal/workspace/contextfs/deletion_linux_amd64.go:211-225; files_linux_amd64.go:227-259
- **What:** Tree removal runs `unlinkVerified` (which verifies the parent) and then `syncDirectory` (which verifies again and fsyncs) for every entry. `directory.verify` recurses through every ancestor and ends in `walkAbsolute` from `/`, with an Fstatfs at each component.
- **Evidence:** `if err := unlinkVerified(dir, name, identity, ...); ...; if err := s.syncDirectory(ctx, dir); err != nil` sits inside the per-name loop. `verify` contains `if err = d.parent.verify(); err != nil` and `reopened, err = walkAbsolute(d.path, nil)`.
- **Impact:** Deleting a context, or collecting a revision with thousands of entries six levels deep, costs about 80 syscalls and one directory fsync per file, all under the exclusive root lock. This is also a likely contributor to the package's 113-second test time.
- **Fix:** Verify once before and once after each directory's batch, unlink inside the batch, and fsync the directory once after its loop. Deletion is already restartable because Deleting is recorded first (effort M).
- **Checked:** No spec clause requires an fsync per unlink: contexts.md:510-513 asks that removal be durable "when it returns". The harness `init-purge` shape shows 66 `sync-directory` checkpoints for a tiny context.

##### G08-03

**Medium · Efficiency · confidence medium**

- **Where:** internal/workspace/contextfs/repository_linux_amd64.go:523-533, 1429-1434, 1302-1306, 1030
- **What:** Every read and every transaction fully reads and decodes every ready context's manifest (the second loop of `verifyMappingsExcept`). `Commit` runs that whole verification again, then `save` → `replaceRegistry`'s `prove` reads the registry again and `registryExpectation` reads it twice more.
- **Evidence:** `_, _, close, err := openManifestBounded(ctx, root, record, sizes[index])` runs for every ready record. `Commit` then calls `verifyMappings(ctx, t.root, registry)` and `t.save` (`readRegistry` plus two `registryExpectation` calls).
- **Impact:** Each command costs O(number of contexts × manifest bytes), up to the 32 MiB aggregate, and a commit pays that more than once while holding the exclusive lock.
- **Fix:** Have the mapping verification keep each manifest's identity and size, and let `Commit` re-prove only the records that changed plus the registry identity (effort M). Label: policy trade-off, because the spec requires verifying every ready mapping on every read (contexts.md:418-420).
- **Checked:** The spec requires the mapping check but not two full decodes per commit.

##### G08-04

**Medium · DDD / Clearness · confidence medium**

- **Where:** internal/workspace/contextfs/repository_unsupported.go:26-114; media_linux_amd64.go:23-33; store.go:43-54
- **What:** One `Store` implements the contexts Repository, lifecycle (Read, Run, Mutate), media, SSH trust, secrets, logs, controller and setup, about 14,700 lines and 25 port methods. The parts truly shared are a handle-safe filesystem kernel (`files_`, `publication_`, `collection_` and the deletion walk, about 2,300 lines) and the root lock. Media shares only that lock and the controller reservations.
- **Evidence:** The media comment reads "Media is shared by every context, so it is coordinated by the root lock alone and never by a context lease". The unsupported stubs cover six different ports.
- **Impact:** Every change in any of these areas pulls in the whole package and its 113-second test suite, and the non-duplication drift in [G08-05](#g08-05) follows from that.
- **Fix:** Move the kernel (`directory`, openWithin, publishStage, collectStages, walkContextTree, the locks) into an internal sub-package, and make the media and trust stores separate types over it (effort L).
- **Checked:** The architecture fitness tests allow `_linux_amd64` adapters, and no named exception requires a single package.

##### G08-05

**Medium · Non-duplication · confidence high**

- **Where:** internal/workspace/contextfs/reconciliation_linux_amd64.go:188-230; trust_linux_amd64.go:62-104; repository_linux_amd64.go:107-125, 750-772; media_linux_amd64.go:67-97
- **What:** The prologue that opens the root, locks it, reads the registry, verifies the mappings, opens the context directory and checks its recorded inode is copied five times. A helper `openContext` (repository:670) exists, but only secrets uses it.
- **Evidence:** The clone data pairs reconciliation:197-212 with trust:75-90 and reconciliation:218-229 with trust:92-103. `withMedia` is the copy that drifted and skips `verifyMappings`, which audit 003 recorded as CTX-05.
- **Impact:** A guard added to one read path is silently missing from the others, as has already happened once.
- **Fix:** Add one `withRead(ctx, lock, scope, fn)` helper and route these entry points, plus `openContext`, through it (effort M).
- **Checked:** These are not consumer-owned ports, so the duplication is not a deliberate repeat.

##### G08-06

**Low · Clearness · confidence high**

- **Where:** internal/workspace/contextfs/repository_linux_amd64.go:1364, 1384
- **What:** `if next != prior` at line 1384 is always true, because line 1364 already continues the loop on equality. It adds a nesting level to the package's most complex function (complexity 41).
- **Evidence:** `if next == prior { continue }` … `if next != prior {`
- **Impact:** Readers look for a case that cannot happen.
- **Fix:** Remove the condition, and move the block that completes initialization into its own helper (effort S).
- **Checked:** Neither `next` nor `prior` changes between the two lines.

##### G08-07

**Low · Clearness · confidence high**

- **Where:** internal/workspace/contextfs/files_linux_amd64.go:855-858
- **What:** `writeExclusiveAtomic` publishes media records, operation records and trust records, but reports the checkpoints `checkpointBeforeSecretImmutableRename` and `checkpointAfterSecretImmutableRename`.
- **Evidence:** `}, checkpointBeforeSecretImmutableRename, checkpointAfterSecretImmutableRename)`; the harness `media-add` shape counts `checkpointBeforeSecretImmutableRename: 1` (checkpoint_harness:976).
- **Impact:** Kill-point ledgers and failure traces name the secret area for non-secret effects.
- **Fix:** Name the pair `before-immutable-rename` and `after-immutable-rename`, or pass the pair in from each caller (effort S).
- **Checked:** The checkpoint catalog test only checks that values are catalogued, not what they mean.

##### G08-08

**Low · Security · confidence medium**

- **Where:** internal/workspace/contextfs/operations_linux_amd64.go:724, 746, 749
- **What:** The operation log is opened `O_WRONLY|O_APPEND` without `O_NONBLOCK`, and the type check comes only after the open. Every other open of a name that could be planted uses `O_NONBLOCK`, and media gives the reason ("a FIFO planted at its name cannot stall the open while the lock is held", media:323).
- **Evidence:** `file, err := openRelative(parent, name, syscall.O_WRONLY|syscall.O_APPEND, 0)`
- **Impact:** A FIFO at a log name blocks `Append` indefinitely while the context lease is held. Planting one needs root, so the risk is low.
- **Fix:** Add `O_NONBLOCK` to both opens and clear it with fcntl once the stat proves a regular file (effort S).
- **Checked:** `readBoundedIdentity` (files:650), `Hold` and `stageHeld` all use `O_NONBLOCK`.

##### G08-09

**Low · Quality · confidence medium**

- **Where:** internal/workspace/contextfs/operations_linux_amd64.go:703-708
- **What:** `Append` turns the write and sync errors into plain state refusals and drops the errno. Other writes go through `storeFailure`, which names capacity (files:811, 819).
- **Evidence:** `return state("private operation log could not be written")`
- **Impact:** An ENOSPC or EDQUOT on a log append gets the wrong code and the wrong remedy (contexts.md:496-500 requires capacity to be named).
- **Fix:** Use `storeFailure(message, err)` (effort S).
- **Checked:** No backlog item or audit 003 finding mentions it.

##### G08-10

**Low · Quality · confidence medium**

- **Where:** internal/workspace/contextfs/media_linux_amd64.go:1045-1055, 966
- **What:** A replacement removes the old image and record, then checks for cancellation (the `before-media-rename` checkpoint), then renames. A cancellation at that point makes `Close` remove the unpublished stage as well.
- **Evidence:** `if err := a.remove(ctx, name)` … `if err := a.store.checkpoint(ctx, checkpointBeforeMediaRename); err != nil { return err }`. The service defers `stage.Close()` and retains a stage only on `ErrBusy` (media/service.go:105, 395).
- **Impact:** A Ctrl-C at that instant loses both the old image and the newly downloaded one. The window is narrow.
- **Fix:** Check cancellation before the removal and run remove and rename with no cancellation point between them, or retain the stage on any failure after the removal (effort S).
- **Checked:** The spec mandates removing before a rename that replaces nothing (contexts.md:770-774), but it does not require a cancellation point in between.

#### Patterns
- **Verification is repeated many times per operation.** `readBoundedIdentity` ends with `parent.verify()` (files:698), `heldNames` verifies twice (285, 314), and `unlinkVerified` and `syncDirectory` each verify again. The safety property is correct, but nothing amortizes its cost (see [G08-02](#g08-02)).
- **Record limits are chosen by type switches in `records.go`, and the two sides disagree.** decodeRecord at records.go:53-60 uses depth 16 and 32 fields for `controllerStateRecord`, while encodeRecord at records.go:144-148 allows depth 20 for the same record. That permits records that can be written but not read back. It is latent, since current records are shallower.
- **Two error-cause renderers with different leak policies.** `storeCauseText` (files:63) is documented as "never the text of any other error", while `causeText` (operations:84, also used by `initializationDamage`) falls back to `cause.Error()`. They are safe today only because callers pass errnos.
- **The bounded 16-candidate collision loop is written three times:** publishInitialRegistry (repository:1208), createStage (publication:102) and Publish (snapshots:131).
- **Locks are released two different ways.** `unlockAndClose` (media:598) unlocks explicitly because of forked children, while the lease and lock paths rely on `Close` (repository:1130, files:918-927, where `lock` returns without unlocking when the post-lock verify fails).

#### Strengths
- All path resolution is descriptor-relative through openat2 with `RESOLVE_BENEATH|NO_SYMLINKS|NO_XDEV|NO_MAGICLINKS`, with EAGAIN retried in a bounded loop (files:177-200). Identity is proved again before and after each effect.
- `publishStage` is the single owner of stage, prove, recheck, rename and read-back, with an explicit committed, not-committed or unknown outcome. Its cleanup takes no context (`discardCreated`), so it still runs after a cancellation.
- Only errnos reach diagnostics, never `*os.PathError` text (files:30-72), and capacity answers are classified separately.
- The checkpoint catalog plus the harness trace shapes (checkpoint_harness:873) fail when an effect or scenario is added or dropped, and fault-injection tests assert that the injection fired (media_test:679).
- The initial-registry recovery artifact and its re-proof under the exclusive lock are carefully argued and pinned by `init-recovery`.

#### Coverage notes
- repository_linux_amd64.go:1201 `publishInitialRegistry` has 72% coverage (17 statements uncovered), mostly the uncertain-outcome branches after the rename, which are the ones operators meet after a crash.
- repository_linux_amd64.go:433 `recoverInitialRegistry` has 71%; the branches where the stage changes during recovery are untested.
- snapshots_linux_amd64.go:75 `Publish` has 78%; no test fails a non-pristine update partway through and inspects what remains ([G08-01](#g08-01)).
- lifecycle_lock_linux_amd64.go:158 and :233 (`prepareBoundedController` 77%, `prepareControllerAccess` 73%): the guard refusals ("registry identity changed", "controller evidence changed") that defend the lock-released effect window are only partly exercised.
- media_linux_amd64.go:327 `Hold` has 41% and :783 `openStage` 50%; the refusals for a replaced root or media directory are untested.
- operations_linux_amd64.go:677 `Append`: no test covers a write or sync failure, the capacity naming, or a non-regular file at the log name.
- The harness re-runs each scenario at every checkpoint occurrence (`init-purge` alone has about 180), which plausibly dominates the 113-second suite. Cheaper fixtures, or sharing a prepared root per scenario, would help.

#### Already recorded
- audit 003 CTX-05 (report-claude.md:4004): `withMedia` skips `verifyMappings`; cited in [G08-05](#g08-05).
- audit 003 CTX-01 (report-claude.md:3948): shared inspection refuses an abandoned stage in `state/` (`admitLease`, repository:1144).
- audit 003 CTX-02 (report-claude.md:3964): kill windows in initialization attribution (`createContextDirectory`).
- audit 003 CTX-04 (report-claude.md:3992): `media/` is created by the exclusive hold even for a delete (media:109-115).
- audit 003 (report-claude.md:3533): `decodeRecord`'s generic "malformed or unsupported" message for other controller-record versions (records.go:67-72).

<a id="lane-g09"></a>

### Lane G09: Workspace: contextfs secrets, logs, runs, controller and setup records; contexts, invokerfs, selectionfs (Go)
Scope read: all of invokerfs (session, helper, channel, opener, unsupported) and selectionfs (helper, files, store); contexts/service.go and selection.go; contextfs secrets_linux_amd64.go, secrets_cleanup, lifecycle_secrets, logs, logs_read, runs, controller_linux_amd64.go, controller_uninstall, setup_linux_amd64.go, setup_runs / Skimmed: controller_records.go (lines 130-306), setup_records.go (1-80), the function list of controller_bundles, controller_clients, controller_retirement and controller_rebind, invokerfs session_linux_amd64_test.go (600-746), the test-name lists of the selectionfs and setup tests / Not read: the bodies of controller_bundles (711 lines), controller_clients, controller_retirement and controller_rebind, contexts/configuration, requests, refusals and contracts (only the descriptor type was read). I ran out of budget for them; the coverage TSV shows nothing alarming in them.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Errors are typed and descriptors held carefully; one misclassification can drive a discard |
| Security | 4 | Every open uses O_NOFOLLOW relative to a held directory, with identity re-proofs and no RemoveAll; the selection helper's answer is trusted more than invokerfs trusts its own |
| Efficiency | 3 | Each secret write rescans the whole secret tree, and the controller record is re-verified on every query |
| Clearness | 3 | Very dense guard chains; "setup tree" messages leak into uninstall; magic literals |
| DDD | 4 | Ports are consumer-owned; some stringly typed modes and operator English inside the domain service |
| Non-duplication | 3 | Three secret-area lenders, three recursive tree removers, two listing helpers, two run-numbering schemes |
| Right language | 5 | All Go, no shelling out |
| Test coverage | 3 | Behavioral tests are strong, but the root-only branches of both privilege-crossing packages never run unprivileged |

#### Findings
##### G09-01

**Medium · Quality · confidence medium**

- **Where:** internal/workspace/contextfs/setup_linux_amd64.go:106-110, :270-273, :278-280
- **What:** A transient read or listing failure of the setup subtree is classified as "another build's format", and `controller install` then plans `DiscardForeign`, which deletes the receipt, the runtimes and the wheel cache. Failures covered: EIO, EMFILE, more than `maxSetupEntries` (32) names, or a child directory that does not open.
- **Evidence:** `return prerequisites.SetupView{Exists: true, Foreign: unreadableSetupFormat}, nil` and, in recognizedSetup, `if err != nil { return false, ctx.Err() }`. prerequisites/setup.go:178-179 then calls `tx.DiscardForeign(ctx)`.
- **Impact:** An I/O fault on a healthy host is shown to the operator as "Discard setup state another Bootwright build wrote" and, once confirmed, wipes state that is recoverable but expensive to rebuild.
- **Fix:** Classify as foreign only content that decodes but carries another format, or that holds an unlisted name. Return an I/O, bound or permission failure as a refusal, as `controller uninstall --discard-unreadable` already does (specs/controller.md:442). (effort S)
- **Checked:** specs/contexts/controller-record.md:270 says "a record that cannot be read ... is another build's", so this is partly a policy trade-off. The tests cover only malformed, oversized and unrecognized entries (setup_linux_amd64_test.go:275-277), never an I/O failure.

##### G09-02

**Medium · Efficiency · confidence high**

- **Where:** internal/workspace/contextfs/secrets_linux_amd64.go:720-722, :782, :838; secrets_cleanup_linux_amd64.go:108-114, :218, :241
- **What:** `capacity` calls `scan`, which opens and stats every secret entry, on every `WriteExclusive`, `PublishExclusive`, `Replace` and `EnsureDirectory`. `Prune` re-reads the registry and fsyncs the parent once per target.
- **Evidence:** `func (a *secretArea) capacity(...) { count, size, err := a.scan(ctx)`. Bounds: `maxSecretEntries = 32768` (store.go:23).
- **Impact:** Writing N secrets in one callback costs O(N²) opens and stats, and a large prune costs N registry reads plus N fsyncs.
- **Fix:** Keep the count and size from the initial scan and update them on each write; prove the context once per prune and sync each distinct parent once. (effort M)
- **Checked:** Nothing caches the scan; `available` re-verifies only the directory identities.

##### G09-03

**Medium · Non-duplication · confidence high**

- **Where:** secrets_linux_amd64.go:126-176 and :178-242; lifecycle_secrets_linux_amd64.go:20-62
- **What:** Three copies of the same block that lends a `secretArea`: build the area, defer its close and the secrets-directory close, open `secrets` while tolerating ENOENT, scan, then `safeError(callback(...))`. The two copies in secrets_linux_amd64.go also repeat the open-root, lock, read-registry, verify-mappings and exact-record-check prologue.
- **Evidence:** the clone data pairs secrets_linux_amd64.go:160-175 with :226-241 (12 lines) and :138-151 with :190-203 (11 lines). lifecycle_secrets_linux_amd64.go:46-61 is the third copy.
- **Impact:** A change to the lending rules (say, an extra proof before the callback) must be made three times; the order of the deferred closes already differs between the copies.
- **Fix:** Extract `lendSecretArea(ctx, area, dir, callback)` and a shared prologue that returns the root, the registry and the record. (effort S)
- **Checked:** The differences are only `readOnly`, `expected` and the lock mode, so they parameterize cleanly.

##### G09-04

**Medium · Non-duplication / Clearness · confidence high**

- **Where:** controller_uninstall_linux_amd64.go:715 `emptyRenamedTree`, :565 `hostNames`; setup_linux_amd64.go:830 `emptySetupTree`, :875 `rawNames`; logs_linux_amd64.go:295 `removeLogTree`
- **What:** Three recursive removers implement the same depth-bounded, same-device, identity-checked unlink walk and differ only in the predicate that admits an entry. `hostNames` and `rawNames` are the same Readdirnames loop. `rawNames` also speaks only about setup, yet uninstall returns its error unchanged.
- **Evidence:** `return nil, state("setup tree exceeds its entry bound")` (setup:886). openStagingForUninstall:409-411 returns that error as is, so a too-large staging directory is reported as a "setup tree".
- **Impact:** There are three places to get a safety walk right, and the operator gets a misleading message.
- **Fix:** One `removeVerifiedTree(ctx, parent, name, identity, admit func(Stat_t) bool, bounds)` and one bounded lister that takes the subject for its message. (effort M)
- **Checked:** All three walks re-prove identity per level and none uses os.RemoveAll; signals-go.txt lists no RemoveAll anywhere in the workspace packages.

##### G09-05

**Low · Security · confidence medium**

- **Where:** internal/workspace/selectionfs/helper_linux_amd64.go:100-112
- **What:** Root decodes the helper's stdout with a non-strict `json.Unmarshal` and returns `response.Selection` without `validSelection`. The helper runs as the invoking account, which may be able to ptrace it, depending on the kernel's ptrace restrictions (Yama).
- **Evidence:** `if overflow || json.Unmarshal(data, &response) != nil {` … `return response.Selection, nil`
- **Impact:** This is defense in depth only: today Current matches the name against the registry (contexts/service.go:610) and contextfs re-validates it (`contextName`, secrets_linux_amd64.go:86). invokerfs, by contrast, explicitly treats helper replies as untrusted (channel_linux_amd64.go:45-46).
- **Fix:** Decode with DisallowUnknownFields and refuse when `!validSelection(response.Selection) && response.Selection != (contexts.Selection{})`. (effort S)
- **Checked:** The downstream consumers listed above; only refusal text is bounded today (`boundedText`).

##### G09-06

**Low · Security · confidence medium**

- **Where:** internal/workspace/invokerfs/session_linux_amd64.go:72-73
- **What:** A nil `*Opener` under euid 0 begins a `direct{root: true}` session, so operator-named paths are opened with root's credentials. That defeats the package's purpose and fails open.
- **Evidence:** `if o == nil || os.Geteuid() != 0 { return newSession(direct{root: os.Geteuid() == 0}), nil }`
- **Impact:** The flaw is latent: production always builds the opener through `invokerfs.New` (cmd/bootwright/wiring_account.go:81), but a future wiring slip would silently run as root.
- **Fix:** When `o == nil` and euid is 0, return `errIdentity`; keep the nil shortcut only for non-root processes. (effort S)
- **Checked:** The only test of the nil opener (session_linux_amd64_test.go:616) skips under root.

##### G09-07

**Low · DDD · confidence high**

- **Where:** contexts/contracts.go:37; contextfs/controller_linux_amd64.go:128, :187, :302, :355-357; controller_rebind_linux_amd64.go:30
- **What:** `ControllerDescriptor.Mode` is a bare `string`, compared against the literals "initializing" and "ready" in five files, while `contexts.Mode` with its `Initializing` and `Ready` constants exists beside it.
- **Evidence:** `Mode string \`json:"mode"\`` and `registry.Controller.Mode = "ready"`
- **Impact:** A typo compiles, and the two lifecycles cannot share their vocabulary.
- **Fix:** Type the field as `contexts.Mode` (or a dedicated controller mode type) and use the constants. (effort S)
- **Checked:** No constant exists for the controller's modes.

##### G09-08

**Low · Clearness · confidence high**

- **Where:** secrets_cleanup_linux_amd64.go:85, :96, :102, :110, :125, :138, :155, :196
- **What:** The literal "store.json" appears eight times, although `secretstore.RecordPath` holds that value and the sibling file uses it (secrets_linux_amd64.go:858).
- **Evidence:** `expected, exists := a.mutable["store.json"]`
- **Impact:** The record's name has two spellings.
- **Fix:** Use `secretstore.RecordPath`. (effort S)
- **Checked:** `RecordPath = "store.json"` at internal/secrets/secretstore/record.go:13.

##### G09-09

**Low · Non-duplication · confidence high**

- **Where:** setup_runs_linux_amd64.go:24, :29-41; runs_linux_amd64.go:32-44
- **What:** `maxSetupRunOutput = 4 << 20` restates `operationstore.MaxAdapterOutputBytes`; its own comment says the two must be one bound. `setupRunNumber` and `setupRunName` re-implement the six-digit numbering that `outputNumber` and `outputName` already take from `reconciliation.ParseNumber` and `FormatNumber`, and `newestSetupOutput` uses the latter.
- **Evidence:** `maxSetupRunOutput = 4 << 20` next to the comment "the bounded run's own bound, operationstore.MaxAdapterOutputBytes".
- **Impact:** The two bounds and the two grammars can drift apart silently.
- **Fix:** Reference the constant and route the setup numbering through `outputNumber`/`outputName` plus a bare-ID helper. (effort S)
- **Checked:** contextfs already imports operationstore (logs_linux_amd64.go:19).

##### G09-10

**Low · Quality · confidence medium**

- **Where:** internal/workspace/contexts/service.go:468
- **What:** `updateUnder` writes to `reg.Contexts[findRecord(reg, record.Name)]` without checking for -1. `Init` guards the same step (:271-274).
- **Evidence:** `reg.Contexts[findRecord(reg, record.Name)] = record`
- **Impact:** If the registry changes under `Publish`, this panics instead of refusing. That is unlikely under the root lock, but the handling is inconsistent.
- **Fix:** Mirror Init's `index < 0` refusal. (effort S)
- **Checked:** `decideUnder` found the record in an earlier `tx.Registry()` snapshot.

##### G09-11

**Low · Efficiency · confidence medium**

- **Where:** controller_linux_amd64.go:416-419, :462, :484
- **What:** `readControllerStored` (root-entry verification, controller listing, decode, bundle-reservation proofs) runs again for each query. `CheckControllerInput` runs it twice in a row, and one deletion decision runs it once each for `HostReservations`, `ExclusiveHostReservations` and `InstallerWorkAreas`.
- **Evidence:** `if err := t.checkControllerRecovery(ctx, name)` (which reads it) followed by `stored, err := readControllerStored(ctx, t.root, t.registry)`
- **Impact:** One command performs several redundant verified reads of the same record under one lock.
- **Fix:** Cache the stored record per transaction, invalidating it on publish; `controllerEvidence` already holds most of this. (effort M)
- **Checked:** The lock is held throughout, so a cached read would stay valid.

##### G09-12

**Low · Clearness / DDD · confidence medium**

- **Where:** internal/workspace/contexts/service.go:31, :115, :622-643
- **What:** The 823-line Service combines the init, update, use, list, current and delete flows. It assembles long operator-facing sentences inside the domain service (`deleteAction`, `retained`), `admit` returns five results, and `New(..., options ...Options)` silently ignores extra arguments.
- **Evidence:** `func (s Service) admit(ctx context.Context, path string) (desiredstate.Sources, string, string, *compilation.Report, error)`
- **Impact:** Presentation wording changes touch domain code, and the API shape invites misuse.
- **Fix:** Split the file by flow, return an `admission` struct, make `Options` a required argument, and move the confirmation wording next to the Presenter. (effort M)
- **Checked:** Function lengths stay within the fitness limit, so this is not a length finding.

#### Patterns
- Two credential-dropping re-exec helpers have diverged. invokerfs (channel_linux_amd64.go:30-43, :84-94) locks the OS thread for Pdeathsig, has an injectable `launch` and treats replies as untrusted. selectionfs (helper_linux_amd64.go:124-132) uses `exec.CommandContext` with no seam and trusts its replies. One shared launcher would give both the stronger behavior.
- Domain invariants are re-implemented as persisted-format validation in the adapter. For example, "one key to one context unless shared" appears in controller_records.go:231-270 and in prerequisites/reservations.go. That is acceptable as an integrity check, but the rule now lives in two places.
- An identical 12-line hand-rolled JSON shape scanner appears in internal/secrets/localkeyring/format.go:388-402 and contextfs/records.go:97-111 (records.go is outside this lane). It is a candidate for canonicaljson.
- Best-effort paths discard their errors entirely (logs_linux_amd64.go:165, :223), and `logTreeBytes` counts an unreadable tree as 0 bytes (:270-272). The spec allows best effort, but a permanently stuck retention leaves no signal.

#### Strengths
- Every open is relative to a held directory with O_NOFOLLOW, and identity is re-proved by dev/ino before each rename or unlink: uninstall's rename-aside-then-verify-then-rename-back (controller_uninstall:660-679) and the selection publish's staged-inode proof (files_linux_amd64.go:119-133).
- The invokerfs protocol is tight: SEQPACKET with MSG_TRUNC/CTRUNC checks, exactly one SCM_RIGHTS descriptor, close-on-exec on receipt, and errno bounded below 4096. The handshake requires the helper's real and effective uid and gid to match the requested account.
- Uninstall refuses a mount point, a parent writable by another user (unless sticky), a foreign owner and a held lock before acting, and bounds depth (256) and entries (16M).
- Uncertain-durability outcomes (publicationUnknown) are carried as distinct `Uncertain` results with remediation, not collapsed into failure.
- The tests name behavior (TestAStalledOrCancelledHelperEndsTheSession, TestRetirementSkipsARuntimeALaunchHolds) and exercise refusal, concurrency and cancellation paths.

#### Coverage notes
- selectionfs/helper_linux_amd64.go:67 `Store.perform` is 19% covered and :139 `boundedBuffer.Write` 0%. The root path (helper launch, timeout, output overflow) runs only under root. An injectable euid and launch seam like invokerfs's `launch` would let it run unprivileged.
- invokerfs/session_linux_amd64.go:68 `Opener.Begin` is 17% covered. The UID/GID switch and the nil-opener-under-root branch ([G09-06](#g09-06)) are never exercised without root.
- setup_linux_amd64.go:270 `recognizedSetup` and :101 `readSetupState` have no test for an I/O or bound failure ([G09-01](#g09-01)).
- secrets_linux_amd64.go:955 `secretArea.Sync` is 31% covered, which matters for durability.
- controller_uninstall_linux_amd64.go:360 `uninstallMedia` is 63% covered; its media-acquisition busy refusal (:395-396) blocks uninstall.
- runs_linux_amd64.go:167 `createRun` is 60% covered; the discard-on-failure branches carry the promise that "a failure leaves no file".
- invokerfs and lifecycle_secrets tests use `time.Sleep` and wall-clock bounds (session_linux_amd64_test.go:717, lifecycle_secrets_linux_amd64_test.go:199), so they can be timing-sensitive under load.

#### Already recorded
- B510: selectionfs `.bootwright` creation skips the parent sync on EEXIST (files_linux_amd64.go:206-214). Tracked, not re-reported.
- Audit 003 ARC-03: invokerfs injected into other adapters through their ports (architecture drift). Not re-reported.

<a id="lane-g10"></a>

### Lane G10: Secrets: custody, material, encryption, local keyring, secret store (Go)

Scope read: I read every production file in `internal/secrets/` fully: root, custody/, material/, encryption/, localkeyring/, and secretstore/ (contracts, record, catalog, values, access). / Skimmed: `secretstore/areacontract`, plus these tests: `localkeyring_linux_amd64_test.go` (the leak test), `encryption/rotate_prompt_test.go`, and the test file list. / Not read: the other `_test.go` bodies. I used the coverage data for them.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful fail-closed handling, context checks and clearing; some dead code. |
| Security | 4 | Sound AEAD, nonces, AAD and key separation; the redaction of secret material can be bypassed by Go's printing, and a few zeroization gaps. |
| Efficiency | 3 | Every session open re-reads every identity file and ledger, several times per command. |
| Clearness | 3 | Very dense boolean guards (cyclomatic complexity 55-57); unused multi-key generality. |
| DDD | 4 | Ports are consumer-owned and backend policy is isolated; the root package mixes admission with value types. |
| Non-duplication | 3 | Mutation prologues, the fingerprint rule, certificate parameters and defaults are each repeated. |
| Right language | 5 | Go only, and appropriate. |
| Test coverage | 3 | 78-84% per package, but Rebind, the produced-material reads and redaction have no tests. |

#### Findings

##### G10-01

**Medium · Efficiency · confidence high**

- **Where:** `localkeyring/artifacts.go:122-158`, `localkeyring/localkeyring.go:158`, `localkeyring/localkeyring.go:266`, `localkeyring/mutations.go:437,473`
- **What:** Each `refreshMetadata` call decrypts and checks every ledger and reads and decodes every `identities/*.json` file. This happens on every `open` and every `Inspect`, and `publish` runs `collectArtifacts` twice more.
- **Evidence:** `data, exists, err := area.Read(ctx, "identities/"+entry.Name, selectorMaximum)` sits inside the loop over all identities. The spec requires identity reservations to be retained after logical deletion (`specs/secrets.md:586`). Every fresh apply that references Secrets creates a new binding, and so a new reservation (`lifecycle/executor.go:106`).
- **Impact:** One `secret set` does a View for holds, a View, a Mutate, an Inspect in each, and two prunes. That is about 8 full scans of a directory that only grows. It is capped at 32,768 entries, so the keyring also has a finite lifetime: once versions plus bindings ever created reach that cap, it refuses with `store.limit`, and the spec forbids removing identity evidence.
- **Fix:** Check identity reservations only when publishing, not on read-only opens. Also consider keeping reservations inside the authenticated index, or as one compacted, authenticated file. The cap is a spec matter (policy trade-off). (effort M)
- **Checked:** I found no cache across sessions. No backlog item covers identity growth.

##### G10-02

**Medium · Security · confidence high (mechanism) / medium (exposure)**

- **Where:** `internal/secrets/values.go:27,74-78`, `custody/bindings.go:96-99`, `reconciliation/lifecycle/scheduler.go:44`
- **What:** `Material` hides its bytes only through `String`, `GoString` and `MarshalJSON`. When a `Material` sits in an unexported struct field, fmt cannot call those methods (the field is not `CanInterface`), so it walks the `parts` map instead. With `%s` it prints the plaintext.
- **Evidence:** `type Material struct{ parts map[Part][]byte }`; `type current struct { ... material secrets.Material }`.
- **Impact:** A `%v` or `%s` (or a slog value) of any struct holding material in an unexported field writes secret bytes to the output. The `%+v` habit common in test failures makes this likely to happen eventually.
- **Fix:** Hold the bytes behind a pointer (`type Material struct{ p *materialParts }`). fmt prints a nested pointer as an address. Add a test that formats a wrapper struct. (effort S)
- **Checked:** No test covers `Material`'s redaction at all; grep found no use of `[confidential material]` in any test.

##### G10-03

**Medium · Non-duplication · confidence high**

- **Where:** `localkeyring/mutations.go:47-55,152-160,239-246`, `localkeyring/produced.go:22-30`, `localkeyring/mutations.go:163-189` vs `248-269`, `custody/bindings.go:13-32` vs `39-66`
- **What:** The same "collect identity names into known IDs" prologue appears in four places. The per-input check (declaration, material, version lookup, `equalVersion`) is copied between `Bind` and `Rebind`. The epilogue (collect, canonicalize, check bounds, get key, publish) appears six times. In custody, the closures of `Bind` and `Rebind` are near-identical.
- **Evidence:** `knownIDs[strings.TrimSuffix(name, ".json")] = true` at produced.go:29, mutations.go:54, 159 and 245.
- **Impact:** A rule change, such as the reservation scheme in [G10-01](#g10-01), has to be made in four to six places in the most security-sensitive code.
- **Fix:** Extract `reservedIDs(ctx)`, `boundVersion(ctx, next, input)` and `commit(ctx, &next, plain, ids)`. (effort M)
- **Checked:** These are not consumer-owned ports and not golden helpers, so this is not deliberate duplication.

##### G10-04

**Medium · Test coverage · confidence high**

- **Where:** `localkeyring/mutations.go:215` (`session.Rebind`, 64 of 64 statements unrun), `custody/bindings.go:39` (0%), production caller `reconciliation/lifecycle/rebind.go:151`
- **What:** The only production path that rebinds an access credential has never run against the real keyring. The lifecycle tests use a fake binder.
- **Evidence:** gocov any_pct is 0 for both functions.
- **Impact:** Its refusals ("already pins this version", "pins no version of", the constant-time material match, the binding limit) and its published shape are unproven. The binding-limit check, `len(s.index.Bindings) >= maxBindings`, applies even though rebind keeps the old binding.
- **Fix:** Add keyring-backed tests for a successful rebind, each refusal, and Release of the replaced binding. (effort S)
- **Checked:** grep found no `Rebind` in any `internal/secrets` test.

##### G10-05

**Medium · Clearness · confidence high**

- **Where:** `material/generation.go:256-341` (cyclomatic complexity 55), `localkeyring/localkeyring.go:377-457` (57)
- **What:** Validating a generated certificate packs 15 to 20 conditions into single expressions. The three accepted validity windows are written inline at lines 326-327.
- **Evidence:** `startCapped := cert.NotBefore.Equal(issuer.NotBefore) && !cert.NotAfter.Before(cert.NotBefore.AddDate(0, 0, days)) && !cert.NotAfter.After(...)`
- **Impact:** A newcomer cannot tell which window corresponds to which clamp in `generateIssuedCertificate` (lines 519-524). A wrong edit would quietly widen what is accepted.
- **Fix:** Split it into named predicates: `issuerChainValid`, `acceptedValidity(cert, issuer, days)` (with one case per clamp), and `exactShape`. (effort S)
- **Checked:** Function length is held by the 100-line fitness test, so I am reporting complexity, not length.

##### G10-06

**Low · Non-duplication · confidence high**

- **Where:** `material/generation.go:261-280` vs `455-476`; `62-68` vs `133-139`
- **What:** Certificate parameter handling is copied between generation and validation: the 3650-day default, the bound, the common-name check, and the DNS and IP loops. The token byte default and bound are copied the same way. The defaults (3650 days, "admin", ed25519, 32 bytes) also live in `admission.go:26-30` and `custody/declarations.go:65-68`.
- **Evidence:** `days := configuration.ValidityDays; if days == 0 { days = 3650 }`, twice.
- **Impact:** If generation and validation drift apart, freshly minted material can be refused as non-conforming.
- **Fix:** Add one `certificateParameters(declaration)` and one `tokenBytes(declaration)`. Rely on the defaults `Normalize` applies, or own every default in one place. (effort S)
- **Checked:** `admission_agreement_test.go` pins only the admission-versus-generation agreement for usernames, common names and comments.

##### G10-07

**Low · Non-duplication · confidence high**

- **Where:** `custody/declarations.go:75-80`, `localkeyring/format.go:633-641`
- **What:** The declaration fingerprint rule (JSON-encode the declaration with an empty fingerprint, then hex SHA-256) is implemented twice. The keyring recomputes it to verify each put.
- **Evidence:** `d.Fingerprint = ""; canonical, _ := json.Marshal(d)` at custody, and `declaration.Fingerprint = ""` at localkeyring.
- **Impact:** If either copy changes, every `PutBatch` and `Bind` refuses with "stored secret declaration is invalid".
- **Fix:** Add `secrets.Declaration.ComputeFingerprint()` in the root package and call it from both. (effort S)
- **Checked:** Both use `encoding/json` on the same struct, so today they agree.

##### G10-08

**Low · Security · confidence high**

- **Where:** `material/files_linux_amd64.go:581-598`
- **What:** `readWithLimit` starts with a capacity of at most 32 KiB and then grows by `append`. Each reallocation drops a backing array that still holds secret bytes, and nothing clears it.
- **Evidence:** `data := make([]byte, 0, min(maximum, 32768))` followed by `data = append(data, buffer[:n]...)`.
- **Impact:** File material larger than 32 KiB (certificate bundles up to 1 MiB) leaves uncleared heap copies. That undoes the clearing discipline used everywhere else; `readInput` preallocates `maximum+1`.
- **Fix:** Preallocate from the size held in `before.Size` (already bounded), or from `maximum`. (effort S)
- **Checked:** Callers clear only the final slice.

##### G10-09

**Low · Security · confidence medium**

- **Where:** `material/generation.go:506-509`, `material/generation.go:411`
- **What:** A generated CA has no path-length constraint (`MaxPathLen` is unset, so the length is unlimited). The validator enforces exactly that shape: it refuses `MaxPathLenZero` and any positive value.
- **Evidence:** `if !certificate.BasicConstraintsValid || certificate.MaxPathLenZero || certificate.MaxPathLen > 0 ...`
- **Impact:** Bootwright's CA only ever signs leaf certificates. `pathlen:0` is the defense-in-depth norm and would stop a misused CA key from minting intermediates that verifiers accept.
- **Fix:** Set `MaxPathLenZero: true` on CA templates and require it in validation. This is pre-1.x, so stored CAs would be refused and re-minted. (effort S)
- **Checked:** Leaf certificates are correctly `IsCA=false`, DigitalSignature plus ServerAuth, P-256 with ECDSA-SHA256, and a 128-bit random serial.

##### G10-10

**Low · Quality · confidence high**

- **Where:** `material/validation.go:35-46`; `localkeyring/mutations.go:394`, `initialization.go:210`, `localkeyring.go:220-233`, `521-531`
- **What:** There is dead generality. `validateCA`'s private-key branch cannot run: a stored-material caBundle has only a certificate part, and a generated one goes to `validateGeneratedCertificate`. Likewise, every index holds exactly one key, because rotation replaces the list, so lazy loading of a non-active key and the "retired" key state never apply to a valid store.
- **Evidence:** `next.Keys = []storedKey{{ID: keyID}}`. Coverage: `validateCA` 47%, `session.key` 22%.
- **Impact:** Readers have to reason about multi-key states that cannot occur.
- **Fix:** Remove the unreachable branch. Note in a comment that rotation keeps a single key, or simplify. (effort S)
- **Checked:** I grepped for every assignment to `Keys`; only those two exist.

##### G10-11

**Low · Efficiency / Clearness · confidence high**

- **Where:** `localkeyring/mutations.go:355-362`, `initialization.go:326-327`, `initialization.go:467-472`
- **What:** The `keys` directory is listed twice, once as "keyNames" and once as "ledgerNames", which suggests a separate ledger directory that does not exist. The artifact check also loops over a table with a single entry.
- **Evidence:** `ledgerNames, err := entryNames(ctx, s.area, "keys")`
- **Impact:** One extra directory walk per rotation or initialization, plus misleading names.
- **Fix:** List the directory once and check both suffixes; inline the single table entry. (effort S)

##### G10-12

**Low · Clearness · confidence high**

- **Where:** `encryption/service.go:131,177`
- **What:** The rotation refusals spell out `bootwright secret encryption ... --context `+name by hand instead of using `secrets.Command`.
- **Impact:** If the context name is empty, the result ends in `--context ` with nothing after it, and the wording drifts from other remedies.
- **Fix:** Use `secrets.Command(name, "encryption status")` and its siblings. (effort S)

#### Patterns
- **Copies of secrets left uncleared.** `Material.Part` always clones, so each consumer has to remember `clear(x)`. This repeats about 20 times (`custody/service.go:673-677`, `localkeyring/mutations.go:610-618`, `material/issuer.go:17-20`). Lending a read-only view and clearing through a callback would remove that burden.
- **Defaults and grammars repeated across layers.** Name validation is hand-written in `localkeyring/format.go:613` but is `api.ValidLexical("name")` in custody. DNS-name checking appears in `material/generation.go:661` and in the API. The defaults are repeated as in [G10-06](#g10-06).
- **Secure file-open plumbing cloned across packages.** `processFiles` and the denial wording are cloned between `material/files_linux_amd64.go:483-506` and `desiredstate/inputfs/reader_linux_amd64.go:738-761`. `pathHandle = 0x200000` is defined in four packages.
- **Hand-rolled JSON bound scanner cloned.** `boundedJSON` (`localkeyring/format.go:388-402`) is an exact clone of `workspace/contextfs/records.go:97-111`. It belongs next to `canonicaljson`.
- **Identical admission entry points.** `ValidateAuthored` and `ValidatePartial` in `admission.go:66-71` are identical.

#### Strengths
- The AEAD use is sound: AES-256-GCM with random 96-bit nonces, a seal budget capped at 2^20 per key and reserved durably before sealing, and associated data that binds context, backend, generation, key, blob, declaration fingerprint, name and part. MAC keys are derived by HMAC under their own domain labels, and comparisons are constant-time.
- File acquisition is careful: descriptor-relative opens with `O_NOFOLLOW`, held ancestor handles, stability checks before and after reading plus a fresh re-walk, ownership and mode refusals, and remedies that name no secret.
- Refusals carry only fixed messages and paths, never material or digests. Cancellation is kept distinct from refusals throughout.
- Generation draws only from `crypto/rand`. The validator refuses generated material whose shape differs from the declaration, down to the SAN encoding and validity.
- Confirmation is never asked while a store lock is held, and the version is re-proved under the lease (with tests).

#### Coverage notes
- `localkeyring/mutations.go:215` and `custody/bindings.go:39` (Rebind): 0%, see [G10-04](#g10-04).
- `custody/produced.go:63` (`Holds`) and `:114` (`ReadProducedInArea`): 0%, although lifecycle relies on both in production (`lifecycle/produced.go:75,120`).
- `values.go:74-78`: nothing pins `String`, `GoString` or `MarshalJSON` redaction. The root package's own tests reach 42.7%.
- `TestMaterialNeverAppearsInMetadataOrErrors` (`localkeyring_linux_amd64_test.go:792`) checks only `snapshot.ActiveKey` and the error text. It never checks the stored files, the identity files or `store.json` for the plaintext.
- `localkeyring/initialization.go:504` (44%) and `:559` (28%): the attribution branches for MAC-signed pending records and pending ledgers decide whether crash leftovers are accepted. Only some are exercised.
- `material/service.go:266` `readInput` is at 66%: the zero-byte read and invalid-count refusals are not exercised.
- localkeyring tests take about 31 seconds. I did not profile them; the file-system integration helpers are the likely cost.

#### Already recorded
- SCR-02 (audit 003): `nextSequence` scans only retained versions (`mutations.go:754-762`). Still present, so I did not restate it.
- SCR-04 (audit 003): the remedy in `secretstore/catalog.go:224-226` still carries a literal `<context>`.
- PRJ-04 / CTX-06 (audit 003): format version of identity reservations. Not re-reported.

<a id="lane-g11"></a>

### Lane G11: Reconciliation core (port vocabulary, operation store, log view, Ansible runner)

Scope read: I read these files in full: the root package (plan.go, stage.go, state.go, evidence.go, frozen.go, identity.go, delta.go, lineage.go, preparation.go), operationstore/{store,records,logs,contracts}.go, logview/{contracts,requests,render,service,session,follow,show}.go, contextguard/guard.go, and ansiblerunner/{process_linux_amd64,jobs_linux_amd64,runner,scope_linux_amd64,scratch_linux_amd64,outputs_linux_amd64,isolated_linux_amd64,process_unsupported}.go.
I skimmed: process_linux_amd64_test.go (lines 30-470), the test list of store_test.go, the timing parameters of the deadline tests and sweepingRunner, operationstore/areacontract against secretstore/areacontract (lines 340-362 / 486-506), and the clone side in stagelocal (staging_linux_amd64.go:425-505). To check claims I also opened contextfs operations_linux_amd64.go (Read, Append, openLog, capacity, measured) and the cli logs presenter.
Not read: areadouble (test support), and the remaining _test.go files beyond their names. The brief limits test reading to sampling.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed records, exact-expectation replacement and careful job locking; small error-classification gaps |
| Security | 4 | Strong process, SSH and file-handle discipline; secret copies left uncleared in two places |
| Efficiency | 3 | Every attempt re-walks the whole area; one fsync per output chunk; status reads a whole log to test that it exists |
| Clearness | 3 | Rationale is present, but the record-name grammar is scattered and Run/AdmitAttempt are dense |
| DDD | 4 | Root package is one coherent lifecycle vocabulary; the store re-derives the area's own accounting |
| Non-duplication | 3 | Verbatim no-follow tree removal is copied from stagelocal; attempt-name parsing and digest checks are repeated |
| Right language | 5 | All Go; extra-vars are marked unsafe, inventory is typed and nothing runs through a shell |
| Test coverage | 4 | 82-90% with behavioural tests and real-process tests; the runner suite is serial and slow, and some logview paths have no tests |

#### Findings

##### G11-01

**Medium · Efficiency · confidence high**

- **Where:** internal/reconciliation/operationstore/store.go:301-334, :444-470, :362-377; internal/workspace/contextfs/operations_linux_amd64.go:811-829
- **What:** `AdmitAttempt` runs for every attempt and resolution. Each call re-walks every entry of the operation area (`held`) and re-reads every block record of the plan (`resolutions` → `BlockStates`), all while holding `s.attempts`. The production records area is `cached: true` (reconciliation_linux_amd64.go:349), so the area already keeps the same entry and byte counts for its own bound.
- **Evidence:** `held, size, err := s.held(ctx, "", "")` under `s.attempts.Lock()`; the area comment says "scanned once and then counted for one that does [cache]"; the store's own comment on `held` says it "counts the entries beneath target as the area's entry bound counts them".
- **Impact:** The same bound rule exists twice, so the two can drift. On a large plan, up to about 8192 directory listings plus up to 256 record decodes per attempt run under one mutex, which serializes the starts of parallel blocks.
- **Fix:** Add a `Usage(ctx) (entries int, bytes int64, error)` method to `Area`, backed by the area's measurement. Count in-flight and unknown blocks from the scheduler's in-memory states instead of re-reading the records. (M)
- **Checked:** The walk stops once the count passes `MaxEntries`, so it is bounded but not cheap. Nothing caches `held`; only `removalReserve` is memoized.

##### G11-02

**Medium · Efficiency · confidence medium**

- **Where:** internal/reconciliation/operationstore/logs.go:130-147, :175-200; internal/workspace/contextfs/operations_linux_amd64.go:677-711
- **What:** Every `AdapterOutput.Write` publishes at once through `Area.Append`. Each Append walks down the path, opens the file, fstats it, writes and calls `file.Sync()`. `exec` copies adapter stdout and stderr into this writer one pipe read at a time, and ansible-playbook prints roughly line by line.
- **Evidence:** `o.pending = append(o.pending, value...)` then `_ = o.flush(o.ctx)`; in the area, `if err := file.Sync(); err != nil {`
- **Impact:** A long playbook triggers one fsync per output burst on /var/log, for material the store itself calls "troubleshooting material only". The spec asks only for "within a bounded delay" (specs/cli/output.md:1277-1278).
- **Fix:** Flush when pending reaches a byte threshold, or on a timer started by the first pending byte (the fix audit OUT-11 proposed). Alternatively let `.output` appends skip the per-write fsync. (S)
- **Checked:** Commit 52f0e90a6 removed the earlier `adapterOutputInterval`, which is how this behaviour arrived. I did not measure the chunk rate, hence medium confidence.

##### G11-03

**Medium · Non-duplication · confidence high**

- **Where:** internal/reconciliation/ansiblerunner/jobs_linux_amd64.go:607-672; internal/controller/stagelocal/staging_linux_amd64.go:439-501
- **What:** `emptyTree`, `emptyChild` and `entries` copy `emptyStage`, `emptyStageChild` and `stageEntries` line for line. These are security-critical no-follow, same-device, bounded tree removals; only the error strings differ. contextfs setup_linux_amd64.go:883 and controller_uninstall_linux_amd64.go:574 repeat the same bounded `Readdirnames(1024)` listing loop.
- **Evidence:** Both sides contain `slices.SortStableFunc(names, func(a, b string) int { return slices.Index(last, a) - slices.Index(last, b) })` and `if stat, ok := listed.Sys().(*syscall.Stat_t); !ok || stat.Dev != device {`
- **Impact:** A hardening fix (for example a new mount or link case) must land in two or more places. A missed copy reopens the hole.
- **Fix:** Move one `RemoveTree(dir *os.Root, device uint64, bound *int, last ...string)` into a small shared filesystem package and call it from both. (M)
- **Checked:** This is not one of the deliberate kinds (it is not a port or a golden helper). No shared package exists today.

##### G11-04

**Low · Clearness / Non-duplication · confidence high**

- **Where:** internal/reconciliation/operationstore/store.go:836-856, :1037, :1129-1142, :1194-1209; logs.go:76-90, :403-429; logview/show.go:77, :87
- **What:** The grammar of attempt and resolution file names is built or parsed in about seven places, each slightly differently. `StartResolution` uses fixed offsets (`len(prefix)+11`, `[len(prefix):len(prefix)+6]`), others use CutPrefix/CutSuffix, and logview re-derives `.output` and `.jsonl` paths by `TrimSuffix` instead of calling `AdapterOutputPath`. The path `path.Join(id, "blocks", block, "attempt-"+name+".json")` is spelled out six times.
- **Evidence:** `chosen.Path, found = ..., strings.TrimSuffix(logPath, ".jsonl")+".output"` (show.go:77)
- **Impact:** A rename or new suffix must change every site.
- **Fix:** One `attemptName(attempt, resolution, ext)` and one `ParseAttemptFile`, used by the store, the logs and logview. (S)
- **Checked:** The StartResolution/LastResolution part is already tracked as B582; this extends it to the logs and logview sites.

##### G11-05

**Low · Efficiency · confidence high**

- **Where:** internal/reconciliation/operationstore/logs.go:315
- **What:** `LogPaths` tests whether the operation log exists by reading it with maximum 0, and the area treats 0 as "up to 8 MiB" (operations_linux_amd64.go:411-413). The bytes are then thrown away.
- **Evidence:** `if _, found, err := s.logs.Read(ctx, OperationLogPath(id), 0); err != nil {`
- **Impact:** Every `status` and every attempt summary (lifecycle/status.go:266, attempts.go:695) reads up to 8 MiB just to test existence.
- **Fix:** Use `s.logs.Entries(ctx, id)` and look for `operation.jsonl`. (S)
- **Checked:** No cheaper existence call is used anywhere else in this function.

##### G11-06

**Low · Security · confidence high**

- **Where:** internal/reconciliation/ansiblerunner/process_linux_amd64.go:207-218, :236-260
- **What:** The runner clears secret bytes on purpose, but two spots work against that.
- `confidentialValue` converts every secret part to an immutable `string` for `strings.Contains`, and those copies can never be cleared.
- `materialize` clears only the value whose write failed; the other values in the `materialBytes` copy are left on the heap.
- **Evidence:** `equal := ok && len(bytes) != 0 && strings.Contains(value, string(bytes))` followed by `clear(bytes)`; and `clear(value); return nil, failure(...)` inside the loop.
- **Impact:** Secret copies outlive the attempt in process memory. The cost is low, but it undoes the clearing the code was written to do.
- **Fix:** Use `bytes.Contains([]byte(value), part)`, which copies the non-secret side instead. Add a deferred loop that clears every entry of `contents`. (S)
- **Checked:** `secrets.Material.Part` returns a clone (values.go:46), so the `clear` calls are meaningful and the string copies are the only leaks.

##### G11-07

**Low · Quality · confidence medium**

- **Where:** internal/reconciliation/preparation.go:24, :27, :31, :43, :47
- **What:** The preparation-scope refusals are plain `fmt.Errorf` values. They have no `lifecycle.state` code and no remediation, yet `ReadPlan` and `Plan.Digest` pass them through next to sibling refusals that carry both. The local name `copy` also shadows the builtin.
- **Evidence:** `return fmt.Errorf("preparation purpose or targets are invalid")` (no format arguments)
- **Impact:** A corrupt `plan.json` preparation reaches the operator as an undiagnosed error, without the "destroy with the build that registered it" remedy that `ReadPlan` gives a non-reproducible plan.
- **Fix:** Use the package's `planError` or `stateError`, and rename `copy`. (S)
- **Checked:** Every other refusal in the root package goes through diagnostics (state.go:172-178).

##### G11-08

**Low · Clearness · confidence medium**

- **Where:** internal/reconciliation/ansiblerunner/process_linux_amd64.go:64-157, :97
- **What:** `Runner.Run` has cyclomatic complexity 30 and 74 statements, and covers validation, staging, claiming, materializing, inventory, execution and reading outputs back. It also takes a required dependency, the invocation scope, out of `ctx.Value` and stores it on its value-receiver copy, which `sweep`, `release` and `owns` then rely on.
- **Evidence:** `r.invocation, _ = ctx.Value(invocationKey{}).(*invocationScope)`
- **Impact:** Whether a sibling run's job counts as "owned" depends on a context value that is invisible at the call site, so it is easy to break in a refactor.
- **Fix:** Split Run into `prepare` (job, materials, inventory) and `execute`. Have `BeginInvocation` return a scoped runner, or pass the scope explicitly. (M)
- **Checked:** The metrics confirm complexity 30 (gometrics-prod.tsv). Tests cover the scope behaviour (scope_linux_amd64_test.go).

##### G11-09

**Low · Non-duplication · confidence high**

- **Where:** internal/reconciliation/plan.go:573-579; operationstore/records.go:330-331, :397-398, :511-512
- **What:** The check "64 lowercase hex digits, canonical" exists as `validDigest` (unexported) in the root package and again inline three times in records.go. Separately, the two packages define `maxIdentifier` with different meanings: 63 in the root package, 256 in operationstore.
- **Evidence:** `decoded, err := hex.DecodeString(digest); ... len(decoded) != 32 || hex.EncodeToString(decoded) != digest`
- **Impact:** There are four copies of an identity rule, and one constant name means two different things.
- **Fix:** Export `reconciliation.ValidDigest` and use it in records.go; rename operationstore's constant to `maxRecordText`. (S)
- **Checked:** No exported helper exists today.

##### G11-10

**Medium · Test coverage (efficiency) · confidence medium**

- **Where:** internal/reconciliation/ansiblerunner/process_linux_amd64_test.go:218-260, :336-396; jobs_linux_amd64_test.go:65-72
- **What:** This is why the suite takes about 35 seconds.
- No process-spawning test calls `t.Parallel`; only node_inventory_test.go does, four times.
- About 90 subtests each start `/bin/sh` or re-exec the test binary, then wait through drain windows of 200-500 ms and polling sleeps.
- `TestARecordWaitingAtTheDrainIsJudgedAsIfReadFirst` alone runs 3×6 rounds, each with a 100 ms sleep plus waiting for /proc to show the adapter gone. The failure matrix runs 5×3.
- **Evidence:** `drain: 200 * time.Millisecond` in sweepingRunner; `for round := range 6 {` … `time.Sleep(100 * time.Millisecond)`
- **Impact:** Every `make check` pays these serial seconds on the path that matters most.
- **Fix:** Add `t.Parallel()` to the subtests. Each already uses its own `jobParent` and scratch through TempDir, and the /proc checks match by PID or process group, so they do not interfere. Cut the round count to what the race needs. (S)
- **Checked:** I did not run the tests (read-only), so the attribution comes from reading the timings; hence medium confidence.

#### Patterns
- Bounds and accounting are computed again at each layer instead of asked of their owner. The store's `held`, `logBytes` and `outputRoom` re-walk areas that already measure themselves (store.go:444, :492; operations_linux_amd64.go:811).
- Sensitive filesystem primitives get re-implemented per bounded context instead of shared: tree removal ([G11-03](#g11-03)), bounded directory listing (contextfs setup:883, uninstall:574), and private-file checks (`runDirectories.file` and `private` against contextfs `private(...)`).
- The JSON reading discipline varies across files:
  - canonical closed decode for records (records.go:253);
  - a hand-rolled order-insensitive token reader for mutation evidence (evidence.go:61);
  - a `DisallowUnknownFields` reader for job records (jobs_linux_amd64.go:487);
  - a member sniffer for refusal hints (records.go:479).

  Each choice is defensible, but none is named as a choice.

#### Strengths
- Durable write ordering is careful and explained:
  - the plan and operation are written before the index names them;
  - the attempt record is created exclusively before the block record counts it;
  - every replacement is guarded by exact expected bytes;
  - adoption is limited to a provably effect-free running record (store.go:945-1060).
- The decoders refuse non-canonical, unknown-field, oversize or non-UTF-8 records, and a refusal names the build that wrote the record (records.go:253-310).
- Job ownership uses an exclusive flock to hold the job, a shared flock to sweep it, and a holder lock to tell a live run from leftovers. All walks are `*os.Root`-relative, never follow links and stay on one device (jobs_linux_amd64.go:309-407).
- The adapter boundary: extra-vars are written with every string marked unsafe (ansible.ExtraVariables); SSH arguments exclude agents, proxies, ambient known_hosts and passwords; outputs are read back as 0600, single-link, exact-size files.
- The root package is a cohesive lifecycle vocabulary (Plan/Block/State/Outcome/Effect/Stage make up the top usages across 27 importers), with pure transition tables and deterministic plan ordering.

#### Coverage notes
- logview/session.go:241 `blockDirectories` is at 0%. It is the path for an operation with no `plan.json` (an interrupted registration), which is exactly when an operator needs logs.
- logview/service.go:62 `Service.resolve` is at 20%: the current-context selection and the "no current context" refusal are untested.
- ansiblerunner/jobs_linux_amd64.go:102 `claim` is at 42%: the failures to create or lock the lock and holder files, and cleanup after a `publishRecord` failure, are untested.
- ansiblerunner/scope_linux_amd64.go:53 `claimRun` is at 66%: the cleanup after a Chmod or Stat failure, which removes the job, is untested.
- logview/show.go:43 `showRaw` is at 79%: the `--attempt` branch's missing output and the missing companion `.jsonl` are not exercised.
- operationstore/store.go:1185 `LastResolution` is at 78%: the refusals for a non-canonical resolution name and for a record that contradicts its location are not all hit.

#### Already recorded
- B582: StartResolution and LastResolution parse resolution names differently (part of [G11-04](#g11-04)).
- B569: the store does not check block state when it starts or completes an attempt (not re-reported).
- B583: bare record-version literals in records.go and store.go (not re-reported).
- Audit 003 OUT-15: `safeLogText` keeps C1 controls such as U+009B. Still present at logs.go:363-368, and I found no B item tracking it.
- Audit 003 OUT-02: `LogPaths` sorts lexically, so `attempt-000001-resolution-…` lists before `attempt-000001.jsonl`. Still present at logs.go:339, with no tracking item found.
- Audit 003 OUT-11: adapter output flush delay. Delivered (B1135) by flushing on every write, which is the root of [G11-02](#g11-02).
- Audit 003 RCP-03: whether adoption rewrites the attempt record (a spec wording conflict; not re-reported).

<a id="lane-g12"></a>

### Lane G12: Lifecycle: planning, registration, decision, requests, admin credentials (Go)
Scope read: I read all 31 files in scope fully: planning, registration, register_run, decision, requests, contracts, operation_plan, authorization, claims, commands, frozenbinding, lineage, lineage_removal, transitions, preparation, automatic_preparation, service, invocation, additive, projection, produced, produced_inputs, rebind, other_build, outdated_clients, installed_placement, binding_proof, handoff, access_tool, admin_credentials, admin_password. I read admin_credentials_test.go fully. I skimmed registration_test.go (the 42 test names and lines 272-361) and the test names in transitions_test, authorization_test, controller_precursor_test, request_templates_test and preparation_test. I also read outside the lane to check claims: execution.go Apply/mutate, finalization.go joinBindings, stage_tails.go, cmd/bootwright/wiring_reconciliation.go:383-409, contextfs Evidence(), and the ceph and agentinstall AdminCredential* methods. I did not read the other 100 or so test files, because they belong to the other lane's files or I only needed their names.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 3 | Careful re-proof under the exclusive lock, but one credential read error is misclassified and one constructor dependency is dead |
| Security | 4 | Re-checks the basis inside the transaction, scopes material lending and pins host keys; the two admin-credential paths have drifted apart |
| Efficiency | 3 | One decision compiles the same revisions and plans the same state several times, and reads custody twice |
| Clearness | 3 | The rationale comments are good, but six functions in scope have complexity 23-31 and mix phases |
| DDD | 3 | `Service` is a god object (224 methods); the secret port hides optional extensions behind type assertions; preparation is a hidden mode flag |
| Non-duplication | 3 | Pending-update validation, produced-material reads and lending each exist twice |
| Right language | 5 | Pure Go control-plane policy with nothing embedded from another language |
| Test coverage | 4 | 90.3% for the package, with strong fault-injection tests; admin-credential refusals and PlanPreparation are thin or untested |

#### Findings

##### G12-01

**Medium · Quality · confidence high**

- **Where:** internal/reconciliation/lifecycle/admin_credentials.go:189-196, admin_credentials.go:282-287
- **What:** `adminProduced` returns `found=false` together with an error. The "missing" check runs before the error check, so for an updater whose `AdminCredentialMayBeMissing()` is false, any custody read failure is reported as a missing credential. That covers corrupt or crypto store errors and also context cancellation.
- **Evidence:** `active, activeFound, err := read(basis.output)` … `if !activeFound && !basis.updater.AdminCredentialMayBeMissing() {` → `failure("access.unavailable", "the original administrator credential is missing", ...)`. Ceph returns `false` (internal/storage/ceph/admin_credentials.go:103).
- **Impact:** For Ceph, a cancelled or corrupt-keyring read yields a wrong code and a wrong remedy ("restore the original credential custody") and hides the real cause.
- **Fix:** Return `err` first when it is non-nil, then judge `activeFound`. (effort S)
- **Checked:** The caller wraps nothing. The test double always returns `MayBeMissing() == true` (admin_credentials_test.go:136), so this branch never runs in tests.

##### G12-02

**Medium · Efficiency · confidence high**

- **Where:** decision.go:91, decision.go:126, additive.go:75-95,137, preparation.go:216-222, decision.go:324-328, service.go:144, planning.go:35-36
- **What:** With no verb given (plan), `decideFrom` runs `s.additive(..., true)` only to choose the verb and drops its error; `decideBy` then runs it again. Each run compiles the frozen revision and the current input (`compiledBoth`) and plans both (`planFrom` at additive.go:95 and :137). With `proveCustody=true` each run also decrypts produced material from custody (`custodyProved` → `lend`). `settleApply` → `namedChange` compiles both revisions again, and `Plan` compiles once more for PlanInspection. `preparationAddition` compiles the current input twice: line 216, then again inside `compiledBoth`.
- **Evidence:** decision.go:91 `admitted, _ := s.additive(ctx, view, store, operation, frozen, states, true)`; decision.go:126 `admitted, refused = s.additive(ctx, view, store, operation, frozen, states, true)`. The comment at planning.go:35 says "a decision never compiles the same revision twice".
- **Impact:** One plan or apply over a changed input compiles the same two revisions four to six times, plans four times and opens the keyring twice. Cost grows with inventory size, and secret material is unsealed more often than needed.
- **Fix:** Compute the additive admission once in `decideFrom` and pass it to `decideBy`. Memoize compilation per decision, keyed by the sources digest (`digestOf`). (effort M)
- **Checked:** The compiler has no cache (`compileSources` calls `s.compiler.Compile` directly). No backlog item tracks this.

##### G12-03

**Medium · Non-duplication / Security · confidence high**

- **Where:** admin_credentials.go:213-233, admin_password.go:29-47, admin_credentials.go:242, admin_password.go:21,29
- **What:** The rule "is the pending update valid and already applied" is implemented twice, and the two copies differ:
- The update path compares the password with `==` on a Go string; the read path uses `subtle.ConstantTimeCompare`.
- The update path decodes the updater's preparation with lenient `json.Unmarshal`, while the pending record uses `canonicaljson.DecodeClosed`.
- `AdministratorPassword` hard-codes `"admin-password"` and `"-update"`, duplicating `AdminPasswordOutput` in ceph and agentinstall instead of asking the updater's `AdminCredential` for the name.
- **Evidence:** admin_credentials.go:226 `completed := string(password) == update.Password` versus admin_password.go:42 `subtle.ConstantTimeCompare(password, []byte(update.Password)) == 1`; admin_credentials.go:242 `json.Unmarshal(data, &update)`.
- **Impact:** A change to the pending-update contract must be made in two places in two styles, and a capability whose output name differs is silently misread.
- **Fix:** Extract one pure `pendingUpdate(active, pending) (adminUpdate, state, error)` used by both paths. Decode the preparation with `DecodeClosed`. Take output names from the updater. (effort S)
- **Checked:** knowledge/admin-console-credentials.md explains the retained candidate design but not this split. No B item tracks it.

##### G12-04

**Medium · DDD · confidence high**

- **Where:** contracts.go:129-138, admin_credentials.go:276, admin_password.go:14, produced.go:61, produced_inputs.go:157,174, rebind.go:47,146
- **What:** The `SecretBinder` port declares 8 methods. The service then upgrades it at runtime with `s.binder.(ProducedBinder)` (5 sites) and `s.binder.(CredentialRebinder)` (2 sites), with no compile-time assertion anywhere. Admin credentials, produced lending and rebind therefore depend on methods that `New` does not ask for.
- **Evidence:** admin_credentials.go:276 `reader, ok := s.binder.(ProducedBinder)` → `failure("access.unavailable", "produced credential custody is unavailable", ...)`. produced.go:62 `if !reads { return nil }` silently skips the identity check in `sameProducer`.
- **Impact:** A binder that misses either extension compiles and only fails, or skips an identity check, at run time.
- **Fix:** Fold `ProducedBinder` and `CredentialRebinder` into the port the service takes, or take them as typed constructor arguments. (effort S)
- **Checked:** The production binder is `*custody.Service` (wiring_reconciliation.go:383), which implements both, so this is latent rather than live. Capability optional interfaces (about 30 type assertions) are a separate, documented design and are not counted here.

##### G12-05

**Medium · DDD / Clearness · confidence medium**

- **Where:** service.go:33-79, preparation.go:87-98, contracts.go:477-514
- **What:** `Service` has 224 methods over 66 production files, 10 fields, a 27-field `Options` and a 9-parameter `New`, and acts as the planner, registrar, executor, status reader and admin-credential use case at once. contracts.go declares 56 interfaces with 130 methods, including the 27-method `OperationStore`, which every path consumes whole even though `recordReader` (7 methods) shows that readers need far less. Preparation is a mode set on a copied value (`s.preparation = &scope`, plus an overwrite of `s.options.EffectBearing`) and read in 20 places across 6 files.
- **Evidence:** preparation.go:95-97 `s.preparation = &scope` / `s.options.EffectBearing = definition.Bearing`; planning.go:165 and decision.go:127 branch on `s.preparation != nil`.
- **Impact:** Every change in this area costs more, because one value carries every use case's dependencies and a hidden mode changes planning, unsupported refusals and additivity.
- **Fix:** Split out an admin-credential service and a pure decision component that takes narrow read ports. Pass the preparation scope as an explicit decision input. (effort L)
- **Checked:** The planner/executor partition and consumer-owned ports are deliberate. This finding is about how wide one consumer is, not about repeated ports.

##### G12-06

**Low · Quality · confidence high**

- **Where:** service.go:67,77, contracts.go:106-108
- **What:** The `Inputs` port is stored as `Service.inputs` and never read anywhere in the package. Composition still requires it: `wireLifecycle` returns a zero `Service` when `deps.Inputs == nil`.
- **Evidence:** grep for `.inputs` and `ReadInputs` outside tests finds only the declaration and `candidateView.inputs`.
- **Impact:** Dead constructor coupling; a missing unused dependency disables the whole lifecycle.
- **Fix:** Remove the port, the field and the parameter. (effort S)
- **Checked:** `available()` does not check it either.

##### G12-07

**Low · Security / Non-duplication · confidence medium**

- **Where:** admin_credentials.go:275-288 and :336-349 versus produced_inputs.go:97-170
- **What:** `adminProduced` duplicates `producedInTransaction`. The admin `LendProduced` re-implements lending with weaker scoping: it checks kind and object but not the output name, so any output of the block can be borrowed, including the pending `-update` candidate. It also does not track lent material for release, unlike `producedLender`, which clears on release.
- **Evidence:** `if input.Kind != basis.block.Kind || input.Object != basis.block.Object {` … `return held.Material, nil`.
- **Impact:** Clearing the plaintext depends entirely on each updater, and the output set an updater may borrow is not declared.
- **Fix:** Build the admin execution's lender with `producedLender` over the block's declared produced inputs. (effort S)
- **Checked:** Only the two updaters (ceph, agentinstall) call it today.

##### G12-08

**Low · Security · confidence medium**

- **Where:** admin_credentials.go:30, :226, :249, :260, :270
- **What:** `adminUpdate.Password` is a `string`. The plaintext is therefore copied into immutable memory at decode, at the `==` comparison, in `json.Marshal`'s pooled buffer, and in `[]byte(update.Password)`. The surrounding `clear()` calls only clear some of the copies.
- **Impact:** This undercuts the "Secret bytes stay in bounded memory" intent (specs/security.md:143-147) that the code otherwise follows.
- **Fix:** Hold the password as `[]byte` and encode the record with a buffer that is cleared afterwards. (effort M)
- **Checked:** The other material paths in the lane use `secrets.Material` and `[]byte` correctly.

##### G12-09

**Low · Quality · confidence high**

- **Where:** planning.go:76-82, lineage_removal.go:24 vs :62, invocation.go:208 vs :217, admin_credentials.go:327
- **What:** Aliasing discipline is uneven at hand-off points:
- `DependOnController` (exported) writes into the caller's backing array, while its siblings `dependOnStageTails` and `dependOnProducedInputs` clone first.
- `removalOver` aliases `applied.Bindings` and the `Contracts` map, while `replacementOf` clones `Bindings`.
- `RunFor` clones `Refusals` but passes `Values` and `Canonical` through as is.
- `Proved` and `ApplyProved` share one slice.
- **Evidence:** lineage_removal.go:24 `release: applied.Bindings` vs :62 `release: slices.Clone(removal.Bindings)`.
- **Impact:** Safety depends on every caller cloning, which executor.go:66,92 do today.
- **Fix:** Clone at the producer side consistently. (effort S)
- **Checked:** No live mutation found: `joinBindings` callers clone, and `View.Evidence` clones (contextfs).

##### G12-10

**Low · Clearness · confidence high**

- **Where:** admin_credentials.go:177, planning.go:134, decision.go:115, preparation.go:207, admin_credentials.go:102, automatic_preparation.go:14
- **What:** Six complexity hotspots (gometrics-prod.tsv: cyclomatic complexity, nesting depth, statements):

| Function | Complexity | Depth | Statements |
|---|---|---|---|
| `updateAdminCredential` | 31 | 3 | 76 |
| `planFrom` | 27 | 3 | 77 |
| `decideBy` | 27 | 3 | 58 |
| `preparationAddition` | 27 | 2 | 58 |
| `credentialBasis` | 24 | 2 | 53 |
| `automaticPreparation` | 23 | 4 | 46 |

`updateAdminCredential` mixes custody reads, validation, decoding, preparation, publication and the remote update in one body.
- **Impact:** These functions are hard to review, and are exactly where [G12-01](#g12-01) and [G12-03](#g12-03) sit.
- **Fix:** Extract the pending-update resolution ([G12-03](#g12-03)) and the plan-contribution loop of `planFrom` into pure helpers. (effort M)

##### G12-11

**Low · Clearness · confidence high**

- **Where:** handoff.go:27-40, registration.go:137-138, authorization.go:33-36
- **What:** Three small clarity defects:
- `handoff` reports an unknown block twice, and labels a *running* block "has an unresolved unknown outcome".
- `registration.go` names an error `listed` (`claimed, listed := store.Claimed(...)`; `listed != nil`).
- `authorize` keeps the last digest when one token is repeated (`digests[token] = digest`), so a stale `--authorize x=<old>` is silently overridden by a later one.
- **Fix:** Merge the two handoff loops with accurate wording, rename `listed` to `err`, and refuse a token supplied twice with different digests. (effort S)

#### Patterns
- Decision work is recomputed rather than threaded through: the additive admission, compilation and planning (decision.go:91/126, additive.go:95/137, preparation.go:216/222). See [G12-02](#g12-02).
- Optional behaviour is discovered by type assertion on injected ports, so a missing method degrades at run time (produced.go:61, rebind.go:47, binding_proof.go:29 with an unexported `bindingProver` defined outside contracts.go).
- Remedy strings are built by concatenating command fragments inline in many places (commands.go, decision.go:351-396, registration.go:225-242). Every refusal names an exact command, but the per-site string assembly is what audit RCS-06 caught dropping flags.

#### Strengths
- Every fresh-apply step is re-proved under the exclusive lock: basis, input digest, evidence bytes and the claimed-directory set (register_run.go:144-171, registration.go:122-175). It is backed by very thorough fault-injection tests (registration_test.go, 42 tests).
- Template-delimiter and ansible-reserved-key refusal runs at plan time, with deterministic sorted diagnostics (planning.go:456-578).
- Determinism is maintained: keys are sorted in `collectTemplateHits`, `digestOf` sorts its parts, `refuseOwnSocketConflicts` orders hosts explicitly, and `recorded()` clones maps so a basis shares nothing.
- `classify` and `chosenVerb` hold the state machine in one small table-like function with a dedicated test (transitions.go, transitions_test.go).
- Admin credential update is crash-safe: the candidate is published before the remote update, retried on a lost reply, and `UpdateAdminCredentials` checks the basis again inside the transaction (admin_credentials.go:84-91).

#### Coverage notes
- admin_credentials.go:177 (75%). These branches are untested: the `MayBeMissing()==false` path (:194; the test double always returns true), the identity mismatch (:197, :208), the oversized and corrupt pending record (:215, :221), the "active credential changed during its pending update" refusal (:229), and the invalid-preparation refusals (:243, :247). All of them are custody-safety refusals.
- admin_credentials.go:312 (44%). The `LendProduced` refusals (:337-347), for unrelated produced access and for unavailable or unproved custody, are never exercised.
- admin_credentials.go:102 (73%) and :42. The external-cluster refusal (:126), the not-done proof (:165), the basis-changed refusal in the transaction (:89-90), binding reopen (:66-73) and a declined confirmation are untested. TestAdminCredentialUpdateRefusesBeforeAnApplyOrForAnUnknownCluster asserts only `err == nil` (admin_credentials_test.go:127), not which refusal.
- preparation.go:72 `PlanPreparation` is at 0% across the module but live: cmd/bootwright/wiring_discovery.go:77 calls it for the discovery preview, the area of the current commit.
- preparation.go:207 (70%). The revision-compare refusal (:224), the unregistered or incomplete-owner refusal (:256) and an `Advance` error are untested.
- lineage_removal.go:53 `unremovable` and admin_password.go:52 `AdminCredentialProved` are at 0% across the module.
- invocation.go:175 `RunFor` (64% own, 93% any). The case where both `Prepare` and `PrepareRemoval` are set, where the removal preparation silently replaces the apply one, has no test.

#### Already recorded
- RCS-06 (audit 003): rebind-related remedies drop `--authorize rebind=` and `--unregistered` flags. It is adjacent to rebind.go and the remedy-string pattern; I have not restated it.
- No B<n> item or audit-003 ID matches [G12-01](#g12-01) to [G12-11](#g12-11). I grepped backlog.md and the audit reports for admin_credentials, AdministratorPassword, compile twice, ProducedBinder and type assertion.

<a id="lane-g13"></a>

### Lane G13: Lifecycle: attempts, execution, scheduler, removal, status, diff (Go)
Scope read: I read all 35 production files in full: attempts, execution, execution_contracts, executor, scheduler, scheduler_priority, runtime, finalization, evidence, reproof, settle, removal_evidence, removal_gates, removal_phases, removal_plan, unresolved, contradictions, status, status_live, status_readiness, status_resources, diff, diff_attribution, diff_compare, diff_observe, diff_overlay, logging, online_gates, presentation, remedies, secret_holds, secret_references, shared_readings, stage_tails, support. I sampled tests: scheduler_test.go (around :900-935), journey_test.go (:2656-2700 and :3095-3140), kill_harness_test.go (:1-60 and :985-1060), plus greps across every `_test.go` for sleeps, `t.Parallel` and fixtures. Read outside the lane only to check claims: lineage.go, additive.go, operationstore/logs.go and store.go, libvirt/ceph `Quiescent`, and spec sections "Execution"/"Selection" (state-reconciliation.md:1115-1205).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful outcome recording and joins; a few swallowed read errors |
| Security | 4 | Destructive steps are gated on positive proof and diff cannot cause effects; raw `err.Error()` can reach diff output |
| Efficiency | 3 | Quiescence probes run serially, and lineage is re-read for every attempt |
| Clearness | 3 | 11–18-parameter functions sitting at the 100-line cap, and a 28-field diff accumulator |
| DDD | 4 | Policy stays in the engine behind capability ports; plan-graph closure is re-derived here instead of in `reconciliation` |
| Non-duplication | 3 | Second hand-rolled scheduler, three dependency-closure loops, duplicated record-selection helpers |
| Right language | 5 | Pure Go engine logic; nothing embedded from other languages |
| Test coverage | 4 | 90.3% own coverage and a kill-at-every-write harness; 0% keyed-list diff and rank helpers |

#### Findings
##### G13-01

**Medium · Efficiency · confidence high**

- **Where:** internal/reconciliation/lifecycle/removal_gates.go:242, internal/substrate/libvirt/capability.go:642
- **What:** `proveQuiescent` probes every block one at a time inside one `withPython`. Each Machine probe runs its own adapter process, so N machines cost N sequential Ansible runs before any removal registers, and again at each online or pre-stop boundary (online_gates.go:44, :76).
- **Evidence:** `for index, block := range blocks { ... state, err := capability.Quiescent(ctx, Probe{...})`; libvirt `result, err := c.run(ctx, execution, "observe", request)`.
- **Impact:** Destroy latency grows linearly with fleet size on a read-only gate, which goes against the owner's "parallel by default" rule.
- **Fix:** Fan the probes out under `s.Concurrency()`/`s.Throttle()`, collect per-index results, and fold `live`/`stops`/`remedies` in plan order so the refusal text stays deterministic (effort M).
- **Checked:** The spec only requires that every block is probed, not that they run in order. No backlog item covers this; B570 and B567 touch `proveQuiescent` for other reasons.

##### G13-02

**Medium · Non-duplication · confidence high**

- **Where:** internal/reconciliation/lifecycle/diff_observe.go:147-187, scheduler.go:183-194,296-307,385-394
- **What:** `inspectAll` is a second hand-built bounded scheduler: its own `running` counter, `throttled` map, buffered channel and cancel drain. It repeats the scheduler's bound and throttle rules, and [G13-01](#g13-01) would need a third copy.
- **Evidence:** `if ctx.Err() != nil || running >= s.Concurrency() || block.Throttle != "" && throttled[block.Throttle] >= s.Throttle() {`
- **Impact:** Admission rules for read-only fan-out can drift from the effect scheduler. They already have ([G13-03](#g13-03)).
- **Fix:** Extract one small ordered, throttle-aware fan-out helper for read-only work and use it for diff observation and quiescence (effort M).
- **Checked:** Both copies are correct today. Each goroutine sends exactly once into a channel sized to the work, and the loop drains until `running==0`.

##### G13-03

**Low · Quality · confidence medium**

- **Where:** internal/reconciliation/lifecycle/scheduler.go:202-207, :324-326
- **What:** Resolutions are admitted without `c.free(block)`, so they ignore exclusive claims and group throttles. They still increment `throttled`, which can go above capacity. Diff observations of the same blocks do honour the throttle (diff_observe.go:158).
- **Evidence:** `case kind == StepResolve: for _, index := range admitted { if !c.running[...] { return c.plan.Blocks[index], index, true }`
- **Impact:** Up to the running bound (8) observations can hit an endpoint the throttle exists to protect, such as BMC/Redfish sessions.
- **Fix:** Apply the throttle check (not the exclusive check) to resolutions, or record in the spec why observations are exempt (effort S).
- **Checked:** The spec allows "several unproved effects may be observed together" (state-reconciliation.md:1195). It does not exempt them from throttles (:1122-1124), hence medium confidence.

##### G13-04

**Medium · Clearness · confidence high**

- **Where:** internal/reconciliation/lifecycle/attempts.go:39, :209, :439; scheduler.go:108, :133
- **What:** Each invocation's context is passed positionally on every call: tx, store, approved, boundary, operation, plan and material. `invoke` takes 18 parameters, `resolveUnknown` 16, and `attempt` and `observe` 13 each. `attempt` and `invoke` are exactly at the 100-line fitness cap with complexity 26 and 24.
- **Evidence:** gometrics: `Service.invoke 100 lines, cx 24, 18 params`; `Service.attempt 100, cx 26, 13 params`. There is also a boolean trap: `removal bool, removalPlan reconciliation.Plan, retired map[string]bool`.
- **Impact:** Every change to the attempt path edits long, same-typed argument lists. The scheduler struct already holds all of these values.
- **Fix:** Introduce an `invocation` value (tx, store, approved, boundary, operation, plan, material) owned by the scheduler, plus a `removalReading{plan, phases}` in place of the bool triple (effort M).
- **Checked:** The project's own policy forbids only length, not parameter count; the brief lists parameter lists as a clearness concern.

##### G13-05

**Medium · Test coverage (efficiency) · confidence medium**

- **Where:** internal/reconciliation/lifecycle/*_test.go; scheduler_test.go:904,927; kill_harness_test.go:1000,1058
- **What:** Across 731 `Test` functions there is no `t.Parallel()`. Tests use in-memory fakes; no `TempDir` was found, so isolation looks feasible. There are fixed waits: `heldInstallationWindow = time.Second`, a 200 ms wait in scheduler_liveness_test.go:183, and sleeps in diff_observe_test.go:887,1038 and shared_readings_test.go:35. The kill harness replays every journey once per durable write, all serially.
- **Evidence:** `grep -c t.Parallel → 0`; `for index, key := range names.keys { killed := journey.begin(ctx, t)`
- **Impact:** These are the most likely causes of the 58 s package time. It cannot be confirmed without per-test timings.
- **Fix:** Mark harness-isolated tests and kill-journey subtests `t.Parallel()`, and replace the 1 s window with a release channel (effort M).
- **Checked:** No shared package-level mutable state appeared in the sampled fixtures. A `go test -json` timing would settle what actually dominates.

##### G13-06

**Low · Non-duplication / DDD · confidence high**

- **Where:** evidence.go:186-200, removal_evidence.go:30-44, removal_evidence.go:110-124; evidence.go:230-253 and removal_evidence.go:77-93
- **What:** The transitive dependency closure is coded twice as a BFS, and the reverse closure (dependents) once more as an O(n²·k) fixpoint. `applyPreparations` and `attemptPreparations` contain the same loop over attempts.
- **Evidence:** `pending := slices.Clone(plan.Blocks[index].Dependencies); for len(pending) != 0 {` and the same lines again in `removalRecords`.
- **Impact:** Plan-graph policy lives in an adapter-facing package instead of `reconciliation.Plan`, which has no closure helper (plan.go:161-214).
- **Fix:** Add `Plan.DependencyClosure(id)` and `Plan.Dependents(id)` and have one `preparations(store, op, block)` helper (effort S).
- **Checked:** Not reported in the clone data. The structures are identical apart from names.

##### G13-07

**Low · Non-duplication · confidence high**

- **Where:** evidence.go:279-289, attempts.go:922-932, unresolved.go:134-139, attempts.go:851
- **What:** The rule "latest attempt, superseded by its last resolution" is re-implemented in four places.
- **Evidence:** `resolution, resolved, err := store.LastResolution(ctx, operation, block.ID, record.Attempts); if resolved { settled = resolution }`
- **Impact:** If the supersession rule changes, all four copies must change together.
- **Fix:** Add one `settledRecord(ctx, store, op, block)` helper (effort S).

##### G13-08

**Low · Security / Non-duplication · confidence medium**

- **Where:** diff.go:292-297, execution_contracts.go:57-62, diff_observe.go:292,357; contrast scheduler.go:445-460
- **What:** `firstMessage` and `contractMessage` are identical, and both fall back to `err.Error()`. The diff result's reasons are built from them. The scheduler deliberately never surfaces raw failure text, because it "may carry a private path or a raw store message".
- **Evidence:** `return err.Error()`
- **Impact:** Undiagnosed store or filesystem errors can reach `diff` output verbatim.
- **Fix:** Keep one helper that maps an undiagnosed error to a fixed `runtime.internal` message (effort S).
- **Checked:** Most errors are typed diagnostics, so exposure is limited to the fallback.

##### G13-09

**Low · Quality · confidence medium**

- **Where:** diff.go:173, status_readiness.go:27-29, status.go:266-269, status.go:322-325
- **What:** Read paths drop errors. `added, _ := s.additive(...)` makes a store read failure read as "this input adds nothing" (`adds:false`). `nameReadiness`, `LogPaths` and `attemptWarnings` silently skip failures.
- **Evidence:** `added, _ := s.additive(ctx, view, store, read.owning, read.owningPlan, read.owningStates, observed)`
- **Impact:** `diff` can return a confident wrong answer after an I/O error.
- **Fix:** Propagate the error, or attach a warning diagnostic when `additive` fails for anything other than a refusal (effort S).
- **Checked:** `additive` does return refusals that are legitimately "no addition" (additive.go:88). Separating the two needs `diagnostics.Of`.

##### G13-10

**Low · Efficiency · confidence medium**

- **Where:** attempts.go:48,55,497; evidence.go:140,175,231; removal_evidence.go:26,99; status.go:394,413,458-474; status_resources.go:96,101
- **What:** Each attempt re-reads its lineage several times: `provedDependencies` calls `lineageOf`, `applyDependencies` calls `lineageOf` again, `applyPreparations` re-reads the operation, `populateRemovalExecution` calls `sourceLineage` twice, and `ownerID` runs again in `invoke`. `status` calls `locateRuntime` up to three times, and `storageResources` calls `appliedView` twice.
- **Impact:** Store reads scale with blocks times lineage depth. The cost is modest on local files but avoidable.
- **Fix:** Compute lineage once per invocation under the held lock and carry it in the [G13-04](#g13-04) struct (effort M).
- **Checked:** Predecessor records are immutable while the exclusive transaction runs.

##### G13-11

**Low · Clearness · confidence high**

- **Where:** diff.go:32-62
- **What:** `diffRead` is a 28-field accumulator. It is filled by `readDiff`, mutated by `observeDiff`, `foldObjects` and `attribute` (`r.patches`), then rendered.
- **Impact:** It is hard to tell which fields are valid at which phase.
- **Fix:** Split it into records, sides and observation values that are returned rather than mutated (effort M).

#### Patterns
- A missing block state is defaulted to pending by hand in at least eight places. `recordFromBlocks` (removal_gates.go:145-152) is a verbatim copy of `orderedStates` (finalization.go:289-299). Others: status.go:118, status.go:704, contradictions.go:80, attempts.go:756.
- The read sequence Index → ReadOperation → ReadPlan is repeated in 17 functions. The clone data pairs evidence.go:68-79, status.go:91-102 and status_resources.go:79-90.
- Near-twin gates: `proveOnlineBoundary` and `provePreStopBoundary` share the same lineage, probe and latch tail (online_gates.go:42-52 vs :74-84). `adapterWarning` maps five codes to themselves through a switch (attempts.go:888-901) where a set would do.
- Recursive api.Value walkers repeat the same Mapping/keyed-Sequence skeleton: `overlay` and `realized` (diff_overlay.go:111-174), `uncompared` and `leaves`, and `differingPaths`. `childShape` exists both in lifecycle/projection.go:14 and desiredstate/encoding/encoding.go:117.

#### Strengths
- Scheduler goroutine discipline: a result channel sized to the plan, one send per goroutine, a drain until `running==0` even on cancel, scheduler maps touched only on the loop goroutine, causes joined in frozen plan order, and warnings sorted by position. What gets recorded is deterministic.
- Outcomes are written under `context.WithoutCancel` (`recordingContext`). The log boundary latch is mutex-guarded, cancels in-flight work, and records the fault on a context that cancellation cannot suppress.
- An unknown outcome never reads as absence. Invalid effects become unknown, and a log-open failure records the attempt as Unknown. A removal check never proves an apply done (`removalEffect`). An inconclusive outcome is replayed only once and only when replay-safe. `rereadBeforeRemoval` refuses a second unreadable target. Quiescence treats an unreadable probe as live.
- Diff is read-only by construction. `readOnlyStage` refuses every host effect, the basis and host binding are re-proved under the run lock, material is cleared, and warnings carrying lent material are dropped.
- The kill harness kills each journey at every durable write and checks that a retry converges within the spec's bound, with a ledger tied to backlog items.

#### Coverage notes
- `differingItems` (diff_compare.go:112) has 0% coverage. Keyed-list field diffs feed the additive change descriptions, so a wrong key would name the wrong fields unseen.
- `realizationRank` (diff.go:387) has 0% coverage. The worst-of fold of `carriedRealizations` over several hosts is never exercised.
- `SecretHolds` (secret_holds.go) is at 66%. The branch for a destroy-headed lineage feeds custody holds, so it is security-relevant.
- `verifyHostBinding` (reproof.go:228) is at 75%, with 5 of 20 statements unrun. Its host-mismatch refusals are the guard against running on the wrong host.
- `Service.status` (status.go:202) is at 79% and `Ownership` (status.go:81) at 74%. The gaps are mostly error returns and the release-recording path of an incomplete removal.
- `diffRead.blocksOf` (diff.go:545) is at 60%. The fallback to the owning apply's blocks is untested.
- `proveQuiescent`'s merge of remedies and stops (removal_gates.go:298-315) has no test that pins the combined remediation text.

#### Already recorded
- B570: `proveQuiescent` runs after removal checks that observe nothing (different defect; I am not reporting it again).
- B567: libvirt host `Quiescent` is unconditional (different defect).
- Audit 003 OUT-01 (attempts.go `blockResults`, which omits outcome and attempts) and RCC-08 (exhaustion test gap): neither overlaps any finding above.

<a id="lane-g14"></a>

### Lane G14: Machine: admission, access, power, disk selection, hardware, inventory, direct SSH

Scope read: I read these in full: sshlocal (argv.go, policy.go, hostkey_policy.go, launcher_linux_amd64.go, copy_linux_amd64.go, copy_worker.py), access (service.go, resolve.go, hostkey.go, copy.go, several.go, prefix.go), diskselection (all 3 production files), hardware (all 4), inventory (service.go, selection.go), power (service.go, batch.go, read.go, selection.go, evidence.go), and in the machine root erase_admission.go, clear_admission.go, session.go, ssh.go, hostkey.go, copy.go, identity.go, power.go, selection.go, ownership.go, hardware.go, sshhost.go, target.go, reach.go. Skimmed: admission.go (lines 1-340 and 956-1015, plus the function inventory), placement.go, power/hosted.go and catalog.go through their callers; for tests, copy_test.go, selection_test.go, the sshlocal test inventory and copyFixture. Not read: nmstate.go, segments.go, network_membership.go, disk_refinement.go and the rest of admission.go. These are network admission, and there was not time to read them after the safety-critical paths. I did not run anything; every claim comes from reading the source.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 3 | Careful fail-closed code, but single-Machine `machine cp` with key auth cannot work |
| Security | 4 | Exemplary pinned-client and descriptor-relative copy boundary; the crypto-policy rule differs across languages |
| Efficiency | 3 | Copy preflight and power reads run serially, and `ssh -G` is re-evaluated per session |
| Clearness | 3 | Long boolean predicates (Select, validateErase, validCopyManifest) and bare worker tokens in errors |
| DDD | 3 | The machine root mixes the Machine model with context selection, ownership vocabulary and CLI operand parsing |
| Non-duplication | 3 | Disk identity grammar in 4+ places, crypto-policy reducer in 3, and the access preamble cloned |
| Right language | 4 | Embedding the Python worker is justified (D236), but it sits outside every Python gate |
| Test coverage | 4 | Strong sshlocal and power suites; refusal tests are weakly specific and the key branches are untested |

#### Findings

##### G14-01

**High · Quality · confidence high**

- **Where:** internal/machine/access/copy.go:69-76, internal/machine/access/several.go:167, internal/secrets/values.go:46
- **What:** For a single Machine, `Copy` builds its `member` without `privateKeyPart`. `prove` then asks the bound key Secret for the empty part `""`, which no Material holds, so any key-authenticated single-Machine copy refuses. That covers every installed Machine (fleet key) and every `privateKeyRef` Machine.
- **Evidence:** `members = []member{{target: resolved, identity: identity, privateKeyRef: ref}}` (copy.go:75). Compare `members(...)`, which sets `privateKeyPart: privateKeyPart(catalog, privateKeyRef)` (several.go:130,153). `privateKey` then returns `"... carries no private half"` when `bound.Part(part)` misses (hostkey.go:218-220).
- **Impact:** The common case, `machine cp --name <one>` on a key-auth Machine, always fails with `access.unavailable`; only operatorIdentity and password single copies work.
- **Fix:** Build the single member through the same helper as `members()`, or call `members` with one name and keep the password exception (effort S). Add a single-Machine key-auth Copy test.
- **Checked:** all six `Copy` tests use two or more names, or end before `prove` with a refusal. `open()` passes the part explicitly (service.go:109). No backlog item or audit entry covers it; B1189's real-machine acceptance is still owed.

##### G14-02

**Medium · Non-duplication / Security · confidence high**

- **Where:** internal/machine/sshlocal/policy.go:87-92; ansible/.../module_utils/ssh_key_policy.py:69-74; ansible/.../module_utils/ceph_tunnel.py:82-84
- **What:** Three implementations reduce the same `/etc/crypto-policies/back-ends/openssh.config`, and they disagree. Go silently drops a directive that is neither crypto nor "unsafe" and keeps values unchecked. Both Python reducers refuse any non-crypto directive and enforce a value grammar.
- **Evidence:** The Go switch has only `case cryptographicDirectives[keyword]` and `case unsafeDirectives[keyword]`, with no default. Python: `if match is None or (... not in DIRECTIVES and not disabled_gssapi): raise PolicyError(...)`.
- **Impact:** On one host the same policy file can open `machine rsh/exec` yet refuse Ceph and placement sessions, or the reverse. Audit SEC-07 was this kind of drift.
- **Fix:** Pick one rule (refuse unknown, or drop unknown) in the security spec. Pin both languages to a shared fixture table of policy lines and their expected verdicts (effort M).
- **Checked:** spec security.md:709-712 says "only allowlisted cryptographic directives" and does not settle unknown-directive handling. SEC-07 (GSSAPIKeyExchange) is fixed in all three.

##### G14-03

**Medium · Non-duplication · confidence high**

- **Where:** internal/machine/erase_admission.go:49-59; internal/machine/clear_admission.go:19,56; internal/machine/diskselection/selection.go:16,21; internal/managedos/installation/selection.go:637; internal/containercluster/agentinstall/requests.go:362
- **What:** The disk-identity grammar is redefined everywhere. The serial regex `^[A-Za-z0-9._:+/=-]{1,256}$` appears three times in Go (plus once in kickstart shell). The 64 TiB / 4096-aligned capacity rule appears as `MaxBytes`, `MaxClearBytes`, two `64*1024*1024*1024*1024` literals and one `64<<40`. Erase admission alone uses a looser "printable ASCII" serial rule.
- **Evidence:** erase: `valid := value != "" && len(value) <= 256 && ...; character >= 0x20 && character <= 0x7e`. clear: `!clearSerial.MatchString(serial)`.
- **Impact:** An erase target serial containing spaces, quotes or `<>&` passes admission, while the same disk would be refused as a clear target or a root. That is an inconsistent identity contract on a destructive path.
- **Fix:** Export one `diskselection` identity validator (serial, WWN, size) and use it in erase, clear, root and agent admission (effort S-M).
- **Checked:** `ValidDiskSerial` exists (serial_admission.go:17), but neither erase admission nor `erase_request.go` uses it. Audit CPD-03 concerns `validateSelectingSerials`, a different defect.

##### G14-04

**Medium · DDD · confidence medium**

- **Where:** internal/machine/selection.go:13, internal/machine/ownership.go:7-23, internal/machine/copy.go:14, internal/machine/session.go
- **What:** The machine root is the Machine admission model, but it also holds context selection (`SelectedContext`), the generic per-object ownership vocabulary (`OwnershipState`, `VerbApply`, `Block*`), CLI operand parsing with CLI wording (`ParseCopyPaths`), and SSH session runtime types. Several of the 33 importers want only these items.
- **Evidence:** containercluster/access imports `machine` only for `machine.SelectedContext`; environment/preflight and environment/clusterpreflight only for `SelectedContext` and `SSHOptions`; storage/preflight and containercluster/preflight use `OwnershipState`/`Block*` for non-Machine objects.
- **Impact:** The import fan-in overstates the coupling, and every Machine-admission change recompiles and re-gates unrelated contexts. "Machine domain" names no single policy.
- **Fix:** Move `SelectedContext` next to the context-selection owner, `OwnershipState` into reconciliation/lifecycle, and `ParseCopyPaths` into internal/cli (effort M).
- **Checked:** `ownership.go` itself claims "the Machine domain's whole vocabulary" while documenting "one API object". No architecture rule names this placement.

##### G14-05

**Medium · Efficiency · confidence high**

- **Where:** internal/machine/access/copy.go:125-133; internal/machine/power/service.go:122-134
- **What:** Work against different hosts runs one after another although each piece is independent. Every Machine's copy `Prepare` (SSH handshake, policy evaluation, two Python workers) runs serially before the bounded parallel transfer. A power reading runs one Ansible survey per placement host serially.
- **Evidence:** `for i, session := range sessions { p, err := s.options.Copier.Prepare(ctx, session, ...)`; `for index, survey := range surveys { ... s.observe(inner, runtime, survey, step)`.
- **Impact:** Preflight latency grows linearly with the fleet, up to N handshakes before any byte moves.
- **Fix:** Run preflights under the same `maxConcurrentSessions` slots, and gather all results before any `Run` (effort S). The same applies to surveys.
- **Checked:** spec machine-copy.md:40-44 requires preflight of every Machine before payload writes, and bounds transfers only. That is satisfied by a gather barrier; serial order is not required.

##### G14-06

**Low · Efficiency · confidence high**

- **Where:** internal/machine/sshlocal/launcher_linux_amd64.go:133-148, internal/machine/access/hostkey.go:20
- **What:** `ssh -G -F /dev/stdin localhost` runs once per `HostKeyAlgorithms` call and again in every `Run`, although its output depends only on the policy bytes and never on the session.
- **Evidence:** `command := exec.CommandContext(evaluation, client, "-G", "-F", "/dev/stdin", "-o", "CanonicalizeHostname=no", "localhost")`. The session is used only in error text.
- **Impact:** `exec` on N Machines spawns 2N identical evaluations; `cp` spawns about 3N.
- **Fix:** Evaluate once per invocation and intersect per key (effort S).
- **Checked:** `admittedAlgorithms` does the per-key work in pure code.

##### G14-07

**Low · Clearness / Quality · confidence high**

- **Where:** internal/machine/diskselection/selection.go:165,175
- **What:** `Select` (complexity 46) checks the exclusion twice with identical code. It also mixes four phases: validation, protected-set resolution, candidate filtering and the uniqueness verdict.
- **Evidence:** `if excluded(d.DiskIdentity, p.Exclude) || excluded(d.DiskIdentity, protected) { continue }` appears at both 165 and 175.
- **Impact:** Disk choice comes before a destructive install, so dead repetition makes a reviewer wonder whether the order matters.
- **Fix:** Delete line 175-177 and extract `resolveProtected` and `eligible(d)` (effort S).
- **Checked:** nothing between the two checks mutates `d`.

##### G14-08

**Low · Right language / Test · confidence medium**

- **Where:** internal/machine/sshlocal/copy_worker.py; internal/machine/sshlocal/copy_linux_amd64_test.go:22-24
- **What:** The shipped worker must run on Python 3.9 at both endpoints (machine-copy.md:34), but it lives outside the collection. So ansible-check's floor interpreter, sanity and pylint never see it, and it is exercised only by the host's `/usr/bin/python3`. The fixture calls `t.Fatal` when that interpreter is absent.
- **Evidence:** `if _, err := os.Stat("/usr/bin/python3"); err != nil { t.Fatal(err) }`. Neither `scripts/ansible-check` nor `scripts/tools` references `internal/`.
- **Impact:** A 3.10+ construct would pass every gate and fail on a RHEL 9 or 3.9 endpoint.
- **Fix:** Add the file to the floor-interpreter compile/import lane, and lint it (effort S).
- **Checked:** the brief's named-exception list covers shipped workers in the collection, not this Go-embedded one.

##### G14-09

**Low · Security / Robustness · confidence medium**

- **Where:** internal/machine/sshlocal/copy_linux_amd64.go:183, argv.go:135-140
- **What:** The remote worker reaches the host as one 419-line single-quoted word that the remote login shell parses. That assumes a POSIX shell: tcsh refuses a newline inside quotes, and fish treats `\'` differently.
- **Evidence:** `session.Command = []string{"/usr/bin/python3", "-I", "-B", "-c", copyWorkerSource}`, quoted by `quoteWord`.
- **Impact:** Copy fails confusingly for an account whose login shell is not POSIX. The spec names no such requirement.
- **Fix:** Send a fixed one-line bootstrap that execs the source read from stdin, or document the POSIX-shell requirement (effort S).
- **Checked:** copy_worker.py contains no `!`, so csh history expansion is not triggered.

##### G14-10

**Low · Clearness / Non-duplication · confidence high**

- **Where:** internal/machine/sshlocal/copy_linux_amd64.go:95-99; copy_worker.py:406-408
- **What:** The Go allowlist of worker error tokens is a hand copy of the Python reasons and has already drifted. `EOFError`, `unknown-source`, `invalid-manifest`, `duplicate-entry`, `missing-root`, `invalid-frame`, `unknown-destination` and `no-stage` all collapse to "file worker refused the operation". Allowed tokens such as `PermissionError` reach the operator with no path.
- **Evidence:** `allowed := []string{"manifest-too-large", "invalid-path", ...}` sits inline in `receive`.
- **Impact:** Diagnoses get vaguer with each worker change; "copy preflight failed: PermissionError" names no file.
- **Fix:** Have the worker return a closed reason plus the relative entry path, and pin both lists with one test (effort S).
- **Checked:** no test compares the two lists.

##### G14-11

**Low · Non-duplication · confidence high**

- **Where:** internal/machine/access/service.go:74-87 vs several.go:46-59 vs copy.go:46-64; access/several.go:34-43, power/batch.go:29-45, inventory/selection.go:131-147; access/several.go:104-117 vs power/selection.go:53-68
- **What:** The open/several/copy preamble is cloned (this clone is what let [G14-01](#g14-01) diverge). Name-list normalization exists three times with different sort and dedupe semantics. The "declares no Machine named" refusal exists twice with the same text.
- **Evidence:** clone pair `access/service.go:74-87 ↔ several.go:46-59`, from clones-go-prod-exact.
- **Impact:** Fixes land in one copy only.
- **Fix:** Have one `resolveMembers(ctx, names)` in access, and share the undeclared-name refusal (effort S).
- **Checked:** the copies are in sibling packages of one context, so these are not deliberate consumer-owned ports.

#### Patterns
- Long single-line boolean guards carry the safety rules: `validCopyManifest` (copy_linux_amd64.go:310, complexity 28), `validateErase` (erase_admission.go:61-71, 43), and the size checks in `Policy.Validate` (selection.go:89). Named predicates would make each rule reviewable.
- Refusal tests often assert only `err != nil`, so a test passes for the wrong refusal. Examples: selection_test.go:36, selection_test.go:53, and the policy table at selection_test.go:104.
- Policy is also applied to strings without canonical comparison. `installedHostKey` compares `proved.Address != selected.address` raw (access/hostkey.go:92), while `SSHHost` canonicalizes the same addresses.

#### Strengths
- sshlocal's boundary: an absolute qualified client, a `-F` config built from an allowlisted policy via `/proc/<pid>/fd`, unlinked mode-0600 material, a terminal-only environment, a pinned key intersected with `ssh -G`, and `IdentitiesOnly`/`IdentityAgent=none`. Its argv is a pure, tested function.
- The copy worker is genuinely descriptor-relative: every ancestor is opened `O_NOFOLLOW` from `/`, objects are held by `O_PATH` and proved before a read open, and stability tokens are checked before and after reads. It stages privately, publishes by `link`/`replace` and guards cleanup by inode. Tests drive the real worker, including stage substitution.
- Power proves identity before any effect: the controller-host self-check, a pin or a complete declared MAC set required for physical Machines, and evidence rejected unless digest, Machine and postcondition all match. Unreachable or unproved states become `lifecycle.unknown`, never success.
- Disk selection fails closed on unknown transport, identity or predicate, on ambiguous WWN or serial, and on protected claims that do not resolve uniquely, with a canonical fingerprint that binds the policy.
- The offered `--ssh-id-file` is opened under the invoking account and proved on the received descriptor.

#### Coverage notes
- access/copy.go:66-77: the single-Machine Copy branch has no test. One test with a key-auth Machine would have caught [G14-01](#g14-01).
- admission.go:976,986 (`successorLogin`, `successorLoginHolders`, 0% by any test): the refusal of root-login revocation without a successor login is unexercised.
- hardware/capability.go:113 `Destroy`, :78 `Removal`, :233 `Unresolved`: 0% by the package's own tests. Release-claim proof and drift classification are tested only incidentally from elsewhere.
- diskselection/selection.go:169-170: the "unknown selection predicate" refusal, and the positive WWN-only protected claim, have no test. `PolicyFromValue` (authoring.go:11) is 0% own and 69% any.
- diskselection/proof.go:78 `Binding.Validate` is 62% covered; the provenance-binding refusals guard a destructive selection.
- sshlocal/copy_linux_amd64.go:366 `copyOne` is 56%: the lost-commit-acknowledgement ("unknown" status) path is not driven.
- sshlocal/copy_linux_amd64.go:149 `Copier.local` is 71%: the invoking-account mismatch refusal (line 157) is untested.

#### Already recorded
- SEC-07 (audit 003, GSSAPIKeyExchange reducer drift): fixed in policy.go:79-86. [G14-02](#g14-02) is the remaining semantic divergence.
- SEC-01 (audit 003, SSH client qualification): fixed; launcher now uses `nativeexec.Qualify`.
- CPD-03 (audit 003, `validateSelectingSerials`): related area, different defect from [G14-03](#g14-03).
- B544 (password-arm offered-key limitation): referenced by machine-copy.md:38, not re-reported.

<a id="lane-g15"></a>

### Lane G15: Substrate: domain root and the bare-metal, libvirt and KubeVirt arms, capacity (Go)

Scope read:
- **Read fully (45 files):**
  - `libvirt/`: capability, evidence, selection, requests, catalog, claims, projection, capacity, secret_references, disks, precursor, routing, legs, evidence_routing, evidence_carrier, unresolved, vlan_refusal
  - `kubevirt/`: capability, evidence, selection, requests, catalog, contracts, lend, unresolved, pin
  - `baremetal/`: all 9 files
  - `capacity/`: all 4 files
  - root: target, facts, admission, managed_network, preboot, machine_bounds, redfish, firmware, effect, partial, data_disk, nics
- **Skimmed:** root `rootdevice.go`, `naming.go`, `nested.go` (first 60 lines), `segments.go` (41-120).
- **Not read (time budget):** root `network.go`, `nmstate_compose.go`, `addressless_network.go`; `libvirt/evidence_connections.go`, `libvirt/contracts.go`.
- **Tests:** I listed every arm and capacity test name and read `baremetal/capability_test.go` (the presence and absence tests) and the libvirt selection test that compares the target with the request. I checked that no test sleeps, reads the clock or environment, spawns a process or dials the network.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Evidence is strict, digest-bound and fail-closed; minor error-swallowing and latent inconsistencies |
| Security | 4 | One controller-endpoint grammar, identity values bounded and refused by property name, lent tokens revoked under WithoutCancel |
| Efficiency | 3 | Capacity probes run serially at up to 60 s per remote host; some per-Machine catalog rescans |
| Clearness | 3 | Root `Validate` has cyclomatic complexity 37; a misplaced doc comment; bare-metal bounds borrow constants named Libvirt |
| DDD | 3 | The root package carries much libvirt-only admission and naming; "capacity" is libvirt's capacity under a generic name |
| Non-duplication | 2 | The lifecycle scaffolding is copied across 5 capabilities; libvirt derives the emulated controller a second time |
| Right language | 4 | Go only; Redfish timing constants mirror Python by design and a test holds them equal |
| Test coverage | 4 | Mostly behavioural, hermetic tests; gaps in KubeVirt `Unresolved` and capacity own-tests |

#### Findings

##### G15-01

**Medium · Non-duplication · confidence high**

- **Where:** `internal/substrate/libvirt/capability.go:308-423` and `:446-561`; `internal/substrate/kubevirt/capability.go:206-214` and `:459-579`; `internal/substrate/baremetal/capability.go:97-165`
- **What:** Each of the five capabilities re-implements the same lifecycle scaffolding:
- the Plan loop and `Removal`;
- `Apply`/`Destroy` → `mutate` (prepare, run, usable outcome, then the presence or absence check);
- `observe`;
- the five-check effect classification;
- `usableOutcome` (2 copies plus one inline in bare metal);
- `groupsFor` (2 copies plus one inline in bare metal);
- the `"bootwright machine stop --context …"` text (`libvirt/capability.go:641`, `kubevirt/capability.go:675`, `preboot.go:55`).

- **Evidence:** `libvirt.hostEffect` (384-407), `libvirt.machineEffect` (522-545) and `kubevirt.machineEffect` (538-561) are the same body over the same five validators. The kubevirt one's own comment says it reads its evidence "as the libvirt arm reads its own". Measured clones: libvirt with itself, 68 lines; kubevirt with libvirt, 32 lines; bare metal with each of the other arms, 25 lines.
- **Impact:** Any change to outcome or effect semantics (for example B606) has to be repeated in up to five places, and the arms have already drifted: kubevirt sets `Outcome: Unchanged` on a completed observation (capability.go:575-577) and libvirt does not.
- **Fix:** Add a generic helper in `reconciliation/lifecycle` (the root `substrate` package does not import lifecycle). It would take an `EffectChecks{Presence, Absence, Partial, Unremoved, Unfinished}`, provide `ReadEffect(verb, checks)`, and run mutate and observe over decode, run and validate callbacks, with a post-check hook for kubevirt's `samePin`. The arms keep their request shapes, validators, `Quiescent` and claims. (effort M)
- **Checked:** The consumer-owned-port policy covers port declarations, not implementation bodies. The kubevirt `samePin` and the bare-metal claim-only removal are real differences, which is why the helper needs a hook.

##### G15-02

**Medium · Non-duplication · confidence high**

- **Where:** `internal/substrate/libvirt/selection.go:170-175`, `:220-235`; `internal/substrate/target.go:232-254`; `internal/substrate/naming.go:107-139`
- **What:** The libvirt machine request derives the emulated controller itself: port = base + ordinal, its own credential and certificate refusals (same text as `virtualTarget`), and the endpoint. It does not read `substrate.TargetFor`, which derives the same controller through `ControllerPort`.
- **Evidence:** `request, err := machineRequestFor(..., int(base)+ordinal)` against `port, ok := ControllerPort(catalog, provider, machine.Name())`. `HostedMachines` says its order "is derived once and never re-derived per Machine", yet `ControllerPort` re-derives it for every Machine.
- **Impact:** The endpoint the emulator listens on and the endpoint every consumer boots through must agree, but two code paths compute them. `selection_test.go:165` asserts a literal, and only the interfaces are compared with `TargetFor` (`:188-200`).
- **Fix:** Build `Controller` in `machineRequestFor` from `TargetFor` (`Endpoint`, `CredentialsRef`, `TrustBundleRef`), and add a test that asserts request endpoint == target endpoint. (S)
- **Checked:** Both paths use the same `HostedMachines` order today, so they agree now. Nothing enforces that.

##### G15-03

**Medium · DDD · confidence medium**

- **Where:** `internal/substrate/admission.go:202-284`, `:26-94`; `internal/substrate/managed_network.go` (410 lines); `internal/substrate/facts.go:67-81`, `:107-145`
- **What:** The arm-neutral root holds libvirt and vSphere admission branches, all of libvirt managed-Network admission, and libvirt readers (`HostMachine`, `NeedsHypervisorClient`, `AddressClaims`). There are 17 `ArmLibvirt`/`"libvirt"` references in `managed_network.go`, 9 in `facts.go` and 8 in `admission.go`. `Validate` has cyclomatic complexity 37 and switches on string literals (`variant == "libvirt"`, `case "vsphere"`) even though `Arm*` constants exist. `partial.go:11` repeats `Arms()` as a literal list.
- **Evidence:** `if variant == "libvirt" { bmc := arm.Get("bmcEmulation") …`; `NeedsHypervisorClient(provider) bool { return provider.Spec().Has(ArmLibvirt) }`, which bypasses the `armFacts` table that `ProviderTools` reads.
- **Impact:** Adding or changing an arm means editing a 37-branch function and several root files, and the facts table is not the single answer that `facts.go`'s own comment promises.
- **Fix:** Move per-arm admission into a dispatch table keyed by arm (`armValidators[arm](o, c)`) in arm-named root files, replace the literals with the constants, and make `NeedsHypervisorClient` a `facts` row. (M) `policy trade-off`: some naming must stay in the root because the arms import it and `TargetFor` names domains, pools and endpoints.
- **Checked:** architecture.md "Adding an implementation" step 7 places `Arms`, `Realized`, `armFacts` and the machine-bound rows in the root, but no rule places per-arm admission there. The identity guard test (`implementation_identity_test.go:93`) exempts the whole root, so nothing checks this.

##### G15-04

**Medium · Efficiency · confidence high**

- **Where:** `internal/substrate/capacity/budget.go:67-73`, `:99-107`, `:221-237`
- **What:** `Budget.Check` observes each host and pool group one after another, and `addWrites` probes each distinct host and path one after another. Each remote probe can take up to `RemoteProbeTimeout` (60 s). `observeGroup` also re-implements the bounded probe that `observeDisk` (139-158) already provides.
- **Evidence:** `for _, group := range groups { row, err := b.observeGroup(ctx, group, dryRun)`.
- **Impact:** An apply gate over N silent SSH hosts waits up to N×60 s. The project's parallel-by-default rule calls serial work over independent hosts a defect.
- **Fix:** Fan out the group and write probes with a bounded errgroup, collect the results in key order, and call `observeDisk` from `observeGroup`. (S)
- **Checked:** The wiring in `cmd/bootwright/wiring_preflight.go:25,46` passes a plain remote observer with no internal parallelism.

##### G15-05

**Low · DDD · confidence medium**

- **Where:** `internal/substrate/capacity/reserves.go:13`, `capacity/budget.go:386`
- **What:** The generic `capacity` package reads `spec.libvirt.capacityReserve` and its remediation text names libvirt. Its only producer is `libvirt.CapacityAllocations`.
- **Evidence:** `value := provider.Spec().Get("libvirt", "capacityReserve")`.
- **Impact:** Another arm cannot use the budget without editing it.
- **Fix:** Have the arm fill `Allocation.Reserve` (the field already exists and is used when `Reserves == nil`), and pass the remediation field path in. (S)
- **Checked:** Audit 003 line 1352 notes reserve disagreement, which is a different issue.

##### G15-06

**Low · Quality · confidence high**

- **Where:** `internal/substrate/kubevirt/capability.go:90`; `internal/substrate/libvirt/capability.go:64-77`
- **What:** The KubeVirt host block sets `Family: "substrate-host"` as a literal and the libvirt host block sets no family. The root defines `MachineFamily` but has no host-family constant; the literal is repeated in `cmd/bootwright/wiring_reconciliation.go:293,306` and `controller/prerequisites/reservations.go:185`.
- **Evidence:** `ResolveRequirements` registers a family-qualified reference only when `definition.Family != ""` (`internal/reconciliation/plan.go:260`).
- **Impact:** Latent. A future family-qualified requirement on an InfraProvider would resolve KubeVirt hosts and refuse libvirt ones.
- **Fix:** Add `substrate.HostFamily` and set it on both host blocks. (S)
- **Checked:** No current `Requires` names that family.

##### G15-07

**Low · Quality · confidence medium**

- **Where:** `internal/substrate/libvirt/capability.go:642-645`; `internal/substrate/kubevirt/capability.go:676-679`
- **What:** `Quiescent` turns every run error, including a context cancelled mid-probe, into `Unproved, nil`.
- **Evidence:** `if err != nil { return unprovedStop("its power state could not be read", stop), nil }`.
- **Impact:** A cancelled destroy reports "could not be read" and a stop remedy instead of the cancellation.
- **Fix:** `if ctx.Err() != nil { return lifecycle.Quiescence{}, ctx.Err() }` before mapping the error. (S)
- **Checked:** `prepare` checks `ctx.Err()` only before the run starts.

##### G15-08

**Low · Clearness · confidence high**

- **Where:** `internal/substrate/libvirt/requests.go:34-37`; `internal/substrate/machine_bounds.go:73-96`; `internal/substrate/kubevirt/catalog.go:31`
- **What:** Three local clarity problems:
- **Misplaced doc comment:** "HostRequest is the complete frozen intent…" sits above `type Trunk`.
- **Borrowed bounds:** bare-metal machine bounds are built by overriding a libvirt bundle, so bare metal silently inherits `LibvirtPowerBound`, `LibvirtPowerReadBound` and `LibvirtBootDiskBound`.
- **Misleading name:** `hostEvidenceVersion` is named like an evidence version, but it is only folded into the content digest. `HostEvidence.Version` holds the Kubernetes `gitVersion`, and nothing validates it (unlike `lendEvidence`, which is checked at `lend.go:159`).

- **Fix:** Move the comment; give each arm a full row of bounds (or explicit `Baremetal*` constants); rename the KubeVirt constant to a digest tag. (S)

#### Patterns
- **Small helpers copied per arm:** `findNamed` (`libvirt/selection.go:296`, `kubevirt/selection.go:289`, and a different, uniqueness-requiring version at `admission.go:410`), `contentDigest` (libvirt, kubevirt), `refusal` (each package).
- **Lookups the request could carry:** the libvirt machine Plan and `CapacityAllocations` re-find the Machine in the catalog to read `providerRef` (`libvirt/capability.go:194`, `libvirt/capacity.go:25`), because `MachineRequest` carries no `Provider` field while the kubevirt request does.
- **Per-Machine catalog rescans:** `HostedMachines` scans the whole catalog on every call; `ControllerPort` per Machine and `bmcPortRange` per provider pair make admission and target derivation O(P²·M) (`naming.go:127`, `admission.go:231-240`).
- **Arm-name comparisons in consumers:** `managedos/admission.go:394` and `cmd/bootwright/acquisitions.go:158` compare against arm constants where a fact (`EmulatedBMC`, physical) exists. Both are allowed by `armSupportFiles` or by being in composition, so this is not a violation, but the facts table could absorb them.

#### Strengths
- **Strict, fail-closed evidence:** decoding is bounded, bound to the request digest, and silence is never read as absence (`Answered`, `PoolAnswered` and `Listener *bool` in `libvirt/evidence.go`; the `Quiescent` order in `libvirt/capability.go:635-666`).
- **One controller-endpoint grammar:** `NormalizeControllerEndpoint` and `canonicalControllerHost` refuse a second spelling of the same server, which keeps the `bmc:`, `bmc-system:` and `bmc-mac:` claims sound (`target.go:384-471`).
- **Bare-metal identity is handled with care:**
  - The proof requires the exact declared MAC set, a recorded identity and a read power state.
  - Reported UUID and serial are bounded, must be printable, and are refused by Redfish property name, never by value.
  - The pin is read only from a done apply.
  - `PreBootRefusals` names hardware-mismatch, identity-mismatch and machine-running refusals with precise remedies (`baremetal/evidence.go`, `preboot.go:41-97`).
  - The powered-off check happens at pre-boot, outside this lane.
- **Lent KubeVirt tokens are handled safely:** a `sync.Once` release, `Material.Clear()`, and a revoke under `WithoutCancel` with its own deadline. Tests cover cancellation and a failed revoke.
- **Arm consumers are policed:** the `armFacts` table plus the `armSupportFiles` identity guard keep arm names out of consumer code.

#### Coverage notes
- `kubevirt/unresolved.go:15` `MachineCapability.Unresolved`: 0% (16 statements). It holds the operator-facing reasons and remedies for a foreign VirtualMachine, an unanswered host and empty evidence, including the `--authorize rebind=` remedy. Nothing pins them.
- `kubevirt/capability.go:563` `MachineCapability.observe`: 6 of 12 statements not run by any test, including the `Outcome: Unchanged` branch that differs from libvirt.
- `capacity/reserves.go:9` `ReservesFromCatalog`: 0% by its own tests. The rule that makes disagreeing providers on one host and pool "unconfigured" is exercised only from other packages.
- `capacity/admission.go:23` `Admit`: 0% by its own tests. Nothing in the package pins how status maps to the `preflight.failed` / `preflight.unknown` codes.
- `capacity/budget.go:254-258`: the refusal of an implausible reusable credit is among the 9 statements of `observeGroup` no test runs.
- No test asserts that the libvirt request endpoint equals `TargetFor`'s endpoint ([G15-02](#g15-02)).
- `baremetal/capability.go:45`: the `Preparation.Targets` filter in `Plan` is among the uncovered statements (6 of 23 not run by any test).

#### Already recorded
- SBV-11 (audit 003, report-claude): the shared `bmc:` reservation is now appended unconditionally at `libvirt/capability.go:228-231`; this looks resolved in code.
- SBV-15 (audit 003): KubeVirt completion ignores the root DataVolume phase (`kubevirt/evidence.go`, `capability.go`); not restated.
- Audit 003 Composition entry: `kubevirt.PinnedIdentity` is decoded by the composition root (`report-claude.md:3023`); not restated.
- tracked: B606 (libvirt `machineEffect` reads a disk-size-only drift as unknown). Relevant to the [G15-01](#g15-01) helper design.

<a id="lane-g16"></a>

### Lane G16: Managed OS installation capability (Go)
Scope read: I read 34 of the 41 production files in full: kickstart, selection, capability, erase, erase_request, erase_unstarted, erase_regions, clear, discovery, discovery_inventory, discovery_preparation, discovery_request, firewall, firewall_request, encryption, bootstrap_network, network, derivation, requests, evidence, registration, install_interface, tree, update, key_policy, capacity, catalog, secret_references, shapes, contracts, projection, dependency_proof, preparation and claims. I did not read seven small files (tree_evidence, package_tree_proof, precursor, unresolved, host_ports, lifecycle_refusals, discovery_progress; about 240 lines) because time went to the destructive paths. Tests: I read discovery_test.go in full. I skimmed target_proof_test, clear_test, kickstart_guard_test, golden_test, the testdata/ layout and the shared erase-region vectors. Outside the lane, to confirm claims, I opened the Python workers physical_erase_worker.py, disk_inventory.py and managedos_firewall_worker.py, plus agentinstall/projection.go, wiring_reconciliation.go and lifecycle/attempts.go.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed throughout; a few proof predicates are weaker copies of their siblings |
| Security | 4 | No secret is written into the Kickstart, and a per-field guard refuses unsafe values; small gaps in quoting and file modes |
| Efficiency | 3 | Every capability re-derives every request, Kickstart included, on each plan, and the media store is read once per machine |
| Clearness | 3 | Over 1,000 lines of shell built as Go string literals; the proof functions have complexity 48 to 53 |
| DDD | 3 | Five capabilities share one package; the host firewall also serves provided Machines |
| Non-duplication | 2 | Disk-identity reading has three implementations, report-provenance checking three, and the exact-size rule seven copies |
| Right language | 4 | The installer-native shell is sanctioned by the spec, but its rules are duplicated in Python |
| Test coverage | 3 | 84.6% own and 90.8% any; the emitted bash is run against fake sysfs; one erase proof is never run and the goldens are brittle |

#### Findings
##### G16-01

**Medium · Non-duplication · confidence high**

- **Where:** internal/managedos/installation/kickstart.go:949-977, kickstart.go:841-855, clear.go:147-153; plugins/module_utils/physical_erase_worker.py:148-158, 240-257; plugins/module_utils/disk_inventory.py:27-70
- **What:** The rule for reading a disk's serial number and WWN from sysfs is implemented three times. One is the bash `bootwright_identity` and `bootwright_resolve` functions in the emitted `%pre`. The other two are the erase worker's `text`/`disk_serial` and disk inventory's `attribute`/`serial_number`. The three disagree in detail. disk_inventory falls back to `<node>/serial`, refuses `<>&` and strips only `" \t\r\n"`. The erase worker strips all whitespace with `.strip()`. The `%pre` trims bytes 9-13 and 32.
- **Evidence:** disk_inventory.py:50-52 has `value = attribute(device / "device/serial", optional=True)` then `if not value: value = attribute(device / "serial", ...)`. The spec itself documents a divergence (managed-os.md:1321-1323): "the `%pre` additionally refuses an attribute longer than 258 bytes, where the worker reads its first 258".
- **Impact:** The serial the inventory freezes is the one the `%pre` must match. Any divergence fails closed, so the risk is a refused installation or erase, not data loss. Still, every rule change has to land in three languages and in spec prose.
- **Fix:** Write one fixture set of sysfs trees (VPD, NUL-padded, surrounding whitespace, `<node>/serial` only) with the expected identities. Run it against the bash function and both Python readers in their existing suites. Optionally run the `%pre` identity step with the installer's python3 and the shipped worker module. (policy trade-off; effort M)
- **Checked:** The erase regions already use this pattern (vectors at test_physical_erase_worker.py:365), but no such fixtures exist for identity reading. No backlog item or knowledge page covers it.

##### G16-02

**Medium · Test coverage / Quality · confidence high**

- **Where:** internal/managedos/installation/erase_request.go:251-272; cmd/bootwright/wiring_reconciliation.go:506
- **What:** `PhysicalEraseProof` has 0% coverage (13 of 13 statements unrun). It is the erasure proof Ceph physical retirement relies on. Its check that a removal record pairs with an applied block is weaker than the same check in `PhysicalEraseUnstarted`. It does not compare `removed.Block.Kind/Object/Implementation`, and it does not recompute the applied digest through `originalEraseBlock`. It also decodes the evidence twice.
- **Evidence:** `removed.Block.ID != applied.ID || removed.Object != applied.Object || removed.Block.RequestDigest != applied.RequestDigest` (line 252-254), compared with the full field set at erase_unstarted.go:78-82.
- **Impact:** Untested code decides whether disks count as erased before a Ceph host retires.
- **Fix:** Extract one `pairedEraseRemoval(applied, removed)` predicate, use it from both functions, and add accept and refuse tests. (S)
- **Checked:** The caller passes a `reconciliation.Block` and `BlockEvidence` taken from records. The digest compare protects the request bytes, but not the block identity fields.

##### G16-03

**Medium · Non-duplication / Clearness · confidence high**

- **Where:** discovery.go:244-258, discovery_inventory.go:39, discovery_inventory.go:95
- **What:** Whether a discovery report belongs to its request is decided three ways. `completedInventory` compares the whole expected binding with `reflect.DeepEqual`, including Incarnation and UUID. `RecordedInventory` lists fields and skips Incarnation and UUID. `proveDiscoveredRoot` lists fields again and adds Incarnation, UUID and the report digest.
- **Evidence:** discovery_inventory.go:39 has `report.Binding.Context != request.Identity.Context || report.Binding.Machine != name || ...` and never reads `Incarnation` or `UUID`.
- **Impact:** The three checks can drift apart, and they account for most of `proveDiscoveredRoot`'s complexity of 48.
- **Fix:** Use one `bindingFor(record, request, incarnation, uuid)` and compare it with `DeepEqual` in all three places. (S)
- **Checked:** `RecordedInventory` feeds the CLI display (wiring_discovery.go:42) and refinement facts, which run only after `Roots` has validated the report. That mitigates the gap but does not remove it.

##### G16-04

**Medium · DDD / Clearness · confidence medium**

- **Where:** capability.go:24, erase.go:22, discovery.go:22, tree.go:51, firewall.go:47; specs/architecture.md:311
- **What:** One package holds five lifecycle capabilities (install, physical erase, disk discovery, package tree, host firewall). The architecture spec describes a capability package as one installation method whose `capability.go` declares "a capability type". The host firewall also plans for provided Machines (firewall_request.go:168-173), so it is not installation at all.
- **Impact:** 41 files with no internal seams; the 319 shape-clone lines measured inside the package are a symptom.
- **Fix:** Move the firewall, and probably erase and discovery, into sibling `internal/managedos/<capability>` packages that share request types through a small common package. (L)
- **Checked:** Neither audit 003 nor the backlog records the split. PRJ-02 is about firewall documentation, not structure.

##### G16-05

**Medium · Clearness / Right language · confidence medium**

- **Where:** clear.go:161-281, kickstart.go:886-893, clear.go:139-145, kickstart.go:923-925, kickstart.go:1031-1033, encryption.go:264-289
- **What:** A large destructive shell program lives as Go string slices: the device-mapper holder walk, `dmsetup` release, mount and swap scans, and `dd` zero-and-readback. Within that emitted shell, the WWN-path snippet (`wwid`, else `device/wwid`) appears four times and the serial selection (`serial`, else VPD) four times, in different functions.
- **Evidence:** `bootwright_wwid="${bootwright_disk}/device/wwid"` appears at kickstart.go:889, kickstart.go:923 and clear.go:140; kickstart.go:1031 uses `bootwright_id`.
- **Impact:** A fix such as MON-02 has to be applied at each copy. Nothing runs a syntax check or shellcheck over the emitted text.
- **Fix:** Emit `bootwright_wwn_of` and `bootwright_serial_of` once, beside `bootwright_identity`, and call them everywhere. (M)
- **Checked:** The spec sanctions shell for the installer (architecture.md:787), so the language itself is not the finding.

##### G16-06

**Medium · Test coverage · confidence high**

- **Where:** testdata/request-*.golden (16 files, e.g. request-physical-host-variants.golden, 15 KB)
- **What:** Each request golden stores the whole Kickstart as one escaped JSON string. Any change to an emitted shell line rewrites many goldens as single-line diffs nobody can review. Audit MOI-02 found the goldens had frozen the weak agent-filter check.
- **Evidence:** `"kickstart": "# Generated by Bootwright. Do not edit.\ntext\neula --agreed\n..."`. Only kickstart-physical-volume.golden stores the Kickstart as raw text.
- **Fix:** Store the Kickstart as raw `.ks` goldens, one per variant, and keep only a digest of it in the request golden. (S)
- **Checked:** Per-package golden helpers are a deliberate policy; the escaped embedding is not part of that policy.

##### G16-07

**Low · Quality / DDD · confidence high**

- **Where:** erase_request.go:82-86
- **What:** The erase request recovers its network declaration by scanning the rendered Kickstart text for `network ` lines. `networkLine(install)` already produces that line as a typed value.
- **Evidence:** `for _, line := range strings.Split(install.Kickstart, "\n") { if strings.HasPrefix(line, "network ") {`
- **Fix:** Carry the derived network line, or the `Installation`, from `deriveRequest`. (S)

##### G16-08

**Low · Non-duplication · confidence high**

- **Where:** network.go:100-131; containercluster/agentinstall/projection.go:370-401; requests.go:86-100 and agentinstall/requests.go:101-114
- **What:** `nativeDocument` is an exact copy of agentinstall's `nativeValue`, and the `Firmware`/`BIOSAttribute` types are duplicated between the two packages.
- **Fix:** Add an `api.Value` method that converts to native Go values, and reuse the substrate firmware types. (S)

##### G16-09

**Low · Non-duplication · confidence high**

- **Where:** kickstart.go:148, clear.go:72, erase_request.go:109; internal/machine/erase_admission.go:59; diskselection/selection.go:57
- **What:** The exact-disk-size predicate (at least 4096, at most 64 TiB, 4096-aligned) appears seven times. erase_request.go and erase_admission.go spell the bound as a literal instead of a constant.
- **Evidence:** `size > 64*1024*1024*1024*1024 || size%4096 != 0`
- **Fix:** Export one `machine.ValidDiskSize`. (S)

##### G16-10

**Low · Efficiency · confidence medium**

- **Where:** discovery.go:82, selection.go:176-199, contracts.go:68-77
- **What:** `DiscoveryCapability.requests` reads `MediaRecords` inside its per-machine loop. Each plan re-derives every installation request, including `RenderKickstart`, in Capability.Plan, EraseCapability.Plan, TreeCapability.Plan, FirewallRequests and CapacityWrites. Install and erase each call `discovery.Roots`, which re-reads the applied evidence.
- **Fix:** Read the media store once before the loop, and memoize the derived requests per plan input. (M)

##### G16-11

**Low · Security · confidence medium**

- **Where:** kickstart.go:550-553, kickstart.go:707-711
- **What:** The delivered private host key is fetched by `curl --output` in a `%post` that sets no `umask`. The file is created world-readable until the `chmod 0600` that follows. The encryption sections do set `umask 077` (encryption.go:133).
- **Impact:** Low. Inside the installer chroot no other account exists, but the gap breaks the project's own pattern.
- **Fix:** Put `umask 077` before the fetches. (S)

##### G16-12

**Low · Quality · confidence high**

- **Where:** erase.go:154,157,174,178; discovery.go:382,386,413,417
- **What:** When Erase and Discovery refuse, they return a zero `lifecycle.Result{}`. The engine turns the empty outcome into Unknown (attempts.go:110-113), so even a refusal before any effect forces a resolution observe. Capability, Firewall and Tree use `AttemptOutcome`.
- **Fix:** Return `OutcomeFailed` for refusals before any effect, and `AttemptOutcome(err)` after `run`. (S)

##### G16-13

**Low · Security · confidence medium**

- **Where:** firewall_request.go:142-155
- **What:** The firewalld service XML is built by string concatenation with no escaping. Its safety rests on upstream grammars (context name, block identity, the apply-operation format). The Python worker refuses `<>&"'` in the mark (`valid_mark`), but Go never checks the mark.
- **Fix:** Validate the mark in Go with the worker's rule, or escape it with `xml.EscapeText`. (S)

#### Patterns
- Hand-written multi-field equality checks for record provenance drive the package's highest complexity scores: erase_unstarted.go:78-82 (53), discovery_inventory.go:95 (48), erase_request.go:252. A small set of comparison helpers would remove most of it.
- Go and Python twins are kept in sync by comments and separately hard-coded vectors: `MetadataClearRegions` and `metadata_regions` ("Go's MetadataClearRegions twins it"), MAX_OVERWRITE, and the firewall bounds (MAX_SERVICES 64 / MAX_NETWORKS 16 in both).
- Evidence validators take raw bytes, so one decision decodes the same bytes repeatedly: `decidedEffect` up to four times (capability.go:409-418), `PhysicalEraseProof` twice, and `ProvedFIPS` re-decodes the request.
- Content digests are lists of tokens appended by hand ("anaconda-home-partition-policy" in catalog.go:131, the long token in erase_request.go:56). A behavior change depends on someone remembering to add a token.

#### Strengths
- The Kickstart guard (kickstart.go:232-373) lists every field with its source and shape. It refuses all `str.splitlines` breaks, U+2028 and U+2029 included, plus shlex separators and a leading `%`. The fleet key is checked separately (capability.go:724).
- No secret is ever written into the Kickstart. The marker, key and URL are tokens filled in at execution, and private material is fetched over TLS pinned to the artifact server's certificate from a minted private URL. The `%post` deletes every retained copy of the Kickstart.
- The emitted bash is executed in tests against fake sysfs and proc trees (target_proof_test.go:96-103, clear_test.go, physical_encryption_test.go). The tests assert refusal before storage is touched, not string presence.
- The destructive proofs fail closed: the discovered root is re-proved at apply, the erase receipt's attempt lineage and UUID are bound, and the firewall effect table is closed.

#### Coverage notes
- erase_request.go:251 `PhysicalEraseProof`: 0%, on the Ceph retirement path (see [G16-02](#g16-02)).
- discovery_preparation.go:58 `ValidatePreparation`: 39%. Only the ownership refusal is tested (discovery_test.go:275-289); the branch without `PreparePrerequisites` and its "prerequisites are not applied" refusal is not.
- discovery_inventory.go:51 `proveDiscoveredRoot`: 76%. The tests cover duplicate records, a foreign incarnation and a stripped BMC bound. They do not cover a report-digest mismatch, a pinned-UUID mismatch, nil identities, or zero or several matching disks.
- discovery.go:390 `Observe` 30%, discovery.go:405 `Destroy` 50%: cleanup of the discovery boot.
- No test runs a rendered Kickstart through pykickstart, even though the guard re-implements its tokenizer. Backlog line 677 records the lesson and notes it has no knowledge page.
- erase.go:241 `installedRootWWN`: 62%. This recorded-WWN path decides which disk is the root during erase.

#### Already recorded
- MOI-02: the guest-agent block-list edit proves only that guest-file-open was removed (kickstart.go:984-991).
- MON-02: the encrypted serial root's `%post` re-resolves without the removable, read-only and size checks (encryption.go:264-273).
- MON-11: the delivered key is fetched after the disks are cleared.
- Audit 003 (report-claude.md:5996): no clear-target test covers the "already-walked" holder rule.
- Audit 003 (report-claude.md:1225): the by-id root check in kickstart.go:143 duplicates the one in selection.go.
- B1104 / MON-03: the WWN of a serial-declared root is recorded but not tracked to closure.

<a id="lane-g17"></a>

### Lane G17: Managed OS root and media store; infrastructure services (Go)

Scope read: all of the following, read in full: managedos/{admission,media,treeinfo,effect,media_references,partial}.go; media/{service,contracts,requests}.go; medialocal/acquirer.go; infrastructureservices/{admission,selections,mirror,reservation,dns_zones,effect,partial}.go; every production file in artifactserver/, managedservice/, mirrorfill/, dnsserver/, ntpserver/, proxy/, registry/. Read outside the lane to check claims: cmd/bootwright/wiring_managedos.go, secrets/{classes,values}.go, secrets/material/password_file.go, squid.conf.j2, infra_service.py (probe_registry). / Skimmed: media/storecontract/contract.go; the test names in media/service_test.go (42 tests), medialocal/*_test.go, registry/catalog_test.go and artifactserver/managed_refusal_table_test.go; the timeout and deadline tests in acquirer_test.go. / Not read: the managedos/installation, native and kickstart subpackages (other lanes).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful error classification and context handling; small slips (a discarded return value, a raw `fmt.Errorf`, misleading plan-time remedies) |
| Security | 4 | medialocal is exemplary (HTTPS only, no redirects, query kept out of records and errors, the file descriptor itself checked); bcrypt cost 12 with the library's crypto/rand salt; proxy deny rules in both Go and the template |
| Efficiency | 4 | Bounded everywhere; only small repeated catalog scans (`consumers()` runs about 4 times per profile) |
| Clearness | 3 | Code reads well function by function, but parallel copies make "which rule applies to this kind" hard to answer |
| DDD | 3 | Kind-specific DNS and proxy derivations sit in the shared `managedservice`; serving-certificate policy sits in `artifactserver` and Registry borrows it |
| Non-duplication | 2 | Two near-copies of one lifecycle capability, three of one fill capability, two `EndpointURL`s and two hosted-layout constant sets, and copies of the same rule have already started to differ |
| Right language | 4 | Go decides the policy, Jinja renders it; the template's own deny list is defence in depth |
| Test coverage | 4 | Hermetic and behaviour-focused; managedservice's security paths are proven only through sibling packages |

#### Findings

##### G17-01

**High · Non-duplication · confidence high**

- **Where:** internal/infrastructureservices/artifactserver/capability.go:43-311, internal/infrastructureservices/managedservice/capability.go:96-518, artifactserver/evidence.go:153-256, managedservice/evidence.go:105-203
- **What:** `artifactserver.Capability` repeats `managedservice.Capability` almost line for line: Plan, the description/impacts/groups helpers, Removal, mutate, Observe and ObserveRemoval, the effect function, prepare and run, plus ValidateAbsence, ValidatePartial, ValidateUnremoved, ValidateRemovalUnfinished, `removalEvidence`, `unremoved` and two `certificateMismatch` sentinels. The copies have already started to differ.
- **Evidence:** the measured clones are 26 + 16 + 10 exact lines and 43 + 33 + 22 shape lines. On differing: artifactserver/evidence.go:188 `if evidence.Postcondition && (evidence.Unit != "active" || !evidence.ContentRoot || !evidence.StartedAfterFiles)` treats a postcondition claimed without a fresh start as a contradiction (unknown). managedservice/evidence.go:116 omits `StartedAfterFiles`, and its comment says that case is read as partial "rather than as a contradiction". Certificate mismatch also has two mechanisms: a `certificate-mismatch` refusal reason for the artifact server (refusals.go:121-123 adds it for ArtifactServer only), and an answer value for Registry (infra_service.py:211).
- **Impact:** every lifecycle-contract change (new evidence field, new refusal) has to be made twice, and the next difference may not be deliberate.
- **Fix:** give `managedservice.Capability` a small per-kind evidence port (presence and partial checks, probe ports, request decode) and make the artifact server one `Definition` with multi-listener evidence; keep Request and Evidence apart only where the wire shapes truly differ. (effort L)
- **Checked:** the artifactserver/requests.go:34-38 comment says the derivation "is implemented once" through type aliases, but that covers only Identity, Egress and Placement. Spec infrastructure-services.md:452-456 and 502-512 give one rule set for both. No B item tracks the merge.

##### G17-02

**Medium · Non-duplication · confidence high**

- **Where:** internal/infrastructureservices/mirrorfill/capability.go:46-287, images.go:44-292, releases.go:40-308
- **What:** the three fill capabilities (Files, ImageFill, ReleaseFill) each repeat Plan, run (identity check, runner nil-check, Canonical, PinInstalledHost, RunFor), mutate, Observe, Unresolved, SecretReferences, SecretClasses and Claimed. ImageFill and ReleaseFill also repeat the mirror-host derivation, and `managedservice.MirroredImage` has the same `ImageSource` construction a third time.
- **Evidence:** images.go:216 and releases.go:224 both have `outcome := map[string]reconciliation.Outcome{"changed": ..., "unchanged": ...}[result.Outcome]`. images.go:103-106, releases.go:95-98 and managedservice/mirror.go:64-69 all build `ImageSource{Host: ..., CredentialsRef: mirror.CredentialsRef, TrustBundleRef: ...}` and then look up `CredentialsType`. Measured: 205 shape-clone lines.
- **Impact:** changing the fill contract means three edits, and the copies already differ (Files observes its removal, the other two fabricate the removal evidence).
- **Fix:** a generic `fill[R]` with the decode, validate, effect, drift and refusal functions as fields, plus one `managedservice.MirrorSource(catalog, mirror)` constructor. (effort M)
- **Checked:** the per-kind effect functions differ on purpose and should stay separate; only the shell around them is repeated.

##### G17-03

**Medium · DDD / Non-duplication · confidence high**

- **Where:** internal/infrastructureservices/artifactserver/catalog.go:22-29, managedservice/selection.go:16-29, artifactserver/selection.go:96, artifactserver/publication.go:85-87
- **What:** the hosted layout is declared twice: `HostedRoot = "/var/lib/bootwright-hosted"`, `servicesArea`/`ServicesArea` and `unitPrefix`/`UnitPrefix` exist in both packages. The artifact server also builds its unit name by hand with a slug (`-artifacts-`) that differs from its content-root slug (`artifact-server`).
- **Evidence:** `Unit: unitPrefix + "-" + contextName + "-artifacts-" + name,` against `HostedRoot + "/" + contextName + "/" + servicesArea + "/artifact-server/" + server`.
- **Impact:** the publication.go:82-84 comment says the layout has "one owner rather than one copy per consumer", but it has two, and a change to one root changes only one ContentDigest.
- **Fix:** `artifactserver` uses `managedservice.ContentRoot`, `UnitName` and `HostedRoot`; keep the current slugs if the digest window is not wanted. (effort S)
- **Checked:** both ContentDigest functions hash their own copy, so neither would notice drift in the other.

##### G17-04

**Medium · Non-duplication · confidence high**

- **Where:** internal/infrastructureservices/mirror.go:122-160, internal/infrastructureservices/artifactserver/publication.go:94-156
- **What:** there are two `EndpointURL` functions for one rule (endpoint, listener, placement address, `protocol://host:port`). One returns "" on failure and brackets IPv6 by hand; the other returns diagnostics and uses `managedservice.HostPort`. `namedEntry` (mirror.go:143) and `named` (publication.go:143) are byte-identical. `managedservice.namedEndpoint` (selection.go:225) is a third lookup with different semantics: it falls back to the only endpoint even when the name it was given does not match.
- **Evidence:** mirror.go:153-157 `if strings.Contains(address, ":") { address = "[" + address + "]" }` against publication.go:118 `managedservice.HostPort(address, int(port))`.
- **Impact:** the artifact mirror URL in admission and in the fill can differ from the URL consumers publish to if either rule changes.
- **Fix:** keep one resolver in `infrastructureservices` that returns `(string, error)`, and have the "" form wrap it. (effort S)
- **Checked:** the schema's `endpointOf` refuses a misspelled endpointRef, so `namedEndpoint`'s fallback cannot fire today.

##### G17-05

**Medium · DDD · confidence medium**

- **Where:** internal/infrastructureservices/managedservice/requests.go:32-56, 110-232; registry/catalog.go:61-71
- **What:** the shared package holds DNS-only derivations (MachineRecords, ClusterRecords, nodeAddress, routed), which pull `containercluster` and `network` into every capability. It also holds the proxy and NTP client ACL (`Clients`) and a union Request with eleven kind-specific fields. Meanwhile serving-certificate validation lives in `artifactserver`, and Registry imports a sibling capability to reach it.
- **Evidence:** go-imports shows `managedservice -> ... internal/containercluster ... internal/network`. registry/catalog.go:62 `bound, err := artifactserver.BoundServingMaterial(...)`.
- **Impact:** on the lead's question: the `Definition` abstraction does earn its keep (one role, one request version, three definitions of about 35 lines each). But dnsserver and ntpserver are thin because their policy was put in the shared package, not because they have none.
- **Fix:** move the record and client derivations into dnsserver and proxy (which call them through `Extend`), and move `validateServing` to `managedservice` or `secrets`. (effort M)
- **Checked:** INF-05 (audit 003) covers record ordering, not where the code lives.

##### G17-06

**Low · Quality · confidence high**

- **Where:** internal/infrastructureservices/artifactserver/requests.go:52-56, selection.go:114
- **What:** `TLS.Fingerprint` is frozen into every artifact-server request but never set. The real fingerprint travels in `values["fingerprint"]`.
- **Evidence:** `request.TLS = &TLS{MinVersion: minimum, Secret: reference}`. The goldens (testdata/request-ipv4-tls.golden:43) hold `"fingerprint": ""`. The Request comment says TLS names "its non-secret fingerprint".
- **Impact:** a dead wire field with a misleading comment, in the request digest.
- **Fix:** drop the field in the next request-version window. (effort S)
- **Checked:** the Ansible artifact_server modules read the fingerprint from the probe and the values, never from `request.tls`.

##### G17-07

**Low · Quality · confidence high**

- **Where:** internal/infrastructureservices/artifactserver/secret_references.go:23
- **What:** the return value of `classes.Add(...)` is thrown away. It works only because `Merge` never returns nil, so `Add` mutates the map in place. The managedservice copy reassigns it correctly.
- **Evidence:** `classes.Add(secrets.EffectMaterial, request.TLS.Secret)` against managedservice/secret_references.go:24 `classes = classes.Add(...)`.
- **Impact:** if `Merge` ever returns nil for an empty input, the TLS secret silently loses its class. No own-package test covers artifactserver SecretClasses.
- **Fix:** `classes = classes.Add(...)`. (effort S)
- **Checked:** secrets/classes.go:35-43 (Merge allocates when nil).

##### G17-08

**Low · Clearness · confidence medium**

- **Where:** internal/infrastructureservices/mirrorfill/requests.go:73-74, images_requests.go:67, releases_requests.go:107; callers capability.go:123, images.go:113, releases.go:111
- **What:** `validate()` serves both plan time and decode time, but its only remedy is the decode-time one. Admission does not bound the image or file count (MaxImages 64, MaxFiles 128), so a graph with more of either fails at plan with that remedy.
- **Evidence:** `"the frozen mirror images request names an image it could not copy ..." , "destroy it with the build that applied it"`.
- **Impact:** on a fresh plan the operator is told to destroy something never applied.
- **Fix:** pass the remedy into `validate`, or add an admission bound. (effort S)
- **Checked:** no use of `MaxImages` or `MaxFiles` outside mirrorfill.

##### G17-09

**Low · Non-duplication · confidence high**

- **Where:** internal/managedos/admission.go:507, managedos/media_references.go:13, managedos/installation/selection.go:365, storage/ceph/package_sources.go:68
- **What:** four packages each parse the `"local-media:"` reference by hand. The managedos copies also check the name with `ValidMediaName`.
- **Evidence:** `name, local := strings.CutPrefix(raw, "local-media:")`.
- **Fix:** export `managedos.StoreMediaName(reference) (string, bool)`. (effort S)

##### G17-10

**Low · Non-duplication · confidence high**

- **Where:** internal/infrastructureservices/managedservice/credential.go:31-35, internal/secrets/material/password_file.go:14-19
- **What:** the password-file grammar (no colon or control characters in the username, 1 to 72 bytes and no NUL in the password) is written twice.
- **Fix:** export the predicate from `secrets/material` and call it from `ProveCredential`. (effort S)

##### G17-11

**Low · Quality · confidence high**

- **Where:** internal/infrastructureservices/dnsserver/catalog.go:73
- **What:** a gateway record conflict returns `fmt.Errorf("gateway DNS record %s conflicts ...")` with no diagnostic code, object or remedy, unlike every other refusal in the lane.
- **Fix:** `managedservice.Refusal("api.invariant", ..., <remedy naming both owners>)`. (effort S)

##### G17-12

**Low · Clearness · confidence high**

- **Where:** internal/managedos/media/service.go:141-143, internal/infrastructureservices/admission.go:338-412
- **What:** the doc comment above `readURL` belongs to `addition` (`// addition refuses what an add names before any store is read`). Separately, `validateIntrinsic` has cyclomatic complexity 30, mixing mode and kind in one switch.
- **Fix:** move the comment; build a per-kind required/forbidden table. (effort S)

#### Patterns
- Hand-bracketing IPv6 with `strings.Contains(host, ":")` instead of `net.JoinHostPort`: managedservice/capability.go:286, mirror.go:85 and 153, proxy/destinations.go:64, managedos/admission.go:413.
- The 999-diagnostic cap is copied as a literal `add` helper in five packages (managedos, infrastructureservices, substrate, machine, openshift admission), and `issue`/`reference` are copied with it; the spec (api/input.md:111) defines one cap.
- The default `"TLSv1.2"` floor is written in three places: infrastructureservices/admission.go:34, artifactserver/selection.go:112, registry/catalog.go:53.
- Derived secret material is cleared after use in one place but not another: ReleaseFill defers `Clear()` (releases.go:174), while managedservice `passwordFile` (credential.go:55-57) leaves the htpasswd material uncleared.

#### Strengths
- medialocal/acquirer.go: refuses redirects, accepts HTTPS only, reads no ambient proxy (the route is injected), never echoes transport text, drops the query from origins, checks the received descriptor (`regularSource`) and bounds the URL file to 16 KiB.
- media.Service: digest checked before publish and before the ISO is parsed, confirmation taken with no root lock held, admission re-proved under the exclusive hold, and every held handle closed on cancellation.
- Strict evidence checks: the adapter cannot widen a postcondition, and the effect states are kept positive or unknown.
- Proxy policy is decided in Go (declared plus mirror destinations, sorted and deduplicated; loopback and link-local refused at admission) and only rendered in squid.conf.j2, which repeats the deny list as defence in depth.
- Tests are hermetic (httptest TLS servers, injected dialers, fake stores, no sleeps) and assert exact diagnostics.

#### Coverage notes
- managedservice is 65.3% covered by its own tests: PlacementRefusals, InstalledHostRefusals, ProveCredential, passwordFile and ProveBinding (refusals.go:20 and 67, credential.go:16 and 43, capability.go:470) have 0% own coverage. They are proven through registry/catalog_test.go:207-235 and artifactserver/managed_refusal_table_test.go, not only by the cmd/bootwright contract suite. That is adequate today but fragile: the shared security checks are tested only through whichever consumers happen to use them.
- mirrorfill is at 71.5%: Observe, Unresolved, Removal and Claimed for all three fills have 0% own coverage (images.go:224 and 258, releases.go:247-294, capability.go:229 and 248). These are the unknown-resolution paths, covered only from cmd/bootwright. Add own-package table tests of effect to Unresolved.
- ntpserver has no test file (0% own, 100% from other packages); acceptable for a 4-statement definition.
- artifactserver SecretClasses (secret_references.go:17) has no own-package test, which would catch [G17-07](#g17-07).
- No test pins the artifactserver/managedservice divergence on `StartedAfterFiles` ([G17-01](#g17-01)); a shared contract test over both evaluators would settle which behaviour is intended.

#### Already recorded
- INF-05 (audit 003): DNS records not sorted as one list (dnsserver/catalog.go). Not restated.
- INF-07 (audit 003): NTPServer wildcard socket. Outside this lane's Go scope.
- CTX-05 and CLI-08 (audit 003): media holds skip the ready-context mapping check; the stdin reader list. Not restated.
- tracked: B413, which covers media download stall visibility and the default 10 MB response-header limit (medialocal has no idle-read timeout below the 6-hour transfer deadline, which specs/managed-os.md:31 fixes).
- The RHEL/Ceph entitlement conflation (managedos/admission.go:402-458, called from storage/cephcluster) is already known from the owner's memory notes; not re-reported.

<a id="lane-g18"></a>

### Lane G18: Storage Ceph capability: requests, evidence, resources, services, inspection (Go)
Scope read: I read all 22 production files in full (capability, claims, contracts, requests, resources, evidence, materials, secret_references, services, service_unit, settings, projection, produced, proofs, inspection, admin_credentials, keyring, endpoint_binding, image, package_sources, stretch, tiebreaker_removal). I listed the package's test files and read part of evidence_test.go. I skimmed the coverage, complexity and clone data, audit 003 and the backlog, and the Python evidence and caps code (ceph_core.py:183-300, ceph_native_resources.py:290-305). I did not read the other test files fully (time; I sampled them with grep), and I left the sibling lane's files alone.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Fail-closed throughout; small gaps: an unguarded runner, dead code, inconsistent proof lookups |
| Security | 4 | Secrets are cleared and never put in a request; native docs are typed and allowlisted; caps rule lives twice |
| Efficiency | 3 | Repeated full request builds and repeated strict re-decoding of the same raw JSON |
| Clearness | 3 | Action-string union drives long boolean chains; lines of 1,000+ characters |
| DDD | 3 | Request is one stringly-tagged union with raw JSON arms; inspection re-parses `any` |
| Non-duplication | 3 | Same action dispatch in about 10 places; registry JSON and proof lookups built twice; Go/Python evidence and caps overlap |
| Right language | 3 | Python recomputes client caps (an authorization decision) and evidence scope rules |
| Test coverage | 4 | Package at 86% (own tests), with behavioral mutation tables; admin-credential port at 0% |

#### Findings
##### G18-01

**High · DDD · confidence medium**

- **Where:** requests.go:265-312, capability.go:367-422, evidence.go:490-582, materials.go:221-268, capability.go:205-293, inspection.go:489-504
- **What:** `Request` is one struct with an `Action string` and 12 optional arm pointers. Every phase re-dispatches on the action literal: request build, identity, placement, subjectOf, exclusiveFor, deadline, validateScope, validateReady, scope rows. Each does it with its own switch or boolean chain.
- **Evidence:** `"nfs-export"` appears 5 times in capability.go, 2 in evidence.go, plus materials.go and inspection.go. identity has complexity 50, validateScope 51, validateReady 41, EvaluateEvidence 56.
- **Impact:** adding or changing a Ceph resource kind means editing about 8 parallel dispatch sites in lockstep, and one missed site fails silently (see [G18-02](#g18-02)).
- **Fix:** add one `actions` table keyed by action with: `arm(Request) bool`, `object(Request) string`, `resource(Evidence, Request) bool`, `ready(Evidence, Request) bool` and a deadline function. identity, validateScope and validateReady then iterate the table (effort M).
- **Checked:** the 100-line fitness test is met; no backlog item or audit 003 entry covers it; the "Go package structure" section does not require this shape.

##### G18-02

**Medium · Quality · confidence high**

- **Where:** capability.go:419-420
- **What:** identity's exact-arm check covers only 7 of the 12 arms. policy, pool, client, rbd, filesystem and subvolume can be absent or extra without refusal, so a "pool" request that also carries a Client passes.
- **Evidence:** `(r.Action == "settings") != (r.Settings != nil) || (r.Action == "service") != (r.Service != nil) || ... (r.Action == "object-bucket") != (r.ObjectBucket != nil)`. There is no Pool, Policy, Client, RBD, Filesystem or Subvolume term.
- **Impact:** defense in depth over the frozen request is uneven. Downstream checks such as `r.Pool != nil` mostly catch a missing arm, but an extra arm reaches the worker.
- **Fix:** check "exactly the arm of this action and no other", which falls out of the [G18-01](#g18-01) table (effort S).
- **Checked:** the request digest binds the request to the plan, so this needs journal tampering to exploit. That is why it is Medium, not High.

##### G18-03

**Medium · Quality · confidence high**

- **Where:** admin_credentials.go:59-89
- **What:** adminCredentialRun calls `c.runner.Run` directly, bypassing `invoke`. It has no nil-runner guard (invoke has one at materials.go:177), no Controller check, no refusal map, no `NativeErrors`, and a hard-coded 3-minute deadline with no rationale.
- **Evidence:** `return c.runner.Run(ctx, run)`; `Deadline: 3 * time.Minute}`. Coverage: adminCredentialRun 0/22, PrepareAdminCredential 0/13, UpdateAdminCredential 0/7 statements.
- **Impact:** an unconfigured runner panics instead of failing. A native refusal from the credential procedure maps to no named diagnostic, unlike every other Ceph operation.
- **Fix:** route the call through invoke, or a shared prelude with the runner and Controller guards and `adapterRefusals()`. Add a fake-runner test for the prepare, update and unproved paths (effort S).
- **Checked:** lifecycle/admin_credentials.go:235 does call PrepareAdminCredential on Ceph, so the path is live.

##### G18-04

**Medium · Right language · confidence high**

- **Where:** produced.go:102-135; ceph_native_resources.py:294-305
- **What:** Go (clientCaps, cephFSClientCaps) and Python (ResourceWorker.caps) each compute the client's capability grants from the same request fields. Python uses them to create the client; Go uses them to judge what was produced.
- **Evidence:** Go: `profile := "rbd"; if c.Access == "readOnly" { profile = "rbd-read-only" }`. Python: `profile = "rbd-read-only" if client["access"] == "readOnly" else "rbd"`.
- **Impact:** a grant is an authorization decision, which the Language responsibility table gives to Go. Two copies drift, and when they disagree every client apply goes Unknown.
- **Fix:** freeze `Client.Caps` into the request in Go and have Python apply the frozen map without deriving it (effort S/M).
- **Checked:** specs/architecture.md:784-786 ("whether it is allowed" belongs to Go; Python never authorizes). There is no backlog item.

##### G18-05

**Medium · Non-duplication · confidence medium**

- **Where:** ceph_core.py:183-300 versus evidence.go:125-206 and 490-540
- **What:** the Python publisher re-implements the closed evidence member set and per-action resource-scope rules (subvolume path, NFS pseudo path, object user and bucket IDs, resource ID patterns) that Go's decodeEvidence and validateScope own. The subvolume path rule alone is written in five places: evidence.go:27, ceph_core.py:192, ceph_native_resources.py:135 and 1369, ceph_native_nfs.py:306.
- **Evidence:** `path = "/volumes/" + (subvolume["group"] or "_nogroup") + "/" + subvolume["name"] + "/" + resource["id"]` (Python) against `return "/volumes/" + group + "/" + s.Name + "/" + id` (Go).
- **Impact:** judging the postcondition belongs to Go, and each scope change needs coordinated edits in both languages.
- **Fix:** keep Python to the closed shape and types ("evidence fields"), and drop its request-relative scope judgments. Go already rejects them (effort M). Label: policy trade-off if the owner wants the producer-side self-check.
- **Checked:** the table allows Python "result normalization, evidence fields" but not judging postconditions.

##### G18-06

**Medium · Quality · confidence medium**

- **Where:** inspection.go:148-185 versus proofs.go:47-63 and image.go:388-417
- **What:** Inspect finds the cluster and image proofs with looser lookups than execution does. inspectCluster finds the cluster record by decoding every proof and dropping settings and service actions, rather than matching `Block.ID == ClusterBlockID`. Neither inspectCluster nor inspectImage checks that the evidence's `Request` equals `proof.Block.RequestDigest`, while provedClusterProof, decodeImageEvidence and endpointProof (inspection.go:366) all do.
- **Evidence:** `evidence, err := decodeEvidence(proof.Evidence); return err == nil && (evidence.Action == "settings" || evidence.Action == "service")`.
- **Impact:** the same proof gets three lookup rules, and `cluster info` can show evidence the execution path would refuse.
- **Fix:** have inspectCluster use provedClusterProof plus a digest check, and inspectImage use decodeImageEvidence (effort S).
- **Checked:** tests cover Inspect's happy and ambiguous paths. I found none with a foreign request digest.

##### G18-07

**Medium · Efficiency · confidence medium**

- **Where:** evidence.go:411-424 and 703-717 (via requests.go:703), evidence.go:443-462, endpoint_binding.go:24, produced.go:35
- **What:** the request carries `Services` and `Documents` as `[]json.RawMessage`, which Go itself marshalled from typed structs. Every evaluation then re-decodes them strictly: servicePlacement runs four times and dataDeviceSet two to three times per EvaluateEvidence, and unitDocuments runs on each unitProxied call. The 60 KiB evidence is strictly decoded and canonical-checked up to three times per execution (EvaluateEvidence, evaluateExecutionEvidence, identifyProduced).
- **Evidence:** `v, err := reconciliation.DecodeEvidence[nativeService](raw, 60*1024, "Ceph native service")` inside a loop that runs per call.
- **Impact:** wasted CPU, and an untyped domain model. inspection.go:256-268 has to re-read `Spec any` as `map[string]any` with float64 checks.
- **Fix:** decode once into typed `[]nativeService` and `[]unitDocument` views at DecodeRequest, and pass the decoded Evidence along instead of bytes (effort M).
- **Checked:** the volumes are bounded (128 services, 60 KiB), so this is a cost, not a hazard.

##### G18-08

**Low · Efficiency · confidence medium**

- **Where:** resources.go:50-69, 304-368 and 426-456; resources.go:231-266
- **What:** every derived request rebuilds the whole cluster request. A subvolume or client request calls filesystemRequest, which calls poolRequest for the metadata pool and each data pool. Each of those runs baseRequest (host binding, service projection, JSON marshal) and pgBudget, which scans all pools. ImageCapability.Plan and Claimed→Unsupported build it again.
- **Evidence:** `for i, name := range append([]string{spec.Get("metadataPoolRef").Text()}, data...) { ... request, err := poolRequest(c, pool, contextName)`.
- **Impact:** plan work grows roughly as objects × pools × baseRequest. That is fine at lab scale but needless.
- **Fix:** memoize baseRequest per cluster within one Plan, and split pool validation from request construction (effort M).
- **Checked:** there are no shared caches today, and inventories are bounded by maxClusterHosts.

##### G18-09

**Low · Clearness · confidence high**

- **Where:** capability.go:77-88 with capability.go:248-257
- **What:** placement emits `Requires` refs that use native pool names (`request.RBD.Pool`, `c.Pool`, `Filesystem.MetadataPool`). Plan then rewrites them back to object names by scanning every pool of every definition.
- **Evidence:** `if pool.Spec().Get("clusterRef").Text() == r.Cluster && nativePoolRef(catalog, pool.Name()) == ref.Object { definition.Requires[i].Object = pool.Name() }`.
- **Impact:** placement returns wrong refs that Plan then has to fix: a hidden two-step with a nested loop over all pools (Plan complexity 26, depth 6).
- **Fix:** freeze the pool object name next to the native name, or have placement take the catalog and emit object refs directly (effort S).
- **Checked:** definition is the only caller of placement and Plan the only caller of definition, so the fix is local.

##### G18-10

**Low · Quality · confidence high**

- **Where:** services.go:40-46; inspection.go:677
- **What:** two pieces of dead code. The check at services.go:45 for `extra_container_args`, `custom_configs`, `config` and `unmanaged` can never fire, because lines 40-43 already refuse any field other than service_type, service_id, placement and spec. nvmeofSubsystemScopeText has no caller.
- **Evidence:** `if !slices.Contains([]string{"service_type", "service_id", "placement", "spec"}, field.Name)` followed by `if svc.Has("extra_container_args") || ...`. A grep finds only the definition of `nvmeofSubsystemScopeText`.
- **Impact:** misleading code; the second refusal message never shows.
- **Fix:** delete both (effort S).
- **Checked:** coverage shows projectServices with 9 uncovered statements, and nvmeofSubsystemScopeText at 0%.

##### G18-11

**Low · Non-duplication · confidence high**

- **Where:** materials.go:80-98; image.go:248-265
- **What:** two functions build the same registry credential JSON (`url`, `username`, `password`) from the same Secret parts, with the same clearing.
- **Evidence:** `json.Marshal(map[string]string{"url": r.Registry, "username": string(user), "password": string(password)})` appears in both.
- **Impact:** two copies of a secret-handling routine.
- **Fix:** have boundMaterial call imageRegistryMaterial (effort S).
- **Checked:** the clone data missed it because the copies are under 18 lines.

##### G18-12

**Low · Clearness · confidence high**

- **Where:** materials.go:366, materials.go:414-416, service_unit.go:125
- **What:** adapterRefusals builds about 75 diagnostics. The first 14 sit in one map literal on a single line of about 3,000 characters, the rest are added one by one, and `foreign-client` is set twice (line 370, then overridden in invoke at line 209). The nvmeof deadline uses unexplained constants.
- **Evidence:** `observation := 640 + 60*len(documents[0].Placement.Hosts) + 60 + 20`.
- **Impact:** hard to review or diff, and the deadline rationale is missing.
- **Fix:** one entry per line, sorted. Name the nvmeof budget terms as constants like the existing `nativeCall` and `readinessWait` (effort S).
- **Checked:** there is a formatting rule against narrating comments; naming the terms as constants needs no comment.

#### Patterns
- **Repeated boolean truth tables instead of named predicates.** evidence.go:592 (17 terms), evidence.go:595 (a state-flag truth table that would read better as an enumerated `{ready, removed, partial, absent}` validation), capability.go:419-420, image.go:65.
- **Errors dropped from `Find` and decodes, relying on zero values downstream.** Entitlement lookup at requests.go:371 (a missing entitlement reports "license not accepted"). decodeEvidence's second decode error is held until line 153 (evidence.go:130). gatewayDependencies swallows the monitoringUnits error (service_unit.go:668); this is unreachable in practice because serviceUnitRequests fails first.
- **Native documents carried as raw JSON or `any` and re-parsed per consumer.** requests.go:306, service_unit.go:27 and 149, inspection.go:256-284.

#### Strengths
- service_unit.go does not render systemd or container unit text (the lead was wrong). It builds cephadm service-spec JSON from typed structs through `json.Marshal`, admits only allowlisted spec members per type (`unitSpecMembers`), and refuses `config`, extra args and custom configs. This leaves no injection surface.
- Evidence decoding is strict and fail-closed: closed member sets, null refusal, a canonical-JSON proof, bounded sizes, and digest, owner and FSID binding before any effect is derived.
- Secret hygiene: material is held only in protected files, parts are cleared after checks, the cleanup closure clears derived known_hosts and registry material, and produced credentials are validated and cleared on every failure path in mutate and observe.
- Deterministic identity: `FSIDFor` derives a UUIDv5-shaped FSID from the operation; block IDs and the deadline-derived host bounds (`largestCluster`) are computed rather than hard-coded.
- The tests are behavioral mutation tables (evidence_test.go:55, 182) that assert refusal of each unsafe variant, not just that code ran.

#### Coverage notes
- admin_credentials.go:18-103: every function is at 0%. This is the dashboard credential rotation path through the lifecycle port, and the security-relevant proof checks (`AdminCredentialProved`) and guard extraction are untested.
- capability.go:419-424: the identity-mismatch and owner/FSID-already-set refusals are 3 uncovered statements. I found no test that feeds a request with a wrong arm or a preset Owner.
- capability.go:503-541: mutate has 7 uncovered statements, including the unknown-outcome and identifyProduced failure branches that clear produced material.
- evidence.go:125-206: decodeEvidence has 9 uncovered statements around malformed `stretch`, `specializations` and `inventory` members.
- materials.go:50-113: boundMaterial has 9 uncovered statements in the registry-part failure paths that run `cleanup()`, which is where a leaked derived secret would hide.
- inspection.go:148-185: no test with a proof whose evidence names another request digest (see [G18-06](#g18-06)).

#### Already recorded
- The lead "the package has 50 functions with complexity over 15": I measured 23 in my 22 files. The rest are in the sibling lane's files. No backlog item covers them.
- Audit 003 CEA-08 (nvmeof bindings refused by unitBinding) appears fixed: service_unit.go:449 now lists `nvmeof`. Not re-reported.
- Audit 003 CPC-01 (mgrModules refusal in baseRequest) cites requests.go:371-372, which is now the entitlement lookup; the refusal no longer appears in baseRequest. Not re-reported.
- The host budget is already tracked as B1183 (materials.go:422). I found no B item for [G18-01](#g18-01) to [G18-12](#g18-12).

<a id="lane-g19"></a>

### Lane G19: Storage Ceph capability: hosts, physical retirement, exports, gateways, replication, SMB (Go)
Scope read: all 27 lane files read fully (5,608 lines). HEAD is 21ccc6234, but `git diff d8d5672ef HEAD -- internal/storage/ceph` is empty, so the code is the same as the briefed commit. / Skimmed: replication_test.go:180-279, export_test.go:340-437, the test name lists of physical_retirement_test.go and proof_disk_test.go, host_reading_test.go names, and outside-lane confirmations at stretch.go:137-143, materials.go:55-84, capability.go:545-569, reconciliation/frozen.go:72-89, secrets/values.go:46 and cmd/bootwright/wiring_inspection.go:103-128. / Not read: the other lane tests (host_test, smb_test, nvmeof_test and the rest) beyond their sizes, because the measured coverage pointed elsewhere.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed throughout, but Quiescent hides causes behind one fixed reason |
| Security | 4 | Credentials travel as refs and material and host keys are pinned or proved; export credential strings stay in memory |
| Efficiency | 4 | Planning-only recomputation of gateway requests per bucket or user |
| Clearness | 3 | Long chained identity predicates (CC 24-40), a sentinel empty field, a variadic bool |
| DDD | 3 | Shared helpers live in SMB files (`smbMaterial`, `smbValue`, `strictlySorted`); the export path borrows SMB material |
| Non-duplication | 3 | Known-hosts material built 4 times, proof predicate 9 times, refusal list 4 times |
| Right language | 5 | Go owns the judgment; the Go/Python list of resource members is pinned by a test |
| Test coverage | 3 | Package 86.2% own / 89.6% any; Apply/Observe of replication and the consumer export run at 27-38% |

#### Findings
##### G19-01

**Medium · Quality · confidence high**

- **Where:** internal/storage/ceph/export_evidence.go:232-237, smb.go:579-583, replication.go:350-354
- **What:** Every Quiescent turns any error (a decode failure, a physical policy refusal from `pendingPhysicalClusters`, and for the consumer also context cancellation) into `Unproved` with the fixed reason "hosts outlive this destroy (B491)". It also returns a nil error.
- **Evidence:** `_, err := c.retirement(ctx, p.Execution()); if err != nil { return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil }`. `ConsumerCapability.Quiescent` has no `ctx.Err()` check of its own, unlike SMB and replication at smb.go:575.
- **Impact:** The operator gets the wrong remedy. For example, `physicalRemovalRefusal`'s "retain this context" or `physicalPolicyFailure`'s "restore the records" is replaced by "remove in the same destroy". A cancelled probe is reported as a topology problem.
- **Fix:** Return the error, or carry its diagnostic reason and remedy, and keep `survivingReason` only for the `!RetiredWithHosts` branch. Check `ctx.Err()` first in the consumer. (S)
- **Checked:** SMB and consumer Quiescent are at 100% coverage, but no test asserts the reason on a policy failure. The result is fail-closed (Unproved), so this is a diagnostic problem, not a safety one.

##### G19-02

**Medium · Non-duplication / DDD · confidence high**

- **Where:** host.go:504-510, smb_materials.go:43-58, replication.go:213-238, materials.go:63-78; export.go:253
- **What:** The fleet-key file, the known-hosts builder, the `maps.Clone(e.Material)` and the cleanup closure are assembled four times. The export consumer reuses the SMB builder by passing an empty SMB request.
- **Evidence:** `execution, files, cleanup, err := smbMaterial(e, SMBRequest{}, scope.Scope, operation, c.proofs)` (export.go:253). Each builder ends with `bound[knownHostsMaterial] = secrets.NewMaterial(...known.String()...)`.
- **Impact:** Any change to host-key trust or to clearing must be made in four places. The consumer's material policy depends on SMB's binding validation.
- **Fix:** Extract one `knownHostsMaterial(e, hosts, security, readers) (Execution, MaterialFile, cleanup, error)` in materials.go. Keep SMB's secret-binding files in `smbMaterial`. (M)
- **Checked:** Every copy calls `fleetHostKey` (stretch.go:137), so the trust decision is already shared. Only the assembly is duplicated.

##### G19-03

**Medium · Non-duplication / Clearness · confidence high**

- **Where:** disk_proof.go:28, physical.go:102, host.go:739-740, object_user.go:34, physical_retirement_proof.go:317-318 and 341-342 (also proofs.go:15/51 and image.go:394 in the sibling lane)
- **What:** The predicate that selects "a completed original apply of kind K, object O and implementation I with a self-consistent block" is spelled out by hand nine times.
- **Evidence:** `proof.Kind != "Machine" || proof.Object != host.Machine || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation`
- **Impact:** This chain causes most of the measured complexity (authorizePhysicalRetirement CC 34, ObjectUserProved 26, bindPhysicalHosts 24). A copy that misses one clause would weaken the proof on the destructive paths.
- **Fix:** Add `func completedApply(p lifecycle.BlockEvidence, kind, object, impl string) bool` next to `matchingRemovalPair`. (S)
- **Checked:** `matchingRemovalPair` (physical_retirement_proof.go:227) already shows this helper shape for removal pairs.

##### G19-04

**Low · Non-duplication · confidence high**

- **Where:** host.go:437, replication_evidence.go:63, smb_refusals.go:27, materials.go:414; outcome maps at host.go:596, export_evidence.go:166, smb.go:525, replication.go:279
- **What:** The same adapter boundary refusal list ("request", "artifact", "ownership", ..., "adapter-exception") with the same message template is written four times. The changed/unchanged outcome map is written five times.
- **Evidence:** `for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command", ...}`
- **Impact:** A new collection-wide reason must be added to each list by hand, and a missed one falls through to a generic refusal.
- **Fix:** Add `boundaryRefusals(subject, remedy string, extra ...string)` and `adapterOutcome(string) (reconciliation.Outcome, bool)`. (S)
- **Checked:** The Ceph refusal table test (B595, delivered) pins the set of reasons, not this duplication.

##### G19-05

**Low · Clearness · confidence high**

- **Where:** host_trust.go:49-52, host.go:586, host.go:216
- **What:** `proveFIPS` skips provided hosts when `r.Cluster == ""`. HostCapability.Apply relies on this by leaving Cluster empty, so that the adapter's evidence (`prepared` checks `e.FIPS.proved()`) proves FIPS instead.
- **Evidence:** `if host.Provided { if r.Cluster == "" { continue } ...`; `proveFIPS(e, Request{Security: r.Cluster.Security, Hosts: []Host{host}}, c.proofs)`
- **Impact:** Whether the check runs depends on an empty field, with no stated reason. A caller that sets Cluster would require a host proof that does not exist yet.
- **Fix:** Pass an explicit mode, or split out `proveInstalledFIPS`, and add a one-line rationale. (S)
- **Checked:** This is correct today: the postcondition at host.go:216 refuses a provided FIPS host without proof. proveFIPS is at 59% coverage.

##### G19-06

**Low · Non-duplication · confidence medium**

- **Where:** retirement.go:230-234, smb_evidence.go:40-48, replication_evidence.go:34-39, host_reading.go:81-86, host.go:188-195
- **What:** Each evidence decoder decodes the same bytes twice, once into the struct and once into `map[string]json.RawMessage`, only to check that required members are present and not null.
- **Evidence:** `fields, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, maxSMBEvidence, "Ceph SMB members")`
- **Impact:** The boilerplate repeats, and the strictness is uneven: host evidence has no presence check while the others do.
- **Fix:** Add `decodeEvidenceRequired[T](data, max, subject, members...)` that does both steps. (S)
- **Checked:** `DecodeEvidence` already refuses undeclared members (frozen.go:72-75), so only the presence check is extra.

##### G19-07

**Low · Security · confidence medium**

- **Where:** export_evidence.go:81-86, 134, 245
- **What:** `validateExportProduced` clears the copied part bytes but decodes the CephX `userKey`, `accessKey` and `secretKey` into Go strings that cannot be cleared. `exportKey` then clears a base64-decoded copy, which gains nothing while the string remains.
- **Evidence:** `entries, err := reconciliation.DecodeEvidence[[]exportEntry](data, ...)`; `defer clear(data)` in exportKey
- **Impact:** Credential copies stay in heap memory until garbage collection. This is a smaller guarantee than the "clear its copy" that specs/security.md:193 states.
- **Fix:** Decode `Data` as `map[string]json.RawMessage` and validate keys from byte slices that are then cleared, or record that this is accepted best effort. (M)
- **Checked:** The test asserts that no secret value reaches an error (export_test.go:404). The values never reach diagnostics.

##### G19-08

**Low · Quality · confidence medium**

- **Where:** export_evidence.go:209-216
- **What:** Consumer retirement refuses `r.PhysicalHosts` before it calls `pendingPhysicalClusters`, so `PendingErase` can never be true. The call only works as a consistency guard against inconsistent records.
- **Evidence:** `if !e.RetiredWithHosts || r.PhysicalHosts { return nil, failure(... survivingReason ...) }` followed by `PendingErase: len(pending) != 0`
- **Impact:** Readers may take this for the same physical-retirement path that SMB and replication have. The spec (ceph-resources.md:1400) refuses physical consumers.
- **Fix:** Drop `PendingErase` here, or comment that the call is only a guard. (S)
- **Checked:** specs/ceph-resources.md:1400 and 1491-1495 confirm the refusal is intended.

##### G19-09

**Low · Efficiency · confidence high**

- **Where:** object_bucket.go:39-43, object_user.go:61-65, nfs_export.go:93
- **What:** Each bucket, user and RGW NFS export calls `gatewayRequests`. That call rebuilds `baseRequest`, runs `pgBudget`, builds every zone-pool request, and then keeps only the last element.
- **Evidence:** `requests, err := gatewayRequests(c, gateway, contextName) ... r := requests[len(requests)-1]`
- **Impact:** Planning costs grow with the number of buckets times the cost of a full gateway build, with repeated catalog scans.
- **Fix:** Call `gatewayRequest(c, cluster, gateway, base)` directly, or memoize it per gateway inside Plan. (S)
- **Checked:** The pg-budget refusal is still reached by the gateway's own block, so skipping it here loses no check.

##### G19-10

**Low · Clearness · confidence high**

- **Where:** host.go:538, 567, 775; nvmeof.go:55-58, 174-177, 266
- **What:** `run(..., removal ...bool)` uses a variadic bool as an optional flag, and `r.target()` is computed twice (host.go:550 and 559). In nvmeof.go the default port 5500 is spelled twice, and "6h0m0s" is hardcoded instead of formatting `lifecycle.MaxDeadline`.
- **Evidence:** `if len(removal) != 0 && removal[0] {`; `past the 6h0m0s adapter limit`
- **Impact:** Small traps for readers, and a message that drifts if the limit changes.
- **Fix:** Use a named parameter or a values map, one `const nvmeofDefaultPort`, and `lifecycle.MaxDeadline.String()`. (S)
- **Checked:** `lifecycle.MaxDeadline = 6 * time.Hour` (invocation.go:135).

#### Patterns
- Package-wide helpers live in SMB files: `strictlySorted` (smb.go:94), `smbValue`, which serves as the generic API-to-JSON converter for bucket policy (object_bucket.go:75), and `smbMaterial` used by export (export.go:253).
- Admission is written as long `||` chains of identity clauses, which makes up the CC 25-40 functions: validateExportProduced (export_evidence.go:74), DecodeConsumerRequest (export.go:80), EvaluateSMBEvidence (smb_evidence.go:90). Named sub-predicates would make each refusal readable.
- Each data service repeats the same lifecycle shape: identity, then prepare against `ProvedCluster`, then `bindPhysicalHosts`, then material, then `runner.Run`, then the evidence check, then the outcome map. See replication.go:174-296, smb.go:422-551 and export.go:222-282 with export_evidence.go:156-191. A shared `scopedRun` helper could take about 40 lines out of each while keeping the per-service evidence functions separate.
- `e.ApplyProved`-or-`e.Proved` is re-implemented by hand (host_trust.go:27-30 and 44-47, disk_proof.go:14-17) even though `provedFor(e)` exists (host.go:410).

#### Strengths
- Destructive retirement requires positive proof at every step: an exact match to the original removal pair, an erase receipt that matches the MACs and UUID, serial/size/WWN coverage of the targets, and a proof that native cleanup happened for retained roots. Any ambiguity or absence returns `physicalPolicyFailure` (physical_retirement_proof.go:171-296, 386-413). No unknown reading passes.
- Credentials appear only as refs in requests: SMB bindings (smb.go:34-37), gateway certificates, host keys. Values are checked as material and cleared (smb_materials.go:16-27). Validators are tested never to echo secret values.
- Host-key trust never fetches a key or falls back to ambient trust. Keys come from pinned known_hosts, a delivered public key or apply proofs (host_trust.go:9-35), and FIPS rejects Ed25519 and RSA keys under 2048 bits (provided_host.go:83-117).
- The host-reading cache key zeroes members whose list is pinned against the Python `RESOURCE_MEMBERS` (host_reading_test.go:241-256), and a failed reading falls back to the block's own read (tested).

#### Coverage notes
- replication_evidence.go:61 `replicationRefusals` is at 0%, and replication.go:249/269/285 `run`/`Apply`/`Observe` at 33%/27%/38%. No runner test covers the replication postcondition gate: unexpected Produced material, an unknown outcome, or partial evidence through Apply.
- export_evidence.go:156/173 consumer `Apply`/`Observe` are at 33%/31%. The paths that clear produced credentials on failure are untested at the capability level.
- export_evidence.go:298 `validateExportGatewayEntries` is at 38%. The TLS chain branch (PEM type, headers, leaf `VerifyHostname`) is never fed a TLS gateway.
- host_trust.go:37 `proveFIPS` is at 59%, which leaves the provided-host branches of the sentinel in [G19-05](#g19-05) weakly pinned.
- physical_retirement_proof.go:327 `authorizePhysicalRetirement` (79%) and :171 `originalPhysicalDevices` (80%) leave some refusal clauses of the destructive gate unexercised.

#### Already recorded
- None of these findings match an audit 003 entry or an open B item. The searches covered smbMaterial, validateExportProduced, smbValue, proveFIPS, the lane file paths in audits/003-code/*.md, and backlog.md for refusal/known-hosts duplication. B595 (delivered) covers the refusal table test only. The B491 surviving-host refusal is the deliberate behaviour that [G19-01](#g19-01)'s reason text names.

<a id="lane-g20"></a>

### Lane G20: Storage: CephCluster kind admission and normalization, storage preflight (Go)
Scope read: I read every production file in internal/storage (effect.go, members.go), cephcluster (all 32), preflight (capacity, contracts, firewall, guests, service), preflightlocal (all 7) and rendering. I read admission_test.go (fixtures and assertion style) and the test lists of reuse_linux_test.go and parity_test.go. I only skimmed the other cephcluster tests, by grepping how they assert. I did not read the preflight and preflightlocal remote/firewall tests; I used the measured coverage for them.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 3 | Admission is pure and collects diagnostics, but it carries a dead retired-OSD vocabulary, and preflight throws away the cause of a failed probe |
| Security | 4 | Paths are opened by walking each component with openat and O_NOFOLLOW, every read is bounded, root ownership is proved, and argv is absolute; one /proc parsing idiom is weak |
| Efficiency | 3 | Service placement is re-resolved from scratch about 35 times per admission; host probes run one after another |
| Clearness | 3 | Rule tables are good, but normalizeCluster, nativeServices and nativePlacement each mix several unrelated phases |
| DDD | 4 | No I/O, typed api.Issue throughout, glossary names; but the placement derivation is computed by running the validator |
| Non-duplication | 2 | A second Shape walker, two copies of the secret-pointer check, two copies of the VIP check, three refusal-table types, and the TPM2 rule in three places |
| Right language | 5 | Go owns all of it; the firewall rules shared with the Python worker are held to one corpus, as the spec requires |
| Test coverage | 4 | 94% by any test, with exact issue assertions and an input-not-mutated check; the /proc runtime proof is never run |

#### Findings
##### G20-01

**Medium · Quality · confidence high**

- **Where:** internal/storage/cephcluster/devices.go:26, devices.go:70, topology.go:260, lexical.go:98-105, normalization.go:97-99, admission.go:263, nvmeof.go:42, api/v1alpha1/storage.go:31-45
- **What:** A whole retired camelCase OSD vocabulary is still in the package:
- `admission.osd`, `admission.selector` and `declaredDataDevices` read `dataDevices`/`pathSpecs`/`dbDevices`.
- `osdLexical` walks `topology.nodes[].osd` and `topology.osdDrivegroups`.
- `monitoring.prometheus.retention*` is still lexically checked.
- Normalize rewrites `node.osd.serviceOverrides.networks`.

None of these keys exists in the closed CephCluster schema any more (storage.go:102: topology.nodes has only name, fqdn, machineRef, site, labels, location).
- **Evidence:** `func (a *admission) osd(` has no caller (grep shows only the definition). Neither does `declaredDataDevices`, nor `nonemptyStrings`. `func (a *admission) nvmeofClusterIntrinsic(ceph api.Value) {` is an empty body that partial.go:68 still calls. `storageOSD`/`storageDeviceSelector` are 0% covered.
- **Impact:** About 120 lines of plausible-looking rules (two TPM2 messages, two selector grammars) mislead anyone changing OSD admission, and they hide which grammar is live.
- **Fix:** Delete devices.go except `cleanAbsolute`/`cleanDevice`/`rootDevice`, plus `declaredDataDevices`, the dead `osdLexical`/monitoring lines, normalization.go:97-99, `nonemptyStrings`, `nvmeofClusterIntrinsic`, and the unused api shapes. (S)
- **Checked:** I grepped callers and the schema. Strict shape validation (desiredstate/compilation/phases.go:44, schema_validation.go) refuses unknown keys before these rules could see them. No backlog item matches.

##### G20-02

**Medium · Efficiency / DDD · confidence high**

- **Where:** internal/storage/cephcluster/native.go:46-55, native.go:138-149, normalization.go:128, pools.go:207-215, topology.go:315-327
- **What:** Each placement is resolved by building a fresh `admission{object: cluster}` and re-running the whole `nativePlacement` validator. That rebuild compiles the regex or glob, scans nodes, and resolves each host with an O(nodes) `nativeNode` lookup. Nothing is memoized. The rebuild is called from 35 sites, 13 of which discard the issues.
- **Evidence:** `a := admission{object: cluster}`; `nodes := a.nativePlacement(cluster, service.Get("placement"), "$.placement", false)`. Some calls nest it per node: `nativeMember` sits inside a node loop in normalization.go:128 and in a sites × nodes × 2 loop in topology.go:315-327. `osdHostCount` is recomputed by every CephPool, CephObjectGateway and policy check.
- **Impact:** Cost is O(sites·nodes²·services) per object, multiplied by every storage child in the catalog. Fusing validation and derivation also means each new rule pays for it again.
- **Fix:** Split a pure `placementHosts(cluster, placement) ([]string, ok)` from the validator. Compute one per-cluster `map[serviceName][]host` once per Validate/Normalize call and pass it down. (M)
- **Checked:** No cache exists in the package. No backlog item matches.

##### G20-03

**Medium · Non-duplication · confidence high**

- **Where:** internal/storage/cephcluster/native.go:216-299; internal/desiredstate/compilation/schema_validation.go:12
- **What:** `nativeShape` is a second interpreter of `api.Shape`, separate from compilation's `validateShape`. It handles alternatives, types, mappings, sequences, bounds, enums and rules, but with its own messages and partial semantics.
- **Evidence:** nativeShape reports `"native value is not a supported option"`; validateString reports `"value is not one of the permitted choices: "`. nativeShape handles only `shape.Rule == "device-path"`, while compilation uses `api.ValidLexical(shape.Rule, ...)`.
- **Impact:** Shape features added to api (a new Rule, MinLength semantics, Unique) must be implemented twice, and they already diverge. For example, nativeShape silently ignores any Rule other than device-path.
- **Fix:** Export one shape walker from api (or from a small internal package) that takes a message style. Have native subtrees call it. (M)
- **Checked:** The native schemas currently use only `lexical("device-path")` (grep of storage_native.go/storage_smb.go), so no rule is lost today.

##### G20-04

**Medium · Non-duplication · confidence high**

- **Where:** internal/storage/cephcluster/native.go:563-576 vs smb.go:300-313; native_ingress.go:230-252 vs smb.go:705-726
- **What:** Two rules are copied by hand:
- The secret-binding pointer check, including its overlap loop.
- The virtual-address rules: inside a public CIDR with the same prefix length, not the network or broadcast address, not a Machine address, not another ingress's VIP.

- **Evidence:** Both binding copies read `if earlier == pointer || strings.HasPrefix(earlier, pointer+"/") || strings.HasPrefix(pointer, earlier+"/") {` with the identical message "secret binding paths cannot duplicate or overlap". Both VIP copies call `slices.IndexFunc(networks, func(network netip.Prefix) bool { return network.Contains(address) && network.Bits() == prefix.Bits() })` and `slices.Contains(machineAddresses(a.catalog), address)`.
- **Impact:** A fix to the VIP rule (IPv6 network address, a /31) or to the pointer grammar has to be made in two places.
- **Fix:** Extract `a.secretPaths(items, field)` and `a.virtualAddress(address, prefix, field, ownerLabel)`. (S)
- **Checked:** Audit 003 (report-claude.md:1745) verified that both behave correctly; it did not raise duplication.

##### G20-05

**Medium · Non-duplication · confidence high**

- **Where:** internal/storage/cephcluster/native_devices.go:94, devices.go:49-51, internal/storage/ceph/requests.go:606-608, requests.go:640-646
- **What:** The rules "TPM2 requires encryption" and "data/DB/WAL paths must not overlap" each exist in two places: admission (native_devices.go:88-95) and the ceph request builder. TPM2 has a third, dead copy in devices.go. Each copy has its own message.
- **Evidence:** requests.go:606 has `if service.Get("spec", "tpm2").Bool() && !service.Get("spec", "encrypted").Bool() { return failure("api.invariant", "TPM2 OSDs require encryption", ...`. native_devices.go:95 says "TPM2 OSD unlock requires encryption".
- **Impact:** The request-builder copies cannot fire after admission. They are a second rule owner whose messages can drift apart.
- **Fix:** Keep the rule in admission only. Have checkDevices keep only the adapter's own limits (paths-only, one OSD per device, one class with DB/WAL). (S)
- **Checked:** Admission runs before request compilation (cmd/bootwright/wiring_desiredstate.go:58).

##### G20-06

**Medium · Efficiency · confidence high**

- **Where:** internal/storage/preflight/service.go:299-306, service.go:394-397, guests.go:57-78
- **What:** Guest prerequisite checks run one host at a time. Each host gets its own 10 s `capacity.ProbeTimeout` and runs 6 or more SSH reads in sequence, plus peer and firewall reads.
- **Evidence:** `for _, name := range hostNames { checks, err := s.guestChecks(ctx, ...` and `probe, cancel := context.WithTimeout(ctx, capacity.ProbeTimeout)`.
- **Impact:** With unreachable or slow hosts, `preflight --stage clusters` grows linearly, about 10 s per host. This breaks the owner's parallel-by-default rule for independent work.
- **Fix:** Run the per-host probes with a bounded errgroup, then keep the existing sort of `result.Checks`. (S)
- **Checked:** grep finds no goroutines or errgroup in preflight or preflightlocal. No backlog item matches.

##### G20-07

**Low · Quality · confidence medium**

- **Where:** internal/storage/preflight/service.go:401-403, firewall.go:471-474
- **What:** When a probe errors, both the error and the cause are thrown away. The user only sees "did not complete". By contrast, preflightlocal's `recordReuse` (observer_linux.go:107-118) keeps a truncated cause.
- **Evidence:** `if err != nil || expired != nil || len(checks) == 0 { check.Reason = "the bounded guest prerequisite observation did not complete"` and `case err != nil: return check`.
- **Impact:** An SSH auth failure, a timeout and an unexpected firewalld output all look the same to the operator.
- **Fix:** Add a bounded, normalized cause class (timeout, unreached, output form) to Reason. Never add raw output. (S)
- **Checked:** The untrusted-key and fleet-unreached cases are already distinguished (guests.go:60-67).

##### G20-08

**Low · Clearness · confidence medium**

- **Where:** internal/storage/cephcluster/normalization.go:57 (cx 44), native.go:410 (cx 42, depth 5), native.go:71 (cx 43, 100 lines)
- **What:** These functions were squeezed under the 100-line ceiling, not split by meaning.
- normalizeCluster runs nine independent derivations in sequence: defaults, image version, checksum case, SSH defaults, CIDRs, node fqdn/site, bootstrap address, stretch defaults, binding sort.
- nativeServices inlines the MON `crush_locations` rule (lines 460-475, depth 5) and the config refusals.
- nativePlacement both validates the selector grammar and selects hosts.

- **Evidence:** The measured metrics are above. nativePlacement is exactly 100 lines.
- **Impact:** Each new field adds to one function that already sits at the ceiling.
- **Fix:** Give each normalization step its own helper, extract `a.monCrushLocations(...)`, and do the [G20-02](#g20-02) split. (S)
- **Checked:** nativeShape's complexity is intrinsic to a shape walker; [G20-03](#g20-03) covers it.

##### G20-09

**Low · Security · confidence medium**

- **Where:** internal/storage/preflightlocal/reuse_linux.go:178-187
- **What:** `processStart` finds the end of the `/proc/<pid>/stat` comm field with the first `") "` instead of the last `)`. Comm may itself contain `") "`.
- **Evidence:** `_, tail, found := strings.Cut(string(data), ") ")`.
- **Impact:** Field 22 (the start time) can be misread. The PID-reuse guard could then compare the wrong field. It fails closed only by chance.
- **Fix:** Use `strings.LastIndexByte(text, ')')`. (S)
- **Checked:** qemu's comm is normally `qemu-kvm`, so this is low risk. The function is 0% covered.

##### G20-10

**Low · Quality · confidence medium**

- **Where:** internal/storage/cephcluster/gateway_dns.go:33-90, disk_protections.go:33,45
- **What:** Exported derivations used by the reconciliation wiring return plain `fmt.Errorf` strings with no diagnostic code or remediation. The rest of storage uses `diagnostics.NewFailureWithRemediation`.
- **Evidence:** `return nil, fmt.Errorf("%s has no gateway endpoint FQDN", export.Identity())`. cmd/bootwright/wiring_reconciliation.go:527 passes the error through unchanged.
- **Impact:** These failures reach output uncoded, so machine output cannot classify them.
- **Fix:** Return typed failures with remedies. (S)

##### G20-11

**Low · Efficiency / Quality · confidence high**

- **Where:** internal/storage/cephcluster/native.go:262-268, firewall.go:566, preflight/service.go:201 and :283
- **What:** Three small issues:
- nativeShape clones the whole sequence inside its own loop (`value.Items()[:i]`), which is O(n²) work, and reports one issue per earlier duplicate.
- `slices.Compact(commands)` runs on an unsorted list, so duplicate remedies that are not adjacent survive.
- Check fetches Ownership twice per run, once in CheckCapacity and once directly.

- **Fix:** Hoist the items, then sort and compact the commands, then pass the ownership map through. (S)

#### Patterns
- There are three refusal-table types with the same "strip a member and issue" algorithm: `refusedMember` (native.go:504), `smbRefusal` (smb.go:13) and `nfsExportRefusal` (nfs_export.go:11). withoutRefusedExportMembers (nfs_export.go:37-48) and the first loop of withoutRefusedSMBMembers (smb.go:113-121) are line-for-line the same. One path-keyed table type would serve all three.
- The same grammar appears more than once. Retention: lexical.go:16-17 and observability.go:240-241. DNS names: observability.go:243 alongside `api.ValidLexical("dns")`. CIDR canonicalization: nfs_export.go:132 and normalization.go:164. Display names are checked with `NativeVersion`, a misleading name (object.go:476).
- One defect can produce several identical diagnostics:
  - The missing-core loop (native.go:488-493) emits the same message once per missing MON/MGR/OSD.
  - The machine-overlap pair loop (replication.go:45-50) has no break.
  - nativeShape's numeric bounds emit two issues on a parse failure.
- Package-level exported mutable maps and slices: `ZonePlacementSuffixes`, `ZonePoolSuffixes` (pools.go:444-446).

#### Strengths
- Admission is pure domain code: no I/O imports in production files, the input value is cloned (`Items()` returns `slices.Clone`), issues are collected rather than returned on the first one and capped at 999, and admission_test.go:127 asserts that Normalize leaves its input unchanged.
- Rule tables make the policy readable and complete: `bindingSlots` (native_bindings.go:31), `nativeServiceTypes`, `ingressArms`, `reservedListeners`/`nativeListeners`.
- preflightlocal's file access is exemplary. It walks each path component with `openat` and `O_NOFOLLOW|O_CLOEXEC`, reads under a byte bound with LimitReader(limit+1), proves root ownership and no group/world write, and parses remote `tail -v` output strictly.
- Firewall grant and selection logic is pinned to one JSON corpus shared with the Python worker.

#### Coverage notes
- preflightlocal/reuse_linux.go:133 `processCredit`, :178 `processStart`, :69 `runtimeMemory` and :235 `readRootProof` are 0% covered. The PID identity and reuse proof and the root-owned domain proof are the security-relevant branches of local capacity credit.
- cephcluster/native.go:193 `nativeHostNetwork` is at 33%: the check that a host-token network agrees with the bound Machine address is never run.
- cephcluster/native.go:172 `nativeGlob` is at 71%: the `[!`/`[^` translation, which is fnmatch semantics, is only partly exercised.
- preflight/service.go:73 Check: 8 statements are uncovered, including the cluster-name refusal and the external-cluster branch.
- cephcluster/gateway_dns.go:55: 7 of the error returns are untested.

#### Already recorded
- Audit 003 (report-claude.md:6926) cites devices.go:58-59 together with native_devices.go for the TPM-profile scope. That is a spec issue; [G20-01](#g20-01) adds that devices.go is dead.
- Audit 003 (report-claude.md:7079, CPR-04/05) covers firewall IPv6 selection and the unsettled-state diagnostic; I did not repeat them.
- None of [G20-01](#g20-01) to [G20-11](#g20-11) is tracked by a B-item.

<a id="lane-g21"></a>

### Lane G21: Container cluster (OpenShift admission, agent installation, networking, access, preflight)
Scope read: read in full: openshift/admission.go, network_admission.go, topology.go, endpoint_ownership.go, network_overlaps.go, addon_facts.go; agentinstall/selection.go, install.go, projection.go, requests.go, evidence.go, install_manifests.go, proxy.go, registry.go, admin_credentials.go, produced_identity.go, volume.go, capability.go (lines 40-440); networking/render.go, planning.go, requests.go, capability.go; access/handoff.go, service.go; preflight/service.go; preflightlocal/resolver.go; root members.go, facts.go, effect.go; installation/service.go. Also read, to compare: storage/ceph/admin_credentials.go, substrate/baremetal/capability.go:100-180, managedos/installation/network.go:95-131, the role containercluster_media_agent/tasks/build.yml:50-119, and specs/cli/cluster-access.md:1-55. Skimmed: access/handoff_test.go and the list of agentinstall tests. Not read, for lack of time: about 12 small agentinstall and openshift files (placement, catalog, encryption, pushed, unresolved, secret_references, inspection, endpoints, virtual_ips, node_network, pushed_admission, controller_tools; together about 900 lines) and most _test.go bodies.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed decoding and evidence checks are rigorous; a few latent asymmetries and unchecked assertions remain |
| Security | 4 | Secrets are named by reference and lent at execution, chrony and proxy inputs are lexically validated; the handoff denylist has a gap |
| Efficiency | 3 | Planning runs the whole derivation twice; admission rebuilds the catalog-wide address list for each endpoint |
| Clearness | 3 | Dense one-line issue builders, three encodings of address family, installer documents carried as `map[string]any` |
| DDD | 3 | openshift holds a cross-context address registry that reads other kinds' wire paths |
| Non-duplication | 3 | The admin-credential flow and the mutate/observe skeleton are cloned across capabilities; endpoint address resolution is written three ways |
| Right language | 3 | Go projects install-config deterministically, but the bound material is composed in sizeable Jinja |
| Test coverage | 4 | openshift 95.7%, agentinstall 88.5%; the admin-credential path and preflightlocal are at 0% |

#### Findings
##### G21-01

**Medium · Test coverage / Quality · confidence high**

- **Where:** internal/containercluster/agentinstall/admin_credentials.go:24-66, :47
- **What:** The console-password preparation has security-relevant branches that no test in the module runs: guard decode, the password length bounds, the hash step and re-encoding. The check for a missing hasher comes after the remote prepare run has already executed.
- **Evidence:** gocov-any: `PrepareAdminCredential` 27/27 statements uncovered and `adminCredentialRun` 22/22. Line 43: `len(prepared.Password) < 23 || len(prepared.Password) > 64`. Line 47: `if c.hashPassword == nil {` follows `c.adminCredentialRun(...)` at line 25.
- **Impact:** A regression in credential bounds or in clearing ships unnoticed, and a mis-wired capability reaches the cluster before it refuses.
- **Fix:** Check `hashPassword == nil` before line 25. Add fake-runner tests for each refusal and for the success encoding. (S)
- **Checked:** Only lifecycle/admin_credentials_test.go and cli tests mention Prepare/UpdateAdminCredential, and they use fakes, not this capability. The ceph twin is also at 0%.

##### G21-02

**Medium · Non-duplication · confidence high**

- **Where:** agentinstall/admin_credentials.go:24-114; internal/storage/ceph/admin_credentials.go:26-89
- **What:** `PrepareAdminCredential`, `UpdateAdminCredential` and `adminCredentialRun` are line-for-line the same skeleton in both packages: run, double `ClearProduced`, the `AdminCredentialProved` check, the `"guard"` output, `prepare_credentials` vs `update_credentials` wiring, `ReadOnly` and `Unrecorded=false`. Only the material binding and the guard post-processing differ.
- **Evidence:** Both have `run.ReadOnly = operation == "prepare_credentials"` / `run.Unrecorded = false` and the same `if operation == "prepare_credentials" { call.Outputs = ... } else { call.Materials = append(... "admin-update" ...) }`.
- **Impact:** A fix to proof or clearing semantics must be made twice, and neither copy is tested.
- **Fix:** Add a lifecycle helper (for example `lifecycle.AdminCredentialRun(ctx, runner, e, op, base Invocation, update)` plus `PrepareGuard(result, digest)`). Each capability then supplies only its invocation base and its guard transform. (M)
- **Checked:** The consumer-owned port policy does not cover this: these are implementations, not ports.

##### G21-03

**Medium · Non-duplication · confidence high**

- **Where:** agentinstall/capability.go:165-194, agentinstall/install.go:221-257, substrate/baremetal/capability.go:136-165 (also libvirt/capability.go:298-336, artifactserver/capability.go:129-164 per clones-go-prod-shape)
- **What:** Apply/Destroy → `mutate` → `prepare` → `run` → the `"changed"/"unchanged"` outcome switch → presence or absence validation, plus `Removal` and `observe`, are cloned per capability. The shape clones are 42, 25, 22 and 21 lines.
- **Evidence:** `case "changed": outcome = reconciliation.OutcomeChanged / case "unchanged": ... default: return unknown, refusal("lifecycle.state", "the ... adapter reported no usable outcome", "")` appears in each.
- **Impact:** Outcome mapping and produced-material clearing drift per arm. The Install copy clears produced material on every exit; the Media and baremetal copies have nothing to clear but keep the same skeleton.
- **Fix:** Add a shared `lifecycle.Mutation{Prepare, Run, Presence, Absence}` helper that owns the outcome mapping, validator dispatch and `ClearProduced`. Each arm keeps only its validators and invocation. (M)
- **Checked:** Opened both sides of the 42-line and 22-line pairs. The differences are names, messages and the produced-output handling.

##### G21-04

**Medium · Right language · confidence medium**

- **Where:** ansible/.../roles/containercluster_media_agent/tasks/build.yml:59-94
- **What:** Go freezes install-config with placeholders (projection.go:60-67), but the role composes the bound material in a 35-line Jinja expression. It selects the trust bundles by index regex, chooses between the username, auth and password mirror credentials with nested inline conditionals, merges them into the pull secret, joins the trimmed bundles and injects the proxy credentials.
- **Evidence:** `range(... trustBundleRefs | length) | map('string') | map('regex_replace', '^', 'trustBundle') | map('extract', bootwright_cluster_media_material)`; `lookup(... mirrorUsername) if ... is defined else lookup(... mirrorAuth) if ... is defined else ''`.
- **Impact:** Secret-bearing composition logic sits in the language the Language responsibility table reserves for thin orchestration. It is hard to unit-test and easy to break silently under `no_log`.
- **Fix:** Move the substitution into one action plugin or filter, for example `installer_config(request, material)`, that takes the frozen document plus the bound files and returns YAML. Keep the task to one call. (M)
- **Checked:** Partial plugins exist (`registry_pull_secret`, `installer_proxy_credentials`, `to_installer_yaml`), so the leftover glue is the cost. tests/unit/test_containercluster_install_config.py exists. This sits on the Y-lane boundary; flagged here because the brief asks about the Go/Jinja split.

##### G21-05

**Medium · DDD / Efficiency · confidence high**

- **Where:** openshift/endpoint_ownership.go:20-89, :106; openshift/network_admission.go:166-198; openshift/addon_facts.go:63
- **What:** `addressClaims` is a catalog-wide registry of who holds which address, and it lives in the OpenShift admission package. It reads other contexts' wire paths directly. `networkHolders` is a second, overlapping holder enumeration in the same package. `endpointCollisions` rebuilds and re-sorts the full claim list for each endpoint slot of each cluster.
- **Evidence:** `case api.CephCluster: ... service.Get("spec", "virtual_ip")`, `case api.CephSMBResource: ... spec.Get("native", "public_addrs")`. At line 106, `for _, claim := range addressClaims(catalog) {` is called from validateEndpoints' slot loop (admission.go:602).
- **Impact:** Every new address-bearing kind must be added in the openshift package. The missing Ceph case (audit ADA-09) shows the failure mode.
- **Fix:** Let each owning context export its claims, as `substrate.AddressClaims(holder)` already does at line 57. Compose them once in `network`. Compute claims once per Validate call and pass them in. (M)
- **Checked:** The Ceph case is now present (line 46), so ADA-09 is resolved; this finding is about placement and repetition, not that defect.

##### G21-06

**Low · Non-duplication · confidence high**

- **Where:** preflight/service.go:161-190; infrastructureservices/managedservice/selection.go:194-221; openshift/endpoint_ownership.go:64-79
- **What:** The managed-service endpoint address is resolved three ways. `ServiceEndpoint` falls back to the sole endpoint and uses `ResolveAddress`. `clusterDNS` matches only an exact `endpointRef`, then falls back to `bindAddress`, and stops at the first server of the first node. `addressClaims` matches `addressRef` names by hand.
- **Evidence:** service.go:175 `if endpoint.Get("name").Text() == selection.Get("endpointRef").Text()` with no sole-endpoint fallback, against selection.go:231 `if items := endpoints.Items(); len(items) == 1`.
- **Impact:** With `endpointRef` omitted, the preflight remedy names `bindAddress` or an empty address instead of the endpoint the installation uses.
- **Fix:** Call `managedservice.ServiceEndpoint` from `clusterDNS`. (S)
- **Checked:** The address only feeds `check.Next` text, so this is Low.

##### G21-07

**Low · Security · confidence medium**

- **Where:** access/handoff.go:70-90
- **What:** The handoff argument denylist refuses `--show-token` but not oc's short form `whoami -t`. The spec says "token reveals ... refuse `access.handoff`".
- **Evidence:** Line 85 lists long flags only. The short-flag guard at line 80 covers only `-s`. The test (handoff_test.go:64) checks only long forms.
- **Impact:** A descriptor can be issued for a token-revealing command, against the spec. The real impact is limited: Bootwright does not run the client, and kubeadmin kubeconfigs are certificate-based.
- **Fix:** Refuse `-t` when `args[0] == "whoami"` (or any `-t`), and add the case to the test table. (S)
- **Checked:** specs/cli/cluster-access.md:33-36.

##### G21-08

**Low · Efficiency · confidence high**

- **Where:** agentinstall/capability.go:50, agentinstall/install.go:56, agentinstall/selection.go:44,249
- **What:** `MediaCapability.Plan` and `InstallCapability.Plan` each call the full `Requests()` derivation and discard half of it. `Refusals` (from each `Unsupported`) and each `requestFor` call `refusedCluster` again. Each pass composes every node's network and `TargetFor` several times. In admission, `machine.InstallAddress` and `ComposeNetwork` run in about eight helpers per Validate.
- **Evidence:** `requests, _, requirements, err := Requests(...)` against `_, requests, requirements, err := Requests(...)`.
- **Impact:** Planning cost is roughly four times what is needed per cluster. It is acceptable at today's catalog sizes but grows with node count.
- **Fix:** Memoize per-node projections per catalog, or have one planner feed both capabilities. (M)
- **Checked:** There is no cache in `Requests`.

##### G21-09

**Low · Clearness · confidence high**

- **Where:** agentinstall/selection.go:161 vs :463
- **What:** The refusal pass calls `substrate.TargetFor(catalog, bound, cluster.Name(), controllerMachine)`, passing the cluster name as the context name. Derivation passes the real `contextName`.
- **Evidence:** target.go:254,265,293 use `contextName` for domain UUIDs, names and MACs.
- **Impact:** This is harmless today, because no refusal depends on derived names. It is a misleading argument, and a future context-dependent refusal would let selection and derivation disagree, which the doc comment says must never happen.
- **Fix:** Thread the context name into `Refusals`/`unsupportedReason`, or pass `""` with a comment. (S)
- **Checked:** Only physical targets are refused on `target.Controller`, and those ignore the context.

##### G21-10

**Low · Clearness / DDD · confidence high**

- **Where:** openshift/admission.go:837-850, :742; openshift/network_admission.go:45-48
- **What:** One package encodes address family three ways: `networkFamily` returns 4, 6 or -1; `familyBits` uses 32 or 128; `network.Family` returns "ipv4" or "ipv6".
- **Evidence:** `next := 6; if prefix.Addr().Is4() { next = 4 }` / `familyBits = networks[0].Addr().BitLen()` / `family = network.Family(out[0].Addr())`.
- **Impact:** It invites comparison bugs and is primitive obsession for a domain concept.
- **Fix:** Use one `network.Family` type everywhere, with a mixed sentinel. (S)
- **Checked:** This is not a deliberate policy.

##### G21-11

**Low · Quality · confidence medium**

- **Where:** networking/planning.go:32,37,50,67; networking/requests.go:47-69
- **What:** The networking capability returns bare `fmt.Errorf` for operator-facing refusals, with no diagnostic code or remediation. The sibling agentinstall uses `refusal(code, msg, remedy)` throughout.
- **Evidence:** `return plan, fmt.Errorf("the node networks of %s require an executable nmstate package", cluster.Identity())`.
- **Impact:** The CLI cannot classify or remediate these refusals uniformly.
- **Fix:** Use `diagnostics.NewFailureWithRemediation` with `lifecycle.state` or `api.required` codes. (S)
- **Checked:** Did not trace how an uncoded error is rendered.

##### G21-12

**Low · Clearness / DDD · confidence medium**

- **Where:** agentinstall/requests.go:168,177; selection.go:313; install_manifests.go:161; capability.go:346
- **What:** The installer documents are frozen as `map[string]any`. Later code inspects them with unchecked assertions that are safe only because of distant invariants.
- **Evidence:** `Rendezvous: agent["rendezvousIP"].(string)`; `m.Content["metadata"].(map[string]any)["labels"].(map[string]any)[machineConfigRole].(string)`; `request.InstallConfig["fips"] == true`.
- **Impact:** A projection change can panic at plan or execution time instead of failing to compile.
- **Fix:** Give `InstallConfig` and `AgentConfig` typed structs with JSON tags, and keep `map[string]any` only for the embedded NMState. (M)
- **Checked:** validateInstallManifests runs before validateTimeManifests (requests.go:314-317), and agentConfig always sets `rendezvousIP` (projection.go:243), so the assertions are guarded today.

##### G21-13

**Low · Quality · confidence high**

- **Where:** agentinstall/install.go:466-479
- **What:** The user material for a hosted (KubeVirt) node is redirected to the lent host credential, but the password material still names `node.Controller.CredentialsRef`.
- **Evidence:** `credential = hosted[hostedKey(node.Controller)]` … `Secret: node.Controller.CredentialsRef, Variable: "controllerPassword"...`.
- **Impact:** Latent only: KubeVirt has no password part (`substrate/facts.go:28`, `credential: [2]secrets.Part{secrets.ValuePart}`), so the branch never runs for hosted nodes.
- **Fix:** Use `credential` for both parts. (S)
- **Checked:** `ControllerCredential` for the other arms returns username and password.

##### G21-14

**Low · Non-duplication · confidence high**

- **Where:** agentinstall/projection.go:370-401; managedos/installation/network.go:100-131
- **What:** `nativeValue` and `nativeDocument` are an exact 31-line clone that converts an `api.Value` to plain data.
- **Impact:** The integer and number fallback rules can diverge between the two installers.
- **Fix:** Expose one `api.Value.Native()`, or a helper in `api`. (S)
- **Checked:** Both sides opened; only the identifiers differ.

#### Patterns
- Test-only seams kept in production code: `InstallCapability.run` (install.go:401) and `MediaCapability.run` wrappers are called only by tests (install_test.go:764, pin_test.go:69, kubevirt_test.go:230), alongside `runWithOutputs`.
- Bounded local process runners are re-implemented per adapter: the `capture` writer plus `nativeexec.Qualify` plus `exec.CommandContext` appear in preflightlocal/resolver.go:16-43 and controller/firewalllocal/native.go:16-34.
- Issue construction mixes the capped `add` (admission.go:933) with uncapped `append` (network_admission.go throughout). The cap holds only because Validate re-wraps the results.

#### Strengths
- Evidence validators (evidence.go) are exhaustive positive-proof readers: request digest binding, `*bool` presence for `booted`/`inserted`, and the OwnMedia ⊆ Media invariant.
- Secrets never enter frozen requests. Install-config carries placeholders (projection.go:60-67), `SecretReferences` names every binding, and kubeconfig identity hashes the CA rather than exposing key material (produced_identity.go).
- Generated content is guarded against injection: chrony servers are checked with `ValidLexical("host")` and round-trip proved (install_manifests.go:26,147), proxy `noProxy` uses grammar translation, and the mirror URL refuses userinfo (registry.go:55).
- No component version rules: topology uses one general table with upstream sources (topology.go:26-39), and releases differ only in data.
- The selection/derivation single-owner design (`refusedCluster` feeds both `Refusals` and `requestFor`) keeps pre-registration refusals identical to derivation failures.

#### Coverage notes
- agentinstall/admin_credentials.go:24,69,82,116: 0% by any test (see [G21-01](#g21-01)).
- preflightlocal/resolver.go:18-43: 0%, including the 64 KiB bound and the argv allowlist that refuses anything but `getent ahosts`.
- networking/requests.go:46 `Request.validate`: 59%. The off-controller placement and the "cannot place secrets or install operators" refusals are the security-relevant branches likely uncovered.
- networking/capability.go:62-117: Removal, Destroy, Quiescent, ObserveRemoval and Unresolved are 0% in the package's own tests.
- networking/planning.go:26 `Plan`: 66% own and 80% any. The missing-nmstate refusal and the controller-mismatch branches are likely uncovered.
- preflight/service.go:192 `capacityChecks` is 74% own; the dry-run Pending rewrite and the ownership-destroyed allocation branches are likely uncovered.
- Root package containercluster: 11.8% own (`SessionNode`, `MemberMachines`, `EffectBearing` at 0% own), covered only from other packages.

#### Already recorded
- Audit 003 SEC-01: getent qualification and working directory. Now fixed at HEAD (resolver.go:29 `nativeexec.Qualify`, :34 `Dir = "/"`).
- Audit 003 RCS-02: the physical agent root-device `deviceName` `/dev/sda` conflict (selection.go:409-446).
- Audit 003 ADA-09: Ceph VIPs missing from address claims. The case is now present (endpoint_ownership.go:46); [G21-05](#g21-05) is about placement and repetition.
- No open B-items match the lane's findings (backlog grep for agentinstall, containercluster, handoff and admin credentials).

<a id="lane-g22"></a>

### Lane G22: Add-ons: admission, catalog, packages, instances, cluster object programs (Go)
Scope read: I read every production file in full under internal/addons/ (root: admission.go, admission_olm.go, authored.go, authored_selection.go, bindings.go, effect.go, instances.go, packages.go), catalog/service.go, clusterobjects/ (types, program, validation, evidence, evidence_validation) and driver/ (run, evidence, placements, contracts), instance/ (all 11 files) and packages/ (all 9 files). / Skimmed: driver/run_test.go and clusterobjects/program_test.go; grep across the other _test.go files; the Python adapter cluster_objects_model.py in full and cluster_objects_engine.py lines 1-110; cmd/bootwright/wiring_desiredstate.go:45-82; compilation/phases.go:270-295; the matching rows of audit 003. / Not read: the remaining tests in full (I ranked them by measured coverage instead), and cluster_objects_native.py (it belongs to ADD-16 and another lane).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Closed, fail-closed validation throughout; errors are classified by message text in one place, and there is some dead code |
| Security | 4 | Bodies are typed value trees under canonical JSON, digest-bound and checked again at freeze and thaw; one content rule is too broad |
| Efficiency | 3 | Selection, cluster facts and program validation run many times for each object and each plan |
| Clearness | 3 | Long boolean chains (complexity 36-47), the repeated magic number 16, and single-line package literals |
| DDD | 4 | clusterobjects is pure policy with no I/O; the ownership rule has a Go mirror with no consumer |
| Non-duplication | 3 | Object builders, the placement-input derivation, the CRD-gate rule and deadline formulas each exist twice or more |
| Right language | 4 | Go owns the program and judges the evidence; the strict-comparison kind table lives in Python |
| Test coverage | 4 | 81-96% by package; some security-relevant refusals and the bound classification are unpinned |

#### Findings

##### G22-01

**Medium · Non-duplication · confidence high**

- **Where:** internal/addons/clusterobjects/evidence.go:130, :160; internal/addons/clusterobjects/program.go:81-91; ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_model.py:28, :44-60
- **What:** `EvaluateOwnership`, `EvaluateDependent`, `OwnerMark`, `FieldManager`, `Probe` and `DependentProbe` are Go copies of the Python ownership and owner-mark rules. Nothing outside tests calls them. The Go golden vectors (`testdata/ownerMark.golden`, `fieldManager.golden`) are read by no Python test, and the Python tests compute the expected mark with `model.mark` itself.
- **Evidence:** grep finds no non-test caller of `EvaluateOwnership(`, `EvaluateDependent(` or `FieldManager(`; `OwnerMark` is called only by `EvaluateOwnership`. Python repeats the rule: `recent = timestamp(...) >= timestamp(context["registered"]) - 5`, while Go has `ClockSkew = 5 * time.Second`. test_cluster_objects.py:26 does `annotations={model.OWNER: model.mark(...)}`.
- **Impact:** Two copies of a rule that protects against writing to the wrong target. The live copy (Python) is checked only against itself, so the Go goldens give false assurance.
- **Fix:** Delete the Go mirrors and their tests, or keep `OwnerMark`/`FieldManager` and make a Python unit test assert the hex in the Go golden files. (S)
- **Checked:** the call graph (none outside tests); the spec defines the mark at add-ons.md:250 (spec text, not a parity test); contract_versions_test.go pins only the domain string.

##### G22-02

**Medium · Quality · confidence high**

- **Where:** internal/addons/instance/selection.go:80-86; internal/addons/clusterobjects/validation.go:64, :102, :105; internal/addons/clusterobjects/program.go:140
- **What:** `unrenderable` chooses its refusal text by substring-matching `err.Error()` against literals that copy the validator's message text. The validator writes the bound numbers as literals, not from `MaxSteps`, `MaxObjects` and `MaxChecks`.
- **Evidence:** `var renderBounds = []string{"64 owned objects", "32 checks", "96 steps", "request bound"}` and `if !strings.Contains(err.Error(), bound) {`; `return fmt.Errorf("cluster object program exceeds 96 steps")`.
- **Impact:** Changing a bound constant or a message silently drops the specific refusal ("passes its 64 owned objects") for a generic one. No test pins these branches (grep for "passes its" in the tests is empty).
- **Fix:** Return a typed `co.BoundError{Bound string}` built from the constants and match it with `errors.As`. (S)
- **Checked:** no sentinel exists; no test covers the bound path.

##### G22-03

**Medium · Efficiency · confidence medium**

- **Where:** cmd/bootwright/wiring_desiredstate.go:55; internal/addons/instances.go:52, :117-129, :224; internal/addons/instance/selection.go:55-57; internal/addons/instance/capability.go:55-62; internal/addons/instance/render.go:61, :89
- **What:**
- Admission: compilation runs every rule's `Validate` for every object (phases.go:281-288). The add-ons closure computes `addonFacts(c, clusterRef)` before checking the kind, so it runs for every object. That includes `AddonExportFacts`, which walks every CephConsumer.
- `ValidatePackages` calls the full `Instances(c, r)` once per binding. `Instances` rebuilds the `uniqueObjects` maps for each binding, and `selectionField` expands every profile again for each selected add-on.
- Planning: `Unsupported` and `Plan` each render every instance. Each render calls `Freeze` and then `Canonical`, and `Plan` calls `validatePlacements` and `Canonical` again, so each program is validated about seven times.
- **Evidence:** `return addons.ValidateWithRegistry(o, c, registry, addonFacts(c, o.Spec().Get("clusterRef").Text()))`; `_, selection := Instances(c, r)`.
- **Impact:** Cost grows roughly as objects × catalog during admission, and repeated JSON thaws during planning. Today's inventories are small, so this is a scaling cost, not a measured slowdown.
- **Fix:** Build facts only for `ClusterAddonBinding`; compute `Instances` once per catalog and index its issues by binding; drop the second `Canonical` in `Plan`, since `Render` already proved it. (M)
- **Checked:** the compilation loop calls each rule for every record; `ValidateWithRegistry` returns `Validate(o, c)` for other kinds only after the facts are built.

##### G22-04

**Low · Quality · confidence high**

- **Where:** internal/addons/instance/render.go:188-207; internal/addons/clusterobjects/types.go:110-117; internal/addons/packages.go:151; internal/addons/admission.go:222
- **What:** No package ever sets `Rendered.SubscriptionConfig`, so the `settings != nil` branch of `subscribe` and the types `co.SubscriptionConfig` and `EnvVar` are unreachable. `uniqueNamedValues` has no caller.
- **Evidence:** grep for `SubscriptionConfig` finds only the declarations, the `subscribe` parameter and a test asserting it is `nil` (authored_test.go:58). Measured: subscribe 36% covered (14 of 22 statements unrun by any test); uniqueNamedValues 0%.
- **Impact:** Dead paths that are never tested sit in the program builder and mislead readers about what a package can render.
- **Fix:** Delete them; there is no compatibility to keep. (S)
- **Checked:** production callers in internal/ and cmd/; backlog (no item).

##### G22-05

**Low · Non-duplication · confidence high**

- **Where:** internal/addons/instance/render.go:115; internal/addons/packages/registry.go:24; internal/addons/packages/gitops.go:80; internal/addons/packages/kubevirt.go:117, :151
- **What:** `instance.nativeObject` and `packages.object` are byte-for-byte the same. gitops builds its ClusterRoleBinding body by hand, duplicating kubevirt's `binding()`. `textList` re-implements `api.StringList(values...)`.
- **Evidence:** both functions build `metadata`, then `body := api.MapValue(...apiVersion...kind...metadata)`, then `if spec.Present() { body = body.With("spec", spec) }`.
- **Impact:** Object-shape rules (namespaced flag, metadata layout) can drift between operator prelude objects and package objects.
- **Fix:** One exported `co.NewObject(apiVersion, kind, namespace, name, spec)` plus an RBAC-binding helper in packages. (S)
- **Checked:** the clone tables (windows below 10 lines are not listed); I opened both sides.

##### G22-06

**Low · Non-duplication · confidence high**

- **Where:** internal/addons/instance/render.go:65-80; internal/addons/instance/requests.go:109-130, :42; internal/addons/clusterobjects/driver/run.go:84; internal/containercluster/networking/requests.go:30
- **What:** The derivation of produced inputs and Secret names from placements is written twice, once to build the request and once to verify it. The deadline formula `budget + 10*time.Minute` is written three times; `validate` checks `r.Deadline()`, but the driver computes the deadline again on its own.
- **Evidence:** `Deadline: time.Duration(config.BudgetSeconds)*time.Second + 10*time.Minute` (run.go:84) and `return time.Duration(r.BudgetSeconds)*time.Second + 10*time.Minute` (requests.go:42).
- **Impact:** Changing the slack in one place lets the validated deadline differ from the one the runner receives.
- **Fix:** Extract `placementInputs(program)` and use it in both places; give `driver.Config` a `Deadline` taken from `Request.Deadline()`. (S)
- **Checked:** the driver test pins the formula itself, not a single source of it.

##### G22-07

**Low · Non-duplication · confidence high**

- **Where:** internal/addons/admission_olm.go:174-186; internal/addons/authored.go:187-199
- **What:** The rule "the gate holds a resourceExists check of a CRD named `<plural>.<group>`, with a non-empty, dot-free plural" is implemented twice: once over `api.Value` for admission and once over `co.Check` for rendering.
- **Evidence:** `if plural, found := strings.CutSuffix(exists.Get("name").Text(), "."+group); found && plural != "" && !strings.Contains(plural, ".")` against `plural, found = strings.CutSuffix(crd.Name, "."+group)` … `if found && plural != "" && !strings.Contains(plural, ".")`.
- **Impact:** Admission and render can disagree, so an add-on would be admitted but refused at plan.
- **Fix:** In admission, convert with `programCheck` and call one helper in clusterobjects. (S)

##### G22-08

**Low · Security · confidence medium**

- **Where:** internal/addons/clusterobjects/validation.go:215
- **What:** `safeBody` refuses the keys `status`, `managedFields` and `bootwright.io/owner` at every depth, not just at the top level and under `metadata.annotations`.
- **Evidence:** `if key == "status" || key == "managedFields" || key == OwnerAnnotation || strings.Contains(key, "{{") ...`, applied recursively.
- **Impact:** An authored custom resource with a nested `spec.x.status` field, or a ConfigMap data key `status`, is refused with "reserved field". The spec (add-ons.md:229-230) does not say at which depth the rule applies.
- **Fix:** Check `status` and `managedFields` at the top level and the owner key under `metadata.annotations`; keep the template-delimiter check at every depth. (S)
- **Checked:** program_test.go:116 tests only top-level and annotation positions.

##### G22-09

**Low · Right language · confidence medium**

- **Where:** ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_model.py:108-113
- **What:** The list of kinds and paths that must compare exactly (Subscription `spec`; RBAC `subjects` and `rules`) is a product rule (add-ons.md:299-300), but it is hard-coded in the Python adapter. The Go program cannot express it.
- **Evidence:** `strict = ((obj["kind"] == "Subscription" and key == "spec") or (obj["kind"] in ("Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding") and key in ("subjects", "rules")))`.
- **Impact:** A new package object that needs exact comparison needs a Python change that Go cannot see or test.
- **Fix:** Have Go emit an `exact: [paths]` list on each `ObjectEntry`; Python only applies it. (M)

##### G22-10

**Low · Clearness · confidence high**

- **Where:** internal/addons/clusterobjects/evidence_validation.go:31, :35, :49, :68; internal/addons/clusterobjects/evidence.go:170, :251; internal/addons/clusterobjects/validation.go:152
- **What:** Validation is written as single boolean expressions with up to 10 terms (ValidateEvidence complexity 47, validateStep 39, validatePlacement 36). The list cap `16` is repeated as a bare literal six times.
- **Evidence:** line 49 chains `!nativeName(olm.Namespace) || !nativeName(olm.Subscription) || ... || !validGap(olm.Gap)`; `len(olm.Plans) > 16 || len(olm.CSVs) > 16`.
- **Impact:** Hard to review which field fails a rule; the cap can drift from the Python adapter (ADD-13).
- **Fix:** Add a `const maxListed = 16`, plus `validOLM` and `validDependent` helpers. (S)

#### Patterns
- Five separate converters from `api.Value` to native data, with different number handling: addons/packages.go:168 `nativeValue` (json.Number), clusterobjects/program.go:30 `valueJSON` (json.Number, refuses absent), containercluster/agentinstall/projection.go:370 and managedos/installation/network.go:100 (int64/float64, falling back to text). They belong in one `api.Value` method.
- Four "last address of a prefix" helpers: addons/packages/pools.go:22, network/admission.go:161, machine/nmstate.go:150, storage/cephcluster/native_ingress.go:303.
- The same budget rule is written as an admission issue and again as a render or executable error: admission_olm.go:198-205, authored.go:85, instances.go:240-245, instance/render.go:27-34.
- Every Capability method calls `DecodeRequest`, which thaws the program twice (requests.go:61, :71) and runs the full `Validate`. The lineage walk in `declaredBy` (unresolved.go:36-55) does this for every block it visits.
- Hand-written sort comparators where `strings.Compare` would do: packages.go:257, instance/render.go:131. There is also a dead assignment, `networks = nil`, at packages/pools.go:198.

#### Strengths
- Every cluster object body is a typed `api.Value` tree encoded by canonical JSON; there are no string templates (the ACM annotation uses `json.Marshal`). Authored custom resources pass through `NewObjectEntry` at admission (admission_olm.go:147) and again at freeze, so authored input cannot inject into rendered objects.
- clusterobjects is pure policy: steps have exactly one arm, objects, checks and steps are bounded, digests are recomputed, and `ThawVersion` re-canonicalizes (program.go:145-163).
- Go re-validates every native value the adapter returns (`ValidateEvidence`), and `EvaluateEvidence` falls to Unknown on any count, identity or digest mismatch.
- Packages hard-code no product versions or channels; channel and `startingCSV` come from configuration. `NewRegistryWithExamples` enforces unique namespaces and OLM package names and a qualified approval policy.
- `driver.Run` lends placement material only on apply, clones the material map and clears what it lent; tests pin both (driver/run_test.go:39-61).

#### Coverage notes
- instance/placements.go:28 `validatePlacements` is 71% covered. No addons test mentions `kubernetes.io/tls`, "undeclared Secret" or "does not map exactly", so the TLS part-mapping refusal and the part-set refusal are never exercised (no built-in package uses a TLS placement yet).
- instance/selection.go:82 `unrenderable`: the bound branches are unpinned ([G22-02](#g22-02)).
- clusterobjects/evidence_validation.go:12 is 68% covered, with 10 statements unrun. Those are the rejection checks for adapter output, which guard against a faulty or hostile adapter.
- instance/capability.go:26 `Plan` is 76% covered, with 10 statements unrun (the controller-mismatch, missing-facts and destroy-removal branches).
- Cross-language parity of the owner mark and field manager is unpinned ([G22-01](#g22-01)).
- clusterobjects/driver is 61.8% covered by its own tests (91.2% by any test): the error paths of `Run` (LocateTool, LendProduced, bad previous evidence) rely on tests in other packages.

#### Already recorded
- ADD-16: the cluster-object projection template quotes with `%q`. The template-delimiter refusal in `safeBody` exists to protect that projection; I did not restate it.
- ADD-15: no test that a placement type other than Opaque or TLS is refused (related to the first coverage note).
- ADD-13: the cap of 16 InstallPlans and CSVs (related to [G22-10](#g22-10)).
- No open B item matches [G22-01](#g22-01) to [G22-10](#g22-10) (I grepped specs/milestones/ and .agents/knowledge/ for the symbols).

<a id="lane-t01"></a>

### Lane T01: The architecture fitness suite and what the gates enforce
Scope read: 25 files read fully: boundaries, source_boundary, complexity, ratchets, layout, imports, comments, comment_ceilings, test_hooks, requests, decoding, native_errors, language_rows, bounds, family_neutral, operator_strings, version_specializations, check_commits, composition, composition_rules, domain_ports, canonical_json, one_executable, unions and stop_check (all `_test.go`). Also read in full: the Makefile, all four workflows, `scripts/quick-test`, `scripts/check-commits` and `scripts/hooks/stop-check`. lifecycle_partition_test.go: lines 1-200 and 400-906 / Skimmed by outline: docs_test, docs_walkers, docs_gates, diagnostics, milestones, release_recipe, container_build, completion_test / Not read, since they are small, single-rule docs or identity checks and time went to the helpers: automation, adapter_protocol, block_identity_prefix, composition_block_identity, development_versions, diagnostic_*, docs_bmc_controller, docs_secret_classes, docs_checks, enum_casing, reference_suffix, implementation_identity, yaml_style, milestones_decisions.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Checks work on syntax trees with exact, shrinking allowlists and actionable messages; a few rules are only syntactic where they need types |
| Security | 3 | The effect boundary cannot see method calls on values; the race gate leaves out concurrent adapters |
| Efficiency | 2 | Every test re-walks, re-lists and re-parses the whole module; one package runs everything serially |
| Clearness | 3 | "architecture" package also holds docs, governance and tooling suites; one check classifies 66 file names and nothing reads the classes |
| DDD | N/A | Test infrastructure |
| Non-duplication | 3 | Three separate analysis engines and about six copies of the import-alias logic |
| Right language | 3 | Selection and parsing logic in shell, Make and awk (B967 tracks two scripts); Python embedded in a Go string |
| Test coverage | 3 | Most rules prove they fire on planted input, but the dependency direction rule, lifecycle reachability and the deadline rule do not; no lint beyond vet, no coverage gate, narrow race scope |

#### Findings
##### T01-01

**Medium · Efficiency · confidence high**

- **Where:** test/architecture/boundaries_test.go:253-265, source_boundary_test.go:47-50, complexity_test.go:28-30
- **What:** `productionSources` re-walks and re-parses all 918 production files on every call, and 27 tests call it. Each call makes 4 `walkSource` calls, and each of those runs `git ls-files` and Lstats all 5,752 tracked paths (`newSourceTree` per walk). The complexity check then parses every file a second time because the first FileSet was thrown away.
- **Evidence:** `syntax, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)` (boundaries:265); `tree, err := newSourceTree(root)` inside `walkSource`; complexity_test.go:30 parses `source.path` again. The memoizing pattern already exists in this package: `operatorStringsOnce sync.Once` (operator_strings_test.go:50).
- **Impact:** about 108 git spawns, about 620k lstat calls and about 25k file parses per run. The suite takes 40 s (.agents/knowledge/inner-loop-check-cost.md:15), and `make quick` always runs it.
- **Fix:** memoize one `sourceTree` per process and `productionSources` with `sync.Once`, keeping the shared FileSet so the complexity check reuses it. (S)
- **Checked:** B1053 tracks concurrency inside the package, not this repeated work, so this is distinct. No cache exists today (grep for `sync.Once` finds only operator_strings).

##### T01-02

**Medium · Efficiency/Clearness · confidence medium**

- **Where:** test/architecture/ (49 files), scripts/quick-test (last line)
- **What:** about 5,800 of the 13,700 lines are not Go architecture checks. They are docs and governance checks (docs_*, milestones_*: about 4,480 lines) and repository-tool tests (ratchets, release_recipe, container_build, stop_check, check_commits: about 1,355). They all share one package, which Go runs serially, and the quick tier runs the whole package on every change.
- **Evidence:** `run ./test/architecture/... $packages`. The knowledge page says "`t.Parallel` on one heavy test gains nothing: Go starts a top-level parallel test only after every serial test".
- **Impact:** one 40 s serial package on every quick and gate run. A reader cannot tell the Go boundary rules apart from the guidance rules.
- **Fix:** split the suite into `test/architecture`, `test/docs` and `test/tooling` packages. `go test` runs packages in parallel, which gets the speed-up without the shared-state audit B1053 waits on. Update docs-checks.txt to match. (M)
- **Checked:** B1053 proposes concurrency inside the package; it does not propose a package split.

##### T01-03

**Medium · Security · confidence high**

- **Where:** test/architecture/boundaries_test.go:591-606, 626-633
- **What:** the effect boundary is checked only on package-qualified selectors (`pkg.Member`). A method call on a value, such as `(*os.File).Write`, `Truncate` or `Chmod`, is never seen. The read-only-adapter clause therefore guards only syscall and unix members, while `inputfs` and `invokerfs` hold `os.File` and `os.NewFile`.
- **Evidence:** `qualifier, ok := selector.X.(*ast.Ident); if !ok || qualifier.Obj != nil { return true }`; `mutatingMember` lists only `syscall` and `golang.org/x/sys/unix`. The comment claims more: "readOnlyInput holds syscall only to read the operator's input: it never opens a file for writing or mutates one."
- **Impact:** a write through a held `*os.File` in a read-only adapter would pass the gate. No such write exists today (grep found only `RawConn.Write` in invokerfs channel_linux_amd64.go:198).
- **Fix:** resolve selections with go/types, using the loader that already exists in requests_test.go, and refuse mutating methods of `*os.File` and `*os.Root` on read-only adapters. (M)
- **Checked:** TestEveryEffectClauseRefusesWhatItGuards has no fixture with a method call.

##### T01-04

**Medium · Test coverage · confidence high**

- **Where:** boundaries_test.go:220 (`permitsDependency`), lifecycle_partition_test.go:831-878, bounds_test.go:150 (`appliedDeadline`)
- **What:** the dependency-direction matrix, the lifecycle planner and executor reachability rule (an approximately 700-line hand-rolled resolver) and the deadline-application rule run only against the real tree. No planted violation proves they fire. Most other rules in the suite do have one (effect clauses, composition, test hooks, decoding, canonical JSON).
- **Evidence:** `permitsDependency(` appears twice in the suite (its definition and one caller). `loadLifecycleGraph(` appears 3 times (its definition and two real-tree tests).
- **Impact:** a resolver regression, such as `typeOf` returning "?" and over-approximating, or missing an edge, passes silently. This breaks the owner's rule that failure injection must assert it fired.
- **Fix:** add table fixtures, for example a planner that calls `Service.execute` through an interface or an embedded field, plus a forbidden role edge and an unclamped deadline. (S-M)
- **Checked:** only stale-entry checks partly prove that these rules walk the tree.

##### T01-05

**Medium · Non-duplication · confidence high**

- **Where:** requests_test.go:242-320, lifecycle_partition_test.go:108-499, docs_walkers_test.go:55-395; import-alias code at boundaries:274-284, test_hooks:73-83, composition_rules:19-29, lifecycle:164-172, decoding:63-72, canonical_json:20-33, bounds:257-270
- **What:** there are three independent analysis engines: a real go/types loader, a hand-rolled scope and type inference (`lifecycleTypes.typeOf`/`callResults`), and a hand-rolled reference follower. On top of that there are 25 separate `parser.ParseFile` sites, and the alias computation is copied about six times.
- **Evidence:** `config := types.Config{Importer: l, ...}` (requests:305) against `func (types lifecycleTypes) typeOf(expression ast.Expr, scope *lifecycleScope)` (lifecycle:474).
- **Impact:** every rule re-solves name resolution with its own approximations, each must be maintained, and each can be wrong in its own way.
- **Fix:** build one shared loader that type-checks each platform once, and move the lifecycle call graph onto `types.Info`. (L)
- **Checked:** x/tools is unavailable in the main module, which justifies a hand-written loader, but only one of them.

##### T01-06

**Medium · Quality (gate gap) · confidence medium**

- **Where:** Makefile:226-235 (`vet`, `fmt-check`); grep across the repository
- **What:** no Go analysis beyond gofmt and vet runs anywhere: no staticcheck, golangci-lint, errcheck or gosec in code, tools or workflows. vet reports neither implicitly dropped errors nor unused code.
- **Evidence:** 294 bare unchecked `X.Close()`/`Remove()`/`Sync()`/... statements in production code, beside the 64 explicit `_ =`; 175 functions that no test runs.
- **Impact:** dead code and dropped errors accumulate without review. Sampled bare closes, for example internal/secrets/material/files_linux_amd64.go:344 `file.Close()` on an error path of a read handle, are harmless, so errcheck alone would be noisy.
- **Fix:** pin staticcheck in scripts/tools/go.mod (which already depends on x/tools) and add a check-go target with an exact, shrinking exclusion list. (M)
- **Checked:** no `//nolint` or linter configuration anywhere; the only hits for linter names are in specs.

##### T01-07

**Medium · Security (race) · confidence high**

- **Where:** Makefile:277-278; .github/workflows/check.yml (race job)
- **What:** `make race` covers a hand-picked set: `./internal/reconciliation/... ./internal/controller/privilege/... ./internal/trust/... ./cmd/bootwright/...`. Packages with goroutines or locks outside that set include internal/machine/access, internal/workspace/contextfs, internal/adapterprotocol, hostdnf, sshlocal, invokerfs, clusterpreflight, kubevirt, stagelocal, comparisonlocal and cli.
- **Evidence:** machine/access/several.go:190 `var group sync.WaitGroup` and prefix.go:12 `mu sync.Mutex`; contextfs/lifecycle_lock_linux_amd64.go:22 `mutex sync.Mutex`; adapterprotocol/process_linux_amd64.go:141 `go func() {`. Backlog line 697 records a real race in internal/trust that was found only after it was added to this list.
- **Impact:** data races in parallel machine access and the context store are not detected.
- **Fix:** run `-race ./...` with a raised `-timeout` (B1339), or derive the list from `go list` imports of sync and go statements. (S)
- **Checked:** the gate runs `make race` only for safety changes (verification.md:13). CI runs it on every push.

##### T01-08

**Low · Quality (CI) · confidence high**

- **Where:** .github/workflows/check.yml (`commits` job)
- **What:** the commit-message check runs only on `pull_request`. Integration is a local fast-forward followed by a push, so a push to main never gets the check in CI. The check runs only if the integration script follows git-integration.md:51.
- **Evidence:** `if: github.event_name == 'pull_request'`.
- **Impact:** on the actual delivery path, the Contract and BREAKING CHANGE rules are enforced by procedure, not by CI.
- **Fix:** also run on push over `${{ github.event.before }}..${{ github.sha }}`, skipping the zero SHA. (S)
- **Checked:** no git hook is installed (`core.hooksPath` is unset).

##### T01-09

**Low · Clearness · confidence high**

- **Where:** lifecycle_partition_test.go:20-40, 880-905
- **What:** `lifecycleFileClasses` gives each of 66 file names a class (planner, executor, ...), but the only reader is TestLifecycleFilesAreClassified, which checks that the list is complete. No rule checks that a "planner" file holds only planner functions.
- **Evidence:** `lifecycleFileClasses(` appears only at lines 20 and 881.
- **Impact:** every file added or renamed in lifecycle needs an edit here, and the classes enforce nothing.
- **Fix:** either check function placement against the class, or delete the map. (S)
- **Checked:** no spec names this check.

##### T01-10

**Low · Right language · confidence medium**

- **Where:** Makefile:157-218 (release), :200
- **What:** the 60-line release recipe parses the executable's JSON with sed and holds the decision logic. The Shell row of the language table forbids parsing repository content.
- **Evidence:** `field() { printf '%s\n' "$$identity" | sed -n 's/.*"'"$$1"'":"\([^"]*\)".*/\1/p'; }`
- **Impact:** the parse breaks silently if the output's layout changes, for example escaping or whitespace.
- **Fix:** move the release checks into a `scripts/tools` Go tool, as vulngate does, and keep the recipe as the launcher. (M)
- **Checked:** B967 covers quick-test and check-commits only. The behavioural release tests would catch a breakage.

##### T01-11

**Low · Quality · confidence medium**

- **Where:** container_build_test.go:178; release_recipe_test.go:466-470
- **What:** Makefile recipe tests pin exact substrings, their order and `strings.Count(recipe, "$(MAKE)") != 2`. Behavioural tests that run `make release`/`container-build` in fixtures already cover the same properties.
- **Impact:** reformatting a recipe fails the gate without any change in behaviour.
- **Fix:** drop the string pins that the behavioural tests already cover. (S)
- **Checked:** TestReleaseRefuses* and TestContainerBuild* exercise these properties behaviourally.

##### T01-12

**Low · Right language/Quality · confidence high**

- **Where:** version_specializations_test.go:97-98, 120-121, 208; operator_strings_test.go:31-47, 124
- **What:** Python registries are read with regexes, and the test demands a fixed layout: "keep every record in that layout". Test definitions are found by `strings.Contains(text, "def "+name+"(")`, which also matches comments. Meanwhile the suite already runs a Python `ast` scanner, but it is embedded as a Go string, runs on the ambient `python3` rather than the pinned interpreter, and globs plugins without `sourceTree`.
- **Impact:** formatting Python breaks the catalog test; results depend on the host's Python.
- **Fix:** put one `ast`-based extractor script under scripts/tools that reports records, seams and definitions as JSON, and run it with the pinned interpreter. (M)

#### Patterns
- Rules hold exact, shrinking lists with removal messages ("remove it", "lower the ceiling to N"), for example boundaries_test.go:332-347, comments_test.go:42-65 and family_neutral_test.go:211-216.
- The cost of a check is paid per test rather than per run: [T01-01](#t01-01), plus the per-grant re-inspection in `effectBoundary` (boundaries:448-458) and the per-value recomputation in TestApplicationImplementationsAreBoundAtComposition (composition_test.go:273-281).
- Complexity ratchet: maximum nesting is low (4 functions at depth 6 or more). The top cyclomatic functions are flat string-dispatch switches (api/v1alpha1/lexical.go:57 `ValidLexical`, cyclomatic complexity 86, depth 2), which a table would serve better. A ratchet adds little unless it is a high-threshold (40 or more) known list that only shrinks.
- Coverage ratchet: a per-package floor that only shrinks fits the house style, but the `TestSudoColorHintDoesNotReachSubprocesses` failure under `-cover` must be fixed first.
- Python: sanity runs in full, and its 64 ignores are all `validate-modules:missing-gplv3-license`, which the README explains. No type checking runs. The 4,305 lines of Python under scripts/tools are covered only by their own tests, with no lint.

#### Strengths
- Rules work on syntax trees rather than grep, and most come with planted-fixture self-tests (TestEveryEffectClauseRefusesWhatItGuards, TestCompositionBoundaryContractsAndConstruction).
- `sourceTree` (source_boundary_test.go) makes source checks independent of ignored or unreadable directories, and a test proves it.
- Failure messages state the remedy.
- CI pins every action by SHA, uses `permissions: contents: read`, and keys its caches on the lock files.
- quick-test, stop-check and check-commits are tested behaviourally against throwaway Git repositories.

#### Coverage notes
- No fuzz target exists for `internal/adapterprotocol` `Decode`/`Read` (record.go:123,164), which parse adapter output lines, or for logview reading. There are 13 fuzz targets elsewhere, and nothing runs `-fuzz`, so only the seed corpora ever run.
- test/completion/completion_test.go:46-56 runs bash only. zsh, fish and PowerShell need environment variables that CI never sets.
- The planted-fixture gaps in [T01-04](#t01-04).
- `internal/containercluster/preflightlocal` is at 0% (18 statements).

#### Already recorded
- B967: selection logic in `scripts/quick-test` and `scripts/check-commits` (shell exceptions).
- B1053: the serial slow packages, `test/architecture` among them ([T01-01](#t01-01) and [T01-02](#t01-02) are separate fixes).
- B1339: the `make race` timeout on cmd/bootwright.
- B583: record-version literals (operationstore/records.go:313/408/421 and store.go:901/986/1151 use `Version: 1` / `!= 1` with no declaration). Note that check-commits' store-format rule only sees `...Version = <int>` declarations, so it cannot detect a change to these.

<a id="lane-t02"></a>

### Lane T02: Go test strategy: contract suites, harnesses, doubles, determinism, speed

Scope read: capability_contract_test.go (lines 1-640 fully, outline of the rest), contract_identity_test.go (lines 20-235), contract_versions_test.go (lines 155-165 and 560-730), the areadouble package; the operationstore golden helper (the model), the kill-harness opening, the checkpoint-harness design (lines 1-80 and 270-310), the privilege TestMain and terminal-hint test, the e2e render helpers, static_binary_test, ansiblerunner child modes, secretstore/access_test testArea, agentinstall fakeArea / skimmed: journey_test outline, contract_markers_test outline, erase_test list, the hardware, secretstore and mirrorfill golden helpers, contract-suite callers (grep) / not read: the full bodies of the areacontract, workspacecontract and storecontract suites (their callers were confirmed instead), contract_markers bodies, and most of the 180 cmd test files (sampled by grep for signals).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Rigorous property suites with exact ledgers, but several dead functions only show up as 0% coverage |
| Security | N/A | |
| Efficiency | 3 | Almost no in-package parallelism and repeated example compilation (tracked) |
| Clearness | 3 | Harnesses are well explained, but hand-kept classification tables make every change touch them |
| DDD | N/A | |
| Non-duplication | 4 | Test clone share is 1.5%; the golden-helper copies are deliberate but drift |
| Right language | N/A | |
| Test coverage | 3 | 89.4% any-test, but key security and data-loss flows are never run and some fixtures run in no gate |

#### Findings
##### T02-01

**High · Test coverage · confidence high**

- **Where:** internal/secrets/localkeyring/mutations.go:215, internal/secrets/custody/bindings.go:39, internal/managedos/installation/erase_request.go:251, cmd/bootwright/wiring_reconciliation.go:505, internal/containercluster/agentinstall/admin_credentials.go:24 and :82, internal/storage/ceph/admin_credentials.go:26 and :59
- **What:** The only production secret-rebind path, the erase-receipt check that Ceph physical retirement relies on before it releases a claim, and both admin-credential implementations are run by no test in the module.
- **Evidence:** gocov-any-funcs: `64 64 0 ... session.Rebind`; `13 13 0 ... PhysicalEraseProof`; `5 5 0 ... physicalCephEraseProof`; `27 27 0 ... InstallCapability.PrepareAdminCredential`. PhysicalEraseProof is what refuses "physical erase receipt has another original machine UUID".
- **Impact:** A regression in the immutable-version check, the UUID match or the password bounds would pass every gate. These are wrong-target and secret-integrity paths.
- **Fix:** Add package tests. For Rebind, use localkeyring's existing in-memory areas. For PhysicalEraseProof, use the erase goldens' receipts. For the admin-credential pair, use a scripted runner like contractRunner. (effort M)
- **Checked:** Lifecycle's admin_credentials_test and rebind_test drive doubles of these ports, not these implementations. No areacontract or contract-suite row covers them.

##### T02-02

**Medium · Test coverage · confidence high**

- **Where:** cmd/bootwright/capability_contract_test.go:544-547 (rows at 175-220)
- **What:** 21 of the 42 contract rows are `completedFrom`. They get only planning checks and a "no runner without dependency evidence" check. Every effect property (request, cancellation, replay, unknown outcome, evidence proof) is left to the package's own tests. Those rows include physical erase, disk discovery, host firewall, physical retirement and replication.
- **Evidence:** `if row.completedFrom != "" { contractDependentEffects(...); continue }`. The erase test files contain zero occurrences of "cancel", and only one fake runner error appears (erase_root_test.go:71).
- **Impact:** The data-destructive bindings get the weakest contract coverage.
- **Fix:** Give completedFrom rows a dependency-evidence fixture builder (like `presence`) so contractEffects runs on them. Keep the current check as an extra property. (effort M-L)
- **Checked:** The comment says the effects are "held by its own package's tests". For erase, those tests cover goldens and refusals, not cancellation or unknown outcomes.

##### T02-03

**Medium · Quality (dead code) · confidence high**

- **Where:** internal/storage/cephcluster/devices.go:26 (`admission.osd`), :70 (`selector`), internal/storage/cephcluster/admission.go:263, internal/addons/admission.go:222, internal/cli/display.go:451, api/v1alpha1/storage.go:48, internal/substrate/nics.go:45
- **What:** Seven 0% functions have no caller at all. `admission.osd` and its selector are an older copy of the OSD rules that native_devices.go now enforces. It repeats messages such as "TPM2 OSD unlock requires encryption" (devices.go:51 and native_devices.go:95).
- **Evidence:** `grep '\.osd('` finds only the declaration. `uniqueNamedValues`, `displayRow`, `storageMonitoringService`, `nonemptyStrings` and `NICInterface` each appear only at their own declaration.
- **Impact:** A reader may edit the dead copy of a safety rule. vet does not catch unused unexported methods.
- **Fix:** Delete them. Confirm native_devices.go covers the selector-exclusivity refusals at devices.go:83-100 before removing them. (effort S)
- **Checked:** Every reference was grepped, including method values without a call. `replicationFrozenScope` is excluded because a dispatch table uses it.

##### T02-04

**Medium · Test coverage · confidence high**

- **Where:** internal/controller/bundlelocal/trust_linux_amd64.go:30-35, internal/controller/bundlelocal/tools_transport.go:36, internal/storage/preflightlocal/reuse_linux.go:133-134, internal/controller/firewalllocal/native.go:45, internal/containercluster/preflightlocal/resolver.go:24
- **What:** Security-relevant adapters hard-code the root, /proc paths, system executables and their own HTTP client. That leaves no seam, so the pure parsing inside them is untestable and untested.
- **Evidence:** `root, err := os.OpenRoot("/")`; `prefix := "/proc/" + strconv.Itoa(pid) + "/"`; `client, closeIdle, err := acquisitionClient(...)`. All are 0% (36, 37, 32, 18 and 15 statements). bundlelocal is 61.5% own / 63.5% any.
- **Impact:** TOCTOU checks, cmdline identity matching, trust-bundle ownership and the publisher allow-list on tool metadata are never exercised.
- **Fix:** Split read from parse (processStart and the cmdline match are already pure). Inject an `*os.Root` and an `http.RoundTripper`, and test with a temp root and `httptest.NewTLSServer`. (effort M)
- **Checked:** privilege shows the right shape: its `accountFrom` is pure and tested, and `Resolve` is a thin shell.

##### T02-05

**Medium · Non-duplication · confidence high**

- **Where:** internal/controller/firewalllocal/native.go:16-42, internal/containercluster/preflightlocal/resolver.go:16-44, internal/controller/hostowned/probe_linux_amd64.go:30-50, plus venvlocal, comparisonlocal, selectionfs, sshlocal, hostdnf and privilege/sudo.go
- **What:** About nine Go adapters each re-implement the same bounded process envelope: a capped writer, `Qualify`, `Dir="/"`, a clean `Env` and `WaitDelay`. Each copy is untested.
- **Evidence:** `type capture struct{ bytes.Buffer }` appears verbatim in firewalllocal and preflightlocal. Five `Write` methods are at 0%.
- **Impact:** Each copy must be reviewed separately for bounds and environment, and none is tested.
- **Fix:** Add a `nativeexec.Run(ctx, path, argv, limits)` with one test suite, the Go counterpart of `bounded_process.py`. (effort M)
- **Checked:** nativeexec exports only `Qualify`.

##### T02-06

**Medium · Quality (determinism) · confidence medium**

- **Where:** internal/controller/privilege/terminal_hint_linux_amd64_test.go:82-83, internal/controller/privilege/process_linux_amd64_test.go:615-622
- **What:** Tests re-execute the test binary and compare `CombinedOutput` byte for byte. A coverage-instrumented child adds a stderr line, so the comparison fails under `-cover`. The grandchild's stderr is also routed into the same stream.
- **Evidence:** `output, err := child.CombinedOutput(); if err != nil || string(output) != "hint absent\n"` and `string(output) != "verified\n"`.
- **Impact:** Coverage cannot run clean, which blocks any coverage ratchet.
- **Fix:** Compare `Output()` (stdout only) and keep stderr for the failure message. (effort S)
- **Checked:** The other re-exec tests (yamlstream qualification, contextfs kill helper, confirmation) do not compare exact combined output.

##### T02-07

**Medium · Test coverage (hermeticity) · confidence high**

- **Where:** cmd/bootwright/e2e_cases_linux_amd64_test.go:193-194, internal/controller/runtimelock/tool_test.go:169-170, cmd/bootwright/static_binary_test.go:52
- **What:** Unit tests fail without helm or python3 on PATH, and they run `e2e/render.sh`. The static-binary test runs a bare `go` from PATH.
- **Evidence:** `if _, err := exec.LookPath("helm"); err != nil { t.Fatalf(...)`; `exec.CommandContext(t.Context(), "go", "build", ...)`.
- **Impact:** This contradicts go.md ("must not depend on ambient ... tools"). It passes on CI only because ubuntu-24.04 ships unpinned helm and python3, whose versions can change rendered output.
- **Fix:** Move these tests behind a build tag or a gate that provisions pinned tools, or pin helm through the tool table. (effort S-M)
- **Checked:** check.yml installs neither tool. GOTOOLCHAIN is inherited from scripts/go, so the `go` case is lower risk.

##### T02-08

**Medium · Test coverage · confidence high**

- **Where:** internal/controller/privilege/process_linux_amd64_test.go:556, internal/workspace/contextfs/lifecycle_linux_amd64_test.go:37
- **What:** The sudo account-provenance fixture needs root plus `BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE=1`. Nothing outside the test sets that variable, and CI runs as non-root.
- **Evidence:** `if os.Geteuid() != 0 || os.Getenv("BOOTWRIGHT_PRIVILEGED_ACCOUNT_FIXTURE") != "1" { t.Skip(...)`. privilege `Resolve`, `sudoParent` and `readAccountFile` are 0%.
- **Impact:** Privilege-boundary code runs in no gate.
- **Fix:** Add a gate step that runs it in an `unshare -Ur` namespace, or record it as a manual gate in verification.md. (effort M)
- **Checked:** Grepped every non-test file for the variable: no hits.

##### T02-09

**Medium · Clearness · policy trade-off · confidence medium**

- **Where:** cmd/bootwright/contract_identity_test.go:108-174, cmd/bootwright/contract_versions_test.go:155-510
- **What:** These tests restate every implementation's ContentDigest inputs by hand, plus about 270 classified automation literals, so every pin or literal change edits them.
- **Evidence:** In the last 866 commits, contract_versions_test.go changed in 125 and contract_identity_test.go in 72, the two top test-churn files.
- **Impact:** About one commit in seven pays this tax.
- **Fix:** Keep the classification, but have each implementation export its recipe parts and golden the listing, so the test pins the recipe without retyping it. (effort M)
- **Checked:** The intent (visible contract moves) is deliberate and valuable; only the restatement is questioned.

##### T02-10

**Low · Quality · confidence high**

- **Where:** cmd/bootwright/capability_contract_test.go:524-565
- **What:** The module-wide contract runs as one test function with no `t.Run`. Any `t.Fatalf` at lines 538-558 stops every remaining row, and `-run` cannot select one binding.
- **Fix:** Run one subtest per row, keeping the shared examples map. (effort S)
- **Checked:** None of the four contract files uses `t.Run`.

##### T02-11

**Low · Non-duplication · policy trade-off · confidence high**

- **Where:** internal/machine/hardware/golden_test.go:22, internal/secrets/secretstore/record_golden_test.go:20
- **What:** 39 golden files contain 23 `matchesGolden` copies in four signatures. Only 16 match go.md's stated `(t, name, data, terminated)`. There are 24 `lineDiff` copies, roughly 2,600 lines in all, and no check keeps them aligned.
- **Fix:** Add a fitness test that compares each helper body with the operationstore model, or relax go.md's signature sentence. (effort S)
- **Checked:** Per-package helpers are deliberate. The cost is the drift and the cost of fixing one helper 24 times.

##### T02-12

**Low · Quality · confidence medium**

- **Where:** internal/secrets/secretstore/access_test.go:16-31
- **What:** `testArea` embeds a nil `Area`, ignores `ReadMutable`'s limit, returns `Entries` in map order, and does not run areacontract.
- **Fix:** Sort the entries and honour the limit, or build the double on areacontract. (effort S)

#### Patterns
- Pyramid (Q1): the big own-versus-any gaps are mostly sound. The four contract packages and areadouble are test support (0% own by design). managedservice (65→93), internal/secrets (43→95) and api/v1alpha1 (76→96) are covered through the composition-root contract suite and compilation. The cost: `go test ./internal/secrets` alone cannot verify a refactor of it, so `make focus` with dependents is required. controller/bundlelocal (61.5/63.5) and secrets/localkeyring (359 uncovered statements) are real gaps, not delegation.
- The 175 zero-coverage functions (Q2) fall into three groups. (a) Adapters with hard-wired I/O ([T02-04](#t02-04), [T02-05](#t02-05)). (b) Security and data-loss flows ([T02-01](#t02-01), [T02-08](#t02-08)). (c) Dead code ([T02-03](#t02-03)). Coverage of re-executed children is invisible, so the privilege figures are a floor. The ten that matter most: session.Rebind, custody.Service.Rebind, PhysicalEraseProof/physicalCephEraseProof, agentinstall PrepareAdminCredential/adminCredentialRun, ceph adminCredentialRun, qualifiedTrustPEM, fetchToolMetadata, processCredit, privilege Resolve/readAccountFile, custody Holds/ReadProducedInArea.
- Assertions (Q3) favour diagnostic codes: 719 `.Code` comparisons against 288 exact error/message strings and 192 `strings.Contains` checks. Ledgers that only shrink (`contractKnownDeviations`, the kill and checkpoint ledgers) make passing-by-deletion hard.
- Fuzzing (Q7): 13 `Fuzz` targets, no committed `testdata/fuzz` corpus, and no gate runs `-fuzz`. There is no `testing/quick`. Byte grammars without a fuzz target: adapterprotocol.Decode (record.go:164, the Ansible-to-Go record line), canonicaljson Decode/DecodeClosed, ansiblelocal strictJSON/uniqueJSON (protocol.go:14, a second hand-written duplicate-key walker beside adapterprotocol.walk), and api/v1alpha1/lexical.go.
- Doubles (Q8): no mock framework. 57 embedded-interface decorators mostly wrap real implementations. Every production Workspace adapter and its in-memory doubles run the matching Verify (contextfs, lifecycle, operationstore, logview, localkeyring, media), and they share one admission helper (areadouble).

#### Strengths
- The kill harness and the contextfs checkpoint harness interrupt every publication at every checkpoint by refusal, cancellation and a real `os.Exit`, under exact ledgers and parallel subtests.
- The capability contract is a declarative property table with spec-cited reasons per row, and a test that forces every new binding to join it.
- The port contract suites run against both the production adapter and the doubles, as go.md requires.
- Clocks are deterministic (`killClock`, `testClock`), and journey_test has no wall-clock or sleep calls.
- Exact duplication in tests is low (1.5%) outside the deliberate golden helpers.

#### Coverage notes
- internal/managedos/installation/erase.go:171: Destroy's runner-error and unknown-outcome path has no test (see [T02-02](#t02-02)).
- internal/storage/ceph/admin_credentials.go:59 and agentinstall:82: the two admin-credential runs are near copies, both untested (lane G21 owns the duplication).
- internal/secrets/custody/produced.go:63 and :114: Holds and ReadProducedInArea are 0%.
- internal/adapterprotocol/record.go:164: the untrusted record grammar has no fuzz target.
- internal/reconciliation/ansiblerunner/process_linux_amd64_test.go:375: an unconditional `time.Sleep(100 * time.Millisecond)` inside a real-process test is a timing assumption.

#### Already recorded
- tracked: B1053 (serial in-package tests in cmd/bootwright, contextfs and lifecycle; parked) and B1339 (`make race` timeout on cmd/bootwright). The speed levers found here (0 `t.Parallel` in 180 cmd files, 3 of 1303 module-wide; 167 uncached `compileAcceptance` calls; one checkpoint-harness child process per kill point) fall under these items.
- tracked: B1345 (Python subprocess flake), not restated.
- No audit 003 ID matched these test-architecture findings.

<a id="lane-p01"></a>

### Lane P01: Ceph native worker core (Python)
Scope read: read in full: `ceph_native_worker.py` (441 lines), `ceph_native_observe.py` (499), and `ceph_native.py` lines 192-4865 (lines 1-191 are constants; I read them through the outline and lines 60-100). Skimmed: `ceph_package_worker.py` 100-207 (a clone lead), `ceph_core.py` 545-600 (how the bundle is built), `test_ceph_native_worker.py` 185-295 plus its test list, and the Go side (`internal/storage/cephcluster/{config,topology,devices}.go`, `internal/storage/ceph/{requests,evidence}.go`), only to confirm or refute claims. Not read: the sibling `ceph_native_*` modules and most of the 60 Ceph test files (outside the lane; I grepped them only).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed and careful about descriptors, but an unreachable transport arm is kept alive and state is passed through hidden attributes |
| Security | 4 | Every process goes through `bounded_process`, there is no shell, opens use `dir_fd` and `O_NOFOLLOW`, secrets are redacted; the only notable gap is a persisted `st_dev` comparison (F04) |
| Efficiency | 3 | About 1.04 MB of source is shipped and compiled on every call; the tool is re-verified within one run (tracked) |
| Clearness | 2 | 4,865 lines with one Worker of about 200 methods; 33 functions have complexity over 15 and 14 over 30 |
| DDD | 3 | Sub-contexts (registry custody, OSD clearing, dashboard, tunnel bootstrap) share one class; the collaborator pattern already exists but is underused |
| Non-duplication | 2 | Five hand-rolled walks along a fixed directory path, three descriptor-identity record formats, repeated LVM parsing, Go admission rules repeated in Python |
| Right language | 3 | Process work is correct; readiness is judged in Python (spec-sanctioned) and Go's business rules are re-checked in Python |
| Test coverage | 3 | 94.7% measured in-process and thorough refusal tests, but 17 test files drive an impossible transport and nothing feeds Go's request goldens to Python |

#### Findings

##### P01-01

**Medium · Quality · confidence high**

- **Where:** `C/plugins/module_utils/ceph_native.py:2924-2966`, `:2710`, `:1619`; `internal/storage/cephcluster/topology.go:112-113`
- **What:** Go admission refuses `clusterSSHTransport: native`, and `Worker.bootstrap` requires `supported_transport`, which is false for anything but `verifiedTunnel`. So every native-transport branch in the worker is unreachable in production: the native bootstrap body, the plain `restrict` authorization line, and each `else contextlib.nullcontext()` arm (33 references to `clusterSSHTransport` in the module). Tests keep these paths alive with `monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)`, 42 times across 17 test files.
- **Evidence:** `require(supported_transport(self.request), "unsupported-release")` (2924); `if self.request["clusterSSHTransport"] == "verifiedTunnel": return self.tunnel_bootstrap(private)` (2929-2930); `topology.go:113`: "the native cluster SSH transport is reserved, and no build realizes it".
- **Impact:** every change in the area has to keep a dead arm consistent, and many tests exercise a configuration production cannot reach instead of the tunnel path it does run.
- **Fix:** delete the native arms and the 42 monkeypatches, and keep one test that `native` refuses. (effort M)
- **Checked:** `specs/api/ceph.md:241-243` says `native` is reserved and refused; no backlog item tracks removing this code.

##### P01-02

**Medium · Non-duplication / Security · confidence high**

- **Where:** `ceph_native.py:379-402` (`registry_directory`), `:1317-1336` (`Markers.create`), `:1381-1397` (`Custody.directory`), `:3428-3467` (`native_source`); `ceph_native_observe.py:94-125` (`fixed_directory`)
- **What:** five separate implementations open each component of a fixed path without following links and check owner and mode. Each applies different rules: whether it creates components, which owners are allowed, whether it fsyncs, whether links are followed. `Markers.read` and `Markers.write` then go back to path strings (`read_regular(os.path.join(...))`, `tempfile.mkstemp(dir=self.path)`) instead of holding a directory descriptor.
- **Evidence:** `following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)` appears 11 times in `ceph_native.py`.
- **Impact:** a fix to the trust walk has to be made five times, and the inconsistent rules are hard to review.
- **Fix:** one `held_directory(path, owners, create=None)` helper in a shipped module, used by all five; marker reads and writes go through its descriptor. (effort M)
- **Checked:** the bundle already ships shared helpers (`ceph_tunnel_custody.directory` is used at line 1769), so a shared helper fits.

##### P01-03

**Medium · Clearness / DDD · confidence high**

- **Where:** `ceph_native.py:1519-4741`; `ceph_native_worker.py:30-36`; `ceph_core.py:585-596`
- **What:** one Worker class owns registry custody (2166-2339), SSH authorization and sshd policy (2689-2891), tunnel bootstrap, configuration and mount (2923-3573), Call Home and license (3173-3290, 3574-3761), OSD device clearing (2341-2687), services (3791-4001), the dashboard with TLS, password and port repair (4019-4386) and observation (4410-4731). The bundle does not force this. It is a closed dict of module sources, and adding one means appending a name to `MODULES` and to `native_sources()`, which are checked equal (`require(tuple(sources) == ceph_native_worker.MODULES ...)`). The collaborator pattern already exists: `ceph_native_stretch.Activation(self)`.
- **Evidence:** 4,865 lines; `dispatch` complexity 73, `observe_cluster` 70, `validate_hosts` 66, `dashboard_repair_ports` 45; 14 functions over 30.
- **Impact:** every Ceph change loads the largest file in the repository, and the sub-contexts are coupled through shared mutable Worker state.
- **Fix:** extract `ceph_native_registry`, `ceph_native_osd` (devices, clear, purge, variant proofs), `ceph_native_dashboard`, `ceph_native_bootstrap` (tunnel staging, configuration, mount) and `ceph_native_validate` as `X(worker)` collaborators, and replace `dispatch`'s boolean chain with a table keyed by (action, phase). (effort L)
- **Checked:** `test_python_sites.py` names only the worker files as stand-alone exceptions, so new modules inside the bundle need no exception.

##### P01-04

**Medium · Security / Quality · confidence medium**

- **Where:** `ceph_native.py:1400-1404`, `:1411`, `:1442`
- **What:** the generic `Custody` record, which holds the dashboard password and the dashboard TLS pair, persists `st_dev` and compares it before every read. A reboot that renumbers the device therefore refuses `dashboard-identity`, and the spec forbids regenerating the password. B1342 lists the tunnel custody, the cache directory and `registry_metadata` for this defect, but not `Custody`.
- **Evidence:** `result = {"dev": info.st_dev, "inode": info.st_ino, ...}` (1401); `self.metadata(info, complete=complete) == metadata` (1442).
- **Impact:** after such a reboot, `cluster info --secrets` and the dashboard phase refuse permanently.
- **Fix:** add `Custody.metadata` to B1342's scope. (effort S)
- **Checked:** B1342's text (backlog.md:3627-3635) names only the three records above.

##### P01-05

**Medium · Right language / Non-duplication · confidence medium · policy trade-off**

- **Where:** `ceph_native.py:799-815`, `:959-960`, `:978`; `internal/storage/cephcluster/config.go:52-73`, `devices.go:51`
- **What:** `validate_config`, `validate_services` and `validate_hosts` repeat Go's admission rules, not just shape bounds:
- the `osd_memory_target` floor of 2 GiB (`2 * 1024 ** 3 <= int(target)`);
- the exact pool-default pair of 2 and 2;
- `count_per_host == 1`;
- tpm2 requires encrypted;
- `singleHostDefaults` requires one host with at least two devices.
Type, regex and closed-key checks at the trust boundary are wanted. Product rules like these belong to Go alone.
- **Impact:** the two copies can drift, and then a request Go admits refuses only on the host at effect time.
- **Fix:** keep shape and bound checks in Python, drop the product rules, or pin both sides with one shared fixture (see [P01-11](#p01-11)). (effort M)
- **Checked:** python.md asks for "closed key sets, exact types and bounds"; the memory floor is a product qualification, not a type bound.

##### P01-06

**Medium · Right language · confidence medium · policy trade-off**

- **Where:** `ceph_native.py:4447-4470`, `:4529-4577`; `internal/storage/ceph/evidence.go:595-627`
- **What:** Python computes `evidence["ready"]`: which health checks are tolerated, which scoped checks count for the block's pools, the PG state, the license and the daemon counts. Go checks only that the flags are consistent and calls `validateReady`. `specs/ceph.md:1976-1981` and `:2148` place `SCOPED_HEALTH` in the adapter, so this is sanctioned. Still, the readiness predicate cannot be tested or changed from Go, and the architecture table says Python never "judges a postcondition".
- **Impact:** the readiness policy lives in Python, in an untyped language, and Go cannot re-judge it.
- **Fix:** publish the raw checks and counts as evidence fields and let Go apply the predicate, or record the exception under architecture.md "Named exceptions". (effort L)
- **Checked:** the spec sentences above.

##### P01-07

**Medium · Non-duplication · confidence high (outside lane, from a clone lead)**

- **Where:** `ceph_package_worker.py:107-195` versus `ceph_native_worker.py:288-391`
- **What:** the package worker is an older copy of the supervisor. On a natural exit it reaps with `os.waitpid(child, os.WNOHANG)` and returns without `stop(child, grace)`. The native worker fixed exactly this: it uses `waitid(... WNOWAIT)` and stops the group before any reap ("reserves the worker PID until that group is stopped").
- **Evidence:** `finished, status = os.waitpid(child, os.WNOHANG)` / `if finished == child: ... return result` (175-182), with no stop on that path.
- **Impact:** inherited dnf scriptlets can outlive the package worker's ceiling, and the fix has already diverged between the copies.
- **Fix:** one shared supervisor source that both launchers prepend. (effort M)
- **Checked:** both files are named exceptions in `test_python_sites.py:30-31, 49-50`, but for running alone, not for divergent logic.

##### P01-08

**Low · Non-duplication · confidence high**

- **Where:** `ceph_native.py:2014-2045` versus `:2349-2373` and `:2448-2453`; `:1972-1981` versus `:2120-2126`; `:2784-2785` versus `:2826-2827`
- **What:** the LVM report is read and its rows and tags parsed three times, although `logical_volumes()` exists. The two tag parsers differ: only 2361 guards an empty `lv_tags`. The `ssh-authorization.json` validation is copied verbatim in `observer_cache_uid` and `prove_authorization`. The sshd policy name token is derived twice. The MGR placement lookup is repeated 8 times, and the `bootstrap_hold`/`nullcontext` proof idiom 4 times.
- **Fix:** helpers `lv_rows()`, `lv_tags(row)`, `authorization_record()`, `sshd_policy_name()`, `placement(kind)`. (effort S)

##### P01-09

**Low · Clearness · confidence high**

- **Where:** `ceph_native.py:2582-2584`, `:2485-2513`, `:4423`, `:4659`, `:4835-4840`
- **What:** `devices()` creates `observed_device_ids`, `osd_clears`, `osd_settled` and `osd_orphans` outside `__init__`, and `clear_partial_osds` and `osd_prepare` read them. `blocking`, `unplaced`, `reading` and `base_ready` are hidden outputs that `dispatch` reads afterwards.
- **Impact:** the order of calls is an implicit protocol, and calling `clear_partial_osds` first raises AttributeError, which the worker reports as native-unknown.
- **Fix:** return a typed result from `devices()` and from `observe()`. (effort M)

##### P01-10

**Low · Quality · confidence high**

- **Where:** `ceph_native.py:4847-4865`; `:4757-4842`
- **What:** `ceph_native.main` is dead. The worker loads the module and calls `dispatch` directly, nothing calls `main`, and it has no cause or timeout handling (0% measured). Separately, `dispatch` admits any `CORE_PHASES` member for every worker kind: a pool request may name `bootstrap`, and only the methods' own guards then refuse.
- **Fix:** delete `main`, and close the (worker kind, phase) pairs in one table. (effort S)

##### P01-11

**Medium · Test coverage · confidence high**

- **Where:** `internal/storage/ceph/testdata/request-cluster-*.golden`; the Python Ceph tests
- **What:** Go keeps request goldens, but no Python test loads one. All 60 Python Ceph test files build requests by hand (9 files define their own `request()` builder), so nothing proves that a request Go emits passes `ceph_native.validate`.
- **Impact:** the drift risk from [P01-05](#p01-05) goes undetected until a live run.
- **Fix:** one parametrized Python test that runs `validate` (and `Worker` construction) over each Go request golden. (effort S)

#### Patterns
- Re-proof is repeated by design (`markers.owned()` 27 times, `self.daemons()` 19, `verify_tool()` 7). `daemons()` itself runs `verify_tool`, with two `rpm --verify` calls and hashes of up to 4 x 64 MiB of binaries, so `prerequisites` and `claim` verify twice (`ceph_native.py:2092-2097`, `:2104-2107`, `:1955`). Tracked as B635.
- Every call ships 29 modules, about 1.04 MB raw (`ceph_core.py:585-596`), including unrelated ones such as `physical_erase_worker`, against the worker's 2 MiB input ceiling. No test guards the headroom; the only size check is the controller's `len(payload) <= MAX_INPUT` (`ceph_core.py:557`).
- Three descriptor-identity record formats sit side by side: `registry_metadata`/`registry_metadata_record` (360-375), `Custody.metadata`/`metadata_record` (1400-1414) and `ceph_tunnel_custody.metadata`.
- Code embedded as strings: the launcher (`ceph_native_worker.py:60-63`) and a ServiceSpec normalizer run in the image (`ceph_native.py:3850-3852`). Both are deliberate and bounded, but they cannot be linted.

#### Strengths
- Process discipline: every process starts through `bounded_command` and then `bounded_process.run`, with an absolute argv, a fixed environment and only an allow-listed proxy on 127.0.0.1 (512-541). There is no shell or subprocess anywhere in the lane.
- The supervisor (`ceph_native_worker.py:267-391`): signals are blocked across the fork, the PID stays reserved with `waitid(WNOWAIT)`, the group is killed before the reap, causes are attested to sites in the shipped source, and output is bounded and redacted. Tests run it in real processes.
- Descriptor-first custody: identity is proved before content is read and again after (`registry_read` 415-434, `Custody.read` 1433-1448, `public_file` in observe). Records are published by hard link, never by overwrite.
- Observation is hermetic and bounded: `daemons()` takes `clock` and `pause` as parameters (`ceph_native_observe.py:426-437`), and every native list has a size limit.
- Broad `except Exception` appears only at process boundaries (`ceph_native_worker.py:257, 276, 282, 383, 432`; `ceph_native.py:2804` re-raises).

#### Coverage notes
- `ceph_native_worker.work`, `publish_result` and `main` measure 0% only because the measurement is in-process. `test_ceph_native_worker.py:158-294` runs them as child processes, so they are covered.
- Those real-process tests assert on timing (`time.sleep(0.4)` then `assert not effect.exists()`, and `< 0.8` s at 253 and 288), which can flake under gate load.
- `Markers.create` (`ceph_native.py:1317`, 0%): the walk that creates the root ownership directory is never run, because tests inject markers. It is the first durable effect on a host.
- `prove_authorization` (2117, 0%) and `prerequisites` (2088, 0%) are the security-relevant read of `authorized_keys` and the host prerequisite proof.
- `tunnel_guest_policy` (2781, 79%): the rollback that unlinks the policy when `prove_tunnel_policy` fails, which edits sshd configuration, is partly unrun.
- `bootstrap_state` (3077, 70%) and `preparation_state` (2148, 36%): the recovery-stage branches.

#### Already recorded
- B635: `verify_tool` runs more than once per worker run.
- B1342: persisted `st_dev` in `registry_metadata`; [P01-04](#p01-04) extends it to `Custody`.
- Audit 003 CPD-01: DB/WAL `device-not-empty` at `ceph_native.py:2630-2633`; not restated.
- The window in which a remote worker outlives cancellation (backlog entry before B576, citing `ceph_native_worker.py:1-9`); not restated.

<a id="lane-p02"></a>

### Lane P02: Ceph native services (Python)
Scope read: I read all 16 scope modules in full, 7,988 lines: ceph_native_object, ceph_native_resources, ceph_native_stretch, ceph_native_units, ceph_native_observability, ceph_smb_native, ceph_native_nfs, ceph_native_nvmeof, ceph_export_native, ceph_replication_native, ceph_monitoring_template, ceph_admin_credentials_native, ceph_replication, ceph_export_dns, admin_credential_io, admin_credentials_protocol. To confirm claims I skimmed parts of ceph_native.py (read_regular, the Refusal classes, dashboard_verify, Selection), ceph_native_worker.py:240-265, ceph_host_runtime.contained_argv, native_specializations.py, modules/ceph_admin_credentials.py, Go internal/storage/ceph/{nvmeof.go:246-272, admin_credentials.go, resources.go}, test_ceph_dashboard.py:1219-1305 and test_ceph_export_native.py (test list). I did not read the other 17 lane test files in full, only grepped them. The coverage figures were high enough that I spent the time on design.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Disciplined closed validation and refusal reasons; a few lost diagnostics and one piece of dead work |
| Security | 4 | Secrets kept off argv, output and logs with real care; private-file primitives are duplicated and one is untested |
| Efficiency | 4 | Bounded everywhere; minor repeated native reads |
| Clearness | 3 | Deep inheritance chains, a `__getattr__` proxy and positional 12-field hook tuples are hard to follow |
| DDD | 3 | Marker-record names are string contracts with no owner; some Go rules are re-derived here |
| Non-duplication | 2 | Many copied methods and helpers across the service modules |
| Right language | 3 | The NVMe-oF budget formula and the PG/MDS rules exist in both Go and Python |
| Test coverage | 4 | 83-99% per file, failure injection at checkpoints; the gateway export path and admin_credential_io are not run |

#### Findings
##### P02-01

**Medium · Non-duplication · confidence high**

- **Where:** ceph_native_object.py:572, :979, :1151; ceph_native_units.py:589; ceph_native_nfs.py:315; ceph_native_nvmeof.py:111; ceph_export_native.py:125; ceph_replication_native.py:102; ceph_smb_native.py:287
- **What:** `recovery_scope` is copied 9 times in two shapes: 6 copies that drop one member, 3 that pop `delegated`.
- **Evidence:** `scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "objectUser"})` / `require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")`. Only the excluded key differs.
- **Impact:** Every copy carries an ownership proof, so a fix to one must be repeated in all nine.
- **Fix:** Put one `recovery_scope` on ResourceWorker, driven by a class attribute `OWN_MEMBER` and a `DELEGATED` flag (effort S).
- **Checked:** SMB adds an extra delegated check, so it would keep a small override. No backlog item covers this.

##### P02-02

**Medium · Non-duplication · confidence high**

- **Where:** ceph_native_object.py:117-127 vs ceph_native_nfs.py:59-69; ceph_native_nfs.py:428-438 vs ceph_native_resources.py:695-705; ceph_native_nfs.py:161-166, ceph_native_observability.py:187-193, ceph_native_object.py:560-567; ceph_native_object.py:59 vs ceph_native_stretch.py:43
- **What:** Identical helpers are copied between sibling modules: `port`/`networks` (object.py already imports nfs), the `fs subvolume getpath` reader with the same docstring, three image-settings checks, and the stretch 4/2 constants.
- **Evidence:** Both files define `def networks(value): require(isinstance(value, list) and 1 <= len(value) <= 8)`. Both files define `STRETCH_SIZE, STRETCH_MIN_SIZE = 4, 2`.
- **Impact:** Native-contract facts can drift between sibling workers.
- **Fix:** Import the existing helpers. Move the general validators out of the NFS module into one shared helper module (effort S).
- **Checked:** I opened both sides of each pair. The clone detector missed them because they are under 10 lines or renamed.

##### P02-03

**Medium · Non-duplication · confidence high**

- **Where:** ceph_native_resources.py:370, :978; ceph_native_object.py:607, :1010; ceph_export_native.py:139; ceph_replication_native.py:133; ceph_smb_native.py:308 (and 4 more in ceph_native.py)
- **What:** One transport-hold expression is repeated 11 times.
- **Evidence:** `proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verifiedTunnel" else contextlib.nullcontext()`
- **Impact:** A new native call site can forget the hold, which is a correctness and trust boundary.
- **Fix:** Add one `Worker.native_hold()` context manager and use it at every site (effort S).
- **Checked:** There is no existing helper (grepped `def bootstrap_hold` and its callers).

##### P02-04

**Medium · DDD · confidence medium**

- **Where:** ceph_export_native.py:151-163; ceph_replication_native.py:125; ceph_native_nvmeof.py:171-172; ceph_native_resources.py:323-351, :685-686, :1382-1383
- **What:** Marker-record file names (`"pool-" + name + ".json"`, `"pool-id-"`, `"filesystem-id." + name`) and the RBD attempt record are rebuilt by hand in other modules. The NVMe-oF worker re-spells the `rbd_attempt()` dict.
- **Evidence:** `require(self.markers.read(attempt_name) == {"version": ceph_native_resources.RBD_ATTEMPT, "owner": ..., "fsid": ..., "rbd": image}, "nvmeof-identity")`
- **Impact:** Renaming one record silently turns ownership proofs elsewhere into refusals, with no test naming the contract.
- **Fix:** Have ResourceWorker own record-name and intent builders (`pool_marks(name)`, `rbd_attempt(image)`, `filesystem_marks(name)`) and use them from every module (effort M).
- **Checked:** No knowledge page or backlog item names this contract.

##### P02-05

**Medium · Right language · confidence high**

- **Where:** ceph_native_nvmeof.py:33-62, :101; ceph_core.py:1511; internal/storage/ceph/nvmeof.go:246-271; ceph_native_resources.py:278-283 vs internal/storage/ceph/resources.go:136; ceph_native_resources.py:198 vs internal/storage/cephcluster/filesystems.go:109
- **What:** The NVMe-oF phase and execution budget formula, including the 6 h cap, is written identically in Go and Python. Python also re-derives the PG clamp and the MDS coverage rule that Go admission already applied.
- **Evidence:** Python `return 60 * (scope + 10 + namespaces + listeners + hosts) + 30 * (2 + namespaces + listeners + hosts) + 60` matches Go `return 60*(scope+10+namespaces+listeners+hosts) + 30*(2+namespaces+listeners+hosts) + 60`.
- **Impact:** These are business rules held in two languages. The bucket-name limit (44 vs 63) already drifted this way once (audit 003). Parity is pinned only by both suites happening to expect `5024`.
- **Fix:** Go freezes the per-phase budgets and the derived pgNum into the request. Python keeps the trust-boundary checks (closed keys, exact types, bounds) and drops the re-derivations (effort M).
- **Checked:** Shape and bound checks are wanted per python.md "Validate the frozen request". `mds_needed` at effect time (resources.py:1184) is a live native check and should stay.

##### P02-06

**Medium · Clearness · confidence high**

- **Where:** ceph_native_object.py:968-1149, :1362-1369; ceph_native_stretch.py:833-843
- **What:** Code is reused through inheritance between unrelated identities. BucketWorker(UserWorker(ObjectWorker(UnitWorker(ResourceWorker)))) overrides `gateway()` and `user()` so inherited methods act on other objects. ExportGateway skips `__init__` and forwards through `__getattr__`. PhysicalHostRemoval copies TiebreakerRemoval's constructor without calling super and refuses with `tiebreaker-foreign` on a non-tiebreaker host.
- **Evidence:** `def __init__(self, worker):  # pylint: disable=super-init-not-called` / `def __getattr__(self, name): return getattr(self.worker, name)`
- **Impact:** A newcomer cannot tell which `recovery_scope`, `user` or `observe_resource` actually runs, and physical-host removal reports a misleading reason.
- **Fix:** Compose small collaborators (a gateway-records object, a user-records object) instead of inheriting. Give the physical removal its own reason (effort M/L).
- **Checked:** Tests pass through these paths; the cost is readability, not correctness.

##### P02-07

**Medium · Quality · confidence medium**

- **Where:** ceph_native_nvmeof.py:126-129
- **What:** `nvme()` turns every Refusal into a bare `Refusal("nvmeof-native")` with `from None`. That includes a NativeFailure (losing its redacted `native_error`) and a ManagerRefusal. The commands go to `self.ceph` directly, not through `manager_retry` as the NFS and SMB workers do.
- **Evidence:** `except Refusal: raise Refusal("nvmeof-native") from None`
- **Impact:** The operator loses the native message, and a restarting manager is not retried.
- **Fix:** Keep `native_error` on the new refusal as nfs.py:458-463 does, and route list and info reads through `manager_retry` (effort S).
- **Checked:** Go maps `nvmeof-native` to lifecycle.unknown (materials.go:400), so no wrong state is recorded.

##### P02-08

**Low · Quality · confidence high**

- **Where:** ceph_admin_credentials_native.py:24-27; ceph_admin_credentials_native.py:31-53
- **What:** `prepare` computes a bcrypt hash through the deprecated `crypt` module and puts it in the candidate. `update` never reads `candidate["hash"]`.
- **Evidence:** `candidate = {"guard": ..., "password": password, "hash": hashed}`; `update` reads only `candidate["guard"], candidate["password"]`. test_ceph_dashboard.py:1247-1250 builds candidates without "hash" and they update successfully.
- **Impact:** This is dead work with an extra refusal path (`dashboard-verify`). It also depends on `crypt` (removed in Python 3.13) and on bcrypt in libxcrypt, which a FIPS host may refuse (backlog line 8321).
- **Fix:** Delete the hash computation and the `crypt` import (effort S).
- **Checked:** Go treats the candidate as an opaque "guard" (admin_credentials.go:36-40). ceph_native.dashboard_verify:4316 also uses `crypt` (lane P01); whether that FIPS risk is real needs P01's judgment.

##### P02-09

**Low · Security · confidence medium**

- **Where:** admin_credential_io.py:9-30; ceph_native.py:1102-1111; modules/ceph_admin_credentials.py:72
- **What:** There are two private-file readers with different rules. `private_read` requires uid 0 and mode exactly 0600. `read_regular(private=True)` requires the effective uid and no group or other bits. The Ceph module reads with one and the container-cluster module with the other. Also, `private_write` raises after `O_CREAT|O_EXCL` without unlinking, and its `truncate(0)` is redundant.
- **Evidence:** `if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600: raise ValueError("private output")`
- **Impact:** These are security-relevant primitives that diverge, and admin_credential_io has 0% test coverage.
- **Fix:** Keep one tested private read/write pair and unlink on a failed post-create check (effort S).
- **Checked:** Not covered in audit 003 or the backlog.

##### P02-10

**Low · Clearness · confidence high**

- **Where:** ceph_native_units.py:32-33, :109-130, :50-79, :175/:185
- **What:** The 12-field `UnitType` hook tuples are built positionally next to six no-op hooks whose bodies are only `del` statements. `CERTIFICATE_VARIABLES` and `KEY_VARIABLES` are each assigned twice, 10 lines apart.
- **Evidence:** `UnitType(crash_validate, mirror_check, mirror_claim, mirror_settle, crash_expected, crash_readiness, CRASH_TYPES, mirror_frontend, {"crash": None}, {}, frozenset(CRASH_TYPES), None)`
- **Impact:** Swapping two hooks is easy and invisible in review.
- **Fix:** Build the tuples with keywords and namedtuple `defaults=` for the no-op hooks; assign each constant once (effort S).
- **Checked:** This is not the deliberate consumer-owned-port repetition.

##### P02-11

**Low · Quality · confidence medium**

- **Where:** ceph_export_native.py:77-78, :83-84; ceph_native_object.py:1016, :1381; ceph_replication.py:62
- **What:** These are local inconsistencies:
- The export validator calls `ipaddress.ip_address` without a guard, so bad input becomes `native-unknown` instead of `request`. nfs.py:72-78 wraps the same call.
- Two radosgw-admin reads use plain `json.loads`, without the `unique_object` duplicate-key guard used everywhere else.
- Replication evidence sets `exists` from `owned`.

- **Evidence:** `"owned": any(pool["owned"] ...), "exists": any(pool["owned"] ...)`
- **Impact:** Failures are misclassified and the duplicate-key policy is uneven.
- **Fix:** Wrap the address parse, use `unique_object` in both reads, and drop or redefine `exists` (effort S).
- **Checked:** ceph_native_worker.py:253-258 classifies a non-Refusal exception as native-unknown. replication_evidence.go:36 accepts exists == owned.

##### P02-12

**Low · Efficiency · confidence medium**

- **Where:** ceph_native_nvmeof.py:206-214, :383-385, :415-420; ceph_replication.py:100-104
- **What:** `qualify_commands` runs `ceph --help` and does 10 regex scans on every `nvme_scope`, which means every apply and observe phase plus each unit check. Replication readiness polls at a fixed 5 s for up to 30 min, and each poll makes 2+2·images remote calls.
- **Evidence:** `output = worker.ceph("--help", json_result=False)`
- **Impact:** Repeated full help renders and fixed-rate remote calls on long waits.
- **Fix:** Qualify once per worker (memoize on the instance) and back off the replication poll (effort S).
- **Checked:** Both are bounded; neither is a correctness issue.

#### Patterns
- The attempt/complete record journal is hand-written about 15 times with the same rules (attempt equals intent; complete implies attempt; write attempt before the effect and complete after readback). Examples: resources.py:378-393 and :494-513, ceph_monitoring_template.py:167-204, observability.py:204-228, stretch.py:141-184, units.py:613-623, nvmeof.py:269-306. One small `Journal(name, intent)` helper would own the most safety-critical rule in the lane.
- Lost-reply adoption in observation (`unrecorded = exists and identity is None and canonical(attempt) == canonical(...)`) is repeated per resource: resources.py:885, :969, :1083, :1283, :1450.
- Hand-rolled durable or private file I/O appears five times with different checks: admin_credential_io.py, ceph_admin_credentials_native.py:92-118, stretch.py:661-684, observability.py:577-606, ceph_native.read_regular.
- `del held` / `del created` / `del output` on immutable `str`/`bytes` as "memory hygiene" (object.py:1051, :1085; smb.py:439) frees nothing and implies a guarantee that does not exist.
- Scope helpers live in the wrong module: units and observability import `ceph_native_nfs.port`, `ceph_native_nfs.networks` and `ceph_native_nfs.spec_members` as general validators (units.py:47, observability.py:140, :395).

#### Strengths
- Secret confinement: the replication `QUIET` log overrides because Mirror.cc logs tokens (replication_native.py:16-18); SMB password filters on both directions; bound leaves replaced by placeholders in records and spliced into stdin only (units.py:545-565, object.py:528-536); S3 keys never placed on argv.
- Unknown is kept separate from absent: S3 `Unanswered` once any byte has left (object.py:386-429); observe_subvolume treats a failed listing as absence only where the native tool guarantees it; the tool's empty `default_info` with stderr is refused (object.py:1377-1379).
- Trust: the TLS leaf is pinned by SHA-256 before any byte is sent; the explicit CA path is only for the cephadm root CA; the gateway DNS proof runs out of process with `-I -B` and the FQDN on stdin (ceph_export_dns.py).
- The offline crushtool compile and the template source reads run in a container with no network, a read-only root, all capabilities dropped and a private tmpfs (ceph_host_runtime.contained_argv:427-440).
- Closed-key, exact-type validation everywhere, with native source citations on each assumption.

#### Coverage notes
- admin_credential_io.py: 0%. Both private-file primitives guard credential output and nothing tests them.
- ceph_export_native.py at 82.9%: `ExportWorker.rgw`, `recovery_scope`, `dispatch` and the gateway branch of `validate` (lines 68-78) never run. The RGW tests (test_ceph_export_native.py:296-365) set `native.export["gateway"]` after validation and stub `rgw`, so the ops-user `--gen-secret` path and gateway validation are untested.
- NVMe-oF budget parity across Go and Python is held only by both suites expecting `5024` (test_ceph_nvmeof.py:327, nvmeof_test.go:168). Nothing checks the two formulas against each other.
- ceph_native_stretch.py at 94.7%: the uncovered lines include `TiebreakerRemoval.conf_names` and a branch of `remove_key`, both destructive-path code.
- Python figures are an in-process floor; worker code run in a child interpreter is not measured.

#### Already recorded
- Audit 003 (report-final.md:645-660): the bucket-name limit 44 vs 63 drift between Go and Python. It is now fixed (MAX_BUCKET_NAME = 63); I cite it only as precedent for [P02-05](#p02-05).
- Audit 003 CPL-02: the spec's list of refusals that leave a Ceph attempt failed omits nvmeof-budget. Spec-only; not restated.
- No open B-item tracks [P02-01](#p02-01) to [P02-12](#p02-12) (grepped backlog.md for recovery_scope, bootstrap_hold, crypt, the budget and marker names).

<a id="lane-p03"></a>

### Lane P03: Ceph host runtime, tunnel, packages, registry (Python)
Scope read: I read all 14 scope modules in full, about 8,080 lines: ceph_core, ceph_host_runtime, ceph_host_route_probe, ceph_registry_discovery, ceph_pull_broker, ceph_tunnel, ceph_tunnel_custody, ceph_tunnel_staging, ceph_package_sources, ceph_packages, ceph_package_worker, ceph_crypto_policy, ceph_ssh_policy and ceph_host_worker. To confirm or refute claims I also checked ssh_key_policy.py, filter/host_key_algorithms.py, test_python_sites.py, ceph_native.supported_transport, internal/trust/hostkeys.go, internal/adapterprotocol/process_linux_amd64.go:110, the ceph.md readiness clauses and the architecture.md Language responsibility table. I only sampled the tests (test names, how the fixtures and fakes are built, the loopback servers) and did not read them in full, because they are large (test_ceph_core 2,786 lines, test_ceph_host_runtime 2,518). The lane files have no diff between d8d5672ef and HEAD.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Fail-closed and strict throughout, with one dead workflow path and one unguarded decode |
| Security | 4 | Tunnel, broker and custody isolation are exemplary; no exposure found |
| Efficiency | 3 | About 1 MB of source is rebuilt and shipped on every native call, with no headroom guard |
| Clearness | 3 | `execute` has complexity 54 and ceph_core.py (1,955 lines) mixes four concerns |
| DDD | 3 | Evidence schema, SSH transport and per-action procedures share one module |
| Non-duplication | 3 | Several parallel implementations of the same rule, two of which have already drifted |
| Right language | 4 | Spec-owned procedure only; untested Python programs sit in string constants |
| Test coverage | 4 | 94–100% on the core modules; security-relevant filesystem walks are faked |

#### Findings
##### P03-01

**Medium · Efficiency · confidence medium**

- **Where:** ceph_core.py:586-596, ceph_core.py:620-631, ceph_core.py:556-557
- **What:** every `Transport.call` rebuilds `native_sources()` (29 modules through `inspect.getsource`) and sends it inside the stdin payload. Readiness polling repeats this for each host every 5 s, for up to 900 s.
- **Evidence:** `sources = {module.__name__.rsplit(".", 1)[-1]: inspect.getsource(module) for module in modules}`. The raw source alone is 1,041,152 bytes against `MAX_INPUT = 2 * 1024 * 1024` (ceph_native.py:39). No test checks the size of the shipped bundle against MAX_INPUT.
- **Impact:** about 1 MB of SSH traffic plus a remote compile per call, and half of the input bound is already used, so the cap will be hit without warning as the modules grow.
- **Fix:** memoize `native_sources()` per process, and add a unit test asserting the canonical payload stays under, say, 75% of MAX_INPUT. Shipping per call is deliberate (policy trade-off); only the headroom guard is missing. (S)
- **Checked:** test_native_diagnostics checks the MAX_INPUT of diagnostics, not this bundle. No backlog item covers it.

##### P03-02

**Medium · Non-duplication · confidence high**

- **Where:** ceph_host_runtime.py:500-555, ceph_host_runtime.py:590-650
- **What:** `pull` and `pull_reference` are copies of the same journaled pull loop: attempt replay, inspect-and-adopt, broker, signature policy, retry. They differ only in record names and in cancellation, and have already drifted.
- **Evidence:** pull pauses with `self.pause(PULL_PAUSE)` (548); pull_reference uses `self.rest(PULL_PAUSE, cancel)` (643) and checks `cancel.is_set()` (634, 641).
- **Impact:** a fix to one pull path (redaction, retry, journal shape) can silently miss the other.
- **Fix:** one `_pull(durable, reference, names, cancel)` with the two record-name templates as parameters. (M)
- **Checked:** no exact-clone pair was flagged because the identifiers differ; the tests drive both paths separately.

##### P03-03

**Medium · Non-duplication · confidence high**

- **Where:** ceph_tunnel.py:32-45 and 74-86; ssh_key_policy.py:13-27 and 62-75; filter/host_key_algorithms.py
- **What:** the crypto-policy reducer (the same regex and the same 13 directives) and the key-type to host-key-algorithm table exist twice in Python, plus `hostkeys.go`. The filter narrows by plain intersection, while `ceph_tunnel.pinned_host_key_algorithms` applies ssh_config `+`, `-` and `^` list semantics.
- **Evidence:** `CRYPTO_DIRECTIVES = frozenset((` (tunnel) and `DIRECTIVES = frozenset((` (ssh_key_policy) list the same names. `HOST_KEY_ALGORITHMS = {"ssh-rsa": ("rsa-sha2-512", "rsa-sha2-256"), ...` vs `ALGORITHMS = {"ssh-rsa": "rsa-sha2-512,rsa-sha2-256", ...`.
- **Impact:** audit SEC-07 was exactly this drift (one parser refused `GSSAPIKeyExchange no`). B1133 fixed the symptom; the second copy remains.
- **Fix:** ceph_tunnel must run alone, so make it the owner and have ssh_key_policy import its reducer and table, as ceph_ssh_policy already does. (S)
- **Checked:** test_python_sites names ceph_tunnel a standalone exception, which constrains only its own imports, not imports of it.

##### P03-04

**Medium · Non-duplication · confidence high**

- **Where:** ceph_registry_discovery.py:243-251, 366-397; ceph_pull_broker.py:295-300, 376-405
- **What:** the registry Bearer challenge parser, token validation and Basic credential construction are implemented twice with different rules. (The two modules already share `delivery_location` and `delivery_addresses`.)
- **Evidence:** discovery admits only `{"realm", "service", "scope"}`, while the broker admits `{"realm", "service", "scope", "error", "error_description", "error_uri"}`. The username bound is `<= 256` in discovery and `<= 32768` in the broker.
- **Impact:** the same registry and entitlement can resolve but fail the pull, or the reverse, with different refusals.
- **Fix:** move `challenge`, `token` and `basic` into one helper in ceph_pull_broker (which the worker already ships) and call it from both. (S)
- **Checked:** no test pins the divergence as intended.

##### P03-05

**Medium · Clearness / DDD · confidence high**

- **Where:** ceph_core.py:1224-1313 (and the module as a whole)
- **What:** `execute` has 11 parameters, cyclomatic complexity 54 and depth 5 (gometrics/pymetrics). It mixes selector admission, output admission, a 6-way action dispatch and the inline cluster/pool/client procedure. The 1,955-line module also holds the closed evidence schema (about 500 lines), the SSH Transport, nine per-action executors and a dashboard TLS probe.
- **Evidence:** `if request["action"] == "service": return execute_unit(...)` is repeated five times, followed by the inline `if operation == "apply": ...` branch tree.
- **Impact:** every new Ceph action edits this one function and module.
- **Fix:** a dispatch table keyed by action, an `execute_cluster` and `execute_resource` split, and separate `ceph_evidence` and `ceph_transport` modules. (M)
- **Checked:** the 100-line fitness test passes (90 lines). The procedure itself is spec-owned (ceph.md:895-897), so this is structure, not language placement.

##### P03-06

**Low · Quality (dead code) · confidence high**

- **Where:** ceph_core.py:1196-1221, 1237-1238, 1267, 1281-1282
- **What:** `apply_native_cluster` can never run in production. `execute` refuses any non-observe operation unless `supported_transport`, which requires `clusterSSHTransport == "verifiedTunnel"`, so `tunnel` is always true for a cluster apply.
- **Evidence:** `if operation != "observe": ceph_native.require(ceph_native.supported_transport(request), "unsupported-release")` and `if request.get("clusterSSHTransport") != "verifiedTunnel" ...: return False` (ceph_native.py:239).
- **Impact:** about 30 lines of maintained but unreachable mutation path, which a test calls directly (test_ceph_device_distinctness.py:77).
- **Fix:** delete it and its `else` branch, or record the native transport as a parked item. (S)
- **Checked:** Go still accepts `native` (requests.go:404), but Python refuses it before reaching this code.

##### P03-07

**Low · Quality (error classification) · confidence medium**

- **Where:** ceph_core.py:724, compare 728-731 and 673-689
- **What:** `Transport.packages` decodes the package CA without the UnicodeError guard that every sibling material reader has. A non-UTF-8 Secret is then reported as `adapter-exception` instead of `material`.
- **Evidence:** `certificate = ceph_native.read_regular(self.material[material], private=True).decode("utf-8")`, while four lines later the same read is wrapped in `except UnicodeError: raise ceph_native.Refusal("material")`.
- **Impact:** wrong remedy text for a bad Secret.
- **Fix:** one `Transport.text(name)` helper used by all six material reads. (S)
- **Checked:** `refusal()` maps any non-Refusal exception to `adapter-exception` (ceph_core.py:180). I did not check whether Go pre-validates the PEM.

##### P03-08

**Low · Non-duplication · confidence high**

- **Where:** ceph_core.py:918-941 vs 944-964; ceph_core.py:929, 952, 1409 vs 1531
- **What:** `wait_ready` and `wait_activation` are line-for-line copies apart from the `pending` flag. The "unsettled@" blocking update is repeated three times and differs in `execute_nvmeof`, which drops the prior checks.
- **Evidence:** `blocking[:] = ["unsettled@" + bootstrap["name"]]` (1531) vs `(["unsettled@" + error.host] + [entry for entry in blocking if not entry.startswith("unsettled@")])[:16]`.
- **Impact:** inconsistent timeout detail, and an extra copy to keep in step.
- **Fix:** a single `wait_until(predicate, ...)` and a single `mark_unsettled(blocking, host)`. (S)

##### P03-09

**Low · Right language / Test coverage · confidence high**

- **Where:** ceph_host_runtime.py:99-211, 87-88 vs 135-136, 53 vs 180
- **What:** three Python programs live as string constants (PROBE, LABEL_PROBE, DEVICE_PROBE) and run inside the image. No test executes them, and they restate module constants.
- **Evidence:** the ARTIFACTS tuple is repeated verbatim inside PROBE, and `b"bootwright-contained-public-label-probe-v1\n"` repeats SENTINEL. A grep of tests/unit for these names finds no execution.
- **Impact:** the probes' ownership and mode checks and the crypto-policy prefix check are outside lint and tests, and the constants can drift.
- **Fix:** make them modules shipped through `module_source()`, as ceph_host_route_probe already is, and give them unit tests. (M)

##### P03-10

**Low · Non-duplication · confidence high**

- **Where:** ceph_host_worker.py:65-66, ceph_core.py:481-483, ceph_core.py:623-626, ceph_core.py:607/636/645/654
- **What:** the launcher code-string template is assembled three times, and the budget formula four times. `export` lacks the `timeout > 2` fallback the others have.
- **Evidence:** `budget = min(640, timeout - 2)` (636) vs `budget = min(640, timeout - 2) if timeout > 2 else timeout / 2` (645).
- **Impact:** a launcher change has to be made in three places. For example, a timeout of 2 or less leaves `export` with a budget of 0 or below, which surfaces as a generic `transport` refusal.
- **Fix:** `ceph_host_worker.launcher(source, limit, phase)` plus one `budget(timeout, ceiling)`. (S)

##### P03-11

**Low · Quality (diagnostics) · confidence medium**

- **Where:** ceph_pull_broker.py:541-544, 465-476
- **What:** `serve` swallows every BrokerError (refused delivery location, failed DNS resolution, header or size bounds). `failure_cause` records only HTTP status and untrusted TLS, so these refusals reach the operator only as Podman's generic pull failure.
- **Evidence:** `except (BrokerError, OSError, ValueError, http.client.HTTPException): pass`
- **Fix:** record a fixed category, such as "delivery-resolution" or "bounds", in `self.failure`. (S)

##### P03-12

**Low · Quality · confidence high**

- **Where:** ceph_package_sources.py:25-26
- **What:** `IBM_BASE` and `IBM_KEY` are hard-coded vendor URLs that no production code reads; only tests use them.
- **Fix:** move them to the test fixtures. (S)

##### P03-13

**Low · Clearness · confidence high**

- **Where:** ceph_host_runtime.py:888, 1294 vs 68; 890-891 vs 1290-1291; 1369-1374 vs ceph_tunnel_custody.py:91-96
- **What:** the `KERNEL_DISK` regex is re-inlined twice although the constant exists, the printable-serial check is repeated, and `cache_exists` duplicates `custody.exists`.
- **Fix:** use the existing constants and helpers. (S)

#### Patterns
- Rules that should have one owner exist as parallel copies inside the lane and drift: the crypto policy ([P03-03](#p03-03)), registry auth ([P03-04](#p03-04)), the pull loop ([P03-02](#p03-02)) and readiness waits ([P03-08](#p03-08)).
- Python is generated as strings: launchers ([P03-10](#p03-10)), probes ([P03-09](#p03-09)), DNS_SOURCE (ceph_pull_broker.py:48) and CONTROL_EXEC (ceph_host_route_probe.py:56).
- The closed evidence invariants are re-checked in Python (ceph_core.py:314-315) and in Go (internal/storage/ceph/evidence.go:595). This is deliberate defence at publication, but it is a second copy to keep aligned.
- Obscure idioms: a lambda raising via a generator throw (ceph_host_route_probe.py:733), and a shadowed `value` inside a generator (ceph_tunnel.py:326).

#### Strengths
- The tunnel is fully isolated: a fixed `outer_configuration` (StrictHostKeyChecking yes, GlobalKnownHostsFile /dev/null, ProxyCommand none, IdentityAgent none, ControlMaster no), `execve` with a closed environment, and the bundle re-proved from manifest digests on every proxy start.
- The pull broker never lets credentials leave it: an empty authfile, the token realm pinned to the registry authority, and credential-free delivery only to global public addresses. Upstream connections are explicit (no ambient proxy), and `ProxyHandler({})` plus `NoRedirect` are used in package sources.
- Descriptor-held custody with inode and metadata journaling and re-proof around effects (ceph_tunnel_custody, Cache).
- `_call` collects every material file's bytes for redaction, and refusals travel only as closed reasons with attested causes.
- The tests use real loopback TLS and sockets and assert named refusals and counters, not just that code ran.

#### Coverage notes
- ceph_tunnel_custody.py:62 `directory` is 0%: it is monkeypatched in test_ceph_tunnel_custody.py:49, so the real root-owned path walk (0o022 / 0o700 checks) never runs.
- ceph_packages.py:45 `ceph_root` and `Journal` (flock) are 0%: the tests use a fake Journal (test_ceph_packages.py:125), so the foreign /var/lib/ceph refusal and lock are unexercised.
- ceph_packages.py:233 `installed_headers` is 0%: this is the drift check for a completed transaction.
- ceph_core.py:944 `wait_activation` is at 41%: its timeout and unsettled paths for stretch activation are untested.
- The embedded PROBE, LABEL_PROBE and DEVICE_PROBE scripts are never executed ([P03-09](#p03-09)).
- `Transport.export`, `bootstrap_material` and `prepare_round` are 0% in-process.
- ceph_package_worker `load` and `work` run only in a child interpreter (test_ceph_packages.py:588, 730), so their figure is a floor, not a gap.

#### Already recorded
- Audit 003 RCP-11: the remote probe temporary directory sits in the default tmp (ceph_host_runtime.py:716). The same class also appears at ceph_host_runtime.py:856 and ceph_packages.py:256. No B item tracks it.
- Audit 003 SEC-07 (closed by B1133): the parser divergence. Its root cause, the duplicated reducer, remains ([P03-03](#p03-03)).

<a id="lane-p04"></a>

### Lane P04: Physical erase, Redfish, disk discovery, hardware (Python)
Scope read: I read all 22 scope files in plugins/module_utils (6,153 lines) in full. I also read tests/unit/test_physical_erase_controller.py and test_physical_erase_redfish.py in full. / Skimmed: the test names and fixtures in test_physical_erase_worker.py, _holders, _identity, test_disk_discovery.py and plugins/module_utils/test_disk_inventory.py; plugins/modules/physical_erase.py main; private_records.write; test_python_sites.py exceptions; security-safety "Rules already broken once"; the MAC-predicate clauses in substrates.md:1318-1332 and managed-os.md:1300-1315. / Not read: test_redfish_boot.py (2,377 lines; only grepped), the other redfish unit tests (the coverage figures stood in for them), and the knowledge pages (index checked; nothing explains these findings as forced). Note: HEAD is 21ccc6234, and no lane file differs from d8d5672ef.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | fail-closed throughout; a few dead branches and one stage-cleanup gap |
| Security | 4 | identity, diskseq and holder proofs before each chunk, management TLS never relaxed, credential held to one authority; one durable-write rule broken |
| Efficiency | 4 | bounded polls; a new SSL opener for every Redfish request and one doubled inventory walk |
| Clearness | 3 | dense but precise docstrings; `Client.insert`'s trust table is hard to follow (complexity 35) |
| DDD | 4 | proofs, transport and custody are separate modules; the identity proof is written twice |
| Non-duplication | 3 | second process runner, second identity proof and second disk-serial reader, with drift |
| Right language | 3 | a 140-line Python runtime plus sshd config and units embedded as strings |
| Test coverage | 3 | the worker tests are excellent; the custody identity and foreign-state refusals never run |

#### Findings
##### P04-01

**Medium · Security · confidence high**

- **Where:** plugins/module_utils/physical_erase_worker.py:543-553; physical_erase_boot.py:100
- **What:** `atomic` creates `<path>.new` with `O_EXCL` and never removes it when the write or fsync fails. The next call for the same path then raises `FileExistsError`. This breaks the rule "a durable write stages and renames, and removes its own stage when it fails". The RAM runtime's `ack` handler uses `atomic(ROOT / "ack.json", ...)`. After one failed ack write, every retried ack fails, and the worker never powers off.
- **Evidence:** `fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)` … `finally: os.close(fd)` (no unlink). The function has 0% measured coverage.
- **Impact:** on an uncommon path (a tmpfs write failure), the controller can never complete the acknowledgement for that boot.
- **Fix:** in an `except BaseException:` block, unlink the stage, or reuse `private_records.write`'s stage-and-link shape (a random stage name, unlinked in `finally`). Effort S.
- **Checked:** `private_records.write` (lines 29-39) does clean up. Nothing reaps `.new`. The RAM tree is fresh on each boot, so only the in-boot paths are affected.

##### P04-02

**Medium · Non-duplication · confidence high (policy trade-off)**

- **Where:** physical_erase_process.py:19-84; test_python_sites.py:42; disk_discovery_boot.py:139; physical_erase_controller.py:20,286
- **What:** `physical_erase_process.run` is a second bounded process runner beside `bounded_process.py`. Its named exception says "the erase RAM image ... carries no sibling module". But the sibling disk-discovery RAM image ships `bounded_process` and `canonical_json` as standalone files. The erase controller runs on the execution host, not in RAM, and still uses the duplicate runner.
- **Evidence:** `modules = (absolute_deadline, bounded_process, canonical_json, disk_inventory, physical_machine_identity, disk_discovery_worker)`. The worker also keeps a private `canonical()` (worker.py:51), the second named exception.
- **Impact:** two runners with different kill, wait and env semantics on safety-critical paths. A fix to one does not reach the other. For example, `process.wait(timeout=5)` in the `finally` block can raise `TimeoutExpired` and hide the original error.
- **Fix:** ship `bounded_process.py` and `canonical_json.py` in the erase kickstart `files` map, the way discovery does. Move the controller to `bounded_process`. Drop both exceptions. Effort M.
- **Checked:** the stated reason, against `disk_discovery_boot.payload`. The disk-discovery worker imports `bounded_process` standalone (disk_discovery_worker.py:27-32).

##### P04-03

**Medium · Right language · confidence high**

- **Where:** physical_erase_boot.py:13-154 (`RUNTIME`), 109-132 (sshd_config), 183-204 (units)
- **What:** The RAM erase runtime (isolation, receipt server, ack and poweroff) is a raw Python string. Tests reach it with `exec(compile(boot.RUNTIME, ...))`. Coverage, pylint and the python-site scans cannot see it. The module reports 100% because only 18 outer statements count. The `serve()` "ack" branch (lines 93-100), the step that authorizes poweroff, has no test.
- **Evidence:** `RUNTIME = r'''` ... `def serve(): ... elif original == "ack":`. The tests grep shows no `ack` or `SSH_ORIGINAL_COMMAND` case.
- **Impact:** the code that ends a destructive boot gets the weakest tooling in the lane. Discovery already shows the better pattern: a real module shipped through `inspect.getsource`.
- **Fix:** move it to `physical_erase_runtime.py`, ship it like the other three files, and add ack tests (wrong digest, missing receipt). Effort M.
- **Checked:** test_physical_erase_worker.py:174, 252 and 699 exec the string; the isolation refusals are well tested.

##### P04-04

**Medium · Non-duplication · confidence high**

- **Where:** physical_erase_redfish.py:41-49; disk_discovery_controller.py:371-376 (also plugins/modules/redfish_system_inspect.py:156)
- **What:** The BMC proof of system identity (UUID plus the complete MAC set) is written twice, and the copies have drifted. Erase lowercases both sides. Discovery compares to `self.binding["uuid"]` unnormalised, and `validate()` never checks the binding's `uuid` or `macs` grammar.
- **Evidence:** `identity["UUID"].strip().lower() != request["uuid"].strip().lower()` vs `identity["UUID"].strip().lower() != self.binding["uuid"]`
- **Impact:** a wrong-target guard kept in two places. The discovery copy has 0% coverage (see Coverage notes), so its drift goes unnoticed.
- **Fix:** one `prove_system(client, uuid, macs)` in `redfish_control` (or `physical_machine_identity`), used by both, with tests. Effort S.
- **Checked:** the copies fail closed (an uppercase binding refuses rather than mis-targets). No B-item tracks this.

##### P04-05

**Medium · Clearness · confidence high**

- **Where:** redfish_control.py:546-583, 773-776
- **What:** `Client.insert` (complexity 35) evaluates the same trust predicates again under different names. Because `delegated` is always true when `trust == TRUST_DISABLED`, the guard `not (delegated and trust == TRUST_DISABLED)` reduces to `trust != TRUST_DISABLED`, and `elif delegated and trust == TRUST_DISABLED` reduces to `else`. As a result, `_trust_media`'s `TRUST_DISABLED` branch can never run, and its docstring ("disableVerification turns it off") describes dead code.
- **Evidence:** `if not (delegated and trust == TRUST_DISABLED): self._trust_media(...)`; `if trust == TRUST_DISABLED: return self._configure_media_verification(member, "", False)`
- **Impact:** readers cannot see the real decision table for the leg that carries private media, which is the boundary where unverified media is allowed.
- **Fix:** compute one explicit table (established/import/disabled × enable_verification) up front, and delete the unreachable branch. Effort S.
- **Checked:** the only caller of `_trust_media` is line 569. Measured coverage of `_trust_media` is 71%, with the disabled branch unrun.

##### P04-06

**Low · Quality · confidence medium**

- **Where:** redfish_control.py:755-758; redfish_media_verification.py:50-51
- **What:** `eject()` on a controller with no media device, with `restoreVerification` set, calls `_configure_media_verification("", "", True)`. When no security service publishes the switch, `preflight` falls back to `client._read("")`, which reads the ComputerSystem as if it were the media device.
- **Evidence:** `body, headers = client._read(member, DOING)` with `member == ""` resolves to the endpoint, then `setting(member, "VerifyCertificate", ...)`.
- **Impact:** cleanup refuses with a misleading "declared boolean property" message when there is nothing to restore. On a system that did publish `VerifyCertificate`, it would PATCH the wrong resource.
- **Fix:** return "nothing to restore" (or refuse with a specific reason) when `member` is empty and no service is found. Effort S.
- **Checked:** no test covers the case where the device is missing and restore is set.

##### P04-07

**Low · Non-duplication · confidence medium**

- **Where:** disk_inventory.py:49-68; physical_erase_worker.py:240-256
- **What:** The disk serial is read in two ways. Discovery reads `device/serial`, then `serial`, then VPD 0x80 (accepting 4 bytes and its own grammar). Erase reads only `device/serial`, then VPD (5..260 bytes, any printable text).
- **Evidence:** `value = attribute(device / "serial", optional=True)` has no counterpart in `disk_serial`.
- **Impact:** a disk whose serial discovery found through `<node>/serial` cannot be resolved by erase. It fails closed with `disk-serial-absent`, so this costs usability, not safety.
- **Fix:** both RAM images can ship `disk_inventory.py`; have the worker call `disk_inventory.serial_number`. Effort S.
- **Checked:** the `NAME` filter (`sd*`/`nvme*`) narrows how often this happens; `serial_number` has 0% coverage.

##### P04-08

**Low · Efficiency · confidence high**

- **Where:** redfish_control.py:263, 135-175
- **What:** Every `fetch` builds a new opener and SSL context and parses the CA bundle again. In certificate mode it calls `tls_trust.context` twice. The ternary at line 263 has two branches that do the same thing.
- **Evidence:** `with (_opener(self.verify, self.ca_data, self.verification) if self.verification else _opener(self.verify, self.ca_data)).open(`
- **Impact:** polls of 24-60 requests rebuild TLS state each time, and the duplicated expression hides that nothing differs.
- **Fix:** build the opener once in `__init__`. Effort S.
- **Checked:** `__init__` already calls `_trust_context` to validate the settings.

##### P04-09

**Low · Efficiency · confidence high**

- **Where:** redfish_root_volume.py:89, 94
- **What:** `discover()` with `{"firmware": true}` walks the full Storage/Drives/Volumes inventory twice, which is up to 256 reads per walk.
- **Fix:** reuse the first `(volumes, drives)`. Effort S.
- **Checked:** no state changes between the two calls.

##### P04-10

**Low · Clearness · confidence high**

- **Where:** disk_discovery_worker.py:71, 227-229, 247
- **What:** `len(set(observed.values())) != len(observed)` runs on every loop iteration and can never fire: the values are distinct sysfs names, and a duplicate MAC has already collapsed in the dict comprehension. The real guard is `identity.prove`. Separately, `collect` writes `/run/discovery/report.json`, which nothing reads, because `serve` answers from memory.
- **Fix:** delete both, or make the observed map refuse duplicate MAC keys explicitly. Effort S.
- **Checked:** `physical_machine_identity._addresses` refuses duplicate addresses (line 78); a grep found no reader of `report.json`.

##### P04-11

**Low · Clearness · confidence medium**

- **Where:** plugins/modules/physical_erase.py:126-128 (consumer of this lane's codes); physical_erase_controller.py:299-307
- **What:** The worker and controller define closed, non-sensitive refusal codes (`foreign-target`, `disk-identity`, `controller-identity`, ...). The module drops all of them except `worker-failed` and prints one generic message, unlike discovery's `diagnostic()` mapping. Also, `await_receipt` checks `state` before the foreign identity, so a foreign failed worker reports `worker-failed`, whose remedy is "repeat".
- **Fix:** return the code as a field, and check identity before state. Effort S.
- **Checked:** the per-custody pinned host key makes a foreign worker unlikely.

#### Patterns
- Wrong-target proofs are tested at the worker but mocked at the custody layer: `instance.target = Mock(return_value="Off")` (test_disk_discovery.py:168), and `instance.load = lambda: ...` (test_physical_erase_controller.py:83, 138, 155).
- Standalone RAM images carry their own copies of shared helpers (process runner, canonical JSON, serial reader, atomic write: physical_erase_worker.py:51, 543; disk_discovery_worker.py:122). Discovery shows that shipping the shared module works.
- Three durable-write shapes with different cleanup semantics: private_records.write (stage+link), physical_erase_worker.atomic (`.new` + replace, no cleanup), disk_discovery_worker.write (direct O_EXCL).
- The MAC predicate differs between machine_hardware.py (permanent address, VF and USB-subsystem exclusion) and physical_machine_identity.py (USB-driver exclusion). Both specs own their rule (substrates.md:1323-1329, managed-os.md:1306-1310), so this is deliberate, but there is no shared test vector tying the BMC-claim and RAM-proof views together.

#### Strengths
- The erase worker re-proves diskseq, size, WWN and serial before every 4 MiB chunk, proves the complete set before the first write, releases only device-mapper stacks whose slaves are all declared (by dev number), and checks mounts in every mount namespace (worker.py:80-92, 418-449, 195-216).
- Operator media is held by descriptor and passed as `/proc/<pid>/fd/<n>`, so a pathname replaced after the hash check is never read (physical_erase_controller.py:197-236; disk_discovery_media.py:146-157), and a test pins this.
- `redfish_control._own` checks every reference against the endpoint authority before a request or credential exists. There is no redirect and no ambient proxy, and failure lines never carry bodies.
- Discovery's BMC inventory and root-volume proofs fail closed on any pagination, count mismatch, duplicate or logical volume, and fall back to booting rather than guessing.
- The worker tests assert behavior: concurrency stop, deadline, hotplug, short write, holder release order, and the isolation refusals.

#### Coverage notes
- disk_discovery_controller.py:371 `Controller.target` and :43 `validate` (the request TLS, path and binding checks): 0%. The wrong-target and TLS gate of discovery never runs in-process.
- physical_erase_controller.py:106 `load` (foreign-custody, foreign-target), :116 `prepare` (50 statements: receipt classification, `custody-policy-drift`, `custody-image-drift`), :321 `control`: 0%.
- physical_erase_boot.py:93-100: the RAM `ack` handler that gates poweroff is untested.
- physical_erase_redfish.py:59 `finish` (the poweroff proof loop before eject): 0%.
- disk_discovery_worker.py:95 `bond_commands` and :220 `prepare_network`: 0%. The bond `ip link add ... type bond` argv that runs on a live installer is never checked.
- disk_inventory.py:49 `serial_number` (VPD parsing): 0%, although it supplies the identity erase later relies on.
- physical_erase_worker.py:332 `BlockDisk.__init__` (`O_EXCL`, `S_ISBLK`, partition refusal): 0% in-process. These checks are kernel-bound, but the refusal branches could be driven with a fake fd.
- disk_discovery_media.py:109 `qualified_initrd` (lsinitrd parsing and symlink resolution): 0%.

#### Already recorded
- None of these findings matches audit 003 or an open B-item. Audit 003's redfish_system_inspect entry (report-final.md:1361, which cites redfish_control.py:384-393) concerns the system-read swallow in the module, a different defect.

<a id="lane-p05"></a>

### Lane P05: KubeVirt, libvirt, managed-OS workers and native resolution (Python)
Scope read: all 20 lane module_utils read in full (8,356 lines) / skimmed: test_native_specializations.py, test_managedos_update_worker.py, test_kubevirt_api_tls.py, test_libvirt_nm_connections.py:480-492, test_libvirt_host_tagged_golden.py header, test_managedos_host_firewall.py:1051-1063, roles' domain.xml.j2 and network.xml.j2 (interpolations only), Go internal/substrate/libvirt/routing.go and internal/substrate/naming.go (derivation lines), specs/architecture.md:778-830 and 1131-1172, specs/managed-os.md:1848-1870 / not read: the remaining ~6,000 test lines, which I sampled by test name only

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Rigorous ownership and answered-versus-absent semantics; a few unclosed dnf bases and null-unsafe reads |
| Security | 4 | Closed kubeconfig form, no ambient trust, descriptor-held file I/O, workers emit closed reasons only |
| Efficiency | 3 | Process spawns per service×zone and per NetworkManager profile, repeated full re-inspections |
| Clearness | 3 | Thorough docstrings, but several CC 25-35 functions and magic syscall constants |
| DDD | 4 | Each module holds one effect's procedure; Go-derived names and rules are re-derived (see Patterns) |
| Non-duplication | 3 | Five atomic-write copies, three systemd-show parsers, two route parsers, duplicated helpers |
| Right language | 4 | Python stays on "how one selected effect is proved"; specialization selection follows D162 |
| Test coverage | 4 | 81-100% per file with behaviour-level tests; the security-relevant leaf readers are never run |

#### Findings
##### P05-01

**Medium · Efficiency · confidence medium**

- **Where:** managedos_firewall_worker.py:486-497, 504-505, 359-366
- **What:** The firewall worker gathers evidence by starting one `firewall-cmd` process for each (service, interface zone) twice, and for each (service, zone in permanent∪runtime) once more. Apply then calls `evidence()` again after its own loops.
- **Evidence:** `permanent = self.lists(zone, name, True)` / `runtime = self.lists(zone, name, False)` and `for zone in sorted(set(zones) | set(live)): ... self.lists(zone, name, True)) or (... self.lists(zone, name, False))`. The same file's rationale for REGISTRY_COMPARISON (75-76) says one process "avoid[s] hundreds of firewall-cmd launches".
- **Impact:** About 25 Python-startup and D-Bus round trips per service on a default ~10-zone firewalld, repeated for apply and inspect. That is tens of seconds per run, inside the session budget.
- **Fix:** Read every zone's permanent and runtime services once, through the same `FirewallClient` child it already uses or one `--list-all-zones` per side, then answer `lists()` from that snapshot. (effort M)
- **Checked:** No cache exists in `Guest` (`owned_names` is the only state). The `MAX_ZONES`/`MAX_SERVICES` bounds cap the worst case at 64×64×2 calls but do not shrink it. No backlog item.

##### P05-02

**Medium · Efficiency · confidence medium**

- **Where:** libvirt_nm_connections.py:546-551, 727-737, 804-806, 839-844
- **What:** `foreign_devices` runs `nmcli connection show uuid <u>` once for every profile the provider does not own. `settle` runs `inspect` three times, and `remove` and `prune` run it twice.
- **Evidence:** `for profile in listing: ... shown = details(runner, profile["uuid"], "connection.interface-name")`. `MAX_PROFILES = 1024` bounds the listing.
- **Impact:** On a host whose NetworkManager holds many profiles, including external in-memory ones for vnet or veth devices, one apply launches several hundred nmcli processes.
- **Fix:** Query `connection.uuid,connection.interface-name` for all UUIDs in one nmcli call (nmcli accepts several `uuid` ids), or reuse the first inspect's foreign-device result across a settle. (effort S-M)
- **Checked:** `profiles()` cannot carry interface-name, because that field is not in nmcli's list fields, so the second read is needed but not per profile. No tracking item.

##### P05-03

**Medium · Non-duplication · confidence high**

- **Where:** libvirt_routing.py:307-340, libvirt_nm_carrier.py:149-164, libvirt_vlan_refusal.py:283-294, libvirt_nm_connections.py:672-685, libvirt_firewall_ready.py:218-229 and 47-75
- **What:** Five sibling module_utils each implement stage, fsync and rename differently. Only two of them fsync the directory after the rename. `firewall_ready.inspect_files` is a near-copy of `vlan_refusal.unit_files`, differing only in the wants directory and in comparing the whole content.
- **Evidence:** routing: `directory = os.open(rules, ...); os.fsync(directory)`. carrier: `os.fsync(descriptor)`. vlan_refusal `write_unit`, nm `write_keyfile` and the firewall_ready inline copy all end at `os.replace(stage, path)` with no directory fsync.
- **Impact:** The durability of a systemd unit or NetworkManager keyfile after a crash depends on which file wrote it, and every ownership-rule change has to be made in two or three places.
- **Fix:** One `publish(dir, name, data, mode)` helper with a directory fsync, and one `unit_files(name, mark, expected, wants=...)`. These are ordinary module_utils, not shipped workers: libvirt_routing already imports `unit_files` and `write_unit` from vlan_refusal. (effort M)
- **Checked:** test_python_sites.py names no exception for these files. The `systemctl show` → dict parsing is also repeated five times (substrate_libvirt.py:117, libvirt_nm_connections.py:398, libvirt_routing.py:492, libvirt_vlan_refusal.py:253, libvirt_firewall_ready.py:159).

##### P05-04

**Low · Quality · confidence medium · policy trade-off**

- **Where:** managedos_update_worker.py:137-168, 192-205
- **What:** The spec's release-drift refusal is proved on the preview's resolve. The transaction that runs is a separate `dnf upgrade --refresh`, which fetches metadata and resolves again.
- **Evidence:** `if package.name == "redhat-release" and package.version != release: raise Refused("update-release-drift"...)` then `argv = [DNF, "upgrade", "--refresh", "--assumeyes", ...]`.
- **Impact:** A repository that publishes between preview and upgrade can move `redhat-release`. That is caught only by step 6's VERSION_ID check after the reboot, when the move cannot be undone.
- **Fix:** Drop `--refresh`, since the preview already set `metadata_expire = 0` and downloaded the packages. Or run the previewed `base` with `conf.comment` set, as native_apply does. (effort S)
- **Checked:** specs/managed-os.md:1852-1858 prescribes both steps, so this is a spec-level trade-off. The window is short.

##### P05-05

**Low · Quality · confidence high**

- **Where:** native_resolution.py:474-477, 400-457; native_signatures.py:136-140
- **What:** `inspect()` builds a `dnf.Base` and never closes it. `solve4` without `return_transaction` does the same. native_apply calls `inspect` twice while its own base is open. In `verify`, `closeDB()` is skipped when the key import raises.
- **Evidence:** `values = inventory4(setup_dnf4(probe, scratch, repositories=False))`, compared with managedos_update_worker.py:169-170 `finally: base.close()`.
- **Impact:** RPMDB handles and the sack stay open until process exit in a root package transaction. Today this is benign because the worker is short-lived.
- **Fix:** Wrap each in `try/finally: base.close()`. (effort S)
- **Checked:** Callers in ceph_packages.py:262 and 271 do not close the base either.

##### P05-06

**Low · Clearness · confidence medium**

- **Where:** native_resolution.py:79-81
- **What:** The RPMDB read lock is a raw `fcntl(lock, 37, struct.pack("hhqqi4x", ...))`. 37 is Linux `F_OFD_SETLK`, which does not block, so a running rpm transaction raises an unclassified `BlockingIOError`. Neither fact is named.
- **Evidence:** `fcntl.fcntl(lock, 37, struct.pack("hhqqi4x", fcntl.F_RDLCK, os.SEEK_SET, 0, 0, 0))`
- **Impact:** Reviewers cannot check the lock semantics or the struct layout, and the busy-RPMDB failure has no closed cause. Coverage is 3%.
- **Fix:** Use a named constant (`getattr(fcntl, "F_OFD_SETLK", 37)`), add a one-line rationale, and map EAGAIN or EACCES to a named refusal. (effort S)
- **Checked:** No test runs this path. test_ceph_packages.py:184 only reaches the unsupported-platform refusal.

##### P05-07

**Low · Clearness · confidence high**

- **Where:** substrate_libvirt.py:582-622, 560-565 vs 577-579
- **What:** `domain_disk_bindings` (CC 35, depth 4) mixes three proofs in one body: SCSI address uniqueness, per-disk file, bus and serial, and SSD/HDD presentation. The rotation-rate parse is copied between `attached_disks` and `disk_media`.
- **Evidence:** `int(rate) if rate.isascii() and rate.isdigit() and len(rate) <= 5 else 0` appears twice.
- **Impact:** This ownership proof gates destroy, and it is the hardest function in the lane to review.
- **Fix:** Split it into `scsi_addresses_unique`, `disk_matches` and `media_matches`, and share a `rotation_rate(target)` helper. (effort S)
- **Checked:** B1119 tracks the proof's strictness, not its structure.

##### P05-08

**Low · Test coverage · confidence high**

- **Where:** tests/unit/plugins/module_utils/test_libvirt_nm_connections.py:486-488
- **What:** A test named `test_the_connection_uuid_is_the_one_go_derives` compares Python with itself.
- **Evidence:** `assert nm.connection_uuid("lab", "bootwright-lab-ceph-public") == nm.connection_uuid("lab", "bootwright-lab-ceph-public")`
- **Impact:** The name claims a cross-language guarantee the test does not check. The real parity check is test_libvirt_host_tagged_golden.py:48, against the golden Go writes.
- **Fix:** Assert a literal UUID produced by `internal/substrate/naming.go:188`, or rename the test. (effort S)
- **Checked:** The golden test does exist, so parity is covered. Only this test is misleading.

##### P05-09

**Low · Efficiency · confidence medium**

- **Where:** kubevirt_api.py:210-243
- **What:** Every request, including each 5-second poll in `Host.wait`, builds a new `SSLContext`, writes the client key and certificate to a fresh temporary directory, and opens a new TLS connection.
- **Evidence:** `directory = tempfile.mkdtemp(...)` then `context = self._context(directory)` inside `_exchange`.
- **Impact:** Repeated private-key writes to disk and a TLS handshake per poll across 300-second waits.
- **Fix:** Build the context once per `Client`: load the certificate chain, then remove the files. Optionally keep the connection alive. (effort S)
- **Checked:** Files are 0600 in a mkdtemp directory and removed, so there is no exposure finding. The TLS test suite would pin the change.

##### P05-10

**Low · Non-duplication · confidence high**

- **Where:** kubevirt_machine.py:70-74, kubevirt_access.py:123-127, kubevirt_machine.py:908/937/980 vs 574
- **What:** Two problems.
- Kubernetes timestamps are parsed three ways. `kubevirt_machine.timestamp` duplicates `cluster_objects_model.timestamp` (37-40), a module this file already imports at line 518, and `kubevirt_access.instant` parses the same format with naive `strptime`.
- The media volume name is re-derived as `vm + "-media"` in eject, upload_volume and attach, while read_machine uses `request["mediaVolume"]`, which Go sets (selection.go:219).

- **Evidence:** `volume = vm + "-media"` vs `for volume in (request["rootVolume"], request["mediaVolume"])`
- **Impact:** The media name is one rule in two languages, and the port and the block could drift apart.
- **Fix:** Import the one timestamp helper, and carry the media volume name in the port request. (effort S)

##### P05-11

**Low · Quality · confidence low**

- **Where:** kubevirt_machine.py:801, 811
- **What:** Two reads use `.get(key, {})`, which fails on an explicit JSON `null`. Everywhere else the file uses `(x.get(k) or {})`.
- **Evidence:** `.get("annotations", {}).get(MEDIA, "")` and `.get("domain", {}).get("devices")`
- **Impact:** An API that returns null annotations would raise an `AttributeError`, which no handler classifies.
- **Fix:** Use the file's `or {}` idiom. (effort S)
- **Checked:** Kubernetes normally omits empty maps rather than sending null, so the risk is low.

#### Patterns
- **Go rules re-derived in Python as fail-closed validation (policy trade-off).** Examples: `libvirt_routing.model` re-checks forwarding as NAT-or-routed and managed devices as trunks∪legs∪bridges (routing.py:127, 136-141, against routing.go:40-75); `connection_uuid` (nm_connections.py:131) and `kubevirt_machine.name` (49) copy naming.go:77/188; trunk and leg name formats (nm_connections.py:89, 114). Goldens and capability_test.go:34 hold parity, but each rule change touches two languages.
- **Shipped workers copy their prelude by design.** `read_bounded`, `Guest.write`/`sync_directory`/`read_record`, `Refused`, `refusal`/`main` and the subprocess wrapper are copied across managedos_rhsm_worker.py:185-195, 346-412, managedos_update_worker.py:81-91, 224-285 and managedos_firewall_worker.py:218-228, 398-403. They are named stand-alone exceptions (test_python_sites.py:33-35). A ship-time prelude concatenation would remove the copies; this is a policy trade-off.
- **"Bounded" capture buffers all output first.** `bounded_capture` (rhsm_worker.py:248-254) truncates after `subprocess.run` has already buffered everything, and firewall `run_command` (209-215) refuses only afterwards. The workers cannot use `bounded_process`, so the name overstates the bound.
- **One process per item inside an observation loop.** `network_guests` runs one `virsh dumpxml` per running domain, per drifted network and per trunk (substrate_libvirt.py:341-350, 860); `packages_present` runs one `rpm --query` per package (100-106).
- **Two parsers of the same route listing with diverging exemption rules.** The "broader main-table gateway route" exemption is implemented in both substrate_libvirt.py:385-421/458 and libvirt_vlan_refusal.py:162-192/230.

#### Strengths
- The kubeconfig acceptance is a closed key set: one cluster, user and context; an https server with no path; embedded CA only; no system trust, proxy or redirect. It is tested against a real TLS server (test_kubevirt_api_tls.py).
- Ownership proofs use a recorded UID, then a domain-separated mark plus a creation time. Lost create replies are adopted, and "unanswered" is never read as "absent" (kubevirt_machine, substrate_libvirt).
- File I/O is held through descriptors throughout: `O_NOFOLLOW`, `dir_fd` traversal, fstat identity before and after, owner and mode proofs (nm_carrier, vendor_dropins, native_apply, rhsm CA publish).
- The guest workers answer one line of closed reasons, never echo secrets (activation key only over D-Bus, never argv), and journal pending state for lost replies.
- native_specializations matches D162: facts come from proved artifact bytes, unknown facts select the general variant, and the test proves exclusivity over every fact assignment (test_native_specializations.py:328).

#### Coverage notes
- native_resolution.py:71 `snapshot_database`, 3%: the RPMDB lock, the root-owner proof and the stable-copy identity check never run, and this is the root package path's TOCTOU guard.
- native_signatures.py:34 `read_key`, 0%: every test mocks the publisher-key authority checks (root-owned, size, identity), which gate signature trust.
- managedos_rhsm_worker.py:209 `consumer_identity`, 0%: the certificate-CN parse decides owned versus foreign registration and is always injected.
- kubevirt_api.py:119 `load`, 0%: the kubeconfig size bound, the `O_NOFOLLOW` open and the parser-failure refusal are untested.
- managedos_firewall_worker.py:317 `Guest.resolve`, 18%: the `getent ahosts` path for hostname addresses, including the "more than one address" refusal.
- The rhsm, update and firewall workers' subprocess wrappers are at 0% (`bounded_run`, `bounded_capture`, `run_command`): the timeout and missing-tool mappings to closed causes are unpinned.
- native_resolution.py:267 `package_record`, 75%: the source-URL containment and integrity refusals (281-297) are partly unexercised.

#### Already recorded
- SBV-01 (audit 003): substrate_libvirt `host_routes` skips default and /0 routes in every table.
- B1119: the strictness of `domain_disk_bindings` under observation. [P05-07](#p05-07) reports only its structure.

<a id="lane-p06"></a>

### Lane P06: Controller, protocol, cluster objects, shared utilities, action plugins, filters, callback (Python)

Scope read: I read these files in full: bounded_process.py, controller_files.py, controller_supervisor.py, controller_channel.py, controller_refusal.py, adapter_protocol.py, artifact_server.py, infra_service.py, cluster_objects_engine.py, cluster_objects_native.py, private_records.py, tls_trust.py, callback/censored.py, action/{artifact_server_protocol, infra_service_protocol, cluster_objects_write, controller_tool, substrate_kubevirt_api}.py, filter/{registry_pull_secret, installer_proxy_credentials, dns_forward_zones, machine_port_state}.py.
I skimmed: hosted_area.py (head), host_sockets.py (clone region), cluster_objects_read.py (diff only), the other *_protocol plugins (clone regions only), test_censored_output.py, the test_bounded_process.py test list, test_controller_supervisor_native.py, and the Go side of the cluster-object evidence (internal/addons/clusterobjects/driver/evidence.go, EvaluateEvidence).
I did not read: cluster_objects_model.py, cluster_objects_secrets.py, the containercluster_* utilities, host_addresses.py, ssh_key_policy.py, transport_faults.py, absolute_deadline.py, remote_command.py, canonical_json.py, or the large substrate/managedos protocol plugins. Time went to the lead hotspots instead; their measured complexity and coverage were unremarkable except where noted.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Careful bounds and refusals; the process runner can hide its own failure kind, and one kernel-dependent syscall is a risk |
| Security | 4 | Descriptor-relative opens, pinned TLS before a credential is sent, no_log hygiene and secret-free errors are exemplary |
| Efficiency | 3 | The cluster-object engine re-reads everything on each poll through one `_execute_module` round trip per object |
| Clearness | 3 | Engine.chain cc 61, Engine.subscription cc 55, bounded_process.run cc 36 / depth 6; unknown is signalled by rewriting an observation |
| DDD | 3 | Python computes a classification Go never reads; the engine's request dict doubles as mutable state |
| Non-duplication | 2 | artifact_server.py copies six infra_service functions even though it imports that module; the twin protocol plugins and read/write actions repeat each other |
| Right language | 3 | Filters have no side effects and protocol publication is clean; the engine holds a lot of OLM procedure and judges a classification |
| Test coverage | 4 | The process runner, the censored callback (tested end to end in a child) and the supervisor are proved; the readiness probes are untested |

#### Findings

##### P06-01

**Medium · Non-duplication · confidence high**

- **Where:** plugins/module_utils/artifact_server.py:10, 23-84, 97-107; plugins/module_utils/infra_service.py:34-95, 171-186
- **What:** artifact_server.py imports infra_service and calls `infra_service.started_after`, yet it carries its own verbatim copies of `probe_failure`, `invoke`, `podman`, `unit_state`, `container_image` and `container_present`, plus the constants. Its `status_line` repeats the status-line reader in `infra_service.probe_http`.
- **Evidence:** `from ansible_collections.bootwright.core.plugins.module_utils import host_sockets, infra_service` (line 10), followed by `def podman(runner, arguments):` (line 50), which is identical to infra_service.py:61. The clone data reports 34 identical lines.
- **Impact:** a fix to podman handling, to unit-state reading or to the probe reason has to be made twice. The two copies already differ in the HTTP request they send (`Connection: close` is only in artifact_server).
- **Fix:** delete the copies, call the infra_service functions, and pull one `read_status_line(sock, host)` helper out of the two readers. (effort S)
- **Checked:** no named exception or comment justifies the copy, and both files run on the same managed-host site.

##### P06-02

**Medium · Quality · confidence medium**

- **Where:** plugins/module_utils/bounded_process.py:119-120, 136-138
- **What:** `stop()` runs in `run()`'s `finally` and raises `Failure("command")` when the kill-and-reap does not finish within 1 s. A raise inside a `finally` replaces whatever was in flight, so a `timeout` or `output` failure becomes `command`, and an already built `Result` is discarded.
- **Evidence:** `finally:\n        stop(process, session)` and, in stop, `process.wait(timeout=1)\n    except (subprocess.TimeoutExpired, OSError):\n        raise Failure("command") from None`
- **Impact:** the module docstring says callers map the three kinds to their own refusals. A process stuck in uninterruptible sleep, for example on a hung NFS mount or a dying device, is then reported as "could not start" instead of "outran its time", which points the operator at the wrong cause.
- **Fix:** in `run`, catch `stop`'s failure and re-raise it only when no other `Failure` is propagating and no result was produced. Or have `stop` return a flag and raise in `run` after the original exception. (effort S)
- **Checked:** test_bounded_process.py:151 covers kill-and-reap after a failure but not a reap that times out. No caller distinguishes the cause in any other way.

##### P06-03

**Medium · Quality · confidence medium**

- **Where:** plugins/module_utils/controller_files.py:680-694, 884-886, 912
- **What:** tools are published with `O_TMPFILE` and then `linkat(fd, "", parent, leaf, AT_EMPTY_PATH)` through ctypes. linkat(2) says AT_EMPTY_PATH "requires the CAP_DAC_READ_SEARCH capability". Kernels before about 6.10 enforce that. The controller runs this unprivileged (`Bundle.open_path` requires `st_uid == os.geteuid()`), and specs/controller.md:43-55 admits RHEL derivatives, so RHEL 9 with kernel 5.14 is admitted, with glibc floor 2.17 and a Python 3.12 ABI.
- **Evidence:** `if linkat(descriptor, b"", parent, leaf.encode("ascii"), AT_EMPTY_PATH) != 0:`; lock.json `"tested"` lists Fedora 43 and 44 only.
- **Impact:** on an admitted RHEL 9 or Rocky 9 controller, every tool acquisition would fail with ENOENT, which shows up as a `storage`/generic refusal. This is a whole-feature failure on a supported host.
- **Fix:** use the portable form `linkat(AT_FDCWD, "/proc/self/fd/<n>", parent, leaf, AT_SYMLINK_FOLLOW)`, which needs no capability, and resolve libc once at import rather than on every call. (effort S)
- **Checked:** link_at is covered by the unit suite, so it works unprivileged on this host (kernel 7.3), which fits the newer relaxation. Whether 5.14 refuses comes from the man page and my memory of the 6.10 change, not from a run. No knowledge page or backlog item mentions it.

##### P06-04

**Medium · Non-duplication · confidence high**

- **Where:** plugins/action/artifact_server_protocol.py:61-133; plugins/action/infra_service_protocol.py:59-131
- **What:** `remaining`, `unproved`, `absence`, `refused` and the postcondition expression are copied between the two plugins. They differ only in subject text, refusal tuple and the name of the evidence list.
- **Evidence:** the diff shows only renames: `"the artifact server"` vs `"the managed service"`, and `listeners` vs `answers`.
- **Impact:** a change to the unit/contentRoot/startedAfterFiles presence rule, for example B-tracked ownership hardening, has to be made in two places.
- **Fix:** add one `managed_unit_capability(name, subject, refusals, list_key, entry)` factory in module_utils that both plugins call. (effort S)
- **Checked:** the clone data reports 23 and 26 identical lines. The difference is not deliberate.

##### P06-05

**Medium · Clearness / DDD · confidence high**

- **Where:** plugins/module_utils/cluster_objects_engine.py:585-595; internal/addons/clusterobjects/driver/evidence.go:13, 41-46
- **What:** the engine computes `classification` and `absent`, but Go never reads `Classification` (no `.Classification` use under internal/addons); Go re-judges the evidence and uses only `Postcondition`. To make Go see "unknown", the engine also overwrites the first observation's ownership.
- **Evidence:** `if self.unknown and readings:\n            readings[0]["ownership"] = "unknown"\n            readings[0]["equal"] = False`
- **Impact:** the evidence misstates a real object's ownership. A program with no create/place observations loses the unknown signal entirely, and Go then reads `Completed` with `postcondition=false` as `Partial` instead of Unknown. That is two parallel judgements, which the Language table forbids Python to make.
- **Fix:** emit an explicit top-level `unknown` evidence field that Go's EvaluateEvidence reads, and drop `classification`/`absent` from the engine. (effort M)
- **Checked:** Go's EvaluateEvidence (evidence.go:350) re-evaluates everything independently. The adapter protocol spec (architecture.md:1106-1111) allows the postcondition fields but says Go alone judges.

##### P06-06

**Medium · Clearness · confidence high**

- **Where:** plugins/module_utils/cluster_objects_engine.py:274-343, 397-468, 48-64
- **What:** `subscription` (cc 55) and `chain` (cc 61) mix probing, OLM proof rules, pausing, fact assembly and observe/apply branching. Nine mutable instance flags (`unknown`, `unproved`, `not_ready`, `gaps`, `chains`, ...) carry results between methods, and `request["receipts"]` is mutated as shared state (lines 53, 104-105, 137).
- **Evidence:** measured cc 61 / 55 / 29 (`execute`, depth 7).
- **Impact:** the audit 003 defects ADD-01, ADD-02, ADD-11 and ADD-16 all sit in these branches. That density of defects shows the structure resists review.
- **Fix:** split the observe and apply paths, and turn `chain` into a pure function over (plans, head, installed, csv) that returns a chain-or-gap value object. (effort M)
- **Checked:** the 100-line fitness rule is met, so this is about complexity, not length.

##### P06-07

**Low · Quality · confidence medium**

- **Where:** plugins/module_utils/bounded_process.py:80-104, 107-116
- **What:** the exit status is observed only after both pipes reach EOF. A descendant that inherits stdout or stderr and outlives the leader turns a finished command into a `timeout` after the whole budget.
- **Evidence:** `while selector.get_map():` comes before the `os.waitid(...)` loop.
- **Impact:** a slow false refusal. The Go runner has a 5 s / 60 s drain bound for exactly this case; this runner has none.
- **Fix:** also poll `waitid(WNOWAIT)` inside the select loop, and once the leader has exited, drain for a short bound before cleanup. (effort S)
- **Checked:** no test exercises a lingering descendant that holds a pipe after the leader exits. The descendant-kill test covers only the timeout path.

##### P06-08

**Low · Clearness · confidence high**

- **Where:** plugins/action/cluster_objects_read.py:8-36; plugins/action/cluster_objects_write.py:8-36
- **What:** the request copy, the secretSources→materials projection and the `read` closure are duplicated between the two plugins.
- **Evidence:** the clone data reports 25 identical lines; the diff differs only in `execute(observe=...)` and the failure handling.
- **Impact:** a change to the material projection has to be made in both plugins.
- **Fix:** move `_run_program`'s preamble into a module_utils helper that returns (request, read, write). (effort S)
- **Checked:** Ansible needs one plugin file per FQCN, not duplicated bodies.

##### P06-09

**Low · Non-duplication · confidence high**

- **Where:** plugins/module_utils/host_sockets.py:34-53; plugins/module_utils/substrate_libvirt.py:665-688
- **What:** `table_lines`, `hexadecimal` and `table_address` are copied verbatim into substrate_libvirt.
- **Evidence:** `packed = b"".join(int(host[at:at + 8], 16).to_bytes(4, sys.byteorder) ...` appears in both.
- **Impact:** the /proc/net decoding has two owners.
- **Fix:** substrate_libvirt imports host_sockets. (effort S)
- **Checked:** both are module_utils on the same site.

##### P06-10

**Low · Efficiency · confidence medium**

- **Where:** plugins/module_utils/cluster_objects_engine.py:233-244; plugins/action/cluster_objects_write.py:28-31
- **What:** each `checks` poll re-reads every check, including ones already satisfied. Each read is one `_execute_module` (an AnsiballZ process) plus one bounded `oc` process.
- **Evidence:** `results = [self.check(check, offset + index, observe) for index, check in enumerate(checks)]` inside `while True`.
- **Impact:** a program with many waits costs two processes per check every 10 s pause until the deadline.
- **Fix:** cache satisfied, non-stale checks across iterations, or read through one module call per poll. (effort M)
- **Checked:** `snapshot` caching applies only in observe mode.

##### P06-11

**Low · Clearness · confidence high**

- **Where:** plugins/module_utils/controller_channel.py:1-29
- **What:** it is named "controller" channel, but 27 action plugins import its `emit`, including every lifecycle capability, not only the controller stage.
- **Evidence:** `"""Bounded runner-owned controller capability channel."""`
- **Impact:** the name misleads readers about which runner owns descriptors 3 and 4.
- **Fix:** rename it `result_channel.py`. (effort S)

#### Patterns
- Python copies of host-observation helpers instead of one shared module: artifact_server.py vs infra_service.py, host_sockets.py vs substrate_libvirt.py, the CERTIFICATE regex in controller_files.py:38 vs tls_trust.py:14.
- Per-capability protocol plugins mostly reduce to `Capability(...)` plus `ActionModule.run` (machine_power_protocol.py:104-119, managedos_tree_protocol.py:34-49, the infrastructureservices_mirror_* plugins). The shared `adapter_protocol.publish` already removes the real duplication; the remaining boilerplate is what Ansible requires.
- Complexity concentrates in a few god-functions while the rest is small: Engine.chain / subscription / execute, bounded_process.run, controller_files.archive_members (cc 23, depth 7).

#### Strengths
- controller_files.py: descriptor-relative no-follow descent, O_TMPFILE publication proved by digest after linking, identity re-checks around every read, a streamed hash bounded to one chunk, and redirects allowed only to an allowlist.
- infra_service.probe_registry pins the certificate fingerprint before the Basic credential is sent (lines 233-241).
- The censored callback's guarantee is proved end to end: test_censored_output.py runs real ansible-playbook in a child under both callbacks and asserts the value is absent.
- Secret-handling filters (registry_pull_secret, installer_proxy_credentials) raise `from None` with fixed messages; their one caller (containercluster_media_agent/tasks/build.yml:59-95) is `no_log`. All nine filters are free of side effects.
- bounded_process has focused tests for limit, time, cancel, environment, invalid arguments and a kill after failure.

#### Coverage notes
- infra_service.py:171 `probe_http` and :365 `probe_ntp` are 0%: the proxy and NTP readiness proofs, which decide whether a service is reported ready.
- artifact_server.py:110 `probe`, :97 `status_line` and :145 `probe_targets` are 0%. `probe_targets` claims to match "the Go contract exactly", but no test pins that parity.
- private_records.py:16 `read` is 10%: the root-ownership, mode and size refusal of custody files that discovery and erase rely on.
- cluster_objects_engine.py:247 `Engine.dependent` is 65%, including the creation-timestamp refusal branch.
- controller_files.py:923 `Bundle.capacity.walk` is 71%; the depth, entry-count and ownership refusal branches are the part that matters.
- bounded_process: no test covers a reap that times out ([P06-02](#p06-02)) or a descendant holding a pipe after the leader exits ([P06-07](#p06-07)).

#### Already recorded
- ADD-01 (pending upgrade plan makes an exact-policy observation unknown): cluster_objects_engine.py plan/subscription.
- ADD-11 (placement transient failure not retried): cluster_objects_engine.py:176-182.
- ADD-02 (create retry by status reason never fires): cluster_objects_native.py:185-201.
- ADD-16 (printf %q template escaping): cluster_objects_native.py.
- INF-05 (DNS TCP readiness): infra_service.py probe_dns.
- SEC-06 (redirect host allowlist): controller_files.py respond.
- tracked: B1092 (the `tool_files` vocabulary in controller_files.py:993-1046 duplicates the Go catalog with no parity test).
- Backlog line 1383 (`infra_service.foreign()` skips the socket check when a unit of that name is active): an ownership-contract item, not restated.

<a id="lane-p07"></a>

### Lane P07: Ansible modules (Python)
Scope read: read in full: managedos_host_firewall.py, infrastructureservices_mirror_release.py, infrastructureservices_mirror_image.py, containercluster_install_inspect.py (code part), image_acquire.py (code part), managedos_update.py (code part), physical_erase.py, ceph_export.py, ceph_admin_credentials.py, containercluster_admin_credentials.py, hosted_area.py, cluster_objects_read.py, cluster_objects_write.py, containercluster_install_placement.py, containercluster_work_area.py, libvirt_nm_connections.py, libvirt_domain_inspect.py, libvirt_firewall_ready.py, libvirt_host_inspect.py, libvirt_routing.py, libvirt_vlan_refusal.py, machine_hardware_read.py, ssh_key_policy.py, managedos_disk_discovery.py, managedos_tree_inspect.py, controller_tool.py, ceph_replication.py, redfish_firmware.py, redfish_system_inspect.py, redfish_system_read.py / skimmed: managedos_registration.py, managedos_firewall.py, ceph_smb.py, artifact_server_inspect.py, infra_service_inspect.py, containercluster_media_layout.py, substrate_kubevirt_upload.py, the *_protocol.py stubs, tests test_managedos_host_firewall.py (lines 920-1094), test_redfish_boot.py and test_infrastructureservices_mirror_release.py (test names only) / not read: redfish_boot.py, containercluster_media_token.py, lifecycle_scratch.py, private_file.py, guest_agent_read.py and the other mid-size modules. They are 87-98% covered and the clone data did not flag them.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 3 | Refusals are mostly closed and typed, but `changed` is sometimes untruthful, some errors are misclassified, and a dead destroy path ships |
| Security | 3 | Secret arguments carry `no_log` and anchors are lent privately, but the SSH-lockout gate can be bypassed and 11 modules start processes outside the bounded, minimal-environment runner |
| Efficiency | 4 | Reads are bounded; readiness probes run one target after another, and a client up to 1 GiB is read whole into memory |
| Clearness | 3 | Names and rationale are strong, but a few large modules mix three concerns each |
| DDD | 4 | Most mains are thin over module_utils; the registry client and assisted-service client live in the modules |
| Non-duplication | 3 | The flagged clones are mostly DOCUMENTATION blocks, but real logic is copied: SSH-worker interpretation, write_private, proxy route, SIGALRM deadline, inspect loops |
| Right language | 4 | No product decisions found in modules; a host-status classification sits in Python |
| Test coverage | 3 | Tests check behaviour with good names, but the SSH-admission success path, physical_erase main and the credential-bearing mirror reader are unexercised |

#### Findings
##### P07-01

**Medium · Security · confidence medium**

- **Where:** plugins/modules/managedos_host_firewall.py:115-121
- **What:** `offline_ssh_shape` detects a rich rule only as a non-empty line with no colon right after `rich rules:`. A rule containing a colon passes the check, for example an IPv6 address or `log prefix="x:"`. Because `rich` resets after that line, every later rule in the zone also goes unchecked. A reject rule could then override the port grant that `ssh_admitted` proves, and enabling firewalld would lock out the session.
- **Evidence:** `key, separator, value = line.strip().partition(":")` / `if rich and line.strip() and not separator:` / `rich = key == "rich rules"`
- **Impact:** The gate meant to prevent a remote lockout before `systemctl enable --now firewalld` can admit a zone with rules that block SSH.
- **Fix:** Inside a `rich rules` section, treat every indented line up to the next blank or unindented line as a rule, and refuse if there is any (or parse it with the `rule ` prefix the same way `harmless_offline_policy` does). Add a positive and a colon-rule test. (S)
- **Checked:** `harmless_offline_policy` does handle `rule ` lines, but only for policies, not zones. The only SSH-admission test (test_managedos_host_firewall.py:1042) is a stub where firewall-offline-cmd always fails, so no zone parsing is ever exercised. Measured coverage: `ssh_admitted` 4%, `offline_ssh_shape` 14%. No backlog item matched "rich rule"; backlog.md:3980 is unrelated.

##### P07-02

**Medium · Security · confidence high**

- **Where:** plugins/modules/image_acquire.py:172-173, plugins/modules/infrastructureservices_mirror_image.py:249-250, plus the libvirt_* modules, artifact_server_inspect.py:119, infra_service_inspect.py:158 and guest_agent_read.py:142
- **What:** 11 modules start processes through `module.run_command`. It inherits the module's whole environment (only the proxy variables are overlaid), has no time bound and does not bound output. This includes long `podman pull` and `skopeo copy` network transfers. security.md:254-255 requires "a minimal allowlisted environment", and python.md says to start a process through `bounded_process.py`.
- **Evidence:** `return module.run_command(argv, environ_update=route or {})`. By contrast, substrate_kubevirt_upload.py:122 runs `bounded_process.run(command, b"", module.params["seconds"], 1 << 20, environment=environment(module.params))` with a fixed HOME, PATH and trust.
- **Impact:** Ambient `CONTAINERS_REGISTRIES_CONF`, `REGISTRY_AUTH_FILE` or `SSL_CERT_FILE` on the host reach skopeo and podman, and a stalled pull hangs until Ansible's task timeout instead of the request's own deadline.
- **Fix:** Route these through `bounded_process.run` with an explicit environment that holds the route plus a fixed HOME and PATH. Extend test_python_sites.py to flag `run_command` as starting a process. (M)
- **Checked:** test_python_sites.py:95 scans only `subprocess`, `os` and `pty` calls, so `run_command` passes unseen. Searching specs/, knowledge, audit 003 and the backlog for `run_command` gave no hits, so it is not a recorded exception.

##### P07-03

**Medium · Non-duplication · confidence high**

- **Where:** plugins/modules/managedos_update.py:205-322, managedos_registration.py:266-439, managedos_firewall.py:195-306
- **What:** The three SSH fixed-worker modules each re-implement the same functions: `ArgumentError`, `worker_source`, `refusal`, the `parse_answer` framing, the `interpret` transport mapping, the `execute` call to `guest_argv` with seven keyword arguments, and the seven-key SSH argument spec. Only the result and refusal validators differ. The copies have already drifted: on exit code 255, registration returns `refusal("registration-unreachable")` while the other two return `refusal("transport", "unreachable")`.
- **Evidence:** All three have `if outcome.code == 255:` (update:290, registration:411, firewall:276) followed by the same `parse_answer` body (`if not isinstance(stdout, bytes) or b"\n" in stdout.rstrip(b"\n"): return None ...`).
- **Impact:** Every transport-rule change must be made three times, and the drift shows how easily the copies diverge.
- **Fix:** Add a `guest_worker_call` helper to module_utils next to `managedos_guest_ssh`, taking `(params, worker, payload, validate_result, validate_refusal)` and exposing a shared SSH argument-spec fragment. (M)
- **Checked:** The clone-data pairs for these files (for example registration 86-118 / update 55-87) all fall inside the DOCUMENTATION blocks. The duplicated logic is in the code regions cited above, which I opened.

##### P07-04

**Medium · Non-duplication · confidence high**

- **Where:** write_private at image_acquire.py:118, infrastructureservices_mirror_image.py:154 and infrastructureservices_mirror_release.py:350 (plus module_utils/admin_credential_io.py:21 and ceph_native.py:1183); proxy route at image_acquire.py:110, infrastructureservices_mirror_image.py:144 and infrastructureservices_mirror_release.py:462; registry HEAD client at infrastructureservices_mirror_image.py:216 and infrastructureservices_mirror_release.py:469; SIGALRM deadline at containercluster_install_inspect.py:521 and module_utils/controller_files.py:359
- **What:** Security-relevant helpers are copied between modules instead of shared from module_utils.
- **Evidence:** install_inspect.py:525: `This is the alarm controller_files.alarm sets`, a docstring admitting the copy. The two `environment()` functions match line for line except for the mirror bypass.
- **Impact:** A fix to the private-file open flags, the proxy-spelling rule (security.md:260) or the TLS reader has to be found and repeated in up to five places.
- **Fix:** Create module_utils `private_files`, `egress_route` and `registry_v2` (HEAD/GET with anchor, credential and `use_proxy=False`), and make `controller_files.alarm` take an exception factory. (M)
- **Checked:** None of these files is a named exception in test_python_sites.py, apart from managedos_firewall_worker, which is unrelated.

##### P07-05

**Medium · Clearness · confidence high**

- **Where:** plugins/modules/infrastructureservices_mirror_release.py:159-551
- **What:** One 555-line module holds three unrelated modes (`read`, `prepare`, `clean`), each with its own `required_if`. It contains an OCI registry client, an index, layer and tar walk, a vendor client download, a stale-directory sweep, and a process-global `os.environ.update` used to steer `open_url`. The credential-bearing `mirror_reads` and `main` are 0% covered.
- **Evidence:** `os.environ.update(route(egress, mirror))` at line 506. pycov: `mirror_reads` 0/4, `mirror_reads.get` 0/7, `mirror_reads.head` 0/6, `main` 0/22.
- **Impact:** The code that sends the mirror password over TLS is the part no test reaches. The environment mutation hides the proxy decision from readers, and the next mode added will grow the module further.
- **Fix:** Move the registry and release reader into module_utils (shared with [P07-04](#p07-04)), pass the route to an opener with an explicit `ProxyHandler` instead of mutating `os.environ`, and split prepare/clean from read. (M)
- **Checked:** The tests cover `held`, `prepare`, `clean` and `route` through injected fakes (test lines 77-249), so the logic is tested and only the I/O wiring is not.

##### P07-06

**Low · Quality · confidence high**

- **Where:** plugins/modules/managedos_host_firewall.py:94,115,129,139,157 and 227-230
- **What:** Each `decode("ascii")` or `decode("utf-8")` can raise `UnicodeDecodeError`, which is a `ValueError`. `main` catches that as "invalid host firewall request". If it happens after `prepare` has run `systemctl enable --now`, the module reports a request error and drops `changed=True`.
- **Evidence:** `except ValueError: module.fail_json(msg="invalid host firewall request")`
- **Impact:** A tooling fault is misreported as a malformed request, and the record of an effect already made is lost.
- **Fix:** Decode through `worker.token` or catch `UnicodeError` and convert it to `worker.failed("ssh-admission")` or `failed("tooling")`. (S)
- **Checked:** `worker.token` (managedos_firewall_worker.py:231) already maps `UnicodeError` to `failed("tooling")`, but these call sites do not use it.

##### P07-07

**Low · Quality · confidence medium**

- **Where:** plugins/modules/ceph_admin_credentials.py:79, plugins/modules/containercluster_admin_credentials.py:76
- **What:** `update` always reports `changed=True`. The native update returns `{"proved": True}` without writing when the material already matches (module_utils/ceph_admin_credentials_native.py:55-56), so a replay still claims a change. Both modules also use `except Exception` with one generic message, which loses the cause.
- **Evidence:** `module.exit_json(changed=params["operation"] == "update", proved=True)`
- **Impact:** ansible.md requires a truthful `changed`, and Go's outcome for a replay reads as changed.
- **Fix:** Return `changed` from the native call and pass it through. (S)
- **Checked:** `prepare` returns `changed=False` correctly; only a local private file is written.

##### P07-08

**Low · Quality · confidence medium**

- **Where:** plugins/modules/ceph_replication.py:73,83; roles/storage_ceph_replication/tasks/destroy.yml:16-19; cmd/bootwright/wiring_reconciliation.go:208
- **What:** The argument spec admits `destroy`, but `admitted()` and module_utils/ceph_replication.py:41 refuse anything except `("apply", "observe")`. The Go `ReplicationCapability.Destroy` (internal/storage/ceph/replication.go:331) builds retirement evidence without the runner. The destroy playbook and role are therefore wired but dead, and would always refuse with `request`.
- **Evidence:** `choices: ["apply", "observe", "destroy"]` versus `params["operation"] in ("apply", "observe")`
- **Impact:** Misleading surface: the module looks like it supports an operation it always fails.
- **Fix:** Drop `destroy` from the choices and remove replication_destroy.yml, destroy.yml and the wiring entry. Alternatively, record an item if a native removal is intended. (S)
- **Checked:** SMB destroy is implemented in ceph_core.py:1293, so that case differs. I did not confirm whether Go's SMB `retire` path reaches its playbook.

##### P07-09

**Low · Non-duplication · confidence high**

- **Where:** plugins/modules/artifact_server_inspect.py:104-166, plugins/modules/infra_service_inspect.py:142-205
- **What:** Both mains repeat the same flow: observe, then bind_address, then foreign, then a readiness retry loop with `time.sleep(RETRY_DELAY)`, then the failure sort. Targets are probed one after another, each with up to `attempts` (default 30) sleeps.
- **Evidence:** Both have `for remaining in range(attempts): ... if remaining + 1 < attempts: time.sleep(RETRY_DELAY)`.
- **Impact:** Duplicated policy, and the worst-case time grows with targets × attempts.
- **Fix:** Move a `readiness(targets, probe, attempts)` helper to module_utils, and consider one shared deadline across all targets. (S)
- **Checked:** The coverage gaps are small (7-18 statements), so this is not a correctness issue.

##### P07-10

**Low · Efficiency · confidence high**

- **Where:** plugins/modules/infrastructureservices_mirror_release.py:389, 366, 374-378
- **What:** The `oc` member (bounded only by `MAX_CLIENT`, 1 GiB) is read whole into memory before it is written. The checksum and client responses are never closed. A `Refused` raised at line 378 sits outside the `try/finally` that unlinks `client.tar.gz`, so the partial archive stays behind.
- **Evidence:** `write_private(os.path.join(binary, "oc"), opened.extractfile(member).read(), 0o700)`
- **Impact:** Peak memory equals the binary's size on the mirror host, and a partial archive is left until `clean` runs.
- **Fix:** Copy the member in chunks, wrap both responses in `with`, and start the `try` before the download loop. (S)
- **Checked:** The role's `clean` mode empties the scratch directory later, so the leftover is not lasting.

#### Patterns
- Duplication: the measured "duplicated module lines" are mostly DOCUMENTATION/RETURN blocks. The Redfish clones (redfish_system_inspect 26-59 against redfish_system_read 33-66 and redfish_firmware 48-80) and the Ceph clones (ceph_cluster against ceph_export, ceph_replication against ceph_smb) are all inside the doc strings. Redfish code duplication is limited to the six-option connection spec and the `Client(...)` construction (redfish_firmware.py:138-152, redfish_system_inspect.py:104-121, redfish_system_read.py:113-125).
- Two process models coexist: `module.run_command` (11 modules) and `bounded_process` with an explicit environment (substrate_kubevirt_upload.py:122), and only the latter meets security.md's process boundary. See [P07-02](#p07-02).
- Dead statements: `return` after `exit_json`/`fail_json` appears in cluster_objects_read.py:44, libvirt_*.py and containercluster_install_placement.py:90,94,97. cluster_objects_write.py:42-44 tests `module.check_mode` even though it declares `supports_check_mode=False`.
- Some request keys are read outside the `try`, so a malformed request produces a traceback instead of a closed failure: managedos_tree_inspect.py:38 `path = request["publication"]["path"]` (only `OSError` is caught); containercluster_install_inspect.py:631 parses the body a second time.

#### Strengths
- `no_log` is on every secret argument checked (material, password, auth, trust, kubeconfig paths, authorization), and credentials reach tools only as 0600 files in private scratch that is removed in `finally` (infrastructureservices_mirror_image.py:160-212).
- `supports_check_mode=True` is claimed only by the 16 read-only modules, and containercluster_install_inspect.py honours `check_mode` on its two writes.
- Bounds are explicit: bounded reads, tar header limits (`BoundedTarInfo`), hashing streams with deadlines, closed refusal reasons.
- The 3.9 floor holds: every module has `from __future__ import annotations`, and none uses `match`, `zip(strict=)` or `removeprefix`.
- The 26 *_protocol.py files and controller_tool.py are confirmed documentation-only entry points.

#### Coverage notes
- managedos_host_firewall.py:151 `ssh_admitted` (4%) and :86 `harmless_offline_policy` (0%): the success path that allows a remote firewall start is never exercised. The only test (test file :1042) fails at the first command.
- physical_erase.py:65-131 `main` is 0%. It holds the destructive-path phase dispatch and the `completion-drift` comparison of stored and computed evidence, which is real logic, not a thin wrapper. `await` reports `changed=True` unconditionally (line 88).
- ceph_export.py:85-131 is 0% and holds real logic: the material key-set rule, private output written before evidence assembly, `json.loads(reply["private"])` entries and the StopIteration on `next(...)`.
- infrastructureservices_mirror_release.py:469-511 and 514-551: the credential-bearing TLS reader and `publisher_fetch` are 0%.
- containercluster_admin_credentials.py and ceph_admin_credentials.py mains are 0%: the operation-to-`changed` mapping ([P07-07](#p07-07)) is untested.
- Thin 0% mains whose logic lives in tested module_utils: cluster_objects_read/write, hosted_area, containercluster_install_placement, containercluster_work_area, libvirt_* (5), machine_hardware_read, ssh_key_policy, managedos_disk_discovery, managedos_tree_inspect, controller_tool. Low value to cover.

#### Already recorded
- None of [P07-01](#p07-01) to [P07-10](#p07-10) matched audit 003 or an open B item. Searching backlog.md for rich rule, run_command, write_private, alarm and host_firewall found only unrelated mentions (backlog.md:3980, 8544).

<a id="lane-pt1"></a>

### Lane PT1: The collection's test design
Scope read: conftest.py, install_phase_tasks.py, runtime_closure.py, test_role_task_order.py, the integration targets (5), sanity/README.md and ignore-2.21.txt, scripts/tools/ansible_check.py lines 22-150, 290-730 and 729-1060, ansible-check.md lines 100-135 / skimmed: test_role_boundaries.py, test_role_structure.py, test_role_templates.py, test_python_sites.py, test_function_length.py, test_playbook_entry_points.py, test_managedos_install_network.py, test_managedos_install_remnants.py, test_managedos_package_tree.py, test_managedos_install_update.py, test_managed_service_replay.py, test_baremetal_replay.py, plugins/modules/test_redfish_boot.py, plugins/action/test_every_action_runs.py, plugins/module_utils/test_bounded_process.py, test_ceph_registry_discovery.py, test_controller_supervisor_native.py, ansible_check_test.py (test names), and the knowledge page inner-loop-check-cost.md / not read: most of the 163 plugin test files, test_ceph_core.py beyond counts, and the goldens (sampled by name only).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | structural rules prove themselves with seeded violations, but one rule scans only top-level tasks and one test uses a signal pattern the project has already banned |
| Security | N/A | |
| Efficiency | 3 | the gate's longest lane (about 300 s) is a serial rerun of all 11k tests, needed for only a handful of fork and signal tests |
| Clearness | 4 | docstrings state each test's reason and failures name the remedy; the harness `main` mixes phases |
| DDD | N/A | |
| Non-duplication | 2 | about 18 hand-written partial Ansible task interpreters and 39 block walkers, each with different semantics |
| Right language | N/A | |
| Test coverage | 3 | plugin logic and the structural rules are well covered; roles are never run through ansible-core past argument validation, and module `main()` has no stubbed-run test |

#### Findings
##### PT1-01

**High · Test coverage · confidence high**

- **Where:** tests/unit/test_playbook_entry_points.py:258-270, tests/unit/test_managedos_install_network.py:467-552, ansible_check.py:575-582
- **What:** No role entry point runs past argument validation through ansible-core in the gate. Role behaviour is "executed" only by test-local interpreters that template `when` and `set_fact` with a real Templar over hand-built scopes and fake every module result. The gate's three integration targets run one module (`controller_tool`), one refusal path (`discovery_contract`) and a supervisor. The two that drive a shipped playbook are operator-only.
- **Evidence:** `assert len(ran) == 2 and ran[1] == "Report the refusal"`; `names = ["controller_tool", "controller_supervisor", "discovery_contract"]`; Guest.step fakes `self.scope["managedos_install_anaconda_guest"] = self.answers[command].pop(0)`.
- **Impact:** defects that only real execution shows can still reach an owner's live run, for example:
- variable precedence (role vars vs include `vars` vs `set_fact`)
- the shape of a real module's registered result
- `loop`, `until` and `retries` interplay
- `include_tasks` scoping
- a filter that fails only on real data
- **Fix:** add one integration target per role family that runs an entry point with `ansible_connection: local` against a tmp root and stubbed executables on PATH, twice, asserting a published `completed` record and no change on replay (effort L).
- **Checked:** test_baremetal_replay.py and test_censored_output.py do run real tasks, but only publication and callback tasks. ansible.md "Tests and gates" requires each entry point's first execution and replay to be tested with explicit isolated inventory.

##### PT1-02

**Medium · Non-duplication · confidence high**

- **Where:** test_managedos_install_network.py:510, test_managedos_install_remnants.py:123-125, test_managedos_package_tree.py:101
- **What:** Each role test file rewrites its own task walker and interpreter, and they disagree on Ansible semantics:
- Network checks a block's `when` once, then runs its children with no inherited condition, and ignores `rescue` and `always`.
- Remnants carries the condition down and runs `always`, but asserts that no block has a `rescue`.
- Package-tree drops the inherited conditions and skips every `uri` task.
- **Evidence:** `self.perform(task["block"], [])`, `reached = self.perform(task["block"], conditions, through)`, `self.tasks(task["block"])`. 39 files repeat `for section in ("block", "rescue", "always")`, and 54 build their own `LOADER = DataLoader()`. Exact-clone share is only 1.1% because every copy differs slightly, and those differences are the risk.
- **Impact:** a role edit that relies on Ansible re-checking a block's `when` per task can pass one model and fail on a host, and each new role test pays to write the walker again.
- **Fix:** move one walker and interpreter into a shared helper beside install_phase_tasks.py and runtime_closure.py. It should implement inherited `when`, rescue/always, register-on-skip and `failed_when`, with self-tests against ansible-core on a small fixture play (effort M).
- **Checked:** the brief's deliberate repetition covers Go golden helpers and consumer-owned ports, not Python test interpreters. The two existing shared helpers show a shared module is accepted practice here.

##### PT1-03

**Medium · Test coverage · confidence high**

- **Where:** plugins/modules/physical_erase.py:99-113, plugins/modules/ceph_admin_credentials.py:62-79, plugins/action/test_every_action_runs.py:1-10
- **What:** Action plugins have a "drive every `run()` with what surrounds it stubbed" test; modules have none. 48 of 77 module files run 0% in process (541 statements). `physical_erase.main` holds real policy that no test reaches in any interpreter:
- the `observe` completion-drift checks over `completed.json` and `finalizing.json`, which duplicate `physical_erase_controller.finish` at lines 360-372
- the `FileNotFoundError` "prepared: False" observation
- **Evidence:** `if stored != evidence: raise worker.Refusal("completion-drift")`. A grep of tests/ for `physical_erase`, `ceph_export`, `ceph_admin_credentials` or `containercluster_admin_credentials` as a module finds nothing.
- **Impact:** a parameter read by the wrong key, or a wrong drift verdict in an erase observation, shows up only on hardware.
- **Fix:** add test_every_module_runs.py that patches `AnsibleModule` (or uses `set_module_args`) per module, and move the module-side drift checks into the controller (effort M).
- **Checked:** censored.py (0%) is run in a child by test_censored_output.py, so it is covered. controller_tool and managedos_disk_discovery run in integration targets. The 4-statement `*_protocol` shims carry no logic.

##### PT1-04

**Medium · Quality · confidence high**

- **Where:** tests/unit/test_role_task_order.py:28-34, 68-89
- **What:** The rule "every role variable is registered before the task that reads it" loads only each file's top-level tasks. A `register` inside a block, rescue or always is never in `later`, so a read of it from an earlier task is not detected. Registers set in included files are not considered either.
- **Evidence:** `return [task for task in parsed if isinstance(task, dict)]` and `registered_names(tasks)` over that list. Measured: 43 of 320 registers in role task files sit inside blocks.
- **Impact:** the rule passes vacuously for about 13% of registers, exactly in the nested files where reordering happens.
- **Fix:** walk nested sections in order (reusing the [PT1-02](#pt1-02) walker) and treat `set_fact` names the same way (effort S).
- **Checked:** no other structural test enforces register ordering. The file also holds an unrelated disk-proof test (lines 49-65).

##### PT1-05

**Medium · Quality · confidence medium**

- **Where:** tests/unit/plugins/module_utils/test_ceph_registry_discovery.py:592-597
- **What:** The test sends itself a process-directed signal, and it runs under xdist in the gate's units lane. The project's own knowledge page names this as the cause of intermittent parallel-only failures and prescribes `signal.pthread_kill`.
- **Evidence:** `os.kill(os.getpid(), signal_number)`. The knowledge page (inner-loop-check-cost.md) says: "A test that signals itself with `os.kill(os.getpid(), signal.SIGALRM)` then sees its handler run early or late and fails in parallel only".
- **Impact:** a likely source of FLAKY exits (75) and of red full gates that the plain lane would pass.
- **Fix:** signal the calling thread with `signal.pthread_kill(threading.get_ident(), signal_number)`, as B866's fix did (effort S).
- **Checked:** the other `os.kill(os.getpid())` hits (test_ceph_native_worker.py:304, test_ceph_packages.py:720, test_ceph_command.py:78) run inside child scripts and are safe. No backlog item names this test.

##### PT1-06

**Medium · Efficiency · confidence medium · policy trade-off**

- **Where:** scripts/tools/ansible_check.py:599-627, 667-669
- **What:** The plain lane reruns the whole unit tree serially in process on every full gate. Its stated purpose is to settle tests that fork or signal, which an execnet worker thread can disturb. That is a small, nameable set.
- **Evidence:** `run(pytest + [str(test_collection.relative_to(collections) / "tests/unit")], ...)`. The knowledge page measures this lane at 238-300 s, the longest lane, against 94 s for the xdist units.
- **Impact:** it roughly triples the gate's critical path for the sake of a few dozen tests.
- **Fix:** mark the fork and signal tests (`@pytest.mark.serial`, enforced by a scan for `os.fork`, `os.kill`, `signal.*` and `setitimer` in test bodies) and run only those serially (effort M).
- **Checked:** the knowledge page explains why a serial in-process run exists, but not why it must cover every test. Keeping the conftest loader path exercised needs only one plain-pytest test.

##### PT1-07

**Low · Quality · confidence medium**

- **Where:** plugins/module_utils/test_bounded_process.py:57-59, 90; test_cluster_objects.py:434
- **What:** Several tests assert wall-clock bounds or use fixed subprocess timeouts while the gate runs lanes side by side.
- **Evidence:** `assert refused.value.kind == "timeout" and time.monotonic() - started < 8`; `timeout=30`.
- **Impact:** failures under load that pass when run alone.
- **Fix:** assert the outcome kind, and keep bounds only as a generous ceiling derived from one constant (effort S).
- **Checked:** tracked for the cluster-objects case as B1345.

##### PT1-08

**Low · Clearness · confidence high**

- **Where:** scripts/tools/ansible_check.py:927-1053
- **What:** The launcher's `main` is 127 lines with complexity 17. It mixes the interpreter pin, the requirements-lock check, environment and inventory building, playbook narrowing, the harness self-test and the pass record. The 100-line rule covers only the collection's plugins.
- **Evidence:** from pymetrics-prod.tsv: `ansible_check.py 927 main 127 17`.
- **Impact:** the gate's entry point is the hardest function to change safely.
- **Fix:** extract `require_locked_requirements()`, `check_environment()` and `selected_playbooks()`, and extend `test_function_length`'s scope to scripts/tools (effort S).
- **Checked:** architecture.md line 1379 limits the rule to "Go or ... the collection's plugins".

#### Patterns
- Structural rules are well built: exact allowlists that fail when an entry goes stale (EXECUTABLES and WRAPPED in test_role_boundaries.py:82-112, SCRATCH_ALLOWED at :997, AWAITING_SPLIT) and "fewer found means the walk went blind" counts (ENTRY_POINTS = 33, UNITS, POLLS). Failures name the file, the task and the remedy.
- Rule boundaries stop at the local argv and at Python. The executable rule checks only argv[0], so the guest scripts `ssh … sudo -n /usr/bin/sh -c` (managedos_install_anaconda/defaults/main.yml:135-222, tasks/os_release.yml:9) are invisible to "never through a shell". The single-quoting-owner rule (test_python_sites.py:257) does not see YAML quoting via `map("quote")` (guest.yml:95).
- Tests identify tasks by their `name` text and pin exact message substrings (test_managedos_install_network.py:91-93 and 222-236), so renaming a task breaks the tests even when behaviour is unchanged.
- "Renders the same bytes twice" (test_role_templates.py:221) runs both renders in one process, so it cannot detect order that changes between runs (set or hash ordering under PYTHONHASHSEED); the golden partly compensates.

#### Strengths
- Rule tests prove their own rules with seeded violations: `test_the_*_rule_rejects` across test_role_boundaries.py and test_role_structure.py, and `test_the_golden_comparison_sees_a_change…`.
- The real-boundary fakes are stateful: test_redfish_boot.py's `Firmware` answers at urllib's `opener.open`, so every test goes through the client's single request path.
- `once_per_run` (conftest.py:40-71) shares expensive playbook runs across xdist workers under a lock, with an atomic publish.
- runtime_closure.py confines child playbook runs to the locked runtime distributions and asserts that the confinement fired (`"cryptography" in refused(record)`).
- The harness is well tested: 39 tests cover lanes, FLAKY, the pass key and the recorded gate. The pass key covers every input the tests read (no test reaches above `ansible/`).

#### Coverage notes
- Question 4 (replay): replay is proved per role only through hand-built models (test_managed_service_replay.py:233, test_substrate_libvirt_replay.py, test_baremetal_replay.py). No gate test applies any entry point twice through ansible-core.
- physical_erase.py:99-113: the observe drift and finalizing branches are untested anywhere ([PT1-03](#pt1-03)).
- Question 7 (sanity): ignore-2.21.txt has 64 entries, all `validate-modules:missing-gplv3-license`. Sanity runs pylint 4.0.5, pycodestyle, validate-modules and yamllint, plus the import test under 3.9. There is no type checker, so the many request-dict shapes are checked only by tests.
- Of the module files that do run, managedos_host_firewall.py is at 45.1% and ceph_replication.py at 42.4% in process.
- discovery_contract and controller_tool are the only gate targets that run a module through ansible-core end to end, and neither replays.
- No integration target has an `aliases` file, so an `ansible-test integration` run without names runs nothing: targets are chosen only by the hardcoded `names` list at ansible_check.py:575.

#### Already recorded
- B1345: cluster-objects real-playbook handoff, 30 s timeout under load ([PT1-07](#pt1-07)).
- B866 (delivered, X186): the pthread_kill fix pattern; test_ceph_registry_discovery.py:597 was not converted ([PT1-05](#pt1-05)).
- inner-loop-check-cost.md, "pytest-xdist is no gate": explains why the serial plain lane exists ([PT1-06](#pt1-06) is reported only as a policy trade-off).
- B967: scripts/quick-test and check-commits launcher exceptions (context for [PT1-08](#pt1-08), not the same defect).

<a id="lane-y01"></a>

### Lane Y01: Roles for the managed OS, physical erase and machine operations (Ansible YAML)

Scope read: I read in full every task file of managedos_install_anaconda except update_observe.yml, registration_observe.yml and unregister.yml, which I only skimmed. I also read in full its defaults and the managedos_host_firewall apply/destroy/observe/session/provided_apply/provided_destroy/installed_apply files with their defaults. The same goes for managedos_package_tree (all tasks and defaults), managedos_disk_discovery (all), physical_erase (all), machine_ssh_identity, machine_hardware, machine_power, machine_power_read and the sample playbooks (install_apply, erase_destroy, machine/power, discovery_apply). Skimmed: the firewall installed_destroy/installed_observe/provided_observe files; the other playbooks, which differ only in names; and the tests test_managedos_install_network.py, test_machine_power_pin.py, test_listener_fetch_retries.py, test_managedos_install_root_wwn.py and the handoff rule in test_role_boundaries.py. Not read: meta/argument_specs.yml files in depth, because B431 already tracks the undeclared nested mappings.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | Careful failure classification and replay reasoning; a few entry points are inconsistent (physical_erase, disk discovery) |
| Security | 4 | Strict host-key pinning, token redaction and no_log discipline; the listener-proof options and the YAML guest SSH bounds have drifted |
| Efficiency | 4 | Mostly bounded; the SSH policy is re-read and session files rewritten for every guest command, and power readings run one by one |
| Clearness | 3 | Very good rationale comments, but apply.yml (897 lines) and network.yml put a lot of decision Jinja inline |
| DDD | 3 | Requirement rules, time budgets and operator diagnostics live in role YAML instead of their Go owners |
| Non-duplication | 2 | The SSH session setup has 10 copies, the listener proof 6, the controller port map 5, and entry validation is repeated per operation |
| Right language | 2 | Postcondition-requirement rules and wrong-target identity rules are written in Jinja; guest shell programs are embedded in YAML |
| Test coverage | 4 | Broad Templar-based tests of conditions and structure; gaps where copies diverge (proxy/redirect options, diagnostic map totality) |

#### Findings

##### Y01-01

**High · Non-duplication · confidence high**

- **Where:** roles/managedos_install_anaconda/tasks/guest.yml:11-118, reachable.yml:10-121, roles/machine_ssh_identity/tasks/main.yml:10-140, register.yml:13-40, update_session.yml (first 70 lines), roles/managedos_host_firewall/tasks/session.yml (first 70 lines); plugins/module_utils/managedos_guest_ssh.py:22-44,114-147
- **What:** The pinned guest SSH session (policy read, known_hosts, ssh_config, host-key algorithms, cleanup) is written out in 10 task files. Three of them also restate the 21-option argv that `managedos_guest_ssh.OPTIONS`/`guest_argv` already owns in Python. The YAML copies lack what the Python builder enforces: path/destination validation, a size bound on the remote command, bounded output, a clean environment and a timeout.
- **Evidence:** `- GlobalKnownHostsFile=none` ... `- KnownHostsCommand=none` repeated in guest.yml:48-91, reachable.yml:53-96 and main.yml:55-98. The Python builder has `OPTIONS = ("GlobalKnownHostsFile=none", "KnownHostsCommand=none", ...)` and its runner calls `runner(argv, data, timeout, MAX_OUTPUT + MAX_DIAGNOSTIC, environment=dict(ENVIRONMENT)...)`. The guest.yml command task has no `timeout:` keyword, and its argv ends `'{{ managedos_install_anaconda_guest_command | map("quote") | join(" ") }}'`.
- **Impact:** A hardening change has to be made in 3 argv lists and 10 preambles. The machine_ssh_identity copy is not covered by the equality test. A hung remote command (for example `nmstatectl show`) with a live peer is bounded only by the run deadline, so the failure is not named at its own step.
- **Fix:** Add one `managedos_guest_command` module built on `guest_argv` + `run` (stdin, bounded output, timeout), and one shared session-preparation task file, or have the module write the per-connection known_hosts and config itself. (effort M)
- **Checked:** `test_guest_commands_pin_the_host_key` (test_managedos_install_network.py:256) keeps only guest.yml and reachable.yml equal, which confirms the duplication is known but not removed. Architecture says SSH remote commands are quoted only in remote_command.py, but the YAML uses `map("quote")`. managed-os.md:795-802 requires "exactly the options of the reachability proof".

##### Y01-02

**Medium · Right language · confidence high**

- **Where:** managedos_install_anaconda/tasks/apply.yml:888-892, observe.yml:199-203, root_wwn.yml:9-13; apply.yml:677,730,883, observe.yml:194, host_security.yml:8; internal/managedos/installation/evidence.go:431-434; plugins/action/managedos_install_protocol.py:220-231
- **What:** Which proofs a completion requires (`rootWWNRequired`, `fipsRequired`, `rootEncryptionRequired`, `registrationRequired`) is computed in Jinja. The root-WWN rule appears three times in YAML and duplicates Go's `Request.serialRoot()`. `'fips=1' in installerArguments` appears five times. The Python protocol plugin then judges `postcondition` from these flags, and the Language table makes postcondition judgment Go's job.
- **Evidence:** `rootWWNRequired: >- {{ (…target.physical…) and (…serialNumber… | length > 0) and (…rootDevice… | length == 0) and …rootVolumeRef is not defined }}` is mirrored in Go by `return r.Target.Physical && hardware != nil && hardware.SerialNumber != "" && hardware.RootDevice == "" && hardware.RootVolumeRef == nil`.
- **Impact:** If Go's rule and the Jinja copies drift, the role silently weakens or strengthens the postcondition it reports.
- **Fix:** Go freezes a `requires` block (fips, rootEncryption, rootWWN, registration, update) in the request, the roles pass it through, and Go judges the evidence. (effort M)
- **Checked:** test_managedos_install_root_wwn.py:76-84 checks that the three YAML copies agree with each other, but not against Go. The protocol's `postcondition` is in my lane only as a consumer; the P lanes own it.

##### Y01-03

**Medium · Security · confidence medium**

- **Where:** physical_erase/tasks/boot.yml:48-67; managedos_install_anaconda/tasks/apply.yml:299-440, private_recovery.yml:39-84; managedos_package_tree/tasks/apply.yml:190-210
- **What:** The "fetch the first byte through the listener" proof exists in six copies with diverging options. physical_erase's copy, which fetches a private token URL, omits `use_proxy: false` and `follow_redirects: none`. It also fails inside `no_log`, with no separate step to judge the result, so the operator gets a censored failure with no cause.
- **Evidence:** apply.yml:309-310 has `use_proxy: false` / `follow_redirects: none`. boot.yml:48-67 has `validate_certs: true`, `ca_path: …`, `no_log: true` and neither option.
- **Impact:** The private erase image can be fetched through an ambient proxy or across a redirect, which the redfish-physical-bmc.md "Proxy and TLS" knowledge names as a trap, and an operator cannot see why a listener refusal happened.
- **Fix:** Use one listener-proof task file or an action that returns a classified cause (403/404/certificate/no-status), included by all six callers. (effort M)
- **Checked:** test_listener_fetch_retries.py:29-33 covers only 3 fetches and asserts retries, timeout and validate_certs, not the proxy or redirect options. Whether the runner scrubs the proxy environment was not proven here.

##### Y01-04

**Medium · Non-duplication · confidence high**

- **Where:** managedos_install_anaconda/tasks/boot.yml:21-35, 95-103; await.yml:77-85; reboot.yml:66-74; proved_boot.yml:44-52; defaults/main.yml:262-269
- **What:** The controller access map (endpoint, verify, ca, user, password, optional `verification`) is rebuilt inline five times, although `managedos_install_anaconda_port_access` already holds it and other callers use `port_access | combine({...})`.
- **Evidence:** `{'endpoint': (managedos_install_anaconda_endpoint), 'verify': …, 'password': (bootwright_os_install_material.controllerPassword), 'power_on': (True)} | combine({'verification': …})` appears in three files.
- **Impact:** A new trust field added to `port_access` would silently be missed by the boot paths.
- **Fix:** `managedos_install_anaconda_port_access | combine({'power_on': true})` and similar for each caller. (effort S)
- **Checked:** No test requires the inline form; test_role_structure only checks that pre_boot precedes insert.

##### Y01-05

**Medium · Right language · confidence high**

- **Where:** machine_power/defaults/main.yml:22-53; substrate_baremetal_machine/tasks/target_proof.yml:93-103
- **What:** The wrong-target safety rules (identity pin UUID trim/lower and serial trim, controller-host UUID/MAC refusal, exact declared MAC set) are Jinja expressions, and the pin comparison is written a second time in the baremetal substrate.
- **Evidence:** `(machine_power_identity.observation.uuid | default('') | trim | lower) == (…pinnedUUIDBase64 | b64decode | trim | lower)` is mirrored verbatim in target_proof.yml:97-99.
- **Impact:** A normalization fix to one copy can leave the other copy accepting the wrong machine for a power-off or erase.
- **Fix:** Add one Python filter (for example `machine_identity_verdict(observation, pins, host)`), unit-tested and used by both roles. (effort S)
- **Checked:** test_machine_power_pin.py:190-345 exercises these conditions through Templar. MACs are canonicalized in redfish_discovery.reported_macs, so I found no case-sensitivity defect today.

##### Y01-06

**Medium · DDD · confidence medium**

- **Where:** managedos_install_anaconda/tasks/register.yml:63-66, update_session.yml:57, unregister.yml:46; managedos_host_firewall/tasks/session.yml:54; internal/managedos/installation/catalog.go:110-115
- **What:** Worker session timeouts (630/750 +60 +60, 1800/120, 330, 300) are chosen in role YAML, and Go's run deadlines are sized by comments that restate those numbers. This contradicts the role's own defaults/main.yml:120-124: "budgets the request froze, never values of this role … a wait this role chose could outlast the deadline".
- **Evidence:** `timeout: {{ (750 if 'update' in … else 630) + (60 if 'satellite' …) + (60 if …contentView …) }}` (up to 870 s) against the Go comment `registrationBound covers the worker's 630 s session, up to 750 s with Satellite content selection`.
- **Impact:** The Go comment is already stale, and nothing ties the two, so a raised timeout could exceed the run deadline and be killed as a hang.
- **Fix:** Freeze session timeouts in `request.budgets` from the Go constants. (effort S)
- **Checked:** test_managedos_install_budgets.py exists but I did not see it bind these literals to Go.

##### Y01-07

**Medium · Right language · confidence medium (policy trade-off)**

- **Where:** managedos_install_anaconda/defaults/main.yml:135-240; root_encryption.yml:15-28; os_release.yml:9-18; host_security.yml:13-20
- **What:** Guest-side shell programs (awk pipelines that decide crypt/tpm2, os-release parsing, nmstate checkpoint scraping) live as `sh -c` strings in YAML. The Language table says YAML "never … runs a command through a shell", while managed-os.md:802 sanctions this form with positional parameters.
- **Evidence:** `if [ -n "$luks" ] && clevis luks list -d "$luks" … | awk -F': ' '$2 ~ /^tpm2 / {found=1} …'`
- **Impact:** The parsing logic cannot be unit-tested as code (tests only match strings), and it is a third pattern next to the shipped Python guest workers (registration, update, firewall).
- **Fix:** Move these reads into a fixed shipped guest worker that uses `managedos_guest_ssh`, as the registration and update workers already do. (effort M)
- **Checked:** The spec sanctions the positional-parameter form, so this is a trade-off rather than a defect, and inputs are injection-safe.

##### Y01-08

**Medium · Clearness · confidence high**

- **Where:** managedos_disk_discovery/tasks/result.yml:18-111; plugins/module_utils/disk_discovery_runtime.py:35-98
- **What:** About 90 operator diagnostics live in a YAML map, while the code vocabulary lives in Python and is partly generated (`"native-" + error.tool`, `"bmc-" + cause`, `media-http-NNN`). No test checks that every code the runtime can emit has a message, and the fallback text is wrong for a code that is real but unmapped.
- **Evidence:** `managedos_disk_discovery_failures.get(… , 'the adapter returned no bounded result')`; the firewall defaults say diagnostics are "Go owns (internal/managedos/installation)".
- **Impact:** A new code degrades to a misleading message, and diagnostics ownership differs from sibling capabilities.
- **Fix:** Move the map next to the codes (Python or Go) and add a totality test. (effort S)
- **Checked:** grep finds no test that references result.yml or the map.

##### Y01-09

**Low · Non-duplication · confidence high (policy trade-off)**

- **Where:** managedos_host_firewall/tasks/apply.yml:2-22 (and destroy/observe), machine_hardware/tasks/*.yml:2-12, managedos_package_tree/tasks/{apply,destroy,observe}.yml, managedos_disk_discovery/tasks/{apply,destroy,observe}.yml
- **What:** Entry-point validation is copied verbatim for each operation, and the three disk-discovery entry points differ only in `operation:` and `observed:`. `handoff_problems` in test_role_boundaries.py:280-296 forces this, because it allows only inline asserts before `loaded`.
- **Impact:** Each request-shape change touches three files, and the copies can drift.
- **Fix:** Let the rule accept an `import_tasks` of an asserts-only `validate.yml`. (effort S)
- **Checked:** The rule's text confirms it is the cause.

##### Y01-10

**Low · Quality · confidence high**

- **Where:** physical_erase/tasks/destroy.yml:2-18, observe.yml:1-22; managedos_disk_discovery/tasks/observe.yml:32
- **What:** physical_erase hands `loaded` before its assert, and its observe validates nothing, unlike every other entry point. Disk discovery observe and destroy publish `outcome: changed`, which Go ignores (discovery.go:388-419 sets its own outcome).
- **Impact:** The handoff record is made for unvalidated input, and a dead, misleading argument is passed.
- **Fix:** Validate first; drop or correct the `outcome` argument. (effort S)
- **Checked:** The handoff test permits asserts after `loaded`, so it does not catch this.

##### Y01-11

**Low · Clearness · confidence high**

- **Where:** managedos_package_tree/tasks/apply.yml:8-21, defaults/main.yml:5,8,22; managedos_install_anaconda/defaults/main.yml:25-28; physical_erase/tasks/boot.yml:41
- **What:** Path structure is decoded by component index (`split('/')[5:7]`, `[:8]`, `length == 12`) and by `dirname | dirname | dirname` in three roles.
- **Impact:** These are magic numbers that break silently if the hosted layout changes.
- **Fix:** Go freezes `servedRoot`/`serverRoot`/`area` in each request. (effort S)

##### Y01-12

**Low · Efficiency · confidence medium**

- **Where:** machine_power_read/tasks/read.yml:35-40, read_one.yml:1-27; guest.yml:11-37
- **What:** Power readings run one target at a time through a looped include, and the list is accumulated quadratically. Each guest command also re-runs `ssh_key_policy` and rewrites both files; network.yml makes about 10 such calls.
- **Impact:** One unresponsive controller delays the whole survey; repeated per-command setup.
- **Fix:** One module call per survey that reads targets concurrently; read the policy once per session. (effort M)

#### Patterns
- The "name the refusal, then fail with a closed vocabulary" block is repeated with inline reason/cause allowlists that restate the Python worker vocabularies: apply.yml:766-797 (the same list twice within one task), destroy.yml:104-114, update_session.yml:199-228, firewall session.yml:71-103.
- The long remedy tail in network.yml ("repeat bootwright apply; if it fails the same way … correct the network of X, then run bootwright apply") appears 6 times (network.yml:92-95, 171-174, 230-233, 243-246, 258-261, 366-368); a templated default would remove it.
- `changed_when: false` is used on real filesystem effects to scratch or work directories (apply.yml:155-168, 631-635, guest.yml:22-31). It is deliberate for scratch, but it hides real removals.
- JSON for the guest is hand-built in Jinja (network.yml:305-306) instead of using the canonical JSON module; it is safe because bootId and digest are validated (module_utils/managedos_network.py:96).

#### Strengths
- Comments explain *why* almost everywhere: Q66 before-state, eager set_fact freezing, the no_log censorship workaround.
- The private-material path mints its token at runtime, stages files then renames them atomically, uses 0711/0640 modes, and redacts tokens from every echoed message (apply.yml:201-213, 392-395, 876).
- Playbooks are thin, and every substrate effect is dispatched by the frozen arm through `substrate_machine` (`admit` before the first effect).
- Commands use argv with absolute executables; there is no shell module, and `expand_argument_vars: false` is used where supported.
- Role conditions are unit-tested through Templar, together with strong structural fitness tests (handoff, dispatch, rescue, scratch path).

#### Coverage notes
- test_listener_fetch_retries.py:29-33 covers 3 of the lane's 6 listener proofs and never asserts `use_proxy`/`follow_redirects`, so the gap in [Y01-03](#y01-03) passes.
- machine_ssh_identity/tasks/main.yml:47-114 is not part of `test_guest_commands_pin_the_host_key`, so its options can drift.
- managedos_disk_discovery/tasks/result.yml has no test that the map covers every code emitted by disk_discovery_runtime.diagnostic.
- No test binds the YAML session timeouts (register.yml:63-66, update_session.yml:57) to Go's registrationBound/updateBound.
- The YAML requirement flags ([Y01-02](#y01-02)) are compared only with each other, never against Go's `Request.serialRoot()`.
- physical_erase/tasks/destroy.yml has no test pinning validate-before-`loaded` ordering.

#### Already recorded
- B568: the anaconda foreign-installation guard passes when nothing answered (apply.yml:459-479 area).
- B431: undeclared nested mappings in managedos_install_anaconda and physical_erase argument specs.
- Audit 003 MOI-04 (private files copied unstaged): fixed in the current private.yml/private_recovery.yml, which now stage and rename; not re-reported.

<a id="lane-y02"></a>

### Lane Y02: Substrate roles (libvirt, KubeVirt, bare metal, dispatcher)
Scope read: all task files, defaults and templates of substrate_libvirt_host, substrate_libvirt_machine, substrate_kubevirt_access, substrate_kubevirt_host, substrate_kubevirt_machine, substrate_baremetal_machine and substrate_machine (including the dispatcher's argument_specs and kubevirt_access argument_specs); one substrate playbook (libvirt_host_apply.yml; the 17 are identical 13-line wrappers) / skimmed: test_machine_ports.py, test_substrate_machine_dispatch.py, test_pre_boot_refusals.py, test_role_boundaries.py:555-600, test_substrate_libvirt_replay.py:330-390, test_baremetal_replay.py, substrate_physical_protocol.py, substrate_kubevirt_api.py:40-75, guest_agent_read.py, consumers release.yml, await.yml and node_release.yml / not read: the large argument_specs.yml of libvirt/kubevirt/baremetal machine (only the contract test's view of them), test_role_structure.py and test_role_templates.py in full (time; structural rules taken from their names and the boundaries excerpt).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 3 | Fail-closed guards are thorough, but the dispatcher can republish stale answers and the arms do not judge results the same way |
| Security | 4 | no_log and judge-next is applied everywhere it matters, argv only, secrets go through stdin or module args; Python is rendered into conf.py through to_json |
| Efficiency | 3 | dnf runs once per package, and virsh net-start and pool-start spawn on every replay |
| Clearness | 3 | Good rationale comments, but three alias sets of the same port fields and progress groups that say "ok" before the check runs |
| DDD | 4 | Dispatcher table, arm ownership and the request/result protocol are clean; the shared Redfish adapter sits in the dispatcher role |
| Non-duplication | 2 | power/state_read/boot_disk are copied between two arms, there are 27 Redfish credential preambles, and the KubeVirt port guard appears 7 times |
| Right language | 3 | Python is embedded in a podman command string, and the bare-metal identity rule exists in both Jinja and Python |
| Test coverage | 3 | Strong structural tests and rendered-expression tests; KubeVirt is left out of the port contract test, and stale republish and locale are untested |

#### Findings
##### Y02-01

**Medium · Quality · confidence medium**

- **Where:** roles/substrate_machine/tasks/dispatch.yml:21-26, roles/substrate_libvirt_machine/tasks/boot_disk.yml:19, roles/substrate_baremetal_machine/tasks/boot_disk.yml:25, roles/substrate_kubevirt_machine/tasks/boot_disk.yml:29
- **What:** After every entry point, the dispatcher's `always` republishes `<arm>_port_result`. But only state_read, power and media_* (and every KubeVirt entry) register that name. Bare-metal and libvirt boot_disk, boot_media and pre_boot register other names, and no entry point clears `_port_result` on entry. So `substrate_machine_port_result` after those entry points is whatever an earlier call left behind.
- **Evidence:** `substrate_machine_port_result: "{{ lookup('ansible.builtin.vars', substrate_machine_prefix ~ '_port_result', default={}) }}"`; libvirt boot_disk `register: substrate_libvirt_machine_disk_selected`.
- **Impact:** Today's consumers do not read port_result after boot_disk or boot_media (await.yml and release.yml end there). Any future reader would silently act on a previous call's answer, possibly one from another machine in the same host loop.
- **Fix:** Clear `<arm>_port_result: {}` as the first task of each arm entry point, or have the dispatcher clear its own published names before the include. (effort S)
- **Checked:** No production consumer reads port_result after boot_disk or boot_media. The structural tests check only that a reporter exists, not that it is fresh.

##### Y02-02

**Medium · Non-duplication · confidence high**

- **Where:** roles/substrate_baremetal_machine/tasks/{power,state_read,boot_disk}.yml vs roles/substrate_libvirt_machine/tasks/{power,state_read,boot_disk}.yml
- **What:** After renaming the arm, the files are byte-identical except that bare metal adds `verification:` (and boot_disk makes ca_data conditional). media_insert and media_eject were already moved into the shared `substrate_machine/redfish_media_*.yml`; these three were not.
- **Evidence:** `diff` after `s/baremetal|libvirt/ARM/` shows only `verification: "{{ bootwright_machine_port.verification | default(omit, true) }}"` lines (and ca_data in boot_disk).
- **Impact:** Every Redfish fix has to land twice. The `verification` drift already exists: the libvirt arm ignores a `verification` its consumers pass (await.yml:83), while its shared media_insert honours it.
- **Fix:** Move power, state_read and boot_disk into shared `redfish_*.yml` task files, as was done for media, and have both arms republish. (effort M)
- **Checked:** test_machine_ports.py:12-27 pins only the shared contract, not equivalence. No backlog item.

##### Y02-03

**Medium · Right language / Non-duplication · confidence high**

- **Where:** roles/substrate_libvirt_machine/tasks/apply.yml:265-294
- **What:** The keep-or-rehash logic for the BMC htpasswd is a Python program in a YAML string, run through `podman run ... python3 -c`. The same family of problem for the Registry and managed services is solved in Go (`internal/secrets/material/password_file.go` `BcryptPasswordFile`), and there the host "does no cryptography" (infra_managed_service/tasks/apply.yml:274-277).
- **Evidence:** `- python3` / `- -c` / `import base64, bcrypt, sys ... bcrypt.checkpw(password, kept)`.
- **Impact:** The program can only be tested by extracting it from YAML and running it against a stand-in bcrypt (test_substrate_libvirt_replay.py:333-365). The project now carries two designs for the same credential-file contract.
- **Fix:** Derive the file in Go, as the Registry does, with a liveness verification instead of checkpw. Or move the program into a shipped module_utils worker. substrates.md:1011-1014 owns the checkpw clause, so this is a spec change. (effort M)
- **Checked:** Not a named exception in architecture.md. The spec mandates the behaviour, not the language.

##### Y02-04

**Medium · Quality · confidence medium**

- **Where:** roles/substrate_libvirt_machine/tasks/destroy.yml:151-153; roles/substrate_libvirt_host/tasks/apply.yml:457-459, 533-535; roles/substrate_libvirt_host/tasks/destroy.yml:200-203, 218-220, 246-249, 261-263
- **What:** Seven virsh tasks decide "already done" by matching English stderr (`"failed to get domain"`, `"already active"`, `"not found"`, `"no storage pool"`) without `environment: LC_ALL: C`. The rmdir tasks in the same files do set it (machine destroy.yml:186-187, host destroy.yml:282-283).
- **Evidence:** `- '"failed to get domain" not in substrate_libvirt_machine_undefined.stderr'`; no module_set_locale is configured anywhere (grep).
- **Impact:** On a host with a translated locale, a replayed destroy or apply fails where it should converge. virsh messages are gettext-translated.
- **Fix:** Add `environment: {LC_ALL: C}` to these tasks. For undefine, also skip when `before.observation.domain` is empty. (effort S)
- **Checked:** There is no knowledge page or backlog item on virsh locale. The SSH session boundary does not force LC_ALL for modules.

##### Y02-05

**Medium · Right language / Non-duplication · confidence high**

- **Where:** roles/substrate_baremetal_machine/tasks/target_proof.yml:58-66; plugins/action/substrate_physical_protocol.py:83-90
- **What:** The rule "the reported addresses equal the declared set, there are no inventory failures, and a UUID or serial is present" is written in Jinja for pre-boot and in Python `presence()` for apply and observe. The Python side bounds each address to 32 characters; the Jinja side does not.
- **Evidence:** Jinja `expected | difference(observation.addresses) | length == 0` both ways; Python `expected == set(evidence["addresses"]) and (evidence["uuid"] or evidence["serial"])`.
- **Impact:** The two proofs of the same machine identity can drift apart. That is the wrong-target guard before an erase.
- **Fix:** Expose the Python predicate (and `unproved()` for the message) as a filter or action, and call it from target_proof.yml. (effort M)
- **Checked:** Grepped the audit and backlog; nothing tracks it.

##### Y02-06

**Low · Quality · confidence medium**

- **Where:** roles/substrate_kubevirt_machine/tasks/boot_disk.yml:20-31; consumer roles/containercluster_install_agent/tasks/release.yml:41-59
- **What:** The bare-metal and libvirt boot_disk entry points stop at their own refusal. The KubeVirt one registers the result and never judges it, and the consumer does not judge it either, so `node_release.yml:12` marks the phase complete.
- **Evidence:** KubeVirt boot_disk ends at `failed_when: false` / `no_log: true` with no assert. release.yml has no task after the include.
- **Impact:** A KubeVirt endpoint or kubeconfig refusal during release (`arm.endpoint` and `Host` run before the `power_on` check, substrate_kubevirt_api.py:49-60) is lost.
- **Fix:** Add the same "Stop at ..." assert that KubeVirt boot_media has (lines 32-39). (effort S)
- **Checked:** Managed OS does not target KubeVirt, so only the agent release path is affected.

##### Y02-07

**Low · Quality · confidence medium**

- **Where:** roles/substrate_machine/defaults/main.yml; roles/*_machine/tasks/{power,state_read,media_*}.yml
- **What:** `<arm>_port_refusal` is written in 10 places, but no production task reads it: the dispatcher republishes only `_refusal` and `_port_result`. It exists to satisfy `returns_port_answer` in test_role_boundaries.py:568-572 and test_machine_ports.py:24.
- **Evidence:** grep for `_port_refusal` outside set_fact finds only tests.
- **Impact:** It is dead state that readers mistake for a contract.
- **Fix:** Drop it and point the structural rule at `_port_result`, or have the dispatcher republish it. (effort S)
- **Checked:** All consumers read `substrate_machine_port_result` or `substrate_machine_refusal`.

##### Y02-08

**Low · Clearness · confidence high**

- **Where:** roles/substrate_baremetal_machine/tasks/apply.yml:68-80; roles/substrate_libvirt_machine/tasks/destroy.yml:202-219; roles/substrate_libvirt_host/tasks/destroy.yml:316-333
- **What:** The "prove-identity" and "verify-absence" groups report `running` and then `ok` with no check in between. The real judgement happens later, in the `completed` phase.
- **Evidence:** "Report proving the machine's identity" `status: running` is followed directly by "Report the proved machine" `status: ok`.
- **Impact:** In the run tree, an identity mismatch shows as prove-identity ok followed by a refusal.
- **Fix:** Derive the status from the evidence, as the KubeVirt roles do (`"ok" if ...postcondition else "failed"`, kubevirt_machine apply.yml:63). (effort S)
- **Checked:** completion() in substrate_physical_protocol.py:173-187 is where the refusal happens.

##### Y02-09

**Low · Efficiency · confidence high**

- **Where:** roles/substrate_libvirt_host/tasks/apply.yml:42-55
- **What:** Each package runs its own `dnf install` transaction (loop), each with its own metadata and solve. `changed_when` reports changed even when dnf did nothing.
- **Evidence:** `- install` / `- '{{ item }}'` / `loop: '{{ bootwright_substrate_host_request.packages }}'`.
- **Impact:** Provider bootstrap takes N times as long, and partial installs happen.
- **Fix:** Pass all packages in one argv. (effort S)
- **Checked:** ARC-04 covers only that the role installs packages at all, not how.

##### Y02-10

**Low · Non-duplication · confidence high**

- **Where:** roles/substrate_kubevirt_machine/tasks/*.yml (7 copies at lines ~9-19); substrate_kubevirt_host/tasks/apply.yml vs observe.yml; kubevirt_machine apply.yml vs destroy.yml
- **What:** The "Refuse port access this arm does not read" block is repeated in 7 files. kubevirt_host apply.yml and observe.yml differ in two lines (task name and `observed`). kubevirt_machine apply.yml and destroy.yml differ only in operation and group names.
- **Impact:** This is most of the 355 cloned lines.
- **Fix:** Use one `port_access.yml` include, and parameterize apply and observe through `include_tasks` with vars. (effort S)
- **Checked:** These are not deliberate per-package ports.

##### Y02-11

**Low · Clearness · confidence high**

- **Where:** roles/substrate_baremetal_machine/defaults/main.yml:31-56; roles/substrate_libvirt_machine/defaults/main.yml:31-49
- **What:** Three alias sets (`_target_`, `_boot_`, `_disk_`) each re-map the same `bootwright_machine_port.endpoint/ca/user/password/verify`. Tasks then mix the aliases with direct reads (`bootwright_machine_port.verification` in boot_disk.yml:19).
- **Impact:** Reading any Redfish call means chasing three indirections to one source.
- **Fix:** Read `bootwright_machine_port` directly, or keep one alias set. (effort S)
- **Checked:** The comment "native task implementation owns its aliases" gives no reason for three sets.

##### Y02-12

**Low · Quality · confidence medium**

- **Where:** roles/substrate_libvirt_machine/tasks/identity_agent.yml:33,37
- **What:** `when: substrate_libvirt_machine_marker_read.answered` has no default. guest_agent_read.py:138 `fail_json`s without `answered` (path refused), and the task has `failed_when: false`.
- **Impact:** That path ends in an undefined-variable error instead of the module's message.
- **Fix:** Use `| default(false)` and an explicit assert on `.msg`. (effort S)
- **Checked:** The other exits always set `answered`.

#### Patterns
- 27 Redfish call sites each repeat a 6-8 line endpoint/user/password/verify/verification/ca_data preamble (baremetal boot_media.yml:14-22, libvirt pre_boot.yml:38-44, substrate_machine/redfish_media_insert.yml:8-16), and `verification` and the conditional ca_data are applied inconsistently. A single port-credential var block or an action wrapper would fix the drift.
- The shared Redfish adapter (`redfish_media_*`) lives in the dispatcher role, so arms include the dispatcher (libvirt media_insert.yml:3, baremetal media_eject.yml:3). That makes the dependency run in both directions, and the adapter has no argument_specs entry.
- The dispatcher's 12 files cost little: 8 are 5-line stubs that buy argument-spec validation per entry point. admit.yml:2-8 restates dispatch.yml:3-10's terminal fail; one shared message would do.

#### Strengths
- The dispatcher contract holds as the spec states: the table lives only in defaults, an unlisted arm fails by name before the lookup, the arm's answer is republished in `always`, and the test_substrate_machine_dispatch.py:63-78 rule plus a seeded check enforce it.
- Bare-metal target proof: identity, full inventory, pin and power are read fresh before media insertion and again after any firmware write (pre_boot.yml:27-31). Every hidden controller call is followed by a judge task outside no_log. I found no exception (until-exhaustion under `failed_when: false` is covered by knowledge/ansible-core-retries-and-timeouts.md:46).
- Guards fail closed on unanswered reads (driver, pool, NM, routing, guests), with remedies that name exact commands.
- Templates: conf.py renders every request string through `to_json`, and the only free-form XML value (bridge) is `| e`-escaped. Domain and network XML are deterministic for a given request.
- Secrets go only through stdin or module args (htpasswd derivation, private_file), never through argv or files that get transferred.

#### Coverage notes
- test_machine_ports.py:12 parametrizes `["libvirt", "baremetal"]` only, although its docstring says "Every realized arm"; the KubeVirt port contract is unpinned (relevant to [Y02-06](#y02-06)).
- Nothing tests that `substrate_machine_port_result` is fresh after boot_disk or boot_media ([Y02-01](#y02-01)).
- No test pins virsh stderr matching under a non-C locale ([Y02-04](#y02-04)).
- The equivalence of the Jinja and Python identity rules is untested ([Y02-05](#y02-05)).
- The embedded bcrypt program is tested only against a stand-in bcrypt module (test_substrate_libvirt_replay.py:333-378), not the image's own bcrypt.
- Role tests evaluate the real expressions through Templar and drive the bare-metal publication through ansible-playbook (test_baremetal_replay.py:219), which is good. The libvirt apply and destroy command sequences are checked for order (test_libvirt_host_apply_order.py) but not executed.

#### Already recorded
- ARC-04 (audit 003): substrate_libvirt_host installs host packages, against the "no role installs a package" sentence. It concerns the fact of installing, not the per-package loop in [Y02-09](#y02-09).
- SBV-01 (audit 003): libvirt route collision skips; it lives in module_utils, not this lane's YAML.
- I found no open B-item for [Y02-01](#y02-01) through [Y02-12](#y02-12) (grepped the backlog for dnf, virsh, LC_ALL/locale and bcrypt).

<a id="lane-y03"></a>

### Lane Y03: Container cluster and add-on roles (Ansible YAML)
Scope read: read in full: every task file of containercluster_install_agent (24) and containercluster_media_agent (6), plus the tasks of containercluster_network, containercluster_admin_credentials, addons_instance and cluster_objects; both defaults/main.yml files; install_agent's meta/argument_specs.yml; playbooks/containercluster/install_apply.yml, install_observe.yml and install_destroy.yml; playbooks/addons/instance_apply.yml. Skimmed: the entry points in the other argument_specs files; test_containercluster_budgets.py (the docstring, the Wait simulator and the test names); test_containercluster_rounds.py; test_containercluster_kubevirt_port.py:90-125; the test names in the resolution, preboot and token_wiring tests; install_phase_tasks.py; specs/openshift-clusters.md:565-585, 725-745 and 1270-1280; the architecture "Language responsibility" table; ansible.md "Implementation". Not read: the bodies of the Python plugins (another lane covers them; I only grepped them to check claims) and the other 13-line playbooks (the clone data shows they are identical wrappers).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful budgets, fail-closed guards and censored-failure handling; a few hidden couplings |
| Security | 4 | no_log on every task with bound material, 0600/0700 files, verified listener probes, tokens minted at run time; one ordering nit |
| Efficiency | 3 | destroy and observe read controllers one node at a time; four `oc` round trips per cluster read; the 41-iteration wait expansion |
| Clearness | 2 | Rationale comments are excellent, but nested Jinja ternaries in diagnostics and three-level state machines in set_fact are hard to follow |
| DDD | 3 | The procedure of one capability (resume policy, settled decision, identity classification) lives in YAML, not in the plugins that own the native API |
| Non-duplication | 2 | Four node plays repeat a 14-line vars block; the port dict is composed four times; node-state collection is implemented twice |
| Right language | 2 | The four heaviest Jinja tasks in the collection carry procedure and native-output interpretation that belong in Python |
| Test coverage | 4 | Behavioral tests render the real task files and even run a real ansible-playbook round, but they need a hand-written ansible-core simulator |

#### Findings
##### Y03-01

**High · Right language · confidence medium**

- **Where:** roles/containercluster_install_agent/tasks/wait_attempt.yml:86-104, wait.yml:37-109, defaults/main.yml:38-47,83-91
- **What:** The installer wait's resume policy is written as Jinja over regexes of `openshift-install` stderr: whether to try again, whether a stall was seen, which hint to give, and whether to read the assisted service. That is the fixed procedure of one frozen request plus interpretation of native output, which the language table assigns to Python. YAML should only bind and invoke it.
- **Evidence:** `containercluster_install_agent_waiting: >- {{ ... attempted.rc not in ..._budget_spent_codes and not (... is search(..._host_error_pattern)) and (... is search(..._resumable_pattern)) }}`. The failure message at wait.yml:93-105 is a five-level nested ternary. Testing it needs the `Wait` class, about 130 lines that re-implement ansible-core block, include, register and skip semantics (tests/unit/test_containercluster_budgets.py:313-445). That class asserts it has "no model of" rescue or always.
- **Impact:** Every change to the wait means editing interleaved Jinja and keeping a private copy of ansible-core's executor semantics in step. The simulator can drift from real ansible-core without anything noticing.
- **Fix:** Move the attempt loop, deadline arithmetic, stderr classification and hint selection into a module that runs the installer through `bounded_process`, returns a typed outcome (`reached`/`stalled`/`host-error`/`budget-spent`/`timeout`/`unclassified`), and leaves one YAML task plus one fail. (effort M)
- **Checked:** The comment at wait.yml:10-16 explains why a looped include is used instead of `until`; that is a constraint of YAML and does not favour keeping the logic there. No B item or audit 003 entry covers it. The language table's "Never" for YAML does not literally forbid this, which is why confidence is medium.

##### Y03-02

**Medium · Right language / DDD / Efficiency · confidence high**

- **Where:** roles/containercluster_install_agent/tasks/cluster_state.yml:15-114
- **What:** Each cluster read makes four separate `oc` calls, three of them against the same ClusterVersion object. It then classifies identity in Jinja from stderr text. Meanwhile a bounded Python `oc` runner already exists with the same flags.
- **Evidence:** `x509: |You must be logged in to the server|Error from server \(Forbidden\)` (line 113), and `.splitlines() == ['True', 'Completed', ...version]` (lines 102-103). Python already has `command = [oc, "--kubeconfig", kubeconfig, "--request-timeout=30s"] + argv` (plugins/module_utils/cluster_objects_native.py:93), and placement also goes through Python.
- **Impact:** Each read costs four TLS handshakes. It runs from apply, waits, after_release, observe and destroy (via state.yml). The output is captured unbounded, and the knowledge of how to call `oc` lives in two languages.
- **Fix:** Add a read operation to `containercluster_install_inspect`, or a sibling module, that runs one `oc get clusterversion version -o json` plus one nodes read through the existing runner and returns `{cluster, release, completed, missing}`. (effort M)
- **Checked:** --request-timeout is set on each call, and test_containercluster_install_identity.py pins the foreign classification. Behaviour is correct; the cost is placement and round trips.

##### Y03-03

**Medium · Non-duplication / Efficiency · confidence high**

- **Where:** tasks/state.yml:11-71 with state_node_serial.yml; tasks/node_state.yml:7-48 with collect_state.yml:13-52; destroy.yml:29,51,139
- **What:** Node observation is implemented twice. Apply uses the parallel node plays and the hostvars collection. Observe and destroy use a serial loop. Each path has its own `ownMedia` derivation: state.yml branches on `deliveryKind == 'push'`, while collect_state relies on node_state having synthesized `{own: ...}`.
- **Evidence:** state.yml:55-62, `ownMedia: >- {{ (... selectattr('media_state', 'equalto', 'own') ...) if ... deliveryKind == 'push' else (...) }}`, against collect_state.yml:40-43, which has no push branch. Destroy reads every controller, ejects every node and reads them all again, each step serially (destroy.yml:29, 51, 139).
- **Impact:** A change to the media-ownership rule has to be made in two places that already differ in form. Destroy costs about 3N serial Redfish or KubeVirt round trips, against the owner's parallel-by-default rule.
- **Fix:** Give install_observe/install_destroy the same node-play fan-out as install_apply, or at least one shared resolve task file. (effort M)
- **Checked:** The fan-out plays exist only in install_apply.yml (the architecture names it as the one multi-play playbook), so the destroy and observe playbooks have `serial: 1` and no node play. No tracked item covers this.

##### Y03-04

**Medium · Non-duplication · confidence high**

- **Where:** playbooks/containercluster/install_apply.yml:23-36 (repeated at 76-89, 129-142, 182-195); tasks/port.yml:22-29; boot.yml:91-107, 226-237; release.yml:48-59; state.yml:18-22; destroy.yml:58-64
- **What:** The per-node credential bindings (`controllerUserNode`/`PasswordNode`/`CANode` with the `portPinned` blank) are spelled six times. The machine-port access dict (endpoint, verify, ca, user, password, optional pin, optional `verification`) is composed four times, three of them inline outside port.yml.
- **Evidence:** The clone data shows four 21-line windows of install_apply.yml as identical. The pattern `combine({'verification': ...tlsVerification | default('')} if (...) | length > 0 else {})` appears in port.yml:28, boot.yml:106, boot.yml:236 and release.yml:58.
- **Impact:** Adding one port field (as `verification` was) means editing four or more sites. Only port.yml's composition is validated against every arm's argument spec (test_containercluster_rounds.py:82-107). For the pre_boot and boot_media dicts, only the KubeVirt arm's spec is checked (test_containercluster_kubevirt_port.py).
- **Fix:** Define `containercluster_install_agent_port_access` and the node credential bindings once in role defaults keyed on `containercluster_install_agent_index`, and have boot.yml and release.yml combine only their extras (`installer_powers_off`, `power_on`, the proof fields). (effort S)
- **Checked:** These are not consumer-owned ports; they are the same composition. A playbook cannot use group_vars, but role defaults can carry it.

##### Y03-05

**Low · Non-duplication · confidence high**

- **Where:** containercluster_media_agent/tasks/apply.yml:2-33 = destroy.yml:2-33; install_agent finish.yml:62-90 ≈ observe.yml:44-72; the epoch expression in 11 places
- **What:** Request validation and handoff are copied between media apply and destroy. The custody copy pair and its 3-4 line "proved identity" guard are repeated. `((now(utc=true) - ('1970-01-01 00:00:00' | to_datetime)).total_seconds() | int)` appears 11 times across six files.
- **Evidence:** The clone pairs (30, 13 and 13 lines). Running `grep -c "1970-01-01 00:00:00"` gives wait_attempt 3, boot 2, readiness 2, build 2, prepare_boot 1, wait 1.
- **Impact:** Drift risk. The epoch expression carries a daylight-saving rationale that is stated only once, at boot.yml:71-73.
- **Fix:** Put the validation in a `validate.yml` included by both, the custody copy in `custody.yml`, and the epoch in a `bootwright.core.epoch_seconds` filter or a role default variable. (effort S)
- **Checked:** test_containercluster_budgets.py:884 pins the daylight-saving behaviour at each site, so the copies are at least kept consistent.

##### Y03-06

**Low · Security · confidence medium**

- **Where:** roles/containercluster_media_agent/tasks/publish.yml:93-125
- **What:** The image is renamed into the served subtree first, and only afterwards set to 0640 and labelled. During that window it carries whatever mode the installer gave it.
- **Evidence:** `- /usr/bin/mv ... '{{ containercluster_media_agent_directory }}/agent.iso'` (line 96-99) comes before `mode: '0640'` (line 113).
- **Impact:** The exposure is narrow, because the directories are 0711, the 64-hex segment is unguessable, and the SELinux label is not yet applied. Still, the image embeds the pull secret (openshift-clusters.md:569).
- **Fix:** Set owner and mode on the product inside the 0700 work area before the `mv`. (effort S)
- **Checked:** The work area is 0700 (build.yml:39-45). I could not confirm the installer's output mode.

##### Y03-07

**Low · Quality / Right language · confidence high**

- **Where:** containercluster_media_agent/tasks/publish.yml:34-44; apply.yml:45-51
- **What:** The publication segment is minted by spawning `openssl rand`, while insert tokens are minted in Python (`secrets.token_hex(32)`, plugins/modules/containercluster_media_token.py:252). Separately, `openshift-install version` runs with no `timeout` wrapper.
- **Evidence:** `- /usr/bin/openssl - rand - -hex - '32'`; and `argv: - '{{ containercluster_media_agent_installer }}' - version` with no time bound.
- **Impact:** There are two token mechanisms where one would do. A hung installer binary would block the media apply until the run's deadline.
- **Fix:** Mint the segment in `containercluster_media_layout` or `containercluster_publication`, and wrap the version read in `/usr/bin/timeout`. (effort S)
- **Checked:** Every other installer call in the lane goes through `/usr/bin/timeout`.

##### Y03-08

**Low · Quality · confidence medium**

- **Where:** roles/containercluster_media_agent/tasks/build.yml:216, 73-93
- **What:** `lookup('ansible.builtin.file', ...)` reads on the Ansible controller. It reads the installer's generated `cluster-manifests/agent-cluster-install.yaml` in the work area, which is correct only because Go pins the placement to `connection: local` (internal/containercluster/agentinstall/requests.go:374). Meanwhile the comment at build.yml:56-58 talks about "a remote connection".
- **Evidence:** `{{ lookup('ansible.builtin.file', containercluster_media_agent_work ~ '/cluster-manifests/agent-cluster-install.yaml') | from_yaml ...`
- **Impact:** If the placement is ever widened, the role would silently read the wrong host.
- **Fix:** Read the manifest with `slurp`, or assert `ansible_connection == 'local'` in the entry validation. (effort S)
- **Checked:** requests.go:369-374 refuses any other placement today, so there is no live defect.

##### Y03-09

**Low · Efficiency / Clearness · confidence high**

- **Where:** roles/containercluster_install_agent/tasks/wait.yml:28-33
- **What:** Each wait always expands `wait_retries + 1` = 41 includes of a 6-task block, even after success.
- **Evidence:** `loop: '{{ range((containercluster_install_agent_wait_retries | int) + 1) | list }}'`, so two waits record about 480 skipped tasks in the log.
- **Impact:** The operator's run log is noisy, and the cost grows with the retry count.
- **Fix:** This disappears with [Y03-01](#y03-01). (effort S)
- **Checked:** The rationale for the looped include is documented at wait.yml:10-16.

#### Patterns
- Diagnostics are composed in Jinja from native output: wait.yml:83-109, waits.yml:64-81, boot.yml:164-170, publish.yml:164-181, preboot_vips.yml:36-38. They are testable only by rendering the task files.
- Requests are validated in YAML with long `assert` lists (install apply.yml:2-13, media apply.yml:2-22) that repeat what the protocol action plugin loads anyway. Node elements are `type: dict` with no options (install_agent argument_specs.yml:59-62).
- The "censored failure, then a judging task" pattern is applied uniformly and correctly: boot.yml:201-217, state.yml:24-33, prepare_boot.yml:22-30, destroy.yml:66-75.

#### Strengths
- Budgets hold in wall-clock time across retries. Every installer call runs under `/usr/bin/timeout --kill-after`, and the 124/137/-9 exit codes are told apart from the installer's own give-ups. The daylight-saving-safe epoch is pinned by tests.
- Durability ordering is explicit and documented: the insert record is written before the insert (boot.yml:180-189), the work-area mark is never rewritten (prepare_boot.yml:76-87), and removal goes in reverse order.
- Secret handling is tight. Material reaches the installer config only inside a pipelined `private_file` payload under no_log and `diff: false`. Each insert gets its own revocable link, proved through the verifying listener with `use_proxy: false` and `follow_redirects: none`.
- Fan-out node rounds report each node's failure only after every sibling has finished, and test_containercluster_rounds.py:15 proves this with a real ansible-playbook run.
- Every command uses argv with an absolute executable (`/usr/bin/getent`, `/usr/sbin/arping`, `/usr/bin/timeout`); there is no shell.

#### Coverage notes
- The `Wait` simulator (test_containercluster_budgets.py:313-445) models ansible-core semantics by hand. No test runs wait.yml under real ansible-playbook, so a change in ansible-core's include or register semantics would go unnoticed.
- Only the node_boot play of install_apply.yml is run as a real playbook. The node_state and node_release plays and their hostvars hand-offs (collect_state.yml:13-29) are tested only by rendering.
- The pre_boot, boot_media and boot_disk port dicts in boot.yml and release.yml are validated against the KubeVirt arm's spec only, not the libvirt or baremetal specs (see [Y03-04](#y03-04)).
- Python coverage: the thin module stubs read 0% because the work runs in action plugins. plugins/action/cluster_objects_read.py is at 50.0% (14 of 28 statements uncovered), the weakest file behind these roles' observe path.

#### Already recorded
- RCP-11 (audit 003): the tokenized insert URL is written in the scratch root (boot.yml:173-178).
- B1326: the `oc` source-repository fallback after a mirror failure is not refused (behind the wrapper at build.yml:173-189).

<a id="lane-y04"></a>

### Lane Y04: Infrastructure service, mirror, storage and controller roles (Ansible YAML and templates)

Scope read: I read these fully: infra_managed_service (every task file, defaults, vars, argument_specs, all 8 templates), infra_artifact_server_nginx (apply, observe, defaults, both templates), infrastructureservices_mirror_release (apply, observe, defaults), mirror_fill (apply, defaults, argument_specs, destroy and observe from line 30 on), mirror_images (apply, defaults, observe and destroy from line 20 on), storage_ceph_core (all three task files), storage_ceph_host (prepare, step), storage_ceph_admin_credentials (both task files), controller_prerequisites (everything), and the playbooks controller/setup.yml, storage/host_apply.yml and infrastructureservices/registry_apply.yml. / Skimmed: I compared the other Ceph role task files and the nginx destroy.yml by diff instead of reading them line by line. I read the first 40 lines of storage_ceph_core argument_specs, plus internal/storage/ceph/role_admission_test.go, test_managed_service_role.py, test_infrastructureservices_mirror_fill.py, test_service_destroy_safety.py, test_role_templates.py, test_ceph_refusal_record.py, and the backlog and audit 003 greps. / Not read: the other 21 playbooks in infrastructureservices/ and storage/ (they share the same 13-line import shape), and the remaining Ceph argument specs, whose exact fields a Go reflection test already holds.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Fail-closed, refusals are named before any effect, replay is reasoned; a few validation gaps are uneven between entry points |
| Security | 4 | argv only, no shell, `no_log`/`diff:false` used carefully, digest-pinned images; mirror downloads have no size bound, and unit names and some template inputs are trusted unchecked |
| Efficiency | 4 | Mostly bounded; full SHA-256 re-reads and a recursive relabel on every run |
| Clearness | 4 | Names are clear and comments give rationale; validation regexes are dense Jinja |
| DDD | 4 | Roles bind one capability each; a few product decisions (SELinux level, "release whole") live in YAML |
| Non-duplication | 2 | The artifact-server role is a near-copy of infra_managed_service; preambles are copied across entry points; a 4285 s budget is written in 5 places |
| Right language | 3 | Non-trivial Jinja derivations and judgments that belong in a plugin or in Go |
| Test coverage | 4 | Strong structural and golden tests; no test runs a task sequence, and some classifications are tested against invented messages |

#### Findings

##### Y04-01

**High · Non-duplication · confidence high**

- **Where:** roles/infra_artifact_server_nginx/tasks/destroy.yml:1-181, roles/infra_managed_service/tasks/destroy.yml:1-181; infra_artifact_server_nginx/tasks/apply.yml:38-137,248-311 vs infra_managed_service/tasks/apply.yml:70-137,343-402
- **What:** The artifact-server role repeats the managed-service lifecycle task for task: the podman probe, socket and bind refusals, the pinned pull, hosted directories, daemon-reload, the restart rule (start time compared with file mtimes), the reset-failed handling and the parent removal.
- **Evidence:** With the role prefixes swapped for placeholders (sed), the two destroy.yml files differ in 4 lines out of 181 (version literal, the implementation check, two task names, one `request:` argument). The two apply.yml files share the same restart expression: `(…_started.stdout | default('') | trim | int) < (…runs_from….results | map(attribute='stat.mtime') | max) * 1000000000`.
- **Impact:** Every lifecycle fix has to be made twice. The tests already show it: test_service_destroy_safety.py ENTRIES and test_managed_service_replay are parametrized over both roles. Delivered history shows DNS, NTP and proxy were folded into one role (test_no_copied_managed_service_role_remains), but this copy stayed.
- **Fix:** Make nginx another row of `infra_managed_service_implementations`, which needs one protocol plugin or one protocol argument for both. Short of that, extract the lifecycle into private task files keyed only by role-owned variables. (effort L)
- **Checked:** architecture.md "Roles" lets implementations share a port when their success, failure, replay and evidence semantics agree, and the identical destroy files show they do. I found no B item tracking this.

##### Y04-02

**Medium · Non-duplication / Right language · confidence high**

- **Where:** roles/storage_ceph_host/tasks/prepare.yml:21; internal/storage/ceph/materials.go:305; plugins/modules/ceph_host.py:122; plugins/module_utils/ceph_host_runtime.py:80; plugins/module_utils/ceph_host_worker.py:16
- **What:** The host image-pull budget is a literal 4285 s in Go, YAML and three Python sites. Go does not freeze it into the request.
- **Evidence:** `storage_ceph_host_pull_seconds: 4285`; `hostImagesBudget     = 4285`; `"seconds": {"type": "int", "default": 4285}`.
- **Impact:** Go computes the block deadline from its own constant (materials.go:322) while the role and workers enforce theirs. If the copies drift, a deadline can be wrong with no test failing: the tests assert each copy against yet another literal (test_ceph_host_execution.py:450).
- **Fix:** Freeze the budget into `bootwright_ceph_host_request` (or the pulls entries) and read it from there everywhere. (effort M)
- **Checked:** I searched every test for a cross-language tie and found none.

##### Y04-03

**Medium · Security · confidence medium**

- **Where:** roles/infrastructureservices_mirror_fill/tasks/apply.yml:58-70; defaults/main.yml:163-164
- **What:** The frozen request carries each file's `bytes`, and the role validates it, but the download never enforces it. get_url streams whatever the publisher sends into a temporary file before the checksum runs.
- **Evidence:** `infrastructureservices_mirror_fill_valid_sizes: … map(attribute='bytes') | select('integer') | select('gt', 0)`; the get_url call has no size bound.
- **Impact:** A misbehaving or compromised publisher, or a proxy in the path, can fill the mirror host's temporary filesystem before the digest refusal. Compare controller_files.py:542 `stream_copy`, which copies exactly `size` bytes.
- **Fix:** Bound the transfer: use a module that reuses the bounded stream copy, or check Content-Length plus a post-download size check against `item.bytes`. (effort M)
- **Checked:** Backlog D251 (19) deliberately chose get_url for verify-then-rename and replay, but says nothing about size. Label: policy trade-off.

##### Y04-04

**Medium · Security / Quality · confidence medium**

- **Where:** roles/infra_managed_service/tasks/apply.yml:7, destroy.yml:2-18, defaults/main.yml:7; infra_artifact_server_nginx/tasks/apply.yml:6, defaults/main.yml:82
- **What:** `unit` is checked only as `is string` at apply and not at all at destroy, yet it builds a path that destroy deletes and is passed to systemctl and podman. The mirror roles regex-check every identity part.
- **Evidence:** `- bootwright_managed_service_request.unit is string`; `infra_managed_service_unit_path: /etc/containers/systemd/{{ bootwright_managed_service_request.unit }}.container` reaches `ansible.builtin.file … state: absent` (destroy.yml:67-71).
- **Impact:** The only guard is Go's derivation (capability.go:218 `UnitName(...)`). A malformed request, for example a unit containing `../`, would delete a file outside the Quadlet directory, while the role's own contentRoot checks are careful about exactly this (test_service_destroy_safety PLANTED).
- **Fix:** Assert `unit is match('^bootwright-[a-z0-9-]+$(?!\n)')` in every entry point of both roles. (effort S)
- **Checked:** I found no B item, and the PLANTED cases cover only contentRoot.

##### Y04-05

**Medium · Security · confidence medium**

- **Where:** roles/infra_managed_service/templates/dnsmasq.conf.j2:25,29,34; chrony.conf.j2:7,12,15; apply.yml:20-41
- **What:** The role guards destinations, localZones and forwardZones against directive injection before rendering, but `records[].name/addresses`, `forwarders`, `clients`, `sources` and `bindAddress` reach dnsmasq and chrony lines unchecked.
- **Evidence:** `host-record={{ record.name }},{{ address }}`; `server {{ source }} iburst`; the tests cover injection only for zones (test_dns_local_zone_refuses_injection_before_effects).
- **Impact:** Defense in depth is uneven. A newline in any of these values would add a root daemon directive (dnsmasq accepts `dhcp-script=`), and Go is the only guard.
- **Fix:** Extend `dns_forward_zones_valid` into one request-shape filter that covers every rendered list. (effort S)
- **Checked:** Go derives records and clients from admitted Machines (requests.go:114,135). That is why this is Medium and not High.

##### Y04-06

**Medium · Right language / DDD · confidence high**

- **Where:** roles/infra_managed_service/defaults/main.yml:17-22,25-29
- **What:** The registry's SELinux MCS level, a product isolation decision, is computed in Jinja from a sha256 of the unit name. Whether a running registry "accepts the credential", which decides whether the password file is republished, is a 4-clause Jinja judgment.
- **Evidence:** `{%- set first = (digest[0:4] | int(base=16)) % 1024 -%}` … `s0:c{{ [first, second] | min }},c{{ [first, second] | max }}`.
- **Impact:** A change to either expression alters an on-host effect (labels, restarts) that no Go version or contract golden sees. It is also the hardest Jinja in the lane to review.
- **Fix:** Have Go freeze `labelLevel` into the request, and move the acceptance predicate into `infra_service_inspect`'s result, for example an `accepted` boolean. (effort M)
- **Checked:** The relabel decision is tested (test_the_storage_is_relabeled_whole_only…), but nothing tests the derivation of the level itself.

##### Y04-07

**Low · Non-duplication / Right language · confidence high**

- **Where:** roles/infrastructureservices_mirror_release/defaults/main.yml:47-50; tasks/apply.yml:157-160,166-169
- **What:** The "release held whole" rule (digest non-empty, missing == 0, components non-empty) is written three times in YAML: once for the first read and twice for the read-back, as the refusal and the fail.
- **Evidence:** `…_verified.digest | default('') == '' or …missing | default(1) | int != 0 or …components | default([]) | length == 0`
- **Impact:** The three copies can drift. Judging this postcondition is a module's (or Go's) job, not YAML's.
- **Fix:** Have the `read` mode return `whole: true/false` and branch on that. (effort S)
- **Checked:** test_the_completion_proves_a_release_held_whole… exercises the expressions but would not catch two copies diverging.

##### Y04-08

**Low · Quality · confidence medium**

- **Where:** roles/infrastructureservices_mirror_fill/tasks/apply.yml:79; defaults/main.yml:150-152
- **What:** The rescue tells a digest mismatch from an unreachable publisher by searching ansible-core's English error text for "checksum".
- **Evidence:** `'file-digest-mismatch' if infrastructureservices_mirror_fill_failure is search('checksum') else 'file-unreachable'`; the test feeds an invented message (`"The checksum for /x did not match"`).
- **Impact:** If a pinned ansible-core changes its wording, a refusal is quietly misclassified (lifecycle.state vs lifecycle.unknown).
- **Fix:** Pin the real get_url message in a test against the locked ansible-core source, or classify from a structured field. (effort S)
- **Checked:** I found no structured field in get_url's failure result.

##### Y04-09

**Low · Non-duplication · confidence high**

- **Where:** roles/storage_ceph_core/tasks/{apply,destroy,observe}.yml; the same in storage_ceph_smb, replication, consumer and image
- **What:** Each Ceph entry point is the same 52-line run, refuse, fail, complete sequence. The files differ only in the operation literal and the observed/removed flags (shown by diff).
- **Evidence:** `diff apply.yml destroy.yml` gives three changed lines (name, `operation: destroy`, `removed: true`).
- **Impact:** About 15 near-identical files to keep in step. The run-under-no_log-then-judge policy forces the three-task shape but not the copies.
- **Fix:** One private `run.yml`, included with `vars: {operation: …}` from each entry point (private includes are used elsewhere, e.g. storage_ceph_host/prepare.yml:31). test_ceph_refusal_record.py would need to follow the include. (effort M) Label: policy trade-off.
- **Checked:** No structural test forbids private includes. The tests read the entry-point files literally.

##### Y04-10

**Low · Quality · confidence high**

- **Where:** roles/storage_ceph_admin_credentials/tasks/prepare.yml:2-8; update.yml:2-27; plugins/modules/ceph_admin_credentials.py:79
- **What:** Unlike its Ceph siblings, this role asserts neither the digest format nor `ansible_connection == 'local'`, and it does not name a module failure through the protocol's `refused` phase. Its update entry point is named "Prove…" and reports `outcome: unchanged`, while the module returns `changed=params["operation"] == "update"`.
- **Impact:** An update's evidence says it changed nothing, and a module failure reaches Go without a classified reason. Go reads only `proved` and the digest (admin_credentials.go:33,52), so the cost is misleading evidence, not a wrong decision.
- **Fix:** Align it with the sibling preamble, report the module's `changed`, and rename the task. (effort S)

##### Y04-11

**Low · Quality · confidence medium**

- **Where:** roles/controller_prerequisites/tasks/main.yml:8; tool.yml:55
- **What:** The role checks only that `acquisition` and `tools` have the same length. The per-tool lookup `selectattr('source','equalto', item.source.id) | map(attribute='seconds') | first` fails as an undefined-variable error, not a named refusal, when no entry matches.
- **Impact:** A malformed request fails with an opaque templating error and no protocol reason.
- **Fix:** Assert that the set of acquisition sources equals the set of tool source ids. (effort S)
- **Checked:** Digest pinning and the exact byte bound are enforced in controller_files.py:206-216,468-474, and no role in the lane installs a host package (a grep for dnf/package/yum/rpm found nothing).

##### Y04-12

**Low · Efficiency · confidence high**

- **Where:** roles/infrastructureservices_mirror_fill/tasks/apply.yml:87-96,104-113; observe.yml:44-53
- **What:** Every apply relabels the whole `public/mirror` tree with `chcon --recursive`, which includes other Environments' subtrees. Every apply and every observe also re-hashes every mirrored file (up to 128) with SHA-256 after get_url has already checksummed it.
- **Impact:** Repeated full reads and relabels grow with mirror size on each converge or observe.
- **Fix:** Limit the label to this Environment's root, and reuse get_url's checksum result in apply. (effort S)

#### Patterns
- Request validation is uneven between entry points of the same role. Apply asserts a lot, destroy asserts a subset, observe asserts nothing (infra_managed_service/tasks/observe.yml:1-5, infra_artifact_server_nginx/tasks/observe.yml). Argument specs cover types, but the format checks run only on apply.
- Validation is written as dense Jinja regex chains repeated per entry point: the identity regex appears 3× per mirror role (mirror_release apply.yml:6-9, observe.yml:6-9, mirror_images apply.yml:6-8). One validating filter per request kind would remove 9+ copies.
- Defaults files hold multi-clause derived judgments (mirror_release defaults:44-50, infra_managed_service defaults:25-29, mirror_fill defaults:150-164), which hides decisions in variables that are evaluated lazily.
- The pattern of a probe task under `failed_when: false`, a protocol `refused` record, then `fail` is applied consistently (mirror_images apply.yml:29-50, managed_service apply.yml:70-92). This is good, but it is copy-pasted per role.

#### Strengths
- The Ceph argument specs are pinned field-for-field to the Go request types by reflection (internal/storage/ceph/role_admission_test.go:65-123). The 843-line spec is held in step by that test; generating it would add little.
- Every template is rendered twice and compared with a golden (test_role_templates.py:221), and the templates are pure projections of the request with no variant branching beyond the frozen implementation table (vars/main.yml:5-27).
- The process boundary is disciplined: absolute argv, `expand_argument_vars: false`, no shell, and a host-isolated environment for `oc` (mirror_release defaults:21-35, `--insecure=false`).
- Destroy safety: content-root normal form is enforced and tested against planted traversal paths (test_service_destroy_safety.py PLANTED), and parent directories are removed only once empty.
- Secret handling: material crosses only inside module arguments (private_file), every bound-material task is `no_log`, and a test checks that (test_every_task_that_reads_bound_material_hides_its_arguments).

#### Coverage notes
- No test runs a role's task sequence. The tests evaluate individual `when` and assert expressions with Templar (`holds`) and render templates. Ordering across tasks is checked structurally (index_of), not behaviourally.
- Unit-name validation is untested and absent ([Y04-04](#y04-04)): infra_managed_service/tasks/destroy.yml:67.
- dnsmasq and chrony record, source and client injection are untested ([Y04-05](#y04-05)): dnsmasq.conf.j2:29, chrony.conf.j2:15.
- The mirror-fill refusal classification is tested against an invented ansible-core message (test_infrastructureservices_mirror_fill.py:100).
- The SELinux level derivation (defaults/main.yml:17-22) has no determinism or collision test.
- The admin-credentials role has no refusal-path test comparable to test_ceph_refusal_record.py (its ENTRIES list only host and core).
- controller_prerequisites has no test for a tool with no matching acquisition entry (tool.yml:55).

#### Already recorded
- tracked: B431 (bare nested mappings in argument specs; mirror_fill `files` elements are bare dicts in argument_specs.yml:12-15).
- Backlog D251 (19) records get_url as a deliberate choice; [Y04-03](#y04-03) adds the missing size bound as a policy trade-off.
- Audit 003: none of its 320 IDs match a file or symbol in this lane.

<a id="lane-b01"></a>

### Lane B01: Build, release, CI, repository tools and supply chain
Scope read: Makefile, Dockerfile, .dockerignore, .gitignore, the 4 workflows, scripts/{go,goimports,govulncheck,cache-dir,ansible-check,integration-guard,runtime-lock,quick-test,check-commits,hooks/stop-check}, scripts/tools/{ansible_check_bootstrap.py, ansible_test_prepare.py, integration_guard.py, focus.py (lines 90-454), runtime_lock.py (1-300, 440-600)}, scripts/tools/vulngate/{main.go, clients.go}, runtimewheels/main.go, clientassets/{assets.go, assets_test.go, modules.go head}, ansible.cfg, .ansible-lint, both go.mod files, ansible-requirements.txt, both interpreter pins. I also profiled vulnerability-exceptions.json, tooltable.json and lock.json. / Skimmed: tooltable/main.go (function list), ansible_check.py (main, lines 925-970), the test lists of vulngate, runtimewheels and tooltable, docs/development.md (build, container and release sections), knowledge build-toolchain.md / Not read: licenses/*.go in full (I only grepped it for duplicated wheel-license logic and found none), focus_test.py, runtime_lock_test.py and integration_guard_test.py (Go tests run all three), scripts/tools/ansible-test-artifacts.json content. `.yamllint` does not exist. The measured data has no rows for the scripts/tools Go module: no coverage, metrics, clones or signals.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Careful fail-closed tools; a few missing make edges and denylist gaps |
| Security | 4 | Everything downloaded is pinned by digest or SHA and actions by SHA; the release scrub and build context are denylists |
| Efficiency | 3 | The CI cache key misses two pinned inputs; the container context carries 22 GB of local worktrees; govulncheck runs twice per gate |
| Clearness | 3 | 60-line release recipe and 156-line quick-test; tool functions with complexity 18-24 |
| DDD | N/A | Tooling lane |
| Non-duplication | 3 | Verify-and-cache download written five times in two languages; client URL templates exist twice |
| Right language | 3 | The release and container-build recipes are shell programs that parse JSON with sed |
| Test coverage | 4 | Every Go tool and the Python launchers have behavioral tests; the bootstrap and prepare scripts have none; the tools module is outside vet and gofmt |

#### Findings
##### B01-01

**Medium · Quality · confidence high**

- **Where:** .github/workflows/check.yml:42-49, .agents/skills/code-implementation/SKILL.md:56
- **What:** CI runs `scripts/check-commits` only on `pull_request` events. This project integrates locally and pushes main without opening PRs, so CI never enforces the commit gate (Conventional subject, body, `Contract:` line, `!` plus `BREAKING CHANGE` for contract and store-format moves). The skill still says CI runs it.
- **Evidence:** `commits:` / `if: github.event_name == 'pull_request'`; SKILL.md: "CI and a fast-forward run `scripts/check-commits BASE HEAD`."
- **Impact:** a contract move pushed without a `BREAKING CHANGE` marker passes CI. The only enforcement is a procedural step.
- **Fix:** on `push`, also run `./scripts/check-commits "${{ github.event.before }}" "${{ github.sha }}"`, skipping the all-zero `before`. (S)
- **Checked:** no other workflow or Make target runs it. integration_guard.py does not call it. The backlog has no item (B665 and B967 cover other aspects).

##### B01-02

**Medium · Security · confidence high**

- **Where:** Makefile:161, Makefile:190, Makefile:45, Makefile:117, Dockerfile:6
- **What:** `release` checks the origin of only `VULNDB GO STAMP VULNERABILITY_CHECK`, and the native build removes only `GOFLAGS GOEXPERIMENT GOOS GOARCH`. These are denylists. Under `RELEASE_BUILD=container`, `GO_IMAGE` from the environment or the command line silently replaces the compiler image, with no check that it is digest-pinned. A command-line `GO_LICENSES=` reaches the sub-make, so the notices are composed from another table. `GOAMD64` and `GOFIPS140` in the environment, and `GOEXPERIMENT` set with `go env -w` (the go env file, which `env -u` does not clear), still change what the release compiles.
- **Evidence:** `$(foreach variable,VULNDB GO STAMP VULNERABILITY_CHECK,test '$(origin $(variable))' = file || refuse ...`; `env -u GOFLAGS -u GOEXPERIMENT -u GOOS -u GOARCH GOFLAGS=-mod=readonly $(MAKE) ... build`; `$(if $(GO_IMAGE),--build-arg GO_IMAGE='$(GO_IMAGE)')`
- **Impact:** the documented guarantee that "nothing inherited changes what is compiled" (docs/development.md, Releasing) does not hold. A leftover `GO_IMAGE` mirror setting changes the release toolchain without a refusal.
- **Fix:** refuse a release when any variable the release path reads does not come from the Makefile (add `GO_IMAGE`, `GO_LICENSES`, `TOOL_TABLE`, `CONTAINER_SETTINGS`). Run the native build under `GOENV=off` plus an explicit allowlist environment (`env -i PATH=... HOME=...`). If `GO_IMAGE` is allowed, require `@sha256:`. (S-M)
- **Checked:** release_recipe_test.go:413-426 covers only the four variables that are removed. container_build_test.go checks only the default `GO_IMAGE`.

##### B01-03

**Medium · Right language · confidence high**

- **Where:** Makefile:157-218, Makefile:94-126, Makefile:200
- **What:** `release` is a 62-line shell program in one recipe, and `container-build` is 33 lines. They hold selection logic: staged-directory ownership checks, verdict and identity checks, and the SHA256SUMS flow. The release parses the executable's JSON with a greedy sed regex. The output-directory ownership loop is duplicated between the two recipes (lines 99-103 and 179-183). The shell row of the Language table forbids selection logic and parsing in shell. B967 names only quick-test and check-commits as exceptions.
- **Evidence:** `field() { printf '%s\n' "$$identity" | sed -n 's/.*"'"$$1"'":"\([^"]*\)".*/\1/p'; }`
- **Impact:** the parse holds only while the envelope stays compact JSON with one `"version"` key. The tests pin it against a stand-in executable that prints the same compact shape (release_recipe_test.go:31, 99), not against the real `version --output json`.
- **Fix:** move the release checks into a `scripts/tools/release` Go command run as one recipe line. That keeps the "no flag can skip a check" property and decodes JSON properly. (M)
- **Checked:** python.md "Repository tools" and architecture.md:787 (shell row). B967 does not cover the Makefile.

##### B01-04

**Medium · Efficiency · confidence high**

- **Where:** .github/workflows/check.yml:22-25
- **What:** the `.cache` key hashes the Ansible pins and the tools' go.sum, but not `internal/controller/tooltable.json` or `internal/controller/runtimelock/lock.json`. Both caches live under `.cache` (`CLIENTS_CACHE`, `WHEELS_CACHE` via scripts/cache-dir). actions/cache never re-saves on a key hit.
- **Evidence:** `key: check-${{ runner.os }}-${{ hashFiles('scripts/tools/ansible-requirements.txt', ..., 'scripts/tools/go.sum') }}`
- **Impact:** after a client or wheel bump, every CI run downloads the new client releases (helm, kubectl, virtctl, govc: hundreds of MB) and wheels again, until an unrelated Ansible pin changes.
- **Fix:** add both files to `hashFiles`, or split the cache per input. (S)
- **Checked:** Makefile:22 and :53 place both caches under `$(./scripts/cache-dir)`. scripts/cache-dir resolves to `<repo>/.cache` in CI.

##### B01-05

**Medium · Security / Efficiency · confidence high**

- **Where:** .dockerignore:1-24, Dockerfile:16
- **What:** this is still open as audit 003 BLD-01: the build context is a root-anchored denylist. New measured fact: this clone's `.worktrees/` holds 22 GB. It is excluded only through `.git/info/exclude` and is absent from .dockerignore, so `make container-build` and `make release RELEASE_BUILD=container` send all of it, including other branches' `examples/wip` evidence, into the build-stage layer. The release's `tree_clean` uses `git status --porcelain`, which does not list ignored or excluded paths, and the ignored-file check runs only on the native build (Makefile:173-176).
- **Evidence:** `COPY . .`; `du -sh .worktrees` → `22G`
- **Impact:** multi-GB contexts and local evidence in engine storage; this is the remedy BLD-01 already gave.
- **Fix:** switch .dockerignore to an allowlist (`*`, then `!` for the inputs, `.git` included). (S)
- **Checked:** audit 003 BLD-01 records the mechanism. The `.worktrees` size and the container-release path are new evidence.

##### B01-06

**Medium · Quality · confidence medium**

- **Where:** Makefile:294, Makefile:84, internal/controller/runtimelock/embed.go:11
- **What:** `test`, `vet`, `vulncheck` and `vulngate` compile `//go:embed all:wheels`, but only `build` and `execution-contracts-check` depend on `wheels`. runtimewheels writes `.part-*` files into that directory (main.go:192) and removes stale files. Under `make -jN check-go` or `make -j build`, a compile can embed a partial file or run before placement. `make check` itself is mostly protected by job-slot timing, not by a dependency edge.
- **Evidence:** `check-go: wheels fmt-check modules-check tidy-check licenses-check test vet completion-test vulncheck vulngate`; `partial, err := os.CreateTemp(filepath.Dir(path), ".part-*")`
- **Impact:** intermittent failures of the embedding check, or a vulnerability verdict over a tree that is still changing, on parallel or cold-cache runs.
- **Fix:** declare `test vet vulncheck vulngate: wheels`, and write runtimewheels' partial files outside the embedded directory, for example in the cache. (S)
- **Checked:** `all:` embeds dot-files. The `.gitignore` in wheels/ ignores only `*.whl`.

##### B01-07

**Medium · Non-duplication · confidence high**

- **Where:** scripts/tools/runtimewheels/main.go:191-236, scripts/tools/vulngate/clients.go:104-162, scripts/tools/runtime_lock.py:133-149, scripts/tools/ansible_check_bootstrap.py:47-62, scripts/tools/ansible_test_prepare.py:76-91
- **What:** "download within a bound, check size and SHA-256, keep in a cache by digest, rename on match" is written five times. The Go versions differ: runtimewheels checks with `Lstat` for a regular file, vulngate's `openVerified` does not and reads the whole file without a bound. The two Python redirect policies also differ: `PublisherRedirect` allows any origin to go to `*.githubusercontent.com` in the bootstrap (lines 36-39) but only from `github.com` in the prepare script (lines 44-47). `cache_root` is copied in three files.
- **Evidence:** bootstrap `target.hostname == original.hostname or target.hostname.endswith(".githubusercontent.com")` vs prepare `(original.hostname == "github.com" or asset_host(original.hostname)) and asset_host(target.hostname)`
- **Impact:** one trust rule drifts per copy, and a hardening fix lands in one copy only.
- **Fix:** one `verifiedfetch` package in the tools module, and one stdlib-only `scripts/tools/_acquire.py` that the two Python bootstraps import. (M)
- **Checked:** DATA/py-func-name-collisions.tsv lists `cache_root` ×3 and `acquire`/`download`. The Go module is not in the clone data, so I compared the Go copies by hand.

##### B01-08

**Medium · Non-duplication · confidence high**

- **Where:** scripts/tools/clientassets/assets.go:13-24, internal/controller/bundlelocal/tools.go:240-253
- **What:** the publisher URL template of each default client is written once in the tools module, which the gate and tool-table verify, and again in the product, which the controller download uses. No test ties the two together.
- **Evidence:** `"https://get.helm.sh/helm-v" + version + "-linux-amd64.tar.gz"` vs `base, filename = "https://get.helm.sh", "helm-"+withV(version)+"-linux-amd64.tar.gz"`
- **Impact:** if one copy moves to a different file, the vulnerability gate and the tool-table workflow pass on a file the controller never installs. The mismatch then appears only as a runtime SHA failure during setup.
- **Fix:** add a test that renders both templates for each tooltable.json default and compares them, or have the product read one asset table that the tools module embeds. (S)
- **Checked:** assets_test.go checks only shape. bundlelocal/tools_test.go does not mention clientassets.

##### B01-09

**Low · Quality · confidence high**

- **Where:** scripts/ansible-check:9, scripts/tools/ansible_check.py:928-951
- **What:** the bootstrap runs only when `venv/bin/python` is missing. After an `ansible-requirements.txt` bump, every existing venv fails with advice to install manually and set `BOOTWRIGHT_ANSIBLE_CHECK_PYTHON`. Re-running `ansible_check_bootstrap.py`, which pip-installs the new lock, would be the fix. The pinned interpreter version is also hard-coded twice in Python, on top of ansible-check-interpreter.json.
- **Evidence:** `if sys.version_info[:3] != (3, 13, 16):`; `"Install scripts/tools/ansible-requirements.txt into an isolated CPython 3.13.16 environment ..."`
- **Impact:** every lock bump makes each developer's `make check` fail with misleading guidance.
- **Fix:** have the wrapper re-run the bootstrap when a lock file is newer than the venv's marker, and read the version from the JSON pin. (S)
- **Checked:** ansible-check.md:1-23 does not describe refreshing the environment.

##### B01-10

**Low · Quality · confidence high**

- **Where:** Makefile:226-235, Makefile:294
- **What:** `fmt-check` and `vet` cover only the root module (`go list ./...` and `vet ./...` do not descend into the nested scripts/tools module). The tools module, which holds the vulnerability gate, gets tests (Makefile:224) but no vet or gofmt in `make check`. focus.py gofmts changed tools files but vets only quick-test's list. quick-test prints "outside the default build" and tests none of them. No linter beyond vet and gofmt exists; another lane covers that.
- **Evidence:** `directories="$$($(GO) list -buildvcs=false -tags=completion -f '{{.Dir}}' ./...)"`; `$(GO) vet ./...`
- **Impact:** unformatted or vet-failing gate code passes every gate.
- **Fix:** add `$(GO) -C scripts/tools vet ./...`, and the tools module's directories, to fmt-check. (S)
- **Checked:** no test/architecture test runs gofmt or vet on scripts/tools.

##### B01-11

**Low · Efficiency · confidence high**

- **Where:** Makefile:248-249, Makefile:31, Makefile:294
- **What:** `check-go` runs govulncheck twice: `vulncheck` in source mode over `./...`, and `vulngate` over `./cmd/bootwright`. Each loads the vulnerability database and type-checks the module.
- **Evidence:** `./scripts/govulncheck -db "$(VULNDB)" ./...` and `CGO_ENABLED=0 ./scripts/govulncheck -db "$(VULNDB)" -format json ./cmd/bootwright`
- **Impact:** a redundant network-bound step in every gate.
- **Fix:** have one scan over `./...` feed both judgements, or drop `vulncheck` from `check-go` if vulngate's exception logic is the gate of record. (S)
- **Checked:** both are reachable in one `make check`.

##### B01-12

**Low · Security · confidence medium (policy trade-off)**

- **Where:** cmd/bootwright/vulnerability-exceptions.json
- **What:** 89 exceptions: virtctl 74, kubectl 9, helm 5, govc 1; 48 `not_affected` with a justification and 41 `no_fix`. Each has a reason and a `component@version` scope, and every one expires on 2027-02-08. The gate fails on expired, over-90-day and unused exceptions (main.go:199-202, 533-536), so no exception can outlive its scope. Because they share one date, all 89 must be re-decided together, and every default `make build` (VULNCHECK=on) fails that day.
- **Evidence:** `expires [('2027-02-08', 89)]`
- **Impact:** a scheduled break of every build, and a review burst rather than a rolling one.
- **Fix:** stagger expiry by component, or show the next expiry in the scheduled vulnerability workflow's summary 14 days ahead. (S)
- **Checked:** vulngate tests cover expired, too-distant, incomplete and unused exceptions (main_test.go:143, 148).

##### B01-13

**Low · Efficiency · confidence high**

- **Where:** .github/workflows/check.yml:3-5
- **What:** `push` plus `pull_request`, with no `concurrency` group, runs the 90-minute check twice for a PR branch and never cancels superseded runs. `actions/checkout` keeps its default `persist-credentials: true` while `make check` runs repository code.
- **Evidence:** `on:\n  push:\n  pull_request:`
- **Impact:** doubled CI minutes. With `contents: read` the token exposure is small.
- **Fix:** add `concurrency: {group: ${{ github.workflow }}-${{ github.ref }}, cancel-in-progress: true}` and `persist-credentials: false`. (S)
- **Checked:** no secrets are used and permissions are read-only, so forks expose nothing.

#### Patterns
- Release and build hygiene relies on denylists: removed env vars (Makefile:190), origin-checked variables (Makefile:161), .dockerignore. Each new input reopens the hole ([B01-02](#b01-02), [B01-05](#b01-05)).
- One rule written in several languages or modules: verified download ([B01-07](#b01-07)), client URLs ([B01-08](#b01-08)), the interpreter version ([B01-09](#b01-09)), the Go version (go.mod, scripts/go and Dockerfile, though container_build_test pins that one).
- High-complexity tool functions with tests but no decomposition: runtime_lock.py:443 `generate` (complexity 24), :527 `verify` (18), focus.py:119 `unit_tests` (20), focus.py:389 `main` (15).

#### Strengths
- Everything downloaded is pinned: actions by SHA, the Go image and the Trivy image by digest, the interpreter archives by size and SHA-256, wheels with `--require-hashes --only-binary=:all:`, the clients and the virtctl go.mod by SHA. runtimewheels refuses non-pythonhosted lock URLs.
- vulngate fails closed: strict exception decoding, a 90-day maximum validity, unused exceptions fail, OSV paging and short answers are refused, and an unknown client kind is refused.
- The container build passes credentials only as a build secret, from a private temp file removed by a trap. The final stage is `FROM scratch` with three files.
- integration_guard uses a kernel `flock` on the common Git directory, an inherited fd, process-group kill, and strips GIT_* variables.
- The executable composes its own bill of materials, and the release cross-checks the SBOM bytes and the notices digest against the built executable.

#### Coverage notes
- scripts/tools/ansible_check_bootstrap.py:27-62 and ansible_test_prepare.py:31-91: the redirect policy and the verify-then-store path are security-relevant and have no tests.
- The scripts/tools Go module is absent from every measured data file, so its statement coverage is unknown.
- vulngate/main.go:499-506: the refusals for paged and short OSV answers have no test I could find.
- release_recipe_test.go tests the sed parse only against a stand-in's compact JSON; no test runs the real `version --output json` through `field()`.
- quick-test and focus run no tests for a change under scripts/tools/*.go; only `make check` does.
- Release tests do not cover `GO_IMAGE`, `GO_LICENSES` or `GOAMD64` reaching a release ([B01-02](#b01-02)).

#### Already recorded
- audit 003 BLD-01: the context denylist ([B01-05](#b01-05) adds the 22 GB measurement and the container-release path).
- audit 003 BLD-04: met; the Dockerfile is now digest-pinned.
- tracked: B967, quick-test and check-commits as shell exceptions (I confirmed this and did not re-report it).
- tracked: B1339, `make race` exceeds the default 10-minute timeout (Makefile:278 has no `-timeout`).

<a id="lane-e01"></a>

### Lane E01: End-to-end cases, examples and acceptance coverage
Scope read: e2e/README.md, e2e/render.sh, all 8 case-workload files (lab-5-nodes-ceph-networks/app/*, lab-sno-fips-controller/app/{server,verify,verify_test}.py, lab-sno-single-ceph-fdf/app/check.py), cmd/bootwright/e2e_cases_linux_amd64_test.go, e2e_sno_isolation_test.go and the relevant parts of e2e_sno_single_ceph_test.go, and the examples helper and golden tests in admission_acceptance_test.go and golden_linux_amd64_test.go. Skimmed: templates of 9 cases (diffed lab-3-nodes-ceph against lab-5-nodes-ceph-networks, and fips-controller against single-ceph-fdf, plus the whole-tree hash census), 8 example trees (add-ons nmstate/metallb, ceph-clusters libvirt-3-nodes-ceph, openshift lab-sno*, multidc-platform README, lab-kubevirt runbook), runbook tables of all 13 cases, docs/acceptance.md structure, docs/development.md testing sections. Not read: the 8,662 runbook lines beyond tables and spot checks, and the other ~25 example-specific Go tests. The brief says 16 case directories; there are 13, all listed in the README.

#### Scorecard
| Dimension | Score | One-clause reason |
| --- | --- | --- |
| Quality | 3 | Workloads are careful, but render.sh dies silently when `ip` fails, and a few lab fixtures swallow errors |
| Security | N/A | |
| Efficiency | N/A | |
| Clearness | 3 | READMEs and runbooks are structured, but nested charts, an orphan review doc and 3 KB ledger cells hurt |
| DDD | N/A | |
| Non-duplication | 2 | 40% of example YAML lines and 24% of e2e template lines are byte-identical copies; two parallel descriptions of one workload |
| Right language | 3 | Row-conformant, but four Python programs are embedded as strings in a bash script, and no shellcheck or ruff gate covers e2e |
| Test coverage | 3 | Every case render and every example is compiled and planned, but the gate needs ambient tools, some switches never render, and 5 of 6 workload files are untested |

#### Findings
##### E01-01

**Medium · Quality · confidence high**

- **Where:** e2e/render.sh:91
- **What:** The route probe runs on every render and, under `set -euo pipefail`, aborts the script with exit 2 and no message when `ip` fails, even when `host.address` is explicit. The friendly "no default route was found" text in `_helpers.tpl` is therefore unreachable on that path.
- **Evidence:** `detected="$(ip -4 route get 192.0.2.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')"`. Commit a7587c67d states "it exits 2 without output".
- **Impact:** A host with no route to 192.0.2.1 cannot render any case, with no diagnostic.
- **Fix:** Probe only when `host.address` is `auto` (or append `|| true` so the template's `required` reports it), and drop the test's `ip` shim. (S)
- **Checked:** Commit a7587c67d (B1347) fixed only the test side with a shim and says "Production rendering is unchanged". The shim does win the PATH race, so TestE2ECaseRenderingDoesNotReadTheHostsRoutes is not vacuous, but it hides the script defect.

##### E01-02

**Medium · Test coverage · confidence high**

- **Where:** cmd/bootwright/e2e_cases_linux_amd64_test.go:193, cmd/bootwright/e2e_sno_isolation_test.go:46, 80, 144, docs/development.md:243
- **What:** The in-tree gate needs an ambient `helm` and `python3` (plus `git`, via `exampleDirectories`), none of them pinned, while development.md promises tests that are "unitary and host-independent". The project pins helm 4.3.1 for the controller, but these tests use whatever is on PATH.
- **Evidence:** `if _, err := exec.LookPath("helm"); err != nil { t.Fatalf("rendering the e2e cases needs helm on PATH...` and `exec.Command("python3", "-B", "-m", "unittest", "discover", ...)`.
- **Impact:** A clean machine fails `make check` with no documented prerequisite. CI passes only because the ubuntu-24.04 runner image ships helm. A helm major change could shift rendering unnoticed.
- **Fix:** Name helm and python3 in docs/development.md's harness list, and either assert a version floor or run the pinned helm from the tool table. (S)
- **Checked:** Grepped docs/development.md, Makefile, .github/workflows/check.yml and the knowledge index for helm. No mention anywhere, and CI installs nothing.

##### E01-03

**Medium · Test coverage · confidence high**

- **Where:** e2e/README.md:20, cmd/bootwright/e2e_cases_linux_amd64_test.go:40-100
- **What:** The README says the tests list "each service switch its runbook offers", but several runbook switches are never rendered, validated or planned.
- **Evidence:** e2eRenders holds only `"lab-3-nodes-ocp": {{name: "metallb off", ...}}` and `"lab-3-nodes-ocp-features": {{name: "mirror off", ...}}`, so the case default (MetalLB on, mirror on) cannot render without operator pins, and no test supplies placeholders. Entries for lab-sno and lab-network-variants are default only.
- **Impact:** These runbook paths are never rendered by the gate: lab-3-nodes-ocp MetalLB on; features `ipv6`, `proxy.enabled`, `security.fips`, `security.tpm2` and mirror on; lab-sno `addons.metallb`, `addons.nmstate`, `security.fips`, `security.tpm2`; lab-network-variants `bond.mode`, `registration.enabled`, `security.fips`, `security.tpm2`. A template regression there shows up only in the owner's live run.
- **Fix:** Add renders using placeholder pins (as `fipsControllerSubscriptions()` does) for each documented switch, or narrow the README sentence. (M)
- **Checked:** Grepped the other tests for those case names. Only lab-sno-fips-controller has dedicated extra tests.

##### E01-04

**Medium · Non-duplication · confidence high**

- **Where:** e2e/*/templates/ (487 files), examples/ (1,013 YAML files)
- **What:** Every case and example is a full copy with no shared base. A census by whitespace-normalized SHA-1 found 241 of 487 e2e template files byte-identical to another case's file, which is 3,191 redundant lines of 13,107. In examples, 631 of 1,013 YAML files are copies, which is 10,503 redundant lines of 26,370.
- **Evidence:** One `controller.yaml` appears in 14 cases, `artifact-server.yaml` in 14, `lab-bmc-credentials.yaml` in 16 and `_helpers.tpl` in 11. A single `ntp.yaml` appears in 37 examples. `rhel-01/02/03.yaml` differ only in the index, via the one-object-per-file policy. Since 2026-10-01, 191 commits touched e2e/ or examples/ (4,164 file touches). Commit 9ba2ccce1 alone touched 72 e2e files.
- **Impact:** Each API change is hand-edited across dozens of copies. The gate catches stale ones through strict decode and goldens, but the edit cost and drift risk grow with every case.
- **Fix:** For e2e, move the identical infra/ and secret-descriptors/ templates into a shared library chart, or have render.sh overlay `e2e/_common/` under each case. For examples, keep the self-contained copies but generate them from one source. (L)
- **Checked:** Examples must be self-contained by the README's own rule, and the Helm one-object-per-file layout is deliberate. The cost is real, but only e2e has a cheap fix.

##### E01-05

**Medium · Non-duplication · confidence high**

- **Where:** e2e/lab-sno-fips-controller/workloads/, e2e/lab-sno-fips-controller/isolation/templates/applications.yaml:1-45, e2e/lab-sno-fips-controller/app/server.py, values.yaml:72
- **What:** One workload is described twice. The static `workloads/` manifests (20 files, hard-coded CIDRs and names) are applied by the parent runbook. The parameterized `isolation/` chart renders the same objects, and its ConfigMap embeds a copy of the app's HTTP server inside YAML. The isolation runbook says "Do not apply the original `workloads/` tree".
- **Evidence:** workloads/staging/egressfirewall.yaml:17 `cidrSelector: 203.0.113.0/24`, values.yaml:72 "The workload manifests under workloads/ assume these defaults".
- **Impact:** The two copies and their defaults can drift silently. No test reads `workloads/`, since `TestSNOIsolationFixtures` renders only `isolation/`.
- **Fix:** Retire `workloads/` in favour of the isolation chart (the parent runbook can render it), or have a test parse `workloads/` and compare it with the chart's defaults. (M)
- **Checked:** Grepped Go tests and docs for `workloads`. No test or link validates the directory.

##### E01-06

**Medium · Right language · confidence high**

- **Where:** e2e/lab-5-nodes-ceph-networks/app/app-setup.sh:38, 48, 99, 113
- **What:** Four `python3 -I -c '...'` programs (keyring parse, MON address parse, a ctypes `mount(2)` call, config.json writer), about 35 lines in total, are embedded as strings in the bash script and passed positionally through `sys.argv`.
- **Evidence:** `/usr/bin/python3 -I -c '` ... `libc.mount((monitors + ":" + path).encode(), target.encode(), b"ceph", 0, options.encode())`.
- **Impact:** The embedded Python is invisible to any Python linter and unit test, and `sys.argv` indexing is error-prone. No shellcheck or ruff gate covers e2e/ at all; the `# shellcheck source=` directives are written but never run.
- **Fix:** Move the logic into one `attach.py` beside `sample-server.py` that the bash script calls (the row already allows Python files under `app/`). (M)
- **Checked:** The row allows both languages, and the ctypes mount is deliberate so the key stays out of process arguments. The finding is about packaging, not the design.

##### E01-07

**Medium · Test coverage · confidence medium**

- **Where:** e2e/lab-5-nodes-ceph-networks/app/sample-server.py, client.py, e2e/lab-sno-single-ceph-fdf/app/check.py, e2e/lab-sno-fips-controller/app/server.py
- **What:** The only tested workload is `verify.py` (22 unittest cases). The S3 SigV4 signer, the name and body-limit validation in the sample server, and the canary comparison in `client.py` have no tests.
- **Evidence:** grep for `sample-server|client.py|check.py` in `*_test.go` and `*.py` tests finds no test file.
- **Impact:** The acceptance rows (for example "44/44 passed") rest on these programs, so a comparison bug could produce a false PASS. Teardown behavior is tracked separately in B1354.
- **Fix:** Add a stdlib unittest for `authorization()` against the AWS SigV4 reference vector, `_target`/NAME refusals, and `canary()` bytes. Wire it like `verify_test.py`. (S)
- **Checked:** Not covered by pycov, which measures only the collection.

##### E01-08

**Low · Quality · confidence medium**

- **Where:** e2e/lab-sno-fips-controller/app/verify.py, e2e/lab-sno-fips-controller/app/verify_test.py, specs/architecture.md:789, cmd/bootwright/e2e_sno_isolation_test.go:80, 144, cmd/bootwright/e2e_sno_single_ceph_test.go:138
- **What:** The "Case workload" row excludes a file that "is read by a render or a test", yet three Go tests execute `verify.py` and run `verify_test.py`. The architecture test maps files by shebang and extension only, so it cannot enforce that exclusion.
- **Evidence:** `exec.Command("python3", "-B", filepath.Join(root, "app", "verify.py"), "--config", configPath, "--mode", "config")`.
- **Impact:** The row and the practice disagree. Meanwhile `verify.py` (371 lines, with its own validator) is a real tool that other cases lean on.
- **Fix:** Amend the row to allow a tested verifier, or move the verifier and its test out of `app/`. (S)
- **Checked:** language_rows_test.go:160-200 checks mapping only.

##### E01-09

**Low · Quality · confidence medium**

- **Where:** e2e/lab-5-nodes-ceph-networks/app/sample-server.py:43-52, 246-251
- **What:** `_health` guards `backend.ready()` with `except OSError`, but `source_address()` (a UDP `connect`) is called unguarded. An unreachable MON or VIP raises, and the handler drops the connection instead of returning the intended 503 with detail.
- **Evidence:** `sources["mon"][monitor] = source_address(monitor)`; the function does `probe.connect((destination, 9))` with no handler.
- **Impact:** In a case whose purpose is proving which source address reaches which network, a segregation failure surfaces as a bare transport error (`client.py` gets status 0).
- **Fix:** Catch `OSError` per target and record the error text, as `ready` already does. (S)
- **Checked:** client.py's health path would parse the JSON only for statuses 200 and 503, so the failure mode is a bare FAIL.

##### E01-10

**Low · Non-duplication · confidence high**

- **Where:** e2e/lab-5-nodes-ceph-networks/app/sample-server.py:81, e2e/lab-sno-single-ceph-fdf/app/check.py:18, ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_object.py:104
- **What:** SigV4 signing is implemented three times, twice standalone in e2e (nearly the same canonical-request code) and once in the collection.
- **Impact:** Low drift risk in lab fixtures. The standalone copies are forced by "target stdlib only", but the two e2e copies could share one file.
- **Fix:** Have `check.py` and `sample-server.py` draw on one shared `sigv4.py` bundled by each runbook, or note the intentional copy in a README line. (S)
- **Checked:** The Case workload row bars imports from the collection, so the collection copy cannot be reused.

##### E01-11

**Low · Efficiency · confidence high**

- **Where:** cmd/bootwright/admission_acceptance_test.go:47, 55-60, cmd/bootwright/e2e_cases_linux_amd64_test.go:151-157, 216-292
- **What:** `exampleDirectory(t, name)` calls `exampleDirectories(t)`, which execs `git ls-files` over all of examples/ on every call. Nine test files each loop over all 39 examples and call it per example, with 80 direct calls besides. The e2e tests re-render each of 42 renders three times (valid, plan, reservation) via helm, and plan each render twice.
- **Impact:** Roughly 400 git spawns and about 130 helm runs per `make check`, repeating the same work.
- **Fix:** Memoize `exampleDirectories` once (sync.Once) and render each case once per `go test` process. (S)
- **Checked:** No cache exists (grep for `sync.Once` in those files). I did not measure wall time.

##### E01-12

**Low · Clearness · confidence medium**

- **Where:** e2e/lab-sno-fips-controller/app/verify.py:233, 285
- **What:** `for phase in ("before", "after")` with a `break`: positive checks run, the negatives run, then the positives run again, but nothing changes between "before" and "after" and no rationale is given. A newcomer reads it as a lifecycle phase.
- **Evidence:** `for phase in ("before", "after"):` ... `if phase == "after": break`.
- **Fix:** Name it (for example "positives hold after the negative probes") or restructure as `positives(); negatives(); positives()`. (S)
- **Checked:** There is no comment or spec sentence explaining it.

##### E01-13

**Low · Clearness · confidence high**

- **Where:** e2e/lab-sno-fips-controller/api-review.md, e2e/README.md:6-10, e2e/lab-sno-single-ceph-fdf/app/Chart.yaml, cmd/bootwright/e2e_sno_single_ceph_test.go:106
- **What:** `api-review.md` is referenced from nowhere, and its 47 lines hold a point-in-time review inside a case directory. The README's case layout (values/templates/runbook) omits the nested Helm charts (`isolation/`, `app/`, `platform/`) in 2 cases, and the single-ceph-fdf test renders the fips case's `isolation` chart with the fdf case's values (a cross-case coupling).
- **Impact:** Mild orientation cost; a reader cannot tell the review is historical.
- **Fix:** Delete or link `api-review.md`, and document the nested-chart layout in the README. (S)
- **Checked:** Grepped the repo for `api-review.md` (hits are other documents).

##### E01-14

**Low · Clearness · confidence medium**

- **Where:** cmd/bootwright/admission_acceptance_test.go:128-160, 175-190
- **What:** `admissibleExampleSources` derives the multidc projection with raw string surgery on YAML (`strings.Cut(body, "    fdf:\n")`, `mustReplaceOnce`, hard-coded `dropped != 14`). It also drives the golden for the one tracked example that cannot compile (104 files).
- **Impact:** It is brittle to any reformatting of the example, though the guards fail loudly.
- **Fix:** Express the projection as a structured edit, or retire the unapplyable example into a testdata fixture. (M)
- **Checked:** The guards (`mustReplaceOnce`, the count) make it fail closed.

##### E01-15

**Low · Clearness · confidence high**

- **Where:** docs/acceptance.md:150-173
- **What:** Ledger rows run up to ~3,200 characters inside one Markdown table cell (11 of 22 exceed 1,500), mixing outcome, deviations, numbered findings and digests.
- **Impact:** Hard to diff, review or scan; a row cannot be linked part by part.
- **Fix:** Keep the table to one summary line per run and move deviations to a per-row subsection. (M)
- **Checked:** `docs/acceptance.md` is the declared row format owner; an architecture test pins the columns only.

#### Patterns
- **What the gate proves.** `TestEveryExampleEffectiveStateMatchesItsGolden`, `TestEveryExampleBlockIdentityKeepsItsBytes` and `TestEveryExampleNICDerivationMatchesItsGolden` compile and plan every one of the 39 examples. Another seven tests add claims, schema, edge catalog and per-profile cases. `TestEveryE2ECaseRendersToValidInput`, `...RenderPlansWithoutRefusal` and `...ContextsClaimDisjointHostReservations` render, validate, plan and compare reservations for all 13 cases (42 renders), each with a golden-style guard that fails on a new unlisted case. Only a live run proves the installer, BMC emulation, guest boot, Ceph health and the workload behavior (client round trips, source addresses, isolation probes).
- **Coverage matrix.** libvirt: all 13 cases plus most examples. Bare metal and BMC: examples only (lab-baremetal, physical-3-nodes-ceph, lab-physical), with Redfish emulated in every e2e case and real hardware an owner gate (B73). KubeVirt: examples only (lab-kubevirt, -external, -nested), with a runbook in examples/ rather than an e2e case. vSphere: neither an example nor a case (the arm refuses; the lab-vsphere profile is planned under M3f). Infrastructure services: one `lab-services` example, and every case embeds them, with no standalone case. Managed mirror hosts (SSH, installed): examples only. OpenShift: SNO (lab-sno, fips-controller, single-ceph-fdf, ceph-df) and compact (lab-3-nodes-ocp, -features); examples without a case are anysize, lab-networks, lab-kubevirt* and lab-physical, and the lab-disconnected profile has neither. Ceph: five cases plus `lab-3-nodes-ceph`'s service toggles; the 2x4 multisite, pools (P1), observability (P3) and disconnected (P6) profiles are examples without a case. Add-ons: nmstate and metallb are in cases, fdf in two, and compact-hub (ACM, GitOps) and authored ClusterAddon are examples only.
- **Examples are validated against the current API in the gate.** `exampleDirectories` walks `git ls-files examples` (ignoring examples/wip), asserts each has a tracked environment.yaml, and the golden test fails on an orphaned golden. All 39 README rows match the 39 tracked directories; no orphaned YAML. The one out-of-pattern file is lab-kubevirt/runbook.md, which is explained in its own opening.
- **Standing-rule cost (compare three cases).** lab-3-nodes-ceph vs lab-5-nodes-ceph-networks share `controller`, `artifact-server`, `ntp`, `proxy`, `bmc-credentials` and the OS templates byte for byte, and differ in machines, networks and storage. fips-controller vs single-ceph-fdf `bootstrap/` differ in 6 of 13 files, mostly by a few lines. Examples are the same, for example `add-ons/lab-sno-nmstate` vs `lab-sno-metallb` differ in only 3 files.
- **Render script.** `set -euo pipefail`, quoting, argument validation, `mktemp` plus `trap`, a repo-refusal guard and a "only replace an earlier render" marker are all good. The `[[ -L "$abs_out" ]]` check at render.sh:75 is dead because `realpath -m` has already resolved symlinks, and the script is GNU/Linux-only (`realpath -m`, `ip`), which the Case renderer row accepts.
- **Python 3.9 floor and secrets.** All workload files parse under 3.9 syntax (no `match`, `|` types, `:=`). I found no literal credential in e2e/ or examples/. S3 keys come from `s3.json` (0600), a Secret or the environment, and the CephFS key is passed to `mount(2)` in-process rather than as an argument.

#### Strengths
- Every case and every example is held by a fail-closed test that also fails on a new unlisted case or orphan golden.
- `verify.py` is a model workload: bounded subprocess output, a private snapshot with symlink refusal, offline `--mode config`, explicit blocked-versus-error classification, and 22 failure-path unit tests.
- `render.sh` and the evidence-preservation block are defensive: private umask, repo and symlink guards, hash verification of archives.
- `-update` goldens plus an orphan-golden guard keep 580 KB of effective-state goldens maintainable.
- Runbooks differ meaningfully: only about 6% of runbook lines (558 of 8,662) sit in 8-line windows repeated across runbooks.

#### Coverage notes
- e2e/lab-5-nodes-ceph-networks/app/sample-server.py:81 (SigV4) and :200-232 (PUT/DELETE limits and NAME): untested, and acceptance evidence trusts them.
- e2e/lab-5-nodes-ceph-networks/app/client.py:104-124 (`canary`): a wrong comparison would give a false PASS.
- e2e/lab-sno-single-ceph-fdf/app/check.py: persistence and object checks, untested.
- e2e/lab-sno-fips-controller/workloads/: 20 manifests with no test of any kind.
- cmd/bootwright/e2e_cases_linux_amd64_test.go: the unrendered switches in [E01-03](#e01-03), and no test that mutates `host.address` to check the route-probe path ([E01-01](#e01-01)).
- cmd/bootwright/e2e_sno_isolation_test.go:68, e2e_sno_single_ceph_test.go:124: chained unchecked `.(map[string]any)` assertions panic with no message if a chart's shape changes.

#### Already recorded
- B1354 (teardown removes a firewall rule it did not create; e2e/lab-5-nodes-ceph-networks/app/app-teardown.sh): open, in specs/milestones/backlog.md.
- B1347 (host-route dependence of the e2e render in the gate): delivered, test-only. [E01-01](#e01-01) is the production-side remainder.
- audits/003-code: no ID matched these e2e or examples files.

<a id="lane-x01"></a>

### Lane X01: Cross-cutting security, Go side (process execution, filesystem safety, privilege, network clients)
Scope read: every launch site in the 18 production files that import `os/exec`; `internal/nativeexec/executable.go`; the bounded writers (18 custom `Write` methods); the RemoveAll and rename/durable-write sites; the network clients (`bundlelocal/acquisition.go`, `mirror.go`, `medialocal/acquirer.go`, `acquisition/transport.go`); the unsafe syscall sites in contextfs and cmd/bootwright; the ignored-result and deferred-Close hits / skimmed: contextfs `files_`/`publication_` kernels, hostdnf tests, storage/preflightlocal walkers, lifecycle `logging.go` / not read: the bodies of the contract-suite packages (`*contract/contract.go`). They are test-support packages imported only by `_test.go` files, so their 66 `context.Background` hits are out of production scope.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | Supervision is careful and documented; one io.Writer contract break produces a real hostdnf defect |
| Security | 4 | Every launch uses an absolute or qualified executable, an explicit environment and no shell; TLS is ≥1.2 and verified; private modes are checked exactly |
| Efficiency | N/A | |
| Clearness | N/A | |
| DDD | N/A | |
| Non-duplication | 3 | About 11 process supervisors, 9 bounded writers with 3 overflow contracts, 5 descriptor walkers, 2 renameat2 copies, 2 transport-failure classifiers |
| Right language | N/A | |
| Test coverage | 3 | The writers and runners in several adapters are 0%; test doubles replace `execute`, so pipe semantics are never exercised |

#### Findings
##### X01-01

**Medium · Quality · confidence high (mechanism) / medium (exposure)**

- **Where:** internal/controller/hostdnf/packages_linux_amd64.go:461-468, :202, :224; internal/controller/hostdnf/removal_linux_amd64.go:124
- **What:** `capped.Write` truncates and then returns the shortened length with a nil error. That is a short write. os/exec's copier (`io.Copy`) stops with `io.ErrShortWrite` and closes the pipe (`pr.Close() // in case io.Copy stopped due to write error`, go1.26.9 os/exec/exec.go:603). dnf then gets EPIPE/SIGPIPE, and the install and removal previews, which are designed to return `Truncated: true`, become "dnf could not preview the install" failures.
- **Evidence:** `p = p[:max(room, 0)]` … `return len(p), nil`; `preview := prerequisites.HostPackagePreview{Text: text, Truncated: stdout.truncated}`
- **Impact:** A dnf preview larger than about 64 KiB (several hundred packages on a minimal host) fails setup or uninstall instead of showing a truncated preview. The outcome depends on timing for outputs between 64 and about 128 KiB.
- **Fix:** Return the original `len(p)` after truncating (drain and flag), as `privilege.limitedOutput`, `comparisonlocal.boundedBuffer` and `sshlocal.boundedOutput` already do. Add an `execute` test that runs a real binary into `capped` with output over the bound. (effort S)
- **Checked:** The "long and hostile" test (packages_linux_amd64_test.go:221-227) and removal_linux_amd64_test.go:88-98 pass only because `packagesWith` replaces `p.run` with a double (:67). The real `execute` tests (:543-554) use `bytes.Buffer`. `hostowned.bounded` (probe_linux_amd64.go:35-43) has the same short write, but it is harmless there because `stdout.overflow` is checked before `err`. No B-item or audit-003 entry matches.

##### X01-02

**Medium · Non-duplication · confidence high**

- **Where:** hostowned/probe_linux_amd64.go:35; hostdnf/packages_linux_amd64.go:461; firewalllocal/native.go:18; containercluster/preflightlocal/resolver.go:18; storage/preflightlocal/capture.go:13; privilege/sudo.go:373; comparisonlocal/helper.go:249; selectionfs/helper_linux_amd64.go:139; sshlocal/launcher_linux_amd64.go:179; venvlocal/build_linux_amd64.go:301
- **What:** The duplication itself is already reported ([T02-05](#t02-05), [G06-04](#g06-04), G21 patterns). This lane adds the horizontal fact: the 10 bounded writers follow three incompatible overflow contracts.
- Drain and flag (5): privilege, comparisonlocal, selectionfs, sshlocal, venvlocal.
- Error, which kills the child by EPIPE (3): firewalllocal, containercluster resolver, and storage's *exported* `preflightlocal.Capture`, which no sibling reuses although it has the same 64 KiB default.
- Short write with a nil error, which breaks the io.Writer contract (2): hostowned, hostdnf.
- **Evidence:** `type capture struct{ bytes.Buffer }` is verbatim in firewalllocal and preflightlocal, while `storage/preflightlocal.Capture` with `MaxOutput = 64 << 10` already exists.
- **Impact:** Each copy picks its contract by accident, and [X01-01](#x01-01) is the first one to break.
- **Fix:** Put the [T02-05](#t02-05) `nativeexec.Run` in place with one drain-and-flag writer, and let callers choose "refuse on overflow" as a parameter. (effort M)
- **Checked:** Every hit was opened. 5 of these Write methods are at 0% coverage (gocov-any-funcs).

##### X01-03

**Low · Security · confidence medium**

- **Where:** internal/nativeartifacts/comparisonlocal/service.go:128-138; firewalllocal/native.go:30; preflightlocal/resolver.go:32; privilege/nss_linux_amd64.go:94
- **What:** Only 3 of the 11 supervisors put the child in its own process group and kill the group: adapterprotocol (:63), hostdnf (:508) and hostowned (:55). The comparison tool is operator-chosen (for example `git diff --no-index`, `meld`). On cancel it gets SIGTERM to the leader only, so any descendants outlive the command. WaitDelay bounds only Bootwright's wait.
- **Evidence:** `command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }` with `SysProcAttr = processAttributes(credential)` (no Setpgid)
- **Impact:** An interrupted `diff` can leave the tool's children running under the invoking account. getent and firewall-cmd spawn none, so they are not affected.
- **Fix:** Add `Setpgid` and a group kill to `toolCommand`. Name the process-group rule in go.md so every new runner states it. (S)
- **Checked:** venvlocal's missing group and WaitDelay are already [G07-01](#g07-01). privilege's relay deliberately signals sudo, which forwards to its child (process_linux_amd64.go:303-306).

##### X01-04

**Low · Quality · confidence medium**

- **Where:** internal/controller/ansiblelocal/runner_linux_amd64.go:70, :107-116
- **What:** The root controller Ansible run puts its scratch in the shared sticky `/var/tmp` and removes job and scratch with deferred, unchecked `os.RemoveAll`. It has no leftover sweep. The sibling ansiblerunner keeps scratch under a proved 0700 parent (scratch_linux_amd64.go:15, :26), removes it through the bounded, same-device `emptyTree` and sweeps leftovers before each run.
- **Evidence:** `processBoundary{owner: 0, jobParent: "/run", scratchParent: "/var/tmp", ...}`; `defer os.RemoveAll(scratch)`
- **Impact:** A killed `controller install` leaves `bootwright-scratch-*` in /var/tmp until tmpfiles ages it out, and a failed removal is silent.
- **Fix:** Reuse ansiblerunner's private scratch parent and sweep, or at least report a RemoveAll failure. (S/M)
- **Checked:** Go's RemoveAll is openat- and no-follow-based, so a symlink cannot redirect it. The other 9 RemoveAll targets are directories that the same function created with MkdirTemp or Mkdir (sshlocal :206/:436, venvlocal :133/:152, ansiblerunner scope :65), or the scratch-prefix-validated path in comparisonlocal/helper.go:165, which runs unprivileged. No report mentions it.

##### X01-05

**Low · Non-duplication · confidence high**

- **Where:** storage/preflightlocal/observer_linux.go:157-176 `openPathSafe`; storage/preflightlocal/reuse_linux.go:208-232 `openSafe`; controller/hostlinux/files_linux_amd64.go:55-90; nativeartifacts/renderlocal/tree.go:114-169; workspace/contextfs/files_linux_amd64.go:193 (openat2)
- **What:** Five hand-written "open each component with O_NOFOLLOW from /" walkers exist, two of them near-identical in one package (only `O_PATH` versus `O_RDONLY|O_NONBLOCK` differ). Alongside them, `os.Root` is used in stagelocal and ansiblerunner (30 hits) and raw `openat2` in contextfs. These are three generations of one idea, not one design.
- **Evidence:** both walkers do `unix.Open("/", …)` then `for i, part := range parts { flags := … | unix.O_NOFOLLOW …; next, err := unix.Openat(descriptor, part, flags, 0)`
- **Impact:** Each walker must be reviewed separately, and two security-relevant ones (`readRootProof`, `processCredit`) are at 0% coverage (G20 coverage note).
- **Fix:** One `nativefs.OpenNoFollow(path, flags)` helper on openat2 `RESOLVE_NO_SYMLINKS` (contextfs already proves the kernel path), used by the storage and hostlinux walkers. (M)
- **Checked:** All are correct individually (G20 praises preflightlocal). os.Root follows in-root symlinks, so it is not a drop-in for no-follow walks.

##### X01-06

**Low · Security · policy trade-off · confidence high**

- **Where:** internal/workspace/contextfs/files_linux_amd64.go:105-106, :193, :880-885; controller_uninstall_linux_amd64.go:681-694; logs_linux_amd64.go:199; deletion_linux_amd64.go:299
- **What:** contextfs hand-codes `renameat2` twice (`renameNoReplaceAt`, `renameNoReplaceIn`), `openat2`, `symlinkat` and `unlinkat` through `syscall.Syscall6` with `unsafe.Pointer` and amd64 trap numbers. `golang.org/x/sys/unix` (already a module dependency) provides typed `Renameat2`, `Openat2`, `Symlinkat` and `Unlinkat`.
- **Evidence:** `openat2Trap = 437`, `renameat2Trap = 316`; `syscall.Syscall6(renameat2Trap, parent.file.Fd(), uintptr(unsafe.Pointer(oldPointer)), …)`
- **Impact:** Magic numbers, two copies, and 4 of the 6 `unsafe` imports exist only because the effect allow-list (test/architecture/boundaries_test.go:515) admits a few named x/sys members.
- **Fix:** Admit `unix.Renameat2`, `unix.Openat2`, `unix.Symlinkat` and `unix.Unlinkat` for contextfs and delete the traps and the second renameat2. (S)
- **Checked:** The cmd/bootwright ioctl/poll uses (stdin_linux_amd64.go) are the remaining unsafe sites and are justified the same way. The uses are correct (conversion inside the call, KeepAlive).

##### X01-07

**Low · Non-duplication · confidence high**

- **Where:** internal/acquisition/transport.go:26-56; internal/managedos/medialocal/acquirer.go:270-345
- **What:** Two classifiers turn net/TLS/DNS errors into operator text. medialocal's `untrusted` names the reason (expired, hostname), and `timedOut` walks the chain. acquisition's `TransportFailure` does neither. The two give different words for the same certificate failure.
- **Evidence:** `condition = "presented a certificate the qualified system trust store does not accept"` against `" presented a certificate this host's system trust store does not accept"+reason`
- **Impact:** The same fault reads differently in setup and in media add.
- **Fix:** Move `untrusted` and `timedOut` into `internal/acquisition` and let medialocal add only its proxy-route remedy. (S)
- **Checked:** medialocal does not import acquisition. The bundle, mirror and anchored-dial clients do.

##### X01-08

**Low · Quality · confidence medium**

- **Where:** cmd/bootwright/wiring_account.go:60, :72
- **What:** `home()` and `uid()` resolve the account with `context.Background()`, because the port has no ctx. This is the same pattern [G02-05](#g02-05) reported for comparisonlocal/artifacts.go:11.
- **Evidence:** `account, err := a.resolver.Resolve(context.Background())`
- **Impact:** Ctrl-C does not cancel a slow NSS lookup. It is bounded by `nameServiceBound` (nss_linux_amd64.go:34, :93).
- **Fix:** Thread ctx through the closure ports. (S)
- **Checked:** The other production Background uses are main.go:18/24/27 (the root, legitimate). All 4 `WithoutCancel` uses are cleanup or recording, and each is bounded: venvlocal discard, comparisonlocal remove under `helperDeadline` (helper.go:75), lifecycle `recordingContext` for log/outcome records, kubevirt revoke under `WithTimeout`.

#### Patterns
- Process execution verdict: 14 `CommandContext` + 1 `exec.Command` + 2 `exec.Cmd` literals across 11 supervisors. All use an absolute or `nativeexec.Qualify`'d executable or a fixed search path with no ambient PATH, so ErrDot cannot arise. All set an explicit environment and no shell. All but venvlocal ([G07-01](#g07-01)) set WaitDelay. 3 of 11 kill the process group ([X01-03](#x01-03)), 2 of 10 writers short-write ([X01-01](#x01-01), [X01-02](#x01-02)). The context-free `exec.Command` in `runRelayed` (privilege/process_linux_amd64.go:308) is justified by its comment and the watcher goroutine.
- Filesystem verdict: of the 23 Clean/EvalSymlinks hits, 18 are lexical canonical-form validations before descriptor I/O (cosmetic). 5 are resolve-then-check decisions: privilege :68, venvlocal walk :15/:31, renderlocal :40, desiredstate/compilation/selection.go:88. The first four are safe because they also require root-owned, non-writable ancestors, and the last is an authoring invariant, not a boundary. All 8 "permissive mode" hits are exact `!= 0600/0700` refusals. All 10 RemoveAll targets are proved ([X01-04](#x01-04) notes the one gap).
- Durable writes: one kernel (contextfs `publishStage`/`renameStage`, file and directory fsync, stage discarded by identity) plus 5 local copies. renderlocal (files.go:105-122) and selectionfs (:110-139) fsync the file and the directory. venvlocal/fetch.go:163 skips the directory fsync (a re-verifiable cache, acceptable). The ansiblerunner record (jobs:137) lives on /run tmpfs (justified).
- Network clients: both `http.Client`s bound dial, handshake and header time. The bundle client limits redirects to 5 within the approved origin and caps headers at 64 KiB. The media client refuses every redirect and caps the transfer at 6 h. All three `tls.Config`s set MinVersion 1.2 with system or anchored roots; there is no InsecureSkipVerify. Under a proxy, `AnchoredDial` is bypassed, but the transport's RootCAs is the anchor pool cloned from system roots and redirects are same-origin, so this is safe.
- Ignored errors (all 64 checked, 45 of 171 deferred Closes sampled): there is no unchecked write, sync or chmod on a file whose durability matters. The three writable-file deferred Closes (controller_bundles :404, files :790, selectionfs :102) all `Sync` and check it first. `_ = parent.file.Sync()` in `discardCreated` (files:848) is deliberate cleanup. The `_ = boundary.append` calls latch faults in `logBoundary.fail`.
- unsafe (6: ioctl, poll, raw *at syscalls; see [X01-06](#x01-06)), panic (2: duplicate schema registration and the built-in add-on catalog, both shipped constants), reflect (16: mostly DeepEqual record comparisons and canonicaljson size prediction), os.Exit (1, in main), ambient environment (12, all in the composition root or the sudo boundary; sshlocal filters `os.Environ` through an allowlist). All are justified.

#### Strengths
- `nativeexec.Qualify` (94% own coverage) is the single executable-identity rule: absolute path, root-owned and non-writable up to `/`, used by 7 adapters.
- Every child gets an explicit environment and `Dir="/"`. Helpers that drop root use `Credential` plus `Pdeathsig` and document the thread caveat (golang/go#27505).
- adapterprotocol's supervisor (process group, parent-death signal, bounded drain, exit classification) is the model the other runners should converge on.
- Private-file checks compare exact modes and identities. Publication goes through descriptors with renameat2 NOREPLACE and directory fsync.

#### Coverage notes
- hostdnf `execute` is never run with `capped` or `tail` over a real pipe ([X01-01](#x01-01)). hostowned `execute` and `bounded.Write`, firewalllocal `command`, preflightlocal `Read` and venvlocal `runCommand` are all at 0%.
- comparisonlocal/helper.go:87 `spawnHelper` 0% and selectionfs/helper_linux_amd64.go:67 `Store.perform` 19%: these are the root-to-invoking-account privilege drops.
- bundlelocal/acquisition.go:22 `acquisitionClient` 0%: the production construction of the egress client (proxy plus qualified roots) is untested. Only `routedClient` is tested.
- storage/preflightlocal/reuse_linux.go:133/:235 at 0% (already in the G20 coverage notes).

#### Already recorded
- [T02-05](#t02-05), [G06-04](#g06-04), G21 patterns: duplicated bounded process envelope ([X01-02](#x01-02) adds the overflow-contract classification).
- [G07-01](#g07-01): venvlocal runCommand has no WaitDelay or process group.
- [G07-05](#g07-05): four copies of the root-owned ancestor rule.
- [G07-07](#g07-07): sudo argv without `--`.
- [G02-05](#g02-05): comparisonlocal Resolve uses context.Background.
- [G02-06](#g02-06): adapterprotocol group kill after reaping.
- [G07-04](#g07-04), [G11-03](#g11-03), [G09-04](#g09-04): duplicated bounded tree removers.
- [T01-03](#t01-03): effect boundary misses method calls on values; [T01-06](#t01-06): no errcheck or gosec gate.

<a id="lane-x02"></a>

### Lane X02: Cross-cutting security: secret and credential flow from custody to target
Scope read: I read these in full or in the relevant ranges: ansiblerunner process_linux_amd64.go (1-330), runner.go (100-203), outputs_linux_amd64.go, jobs_linux_amd64.go (100-190); adapterprotocol process_linux_amd64.go (86-132); ansible/ansible.cfg; plugins/callback/censored.py; modules infrastructureservices_mirror_image.py (154-260), infrastructureservices_mirror_release.py (147-155, 350-520), image_acquire.py (110-180); module_utils redfish_control.py (125-330), tls_trust.py, infra_service.py, artifact_server.py, ceph_core.py, ceph_native_object.py, ceph_native_observability.py, kubevirt_api.py, ceph_host_route_probe.py; roles containercluster_media_agent build.yml and publish.yml, managedos_install_anaconda register.yml, substrate_libvirt_machine apply.yml (228-262), substrate_kubevirt_machine power.yml; installation encryption.go (108-160) and the kickstart.go credential lines; security.md (140-200, 965-1006); secrets.md (165-180); managed-os.md (1346-1390).
Skimmed: the signal lists (TLS 13, url opener 9, tempfile 31, eval/exec 8, ambient env 15 Python and 12 Go, broad except: 30 of 88 opened); every role task file, mechanically. I parsed all roles/*/tasks/*.yml with a read-only inline script for file lookups without no_log, and for `failed_when: false` under no_log with no judgement.
Not read: secrets/custody and the material internals, lifecycle secret_holds.go and admin_credentials. Lanes G10 and G12 own them, and I only confirmed that the Clear calls exist.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 4 | the runner's material lifecycle is rigorous; one systematic whitespace-truncation defect in how roles read passwords |
| Security | 4 | every class traced reaches its target through files, stdin or a pinned or verified channel; residual risk is crash leftovers on SSH targets |
| Efficiency | N/A | |
| Clearness | N/A | |
| DDD | N/A | |
| Non-duplication | N/A | |
| Right language | 4 | secrets cross into Python only as lent-file paths or no_log module arguments; one Python credential program sits in YAML ([Y02-03](#y02-03)) |
| Test coverage | 4 | the censoring callback, pipelining, the material-value refusal and the libvirt hash replay are pinned; nothing pins lent-file read fidelity |

#### Findings
##### X02-01

**Medium · Quality · confidence high**

- **Where:** roles/substrate_baremetal_machine/tasks/boot_media.yml:17, roles/substrate_libvirt_machine/tasks/apply.yml:232 and :242, roles/substrate_libvirt_machine/tasks/power.yml:11 (27 password lookups in all)
- **What:** A password Secret part is admitted as "one nonempty UTF-8 line", so leading and trailing spaces are legal (material/service.go:395-399; secrets.md:172). But 27 role lookups of a lent password file use `lookup('ansible.builtin.file', …)` with the default `rstrip=True`. That silently drops trailing whitespace before the value reaches Redfish, libvirt or sushy. Fifteen other lookups pass `rstrip=false` on purpose (build.yml:90-92; infra_artifact_server_nginx apply.yml:124-127), so the rule exists but is not applied evenly. The libvirt BMC htpasswd program then hashes `rest.strip()`, which strips both ends, while its clients strip only the right end.
- **Evidence:** `password: '{{ lookup("ansible.builtin.file", substrate_baremetal_machine_boot_password) }}'`. In the libvirt program: `password = rest.strip()`.
- **Impact:** a BMC or libvirt password whose last character is whitespace sends a different credential from the one stored (an authentication failure that names no cause). A leading space makes the emulated BMC's hash disagree with what its own client sends.
- **Fix:** use `rstrip=false` on every lent secret-file lookup, and have the bcrypt program strip only the one separator newline. Add a role-boundary test that refuses a password or token lookup without `rstrip=false`. (S)
- **Checked:** Go admission (`line()` rejects only CR, LF and NUL; `username()` alone rejects whitespace). No backlog, audit-003 or lane entry mentions rstrip. [Y02-03](#y02-03) reports the embedded program's language, not this mismatch.

##### X02-02

**Medium · Security · confidence medium**

- **Where:** plugins/modules/infrastructureservices_mirror_image.py:196-197 and 249-250; image_acquire.py:150-152 and 168-169
- **What:** On an SSH-placed mirror host, these modules write the registry `auth.json` (a base64 username:password) under `module.tmpdir`. That is beneath `ANSIBLE_REMOTE_TEMP=<job>/remote` on the target, and they remove it only in a Python `finally`. If the adapter is killed, ssh drops, the module takes SIGHUP and the `finally` never runs. Nothing on the target ever sweeps that directory. The sibling module mirror_release has exactly this sweep: `sweep()` at mirror_release.py:394 removes stale lent secrets on the next run.
- **Evidence:** `scratch = tempfile.mkdtemp(prefix="bootwright-mirror-", dir=base)` / `authfile, certificates = lend(scratch, …)`. `base` is `module.tmpdir`.
- **Impact:** a mirror-registry credential stays at rest (0600, root-only, on tmpfs) on the mirror host until reboot. security.md:155 promises "no remote temporary file" for material.
- **Fix:** lend the auth file through mirror_release's `/run/bootwright-release-*` prepare-and-sweep pattern, or close B1096 by removing `<job>/remote` on the target at the end of the run and on the next run's sweep. (M)
- **Checked:** the SSH config (no ControlPersist) and `adapterprotocol.compose` (REMOTE_TEMP is the job path reused on the target). B1096 records the leftover directory but not that it can hold a credential. This is the end-to-end addition to B1096.

##### X02-03

**Low · Security · confidence medium**

- **Where:** plugins/module_utils/ceph_host_route_probe.py:261-266, :271-275, :300
- **What:** `provider_source` admits the installed cephadm `ssh.py` and `utils.py` for `exec` after checking only "regular, not group- or world-writable, at most 1 MiB". It never checks `st_uid == 0`. A file owned by a non-root account at 0644 passes and is then executed in the root worker.
- **Evidence:** `require(stat.S_ISREG(info.st_mode) and info.st_mode & 0o022 == 0 and 0 < info.st_size <= 1024 * 1024)` / `exec(compile(source, path, "exec"), …)`
- **Impact:** on a host where /usr/share/ceph is not root-owned, root executes code that account controls. Package ownership makes this unlikely.
- **Fix:** also require `info.st_uid == 0`, as ceph_packages.py:61 does for host packages. (S)
- **Checked:** the other seven exec/compile sites compile fixed collection source (inspect.getsource or shipped module text) and are fine.

#### Patterns
- Secret-file readers make their own whitespace choices instead of sharing one rule: Ansible `lookup(file)` with and without `rstrip` ([X02-01](#x02-01)), Python `rest.strip()` in YAML, and Go's exact-bytes lending. One "read lent part exactly" convention would close the class.
- Credential leftovers after a crash are handled per module, not by the run. mirror_release sweeps by age, mirror_image and image_acquire rely on `finally`, and the target-side remote temp has no owner (B1096). On the controller, the runner's `/run` job and its sweep cover all of this.
- Placeholders are duplicated across languages: `@@BOOTWRIGHT_PRIVATE_URL@@` is a Go constant (kickstart.go:47) and a literal again in managedos_install_anaconda/tasks/apply.yml:227.

#### Strengths
- Runner custody: material goes as 0600 files in a 0700 `/run` job held by a flock. The material list is refused before writing on duplicate or unsafe names, or when a non-secret value contains lent bytes (`confidentialValue`). Each Go copy is cleared after writing. Extra-vars are written as data, not templates (`ansible.ExtraVariables`). Outputs are read back through `os.Root` with checks on mode, link count and exact size. Jobs are swept only once no holder remains.
- Verdicts per class:
  - (a) The Kickstart carries no password: `rootpw --lock`, the user `--lock`, a public authorized key only. The private host key and the LUKS recovery passphrase are fetched once from a token URL against a pinned CA. anaconda-ks.cfg, original-ks.cfg and ks.cfg are deleted.
  - (b) and (f) The pull secret and proxy credentials enter install-config only through `private_file` under no_log. The agent ISO is served only beneath an attempt-minted 256-bit token directory (0711/0640), and the token is redacted from the probe's refusal.
  - (c) BMC credentials go only into Basic auth over an opener with an empty ProxyHandler and no redirects, with the endpoint's own authority enforced.
  - (d) Ceph keys pass `-i -` on stdin.
  - (e) The RHSM organization and activation key are read from files and reach the guest on a fixed worker's stdin, never as a variable.
- TLS-weakened sites (13): each one is either the explicit per-BMC `verification: none` policy or a CERT_NONE handshake followed by a SHA-256 leaf pin checked before any credential or byte is sent (infra_service.py:226-240, ceph_native_object.py:407-414). url openers (9): all collection openers carry `ProxyHandler({})`. The scripts/tools ones are build-time only.
- no_log: no task reads a lent file without no_log. The only hit lends public trust bundles. All 93 `failed_when: false` tasks under no_log are judged by a later task, either in the same file or through `substrate_machine/dispatch.yml`'s generic `_port_result`. The censored callback plus `display_args_to_stdout=False`, `ANSIBLE_LOAD_CALLBACK_PLUGINS=0` and pipelining close the remaining output paths.
- Ambient environment: no Python code takes an endpoint, trust anchor or credential from the environment. `publisher_fetch` overwrites the proxy variables with the frozen route, ACCEPT_EULA and SSH_ORIGINAL_COMMAND are deliberate, and the Go reads are presentation settings or the vetted sudo relationship. tempfile (31): all are mkdtemp/mkstemp (0700/0600) under the run scratch or a private stage, and removed in `finally`. Broad except (30 sampled): each either re-raises a bounded refusal or is documented best-effort cleanup (rhsm `quietly`). None swallows a proof or a security check.

#### Coverage notes
- No test pins lent-file read fidelity, for example a password with a trailing space reaching the module byte-exact ([X02-01](#x02-01)).
- No test kills an SSH-placed mirror fill and checks that the remote auth file is gone ([X02-02](#x02-02), B1096).
- route_probe `provider_source` has no test with a non-root-owned provider ([X02-03](#x02-03)).

#### Already recorded
- tracked: B1096. [X02-02](#x02-02) adds that the leftover remote temp can hold a registry credential.
- [Y02-03](#y02-03): the bcrypt program embedded in YAML. [X02-01](#x02-01) adds the strip mismatch.
- [G16-11](#g16-11) (curl umask on the private key), [Y01-03](#y01-03) (physical_erase probe without use_proxy), [G04-03](#g04-03) (reveal fallback), [G10-02](#g10-02) (Material in unexported fields), [G11-06](#g11-06) (runner clear gaps), [P01-04](#p01-04) (custody st_dev), [P07-02](#p07-02) (`run_command` environment): confirmed on the trace and not restated.

<a id="lane-x03"></a>

### Lane X03: Cross-cutting concurrency and efficiency (Go, Python, Ansible, tests)
Scope read: I read these in full: cmd/bootwright/interrupts.go, privilege/signals_linux_amd64.go, privilege/sudo.go:95-193, privilege/process_linux_amd64.go:295-334, adapterprotocol/process_linux_amd64.go:100-229, hostdnf/packages_linux_amd64.go:110-150, sshlocal/copy_linux_amd64.go:110-190, comparisonlocal/service.go:92-205 and helper.go:59-85, lifecycle/scheduler.go:90-470, scheduler_priority.go, diff_observe.go:131-190, shared_readings.go, logging.go:10-60, removal_gates.go:185-260, attempts.go:868-883, trust/enrollment/service.go:276-312, invokerfs/channel_linux_amd64.go:40-200, machine/access several.go:184-213, copy.go:116-170 and prefix.go, power/batch.go:201-245, clusterpreflight/combine.go, contextfs/lifecycle_lock_linux_amd64.go, venvlocal/guard_linux_amd64.go, kubevirt/lend.go:70-160, ansible/ansible.cfg, controller_prerequisites tasks, libvirt_firewall_ready.py, and reconciliation/stage.go:150-271 with plan.go:596-614.
Skimmed: awaitRoot, lockSetup, holdRuntimeLock, HoldRuntime, readLifecycle, operations_linux_amd64.go:100-230, yamlstream/parser.go, the Python poll sites (physical_erase_boot, disk_discovery_controller, physical_erase_controller), the until/delay sites, and cli/progress.go:40-130.
Not read: operationstore and ansiblerunner lock internals (G11 covered them), cli/lifecycle_presentation (G04 covered it), and the stagelocal Once sites (single init).

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | 5 | Every goroutine has an owner, an exit path and a join, and every flock is non-blocking or a bounded poll |
| Security | N/A | |
| Efficiency | 3 | Block-level parallelism is sound, but about a dozen host fan-outs are serial and recomputation is widespread (mostly already filed by lanes) |
| Clearness | 4 | Concurrency rationale sits beside each goroutine; the scheduler's admission is dense |
| DDD | N/A | |
| Non-duplication | N/A | |
| Right language | N/A | |
| Test coverage | 3 | Concurrent code is 85-98% covered, but the packages that own most fan-out never run their own tests under `-race` |

#### Findings
##### X03-01

**Low · Efficiency · confidence medium**

- **Where:** internal/reconciliation/stage.go:158-167, :254-264; internal/reconciliation/lifecycle/scheduler.go:199-219, :278-283
- **What:** Every `scheduler.next()` call goes through `firstAdmission` to `Startable` to `Ready`.
- `Ready` runs `waitingOn` for every block, and `waitingOn` scans every block with `slices.Contains(block.Dependencies, …)`. That is O(B²·D).
- `Ready` deep-clones each ready block's `Request` bytes, and `firstAdmission` keeps only the indices.
- `firstAdmission` then runs `ContainsFunc` over the startable set (O(B·S)), and `remainingChains` is rebuilt as well.
`next()` runs once per start and once per settle, about 2B times per converge, so the total is O(B³·D) on the single loop goroutine that also settles results.
- **Evidence:** `for _, candidate := range plan.Blocks { if !slices.Contains(block.Dependencies, candidate.ID)`; `ready = append(ready, cloneBlock(block))`; `definition.Request = slices.Clone(definition.Request)`.
- **Impact:** The bounds are `MaxBlocks = 256` and `MaxPlanRequestBytes = 4 MiB`. A full plan then costs about 10⁸ string compares plus up to about 512 × (ready request bytes) of copying. That is tens to hundreds of milliseconds of admission latency, not minutes.
- **Fix:** Build an ID→index map and a remaining-dependency counter once in `newScheduler`, and decrement the counter in `settle`. Give the scheduler an ID-only `Startable` variant that does not clone. (S)
- **Checked:** No lane report names `Ready`, `waitingOn`, `firstAdmission` or `remainingChains`. [G13-01](#g13-01) and [G13-10](#g13-10) are different sites.

##### X03-02

**Low · Efficiency · confidence medium**

- **Where:** ansible/collections/ansible_collections/bootwright/core/roles/controller_prerequisites/tasks/main.yml:27-32; tool.yml:7-16
- **What:** The four target tools (govc, helm, kubectl, virtctl, per internal/controller/tooltable.json) are downloaded and verified one at a time through a looped `include_tasks`. Each download is independent and has its own deadline.
- **Evidence:** `ansible.builtin.include_tasks: tool.yml` / `loop: '{{ bootwright_controller_request.tools }}'`
- **Impact:** Controller preparation takes the sum of four downloads instead of the longest one. [B01-04](#b01-04) puts that at hundreds of MB, and the owner's parallel-by-default rule says it should be the maximum.
- **Fix:** Let `controller_tool` take the tool list and fetch with a bounded thread pool, keeping one evidence entry per tool. Alternatively, keep the per-tool `continue` handshake but issue it once for the whole batch. (M)
- **Checked:** [G07-08](#g07-08) covers only the serial publisher metadata fetches in Go, and [Y04-11](#y04-11) covers this loop's lookup error, not its serial order. I found no B item. `forks = 1` in ansible.cfg is not the cause, because each play here is a single host.

##### X03-03

**Low · Quality · confidence medium**

- **Where:** internal/reconciliation/lifecycle/removal_gates.go:197-216
- **What:** `ReportProgress` computes `completed` under the mutex, then calls `p.report` after unlocking. Two blocks settling at once can therefore deliver counts out of order, for example 5 of 9 and then 4 of 9.
- **Evidence:** `completed := len(p.settled)` / `p.mutex.Unlock()` / `p.report(ctx, ProgressEvent{… Completed: completed …})`
- **Impact:** The resolution check row can briefly show progress going backwards. This is cosmetic only.
- **Fix:** Call `p.report` while holding the mutex, which is what the CLI presenter already does internally. Alternatively, report `max(previous, completed)`. (S)
- **Checked:** The comment says "The engine observes blocks concurrently". The presenter's own mutex orders the writes but cannot reorder the values.

No concurrency defect found. Every goroutine in the inventory below is joined, cancellable and free of shared-state races as written.

#### Patterns
- **Goroutine inventory (14 `go` statements plus 3 implicit timer goroutines).** Each row gives owner, then cancel, then join, then verdict.
  - interrupts.go:31/38: invocation; `stop`/`cancel`; `<-done`, `<-escalationDone`; correct.
  - signals_linux:44: `Begin`; `stop`; `<-finished`; correct.
  - privilege/process:316: `runRelayed`; `waited`; `<-watched`; correct.
  - sudo.go:169: `Supervisor.Run`; `stopRefresh`; `<-finished`. The flags written by the goroutine are read only after the channel close, so this is race-free.
  - adapterprotocol:141 and comparisonlocal:194, invokerfs:66: these are locked-thread fork goroutines for Pdeathsig. Each is joined by `waited`, `done` or `exited`. Correct.
  - adapterprotocol:187: reader; ended by drain or close; `<-readResult`; correct.
  - hostdnf:128: WaitGroup; correct, though unbounded ([G06-11](#g06-11)).
  - sshlocal:140: `copyWorker.close` with an AfterFunc grace; correct.
  - scheduler.go:329 and diff_observe:165: result channels sized to the plan, drained until `running==0` (G13 strength).
  - enrollment:295 (bounded worker pool), access several:202 and copy:145, power:230, clusterpreflight:28: WaitGroup joins, with result slots per index. Correct.
  - Timer goroutines: cli/progress AfterFunc uses a generation counter, and invokerfs `context.AfterFunc` is stopped in `close`.
- **Locks.** In-process mutexes guard only memory. All 50+ flock sites are `LOCK_NB`, or the bounded `awaitRoot`/`lockSetup` backoff (doubling to a cap, under `rootWait`).
  - Kernel flocks vanish on crash, so no lock can go stale.
  - Root→context-dir order (readLifecycle) and the lease's dir→root order (leasedLifecycle.shared) are opposite, but neither blocks, so the result is a `busy` refusal, never a deadlock.
  - Re-entrancy uses a context-value marker (`lifecycleRootHeld`) to avoid self-deadlock.
  - The venvlocal launch lock is shared for the whole `WithPython`. Retirement takes it exclusively and non-blocking, and `holdRuntimeLock` plus `verifyRuntime` re-walk the path after the flock, which closes the unlink window.
- **Serial where parallel is possible.** All are already filed except [X03-02](#x03-02).
  - lifecycle `proveQuiescent`, N Ansible runs ([G13-01](#g13-01))
  - access copy Prepare ([G14-05](#g14-05))
  - power survey per placement host ([G14-05](#g14-05))
  - storage preflight guests ([G20-06](#g20-06))
  - firewall CheckSockets ([G06-02](#g06-02))
  - hostowned dumpxml ([G06-11](#g06-11))
  - bundlelocal metadata ([G07-08](#g07-08))
  - agent-install observe/destroy node loop ([Y03-03](#y03-03))
  - machine_power_read include loop ([Y01-12](#y01-12))
  - inspect retries per target ([P07-09](#p07-09))
  - controller tools ([X03-02](#x03-02))
  - `diff --tool` pairs run in path order on purpose, as specs/cli.md:1156 requires, so that one is not a finding.
- **Recomputation sites, with bounds.** All are filed except [X03-01](#x03-01).
  - 13 rule sets per record ([G01-01](#g01-01)) and source roots per record ([G01-08](#g01-08))
  - additive run twice per plan ([G12-02](#g12-02))
  - lineage re-read per attempt ([G13-10](#g13-10))
  - every manifest per transaction ([G08-03](#g08-03))
  - secret scan per write ([G09-02](#g09-02)) and controller store per query ([G09-11](#g09-11))
  - keyring ledgers per open ([G10-01](#g10-01))
  - area re-walk under `s.attempts`, up to about 8192 listings plus 256 decodes ([G11-01](#g11-01))
  - fsync per output chunk ([G11-02](#g11-02)) and an 8 MiB read to test existence ([G11-05](#g11-05))
  - Ceph request re-decode and rebuild ([G18-07](#g18-07), [G18-08](#g18-08), [G19-09](#g19-09))
  - placement validator at 35 call sites ([G20-02](#g20-02))
  - claims rebuilt ([G21-05](#g21-05)) and Requests() ([G21-08](#g21-08))
  - add-on render ([G22-03](#g22-03)) and kickstart re-render ([G16-10](#g16-10))
  - `ssh -G` per run ([G14-06](#g14-06))
  - Python: `getsource` of 29 modules per call every 5 s for 900 s ([P03-01](#p03-01)), firewall-cmd and nmcli per item ([P05-01](#p05-01), [P05-02](#p05-02)), `ceph --help` per scope ([P02-12](#p02-12)), TLS context per request ([P04-08](#p04-08), [P05-09](#p05-09))
  - YAML: four `oc` calls per read ([Y03-02](#y03-02)) and re-hash of 128 files ([Y04-12](#y04-12))
- **Polling is bounded, with budgets owned by Go.** All 35 `until/retries` sites take `delay` and `retries` from the frozen request budgets or fixed small constants (2 s × 30). The Python `time.sleep` sites all sit under a monotonic deadline at fixed intervals (2-5 s). None backs off, which is acceptable at these intervals. physical_erase_boot.py:142-146 waits for `ack.json` with no limit, by design: the RAM service is left running on cancel, as its comment says.

#### Strengths
- The scheduler admits a whole wave up to `Concurrency()` (default 8, `--parallelism`), with exclusive keys and per-throttle caps. Retries of independent failed blocks are admitted together, never one by one.
- One playbook process handles one host's block (`serial: 1` and `forks = 1` are deliberate). Go owns the fan-out, and `--forks` is passed only for the agent-install fan-out (`Parallelism` ≤ 16).
- Outcomes are recorded under `context.WithoutCancel`, and every cleanup with WithoutCancel is time-bounded (`revokeDeadline`, `helperDeadline`).
- The desired-state parse is a single streaming pass per file under byte and document limits (yamlstream/parser.go:73-118).

#### Coverage notes
- The race gate ([T01-07](#t01-07)) is incomplete. Production packages with goroutines or locks outside `make race`: internal/adapterprotocol, controller/hostdnf, controller/stagelocal, machine/access, **machine/power** (missing from [T01-07](#t01-07)), machine/sshlocal, nativeartifacts/comparisonlocal, environment/clusterpreflight, substrate/kubevirt, workspace/contextfs, workspace/invokerfs, cli. `cmd/bootwright -race` instruments these only on the paths its own tests reach. Their own concurrency tests never run under `-race`: access launch/copySessions, power executeAll, RunTogether, hostdnf queries and the invokerfs AfterFunc abort.
- internal/reconciliation/lifecycle/removal_gates.go:197: concurrent `ReportProgress` ordering is untested ([X03-03](#x03-03)).
- **Test cost, cmd/bootwright (151 s).**
  - 0 `t.Parallel` in 180 files.
  - 167 uncached `compileAcceptance` calls, each paying [G01-01](#g01-01)'s 13 rule sets per record.
  - `exampleDirectory` runs `git ls-files` on every call ([E01-11](#e01-11)).
  - 42 e2e renders, each done three times.
  - The static-binary `make build` takes about 38 s (inner-loop-check-cost.md).
  - What would cut it: a per-example `sync.Once` cache of compiled state, splitting the e2e and acceptance tests into their own package, then `t.Parallel`.
- **Test cost, workspace/contextfs (113 s).** The checkpoint harness starts a child process per kill point. Each publication repeats the `directory.verify` walk from `/` ([G08-02](#g08-02) and the knowledge page: "the crash harness repeats it hundreds of times"), and the top-level tests run serially. What would cut it: fixing [G08-02](#g08-02) (verify once per held descriptor) and running the harness subtests in parallel.
- The Python 320 s serial loop is explained on the knowledge page: xdist is unsafe for fork and signal tests (B866), so it is not a finding.

#### Already recorded
- [T01-07](#t01-07): race gate list (extended above with machine/power and the instrumentation nuance)
- B1053: serial in-package tests in cmd/bootwright, contextfs and lifecycle (parked)
- B1339: `make race` timeout on cmd/bootwright
- [G13-01](#g13-01), [G13-02](#g13-02), [G13-10](#g13-10), [G14-05](#g14-05), [G14-06](#g14-06), [G06-02](#g06-02), [G06-11](#g06-11), [G07-08](#g07-08), [G20-06](#g20-06), [G11-01](#g11-01), [G11-02](#g11-02), [G11-05](#g11-05), [G08-02](#g08-02), [G08-03](#g08-03), [G09-02](#g09-02), [G09-11](#g09-11), [G10-01](#g10-01), [G12-02](#g12-02), [G01-01](#g01-01), [G01-08](#g01-08), [G18-07](#g18-07), [G18-08](#g18-08), [G19-09](#g19-09), [G20-02](#g20-02), [G21-05](#g21-05), [G21-08](#g21-08), [G22-03](#g22-03), [G16-10](#g16-10), [P03-01](#p03-01), [P05-01](#p05-01), [P05-02](#p05-02), [P02-12](#p02-12), [P04-08](#p04-08), [P04-09](#p04-09), [P05-09](#p05-09), [P07-09](#p07-09), [Y01-12](#y01-12), [Y03-02](#y03-02), [Y03-03](#y03-03), [Y04-12](#y04-12), [E01-11](#e01-11), [T01-01](#t01-01), [T01-02](#t01-02): serial and recomputation sites, cited in the tables above rather than restated

<a id="lane-x04"></a>

### Lane X04: Cross-cutting domain-driven design: bounded contexts, dependency direction, domain model
Scope read: I read these fully: the BRIEF and LANE X04; specs/architecture.md:14-192 and 293-411; specs/glossary.md; test/architecture/boundaries_test.go:30-352; internal/containercluster/{facts,members}.go; internal/storage/members.go; internal/machine/placement.go:1-60; internal/substrate/target.go:120-160.
I skimmed these: go-imports.txt, which I analysed with a script into a context graph with role tags; internal/controller/{selection,tools}.go; reconciliation/lifecycle/planning.go:236-395; prerequisites/storage.go:80-120; cmd/bootwright/acquisitions.go; the root packages of machine, substrate, storage/cephcluster, containercluster/openshift and secrets, by type, method and function counts; the contracts.go sizes; the 33 lane reports; audit 004 ARC-07/21/28; backlog B464.
I did not read the large packages line by line. G08, G12, G16 and G18 already reviewed them.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
| --- | --- | --- |
| Quality | 4 | No Go cycles and no consumer asserts a concrete adapter type; the gaps are in guards, not in behaviour. |
| Security | N/A | Out of lane. |
| Efficiency | N/A | Out of lane. |
| Clearness | 3 | Adapter names follow five conventions, and several glossary terms carry other meanings in code. |
| DDD | 3 | Application-to-application edges are guarded and the engine is kind-agnostic. But the domain tier is free functions over the wire tree, and its context graph is unguarded. |
| Non-duplication | 4 | Few cross-context copies; the reservation key grammar and one enum are spread or repeated. |
| Right language | N/A | Out of lane. |
| Test coverage | N/A | Out of lane. |

#### Findings
##### X04-01

**Medium · DDD · confidence high**

- **Where:** test/architecture/boundaries_test.go:222-223, :318-330
- **What:** The fitness test guards cross-context coupling only between two application packages. A domain or application package may import any other context's domain root with no stated reason. Most measured context pairs that depend on each other in both directions grew through these unguarded edges.
- **Evidence:** `case domainRole: return provider == domainRole || provider == technicalRole`. Only `consumer == applicationRole && provider == applicationRole` consults `applicationDependencies`. Measured: 36 domain→domain and 94 application→domain cross-context edges, none with a stated reason. Examples: `internal/controller` → addons, containercluster, infrastructureservices, machine, substrate; `containercluster/openshift` → addons, environment, infrastructureservices, machine, network, substrate; `storage/cephcluster` → 6 contexts.
- **Impact:** Entanglement such as [G06-05](#g06-05) and [G21-05](#g21-05) accrues with no review point, and the spec's rule to identify each cross-context producer and consumer (architecture.md:112-117) has no check at the domain tier.
- **Fix:** Add `contextDependencies()`: allowed `internal/<a>` → `internal/<b>` edges, each with a reason, seeded from today's 14-context graph. Check it in `TestPackageDependencyDirection` for every first-party import that crosses a context. (S/M)
- **Checked:** imports_test.go, family_neutral_test.go and layout_test.go. family_neutral only forbids storage, containercluster and addons imports in the general packages. No context-level rule exists.

##### X04-02

**High · DDD · confidence high**

- **Where:** internal/controller/prerequisites/storage.go:96, internal/substrate/target.go:431-465, internal/managedos/media.go:210, internal/machine/sshhost.go:25-49, internal/reconciliation/lifecycle/planning.go:245-392
- **What:** [G06-01](#g06-01) reports that the host-reservation value lives in Controller setup. Seen across the module, it is the main cause of the context entanglement. Three of the nine measured two-way pairs exist only for it: controller↔substrate, controller↔machine and controller↔infrastructureservices. Its rules are also spread over five packages: the value and `Conflicts` in prerequisites; key builders in substrate (`ControllerReservationKey`, `SystemReservationKey`, `MACReservationKey`), in managedos (`MediaReservationKey`) and in machine (`SSHReservationQualifier`, `HostKeyQualifier`); and SSH-host qualification inside the lifecycle engine.
- **Evidence:** libvirt, kubevirt, baremetal, machine/hardware, artifactserver and managedservice import prerequisites only for `HostReservation`, plus `MaxReservationKeys` in two of them. The lifecycle code `reservation.Keys = prerequisites.QualifiedKeys(machine.SSHReservationQualifier(...), ...)` (planning.go:297). The keys are `"media:" + name`-style strings that `Conflicts` parses by prefix.
- **Impact:** Changing the reservation key grammar touches five contexts, and the glossary owner (infrastructure-services.md#host-reservations) holds none of the code.
- **Fix:** Move the value, the key builders and `Conflicts` into `internal/infrastructureservices`. It is a domain root with 13 importers that imports only api and secrets, so no cycle forms. Make the key a typed value (`Key{Class, Subject}`) and drop `controller/prerequisites` from `capabilityDependencies()`. (M)
- **Checked:** I verified each importer's prerequisites symbols by grep. B464 covers only a duplicated key constant.

##### X04-03

**Medium · DDD · confidence high**

- **Where:** api/v1alpha1 (60 of 106 importers); internal/desiredstate/compilation/requests.go:21; internal/substrate/target.go:149
- **What:** Verdict: the immutable `api.Value` tree is the working domain model through admission, selection and request building. Code translates out of it at two points only. The first is published value objects: `machine.Placement` (placement.go:23), `substrate.Target` and `HardwareIdentity`. The second is each capability's frozen request: installation, agentinstall, power and instance requests.go hold 0 api references. The compiler hands on the wire catalog itself (`Effective api.Catalog`).
- **Evidence:** 3,096 `.Get("` and 540 `.Has("` sites in production. 1,396 of 7,648 production functions (18%) take `api.Object`, `api.Catalog` or `api.Value`. Path reads by context: storage 1,212, containercluster 611, managedos 312, substrate 313, infrastructureservices 290, machine 457, but desiredstate, which owns "API-to-domain translation", only 42. The typed target still carries the wire object: `PlacementMachine api.Object`.
- **Impact:** This choice avoids a parallel type hierarchy and shared mutable state; that benefit is real and should be kept. The costs: path typos read as absent ([G01-03](#g01-03)); every consumer re-derives facts per call ([G20-02](#g20-02), [G21-05](#g21-05)); and a schema change has no compile-time reach.
- **Fix:** Keep the tree for admission. Add typed views only for the two hot kinds, Machine and CephCluster, produced once per compile. Adopt [G01-03](#g01-03)'s path-resolution fitness test first; it is cheaper and catches the worst failure. (L)
- **Checked:** architecture.md:297-299 permits translation "when their invariants require". The tree is immutable, per [G01-03](#g01-03). I counted on non-test files.

##### X04-04

**Medium · DDD · confidence high**

- **Where:** internal/substrate, internal/machine, internal/storage/cephcluster, internal/containercluster/openshift, internal/secrets (root files)
- **What:** The domain layer is mostly transaction script with primitive obsession: free functions validating and deriving from the wire tree, with few behaviour-bearing types. Secrets is the exception.
- **Evidence:**
- Methods versus free functions: openshift 0/98, cephcluster 1/155, substrate 7/175 (78 exported free functions take api values), machine 10/156, secrets 16/28.
- Context names travel as bare strings: 285 production functions take a `contextName` parameter and 49 take `controllerMachine`.
- Arms and connections are untyped constants: `ArmBaremetal = "baremetal"` (target.go:18), `ConnectionLocal = "local"` (placement.go:14).
- machine/power `Request` has `Arm string` and `Verb string` (requests.go:45,50), while reconciliation declares a typed `Verb` (plan.go:30) and eight other typed enums.
- **Impact:** An arm or verb typo compiles, and identity rules (context name, arm) are re-checked by each consumer instead of once at construction.
- **Fix:** Introduce `substrate.Arm`, `machine.Connection` and a validated `ContextName` value with constructors, and adopt them at the frozen-request fields first. (M)
- **Checked:** I counted with grep over non-test root files. The policy of not duplicating the API does not cover typed scalars.

##### X04-05

**Medium · DDD · confidence medium**

- **Where:** internal/controller/tools.go:43-157, internal/controller/selection.go:112-411
- **What:** The controller root plays two unrelated roles in one package:
- Published controller evidence that others consume: `controller.State` (44 uses), `InstalledTool` (29), `InstalledHostIdentity` (23), `ToolRequest` (16).
- Selection policy that reads five other contexts ([G06-05](#g06-05) covers the raw field reads).
The pairs controller↔containercluster and controller↔addons exist only because these two roles share a package: agentinstall, networking, addons/instance and clusterobjects/driver import `internal/controller` for the evidence types.
- **Evidence:** `facts, _ := containercluster.FactsOf(cluster.Kind())` (tools.go:55) and `addons.ExpandBinding` (tools.go:69) sit beside the evidence types that `containercluster/agentinstall` imports.
- **Impact:** A consumer of controller evidence depends transitively on add-on, cluster, machine and substrate admission, which inflates the fan-in of 15.
- **Fix:** Move the evidence values into a leaf domain package, such as `internal/controller/evidence`, that imports only api. Selection stays in the root. Add the leaf to `packageRoles`. (S/M)
- **Checked:** Symbol usage by grep. No Go cycle forces the current placement.

##### X04-06

**Low · DDD · confidence medium**

- **Where:** internal/reconciliation/lifecycle/invocation.go:76,155, requests.go:90, planning.go:316-337
- **What:** The engine's `Execution` embeds Machine's `Placement` and `SSHOptions`. Its planner writes Machine access remediation itself.
- **Evidence:** `"author access.ssh.knownHostsRef on Machine/"+claim.Machine` (planning.go:317).
- **Impact:** Reconciliation (which "does not own domain lifecycle-capability mechanics") changes whenever Machine access changes, and this is the reverse edge of machine↔reconciliation.
- **Fix:** Have Machine publish an execution-host value with its own refusal text, and have the engine carry it opaquely. Alternatively, name the dependency in a context map ([X04-01](#x04-01)). (M)
- **Checked:** [G14-04](#g14-04) covers the other misplaced items in the machine root, not this direction.

##### X04-07

**Low · DDD · confidence medium**

- **Where:** internal/containercluster/facts.go:4-24
- **What:** The container-cluster family root imports its one implementation and re-exports that implementation's types as the family vocabulary. The storage family root does not import cephcluster: composition binds its storage evidence.
- **Evidence:** `Tool = openshift.ControllerTool`, `VirtualIP = openshift.VirtualIP`; `api.OpenShiftCluster: {ControllerTools: openshift.ControllerTools, ...}`.
- **Impact:** A second distribution would have to produce `openshift.*` types, and the two families follow different patterns.
- **Fix:** Declare the fact types in the root and let composition bind the per-kind table, as Storage does. (S)
- **Checked:** architecture.md:308 allows `FactsOf` in the root but says nothing on type ownership; family_neutral_test.go does not inspect the root.

##### X04-08

**Low · Clearness · confidence high**

- **Where:** substrate/admission.go:13, substrate/facts.go:86, secrets/localkeyring/localkeyring.go:28, addons/clusterobjects/evidence.go:27, managedos/installation/erase_request.go:195, containercluster/installation/service.go:9
- **What:** The code departs from the glossary in several places:
- `Variant(provider)` returns the present arm (`Variant(provider) != ArmKubeVirt`), but the glossary says a variant is never an arm.
- `localkeyring.Implementation` is an adapter struct, not a registry entry.
- `clusterobjects.Receipt` and `EraseReceipt` are a third and fourth sense of "Receipt", which the glossary says is "always" one of two.
- `installation` names a stub command service in containercluster and a five-capability lifecycle package in managedos.
- The machine root is imported as `machineref` in 46 production files and plainly as `machine` in 96.
- **Impact:** A newcomer is misled exactly where the glossary promises one meaning per term.
- **Fix:** Rename `Variant` to `SelectedArm`, `Implementation` to `Keyring`, and the two receipts to `...Record`. Settle on one import name. (S)
- **Checked:** glossary.md:89-109.

##### X04-09

**Low · Clearness · confidence high**

- **Where:** test/architecture/boundaries_test.go:42-68; architecture.md:315
- **What:** The 24 driven adapters use five naming conventions:
- `*local` (11)
- `*fs` (4)
- `host*` (3)
- `local*` (1: `localkeyring`)
- a plain noun (5: privilege, material, encoding, yamlstream, ansiblerunner)
Two packages are both named `preflightlocal`. `ansiblelocal` and `ansiblerunner` both run Ansible under different conventions. The spec rule is "named by what it binds", which none of the suffixes expresses.
- **Impact:** A reader cannot tell from the name which packages are adapters, and the `cmd` wiring needs import aliases.
- **Fix:** Settle on `<binds>local` or `<binds>fs`, rename the plain-noun adapters, and add the rule to layout_test.go. (S)
- **Checked:** Every `adapterRole` entry.

##### X04-10

**Low · Non-duplication · confidence high**

- **Where:** internal/addons/packages.go:36-41, internal/addons/clusterobjects/types.go:119-124, internal/addons/instance/render.go:212
- **What:** The `Approval` enum, with `Exact` and `OperatorManaged`, is declared twice and bridged with a conversion.
- **Evidence:** `Approval: co.Approval(olm.Approval)`.
- **Impact:** Two enums can drift.
- **Fix:** The addons root already imports clusterobjects, so alias it: `type Approval = clusterobjects.Approval`. (S)
- **Checked:** No lane reported it.

#### Patterns
- **Context graph, the nine measured two-way pairs judged by their package edges:**
  - machine↔substrate, machine↔trust and machine↔infrastructureservices are layered, not tangled. machine root → substrate, trust and infrastructureservices roots (published facts: `MachineBMC`, `HostKey`, `ValidateProxy`). The other side's capabilities, or `trust/enrollment`, → machine root, mostly for `Placement` (61 uses) and `HardwareIdentity` (40).
  - controller↔substrate, controller↔machine and controller↔infrastructureservices exist only through `HostReservation` ([X04-02](#x04-02)).
  - controller↔containercluster and controller↔addons come from the evidence-and-selection mix ([X04-05](#x04-05)).
  - controller↔reconciliation: lifecycle → prerequisites (`PythonLaunch`, `Runtime`, reservations) and clients → lifecycle (port vocabulary); declared, but would shrink after [X04-02](#x04-02).
  - reconciliation↔machine: capabilities → the lifecycle port is fine; the reverse edge is [X04-06](#x04-06).
  - containercluster↔infrastructureservices: agentinstall → artifactserver is declared, but managedservice → containercluster root carries DNS policy ([G17-05](#g17-05)).
  - reconciliation↔workspace is mutual port implementation (contextfs and contextguard), which is fine.
  - Hubs: `controller/prerequisites` (23 importers, 7 only for reservations), `internal/controller` (15), `internal/machine` (33, inflated per [G14-04](#g14-04)).
- **Several capabilities per package:** storage/ceph declares 7 capability types (capability.go:21, export.go:148, host.go:263, image.go:178, physical_retirement.go:52, replication.go:89, smb.go:354; 49 files, 12,282 lines) and managedos/installation 5 ([G16-04](#g16-04)). architecture.md:311 describes one. contextfs ([G08-04](#g08-04)) and lifecycle ([G12-05](#g12-05)) are single-context but multi-port.
- **Policy outside the domain:** the composition root ([G05-01](#g05-01), acquisitions), the CLI ([G03-02](#g03-02), [G03-03](#g03-03), [G04-05](#g04-05)), Controller selection ([G06-05](#g06-05)) and OpenShift admission ([G21-05](#g21-05)) each hold another context's rules.
- **Ports:** lifecycle/contracts.go has 55 interfaces ([G12-05](#g12-05)) and prerequisites/contracts.go 31. All others have 12 or fewer. The only port upgrades are runtime interface assertions ([G12-04](#g12-04)).
- **Ranked structural changes, most payoff first:**
  1. Context-dependency fitness rule ([X04-01](#x04-01)).
  2. Move reservations to infrastructureservices with a typed key ([X04-02](#x04-02) with [G06-01](#g06-01)).
  3. Split controller evidence into a leaf ([X04-05](#x04-05)).
  4. Path-resolution test, then typed views for Machine and CephCluster ([G01-03](#g01-03), [X04-03](#x04-03)).
  5. Split the contextfs kernel ([G08-04](#g08-04)) and the lifecycle decision and admin services ([G12-05](#g12-05)).
  6. One capability per package in storage/ceph and managedos/installation ([G16-04](#g16-04)).
  7. Typed Arm, Connection and ContextName ([X04-04](#x04-04)).

#### Strengths
- Each of the roughly 40 application-to-application edges carries a stated reason in `applicationDependencies` and is pruned when unused.
- The lifecycle engine is kind-agnostic: 5 path reads in 15,388 lines.
- No consumer asserts a concrete adapter type. The only `.(*T)` assertions in production are on standard-library types (`syscall.Stat_t`, crypto keys).
- Frozen capability requests are typed and free of the wire tree at the Ansible boundary.
- The secrets and reconciliation roots model state with typed enums and methods.

#### Coverage notes
- No fitness test covers context-level edges ([X04-01](#x04-01)), adapter naming ([X04-09](#x04-09)), or the ownership of family fact types ([X04-07](#x04-07)).

#### Already recorded
- [G06-01](#g06-01) (reservation placement; [X04-02](#x04-02) adds the graph cost and the scattered key grammar). [G06-05](#g06-05), [G01-03](#g01-03), [G08-04](#g08-04), [G12-04](#g12-04), [G12-05](#g12-05), [G14-04](#g14-04), [G16-04](#g16-04), [G17-05](#g17-05), [G18-01](#g18-01), [G21-05](#g21-05), [G05-01](#g05-01), [G03-02](#g03-02), [G03-03](#g03-03), [G04-05](#g04-05) are cited and not restated.
- tracked: B464 (duplicated reservation key constant only).
- Audit 004 ARC-26 (composition decisions) and ARC-07/ARC-28 (arm touch lists) are adjacent; they record spec text, not these code structures.

<a id="lane-x05"></a>

### Lane X05: Cross-cutting duplication in Go and across languages
Scope read: I opened both sides of all 33 exact Go clone pairs and the 30 largest shape pairs. I opened the definitions of all 17 colliding names. I read three request paths end to end: Ceph core (requests.go, role_admission_test.go, ceph_native.py:596-700), managed OS install (anaconda apply.yml:1-60, argument_spec_test.go) and cluster objects (validation.go, cluster_objects_model.py, cluster_objects_engine.py:559-595, the role's argument_specs). I also read cmd/bootwright/role_admission_test.go and contract_versions_test.go:86-372. / Skimmed: the four contract suites, adapterprotocol record.go/record_test.go, controller_channel.py and adapter_protocol.py. / Not read: no substrate (kubevirt/libvirt) lane report exists, so I reviewed that family myself. I did not re-open families a lane already settled, and cite those lanes instead.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | N/A | |
| Security | N/A | |
| Efficiency | N/A | |
| Clearness | 3 | One name means different rules in different packages (findNamed, validName, printable, named). |
| DDD | 3 | The adapter outcome is a string at the port even though the decoder already types it; every capability copies the same adapter skeleton. |
| Non-duplication | 2 | About 20 duplicated families; two adapter-capability skeletons are copied across at least 7 packages. |
| Right language | 3 | The trust-boundary checks in three languages are mostly sound; the Python copy of Ceph product rules is already reported as [P01-05](#p01-05). |
| Test coverage | N/A | |

#### Duplication families (task 1)
Class: a = deliberate and cheap; b = accidental copy-paste; c = parallel structure that is missing a shared abstraction; FP = false positive.

| # | Family | Copies (path:line) | Duplicated lines (clone data) | Class | Proposed owner | Blocker to a naive merge | Lane ID |
|---|---|---|---|---|---|---|---|
| 1 | Adapter capability skeleton: verb-ladder style (Plan, Removal, mutate, prepare, run, observe, `*Effect` ladder, outcome switch) | substrate/libvirt/capability.go:316-438 vs :458-576; kubevirt/capability.go:404-573; baremetal/capability.go:106-173; agentinstall/capability.go:135-257 and install.go:185-257; artifactserver/capability.go:126-270 | exact 127 (8 pairs); shape 68/42/32/25/22/21 | c | generic `lifecycle.Adapter[R]` holding Prepare/Run/Presence/Absence/Partial/Unremoved/Unfinished funcs | each arm has its own Request type, extra validator arguments (fingerprint, marker, bootRecorded) and produced-material clearing | [G21-03](#g21-03) (mutate only); the effect ladder is new: [X05-01](#x05-01) |
| 2 | Adapter capability skeleton: removal-bool style (Observe/ObserveRemoval, observe, Quiescent, Unresolved, SecretReferences, SecretClasses) | mirrorfill/capability.go:216-271, images.go, releases.go; managedos/installation/tree.go:245-283 | shape 56/26/25/21/19 | c | same as #1 | the second style takes `removal bool` where the first takes a verb; both should converge on one | [G17-02](#g17-02) (in-package only); the cross-package tree.go copy is new |
| 3 | Artifact server vs managed service (Go and YAML) | artifactserver/capability.go:126-270, managedservice/capability.go:301-443, the two evidence.go files; infra_* roles' destroy.yml 1-181 | exact 52, shape 98, YAML 181 | c | `managedservice` with a per-kind evidence port | the copies have already diverged (`StartedAfterFiles`) | [G17-01](#g17-01), [Y04-01](#y04-01) |
| 4 | contextfs open/lock/registry prologue | logs_read:151-196, reconciliation:188-229, trust:75-106, secrets:138-241, operations:516-641 | exact 107 | b | contextfs `openContext` (repository:670) | descriptor lifetimes differ per caller | [G08-05](#g08-05), [G09-03](#g09-03) |
| 5 | Stage and run trees | stagelocal/staging:439-501 vs ansiblerunner/jobs:607-669 | exact 10, shape 45 | b | a shared owned-tree package under internal/controller | the lock/marker protocol must stay one | [G07-04](#g07-04), [G11-03](#g11-03) |
| 6 | Descriptor-walk file reads | inputfs/reader:738-761 vs secrets/material/files:483-506 | exact 13 | b | one fs-walk helper | the copies have drifted (Uid/Gid, buffer clearing) | [G01-04](#g01-04) |
| 7 | Native document and request projection | agentinstall/projection.go:365-400 vs installation/network.go:97-130; requests.go 101-114 vs 86-99 | exact 14+11, shape 27 | b | `machine` (shared Firmware/BIOS types) | none beyond the import direction | [G16-08](#g16-08), [G21-14](#g21-14) |
| 8 | Capacity summary | containercluster/preflight/service.go:83-99, clusterpreflight/combine.go:51-67, storage/preflight | exact 15 | b | `capacity` | none | [G02-01](#g02-01) |
| 9 | Bounded JSON scanner | localkeyring/format.go:388-402 = contextfs/records.go:97-111 | exact 12 | b | `canonicaljson` | none | G09/G10 patterns |
| 10 | Operation read sequence | lifecycle evidence.go:68-79, status.go:91-102, status_resources.go:79-90 | exact 10 ×3 | b | a lifecycle helper | none | G13 pattern |
| 11 | Machine access fan-out | access/service.go:74-87, several.go:46-59, copy.go | exact 11 | b | `machine/access` | none | [G14-11](#g14-11) |
| 12 | Trust enrollment rendering | cli/output_trust.go:160-169 vs enrollment/requests.go:42-51 | exact 10 | b | `enrollment` | none | [G02-09](#g02-09) |
| 13 | Disk identity, serial grammar, 64 TiB bound | erase_admission, diskselection, kickstart %pre, two Python files | n/a | b | `machine/diskselection` | the bash and Python copies run on the target | [G14-03](#g14-03), [G16-01](#g16-01), [G16-09](#g16-09) |
| 14 | Admin credential skeleton | agentinstall/admin_credentials.go vs ceph/admin_credentials.go | n/a | c | `lifecycle` | the material binding differs | [G21-02](#g21-02) |
| 15 | Issue cap `add` (999) plus `issue`/`reference` | machine/nmstate.go:452-465, openshift/admission.go:928-934, infrastructureservices/admission.go:587-594, managedos/admission.go:586-592, substrate/admission.go:511-518; customplaybooks/validation.go (16 literals) | 6 copies | b | `api` (`MaxIssues`, `AppendIssues`, `ReferenceIssue`) | none | G17, G02, G01 patterns |
| 16 | Contract-suite helpers `succeeds`/`refuses`/`holds` | areacontract ×2, workspacecontract, media storecontract | 6-7 lines each | a | keep | suites are per port by design | n/a |
| 17 | Port result vocabularies | environment/inspection/result.go:62-85 vs ceph/inspection.go:16-39 | shape 18 | a | keep (consumer-owned port) | n/a | G02 pattern |
| 18 | False positives | cli/dispatch.go:34-80 (struct field list); cli/output_controller.go:201-244 (label switch); commands_environment vs commands_secrets (table dispatch); ceph/materials.go:371-407 (refusal table); api object.go vs contextfs checkpoints.go (constant lists) | 19-26 | FP | n/a | n/a | [G03-01](#g03-01) checked, G01 |

#### Findings
##### X05-01

**Medium · Non-duplication · confidence high**

- **Where:** internal/substrate/libvirt/capability.go:384, :522; internal/substrate/kubevirt/capability.go:538; internal/substrate/baremetal/capability.go:195; internal/containercluster/agentinstall/capability.go:219, install.go:310; internal/infrastructureservices/artifactserver/capability.go:222; managedservice/capability.go:397; internal/managedos/installation/capability.go:392, :405
- **What:** The rule that turns an observation into an effect state (apply: presence, then absence, then partial; destroy: absence, then unremoved, then unfinished) is hand-written in 10 functions across 7 packages. These sit beside 28 `ObserveRemoval` implementations. A second skeleton style (`filesEffect`/`treeEffect` with `removal bool`) exists in mirrorfill and installation/tree.go.
- **Evidence:** libvirt:386-398 `case ValidateHostAbsence(evidence, digest) == nil: return reconciliation.EffectCompleted ... case ValidateHostUnremoved(...) == nil: return reconciliation.EffectNoEffect`. Lines 522-536 repeat this for Machine. kubevirt:538-573 is the same code (exact clone, 32 lines).
- **Impact:** A change to the effect-resolution contract (for example, a new unknown-versus-partial distinction) has to be made in 10 places in two styles. The libvirt/evidence.go Unremoved/RemovalUnfinished/Remains trio (:352-410 vs :638-693) shows the copies multiplying even inside one package.
- **Fix:** Add `lifecycle.EffectReading{Presence, Absence, Partial, Unremoved, Unfinished func([]byte) error}` with one `Effect(verb)` method. Each arm keeps only its validators, as closures over its request. (effort M)
- **Checked:** [G21-03](#g21-03) covers only `mutate` and the outcome switch, and [G17-01](#g17-01)/G17-02 cover only their own packages. No B item or audit-003 ID matches. The semantics are identical in every copy I opened.

##### X05-02

**Medium · DDD · confidence high**

- **Where:** internal/reconciliation/lifecycle/invocation.go:138; internal/adapterprotocol/record.go:308; internal/substrate/libvirt/capability.go:593; internal/substrate/kubevirt/capability.go:206; internal/substrate/baremetal/capability.go:147-154
- **What:** `RunResult.Outcome` is a plain `string`. The protocol decoder already admits only `changed`/`unchanged`, and the one producer (ansiblerunner/process_linux_amd64.go:450) copies the validated value. Despite that, 8 switch copies and about 10 inline `!= "changed"` checks map it to `reconciliation.Outcome` again. The copies have different signatures: kubevirt `usableOutcome(reported string, …)`, libvirt `usableOutcome(result RunResult, …)`.
- **Evidence:** `Outcome  string` (invocation.go:138); `(record.Outcome == "changed" || record.Outcome == "unchanged")` (record.go:308).
- **Impact:** The duplication exists only because the port type is weaker than its single producer.
- **Fix:** Type the field as `reconciliation.Outcome` and set it at decode. Delete the per-capability mapping; test doubles then need a typed value. (effort S-M)
- **Checked:** grep found one producer. Test doubles may set other strings, and those would now fail to compile, which is the intent.

##### X05-03

**Medium · Clearness · confidence high**

- **Where:** internal/substrate/admission.go:410; internal/substrate/kubevirt/selection.go:289; internal/substrate/libvirt/selection.go:308; internal/managedos/installation/selection.go:760; internal/containercluster/openshift/admission.go:909
- **What:** `findNamed` has two meanings. The substrate root version succeeds only on a unique match. The three child and installation versions return the first match and refuse an empty name. The unique-match rule is written a third time as openshift's `named`.
- **Evidence:** root: `return found, count == 1`; children: `if value.Get(key).Text() == name { return value, true }`.
- **Impact:** Someone moving code between the substrate parent and an arm silently changes how duplicates are handled.
- **Fix:** Add one `api.Value` method, for example `Named(key, name) (Value, bool)` with unique-match semantics, which is safe after admission, and delete the five helpers. (effort S)
- **Checked:** the selection code runs after admission, which refuses duplicate names, so first-match and unique-match agree today.

##### X05-04

**Medium · Non-duplication · confidence high**

- **Where:** cmd/bootwright/role_admission_test.go:28, :119; internal/managedos/installation/argument_spec_test.go:24, :86; internal/containercluster/agentinstall/role_spec_test.go:26, :75; internal/controller/ansiblelocal/role_spec_test.go:22, :34; internal/storage/ceph/role_admission_test.go:17-60
- **What:** Five Go test files each re-implement ansible-core argument-spec validation, each with its own option type, and they check different subsets. The cmd version handles `raw`, `elements` and `choices`. The agentinstall and ansiblelocal versions have no `elements`. The ceph version uses reflection and ignores `choices`. The central check already runs on every contract-suite request (capability_contract_test.go:929).
- **Evidence:** `type roleOption struct {… Elements string …}` (cmd); agentinstall's `roleOption` has no `Elements`.
- **Impact:** A spec construct one emulator rejects passes another, and the per-package copies largely repeat the central check.
- **Fix:** Move one validator into a non-test file of the `ansible` package (for example `ansible.ArgumentRefusals(spec, entry, variable, value)`). Delete the four copies, or reduce them to golden feeds of the central one. (effort M)
- **Checked:** the deliberate per-package golden helpers are a different thing; this code emulates ansible-core semantics.

##### X05-05

**Medium · Test coverage · confidence high**

- **Where:** ansible/.../plugins/module_utils/ceph_native.py:68-75, :596-600; internal/storage/ceph/host_reading_test.go:243-256; internal/storage/ceph/testdata/request-cluster*.golden
- **What:** The Ceph Python validator requires `SCOPE_MEMBERS` and forbids every key outside `SCOPE_OPTIONAL`/`RESOURCE_MEMBERS`/`delegated`. Only `RESOURCE_MEMBERS` and `AUXILIARY_NAMES` are pinned to Go. No Python test runs `ceph_native.validate` on a Go-produced request golden. By contrast, the managed OS and agent paths do: test_substrate_port.py:15 and test_containercluster_kubevirt_port.py:11 read Go goldens.
- **Evidence:** grep finds no `request-cluster` reference under ansible/; SCOPE_MEMBERS is pinned by no test.
- **Impact:** A new Go Request field passes the reflection argument-spec test (role_admission_test.go:65) once the YAML is edited, but the cluster refuses it only on the host at effect time.
- **Fix:** Add one Python unit test that feeds every `internal/storage/ceph/testdata/request-*.golden` through `ceph_native.validate`, as test_substrate_port does. (effort S)
- **Checked:** test_ceph_core.py reads only a synthetic CA golden.

##### X05-06

**Low · Non-duplication · confidence high**

- **Where:** internal/storage/ceph/contracts.go:83-85; internal/substrate/libvirt/catalog.go:85, kubevirt/catalog.go:79, agentinstall/catalog.go:120; 19 sites of `sha256.Sum256([]byte(strings.Join(`
- **What:** Nine exported `ContentDigest` functions (same concept, inputs per capability) and three private `contentDigest(parts...)` helpers all re-implement "sha256 over NUL-joined parts, hex". Ceph builds the same bytes by `+ "\x00" +` concatenation.
- **Impact:** The mechanism of a digest-moving contract is spelled out 19 times.
- **Fix:** Add `reconciliation.ContentDigestOf(parts ...string)`. Joining with "\x00" produces the same bytes as Ceph's concatenation, so no digest moves. (effort S)
- **Checked:** I opened all 9 definitions. The inputs are deliberately per capability; only the mechanism repeats.

##### X05-07

**Low · Clearness · confidence high**

- **Where:** validName: addons/clusterobjects/validation.go:31 (DNS subdomain), localkeyring/format.go:613 (hand-rolled DNS label), workspace/contexts/service.go:39,51 (DNS-label regex, returns error), custody/declarations.go:82 (`api.ValidLexical("name")`), invokerfs/helper:46 (path component), hostowned/probe.go:150 (printable file name). printable: adapterprotocol/record.go:188 (bool, ASCII), operationstore/records.go:549 (bool with a limit), hostdnf:440 (bool), hostowned/probe.go:154 (returns a sanitized string).
- **What:** These are the same names for different rules. Inside them, the DNS-label rule exists three times in Go even though `api.ValidLexical("name"/"dns")` owns it.
- **Fix:** Call `api.ValidLexical` in localkeyring, contexts and clusterobjects. Rename hostowned `printable` to `sanitized`. (effort S)
- **Checked:** localkeyring is already reported in the G10 patterns; contexts and clusterobjects are new.

##### X05-08

**Low · Non-duplication · confidence high**

- **Where:** ansible/.../module_utils/controller_channel.py:12; adapter_protocol.py:41; internal/adapterprotocol/record.go:65; record_test.go:425
- **What:** The 64 KiB record bound is pinned between Go `MaxLine` and Python `adapter_protocol.MAX_LINE`. The writer that enforces it, `controller_channel.emit`, has its own literal `65536`, and no test pins that literal.
- **Fix:** Import `MAX_LINE` in controller_channel, or add it to the record_test parity map. (effort S)
- **Checked:** controller_channel is used by both kinds of protocol actions (controller_protocol.py:8, managedos_firewall_protocol.py:6).

##### X05-09

**Low · Test coverage · confidence medium**

- **Where:** internal/addons/clusterobjects/program_test.go:278 (vocabulary.golden); cluster_objects_engine.py:564-582; roles/cluster_objects/meta/argument_specs.yml:21-30
- **What:** Go `Validate` holds the full program grammar. The role spec declares `steps`/`checks` as dict lists with no options. Python checks only the version, the step-kind set and the approval set. No Python test reads vocabulary.golden.
- **Impact:** A new Go step kind fails closed (`unsupported cluster object step`) only on a live cluster.
- **Fix:** Have a Python unit test run the engine's kind dispatch over vocabulary.golden. (effort S)
- **Checked:** Python fails closed, so this is a coverage gap and not a safety gap. The shallow nested mappings relate to B431.

#### Name collisions (task 2)
- **Same function repeated, cheap:** `failure`: 11 of the 21 are exact `(code, message, remediation)` pass-throughs to `diagnostics.NewFailureWithRemediation`, and the rest fix a code. `refusal`: 8 of the 12 are the same pass-through, but secrets/refusals.go:143 returns a Diagnostic with an Object and selectionfs:53 is a string sanitizer. `diagnostics` is a leaf every layer may import, so it could export `Refuse(code, msg, remedy)`. Low value at 3 lines each.
- **Same concept, per-capability content, deliberate:** `DecodeRequest` (9, all on `reconciliation.ThawVersion`; four re-validate after thaw, three do not), `EffectBearing` (7, each domain root's predicate for `lifecycle.EffectBearing`), `groups`, `impacts`, `decodeEvidence`, `requestFor`. The artifactserver/managedservice `groups` copies belong to [G17-01](#g17-01).
- **Same rule:** `reference` (5) plus `add`, covered by family 15.
- **Different things:** `named` (5: schema builder, unique lookup, label formatter ×2, file name), `holds` (2 identical contract assertions; storecontract, workspacecontract and kubevirt are unrelated), `unsupported` (3 platform refusals as policy; controller/selection.go:324 is unrelated), `validName`/`printable` ([X05-07](#x05-07)), `findNamed` ([X05-03](#x05-03)).

#### Three-way request validation (task 3)
- **Ceph core:** Go `Request` (requests.go:280 and following) → argument spec of 843 lines, pinned field-for-field by reflection (role_admission_test.go:65-123), so it is fully derivable and could be generated → Python `validate` (ceph_native.py:596) applies closed sets, bounds (delegated 1..128, names in order) and the auxiliary digest grammar. Keep the shape, bound and grammar checks: they are the trust boundary. The product rules are [P01-05](#p01-05). The gap is [X05-05](#x05-05).
- **Managed OS install:** Go requests.go and catalog.go:95 constants → argument spec of 194 lines (argument_spec_test.go:41, run over goldens, plus the central `roleRefusals`) → YAML asserts (apply.yml:2-24) that restate MarkerPath, HostKeyPath, NetworkDocumentPath, NetworkMarkerPath, the hosted root and the version. contract_versions_test.go:365-372 classifies those literals as `validated`, which proves each is classified, not that it equals the Go value. The version is held by `TestRoleVersionAssertionsMatchTheirArgumentSpecs` (ansible/assets_test.go:107). The Python side reads the Go goldens through ansible-core's validator (test_substrate_port.py). I did not confirm that the assert block runs there.
- **Cluster objects:** see [X05-09](#x05-09); the ownership mark is [G22-01](#g22-01).

#### Constants across languages (task 4)
- `AUXILIARY_NAMES`: contracts.go:40 and ceph_native.py:77. Pinned by auxiliary_test.go:137, which scrapes the Python tuple.
- Record bound 65536: record.go:65, adapter_protocol.py:41 (pinned) and controller_channel.py:12 (not pinned, [X05-08](#x05-08)).
- `/etc/bootwright/install-marker.json`: installation/catalog.go:95, anaconda apply.yml:14 and guest_agent_read.py:77. Classified, not equality-pinned.
- Issue cap 999: specs/api/input.md:111, one named const at addons/admission.go:251, and literals in 6 packages (family 15).
- `cluster-objects-v1`, `bootwright.io/owner` and the owner domain: clusterobjects/types.go:11-13 and cluster_objects_model.py:10-12. Pinned by the contract_versions_test domain string ([G22-01](#g22-01)).
- Already reported by lanes: the pull budget 4285 ([Y04-02](#y04-02)), the SSH crypto-policy reducer ([G14-02](#g14-02), [P03-03](#p03-03)) and the 64 TiB disk bound ([G14-03](#g14-03)).

#### Patterns
- Parity is kept by Go tests that scrape Python source with a regex: `pythonTuple` (auxiliary_test.go:92), host_reading_test.go:243 and record_test.go:417. That works, but each test writes its own scraper. One shared literal-extraction helper would make them uniform.
- Go-written goldens stored under tests/unit/goldens and read by both languages (kubevirt_grants_test.go:13, firewall_test.go:18, fdf_test.go:91) are the right parity mechanism and could replace the source scraping.

#### Strengths
- Consumer-owned ports and per-package goldens really are cheap. Most "collisions" are 3-line curries.
- The `contract_versions_test` literal classification and the central `roleRefusals` in the contract suite give the module one place that sees every request and every automation literal.
- The Ceph argument spec is mechanically pinned to the Go types, nested levels included.

#### Coverage notes
- ceph_native.validate is not exercised on Go request goldens ([X05-05](#x05-05)).
- The cluster-object step vocabulary is not shared with Python tests ([X05-09](#x05-09)).
- controller_channel.py:12 bound is not pinned ([X05-08](#x05-08)).

#### Already recorded
- [G21-03](#g21-03), [G17-01](#g17-01), [G17-02](#g17-02), [Y04-01](#y04-01), [G08-05](#g08-05), [G09-03](#g09-03), [G07-04](#g07-04), [G11-03](#g11-03), [G01-04](#g01-04), [G16-08](#g16-08), [G21-14](#g21-14), [G02-01](#g02-01), [G02-09](#g02-09), [G14-11](#g14-11), [G14-03](#g14-03), [G16-01](#g16-01), [G16-09](#g16-09), [G21-02](#g21-02), [G22-01](#g22-01), [P01-05](#p01-05), [Y04-02](#y04-02), [G14-02](#g14-02), [P03-03](#p03-03); the G09/G10 patterns (boundedJSON); the G13 pattern (read sequence); the G17/G02/G01 patterns (999 cap).
- Related: B431 (bare nested mappings in argument specs).

<a id="lane-x06"></a>

### Lane X06: Cross-cutting duplication in Python and Ansible YAML

Scope read: read in full: 4 protocol module stubs (ceph_smb, ceph_export, plus the DOCUMENTATION of all 27 parsed with yaml), 4 protocol actions (ceph_smb, ceph_export, artifact_server, infra_service), cluster_objects_read/write actions, artifact_server.py 1-180 and its diff against infra_service.py, all 16 `require`, 6 `canonical` and 5 `refusal` definitions, test_python_sites.py 1-60, test_collection_names.py, test_role_boundaries.py 255-433, storage_ceph_smb/tasks/*, managedos_host_firewall/tasks/provided_*. Skimmed: redfish_system_inspect main, ceph_smb main, libvirt_* helper heads, architecture.md 820-980. I classified all 381+215 Python pairs and 269 YAML pairs by script (read-only, inline). Not read: the YAML families that Y01, Y02, Y03 and Y04 already opened.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | N/A | |
| Security | N/A | |
| Efficiency | N/A | |
| Clearness | 3 | `refusal`, `require` and `canonical` each name two or three different things |
| DDD | N/A | |
| Non-duplication | 3 | 91% of the measured Python duplication is doc boilerplate; the YAML entry-point bodies and the guest-SSH copies are real logic |
| Right language | 4 | The cross-language copies are already reported ([Y01-01](#y01-01) SSH argv, [Y02-05](#y02-05), Y01 root-WWN) |
| Test coverage | 3 | Only 1 of 5 doc refusal lists is checked against code, and one has already drifted |

#### Measured split: documentation versus logic
- **Python, exact mode.** 5,021 pair-weighted lines. 4,562 (91%) fall inside DOCUMENTATION/EXAMPLES/RETURN, all in plugins/modules. 459 (9%) are code.
  - Within plugins/modules, 4,562 of 4,649 lines (98%) are documentation. This confirms P07.
- **Python, shape mode.** 3,857 of 4,424 (87%) are documentation. Of the 567 code lines, about 300 are literal tables that only look alike: nft rule lines in libvirt_routing.py:195-218 against the SSH option tuple in managedos_guest_ssh.py:23-43, and the kernel argument tuple in disk_discovery_boot.py:29-42.
- **Python logic actually copied** is about 160 lines: artifact_server/infra_service utilities and actions; cluster_objects read/write; the managedos SSH-worker trio ([P07-03](#p07-03)); the replication/smb argument spec; and worker preludes (forced, see the table).
- **YAML.** 3,672 pair-weighted lines:
  - (a) role entry-point bodies: 55%, 86 files in 29 roles.
  - (b) guest SSH and reachability: 13%.
  - (f) private task files within one role: 14%.
  - (d) playbooks: 8%.
  - (c) provided_* firewall files: 6%.
  - (e) across roles: 3%.
  - None of the YAML duplication is documentation.

#### Consolidated family table
| Family | Copies / files | Dup lines (removable, approx.) | Proposed owner | What forbids or limits it |
|---|---|---|---|---|
| Protocol module DOCUMENTATION (`*_protocol.py`, ceph module docs, redfish docs) | 41 module files in 9 groups; the largest group has 16 files | 1,618 covered (≤1,250; about 800 realistic) | a `plugins/doc_fragments/adapter_protocol.py` fragment via `extends_documentation_fragment` | Ansible needs a module file per action name for ansible-doc. No test forbids fragments. architecture.md:832 lists plugin roles without doc fragments, so it needs one sentence |
| One generic protocol action instead of 27 | 27 actions, already thin over `adapter_protocol.publish` | ~0 worth removing | already `adapter_protocol.Capability` | test_collection_names.py `plugin_misfit` requires each action to open with its invoking role's domain, so a shared action fails. Ansible task action names cannot be templated |
| managedos SSH worker modules (update/registration/firewall) | 3 | ~70 code | module_utils helper | nothing; [P07-03](#p07-03) |
| Shipped worker preludes (rhsm/update/firewall workers) | 3 | ~60 | keep | **forced**: named stand-alone exceptions at test_python_sites.py:33-35, 43-45 |
| redfish_system_inspect/read/firmware/boot | 4 | 33-34 doc lines each; 12 lines of code each | `redfish_control.ARGUMENT_SPEC` + `client(params)` | nothing (AnsiballZ ships module_utils); P07 Patterns |
| ceph_cluster/export, ceph_replication/smb | 2 pairs | doc only, plus a 12-line argument spec (ceph_replication.py:76-91 = ceph_smb.py:80-95) | doc fragment | nothing |
| artifact_server.py:23-84 = infra_service.py:34-95; actions 57-89 / 59-91 | 2 + 2 | 34 + 23 | infra_service.py | nothing; artifact_server already imports infra_service (line 10). tracked: B461 |
| (a) role entry-point run, refuse, fail and complete | 86 files, 29 roles (Ceph alone: 6 roles × 3) | 2,533 covered (~1,000 removable) | one private `run.yml` per role, with `operation`/`observed`/`removed` vars | **test**: `handoff_problems` (test_role_boundaries.py:280-296) allows only inline asserts before `loaded`, which pins the 12-13-line assert preamble. **Ansible**: the protocol FQCN differs per role and cannot be templated, so no file can be shared across roles. After `loaded`, a per-role fold is allowed: the observe allowlist follows `include_tasks` (cmd/bootwright/observe_playbooks_test.go:38), and only `PRINTED_COMPLETIONS` needs re-keying. [Y01-09](#y01-09), [Y04-09](#y04-09), [Y02-10](#y02-10) |
| (b) guest SSH session | 10 task files | 336 covered (~250) | an action using `managedos_guest_ssh` | none; [Y01-01](#y01-01) |
| (c) host_firewall provided_* | 3 private files | 222 covered (~160) | one `provided.yml` | none (private files); new, [X06-04](#x06-04) |
| (d) playbooks | install_apply 4×21; 10-line headers | 278 (~85 real) | play vars | headers are one playbook per port operation by design (architecture.md:830); [Y03-04](#y03-04) |
| (e)/(f) across or within roles | 33 files | ~430 | per finding | [Y02-02](#y02-02), [Y04-01](#y04-01), [Y01-04](#y01-04), [Y03-05](#y03-05) |

Files a change must touch today:
- a shared protocol option: 27 module docs.
- the Ceph run sequence: 18 entry files.
- the guest SSH pinning: 10 files plus Python.
- the KubeVirt port guard: 7 files.
- a Redfish connection option: 4 modules plus their docs.
- a libvirt run-with-environment rule: 4 files.

#### Findings
##### X06-01

**Medium · Test coverage / Quality · confidence high**

- **Where:** plugins/modules/ceph_export_protocol.py:12, 46-53; plugins/action/ceph_export_protocol.py:35; tests/unit/test_ceph_refusal_record.py:231-234
- **What:** The export protocol's DOCUMENTATION was copied from SMB and has drifted. It describes "SMB resource evidence" and lists `foreign-service` plus five `smb-*` reasons. It omits 108 reasons the action accepts: `CLUSTER_REASONS` plus 14 `export-*` reasons such as `export-prerequisite-zone`.
- **Evidence:** `- Publishes the shared adapter handshake, a named refusal and the closed SMB resource evidence.`; `refusals=ceph_core.CLUSTER_REASONS + ceph_export_native.REASONS`. Only ceph_cluster's choices are checked: `assert set(choices) == set(protocol.REFUSALS)`.
- **Impact:** Nothing validates action arguments against DOCUMENTATION (no `validate_argument_spec` in plugins), so this misleads only readers and ansible-doc. Every copied list can drift silently in the same way.
- **Fix:** Parametrize the test over every `*_protocol` module whose doc has `reason.choices` (ceph_cluster, export, image, replication, smb) against `CAPABILITY.refusals`. Fix the export doc. (S)
- **Checked:** I compared all five lists by script. smb, replication and image match; export drifted.

##### X06-02

**Medium · Non-duplication · confidence high**

- **Where:** plugins/modules/*_protocol.py (27 files, 41–247 lines); plugins/modules/ceph_{cluster,export,smb,replication}.py and redfish_*.py docs
- **What:** The 41 module files repeat the same option docs (`phase`, `detail`, `native_error`, `outcome`, `digest`, `evidence`, `observed`, `removed`) and EXAMPLES/RETURN boilerplate. The collection has no `doc_fragments`.
- **Evidence:** 4,562 of 4,649 duplicated module lines are inside doc strings. 1,618 doc lines are covered, of which one copy per group is 366.
- **Impact:** Every change to a shared option means editing 27 files, and [X06-01](#x06-01) shows those hand copies drift.
- **Fix:** Add `plugins/doc_fragments/adapter_protocol.py` and `extends_documentation_fragment: bootwright.core.adapter_protocol`. Each stub keeps only its own `reason` choices and `evidence`. Add one sentence to architecture.md:832 to admit doc fragments. (M)
- **Checked:** No unit test pins the plugin subdirectories. test_collection_names constrains only action and role names. P07 confirmed that the stubs hold documentation only.

##### X06-03

**Low · Non-duplication · confidence high**

- **Where:** module_utils/libvirt_firewall_ready.py:13, 78, 193; libvirt_vlan_refusal.py:41, 109, 277; libvirt_nm_connections.py:40, 62, 666; libvirt_routing.py:591
- **What:** Four sibling utilities each declare `ENVIRONMENT`, `command(runner, arguments)` and `require(runner, arguments)`. The copies have drifted: only nm_connections truncates output to `MAX_OUTPUT`. These siblings already import each other (routing imports `command` from vlan_refusal at line 27-28; vlan_refusal imports firewall_ready), so the stand-alone rule does not apply.
- **Evidence:** `return runner(arguments, check_rc=False, environ_update=ENVIRONMENT)` (firewall_ready:79) against `return code, output[:MAX_OUTPUT], error[:MAX_OUTPUT]` (nm_connections:64).
- **Impact:** The clean-environment rule and the output bound are kept in four places with different answers.
- **Fix:** Keep one bounded `command` plus `run_checked(runner, argv, message)` in libvirt_firewall_ready (the root of the import graph) and import it in the others. (S)
- **Checked:** [P05-03](#p05-03) covers the staging/fsync copies in the same files, not this helper.

##### X06-04

**Low · Non-duplication · confidence high**

- **Where:** roles/managedos_host_firewall/tasks/provided_apply.yml:8-44, provided_destroy.yml:1-39, provided_observe.yml:1-39
- **What:** The three private files are identical apart from the task name, `operation`, `observed`/`removed`, and apply's extra package install with its `changed` term.
- **Evidence:** `diff provided_destroy.yml provided_observe.yml` shows 3 changed lines (`operation: remove`/`inspect`, `removed: true`/`observed: true`, and the name).
- **Impact:** The refusal-detail rule (`host-firewall-foreign`, `host-firewall-no-interface`) must be kept equal three times.
- **Fix:** Use one `provided.yml` included with `operation`, `observed` and `removed` vars. Apply keeps its package task before the include. (S)
- **Checked:** These are private files, not entry points, so `handoff_problems` does not apply. [Y01-09](#y01-09) covers only the entry points of this role.

##### X06-05

**Low · Clearness · confidence high**

- **Where:** module_utils/machine_hardware.py:41; ceph_host_runtime.py:218-219; adapter_protocol.py:99, ceph_core.py:159, modules/managedos_update.py:224; libvirt_routing.py:591 against ceph_native.py:302
- **What:** Verdict on the name collisions:
- **`canonical`**: of 6 definitions, 1 is the owner (canonical_json.py:18). ceph_host_runtime's is a pass-through to `ceph_native.canonical`, which is an import of the owner. route_probe, tunnel and erase_worker are byte-equivalent copies, all named exceptions (test_python_sites.py:28-32). machine_hardware's is a different thing: it formats a MAC address.
- **`refusal`**: three meanings. It validates a reason name (adapter_protocol), classifies an exception into (reason, detail) (ceph_core and ceph_registry_discovery, the same concept twice), and builds a reply dict (3 workers and 3 modules).
- **`require`**: two meanings. Twelve are a raise-if-false assertion with a module-local refusal type, an acceptable idiom. Four libvirt ones run a command and check its exit code ([X06-03](#x06-03)).
- **`completion`** (25) and **`unproved`/`absence`/`presence`** are one deliberate slot, `Capability(completion)`, with per-capability bodies: not duplication.
- **Evidence:** `def canonical(raw): """The colon-separated lower-case form of one unicast six-byte address...`; `def canonical(value): return ceph_native.canonical(value)`.
- **Impact:** A reader cannot tell from a name whether `canonical` encodes JSON or `require` runs a process.
- **Fix:** Rename machine_hardware's to `mac_text`, ceph_host_runtime should import `canonical` directly, rename libvirt's to `run_checked`, and rename the reply builders to `refusal_reply`. (S)
- **Checked:** I opened every listed definition. python.md's one-module canonical rule holds: no unregistered copy exists.

##### X06-06

**Low · Quality / Non-duplication · confidence high**

- **Where:** plugins/action/cluster_objects_read.py:33-36, 39; cluster_objects_write.py:8-36
- **What:** The read action defines a `write` closure it never uses: it passes `None` to `Engine`. The 25-line request and closure setup is copied between the two actions.
- **Evidence:** `def write(action, obj, extra):` (read:33); `return Engine(request, read, None).execute(observe=True)` (read:39).
- **Impact:** Dead code, and two copies of the secret-materials remapping.
- **Fix:** Move `_run_program` setup into a module_utils helper that both actions call. Drop the unused closure. (S)
- **Checked:** `Engine.__init__(self, request, read, write=None, ...)` (cluster_objects_engine.py:49). P07 noted only the dead `return`s.

##### X06-07

**Low · Non-duplication · confidence high (tracked extension)**

- **Where:** plugins/action/artifact_server_protocol.py:57-117 and infra_service_protocol.py:59-115 (`remaining`, `unproved`, `absence`, `completion`)
- **What:** B461 tracks the module_utils half of this pair. The action half, 23 identical lines plus `completion`, is not in B461's exit evidence.
- **Evidence:** The diff shows only renamed nouns (`listeners`/`answers`, "artifact server"/"managed service").
- **Fix:** Widen B461's exit evidence to the actions, using a shared `service_completion(...)` in module_utils. (S)
- **Checked:** backlog.md:1025-1036.

#### Patterns
- The clone detector's Python figure is mostly ansible-doc boilerplate. Use the code-only share (about 9%) when you rank Python duplication.
- Cross-role YAML sharing is blocked by Ansible's static action names and by test_collection_names' per-domain protocol actions. The feasible unit of consolidation is one private task file per role (storage_ceph_*/tasks/{apply,destroy,observe}.yml; substrate_kubevirt_machine/tasks/*).
- Structural tests pin only the step before `loaded`. Most of the per-entry-point copying after `loaded` is habit, not constraint.

#### Strengths
- The protocol action plugins are already thin. `adapter_protocol.Capability` plus `publish` holds all the protocol logic once (ceph_smb_protocol.py:24-36).
- The admin-credential protocol is shared correctly: module_utils/admin_credentials_protocol.py serves two 17-line actions.
- test_python_sites.py names every stand-alone copy with a reason, and the exception lists can only shrink.

#### Coverage notes
- Only test_ceph_refusal_record.py:231 ties a doc choice list to code ([X06-01](#x06-01)).
- No test asserts that the libvirt `command` copies share the environment and bounds ([X06-03](#x06-03)).

#### Already recorded
- tracked: B461 (artifact_server/infra_service utilities)
- [P07-03](#p07-03) (managedos SSH-worker trio); P07 Patterns (Redfish connection spec)
- P05 Patterns (worker preludes, a policy trade-off)
- [Y01-01](#y01-01) (guest SSH, 10 files); [Y01-04](#y01-04); [Y01-09](#y01-09) and [Y04-09](#y04-09) (entry-point preambles); [Y02-02](#y02-02) and [Y02-10](#y02-10) (substrate arms); [Y03-04](#y03-04) (install_apply); [Y03-05](#y03-05); [Y04-01](#y04-01) (artifact/managed-service roles)
- B966 (role and plugin name misfits that constrain the protocol-action naming)

<a id="lane-x07"></a>

### Lane X07: Cross-cutting language placement
Scope read: architecture.md "Go and Ansible responsibility boundary", "Language responsibility", "Named exceptions", :1088-1111 (plugin results), :1132-1166 (version specialization); python.md Placement; the 25 heaviest Jinja tasks in full; native_specializations.py; native_resolution.py :215-460; ceph_core.py `evidence` and `execute`; ceph_native.py `dispatch`; the other five `dispatch` functions; cluster_objects_engine.py :274-470; managedos_host_firewall.py :85-215; libvirt_routing.py :165-252; bundlelocal/mirror.go; cmd/bootwright/mirror_probe.go; preflightlocal/resolver.go; e2e/render.sh; scripts/* wrappers; language_rows_test.go. / Skimmed: all 33 lane reports, the Go signals, the Go exec and network sites, the embedded-source constants, and the Templar-based role tests. / Not read: the Python plugin bodies beyond the listed functions, which other lanes own.

#### Scorecard
| Dimension | Score 1-5 | One-clause reason |
|---|---|---|
| Quality | N/A | |
| Security | N/A | |
| Efficiency | N/A | |
| Clearness | 3 | 13 tasks hold over 1,200 characters of Jinja each, with nested five-level ternaries |
| DDD | 3 | Rules such as proof requirements, budgets and port access are split across Go, Jinja and Python |
| Non-duplication | N/A | |
| Right language | 3 | Go's network contacts and Python's routing all stay within the boundary, but Jinja holds transformations, budget arithmetic and classifiers of native output |
| Test coverage | 3 | Real-Templar render tests pin most expressions, but embedded scripts are checked only by substring and the language-row test reads only paths and shebangs |

#### Findings
##### X07-01

**Medium · Right language / DDD · confidence high**

- **Where:** managedos_install_anaconda/tasks/boot.yml:15; containercluster_install_agent/tasks/boot.yml:85,219, release.yml:41, port.yml:12; machine_power_read/tasks/read_one.yml:2; machine_power/tasks/power.yml
- **What:** Every role that calls a machine port builds the `bootwright_machine_port` access dict in Jinja from request and material. That dict holds endpoint, verify, ca, user, password, optional pins and an optional `verification`. [Y03-04](#y03-04) reports this inside one role only; across roles there are 21 assignments in 12 task files of 4 roles, and 10 copies of the `verification` combine.
- **Evidence:** `| combine({'verification': ...controller.tlsVerification | default('')} if (...) | length > 0 else {})`. The pins also differ by copy: pre_boot (boot.yml:85) always passes `pinned_uuid_base64`, while boot_media (:219) and boot_disk (release.yml:41) pass it only `if ...portPinned`.
- **Impact:** Each new port field touches 4 roles. It is not visible whether the differences between copies are intended.
- **Fix:** Go freezes one per-target `machinePort` access object, naming the material variables the way machine_power_read already does (`userVariable` and the others). Each role then adds only its operation's arguments. (M)
- **Checked:** [Y03-04](#y03-04) (containercluster only); [Y02-05](#y02-05)/06 (the arm side); no B item.

##### X07-02

**Medium · Right language / Non-duplication · confidence high**

- **Where:** managedos_install_anaconda/tasks/apply.yml:371-397, private_recovery.yml:63-83; containercluster_media_agent/tasks/publish.yml:162-182; containercluster_install_agent/tasks/boot.yml:161-170; managedos_package_tree/tasks/apply.yml:207; physical_erase/tasks/boot.yml:64
- **What:** The classifier for a failed artifact-listener fetch is written in Jinja in 6 roles, with 11 copies of the regex. It sorts the failure into 403, 404, `CERTIFICATE_VERIFY_FAILED`, no status line, or another status. The copies have drifted.
- **Evidence:** apply.yml:392 bounds and cleans the cause with `| bootwright.core.printable)[:512]`. The publish.yml:179-181 copy does neither: `containercluster_media_agent_probe.msg | default(...) | replace(...token..., '<token>')`. boot.yml:166 uses different wording again.
- **Impact:** The media refusal prints native text without the `printable` filter or the 512-character bound, and no test renders that message: test_served_content_is_readable.py checks only the fetch.
- **Fix:** Add one `listener_refusal(result, token, secretRef, subject)` filter beside `printable.py`, with unit tests, and call it in all six roles. (S)
- **Checked:** grep of the backlog, audit 003 and the lane reports found nothing for "listener does not serve"; test_listener_fetch_retries.py covers only the retry condition.

##### X07-03

**Medium · Right language / DDD · confidence high**

- **Where:** managedos_install_anaconda/tasks/register.yml:63-66; internal/managedos/installation/catalog.go:110-111; plugins/modules/managedos_registration.py:463
- **What:** The registration worker's time budget is computed in Jinja: `(750 if 'update' ... else 630) + (60 if 'satellite' ...) + (60 if ...contentView ...)`, up to 870 s. Go's block bound is a separate constant with a stale rationale.
- **Evidence:** `// registrationBound covers the worker's 630 s session, up to 750 s with Satellite content selection ...` / `const registrationBound = 15 * time.Minute`. In the YAML, 750 is the update case, and Satellite plus a content view reaches 870 s, which leaves 30 s for "its policy read and inspection". The module also defaults to 630.
- **Impact:** A budget change in YAML can exceed the Go envelope silently. Nothing derives one from the other.
- **Fix:** Go freezes `registration.timeout` into the request and derives `registrationBound` from it; the YAML passes it through. (S)
- **Checked:** test_managedos_install_registration.py:530-554 pins the Jinja values by Templar render. No Go test ties them to `registrationBound`.

##### X07-04

**Low · Right language / Non-duplication · confidence high · policy trade-off**

- **Where:** infrastructureservices_mirror_release/defaults/main.yml:47-50 (plus [Y04-07](#y04-07)'s two task copies); plugins/action/infrastructureservices_mirror_release_protocol.py:37-40; internal/infrastructureservices/mirrorfill/releases_evidence.go:47-60; plugins/action/machine_power_protocol.py:43 against internal/machine/power/evidence.go:42-50
- **What:** architecture.md:1106-1110 has the plugin refuse a mutation whose evidence is missing and has Go decide again. So every postcondition is written at least twice, and the mirror release "held whole" rule five times in three languages.
- **Evidence:** Python `postcondition = bool(digest) and bool(entries) and missing == 0 and (not pinned or digest == pinned)`; Go `evidence.Digest != "" && len(evidence.Components) != 0 && evidence.Missing == 0 && evidence.Postcondition`.
- **Impact:** The copies agree today, but each change to a rule touches YAML, Python and Go.
- **Fix:** Have the plugin check only the presence of fields, as the spec words it, and leave the value predicate to Go. Delete the YAML copies ([Y04-07](#y04-07)). (M)
- **Checked:** Go re-derives both rules, so the Python boolean is redundant. [Y01-02](#y01-02) is the case where Go relies on flags computed in Jinja.

##### X07-05

**Low · Right language / Test coverage · confidence high**

- **Where:** plugins/module_utils/disk_discovery_boot.py:45-67 (`INIT`) and :70-92 (`DRIVERS`)
- **What:** The discovery initrd's PID-1 `init` and its driver probe are about 46 lines of shell held as Python strings. The Shell row allows only installer sections that Go emits. No test runs either script.
- **Evidence:** tests reference only `assert "/sys/bus/scsi/devices/*/modalias" in boot.DRIVERS` and `"--quiet" in boot.DRIVERS` (test_disk_discovery.py:57-59). `INIT` is never asserted.
- **Impact:** A syntax or `set -eu` error appears only on a real discovery boot.
- **Fix:** Keep both as real files under the collection that the payload reads. Add a `sh -n` check and a mocked-root run, the way target_proof_test.go runs Go-emitted shell. (S)
- **Checked:** no lane report or B item names `disk_discovery_boot` INIT or DRIVERS. [P04-03](#p04-03) covers the erase runtime only.

##### X07-06

**Low · DDD / Clearness · confidence medium**

- **Where:** specs/architecture.md:760-766; cmd/bootwright/mirror_probe.go:29; internal/controller/bundlelocal/mirror.go:36; internal/containercluster/preflightlocal/resolver.go:24-34
- **What:** architecture.md lists exactly two allowed Go network contacts (bundlelocal publisher metadata and medialocal). Preflight also sends HEAD requests to the artifact mirror from Go, and with `serverRef` that mirror is a managed ArtifactServer. Go also runs `getent ahosts`.
- **Evidence:** environment.md:383-395 sanctions the probe; openshift-coverage.md:84 sanctions getent. Both are read-only, bounded, and use the admitted route or NSS.
- **Impact:** The architecture's "Never contacts a managed component" row contradicts a sanctioned path, so a reviewer cannot tell from it which contacts are allowed.
- **Fix:** Name the mirror probe and the controller name-service read in the architecture's list. (S)

#### Go network and remote contacts (task 4)
Each site is an allowed case:
- bundlelocal/acquisition.go:44-54: publisher metadata.
- medialocal/acquirer.go:57-64: media import.
- acquisition/transport.go:73 `AnchoredDial`: used only by bundlelocal.
- machine/sshlocal launcher and copy: the named Machine SSH exception.
- hostowned/probe.go:27,189: `virsh -c qemu:///system`, read-only; named in the boundary paragraph.
- hostdnf: the controller's own dependencies.
- venvlocal: pip `--no-index` (:159), so it is offline.
- firewalllocal, comparisonlocal, selectionfs, privilege, nss: local only.
- mirror probe and getent: sanctioned elsewhere ([X07-06](#x07-06)).

No `InsecureSkipVerify` and no default HTTP client.

#### Heaviest Jinja tasks (task 1)
Kinds: D = decision, T = transformation, R = refusal, E = evidence. Tests: Tm = real-Templar render, Sim = hand-written Ansible simulator, St = structural, none.
| # | Task | Kind | Target | Test |
|---|---|---|---|---|
|1|install_agent wait.yml:67|R+D (stderr classification)|Python module ([Y03-01](#y03-01))|Sim|
|2|port.yml:12|T+D (deliveryKind, portPinned)|Go ([X07-01](#x07-01))|Tm|
|3|media build.yml:59|T (install-config with secrets)|filter or action ([G21-04](#g21-04))|Tm + filter units|
|4|managedos apply.yml:860|E+D (required-proof flags)|Go ([Y01-02](#y01-02))|Tm|
|5|observe.yml:174|copy of 4|Go ([Y01-02](#y01-02))|Tm|
|6|install_agent boot.yml:85|T|Go ([X07-01](#x07-01))|Tm|
|7|managedos apply.yml:371|R+classifier|filter ([X07-02](#x07-02))|Tm|
|8|register.yml:42|T+D (budget)|Go ([X07-03](#x07-03))|Tm|
|9|read_one.yml:2|T|Go ([X07-01](#x07-01))|Tm|
|10|waits.yml:63|R (rendezvous remedy)|keep; Go may freeze the rendezvous name|Sim/Tm|
|11|managedos boot.yml:15|T|Go ([X07-01](#x07-01))|Tm|
|12|private_recovery.yml:63|R+classifier|filter ([X07-02](#x07-02))|Tm|
|13|media apply.yml:119|R (rebuild causes)|keep|Tm|
|14|media publish.yml:162|R+classifier, drifted|filter ([X07-02](#x07-02))|none|
|15|install_agent boot.yml:219|T|Go ([X07-01](#x07-01))|Tm (KubeVirt spec only)|
|16-19|install_apply.yml:15/68/121/174|T (per-node credentials in playbook vars)|Go or role defaults ([Y03-04](#y03-04))|St|
|20|state.yml:46|E (branch on deliveryKind)|keep or filter|Tm|
|21|wait_attempt.yml:86|D (resume policy)|Python ([Y03-01](#y03-01))|Sim|
|22|release.yml:41|T|Go ([X07-01](#x07-01))|Tm|
|23|cluster_state.yml:89|D (stderr identity)|Python ([Y03-02](#y03-02))|Tm|
|24|libvirt_host apply.yml:562|E (changed roll-up)|keep|Tm|
|25|libvirt_machine apply.yml:408|E|keep|Tm|

Of these, 18 are decisions or transformations that should move, 3 are copies of one classifier, and 6 are legitimate refusals or evidence.

#### Python decisions (task 2)
- **The six `dispatch` functions:** route on `phase` and `action`, which Go chose and froze. Allowed. Clearness is [P01-04](#p01-04)/P01-11, and [P01-11](#p01-11) also notes that `dispatch` admits any CORE phase.
- **native_specializations:** allowed, because architecture.md:1134 says "Whatever the language" and the facts come from frozen artifact bytes.
- **native_resolution and ceph_packages:** allowed. ceph.md:195,552 makes DNF resolution an apply-time native step, and Go reads the "package decision the first step reported".
- **ceph_core `evidence` and `prove_*`:** these collect and check closed evidence, which is allowed. The exception is readiness: [P01-06](#p01-06) has Python compute `ready`, and Go should own that predicate.
- **Engine.chain and Engine.subscription:** collect evidence only. Go judges the chain at clusterobjects/evidence.go:245-260. The approval mode comes from the frozen request.
- **managedos_host_firewall `ssh_admitted`:** a precondition the spec owns (managed-os.md:2102-2103), so it is allowed. Coverage is 4% (P07).

Decisions that Go should own are already reported by other lanes: [P01-06](#p01-06), [P01-05](#p01-05), [G18-04](#g18-04), [P02-05](#p02-05), [G22-09](#g22-09), [Y01-02](#y01-02), and the P05 routing-model pattern.

#### Embedded languages (task 3)
| Kind | Where and size | Tested by | Safer form |
|---|---|---|---|
|Kickstart and shell in Go|kickstart.go (1,040 lines, about 100 shell lines), clear.go (about 84), encryption.go (about 33)|1 golden, `strings.Contains`, and bash runs with mocks in target_proof, physical_encryption and kickstart tests|sanctioned; [G16-05](#g16-05) covers the duplicated snippets|
|systemd units in Python|libvirt_routing.py:226-252, physical_erase_boot.py:183-204, vlan_refusal `unit_files`|exact-text asserts|acceptable; one helper ([P05-03](#p05-03))|
|nftables in Python|libvirt_routing.py:171-219 (49 lines)|frozen-text golden (test_libvirt_routing.py:332)|good|
|libvirt XML|Jinja templates (526 lines across all role templates); Python only parses with ElementTree|goldens (test_libvirt_host_tagged_golden)|good|
|Ceph service specs in Go|service_unit.go: typed JSON documents, not text|unit tests|good (the brief's premise does not hold)|
|YAML by hand in Go|addons/catalog/service.go:196-238 (skeleton writer, unquoted values)|none found|low risk: names are validated|
|Python as strings in Python|RUNTIME (142 lines, [P04-03](#p04-03)); host probes (about 113, [P03-09](#p03-09)); REGISTRY_COMPARISON 23; SOURCE_SCRIPT 19; DNS_SOURCE 13; RESOLVE 11; CONTROL_EXEC 4|`exec()` or none|shipped real files|
|Python as strings in Go|adapterprotocol entrypoint ([G02-10](#g02-10)); venvlocal probes (6 and 7 lines); test AST scanner ([T01-12](#t01-12))|Go tests|acceptable at this size|
|Shell in Python|disk_discovery_boot INIT and DRIVERS, about 46 lines ([X07-05](#x07-05))|substring only|real file plus `sh -n`|
|Shell in YAML|`sh -c` programs in 4 managedos files ([Y01-07](#y01-07)); oc wrapper build.yml:173-189|the wrapper is executed in test_containercluster_media_build_trust.py|acceptable|
|Shell in Make|release (62 lines), container-build (33)|none|[B01-03](#b01-03), [T01-10](#t01-10)|
|Python in e2e bash|app-setup.sh, about 35 lines|none|[E01-06](#e01-06)|

#### Shell (task 5)
All `scripts/*` except quick-test and check-commits (B967) only pin an environment and run a tool. `stop-check` holds a little selection logic, but python.md:82 names it as a sanctioned guard. e2e/render.sh stays within its row: a helm render and an awk post-pass, with no Bootwright command and no host contact.

#### Summary per language (task 6)
| Language | What it holds today that the table forbids | Migration target |
|---|---|---|
|Ansible YAML/Jinja|212,000 characters in 18,100 lines; 56 tasks over 600 characters; 18 of the top 25 are decisions or transformations (port access, budgets, required-proof flags, stderr classifiers); 11 classifier copies|Go freezes port access, budgets and flags; filters for classifiers; Python modules for the wait loop|
|Python|readiness predicate, capability grants, budget and PG formulas, exact-compare list, admission re-checks; about 330 lines of Python and 46 of shell as strings|Go owns the judgements; shipped real files|
|Go|no forbidden contacts; two sanctioned contacts missing from the architecture list|update the spec ([X07-06](#x07-06))|
|Shell|quick-test and check-commits (B967); Makefile release and container-build|`scripts/tools` launchers|

#### Patterns
- Proof requirements are computed in Jinja and judged in Python: apply.yml:888, observe.yml:199, protocol.py:220 ([Y01-02](#y01-02)).
- Budgets are written in several languages: register.yml:64 ([X07-03](#x07-03)), ceph host pull 4285 s ([Y04-02](#y04-02)), NVMe-oF ([P02-05](#p02-05)).
- One classifier of native output per role instead of one filter: [X07-02](#x07-02), [Y03-01](#y03-01), [Y03-02](#y03-02).

#### Strengths
- Go's network surface is small, pinned and fully enumerable. No `InsecureSkipVerify`, no default client, and pip runs `--no-index`.
- Python routing honours "Go chose the phase and action". Version specialization follows the spec's record shape.
- Role Jinja is tested with the real Templar, not with regexes. Exact-text goldens cover nft and the kickstart.

#### Coverage notes
- test/architecture/language_rows_test.go:64-66 classifies a file only by its path and shebang. Logic in Jinja, embedded sources and Makefile recipes are invisible to it, and no fitness test bounds Jinja size per task.
- containercluster_media_agent/tasks/publish.yml:162: the refusal text is never rendered by any test.
- disk_discovery_boot.py:45: `INIT` is never executed or asserted.
- managedos_host_firewall.py:151: `ssh_admitted` has 4% coverage (P07).

#### Already recorded
- [Y03-01](#y03-01), [Y03-02](#y03-02), [Y03-04](#y03-04), [G21-04](#g21-04), [Y01-02](#y01-02), [Y01-05](#y01-05), [Y01-07](#y01-07), [Y04-02](#y04-02), [Y04-06](#y04-06), [Y04-07](#y04-07), [P01-05](#p01-05), [P01-06](#p01-06), [P01-11](#p01-11), [P02-05](#p02-05), [G18-04](#g18-04), [G22-09](#g22-09), [P03-09](#p03-09), [P04-03](#p04-03), [G02-10](#g02-10), [T01-12](#t01-12), [G16-05](#g16-05), [B01-03](#b01-03), [T01-10](#t01-10), [E01-06](#e01-06), [P05-03](#p05-03), [P07-01](#p07-01): cited, not restated.
- B967: quick-test and check-commits.
