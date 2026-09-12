package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompositionSuppliesRuntimeBuildInformation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("version stderr = %q", stderr.String())
	}
	for _, line := range []string{
		"Go                 " + runtime.Version() + "\n",
		"Target             " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
	} {
		if !strings.Contains(stdout.String(), line) {
			t.Fatalf("version %q does not contain linked runtime %q", stdout.String(), line)
		}
	}
}

func TestCompositionWiresEveryApplicationCommand(t *testing.T) {
	services := isolatedServices(t)
	commandsWithSyntheticInputs := [][]string{
		{"validate", "-f", filepath.Join(t.TempDir(), "missing.yaml")},
		{"context", "init", "--name", "example", "--input-dir", "inputs"},
		{"context", "update", "--name", "example", "--input-dir", "inputs"},
		{"context", "use", "--name", "example"},
		{"context", "list"},
		{"context", "current"},
		{"context", "delete", "--name", "example", "--purge"},
		{"add-ons", "list"},
		{"add-ons", "add", "--name", "example"},
		{"add-ons", "delete", "--name", "example"},
		{"secret", "set", "--name", "example", "--value-file", "secret.bin"},
		{"secret", "generate"},
		{"secret", "check"},
		{"secret", "list"},
		{"secret", "show", "--name", "example", "--part", "value"},
		{"secret", "delete", "--name", "example"},
		{"secret", "encryption", "init"},
		{"secret", "encryption", "status"},
		{"secret", "encryption", "rotate"},
		{"media", "add", "--name", "example.iso", "--from-file", "source.iso"},
		{"media", "list"},
		{"media", "delete", "--name", "example.iso"},
		{"validate"},
		{"preflight", "infra"},
		{"preflight", "clusters"},
		{"preflight", "container-cluster"},
		{"preflight", "storage-cluster"},
		{"preflight", "add-ons"},
		{"preflight", "all"},
		{"plan"},
		{"status"},
		{"render", "--input-dir", "inputs", "--output-dir", "artifacts"},
		{"render", "effective"},
		{"render", "installer"},
		{"render", "storage"},
		{"apply"},
		{"destroy"},
		{"machine", "list"},
		{"machine", "rsh", "--name", "example"},
		{"machine", "exec", "--name", "example", "--", "--help"},
		{"machine", "trust"},
		{"cluster", "list"},
		{"cluster", "info"},
		{"cluster", "rsh", "--name", "example"},
		{"cluster", "exec", "--name", "example", "--", "--help"},
		{"cluster", "oc", "--name", "example", "--", "--help"},
		{"cluster", "kubectl", "--name", "example", "--", "--help"},
		{"cluster", "kubeconfig", "--name", "example"},
	}
	for _, args := range commandsWithSyntheticInputs {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runServices(context.Background(), args, &stdout, &stderr, services)
			prefix := "[FAIL] cli.not-implemented: bootwright "
			if args[0] == "validate" && len(args) > 1 || args[0] == "context" && (args[1] == "init" || args[1] == "update") {
				prefix = "[FAIL] input.not-found "
			} else if args[0] == "context" || args[0] == "secret" || args[0] == "validate" ||
				args[0] == "plan" || args[0] == "status" || args[0] == "apply" || args[0] == "destroy" ||
				args[0] == "render" && len(args) > 1 && args[1] == "effective" {
				prefix = "[FAIL] context.state:"
			}
			if args[0] == "context" && args[1] == "list" {
				if code != 0 || stderr.Len() != 0 {
					t.Fatalf("empty list: %d %s %s", code, stdout.String(), stderr.String())
				}
				return
			}
			if code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), prefix) {
				t.Fatalf("composition result: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestComposedExplicitValidationIsContextFree(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("input admission is qualified for linux/amd64")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	input := filepath.Join(root, "environment.yaml")
	content := []byte(serviceEnvironment + "\n---\n" + serviceHost)
	if err := os.WriteFile(input, content, 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "store")
	repository := testRepository(state)
	services := testServices(t, repository, state)
	for _, args := range [][]string{
		{"validate", "-f", input, "--context", "missing"},
		{"validate", "-f", input, "--context", "missing", "--output", "json"},
	} {
		var out, errOut bytes.Buffer
		if code := runServices(context.Background(), args, &out, &errOut, services); code != 0 || errOut.Len() != 0 {
			t.Fatalf("explicit validation: %d %s %s", code, out.String(), errOut.String())
		}
		if args[len(args)-1] == "json" {
			var envelope struct {
				OK     bool
				Result struct {
					Counts struct{ FilesSeen, ObjectsDecoded int }
				}
				Diagnostics []any
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !envelope.OK || envelope.Result.Counts.FilesSeen != 1 || envelope.Result.Counts.ObjectsDecoded != 2 || len(envelope.Diagnostics) != 0 {
				t.Fatalf("complete compiler result %s: %v", out.String(), err)
			}
		} else if out.String() != "[OK] Desired state is valid (files seen: 1, objects decoded: 2)\n" {
			t.Fatal(out.String())
		}
	}
	for _, args := range [][]string{{"validate", "--context", "missing"}, {"render", "effective", "--context", "missing"}} {
		var out, errOut bytes.Buffer
		if code := runServices(context.Background(), args, &out, &errOut, services); code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "context.state") {
			t.Fatal("absent context failed without a typed state diagnostic", code, out.String(), errOut.String())
		}
	}
	for _, args := range [][]string{{"version"}, {"validate", "-f", input, "--help"}, {"__bootwright_complete", "validate", ""}, {"validate", "-f", input, "--unknown"}} {
		var out, errOut bytes.Buffer
		runServices(context.Background(), args, &out, &errOut, services)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("read-only composition created workspace state", entries, err)
	}
	got, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("validation modified input")
	}
}

func TestComposedValidationPreservesTypedInputFailures(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("input admission is qualified for linux/amd64")
	}
	var out, errOut bytes.Buffer
	path := filepath.Join(t.TempDir(), "missing.yaml")
	code := run(context.Background(), []string{"validate", "-f", path, "--output", "json"}, &out, &errOut)
	var envelope struct {
		OK          bool
		Result      any
		Diagnostics []struct {
			Code   string
			Source struct{ Path string }
		}
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || code != 1 || errOut.Len() != 0 || envelope.OK || envelope.Result != nil || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "input.not-found" || envelope.Diagnostics[0].Source.Path != path {
		t.Fatalf("typed input failure: %d %s %s %v", code, out.String(), errOut.String(), err)
	}
}
