package custody

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

type fixedHolds struct {
	holds Holds
	err   error
	reads int
}

func (f *fixedHolds) SecretHolds(context.Context, string) (Holds, error) {
	f.reads++
	return f.holds, f.err
}

type hookConfirmer struct {
	calls   int
	decline error
	during  func()
}

func (c *hookConfirmer) Confirm(context.Context, string, string) error {
	c.calls++
	if c.during != nil {
		c.during()
	}
	return c.decline
}

func generatedPair(t *testing.T) (*Service, *serviceAccess, *hookConfirmer, *fixedHolds) {
	t.Helper()
	base, access, material, _ := serviceFixture(t, declarationYAML("passphrase", "  source: {generated: {}}\n")+declarationYAML("bmc-password", "  source: {generated: {}}\n")+declarationYAML("recovery", ""))
	confirmer := &hookConfirmer{}
	service := New(access, base.compiler, material, confirmer)
	if _, err := service.Generate(context.Background(), GenerateRequest{}); err != nil {
		t.Fatalf("first generation: %v", err)
	}
	if _, err := service.Set(context.Background(), SetRequest{Name: "recovery", Input: secrets.Input{ValueFile: "first"}}); err != nil {
		t.Fatalf("first set: %v", err)
	}
	binding, err := service.Bind(context.Background(), BindRequest{Names: []string{"bmc-password", "passphrase", "recovery"}})
	if err != nil {
		t.Fatal(err)
	}
	holds := &fixedHolds{holds: Holds{Bindings: []Hold{{Binding: binding.ID, Operation: "the apply op-1 (done)", Secrets: []string{"passphrase", "recovery"}}}}}
	return service.WithHolds(holds), access, confirmer, holds
}

func currentOf(access *serviceAccess, name string) string {
	version, _ := currentVersion(access.session.snapshot, name)
	return version.ID
}

// A live binding's effect material is never renewed: a named renewal refuses
// naming the Secret, the operation and destroy, and writes nothing; an unnamed
// one renews the access credential beside it and reports the effect material
// unchanged. Renewing a current version asks first.
func TestRenewRefusesALiveEffectMaterialBinding(t *testing.T) {
	service, access, confirmer, _ := generatedPair(t)
	passphrase, password := currentOf(access, "passphrase"), currentOf(access, "bmc-password")
	writes := access.session.writes
	_, err := service.Generate(context.Background(), GenerateRequest{Name: "passphrase", Renew: true, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "secret.store.conflict" || !strings.Contains(reported[0].Message, "Secret passphrase is held by the apply op-1 (done) as effect material, and no command reveals a version other than the current one") ||
		!strings.Contains(reported[0].Remediation, "bootwright destroy --context fixture") || access.session.writes != writes || currentOf(access, "passphrase") != passphrase {
		t.Fatalf("a named renewal of held effect material reported %#v and wrote %d", reported, access.session.writes-writes)
	}
	calls := confirmer.calls
	result, err := service.Generate(context.Background(), GenerateRequest{Renew: true})
	if err != nil || !slices.Equal(result.ChangedNames, []string{"bmc-password"}) || !slices.Equal(result.UnchangedNames, []string{"passphrase"}) ||
		currentOf(access, "passphrase") != passphrase || currentOf(access, "bmc-password") == password || confirmer.calls != calls+1 {
		t.Fatalf("an unnamed renewal = %+v (%v), confirmations %d", result, err, confirmer.calls-calls)
	}
	recovery := currentOf(access, "recovery")
	for _, yes := range []bool{true, false} {
		calls, writes := confirmer.calls, access.session.writes
		_, err = service.Set(context.Background(), SetRequest{Name: "recovery", Input: secrets.Input{ValueFile: "second"}, SkipConfirmation: yes})
		if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "Secret recovery is held by the apply op-1 (done) as effect material, and no command reveals a version other than the current one") ||
			access.session.writes != writes || confirmer.calls != calls || currentOf(access, "recovery") != recovery {
			t.Fatalf("a set over held effect material (--yes %v) reported %#v", yes, reported)
		}
	}
}

// A delete of held effect material refuses before any prompt and again under
// the lease, so a later generation cannot replace the only revealable copy;
// the access credential beside it still deletes.
func TestDeleteRefusesALiveEffectMaterialBinding(t *testing.T) {
	for _, yes := range []bool{true, false} {
		service, access, confirmer, _ := generatedPair(t)
		for _, name := range []string{"passphrase", "recovery"} {
			current, writes, calls := currentOf(access, name), access.session.writes, confirmer.calls
			_, err := service.Delete(context.Background(), DeleteRequest{Name: name, SkipConfirmation: yes})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "secret.store.conflict" || !strings.Contains(reported[0].Message, "Secret "+name+" is held by the apply op-1 (done) as effect material, and no command reveals a version other than the current one") ||
				!strings.Contains(reported[0].Remediation, "bootwright destroy --context fixture") || access.session.writes != writes || confirmer.calls != calls || currentOf(access, name) != current {
				t.Fatalf("a delete of held %s (--yes %v) reported %#v, wrote %d, asked %d", name, yes, reported, access.session.writes-writes, confirmer.calls-calls)
			}
		}
		if result, err := service.Delete(context.Background(), DeleteRequest{Name: "bmc-password", SkipConfirmation: yes}); err != nil || result.Changed != 1 || currentOf(access, "bmc-password") != "" {
			t.Fatalf("a delete of an access credential (--yes %v) = %+v (%v)", yes, result, err)
		}
	}
	late, access, confirmer, reader := generatedPair(t)
	reader.holds = Holds{}
	before := currentOf(access, "bmc-password")
	confirmer.during = func() {
		if _, err := late.Bind(context.Background(), BindRequest{Names: []string{"bmc-password"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := late.Delete(context.Background(), DeleteRequest{Name: "bmc-password"}); err == nil || currentOf(access, "bmc-password") != before {
		t.Fatal("a binding issued during the delete prompt let its Secret be deleted")
	}
}

// A binding no registered operation names holds every Secret it pins while an
// apply is registering, and when the lifecycle records cannot be read; beside
// a context at rest it holds nothing.
func TestAnUnnamedBindingHoldsWhileAnApplyRegisters(t *testing.T) {
	for name, test := range map[string]struct {
		holds  Holds
		refuse bool
	}{
		"registering": {holds: Holds{Registering: true}, refuse: true},
		"at rest":     {holds: Holds{}},
	} {
		t.Run(name, func(t *testing.T) {
			service, access, _, reader := generatedPair(t)
			reader.holds = test.holds
			before := currentOf(access, "bmc-password")
			_, err := service.Generate(context.Background(), GenerateRequest{Name: "bmc-password", Renew: true, SkipConfirmation: true})
			if refused := err != nil; refused != test.refuse || (currentOf(access, "bmc-password") == before) != test.refuse {
				t.Fatalf("renewal refused %v (%v), want %v", refused, diagnostics.Of(err), test.refuse)
			}
		})
	}
	late, access, confirmer, reader := generatedPair(t)
	reader.holds = Holds{}
	before := currentOf(access, "bmc-password")
	confirmer.during = func() {
		if _, err := late.Bind(context.Background(), BindRequest{Names: []string{"bmc-password"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := late.Generate(context.Background(), GenerateRequest{Name: "bmc-password", Renew: true}); err == nil || currentOf(access, "bmc-password") != before {
		t.Fatal("a binding issued after the holds were read let its Secret be renewed")
	}
	unread, access, _, _ := generatedPair(t)
	before = currentOf(access, "bmc-password")
	unread = unread.WithHolds(&fixedHolds{err: diagnostics.NewFailure("lifecycle.state", "unreadable", "")})
	if _, err := unread.Generate(context.Background(), GenerateRequest{Name: "bmc-password", Renew: true, SkipConfirmation: true}); err == nil || currentOf(access, "bmc-password") != before {
		t.Fatal("unreadable lifecycle records let a bound Secret be renewed")
	}
}

// A declined renewal writes nothing, and a renewal whose current versions
// changed between the prompt and the lease refuses as a conflict.
func TestARenewalConfirmsBeforeTheLease(t *testing.T) {
	service, access, confirmer, _ := generatedPair(t)
	confirmer.decline = diagnostics.NewFailure("secret.store.conflict", "declined", "")
	writes := access.session.writes
	if _, err := service.Generate(context.Background(), GenerateRequest{Name: "bmc-password", Renew: true}); err == nil || access.session.writes != writes {
		t.Fatalf("a declined renewal wrote %d (%v)", access.session.writes-writes, err)
	}
	confirmer.decline = nil
	confirmer.during = func() {
		for index := range access.session.snapshot.Current {
			if access.session.snapshot.Current[index].Name == "bmc-password" {
				access.session.snapshot.Current[index].Version = currentOf(access, "passphrase")
			}
		}
	}
	_, err := service.Generate(context.Background(), GenerateRequest{Name: "bmc-password", Renew: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "secret.store.conflict" || !strings.Contains(reported[0].Message, "changed in context fixture") {
		t.Fatalf("a renewal over a version changed meanwhile reported %#v", reported)
	}
}
