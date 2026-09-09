package contexts_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const pristineEvidence = `{"version":1,"operation":"none","ownership":"none"}`

const environmentInput = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example

spec:
  domains:
    base: example.test
`

type repository struct {
	t              *testing.T
	registry       contexts.Registry
	selection      contexts.Selection
	configurations map[string][]byte
	configInput    []byte
	calls          []string
	locked         bool
	leased         bool
	create         bool
	roots          []string
	evidence       map[string][]byte
	published      map[string]desiredstate.Sources
	revisions      int
	failure        string
	failureErr     error
	cancelAt       string
	cancel         context.CancelFunc
	emptyCallback  bool
}

func newRepository(t *testing.T) *repository {
	return &repository{t: t, registry: contexts.Registry{Version: 1, Contexts: []contexts.Record{}, Identities: []contexts.Identity{}}, configurations: map[string][]byte{}, evidence: map[string][]byte{}, published: map[string]desiredstate.Sources{}, failureErr: contexts.StateError("synthetic repository failure")}
}

func cloneRegistry(reg contexts.Registry) contexts.Registry {
	reg.Contexts = slices.Clone(reg.Contexts)
	reg.Identities = slices.Clone(reg.Identities)
	return reg
}

func copySources(input desiredstate.Sources) desiredstate.Sources {
	return desiredstate.Sources{Roots: slices.Clone(input.Roots), Files: slices.Clone(input.Files), Markers: slices.Clone(input.Markers)}
}

func (r *repository) step(ctx context.Context, stage string) error {
	r.t.Helper()
	r.calls = append(r.calls, stage)
	if stage == r.cancelAt {
		r.cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if stage == r.failure {
		return r.failureErr
	}
	return nil
}

func (r *repository) CheckInputDirectory(ctx context.Context, path string) error {
	if r.locked || path == "" {
		r.t.Fatal("invalid state-root preflight")
	}
	return r.step(ctx, "preflight")
}

func (r *repository) ReadInputs(context.Context, string, string) (desiredstate.Sources, error) {
	r.t.Fatal("context management used the inspection input port")
	return desiredstate.Sources{}, nil
}

func (r *repository) View(ctx context.Context) (contexts.Registry, error) {
	if r.locked {
		r.t.Fatal("read-only view was called inside a mutation")
	}
	if err := r.step(ctx, "view"); err != nil {
		return contexts.Registry{}, err
	}
	return cloneRegistry(r.registry), nil
}

func (r *repository) Transact(ctx context.Context, create bool, roots []string, fn func(contexts.Transaction) error) error {
	if r.locked {
		r.t.Fatal("nested transaction")
	}
	if err := r.step(ctx, "transaction"); err != nil {
		return err
	}
	r.create, r.roots = create, slices.Clone(roots)
	if r.emptyCallback {
		return nil
	}
	r.locked = true
	defer func() { r.locked, r.leased = false, false }()
	return fn(transaction{r})
}

type transaction struct{ r *repository }

func (tx transaction) requireLock() {
	tx.r.t.Helper()
	if !tx.r.locked {
		tx.r.t.Fatal("transaction operation escaped the root lock")
	}
}

func (tx transaction) Registry() contexts.Registry {
	tx.requireLock()
	tx.r.calls = append(tx.r.calls, "registry")
	return cloneRegistry(tx.r.registry)
}

func (tx transaction) Reserve(ctx context.Context, name, directory string, data []byte) (contexts.Record, error) {
	tx.requireLock()
	if err := tx.r.step(ctx, "reserve"); err != nil {
		return contexts.Record{}, err
	}
	for _, record := range tx.r.registry.Contexts {
		if record.Name == name {
			if record.Mode != contexts.Initializing || record.EnvironmentDirectory != directory || !reflect.DeepEqual(tx.r.configurations[record.ID], data) {
				return contexts.Record{}, contexts.StateError("pending init differs")
			}
			return record, nil
		}
	}
	id := fmt.Sprintf("ctx-%032x", len(tx.r.registry.Identities)+1)
	config, err := contexts.ParseConfiguration(name, data)
	if err != nil {
		return contexts.Record{}, err
	}
	record := contexts.Record{Name: name, ID: id, Mode: contexts.Initializing, EnvironmentDirectory: directory, SecretStoreType: config.SecretStore.Type}
	tx.r.evidence[id] = []byte(pristineEvidence)
	tx.r.configurations[id] = slices.Clone(data)
	tx.r.registry.Identities = append(tx.r.registry.Identities, contexts.Identity{ID: id})
	tx.r.registry.Contexts = append(tx.r.registry.Contexts, record)
	return record, nil
}

func (tx transaction) Configuration(ctx context.Context, id string) ([]byte, error) {
	tx.requireLock()
	if err := tx.r.step(ctx, "configuration"); err != nil {
		return nil, err
	}
	return slices.Clone(tx.r.configurations[id]), nil
}

func (tx transaction) InitializeSecrets(ctx context.Context, id string, callback func(storage.Area) error) error {
	tx.requireLock()
	tx.r.leased = true
	if err := tx.r.step(ctx, "initialize"); err != nil {
		return err
	}
	return callback(nil)
}

func (tx transaction) MutationState(ctx context.Context, id string) ([]byte, error) {
	tx.requireLock()
	if err := tx.r.step(ctx, "lease"); err != nil {
		return nil, err
	}
	tx.r.leased = true
	return slices.Clone(tx.r.evidence[id]), nil
}

func (tx transaction) Publish(ctx context.Context, id, directory string, input desiredstate.Sources) (string, error) {
	tx.requireLock()
	if !tx.r.leased {
		tx.r.t.Fatal("publication escaped the context lease")
	}
	if err := tx.r.step(ctx, "publish"); err != nil {
		return "", err
	}
	if directory == "" || id == "" {
		tx.r.t.Fatal("publication omitted identity")
	}
	tx.r.revisions++
	revision := fmt.Sprintf("rev-%032x", tx.r.revisions)
	tx.r.published[revision] = copySources(input)
	return revision, nil
}

func (tx transaction) Delete(ctx context.Context, record contexts.Record) error {
	tx.requireLock()
	if err := tx.r.step(ctx, "delete"); err != nil {
		return err
	}
	tx.r.registry.Contexts = slices.DeleteFunc(tx.r.registry.Contexts, func(r contexts.Record) bool { return r.ID == record.ID })
	delete(tx.r.configurations, record.ID)
	return nil
}

func (tx transaction) Commit(ctx context.Context, reg contexts.Registry) error {
	tx.requireLock()
	if err := tx.r.step(ctx, "commit"); err != nil {
		return err
	}
	tx.r.registry = cloneRegistry(reg)
	return nil
}

type directoryReader func(context.Context, string) (desiredstate.Sources, error)

func (f directoryReader) ReadDirectory(ctx context.Context, path string) (desiredstate.Sources, error) {
	return f(ctx, path)
}

type compilerFunc func(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)

func (f compilerFunc) Compile(ctx context.Context, sources desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return f(ctx, sources)
}

type guard struct{ r *repository }

func (g guard) Check(ctx context.Context, data []byte) (contexts.Disposition, error) {
	if !g.r.locked || !g.r.leased {
		g.r.t.Fatal("mutation guard ran outside root lock and context lease")
	}
	if err := g.r.step(ctx, "guard"); err != nil {
		return contexts.Disposition{}, err
	}
	return (contextguard.Guard{}).Check(ctx, data)
}

type confirmer struct{ r *repository }

func (c confirmer) Confirm(ctx context.Context, action, name string) error {
	if !c.r.locked || !c.r.leased || !slices.Contains(c.r.calls, "guard") {
		c.r.t.Fatal("confirmation preceded locked mutation safeguards")
	}
	if name == "" || action != "update" && action != "delete" {
		c.r.t.Fatal("unexpected confirmation request", action, name)
	}
	return c.r.step(ctx, "confirm")
}

func sourceFixture(directory string) desiredstate.Sources {
	return desiredstate.Sources{Roots: []string{directory}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(directory, "environment.yaml"), []byte(environmentInput))}}
}

func service(t *testing.T, r *repository, input desiredstate.Sources) contexts.Service {
	t.Helper()
	realCompiler := compilation.NewCompiler(yamlstream.Parser{}, nil,
		compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate})
	reader := directoryReader(func(ctx context.Context, path string) (desiredstate.Sources, error) {
		if r.locked || slices.Contains(r.calls, "transaction") || slices.Contains(r.calls, "view") || !slices.Contains(r.calls, "preflight") {
			t.Fatal("input acquisition must follow read-only preflight and precede repository access")
		}
		if len(input.Roots) == 1 && path != input.Roots[0] {
			t.Fatal("directory request changed", path)
		}
		if err := r.step(ctx, "read"); err != nil {
			return desiredstate.Sources{}, err
		}
		return copySources(input), nil
	})
	compiler := compilerFunc(func(ctx context.Context, sources desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
		if r.locked || slices.Contains(r.calls, "transaction") || slices.Contains(r.calls, "view") {
			t.Fatal("compilation followed repository access")
		}
		if err := r.step(ctx, "compile"); err != nil {
			return nil, nil, err
		}
		return realCompiler.Compile(ctx, sources)
	})
	return contexts.New(reader, compiler, r, guard{r}, confirmer{r}, options(r))
}

func existingRepository(t *testing.T) *repository {
	r := newRepository(t)
	id := "ctx-00000000000000000000000000000001"
	r.registry = contexts.Registry{Version: 1, Identities: []contexts.Identity{{ID: id}}, Contexts: []contexts.Record{{Name: "example", ID: id, EnvironmentDirectory: "/synthetic/input", Revision: "rev-00000000000000000000000000000001", Mode: contexts.Ready}}}
	r.evidence[id] = []byte(pristineEvidence)
	r.selection = contexts.Selection{Version: 1, Name: "example", ID: id}
	r.configurations[id] = contexts.DefaultConfiguration("example").Canonical()
	r.revisions = 1
	return r
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	for _, diagnostic := range desiredstate.DiagnosticsOf(err) {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("wanted %s, got %v: %#v", code, err, desiredstate.DiagnosticsOf(err))
}

func TestInitCompilesBeforeTransactionAndPublishesOriginalAcquisition(t *testing.T) {
	r := newRepository(t)
	input := sourceFixture("/synthetic/input")
	env := environmentInput + `
  resources:
    - declared.yaml

  defaults:
    Secret:
      type: opaque
      source:
        file:
          path: secrets/unopened
`
	input.Files[0] = desiredstate.NewSourceFile(input.Files[0].Path(), []byte(env))
	for _, item := range []struct{ file, name, spec string }{{"declared.yaml", "material", " {}"}, {"excluded.yaml", "excluded", "\n  type: unsupported"}} {
		content := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + item.name + "\n\nspec:" + item.spec + "\n"
		input.Files = append(input.Files, desiredstate.NewSourceFile(filepath.Join(input.Roots[0], item.file), []byte(content)))
	}
	input.Markers = []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/input/add-ons/_store/example/.bootwright-addon", []byte("example\n"))}
	got, err := service(t, r, input).Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input"})
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	if got.Counts != (compilation.Counts{FilesSeen: 3, ObjectsDecoded: 2}) || got.FilesCopied != 4 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Source.Path != "/synthetic/input/excluded.yaml" {
		t.Fatalf("admission result lost counts or warnings: %#v", got)
	}
	if !got.Context.Current || got.Context.Mode != contexts.Ready || got.Context.ID == "" || !r.create || !reflect.DeepEqual(r.roots, []string{"/synthetic/input"}) {
		t.Fatal("publication omitted identity, selection or forbidden roots")
	}
	if !reflect.DeepEqual(r.calls, []string{"preflight", "read", "compile", "transaction", "registry", "reserve", "initialize", "publish", "registry", "commit", "select"}) {
		t.Fatal("init order", r.calls)
	}
	published := r.published[r.registry.Contexts[0].Revision]
	if !reflect.DeepEqual(published, input) || strings.Contains(string(published.Files[1].Bytes()), "source:") {
		t.Fatal("publication changed authored bytes or omitted excluded acquisition")
	}
}

func TestInvalidAdmissionAndNamesNeverStartTransaction(t *testing.T) {
	for _, operation := range []string{"init", "update"} {
		for _, invalid := range []string{"name", "syntax", "duplicate-environment", "preflight", "read", "compile", "canceled-preflight", "canceled-read", "canceled-compile", "empty-roots"} {
			t.Run(operation+"/"+invalid, func(t *testing.T) {
				r := newRepository(t)
				input := sourceFixture("/synthetic/input")
				name := "example"
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch invalid {
				case "name":
					name = "../invalid"
				case "syntax":
					input.Files[0] = desiredstate.NewSourceFile(input.Files[0].Path(), []byte("["))
				case "duplicate-environment":
					input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/input/duplicate.yaml", []byte(environmentInput)))
				case "preflight", "read", "compile":
					r.failure = invalid
				case "canceled-preflight", "canceled-read", "canceled-compile":
					r.cancelAt, r.cancel = strings.TrimPrefix(invalid, "canceled-"), cancel
				case "empty-roots":
					input.Roots = nil
				}
				s := service(t, r, input)
				var got *contexts.AdmissionResult
				var err error
				if operation == "init" {
					got, err = s.Init(ctx, contexts.InitRequest{Name: name, InputDirectory: "/synthetic/input"})
				} else {
					got, err = s.Update(ctx, contexts.UpdateRequest{Name: name, InputDirectory: "/synthetic/input"})
				}
				if got != nil || err == nil || slices.Contains(r.calls, "transaction") || slices.Contains(r.calls, "view") {
					t.Fatalf("invalid admission reached storage: %#v %v %v", got, err, r.calls)
				}
				if invalid == "name" && len(r.calls) != 0 || invalid == "read" && slices.Contains(r.calls, "compile") {
					t.Fatal("earlier failure reached a later stage", r.calls)
				}
				if strings.HasPrefix(invalid, "canceled-") && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation was lost", err)
				}
			})
		}
	}
}

func TestUpdatePreservesIdentityAndSelectionAndConfirmsUnderLease(t *testing.T) {
	r := existingRepository(t)
	r.selection = contexts.Selection{Version: 1, Name: "other", ID: "ctx-00000000000000000000000000000002"}
	r.registry.Contexts = append(r.registry.Contexts, contexts.Record{Name: "other", ID: "ctx-00000000000000000000000000000002", EnvironmentDirectory: "/synthetic/other", Revision: "rev-00000000000000000000000000000002", Mode: contexts.Ready})
	r.registry.Identities = append(r.registry.Identities, contexts.Identity{ID: "ctx-00000000000000000000000000000002"})
	before := cloneRegistry(r.registry)
	got, err := service(t, r, sourceFixture("/synthetic/input")).Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input"})
	if err != nil || got.Context.ID != before.Contexts[0].ID || got.Context.Current || r.selection.Name != "other" || r.registry.Contexts[0].Revision == before.Contexts[0].Revision || !reflect.DeepEqual(r.registry.Contexts[1], before.Contexts[1]) || !reflect.DeepEqual(r.registry.Identities, before.Identities) || r.create {
		t.Fatalf("update changed unrelated state: %#v %v", got, err)
	}
	if !reflect.DeepEqual(r.calls, []string{"preflight", "read", "compile", "transaction", "registry", "lease", "guard", "confirm", "publish", "commit"}) {
		t.Fatal("update order", r.calls)
	}
}

func TestProtectedStatesRefuseRecreationAndUnsafeUpdateOrDelete(t *testing.T) {
	for _, operation := range []string{"pending", "failed", "unknown", "applied", "none"} {
		for _, ownership := range []string{"none", "retained"} {
			if operation == "none" && ownership == "none" {
				continue
			}
			for _, command := range []string{"init", "update", "delete"} {
				t.Run(operation+"/"+ownership+"/"+command, func(t *testing.T) {
					r := existingRepository(t)
					before := cloneRegistry(r.registry)
					id := r.registry.Contexts[0].ID
					data := []byte(`{"version":1,"operation":"` + operation + `","ownership":"` + ownership + `"}`)
					r.evidence[id] = data
					s := service(t, r, sourceFixture("/synthetic/input"))
					var succeeded bool
					var err error
					switch command {
					case "init":
						result, failure := s.Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input"})
						succeeded, err = result != nil, failure
					case "update":
						result, failure := s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
						succeeded, err = result != nil, failure
					case "delete":
						result, failure := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: true})
						succeeded, err = result != nil, failure
					}
					allowed := command == "update" && (operation == "none" || operation == "applied")
					if allowed {
						if !succeeded || err != nil || r.registry.Contexts[0].ID != id || !reflect.DeepEqual(r.evidence[id], data) {
							t.Fatal("allowed update changed identity or mutation evidence", err)
						}
					} else if succeeded || err == nil || !reflect.DeepEqual(r.registry, before) || slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "delete") || slices.Contains(r.calls, "commit") {
						t.Fatal("protected state reached mutation or confirmation", err, r.calls)
					}
				})
			}
		}
	}
}

func TestMissingCorruptAndLiveLeaseEvidenceCannotBeOverridden(t *testing.T) {
	for _, failure := range []string{"missing", "corrupt", "unsupported", "lease"} {
		for _, command := range []string{"init", "update", "delete"} {
			t.Run(failure+"/"+command, func(t *testing.T) {
				r := existingRepository(t)
				id := r.registry.Contexts[0].ID
				switch failure {
				case "missing":
					delete(r.evidence, id)
				case "corrupt":
					r.evidence[id] = []byte("{")
				case "unsupported":
					r.evidence[id] = []byte(`{"version":1,"operation":"future","ownership":"none"}`)
				case "lease":
					r.failure = "lease"
				}
				s := service(t, r, sourceFixture("/synthetic/input"))
				var succeeded bool
				var err error
				switch command {
				case "init":
					got, failure := s.Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input"})
					succeeded, err = got != nil, failure
				case "update":
					got, failure := s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
					succeeded, err = got != nil, failure
				case "delete":
					got, failure := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: true})
					succeeded, err = got != nil, failure
				}
				if succeeded || err == nil || slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "delete") || slices.Contains(r.calls, "commit") {
					t.Fatal("override bypassed missing safety proof", err, r.calls)
				}
				if failure == "lease" && slices.Contains(r.calls, "guard") {
					t.Fatal("live lease failure reached the guard")
				}
			})
		}
	}
}

func invokeCommand(ctx context.Context, s contexts.Service, command string) (bool, error) {
	switch command {
	case "init":
		got, err := s.Init(ctx, contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input"})
		return got != nil, err
	case "update":
		got, err := s.Update(ctx, contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input"})
		return got != nil, err
	case "use":
		got, err := s.Use(ctx, contexts.UseRequest{Name: "example"})
		return got != nil, err
	case "list":
		got, err := s.List(ctx, contexts.ListRequest{})
		return got != nil, err
	case "current":
		got, err := s.Current(ctx, contexts.CurrentRequest{})
		return got != nil, err
	case "delete":
		got, err := s.Delete(ctx, contexts.DeleteRequest{Name: "example", Purge: true})
		return got != nil, err
	}
	panic("unknown test command")
}

func TestFailuresAndCancellationNeverClaimPublication(t *testing.T) {
	for _, command := range []string{"init", "update", "use", "list", "current", "delete"} {
		stages := map[string][]string{
			"init":    {"preflight", "read", "compile", "transaction", "reserve", "initialize", "publish", "commit"},
			"update":  {"preflight", "read", "compile", "transaction", "lease", "guard", "confirm", "publish", "commit"},
			"use":     {"transaction", "select"},
			"list":    {"view"},
			"current": {"view"},
			"delete":  {"transaction", "lease", "guard", "confirm", "delete"},
		}[command]
		for _, stage := range stages {
			for _, cancelStage := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/canceled=%t", command, stage, cancelStage), func(t *testing.T) {
					r := existingRepository(t)
					if command == "init" {
						r = newRepository(t)
					}
					before := cloneRegistry(r.registry)
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if cancelStage {
						r.cancelAt, r.cancel = stage, cancel
					} else {
						r.failure = stage
					}
					succeeded, err := invokeCommand(ctx, service(t, r, sourceFixture("/synthetic/input")), command)
					if succeeded || err == nil || (command != "init" && !reflect.DeepEqual(r.registry, before)) || r.locked || r.leased {
						t.Fatal("failed command claimed publication or retained locks", err, r.calls)
					}
					if cancelStage && !errors.Is(err, context.Canceled) || !cancelStage && !errors.Is(err, r.failureErr) {
						t.Fatal("failure identity changed", err)
					}
					if stage != "publish" && stage != "delete" && stage != "commit" && (slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "delete") || slices.Contains(r.calls, "commit")) {
						t.Fatal("earlier failure reached publication", r.calls)
					}
				})
			}
		}
	}
}

func TestCanceledAndUnresolvedCommandsHaveNoMutation(t *testing.T) {
	for _, command := range []string{"init", "update", "use", "list", "current", "delete"} {
		t.Run(command, func(t *testing.T) {
			r := existingRepository(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			got, err := invokeCommand(ctx, service(t, r, sourceFixture("/synthetic/input")), command)
			if got || !errors.Is(err, context.Canceled) || len(r.calls) != 0 {
				t.Fatal("pre-canceled command had effects", err, r.calls)
			}
		})
	}
	for _, command := range []string{"init", "update", "use", "delete"} {
		t.Run("empty-transaction/"+command, func(t *testing.T) {
			r := existingRepository(t)
			r.emptyCallback = true
			got, err := invokeCommand(context.Background(), service(t, r, sourceFixture("/synthetic/input")), command)
			if got || err == nil {
				t.Fatal("empty transaction result succeeded")
			}
			requireCode(t, err, "context.state")
		})
	}
	r := newRepository(t)
	s := service(t, r, sourceFixture("/synthetic/input"))
	listed, err := s.List(context.Background(), contexts.ListRequest{})
	if err != nil || listed.Contexts == nil || len(listed.Contexts) != 0 || !reflect.DeepEqual(r.calls, []string{"view"}) {
		t.Fatal("empty list did not remain read-only", err, r.calls)
	}
	if got, err := s.Current(context.Background(), contexts.CurrentRequest{}); got != nil || err == nil {
		t.Fatal("empty current selection succeeded")
	}
	r.calls = nil
	if got, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "missing"}); got != nil || err == nil || len(r.calls) != 0 {
		t.Fatal("delete without purge accessed repository", err, r.calls)
	}
	for _, command := range []string{"use", "delete"} {
		r.calls = nil
		got, err := invokeCommand(context.Background(), s, command)
		if got || err == nil || slices.Contains(r.calls, "lease") || slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "commit") {
			t.Fatal("unknown target reached mutation", command, err, r.calls)
		}
	}
}

func TestIncompleteCompilerResultsNeverStartTransaction(t *testing.T) {
	for _, missing := range []string{"state", "report", "environment"} {
		t.Run(missing, func(t *testing.T) {
			r := newRepository(t)
			reader := directoryReader(func(context.Context, string) (desiredstate.Sources, error) {
				return sourceFixture("/synthetic/input"), nil
			})
			compiler := compilerFunc(func(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
				if missing == "state" {
					return nil, &compilation.Report{}, nil
				}
				if missing == "report" {
					return &compilation.State{}, nil, nil
				}
				return &compilation.State{}, &compilation.Report{}, nil
			})
			s := contexts.New(reader, compiler, r, guard{r}, confirmer{r}, options(r))
			if got, err := s.Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input"}); got != nil || err == nil || !reflect.DeepEqual(r.calls, []string{"preflight"}) {
				t.Fatal("incomplete admission reached repository", got, err, r.calls)
			}
		})
	}
}

func TestMissingSafeguardsCannotBeReplacedByConfirmationFlags(t *testing.T) {
	for _, missing := range []string{"guard", "confirmer"} {
		for _, command := range []string{"update", "delete"} {
			t.Run(missing+"/"+command, func(t *testing.T) {
				r := existingRepository(t)
				input := sourceFixture("/synthetic/input")
				reader := directoryReader(func(context.Context, string) (desiredstate.Sources, error) { return input, nil })
				compiler := compilation.NewCompiler(yamlstream.Parser{}, nil)
				var mutationGuard contexts.ContextMutationGuard = guard{r}
				var confirmation contexts.Confirmer = confirmer{r}
				if missing == "guard" {
					mutationGuard = nil
				} else {
					confirmation = nil
				}
				s := contexts.New(reader, compiler, r, mutationGuard, confirmation, options(r))
				var succeeded bool
				var err error
				if command == "update" {
					got, failure := s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: missing == "guard"})
					succeeded, err = got != nil, failure
				} else {
					got, failure := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: missing == "guard"})
					succeeded, err = got != nil, failure
				}
				if succeeded || err == nil || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "delete") || slices.Contains(r.calls, "commit") {
					t.Fatal("missing capability reached publication", err, r.calls)
				}
				requireCode(t, err, "context.state")
			})
		}
	}
}

func TestUpdateMayChangeAcquisitionRootWithoutChangingEnvironmentIdentity(t *testing.T) {
	r := existingRepository(t)
	input := sourceFixture("/synthetic/input")
	input.Roots = []string{"/synthetic"}
	original := r.registry.Contexts[0]
	got, err := service(t, r, input).Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic", SkipConfirmation: true})
	if err != nil || got.Context.ID != original.ID || r.registry.Contexts[0].EnvironmentDirectory != original.EnvironmentDirectory || !reflect.DeepEqual(r.roots, []string{"/synthetic"}) {
		t.Fatalf("equivalent Environment location changed identity: %#v %v", got, err)
	}
	if !reflect.DeepEqual(r.published[r.registry.Contexts[0].Revision].Roots, input.Roots) || slices.Contains(r.calls, "confirm") {
		t.Fatal("replacement acquisition was not preserved or --yes prompted")
	}
}

type selectionStore struct{ r *repository }

func (s selectionStore) Read(ctx context.Context) (contexts.Selection, error) {
	return s.r.selection, ctx.Err()
}

func (s selectionStore) Write(ctx context.Context, value contexts.Selection) error {
	if err := s.r.step(ctx, "select"); err != nil {
		return err
	}
	s.r.selection = value
	return nil
}

func (s selectionStore) Clear(ctx context.Context, expected contexts.Selection) error {
	if err := s.r.step(ctx, "clear"); err != nil {
		return err
	}
	if s.r.selection == expected {
		s.r.selection = contexts.Selection{}
	}
	return nil
}

type configurationReader struct{ r *repository }

func (c configurationReader) ReadConfiguration(ctx context.Context, path string) ([]byte, error) {
	if err := c.r.step(ctx, "read-config"); err != nil {
		return nil, err
	}
	return slices.Clone(c.r.configInput), nil
}

func options(r *repository) contexts.Options {
	return contexts.Options{Selection: selectionStore{r}, ConfigurationReader: configurationReader{r}, ValidateConfiguration: func(ctx context.Context, c contexts.Configuration) error {
		if c.SecretStore.Type != "local-keyring" {
			return contexts.ConfigurationError("unavailable implementation")
		}
		return ctx.Err()
	}, InitializeSecrets: func(ctx context.Context, record contexts.Record, area storage.Area) error { return ctx.Err() }}
}

func TestDefaultInitThenFirstInputImport(t *testing.T) {
	r := newRepository(t)
	s := service(t, r, sourceFixture("/synthetic/input"))
	got, err := s.Init(context.Background(), contexts.InitRequest{Name: "example"})
	if err != nil || got == nil || got.Context.Mode != contexts.Ready || got.Context.Configured || !got.Context.Current || got.InputChanged || got.FilesCopied != 0 {
		t.Fatalf("default init: %+v %v", got, err)
	}
	if slices.Contains(r.calls, "preflight") || slices.Contains(r.calls, "read") || slices.Contains(r.calls, "compile") || !slices.Contains(r.calls, "initialize") {
		t.Fatal("default init acquired input or omitted encryption", r.calls)
	}
	id := got.Context.ID
	if !reflect.DeepEqual(r.configurations[id], contexts.DefaultConfiguration("example").Canonical()) || r.selection.ID != id {
		t.Fatal("default configuration or pointer was not published")
	}
	r.calls = nil
	got, err = s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input"})
	if err != nil || !got.Context.Configured || got.Context.ID != id || !got.InputChanged || !slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "initialize") {
		t.Fatal("first import failed to preserve configured identity", got, err, r.calls)
	}
}

func TestEquivalentConfigurationUpdateDoesNotPublishOrConfirm(t *testing.T) {
	r := existingRepository(t)
	r.configInput = contexts.DefaultConfiguration("example").Canonical()
	before := cloneRegistry(r.registry)
	got, err := service(t, r, desiredstate.Sources{}).Update(context.Background(), contexts.UpdateRequest{Name: "example", ConfigurationFile: "context.yaml"})
	if err != nil || got == nil || got.InputChanged || !reflect.DeepEqual(before, r.registry) {
		t.Fatal("equivalent configuration changed state", got, err)
	}
	for _, forbidden := range []string{"preflight", "read", "compile", "lease", "guard", "confirm", "publish", "commit", "select"} {
		if slices.Contains(r.calls, forbidden) {
			t.Fatal("equivalent update had unnecessary effects", r.calls)
		}
	}
}

func TestConfigurationAdmissionFailurePrecedesStateWrites(t *testing.T) {
	for _, config := range []string{"kind: Environment\n", strings.ReplaceAll(string(contexts.DefaultConfiguration("example").Canonical()), "local-keyring", "unavailable-store")} {
		r := newRepository(t)
		r.configInput = []byte(config)
		got, err := service(t, r, desiredstate.Sources{}).Init(context.Background(), contexts.InitRequest{Name: "example", ConfigurationFile: "context.yaml"})
		if err == nil || got != nil || slices.Contains(r.calls, "transaction") || r.selection.Name != "" {
			t.Fatal("invalid or unavailable configuration reached state mutation", got, err, r.calls)
		}
	}
}

func TestIncompleteInitializationCanResumeWithSameIdentity(t *testing.T) {
	r := newRepository(t)
	r.failure = "initialize"
	s := service(t, r, desiredstate.Sources{})
	if result, err := s.Init(context.Background(), contexts.InitRequest{Name: "example"}); result != nil || err == nil {
		t.Fatal("failed initializer claimed success")
	}
	if len(r.registry.Contexts) != 1 || r.registry.Contexts[0].Mode != contexts.Initializing || r.selection.Name != "" {
		t.Fatal("incomplete initialization was lost or selected", r.registry, r.selection)
	}
	id := r.registry.Contexts[0].ID
	r.failure, r.calls = "", nil
	listed, err := s.List(context.Background(), contexts.ListRequest{})
	if err != nil || len(listed.Contexts) != 1 || listed.Contexts[0].Mode != contexts.Initializing {
		t.Fatal("incomplete initialization cannot be inspected", listed, err)
	}
	if result, err := s.Use(context.Background(), contexts.UseRequest{Name: "example"}); result != nil || err == nil {
		t.Fatal("incomplete context was selected")
	}
	r.calls = nil
	result, err := s.Init(context.Background(), contexts.InitRequest{Name: "example"})
	if err != nil || result.Context.ID != id || result.Context.Mode != contexts.Ready || len(r.registry.Identities) != 1 {
		t.Fatal("initialization retry did not retain identity", result, err)
	}
	if repeated, err := s.Init(context.Background(), contexts.InitRequest{Name: "example"}); repeated != nil || err == nil {
		t.Fatal("ready context was recreated")
	}
}

func TestSelectionFailureAfterInitializationPreservesReadyContext(t *testing.T) {
	r := newRepository(t)
	r.selection = contexts.Selection{Version: 1, Name: "prior", ID: "ctx-prior"}
	r.failure = "select"
	got, err := service(t, r, desiredstate.Sources{}).Init(context.Background(), contexts.InitRequest{Name: "example"})
	if got != nil || err == nil || len(r.registry.Contexts) != 1 || r.registry.Contexts[0].Mode != contexts.Ready || r.selection.Name != "prior" {
		t.Fatal("selection failure destroyed or misreported published context", got, err, r.registry)
	}
	if !strings.Contains(desiredstate.DiagnosticsOf(err)[0].Message, "context was created") {
		t.Fatal("partial success lacks recovery guidance", err)
	}
}

func TestPerUserSelectionAndDeletedNameCannotRebindIdentity(t *testing.T) {
	r := existingRepository(t)
	s := service(t, r, desiredstate.Sources{})
	old := r.selection
	before := cloneRegistry(r.registry)
	if got, err := s.Use(context.Background(), contexts.UseRequest{Name: "example"}); err != nil || !got.Context.Current || !reflect.DeepEqual(r.registry, before) || slices.Contains(r.calls, "commit") {
		t.Fatal("use changed shared registry", got, err, r.calls)
	}
	r.calls = nil
	deleted, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: true})
	if err != nil || !deleted.CurrentCleared || len(r.registry.Contexts) != 0 || len(r.registry.Identities) != 1 || len(r.configurations) != 0 {
		t.Fatal("delete did not remove context while retaining ID reservation", deleted, err)
	}
	r.calls = nil
	created, err := s.Init(context.Background(), contexts.InitRequest{Name: "example"})
	if err != nil || created.Context.ID == old.ID {
		t.Fatal("deleted name reused its old identity", created, err)
	}
	r.selection = old
	if got, err := s.Current(context.Background(), contexts.CurrentRequest{}); got != nil || err == nil {
		t.Fatal("stale pointer selected new context identity")
	}
}

func TestIdentityConflictsPreserveExistingContexts(t *testing.T) {
	for _, tc := range []struct{ command, name, directory string }{
		{"init", "example", ""},
		{"init", "another", "/synthetic/input"},
		{"update", "example", "/synthetic/moved"},
		{"update", "missing", "/synthetic/input"},
	} {
		t.Run(tc.command+"/"+tc.name+"/"+tc.directory, func(t *testing.T) {
			r := existingRepository(t)
			before := cloneRegistry(r.registry)
			s := service(t, r, sourceFixture(tc.directory))
			var got *contexts.AdmissionResult
			var err error
			if tc.command == "init" {
				got, err = s.Init(context.Background(), contexts.InitRequest{Name: tc.name, InputDirectory: tc.directory})
			} else {
				got, err = s.Update(context.Background(), contexts.UpdateRequest{Name: tc.name, InputDirectory: tc.directory, SkipConfirmation: true})
			}
			if got != nil || err == nil || !reflect.DeepEqual(r.registry, before) || slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "reserve") {
				t.Fatal("identity conflict reached publication", got, err, r.calls)
			}
		})
	}
}

type inputRepositoryFunc func(context.Context, string, string) (desiredstate.Sources, error)

func (f inputRepositoryFunc) ReadInputs(ctx context.Context, name, id string) (desiredstate.Sources, error) {
	return f(ctx, name, id)
}

func TestCurrentInputAcquisitionCarriesSelectedIdentity(t *testing.T) {
	r := existingRepository(t)
	calls := 0
	inputs := contexts.Inputs{Selection: selectionStore{r}, Repository: inputRepositoryFunc(func(ctx context.Context, name, id string) (desiredstate.Sources, error) {
		calls++
		if name != "example" || calls == 1 && id != r.selection.ID || calls == 2 && id != "" {
			t.Fatal("selection identity was discarded or explicit target changed", name, id)
		}
		return sourceFixture("/synthetic/input"), nil
	})}
	if _, err := inputs.ReadInputs(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := inputs.ReadInputs(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	r.selection = contexts.Selection{}
	if _, err := inputs.ReadInputs(context.Background(), ""); err == nil || calls != 2 {
		t.Fatal("missing selection reached repository", err, calls)
	}
}
