# Audits

Keep each audit and its follow-ups under `audits/<NNN>-<topic>/`. Number
audits across topics, starting at `001`; use a short topic name. The
specification audit is `001-spec`, and the add-ons audit is `002-addons`.

## Directory format

```text
audits/<NNN>-<topic>/
  README.md
  report-final.md
  report-<reviewer>.md
  follow-up/
    001/
      report-<reviewer>.md
```

Use numbered follow-up rounds beneath one undated `follow-up/` directory.
Number rounds independently within each audit, starting at `001`; a subsequent
reassessment uses the next number. A reviewer's follow-up is one self-contained
Markdown file, such as `audits/001-spec/follow-up/001/report-codex.md`. Its
assessment, plan, finding ledger and retained evidence belong in that file,
with no companion plan, JSON or log files.

The audit `README.md` provides navigation. Source reports retain the original
audit findings, scope and contributor observations. Follow-up reports assess
those findings at explicitly identified dates and commits. Create only the
reports that exist; the layout requires no empty placeholders.

## Follow-up report format

Open with the source audit, follow-up number, reviewer, assessment date,
assessed commits and scope. Identify different completion and quality
baselines separately. Distinguish editorial consolidation from a fresh code
assessment. Provide section links in this reading order:

1. **Executive summary:** disposition counts, consequential remaining work
   and a quality judgment bounded by the evidence inspected.
2. **Remaining work:** open/partial findings and carried limitations.
3. **Quality assessment:** implementation, tests, maintainability, decisions,
   documentation corrections and operational limits; state sampling depth.
4. **Implementation plan:** work packages, dependencies, decisions, milestone
   carriers and exit evidence.
5. **Verification and limits:** commands, results and relevant execution
   conditions. Distinguish failures, skips and unrun checks from passes.
6. **Complete findings:** one disposition and qualification per source ID.
7. **Structured ledger and retained output:** embed JSON and original check
   output in labeled fenced appendices; expandable sections keep them readable.

Keep the human findings and embedded ledger consistent. Identify the command,
date and assessed commit for each retained output. Historical output remains
verbatim and is distinguished from checks of later document edits.

## Revisions and ownership

Correct a report in a new commit; Git preserves its earlier revisions. A new
assessment round gets the next follow-up number and records what was rechecked.
Keep older baselines explicit for claims not reassessed. A move or format edit
changes no finding disposition and supplies no fresh product evidence.

Retain source reports as historical records. Repair relocation links without
rewriting their observations as current findings. Closure at an accepted
documentation or decision scope does not imply implementation or live acceptance.
Milestones own current delivery status; a plan authorizes no implementation
or effects.

## Audit index

- [Specification audit 001](001-spec/README.md)
- [Add-ons audit 002](002-addons/README.md)
