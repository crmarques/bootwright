package custody

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

type serviceAccess struct {
	snapshot       storage.ContextSnapshot
	session        *serviceSession
	transactions   int
	contextFailure error
	failure        error
}

func (a *serviceAccess) Context(context.Context, string) (storage.ContextSnapshot, error) {
	return a.snapshot, a.contextFailure
}
func (a *serviceAccess) View(_ context.Context, _ storage.Context, _ bool, callback func(storage.StoreSession, storage.Selection) error) error {
	if a.failure != nil {
		return a.failure
	}
	if a.session == nil {
		return callback(nil, storage.Selection{})
	}
	return callback(a.session, storage.Selection{})
}
func (a *serviceAccess) Mutate(_ context.Context, _ storage.Context, callback func(storage.StoreSession, storage.Selection) error) error {
	a.transactions++
	if a.failure != nil {
		return a.failure
	}
	if a.session == nil {
		return storage.Failure("store.uninitialized", "uninitialized")
	}
	return callback(a.session, storage.Selection{})
}

type serviceSession struct {
	storage.StoreSession
	snapshot      storage.Snapshot
	materials     map[string]secrets.Material
	reads, writes int
}

func (s *serviceSession) Inspect(context.Context) (storage.Snapshot, error) { return s.snapshot, nil }
func (s *serviceSession) Read(_ context.Context, id string) (secrets.Material, error) {
	s.reads++
	m, ok := s.materials[id]
	if !ok {
		return secrets.Material{}, storage.Failure("store.corrupt", "missing version")
	}
	parts := map[secrets.Part][]byte{}
	for _, p := range m.Parts() {
		parts[p], _ = m.Part(p)
	}
	return secrets.NewMaterial(parts), nil
}
func (s *serviceSession) PutBatch(_ context.Context, puts []storage.Put) ([]storage.Version, error) {
	s.writes++
	versions := []storage.Version{}
	for _, put := range puts {
		id := fmt.Sprintf("version-%d", len(s.materials))
		parts := map[secrets.Part][]byte{}
		for _, p := range put.Material.Parts() {
			parts[p], _ = put.Material.Part(p)
		}
		s.materials[id] = secrets.NewMaterial(parts)
		version := storage.Version{ID: id, Declaration: put.Declaration.Summary(), Parts: put.Material.Parts()}
		s.snapshot.Versions = append(s.snapshot.Versions, version)
		found := false
		for i := range s.snapshot.Current {
			if s.snapshot.Current[i].Name == put.Declaration.Name {
				s.snapshot.Current[i].Version = id
				found = true
			}
		}
		if !found {
			s.snapshot.Current = append(s.snapshot.Current, storage.Current{Name: put.Declaration.Name, Version: id})
		}
		versions = append(versions, version)
	}
	return versions, nil
}
func (s *serviceSession) Delete(_ context.Context, name string) (bool, error) {
	s.writes++
	for i, c := range s.snapshot.Current {
		if c.Name == name {
			s.snapshot.Current = append(s.snapshot.Current[:i], s.snapshot.Current[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

type serviceMaterial struct {
	acquired, files, generated, validated int
	failAt                                int
}

func (m *serviceMaterial) Acquire(context.Context, secrets.Declaration, secrets.Input) (secrets.Material, error) {
	m.acquired++
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-value")}), nil
}
func (m *serviceMaterial) File(context.Context, secrets.Declaration) (secrets.Material, error) {
	m.files++
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-file")}), nil
}
func (m *serviceMaterial) Generate(context.Context, secrets.Declaration) (secrets.Material, error) {
	m.generated++
	if m.generated == m.failAt {
		return secrets.Material{}, errors.New("generation failed")
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-generated")}), nil
}
func (m *serviceMaterial) Validate(context.Context, secrets.Declaration, secrets.Material) error {
	m.validated++
	return nil
}

type serviceConfirmer struct{ calls int }

func (c *serviceConfirmer) Confirm(context.Context, string, string) error { c.calls++; return nil }

func serviceFixture(t *testing.T, secretYAML string) (*Service, *serviceAccess, *serviceMaterial, *serviceConfirmer) {
	t.Helper()
	content := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: fixture\nspec:\n  domains:\n    base: example.test\n" + secretYAML
	access := &serviceAccess{snapshot: storage.ContextSnapshot{Context: storage.Context{Name: "fixture", ID: "ctx-fixture", Revision: "rev-fixture", Mode: "ready"}, Inputs: desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}}}, session: &serviceSession{materials: map[string]secrets.Material{}}}
	material := &serviceMaterial{}
	confirmer := &serviceConfirmer{}
	compiler := compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: secrets.Normalize, Validate: secrets.Validate})
	service := New(access, compiler, material, confirmer)
	if _, _, err := service.resolve(context.Background(), ""); err != nil {
		t.Fatalf("fixture admission: %v %+v", err, desiredstate.DiagnosticsOf(err))
	}
	return service, access, material, confirmer
}
func declarationYAML(name, source string) string {
	return "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + name + "\nspec:\n  type: token\n" + source
}

func TestMissingDesiredStateStopsBeforeMaterialOrStoreMutation(t *testing.T) {
	service, access, material, confirmer := serviceFixture(t, declarationYAML("token", ""))
	access.snapshot.Context.Revision = ""
	access.snapshot.Inputs = desiredstate.Sources{}
	_, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}})
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "context.input" || material.acquired != 0 || access.transactions != 0 || confirmer.calls != 0 {
		t.Fatal("empty context did not stop before secret effects", err, diagnostics)
	}
}

func TestSetAuthorizationBeforeInputAndSameMaterial(t *testing.T) {
	s, a, m, c := serviceFixture(t, declarationYAML("token", ""))
	ctx := context.Background()
	request := SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}}
	first, err := s.Set(ctx, request)
	if err != nil || first.Changed != 1 || c.calls != 0 {
		t.Fatal(first, err, c.calls)
	}
	if _, err := s.Set(ctx, request); err == nil || m.acquired != 1 || c.calls != 0 {
		t.Fatal("stdin replacement read or prompted before yes")
	}
	request.SkipConfirmation = true
	second, err := s.Set(ctx, request)
	if err != nil || second.Unchanged != 1 || a.session.writes != 1 {
		t.Fatal("equal material created a version", second, err)
	}
	request.Input = secrets.Input{ValueFile: "explicit"}
	request.SkipConfirmation = false
	if _, err := s.Set(ctx, request); err != nil || c.calls != 1 {
		t.Fatal("replacement did not confirm", err, c.calls)
	}
	request.Name = "absent"
	reads := m.acquired
	if _, err := s.Set(ctx, request); err == nil || m.acquired != reads {
		t.Fatal("undeclared secret acquired material")
	}
	a.snapshot.Context.Mode = "deleting"
	request.Name = "token"
	transactions := a.transactions
	if _, err := s.Set(ctx, request); err == nil || a.transactions != transactions {
		t.Fatal("deleting-context set reached mutation")
	}
}

func TestStoreAccessFailuresStopCustodyBeforeMaterial(t *testing.T) {
	operations := map[string]func(*Service) error{
		"set": func(service *Service) error {
			_, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}})
			return err
		},
		"generate": func(service *Service) error {
			_, err := service.Generate(context.Background(), GenerateRequest{Name: "generated"})
			return err
		},
		"check": func(service *Service) error {
			_, err := service.Check(context.Background(), CheckRequest{})
			return err
		},
		"list": func(service *Service) error {
			_, err := service.List(context.Background(), ListRequest{})
			return err
		},
		"show": func(service *Service) error {
			_, err := service.Show(context.Background(), ShowRequest{Name: "token", Part: secrets.ValuePart})
			return err
		},
		"delete": func(service *Service) error {
			_, err := service.Delete(context.Background(), DeleteRequest{Name: "token"})
			return err
		},
	}
	for name, invoke := range operations {
		for _, stage := range []string{"context", "session"} {
			t.Run(name+"/"+stage, func(t *testing.T) {
				service, access, material, confirmer := serviceFixture(t, declarationYAML("token", "")+declarationYAML("generated", "  source: {generated: {}}\n"))
				want := storage.Failure("store.conflict", "injected access refused context")
				if stage == "context" {
					access.contextFailure = want
				} else {
					access.failure = want
				}
				if err := invoke(service); !errors.Is(err, want) {
					t.Fatal("access failure was not preserved", err)
				}
				if material.acquired != 0 || material.generated != 0 || material.files != 0 || material.validated != 0 || confirmer.calls != 0 || access.session.reads != 0 || access.session.writes != 0 {
					t.Fatal("access failure allowed secret material or confirmation effects")
				}
				if stage == "context" && access.transactions != 0 {
					t.Fatal("context failure reached mutation")
				}
			})
		}
	}
}

func TestGeneratedBatchFailurePublishesNothing(t *testing.T) {
	s, a, m, _ := serviceFixture(t, declarationYAML("a", "  source: {generated: {}}\n")+declarationYAML("b", "  source: {generated: {}}\n"))
	m.failAt = 2
	if _, err := s.Generate(context.Background(), GenerateRequest{}); err == nil || a.session.writes != 0 {
		t.Fatal("partial generation published")
	}
	m.failAt = 0
	result, err := s.Generate(context.Background(), GenerateRequest{})
	if err != nil || result.Changed != 2 || a.session.writes != 1 {
		t.Fatal(result, err)
	}
	result, err = s.Generate(context.Background(), GenerateRequest{})
	if err != nil || result.Unchanged != 2 || a.session.writes != 1 {
		t.Fatal(result, err)
	}
	result, err = s.Generate(context.Background(), GenerateRequest{Name: "a", Renew: true})
	if err != nil || result.Changed != 1 || a.session.writes != 2 {
		t.Fatal(result, err)
	}
}

func TestStoredDeclarationSummaryRetainsStalenessIdentity(t *testing.T) {
	for _, change := range []string{"generation-parameters", "origin-path", "document-position"} {
		t.Run(change, func(t *testing.T) {
			service, access, material, _ := serviceFixture(t, declarationYAML("token", "  source: {generated: {bytes: 32}}\n"))
			ctx := context.Background()
			if _, err := service.Generate(ctx, GenerateRequest{}); err != nil {
				t.Fatal(err)
			}
			original := access.session.snapshot.Versions[0].Declaration
			encoded, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 4 {
				t.Fatal("stored declaration retained acquisition fields", string(encoded), err)
			}
			for _, field := range []string{"name", "type", "source", "fingerprint"} {
				if _, exists := fields[field]; !exists {
					t.Fatal("stored declaration lost required metadata", field)
				}
			}
			if strings.Contains(string(encoded), "/synthetic") {
				t.Fatal("stored declaration retained its acquisition path")
			}
			if result, err := service.Check(ctx, CheckRequest{}); err != nil || len(result.Secrets) != 1 || result.Secrets[0].Status != "available" {
				t.Fatal("unchanged summary lost available material", result, err)
			}
			source := access.snapshot.Inputs.Files[0]
			path, content := source.Path(), string(source.Bytes())
			switch change {
			case "generation-parameters":
				content = strings.Replace(content, "bytes: 32", "bytes: 48", 1)
			case "origin-path":
				path = "/synthetic/relocated.yaml"
			case "document-position":
				environment, secret, ok := strings.Cut(content, "\n---\n")
				if !ok {
					t.Fatal("fixture lacks separate declarations")
				}
				content = secret + "\n---\n" + environment
			}
			access.snapshot.Inputs.Files = []desiredstate.SourceFile{desiredstate.NewSourceFile(path, []byte(content))}
			reads, validations := access.session.reads, material.validated
			result, err := service.Check(ctx, CheckRequest{})
			if err == nil || result == nil || len(result.Secrets) != 1 || result.Secrets[0].Status != "stale" || access.session.reads != reads || material.validated != validations {
				t.Fatal("changed complete declaration did not preserve stale refusal", result, err)
			}
			if result, err := service.Generate(ctx, GenerateRequest{}); err != nil || result.Changed != 1 {
				t.Fatal("changed declaration did not renew its material", result, err)
			}
			updated, exists := currentVersion(access.session.snapshot, "token")
			if !exists || updated.Declaration.Fingerprint == original.Fingerprint {
				t.Fatal("renewed summary lost the new declaration identity")
			}
		})
	}
}

func TestCheckNegativeResultListMetadataAndAllSourceReveal(t *testing.T) {
	s, a, m, _ := serviceFixture(t, declarationYAML("absent", "")+declarationYAML("file", "  source: {file: {path: secrets/token}}\n"))
	a.session = nil
	result, err := s.Check(context.Background(), CheckRequest{})
	if err == nil || result == nil || len(result.Secrets) != 2 || result.Secrets[0].Status != "missing" || result.Secrets[1].Status != "available" {
		t.Fatal(result, err)
	}
	if m.files != 1 {
		t.Fatal("check did not read live file")
	}
	list, err := s.List(context.Background(), ListRequest{})
	if err != nil || len(list.Secrets) != 0 || m.files != 1 {
		t.Fatal("metadata list acquired file material", err)
	}
	show, err := s.Show(context.Background(), ShowRequest{Name: "file", Part: secrets.ValuePart})
	if err != nil {
		t.Fatal(err)
	}
	defer show.Material.Clear()
	if m.files != 2 {
		t.Fatal("show did not re-open live file")
	}
	if _, err := s.Show(context.Background(), ShowRequest{Name: "file", Part: secrets.PrivateKeyPart}); err == nil || m.files != 2 {
		t.Fatal("invalid reveal part acquired material")
	}
	a.failure = storage.Failure("store.corrupt", "tampered")
	result, err = s.Check(context.Background(), CheckRequest{})
	if err == nil || result != nil {
		t.Fatal("corrupt store produced trustworthy partial result")
	}
}

func TestInputMatrixRejectsInapplicableFlags(t *testing.T) {
	inputs := map[string]secrets.Input{
		"opaque": {ValueStdin: true}, "token": {ValueFile: "v"}, "dockerConfigJson": {ValueFile: "v"},
		"usernamePassword": {Username: "operator", PasswordStdin: true}, "caBundle": {CertificateFile: "c"},
		"tlsCertificate": {CertificateFile: "c", PrivateKeyFile: "k"}, "sshKeyPair": {PrivateKeyFile: "k", PublicKeyFile: "p"},
	}
	for kind, input := range inputs {
		if err := validateInput(kind, input); err != nil {
			t.Fatal(kind, err)
		}
		explicitEmpty := input
		explicitEmpty.Provided = ^secrets.AllowedInputFields(kind)
		if err := validateInput(kind, explicitEmpty); err == nil {
			t.Fatal("explicitly empty inapplicable flag accepted", kind)
		}
		input.Username = "extra"
		if kind == "usernamePassword" {
			input.PublicKeyFile = "extra"
		}
		if err := validateInput(kind, input); err == nil {
			t.Fatal("inapplicable flag accepted", kind)
		}
	}
}

func TestGeneratedDeclarationDefaultsRemainTypeScoped(t *testing.T) {
	for _, kind := range []string{"token", "usernamePassword", "caBundle", "tlsCertificate", "sshKeyPair"} {
		t.Run(kind, func(t *testing.T) {
			parameters := "{}"
			if kind == "caBundle" || kind == "tlsCertificate" {
				parameters = "{commonName: fixture.example.test}"
			}
			document := "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: fixture-secret\nspec:\n  type: " + kind + "\n  source: {generated: " + parameters + "}\n"
			service, _, _, _ := serviceFixture(t, document)
			_, declarations, err := service.resolve(context.Background(), "")
			if err != nil || len(declarations) != 1 {
				t.Fatal(err)
			}
			want := 0
			if kind == "token" {
				want = 32
			}
			if declarations[0].Generation.Bytes != want {
				t.Fatal("token entropy default leaked across generation types")
			}
		})
	}
}
