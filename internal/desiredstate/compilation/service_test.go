package compilation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type sourceReaderFunc func(context.Context, []string) (desiredstate.Sources, error)

func (f sourceReaderFunc) Read(ctx context.Context, paths []string) (desiredstate.Sources, error) {
	return f(ctx, paths)
}

type inputCompilerFunc func(context.Context, desiredstate.Sources) (*State, *Report, error)

func (f inputCompilerFunc) Compile(ctx context.Context, sources desiredstate.Sources) (*State, *Report, error) {
	return f(ctx, sources)
}

func TestServiceAvailabilityPrecedesAnyInputEffects(t *testing.T) {
	reader := sourceReaderFunc(func(context.Context, []string) (desiredstate.Sources, error) {
		t.Fatal("unavailable or canceled call read input")
		return desiredstate.Sources{}, nil
	})
	compiler := inputCompilerFunc(func(context.Context, desiredstate.Sources) (*State, *Report, error) {
		t.Fatal("unavailable or canceled call compiled input")
		return nil, nil, nil
	})
	service := New(reader, compiler)
	if report, err := service.Validate(context.Background(), ValidateRequest{ContextName: "absent"}); report != nil || !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatalf("context-only validation = %#v, %v", report, err)
	}
	if result, err := service.RenderEffective(context.Background(), EffectiveRequest{ContextName: "absent"}); result != nil || !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, request := range []ValidateRequest{{ContextName: "absent"}, {Files: []string{"unopened"}}} {
		if report, err := service.Validate(ctx, request); report != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled validation = %#v, %v", report, err)
		}
	}
}

func TestServiceReadsExplicitFilesAndReturnsOnlyCompleteReports(t *testing.T) {
	want := &Report{Counts: Counts{FilesSeen: 1, ObjectsDecoded: 1}}
	paths := []string{"input.yaml"}
	ctx := context.Background()
	calls := []string{}
	service := New(sourceReaderFunc(func(got context.Context, files []string) (desiredstate.Sources, error) {
		calls = append(calls, "read")
		if got != ctx || !reflect.DeepEqual(files, paths) {
			t.Fatal("input request changed")
		}
		files[0] = "reader-owned"
		return desiredstate.Sources{Roots: []string{"read-result"}}, nil
	}), inputCompilerFunc(func(got context.Context, sources desiredstate.Sources) (*State, *Report, error) {
		calls = append(calls, "compile")
		if got != ctx || !reflect.DeepEqual(sources.Roots, []string{"read-result"}) {
			t.Fatal("compiler did not receive reader result")
		}
		return &State{}, want, nil
	}))
	report, err := service.Validate(ctx, ValidateRequest{ContextName: "ignored", Files: paths})
	if err != nil || report != want || !reflect.DeepEqual(calls, []string{"read", "compile"}) || paths[0] != "input.yaml" {
		t.Fatalf("result=%#v err=%v calls=%v paths=%v", report, err, calls, paths)
	}
}

func TestServiceFailuresHideReportsAndStopLaterStages(t *testing.T) {
	for _, stage := range []string{"read", "compile", "empty-state", "empty-report", "canceled-read", "canceled-compile"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := diagnostics.NewFailure("input.read", "unreadable input", "input.yaml")
			compiled := 0
			service := New(sourceReaderFunc(func(context.Context, []string) (desiredstate.Sources, error) {
				if stage == "canceled-read" {
					cancel()
				}
				if stage == "read" {
					return desiredstate.Sources{}, failure
				}
				return desiredstate.Sources{}, nil
			}), inputCompilerFunc(func(context.Context, desiredstate.Sources) (*State, *Report, error) {
				compiled++
				if stage == "canceled-compile" {
					cancel()
				}
				if stage == "compile" {
					return &State{}, &Report{}, failure
				}
				if stage == "empty-state" {
					return nil, &Report{}, nil
				}
				if stage == "empty-report" {
					return &State{}, nil, nil
				}
				return &State{}, &Report{}, nil
			}))
			report, err := service.Validate(ctx, ValidateRequest{Files: []string{"input.yaml"}})
			if report != nil || err == nil {
				t.Fatalf("failure leaked success: %#v, %v", report, err)
			}
			if (stage == "read" || stage == "canceled-read") && compiled != 0 {
				t.Fatal("read failure reached compiler")
			}
			if (stage == "read" || stage == "compile") && !errors.Is(err, failure) {
				t.Fatal("typed failure lost", err)
			}
			if (stage == "canceled-read" || stage == "canceled-compile") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
	if report, err := (Service{}).Validate(context.Background(), ValidateRequest{Files: []string{"input.yaml"}}); report != nil || err == nil {
		t.Fatal("missing dependencies succeeded")
	}
}

type contextInputsFunc func(context.Context, string) (desiredstate.Sources, error)

func (f contextInputsFunc) ReadInputs(ctx context.Context, name string) (desiredstate.Sources, error) {
	return f(ctx, name)
}

func TestServiceExplicitFilesNeverResolveContext(t *testing.T) {
	reads := 0
	service := New(sourceReaderFunc(func(context.Context, []string) (desiredstate.Sources, error) {
		reads++
		return desiredstate.Sources{}, nil
	}), inputCompilerFunc(func(context.Context, desiredstate.Sources) (*State, *Report, error) {
		return &State{}, &Report{}, nil
	}), contextInputsFunc(func(context.Context, string) (desiredstate.Sources, error) {
		t.Fatal("explicit input resolved a context")
		return desiredstate.Sources{}, nil
	}))
	if reads != 0 {
		t.Fatal("construction acquired input")
	}
	if _, err := service.Validate(context.Background(), ValidateRequest{ContextName: "unopened", Files: []string{"declared.yaml"}}); err != nil || reads != 1 {
		t.Fatalf("explicit validation: reads=%d err=%v", reads, err)
	}
}

func TestServiceContextInputsStayImmutableAcrossCompilerCalls(t *testing.T) {
	input := desiredstate.Sources{
		Roots:   []string{"/synthetic/original"},
		Files:   []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/original/environment.yaml", []byte("authored"))},
		Markers: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/original/add-ons/_store/demo/.bootwright-addon", []byte("demo\n"))},
	}
	var names []string
	view := contextInputsFunc(func(_ context.Context, name string) (desiredstate.Sources, error) {
		names = append(names, name)
		return input, nil
	})
	compiler := inputCompilerFunc(func(_ context.Context, got desiredstate.Sources) (*State, *Report, error) {
		if got.Roots[0] != input.Roots[0] || string(got.Files[0].Bytes()) != "authored" || string(got.Markers[0].Bytes()) != "demo\n" {
			t.Fatal("a prior call changed the context input view")
		}
		got.Files[0].Bytes()[0] = '!'
		got.Markers[0].Bytes()[0] = '!'
		got.Roots[0] = "changed-root"
		got.Files[0] = desiredstate.NewSourceFile("changed-file", nil)
		got.Markers[0] = desiredstate.NewSourceFile("changed-marker", nil)
		return &State{}, &Report{Counts: Counts{FilesSeen: 1, ObjectsDecoded: 1}}, nil
	})
	service := New(nil, compiler, view)
	if len(names) != 0 {
		t.Fatal("construction resolved a context")
	}
	if _, err := service.Validate(context.Background(), ValidateRequest{ContextName: "named"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenderEffective(context.Background(), EffectiveRequest{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"named", ""}) || input.Roots[0] != "/synthetic/original" || input.Files[0].Path() != "/synthetic/original/environment.yaml" || input.Markers[0].Path() != "/synthetic/original/add-ons/_store/demo/.bootwright-addon" {
		t.Fatal("lookup selection or frozen collections changed")
	}
}

func TestServiceContextFailuresAndCancellationNeverReturnPartialResults(t *testing.T) {
	for _, operation := range []string{"validate", "render"} {
		for _, stage := range []string{"canceled", "read", "canceled-read", "compile", "canceled-compile", "empty-state", "empty-report", "missing-compiler"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				failure := diagnostics.NewFailure("context.state", "immutable input cannot be read", "")
				reads, compiles := 0, 0
				inputs := contextInputsFunc(func(got context.Context, _ string) (desiredstate.Sources, error) {
					reads++
					if got != ctx {
						t.Fatal("lookup context changed")
					}
					if stage == "canceled-read" {
						cancel()
					}
					if stage == "read" {
						return desiredstate.Sources{}, failure
					}
					return desiredstate.Sources{}, nil
				})
				var compiler InputCompiler = inputCompilerFunc(func(got context.Context, _ desiredstate.Sources) (*State, *Report, error) {
					compiles++
					if got != ctx {
						t.Fatal("compiler context changed")
					}
					if stage == "canceled-compile" {
						cancel()
					}
					if stage == "compile" {
						return &State{}, &Report{}, failure
					}
					if stage == "empty-state" {
						return nil, &Report{}, nil
					}
					if stage == "empty-report" {
						return &State{}, nil, nil
					}
					return &State{}, &Report{}, nil
				})
				if stage == "canceled" {
					cancel()
				}
				if stage == "missing-compiler" {
					compiler = nil
				}
				service := New(nil, compiler, inputs)
				var err error
				if operation == "validate" {
					var report *Report
					report, err = service.Validate(ctx, ValidateRequest{})
					if report != nil {
						t.Fatal("failed validation returned a report")
					}
				} else {
					var result *EffectiveResult
					result, err = service.RenderEffective(ctx, EffectiveRequest{})
					if result != nil {
						t.Fatal("failed rendering returned effective state")
					}
				}
				if err == nil {
					t.Fatal("failed stage succeeded")
				}
				if (stage == "read" || stage == "compile") && !errors.Is(err, failure) {
					t.Fatal("typed failure was replaced", err)
				}
				if (stage == "canceled" || stage == "canceled-read" || stage == "canceled-compile") && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation was replaced", err)
				}
				if (stage == "canceled" || stage == "missing-compiler") && reads != 0 || (stage == "canceled" || stage == "read" || stage == "canceled-read" || stage == "missing-compiler") && compiles != 0 {
					t.Fatal("failure reached a later effect boundary")
				}
			})
		}
	}
}
