package logview

import "github.com/crmarques/bootwright/internal/diagnostics"

const NewestSetup = "newest"

const (
	SourceAttempt = "attempt"
	SourceRun     = "run"
	SourceSetup   = "setup"
)

type Selection struct {
	Context   string
	Operation string
	Block     string
	Raw       bool
	Attempt   int
	Run       string
	Setup     string
	FullTimes bool
}

type Event struct {
	Time       string
	Block      string
	Attempt    int
	Resolution int
	Event      string
	Group      string
	Detail     string
}

type Source struct {
	Kind       string
	Operation  string
	Block      string
	Attempt    int
	Resolution int
	Run        string
	Setup      string
}

type Raw struct {
	Source    Source
	Path      string
	Bytes     int64
	Truncated bool
	Content   []byte
}

type Result struct {
	Context   string
	Operation string
	Verb      string
	State     string
	Events    []Event
	Raw       *Raw
	Warnings  []diagnostics.Diagnostic
}

type Line struct {
	Source string
	Text   string
}

type Summary struct {
	Context   string
	Operation string
	Run       string
	Setup     string
	State     string
	Warnings  []diagnostics.Diagnostic
}
