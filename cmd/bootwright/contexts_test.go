//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const syntheticEnvironment = "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: synthetic\nspec:\n  controller: {machineRef: service-host}\n  domains:\n    base: example.test\n"

func contextFixture(t *testing.T) (cli.Services, *contextfs.Store, string, string) {
	t.Helper()
	parent := t.TempDir()
	input := filepath.Join(parent, "input")
	root := filepath.Join(parent, "state")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte(syntheticEnvironment), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "controller.yaml"), []byte(serviceHost), 0600); err != nil {
		t.Fatal(err)
	}
	repository := testRepository(root)
	return testServices(t, repository, root), repository, input, root
}

func contextRun(t *testing.T, services cli.Services, want int, args ...string) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runServices(t.Context(), args, &out, &errOut, services)
	if code != want {
		t.Fatalf("%v: code=%d want=%d\nstdout=%s\nstderr=%s", args, code, want, out.String(), errOut.String())
	}
	return out.String(), errOut.String()
}

func stateFingerprint(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		result[relative] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCompleteContextJourney(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "list")
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("read-only list created state: %v", err)
	}
	contextRun(t, services, 1, "context", "current", "--short")
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Contexts) != 1 {
		t.Fatal(registry)
	}
	first := registry.Contexts[0]
	short, _ := contextRun(t, services, 0, "context", "current", "--short")
	if short != "alpha\n" {
		t.Fatal(short)
	}
	contextRun(t, services, 1, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "context", "init", "--name", "other", "--input-dir", input)
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Contexts) != 2 || registry.Contexts[0].Name == registry.Contexts[1].Name || registry.Contexts[0].EnvironmentDirectory != registry.Contexts[1].EnvironmentDirectory {
		t.Fatal("second context from one input directory", registry)
	}
	identical, _ := contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input)
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(identical, "[OK] Context unchanged\n") || registry.Contexts[0].Revision != first.Revision {
		t.Fatalf("an identical update replaced the revision: %s %+v", identical, registry.Contexts[0])
	}
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte(strings.ReplaceAll(syntheticEnvironment, "example.test", "changed.test")), 0600); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 1, "context", "update", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if registry.Contexts[0].Name != first.Name || registry.Contexts[0].Revision == first.Revision {
		t.Fatal("update changed identity or did not replace revision", registry)
	}
	other := filepath.Join(t.TempDir(), "input")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "environment.yaml"), []byte(syntheticEnvironment), 0600); err != nil {
		t.Fatal(err)
	}
	addSecretInput(t, other, "controller.yaml", serviceHost)
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", other, "--yes")
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if registry.Contexts[0].Name != first.Name || registry.Contexts[0].EnvironmentDirectory != filepath.Clean(other) {
		t.Fatal("update from another directory changed identity or kept the old provenance", registry)
	}
	contextRun(t, services, 0, "context", "init", "--name", "beta", "--input-dir", other)
	contextRun(t, services, 0, "context", "use", "--name", "alpha")
	list, _ := contextRun(t, services, 0, "context", "list")
	if strings.Index(list, "alpha") > strings.Index(list, "beta") {
		t.Fatal(list)
	}
	contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
	contextRun(t, services, 1, "context", "current", "--short")
	contextRun(t, services, 1, "validate", "--context", "alpha")
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range registry.Contexts {
		if record.Name == "alpha" && (record.Revision == first.Revision || record.Revision == "") {
			t.Fatal("reinitialization inherited or lost the deleted context's input", record)
		}
	}
}

func TestMissingContextRegistryExplainsSafeRecovery(t *testing.T) {
	services, _, _, root := contextFixture(t)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(root, "retained-state")
	if err := os.WriteFile(retained, []byte("retain\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := stateFingerprint(t, root)
	want := "[FAIL] context.state: the state root holds state but no registry.json this build can read; next: another Bootwright build may have created this root and may manage live environments there: run this build on another controller host, or retire that build's environments before archiving its root, and never move it aside while its services run; a store this build created is restored whole from a matching backup\n"
	for _, args := range [][]string{
		{"context", "list"},
		{"context", "delete", "--name", "test", "--purge"},
		{"context", "init", "--name", "test"},
	} {
		out, stderr := contextRun(t, services, 1, args...)
		if out != "" || stderr != want {
			t.Fatalf("%v: stdout=%q stderr=%q", args, out, stderr)
		}
		if !sameFingerprints(before, stateFingerprint(t, root)) {
			t.Fatalf("%v changed unrecognized state", args)
		}
	}
}

// A context directory removed by hand refuses every store command naming the
// context, its entry and the kernel's answer; its purge refuses naming the
// orphan acknowledgement, which then abandons exactly that context.
func TestAHandRemovedContextIsNamedAndAbandonedByItsPurge(t *testing.T) {
	services, _, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "context", "init", "--name", "beta", "--input-dir", input)
	if err := os.RemoveAll(filepath.Join(root, "contexts", "beta")); err != nil {
		t.Fatal(err)
	}
	_, stderr := contextRun(t, services, 1, "context", "list")
	if !strings.Contains(stderr, "context.state: context beta cannot be verified: contexts/beta: no such file or directory") ||
		!strings.Contains(stderr, "--allow-orphans") || strings.Contains(stderr, root) {
		t.Fatalf("list refused with %q", stderr)
	}
	_, stderr = contextRun(t, services, 1, "context", "delete", "--name", "beta", "--purge", "--yes")
	if !strings.Contains(stderr, "context.unsafe-delete: context beta has lost its directory") ||
		!strings.Contains(stderr, "bootwright context delete --name beta --purge --allow-orphans") {
		t.Fatalf("the purge refused with %q", stderr)
	}
	contextRun(t, services, 0, "context", "delete", "--name", "beta", "--purge", "--allow-orphans", "--yes")
	list, _ := contextRun(t, services, 0, "context", "list")
	if !strings.Contains(list, "alpha") || strings.Contains(list, "beta") {
		t.Fatalf("after the abandonment the list is %q", list)
	}
}

func TestContextInitWithoutInputCreatesSelectableEncryptedContext(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	out, stderr := contextRun(t, services, 0, "context", "init", "--name", "test")
	if stderr != "" || !strings.Contains(out, "Input configured  false") || strings.Contains(out, "Files copied") {
		t.Fatal("unexpected empty context result", out, stderr)
	}
	registry, err := repository.View(context.Background())
	if err != nil || len(registry.Contexts) != 1 {
		t.Fatal(registry, err)
	}
	record := registry.Contexts[0]
	if record.Mode != contexts.Ready || record.Revision != "" || record.EnvironmentDirectory != "" || record.SecretStoreType != "local-keyring" {
		t.Fatal("default context is not ready and unconfigured", record)
	}
	selection, err := testContextWiring(t, root).Selection.Read(context.Background())
	if err != nil || selection.Name != record.Name || selection.Version != contexts.SelectionVersion {
		t.Fatal("init did not select its new context", selection, err)
	}
	contextRoot := filepath.Join(root, "contexts", "test")
	for _, relative := range []string{"context.yaml", "state/reservation.json", "state/mutation.json", "desired-state/revisions", "secrets"} {
		if _, err := os.Stat(filepath.Join(contextRoot, relative)); err != nil {
			t.Fatal("required context entry is missing", relative, err)
		}
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		owner := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm() != want || int(owner.Uid) != os.Getuid() || int(owner.Gid) != os.Getgid() {
			t.Errorf("private store entry has unexpected permissions or ownership: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status := secretResult(t, services, 0, "secret", "encryption", "status")
	if string(status["initialized"]) != "true" {
		t.Fatal("default keyring was not eagerly initialized", status)
	}
	contextRun(t, services, 1, "validate")
	_, stderr = contextRun(t, services, 1, "secret", "generate")
	if !strings.Contains(stderr, "context update --name test --input-dir") {
		t.Fatal("missing input diagnostic does not explain import", stderr)
	}
	configuration := filepath.Join(contextRoot, "context.yaml")
	before := stateFingerprint(t, root)
	contextRun(t, services, 0, "context", "update", "--name", "test", "--file", configuration)
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("equivalent context configuration changed durable state")
	}
	contextRun(t, services, 0, "context", "update", "--name", "test", "--input-dir", input, "--yes")
	contextRun(t, services, 0, "validate")
	updated, err := repository.View(context.Background())
	if err != nil || updated.Contexts[0].Name != record.Name || updated.Contexts[0].Revision == "" {
		t.Fatal("first import did not preserve the context identity", updated, err)
	}
}

// A context's name is its identity, so a retained user marker follows name
// reuse instead of being refused. What still refuses is a marker naming a
// context that does not exist, and deletion clears the deleting user's own.
func TestUserSelectionRefusesAnAbsentContextAndFollowsNameReuse(t *testing.T) {
	services, _, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	pointer := testContextWiring(t, root).Selection
	retained, err := pointer.Read(context.Background())
	if err != nil || retained.Name != "alpha" {
		t.Fatal("init did not select its context", retained, err)
	}
	contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
	if cleared, err := pointer.Read(context.Background()); err != nil || cleared.Name != "" {
		t.Fatal("delete did not clear its own marker", cleared, err)
	}
	if err := pointer.Write(context.Background(), retained); err != nil {
		t.Fatal(err)
	}
	before := stateFingerprint(t, root)
	for _, args := range [][]string{{"context", "current"}, {"validate"}, {"secret", "encryption", "status"}} {
		_, stderr := contextRun(t, services, 1, args...)
		if !strings.Contains(stderr, "context.state") {
			t.Fatal("marker naming an absent context was accepted", args, stderr)
		}
	}
	contextRun(t, services, 0, "context", "list")
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("selection inspection changed context state")
	}
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	if err := pointer.Write(context.Background(), retained); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 0, "context", "current")
	contextRun(t, services, 0, "validate")
	contextRun(t, services, 0, "secret", "encryption", "status")
}

func TestContextReplayAndReadOnlyEffects(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	secret := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: credential\nspec:\n  type: opaque\n  source:\n    contextStore: {}\n"
	if err := os.WriteFile(filepath.Join(input, "credential.yaml"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(input, "secrets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(input, "secrets", "value"), 0600); err != nil {
		t.Fatal(err)
	}
	explicit, _ := contextRun(t, services, 0, "validate", "-f", input, "--output", "json")
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	frozen, _ := contextRun(t, services, 0, "validate", "--output", "json")
	if explicit != frozen {
		t.Fatalf("frozen admission changed results:\n%s\n%s", explicit, frozen)
	}
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mutation := filepath.Join(root, "contexts", registry.Contexts[0].Name, "state", "mutation.json")
	if err := os.Remove(mutation); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(mutation, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(input); err != nil {
		t.Fatal(err)
	}
	before := stateFingerprint(t, root)
	yaml, stderr := contextRun(t, services, 0, "render", "effective")
	if stderr != "" || !strings.Contains(yaml, "name: credential") {
		t.Fatal(yaml, stderr)
	}
	output, stderr := contextRun(t, services, 0, "render", "effective", "--output", "json")
	if stderr != "" {
		t.Fatal(stderr)
	}
	var envelope struct {
		OK     bool
		Result struct {
			Counts         struct{ FilesSeen, ObjectsDecoded int }
			EffectiveState []json.RawMessage
		}
		Diagnostics []json.RawMessage
		Logs        []json.RawMessage
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil || !envelope.OK || len(envelope.Result.EffectiveState) != 3 || envelope.Result.Counts.FilesSeen != 3 || len(envelope.Diagnostics) != 0 || len(envelope.Logs) != 0 {
		t.Fatalf("effective result %s: %v", output, err)
	}
	frozen, _ = contextRun(t, services, 0, "validate", "--context", "alpha", "--output", "json")
	if frozen != explicit {
		t.Fatal("original deletion changed replay")
	}
	after := stateFingerprint(t, root)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("inspection modified stored data")
	}
}

func TestInvalidAdmissionAndUnavailableRoutesDoNotWrite(t *testing.T) {
	services, _, input, root := contextFixture(t)
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 1, "context", "init", "--name", "alpha", "--input-dir", input)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("failed admission created state: %v", err)
	}
	for _, args := range [][]string{{"render", "installer", "--context", "alpha"}} {
		_, stderr := contextRun(t, services, 1, args...)
		if !strings.Contains(stderr, "cli.not-implemented") {
			t.Fatal(stderr)
		}
	}
	// An implemented lifecycle route validates the context instead, and a
	// failed admission still leaves no state behind.
	for _, args := range [][]string{{"apply", "--context", "alpha", "--yes"}, {"destroy", "--context", "alpha", "--yes"}, {"status", "--context", "alpha"}, {"plan", "--context", "alpha"}, {"machine", "list", "--context", "alpha"}, {"machine", "trust", "--context", "alpha", "--yes"}} {
		_, stderr := contextRun(t, services, 1, args...)
		if !strings.Contains(stderr, "context.state") {
			t.Fatal(stderr)
		}
	}
	_, stderr := contextRun(t, services, 1, "secret", "generate", "--context", "alpha")
	if !strings.Contains(stderr, "context.state") {
		t.Fatal("implemented secret route did not validate the context", stderr)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("unavailable route created state: %v", err)
	}
	nested := filepath.Join(input, "state")
	nestedRepository := testRepository(nested)
	nestedServices := testServices(t, nestedRepository, nested)
	_, stderr = contextRun(t, nestedServices, 1, "context", "init", "--name", "alpha", "--input-dir", input)
	if !strings.Contains(stderr, "context.state") {
		t.Fatal("nested store did not fail before parsing input", stderr)
	}
}

func TestProtectedContextCannotBeDeleted(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mutation := filepath.Join(root, "contexts", "alpha", "state", "mutation.json")
	if err := os.WriteFile(mutation, []byte(`{"version":1,"operation":"failed","ownership":"retained"}`), 0600); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 1, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	contextRun(t, services, 1, "context", "delete", "--name", "alpha", "--purge", "--yes")
	contextRun(t, services, 2, "context", "delete", "--name", "alpha", "--purge", "--yes", "--abandon-resources")
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Ready {
		t.Fatal(registry)
	}
	contextRun(t, services, 0, "validate")
	contextRun(t, services, 0, "context", "use", "--name", "alpha")
	contextRun(t, services, 1, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 1, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	retained, err := os.ReadFile(mutation)
	if err != nil || !bytes.Contains(retained, []byte("retained")) {
		t.Fatal("refused deletion lost lifecycle evidence", err)
	}
}

// Mutation evidence absent from a present context directory is evidence the
// guard cannot read, exactly as corrupt evidence is: status names the orphan
// acknowledgement as the next step, an update and a default deletion refuse
// naming the context, status and that acknowledgement, and the acknowledged
// deletion abandons the context. Status and the apply refusal call missing
// evidence absent or empty, not an unrecognized record.
func TestMissingEvidenceIsRefusedAndAbandonedAsCorruptEvidenceIs(t *testing.T) {
	exit := "bootwright context delete --name alpha --purge --allow-orphans"
	refusal := ": the mutation evidence of context alpha is missing, corrupt or unsupported, so nothing proves what it owns; " +
		"next: review its records with bootwright status --context alpha, then abandon whatever it owns with " + exit + "\n"
	for name, test := range map[string]struct {
		damage func(string) error
		entry  string
	}{
		"missing": {os.Remove, "the mutation evidence is absent or empty"},
		"corrupt": {func(path string) error { return os.WriteFile(path, []byte("{"), 0600) }, "the mutation evidence reads an unrecognized record"},
	} {
		t.Run(name, func(t *testing.T) {
			services, repository, input, root := contextFixture(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			if err := test.damage(filepath.Join(root, "contexts", "alpha", "state", "mutation.json")); err != nil {
				t.Fatal(err)
			}
			if out, _ := contextRun(t, services, 0, "status", "--context", "alpha"); !strings.Contains(out, "Next steps\n  "+exit+"\n") ||
				!strings.Contains(out, "Contradictions\n  "+test.entry+"\n") {
				t.Fatalf("status named no orphan acknowledgement or not %q:\n%s", test.entry, out)
			}
			if _, stderr := contextRun(t, services, 1, "apply", "--context", "alpha", "--yes"); !strings.Contains(stderr, "lifecycle.state") || !strings.Contains(stderr, test.entry) {
				t.Fatalf("the apply refused with %q, want it to name %q", stderr, test.entry)
			}
			if _, stderr := contextRun(t, services, 1, "context", "update", "--name", "alpha", "--input-dir", input, "--yes"); stderr != "[FAIL] context.state"+refusal {
				t.Fatalf("the update refused with %q", stderr)
			}
			if _, stderr := contextRun(t, services, 1, "context", "delete", "--name", "alpha", "--purge", "--yes"); stderr != "[FAIL] context.unsafe-delete"+refusal {
				t.Fatalf("the default deletion refused with %q", stderr)
			}
			if out, _ := contextRun(t, services, 0, append(strings.Fields(strings.TrimPrefix(exit, "bootwright ")), "--yes")...); !strings.Contains(out, "Orphans abandoned  true") {
				t.Fatalf("the acknowledged deletion reported %s", out)
			}
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 0 {
				t.Fatalf("the registry still holds %+v (%v)", registry.Contexts, err)
			}
			if _, err := os.Stat(filepath.Join(root, "contexts", "alpha")); !os.IsNotExist(err) {
				t.Fatalf("the abandoned context remains (%v)", err)
			}
		})
	}
}

func TestCompleteExampleContextRoundTrip(t *testing.T) {
	services, _, input, _ := contextFixture(t)
	if err := os.RemoveAll(input); err != nil {
		t.Fatal(err)
	}
	sources := exampleSources(t)
	for _, collection := range [][]desiredstate.SourceFile{sources.Files, sources.Markers} {
		for _, file := range collection {
			relative, err := filepath.Rel(sources.Roots[0], file.Path())
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(input, relative)
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, file.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	direct, _ := contextRun(t, services, 0, "validate", "-f", input, "--output", "json")
	contextRun(t, services, 0, "context", "init", "--name", "complete", "--input-dir", input)
	if err := os.RemoveAll(input); err != nil {
		t.Fatal(err)
	}
	replay, _ := contextRun(t, services, 0, "validate", "--output", "json")
	if replay != direct {
		t.Fatal("complete example did not replay equivalently")
	}
	rendered, stderr := contextRun(t, services, 0, "render", "effective", "--output", "json")
	var result struct {
		Result struct{ EffectiveState []json.RawMessage }
	}
	if err := json.Unmarshal([]byte(rendered), &result); err != nil || len(result.Result.EffectiveState) != 106 || stderr != "" {
		t.Fatalf("complete effective state: count=%d stderr=%s err=%v", len(result.Result.EffectiveState), stderr, err)
	}
}

func TestContextRunOutlastsAHostSlowerThanAnyDeadline(t *testing.T) {
	_, repository, input, root := contextFixture(t)
	deps := testContextWiring(t, root)
	deps.Selection = loadedHostSelection{deps.Selection}
	deps.Repository, deps.Workspace = repository, repository
	contextRun(t, assembleServices(deps), 0, "context", "init", "--name", "alpha", "--input-dir", input)
}

// loadedHostSelection stands for a host loaded past any wall-clock deadline:
// it returns only once a bounded context expires.
type loadedHostSelection struct{ contexts.SelectionStore }

func (s loadedHostSelection) Write(ctx context.Context, selection contexts.Selection) error {
	if _, bounded := ctx.Deadline(); bounded {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.SelectionStore.Write(ctx, selection)
}

// An identical re-import over a completed apply keeps the selected revision:
// it publishes nothing and asks nothing, so it needs neither a terminal nor
// --yes and the store is unchanged, and the next apply still settles over the
// same revision and frozen bytes. Changed input over that apply asks, and with
// --yes publishes, warning that apply refuses it until a destroy.
func TestAnIdenticalReimportOverACompletedApplyPublishesNothing(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	mutation := filepath.Join(root, "contexts", "alpha", "state", "mutation.json")
	if err := os.WriteFile(mutation, []byte(`{"version":1,"operation":"applied","ownership":"retained"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := stateFingerprint(t, root)
	out, stderr := contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input)
	if !strings.HasPrefix(out, "[OK] Context unchanged\n") || stderr != "" || !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatalf("the identical re-import changed the store or asked: %s %s", out, stderr)
	}
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	selected := registry.Contexts[0].Revision
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte(strings.ReplaceAll(syntheticEnvironment, "example.test", "changed.test")), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr = contextRun(t, services, 1, "context", "update", "--name", "alpha", "--input-dir", input)
	if !strings.Contains(stderr, "confirmation requires an interactive terminal or --yes") || !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatalf("changed input over a completed apply published without confirmation: %s", stderr)
	}
	out, stderr = contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	want := "[WARN] lifecycle.state: context alpha holds a completed apply, and apply refuses changed desired state until what that apply owns is taken back; next: take it back with bootwright destroy --context alpha, then run bootwright apply --context alpha\n"
	if !strings.HasPrefix(out, "[OK] Context updated\n") || stderr != want {
		t.Fatalf("changed input over a completed apply: %s %q", out, stderr)
	}
	registry, err = repository.View(context.Background())
	if err != nil || registry.Contexts[0].Revision == selected {
		t.Fatalf("changed input kept the revision %+v (%v)", registry.Contexts, err)
	}
}

// An identical re-import still reports what admission warned of, once on
// standard error, while it publishes nothing and asks nothing.
func TestAnIdenticalReimportReportsItsWarningsOnce(t *testing.T) {
	services, _, input, root := contextFixture(t)
	addSecretInput(t, input, "environment.yaml", syntheticEnvironment+"  resources:\n    - controller.yaml\n")
	addSecretInput(t, input, "excluded.yaml", strings.Replace(serviceHost, "name: service-host", "name: excluded-host", 1))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	before := stateFingerprint(t, root)
	out, stderr := contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input)
	warning := "[WARN] api.selection " + filepath.Join(input, "excluded.yaml") + ":"
	if !strings.HasPrefix(out, "[OK] Context unchanged\n") || strings.Count(stderr, "[WARN] ") != 1 || !strings.HasPrefix(stderr, warning) || !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatalf("the identical re-import lost or repeated its warning, or changed the store: %s %q", out, stderr)
	}
}
