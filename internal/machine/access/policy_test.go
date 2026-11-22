package access

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func TestDelegatedBatchDenialPrecedesCredentialLending(t *testing.T) {
	refusal := errors.New("denied by policy")
	var checks int
	h := newHarness(t, func(options *Options) {
		options.Authorize = func(_ context.Context, ctxName string, _ api.Catalog, targets []machine.SessionTarget, argv []string) error {
			checks++
			if len(targets) != 2 || targets[0].Machine != "declared" || targets[1].RemoteUser != "bootwright" ||
				ctxName != "lab" || len(argv) != 1 || argv[0] != "/usr/bin/uptime" {
				t.Fatalf("authorization received %q %+v %v", ctxName, targets, argv)
			}
			return refusal
		}
	})
	_, err := h.service.Exec(t.Context(), ExecRequest{Names: []string{"declared", "guest"}, Command: []string{"/usr/bin/uptime"}})
	if !errors.Is(err, refusal) || checks != 1 || h.lender.lends != 0 {
		t.Fatalf("error %v, authorizations %d, lends %d", err, checks, h.lender.lends)
	}
}

func TestDelegatedShellChecksTheGrantBeforeLendingAndKeepsInteractiveInput(t *testing.T) {
	refusal := errors.New("denied by policy")
	input := strings.NewReader("local input")
	h := newHarness(t, func(options *Options) {
		options.Streams.In = input
		options.Authorize = func(_ context.Context, name string, _ api.Catalog, targets []machine.SessionTarget, argv []string) error {
			want := []machine.SessionTarget{{Machine: "declared", RemoteUser: "admin"}}
			if name != "lab" || !slices.Equal(targets, want) || len(argv) != 0 {
				t.Fatalf("shell authorization received %q %+v %v", name, targets, argv)
			}
			return refusal
		}
	})
	service := h.service.WithCatalog(catalog())
	request := RshRequest{Name: "declared"}
	if _, err := service.Rsh(t.Context(), request); !errors.Is(err, refusal) || h.lender.lends != 0 {
		t.Fatalf("refused shell: %v, lends %d", err, h.lender.lends)
	}
	refusal = nil
	if _, err := service.Rsh(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if len(h.client.sessions) != 1 || !h.client.sessions[0].Delegated || h.client.sessions[0].Batch ||
		len(h.client.inputs) != 1 || h.client.inputs[0] != input {
		t.Fatalf("shell sessions %+v, input %v", h.client.sessions, h.client.inputs)
	}
}

func TestDelegatedSessionsRefuseOverridesAndUnconfiguredKeys(t *testing.T) {
	h := newHarness(t, func(options *Options) {
		options.Authorize = func(context.Context, string, api.Catalog, []machine.SessionTarget, []string) error {
			t.Fatal("invalid identity reached policy")
			return nil
		}
	})
	for _, request := range []ExecRequest{
		{Names: []string{"declared"}, SSH: machine.SSHOptions{IdentityFile: "file"}, Command: []string{"/usr/bin/uptime"}},
		{Names: []string{"host"}, Command: []string{"/usr/bin/uptime"}},
	} {
		_, err := h.service.Exec(t.Context(), request)
		if code(t, err) != "access.denied" || h.lender.lends != 0 || h.client.proofs != 0 {
			t.Fatalf("error %v, lends %d, identity file proofs %d", err, h.lender.lends, h.client.proofs)
		}
	}
}

func TestDelegatedExecNeedsExistingTrustAndClosesInput(t *testing.T) {
	prepared := false
	h := newHarness(t, func(options *Options) {
		options.Streams.In = strings.NewReader("local input")
		options.Prepared = func(context.Context) { prepared = true }
		options.Authorize = func(context.Context, string, api.Catalog, []machine.SessionTarget, []string) error { return nil }
	})
	if _, err := exec(t, h, "keyed", machine.SSHOptions{}, "/usr/bin/uptime"); code(t, err) != "trust.identity" {
		t.Fatal(err)
	}
	if prepared || h.observer.probes != 0 || len(h.prompt.asked) != 0 || len(h.client.sessions) != 0 {
		t.Fatal("missing host trust reached effects or session launch")
	}
	h.client.run = func(session machine.Session, _, _ io.Writer) (int, error) {
		if !prepared || !session.Delegated || !session.Batch {
			t.Errorf("delegated exec preparation = %t, session = %+v", prepared, session)
		}
		return 0, nil
	}
	if _, err := exec(t, h, "declared", machine.SSHOptions{}, "/usr/bin/uptime"); err != nil {
		t.Fatal(err)
	}
	if len(h.client.inputs) != 1 || h.client.inputs[0] != nil {
		t.Fatal("delegated exec inherited local input")
	}
}
