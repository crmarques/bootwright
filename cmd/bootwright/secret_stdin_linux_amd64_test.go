//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSecretPipeInputDrainsThenReturnsEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = writer.Write([]byte("synthetic\n")); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	var result []byte
	buffer := make([]byte, 3)
	for {
		n, err := readInputFD(context.Background(), int(reader.Fd()), buffer)
		result = append(result, buffer[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(result) != "synthetic\n" {
		t.Fatal("pipe bytes changed")
	}
}

func TestComposedSecretStdinIsLazyAndExact(t *testing.T) {
	_, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", secretDocument("opaque", "opaque", ""))
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	canary := "synthetic-stdin-canary\x00\n"
	if _, err := writer.Write([]byte(canary)); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	reads := 0
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace = repository, repository
	deps.SecretInput = secretInputFunc(func(ctx context.Context, buffer []byte) (int, error) {
		reads++
		return readInputFD(ctx, int(reader.Fd()), buffer)
	})
	services := assembleServices(deps)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	stdout, stderr := contextRun(t, services, 0, "secret", "set", "--name", "opaque", "--value-stdin")
	if reads == 0 || strings.Contains(stdout+stderr, "synthetic-stdin-canary") {
		t.Fatal("stdin was not acquired confidentially")
	}
	stdout, stderr = contextRun(t, services, 0, "secret", "show", "--name", "opaque", "--part", "value")
	if stdout != canary || stderr != "" {
		t.Fatal("stdin bytes did not survive explicit reveal exactly")
	}
	before := reads
	contextRun(t, services, 1, "secret", "set", "--name", "opaque", "--value-stdin")
	contextRun(t, services, 1, "secret", "set", "--name", "opaque", "--value-stdin", "--password-stdin=false", "--yes")
	if reads != before {
		t.Fatal("replacement or inapplicable input consumed stdin before authorization")
	}
}
