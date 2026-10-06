package diagnostics

import (
	"errors"
	"slices"
)

type SourceLocation struct {
	Path     string `json:"path"`
	Document int    `json:"document"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type ObjectIdentity struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type Diagnostic struct {
	Severity    string          `json:"severity"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	Source      *SourceLocation `json:"source,omitempty"`
	Object      *ObjectIdentity `json:"object,omitempty"`
	Field       string          `json:"field,omitempty"`
	Remediation string          `json:"remediation,omitempty"`
}

// Failure carries a refusal's diagnostics. Usage marks a refusal of how the
// command was invoked, which the CLI reports as a usage failure.
type Failure struct {
	Diagnostics []Diagnostic
	Usage       bool
}

func (f *Failure) Error() string { return "failed with diagnostics" }

func IsUsage(err error) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Usage
}

func NewFailure(code, message, path string) error {
	return NewFailureWithRemediation(code, message, path, "")
}

func NewFailureWithRemediation(code, message, path, remediation string) error {
	d := Diagnostic{Severity: "error", Code: code, Message: message, Remediation: remediation}
	if path != "" {
		d.Source = &SourceLocation{Path: path}
	}
	return &Failure{Diagnostics: []Diagnostic{d}}
}

func Of(err error) []Diagnostic {
	var failure *Failure
	if errors.As(err, &failure) {
		out := append([]Diagnostic(nil), failure.Diagnostics...)
		for i := range out {
			if out[i].Source != nil {
				source := *out[i].Source
				out[i].Source = &source
			}
			if out[i].Object != nil {
				object := *out[i].Object
				out[i].Object = &object
			}
		}
		return out
	}
	return nil
}

func Sort(ds []Diagnostic) {
	slices.SortStableFunc(ds, Compare)
}

func Compare(a, b Diagnostic) int {
	if a.Source == nil && b.Source != nil {
		return 1
	}
	if a.Source != nil && b.Source == nil {
		return -1
	}
	if a.Source != nil && b.Source != nil {
		if c := compare(a.Source.Path, b.Source.Path); c != 0 {
			return c
		}
		if c := a.Source.Document - b.Source.Document; c != 0 {
			return c
		}
		if c := a.Source.Line - b.Source.Line; c != 0 {
			return c
		}
		if c := a.Source.Column - b.Source.Column; c != 0 {
			return c
		}
	}
	if c := compare(a.Code, b.Code); c != 0 {
		return c
	}
	if a.Object == nil && b.Object != nil {
		return -1
	}
	if a.Object != nil && b.Object == nil {
		return 1
	}
	if a.Object != nil && b.Object != nil {
		if c := compare(a.Object.APIVersion, b.Object.APIVersion); c != 0 {
			return c
		}
		if c := compare(a.Object.Kind, b.Object.Kind); c != 0 {
			return c
		}
		if c := compare(a.Object.Name, b.Object.Name); c != 0 {
			return c
		}
	}
	if c := compare(a.Field, b.Field); c != 0 {
		return c
	}
	return compare(a.Message, b.Message)
}

func compare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
