package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The helpers below stage fixture repositories. test/architecture and
// test/tooling, which go test runs beside this package, hold their own copies
// of the ones they use.

// modulePath prefixes the import path of every package of the module.
const modulePath = "github.com/crmarques/bootwright/"

func ratchetWrite(t *testing.T, root, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// ratchetEnvironment isolates Git from the caller's configuration and from a
// repository the caller's environment names.
func ratchetEnvironment() []string {
	var environment []string
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			environment = append(environment, variable)
		}
	}
	return append(environment, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.test",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
}

func ratchetGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	command.Env = ratchetEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
