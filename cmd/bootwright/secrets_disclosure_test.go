//go:build linux && amd64

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretNormalOutputsAndStateNeverContainMaterialOrDigests(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", strings.Join([]string{
		secretDocument("payload", "opaque", ""),
		secretDocument("password", "usernamePassword", ""),
		secretDocument("docker", "dockerConfigJson", ""),
	}, "\n---\n"))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	canaries := []string{"synthetic-output-opaque-canary", "synthetic-output-password-canary", "synthetic-invalid-docker-canary", "synthetic-produced-kubeconfig-canary"}
	for _, value := range append([]string{}, canaries...) {
		digest := sha256.Sum256([]byte(value))
		canaries = append(canaries, hex.EncodeToString(digest[:]))
	}
	payload := addSecretInput(t, t.TempDir(), "payload", canaries[0])
	password := addSecretInput(t, t.TempDir(), "password", canaries[1])
	docker := addSecretInput(t, t.TempDir(), "docker", canaries[2])
	run := func(exit int, args ...string) {
		t.Helper()
		out, stderr := contextRun(t, services, exit, args...)
		for _, canary := range canaries {
			if strings.Contains(out, canary) || strings.Contains(stderr, canary) {
				t.Fatal("normal command output disclosed confidential material")
			}
		}
	}
	produceThroughLentArea(t, services, repository, "alpha", "cluster-install-alpha", "kubeconfig", canaries[3])
	run(0, "secret", "set", "--name", "payload", "--value-file", payload)
	run(0, "secret", "set", "--name", "password", "--username", "fixture-user", "--password-file", password)
	for _, output := range []string{"text", "json"} {
		for _, invocation := range []struct {
			exit int
			args []string
		}{
			{0, []string{"secret", "list"}},
			{1, []string{"secret", "check"}},
			{0, []string{"secret", "encryption", "status"}},
			{0, []string{"secret", "set", "--name", "payload", "--value-file", payload, "--yes"}},
			{1, []string{"secret", "set", "--name", "docker", "--value-file", docker}},
			{1, []string{"secret", "set", "--name", "payload", "--value-file", payload}},
			{1, []string{"secret", "generate", "--name", "payload"}},
			{1, []string{"secret", "encryption", "rotate"}},
			{0, []string{"secret", "encryption", "rotate", "--yes"}},
			{0, []string{"render", "effective"}},
		} {
			args := append(append([]string{}, invocation.args...), "--output", output)
			structured := invocation.args[0] == "render" || invocation.args[1] == "list" || invocation.args[1] == "check" || invocation.args[1] == "encryption" && invocation.args[2] == "status"
			if structured {
				run(invocation.exit, args...)
			} else {
				run(invocation.exit, invocation.args...)
				run(2, args...)
			}
		}
		// Secret commands have no verbose mode; rejecting it must also be safe.
		run(2, "secret", "list", "--verbose", "--output", output)
	}
	run(1, "secret", "show", "--name", "payload", "--part", "password")
	run(0, "secret", "delete", "--name", "payload", "--yes")
	run(1, "secret", "show", "--name", "payload", "--part", "value")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, canary := range canaries {
			if bytes.Contains(data, []byte(canary)) {
				t.Error("persisted state disclosed material or material digest")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
