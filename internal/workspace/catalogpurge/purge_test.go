package catalogpurge

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type testStore struct {
	plan        Plan
	previews    int
	commits     int
	commitError error
}

func (s *testStore) PlanCatalogPurge(_ context.Context, _ Kind, force bool, _ References) (Plan, error) {
	s.previews++
	s.plan.Force = force
	return s.plan, nil
}

func (s *testStore) CommitCatalogPurge(_ context.Context, plan Plan, _ References) (int, error) {
	s.commits++
	return selectedCount(plan), s.commitError
}

type confirmFunc func(context.Context, string, string) error

func (f confirmFunc) Confirm(ctx context.Context, name, scope string) error {
	return f(ctx, name, scope)
}

func noReferences(context.Context, Kind, desiredstate.Sources) ([]string, error) { return nil, nil }

func TestCatalogPurgeAsksOnlyForAnActualMutation(t *testing.T) {
	store := &testStore{plan: Plan{Kind: Artifact, Entries: []Entry{{Name: "archive", ReferencedBy: []string{"lab"}}}}}
	service := Service{Store: store, References: noReferences}
	for _, request := range []Request{{Kind: Artifact}, {Kind: Artifact, DryRun: true, Force: true}} {
		if result, err := service.Purge(t.Context(), request); err != nil || result.DryRun != request.DryRun || store.commits != 0 {
			t.Fatalf("read-only result: %+v, error %v", result, err)
		}
	}
	if _, err := service.Purge(t.Context(), Request{Kind: Artifact, Force: true}); err == nil || store.commits != 0 {
		t.Fatal("force waived confirmation")
	}
	var presented bool
	service.Present = func(_ context.Context, plan Plan) error {
		presented = plan.Force
		return nil
	}
	refusal := errors.New("not approved")
	service.Confirmer = confirmFunc(func(_ context.Context, action, scope string) error {
		if !presented || action != "purge catalog artifact" || scope != "1 entry" {
			t.Fatalf("unexpected confirmation: %v, %q, %q", presented, action, scope)
		}
		return refusal
	})
	if _, err := service.Purge(t.Context(), Request{Kind: Artifact, Force: true}); !errors.Is(err, refusal) || store.commits != 0 {
		t.Fatal("a declined purge mutated the catalog")
	}
	store.commitError = Failure(Artifact, "test failure", "retry")
	if _, err := service.Purge(t.Context(), Request{Kind: Artifact, Force: true, Yes: true}); len(diagnostics.Of(err)) != 2 || store.commits != 1 {
		t.Fatalf("partial mutation was not reported: %v", err)
	}
}
