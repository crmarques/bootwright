package rendering

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type compilerFake struct {
	calls   int
	request compilation.EffectiveRequest
	result  *compilation.EffectiveResult
	err     error
}

func (f *compilerFake) RenderEffective(_ context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

type published struct {
	directory, name string
	data            []byte
	private         bool
}

type filesFake struct {
	resolved   map[string]string
	writes     []published
	publishErr error
}

func (f *filesFake) Resolve(path string) (string, error) {
	if resolved, found := f.resolved[path]; found {
		return resolved, nil
	}
	return "/work/" + path, nil
}

func (f *filesFake) Publish(_ context.Context, directory, name string, data []byte, private bool) (string, error) {
	if f.publishErr != nil {
		return "", f.publishErr
	}
	f.writes = append(f.writes, published{directory, name, append([]byte(nil), data...), private})
	return directory + "/" + name, nil
}

func fixture() (Service, *compilerFake, *filesFake) {
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.OpenShiftCluster, "sno", api.Value{}, api.Value{}), api.NewObject(api.CephCluster, "store", api.Value{}, api.Value{})})
	compiler := &compilerFake{result: &compilation.EffectiveResult{Effective: catalog}}
	files := &filesFake{}
	encode := func(_ context.Context, catalog api.Catalog) ([]byte, error) {
		return []byte("objects: " + string(rune('0'+len(catalog.Objects()))) + "\n"), nil
	}
	return Service{Compiler: compiler, Encode: encode, Files: files}, compiler, files
}

func TestRenderWritesTheEffectiveStateInsteadOfPrintingIt(t *testing.T) {
	service, compiler, files := fixture()
	result, err := service.Render(context.Background(), RenderRequest{InputPath: "inputs", OutputDirectory: "artifacts"})
	if err != nil {
		t.Fatal(err)
	}
	want := &Result{InputPath: "/work/inputs", OutputDirectory: "/work/artifacts", EffectiveStatePath: "/work/artifacts/effective-state.yaml", Objects: 2}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result %+v, want %+v", result, want)
	}
	if !reflect.DeepEqual(files.writes, []published{{"/work/artifacts", EffectiveStateFile, []byte("objects: 2\n"), false}}) {
		t.Fatalf("writes %+v", files.writes)
	}
	if want := (compilation.EffectiveRequest{Files: []string{"inputs"}}); !reflect.DeepEqual(compiler.request, want) {
		t.Fatalf("a context-free render reads its input and no context: %+v", compiler.request)
	}
}

func TestRenderReadsTheSelectedContextAndPublishesPrivatelyWhenSensitive(t *testing.T) {
	service, compiler, files := fixture()
	result, err := service.Render(context.Background(), RenderRequest{ContextName: "lab", OutputDirectory: "artifacts", Sensitive: true})
	if err != nil || result.InputPath != "" || len(files.writes) != 1 || !files.writes[0].private {
		t.Fatalf("result %+v, error %v, writes %+v", result, err, files.writes)
	}
	if want := (compilation.EffectiveRequest{ContextName: "lab"}); !reflect.DeepEqual(compiler.request, want) {
		t.Fatalf("request %+v", compiler.request)
	}
}

func TestRenderRefusesAnOutputInsideTheInput(t *testing.T) {
	for _, row := range []struct {
		name, input, output string
		refused             bool
	}{
		{"the input itself", "/work/in", "/work/in", true},
		{"below the input", "/work/in", "/work/in/out", true},
		{"far below the input", "/work/in", "/work/in/a/b", true},
		{"a sibling with the same prefix", "/work/in", "/work/in2", false},
		{"beside an input file", "/work/in.yaml", "/work/out", false},
		{"below an input file path", "/work/in.yaml", "/work/in.yaml/out", true},
		{"the parent of the input", "/work/in", "/work", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			service, compiler, files := fixture()
			files.resolved = map[string]string{"input": row.input, "output": row.output}
			_, err := service.Render(context.Background(), RenderRequest{InputPath: "input", OutputDirectory: "output"})
			found := diagnostics.Of(err)
			if row.refused {
				if len(found) != 1 || found[0].Code != "render.publish" || found[0].Remediation == "" || compiler.calls != 0 || len(files.writes) != 0 {
					t.Fatalf("error %#v, compiled %d, writes %d", err, compiler.calls, len(files.writes))
				}
			} else if err != nil || len(files.writes) != 1 {
				t.Fatalf("error %v, writes %d", err, len(files.writes))
			}
		})
	}
}

func TestRenderRefusesAnInputThatIsTheFileItWouldWrite(t *testing.T) {
	service, compiler, files := fixture()
	files.resolved = map[string]string{"input": "/work/out/" + EffectiveStateFile, "output": "/work/out"}
	_, err := service.Render(context.Background(), RenderRequest{InputPath: "input", OutputDirectory: "output"})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "render.publish" || compiler.calls != 0 || len(files.writes) != 0 {
		t.Fatalf("error %#v", err)
	}
	files.resolved["input"] = "/work/out/other.yaml"
	if _, err := service.Render(context.Background(), RenderRequest{InputPath: "input", OutputDirectory: "output"}); err != nil {
		t.Fatalf("another file in the output directory is refused: %v", err)
	}
}

func TestRenderWritesNothingWhenCompilationEncodingOrPublicationFails(t *testing.T) {
	boom := errors.New("compile stopped")
	service, compiler, files := fixture()
	compiler.result, compiler.err = nil, boom
	if _, err := service.Render(context.Background(), RenderRequest{OutputDirectory: "artifacts"}); !errors.Is(err, boom) || len(files.writes) != 0 {
		t.Fatalf("error %v, writes %d", err, len(files.writes))
	}
	for _, encoded := range [][]byte{nil, []byte("no final newline")} {
		service, _, files = fixture()
		service.Encode = func(context.Context, api.Catalog) ([]byte, error) { return encoded, nil }
		_, err := service.Render(context.Background(), RenderRequest{OutputDirectory: "artifacts"})
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "runtime.encode" || len(files.writes) != 0 {
			t.Fatalf("error %#v, writes %d", err, len(files.writes))
		}
	}
	service, _, files = fixture()
	service.Encode = func(context.Context, api.Catalog) ([]byte, error) { return []byte("x\n"), boom }
	if _, err := service.Render(context.Background(), RenderRequest{OutputDirectory: "artifacts"}); len(diagnostics.Of(err)) != 1 || len(files.writes) != 0 {
		t.Fatalf("error %v", err)
	}
	service, _, files = fixture()
	files.publishErr = boom
	if result, err := service.Render(context.Background(), RenderRequest{OutputDirectory: "artifacts"}); !errors.Is(err, boom) || result != nil {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestRenderIsUnavailableWithoutItsCollaboratorsAndStopsOnACancelledContext(t *testing.T) {
	for _, mutate := range []func(*Service){func(s *Service) { s.Compiler = nil }, func(s *Service) { s.Encode = nil }, func(s *Service) { s.Files = nil }} {
		service, _, _ := fixture()
		mutate(&service)
		if _, err := service.Render(context.Background(), RenderRequest{OutputDirectory: "artifacts"}); !errors.Is(err, availability.ErrNotImplemented) {
			t.Fatalf("error %v", err)
		}
	}
	service, compiler, files := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Render(ctx, RenderRequest{OutputDirectory: "artifacts"}); !errors.Is(err, context.Canceled) || compiler.calls != 0 || len(files.writes) != 0 {
		t.Fatalf("error %v", err)
	}
}

func TestRenderRefusesWithoutAnOutputDirectory(t *testing.T) {
	service, compiler, _ := fixture()
	_, err := service.Render(context.Background(), RenderRequest{})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "cli.usage" || !strings.Contains(found[0].Remediation, "--output-dir") || compiler.calls != 0 {
		t.Fatalf("error %#v", err)
	}
}
