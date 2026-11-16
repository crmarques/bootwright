package architecture_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The helpers below list and stage a repository. test/docs and test/tooling,
// which go test runs beside this package, hold their own copies of the ones
// they use.

// trackedFiles lists, relative to root, the files Git tracks there, so no
// untracked or ignored file of a checkout decides a verdict that CI reaches
// without it. The caller's GIT_ variables never redirect the listing.
func trackedFiles(root string) ([]string, error) { return gitListing(root, "ls-files", "-z") }

func gitListing(root string, arguments ...string) ([]string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = root
	command.Env = slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(variable, "GIT_") })
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git %s: %w: %s", arguments[0], err, bytes.TrimSpace(exit.Stderr))
		}
		return nil, fmt.Errorf("git %s: %w", arguments[0], err)
	}
	var files []string
	for name := range strings.SplitSeq(string(output), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// trackedPaths returns the tracked files together with every directory that
// holds one, each spelled as path.Clean spells it: the names a checkout without
// untracked or ignored files has.
func trackedPaths(files []string) map[string]bool {
	present := map[string]bool{}
	for _, name := range files {
		present[name] = true
		for directory := path.Dir(name); !present[directory]; directory = path.Dir(directory) {
			present[directory] = true
		}
	}
	return present
}

// repositoryFiles lists the files the repository tracks.
func repositoryFiles(t *testing.T) []string {
	t.Helper()
	files, err := trackedFiles(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("list tracked files: %v", err)
	}
	return files
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
