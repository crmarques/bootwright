package custody

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// A bounded consumer's read opens one keyring session, unlocked to read
// material, and writes nothing: no transaction, binding or reservation. It
// returns each named Secret's current version, and a caBundle as its
// certificate alone.
func TestReadCurrentReadsInOneSessionAndPublishesNothing(t *testing.T) {
	service, access, material, _ := serviceFixture(t, declarationYAML("stored", "")+typedDeclarationYAML("ca", "caBundle"))
	ctx := context.Background()
	if _, err := service.Set(ctx, SetRequest{Name: "stored", Input: secrets.Input{ValueFile: "value"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Set(ctx, SetRequest{Name: "ca", Input: secrets.Input{CertificateFile: "c"}}); err != nil {
		t.Fatal(err)
	}
	session := access.session
	stored, _ := currentVersion(session.snapshot, "stored")
	ca, _ := currentVersion(session.snapshot, "ca")
	session.materials[ca.ID] = secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: []byte("CA CERTIFICATE"), secrets.PrivateKeyPart: []byte("CA SIGNING KEY"),
	})
	views, transactions, writes, validated := access.views, access.transactions, session.writes, material.validated
	access.unlocks = nil
	read, err := service.ReadCurrent(ctx, ReadCurrentRequest{ContextName: "fixture", Names: []string{"stored", "ca"}})
	if err != nil {
		t.Fatal(err)
	}
	defer clearBound(read)
	if access.views != views+1 || !slices.Equal(access.unlocks, []bool{true}) || access.transactions != transactions || session.writes != writes {
		t.Fatalf("the read opened %d sessions unlocking %v, %d transactions and %d writes", access.views-views, access.unlocks, access.transactions-transactions, session.writes-writes)
	}
	if len(session.bound) != 0 || len(session.snapshot.Bindings) != 0 || material.validated != validated+2 {
		t.Fatalf("the read bound %v, left bindings %v and validated %d versions", session.bound, session.snapshot.Bindings, material.validated-validated)
	}
	if len(read) != 2 || read[0].Version.ID != ca.ID || read[1].Version.ID != stored.ID {
		t.Fatalf("the read returned %+v, want the current versions of ca and stored", read)
	}
	if parts := read[0].Material.Parts(); !slices.Equal(parts, []secrets.Part{secrets.CertificatePart}) || !slices.Equal(read[0].Version.Parts, parts) {
		t.Fatalf("the caBundle came back with parts %v and names %v", parts, read[0].Version.Parts)
	}
	if certificate, _ := read[0].Material.Part(secrets.CertificatePart); string(certificate) != "CA CERTIFICATE" {
		t.Fatalf("the caBundle's certificate reads %q", certificate)
	}
	if value, _ := read[1].Material.Part(secrets.ValuePart); string(value) != "synthetic-value" {
		t.Fatalf("the stored Secret reads %q", value)
	}
}

// A read refuses exactly as a bind does: one refusal names every requested
// Secret that is missing or stale, before anything is read or validated, and
// a stored version its declaration does not validate refuses with the bind's
// own diagnostic. A store never initialized refuses before anything is read.
func TestReadCurrentNamesEveryMissingAndStaleSecret(t *testing.T) {
	service, access, material, _ := serviceFixture(t, declarationYAML("alpha", "  source: {generated: {}}\n")+declarationYAML("beta", ""))
	ctx := context.Background()
	session := access.session
	session.materials["version-beta"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-beta")})
	session.snapshot.Versions = []secretstore.Version{{ID: "version-beta", Sequence: 1, Declaration: secrets.VersionDeclaration{Name: "beta", Type: "token", Source: "contextStore", Fingerprint: "an-earlier-declaration"}, Parts: []secrets.Part{secrets.ValuePart}}}
	session.snapshot.Current = []secretstore.Current{{Name: "beta", Version: "version-beta"}}
	_, err := service.ReadCurrent(ctx, ReadCurrentRequest{ContextName: "fixture", Names: []string{"beta", "alpha"}})
	want := []diagnostics.Diagnostic{
		{Severity: "error", Code: "secret.input", Message: "Secret alpha has no stored material", Object: secretIdentity("alpha"), Remediation: "bootwright secret generate --context fixture --name alpha"},
		{Severity: "error", Code: "secret.source", Message: "Secret beta's stored material is stale for its declaration", Object: secretIdentity("beta"), Remediation: "bootwright secret set --context fixture --name beta --value-file <path>"},
	}
	if found := diagnostics.Of(err); !reflect.DeepEqual(found, want) {
		t.Fatalf("read refusal = %+v, want %+v", found, want)
	}
	if session.reads != 0 || material.validated != 0 || session.writes != 0 {
		t.Fatalf("a refused read read %d versions, validated %d and wrote %d times", session.reads, material.validated, session.writes)
	}
	_, bindErr := service.Bind(ctx, BindRequest{Names: []string{"alpha", "beta"}})
	if bound := diagnostics.Of(bindErr); !reflect.DeepEqual(bound, want) {
		t.Fatalf("the bind refused with %+v, the read with %+v", bound, want)
	}

	if _, err := service.Generate(ctx, GenerateRequest{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Set(ctx, SetRequest{Name: "beta", Input: secrets.Input{ValueFile: "value"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	material.refused = map[string]error{"alpha": diagnostics.NewFailure("secret.part", "synthetic validation refusal", "")}
	_, err = service.ReadCurrent(ctx, ReadCurrentRequest{ContextName: "fixture", Names: []string{"alpha", "beta"}})
	want = []diagnostics.Diagnostic{{Severity: "error", Code: "secret.part", Message: "synthetic validation refusal", Object: secretIdentity("alpha"), Remediation: "bootwright secret generate --context fixture --name alpha --renew"}}
	if found := diagnostics.Of(err); !reflect.DeepEqual(found, want) {
		t.Fatalf("an invalid stored version gave %+v, want %+v", found, want)
	}

	access.session = nil
	_, err = service.ReadCurrent(ctx, ReadCurrentRequest{ContextName: "fixture", Names: []string{"beta"}})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.store.uninitialized" || found[0].Remediation != "bootwright secret encryption init --context fixture" {
		t.Fatalf("an uninitialized store gave %+v", found)
	}
	_, err = service.Reopen(ctx, BindingRequest{ContextName: "fixture", BindingID: "binding-earlier"})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.store.uninitialized" ||
		found[0].Message != "the secret store of context fixture is not initialized" || found[0].Remediation != "bootwright secret encryption init --context fixture" {
		t.Fatalf("a reopen over an uninitialized store gave %+v", found)
	}
}
