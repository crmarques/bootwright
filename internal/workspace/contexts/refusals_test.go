package contexts_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// emptyStoreRefusal is a repository's refusal of a store that holds no
// context yet, as the store reports it.
type emptyStoreRefusal struct{ failure error }

func (e emptyStoreRefusal) Error() string { return e.failure.Error() }

func (e emptyStoreRefusal) Unwrap() error { return e.failure }

func (emptyStoreRefusal) Is(target error) bool { return target == contexts.ErrNoContexts }

func requireRefusal(t *testing.T, err error, code, message, remediation string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code || reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("got %v %#v\nwant %s: %s; next: %s", err, reported, code, message, remediation)
	}
}

func TestContextRefusalsNameTheContextAndTheNextCommand(t *testing.T) {
	const (
		absent       = "context ghost does not exist"
		absentRemedy = "create it with bootwright context init --name ghost, or select an existing one with bootwright context use --name <context>; bootwright context list names them"
		initializing = "context example is still initializing: an earlier context init did not finish"
		resume       = "finish it with bootwright context init --name example and its original Context file, from any input directory, or discard it with bootwright context delete --name example --purge"
		deleting     = "context example is being deleted"
		finish       = "finish the deletion with bootwright context delete --name example --purge"
	)
	example := func(mode contexts.Mode) contexts.Record { return contexts.Record{Name: "example", Mode: mode} }
	t.Run("constructors", func(t *testing.T) {
		requireRefusal(t, contexts.AbsentContext("ghost"), "context.state", absent, absentRemedy)
		requireRefusal(t, contexts.NoSelection(), "context.state", "no current context is selected",
			"select one with bootwright context use --name <context>, or create one with bootwright context init --name <context>")
		requireRefusal(t, contexts.MissingInput("example"), "context.input", "context example has no desired state",
			"import it with bootwright context update --name example --input-dir <dir>")
		requireRefusal(t, contexts.NotReady(example(contexts.Initializing)), "context.state", initializing, resume)
		requireRefusal(t, contexts.NotReady(example(contexts.Deleting)), "context.state", deleting, finish)
		requireRefusal(t, contexts.AlreadyExists(example(contexts.Ready)), "context.state", "context example already exists",
			"replace its input with bootwright context update --name example --input-dir <dir>, or delete it with bootwright context delete --name example --purge")
		requireRefusal(t, contexts.AlreadyExists(example(contexts.Deleting)), "context.state", deleting, finish+", then repeat the init")
		requireRefusal(t, contexts.IncompleteOperation("example"), "context.state", "context example has an incomplete operation, which continues from the input it froze",
			"continue it with the command bootwright status --context example names, or take back what it owns with bootwright destroy --context example, then repeat the update")
		requireRefusal(t, contexts.NoContexts(), "context.state", "no context exists on this host yet", "create one with bootwright context init --name <name>")
	})
	t.Run("init over an existing name", func(t *testing.T) {
		for _, mode := range []contexts.Mode{contexts.Ready, contexts.Deleting} {
			r := existingRepository(t)
			r.registry.Contexts[0].Mode = mode
			_, err := service(t, r, sourceFixture("/synthetic/input")).Init(context.Background(), contexts.InitRequest{Name: "example"})
			want := diagnostics.Of(contexts.AlreadyExists(example(mode)))[0]
			requireRefusal(t, err, want.Code, want.Message, want.Remediation)
		}
		r := existingRepository(t)
		r.registry.Contexts[0].Mode, r.registry.Contexts[0].Revision = contexts.Initializing, ""
		if got, err := service(t, r, sourceFixture("/synthetic/input")).Init(context.Background(), contexts.InitRequest{Name: "example"}); err != nil || got.Context.Mode != contexts.Ready {
			t.Fatalf("an initializing name did not resume: %+v %v", got, err)
		}
	})
	t.Run("an absent name", func(t *testing.T) {
		for name, call := range map[string]func(contexts.Service, *repository) error{
			"use": func(s contexts.Service, _ *repository) error {
				_, err := s.Use(context.Background(), contexts.UseRequest{Name: "ghost"})
				return err
			},
			"update": func(s contexts.Service, _ *repository) error {
				_, err := s.Update(context.Background(), contexts.UpdateRequest{Name: "ghost", InputDirectory: "/synthetic/input", SkipConfirmation: true})
				return err
			},
			"delete": func(s contexts.Service, _ *repository) error {
				_, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "ghost", Purge: true, SkipConfirmation: true})
				return err
			},
			"current": func(s contexts.Service, r *repository) error {
				r.selection = contexts.Selection{Version: contexts.SelectionVersion, Name: "ghost"}
				_, err := s.Current(context.Background(), contexts.CurrentRequest{})
				return err
			},
		} {
			t.Run(name, func(t *testing.T) {
				r := existingRepository(t)
				requireRefusal(t, call(service(t, r, sourceFixture("/synthetic/input")), r), "context.state", absent, absentRemedy)
			})
		}
	})
	t.Run("a context that is not ready", func(t *testing.T) {
		for mode, want := range map[contexts.Mode][2]string{contexts.Initializing: {initializing, resume}, contexts.Deleting: {deleting, finish}} {
			r := existingRepository(t)
			r.registry.Contexts[0].Mode = mode
			_, err := service(t, r, sourceFixture("/synthetic/input")).Use(context.Background(), contexts.UseRequest{Name: "example"})
			requireRefusal(t, err, "context.state", want[0], want[1])
		}
	})
	t.Run("update over pending evidence", func(t *testing.T) {
		r := existingRepository(t)
		r.evidence["example"] = []byte(`{"version":1,"operation":"pending","ownership":"retained"}`)
		_, err := service(t, r, sourceFixture("/synthetic/input")).Update(context.Background(), contexts.UpdateRequest{Name: "example", InputDirectory: "/synthetic/input", SkipConfirmation: true})
		want := diagnostics.Of(contexts.IncompleteOperation("example"))[0]
		requireRefusal(t, err, want.Code, want.Message, want.Remediation)
	})
	t.Run("an empty store names the context asked for", func(t *testing.T) {
		for name, call := range map[string]func(contexts.Service) error{
			"use": func(s contexts.Service) error {
				_, err := s.Use(context.Background(), contexts.UseRequest{Name: "ghost"})
				return err
			},
			"update": func(s contexts.Service) error {
				_, err := s.Update(context.Background(), contexts.UpdateRequest{Name: "ghost", InputDirectory: "/synthetic/input", SkipConfirmation: true})
				return err
			},
			"delete": func(s contexts.Service) error {
				_, err := s.Delete(context.Background(), contexts.DeleteRequest{Name: "ghost", Purge: true, SkipConfirmation: true})
				return err
			},
		} {
			t.Run(name, func(t *testing.T) {
				r := newRepository(t)
				r.failure, r.failureErr = "transaction", emptyStoreRefusal{contexts.NoContexts()}
				requireRefusal(t, call(service(t, r, sourceFixture("/synthetic/input"))), "context.state", absent, absentRemedy)
			})
		}
	})
}
