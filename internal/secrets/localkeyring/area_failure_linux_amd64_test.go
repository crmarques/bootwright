//go:build linux && amd64

package localkeyring

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestAreaFailurePreservesWorkspaceLimit(t *testing.T) {
	original := secretstore.Failure("store.limit", "bounded secret storage is full")
	actual := areaFailure(context.Background(), "store.conflict", "secret publication failed", original)
	if actual != original || failureCode(actual) != "secret.store.limit" {
		t.Fatalf("area failure = %#v (%q), want original secret.store.limit", actual, failureCode(actual))
	}
}

func TestPublicationFailurePreservesOnlyPrecommitTypedFailures(t *testing.T) {
	limit := secretstore.Failure("store.limit", "bounded secret storage is full")
	for _, test := range []struct {
		name    string
		outcome secretstore.Outcome
		cause   error
		code    string
		message string
	}{
		{"precommit limit", secretstore.NotCommitted, limit, "secret.store.limit", "bounded secret storage is full"},
		{"precommit raw", secretstore.NotCommitted, errors.New("synthetic-error-canary"), "secret.store.conflict", "was not committed"},
		{"uncertain limit", secretstore.Uncertain, limit, "secret.store.conflict", "uncertain durability"},
		{"committed error", secretstore.Committed, limit, "secret.store.conflict", "committed but did not finish"},
		{"unknown outcome", secretstore.Outcome("invalid"), limit, "secret.store.conflict", "outcome is unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := publicationFailure(context.Background(), test.outcome, test.cause)
			diagnostics := diagnostics.Of(err)
			if failureCode(err) != test.code || len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, test.message) || strings.Contains(diagnostics[0].Message, "canary") {
				t.Fatalf("publication failure: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publicationFailure(ctx, secretstore.NotCommitted, limit); !errors.Is(err, context.Canceled) {
		t.Fatalf("precommit cancellation: %v", err)
	}
	if err := publicationFailure(ctx, secretstore.NotCommitted, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("precommit cancellation without a backend error: %v", err)
	}
	if err := publicationFailure(ctx, secretstore.Uncertain, context.Canceled); errors.Is(err, context.Canceled) || failureCode(err) != "secret.store.conflict" {
		t.Fatalf("uncertain cancellation concealed the publication outcome: %v", err)
	}
}
