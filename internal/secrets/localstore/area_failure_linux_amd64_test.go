//go:build linux && amd64

package localstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

func TestAreaFailurePreservesWorkspaceLimit(t *testing.T) {
	original := storage.Failure("store.limit", "bounded secret storage is full")
	actual := areaFailure(context.Background(), "store.conflict", "secret publication failed", original)
	if actual != original || failureCode(actual) != "secret.store.limit" {
		t.Fatalf("area failure = %#v (%q), want original secret.store.limit", actual, failureCode(actual))
	}
}

func TestPublicationFailurePreservesOnlyPrecommitTypedFailures(t *testing.T) {
	limit := storage.Failure("store.limit", "bounded secret storage is full")
	for _, test := range []struct {
		name    string
		outcome storage.Outcome
		cause   error
		code    string
		message string
	}{
		{"precommit limit", storage.NotCommitted, limit, "secret.store.limit", "bounded secret storage is full"},
		{"precommit raw", storage.NotCommitted, errors.New("synthetic-error-canary"), "secret.store.conflict", "was not committed"},
		{"uncertain limit", storage.Uncertain, limit, "secret.store.conflict", "uncertain durability"},
		{"committed error", storage.Committed, limit, "secret.store.conflict", "committed but did not finish"},
		{"unknown outcome", storage.Outcome("invalid"), limit, "secret.store.conflict", "outcome is unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := publicationFailure(context.Background(), test.outcome, test.cause)
			diagnostics := desiredstate.DiagnosticsOf(err)
			if failureCode(err) != test.code || len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, test.message) || strings.Contains(diagnostics[0].Message, "canary") {
				t.Fatalf("publication failure: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publicationFailure(ctx, storage.NotCommitted, limit); !errors.Is(err, context.Canceled) {
		t.Fatalf("precommit cancellation: %v", err)
	}
	if err := publicationFailure(ctx, storage.NotCommitted, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("precommit cancellation without a backend error: %v", err)
	}
	if err := publicationFailure(ctx, storage.Uncertain, context.Canceled); errors.Is(err, context.Canceled) || failureCode(err) != "secret.store.conflict" {
		t.Fatalf("uncertain cancellation concealed the publication outcome: %v", err)
	}
}
