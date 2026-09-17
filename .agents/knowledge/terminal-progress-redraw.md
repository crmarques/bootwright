# Terminal progress redraw width

Observed on 2026-09-15. Required behavior stays in
[long-running progress](../../specs/cli/output.md#long-running-progress).

## Symptom

`apply` printed its plan and then grew one identical `[RUNNING]` line per
second under `Progress`, each cut at exactly the terminal width, instead of
rewriting a single row in place. `setup` on the same terminal settled each step
as one line, because its step labels and details are shorter than the window.

## Mechanism

The redrawn row is `\r\x1b[2K` followed by the row text. `\x1b[2K` erases the
line the cursor is on, and a row wider than the window has already wrapped onto
a second physical line, where the cursor sits. The erase therefore clears only
the overflow, the replacement wraps again, and every refresh leaves one more
copy of the row's first physical line on screen. A lifecycle row reaches about
150 columns on its own: a block description, a presentation group as its
detail, the `(position/total)` counter and the `still running` note.

## Decisions

- [progress.go](../../internal/cli/progress.go) composes the row as a lead, a
  subject and a trailing note, and `fitTerminalRow` bounds the row to the
  reported width, eliding the subject from the right with `...` and keeping the
  note. It leaves the last column empty, so a terminal that wraps as soon as a
  line is filled cannot start a second physical line either.
- [stdin_linux_amd64.go](../../cmd/bootwright/stdin_linux_amd64.go) reads the
  width with `TIOCGWINSZ` for every row rather than once at startup, so a
  resize takes effect on the next refresh, and reports the classic eighty
  columns for a terminal that reports no size. The presenter takes that reader
  in place of a terminal flag: no reader means rows are appended whole.
- Width is counted in Unicode code points after display escaping, as the
  shared layout counts it, so a double-width glyph still overstates the fit.
- A settled sub-step no longer writes a row, so a block that once occupied one
  row per presentation group now occupies one. That shortens the stream rather
  than the row: a lifecycle subject is as wide as before and still needs the
  bound above. The position moved to the front of the subject, where eliding
  the text after it cannot take the counter with it.
- The presenter holds one current step, and only that step's own rows may
  overwrite its line, so every change of step label costs a line. A phase that
  iterates over targets therefore has to report them as sub-steps of one step,
  or it prints one settled line per target. Observed on 2026-09-17: the
  [quiescence gate](../../specs/state-reconciliation.md#quiescence-before-removal)
  reported a row per block under `Progress`, so an eight-block destroy printed
  every step twice, once as a probe before the `Logs` field and once as the
  effect after it. It is one check step now, under `Checks`.

Evidence: `TestTerminalProgressFitsEveryRowInTheTerminalWidth` drives a row
wider than its window and fails on any drawn line that reaches the width,
`TestAppendedProgressRowsAreNeverElided` holds the pipe form whole, and
`TestTerminalColumnsFollowTheWindowSize` resizes a pseudo-terminal through the
same reader. `TestTerminalQuiescenceChecksOccupyOneLine` and
`TestQuiescenceChecksSettleAsOneRowBeforeTheEffects` hold the gate to one row
in both forms.

## A block of running steps, not a single row (2026-09-17)

Blocks of one operation now run concurrently, so the presenter draws one line
per running step instead of one row in total. What that costs to get right:

- **The block's height must be exact.** The redraw returns to the first line
  with `\x1b[<n-1>A`, where `n` is the number of lines it last drew. That is
  only correct because every row is already bounded to the terminal width by
  `fitTerminalRow`: one wrapped row makes the physical height larger than `n`
  and the cursor lands in the middle of the block, smearing it. The width bound
  and the cursor-up are one mechanism, not two features.
- **Erase downward, then return.** `\x1b[2K` does not move the cursor, so the
  erase walks down with `\n` between lines and then moves back up by the same
  count. For a one-line block this is byte-identical to the old
  `\r\x1b[2K` prefix, which is what keeps `setup` output unchanged.
- **A heading owns its block.** `closeLine` now also drops the steps, because a
  block that survived a heading change was redrawn under the new heading and
  printed the previous heading's rows a second time. The visible symptom is
  duplicated check rows under `Progress`.
- **The trailing note belongs to the step, not to the draw.** An event-driven
  row carries no note and a heartbeat row carries `still running, <elapsed>`.
  Storing it on the step keeps that distinction when one step's event redraws a
  block that another step's heartbeat had annotated.
- **Never arm a timer for a cancelled step.** The heartbeat skips a step whose
  context is done without updating its last-row time, so including it in the
  "earliest due" calculation schedules a wake-up in the past, which fires
  immediately, skips again and re-arms — a spin that allocates a row per
  iteration. Under the manual test clock this is an unbounded loop inside one
  `advance` call and the test process is killed. `arm` counts only steps that
  can still report.
