# Code audit 003: consolidated report

Consolidation of [report-codex.md](report-codex.md) (Codex with GPT-6 Astra
and GPT-6.1 Sol agents, 2026-11-05, product baseline `f8b15669f`, completion
baseline `88fbf7be1`) and [report-claude.md](report-claude.md) (Claude Fable
5.1 with Claude Opus 5.5, Sonnet 5.5 and Haiku 5.5 agents, 2026-11-05,
baseline `f8b15669f`). Written by Claude Fable 5.1 on 2026-11-05 at
`85cd3ac9a`, after both reports existed. Between `f8b15669f` and `85cd3ac9a`
only the container build (`Dockerfile`, `Makefile`, `.dockerignore`,
`README.md`, `docs/development.md`,
`test/architecture/container_build_test.go`) and the two audit reports
changed; every Go, Ansible, specification and e2e file either report cites is
byte-identical at `85cd3ac9a`, so both reports' citations resolve at the
current tree and the two assessments compare directly.

Method of the merge. Each of the 18 Codex findings was re-read against the
tree and against its Claude counterpart by a reconciler (Opus), then checked
by an independent refuter where the two reports agree and by three lenses
(code reproduction, specification reading, refutation) where they do not. The
three severity disputes went to a panel of three judges with distinct lenses
(operator consequence on a qualified path, trust and privilege rules,
reachability and prevalence), and a Fable arbiter decided every split. The
three Codex findings that have no Claude counterpart and the ADD-02 candidate
that Codex declined to promote were verified from scratch the same way.
Because Codex did not examine the other Claude findings and declined to adopt
their count, every one of the 53 Claude-only Medium findings received a fresh
refutation pass (Opus), with a defender re-reading each challenge and an
arbiter on any split; the Low findings carry Claude's three-lens verification
and were not re-verified here beyond those Codex matched. The container-build
delta that only Codex reviewed was reviewed again, and the two reports' claims
about each other were checked at the report level. Codex's P2 and P3 map to
Medium and Low on the Claude scale, which this report uses throughout; Codex
reported no P1. In total 133 agents ran; every citation in the entries below
was re-read at `85cd3ac9a` by at least two of them, and the effect-free
probes they ran are listed under [verification](#verification).

This document changes no product code, contract, specification or milestone
state and authorizes no implementation or live run. Milestones own delivery
state; the [sequence](#consolidated-remediation-sequence) is a proposal for
the owner, as [D210](../../specs/milestones/backlog.md#d210) was for audit
002.

| If you want | Read |
| --- | --- |
| The combined verdict, the consolidated counts and what to fix first | [Verdict](#verdict) |
| Where each Codex finding landed and what the merge changed about it | [Agreement map](#agreement-map) |
| Every High and Medium finding in one table, with its sources | [Consolidated findings](#consolidated-findings) |
| The entries the merge changed, added or corrected | [Entries](#entries-changed-added-or-corrected-by-the-merge) |
| The three severity disputes and the other disagreements, with the decision on each | [Where the reports disagree](#where-the-reports-disagree) |
| What one report found and the other did not | [What each report missed](#what-each-report-missed) |
| The ordered work, the owner decisions and the carriers | [Consolidated remediation sequence](#consolidated-remediation-sequence) |
| What this consolidation ran and did not run | [Verification](#verification) |
| Every entry of both reports with its final severity | [Complete catalog](#complete-catalog) |

## Verdict

The two reports agree on the shape of the result. Both find the destructive
boundaries defended before the effect they guard: disk ownership, removal
ordering, controller-host protection, physical erase, uninstall, the secret
store, strict admission and the frozen-request bounds. Both find the same
discrepancy classes behind their findings: admission accepts what planning or
a native worker then refuses; adapters fold different remote answers into one
outcome; the privilege boundary is enforced call site by call site rather than
by construction; and delivered decisions leave specification text behind.
Neither finds anything Blocking.

Every one of Codex's 18 findings is real at `85cd3ac9a`. Fifteen are the same
finding as a Claude entry: the fourteen Codex cross-referenced, and C16, which
Codex did not cross-reference but which is Claude's SCR-02. Three have no
Claude counterpart and enter the catalog as new entries: KubeVirt completion
proof ([SBV-15](#sbv-15)), the Go `%q` quoting in the cluster-object
projection template ([ADD-16](#add-16)) and the uncleared `cluster info`
result on cancellation ([CLJ-08](#clj-08)). The two candidates Codex recorded
as observations rather than findings, the scratch-layout clause
([RCP-11](#rcp-11)) and the `oc` error-format parse ([ADD-02](#add-02)), stay
confirmed findings here: the facts are not in dispute for the first, and
three independent lenses confirmed the second from the pinned client's own
binary.

Of the three Claude High findings that Codex rated P2, two stay High and one
becomes Medium. `render` publishing as root ([CLI-01](#cli-01)) stays High
because the scale's "a trust, privilege or durability rule is not enforced"
matches literally on the default non-root journey, and Codex's bounds (a
fixed file name, atomic publication, a regular-file-only replacement, no
secret content) rule out Blocking without making the path narrow. The pending
upgrade InstallPlan ([ADD-01](#add-01)) stays High on its observation half
alone: a healthy, delivered nmstate or metallb instance reads unknown with a
cluster-destroying remedy once the catalog head moves, which the spec forbids
in two places; Codex is right that the apply half rests on a clause that
literally supports the refusal, so clarifying that clause is part of the
remedy rather than a second contradiction. The controller SSH policy parser
([SEC-07](#sec-07)) becomes Medium: the defect and its FIPS prevalence are
proved, but the paths it blocks (Ceph from a FIPS controller, placement
checks on a FIPS host) are not yet qualified, the accepted FIPS controller
runs never reached this parser, it fails closed, and a non-FIPS placement host
is a workaround. It still blocks the FIPS qualification rows deterministically
once they run, so it needs an item ahead of them.

Codex's doubt about the larger report's count did not survive the check for
the Medium tier. Of the 53 Claude-only Medium findings, 39 hold exactly as
written, 13 hold at Medium with a correction to a trigger, consequence, title
or citation, and one ([CLI-03](#cli-03)) is lowered to Low because the owning
specification describes the refusal the entry called a defect. None was
refuted and none turned out to be already tracked. Claude's own accounting is
internally consistent (325 catalog rows: 313 confirmed, 9 merged, 3 tracked),
and Claude makes no clause-completeness claim; it bounds coverage by lane
ledgers and calls its registers incomplete by construction. The 246 Low
findings carry Claude's three-lens verification only, except the three Codex
matched.

| Measure | Claude | Codex | Consolidated |
| --- | --- | --- | --- |
| Confirmed findings | 313 (3 High, 64 Medium, 246 Low) | 18 (15 P2, 3 P3) | 320 (2 High, 66 Medium, 252 Low) |
| Of which unsafe | 7 | not classified | 8 |
| By kind | 146 defect, 64 doc-stale, 52 drift, 29 test-gap, 10 gap, 7 unsafe, 5 improvement | not classified | 148 defect, 65 doc-stale, 52 drift, 30 test-gap, 10 gap, 8 unsafe, 7 improvement |
| Confirmed but already recorded as open work | 3 (B248, B412, B99) | 1 partly (B122) | 3, plus B122 as a partial record of OUT-01 |
| Merged duplicates | 9 | none | 9 |
| Entries changed by the merge | | | 2 lowered (SEC-07, CLI-03), 13 narrowed, 16 re-stated with corrections |
| Entries added by the merge | | | 7: SBV-15, ADD-16, CLJ-08 from Codex; BLD-01 to BLD-04 from the build delta |

**What to fix first.** The order below merges Claude's roadmap with Codex's
correction sequence; the full sequence is at the end.

1. [CLI-01](#cli-01) (High, unsafe): `render` writes as root and checks
   `--sensitive` ownership against root. Route publication through the
   invoking account as `diff --output-dir` already does.
2. [MOI-04](#moi-04) (Medium, unsafe): stage the identity pair and the
   recovery passphrase at their final mode, owner and label before the rename,
   as the images are staged.
3. [ADD-01](#add-01) (High) with [ADD-02](#add-02), [ADD-11](#add-11) and
   [ADD-16](#add-16) (Medium): the cluster-object engine's OLM and JSON
   edges. A pending upgrade plan must be reported, never approved and never
   fatal; the webhook retry must read the reason `oc` prints; a transient
   placement failure must retry; the projection must emit JSON.
4. [SEC-07](#sec-07) (Medium): admit the fixed `GSSAPIKeyExchange no` form in
   the controller parser before B484's Ceph FIPS rows and B346's live FIPS
   row run, because both would refuse deterministically today.
5. [SEC-01](#sec-01), [PRJ-03](#prj-03), [RCP-11](#rcp-11),
   [SCR-05](#scr-05) and [SEC-06](#sec-06): the remaining ambient-trust
   paths, each narrow, together a boundary enforced site by site.
6. [RCS-02](#rcs-02) (Medium, unsafe, owner decision) before the physical
   agent path is qualified, and [SBV-15](#sbv-15) (Medium) before the
   KubeVirt arm is accepted: one proves the disk the installer erases, the
   other proves that a machine the arm calls complete has a ready root and its
   frozen shape.

**What this report does not claim.** It is a source and test assessment with
effect-free probes, like both reports it merges. No host, cluster, BMC or
container engine was reached, no command with effects ran, and every clause
that only a live run can prove remains unverified. The consolidation
re-verified the Medium tier and the Codex set; it did not re-run the Low tier
or re-read the 36 lane ledgers, so the coverage limits each lane states in
the Claude report still bound what "met" means for the clauses it examined,
and the three Codex-only findings show that a ledger row marked met can hide a
defect on a path the finder did not trace.

## Agreement map

One row per Codex finding, plus the two candidates Codex recorded as
observations and the build delta only Codex reviewed. "Same" means the two
reports describe one discrepancy with the same trigger and consequence; the
disposition names what the merge settled. Codex's P2 is Medium and P3 is Low
on the scale used here.

| Codex | Claude | Consolidated | Final | Disposition |
| --- | --- | --- | --- | --- |
| C01 (P2) | RCS-02 (Medium) | [RCS-02](#rcs-02) | Medium, unsafe | same; Claude's "no existing test covers a physical node's root selector" is wrong (an existing test pins `/dev/sda` as accepted on the physical catalog); Codex's "require a supported stable identity or refuse" adopted, because the agent path carries no size and a serial-with-size hint cannot work there |
| C02 (P2) | MOI-04 (Medium) | [MOI-04](#moi-04) | Medium, unsafe | same; the Claude entry contradicts itself (Medium headline, Low body) and the merge settles Medium on the private-publication clause Codex cites; the primitive window is proved, a live disclosure is not |
| C03 (P2) | none | [SBV-15](#sbv-15) | Medium, defect | Codex only; verified by three lenses; the frozen-shape half breaks an explicit clause, the readiness half is the code's own intent and a spec gap |
| C04 (P2) | CLI-01 (High) | [CLI-01](#cli-01) | High, unsafe | severity dispute; High by the arbiter after three lenses and three judges (two of three lenses and all three judges voted High) |
| C05 (P2) | SEC-01 (Medium) | [SEC-01](#sec-01) | Medium, defect | same; one citation corrected; two qualifier copies already exist and should merge into one |
| C06 (P2) | PRJ-03 (Medium) | [PRJ-03](#prj-03) | Medium, unsafe | same; both reports' environment citation off by one line; the runtime lock pins the same ansible-core release Codex inspected |
| C07 (P2) | SEC-07 (High) | [SEC-07](#sec-07) | Medium, defect | severity dispute; Medium by the arbiter (all three lenses and two of three judges voted Medium); prevalence proved, qualified path not established |
| C08 (P2) | ADD-01 (High) | [ADD-01](#add-01) | High, defect | severity dispute; High by the arbiter (two of three lenses and all three judges voted High); Codex's `upgradePending` correction adopted; the apply-half clause becomes part of the remedy |
| C09 (P2) | none | [ADD-16](#add-16) | Medium, defect | Codex only; verified by three lenses and a probe over the exact production template |
| C10 (P2) | CRB-07 (Medium) | [CRB-07](#crb-07) | Medium, defect | same; the source supports Claude's direction (raise the worker bound to 63); one Claude history citation is a cluster-name limit, not the bucket limit |
| C11 (P2) | CEA-08 (Medium) | [CEA-08](#cea-08) | Medium, defect | same; the owner must settle which spec page gives way; admission acceptance with all five bindings is inferred, not tested |
| C12 (P2) | ADD-11 (Medium) | [ADD-11](#add-11) | Medium, defect | same; Codex's title says observation, the path is placement during apply; the placement code discards the create's output, which the fix must capture |
| C13 (P2) | API-01 (Medium) | [API-01](#api-01) | Medium, defect | same; no factual disagreement |
| C14 (P2) | ENV-06 (Medium) | [ENV-06](#env-06) | Medium, defect | same; no factual disagreement |
| C15 (P2) | OUT-01 (Medium) | [OUT-01](#out-01) | Medium, defect | same; both see B122 as a partial, stale record; `status` never writes a block outcome either, and the spec does not define one |
| C16 (P3) | SCR-02 (Low), not cross-referenced by Codex | [SCR-02](#scr-02) | Low, defect | same finding; both triggers reproduced |
| C17 (P3) | none; the CLJ ledger marks the clause met | [CLJ-08](#clj-08) | Low, defect | Codex only; verified by three lenses and a cancellation probe; the ledger verdict rested on the service-side clearing only |
| C18 (P3) | SCR-05 (Low) | [SCR-05](#scr-05) | Low, unsafe | same; trigger inferred on both sides, not observed on a host |
| other observations | RCP-11 (Low) | [RCP-11](#rcp-11) | Low, defect | facts and impact agreed; kept as a counted Low conformance defect, as the clause is not met as written |
| other observations | ADD-02 (Medium) | [ADD-02](#add-02) | Medium, defect | Codex declined to promote pending the pinned `oc` error form; three lenses confirmed the parser rejects the only form the pinned binary can print |
| container-build delta | not assessed | [BLD-01](#bld-01) to [BLD-04](#bld-04) | Low | Codex found no defect in the delta and every Codex statement about it holds; this consolidation's review adds four Low items |

The 295 Claude entries Codex did not examine have no row: 53 Medium ones are
settled in the [sweep](#claude-only-medium-findings-after-the-sweep) and the
rest carry Claude's verification unchanged.

## Consolidated findings

Severity on the Claude scale: High is a specified behavior missing or wrong
with an operator-visible consequence on a qualified path, or an unenforced
trust, privilege or durability rule; Medium is wrong or missing behavior with
a workaround or on a narrow or unqualified path, a spec that contradicts
intentional code, or a safety-relevant clause with no test; Low is naming,
wording, output detail, documentation drift and test gaps without behavioral
consequence. Kinds are the Claude report's. Every High and Medium entry is
listed; a linked ID has an entry below because the merge changed, added or
corrected it, and an unlinked ID is carried from the Claude report as
written, where its entry holds the evidence, counterevidence and exit
evidence. Low findings are in the [complete catalog](#complete-catalog).

| ID | Severity | Kind | Lane | Sources | Title |
| --- | --- | --- | --- | --- | --- |
| [ADD-01](#add-01) | High | defect | addons | Claude ADD-01; Codex C08 | A pending upgrade InstallPlan makes an exact-policy observation of a healthy pinned add-on read unknown and a continued apply refuse |
| [CLI-01](#cli-01) | High | unsafe | cli-parsing | Claude CLI-01; Codex C04 | Under sudo elevation, render publishes its output with root credentials and holds the --sensitive ownership check to root instead of the invoking account |
| [ADD-02](#add-02) | Medium | defect | addons | Claude ADD-02; Codex other observations | The webhook-window create retry never fires, because the status reason is parsed only from a JSON Status, which oc does not print on a refused create |
| ADD-03 | Medium | defect | addons | Claude ADD-03 | maxOpenShiftVersion is read from a CSV annotation key that does not exist, so the evidence never records it |
| [ADD-11](#add-11) | Medium | defect | addons | Claude ADD-11; Codex C12 | A placed Secret whose module-side read or create fails transiently fails the attempt at once as 'differs at unproved placement' instead of being retried until the deadline |
| ADD-15 | Medium | test-gap | addons | Claude ADD-15 | No test proves that a Secret placement of a type other than Opaque or kubernetes.io/tls is refused at planning |
| [ADD-16](#add-16) | Medium | defect | addons | Codex C09 | The cluster-object projection template quotes strings and keys with Go printf %q, so a declared value holding a control or non-printable character yields invalid JSON, and a persisted create can never be read back or receipted |
| [ADA-09](#ada-09) | Medium | defect | api-addons | Claude ADA-09 | MetalLB held-address check omits CephCluster ingress, NFS and management-gateway virtual_ip addresses |
| CEA-05 | Medium | test-gap | api-ceph | Claude CEA-05 | Cluster SSH key separation refusals in CephCluster admission have no test |
| [CEA-08](#cea-08) | Medium | defect | api-ceph | Claude CEA-08; Codex C11 | An admitted nvmeof service with enable_auth: true and its five required mTLS bindings refuses every plan of its cluster as an unrealized service binding |
| CEA-10 | Medium | drift | api-ceph | Claude CEA-10 | An omitted CephNFSExport native.sectype is admitted, so NFS-Ganesha's default security flavors (including AUTH_NONE) apply instead of sys only |
| [ENV-06](#env-06) | Medium | defect | api-environment-secrets | Claude ENV-06; Codex C14 | An HTTP-only ArtifactServer inherits a defaults.ArtifactServer tls block and is then refused, instead of HTTP suppressing the inherited TLS |
| ENV-08 | Medium | drift | api-environment-secrets | Claude ENV-08 | An authored empty or all-zero CephPool replicated block selects the replicated arm and blocks an inherited defaults.CephPool.erasure |
| [API-01](#api-01) | Medium | defect | api-grammar | Claude API-01; Codex C13 | Image grammar admits a reference with no repository component, such as a short name `squid:6.10` or `registry.example.test:5000` |
| MCH-01 | Medium | defect | api-machines | Claude MCH-01 | Install-address containment in a managed libvirt network is skipped when the Machine attaches by substrate.libvirt.interfaceAttachments |
| OCA-05 | Medium | defect | api-openshift | Claude OCA-05 | Secondary-network uplink checks find the install interface from the default route, so a node with no default route escapes the install-interface and br-ex VLAN refusals |
| ARC-01 | Medium | defect | architecture | Claude ARC-01 | General-flow packages read the CephCluster's spec.management field path, and the architecture test explicitly does not look for field paths |
| [CPC-01](#cpc-01) | Medium | defect | ceph-coverage | Claude CPC-01 | Planning refuses every CephCluster whose spec.mgrModules admission accepts (cephadm, rbd_support) with a lifecycle.unsupported refusal no spec states |
| [CPD-01](#cpd-01) | Medium | defect | ceph-devices-resources | Claude CPD-01 | A completed DB/WAL allocation is refused device-not-empty whenever cluster preparation runs again, and the remedy tells the operator to clear that disk |
| [CPD-03](#cpd-03) | Medium | gap | ceph-devices-resources | Claude CPD-03 | The same disk selected by WWN on one host and by serial on another, or seen by two hosts, is never compared across hosts |
| CPD-04 | Medium | defect | ceph-devices-resources | Claude CPD-04 | Coordinator accepts any well-formed endpoint digest from a bound mgmt-gateway unit; the specified `evidence` refusal for another digest does not exist |
| CPR-01 | Medium | defect | ceph-ingress-removal | Claude CPR-01 | A pre-existing NVMe-oF subsystem created by another tool refuses nvmeof-identity, not nvmeof-foreign |
| CPR-02 | Medium | test-gap | ceph-ingress-removal | Claude CPR-02 | The tiebreaker authorized_keys rewrite's changed-since-read refusal has no test, and refusals raised after the attempt record report that nothing changed |
| CPL-06 | Medium | gap | ceph-lifecycle | Claude CPL-06 | A cephadm.addressRef or bootstrap.addressRef that differs from the fleet SSH address is admitted by the API, then refused at planning as 'not yet modeled'. No spec or item tracks this refusal |
| CPN-02 | Medium | defect | ceph-native-api | Claude CPN-02 | Service-unit placements silently drop the network and daemon-name parts of decorated host tokens that admission accepts |
| CPN-08 | Medium | drift | ceph-native-api | Claude CPN-08 | Host-layout owner exception and FSID-directory rule are wider in code than ceph-native-api states: mgmt-gateway and oauth2-proxy gain the image-user owner, and the FSID directory may belong to a singleton's user |
| CRA-01 | Medium | defect | ceph-resources-a | Claude CRA-01 | Placement-group budget counts an unauthored replicated pool at width 3 instead of the cluster default replica size |
| CRA-02 | Medium | drift | ceph-resources-a | Claude CRA-02 | Replicated size or minSize of 1 is admitted but refused at plan for pools and by an opaque worker 'request' refusal for policies; the spec states no such floor |
| [CRA-08](#cra-08) | Medium | defect | ceph-resources-a | Claude CRA-08 | The plan refuses an RGW count_per_host of 1 that the spec and admission accept, and the MDS coverage term for count_per_host can only be 1 |
| [CRB-01](#crb-01) | Medium | defect | ceph-resources-b | Claude CRB-01 | A CephConsumer named with 52 to 63 bytes is admitted and planned, then refused at apply by the consumer worker's 51-byte name limit |
| CRB-02 | Medium | defect | ceph-resources-b | Claude CRB-02 | CephNFSExport admits `protocols: []` and `transports: []`; the worker refuses them only at apply, with an opaque request refusal |
| CRB-03 | Medium | defect | ceph-resources-b | Claude CRB-03 | Consumer prerequisite and key-type refusals do not name the missing prerequisite or the allowed key types |
| CRB-06 | Medium | defect | ceph-resources-b | Claude CRB-06 | A filesystem-wide SMB share is admitted beside a share of another SMB cluster on the same filesystem |
| [CRB-07](#crb-07) | Medium | defect | ceph-resources-b | Claude CRB-07; Codex C10 | A CephObjectBucket named with 45 to 63 characters is admitted and planned, then refused at apply by the bucket worker's stale 44-character limit with a generic request reason |
| [OUT-01](#out-01) | Medium | defect | cli-output | Claude OUT-01; Codex C15 | apply and destroy results report every block with outcome "" and attempts 0 and never carry unresolved, and human result rows omit the outcome |
| OUT-10 | Medium | defect | cli-output | Claude OUT-10 | A setup run that cannot be created, removed or numbered fails the whole controller install instead of discarding dnf's output |
| OUT-11 | Medium | defect | cli-output | Claude OUT-11 | Retained adapter output is published only on the next write, so the lines a wedged adapter printed last stay in memory until the run ends |
| CLI-02 | Medium | defect | cli-parsing | Claude CLI-02 | Bare `render`, which only prints help, is classified as requiring root and elevates through sudo |
| CLI-04 | Medium | drift | cli-parsing | Claude CLI-04 | The sudo-entry environment guard runs before raw bounds and on help, version and completion, contradicting their no-effect contracts |
| CTX-01 | Medium | defect | contexts | Claude CTX-01 | A prompted context update or delete refuses over an abandoned publication stage in state/ with no entry or exit named, though --yes succeeds |
| [CTX-02](#ctx-02) | Medium | defect | contexts | Claude CTX-02 | An init killed between creating the context directory and recording its identity leaves the name stuck: init retry and delete --purge both refuse with no exit |
| CTH-01 | Medium | defect | controller-host | Claude CTH-01 | A re-setup rewrites the install receipt so packages Bootwright installed become 'present before bootwright' or vanish, and uninstall then keeps them |
| CTH-02 | Medium | defect | controller-host | Claude CTH-02 | A setup resumed after an interrupted package install records the packages that install added as 'present before bootwright' |
| CTS-01 | Medium | defect | controller-stage | Claude CTS-01 | Wheel fetch collapses DNS, certificate, timeout and unreachable failures into one routable 'could not be fetched' diagnostic, so a certificate failure is told to set HTTPS_PROXY |
| CTS-02 | Medium | drift | controller-stage | Claude CTS-02 | Spec still describes a Go read-only tool verification that streams sources and members and re-checks oc's release stamp, but X89 deleted it and Present is presence only |
| INF-01 | Medium | defect | infra-services | Claude INF-01 | Readiness outcome classification is inverted: an unanswered listener fails the attempt, while a certificate mismatch is recorded unknown |
| [INF-05](#inf-05) | Medium | defect | infra-services | Claude INF-05 | Managed DNS readiness proves TCP with one A query for records[0] only, so an IPv6-only first record never completes and TCP proves no declared address |
| INF-07 | Medium | defect | infra-services | Claude INF-07 | A managed NTPServer bound to one address also opens an unreserved wildcard server socket of the other address family |
| [MOI-01](#moi-01) | Medium | defect | managedos-media-install | Claude MOI-01 | A fresh apply converges the network and records the marker proof without comparing the installed marker to the frozen one |
| [MOI-04](#moi-04) | Medium | unsafe | managedos-media-install | Claude MOI-04; Codex C02 | Private installer files (identity, identity.pub, recovery) are published with ansible.builtin.copy straight into the served private directory, so each sits at a umask-derived mode before chmod 0640 and the label, unlike the staged image |
| MOI-05 | Medium | doc-stale | managedos-media-install | Claude MOI-05 | Budgets section claims a physical target stays within the stated bounds with at most six interfaces, but the code bounds 64 interfaces under separate, larger physical bounds whose deadline passes the 6-hour ceiling, and the spec has no Not yet met line |
| OCI-02 | Medium | defect | openshift-install | Claude OCI-02 | Own-image recognition also requires the port to match, so a BMC that echoes the URL without the listener's non-default port reports a foreign image |
| OCN-01 | Medium | defect | openshift-nodes-coverage | Claude OCN-01 | diff --observe reports taint drift on a fully placed node whose authored taints are unsorted or repeated |
| [PRJ-03](#prj-03) | Medium | unsafe | project-glossary | Claude PRJ-03; Codex C06 | Adapter ansible-core still searches the system-wide /usr/share/ansible/plugins paths, so a vars plugin installed there by root is imported into every adapter run |
| [RCC-06](#rcc-06) | Medium | defect | reconciliation-core | Claude RCC-06 | The context store refuses a plan.json over 1 MiB, so the 6 MiB stored-plan bound is never reachable |
| RCD-07 | Medium | defect | reconciliation-diff | Claude RCD-07 | diff --observe bounds concurrent observations only by the engine bound, ignoring the per-cluster throttle and exclusive claims an operation enforces |
| RCS-01 | Medium | defect | reconciliation-safety | Claude RCS-01 | The RAM erase worker and the Ceph host runtime prove a physical machine's MAC set without the management controller host-interface exception |
| [RCS-02](#rcs-02) | Medium | unsafe | reconciliation-safety | Claude RCS-02; Codex C01 | A physical OpenShift agent node may select the root disk it erases by a kernel name such as /dev/sda, which physical disk safety forbids but the OpenShift domain spec admits |
| RCS-05 | Medium | test-gap | reconciliation-safety | Claude RCS-05 | The RAM erase worker's installer-isolation and RAM-root refusals (isolate) have no unit test; the only test replaces isolate with a mock |
| SCR-01 | Medium | defect | secrets | Claude SCR-01 | A keyring whose store.json or metadata fails authentication refuses secret.store.corrupt without the two exits the spec requires |
| [SEC-01](#sec-01) | Medium | defect | security | Claude SEC-01; Codex C05 | Root-run exec sites for /usr/bin/getent, /usr/bin/firewall-cmd and the SSH client skip the root-owned executable and ancestry qualification, and the first two inherit the invoker's working directory |
| SEC-02 | Medium | gap | security | Claude SEC-02 | No internal-error boundary: a Go panic prints a full goroutine stack, and no correlation identity exists |
| [SEC-06](#sec-06) | Medium | gap | security | Claude SEC-06 | Wheel fetch redirects are not revalidated for destination or private-address policy, and no Go dialer checks the resolved connection target |
| [SEC-07](#sec-07) | Medium | defect | security | Claude SEC-07; Codex C07 | The controller-side SSH policy parser refuses the GSSAPIKeyExchange no line that the FIPS crypto-policy back end carries, so Ceph controller sessions and placement-host key checks refuse ssh-policy-unavailable on a FIPS host whose policy is readable, while the tunnel parser admits the same line |
| [SBP-11](#sbp-11) | Medium | defect | substrates-physical | Claude SBP-11 | An unanswered system read in the bare-metal target proof is swallowed and reported as hardware-mismatch, dropping the BMC's own message |
| [SBV-01](#sbv-01) | Medium | defect | substrates-virtual | Claude SBV-01 | Default routes in any table are dropped from the managed-network collision check, so a policy-routed full-tunnel default (strongSwan table 220) never refuses |
| SBV-11 | Medium | defect | substrates-virtual | Claude SBV-11 | An emulated BMC on an SSH-placed libvirt provider never claims its shared bmc: key, so a physical Machine of another context addressing that endpoint is never refused |
| [SBV-15](#sbv-15) | Medium | defect | substrates-virtual | Codex C03 | KubeVirt machine completion ignores the result of the root DataVolume wait, and observation never re-checks the frozen VirtualMachine fields, so a Pending root or a drifted VirtualMachine reads as completed |

## Entries changed, added or corrected by the merge

Each entry states the consolidated severity and kind, what holds at
`85cd3ac9a`, what each source report adds, what the merge corrects in the
source entries, and the remedy and exit evidence both reports support. Line
numbers are at `85cd3ac9a`; the source entries hold the fuller evidence and
counterevidence and are linked rather than repeated.

### CLI-01

**High, unsafe.** Sources: Claude [CLI-01](report-claude.md#cli-01) (High),
Codex [C04](report-codex.md#c04-render-publishes-as-the-effective-account)
(P2). Decided by the arbiter after three lenses (reproduce High, spec Medium,
refuter High) and three judges (all High).

Under sudo elevation, `render` publishes its output with root credentials and
holds the `--sensitive` ownership check to root instead of the invoking
account. Proved from source: `render` is registered through `available`,
which sets `privileged` ([catalog.go:20-23](../../internal/cli/catalog.go),
[commands_nativeartifacts.go:11](../../internal/cli/commands_nativeartifacts.go));
`ClassifyInvocation` has no `render` exception before it sets `RequiresRoot`
([invocation.go:50-61](../../internal/cli/invocation.go)); production wiring
passes `renderlocal.Files{}` with no UID
([wiring.go:175](../../cmd/bootwright/wiring.go)); `Publish` runs
`os.MkdirAll` under the elevated credentials
([files.go:58](../../internal/nativeartifacts/renderlocal/files.go)) and the
ownership check compares the directory owner with `os.Geteuid`
([files.go:130-135](../../internal/nativeartifacts/renderlocal/files.go)).
That contradicts [cli.md:502-504](../../specs/cli.md) ("the output directory
must be owned by the invoking account") and [cli.md:396-398](../../specs/cli.md)
("Root identity governs only root-owned runtime storage and the Bootwright
executable"). Because context-backed `render` always requires `--sensitive`
([commands_nativeartifacts.go:13](../../internal/cli/commands_nativeartifacts.go),
[cli.md:491-493](../../specs/cli.md)), every valid non-root context-backed
`render` either refuses the operator's own 0700 directory with "is not owned
by this account" or creates root-owned 0700/0600 output the operator cannot
read. A probe confirmed `RequiresRoot` for context-free, context-backed and
bare `render`.

Codex adds the bounds: a fixed file name, publication through one held handle
with an atomic rename, a regular-file-only replacement, no secret content, no
code execution shown, direct root unaffected. Claude adds the production
wiring, the inverted check, the `diff --output-dir` model for the fix and the
unit tests that inject a UID and so pass without covering production. The
merge corrects two details: Claude's "only the comparisonlocal helper switches
credentials" is too broad (two read-side openers also do, but comparisonlocal
is still the only helper that writes as the invoking account), and the
refused-own-directory and root-owned-output outcomes are inferred from
`Geteuid() == 0` in the elevated child, not observed in a sudo run. The spec
does not state who owns a directory `render` creates; it is silent, and the
ownership rule for non-sensitive output follows from the operator-named-path
principle at [cli.md:969-972](../../specs/cli.md). Elevating context-free
`render --input-dir` at all is a catalog choice the spec neither mandates nor
forbids.

Severity: High. The scale's "a trust, privilege or durability rule is not
enforced" matches literally on the default non-root journey. Codex's bounds
rule out Blocking (nothing secret is exposed, nothing beyond a regular
`effective-state.yaml` is replaced) but do not reach Medium, which needs a
narrow or unqualified path. The inferred workarounds (`render effective` to
stdout, a root login) do not lower the grade because the spec defines
`render --output-dir` as its own journey.

Remedy and exit: write `render`'s output through the invoking-account helper
as `diff --output-dir` does, or inject the invoking account's UID into
`renderlocal.Files` and publish through a descriptor opened under that
account; keep the path checks; decide separately whether context-free `render`
elevates; add the spec sentence on who owns a created directory. Exit: a
`cmd/bootwright` test composing `render` with an elevated child and a non-root
invoking account shows a user-owned 0700 directory accepted with
`--sensitive`, a root-owned one refused, and the created file owned by the
invoking account; extend the existing `render --input-dir` row in
`invocation_test.go` (which today asserts only the ambient route) to assert
the chosen class.

### SEC-07

**Medium, defect.** Sources: Claude [SEC-07](report-claude.md#sec-07) (High),
Codex [C07](report-codex.md#c07-controller-ssh-policy-rejects-a-disabling-directive)
(P2). Decided by the arbiter after three lenses (all Medium) and three judges
(rule Medium, consequence Medium, reach High).

The controller-side SSH policy parser refuses the `GSSAPIKeyExchange no` line
that the FIPS crypto-policy back end carries, so Ceph controller sessions and
placement-host key checks refuse `ssh-policy-unavailable` on a FIPS host whose
policy is readable, while the tunnel parser admits the same line. Proved from
source and reproduced: `DIRECTIVES` has no `gssapikeyexchange` and any other
directive raises `PolicyError` with reason `ssh-policy-unavailable`
([ssh_key_policy.py:22-27, 72-73](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ssh_key_policy.py));
[ceph_ssh_policy.py:27-33](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_ssh_policy.py)
maps it to `PolicyRefusal(UNAVAILABLE)`; the tunnel parser admits the fixed
`no` form
([ceph_tunnel.py:83-84](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_tunnel.py));
the Go reducer neither admits nor refuses the line and drops it
([policy.go:78-84](../../internal/machine/sshlocal/policy.go)). Fed this
host's shipped back ends, DEFAULT, FUTURE and LEGACY pass all three parsers
and only FIPS is refused, by the two controller parsers. The binding clause
is [security.md:595-602](../../specs/security.md): the back end "reduced to
its crypto directives", with `ssh-policy-unavailable` reserved for "a policy
that cannot be read or evaluated"; the FIPS back end can be read and
evaluated.

Prevalence is proved for this crypto-policies release, which settles the
factual dispute in Claude's favor: the generator writes the line whenever a
policy admits no GSS key exchange
(`/usr/share/crypto-policies/python/policygenerators/openssh.py:183-186`),
the FIPS policy admits none, no shipped subpolicy module re-enables GSS, and
the repository's own knowledge page records the same line in a RHEL-generated
FIPS back end
([ibm-ceph-native-ssh.md:158-167](../../.agents/knowledge/ibm-ceph-native-ssh.md)).
The RHEL 9.8 controller back end itself was not read here, so Codex's
narrower "not established for every qualified FIPS controller" is right
about RHEL and wrong about the mechanism.

Severity: Medium, which lowers Claude's High. The scale's High needs a
qualified path, and the affected paths are not qualified yet: the Ceph FIPS
rows "belong to B484 and stay unrun"
([delivered.md:7411-7412](../../specs/milestones/delivered.md)), B346's live
FIPS controller row is open ([m1.md:731-736](../../specs/milestones/m1.md)),
"Bootwright claims no FIPS compliance" and the FIPS runtime is parked as B331
([controller.md:84-85](../../specs/controller.md)), and the accepted FIPS
RHEL 9.8 controller runs X294 and X302 built OpenShift against a DEFAULT
placement host and never reached this parser. The refusal fails closed and a
non-FIPS controller or placement host is a workaround. It still blocks
B484's and B346's rows deterministically once they run, so it needs an item
scheduled ahead of them. The merge also corrects the clause base: the
spec's `GSSAPIKeyExchange no` admission at
[security.md:602-607](../../specs/security.md) is stated for the tunnel
client only, and [security.md:690-693](../../specs/security.md) governs the
Go machine session, which works on FIPS by dropping the line; the controller
parsers' behavior is a spec gap plus a Python-versus-Go inconsistency, not a
clause the spec already settles.

Remedy and exit: admit the fixed `GSSAPIKeyExchange no` form,
case-insensitively, in `ssh_key_policy.configuration`, refusing `yes` and
unknown variants, or drop non-crypto non-unsafe lines as `policy.go` does;
state one reduction rule for the three parsers at security.md:595-607. Exit:
a parametrized test in `test_ssh_key_policy.py` accepting a back end with the
line plus algorithm lines and refusing `GSSAPIKeyExchange yes`; a
`ceph_ssh_policy` test that `evaluate_controller_policy` does not raise
`UNAVAILABLE` for it; the physical-erase controller and the managed-OS,
firewall and bare-metal role tasks covered by the same fixture.

### ADD-01

**High, defect.** Sources: Claude [ADD-01](report-claude.md#add-01) (High),
Codex [C08](report-codex.md#c08-a-pending-upgrade-plan-invalidates-a-healthy-pinned-add-on)
(P2). Decided by the arbiter after three lenses (reproduce High, spec Medium,
refuter High) and three judges (all High).

A pending upgrade InstallPlan makes an exact-policy observation of a healthy
pinned add-on read unknown and a continued apply refuse. Proved from source
and reproduced with the real engine: the exact-policy branch hands whatever
`status.installPlanRef` names to `plan()`
([cluster_objects_engine.py:271](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py)),
whose validity test requires `clusterServiceVersionNames == [csv]` (line
365); a pending upgrade plan lists the newer CSV, so observation sets
`self.unknown` and returns (372-374), apply raises "approval proof differs;
CSVs ..." (375), and the first reading, the Namespace, is marked unknown
(456-458). The `installed_plan()` recovery runs only on the operator-managed
branch. Five of six built-in packages use exact approval (acm, gitops,
kubevirt, metallb, nmstate); only fdf is operator-managed. The spec says "the
current CSV is reported, never compared, and a pending upgrade waits for the
operator" ([add-ons.md:341-343](../../specs/add-ons.md)) and makes an
observation unknown only for "a foreign object, a failed read or an installed
CSV other than the expected one", with the remedy "destroy and apply again"
([add-ons.md:446-449](../../specs/add-ons.md)). The repository's own
knowledge records that a newer channel head points `installPlanRef` at a
pending plan while the installed operator is healthy
([olm-v0-exact-install.md:35-39, 71-75](../../.agents/knowledge/olm-v0-exact-install.md)).

Codex's correction is adopted: observation does set `upgradePending` to true
(line 281 runs after `plan()` returns `None`) while ending unknown; Claude's
entry said the exact path cannot reach it, which is true only for apply, where
the refusal is raised before line 281. Codex's second point is also adopted:
[add-ons.md:334-336](../../specs/add-ons.md) ("any other plan fails the
attempt naming its CSVs") literally supports the apply refusal, and the exact
policy has no counterpart to the operator-managed sentence "approves nothing
and fails on no plan" at 359-360, so the apply half is a spec-versus-spec
conflict (334-336 against 341-343 and 446-449) that the fix must settle, not a
second contradiction. Further corrections to the Claude entry: cite 341-343,
446-449 and 355-361 rather than 340-343, 444-449 and 352-358; "failing B1125
drills" is inferred, since B1125 is "Specified, not started"
([m4.md:613](../../specs/milestones/m4.md)); the fresh-apply race and the
dependent ACM-hub Subscription variant are inferred from the shared code
path, not probed.

Severity: High, on the observation half alone. A specified behavior is wrong
with an operator-visible consequence (a healthy, delivered nmstate or metallb
instance read as unknown with a cluster-destroying remedy, continuation
refused) on the default catalog path of five packages; nothing is written
(the probe recorded zero writes), so it is not Blocking. Codex's Medium is
right only about the apply half.

Remedy and exit: when `status.installedCSV` equals the expected CSV, find the
plan that installed it among the Subscription's InstallPlans, as
`installed_plan()` does for the operator-managed policy; treat a plan whose
CSV list differs as the pending upgrade, report it and never approve or fail
on it; keep the refusal for a plan that would install the first CSV with
extra CSVs; clarify add-ons.md:334-336 in the same change. Exit: a unit test
where `installedCSV` equals `startingCSV`, `currentCSV` is newer and
`installPlanRef` names an unapproved upgrade plan, in which observe
classifies completed with `upgradePending` true, apply returns unchanged with
no approve write, and the dependent variant behaves the same.

### RCS-02

**Medium, unsafe.** Sources: Claude [RCS-02](report-claude.md#rcs-02),
Codex [C01](report-codex.md#c01-physical-agent-installation-admits-an-unstable-root-disk-selector).
Unanimous.

A physical OpenShift agent node may select the root disk it erases by a
kernel name such as `/dev/sda`, which physical disk safety forbids but the
OpenShift domain spec admits. [state-reconciliation.md:2416-2421](../../specs/state-reconciliation.md)
requires selection "by a durable disk identity ... never by a kernel device
name another boot may give another disk";
[openshift-clusters.md:40-42](../../specs/openshift-clusters.md) admits
physical agent nodes and the refusal row at line 120 refuses only a
`deviceName` other than `/dev/<name>` or `/dev/disk/by-path/<name>`. The
agent installer's physical branch adds only trust refusals
([selection.go:171-175](../../internal/containercluster/agentinstall/selection.go))
and `installerRootDeviceHints` passes `/dev/sda` through (429-460); the
managed-OS by-id rule never reaches this path because it needs an installed
Machine with `installProfileRef`, which agent nodes refuse. Both reports agree
the conflict is between the two spec pages and that the owner must settle it.

The merge corrects Claude's claim that no existing test covers a physical
node's root selector: `TestEveryDeclaredRootDeviceHintReachesTheAgentConfig`
on the physical catalog pins `/dev/sda` as accepted
([projection_test.go:652-675](../../internal/containercluster/agentinstall/projection_test.go),
line 669), and it passes at HEAD, so the current behavior is guarded, not
untested. It also narrows Claude's remedy: a serial-with-size hint is not
viable on the agent path, because the installer renders no `sizeBytes`
(selection.go:448-451) and admission accepts serial-with-size only for
installed Machines; Codex's "require a supported stable identity (a `wwn`
hint) or refuse the physical shape" is the executable form. Codex adds that
the residual-risk exception at state-reconciliation.md:2441-2450 does not
waive durable selection. A related inconsistency:
[openshift-clusters.md:579-580](../../specs/openshift-clusters.md) says a
physical node "refuses before registration", which fits neither lines 40-42
nor the code.

Severity: Medium, unsafe, as both reports rate it: the path is not qualified
on real hardware ([m3.md:588-590](../../specs/milestones/m3.md)), UUID, MAC
and power proofs still identify the machine, and the `data-loss` token the
operator consumes covers an erase whose target is unproven. Owner decision:
either require a `wwn` hint for a physical agent node and refuse otherwise,
or record the agent exception in physical disk safety and correct
openshift-clusters.md:120 to match. Exit: a test beside
`TestARootDeviceTheAgentInstallerCannotCarryRefuses` that a physical node with
`deviceName: /dev/sda` and no durable hint refuses before registration, and
the physical catalog fixture changed accordingly.

### MOI-04

**Medium, unsafe.** Sources: Claude [MOI-04](report-claude.md#moi-04),
Codex [C02](report-codex.md#c02-private-publication-temporarily-relaxes-file-permissions).
Unanimous.

Private installer files (`identity`, `identity.pub`, `recovery`) are
published with `ansible.builtin.copy` straight into the served private
directory, so each sits at a umask-derived mode before `chmod 0640` and the
label are applied, unlike the staged image. Proved from source:
[private.yml:28-54](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/private.yml)
and
[private_recovery.yml:9-27](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/private_recovery.yml)
copy with `remote_src` and `mode: '0640'` and label afterwards; the pinned
ansible-core 2.21.5 `copy` module calls `atomic_move` before applying the
requested attributes, and for a new destination that primitive sets
`0666 & ~umask` (basic.py:1814-1819, copy.py:637 and 667 in the gate venv);
no production code sets a restrictive umask for these tasks. The images are
staged, moded and labelled before their rename
([apply.yml:266-294](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/apply.yml)).

The merge settles two things. The Claude entry contradicts itself: its
headline says Medium and its "Severity and kind" line says Low, calling the
finding a hardening gap because [managed-os.md:422-425](../../specs/managed-os.md)
promises umask independence for the images only. Codex cites the stronger
clause: the private-publication contract at
[infrastructure-services.md:508-521](../../specs/infrastructure-services.md)
requires every file to be `0640 root:root` so that "no other account is
granted anything", and the transient 0644 breaches it for secret material.
That settles Medium. Second, the consequence: Claude's probe read the
passphrase in 20 of 20 runs of a synthetic race under umask 022 and notes the
directory token in the image `mv` argument vector; Codex proves the primitive
window and stops there. The source supports the ordering (the image `mv`
runs after the identity copy and before the recovery copy, so argv token
discovery reaches the recovery window only), but a live disclosure depends on
the remote host's umask, SELinux policy and process visibility and is not
established here. The 0711 directories, the random token, `no_log`, the final
mode and per-attempt cleanup remain.

Remedy and exit: stage the three files in the work area or a 0700 directory
at `0640 root:root` with the served root's label and publish each with
`mv --no-target-directory`, as the image is; test the publication boundary,
not only the final mode. Exit: an Ansible unit test asserting that every
private publication task is a rename from a pre-moded, pre-labelled staged
file and that no `copy` has a destination under the private directory.

### SEC-01

**Medium, defect.** Sources: Claude [SEC-01](report-claude.md#sec-01),
Codex [C05](report-codex.md#c05-some-native-executables-bypass-required-qualification).
Unanimous.

Root-run exec sites for `/usr/bin/getent`, `/usr/bin/firewall-cmd` and the
SSH client skip the root-owned executable and ancestry qualification, and the
first two inherit the invoker's working directory.
[resolver.go:26-28](../../internal/containercluster/preflightlocal/resolver.go)
and [native.go:24-26](../../internal/controller/firewalllocal/native.go)
run their binaries with only `Env` and `WaitDelay` set, no `Dir` and no
ownership check; the SSH launcher's qualifier checks a regular file with an
executable bit and nothing else
([launcher_linux_amd64.go:395-409](../../internal/machine/sshlocal/launcher_linux_amd64.go)),
while `qualifiedSystemExecutable` proves root ownership and no group- or
other-writable bit on the file and every parent
([account_linux_amd64.go:90-120](../../internal/controller/privilege/account_linux_amd64.go)).
Codex's probe had the SSH qualifier accept a synthetic mode-0777 executable
without running it. [security.md:55-60](../../specs/security.md) admits an
executed file "only when it and every directory above it are owned by root",
and [security.md:245-246](../../specs/security.md) requires a fixed safe
working directory.

Corrections: Codex's `native.go:24-27` is 24-26 (line 27 is the output
capture); Claude's D152 citation ([backlog.md:4245](../../specs/milestones/backlog.md))
is a proposal that authorizes nothing, so security.md:55-60 owns the rule;
and two copies of the qualifier already exist (privilege and hostdnf), so the
fix is one shared qualifier, not a third copy. Both reports agree that the
trigger needs a misconfigured host and that no compromised binary or
escalation was observed; working-directory inheritance through sudo is
inferred.

### PRJ-03

**Medium, unsafe.** Sources: Claude [PRJ-03](report-claude.md#prj-03),
Codex [C06](report-codex.md#c06-the-adapter-retains-system-plugin-search-paths).
Unanimous.

Adapter ansible-core still searches the system-wide
`/usr/share/ansible/plugins` paths, so a vars plugin installed there by root
is imported into every adapter run. [ansible.cfg](../../ansible/ansible.cfg)
pins `collections_path` and `roles_path` only; the adapter environment sets no
plugin-path variable
([process_linux_amd64.go:105-110](../../internal/adapterprotocol/process_linux_amd64.go),
both reports cite 104-110, but line 104 is the working directory); the pinned
ansible-core 2.21.5 defaults every plugin type to `ANSIBLE_HOME/plugins/<type>`
plus `/usr/share/ansible/plugins/<type>` and imports every legacy vars plugin
before `REQUIRES_ENABLED` is checked. [security.md:247-248](../../specs/security.md)
says ambient plugins are not authority. Codex inspected the effective paths
under the shipped configuration; Claude lists every plugin type to pin and
names the privileged-run secret exposure. The merge adds that the runtime
lock pins the same ansible-core release Codex inspected
([lock.json](../../internal/controller/runtimelock/lock.json), lines 37-39),
and that the FQCN-strategy counterevidence does not cover connection
plugins, which are selected unqualified (`local`, `ssh`), so shadowing them
from the system path is reachable in principle (inferred, not reproduced).
Writing to `/usr/share/ansible` needs root, so this is not an unprivileged
escalation; no plugin was planted. Remedy: pin every plugin-path variable to
the bundle in the adapter environment and test the effective loader paths
with the pinned runtime, including a planted-plugin negative test.

### CRB-07

**Medium, defect.** Sources: Claude [CRB-07](report-claude.md#crb-07),
Codex [C10](report-codex.md#c10-bucket-names-fail-only-at-the-native-worker).
Unanimous.

A CephObjectBucket named with 45 to 63 characters is admitted and planned,
then refused at apply by the bucket worker's stale 44-character limit with a
generic `request` reason. Go admission checks only `len(name) < 3`
([object.go:188-192](../../internal/storage/cephcluster/object.go)) and its
test admits 63 characters; the planner freezes the name unchanged and hashes
only the block identity
([object_bucket.go:28-30, 51](../../internal/storage/ceph/object_bucket.go))
and plans a 63-character bucket
([object_bucket_test.go:131-138](../../internal/storage/ceph/object_bucket_test.go));
the worker requires at most `MAX_BUCKET_NAME = 44`
([ceph_native_object.py:98, 286](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_object.py))
and its test pins the refusal of 45 characters with `^request$`. The specs
disagree with each other: [ceph-resources.md:1626](../../specs/ceph-resources.md)
says 3 to 63, [api/ceph.md:428](../../specs/api/ceph.md) still says 3 to 44.

The merge settles the direction Codex left open: raise the worker to 63 and
correct api/ceph.md:428, because [api.md:249-257](../../specs/api.md) forbids
shortening a name for an internal prefix, the hashed block identity removed
the reason for 44, and both Go tests pin 63. One Claude citation is wrong:
[m2.md:1736](../../specs/milestones/m2.md) is the X57 cluster-name limit, not
bucket history; only m2.md:858 is. The generic `request` refusal also breaks
api.md:255-257, which requires a native-limit refusal to name the rule. Exit:
the worker unit test admits 63 and refuses 64, the Go tests keep admitting
63, and both spec lines say 3 to 63.

### CEA-08

**Medium, defect.** Sources: Claude [CEA-08](report-claude.md#cea-08),
Codex [C11](report-codex.md#c11-authenticated-nvme-of-bindings-are-refused-by-planning).
Unanimous.

An admitted `nvmeof` service with `enable_auth: true` and its five required
mTLS bindings refuses every plan of its cluster as an unrealized service
binding. Admission requires the five bindings
([native_bindings.go:32-36](../../internal/storage/cephcluster/native_bindings.go));
planning returns `unrealizedBinding` unless `gatewayExecuted` (cases only for
ingress and rgw, [services.go:198-214](../../internal/storage/ceph/services.go))
or `unitBinding` (grafana, snmp-gateway, mgmt-gateway, oauth2-proxy only,
[service_unit.go:445-457](../../internal/storage/ceph/service_unit.go))
holds ([requests.go:374-378](../../internal/storage/ceph/requests.go)),
although `nvmeofLeaves` defines the paths; both the cluster plan and the
subsystem plan go through `baseRequest`, so both refuse, with a message that
never mentions nvmeof. Corrections: the realization clause runs to
[api/ceph.md:124](../../specs/api/ceph.md), not 123; the historical "`enable_auth`
(`false` only)" record is m2.md:1011-1014 and is history; `nvmeofLeaves` is
reachable through `unitLeaf`, so only the type allowlist excludes nvmeof; and
admission's acceptance of a service with all five bindings is inferred from
the predicates, since no test or probe covers it. Codex's direction stands:
do not weaken TLS checks or ignore bindings; either realize the admitted
mode or refuse it at admission with a field-named diagnostic. Which spec page
gives way (api/ceph.md:119-124 against the native mTLS contract at
[ceph-native-api.md:982-990](../../specs/ceph-native-api.md)) is an owner
decision; B480 awaits operator acceptance and the example README directs
operators onto this path.

### ADD-11

**Medium, defect.** Sources: Claude [ADD-11](report-claude.md#add-11),
Codex [C12](report-codex.md#c12-transient-placed-secret-observation-is-not-retried).
Unanimous.

A placed Secret whose module-side read or create fails transiently fails the
attempt at once as "differs at unproved placement" instead of being retried
until the deadline. `secrets.place` returns not accepted with `unknown` when
the private read fails or the post-create read does not prove the Secret
([cluster_objects_secrets.py:70-72, 84-94](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_secrets.py));
the engine raises on any not-accepted result without reading `unknown`,
pausing or looping
([cluster_objects_engine.py:149-150](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py)),
while ordinary objects reprobe inside a loop (116-125). The retry duty follows
from [add-ons.md:410-412](../../specs/add-ons.md) (placement creates as any
owned object) and 286-295. Corrections: Codex's title says observation, but
observe mode returns before any write (139-141), so the defect is on the
placement path during apply; the spec sentence is at 410-412 (Claude 411-413,
Codex 407-413); and `secrets.place` discards the create's stdout and stderr
(line 83), so the fix must capture them and parse the status reason as
`native.create` does, which makes [ADD-02](#add-02) a dependency only if that
parse is reused. No secret is disclosed and the attempt ends unknown, so
repeating apply recovers. Exit: unit tests for eventual success within the
deadline, deadline expiry and a proved mismatch, kept distinct.

### API-01

**Medium, defect.** Sources: Claude [API-01](report-claude.md#api-01),
Codex [C13](report-codex.md#c13-image-admission-accepts-unqualified-references).
Unanimous; no factual disagreement.

Image grammar admits a reference with no repository component, such as
`squid:6.10`, `squid@sha256:<64 hex>` or `registry.example.test:5000`.
`validImage` checks the part before the tag or digest only with
`validRegistry`, which leaves its component loop on an empty path
([lexical.go:332-350, 361-371](../../api/v1alpha1/lexical.go)), against
[api.md:596-603](../../specs/api.md). Both reports reproduced the three
admissions. The merge adds the five fields the shared rule guards (the
OpenShift release image, two OLM catalog source images, the managed-service
local and public images) and that the existing test has no refused row
without a repository ([lexical_test.go:219-240](../../api/v1alpha1/lexical_test.go)).
Untracked. The downstream outcome (a pull deferred to the native runtime, or
a short-name resolution) depends on the consumer's registry configuration
and was executed by neither report. Exit: the grammar test refuses the three
forms, still admits every currently admitted row and a one-component form,
and every `image()` field runs through the shared rule.

### ENV-06

**Medium, defect.** Sources: Claude [ENV-06](report-claude.md#env-06),
Codex [C14](report-codex.md#c14-http-listeners-inherit-forbidden-tls-defaults).
Unanimous; no factual disagreement.

An HTTP-only ArtifactServer inherits a `defaults.ArtifactServer` `tls` block
and is then refused, instead of HTTP suppressing the inherited TLS as
[environment.md:305-310](../../specs/api/environment.md) requires. The
ArtifactServer suppressions key only on `management`
([infrastructure_services.go:52-60](../../api/v1alpha1/infrastructure_services.go)),
`inherit` copies the default after suppression
([defaults.go:44-59](../../internal/desiredstate/compilation/defaults.go)),
and `validateTransport` refuses `tls` on an HTTP-only listener set
([admission.go:490-491](../../internal/infrastructureservices/admission.go)).
Both reproduced it; Claude adds that the diagnostic's remedy "(from
Environment defaults)" cannot be followed, and that the `management: external`
suppression works. No unsafe state is admitted. Exit: an inheritance test
where an HTTP-only listener set suppresses an inherited `tls`, omitted
listeners keep the HTTPS default, and an authored `tls` on an HTTP-only
server still refuses.

### OUT-01

**Medium, defect.** Sources: Claude [OUT-01](report-claude.md#out-01),
Codex [C15](report-codex.md#c15-operation-results-omit-required-block-facts).
Unanimous. Partly recorded by [B122](../../specs/milestones/backlog.md#b122).

`apply` and `destroy` results report every block with `outcome ""` and
`attempts 0` and never carry `unresolved`, and human result rows omit the
outcome. `blockResults` fills only ID, description, stage and state
([attempts.go:724-733](../../internal/reconciliation/lifecycle/attempts.go)),
on both the run path and the settled path; only `status` fills attempts and
unresolved; no production code writes `BlockResult.Outcome`, `status`
included; the structured writer serializes the zero values
([cli-apply-warned-json.golden](../../internal/cli/testdata/cli-apply-warned-json.golden)).
The contract is [output.md:302-303 and 842-843](../../specs/cli/output.md)
(Claude cites 843-844). Both reports read B122 as partial: its text covers
attempts only and still says X22 left the JSON results out, which the goldens
contradict. The merge adds that the spec never defines what a block outcome
is, so the fix must define it or drop the field. Exit: a JSON golden of an
apply whose block retried once shows attempts 2 and a non-empty outcome, one
of an unknown block shows `unresolved {reason, remedy}`, and B122 is
rewritten to the remaining gap.

### SCR-02

**Low, defect.** Sources: Claude [SCR-02](report-claude.md#scr-02), Codex
[C16](report-codex.md#c16-secret-version-ordinals-can-restart), which
cross-referenced no Claude entry. Unanimous.

A secret's version ordinal restarts below its highest earlier value once
deleted versions are collected, because `nextSequence` scans retained
versions only and no per-secret high-water mark is persisted
([mutations.go:755-763](../../internal/secrets/localkeyring/mutations.go),
`collectVersions` at 643-663, called from `PutBatch` and `Delete`), against
[secrets.md:517-519](../../specs/secrets.md). Codex reproduced set, set,
delete, set giving v1 through the real contextfs and keyring path and noted
the new random ID stays distinct, so bindings and identity are not
substituted; the merge reproduced Claude's second trigger as well, where the
highest version is collected while a lower one stays bound and its ordinal
is reused. Remedy: persist an authenticated high-water ordinal as a member
that raises `minReader`, or reword the clause; test both triggers.

### SCR-05

**Low, unsafe.** Sources: Claude [SCR-05](report-claude.md#scr-05), Codex
[C18](report-codex.md#c18-malformed-sudo-default-lists-lose-the-unknown-result).
Unanimous.

A `sudo -ll` Defaults list that does not parse is skipped as naming no input
logging, so a malformed listing that sets `log_input` lets a stdin secret be
read under sudo's I/O log. `inputLogging` skips an invalid list
([sudo.go:299-302](../../internal/controller/privilege/sudo.go)),
`defaultFields` returns invalid on an open quote or trailing escape
(362-363; Codex's 340-368 is 340-366), `InputLogged` fires only on a
nonempty option and `InputUnproven` only on an unreadable listing (144-151),
and the existing test expects the fail-open result. A probe confirmed that
one trailing backslash hides a `log_input` earlier in the same Matching
Defaults block. Both reports agree that no qualified sudo was shown to print
such a form and that no secret was shown logged; the merge narrows Claude's
counterevidence that sudo escapes every printed value, which is inferred. The
Claude entry carries a truncated sentence ("the settled") to complete. Remedy:
propagate validity and refuse with `InputUnproven` on a malformed list when a
stdin secret is requested.

### ADD-02

**Medium, defect.** Sources: Claude [ADD-02](report-claude.md#add-02)
(Medium); Codex, other observations
([report-codex.md](report-codex.md#other-observations-not-additional-confirmed-defect-counts)),
not promoted. Unanimous after three lenses.

The webhook-window create retry never fires, because the status reason is
parsed only from a JSON Status, which `oc` does not print on a refused
create. `create()` sets a reason only when a whole stdout or stderr stream
parses as a Status document; otherwise `retryable` is false
([cluster_objects_native.py:157-166](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_native.py)),
and the engine then raises "create unproved: absent"
([cluster_objects_engine.py:123-125](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py)).
The spec requires the retry by status reason
([add-ons.md:289-295](../../specs/add-ons.md),
[m4.md:1612-1617](../../specs/milestones/m4.md)). Codex declined to promote
the candidate because the pinned client's error form was unproved. The merge
checked the pinned client: `oc` 4.21.11 carries a single
`Error from server (%s): %s` format string, its `-o` formats cover object
output only, and a fake `oc` printing that text for `InternalError` and
`ServiceUnavailable` gives `retryable: False` while only a JSON Status gives
true. The only unit test scripts a JSON Status on stdout, a channel `oc` does
not use for errors. That `oc` prints a refused create as stderr text is
therefore inferred from the binary, not observed against a server, and
[add-on-coverage.md:405-411](../../specs/add-on-coverage.md) already lists
the form as an unverified native fact for the first run to confirm; B1125's
fact list leaves it out. Medium: the in-attempt retry is lost, repeating
`apply` creates the absent object, nothing is bypassed. Remedy: read the
reason from the `Error from server (<Reason>):` prefix, or send the create
through a channel that returns the Status body; exit: a unit test whose
scripted `oc` prints the real text for both retryable reasons and expects a
retry, plus the pinned client observed against a mock server or in B1125.

### RCP-11

**Low, defect.** Sources: Claude [RCP-11](report-claude.md#rcp-11) (Low);
Codex, other observations. Facts agreed.

Two adapters write temporary material outside a role-owned, root-only child
of the runner's private scratch: the Ceph host runtime's probe directory lands
in the remote host's default temporary directory
([ceph_host_runtime.py:686-691](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_host_runtime.py))
and the agent-install role writes the tokenized insert URL in the scratch root
([boot.yml:167-171](../../ansible/collections/ansible_collections/bootwright/core/roles/containercluster_install_agent/tasks/boot.yml)),
against [state-reconciliation.md:1130-1135](../../specs/state-reconciliation.md).
Both reports state the same impact: the clause is not met as written, and
neither exposure nor deletion across invocations is established, because
the directories are per-invocation, private and cleaned. Codex recorded it as
an observation for a separate conformance review; Claude counted a Low
defect and listed further sites of the same pattern. The merge keeps the
counted Low: a clause that is not met as written is a conformance defect
whatever its risk, and the two classifications differ in bookkeeping, not in
fact.

### SBV-15

**Medium, defect. New entry from Codex
[C03](report-codex.md#c03-kubevirt-completion-does-not-prove-readiness-and-frozen-shape).**
Verified by three lenses and three probes; no Claude counterpart (the SBV
ledger row for KubeVirt machine realization records the VM body, pin, bounds
and removal as checked and gives no verdict on the frozen-reread clause or
root readiness).

KubeVirt machine completion ignores the result of the root DataVolume wait,
and observation never re-checks the frozen VirtualMachine fields, so a
Pending root or a drifted VirtualMachine reads as completed. `Host.wait`
returns "the last reading either way" on expiry
([kubevirt_machine.py:130-137](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/kubevirt_machine.py));
apply discards that result (486-487); `settled` needs only an owned, present
root volume and a receipt (444-446); `observe` is `settled(read_machine(...))`
and never calls `frozen_difference`, which only apply runs (450-451,
482-485); the Go side maps a nil presence error to `EffectCompleted` without
reading any phase
([evidence.go:122-144](../../internal/substrate/kubevirt/evidence.go),
[capability.go:528-531](../../internal/substrate/kubevirt/capability.go)).
Probes at HEAD: apply with a permanently Pending root returns
`postcondition=True`; observe with a Pending root returns `postcondition=True`;
observe after CPU cores changed from 4 to 99 returns `postcondition=True`,
while a re-run apply refuses the drift.

The two halves rest on different bases. The frozen-shape half breaks an
explicit clause: "A reread VirtualMachine whose frozen fields ... differ from
the request refuses" ([substrates.md:1067-1069](../../specs/substrates.md)),
and the observe-time obligation follows from the general rules that a
resolution observation names the first difference from the frozen request
([state-reconciliation.md:1559-1560](../../specs/state-reconciliation.md))
and that a differing own target is converged only where declared convergible
(1474-1476). The readiness half has no clause: nothing in substrates.md
903-1142 requires the machine block to wait for the root DataVolume's
`Succeeded` phase (the `Succeeded` text at 1104-1107 concerns the upload
DataVolume), so Codex's "the spec requires waiting for root DataVolume
success" is not supported; the code contradicts its own docstring ("wait for
its root disk", line 455) and the wait it performs, which is a spec gap to
close alongside the fix. Medium: the arm is unqualified (B87 awaits operator
acceptance, the real-host run B731 is open), re-running apply refuses drift,
a Pending root surfaces later as a boot failure, and ownership and
kube-system UID checks still prevent acting on foreign objects. Remedy: one
completion proof for apply and observe that inspects the wait result, records
the root phase in the evidence, requires `Succeeded` in `settled` and in
`ValidateMachinePresence` or reports partial or unknown, and runs
`frozen_difference` in observe with drift as unknown, keeping the removal
inventory independent so drift never blocks `destroy`
(state-reconciliation.md:700-704). Exit: unit tests for a Pending or Failed
root on apply and observe, for observe after each frozen-field family
changes, for destroy proceeding on a drifted VirtualMachine, and a Go test
mapping a non-Succeeded root to partial or unknown.

### ADD-16

**Medium, defect. New entry from Codex
[C09](report-codex.md#c09-cluster-object-projection-is-not-json-safe).**
Verified by three lenses and a probe over the exact production template; no
Claude counterpart (the ADD ledger rows for the program and the adapter mark
"bodies are canonical JSON" and the create receipt as met).

The cluster-object projection template quotes strings and keys with Go
`printf "%q"`, so a declared value holding a control or non-printable
character yields invalid JSON, and a persisted create can never be read back
or receipted. The template's comment says "strings escaped by printf" and
lines 22 and 25 quote keys and strings with `%q`
([cluster_objects_native.py:17-26](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_native.py));
`decode()` uses JSON decoding, so a failure turns `get()` into "not answered"
(136-141) and `create()` into not accepted and not retryable (167-170); the
engine then repeats "read unknown" until the deadline
([cluster_objects_engine.py:46-63, 104-125](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py))
and keeps no receipt. `safeBody` refuses only template delimiters and
reserved keys ([validation.go:204-231](../../internal/addons/clusterobjects/validation.go)).
The exact template run through Go `text/template` over BEL, VT, ESC and a
non-printable supplementary rune produced `\a`, `\v`, `\x1b` and
`\U000e0001`, each of which Python JSON rejects, including inside a map key;
U+2028 and the ordinary escapes decode. `bootwright validate` accepts an
authored ClusterAddon custom resource with ESC. The spec admits canonical
JSON bodies with no limit on such characters
([add-ons.md:229-230](../../specs/add-ons.md)), requires every declared field
present and equal (296-299) and projects declared fields on reads and creates
(305-307), so the requirement is implied by those clauses rather than
spelled out, and the unknown-until-deadline behavior is what 286-288
prescribes for a failed read; the defect is that a valid body can never be
read back. Medium: the trigger is narrow (a control character or
non-printable supplementary rune in a declared string or key, in practice an
authored custom resource), the failure is closed, but the object is wedged
until removed by hand. Remedy: encode selected strings and keys
JSON-compatibly without widening the projection, or decode the Go-quoted form
explicitly in Python; alternatively or in addition, refuse control and
non-printable characters in program bodies at planning and state it in
add-ons.md. Exit: a round-trip test over `\a`, `\v`, `\x1b`, `\x7f` and
U+E0001 through the real template, or a planning refusal naming the object,
and an engine test recovering a persisted create whose response was
undecodable.

### CLJ-08

**Low, defect. New entry from Codex
[C17](report-codex.md#c17-canceled-cluster-info-leaves-borrowed-material-uncleared).**
Verified by three lenses and a cancellation probe; the CLJ ledger row for
[cli.md:1390-1401](../../specs/cli.md) recorded the clause as met on the
service-side clearing only.

A `cluster info --object-user` or `--secrets` result canceled or interrupted
after the service returns is never cleared, because `clearSensitive` omits
`clusterInfo` and only the cluster renderer clears it.
`commandResult.clearSensitive` clears `secretReveal`, `kubeconfig` and
`keyring` and not the `clusterInfo` field
([dispatch.go:70-83](../../internal/cli/dispatch.go)); the only cleanup is
the deferred `clearDashboards` inside `writeClusterResult`
([output_cluster.go:370-372](../../internal/cli/output_cluster.go)); in the
runner the cancellation check and the interrupted and canceled returns come
after a successful invoke and before `writeResult`
([runner.go:266-276, 284-286](../../internal/cli/runner.go)). The clause
requires that "all borrowed material is cleared on failure and after
rendering" ([cli.md:1398-1400](../../specs/cli.md)); service failure does
clear ([service.go:108-112](../../internal/environment/inspection/service.go)),
the cancel and interrupt window does not. The probe reproduced it: a cancel
in `FinishProgress` after a valid object-user result exits 1 with nothing
written and the credential bytes still in memory; an interrupt exits 130 the
same way. Low: a best-effort memory-hygiene omission in a narrow window, no
byte reaches a stream, log or file, and the process exits right after.
Remedy: clear `clusterInfo` in `clearSensitive` through `inspection.ClearSecrets`
so cleanup no longer depends on reaching the renderer. Exit: runner tests for
cancel, interrupt, success, invalid presentation and service failure, each
asserting cleared bytes and no output of the material.

### BLD-01

**Low, unsafe. New entry from this consolidation's review of the
container-build delta** (`0811da0e1`, `88fbf7be1`), which Codex reviewed
without recording a defect and Claude did not assess.

The build-context denylist misses Git-ignored and excluded local material,
which `COPY . .` carries into the build context and the build-stage layer.
[Dockerfile:15](../../Dockerfile) copies the whole context into the build
stage, and [.dockerignore](../../.dockerignore) is a root-anchored list of
24 paths that mirrors most of `.gitignore` but not every entry, nor a clone's
`.git/info/exclude` or a global excludes file. Any untracked material outside
those paths (worktree checkouts, agent caches, evidence directories, a
credential file an operator leaves in the checkout) reaches the engine and
stays in the build-stage layer in local engine storage or the BuildKit cache
until pruned. Inferred, since no build was run; nothing leaves the host and
no secret was shown to be copied. Codex's statement that the file "is not a
general interpreter of Git ignore rules" and that "a secret mount does not
protect arbitrary files included in a build context" describes the same
mechanism without counting it. Remedy: make the context an allowlist (`*`
first, then `!` entries for the inputs the build needs, `.git` included
while the version stamp needs it), or build from a `git archive` context,
and add a structural test that Git-ignored paths stay out of the context.

### BLD-02

**Low, doc-stale. New entry from the build-delta review.**

The documented command-line proxy form silently make-expands a `$` inside a
credential and places the credential in argv.
[docs/development.md:104-114](../../docs/development.md) offers
`make container-build HTTPS_PROXY=...` beside the environment form and says
any setting may carry a credential. Reproduced on a scratch copy of the
Makefile with a fake engine: the environment form wrote the credential
intact, the command-line form rewrote `pa$word` to `paord` with no error,
because GNU make expands a command-line variable before exporting it; the
command-line form also puts the credential in make's argument vector, which
the recipe's secret handling cannot protect. Remedy: recommend the
environment form for any credential-bearing value, or state that `$` must be
written `$$` and that argv is visible to other processes.

### BLD-03

**Low, test-gap. New entry from the build-delta review.**

The container-build tests pin only build-argument names, not the secret
file's privacy and removal, `ENV` lines, the `--build-arg=` form or the build
context. `TestContainerBuildPassesSettingsOnlyAsASecret` matches `ARG` names
in the Dockerfile and `--build-arg ` names in the recipe (a form with `=`
would pass unseen) and checks that `--secret id=build-env,src=` is present
([container_build_test.go:59-77](../../test/architecture/container_build_test.go));
it does not check that the settings file is created by `mktemp` at mode 0600
and removed by the `EXIT` trap ([Makefile:33-35](../../Makefile)), that
settings reach the file rather than the command line, that the Dockerfile
carries no `ENV` with a setting, or anything about `.dockerignore`. The
behavior at HEAD is correct (reproduced: mode 0600, removal on `EXIT`, `INT`,
`TERM` and `HUP`, values exported with `read -r` and never executed), so
there is no current consequence. Remedy: a recipe-level test with a fake
engine asserting the file's mode, contents and removal and an argv free of
setting values; widen the regexes to `--build-arg[= ]` and to `ENV` lines.

### BLD-04

**Low, improvement. New entry from the build-delta review.**

The default build image is tag-pinned, not digest-pinned, so the container
build trusts a registry tag where the host build trusts the Go checksum
database. [Dockerfile:6](../../Dockerfile) defaults `GO_IMAGE` to
`docker.io/library/golang:${GO_VERSION}-trixie`, a tag the publisher rebuilds,
while [scripts/go](../../scripts/go) pins `GOTOOLCHAIN` so a host build
fetches the toolchain through the checksum database. The pinning rule at
[security.md:722-728](../../specs/security.md) governs runtime supply-chain
artifacts; that it covers a developer build image is inferred, not stated,
and [docs/development.md:128-131](../../docs/development.md) already offers a
digest-pinned override. Remedy: default `GO_IMAGE` to a digest-pinned
reference updated with the `go` directive and extend
`TestContainerBuildTakesGoFromGoMod`, or state that the default trusts the
registry tag.

## Claude-only Medium findings after the sweep

Codex examined none of these 53 entries and declined to adopt the larger
report's count. Each received a fresh refutation pass against the source at
`85cd3ac9a` (Opus, with the entry's citations re-read and an effect-free probe
where one decided a claim); where the refuter dissented, a second agent
re-read the entry and the challenge, and a Fable arbiter would have decided
any split. Every one of the 14 dissents was confirmed by the defender, so no
arbitration was needed. Result: 39 hold exactly as written, 13 hold at Medium
with a correction, 1 is lowered to Low. None was refuted and none turned out
to be tracked by an item, decision or Not yet met line.

Hold as written (39): ADD-03, ADD-15, ARC-01, CEA-05, CEA-10, CLI-02, CLI-04,
CPD-04, CPL-06, CPN-02, CPN-08, CPR-01, CPR-02, CRA-01, CRA-02, CRB-02,
CRB-03, CRB-06, CTH-01, CTH-02, CTS-01, CTS-02, CTX-01, ENV-08, INF-01,
INF-07, MCH-01, MOI-05, OCA-05, OCI-02, OCN-01, OUT-10, OUT-11, RCD-07,
RCS-01, RCS-05, SBV-11, SCR-01, SEC-02. Two of them need an editorial repair
in the Claude report without a change of substance: the ENV-08 entry carries
a garbled line after its severity line ("Ure`, while the spec sentence says it
does not.", report-claude.md:1052), and MCH-01's rule citation starts one
line earlier than cited (machines.md:367-371). The 14 changed entries follow.

### CLI-03

**Lowered to Low, improvement** (Claude: Medium, defect). Refuter and
defender agreed. `preflight container-cluster` advertises and accepts every
`--ssh-*` flag, which its use case then refuses with `cli.usage` after
elevation. Every code citation holds, but the owning specification describes
exactly that refusal: [output.md:375-378](../../specs/cli/output.md) says the
OpenShiftCluster checks take no SSH overrides, "so SSH overrides with no
CephCluster selected, named or declared, are the `cli.usage` refusal
`preflight container-cluster` gives", and
[openshift-clusters.md:1072-1073](../../specs/openshift-clusters.md) says
"These controller checks refuse `--trust-on-first-use` and borrowed SSH
options". Those clauses agree with [commands.md:12-13](../../specs/cli/commands.md),
with the inherited-scope table and with [delivered.md:9277-9278](../../specs/milestones/delivered.md),
so the entry's "say the opposite of the list" and "the stale half" are wrong.
The flags do not cause the elevation: a probe shows `RequiresRoot` is true for
`preflight container-cluster` with and without `--ssh-user`, so the cost is a
usage refusal after a sudo prompt that happens anyway. What remains is a
usability inconsistency: help and completion offer four flags that always end
in a usage error. If the owner wants it fixed, hide or refuse the flags at
resolution and edit output.md:375-378 and openshift-clusters.md:1072-1073
together.

### ADA-09

**Medium, defect; narrowed.** The core claim holds: the MetalLB held-address
check builds its holders from endpoints, address claims, node addresses and
next hops ([pools.go:192-211](../../internal/addons/packages/pools.go)), the
claim switch has no CephCluster case
([endpoint_ownership.go:32-65](../../internal/containercluster/openshift/endpoint_ownership.go)),
and a CephCluster declares `virtual_ip` for its ingress, NFS and
management-gateway services (storage_native.go:38, 40, 54), so a pool may
include a Ceph virtual IP against [addons.md:473-477](../../specs/api/addons.md).
Narrowed: the entry's second consequence, that `endpointCollisions` should
also flag an OpenShift endpoint equal to a Ceph VIP, breaks no clause,
because [openshift-clusters.md:254-262](../../specs/api/openshift-clusters.md)
lists the endpoint holders as a closed list without Ceph; drop it or call it
an improvement. `endpointCollisions` spans lines 76-90, not 76-83. The ARP
race on a shared subnet is inferred, not reproduced. Medium stands: a
specified refusal is missing and the workaround is to exclude the address by
hand.

### CPC-01

**Medium, defect; narrowed and retitled.** The `spec.mgrModules` half holds
and was reproduced: [api/ceph.md:127-128](../../specs/api/ceph.md) admits
`cephadm` and `rbd_support`, admission accepts them, and `baseRequest`
refuses any non-empty list with `lifecycle.unsupported` and no field or
specific remedy ([requests.go:371-372](../../internal/storage/ceph/requests.go)),
a refusal no spec states. Narrowed: the catalog and entry titles also blamed
"every singleHostDefaults cluster", which the entry's own counterevidence
disproves, since that refusal is specified ("Fewer than three hosts keep
their existing refusal", [ceph.md:957](../../specs/ceph.md); requests.go:493-495).
The refusal is raised from `Capability.request` through `baseRequest`
([capability.go:29-32, 129](../../internal/storage/ceph/capability.go)),
outside `Capability.Unsupported` (296-330), so
`TestCephRefusalTableMatchesUnsupported` cannot see it by design; the entry's
sentence about that test should say so. Cite
[ceph-coverage.md:109-112](../../specs/ceph-coverage.md) for the pre-effect
refusal clause. New title: "Planning refuses every CephCluster whose
spec.mgrModules admission accepts (cephadm, rbd_support) with a
lifecycle.unsupported refusal no spec states."

### CPD-01

**Medium, defect; narrowed.** Holds: `devices()` refuses every declared
DB/WAL disk that carries any child, filesystem, mountpoint or signature,
before `owned_osd_device` and without regard to what the disk holds
([ceph_native.py:2440-2441](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py),
`auxiliary_device_paths` at 863-867), and the remedy text at
[materials.go:336-342](../../internal/storage/ceph/materials.go) tells the
operator to clear a signature "that this operation did not write". Existing
tests show a DB/WAL disk reported as `LVM2_member` refusing `device-not-empty`
even with intent recorded. Narrowed: the refusal holds only while the cluster
is not ready. A stop in a readiness wait continues once cephadm converges,
because the refusing path runs only when the before-state is not ready and
the stage is not complete ([ceph_core.py:1113-1117](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py));
it persists when the apply stopped inside the services calls after the DB/WAL
OSD was created but before every service was submitted, or when the cluster
never reaches ready. B1084 covers partial DB/WAL shapes only. Every citation
matches HEAD.

### CPD-03

**Medium, gap; narrowed.** Holds: `uniquePhysicalDevices` keys a device by
its WWN, or by its serial when there is no WWN, and the request builder gives
a WWN device no serial, so a disk selected by WWN on one host and by serial
on another never collides ([physical.go:63-70, 130-149](../../internal/storage/ceph/physical.go));
the worker keeps separate WWN and serial sets, and runtime checks are per
host. Reproduced by probe. Narrowed: the entry missed a Machine-level guard.
`validateSelectingSerials` ([serial_admission.go:59-80](../../internal/machine/serial_admission.go),
wired at admission.go:287) refuses a by-serial selection when any other
Machine declares that serial as its root serial or in any `eraseOnRemoval` or
`clearOnInstall` target, every erase target carries a serial, every by-serial
OSD selection must be such a target, and physical retirement requires the
WWN-selected disk to be among the host's erase targets. So in the shape that
can be retired, host B's by-serial selection is already refused at Machine
admission; the gap remains when host A leaves the shared disk undeclared (the
cluster then cannot be retired) or declares a wrong serial. Add
serial_admission.go to the counterevidence. [ceph.md:1225-1227](../../specs/ceph.md)
records each disk's observed serial and compares WWNs; it does not say the WWN
is recorded, although the runtime row does record it. The overlapping
selections clause is at 1270-1271.

### CRA-08

**Medium, defect; narrowed and retitled.** The RGW half holds and was
reproduced: admission accepts an `rgw` service with `count_per_host: 1`
([native_rgw.go:33-34](../../internal/storage/cephcluster/native_rgw.go),
[ceph-native-api.md:427](../../specs/ceph-native-api.md) "at most 1"), and
the plan refuses it as `lifecycle.unsupported` with a message that falsely
says the service places "more than one daemon of a type per host"
([service_unit.go:224-229](../../internal/storage/ceph/service_unit.go)).
Narrowed: the entry headline and both catalog rows still claim that the plan
refuses any MDS `count_per_host` including 1, which the entry body withdraws;
the MDS half is a spec gap, since [services.go:65-69](../../internal/storage/ceph/services.go)
refuses an `mds` value other than 1 while no clause states that refusal and
ceph-native-api.md:730 says placement preserves `count_per_host`, so the
coverage formula's multiplier is always 1. New title: "The plan refuses an
RGW count_per_host of 1 that the spec and admission accept, and the MDS
coverage term for count_per_host can only be 1."

### CRB-01

**Medium, defect; narrowed.** Holds and reproduced: CephConsumer names of 52
to 63 bytes pass Go admission and planning (the block identity is hashed) and
decode in the frozen request, and the worker refuses them with the generic
`request` reason because of its 51-byte bound
([ceph_export_native.py:55](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_export_native.py)),
a leftover of the 12-byte `ceph-export-` prefix the hashed identity removed
(commit 77033e8f5), against the one-name rule at
[api.md:249-251](../../specs/api.md) applied by
[ceph-resources.md:1390-1391](../../specs/ceph-resources.md). Narrowed: the
counterevidence that a Go planning test already plans a 63-byte CephConsumer
is wrong, since `TestSixtyThreeByteNamesPlanUnderTheIdentitiesEveryBuilderNames`
([long_name_identity_test.go:16](../../internal/storage/ceph/long_name_identity_test.go))
covers no CephConsumer, which appears only in the 64-byte decode refusal. No
test on either side plans or validates a 52 to 63 byte consumer name; the
exit evidence should add CephConsumer to that Go test as well as to the
collection unit test.

### CTX-02

**Medium, defect; narrowed.** Holds and reproduced by an overlay probe: an
`init` that fails after the context directory exists and before its identity
is recorded leaves a name that `init` refuses ("context reservation directory
is missing or unsafe") and `delete --purge` refuses ("initializing directory
identity is not attributable"), against [contexts.md:473-476](../../specs/contexts.md).
Narrowed in three ways. The trigger is broader than a kill: a cancellation
(`store.checkpoint` returns the context error,
[store.go:61-69](../../internal/workspace/contextfs/store.go)) or any error
from the `state/` mkdir or the reservation write strands the name, because
`createContextDirectory` closes the directory without removing it
([initialization_linux_amd64.go:122-145](../../internal/workspace/contextfs/initialization_linux_amd64.go)).
"Both messages have an empty Remediation" holds at the contextfs layer only:
the CLI applies the default `context.state` remedy "run bootwright context
list to see the contexts" ([remediation.go:45](../../internal/diagnostics/remediation.go)),
and the list's next step for an initializing context is `context init --name`,
which refuses again, so neither required exit is named and the pointer loops.
The manual exit removes the leftover tree, which may hold an empty `state/`
or a partial reservation file, not only an empty directory. The second-window
delete refusal is proved from source
([deletion_linux_amd64.go:401-405](../../internal/workspace/contextfs/deletion_linux_amd64.go)),
not by the probe. The `NotReady` remedy is at refusals.go:27-34.

### INF-05

**Medium, defect; narrowed.** Holds and reproduced: the TCP readiness check
sends one A query for the first record and requires only a non-empty answer
count ([infra_service.py:252, 169, 175-179, 258](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/infra_service.py)),
against [infrastructure-services.md:672-673](../../specs/infrastructure-services.md)
("every declared address of every one of its records over UDP, and over TCP
as well"); a stub resolver answering every name correctly fails the check
when an IPv6-only record comes first. Narrowed: the records are not sorted as
one list, so the first record is the alphabetically first Machine record
whenever any Machine record exists
([catalog.go:35, 48-76](../../internal/infrastructureservices/dnsserver/catalog.go));
the outcome is "the address stays unproved", which the spec classes as
unknown ([infrastructure-services.md:676-678](../../specs/infrastructure-services.md)),
not failure, after the retries in `infra_service_inspect.py:155-162`, and the
DNSServer apply still never completes; and the trigger needs an IPv6-only
Machine that Bootwright does not install with Anaconda, because an
Anaconda-installed Machine must have a static IPv4 install address
([machines.md:448-451](../../specs/api/machines.md)), for example a node of
an IPv6 single-stack cluster (B725, awaiting operator acceptance), which is
an unqualified path. B1067 covers only the UDP per-address check.

### MOI-01

**Medium, defect; narrowed.** Holds: after the install, the identity read
checks only that the guest answered with a non-empty marker
([identity_until_answered.yml:16-28](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/identity_until_answered.yml)),
network convergence follows, and the reboot step publishes phase `prepared`
with the frozen marker before any comparison
([reboot.yml:12-17](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/reboot.yml),
whose comment at line 8 still claims the proof), so the before-state that
[managed-os.md:649-655](../../specs/managed-os.md) calls "this installation's
marker is proved" is recorded unsoundly; the only equality check on the path
runs when the request registers (apply.yml:719-726). Narrowed: the path is
unreachable on KubeVirt, whose identity read publishes `answered: false`
(identity_read.yml:5-9; target.go:293 `ChannelNone`), so grouping it with
libvirt is wrong; on bare metal the SSH read pins this Machine's delivered
host key with strict checking and admission forbids sharing a `hostKeyRef`
(admission.go:570-572), so the foreign system is realistically an older
installation of the same Machine, with a different marker, on another boot
disk; and [managed-os.md:1027-1031](../../specs/managed-os.md) orders the
registration marker proof after network convergence, so the defect is the
unsound `prepared` record plus network and power effects nothing proved, on a
narrow physical path, not network convergence before a proof as such.
`ValidatePresence` still refuses at completion, so no false success is
reported. Citation fixes: identity_until_answered.yml:16-28 (not 15-21),
apply.yml:719-726 and 728-735 (not 719-723 and 726-731).

### RCC-06

**Medium, defect; narrowed.** Holds and reproduced: the production operation
area refuses any record over 1 MiB on `WriteExclusive`, `Replace`,
`publishStage` and staged-file collection
([operations_linux_amd64.go:26, 583-585, 614-616, 642, 645](../../internal/workspace/contextfs/operations_linux_amd64.go);
collection_linux_amd64.go:153), while the spec and `MaxPlanBytes` admit a
6 MiB plan ([state-reconciliation.md:539-542](../../specs/state-reconciliation.md),
[contexts.md:596](../../specs/contexts.md)), so a plan between the two
bounds is refused after the operation directory exists; `WriteExclusive`
succeeds at 1,048,576 bytes and fails at 1,048,577. Narrowed: the plain read
path is not capped at 1 MiB (reads are bounded by the 8 MiB log bound at
395-397), so drop "and the read paths" from the entry; and the counterevidence
is now measured: the B715 48-node plan and its inverse encode to about 113 KB,
roughly 11 percent of the cap, so the qualified envelope is not shown to
reach the defect. Medium stands as wrong behavior on an unqualified path with
a spec that contradicts the code.

### SBP-11

**Medium, defect; narrowed.** Holds and reproduced: when the Redfish system
read fails and `require_tpm` is false, `observe()` swallows the controller
error and exits normally with an empty identity
([redfish_system_inspect.py:126-131](../../ansible/collections/ansible_collections/bootwright/core/plugins/modules/redfish_system_inspect.py)),
the role's "msg is not defined" assertion passes and the proof refuses
`hardware-mismatch`, dropping the BMC's own message, against
[substrates.md:1512-1514](../../specs/substrates.md) (not 1513-1514): "A read
the BMC did not answer names none, because the BMC's own message in the
retained output is the reason". Narrowed: the interface read does not only
replay the cached failure, it falls back to a fresh fetch of the interface
collection ([redfish_control.py:384-393, 368-376](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/redfish_control.py)),
so with an answering collection the mismatch comes only from the empty UUID
and serial check; a test does fail the system read inside `observe()` and
pins the swallow ([test_redfish_boot.py:1468-1473](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/modules/test_redfish_boot.py)),
so what is untested is the module's main path and the role with
`require_tpm` false; and the apply step is not a false success, because its
completion phase refuses for a missing system identity
(substrate_physical_protocol.py:82-88, 147-148). The spec is in tension
(1217-1221 and 1485-1486 give the code a reading), and the explicit
unanswered-read clause wins. Medium stands: the diagnostic and remedy are
wrong, the refusal still blocks media insertion, and the path is the narrow
physical one.

### SBV-01

**Medium, defect; narrowed.** Holds and reproduced: `host_routes` skips every
route whose destination is empty or `default` and every `/0` route, in every
table ([substrate_libvirt.py:400-401, 405](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/substrate_libvirt.py);
the docstring at 384-385 states the choice), against
[substrates.md:191-193 and 205-208](../../specs/substrates.md), which refuse a
broader on-link route and "such a route in any other table". Four default
routes passed the collision check (a gateway default in table 220, `0.0.0.0/0`
in table 220, an on-link default on `wg0` in table 51820, an on-link default
on `tun0` in main) while the control route `10.0.0.0/8` in table 220 refused.
No record accepts the exclusion: D188 and X178 cover only the main-table
gateway warning and the on-link, other-table and nested refusals, and the
audit-001 follow-up's "stated owner choice (B843)" has no record behind it.
Narrowed: a main-table on-link default does not stop the controller reaching
its guests, because the connected route wins by longest prefix, so it breaks
only the spec's literal on-link rule; the reachability loss applies only to a
default in a non-main table that a policy rule consults before main, and even
there it is inferred, not observed. The test comment is at line 890. Cite the
skip as 400-401, not 399-401.

### SEC-06

**Medium, gap; narrowed and retitled.** The wheel half holds: the wheel
client's `CheckRedirect` tests only the hop count and `approvedURL`, which
checks the scheme, the absence of user information, a host and port 443 and
never compares the redirect host with the lock's host or the mirror
([fetch.go:274-277, 300-305](../../internal/controller/venvlocal/fetch.go));
no Go client installs a dial control hook or checks a resolved target for a
private address; no venvlocal test mentions redirects. Narrowed: the title's
"wheel and tool fetch redirects" is too wide, because tool fetches are
destination-checked: the Go tool-metadata client is limited by the
`toolMetadataOrigin` hostname allowlist
([tools_transport.go:25-34](../../internal/controller/bundlelocal/tools_transport.go),
the only acquisition-client caller at line 44) and the Ansible downloads
refuse any redirect host outside `REDIRECT_HOSTS`
([controller_files.py:447-455](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/controller_files.py)).
The spec side is a conflict: [controller.md:141 and 766-767](../../specs/controller.md)
allow HTTPS-only bounded redirects, which the code meets, while
[security.md:487-489 and 506-508](../../specs/security.md) require destination
and private-address revalidation, so Medium stands as a spec contradicting
intentional code plus an untested safety-relevant clause. Integrity holds
through the size and SHA-256 checks and system roots; reaching another host
needs a hostile redirect from PyPI or a declared mirror. New title: "Wheel
fetch redirects are not revalidated for destination or private-address
policy, and no Go dialer checks the resolved connection target."

## Where the reports disagree

Each disagreement is stated with both positions and the decision the
consolidation reached, with the evidence that settled it.

**Severity of `render` as root (CLI-01 High against C04 P2).** Decided High.
Both reports agree on every fact. Codex weighs the bounds (fixed file name,
atomic publication through a held handle, regular-file-only replacement, no
secret content, no code execution shown, direct root unaffected) and rates
the boundary error P2. On the Claude scale, which this report uses, High
covers "a trust, privilege or durability rule is not enforced", and two
stated rules are unenforced on the default non-root journey
([cli.md:396-398 and 502-504](../../specs/cli.md)); the bounds rule out
Blocking but Medium needs a narrow or unqualified path, and every non-root
context-backed `render` takes this one. Two of three lenses and all three
judges voted High; the dissenting spec lens voted Medium because the spec is
silent on who owns a directory `render` creates, which the entry now says.

**Severity of the controller SSH parser (SEC-07 High against C07 P2).**
Decided Medium, which lowers the Claude rating. The factual dispute went to
Claude: the FIPS back end carries the refused line structurally for this
crypto-policies release, and a RHEL-generated FIPS back end in the
repository's knowledge carries it too; Codex's "not established for every
qualified FIPS controller" is right only in that the RHEL 9.8 back end was
not read here. The severity went to Codex: the High clause needs a qualified
path, and the paths this parser blocks are not qualified yet (the Ceph FIPS
rows "stay unrun" under B484, B346's live FIPS row is open, B331 is parked
and the controller spec claims no FIPS compliance), the accepted FIPS
controller runs X294 and X302 never reached the parser, the refusal fails
closed and a non-FIPS placement host works. All three lenses and two of three
judges voted Medium; the reachability judge voted High on prevalence alone.
The finding keeps its urgency in the sequence because it blocks the FIPS
qualification rows deterministically.

**Severity of the pending upgrade plan (ADD-01 High against C08 P2).**
Decided High, on the observation half alone. Codex's two corrections are
adopted: observation does set `upgradePending` while ending unknown (Claude's
"cannot reach it" is true for apply only), and the apply refusal is literally
supported by [add-ons.md:334-336](../../specs/add-ons.md), so that clause is a
spec-versus-spec conflict the fix must settle, not a second contradiction.
What remains is enough for High: a healthy, delivered nmstate or metallb
instance reads unknown with a cluster-destroying remedy once the catalog head
moves, which [add-ons.md:341-343 and 446-449](../../specs/add-ons.md) forbid,
on the default catalog path of five packages. Two of three lenses and all
three judges voted High; the spec lens voted Medium on the apply-half
ambiguity, which the entry now records.

**`oc` error-format parsing (ADD-02 Medium against an unpromoted
observation).** Decided Medium, confirmed. Codex held that whether the
pinned `oc` prints a JSON Status for retryable refusals was unproved. The
consolidation read the pinned client: `oc` 4.21.11 carries one
`Error from server (%s): %s` format string and object-only output formats,
the parser accepts only a whole stream that decodes as a Status document, and
a fake client printing the text form gives no retry. The text form is
inferred from the binary rather than observed against a server, and
[add-on-coverage.md:405-411](../../specs/add-on-coverage.md) already lists it
as a fact for the first run to confirm; the entry carries both qualifications
and the exit evidence asks for the observation.

**The scratch-layout clause (RCP-11 Low against an observation).** Decided a
counted Low defect. The reports agree on the facts and on the absence of
exposure. A clause that is not met as written is a conformance defect at Low
whatever its risk; recording it as an observation changes the bookkeeping,
not the work.

**The Claude report's counts and completeness.** Codex writes that the Claude
report's "counts, clause-completeness claim and all unexamined findings are
not adopted". The counts are internally consistent (325 catalog rows, 313
confirmed, 9 merged, 3 tracked, the lane table summing to 354 candidates), and
the Claude report makes no clause-completeness claim: it registers 12,925
clauses, calls the registers "incomplete by construction", bounds coverage by
per-lane ledgers and says verification defaults to omission. Not adopting the
unexamined entries was a fair position for Codex, which says itself that no
conclusion about their validity follows. This consolidation adopted the
Medium tier after re-verifying it and the Low tier on Claude's verification,
and found in the three Codex-only findings that a ledger row marked met can
hide a defect the finder did not trace.

**The existing test on the physical root selector (RCS-02).** Claude wrote
that no existing test covers a physical node's root selector; a test on the
physical catalog pins `/dev/sda` as accepted and passes, so the behavior is
guarded, not untested. Claude's serial-with-size remedy does not fit the agent
path, which renders no size; a `wwn` hint or a refusal does.

**The gate records.** Both reports ran the Ansible gate with the same result
(8,725 passed, 1 skipped, the stale held-listing case). Claude's full
`make check` exited 2 in the primary checkout for an environment reason and
substituted a `check-go` run in a detached worktree at the baseline (89
packages ok); Codex ran `make check`, `make docs-check`, `make race` and a
verbose vulnerability scan to a pass on the completion baseline but reports
no Go package counts, and its 3-second lint lane suggests cache reuse despite
its "afresh" statement (inferred). This consolidation treats Codex's
completion gate as the pass of record for `88fbf7be1` and Claude's `check-go`
as the baseline evidence for `f8b15669f`; neither ran a live or e2e case.

**Deferrals.** Codex does not count deferred behavior (B491, B767, B93, the
qualification limits); neither does Claude, which counts only missing or
stale tracking markers on deferred clauses (CRA-07, CRB-08, SBP-12) as Low
doc-stale entries. Those are spec-marker defects, not deferral defects, and
the two treatments are consistent.

## What each report missed

**Codex found, Claude missed.** Three defects on paths the Claude finders did
not trace: the KubeVirt completion proof ([SBV-15](#sbv-15)), the `%q`
quoting of the cluster-object projection ([ADD-16](#add-16)) and the
uncleared `cluster info` result on cancellation ([CLJ-08](#clj-08)). In each
case the Claude lane's ledger recorded the owning clause as met on a partial
trace: the KubeVirt row checked the VM body, pin, bounds and removal and gave
no verdict on the frozen-reread or readiness clauses; the add-ons rows marked
canonical JSON bodies and the create receipt met; the CLI-journeys row marked
the clearing clause met on the service-side clearing alone. None of the three
was a refuted candidate, so these are coverage misses, not verdict errors.
Codex also corrected one detail of ADD-01 (`upgradePending` is set during the
unknown observation) and cited the stronger clause for MOI-04.

**Claude found, Codex did not examine.** 295 confirmed entries, 3 tracked and
9 merged ones. Codex's report is risk-directed and says so; it did not
adjudicate them and draws no conclusion about them. After this
consolidation's sweep, the 53 Medium entries among them stand (39 as written,
13 with corrections, 1 lowered), and the Low tier stands on Claude's
three-lens verification. Claude also matched Codex's C16 (as SCR-02), which
Codex presented without a cross-reference.

**Both reports and the build delta.** Codex reviewed the container-build
commits that followed the audited baseline and found no defect; every one of
its statements about the delta holds (the proxy and mirror settings travel as
a secret mount, the local secret file is removed on exit and signals, the
Dockerfile reads it as quoted exported values and executes nothing, only the
scratch stage with the executable is exported, the two architecture tests are
structural and pass offline). Claude did not assess the delta. This
consolidation's review adds four Low items ([BLD-01](#bld-01) to
[BLD-04](#bld-04)): the denylist build context, the make-expanded
command-line credential, the narrow structural tests and the tag-pinned base
image. No container build was run, so how an engine stores layers and secret
mounts is inferred.

**Neither report.** Live behavior: NetworkManager, RHSM and Satellite, Redfish
firmware, cephadm, OLM, the OpenShift agent installer, `oc`'s error output
against a server, the FIPS back end of a RHEL 9.8 controller, and every
operator acceptance row. Both reports and this one mark those clauses
unverifiable rather than met.

## Consolidated remediation sequence

The sequence merges Claude's twelve roadmap steps with Codex's seven work
packages and places every High and Medium entry, including the 29 Medium
entries Claude's roadmap did not name and the entries this consolidation
added. Exposure and privilege come first, then the fail-closed defects that
block accepted or soon-qualified configurations, then wrong outcomes on
qualified paths, then the spec repairs and the guards that stop the largest
classes from growing. A step marked "owner decision" changes a contract and
needs a recorded decision first. Carriers are suggestions; milestones remain
the delivery authority, and nothing here creates or activates an item.

| Order | Outcome | Findings | Owner decision | Suggested carrier |
| --- | --- | --- | --- | --- |
| 1 | `render` publishes as the invoking account; help, usage and raw-bound outcomes come before elevation | [CLI-01](#cli-01), CLI-02, CLI-04, [CLI-03](#cli-03) (Low, with the spec edit) | whether context-free `render` elevates at all | M3/M4 defect on a delivered path (Claude step 1, Codex package 3) |
| 2 | Private installer files published at their final mode, owner and label | [MOI-04](#moi-04) | none | M3 managed-OS defect (Claude step 2, Codex package 1) |
| 3 | The cluster-object engine's OLM and JSON edges: pending upgrade plans reported and never fatal; webhook retry reads the printed reason; transient placement retried; projection emits JSON; `maxOpenShiftVersion` read from the real property | [ADD-01](#add-01), [ADD-02](#add-02), [ADD-11](#add-11), [ADD-16](#add-16), ADD-03 | none beyond clarifying add-ons.md:334-336 | M4 add-ons defects (Claude step 7, Codex package 4) |
| 4 | One SSH policy reduction rule for the three parsers; the controller parser admits the fixed disabling form | [SEC-07](#sec-07) | none | M1 controller item scheduled before B484's and B346's FIPS rows (Claude step 3, Codex package 3) |
| 5 | The remaining ambient-trust paths closed: one shared executable qualifier with a fixed working directory, every Ansible plugin path pinned, adapter scratch confined, malformed sudo lists refused, wheel redirects revalidated | [SEC-01](#sec-01), [PRJ-03](#prj-03), [RCP-11](#rcp-11), [SCR-05](#scr-05), [SEC-06](#sec-06), SEC-02 | none | M1 controller and adapter items (Claude step 4, Codex packages 3 and 7 in part) |
| 6 | A durable root selector on physical agent nodes; the kernel-name refusal in Machine admission; the two spec pages reconciled | [RCS-02](#rcs-02), MCH-04, MCH-08 | yes: require a `wwn` hint or record the agent exception | gates the physical agent acceptance row (Claude step 5, Codex package 1) |
| 7 | The physical MAC set defined once | RCS-01, SBP-02, SBP-09 | none | gates the physical rows (Claude step 6) |
| 8 | KubeVirt and libvirt completion and collision proofs: one completion proof for apply and observe with the root phase recorded; default routes in the collision check; SSH-placed BMC claims | [SBV-15](#sbv-15), [SBV-01](#sbv-01), SBV-11 | a spec sentence on root readiness | M1 substrate items, SBV-15 before B87's acceptance (Codex package 2) |
| 9 | Admission, planning and the Ceph workers agree; bounds carried in the frozen request; the admitted-fixture-through-plan parity test | [CPC-01](#cpc-01), [CEA-08](#cea-08), [CRB-01](#crb-01), CRB-02, [CRB-07](#crb-07), CPN-04, [CRA-08](#cra-08), CRA-02, CPL-06, CPC-07, CPN-02, CRA-01, CRB-03, CRB-06, [CPD-03](#cpd-03), CPD-04, CPR-01, CPN-08 | yes, per input: realize it or refuse it at admission; which Ceph spec page gives way for NVMe-oF auth; the bucket bound is settled at 63 | M2 storage items (Claude step 8, Codex package 5) |
| 10 | Remote answers classified once per adapter family and mapped to the specified outcome | INF-01, [INF-05](#inf-05), INF-07, INF-08 (B412), [SBP-11](#sbp-11), CTS-01, MON-01, OCI-02, OCN-01 | none | M1 and M3 adapter items (Claude step 9) |
| 11 | Intent recorded before effect on resumable paths, and resume reconciles against it | CTH-01, CTH-02, [CPD-01](#cpd-01), CTX-01, [CTX-02](#ctx-02), OUT-10, [MOI-01](#moi-01), SCR-01 | none | M1 and M2 items (Claude step 10) |
| 12 | Admission and output consistency: qualified image references, mode-aware kind defaults, populated block results, the remaining admission and output defects | API-01, ENV-06, ENV-08, MCH-01, OCA-05, [ADA-09](#ada-09), OUT-01 (rewrite B122), OUT-11, [RCC-06](#rcc-06), RCD-07, ARC-01 | none | M1 API, compiler and CLI items; B122 where it applies (Codex package 6) |
| 13 | Custody bookkeeping and cleanup: a persisted ordinal high-water mark, unconditional sensitive-result cleanup | [SCR-02](#scr-02), [CLJ-08](#clj-08) | none | M1 secret-store and inspection items (Codex package 7) |
| 14 | The NFS security default settled and the enforced refusals that have no test pinned | CEA-10, CEA-05, RCS-05, CPR-02, ADD-15 | yes: `sys` only against no default for NFS `sectype` | M2 and M4 items (Claude step 11) |
| 15 | Spec text left behind by delivered decisions repaired, the two guards added (a delivered record cites the spec lines it changed; every refusal-table guard is bidirectional), and the build hygiene items | CTS-02, MOI-05, CPN-08 and the doc-stale and drift entries of the catalog; [BLD-01](#bld-01) to [BLD-04](#bld-04) | none | a docs and gates slice (Claude step 12) |

Steps 1 to 5 are the exposure, privilege and fail-closed items both reports
put first. Steps 6 to 8 gate acceptance rows that have not run; each should
land before its row. Steps 9 to 13 are defects on delivered paths with
workarounds. Steps 14 and 15 change no behavior except the tests they add.
Every safety or durable-format change needs the repository's independent
review, a contract or digest decision where a frozen request or stored
format moves, focused regressions that fail before the fix, and the race and
full gates at integration.

## Verification

What this consolidation ran, on `85cd3ac9a`, all effect-free. No command
with effects ran, no host, cluster, BMC or container engine was reached, and
the repository tree was read-only for every agent; probe files lived under the
session scratch directory and reached the packages through Go overlays or the
gate venv's Python.

| Check | Result |
| --- | --- |
| Reconciliation workflow: 19 reconcilers, 33 verification lenses, 9 severity judges, 3 arbiters, 2 extra lanes (66 agents) | completed; every Codex finding real at HEAD; 3 splits arbitrated |
| Medium sweep: 53 refuters, 14 defenders (67 agents) | completed; 39 hold, 13 narrowed, 1 lowered, 0 refuted, 0 arbitrations needed |
| Probes re-run or newly run by the agents | `ClassifyInvocation` for the `render` forms (RequiresRoot true for all three); the three SSH parsers over this host's DEFAULT, FUTURE, LEGACY and FIPS back ends (only FIPS refused, by the two controller parsers); the real cluster-object engine with a pinned Succeeded CSV and a pending upgrade plan (observe unknown with `upgradePending` true, apply refused, zero writes); the KubeVirt module with a Pending root on apply and observe and with CPU cores 4 to 99 on observe (`postcondition` true each time); the exact projection template over BEL, VT, ESC and U+E0001 (invalid JSON each); `bootwright validate` over a copied authored add-on with ESC (ok); `cluster info` canceled and interrupted after a valid object-user result (bytes uncleared, exit 1 and 130); the pinned `oc` 4.21.11 binary's error format string and a fake `oc` printing it (no retry); the keyring index with both ordinal triggers; the sudo listing with a trailing backslash; the `atomic_move` ordering in the gate venv's ansible-core 2.21.5; the libvirt route collision check with four default routes; the DNS TCP readiness check with an IPv6-only first record; a stranded `init` at `after-context-directory`; `WriteExclusive` at 1,048,576 and 1,048,577 bytes; the B715 48-node plan encoding; the Machine containment check in both attachment forms; the uplink checks without a default route; the Makefile recipe with a fake engine and the trap pair under signals |
| `GOPROXY=off ./scripts/go test ./test/architecture -run '^TestContainerBuild' -count=1` | passed, both tests |
| `make docs-check` in the task worktree, before and after adding this report | passed, exit 0 (the audits directory is a dated record the guidance checks skip; this report's own links, anchors and tables were checked by a separate script, 0 problems) |
| `make check` in the task worktree with this report and the README in place | passed, exit 0: both modules verified and tidy, `go vet` for linux and for darwin/arm64, completion tests, `govulncheck` (no vulnerability called by the code, one in a required module the code does not call), `go test ./...` with 89 packages ok and 11 without tests, `git diff --check`; the Ansible gate reported every input unchanged since its complete pass of 2026-11-05 19:42 and reused that pass rather than rerunning, which is a reused pass, not a fresh execution |
| `make race`, live, e2e or operator acceptance | not run; Codex's completion gate on `88fbf7be1` is the record of `make race`, and no product file changed since |

Limits. The consolidation re-verified the Codex set and the Medium tier; the
246 Low entries carry Claude's verification only. It re-read no lane ledger
and registered no clause, so coverage is bounded as the Claude report states
it, and the three Codex-only findings show a met row can hide a defect. Every
"inferred" marker in the entries above names a fact the source supports but
no probe observed: the sudo working directory, the `oc` text form against a
server, the RHEL 9.8 FIPS back end, the live disclosure window of MOI-04,
the ARP race of ADA-09, the strongSwan consequence of SBV-01, the engine
storage of a build context. Severity follows the Claude scale applied to what
is established, not to the worst hypothetical, and where a lens or judge
dissented the entry says so.

## Complete catalog

Every entry of both reports with its final severity, kind, status and
sources, ordered by severity, status and lane. Status `confirmed` is a
discrepancy that holds at `85cd3ac9a`; `merged` means the Claude report
folded the entry into the named primary; `tracked` means a record already
names it. The parenthesized disposition says what this consolidation did:
`agreed` is a Codex match re-stated with corrections, `narrowed` and
`lowered` are sweep or arbitration outcomes, `added` is an entry neither
source report carried in this form. A linked ID has an entry above; every
other entry is read in the Claude report under its heading.

| ID | Severity | Kind | Status | Lane | Sources | Title |
| --- | --- | --- | --- | --- | --- | --- |
| [ADD-01](#add-01) | High | defect | confirmed (agreed) | addons | Claude ADD-01; Codex C08 | A pending upgrade InstallPlan makes an exact-policy observation of a healthy pinned add-on read unknown and a continued apply refuse |
| [CLI-01](#cli-01) | High | unsafe | confirmed (agreed) | cli-parsing | Claude CLI-01; Codex C04 | Under sudo elevation, render publishes its output with root credentials and holds the --sensitive ownership check to root instead of the invoking account |
| [ADD-02](#add-02) | Medium | defect | confirmed (agreed) | addons | Claude ADD-02; Codex other observations | The webhook-window create retry never fires, because the status reason is parsed only from a JSON Status, which oc does not print on a refused create |
| ADD-03 | Medium | defect | confirmed | addons | Claude ADD-03 | maxOpenShiftVersion is read from a CSV annotation key that does not exist, so the evidence never records it |
| [ADD-11](#add-11) | Medium | defect | confirmed (agreed) | addons | Claude ADD-11; Codex C12 | A placed Secret whose module-side read or create fails transiently fails the attempt at once as 'differs at unproved placement' instead of being retried until the deadline |
| ADD-15 | Medium | test-gap | confirmed | addons | Claude ADD-15 | No test proves that a Secret placement of a type other than Opaque or kubernetes.io/tls is refused at planning |
| [ADD-16](#add-16) | Medium | defect | confirmed (added) | addons | Codex C09 | The cluster-object projection template quotes strings and keys with Go printf %q, so a declared value holding a control or non-printable character yields invalid JSON, and a persisted create can never be read back or receipted |
| [ADA-09](#ada-09) | Medium | defect | confirmed (narrowed) | api-addons | Claude ADA-09 | MetalLB held-address check omits CephCluster ingress, NFS and management-gateway virtual_ip addresses |
| CEA-05 | Medium | test-gap | confirmed | api-ceph | Claude CEA-05 | Cluster SSH key separation refusals in CephCluster admission have no test |
| [CEA-08](#cea-08) | Medium | defect | confirmed (agreed) | api-ceph | Claude CEA-08; Codex C11 | An admitted nvmeof service with enable_auth: true and its five required mTLS bindings refuses every plan of its cluster as an unrealized service binding |
| CEA-10 | Medium | drift | confirmed | api-ceph | Claude CEA-10 | An omitted CephNFSExport native.sectype is admitted, so NFS-Ganesha's default security flavors (including AUTH_NONE) apply instead of sys only |
| [ENV-06](#env-06) | Medium | defect | confirmed (agreed) | api-environment-secrets | Claude ENV-06; Codex C14 | An HTTP-only ArtifactServer inherits a defaults.ArtifactServer tls block and is then refused, instead of HTTP suppressing the inherited TLS |
| ENV-08 | Medium | drift | confirmed | api-environment-secrets | Claude ENV-08 | An authored empty or all-zero CephPool replicated block selects the replicated arm and blocks an inherited defaults.CephPool.erasure |
| [API-01](#api-01) | Medium | defect | confirmed (agreed) | api-grammar | Claude API-01; Codex C13 | Image grammar admits a reference with no repository component, such as a short name `squid:6.10` or `registry.example.test:5000` |
| MCH-01 | Medium | defect | confirmed | api-machines | Claude MCH-01 | Install-address containment in a managed libvirt network is skipped when the Machine attaches by substrate.libvirt.interfaceAttachments |
| OCA-05 | Medium | defect | confirmed | api-openshift | Claude OCA-05 | Secondary-network uplink checks find the install interface from the default route, so a node with no default route escapes the install-interface and br-ex VLAN refusals |
| ARC-01 | Medium | defect | confirmed | architecture | Claude ARC-01 | General-flow packages read the CephCluster's spec.management field path, and the architecture test explicitly does not look for field paths |
| [CPC-01](#cpc-01) | Medium | defect | confirmed (narrowed) | ceph-coverage | Claude CPC-01 | Planning refuses every CephCluster whose spec.mgrModules admission accepts (cephadm, rbd_support) with a lifecycle.unsupported refusal no spec states |
| [CPD-01](#cpd-01) | Medium | defect | confirmed (narrowed) | ceph-devices-resources | Claude CPD-01 | A completed DB/WAL allocation is refused device-not-empty whenever cluster preparation runs again, and the remedy tells the operator to clear that disk |
| [CPD-03](#cpd-03) | Medium | gap | confirmed (narrowed) | ceph-devices-resources | Claude CPD-03 | The same disk selected by WWN on one host and by serial on another, or seen by two hosts, is never compared across hosts |
| CPD-04 | Medium | defect | confirmed | ceph-devices-resources | Claude CPD-04 | Coordinator accepts any well-formed endpoint digest from a bound mgmt-gateway unit; the specified `evidence` refusal for another digest does not exist |
| CPR-01 | Medium | defect | confirmed | ceph-ingress-removal | Claude CPR-01 | A pre-existing NVMe-oF subsystem created by another tool refuses nvmeof-identity, not nvmeof-foreign |
| CPR-02 | Medium | test-gap | confirmed | ceph-ingress-removal | Claude CPR-02 | The tiebreaker authorized_keys rewrite's changed-since-read refusal has no test, and refusals raised after the attempt record report that nothing changed |
| CPL-06 | Medium | gap | confirmed | ceph-lifecycle | Claude CPL-06 | A cephadm.addressRef or bootstrap.addressRef that differs from the fleet SSH address is admitted by the API, then refused at planning as 'not yet modeled'. No spec or item tracks this refusal |
| CPN-02 | Medium | defect | confirmed | ceph-native-api | Claude CPN-02 | Service-unit placements silently drop the network and daemon-name parts of decorated host tokens that admission accepts |
| CPN-08 | Medium | drift | confirmed | ceph-native-api | Claude CPN-08 | Host-layout owner exception and FSID-directory rule are wider in code than ceph-native-api states: mgmt-gateway and oauth2-proxy gain the image-user owner, and the FSID directory may belong to a singleton's user |
| CRA-01 | Medium | defect | confirmed | ceph-resources-a | Claude CRA-01 | Placement-group budget counts an unauthored replicated pool at width 3 instead of the cluster default replica size |
| CRA-02 | Medium | drift | confirmed | ceph-resources-a | Claude CRA-02 | Replicated size or minSize of 1 is admitted but refused at plan for pools and by an opaque worker 'request' refusal for policies; the spec states no such floor |
| [CRA-08](#cra-08) | Medium | defect | confirmed (narrowed) | ceph-resources-a | Claude CRA-08 | The plan refuses an RGW count_per_host of 1 that the spec and admission accept, and the MDS coverage term for count_per_host can only be 1 |
| [CRB-01](#crb-01) | Medium | defect | confirmed (narrowed) | ceph-resources-b | Claude CRB-01 | A CephConsumer named with 52 to 63 bytes is admitted and planned, then refused at apply by the consumer worker's 51-byte name limit |
| CRB-02 | Medium | defect | confirmed | ceph-resources-b | Claude CRB-02 | CephNFSExport admits `protocols: []` and `transports: []`; the worker refuses them only at apply, with an opaque request refusal |
| CRB-03 | Medium | defect | confirmed | ceph-resources-b | Claude CRB-03 | Consumer prerequisite and key-type refusals do not name the missing prerequisite or the allowed key types |
| CRB-06 | Medium | defect | confirmed | ceph-resources-b | Claude CRB-06 | A filesystem-wide SMB share is admitted beside a share of another SMB cluster on the same filesystem |
| [CRB-07](#crb-07) | Medium | defect | confirmed (agreed) | ceph-resources-b | Claude CRB-07; Codex C10 | A CephObjectBucket named with 45 to 63 characters is admitted and planned, then refused at apply by the bucket worker's stale 44-character limit with a generic request reason |
| [OUT-01](#out-01) | Medium | defect | confirmed (agreed) | cli-output | Claude OUT-01; Codex C15 | apply and destroy results report every block with outcome "" and attempts 0 and never carry unresolved, and human result rows omit the outcome |
| OUT-10 | Medium | defect | confirmed | cli-output | Claude OUT-10 | A setup run that cannot be created, removed or numbered fails the whole controller install instead of discarding dnf's output |
| OUT-11 | Medium | defect | confirmed | cli-output | Claude OUT-11 | Retained adapter output is published only on the next write, so the lines a wedged adapter printed last stay in memory until the run ends |
| CLI-02 | Medium | defect | confirmed | cli-parsing | Claude CLI-02 | Bare `render`, which only prints help, is classified as requiring root and elevates through sudo |
| CLI-04 | Medium | drift | confirmed | cli-parsing | Claude CLI-04 | The sudo-entry environment guard runs before raw bounds and on help, version and completion, contradicting their no-effect contracts |
| CTX-01 | Medium | defect | confirmed | contexts | Claude CTX-01 | A prompted context update or delete refuses over an abandoned publication stage in state/ with no entry or exit named, though --yes succeeds |
| [CTX-02](#ctx-02) | Medium | defect | confirmed (narrowed) | contexts | Claude CTX-02 | An init killed between creating the context directory and recording its identity leaves the name stuck: init retry and delete --purge both refuse with no exit |
| CTH-01 | Medium | defect | confirmed | controller-host | Claude CTH-01 | A re-setup rewrites the install receipt so packages Bootwright installed become 'present before bootwright' or vanish, and uninstall then keeps them |
| CTH-02 | Medium | defect | confirmed | controller-host | Claude CTH-02 | A setup resumed after an interrupted package install records the packages that install added as 'present before bootwright' |
| CTS-01 | Medium | defect | confirmed | controller-stage | Claude CTS-01 | Wheel fetch collapses DNS, certificate, timeout and unreachable failures into one routable 'could not be fetched' diagnostic, so a certificate failure is told to set HTTPS_PROXY |
| CTS-02 | Medium | drift | confirmed | controller-stage | Claude CTS-02 | Spec still describes a Go read-only tool verification that streams sources and members and re-checks oc's release stamp, but X89 deleted it and Present is presence only |
| INF-01 | Medium | defect | confirmed | infra-services | Claude INF-01 | Readiness outcome classification is inverted: an unanswered listener fails the attempt, while a certificate mismatch is recorded unknown |
| [INF-05](#inf-05) | Medium | defect | confirmed (narrowed) | infra-services | Claude INF-05 | Managed DNS readiness proves TCP with one A query for records[0] only, so an IPv6-only first record never completes and TCP proves no declared address |
| INF-07 | Medium | defect | confirmed | infra-services | Claude INF-07 | A managed NTPServer bound to one address also opens an unreserved wildcard server socket of the other address family |
| [MOI-01](#moi-01) | Medium | defect | confirmed (narrowed) | managedos-media-install | Claude MOI-01 | A fresh apply converges the network and records the marker proof without comparing the installed marker to the frozen one |
| [MOI-04](#moi-04) | Medium | unsafe | confirmed (agreed) | managedos-media-install | Claude MOI-04; Codex C02 | Private installer files (identity, identity.pub, recovery) are published with ansible.builtin.copy straight into the served private directory, so each sits at a umask-derived mode before chmod 0640 and the label, unlike the staged image |
| MOI-05 | Medium | doc-stale | confirmed | managedos-media-install | Claude MOI-05 | Budgets section claims a physical target stays within the stated bounds with at most six interfaces, but the code bounds 64 interfaces under separate, larger physical bounds whose deadline passes the 6-hour ceiling, and the spec has no Not yet met line |
| OCI-02 | Medium | defect | confirmed | openshift-install | Claude OCI-02 | Own-image recognition also requires the port to match, so a BMC that echoes the URL without the listener's non-default port reports a foreign image |
| OCN-01 | Medium | defect | confirmed | openshift-nodes-coverage | Claude OCN-01 | diff --observe reports taint drift on a fully placed node whose authored taints are unsorted or repeated |
| [PRJ-03](#prj-03) | Medium | unsafe | confirmed (agreed) | project-glossary | Claude PRJ-03; Codex C06 | Adapter ansible-core still searches the system-wide /usr/share/ansible/plugins paths, so a vars plugin installed there by root is imported into every adapter run |
| [RCC-06](#rcc-06) | Medium | defect | confirmed (narrowed) | reconciliation-core | Claude RCC-06 | The context store refuses a plan.json over 1 MiB, so the 6 MiB stored-plan bound is never reachable |
| RCD-07 | Medium | defect | confirmed | reconciliation-diff | Claude RCD-07 | diff --observe bounds concurrent observations only by the engine bound, ignoring the per-cluster throttle and exclusive claims an operation enforces |
| RCS-01 | Medium | defect | confirmed | reconciliation-safety | Claude RCS-01 | The RAM erase worker and the Ceph host runtime prove a physical machine's MAC set without the management controller host-interface exception |
| [RCS-02](#rcs-02) | Medium | unsafe | confirmed (agreed) | reconciliation-safety | Claude RCS-02; Codex C01 | A physical OpenShift agent node may select the root disk it erases by a kernel name such as /dev/sda, which physical disk safety forbids but the OpenShift domain spec admits |
| RCS-05 | Medium | test-gap | confirmed | reconciliation-safety | Claude RCS-05 | The RAM erase worker's installer-isolation and RAM-root refusals (isolate) have no unit test; the only test replaces isolate with a mock |
| SCR-01 | Medium | defect | confirmed | secrets | Claude SCR-01 | A keyring whose store.json or metadata fails authentication refuses secret.store.corrupt without the two exits the spec requires |
| [SEC-01](#sec-01) | Medium | defect | confirmed (agreed) | security | Claude SEC-01; Codex C05 | Root-run exec sites for /usr/bin/getent, /usr/bin/firewall-cmd and the SSH client skip the root-owned executable and ancestry qualification, and the first two inherit the invoker's working directory |
| SEC-02 | Medium | gap | confirmed | security | Claude SEC-02 | No internal-error boundary: a Go panic prints a full goroutine stack, and no correlation identity exists |
| [SEC-06](#sec-06) | Medium | gap | confirmed (narrowed) | security | Claude SEC-06 | Wheel fetch redirects are not revalidated for destination or private-address policy, and no Go dialer checks the resolved connection target |
| [SEC-07](#sec-07) | Medium | defect | confirmed (lowered) | security | Claude SEC-07; Codex C07 | The controller-side SSH policy parser refuses the GSSAPIKeyExchange no line that the FIPS crypto-policy back end carries, so Ceph controller sessions and placement-host key checks refuse ssh-policy-unavailable on a FIPS host whose policy is readable, while the tunnel parser admits the same line |
| [SBP-11](#sbp-11) | Medium | defect | confirmed (narrowed) | substrates-physical | Claude SBP-11 | An unanswered system read in the bare-metal target proof is swallowed and reported as hardware-mismatch, dropping the BMC's own message |
| [SBV-01](#sbv-01) | Medium | defect | confirmed (narrowed) | substrates-virtual | Claude SBV-01 | Default routes in any table are dropped from the managed-network collision check, so a policy-routed full-tunnel default (strongSwan table 220) never refuses |
| SBV-11 | Medium | defect | confirmed | substrates-virtual | Claude SBV-11 | An emulated BMC on an SSH-placed libvirt provider never claims its shared bmc: key, so a physical Machine of another context addressing that endpoint is never refused |
| [SBV-15](#sbv-15) | Medium | defect | confirmed (added) | substrates-virtual | Codex C03 | KubeVirt machine completion ignores the result of the root DataVolume wait, and observation never re-checks the frozen VirtualMachine fields, so a Pending root or a drifted VirtualMachine reads as completed |
| CEA-03 | Medium | gap | merged | api-ceph | Claude CEA-03; merged into CPC-01 | Any nonempty spec.mgrModules or singleHostDefaults refuses at request planning, a refusal no spec states |
| ENV-03 | Medium | test-gap | merged | api-environment-secrets | Claude ENV-03; merged into CEA-05 | No test proves that a CephCluster cluster SSH key reusing the fleet key or a Machine access key is refused |
| CPN-01 | Medium | defect | merged | ceph-native-api | Claude CPN-01; merged into CEA-08 | An nvmeof service with enable_auth: true can never be planned: the cluster request refuses its five mandatory mTLS bindings |
| INF-02 | Medium | defect | tracked | infra-services | Claude INF-02; tracked as B248 | An artifact server's presence never compares its container start with the files it runs from, so an interrupted apply can resolve completed on stale configuration |
| INF-08 | Medium | defect | tracked | infra-services | Claude INF-08; tracked as B412 | A wildcard bind serves one address family while readiness probes endpoint addresses of both, and the derived wildcard is always IPv4 |
| MON-07 | Medium | defect | merged | managedos-network-fips-physical | Claude MON-07; merged into RCS-01 | The physical erase RAM worker's MAC-set proof does not except the management controller's USB CDC/RNDIS host interface that the installer's %pre excepts |
| ADD-04 | Low | doc-stale | confirmed | addons | Claude ADD-04 | The spec still names the cluster object program version cluster-objects-v1 after the program moved to v2 |
| ADD-05 | Low | gap | confirmed | addons | Claude ADD-05 | Instance completion evidence does not name the package, the cluster or the provided capabilities |
| ADD-06 | Low | gap | confirmed | addons | Claude ADD-06 | Operator-managed namespace evidence lists later plans without the catalog they resolved from |
| ADD-07 | Low | defect | confirmed | addons | Claude ADD-07 | A declared failure condition's diagnostic omits the condition type |
| ADD-08 | Low | defect | confirmed | addons | Claude ADD-08 | An observation's engine-level unknown is pinned on the first program object (the owned Namespace), so the diagnostic and additive remedy name the wrong object |
| ADD-09 | Low | drift | confirmed | addons | Claude ADD-09 | The cluster object reader treats oc's 'server doesn't have a resource type' error as absence, which the spec does not admit |
| ADD-10 | Low | defect | confirmed | addons | Claude ADD-10 | A condition is flagged stale whenever any sibling condition transitioned later, not only when it is unchanged 'long after' them |
| ADD-12 | Low | defect | confirmed | addons | Claude ADD-12 | csvSucceeded reports last observation 'Succeeded' for an installed CSV that is still Installing or Failed |
| ADD-13 | Low | defect | confirmed | addons | Claude ADD-13 | Operator-managed evidence silently keeps only the first 16 InstallPlans and CSVs of the namespace, though the spec says it lists each |
| ADD-14 | Low | test-gap | confirmed | addons | Claude ADD-14 | The kubevirt package has no fake-cluster journey, and content safety is not tested at the largest admitted configuration |
| ADA-01 | Low | drift | confirmed | api-addons | Claude ADA-01 | Executable shape admits a subscribe-only authored OLM add-on although the spec requires an owned catalog with an explicit priority |
| ADA-02 | Low | defect | confirmed | api-addons | Claude ADA-02 | One package configuration fault yields two or three diagnostics at the same field, one with an empty catalog name |
| ADA-03 | Low | defect | confirmed | api-addons | Claude ADA-03 | An empty ClusterAddonProfile is refused with the binding's message and a remedy naming spec.packages, a field profiles do not have |
| ADA-04 | Low | defect | confirmed | api-addons | Claude ADA-04 | acm single-node remedy text differs from the spec |
| ADA-05 | Low | drift | confirmed | api-addons | Claude ADA-05 | fdf subscription.source is defaulted from catalogSource.name although the spec says it is required with no default |
| ADA-06 | Low | defect | confirmed | api-addons | Claude ADA-06 | Overlapping entries inside one MetalLB pool are reported as 'pool X overlaps pool X' |
| ADA-07 | Low | defect | confirmed | api-addons | Claude ADA-07 | Nested missing-profile diagnostic is suppressed when a nested profile shares the binding's name |
| ADA-08 | Low | doc-stale | confirmed | api-addons | Claude ADA-08 | 'The four rows above' no longer points at the rows it qualifies |
| ADA-10 | Low | defect | confirmed | api-addons | Claude ADA-10 | acm hub.nodeSelector keys and values are not validated as Kubernetes labels |
| ADA-11 | Low | test-gap | confirmed | api-addons | Claude ADA-11 | No test asserts the field or remedy of add-on step-content refusals, and an escaping step path is tested only through ValidatePartial |
| ADA-12 | Low | drift | confirmed | api-addons | Claude ADA-12 | A foreign csvSucceeded readiness check is refused one level below the check's indexed field |
| CEA-01 | Low | doc-stale | confirmed | api-ceph | Claude CEA-01 | specs/api/ceph.md names request and evidence version numbers (v11, v2, v3) that no longer match the Go constants |
| CEA-02 | Low | doc-stale | confirmed | api-ceph | Claude CEA-02 | Complete-graph invariants still say a bucket name has 3 to 44 characters; the field record and code use 3 to 63 |
| CEA-04 | Low | doc-stale | confirmed | api-ceph | Claude CEA-04 | The list of service bindings planning realizes leaves out oauth2-proxy bindings that the code realizes |
| CEA-06 | Low | drift | confirmed | api-ceph | Claude CEA-06 | With no networks.publicCIDRs, the 'declared public CIDR' rules quietly use the topology Machines' address prefixes instead |
| CEA-07 | Low | doc-stale | confirmed | api-ceph | Claude CEA-07 | packageRepository refuses a query string, which its field rule does not mention |
| CEA-09 | Low | test-gap | confirmed | api-ceph | Claude CEA-09 | No test proves that the packageRepositoryKey origin check compares hosts case-insensitively |
| CEA-11 | Low | improvement | confirmed | api-ceph | Claude CEA-11 | An unknown spec.mgrModules entry refuses at unindexed $.spec.mgrModules while every other module refusal names its entry |
| ENV-01 | Low | defect | confirmed | api-environment-secrets | Claude ENV-01 | Download mirror grammar admits a percent-escaped path whose escape is canonical (for example %20 or %25) |
| ENV-02 | Low | doc-stale | confirmed | api-environment-secrets | Claude ENV-02 | Not yet met line for B760 is stale: a package configuration already retains its CephConsumer in the cluster closure |
| ENV-04 | Low | defect | confirmed | api-environment-secrets | Claude ENV-04 | An unused Secret kind default with generation parameters but no type escapes their grammar checks |
| ENV-05 | Low | test-gap | confirmed | api-environment-secrets | Claude ENV-05 | No test proves that a Machine placement.site naming an undeclared Environment site is refused |
| ENV-07 | Low | doc-stale | confirmed | api-environment-secrets | Claude ENV-07 | The kind-defaults example 'disabled authentication suppresses inherited OAuth configuration' names a schema choice that no kind declares |
| ENV-09 | Low | defect | confirmed | api-environment-secrets | Claude ENV-09 | A reference to an unaccounted excluded Machine or InfraProvider gets a remedy that names no authored selection list |
| API-02 | Low | drift | confirmed | api-grammar | Claude API-02 | api.md still lists node labels and taints and a stretch topology without a tiebreaker as api.deferred warnings, but D199 removed the first and admission now refuses the second |
| API-03 | Low | defect | confirmed | api-grammar | Claude API-03 | An unknown key that is not a schema identifier gets no list of permitted fields and no rename offer |
| API-04 | Low | defect | confirmed | api-grammar | Claude API-04 | The secrets-directory check gives each skipped secrets directory a fresh 65,536-entry budget and ignores entries of unreported type |
| API-05 | Low | defect | confirmed | api-grammar | Claude API-05 | Canonical output of a finite-number field is not the plain shortest spelling: an integral value emits `!!float 1`, a small one `1e-05` |
| API-06 | Low | defect | confirmed | api-grammar | Claude API-06 | Some compiler diagnostics repeat an object name that is not a DNS label |
| API-07 | Low | defect | confirmed | api-grammar | Claude API-07 | plan and apply never emit the api.unrealized (or api.deferred) warnings the spec and delivered record promise; only validate does |
| API-08 | Low | defect | confirmed | api-grammar | Claude API-08 | The api.unrealized message always says 'plan refuses it', but plan refuses the dependencyVersions.libvirt row only when the controller selects the libvirt client |
| API-09 | Low | defect | confirmed | api-grammar | Claude API-09 | CustomPlaybook forbids host groups that do not exist (bootwright_controller_hosts, bootwright_ocp_hosts) and accepts the real controller identities and the controller Machine as targets |
| API-10 | Low | defect | confirmed | api-grammar | Claude API-10 | CephNFSExport.spec.serviceID is not a typed native-service reference; kind admission resolves it again with api.invariant instead of the shared api.reference |
| API-11 | Low | drift | confirmed | api-grammar | Claude API-11 | api.md says native maps stay inert during compilation except NMState, but Ceph admission inspects native ServiceSpec and NFS export documents |
| API-12 | Low | drift | confirmed | api-grammar | Claude API-12 | ClusterAddon normalization sorts provides, requires, inputs and step secretRefs, but the owning page never says the effective list is sorted |
| MCH-02 | Low | doc-stale | confirmed | api-machines | Claude MCH-02 | Spec still says a libvirt provider sharing a hosted Machine's name is refused, but code admits it on purpose because the directories no longer overlap |
| MCH-03 | Low | doc-stale | confirmed | api-machines | Claude MCH-03 | Spec calls the KubeVirt network-attachment arm refused, but KubeVirt attachments are admitted, normalized and realized |
| MCH-04 | Low | drift | confirmed | api-machines | Claude MCH-04 | A kernel-name root device (/dev/sda) on a bare-metal Bootwright-installed Machine passes validate and is refused only at plan, on the Machine rather than at the field |
| MCH-05 | Low | defect | confirmed | api-machines | Claude MCH-05 | Refusal of a host key shared by two Machines names neither the Secret nor the other Machine |
| MCH-07 | Low | drift | confirmed | api-machines | Claude MCH-07 | Erase admission requires verified BMC TLS and verified virtual-media trust, which the erase section never states |
| MCH-08 | Low | doc-stale | confirmed | api-machines | Claude MCH-08 | A wwn-only root disk on a bare-metal Bootwright-installed Machine passes validate (whose remedy suggests it) and fails only at plan as 'not yet supported', with no item tracking it |
| MCH-09 | Low | doc-stale | confirmed | api-machines | Claude MCH-09 | The Machine field table omits spec.hardware.clearOnInstall, which the type admits and a later section specifies |
| OCA-01 | Low | defect | confirmed | api-openshift | Claude OCA-01 | Disk-encryption role admission requires every selected role to have a node of that exact role, contradicting the spec's 'resolves to at least one declared node' with infra/worker pool folding |
| OCA-02 | Low | drift | confirmed | api-openshift | Claude OCA-02 | Cluster MTU range and the default-route-MTU-minus-100 refusal are enforced but absent from the owning spec |
| OCA-03 | Low | test-gap | confirmed | api-openshift | Claude OCA-03 | No test proves the multi-node VIP equalities (api-int equals api, ingress differs from api) or the node-source one-node refusal |
| OCA-04 | Low | doc-stale | confirmed | api-openshift | Claude OCA-04 | B733 contract text still uses retired vocabulary: the `openshift` endpoint source and `platform.type` |
| OCA-09 | Low | test-gap | confirmed | api-openshift | Claude OCA-09 | No test covers the node endpoint source refusal for a missing or ambiguous installation address |
| OCA-10 | Low | drift | confirmed | api-openshift | Claude OCA-10 | Secondary-network bridge admission refuses an OVN-reserved name list the spec never states |
| OCA-11 | Low | test-gap | confirmed | api-openshift | Claude OCA-11 | No test proves the secondary-network MTU ceiling (at most the uplink's, or the install interface's for br-ex) |
| ARC-02 | Low | defect | confirmed | architecture | Claude ARC-02 | Clock reads sit outside the effect boundary: the CLI and two application services read the system clock and timers directly |
| ARC-03 | Low | drift | confirmed | architecture | Claude ARC-03 | Adapter-to-adapter wiring beyond the one named exception: invokerfs is injected into four other adapters through ports those adapters own |
| ARC-04 | Low | drift | confirmed | architecture | Claude ARC-04 | "No Ansible role installs a host package" contradicts substrate_libvirt_host, which dnf-installs the hypervisor closure on an SSH provider host as substrates.md specifies |
| ARC-05 | Low | drift | confirmed | architecture | Claude ARC-05 | Composition root holds preflight decision logic over the SSH client, and the sshlocal exception does not admit preflight or capacity probes |
| ARC-06 | Low | drift | confirmed | architecture | Claude ARC-06 | Composition translation also decodes KubeVirt-proved identity through kubevirt.PinnedIdentity, which the named exception does not list |
| ARC-07 | Low | defect | confirmed | architecture | Claude ARC-07 | The adapter protocol decoder admits a controller "native" phase that the phase table does not define and no runner accepts |
| ARC-08 | Low | defect | confirmed | architecture | Claude ARC-08 | Command-service file convention (requests.go holds the CLI's request and result types) is not enforced, and several services place them elsewhere |
| ARC-09 | Low | test-gap | confirmed | architecture | Claude ARC-09 | "Context root: no ports" is only checked for function types; managedos declares an exported interface port and reads through it |
| ARC-10 | Low | test-gap | confirmed | architecture | Claude ARC-10 | The ban on package-global mutable state has no fitness check; an exported mutable slice backs the sudo environment allowlist |
| ARC-11 | Low | drift | confirmed | architecture | Claude ARC-11 | KubeVirt arm identities added after the naming rule do not read `<domain>`-`<implementation>`-`<capability>`-`v<N>`, and no test holds the pattern |
| [BLD-01](#bld-01) | Low | unsafe | confirmed (added) | build | consolidation review of the build delta | The build-context denylist misses Git-ignored and excluded local material, which COPY . . carries into the build context and the build-stage layer |
| [BLD-02](#bld-02) | Low | doc-stale | confirmed (added) | build | consolidation review of the build delta | The documented command-line proxy form silently make-expands a $ inside a credential and places the credential in argv |
| [BLD-03](#bld-03) | Low | test-gap | confirmed (added) | build | consolidation review of the build delta | The container-build tests pin only build-argument names, not the secret file's privacy and removal, ENV lines, the --build-arg= form or the build context |
| [BLD-04](#bld-04) | Low | improvement | confirmed (added) | build | consolidation review of the build delta | The default build image is tag-pinned, not digest-pinned, so the container build trusts a registry tag where the host build trusts the Go checksum database |
| CPC-02 | Low | doc-stale | confirmed | ceph-coverage | Claude CPC-02 | U06's owning diagnostic names a nonexistent CephPool $.spec.type, and its fixture is a byte-for-byte copy of U05 |
| CPC-03 | Low | test-gap | confirmed | ceph-coverage | Claude CPC-03 | The combination table has no rows for the stretch exclusions the code refuses (EC pools, device-class rules, NFS/SMB, NVMe-oF), and the EC-in-stretch message is pinned by no test |
| CPC-04 | Low | doc-stale | confirmed | ceph-coverage | Claude CPC-04 | The catalog still says OD21 is unanswered and the budget test accepts either RAM option, but D160 chose OD21 (b) |
| CPC-05 | Low | doc-stale | confirmed | ceph-coverage | Claude CPC-05 | The spec says the lab-3-nodes-ceph e2e rehearses the object example, but the e2e uses a TLS ingress VIP, not daemon-terminated TLS, and has no host-address name check |
| CPC-06 | Low | doc-stale | confirmed | ceph-coverage | Claude CPC-06 | The Ceph matrix row template omits fields the acceptance-evidence section requires (effective-intent and plan digests, qualification tier, submilestone) |
| CPC-07 | Low | defect | confirmed | ceph-coverage | Claude CPC-07 | Admission exempts clusterSSH.user root from keyRef, but planning refuses every cluster without a cluster SSH key with an unrelated 'trust profile' message |
| CPC-10 | Low | drift | confirmed | ceph-coverage | Claude CPC-10 | Release qualification does not state that an authored digest must name the linux/amd64 platform manifest, though the resolver refuses an index digest and B1004 cites this section as requiring it |
| CPD-02 | Low | doc-stale | confirmed | ceph-devices-resources | Claude CPD-02 | tpm2 OSDs are admitted on physical hosts, where no TPM profile can be declared |
| CPD-07 | Low | drift | confirmed | ceph-devices-resources | Claude CPD-07 | An unowned dashboard admin user refuses dashboard-identity, not 'drift naming the custodied password' as the spec says |
| CPD-08 | Low | drift | confirmed | ceph-devices-resources | Claude CPD-08 | No-install retirement cleanup is decided per host, not only when every physical OS installation is unstarted |
| CPR-03 | Low | doc-stale | confirmed | ceph-ingress-removal | Claude CPR-03 | NVMe-oF section still names request formats ceph-core-v32 and ceph-host-v7; the code is at ceph-core-v41 and ceph-host-v13 |
| CPR-04 | Low | defect | confirmed | ceph-ingress-removal | Claude CPR-04 | Go preflight and the apply worker select virtual_interface_networks interfaces differently for non-canonical IPv6 prefixes |
| CPR-05 | Low | improvement | confirmed | ceph-ingress-removal | Claude CPR-05 | An unsettled firewalld refuses with the operator reason 'has an active firewalld that does not permit VRRP' |
| CPL-01 | Low | gap | confirmed | ceph-lifecycle | Claude CPL-01 | Ceph requires NTP-synchronized host clocks (preflight host.time and the cluster block's host-time refusal), but the spec says Storage accepts any time source that answers |
| CPL-02 | Low | doc-stale | confirmed | ceph-lifecycle | Claude CPL-02 | The spec's list of the only refusals that leave a Ceph attempt failed misses cephx-policy, nvmeof-budget and stretch-intent, plus the api.value and lifecycle.state refusals |
| CPL-03 | Low | defect | confirmed | ceph-lifecycle | Claude CPL-03 | The refusal table's row-precedence conditions ("that the previous row does not refuse") are not enforced against the replication-network row |
| CPL-04 | Low | test-gap | confirmed | ceph-lifecycle | Claude CPL-04 | TestCephRefusalTableMatchesUnsupported never checks CephPlacementPolicy, which the spec says every row also refuses |
| CPL-05 | Low | doc-stale | confirmed | ceph-lifecycle | Claude CPL-05 | The spec's .rgw.root create command passes the rule positionally and omits the confirmation, while the code (B1073) uses --rule and --yes-i-really-mean-it |
| CPL-07 | Low | doc-stale | confirmed | ceph-lifecycle | Claude CPL-07 | The under-three-hosts refusal contradicts 'acceptance-profile sizes impose no additional restriction', has no refusal-table row or owner, and has no test |
| CPL-08 | Low | defect | confirmed | ceph-lifecycle | Claude CPL-08 | The cluster block's Go remedy map codes package-license and package-platform lifecycle.unknown, against the named-outcome list that makes them lifecycle.unsupported |
| CPL-09 | Low | test-gap | confirmed | ceph-lifecycle | Claude CPL-09 | The 128 MON/MGR/OSD/MDS services bound has no boundary test: no test admits 128 and refuses 129 |
| CPN-03 | Low | defect | confirmed | ceph-native-api | Claude CPN-03 | An authored service_type: host refuses with the generic 'not implemented yet' diagnostic instead of the second-owner reason, and no test proves it |
| CPN-04 | Low | defect | confirmed | ceph-native-api | Claude CPN-04 | Planning refuses native spellings admission accepts: count_per_host 1 on unit services and an agreeing service_name on core services |
| CPN-05 | Low | drift | confirmed | ceph-native-api | Claude CPN-05 | Spec names delegated service listing only for RGW, ingress, NFS, monitoring, rbd-mirror and crash; code also lists nvmeof.`<id>` and admits nvmeof daemons |
| CPN-06 | Low | doc-stale | confirmed | ceph-native-api | Claude CPN-06 | Spec still calls B659 parked although D183 moved it to M2 |
| CPN-07 | Low | defect | confirmed | ceph-native-api | Claude CPN-07 | The NVMe-oF service-unit readiness read calls gateway info by --server_address without first proving the route is unambiguous in the dashboard gateway registry |
| CPN-09 | Low | doc-stale | confirmed | ceph-native-api | Claude CPN-09 | ceph-native-api says later HostSpec location changes are ignored, but a changed location refuses stretch-intent, as ceph.md requires |
| CRA-03 | Low | drift | confirmed | ceph-resources-a | Claude CRA-03 | Filesystem creation no longer carries allow_standby_replay in fs new and repairs it with fs set; the spec still says the creating proposal carries it and no fs set follows |
| CRA-04 | Low | defect | confirmed | ceph-resources-a | Claude CRA-04 | pgNumMin above an explicit pgNumMax of 0 refuses, though the bounds are compared only when both are positive |
| CRA-05 | Low | defect | confirmed | ceph-resources-a | Claude CRA-05 | Subvolume and subvolume-group mode outside ^0?[0-7]{3}$ refuses api.value, not the api.invariant the admission lists name |
| CRA-06 | Low | improvement | confirmed | ceph-resources-a | Claude CRA-06 | Gateway zone pools and the .mgr budget term read a second, narrower copy of the cluster default pair instead of the one default |
| CRA-07 | Low | doc-stale | confirmed | ceph-resources-a | Claude CRA-07 | CephClient removal clause (revoke principal, withdraw material) states current behavior but carries no Not yet met line; online client revocation is B491's and only host retirement is implemented |
| CRA-10 | Low | doc-stale | confirmed | ceph-resources-a | Claude CRA-10 | Placement-group budget clause says the .mgr term uses 2 or 3, but on a stretch cluster the code counts 4 |
| CRB-04 | Low | test-gap | confirmed | ceph-resources-b | Claude CRB-04 | No test proves the consumer's ambiguous public address refusal on a selected MON or MGR host |
| CRB-05 | Low | defect | confirmed | ceph-resources-b | Claude CRB-05 | CephConsumer admission diagnostics still call the object a "managed export" |
| CRB-08 | Low | doc-stale | confirmed | ceph-resources-b | Claude CRB-08 | Lifecycle destroy clauses claim per-resource absence evidence and export-before-backing removal with no Not yet met line, while every Ceph resource retires with its hosts |
| CLJ-01 | Low | defect | confirmed | cli-journeys | Claude CLJ-01 | Machine and cluster session refusals at host-key proof and crypto-policy admission run after the session's private key has been decrypted |
| CLJ-02 | Low | doc-stale | confirmed | cli-journeys | Claude CLJ-02 | Keyring export spec says an empty --name or --client fails access.target with exit 1, but the CLI refuses it as cli.usage with exit 2 |
| CLJ-03 | Low | defect | confirmed | cli-journeys | Claude CLJ-03 | A settled destroy, and a finalization-only settle, do not name bootwright diff --observe |
| CLJ-04 | Low | defect | confirmed | cli-journeys | Claude CLJ-04 | cluster kubeconfig refusal for an external CephCluster reads "a external CephCluster" |
| CLJ-05 | Low | test-gap | confirmed | cli-journeys | Claude CLJ-05 | Status parity sweep never derives a destroy over a completed apply and has no lost-binding or no-execution-definition row, though the spec cites it as proving every case |
| CLJ-07 | Low | defect | confirmed | cli-journeys | Claude CLJ-07 | diff --observe over an incomplete destroy labels the destroy's state as the apply's in the Against header |
| [CLJ-08](#clj-08) | Low | defect | confirmed (added) | cli-journeys | Codex C17 | A cluster info --object-user or --secrets result canceled or interrupted after the service returns is never cleared, because clearSensitive omits clusterInfo and only the cluster renderer clears it |
| OUT-02 | Low | defect | confirmed | cli-output | Claude OUT-02 | JSON logs list a resolution attempt's log before the effect attempt it resolves |
| OUT-03 | Low | doc-stale | confirmed | cli-output | Claude OUT-03 | Stream table and exitCode contract omit unmanaged from the differences that make diff --exit-code return 3 |
| OUT-04 | Low | doc-stale | confirmed | cli-output | Claude OUT-04 | Result-fields table is incomplete and the plan DTO orders checks first |
| OUT-05 | Low | defect | confirmed | cli-output | Claude OUT-05 | Human preflight reports bypass the shared renderer and name the context |
| OUT-06 | Low | doc-stale | confirmed | cli-output | Claude OUT-06 | Stream table lists no stdout exception for a failed preflight report or a multi-Machine power table |
| OUT-07 | Low | drift | confirmed | cli-output | Claude OUT-07 | Cluster discovery says passwordValue is the only secret field, but objectUsers.secretKey is also revealed |
| OUT-08 | Low | test-gap | confirmed | cli-output | Claude OUT-08 | Operator-string guard does not check question IDs |
| OUT-09 | Low | defect | confirmed | cli-output | Claude OUT-09 | Media list and several-Machine power tables give the renderer no identity or state metadata |
| OUT-12 | Low | drift | confirmed | cli-output | Claude OUT-12 | CephClient resource scope has CephFS filesystem and subvolume forms the spec's scope table does not list |
| OUT-13 | Low | defect | confirmed | cli-output | Claude OUT-13 | add-ons list and add-ons info write hand-formatted text that skips the shared renderer: columns are not aligned, they start at column 0 and use two spaces |
| OUT-15 | Low | defect | confirmed | cli-output | Claude OUT-15 | The private operation log sanitizer strips C0 controls and DEL but keeps C1 controls such as U+009B CSI |
| [CLI-03](#cli-03) | Low | improvement | confirmed (lowered) | cli-parsing | Claude CLI-03 | preflight container-cluster advertises and accepts every --ssh-* flag, which its use case then refuses with cli.usage after elevation; the spec describes that refusal, so the cost is a usage error after a sudo prompt that happens anyway |
| CLI-05 | Low | defect | confirmed | cli-parsing | Claude CLI-05 | Root human help omits the public `schema` command |
| CLI-06 | Low | defect | confirmed | cli-parsing | Claude CLI-06 | version accepts any letter case for the source state, where the spec requires exactly `clean` or `modified` |
| CLI-07 | Low | doc-stale | confirmed | cli-parsing | Claude CLI-07 | The ordinary-confirmation code list omits controller rebind's controller.identity |
| CLI-08 | Low | doc-stale | confirmed | cli-parsing | Claude CLI-08 | The closed list of standard-input readers omits media add --from-url-file - and the cluster sessions |
| CLI-09 | Low | drift | confirmed | cli-parsing | Claude CLI-09 | cli.md says the trust command refuses the local-access controller; machine trust skips it, and --replace refuses with no remedy |
| CLI-11 | Low | defect | confirmed | cli-parsing | Claude CLI-11 | The refusal for an executable path the invoking account cannot search names the path but not the account |
| CLI-12 | Low | drift | confirmed | cli-parsing | Claude CLI-12 | A positive sudo timestamp_timeout that halves to less than 1 ms gets no refresh, but the spec says every unambiguous positive timeout refreshes at half its duration |
| CLI-13 | Low | doc-stale | confirmed | cli-parsing | Claude CLI-13 | Structured machine trust without --yes or --dry-run is refused even when it has no pending trust-store write |
| CLI-14 | Low | defect | confirmed | cli-parsing | Claude CLI-14 | render effective --origins names leaves by effective list positions, not the authored positions the spec requires |
| CTX-03 | Low | defect | confirmed | contexts | Claude CTX-03 | A deletion whose selection clear fails drops the selection store's diagnosis and repair |
| CTX-04 | Low | defect | confirmed | contexts | Claude CTX-04 | media delete --yes on a store without media/ creates the media container, then refuses, leaving it behind |
| CTX-05 | Low | defect | confirmed | contexts | Claude CTX-05 | Media reads and media add/delete holds skip the ready-context mapping verification every read and transaction must perform |
| CTH-04 | Low | gap | confirmed | controller-host | Claude CTH-04 | Glibc-floor admission is checked only through an installed interpreter, so a host without one installs python3.X before the floor is checked |
| CTH-05 | Low | defect | confirmed | controller-host | Claude CTH-05 | A runtime whose .lock exists but is not a private regular file makes setup refuse instead of retiring and rebuilding it |
| CTH-06 | Low | defect | confirmed | controller-host | Claude CTH-06 | A controller record of another version whose schema differs is refused as malformed, not with the version diagnostic and fresh-install remedy |
| CTH-07 | Low | defect | confirmed | controller-host | Claude CTH-07 | Setup makes a dnf repoquery call on a host that already holds every package |
| CTH-08 | Low | doc-stale | confirmed | controller-host | Claude CTH-08 | The spec says setup accepts --output json only with --dry-run and that --download-only takes none, but the code and the output contract accept both |
| CTH-09 | Low | doc-stale | confirmed | controller-host | Claude CTH-09 | The preflight controller help omits the nmstate and iputils context checks |
| CTH-10 | Low | defect | confirmed | controller-host | Claude CTH-10 | The install's post-dnf presence refusal uses different words and remedy from the spec |
| CTH-11 | Low | drift | confirmed | controller-host | Claude CTH-11 | The Ansible launch environment carries seven ANSIBLE_* variables and a second PATH, against the spec's 'only LC_ALL, LANG, PATH, HOME and TMPDIR' |
| CTH-12 | Low | drift | confirmed | controller-host | Claude CTH-12 | A context-selected FIPS cluster turns the informational fips-mode check into a host-scoped controller.unsupported refusal that controller.md does not admit |
| CTH-13 | Low | defect | confirmed | controller-host | Claude CTH-13 | The install preview's standard output is scanned for 'No match' and 'Problem:' lines that decide a refusal |
| CTS-03 | Low | defect | confirmed | controller-stage | Claude CTS-03 | A dnf install failure's remedy names neither run.output nor that reading it needs root, and its message carries dnf's raw last error line |
| CTS-04 | Low | defect | confirmed | controller-stage | Claude CTS-04 | The controller stage runs `dnf repoquery --installed` after a publication that installed no host package |
| CTS-06 | Low | doc-stale | confirmed | controller-stage | Claude CTS-06 | controller.md states the libvirt pin refusal unconditionally, while the refusal table and code refuse it only when the libvirt closure is selected |
| CTS-07 | Low | defect | confirmed | controller-stage | Claude CTS-07 | The controller.runtime-retention fault line embeds err.Error(), which can carry a raw errno string from the setup store |
| CTS-08 | Low | defect | confirmed | controller-stage | Claude CTS-08 | The controller adapter never emits the `internal` refused class the spec names; Go's internal-record branch is unreachable |
| CTS-09 | Low | doc-stale | confirmed | controller-stage | Claude CTS-09 | UnreleasedClient's doc comment still names Go's read-only projection as a caller, though only the adapter's refused record produces it |
| CTS-10 | Low | drift | confirmed | controller-stage | Claude CTS-10 | Non-dry-run preflight reports an unknown listener firewall and an unreadable closure as `unverified`, which the spec reserves for a dry run |
| INF-04 | Low | doc-stale | confirmed | infra-services | Claude INF-04 | "Two consumers use it" introduces three consumers of private publication |
| INF-06 | Low | defect | confirmed | infra-services | Claude INF-06 | Service content-root asserts never bind the `<kind>` and `<name>` components to the request, so the 'exactly `<context>`/`<kind>`/`<name>`' rule is only partly enforced |
| INF-09 | Low | drift | confirmed | infra-services | Claude INF-09 | The controller-arm clause says every missing, contradictory or stale binding names controller install, but the owning contexts spec and the code give other remedies |
| MOD-01 | Low | test-gap | confirmed | managedos-day2 | Claude MOD-01 | Package-tree 'conflicting source pins for the same publication path' refusal can never fire and no test covers it |
| MOD-02 | Low | test-gap | confirmed | managedos-day2 | Claude MOD-02 | Refusal-table row predicates have no tests at their accepting boundaries: 32-hex wwn path, 256-character serial, VLAN ids 1 and 4094, and the backslash and single-quote bond option values |
| MOD-05 | Low | doc-stale | confirmed | managedos-day2 | Claude MOD-05 | The host-firewall clause that a changed requirement rewrites only its service cannot happen, because a changed input over a completed apply refuses and the per-apply mark ends ownership at destroy |
| MOI-02 | Low | defect | confirmed | managedos-media-install | Claude MOI-02 | The Kickstart's guest-agent block-list edit proves only that guest-file-open was removed |
| MON-01 | Low | defect | confirmed | managedos-network-fips-physical | Claude MON-01 | An unreadable NetworkManager Checkpoints listing after the apply is reported as 'the declared network cut the controller off', with a destroy recovery, instead of as an unreadable state |
| MON-02 | Low | defect | confirmed | managedos-network-fips-physical | Claude MON-02 | The TPM2-encrypted root's %post re-resolves a serial-declared root without the removable, read-only and exact-size checks of 'the same rule' |
| MON-03 | Low | doc-stale | confirmed | managedos-network-fips-physical | Claude MON-03 | The Not yet met line for the serial-declared root WWN is tracked under B1104, which is delivered in-tree and awaits acceptance and whose exit evidence does not cover the gap |
| MON-04 | Low | doc-stale | confirmed | managedos-network-fips-physical | Claude MON-04 | api/secrets.md says managed RHSM registers only virtual Machines, while the Registration section and the code admit physical ones |
| MON-05 | Low | drift | confirmed | managedos-network-fips-physical | Claude MON-05 | The unregistration session is bounded at 330 seconds; the spec says 300 |
| MON-09 | Low | test-gap | confirmed | managedos-network-fips-physical | Claude MON-09 | No test proves the clear %pre refuses a thin pool, an LVM RAID or a mapping over two sibling LVs of the declared targets |
| MON-11 | Low | defect | confirmed | managedos-network-fips-physical | Claude MON-11 | The delivered host key is fetched in %post, after clearpart and the data-disk clear, so a key the installer cannot fetch fails after the disks are cleared, not before |
| MON-12 | Low | defect | confirmed | managedos-network-fips-physical | Claude MON-12 | The frozen-network number check never sees authored precision, so an integer beyond int64 freezes as text and an over-precise decimal freezes rounded, without the refusal |
| MON-13 | Low | doc-stale | confirmed | managedos-network-fips-physical | Claude MON-13 | The physical-installation status line still lists 'the emulated rehearsal' as an unrun gate, though D224 removed that rehearsal |
| OCI-01 | Low | doc-stale | confirmed | openshift-install | Claude OCI-01 | Spec still says physical nodes refuse and only direct-access virtual clusters are supported, while the code admits physical nodes (B67/X124) and installation proxies |
| OCI-03 | Low | test-gap | confirmed | openshift-install | Claude OCI-03 | Unsupported refuses feature selections that have no refusal-table row, and the table test cannot detect a code-side refusal missing from the table |
| OCI-04 | Low | doc-stale | confirmed | openshift-install | Claude OCI-04 | The reserved-interface row names only br-int, while the code refuses about twenty OVN-reserved names and prefixes |
| OCI-05 | Low | defect | confirmed | openshift-install | Claude OCI-05 | A sweep refusal names the removed and refused counts but not the kept count |
| OCI-06 | Low | doc-stale | confirmed | openshift-install | Claude OCI-06 | The Selection text still lists a 'placement' refusal naming the bound Machine, but D227 lifted the cluster placement row |
| OCN-02 | Low | doc-stale | confirmed | openshift-nodes-coverage | Claude OCN-02 | Coverage Not yet met line still says clusters over 9 nodes and physical nodes refuse, though both are admitted |
| OCN-03 | Low | doc-stale | confirmed | openshift-nodes-coverage | Claude OCN-03 | Trust bundle policy matrix row says API 'none yet' and 'Needs definition (Q13)' though the field ships and Q13 is answered |
| OCN-04 | Low | defect | confirmed | openshift-nodes-coverage | Claude OCN-04 | Placement JSON patch pins only what it writes, not the uid, label map and spec together, and an absent label map is added unpinned |
| OCN-05 | Low | doc-stale | confirmed | openshift-nodes-coverage | Claude OCN-05 | Feature matrix rows cite the lab-sno-fips-controller acceptance and delivery state, but the profiles table has no such profile |
| OCN-07 | Low | test-gap | confirmed | openshift-nodes-coverage | Claude OCN-07 | No test proves that an NNCP matching no node, or a CUDN blocked by a foreign NetworkAttachmentDefinition, fails the network block instead of waiting |
| OCN-08 | Low | test-gap | confirmed | openshift-nodes-coverage | Claude OCN-08 | Placement's refusal for a declared node absent from the cluster has no test |
| OCN-09 | Low | drift | confirmed | openshift-nodes-coverage | Claude OCN-09 | Secondary-network NNCP sets OVS bridge options stp:false and mcast-snooping-enable:true that the spec never admits |
| OCN-10 | Low | doc-stale | confirmed | openshift-nodes-coverage | Claude OCN-10 | Coverage says every undelivered row refuses through the refusal table, but the arbiter, fencing and dual-stack rows refuse through schema or admission invariants that have no table row |
| PRJ-01 | Low | drift | confirmed | project-glossary | Claude PRJ-01 | project.md still says authored add-ons are refused until B90, but X235 made authored OLM add-ons executable |
| PRJ-02 | Low | drift | confirmed | project-glossary | Claude PRJ-02 | project.md says Bootwright never changes host firewall configuration, but D220's managed-OS firewalld services do |
| PRJ-04 | Low | defect | confirmed | project-glossary | Claude PRJ-04 | Local keyring still accepts identity reservations at format version 1, an earlier-format reader left behind when the upgrade path was removed |
| PRJ-05 | Low | defect | confirmed | project-glossary | Claude PRJ-05 | The SSH trust store's format-version refusal names no remedy |
| RCC-01 | Low | drift | confirmed | reconciliation-core | Claude RCC-01 | The additive-apply admission spec does not mention two rules the code enforces: added Secrets are admitted, and host or SSH reservations are refused |
| RCC-02 | Low | test-gap | confirmed | reconciliation-core | Claude RCC-02 | TestTransitionTablesMatchSpec drives only four of the five tables below the state machine, so the removal's-check table can drift from removalEffect |
| RCC-04 | Low | defect | confirmed | reconciliation-core | Claude RCC-04 | An index of any version other than 1 or 2 refuses without the build and purge remedy the spec requires |
| RCC-05 | Low | defect | confirmed | reconciliation-core | Claude RCC-05 | Additive admission never checks that each predecessor's operation record reads done; it checks only their block records |
| RCC-07 | Low | drift | confirmed | reconciliation-core | Claude RCC-07 | An additive refusal over a non-binding or changed-member input names objects and fields, not 'the first block that differs' |
| RCC-08 | Low | test-gap | confirmed | reconciliation-core | Claude RCC-08 | No store or lifecycle test proves attempt or resolution exhaustion refuses before 'running' is recorded or any effect runs |
| RCC-09 | Low | defect | confirmed | reconciliation-core | Claude RCC-09 | sameProducer refuses a recapture that presents no identity when an identity is recorded, instead of keeping the recorded one |
| RCC-10 | Low | defect | confirmed | reconciliation-core | Claude RCC-10 | An additive apply that adds exactly 32 package members to a binding refuses as a cut change list |
| RCC-12 | Low | doc-stale | confirmed | reconciliation-core | Claude RCC-12 | State-reconciliation says a bounded run creates one thing, but the run also creates its run directory and retires older runs |
| RCD-01 | Low | unsafe | confirmed | reconciliation-diff | Claude RCD-01 | diff --observe checks warnings and projections only against the bound Secret material, not the produced material it lends to the observation |
| RCD-02 | Low | defect | confirmed | reconciliation-diff | Claude RCD-02 | An observed set made only of NothingLive or rebuilt blocks still reopens the binding, proves the host and locates or rebuilds the runtime |
| RCD-03 | Low | defect | confirmed | reconciliation-diff | Claude RCD-03 | status handoff under a destroy lists the destroy's own blocks as not-done handoff reasons |
| RCD-04 | Low | doc-stale | confirmed | reconciliation-diff | Claude RCD-04 | Discovered Ceph pools get an undocumented hash-suffixed name when the plain or cluster-prefixed name is invalid or taken |
| RCD-05 | Low | defect | confirmed | reconciliation-diff | Claude RCD-05 | The lent-material refusal does not scan a projection's holder name or field |
| RCD-06 | Low | test-gap | confirmed | reconciliation-diff | Claude RCD-06 | No test covers the handoff reason for a selected input whose bytes moved under the frozen revision |
| RCD-08 | Low | defect | confirmed | reconciliation-diff | Claude RCD-08 | diff plan reads 'unplanned' instead of 'none' when there is no owning apply, and no test pins the unplanned plan state |
| RCD-09 | Low | drift | confirmed | reconciliation-diff | Claude RCD-09 | An observation's execution also carries PreviousEvidence and lent produced material, which the Boundary and Material paragraphs do not admit |
| RCD-10 | Low | drift | confirmed | reconciliation-diff | Claude RCD-10 | Conflict and split notes truncate observed values at 40 bytes mid-rune, a format the spec never states |
| RCD-11 | Low | defect | confirmed | reconciliation-diff | Claude RCD-11 | Ceph cluster projection silently drops the unplaceable-OSD count note and the omitted-pools note once 8 hosts carry stray OSDs |
| RCD-12 | Low | defect | confirmed | reconciliation-diff | Claude RCD-12 | withWarnings attaches observation warnings to absent (no-effect) readings, not only to present or partial ones |
| RCD-13 | Low | improvement | confirmed | reconciliation-diff | Claude RCD-13 | A Project error is replaced by an unspecified generic reason that drops the capability's own explanation, and no test pins it |
| RCP-01 | Low | defect | confirmed | reconciliation-plan | Claude RCP-01 | Pre-stop removal admits further first attempts after a pre-stop inverse it worked fails |
| RCP-03 | Low | doc-stale | confirmed | reconciliation-plan | Claude RCP-03 | Spec contradicts itself on whether adopting an interrupted start rewrites the attempt record |
| RCP-04 | Low | doc-stale | confirmed | reconciliation-plan | Claude RCP-04 | Effect-state enumeration omits `inconclusive`, which resolutions record |
| RCP-05 | Low | defect | confirmed | reconciliation-plan | Claude RCP-05 | An adapter-invented group ID is presented and logged instead of refused |
| RCP-06 | Low | drift | confirmed | reconciliation-plan | Claude RCP-06 | Removal replaces the frozen block's presentation groups, IDs and Machines included, though the spec says only words, impacts and authorizations change |
| RCP-07 | Low | doc-stale | confirmed | reconciliation-plan | Claude RCP-07 | MaxDeadline doc comment still says an operation runs one block at a time |
| RCP-09 | Low | defect | confirmed | reconciliation-plan | Claude RCP-09 | Removal refusal for an implementation this executable no longer provides names no request version |
| RCP-10 | Low | drift | confirmed | reconciliation-plan | Claude RCP-10 | state-reconciliation says a destroy proves the controller block's shared area; the owning controller spec and the code say the stage's removal proves nothing and runs nothing |
| [RCP-11](#rcp-11) | Low | defect | confirmed (agreed) | reconciliation-plan | Claude RCP-11; Codex other observations | Two adapters write temporary material outside a role-owned, root-only child of the runner's private scratch |
| RCS-04 | Low | defect | confirmed | reconciliation-safety | Claude RCS-04 | The rebind refusal's remedy drops the invocation's other flags, so the command it suggests can refuse again |
| RCS-06 | Low | improvement | confirmed | reconciliation-safety | Claude RCS-06 | The lifecycle.live remedy names a destroy that drops the invocation's --authorize rebind=`<secret>` and --unregistered flags |
| [SCR-02](#scr-02) | Low | defect | confirmed (agreed) | secrets | Claude SCR-02; Codex C16 | A secret's version ordinal restarts below its highest earlier value once deleted versions are collected, because nextSequence scans retained versions only and no per-secret high-water mark is persisted |
| SCR-03 | Low | defect | confirmed | secrets | Claude SCR-03 | A subtree holding only initialization evidence is refused as corrupt with purge exits, although secret encryption init would resume it |
| SCR-04 | Low | defect | confirmed | secrets | Claude SCR-04 | The unknown-backend refusal's remedy carries a literal `<context>` placeholder instead of the context name |
| [SCR-05](#scr-05) | Low | unsafe | confirmed (agreed) | secrets | Claude SCR-05; Codex C18 | A sudo -ll Defaults list that does not parse is skipped as naming no input logging, so a malformed listing that sets log_input lets a stdin secret be read under sudo's I/O log |
| SEC-03 | Low | doc-stale | confirmed | security | Claude SEC-03 | The B721 Not yet met line is stale: the image build already receives the controller Machine's proxy route |
| SEC-04 | Low | defect | confirmed | security | Claude SEC-04 | The image-build route sets only upper-case proxy variables, against the rule that both spellings carry the same value |
| SEC-05 | Low | doc-stale | confirmed | security | Claude SEC-05 | Required-security-proof Gap cells name delivered items, and the Ceph consumer row says 'none yet' although B86 and B760 are in-tree with tests |
| SBP-01 | Low | doc-stale | confirmed | substrates-physical | Claude SBP-01 | Spec states the insert bound as 39 minutes 20 seconds; code, test and the B449 record give 46m20s |
| SBP-02 | Low | unsafe | confirmed | substrates-physical | Claude SBP-02 | The controller-host guard compares a BMC's MACs with live sysfs addresses, so bonded or cloned NICs drop the host's own hardware MACs |
| SBP-03 | Low | drift | confirmed | substrates-physical | Claude SBP-03 | Restoring media verification silently accepts a device that answers 400/405/501 and leaves verification off |
| SBP-04 | Low | drift | confirmed | substrates-physical | Claude SBP-04 | A boot-mode-only firmware difference is written with no setup override and no power cycle, which the spec's sequence does not allow |
| SBP-05 | Low | defect | confirmed | substrates-physical | Claude SBP-05 | The Redfish client falls back to conventional paths a BMC never advertised (EthernetInterfaces, ComputerSystem.Reset, Insert/EjectMedia) |
| SBP-06 | Low | drift | confirmed | substrates-physical | Claude SBP-06 | The vendor VmmControl attach sends {Image, VmmControlType}, which the spec's 'exactly Image, Inserted and TransferProtocolType' does not allow |
| SBP-07 | Low | defect | confirmed | substrates-physical | Claude SBP-07 | A Manufacturer or Model over 128 characters fails the proof with a generic message rather than being bounded |
| SBP-08 | Low | gap | confirmed | substrates-physical | Claude SBP-08 | The bmc-system claim refuses a printable UUID outside [0-9a-z._-], and the recorded-proof refusal on that path names no Machine |
| SBP-09 | Low | defect | confirmed | substrates-physical | Claude SBP-09 | The target proof adds both MACAddress and PermanentMACAddress to the observed set, so a NIC whose two addresses differ fails the equality predicate |
| SBP-10 | Low | doc-stale | confirmed | substrates-physical | Claude SBP-10 | The firmware section calls itself 'the only bare-metal step that powers an operator-owned machine on', but boot_media and boot_disk also power it on |
| SBP-12 | Low | doc-stale | confirmed | substrates-physical | Claude SBP-12 | Identity and power operations states vSphere machine operations and per-arm bounds as available, with no Not yet met line, while no vSphere machine port exists |
| SBP-13 | Low | drift | confirmed | substrates-physical | Claude SBP-13 | The pre-boot refusal table omits tpm-absent's remedy and the KubeVirt identity-mismatch remedy that PreBootRefusals emits |
| SBV-02 | Low | doc-stale | confirmed | substrates-virtual | Claude SBV-02 | The hosted-provider attachment refusal is a capability refusal with no refusal-table row and no test |
| SBV-03 | Low | doc-stale | confirmed | substrates-virtual | Claude SBV-03 | The Not yet met line at substrates.md:65 claims the vSphere refusal is unmet, but the code refuses before registration |
| SBV-04 | Low | gap | confirmed | substrates-virtual | Claude SBV-04 | KubeVirt attachments are never proved to be a Localnet CUDN or a bridge-type NAD, only to be one of the two kinds |
| SBV-05 | Low | defect | confirmed | substrates-virtual | Claude SBV-05 | The hosted-namespace label-conflict admission names only one of the two conflicting secondary networks |
| SBV-06 | Low | defect | confirmed | substrates-virtual | Claude SBV-06 | The managed-attachment admission message omits the readiness-unit key it budgets for |
| SBV-08 | Low | defect | confirmed | substrates-virtual | Claude SBV-08 | libvirt machine apply proceeds without proving the hypervisor answered, so a silent lookup bypasses the foreign-domain refusal before disks are created and the domain is defined |
| SBV-09 | Low | doc-stale | confirmed | substrates-virtual | Claude SBV-09 | KubeVirt media_eject and the final attach never re-prove the VirtualMachineInstance absent, and the spec contradicts itself about eject on a running machine |
| SBV-10 | Low | defect | confirmed | substrates-virtual | Claude SBV-10 | KubeVirt media_insert verifies against the upload proxy URL read at insert time, not the one the provider proof recorded |
| SBV-12 | Low | drift | confirmed | substrates-virtual | Claude SBV-12 | Substrates spec says the SSH placement arm is coordinated by the context lease alone, but SSH claims are published and conflict across contexts (Q96, D188) |
| SBV-13 | Low | drift | confirmed | substrates-virtual | Claude SBV-13 | The capacity spec gives the remote domain listing the 64 KiB single-fact bound, but the code bounds it at about 8 MiB, and no test proves output past a bound makes the fact unknown |
| SBV-14 | Low | test-gap | confirmed | substrates-virtual | Claude SBV-14 | TestSubstrateRefusalTableMatchesUnsupported checks only table rows against its own case map, so a refusal Unsupported emits with no table row passes |
| ENV-10 | Low | doc-stale | merged | api-environment-secrets | Claude ENV-10; merged into MON-04 | secrets.md says managed RHSM registers only virtual Machines, but managed-os.md and the code also register physical ones |
| CPD-05 | Low | drift | merged | ceph-devices-resources | Claude CPD-05; merged into CRA-03 | Spec still says `ceph fs new` sets allow_standby_replay; X263 moved it to a separate `fs set` after creation |
| CTX-06 | Low | defect | merged | contexts | Claude CTX-06; merged into PRJ-04 | Keyring identity reservations still accept formatVersion 1 inside a version 4 store, so one record format is read rather than refused |
| CTH-03 | Low | defect | merged | controller-host | Claude CTH-03; merged into OUT-10 | A setup whose next run number needs a seventh digit refuses instead of keeping no run |
| CTS-05 | Low | doc-stale | merged | controller-stage | Claude CTS-05; merged into CTH-08 | controller.md says `controller install --download-only` takes no --output json, but output.md, the CLI and its tests give it a JSON result |
| SBV-07 | Low | defect | tracked | substrates-virtual | Claude SBV-07; tracked as B99 | The vSphere arm fact says delivery fetch, where the spec says the vSphere realized target uses delivery push |
