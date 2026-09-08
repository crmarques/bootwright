package main

import (
	"bytes"
	"context"
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
		"go: " + runtime.Version() + "\n",
		"target: " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
	} {
		if !strings.Contains(stdout.String(), line) {
			t.Fatalf("version %q does not contain linked runtime %q", stdout.String(), line)
		}
	}
}

func TestCompositionWiresEveryApplicationCommand(t *testing.T) {
	commandsWithSyntheticInputs := [][]string{
		{"context", "init", "--name", "example", "-f", "inputs"},
		{"context", "update", "--name", "example", "-f", "inputs"},
		{"context", "use", "--name", "example"},
		{"context", "list"},
		{"context", "current"},
		{"context", "delete", "--name", "example", "--purge"},
		{"add-ons", "list"},
		{"add-ons", "add", "--name", "example"},
		{"add-ons", "delete", "--name", "example"},
		{"secret", "set", "--name", "example", "--raw-file", "secret.bin"},
		{"secret", "generate"},
		{"secret", "check"},
		{"secret", "list"},
		{"secret", "show", "--name", "example"},
		{"secret", "delete", "--name", "example"},
		{"secret", "encryption", "init"},
		{"secret", "encryption", "status"},
		{"secret", "encryption", "rotate"},
		{"media", "add", "--name", "example.iso", "--from-file", "source.iso"},
		{"media", "list"},
		{"media", "delete", "--name", "example.iso"},
		{"validate"},
		{"preflight", "bastion"},
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
		{"bastion", "setup"},
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
			code := run(context.Background(), args, &stdout, &stderr)
			if code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "[FAIL] cli.not-implemented: bootwright ") {
				t.Fatalf("composition result: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}
