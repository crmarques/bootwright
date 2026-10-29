# Audits

Keep each audit and its follow-up together under `audits/<topic>/<NNN>/`.
Number audits independently within each topic, starting at `001`; the next
specification audit after `spec/001` is `spec/002`. An audit number identifies
one source audit, not each later reassessment of its findings.

## Directory format

```text
audits/<topic>/<NNN>/
  README.md
  report-final.md
  report-<reviewer>.md
  follow-up/
    report.md
    implementation-plan.md
    findings.json
    evidence/
      docs-check.txt
```

Use one undated `follow-up/` directory. Keep assessment dates and reviewed
commits in the documents, and record subsequent revisions in Git. Do not
create a new dated folder for each reassessment.

| File | Owns |
| --- | --- |
| Audit `README.md` | Navigation to source reports and follow-up artifacts. Link to conclusions instead of repeating status counts or quality judgments. |
| `report-final.md` | The source audit's consolidated findings and original scope. |
| `report-<reviewer>.md` | Each retained contributor report, named by reviewer. Keep the original observations distinguishable from the consolidated report. |
| `follow-up/report.md` | The human assessment of finding disposition, remediation quality, remaining risks and verification limits, followed by the full finding appendix. |
| `follow-up/findings.json` | The structured finding ledger, including stable finding IDs, dispositions, evidence and assessment metadata. Keep its data consistent with the report. |
| `follow-up/implementation-plan.md` | Proposed work, dependencies, decisions and exit evidence for remaining findings. Link to milestone items for current delivery state. |
| `follow-up/evidence/` | Retained check output and other supporting artifacts cited by the assessment. |

Create only the artifacts that exist for the audit's current stage; the
layout does not require empty reviewer reports or evidence files.

## Follow-up report format

Open with the audit reference, assessment date, assessed commit and scope.
When completion and quality were assessed at different commits, identify
both. Distinguish an editorial update from a fresh reassessment of code.

Use this reading order:

1. **Executive summary:** completion counts, the most consequential remaining
   work and an overall quality judgment bounded by the evidence inspected.
2. **Remaining work:** open and partial findings, carried limitations and
   links to the implementation plan and milestone items.
3. **Quality of addressed work:** implementation and test evidence,
   maintainability, accepted decisions, documentation-only corrections and
   operational qualification limits. State the depth of sampling.
4. **Verification and limits:** exact commands, results, assessed baselines
   and links to retained evidence. Keep failures, skips and unrun checks
   distinguishable from passes.
5. **Full findings:** one disposition for every source finding ID, with its
   evidence and any remaining qualification. Reconcile counts and IDs with
   the structured ledger.

Define disposition terms in the report. Closure at an accepted documentation
or decision scope does not imply implemented behavior or live acceptance.
The source audit's findings remain identifiable even when a later decision
changes the accepted remedy.

## Revisions and evidence

Update the same follow-up files in a new commit when a reassessment changes
their conclusions. Record what was reviewed, its commit and its date; keep
older assessed baselines explicit for any claims not rechecked. Git preserves
the preceding versions. A move, rename or report-format edit changes no
finding disposition and supplies no fresh evidence about product behavior.

Retain source reports as the original audit record. Fix relocation links
without silently rewriting their historical observations as current findings.

For each retained check output, the report records the command, assessed
commit, outcome and relevant execution conditions. Give new evidence files
descriptive names such as `docs-check-<short-commit>.txt`; retain existing
names when their baseline is already identified. Different baselines should
not share an ambiguous output file.

Milestone pages own current delivery status. Audit assessments are judgments
at their stated baselines, and an implementation plan does not activate work
or authorize effects.

## Audit index

- [Specification audit 001](spec/001/README.md)
