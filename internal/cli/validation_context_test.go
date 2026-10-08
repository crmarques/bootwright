package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type retiredFileSourceState struct {
	DesiredStateService
	requests []compilation.ValidateRequest
}

func (s *retiredFileSourceState) Validate(_ context.Context, request compilation.ValidateRequest) (*compilation.Report, error) {
	s.requests = append(s.requests, request)
	return nil, &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{
		{
			Severity: "error", Code: "api.field", Field: "$.spec.source.file", Message: "the Secret file source is retired",
			Remediation: "declare source: {contextStore: {}} and run bootwright secret set --name a --context <context> --value-file <path>",
		},
		{Severity: "error", Code: "api.field", Field: "$.spec.other", Message: "other", Remediation: "keep --context <context> here"},
	}}
}

type currentContext struct {
	ContextService
	name  string
	err   error
	calls int
}

func (c *currentContext) Current(context.Context, contexts.CurrentRequest) (*contexts.CurrentResult, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return &contexts.CurrentResult{Context: contexts.Summary{Name: c.name}}, nil
}

// The retired file source remedy names the context the validation read: the
// one named, or the selected one when it reads no file; a validation of files
// reads no context and keeps the placeholder, as does a selection that cannot
// be resolved (D66).
func TestValidateNamesTheContextInTheRetiredFileSourceRemedy(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		current *currentContext
		want    string
		asked   int
	}{
		{"a named context", []string{"validate", "--context", "beta"}, &currentContext{name: "lab"}, "--context beta --value-file", 0},
		{"the selected context", []string{"validate"}, &currentContext{name: "lab"}, "--context lab --value-file", 1},
		{"input files", []string{"validate", "-f", "x"}, &currentContext{name: "lab"}, "--context <context> --value-file", 0},
		{"an unresolved selection", []string{"validate"}, &currentContext{err: errors.New("no current context")}, "--context <context> --value-file", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &retiredFileSourceState{}
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: Services{DesiredState: state, Contexts: test.current}}).Run(context.Background(), test.args)
			if code != 1 || len(state.requests) != 1 {
				t.Fatalf("exit = %d, validations = %d, stderr %q", code, len(state.requests), errOut.String())
			}
			stderr := errOut.String()
			if !strings.Contains(stderr, "bootwright secret set --name a "+test.want) || !strings.Contains(stderr, "keep --context <context> here") {
				t.Fatalf("stderr = %q, want a remedy naming %q", stderr, test.want)
			}
			if test.current.calls != test.asked {
				t.Fatalf("current context read %d times, want %d", test.current.calls, test.asked)
			}
		})
	}
}
