package compilation

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type SyntaxParser interface {
	Parse(context.Context, []desiredstate.SourceFile) ([]desiredstate.Document, []diagnostics.Diagnostic, error)
}

type SourceReader interface {
	Read(context.Context, []string) (desiredstate.Sources, error)
}

type InputCompiler interface {
	Compile(context.Context, desiredstate.Sources) (*State, *Report, error)
}

// ContextInputs returns the selected immutable acquisition with its original
// logical paths. Reading an input view grants no context mutation authority.
type ContextInputs interface {
	ReadInputs(context.Context, string) (desiredstate.Sources, error)
}
