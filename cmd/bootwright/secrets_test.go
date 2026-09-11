//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func secretDocument(name, kind, source string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + name + "\nspec:\n  type: " + kind + "\n" + source
}

func addSecretInput(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func secretResult(t *testing.T, services cli.Services, want int, args ...string) map[string]json.RawMessage {
	t.Helper()
	args = append(args, "--output", "json")
	out, stderr := contextRun(t, services, want, args...)
	if stderr != "" {
		t.Fatal("JSON wrote stderr", stderr)
	}
	var envelope struct {
		OK       bool
		ExitCode int
		Result   map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.ExitCode != want || envelope.OK != (want == 0) {
		t.Fatal("invalid envelope", out, err)
	}
	return envelope.Result
}

func TestCompleteSecretCommandAndBindingJourney(t *testing.T) {
	for _, implementation := range []string{"local-keyring", "session-test"} {
		t.Run(implementation, func(t *testing.T) { secretCommandAndBindingJourney(t, implementation) })
	}
}

func secretCommandAndBindingJourney(t *testing.T, implementation string) {
	services, repository, input, root := contextFixture(t)
	if implementation == "session-test" {
		services = withMemoryStore(t, repository, root)
	}
	declarations := []string{
		secretDocument("opaque", "opaque", ""), secretDocument("password", "usernamePassword", ""), secretDocument("docker", "dockerConfigJson", ""),
		secretDocument("token", "token", "  source: {generated: {}}\n"),
		secretDocument("ca", "caBundle", "  source: {generated: {commonName: fixture-ca, validityDays: 30}}\n"),
		secretDocument("tls", "tlsCertificate", "  source: {generated: {commonName: fixture.example.test, dnsNames: [fixture.example.test], validityDays: 30}}\n"),
		secretDocument("ssh", "sshKeyPair", "  source: {generated: {keyType: ed25519}}\n"),
		secretDocument("file", "token", "  source: {file: {path: secrets/token}}\n"),
	}
	addSecretInput(t, input, "secret.yaml", strings.Join(declarations, "\n---\n"))
	if err := os.Mkdir(filepath.Join(input, "secrets"), 0700); err != nil {
		t.Fatal(err)
	}
	file := addSecretInput(t, filepath.Join(input, "secrets"), "token", "synthetic-file-canary\n")
	configuration := contexts.DefaultConfiguration("alpha")
	configuration.SecretStore.Type = implementation
	configPath := addSecretInput(t, t.TempDir(), "context.yaml", string(configuration.Canonical()))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--file", configPath, "--input-dir", input)
	status := secretResult(t, services, 0, "secret", "encryption", "status")
	if string(status["initialized"]) != "true" {
		t.Fatal(status)
	}
	before := stateFingerprint(t, root)
	negative := secretResult(t, services, 1, "secret", "check")
	if len(negative) == 0 {
		t.Fatal("negative check lost complete result")
	}
	secretResult(t, services, 0, "secret", "list")
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("read-only secret inspection wrote state")
	}
	contextRun(t, services, 0, "secret", "encryption", "init")
	contextRun(t, services, 0, "secret", "encryption", "init")
	contextRun(t, services, 2, "secret", "encryption", "init", "--type", "unavailable")
	opaque := addSecretInput(t, t.TempDir(), "value", "synthetic-opaque-canary\x00\n")
	password := addSecretInput(t, t.TempDir(), "password", "synthetic-password-canary\n")
	docker := addSecretInput(t, t.TempDir(), "docker.json", `{"auths":{"registry.example.test":{"auth":"c3ludGhldGljOmNhbmFyeQ=="}}}`)
	contextRun(t, services, 0, "secret", "set", "--name", "opaque", "--value-file", opaque)
	contextRun(t, services, 0, "secret", "set", "--name", "password", "--username", "fixture-user", "--password-file", password)
	contextRun(t, services, 0, "secret", "set", "--name", "docker", "--value-file", docker)
	contextRun(t, services, 0, "secret", "generate")
	contextRun(t, services, 0, "secret", "generate", "--name", "token")
	secretResult(t, services, 0, "secret", "check")
	list := secretResult(t, services, 0, "secret", "list")
	var rows []custody.ListRow
	if err := json.Unmarshal(list["secrets"], &rows); err != nil || len(rows) != 7 {
		t.Fatal("wrong list membership", err, len(rows))
	}
	for _, check := range []struct {
		name string
		part secrets.Part
		want string
	}{
		{"opaque", secrets.ValuePart, "synthetic-opaque-canary\x00\n"}, {"password", secrets.PasswordPart, "synthetic-password-canary"}, {"file", secrets.ValuePart, "synthetic-file-canary"},
	} {
		out, stderr := contextRun(t, services, 0, "secret", "show", "--name", check.name, "--part", string(check.part))
		if out != check.want || stderr != "" {
			t.Fatal("reveal bytes changed", check.name)
		}
	}
	for _, check := range []struct{ name, part string }{{"ca", "certificate"}, {"ca", "private-key"}, {"tls", "certificate"}, {"tls", "private-key"}, {"ssh", "private-key"}, {"ssh", "public-key"}} {
		out, stderr := contextRun(t, services, 0, "secret", "show", "--name", check.name, "--part", check.part)
		if out == "" || stderr != "" {
			t.Fatal("empty generated part", check)
		}
	}
	bindings, ok := services.Secrets.(interface {
		Bind(context.Context, custody.BindRequest) (secretstore.Binding, error)
		Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error)
		Release(context.Context, custody.BindingRequest) (bool, error)
	})
	if !ok {
		t.Fatal("binding capability not composed")
	}
	binding, err := bindings.Bind(context.Background(), custody.BindRequest{Names: []string{"opaque", "token", "file", "ca"}})
	if err != nil {
		t.Fatal(err)
	}
	independent, err := bindings.Bind(context.Background(), custody.BindRequest{Names: []string{"token"}})
	if err != nil {
		t.Fatal(err)
	}
	request := custody.BindingRequest{BindingID: binding.ID}
	frozen, err := bindings.Reopen(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[secrets.Part][]byte{}
	for _, item := range frozen {
		parts := map[secrets.Part][]byte{}
		for _, part := range item.Material.Parts() {
			parts[part], _ = item.Material.Part(part)
		}
		want[item.Version.Declaration.Name] = parts
		item.Material.Clear()
	}
	if _, ok := want["ca"][secrets.PrivateKeyPart]; ok {
		t.Fatal("CA consumer received signing key")
	}
	addSecretInput(t, filepath.Dir(opaque), filepath.Base(opaque), "replacement-canary")
	contextRun(t, services, 0, "secret", "set", "--name", "opaque", "--value-file", opaque, "--yes")
	contextRun(t, services, 0, "secret", "generate", "--name", "token", "--renew")
	contextRun(t, services, 0, "secret", "delete", "--name", "opaque", "--yes")
	contextRun(t, services, 0, "secret", "delete", "--name", "opaque", "--yes")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	// New desired state may orphan/remove names; an exact continuation still
	// uses the whole versions pinned before replacement and live-file removal.
	addSecretInput(t, input, "secret.yaml", strings.Join(declarations[1:], "\n---\n"))
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	contextRun(t, services, 0, "secret", "encryption", "rotate", "--yes")
	reopened, err := bindings.Reopen(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range reopened {
		for _, part := range item.Material.Parts() {
			got, _ := item.Material.Part(part)
			if !bytes.Equal(got, want[item.Version.Declaration.Name][part]) {
				t.Fatal("immutable binding changed")
			}
			clear(got)
		}
		item.Material.Clear()
	}
	changed, err := bindings.Release(context.Background(), request)
	if err != nil || !changed {
		t.Fatal("release failed", err)
	}
	if _, err := bindings.Reopen(context.Background(), request); err == nil {
		t.Fatal("released binding remained reachable")
	}
	remaining, err := bindings.Reopen(context.Background(), custody.BindingRequest{BindingID: independent.ID})
	if err != nil || len(remaining) != 1 {
		t.Fatal("release changed another binding", err)
	}
	value, _ := remaining[0].Material.Part(secrets.ValuePart)
	if !bytes.Equal(value, want["token"][secrets.ValuePart]) {
		t.Fatal("unrelated binding lost its pinned token")
	}
	clear(value)
	remaining[0].Material.Clear()
	if changed, err := bindings.Release(context.Background(), custody.BindingRequest{BindingID: independent.ID}); err != nil || !changed {
		t.Fatal("remaining release failed", err)
	}
	for _, parts := range want {
		for _, value := range parts {
			clear(value)
		}
	}
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secretRoot := filepath.Join(root, "contexts", registry.Contexts[0].Name, "secrets")
	if err := filepath.WalkDir(secretRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, canary := range []string{"synthetic-opaque-canary", "synthetic-password-canary", "synthetic-file-canary", "replacement-canary"} {
			if bytes.Contains(data, []byte(canary)) {
				t.Errorf("plaintext in store file %s", entry.Name())
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func sameFingerprints(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestSecretContextReplacementAndProtectedDeletion(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	declaration := secretDocument("token", "token", "  source: {generated: {bytes: 32}}\n")
	addSecretInput(t, input, "secret.yaml", declaration)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	contextRun(t, services, 0, "secret", "generate")
	initial, _ := contextRun(t, services, 0, "secret", "show", "--name", "token", "--part", "value")
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	same, _ := contextRun(t, services, 0, "secret", "show", "--name", "token", "--part", "value")
	if same != initial {
		t.Fatal("unchanged declaration lost material")
	}
	contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
	contextRun(t, services, 0, "context", "init", "--name", "renamed", "--input-dir", input)
	contextRun(t, services, 1, "secret", "show", "--name", "token", "--part", "value")
	contextRun(t, services, 0, "secret", "generate")
	same, _ = contextRun(t, services, 0, "secret", "show", "--name", "token", "--part", "value")
	if same == initial {
		t.Fatal("new context reused deleted secret material")
	}
	addSecretInput(t, input, "secret.yaml", strings.ReplaceAll(declaration, "bytes: 32", "bytes: 48"))
	contextRun(t, services, 0, "context", "update", "--name", "renamed", "--input-dir", input, "--yes")
	contextRun(t, services, 1, "secret", "show", "--name", "token", "--part", "value")
	check := secretResult(t, services, 1, "secret", "check")
	if !bytes.Contains(check["secrets"], []byte(`"stale"`)) {
		t.Fatal("changed generation not stale")
	}
	contextRun(t, services, 0, "secret", "generate")
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mutation := filepath.Join(root, "contexts", registry.Contexts[0].Name, "state", "mutation.json")
	if err := os.WriteFile(mutation, []byte(`{"version":1,"operation":"failed","ownership":"retained"}`), 0600); err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 1, "context", "delete", "--name", "renamed", "--purge", "--yes")
	secretResult(t, services, 0, "secret", "check")
	secretResult(t, services, 0, "secret", "list")
	secretResult(t, services, 0, "secret", "encryption", "status")
	contextRun(t, services, 0, "secret", "show", "--name", "token", "--part", "value")
	contextRun(t, services, 0, "secret", "encryption", "rotate", "--yes")
	before := stateFingerprint(t, root)
	contextRun(t, services, 1, "context", "update", "--name", "renamed", "--input-dir", input, "--yes")
	contextRun(t, services, 1, "context", "delete", "--name", "renamed", "--purge", "--yes")
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("protected context refusal changed state")
	}
}

func TestSecretNameReuseCannotExposeAnotherContextIdentity(t *testing.T) {
	services, repository, input, _ := contextFixture(t)
	declaration := secretDocument("token", "token", "  source: {generated: {}}\n")
	addSecretInput(t, input, "secret.yaml", declaration)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	contextRun(t, services, 0, "secret", "generate")
	before, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
	newInput := t.TempDir()
	addSecretInput(t, newInput, "environment.yaml", syntheticEnvironment)
	addSecretInput(t, newInput, "controller.yaml", serviceHost)
	addSecretInput(t, newInput, "secret.yaml", declaration)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", newInput)
	after, err := repository.View(context.Background())
	if err != nil || len(after.Contexts) != 1 || before.Contexts[0].ID == after.Contexts[0].ID {
		t.Fatal("context name reuse did not establish a new durable identity", err)
	}
	status := secretResult(t, services, 0, "secret", "encryption", "status")
	if string(status["initialized"]) != "true" {
		t.Fatal("new identity did not receive its own initialized secret store")
	}
	out, _ := contextRun(t, services, 1, "secret", "show", "--name", "token", "--part", "value")
	if out != "" {
		t.Fatal("new identity revealed another context's material")
	}
}
