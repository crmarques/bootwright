package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"go.yaml.in/yaml/v3"
)

func TestPresentationPreservesSessionDescriptors(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errOut, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errOut.Close()
	args := []string{"machine", "exec", "--name", "example", "--", "uptime"}
	process, _ := interactiveProcess(cli.ClassifyInvocation(args), args, out, errOut, controller.Route{})
	if process.Streams.In != os.Stdin || process.Streams.Out != out || process.Streams.Err != errOut {
		t.Fatal("presentation changed a session's inherited descriptors")
	}
}

func TestStructuredTerminalSecretInputRefusesBeforePrivilege(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		var out, stderr bytes.Buffer
		probes := 0
		boundary := privilegeBoundary{inputTerminal: func() (bool, error) { probes++; return true, nil }}
		args := []string{"secret", "set", "--name", "example", "--value-stdin", "--output", format}
		if code := boundary.run(context.Background(), args, &out, &stderr); code != 2 || probes != 1 || stderr.Len() != 0 {
			t.Fatalf("%s: code=%d probes=%d stderr=%q", format, code, probes, stderr.String())
		}
		var envelope struct {
			Command  string `json:"command" yaml:"command"`
			ExitCode int    `json:"exitCode" yaml:"exitCode"`
		}
		decode := json.Unmarshal
		if format == "yaml" {
			decode = yaml.Unmarshal
		}
		if err := decode(out.Bytes(), &envelope); err != nil || envelope.Command != "secret set" || envelope.ExitCode != 2 {
			t.Fatalf("%s: error=%v output=%q", format, err, out.String())
		}
		process, _ := interactiveProcess(cli.ClassifyInvocation(args), args, &out, &stderr, controller.Route{})
		if _, ok := process.SecretTerminal.(structuredSecretTerminal); !ok {
			t.Fatalf("%s did not bind noninteractive secret input", format)
		}
	}
}

type presentationTerminal struct {
	interactive bool
	reads       int
}

func (t *presentationTerminal) Interactive() (bool, error) { return t.interactive, nil }
func (t *presentationTerminal) ReadHidden(context.Context, string, []byte) (int, error) {
	t.reads++
	return 0, nil
}

func TestStructuredSecretTerminalPreservesPipesAndRefusesHiddenReads(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		input := &presentationTerminal{interactive: interactive}
		terminal := structuredSecretTerminal{source: input}
		actual, err := terminal.Interactive()
		if actual || interactive != errors.Is(err, errStructuredSecretTerminal) {
			t.Fatalf("interactive=%t: actual=%t error=%v", interactive, actual, err)
		}
		if _, err := terminal.ReadHidden(context.Background(), "", make([]byte, 1)); !errors.Is(err, errStructuredSecretTerminal) || input.reads != 0 {
			t.Fatalf("hidden read reached source: %d, %v", input.reads, err)
		}
	}
}

func TestElevationCarriesNoColorWithoutChangingPayload(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, color := range []string{"auto", "always", "never"} {
		args := []string{"machine", "exec", "--name", "example", "--color", color, "--", "printf", "--color", "always"}
		invocation := elevationInvocation(cli.ClassifyInvocation(args), args, controller.Route{}, &bytes.Buffer{}, &bytes.Buffer{})
		options := cli.ResolvePresentation(invocation.Arguments, cli.PresentationOptions{})
		want := color
		if want == "auto" {
			want = "never"
		}
		if options.Color != want {
			t.Fatalf("color %s became %s across elevation", color, options.Color)
		}
		payload := invocation.Arguments[len(invocation.Arguments)-3:]
		if payload[0] != "printf" || payload[1] != "--color" || payload[2] != "always" {
			t.Fatalf("remote payload changed: %q", invocation.Arguments)
		}
	}
}
