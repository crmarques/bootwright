//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestACommandThatKeepsItsPipesOpenAfterCancellationReturnsWithinTheBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runCommand(ctx, command{Path: "/bin/sh", Arguments: []string{"-c", "sleep 15 & sleep 15"}, Directory: "/"})
	if err == nil {
		t.Fatal("a command past its deadline reported success")
	}
	if elapsed := time.Since(started); elapsed > commandWaitDelay+5*time.Second {
		t.Fatalf("runCommand blocked %s on a descendant holding its pipes", elapsed)
	}
}

func TestAFailingCommandCarriesTheBoundedPrintableCause(t *testing.T) {
	script := "head -c 200000 /dev/zero | tr '\\0' 'x' >&2; printf '\\nfirst\\033[31m line\\nERROR: no matching distribution\\n' >&2; exit 3"
	_, err := runCommand(context.Background(), command{Path: "/bin/sh", Arguments: []string{"-c", script}, Directory: "/"})
	if err == nil {
		t.Fatal("a failing command reported success")
	}
	text := err.Error()
	if !strings.Contains(text, "exit status 3") || !strings.Contains(text, "ERROR: no matching distribution") {
		t.Fatalf("error = %q, want the exit status and the last stderr line", text)
	}
	if len(text) > stderrTail+64 {
		t.Fatalf("error is %d bytes, want at most the %d byte tail", len(text), stderrTail)
	}
	if strings.ContainsAny(text, "\x1b\n") || strings.Contains(text, "first") && strings.Contains(text, "\x1b") {
		t.Fatalf("error = %q is not printable on one line", text)
	}
}

func TestEveryFailedBuildStepNamesItsCause(t *testing.T) {
	steps := []struct {
		name  string
		match func([]string) bool
	}{
		{"venv", func(arguments []string) bool { return slices.Contains(arguments, "venv") }},
		{"pip", func(arguments []string) bool { return slices.Contains(arguments, "--require-hashes") }},
		{"compileall", func(arguments []string) bool { return slices.Contains(arguments, "compileall") }},
		{"probe", func(arguments []string) bool { return slices.Contains(arguments, venvProbe) }},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			host, adapter, interpreter := buildable(t)
			adapter.run = func(ctx context.Context, request command) ([]byte, error) {
				if step.match(request.Arguments) {
					return nil, errors.New("exit status 1: the cause " + step.name)
				}
				return host.run(ctx, request)
			}
			_, err := testBuild(adapter, context.Background(), interpreter)
			if err == nil || !strings.Contains(told(err), "the cause "+step.name) {
				t.Fatalf("build error = %q, want the %s cause", told(err), step.name)
			}
		})
	}
}
