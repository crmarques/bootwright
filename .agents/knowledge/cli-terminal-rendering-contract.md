# Terminal rendering and console-noise contract

**Constraint: one style gate for all colored output.** `NO_COLOR`,
`TERM=dumb`, non-TTY, and piped output strip color entirely — which for
diff rendering yields a plain unified diff consumable by review tooling
or `patch`. `DiffObjectHeader` writes git-analogue `--- desired` /
`+++ real (cluster)` file markers and `DiffHunk` writes `@@ label @@`
so the block reads as a unified diff; add lines are real-state-only
(`+`, green), delete lines are desired-only (`-`, red).

**Semantics: RunView redraws in place or appends, never both.** On an
interactive terminal it redraws the frame in place on every `Render`;
otherwise (piped, CI, or forced via `Streaming()` so the frame does not
fight raw ansible output for the cursor) it emits one append-only
transition line per step as it
becomes RUNNING or terminal, deduplicated by (step ID, status), with no
cursor control. RunView is NOT safe for concurrent use — the
apply/destroy scheduler drives it from a single goroutine reading task
events serially, so it needs no locking (the same invariant is stated on
`applyReporter` and `destroyReporter`).

**Semantics: RenderFrame's `collapse` and the single-writer rule.**
`collapse` summarizes titled groups whose every step finished cleanly
(DONE/SKIPPED/CANCELLED) to one line — wanted for the live in-place
redraw so the operator's eye lands on groups still working; a one-shot
`status` report passes `collapse=false` to print the full record.
`RenderFrame` is deliberately the only frame writer, so all frame byte
output stays in the file allowlisted by
`TestHumanOutputUsesOutputPackage`.

**Gotcha: in-place redraw height accounting.** `visibleWidth` counts
rune width while skipping ANSI CSI escape sequences (`ESC[ … letter`) so
colored lines measure the same as plain ones; `physicalRows` caps at 200
rows per logical line; `width <= 0` means unknown width and disables
wrap accounting entirely, which is safe because a non-TTY never redraws
(`terminalWidth` returns 0 off-TTY, off-Linux, or when the `TIOCGWINSZ`
ioctl fails). Getting this wrong makes `ClearLines` erase too few or too
many rows on redraw.

**Semantics: `status --watch` refresh preamble.** On a TTY each refresh
emits `\x1b[H\x1b[2J` (home cursor + clear screen, like `watch(1)`) so
reports redraw in place instead of stacking multi-section reports onto
scrollback; piped output cannot redraw, so refreshes after the first are
delimited by a timestamped `----- status refresh <RFC3339> -----`
separator and must never emit ANSI cursor control. Pinned by
`TestStatusWatchRefreshPreamble`.

**Constraint: expected read-only oc noise stays off the console.**
`RunConfig.ReadRunner` is a second, quiet OCRunner used for all
read-only `oc` calls in the add-on engine — readiness polls, the
idempotency pre-check, the CSV gate, and the CatalogSource READY gate —
so their commands and full JSON output stay out of both the console and task
log. Active waits emit only `<resource>/<name> <state>` observations to the
task and cluster logs; timeout diagnostics may expand after the wait fails.
The mutating `oc apply` always uses the runner passed to `Apply`, and a nil
`ReadRunner` falls back to it.
`waitCSVSucceeded` and `waitCatalogSourceReady` take the read runner for
the same reason.
