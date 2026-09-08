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
	t             *testing.T
	registry      contexts.Registry
	calls         []string
	locked        bool
	leased        bool
	create        bool
	roots         []string
	evidence      map[string][]byte
	published     map[string]desiredstate.Sources
	archived      []contexts.Record
	outcomes      []string
	revisions     int
	failure       string
	failureErr    error
	cancelAt      string
	cancel        context.CancelFunc
	emptyCallback bool
}

func newRepository(t *testing.T) *repository {
	return &repository{t: t, registry: contexts.Registry{Version: 1, Contexts: []contexts.Record{}, Identities: []contexts.Identity{}}, evidence: map[string][]byte{}, published: map[string]desiredstate.Sources{}, failureErr: contexts.StateError("synthetic repository failure")}
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

func (r *repository) ReadInputs(context.Context, string) (desiredstate.Sources, error) {
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

func (tx transaction) Reserve(ctx context.Context, directory string) (string, error) {
	tx.requireLock()
	if err := tx.r.step(ctx, "reserve"); err != nil {
		return "", err
	}
	for _, identity := range tx.r.registry.Identities {
		if identity.EnvironmentDirectory == directory {
			return identity.ID, nil
		}
	}
	id := fmt.Sprintf("ctx-%032x", len(tx.r.registry.Identities)+1)
	tx.r.evidence[id] = []byte(pristineEvidence)
	return id, nil
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

func (tx transaction) Archive(ctx context.Context, record contexts.Record, outcome string) error {
	tx.requireLock()
	if !tx.r.leased {
		tx.r.t.Fatal("archival escaped the context lease")
	}
	if err := tx.r.step(ctx, "archive"); err != nil {
		return err
	}
	tx.r.archived = append(tx.r.archived, record)
	tx.r.outcomes = append(tx.r.outcomes, outcome)
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
	return contexts.New(reader, compiler, r, guard{r}, confirmer{r})
}

func existingRepository(t *testing.T) *repository {
	r := newRepository(t)
	id := "ctx-00000000000000000000000000000001"
	r.registry = contexts.Registry{Version: 1, Current: "example", Identities: []contexts.Identity{{ID: id, EnvironmentDirectory: "/synthetic/input"}}, Contexts: []contexts.Record{{Name: "example", ID: id, EnvironmentDirectory: "/synthetic/input", Revision: "rev-00000000000000000000000000000001", Mode: contexts.Active}}}
	r.evidence[id] = []byte(pristineEvidence)
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
	if !got.Context.Current || got.Context.Mode != contexts.Active || got.Context.ID == "" || !r.create || !reflect.DeepEqual(r.roots, []string{"/synthetic/input", "/synthetic/input"}) {
		t.Fatal("publication omitted identity, selection or forbidden roots")
	}
	if !reflect.DeepEqual(r.calls, []string{"preflight", "read", "compile", "transaction", "registry", "reserve", "lease", "guard", "publish", "commit"}) {
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
	r.registry.Current = "other"
	r.registry.Contexts = append(r.registry.Contexts, contexts.Record{Name: "other", ID: "ctx-00000000000000000000000000000002", EnvironmentDirectory: "/synthetic/other", Revision: "rev-00000000000000000000000000000002", Mode: contexts.Active})
	r.registry.Identities = append(r.registry.Identities, contexts.Identity{ID: "ctx-00000000000000000000000000000002", EnvironmentDirectory: "/synthetic/other"})
	before := cloneRegistry(r.registry)
	got, err := service(t, r, sourceFixture("/synthetic/input")).Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input"})
	if err != nil || got.Context.ID != before.Contexts[0].ID || got.Context.Current || r.registry.Current != "other" || r.registry.Contexts[0].Revision == before.Contexts[0].Revision || !reflect.DeepEqual(r.registry.Contexts[1], before.Contexts[1]) || !reflect.DeepEqual(r.registry.Identities, before.Identities) || r.create {
		t.Fatalf("update changed unrelated state: %#v %v", got, err)
	}
	if !reflect.DeepEqual(r.calls, []string{"preflight", "read", "compile", "transaction", "registry", "lease", "guard", "confirm", "publish", "commit"}) {
		t.Fatal("update order", r.calls)
	}
}

func TestIdentityConflictsAndRecreationRequireIndependentProof(t *testing.T) {
	for _, test := range []struct {
		name, target, directory, evidence string
		update, yes                       bool
	}{
		{name: "duplicate environment", target: "another", directory: "/synthetic/input", yes: true},
		{name: "duplicate name", target: "example", directory: "/synthetic/input"},
		{name: "moved update", target: "example", directory: "/synthetic/moved", update: true, yes: true},
		{name: "moved recreation", target: "example", directory: "/synthetic/moved", yes: true},
		{name: "unknown update", target: "missing", directory: "/synthetic/input", update: true, yes: true},
		{name: "owned recreation", target: "example", directory: "/synthetic/input", evidence: `{"version":1,"operation":"none","ownership":"retained"}`, yes: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := existingRepository(t)
			before := cloneRegistry(r.registry)
			if test.evidence != "" {
				r.evidence[r.registry.Contexts[0].ID] = []byte(test.evidence)
			}
			s := service(t, r, sourceFixture(test.directory))
			var result *contexts.AdmissionResult
			var err error
			if test.update {
				result, err = s.Update(context.Background(), contexts.UpdateRequest{Name: test.target, InputDirectory: test.directory, SkipConfirmation: test.yes})
			} else {
				result, err = s.Init(context.Background(), contexts.InitRequest{Name: test.target, InputDirectory: test.directory, SkipConfirmation: test.yes})
			}
			if result != nil || err == nil || !reflect.DeepEqual(r.registry, before) || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "confirm") {
				t.Fatalf("identity refusal published or prompted: %#v %v %v", result, err, r.calls)
			}
			requireCode(t, err, "context.state")
		})
	}
	r := existingRepository(t)
	id := r.registry.Contexts[0].ID
	got, err := service(t, r, sourceFixture("/synthetic/input")).Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
	if err != nil || got.Context.ID != id || !got.Context.Current || slices.Contains(r.calls, "reserve") || slices.Contains(r.calls, "confirm") {
		t.Fatalf("pristine recreation changed identity or prompted: %#v %v %v", got, err, r.calls)
	}
}

func TestReadSelectionDeleteAndReinitializeJourneys(t *testing.T) {
	r := existingRepository(t)
	original := r.registry.Contexts[0]
	r.registry.Contexts = append(r.registry.Contexts, contexts.Record{Name: "another", ID: "ctx-00000000000000000000000000000002", EnvironmentDirectory: "/synthetic/another", Revision: "rev-00000000000000000000000000000002", Mode: contexts.Active})
	r.registry.Identities = append(r.registry.Identities, contexts.Identity{ID: "ctx-00000000000000000000000000000002", EnvironmentDirectory: "/synthetic/another"})
	s := service(t, r, sourceFixture("/synthetic/input"))
	listed, err := s.List(context.Background(), contexts.ListRequest{})
	if err != nil || len(listed.Contexts) != 2 || listed.Contexts[0].Name != "another" || listed.Contexts[1].Name != "example" || !listed.Contexts[1].Current || !reflect.DeepEqual(r.calls, []string{"view"}) {
		t.Fatalf("list journey: %#v %v %v", listed, err, r.calls)
	}
	listed.Contexts[0].Name = "changed"
	current, err := s.Current(context.Background(), contexts.CurrentRequest{Short: true})
	if err != nil || current.Context.Name != "example" {
		t.Fatalf("current journey: %#v %v", current, err)
	}
	r.calls = nil
	used, err := s.Use(context.Background(), contexts.UseRequest{Name: "another"})
	if err != nil || !used.Context.Current || r.registry.Current != "another" || !reflect.DeepEqual(r.calls, []string{"transaction", "registry", "commit"}) {
		t.Fatalf("use journey: %#v %v %v", used, err, r.calls)
	}
	r.calls = nil
	deleted, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true})
	if err != nil || deleted.Outcome != "deleted" || deleted.CurrentCleared || r.registry.Current != "another" || len(r.archived) != 1 || r.archived[0] != original || !reflect.DeepEqual(r.outcomes, []string{"deleted"}) || len(r.registry.Identities) != 2 || !reflect.DeepEqual(r.calls, []string{"transaction", "registry", "lease", "guard", "confirm", "archive", "commit"}) {
		t.Fatalf("delete journey: %#v %v %v", deleted, err, r.calls)
	}
	r.calls = nil
	reinitialized, err := s.Init(context.Background(), contexts.InitRequest{Name: "restored", InputDirectory: "/synthetic/input"})
	if err != nil || reinitialized.Context.ID != original.ID || r.registry.Current != "restored" || len(r.registry.Identities) != 2 {
		t.Fatalf("reinitialize lost permanent identity: %#v %v", reinitialized, err)
	}
	r.calls = nil
	deleted, err = s.Delete(context.Background(), contexts.DeleteRequest{Name: "restored", Purge: true, SkipConfirmation: true})
	if err != nil || !deleted.CurrentCleared || r.registry.Current != "" || slices.Contains(r.calls, "confirm") {
		t.Fatalf("current deletion did not clear selection: %#v %v", deleted, err)
	}
	r.calls = nil
	if current, err := s.Current(context.Background(), contexts.CurrentRequest{}); current != nil || err == nil {
		t.Fatal("missing current selection succeeded")
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
						result, failure := s.Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
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
					} else if succeeded || err == nil || !reflect.DeepEqual(r.registry, before) || slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "archive") || slices.Contains(r.calls, "commit") {
						t.Fatal("protected state reached mutation or confirmation", err, r.calls)
					}
				})
			}
		}
	}
}

func TestRecoveryOnlyArchivalPreservesIdentitySelectionAndEvidence(t *testing.T) {
	r := existingRepository(t)
	before := r.registry.Contexts[0]
	data := []byte(`{"version":1,"operation":"failed","ownership":"retained"}`)
	r.evidence[before.ID] = data
	s := service(t, r, sourceFixture("/synthetic/input"))
	result, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, AbandonResources: true})
	if err != nil || result.Outcome != "recoveryOnly" || result.CurrentCleared || len(r.registry.Contexts) != 1 || r.registry.Current != "example" || r.registry.Contexts[0].Mode != contexts.RecoveryOnly || r.registry.Contexts[0].ID != before.ID || r.registry.Contexts[0].Revision != before.Revision || !reflect.DeepEqual(r.evidence[before.ID], data) || !reflect.DeepEqual(r.archived, []contexts.Record{before}) || !reflect.DeepEqual(r.outcomes, []string{"recoveryOnly"}) {
		t.Fatalf("archival discarded recovery state: %#v %v", result, err)
	}
	r.calls = nil
	if got, err := s.Use(context.Background(), contexts.UseRequest{Name: "example"}); err != nil || got.Context.Mode != contexts.RecoveryOnly || !got.Context.Current {
		t.Fatalf("recovery-only identity is not selectable: %#v %v", got, err)
	}
	for _, command := range []string{"init", "update", "delete"} {
		r.calls = nil
		var succeeded bool
		switch command {
		case "init":
			got, failure := s.Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
			succeeded, err = got != nil, failure
		case "update":
			got, failure := s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
			succeeded, err = got != nil, failure
		case "delete":
			got, failure := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: true})
			succeeded, err = got != nil, failure
		}
		if succeeded || err == nil || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "archive") || slices.Contains(r.calls, "commit") || slices.Contains(r.calls, "confirm") {
			t.Fatal("recovery-only context allowed forbidden transition", command, err, r.calls)
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
					got, failure := s.Init(context.Background(), contexts.InitRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
					succeeded, err = got != nil, failure
				case "update":
					got, failure := s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
					succeeded, err = got != nil, failure
				case "delete":
					got, failure := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: true, AbandonResources: true})
					succeeded, err = got != nil, failure
				}
				if succeeded || err == nil || slices.Contains(r.calls, "confirm") || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "archive") || slices.Contains(r.calls, "commit") {
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
			"init":    {"preflight", "read", "compile", "transaction", "reserve", "lease", "guard", "publish", "commit"},
			"update":  {"preflight", "read", "compile", "transaction", "lease", "guard", "confirm", "publish", "commit"},
			"use":     {"transaction", "commit"},
			"list":    {"view"},
			"current": {"view"},
			"delete":  {"transaction", "lease", "guard", "confirm", "archive", "commit"},
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
					if succeeded || err == nil || !reflect.DeepEqual(r.registry, before) || r.locked || r.leased {
						t.Fatal("failed command claimed publication or retained locks", err, r.calls)
					}
					if cancelStage && !errors.Is(err, context.Canceled) || !cancelStage && !errors.Is(err, r.failureErr) {
						t.Fatal("failure identity changed", err)
					}
					if stage != "publish" && stage != "archive" && stage != "commit" && (slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "archive") || slices.Contains(r.calls, "commit")) {
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
			s := contexts.New(reader, compiler, r, guard{r}, confirmer{r})
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
				s := contexts.New(reader, compiler, r, mutationGuard, confirmation)
				var succeeded bool
				var err error
				if command == "update" {
					got, failure := s.Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: missing == "guard"})
					succeeded, err = got != nil, failure
				} else {
					got, failure := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: missing == "guard", AbandonResources: true})
					succeeded, err = got != nil, failure
				}
				if succeeded || err == nil || slices.Contains(r.calls, "publish") || slices.Contains(r.calls, "archive") || slices.Contains(r.calls, "commit") {
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
	if err != nil || got.Context.ID != original.ID || r.registry.Contexts[0].EnvironmentDirectory != original.EnvironmentDirectory || !reflect.DeepEqual(r.roots, []string{"/synthetic", "/synthetic/input"}) {
		t.Fatalf("equivalent Environment location changed identity: %#v %v", got, err)
	}
	if !reflect.DeepEqual(r.published[r.registry.Contexts[0].Revision].Roots, input.Roots) || slices.Contains(r.calls, "confirm") {
		t.Fatal("replacement acquisition was not preserved or --yes prompted")
	}
}

func TestRecoveredArchiveMayFinallyPurgeWithDisposalEvidence(t *testing.T) {
	r := existingRepository(t)
	r.registry.Contexts[0].Mode = contexts.RecoveryOnly
	id := r.registry.Contexts[0].ID
	r.evidence[id] = []byte(`{"version":1,"operation":"none","ownership":"none"}`)
	s := service(t, r, sourceFixture("/synthetic/input"))
	result, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "example", Purge: true, SkipConfirmation: true})
	if err != nil || result == nil || result.Outcome != "deleted" || !result.CurrentCleared || len(r.registry.Contexts) != 0 || len(r.registry.Identities) != 1 || len(r.archived) != 1 {
		t.Fatalf("recovered archive could not purge: %+v %v", result, err)
	}
}
