package compilation

import (
	"context"
	"errors"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

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

type Service struct {
	reader   SourceReader
	compiler InputCompiler
	inputs   ContextInputs
}

// New composes input capabilities without opening or inspecting any input.
// The optional reader enables named and current context input lookup.
func New(reader SourceReader, compiler InputCompiler, inputs ...ContextInputs) Service {
	service := Service{reader: reader, compiler: compiler}
	if len(inputs) > 0 {
		service.inputs = inputs[0]
	}
	return service
}

type ValidateRequest struct {
	ContextName string
	Files       []string
}

func (s Service) Validate(ctx context.Context, request ValidateRequest) (*Report, error) {
	_, report, err := s.compile(ctx, request.ContextName, request.Files)
	return report, err
}

type EffectiveRequest struct {
	ContextName string
}

type EffectiveResult struct {
	Counts    Counts
	Effective api.Catalog
}

func (s Service) RenderEffective(ctx context.Context, request EffectiveRequest) (*EffectiveResult, error) {
	state, report, err := s.compile(ctx, request.ContextName, nil)
	if err != nil {
		return nil, err
	}
	// Inspection has no successful warning channel. Validation retains its
	// complete report, while rendering exposes only canonical state and counts.
	return &EffectiveResult{Counts: report.Counts, Effective: state.Effective()}, nil
}

func (s Service) compile(ctx context.Context, contextName string, files []string) (*State, *Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(files) == 0 && s.inputs == nil {
		return nil, nil, availability.ErrNotImplemented
	}
	if s.compiler == nil || len(files) > 0 && s.reader == nil {
		return nil, nil, errors.New("desired-state compilation is not configured")
	}
	var sources desiredstate.Sources
	var err error
	if len(files) > 0 {
		sources, err = s.reader.Read(ctx, slices.Clone(files))
	} else {
		sources, err = s.inputs.ReadInputs(ctx, contextName)
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, nil, canceled
	}
	if err != nil {
		return nil, nil, err
	}
	// SourceFile already owns its bytes. Copy the surrounding collections so
	// compiler calls cannot change the reader's frozen input view.
	sources.Files = slices.Clone(sources.Files)
	sources.Markers = slices.Clone(sources.Markers)
	sources.Roots = slices.Clone(sources.Roots)
	state, report, err := s.compiler.Compile(ctx, sources)
	if canceled := ctx.Err(); canceled != nil {
		return nil, nil, canceled
	}
	if err != nil {
		return nil, nil, err
	}
	if state == nil || report == nil {
		return nil, nil, errors.New("desired-state compilation returned no result")
	}
	return state, report, nil
}
