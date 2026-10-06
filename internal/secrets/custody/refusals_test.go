package custody

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func typedDeclarationYAML(name, kind string) string {
	return "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + name + "\nspec:\n  type: " + kind + "\n"
}

func secretIdentity(name string) *diagnostics.ObjectIdentity {
	return &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Secret", Name: name}
}

// staleVersion makes the stored version of a Secret one an earlier
// declaration of it stored.
func staleVersion(t *testing.T, access *serviceAccess, name string) {
	t.Helper()
	current, exists := currentVersion(access.session.snapshot, name)
	if !exists {
		t.Fatalf("%s has no stored version to make stale", name)
	}
	for i := range access.session.snapshot.Versions {
		if access.session.snapshot.Versions[i].ID == current.ID {
			access.session.snapshot.Versions[i].Declaration.Fingerprint = "an-earlier-declaration"
		}
	}
}

// One bind refusal names every requested Secret it cannot bind, each with the
// command that gives it material, and reads, validates and binds nothing.
func TestBindNamesEveryMissingAndStaleSecret(t *testing.T) {
	service, access, material, _ := serviceFixture(t, declarationYAML("alpha", "  source: {generated: {}}\n")+declarationYAML("beta", ""))
	ctx := context.Background()
	session := access.session
	session.materials["version-beta"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-beta")})
	session.snapshot.Versions = []secretstore.Version{{ID: "version-beta", Sequence: 1, Declaration: secrets.VersionDeclaration{Name: "beta", Type: "token", Source: "contextStore", Fingerprint: "an-earlier-declaration"}, Parts: []secrets.Part{secrets.ValuePart}}}
	session.snapshot.Current = []secretstore.Current{{Name: "beta", Version: "version-beta"}}
	_, err := service.Bind(ctx, BindRequest{Names: []string{"beta", "alpha"}})
	want := []diagnostics.Diagnostic{
		{Severity: "error", Code: "secret.input", Message: "Secret alpha has no stored material", Object: secretIdentity("alpha"), Remediation: "bootwright secret generate --context fixture --name alpha"},
		{Severity: "error", Code: "secret.source", Message: "Secret beta's stored material is stale for its declaration", Object: secretIdentity("beta"), Remediation: "bootwright secret set --context fixture --name beta --value-file <path>"},
	}
	if found := diagnostics.Of(err); !reflect.DeepEqual(found, want) {
		t.Fatalf("bind refusal = %+v, want %+v", found, want)
	}
	if session.reads != 0 || material.validated != 0 || session.writes != 0 || len(session.bound) != 0 {
		t.Fatalf("a refused bind read %d versions, validated %d and wrote %d times", session.reads, material.validated, session.writes)
	}

	if _, err := service.Generate(ctx, GenerateRequest{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Set(ctx, SetRequest{Name: "beta", Input: secrets.Input{ValueFile: "value"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	writes := session.writes
	material.refused = map[string]error{"alpha": diagnostics.NewFailure("secret.part", "synthetic validation refusal", "")}
	_, err = service.Bind(ctx, BindRequest{Names: []string{"alpha", "beta"}})
	want = []diagnostics.Diagnostic{{Severity: "error", Code: "secret.part", Message: "synthetic validation refusal", Object: secretIdentity("alpha"), Remediation: "bootwright secret generate --context fixture --name alpha --renew"}}
	if found := diagnostics.Of(err); !reflect.DeepEqual(found, want) || session.writes != writes {
		t.Fatalf("an invalid stored version gave %+v and %d writes, want %+v and none", found, session.writes-writes, want)
	}
}

// Every refusal about a declared Secret names it and the exact command, bound
// to the context, that is the next step.
func TestSecretRefusalsNameTheSecretAndTheNextCommand(t *testing.T) {
	const setBeta = "bootwright secret set --context fixture --name beta --value-file <path>"
	invalid := func(t *testing.T, service *Service, _ *serviceAccess, material *serviceMaterial) {
		if _, err := service.Generate(context.Background(), GenerateRequest{Name: "alpha"}); err != nil {
			t.Fatal(err)
		}
		material.refused = map[string]error{"alpha": diagnostics.NewFailure("secret.part", "synthetic validation refusal", "")}
	}
	storedBeta := func(t *testing.T, service *Service, _ *serviceAccess, _ *serviceMaterial) {
		if _, err := service.Set(context.Background(), SetRequest{Name: "beta", Input: secrets.Input{ValueFile: "value"}}); err != nil {
			t.Fatal(err)
		}
	}
	staleBeta := func(t *testing.T, service *Service, access *serviceAccess, material *serviceMaterial) {
		storedBeta(t, service, access, material)
		staleVersion(t, access, "beta")
	}
	uninitialized := func(_ *testing.T, _ *Service, access *serviceAccess, _ *serviceMaterial) { access.session = nil }
	set := func(name string) func(*Service) error {
		return func(s *Service) error {
			_, err := s.Set(context.Background(), SetRequest{Name: name, Input: secrets.Input{ValueStdin: true}})
			return err
		}
	}
	show := func(name string, part secrets.Part) func(*Service) error {
		return func(s *Service) error {
			_, err := s.Show(context.Background(), ShowRequest{Name: name, Part: part})
			return err
		}
	}
	generate := func(s *Service) error {
		_, err := s.Generate(context.Background(), GenerateRequest{Name: "beta"})
		return err
	}
	deleteInvalid := func(s *Service) error {
		_, err := s.Delete(context.Background(), DeleteRequest{Name: "Not_A_Name"})
		return err
	}
	for _, test := range []struct {
		name                          string
		setup                         func(*testing.T, *Service, *serviceAccess, *serviceMaterial)
		invoke                        func(*Service) error
		code, object, message, remedy string
	}{
		{"set of a generated Secret", nil, set("alpha"), "secret.source", "alpha", "Secret alpha is generated; secret set stores only a contextStore Secret", "bootwright secret generate --context fixture --name alpha"},
		{"generate of a contextStore Secret", nil, generate, "secret.source", "beta", "Secret beta is a contextStore Secret; secret generate mints only generated Secrets", setBeta},
		{"show of an undeclared Secret", nil, show("absent", secrets.ValuePart), "secret.declaration", "absent", "Secret absent is not declared in context fixture", "bootwright secret check --context fixture"},
		{"show of a part the type lacks", nil, show("beta", secrets.PasswordPart), "secret.part", "beta", "Secret beta (token) has no password part; its parts are value", "bootwright secret show --context fixture --name beta --part value"},
		{"show of missing material", nil, show("beta", secrets.ValuePart), "secret.input", "beta", "Secret beta has no stored material", setBeta},
		{"show of stale material", staleBeta, show("beta", secrets.ValuePart), "secret.source", "beta", "Secret beta's stored material is stale for its declaration", setBeta},
		{"show of invalid material", invalid, show("alpha", secrets.ValuePart), "secret.part", "alpha", "synthetic validation refusal", "bootwright secret generate --context fixture --name alpha --renew"},
		{"show from an uninitialized store", uninitialized, show("beta", secrets.ValuePart), "secret.store.uninitialized", "beta", "secret store is not initialized", "bootwright secret encryption init --context fixture"},
		{"stdin replacement without --yes", storedBeta, set("beta"), "secret.input", "beta", "stdin replacement requires --yes before reading material", "repeat the command with --yes after reviewing that Secret beta is replaced"},
		{"delete of an invalid name", nil, deleteInvalid, "secret.declaration", "", "secret name is invalid", "bootwright secret list --context fixture"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, access, material, _ := serviceFixture(t, declarationYAML("alpha", "  source: {generated: {}}\n")+declarationYAML("beta", ""))
			if test.setup != nil {
				test.setup(t, service, access, material)
			}
			var object *diagnostics.ObjectIdentity
			if test.object != "" {
				object = secretIdentity(test.object)
			}
			want := []diagnostics.Diagnostic{{Severity: "error", Code: test.code, Message: test.message, Object: object, Remediation: test.remedy}}
			if found := diagnostics.Of(test.invoke(service)); !reflect.DeepEqual(found, want) {
				t.Fatalf("refusal = %+v, want %+v", found, want)
			}
		})
	}

	t.Run("check", func(t *testing.T) {
		service, access, material, _ := serviceFixture(t, declarationYAML("alpha", "  source: {generated: {}}\n")+declarationYAML("beta", "")+declarationYAML("gamma", "  source: {generated: {}}\n")+declarationYAML("delta", ""))
		ctx := context.Background()
		if _, err := service.Generate(ctx, GenerateRequest{Name: "alpha"}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"beta", "delta"} {
			if _, err := service.Set(ctx, SetRequest{Name: name, Input: secrets.Input{ValueFile: "value"}}); err != nil {
				t.Fatal(err)
			}
		}
		staleVersion(t, access, "beta")
		material.refused = map[string]error{"alpha": diagnostics.NewFailure("secret.part", "synthetic validation refusal", "")}
		result, err := service.Check(ctx, CheckRequest{})
		want := []diagnostics.Diagnostic{
			{Severity: "error", Code: "secret.input", Message: "Secret gamma has no stored material", Object: secretIdentity("gamma"), Remediation: "bootwright secret generate --context fixture --name gamma"},
			{Severity: "error", Code: "secret.part", Message: "synthetic validation refusal", Object: secretIdentity("alpha"), Remediation: "bootwright secret generate --context fixture --name alpha --renew"},
			{Severity: "error", Code: "secret.source", Message: "Secret beta's stored material is stale for its declaration", Object: secretIdentity("beta"), Remediation: setBeta},
		}
		if found := diagnostics.Of(err); !reflect.DeepEqual(found, want) {
			t.Fatalf("check diagnostics = %+v, want %+v", found, want)
		}
		statuses := map[string]string{}
		for _, row := range result.Secrets {
			statuses[row.Name] = row.Status
		}
		if !reflect.DeepEqual(statuses, map[string]string{"alpha": "invalid", "beta": "stale", "delta": "available", "gamma": "missing"}) {
			t.Fatalf("check rows = %v", statuses)
		}
	})
}

// A flag set the declared type does not take is a mistake in how secret set
// was invoked: one refusal names the Secret, its type and the flags it takes,
// before any material is read or the store is opened.
func TestSetShapeMistakesAreUsageRefusalsNamingTheType(t *testing.T) {
	const values = "--value-file <path> or --value-stdin"
	value := []secrets.Input{{}, {CertificateFile: "c"}, {ValueFile: "v", ValueStdin: true}, {ValueFile: "v", Username: "u"}, {Provided: secrets.CertificateFileInput, ValueFile: "v"}}
	types := []struct {
		name, kind, takes, remedy string
		valid                     secrets.Input
		wrong                     []secrets.Input
	}{
		{"opaque", "opaque", values, "--value-file <path>", secrets.Input{ValueFile: "v"}, value},
		{"token", "token", values, "--value-file <path>", secrets.Input{ValueStdin: true}, value},
		{"docker", "dockerConfigJson", values, "--value-file <path>", secrets.Input{ValueFile: "v"}, value},
		{
			"password", "usernamePassword", "--username <username> with --password-file <path> or --password-stdin", "--username <username> --password-stdin",
			secrets.Input{Username: "operator", PasswordStdin: true},
			[]secrets.Input{{}, {ValueFile: "v"}, {Username: "operator"}, {PasswordStdin: true}, {Username: "operator", PasswordFile: "p", PasswordStdin: true}, {Provided: secrets.ValueFileInput, Username: "operator", PasswordStdin: true}},
		},
		{
			"ca", "caBundle", "--certificate-file <path>", "--certificate-file <path>", secrets.Input{CertificateFile: "c"},
			[]secrets.Input{{}, {ValueFile: "v"}, {CertificateFile: "c", PrivateKeyFile: "k"}, {Provided: secrets.PublicKeyFileInput, CertificateFile: "c"}},
		},
		{
			"tls", "tlsCertificate", "--certificate-file <path> --private-key-file <path>", "--certificate-file <path> --private-key-file <path>", secrets.Input{CertificateFile: "c", PrivateKeyFile: "k"},
			[]secrets.Input{{}, {CertificateFile: "c"}, {PrivateKeyFile: "k"}, {ValueStdin: true}, {Provided: secrets.ValueStdinInput, CertificateFile: "c", PrivateKeyFile: "k"}},
		},
		{
			"ssh", "sshKeyPair", "--private-key-file <path> [--public-key-file <path>]", "--private-key-file <path> [--public-key-file <path>]", secrets.Input{PrivateKeyFile: "k"},
			[]secrets.Input{{}, {PublicKeyFile: "p"}, {CertificateFile: "c", PrivateKeyFile: "k"}, {Provided: secrets.UsernameInput, PrivateKeyFile: "k"}},
		},
	}
	yaml := ""
	for _, declared := range types {
		yaml += typedDeclarationYAML(declared.name, declared.kind)
	}
	service, access, material, _ := serviceFixture(t, yaml)
	refused := func(t *testing.T, name string, input secrets.Input, message, remedy string) {
		t.Helper()
		transactions := access.transactions
		_, err := service.Set(context.Background(), SetRequest{Name: name, Input: input})
		want := []diagnostics.Diagnostic{{Severity: "error", Code: "secret.input", Message: message, Object: secretIdentity(name), Remediation: remedy}}
		if found := diagnostics.Of(err); !reflect.DeepEqual(found, want) || !diagnostics.IsUsage(err) {
			t.Fatalf("%+v gave %+v (usage %t), want the usage refusal %+v", input, found, diagnostics.IsUsage(err), want)
		}
		if material.acquired != 0 || access.transactions != transactions {
			t.Fatalf("%+v acquired material or opened the store", input)
		}
	}
	for _, declared := range types {
		t.Run(declared.kind, func(t *testing.T) {
			remedy := "bootwright secret set --context fixture --name " + declared.name + " " + declared.remedy
			for _, input := range declared.wrong {
				refused(t, declared.name, input, "Secret "+declared.name+" is of type "+declared.kind+"; secret set takes "+declared.takes, remedy)
			}
		})
	}
	usernames := []string{"two words", "user:name", "line\nbreak", string([]byte{0xff})}
	for _, username := range usernames {
		refused(t, "password", secrets.Input{Username: username, PasswordStdin: true},
			"Secret password takes a --username that is one nonempty UTF-8 line of at most 1 MiB with no whitespace or colon",
			"bootwright secret set --context fixture --name password --username <username> --password-stdin")
	}
	for _, declared := range types {
		if _, err := service.Set(context.Background(), SetRequest{Name: declared.name, Input: declared.valid}); err != nil {
			t.Fatalf("%s: a flag set its type takes was refused: %+v", declared.kind, diagnostics.Of(err))
		}
	}
}

// Generate names the generated Secrets it changed and those it left current.
func TestGenerateReportsChangedAndUnchangedNames(t *testing.T) {
	service, _, _, _ := serviceFixture(t, declarationYAML("b", "  source: {generated: {}}\n")+declarationYAML("a", "  source: {generated: {}}\n")+declarationYAML("stored", ""))
	ctx := context.Background()
	if _, err := service.Generate(ctx, GenerateRequest{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Generate(ctx, GenerateRequest{})
	if err != nil || result.Changed != 1 || result.Unchanged != 1 || !slices.Equal(result.ChangedNames, []string{"b"}) || !slices.Equal(result.UnchangedNames, []string{"a"}) || len(result.Parts) != 0 {
		t.Fatalf("generate = %+v (%v), want b changed and a unchanged", result, err)
	}
	result, err = service.Generate(ctx, GenerateRequest{Renew: true})
	if err != nil || !slices.Equal(result.ChangedNames, []string{"a", "b"}) || len(result.UnchangedNames) != 0 || result.Changed != 2 {
		t.Fatalf("renewal = %+v (%v), want a and b changed", result, err)
	}
}

// A replacement or a delete that was not confirmed, because the operator
// declined it or no terminal could ask, names the Secret and the command that
// repeats it with --yes, bound to the context, and writes nothing.
func TestAnUnconfirmedChangeNamesTheSecretAndTheCommandWithYes(t *testing.T) {
	const review = "review it with bootwright secret check --context fixture, then run "
	ctx := context.Background()
	for name, decline := range map[string]func(*Service, *serviceConfirmer){
		"declined":     func(_ *Service, c *serviceConfirmer) { c.decline = errors.New("confirmation was declined") },
		"not asked":    func(_ *Service, c *serviceConfirmer) { c.decline = errors.New("requires interactive input") },
		"no confirmer": func(s *Service, _ *serviceConfirmer) { s.confirmer = nil },
	} {
		t.Run(name, func(t *testing.T) {
			service, access, _, confirmer := serviceFixture(t, declarationYAML("token", ""))
			if _, err := service.Set(ctx, SetRequest{Name: "token", Input: secrets.Input{ValueFile: "value"}}); err != nil {
				t.Fatal(err)
			}
			decline(service, confirmer)
			writes := access.session.writes
			for _, probe := range []struct {
				run  func() error
				want diagnostics.Diagnostic
			}{
				{
					run: func() error {
						_, err := service.Set(ctx, SetRequest{Name: "token", Input: secrets.Input{ValueFile: "other"}})
						return err
					},
					want: diagnostics.Diagnostic{Severity: "error", Code: "secret.store.conflict", Message: "Secret token was not replaced: the change was declined or could not be confirmed at a terminal",
						Object: secretIdentity("token"), Remediation: review + "bootwright secret set --context fixture --name token --value-file <path> --yes"},
				},
				{
					run: func() error {
						_, err := service.Delete(ctx, DeleteRequest{Name: "token"})
						return err
					},
					want: diagnostics.Diagnostic{Severity: "error", Code: "secret.store.conflict", Message: "Secret token was not deleted: the change was declined or could not be confirmed at a terminal",
						Object: secretIdentity("token"), Remediation: review + "bootwright secret delete --context fixture --name token --yes"},
				},
			} {
				if found := diagnostics.Of(probe.run()); !reflect.DeepEqual(found, []diagnostics.Diagnostic{probe.want}) {
					t.Errorf("unconfirmed change = %+v, want %+v", found, probe.want)
				}
			}
			if access.session.writes != writes {
				t.Fatalf("an unconfirmed change wrote %d times", access.session.writes-writes)
			}
		})
	}
}

// A delete of a Secret with no current version changes nothing and asks
// nothing.
func TestDeleteOfAnAbsentSecretChangesNothing(t *testing.T) {
	service, access, _, confirmer := serviceFixture(t, declarationYAML("token", ""))
	result, err := service.Delete(context.Background(), DeleteRequest{Name: "token"})
	if err != nil || result.Name != "token" || result.Changed != 0 || result.Unchanged != 1 || len(result.Parts) != 0 || confirmer.calls != 0 || access.session.writes != 0 {
		t.Fatalf("delete = %+v (%v), confirmations %d, writes %d", result, err, confirmer.calls, access.session.writes)
	}
	if _, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueFile: "value"}}); err != nil {
		t.Fatal(err)
	}
	result, err = service.Delete(context.Background(), DeleteRequest{Name: "token"})
	if err != nil || result.Changed != 1 || result.Unchanged != 0 || len(result.Parts) != 0 || confirmer.calls != 1 {
		t.Fatalf("delete = %+v (%v), confirmations %d", result, err, confirmer.calls)
	}
}
