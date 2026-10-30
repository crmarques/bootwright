# Add-ons audit 002

[Audit directory format](../README.md)

- [Consolidated report: findings, disagreements and the merged design](report-final.md)
- [Claude report: architecture, API, operator CLI, developer path, default and optional add-ons](report-claude.md)
- [Codex report: architecture, API, operator CLI and add-on development](report-codex.md)

Codex assessed `2f35ee8c3` on 2026-10-29; Claude assessed `4294e7520` on
2026-10-30, and no add-on code, schema, spec or example changed between the
two. The consolidated report was written after both, checks each source
finding against the tree and records where the reports disagree. Every
report distinguishes implemented behavior, reproduced defects, intentional
limits and proposals, and authorizes no implementation or live run.
