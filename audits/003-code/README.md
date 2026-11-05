# Code audit 003

[Audit directory format](../README.md)

- [Codex report: independently checked findings, coverage and validation](report-codex.md)
- [Claude report: the code against its specifications, lane by lane](report-claude.md)

Claude Fable 5.1 assessed `f8b15669f` on 2026-11-05, orchestrating Claude
Opus 5.5, Sonnet 5.5 and Haiku 5.5 agents with each sub-task routed to the
model its difficulty needs. Scope: the Go code, the Ansible collection, the
fitness suite and the e2e cases, judged against the specification page or
section that owns each clause, in 36 lanes. The report distinguishes
confirmed discrepancies (313: 3 High, 64 Medium, 246 Low), discrepancies a
milestone item already records (3), candidates its verifiers refuted (29),
and the limits of a source-level assessment with effect-free probes. It
changes no product code, contract, specification or milestone state, and
authorizes no implementation or live run.

Reading order: section 1 for the verdict and the six items to fix first,
section 4 for the ordered roadmap, section 6 for any one lane's full record,
section 7 for the method and the gates.

Codex assessed the same product baseline and reviewed the later container-build
delta through `88fbf7be1`. Its report records 18 confirmed discrepancies,
separates source proof from synthetic execution and operator qualification,
and documents the models and effort used for the validation subtasks.
