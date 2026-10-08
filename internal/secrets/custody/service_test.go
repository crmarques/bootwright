package custody

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type serviceAccess struct {
	snapshot       secretstore.ContextSnapshot
	session        *serviceSession
	transactions   int
	contextFailure error
	failure        error
	// unlocks records the unlock argument of every View, so a test can prove
	// a read acquired no unlock material.
	unlocks []bool
	// contexts and views count Context and View calls; areas lists every area
	// MutateArea was handed.
	contexts, views int
	areas           []secretstore.Area
}

func (a *serviceAccess) MutateArea(_ context.Context, _ secretstore.Context, area secretstore.Area, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.areas = append(a.areas, area)
	if a.failure != nil {
		return a.failure
	}
	if a.session == nil {
		return callback(nil, secretstore.Selection{})
	}
	return callback(a.session, secretstore.Selection{})
}

func (a *serviceAccess) Context(context.Context, string) (secretstore.ContextSnapshot, error) {
	a.contexts++
	return a.snapshot, a.contextFailure
}
func (a *serviceAccess) View(_ context.Context, _ secretstore.Context, unlock bool, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.views++
	a.unlocks = append(a.unlocks, unlock)
	if a.failure != nil {
		return a.failure
	}
	if a.session == nil {
		return callback(nil, secretstore.Selection{})
	}
	return callback(a.session, secretstore.Selection{})
}
func (a *serviceAccess) Mutate(_ context.Context, _ secretstore.Context, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.transactions++
	if a.failure != nil {
		return a.failure
	}
	if a.session == nil {
		return secretstore.Failure("store.uninitialized", "uninitialized")
	}
	return callback(a.session, secretstore.Selection{})
}

type serviceSession struct {
	secretstore.StoreSession
	snapshot      secretstore.Snapshot
	materials     map[string]secrets.Material
	reads, writes int
	// bound lists every input a Bind received; reopened is what Reopen serves.
	bound    []secretstore.BoundInput
	reopened []secretstore.BoundMaterial
}

func (s *serviceSession) Bind(_ context.Context, inputs []secretstore.BoundInput) (secretstore.Binding, error) {
	s.writes++
	binding := secretstore.Binding{ID: fmt.Sprintf("binding-%d", len(s.snapshot.Bindings))}
	for _, input := range inputs {
		s.bound = append(s.bound, secretstore.BoundInput{Declaration: input.Declaration, Version: input.Version})
		binding.Versions = append(binding.Versions, input.Version)
	}
	s.snapshot.Bindings = append(s.snapshot.Bindings, binding)
	return binding, nil
}

func (s *serviceSession) Reopen(_ context.Context, id string) ([]secretstore.BoundMaterial, error) {
	s.reads++
	for _, binding := range s.snapshot.Bindings {
		if binding.ID == id {
			return s.reopened, nil
		}
	}
	return nil, secretstore.Failure("input", "secret binding does not exist")
}

func (s *serviceSession) Inspect(context.Context) (secretstore.Snapshot, error) {
	return s.snapshot, nil
}
func (s *serviceSession) Read(_ context.Context, id string) (secrets.Material, error) {
	s.reads++
	m, ok := s.materials[id]
	if !ok {
		return secrets.Material{}, secretstore.Failure("store.corrupt", "missing version")
	}
	parts := map[secrets.Part][]byte{}
	for _, p := range m.Parts() {
		parts[p], _ = m.Part(p)
	}
	return secrets.NewMaterial(parts), nil
}
func (s *serviceSession) PutBatch(_ context.Context, puts []secretstore.Put) ([]secretstore.Version, error) {
	s.writes++
	versions := []secretstore.Version{}
	for _, put := range puts {
		id := fmt.Sprintf("version-%d", len(s.materials))
		parts := map[secrets.Part][]byte{}
		for _, p := range put.Material.Parts() {
			parts[p], _ = put.Material.Part(p)
		}
		s.materials[id] = secrets.NewMaterial(parts)
		version := secretstore.Version{ID: id, Declaration: put.Declaration.Summary(), Parts: put.Material.Parts()}
		s.snapshot.Versions = append(s.snapshot.Versions, version)
		found := false
		for i := range s.snapshot.Current {
			if s.snapshot.Current[i].Name == put.Declaration.Name {
				s.snapshot.Current[i].Version = id
				found = true
			}
		}
		if !found {
			s.snapshot.Current = append(s.snapshot.Current, secretstore.Current{Name: put.Declaration.Name, Version: id})
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
	acquired, generated, validated int
	failAt                         int
	// refused is what Validate returns for the Secret of each name.
	refused map[string]error
}

func (m *serviceMaterial) Acquire(context.Context, secrets.Declaration, secrets.Input) (secrets.Material, error) {
	m.acquired++
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-value")}), nil
}
func (m *serviceMaterial) Generate(context.Context, secrets.Declaration) (secrets.Material, error) {
	m.generated++
	if m.generated == m.failAt {
		return secrets.Material{}, errors.New("generation failed")
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-generated")}), nil
}
func (m *serviceMaterial) Validate(_ context.Context, d secrets.Declaration, _ secrets.Material) error {
	m.validated++
	return m.refused[d.Name]
}

type serviceConfirmer struct {
	calls   int
	decline error
}

func (c *serviceConfirmer) Confirm(context.Context, string, string) error {
	c.calls++
	return c.decline
}

func serviceFixture(t *testing.T, secretYAML string) (*Service, *serviceAccess, *serviceMaterial, *serviceConfirmer) {
	t.Helper()
	service, access, material, confirmer := unresolvedFixture(secretYAML)
	if _, _, err := service.resolve(context.Background(), ""); err != nil {
		t.Fatalf("fixture admission: %v %+v", err, diagnostics.Of(err))
	}
	return service, access, material, confirmer
}

type countingCompiler struct {
	Compiler
	calls int
}

func (c *countingCompiler) Compile(ctx context.Context, sources desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	c.calls++
	return c.Compiler.Compile(ctx, sources)
}

func unresolvedFixture(secretYAML string) (*Service, *serviceAccess, *serviceMaterial, *serviceConfirmer) {
	content := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: fixture\nspec:\n  controller: {machineRef: controller}\n  domains:\n    base: example.test\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: controller}\nspec:\n  os: {provided: true}\n  access: {local: true}\n" + secretYAML
	access := &serviceAccess{snapshot: secretstore.ContextSnapshot{Context: secretstore.Context{Name: "fixture", Revision: "rev-fixture", Mode: "ready"}, Inputs: desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content))}}}, session: &serviceSession{materials: map[string]secrets.Material{}}}
	material := &serviceMaterial{}
	confirmer := &serviceConfirmer{}
	compiler := &countingCompiler{Compiler: compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: secrets.Normalize, Validate: secrets.Validate})}
	return New(access, compiler, material, confirmer), access, material, confirmer
}
func declarationYAML(name, source string) string {
	return "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + name + "\nspec:\n  type: token\n" + source
}

func TestMissingDesiredStateStopsBeforeMaterialOrStoreMutation(t *testing.T) {
	service, access, material, confirmer := serviceFixture(t, declarationYAML("token", ""))
	access.snapshot.Context.Revision = ""
	access.snapshot.Inputs = desiredstate.Sources{}
	_, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueStdin: true}})
	diagnostics := diagnostics.Of(err)
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
				want := secretstore.Failure("store.conflict", "injected access refused context")
				if stage == "context" {
					access.contextFailure = want
				} else {
					access.failure = want
				}
				if err := invoke(service); !errors.Is(err, want) {
					t.Fatal("access failure was not preserved", err)
				}
				if material.acquired != 0 || material.generated != 0 || material.validated != 0 || confirmer.calls != 0 || access.session.reads != 0 || access.session.writes != 0 {
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

// A declaration's fingerprint covers its type, source and parameters (D71): a
// changed parameter stales its stored version, while the same declaration read
// from another path or document position keeps it current.
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
			reads, validations, generated := access.session.reads, material.validated, material.generated
			result, err := service.Check(ctx, CheckRequest{})
			if change != "generation-parameters" {
				if err != nil || result == nil || len(result.Secrets) != 1 || result.Secrets[0].Status != "available" {
					t.Fatal("the same declaration read from another place lost its material", result, err)
				}
				renewed, err := service.Generate(ctx, GenerateRequest{})
				if err != nil || renewed.Changed != 0 || renewed.Unchanged != 1 || material.generated != generated {
					t.Fatal("the same declaration read from another place re-minted its material", renewed, err)
				}
				if current, exists := currentVersion(access.session.snapshot, "token"); !exists || current.Declaration != original {
					t.Fatal("the stored version was replaced")
				}
				return
			}
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

// legacyFingerprint is the digest an earlier build stored: the declaration's
// canonical JSON with the path and document index it was read from.
func legacyFingerprint(t *testing.T, d secrets.Declaration, path string, document int) string {
	t.Helper()
	d.Origin, d.Document, d.Fingerprint = path, document, ""
	d.LegacyFingerprint, d.LegacyOrigin, d.LegacyDocument = "", "", 0
	canonical, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

// A version an earlier build stored under the fingerprint that covered its
// declaring path and document stays current for every command while that place
// is unchanged, and a version matching neither digest is stale (D71).
func TestAVersionStoredUnderTheLegacyFingerprintStaysCurrent(t *testing.T) {
	for _, stored := range []string{"legacy", "neither"} {
		t.Run(stored, func(t *testing.T) {
			service, access, material, _ := serviceFixture(t, declarationYAML("generated", "  source: {generated: {bytes: 32}}\n")+declarationYAML("stored", ""))
			ctx := context.Background()
			_, declarations, err := service.resolve(ctx, "")
			if err != nil || len(declarations) != 2 {
				t.Fatal(declarations, err)
			}
			session := access.session
			for index, d := range declarations {
				if d.Origin != "" || d.Document != 0 || d.LegacyOrigin != "/synthetic/environment.yaml" {
					t.Fatalf("declaration %s carries its provenance in its fingerprint: %+v", d.Name, d)
				}
				fingerprint := legacyFingerprint(t, d, "/synthetic/environment.yaml", d.LegacyDocument)
				if fingerprint != d.LegacyFingerprint || fingerprint == d.Fingerprint {
					t.Fatalf("legacy fingerprint of %s = %s, want %s", d.Name, d.LegacyFingerprint, fingerprint)
				}
				if stored == "neither" {
					fingerprint = legacyFingerprint(t, d, "/synthetic/moved.yaml", d.LegacyDocument)
				}
				id := fmt.Sprintf("version-legacy-%d", index)
				session.materials[id] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-value")})
				session.snapshot.Versions = append(session.snapshot.Versions, secretstore.Version{ID: id, Declaration: secrets.VersionDeclaration{Name: d.Name, Type: d.Type, Source: d.Source, Fingerprint: fingerprint}, Parts: []secrets.Part{secrets.ValuePart}})
				session.snapshot.Current = append(session.snapshot.Current, secretstore.Current{Name: d.Name, Version: id})
			}
			check, checkErr := service.Check(ctx, CheckRequest{})
			list, listErr := service.List(ctx, ListRequest{})
			if stored == "neither" {
				if checkErr == nil || check == nil || check.Secrets[0].Status != "stale" || check.Secrets[1].Status != "stale" || listErr != nil || list.Secrets[0].State != "stale" {
					t.Fatal("a version matching neither fingerprint read as current", check, checkErr, list, listErr)
				}
				if _, err := service.Bind(ctx, BindRequest{Names: []string{"generated", "stored"}}); err == nil || len(session.bound) != 0 {
					t.Fatal("a stale version was bound")
				}
				return
			}
			if checkErr != nil || check.Secrets[0].Status != "available" || check.Secrets[1].Status != "available" {
				t.Fatal("a version under the legacy fingerprint read as unavailable", check, checkErr)
			}
			if listErr != nil || len(list.Secrets) != 2 || list.Secrets[0].State != "current" || list.Secrets[1].State != "current" {
				t.Fatal("a version under the legacy fingerprint listed as not current", list, listErr)
			}
			generated, err := service.Generate(ctx, GenerateRequest{})
			if err != nil || generated.Changed != 0 || generated.Unchanged != 1 || material.generated != 0 {
				t.Fatal("generate re-minted a version under the legacy fingerprint", generated, err)
			}
			shown, err := service.Show(ctx, ShowRequest{Name: "stored", Part: secrets.ValuePart})
			if err != nil {
				t.Fatal("show refused a version under the legacy fingerprint", err)
			}
			shown.Material.Clear()
			if _, err := service.Bind(ctx, BindRequest{Names: []string{"generated", "stored"}}); err != nil || len(session.bound) != 2 {
				t.Fatal("bind refused a version under the legacy fingerprint", err)
			}
			for _, input := range session.bound {
				version, _ := currentVersion(session.snapshot, input.Declaration.Name)
				if input.Declaration.Summary() != version.Declaration || input.Declaration.Origin != "/synthetic/environment.yaml" {
					t.Fatalf("bind of %s carried a declaration its stored version was not written with: %+v", input.Declaration.Name, input.Declaration)
				}
			}
			set, err := service.Set(ctx, SetRequest{Name: "stored", SkipConfirmation: true, Input: secrets.Input{ValueStdin: true}})
			if err != nil || set.Unchanged != 1 || set.Changed != 0 {
				t.Fatal("equal material replaced a version under the legacy fingerprint", set, err)
			}
		})
	}
}

// Bind, check and show read only keyring versions for every source: the one
// acquisition that opens an operator path is secret set's.
func TestBindCheckAndShowReadOnlyKeyringVersions(t *testing.T) {
	s, a, m, _ := serviceFixture(t, declarationYAML("stored", "")+declarationYAML("generated", "  source: {generated: {}}\n"))
	ctx := context.Background()
	session := a.session
	a.session = nil
	result, err := s.Check(ctx, CheckRequest{})
	if err == nil || result == nil || len(result.Secrets) != 2 || result.Secrets[0].Status != "missing" || result.Secrets[1].Status != "missing" || m.acquired != 0 {
		t.Fatal("an uninitialized store did not read as missing without acquisition", result, err)
	}
	a.session = session
	if _, err := s.Set(ctx, SetRequest{Name: "stored", Input: secrets.Input{ValueStdin: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Generate(ctx, GenerateRequest{}); err != nil {
		t.Fatal(err)
	}
	acquired, reads := m.acquired, session.reads
	binding, err := s.Bind(ctx, BindRequest{Names: []string{"stored", "generated"}})
	if err != nil || len(binding.Versions) != 2 {
		t.Fatal(binding, err)
	}
	for _, input := range session.bound {
		if current, exists := currentVersion(session.snapshot, input.Declaration.Name); !exists || input.Version != current.ID {
			t.Fatalf("bind pinned %q, not the stored version of %s", input.Version, input.Declaration.Name)
		}
	}
	if result, err := s.Check(ctx, CheckRequest{}); err != nil || result.Secrets[0].Status != "available" || result.Secrets[1].Status != "available" {
		t.Fatal(result, err)
	}
	for _, name := range []string{"stored", "generated"} {
		shown, err := s.Show(ctx, ShowRequest{Name: name, Part: secrets.ValuePart})
		if err != nil {
			t.Fatal(name, err)
		}
		shown.Material.Clear()
	}
	if m.acquired != acquired || session.reads != reads+6 {
		t.Fatalf("bind, check and show acquired %d times and read %d stored versions", m.acquired-acquired, session.reads-reads)
	}
	list, err := s.List(ctx, ListRequest{})
	if err != nil || len(list.Secrets) != 2 || m.acquired != acquired {
		t.Fatal("metadata list acquired material", list, err)
	}
	a.failure = secretstore.Failure("store.corrupt", "tampered")
	result, err = s.Check(ctx, CheckRequest{})
	if err == nil || result != nil {
		t.Fatal("corrupt store produced trustworthy partial result")
	}
}

// A declaration that still names the retired file source refuses every
// command at compilation, before the store is opened or a path is read.
func TestAFileSourceRefusesEveryCommandBeforeTheStore(t *testing.T) {
	operations := map[string]func(*Service) error{
		"bind": func(s *Service) error {
			_, err := s.Bind(context.Background(), BindRequest{Names: []string{"file"}})
			return err
		},
		"check": func(s *Service) error {
			_, err := s.Check(context.Background(), CheckRequest{})
			return err
		},
		"show": func(s *Service) error {
			_, err := s.Show(context.Background(), ShowRequest{Name: "file", Part: secrets.ValuePart})
			return err
		},
		"set": func(s *Service) error {
			_, err := s.Set(context.Background(), SetRequest{Name: "file", Input: secrets.Input{ValueFile: "value"}})
			return err
		},
		"list": func(s *Service) error {
			_, err := s.List(context.Background(), ListRequest{})
			return err
		},
	}
	for name, invoke := range operations {
		t.Run(name, func(t *testing.T) {
			service, access, material, _ := unresolvedFixture(declarationYAML("file", "  source: {file: {path: secrets/token}}\n"))
			refused := false
			for _, d := range diagnostics.Of(invoke(service)) {
				refused = refused || d.Code == "api.field" && d.Field == "$.spec.source.file" && strings.Contains(d.Remediation, "secret set --name file --context <context> --value-file <path>")
			}
			if !refused {
				t.Fatal("the file source was not refused with its remedy")
			}
			if access.views != 0 || access.transactions != 0 || material.acquired != 0 || material.validated != 0 || access.session.reads != 0 {
				t.Fatal("a refused declaration reached the store or its material")
			}
		})
	}
}

// A removal reopens the binding its apply froze without compiling the desired
// state, so a version an earlier build froze from a file source still reaches
// it after compilation began refusing that source, and nothing is read from
// the path it once named.
func TestAVersionFrozenFromAFileStillReopens(t *testing.T) {
	service, access, material, _ := unresolvedFixture(declarationYAML("file", "  source: {file: {path: secrets/token}}\n"))
	frozen := secretstore.Version{ID: "version-file", Declaration: secrets.VersionDeclaration{Name: "file", Type: "token", Source: "file"}, Parts: []secrets.Part{secrets.ValuePart}}
	access.session.snapshot.Bindings = []secretstore.Binding{{ID: "binding-earlier", Versions: []string{frozen.ID}}}
	access.session.reopened = []secretstore.BoundMaterial{{Version: frozen, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("frozen-token")})}}
	bound, err := service.Reopen(context.Background(), BindingRequest{ContextName: "fixture", BindingID: "binding-earlier"})
	if err != nil || len(bound) != 1 || bound[0].Version.ID != frozen.ID {
		t.Fatal(bound, err)
	}
	value, _ := bound[0].Material.Part(secrets.ValuePart)
	defer clear(value)
	if string(value) != "frozen-token" {
		t.Fatal("reopened material differs from the frozen version")
	}
	if compiled := service.compiler.(*countingCompiler).calls; compiled != 0 || material.acquired != 0 || material.validated != 0 {
		t.Fatalf("reopen compiled %d times or acquired material", compiled)
	}
	if _, err := service.Bind(context.Background(), BindRequest{Names: []string{"file"}}); err == nil || len(access.session.bound) != 0 {
		t.Fatal("a new binding of the file source was made")
	}
}

func TestInputMatrixRejectsInapplicableFlags(t *testing.T) {
	inputs := map[string]secrets.Input{
		"opaque": {ValueStdin: true}, "token": {ValueFile: "v"}, "dockerConfigJson": {ValueFile: "v"},
		"usernamePassword": {Username: "operator", PasswordStdin: true}, "caBundle": {CertificateFile: "c"},
		"tlsCertificate": {CertificateFile: "c", PrivateKeyFile: "k"}, "sshKeyPair": {PrivateKeyFile: "k", PublicKeyFile: "p"},
	}
	for kind, input := range inputs {
		if !secrets.ValidInput(kind, input) {
			t.Fatal(kind)
		}
		explicitEmpty := input
		explicitEmpty.Provided = ^secrets.AllowedInputFields(kind)
		if secrets.ValidInput(kind, explicitEmpty) {
			t.Fatal("explicitly empty inapplicable flag accepted", kind)
		}
		input.Username = "extra"
		if kind == "usernamePassword" {
			input.PublicKeyFile = "extra"
		}
		if secrets.ValidInput(kind, input) {
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

// A consumer lists a context's binding identities to release those no record
// of its own names. The listing reveals no material and no version, takes no
// transaction, and a store that was never initialized lists nothing.
func TestBindingsListsIdentitiesWithoutMaterial(t *testing.T) {
	service, access, material, _ := serviceFixture(t, declarationYAML("token", ""))
	access.session.snapshot.Bindings = []secretstore.Binding{
		{ID: "binding-b", Versions: []string{"version-1"}},
		{ID: "binding-a", Versions: []string{"version-0", "version-1"}},
	}
	listed, err := service.Bindings(context.Background(), BindingsRequest{ContextName: "fixture"})
	if err != nil || strings.Join(listed, ",") != "binding-a,binding-b" {
		t.Fatalf("bindings = %v (%v)", listed, err)
	}
	if access.session.reads != 0 || access.session.writes != 0 || access.transactions != 0 || material.acquired != 0 {
		t.Fatal("listing bindings read material or mutated the store")
	}
	if len(access.unlocks) != 1 || access.unlocks[0] {
		t.Fatalf("listing bindings opened the store with unlock %v", access.unlocks)
	}
	access.session = nil
	if listed, err := service.Bindings(context.Background(), BindingsRequest{ContextName: "fixture"}); err != nil || len(listed) != 0 {
		t.Fatalf("an uninitialized store lists %v (%v)", listed, err)
	}
}
