package logview

import "github.com/crmarques/bootwright/internal/diagnostics"

const NewestInstall = "newest"

const (
	SourceAttempt = "attempt"
	SourceRun     = "run"
	SourceInstall = "install"
)

type Selection struct {
	Context   string
	Operation string
	Block     string
	Raw       bool
	Attempt   int
	Run       string
	Install   string
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
	Install    string
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
	Install   string
	State     string
	Warnings  []diagnostics.Diagnostic
}
