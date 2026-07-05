package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
)

type Status string

const (
	StatusOK      Status = "OK"
	StatusDone    Status = "DONE"
	StatusRunning Status = "RUNNING"
	StatusPending Status = "PENDING"
	StatusMissing Status = "MISSING"
	StatusFail    Status = "FAIL"
	StatusFailed  Status = "FAILED"
	StatusInfo    Status = "INFO"
	StatusWarn    Status = "WARN"
	StatusSkip    Status = "SKIP"
	StatusSkipped Status = "SKIPPED"
	StatusBlocked Status = "BLOCKED"
	StatusCancel  Status = "CANCELLED"
)

type Field struct {
	Key   string
	Value string
}

type Item struct {
	Label  string
	Detail string
}

type PlanItem struct {
	Name        string
	Description string
	Root        bool
}

type ArtifactGroup struct {
	Name  string
	Paths []string
}

type Check struct {
	Group       string
	Name        string
	Status      Status
	Evidence    string
	Impact      string
	Remediation string
}

type ProgressField struct {
	Label string
	Count int
}

type TaskLine struct {
	Status Status
	Label  string
	Detail string
}

type Printer struct {
	w     io.Writer
	color bool
	wrote bool
}

func New(w io.Writer) *Printer {
	return &Printer{w: w, color: colorEnabled(w)}
}

func NewContinuation(w io.Writer) *Printer {
	return &Printer{w: w, color: colorEnabled(w), wrote: true}
}

func JSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (p *Printer) Command(label string) {
	if p == nil || p.w == nil {
		return
	}
	if p.wrote {
		fmt.Fprintln(p.w)
	}
	fmt.Fprintf(p.w, "%s: %s\n", p.style("Bootwright", color.Bold, color.FgCyan), label)
	p.wrote = true
}

func (p *Printer) Section(label string) {
	if p == nil || p.w == nil {
		return
	}
	if p.wrote {
		fmt.Fprintln(p.w)
	}
	fmt.Fprintln(p.w, p.style(label, color.Bold, color.FgCyan))
	p.wrote = true
}

func (p *Printer) Fields(fields []Field) {
	if p == nil || p.w == nil || len(fields) == 0 {
		return
	}
	for _, field := range fields {
		fmt.Fprintf(p.w, "  %s: %s\n", field.Key, field.Value)
	}
	p.wrote = true
}

func (p *Printer) List(items []Item) {
	if p == nil || p.w == nil || len(items) == 0 {
		return
	}
	for _, item := range items {
		if item.Detail == "" {
			fmt.Fprintf(p.w, "  - %s\n", item.Label)
			continue
		}
		fmt.Fprintf(p.w, "  - %s: %s\n", item.Label, item.Detail)
	}
	p.wrote = true
}

func (p *Printer) Plan(items []PlanItem) {
	if p == nil || p.w == nil || len(items) == 0 {
		return
	}
	for _, item := range items {
		name := item.Name
		if item.Root {
			name += " [root]"
		}
		fmt.Fprintf(p.w, "  - %s: %s\n", name, item.Description)
	}
	p.wrote = true
}

func (p *Printer) Progress(label string, fields []ProgressField) {
	if p == nil || p.w == nil {
		return
	}
	var parts []string
	for _, field := range fields {
		if field.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", field.Count, strings.ToUpper(field.Label)))
	}
	if len(parts) == 0 {
		parts = append(parts, "0 tasks")
	}
	p.Status(StatusOK, label, strings.Join(parts, ", "))
}

func (p *Printer) ProgressBar(label string, done int, total int, fields []ProgressField) {
	if p == nil || p.w == nil {
		return
	}
	fmt.Fprintf(p.w, "%s\n", progressBarLine(label, done, total, fields))
	p.wrote = true
}

// progressBarLine renders the bar line shared by ProgressBar and RenderFrame,
// without a trailing newline or leading indent.
func progressBarLine(label string, done int, total int, fields []ProgressField) string {
	if total < 0 {
		total = 0
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	const width = 20
	filled := 0
	if total > 0 {
		filled = done * width / total
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", field.Count, strings.ToUpper(field.Label)))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "0 tasks")
	}
	return fmt.Sprintf("%s  [%s] %d/%d %s", label, bar, done, total, strings.Join(parts, "  "))
}

func (p *Printer) Tasks(items []TaskLine) {
	if p == nil || p.w == nil || len(items) == 0 {
		return
	}
	for _, item := range items {
		if item.Detail == "" {
			fmt.Fprintf(p.w, "  %s %s\n", p.statusLabel(item.Status), item.Label)
			continue
		}
		fmt.Fprintf(p.w, "  %s %s: %s\n", p.statusLabel(item.Status), item.Label, item.Detail)
	}
	p.wrote = true
}

func (p *Printer) Artifacts(groups []ArtifactGroup) {
	if p == nil || p.w == nil || len(groups) == 0 {
		return
	}
	for _, group := range groups {
		if group.Name != "" {
			fmt.Fprintf(p.w, "  %s\n", group.Name)
		}
		for _, path := range group.Paths {
			fmt.Fprintf(p.w, "    - %s\n", path)
		}
	}
	p.wrote = true
}

func (p *Printer) Checks(checks []Check) {
	if p == nil || p.w == nil || len(checks) == 0 {
		return
	}
	for _, group := range groupedChecks(checks) {
		p.Section(group.name)
		for _, check := range group.checks {
			fmt.Fprintf(p.w, "  %s %s", p.statusLabel(check.Status), check.Name)
			if check.Evidence != "" {
				fmt.Fprintf(p.w, ": %s", check.Evidence)
			}
			fmt.Fprintln(p.w)
			if check.Status != StatusOK {
				if check.Impact != "" {
					fmt.Fprintf(p.w, "      impact: %s\n", check.Impact)
				}
				if check.Remediation != "" {
					fmt.Fprintf(p.w, "      fix: %s\n", check.Remediation)
				}
			}
		}
	}
	p.wrote = true
}

func (p *Printer) Summary(status Status, label string, detail string) {
	if p == nil || p.w == nil {
		return
	}
	p.Section("Summary")
	p.Status(status, label, detail)
}

func (p *Printer) Status(status Status, label string, detail string) {
	if p == nil || p.w == nil {
		return
	}
	if detail == "" {
		fmt.Fprintf(p.w, "  %s %s\n", p.statusLabel(status), label)
		p.wrote = true
		return
	}
	fmt.Fprintf(p.w, "  %s %s: %s\n", p.statusLabel(status), label, detail)
	p.wrote = true
}

func (p *Printer) Warning(label string, detail string) {
	p.Status(StatusWarn, label, detail)
}

func (p *Printer) BlankLine() {
	if p == nil || p.w == nil {
		return
	}
	fmt.Fprintln(p.w)
	p.wrote = true
}

func (p *Printer) Details(fields []Field) {
	if p == nil || p.w == nil || len(fields) == 0 {
		return
	}
	for _, field := range fields {
		fmt.Fprintf(p.w, "      %s: %s\n", field.Key, field.Value)
	}
	p.wrote = true
}

func (p *Printer) Error(err error) {
	if err == nil {
		return
	}
	p.Section("Error")
	lines := strings.Split(err.Error(), "\n")
	p.Status(StatusFail, "command failed", lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(p.w, "      %s\n", line)
	}
}

func (p *Printer) CommandLine(label string, args []string) {
	if p == nil || p.w == nil {
		return
	}
	if label == "" {
		label = "command"
	}
	fmt.Fprintf(p.w, "  %s\n", label)
	fmt.Fprintf(p.w, "    $ %s\n", ShellQuote(args))
	p.wrote = true
}

// DiffLineKind classifies one line of a git-diff-style rendering.
type DiffLineKind int

const (
	// DiffContext is an unchanged line (rendered plain, space-prefixed).
	DiffContext DiffLineKind = iota
	// DiffAdd is a line present in real state but not desired (green, "+").
	DiffAdd
	// DiffDel is a line present in desired state but not real (red, "-").
	DiffDel
)

// DiffLine is one rendered diff line: its kind and its text (without the
// +/-/space prefix, which the renderer adds).
type DiffLine struct {
	Kind DiffLineKind
	Text string
}

// DiffObjectHeader opens a per-object diff section, git's "diff --git a/… b/…"
// analogue: a bold title naming the resource, optionally with a one-line note
// (e.g. "drifted", "only on cluster"). It writes the "--- desired" / "+++ real
// (cluster)" file markers so the block reads as a unified diff.
func (p *Printer) DiffObjectHeader(title, note string) {
	if p == nil || p.w == nil {
		return
	}
	if p.wrote {
		fmt.Fprintln(p.w)
	}
	heading := title
	if note != "" {
		heading += "  (" + note + ")"
	}
	fmt.Fprintln(p.w, p.style(heading, color.Bold))
	fmt.Fprintln(p.w, p.style("--- desired", color.Bold))
	fmt.Fprintln(p.w, p.style("+++ real (cluster)", color.Bold))
	p.wrote = true
}

// DiffHunk writes a hunk header ("@@ <label> @@" in cyan, git's hunk-header
// color) grouping the lines that follow under one facet, e.g. a pool or the OSD
// device selection.
func (p *Printer) DiffHunk(label string) {
	if p == nil || p.w == nil {
		return
	}
	fmt.Fprintln(p.w, p.style("@@ "+label+" @@", color.FgCyan))
	p.wrote = true
}

// DiffLines writes a run of diff lines, coloring add lines green and delete
// lines red through the same style gate as every other output (so NO_COLOR /
// TERM=dumb / non-TTY / piped output strip color and yield a plain unified diff
// consumable by review tooling).
func (p *Printer) DiffLines(lines []DiffLine) {
	if p == nil || p.w == nil || len(lines) == 0 {
		return
	}
	for _, line := range lines {
		fmt.Fprintln(p.w, p.diffLine(line))
	}
	p.wrote = true
}

func (p *Printer) diffLine(line DiffLine) string {
	switch line.Kind {
	case DiffAdd:
		return p.style("+"+line.Text, color.FgGreen)
	case DiffDel:
		return p.style("-"+line.Text, color.FgRed)
	default:
		return " " + line.Text
	}
}

func (p *Printer) statusLabel(status Status) string {
	label := "[" + string(status) + "]"
	switch status {
	case StatusOK, StatusDone:
		return p.style(label, color.FgGreen)
	case StatusRunning:
		return p.style(label, color.FgYellow)
	case StatusPending:
		return p.style(label, color.FgHiBlack)
	case StatusMissing:
		return p.style(label, color.Bold, color.FgYellow)
	case StatusFail, StatusFailed:
		return p.style(label, color.Bold, color.FgRed)
	case StatusInfo:
		return p.style(label, color.FgBlue)
	case StatusWarn:
		return p.style(label, color.FgYellow)
	case StatusBlocked:
		return p.style(label, color.Bold, color.FgYellow)
	case StatusSkip, StatusSkipped, StatusCancel:
		return p.style(label, color.FgHiBlack)
	default:
		return label
	}
}

func (p *Printer) style(value string, attrs ...color.Attribute) string {
	if !p.color {
		return value
	}
	return color.New(attrs...).Sprint(value)
}

func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return Interactive(w)
}

func Interactive(w io.Writer) bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func Write(w io.Writer, text string) {
	if w == nil || text == "" {
		return
	}
	_, _ = io.WriteString(w, text)
}

func ClearLines(w io.Writer, lines int) {
	if w == nil || lines <= 0 {
		return
	}
	fmt.Fprintf(w, "\x1b[%dA\x1b[J", lines)
}

type checkGroup struct {
	name   string
	checks []Check
}

func groupedChecks(checks []Check) []checkGroup {
	var groups []checkGroup
	index := map[string]int{}
	for _, check := range checks {
		group := check.Group
		if group == "" {
			group = "Checks"
		}
		if i, ok := index[group]; ok {
			groups[i].checks = append(groups[i].checks, check)
			continue
		}
		index[group] = len(groups)
		groups = append(groups, checkGroup{name: group, checks: []Check{check}})
	}
	return groups
}

func ShellQuote(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			quoted = append(quoted, "''")
			continue
		}
		if strings.ContainsAny(arg, " \t\n'\"$`\\") {
			quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}

// Step is one unit of work shown in a RunFrame.
type Step struct {
	ID     string
	Label  string
	Status Status
	Detail string
}

// StepGroup is a titled set of steps, e.g. one cluster or the "infra" group.
type StepGroup struct {
	Title string
	Steps []Step
}

// RunFrame is one progress snapshot: a header progress bar plus grouped steps.
// It is source-agnostic so apply, destroy, preflight, and `status --watch` can
// all feed the same RunView. Done/Total/Counts drive the bar; Groups drive the
// step list.
type RunFrame struct {
	BarLabel string
	Done     int
	Total    int
	Counts   []ProgressField
	Groups   []StepGroup
}

// RenderFrame writes a RunFrame (header progress bar + grouped step list) and
// returns the number of physical terminal rows it occupied. width<=0 disables
// wrap accounting (one row per logical line). collapse summarizes titled groups
// whose every step finished cleanly to a single line — wanted for the live
// in-place redraw (so the operator's eye lands on the groups still working)
// but not for a one-shot `status` report, which prints the full record.
// Callers that redraw in place clear the returned row count before writing the
// next frame. RenderFrame is the only frame writer, so all frame byte output
// stays in this allowlisted file.
func (p *Printer) RenderFrame(frame RunFrame, width int, collapse bool) int {
	if p == nil || p.w == nil {
		return 0
	}
	rows := 0
	emit := func(line string) {
		fmt.Fprintln(p.w, line)
		rows += physicalRows(line, width)
	}
	emit("")
	emit("  " + progressBarLine(frame.BarLabel, frame.Done, frame.Total, frame.Counts))
	emit("")
	for i, group := range frame.Groups {
		if i > 0 {
			emit("")
		}
		if summary, done := finishedGroupSummary(group); collapse && done {
			// Every step of a titled group reached a clean terminal state: collapse
			// it to its heading plus a one-line summary so the operator's eye lands
			// on the groups still doing work instead of scanning a wall of DONE
			// steps. Groups with a failed/blocked/pending/running step (or no
			// title, e.g. the destroy group) stay fully expanded.
			emit("  " + p.style(group.Title+"  "+summary, color.Bold))
			continue
		}
		if group.Title != "" {
			emit("  " + p.style(group.Title, color.Bold))
		}
		for _, step := range group.Steps {
			emit(p.stepLine(step))
		}
	}
	p.wrote = true
	return rows
}

// finishedGroupSummary reports a collapsed one-line summary for a titled group
// whose every step finished cleanly (DONE/SKIPPED/CANCELLED, none failed,
// blocked, pending, or running). It returns ("", false) when the group must
// stay expanded.
func finishedGroupSummary(group StepGroup) (string, bool) {
	if group.Title == "" || len(group.Steps) == 0 {
		return "", false
	}
	done, skipped, cancelled := 0, 0, 0
	for _, step := range group.Steps {
		switch step.Status {
		case StatusOK, StatusDone:
			done++
		case StatusSkip, StatusSkipped:
			skipped++
		case StatusCancel:
			cancelled++
		default:
			return "", false
		}
	}
	parts := make([]string, 0, 3)
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d done", done))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	if cancelled > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", cancelled))
	}
	return "(" + strings.Join(parts, ", ") + ")", true
}

func (p *Printer) stepLine(step Step) string {
	label := step.Label
	if step.Detail != "" {
		label += ": " + step.Detail
	}
	return "    " + p.statusLabel(step.Status) + " " + label
}

// newBufferPrinter returns a Printer that writes to an in-memory buffer with the
// given color setting, so a caller (RunView) can render a frame once, measure
// and compare it, then write the result to the real terminal with color intact.
func newBufferPrinter(colored bool) (*Printer, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &Printer{w: buf, color: colored, wrote: true}, buf
}

// physicalRows reports how many terminal rows a logical line occupies once
// wrapped to width columns. width<=0 means width is unknown (no wrap
// accounting), so the line counts as one row.
func physicalRows(line string, width int) int {
	if width <= 0 {
		return 1
	}
	w := visibleWidth(line)
	if w <= width {
		return 1
	}
	rows := (w + width - 1) / width
	const maxRows = 200
	if rows > maxRows {
		rows = maxRows
	}
	return rows
}

// visibleWidth counts the rune width of a line, skipping ANSI CSI escape
// sequences (ESC[ ... letter) so colored lines measure the same as plain ones.
func visibleWidth(s string) int {
	n := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b { // ESC
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) {
					c := s[i]
					i++
					if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
						break
					}
				}
			}
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}
