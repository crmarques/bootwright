# Lifecycle flows audit 006

[Audit directory format](../README.md)

- [Claude Fable report: the apply and destroy flows reviewed for weak and unsafe points, for dead ends an operator cannot leave and for what the output says when they fail, with a design that makes the flows one explicit state machine with a code for every stop, and a plan](report-fable.md)

Claude Fable 5.2 assessed `91f6e71b9` on 2026-11-18, orchestrating Claude
Opus 5.5 and Claude Sonnet 5.5 agents, each given an explicit model and
effort. Scope: everything `bootwright apply` and `bootwright destroy` do,
from the command line to the last durable write: the lifecycle engine, the
operation and context stores, the adapter runner and protocol, every
capability in Go and the roles, playbooks and Python of the embedded Ansible
collection that those capabilities run; the commands an operator uses around
a failure; the operator guide and the end-to-end runbooks; and the private
operation logs of 29 retained real runs.

The report records 380 verified findings (16 High, 126 Medium, 238 Low) and
14 refuted candidates, a measured baseline, a map of 1,247 families of
failure points and 497 implicit states, a design for the meta view the owner
asked for, a plan of seven workstreams and thirteen decisions only the owner
can take. It changes no product code, contract, specification or milestone
state, and authorizes no implementation or live run.

Reading order: section 1 for the verdict and the ten actions that matter
most; section 2 for the measured baseline; section 3 for what the two flows
do today and how much of them is already a state machine; sections 5 to 7
for the unsafe points, the dead ends and what the operator is told;
section 8 for the design; section 9 for the plan and section 10 for the
owner's decisions; section 11 for the method and its limits; section 12 for
the catalog of every finding; Appendix A for any one finding's record.

Sixty-two review lanes read the frozen tree. Every finding was then checked
by at least one independent verifier, every High finding by two with an
arbiter on a split, and each record was rewritten to state what the
verifiers established. No test, build or command was run: every scenario was
established by reading code, and the report says so.
