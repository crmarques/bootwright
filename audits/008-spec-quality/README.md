# Specification quality audit 008

[Audit directory format](../README.md)

- [Claude Fable report: the specifications audited for incoherences and errors, for legibility, objectivity and redundancy, and as the input of AI coding agents, with a plan to optimize them](report-fable.md)

Claude Fable 5.2 assessed `e88d0c6cb` on 2026-11-24, orchestrating Claude
Opus 5.5 and Claude Sonnet 5.5 agents, each given an explicit model and
effort. Scope: every page under `specs/` (63 pages, 5.34 MB: the normative
pages, the deferred designs and the milestone pages), with the code, the
tests, the skills and the earlier audits read as evidence. 249 lanes read
the corpus: every line of every page belonged to a lane that read it in
full, and other lanes swept one subject across pages, traced routes, counted
rule form and ran twelve implementation dry runs.

The report records 1,574 verified findings (33 High, 655
Medium, 886 Low; none Blocking) and 51 refuted candidates, a
measured baseline (size and growth, routing, units of loading, sentence
form, repetition, inventories and their tests, effect rules, delivery
pages), a plan in four lanes and seventeen decisions that only the owner can
take. It changes no product code, contract, specification or milestone
state, and authorizes no implementation or live run.

Reading order: section 1 for the verdict, the scorecard and the seven
actions that matter most; section 7 for the plan, with the owner's decisions
in 7.11; section 2 for the measured baseline; sections 3 to 6 for one class
of finding each; section 8 for the method and its limits; section 9 for what
became of audit 004's proposals; section 10 for the catalog of every
finding; Appendix A for what each lane read; Appendix B for the working
tables of the cross-cutting lanes; Appendix C for leads that nobody
verified.

Every candidate was checked by at least one independent verifier; every
candidate rated Medium or above by a refuter and a judge who did not see
each other's work, with an arbiter on a split; and every finding that stood
at High was read a third time. The orchestrator re-read 29 of the 33 High
findings at the cited text. No auditor ran a build, a test or a Bootwright
command: findings about code rest on reading it, and the report says so.

Main moved while the audit ran, and the findings stay on the audited commit.
Section 8.8 names the three later commits, up to `ea3eeb10d`: they add 219
lines to eight spec pages, which were not audited, and change one line that a
Low finding cites.
