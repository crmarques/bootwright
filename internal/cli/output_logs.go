package cli

import (
	"io"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/logview"
)

type logsResult struct {
	Context   string      `json:"context"`
	Operation string      `json:"operation"`
	Verb      string      `json:"verb"`
	State     string      `json:"state"`
	Events    []logsEvent `json:"events"`
}

func (logsResult) documentedResult() {}

type logsEvent struct {
	Time       string `json:"time"`
	Block      string `json:"block"`
	Attempt    int    `json:"attempt"`
	Resolution int    `json:"resolution"`
	Event      string `json:"event"`
	Group      string `json:"group"`
	Detail     string `json:"detail"`
}

type logsOutputResult struct {
	Source    logsSource         `json:"source"`
	Path      string             `json:"path"`
	Bytes     int64              `json:"bytes"`
	Truncated *bool              `json:"truncated,omitempty"`
	Output    exportPresentation `json:"output"`
}

func (logsOutputResult) documentedResult() {}

type logsSource struct {
	Kind       string `json:"kind"`
	Operation  string `json:"operation,omitempty"`
	Block      string `json:"block,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	Resolution int    `json:"resolution,omitempty"`
	Run        string `json:"run,omitempty"`
	Install    string `json:"install,omitempty"`
}

func displayLogs(result *logview.Result) jsonResult {
	if raw := result.Raw; raw != nil {
		source := raw.Source
		shown := logsOutputResult{
			Source: logsSource{Kind: escapeDisplayLine(source.Kind), Operation: escapeDisplayLine(source.Operation), Block: escapeDisplayLine(source.Block),
				Attempt: source.Attempt, Resolution: source.Resolution, Run: escapeDisplayLine(source.Run), Install: escapeDisplayLine(source.Install)},
			Path: escapeDisplayLine(raw.Path), Bytes: raw.Bytes, Output: exportedResult("text/plain", raw.Content).Export,
		}
		if source.Kind == logview.SourceAttempt {
			shown.Truncated = &raw.Truncated
		}
		return shown
	}
	shown := logsResult{Context: escapeDisplayLine(result.Context), Operation: escapeDisplayLine(result.Operation),
		Verb: escapeDisplayLine(result.Verb), State: escapeDisplayLine(result.State), Events: make([]logsEvent, 0, len(result.Events))}
	for _, event := range result.Events {
		shown.Events = append(shown.Events, logsEvent{Time: escapeDisplayLine(event.Time), Block: escapeDisplayLine(event.Block), Attempt: event.Attempt,
			Resolution: event.Resolution, Event: escapeDisplayLine(event.Event), Group: escapeDisplayLine(event.Group), Detail: escapeDisplayLine(event.Detail)})
	}
	return shown
}

func writeLogs(out, errOut io.Writer, result *logview.Result, structured bool) error {
	if structured {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: "logs", OK: true, ExitCode: 0,
			Result: displayLogs(result), Diagnostics: displayDiagnostics(result.Warnings), Logs: []string{}})
	}
	presenter := newLogsPresenter(out)
	if raw := result.Raw; raw != nil {
		source := raw.Source.Block
		switch raw.Source.Kind {
		case logview.SourceRun:
			source = raw.Source.Run
		case logview.SourceInstall:
			source = raw.Source.Install
		}
		for _, line := range outputLines(raw.Content) {
			if err := presenter.Output(logview.Line{Source: source, Text: line}); err != nil {
				return err
			}
		}
	} else {
		for _, line := range logview.RenderEvents(result.Events, presentationFor(out).Detail == "full") {
			if err := presenter.Event(logview.Line{Text: line}); err != nil {
				return err
			}
		}
	}
	return writeHumanDiagnostics(errOut, displayDiagnostics(result.Warnings))
}

func outputLines(content []byte) []string {
	text := strings.TrimSuffix(string(content), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

func writeLogsFollowed(errOut io.Writer, summary *logview.Summary) error {
	return writeHumanDiagnostics(errOut, displayDiagnostics(summary.Warnings))
}

type logsPresenter struct{ out io.Writer }

type logsOutputFailure struct{}

func (*logsOutputFailure) Error() string { return "log output failed" }

func newLogsPresenter(out io.Writer) logsPresenter {
	if out == nil {
		out = io.Discard
	}
	return logsPresenter{out: out}
}

func (p logsPresenter) Event(line logview.Line) error { return p.write(escapeDisplayLine(line.Text)) }

func (p logsPresenter) Output(line logview.Line) error {
	return p.write("[" + escapeDisplayLine(line.Source) + "] " + escapeDisplayLine(line.Text))
}

func (p logsPresenter) write(line string) error {
	if _, err := io.WriteString(p.out, line+"\n"); err != nil {
		return &logsOutputFailure{}
	}
	return nil
}

func humanBytes(size int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return strconv.FormatInt(size, 10) + " " + units[unit]
}
