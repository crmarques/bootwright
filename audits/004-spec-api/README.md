# Specification and user API audit 004

[Audit directory format](../README.md)

- [Claude Fable report: the specifications, the desired-state API and the CLI as written contracts](report-fable.md)

Claude Fable 5.2 assessed `931cfb596` on 2026-11-10, orchestrating Claude
Opus 5.5 and Sonnet 5.5 agents with each sub-task routed to the model its
difficulty needs. Scope: every page under `specs/`, the skills and agent
instructions, the operator documentation and the tracked examples, with the Go
package map, the API types, the command catalog and the Ansible collection
layout read as evidence only, in 150 lanes. The report distinguishes verified
findings (637: 1 Blocking, 30 High, 182 Medium, 424 Low), findings an open
item already records (3), candidates a recorded decision settles (11) and
candidates its verifiers refuted (21). It changes no product code, contract,
specification or milestone state, and authorizes no implementation or live
run.

Reading order: section 2 for the verdict, the scorecard and the five actions;
section 15 for the ordered roadmap and the decisions only the owner can take;
sections 3 to 11 for one subject each; section 12 for any one finding;
section 16 for the method and its limits; section 17 for what later commits
change.

Main moved while the audit ran, and the findings above stay on the audited
commit. Section 17 covers the first three later commits, up to `aa46736e7`:
of the 27 findings that cite text or a subject they changed, 2 are resolved
and 5 partly resolved, and the text they added has 20 findings of its own
(1 High, 5 Medium, 14 Low), counted apart. Two commits after `aa46736e7` were
not read; section 17.7 names them and the two findings to check against them
first.
