//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
)

// promptWriter hands each prompt to the test as it is written.
type promptWriter chan string

func (w promptWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func termiosOf(t *testing.T, terminal *os.File) syscall.Termios {
	t.Helper()
	var settings syscall.Termios
	if err := termiosControl(int(terminal.Fd()), syscall.TCGETS, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

// drain reads what the terminal wrote back to its master for a while.
func drain(master *os.File, wait time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	var echoed strings.Builder
	buffer := make([]byte, 64)
	for {
		n, err := readInputFD(ctx, int(master.Fd()), buffer)
		echoed.Write(buffer[:n])
		if err != nil {
			return echoed.String()
		}
	}
}

type hiddenRead struct {
	value string
	err   error
}

func readHiddenLine(ctx context.Context, terminal secretTerminal) chan hiddenRead {
	done := make(chan hiddenRead, 1)
	go func() {
		buffer := make([]byte, 64)
		n, err := terminal.ReadHidden(ctx, "Password for Secret fixture in context lab: ", buffer)
		done <- hiddenRead{string(buffer[:n]), err}
	}()
	return done
}

func awaitPrompt(t *testing.T, prompts promptWriter) string {
	t.Helper()
	select {
	case prompt := <-prompts:
		return prompt
	case <-time.After(10 * time.Second):
		t.Fatal("no prompt was written")
	}
	return ""
}

// A password typed at a terminal is read with echo off after a prompt on the
// error stream, and the terminal's settings are restored once it is read and
// when the read is canceled (D73).
func TestTerminalSecretPromptTurnsEchoOffAndRestoresIt(t *testing.T) {
	master, slave := openTestTerminal(t)
	original := termiosOf(t, slave)
	if original.Lflag&syscall.ECHO == 0 || original.Lflag&syscall.ICANON == 0 {
		t.Skipf("the pseudo-terminal does not start echoing canonical lines (lflag %#o)", original.Lflag)
	}
	prompts := make(promptWriter, 1)
	terminal := newSecretTerminal(slave, prompts)
	if interactive, err := terminal.Interactive(); err != nil || !interactive {
		t.Fatalf("a terminal reads as interactive %v (%v)", interactive, err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if interactive, err := newSecretTerminal(reader, prompts).Interactive(); err != nil || interactive {
		t.Fatalf("a pipe reads as interactive %v (%v)", interactive, err)
	}

	done := readHiddenLine(context.Background(), terminal)
	if prompt := awaitPrompt(t, prompts); prompt != "Password for Secret fixture in context lab: " {
		t.Fatalf("prompt = %q", prompt)
	}
	waiting := termiosOf(t, slave)
	if waiting.Lflag&(syscall.ECHO|syscall.ECHOE|syscall.ECHOK) != 0 || waiting.Lflag&syscall.ECHONL == 0 ||
		waiting.Lflag&(syscall.ICANON|syscall.ISIG) != original.Lflag&(syscall.ICANON|syscall.ISIG) {
		t.Fatalf("waiting lflag = %#o from %#o", waiting.Lflag, original.Lflag)
	}
	if _, err := master.Write([]byte("typed-password\n")); err != nil {
		t.Fatal(err)
	}
	var read hiddenRead
	select {
	case read = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the typed line was not read")
	}
	if read.err != nil || read.value != "typed-password\n" {
		t.Fatalf("read %q (%v)", read.value, read.err)
	}
	if echoed := drain(master, 300*time.Millisecond); strings.Contains(echoed, "typed") || !strings.Contains(echoed, "\n") {
		t.Fatalf("the terminal echoed %q", echoed)
	}
	if restored := termiosOf(t, slave); restored != original {
		t.Fatalf("restored lflag %#o, want %#o", restored.Lflag, original.Lflag)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done = readHiddenLine(ctx, terminal)
	awaitPrompt(t, prompts)
	cancel()
	select {
	case read = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a canceled read did not return")
	}
	if !errors.Is(read.err, context.Canceled) || read.value != "" {
		t.Fatalf("a canceled read returned %q (%v)", read.value, read.err)
	}
	if restored := termiosOf(t, slave); restored != original {
		t.Fatalf("restored lflag after cancellation %#o, want %#o", restored.Lflag, original.Lflag)
	}
}

// recordingTerminal is an interactive terminal at which line is typed.
type recordingTerminal struct {
	line    string
	prompts []string
}

func (r *recordingTerminal) Interactive() (bool, error) { return true, nil }

func (r *recordingTerminal) ReadHidden(_ context.Context, prompt string, buffer []byte) (int, error) {
	r.prompts = append(r.prompts, prompt)
	return copy(buffer, r.line), nil
}

// The composed graph hands secret set the terminal behind standard input: a
// token typed there is prompted for and stored without its line feed, and an
// opaque value refuses before anything is read (D73). An interactive process
// binds that terminal over standard input and its error stream.
func TestTheComposedSecretSetReadsFromTheTerminalBehindStandardInput(t *testing.T) {
	_, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "opaque.yaml", secretDocument("opaque", "opaque", ""))
	addSecretInput(t, input, "token.yaml", secretDocument("token", "token", ""))
	terminal := &recordingTerminal{line: "typed-token\n"}
	reads := 0
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace = repository, repository
	deps.SecretInput = secretInputFunc(func(context.Context, []byte) (int, error) {
		reads++
		return 0, io.EOF
	})
	deps.SecretTerminal = terminal
	services := assembleServices(deps)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")

	_, stderr := contextRun(t, services, 2, "secret", "set", "--name", "opaque", "--value-stdin")
	if !strings.Contains(stderr, "pipe the value into bootwright secret set --context alpha --name opaque --value-stdin, or use --value-file <path>") ||
		len(terminal.prompts) != 0 || reads != 0 {
		t.Fatalf("an opaque value at a terminal: %d prompts, %d reads, stderr %q", len(terminal.prompts), reads, stderr)
	}
	stdout, stderr := contextRun(t, services, 0, "secret", "set", "--name", "token", "--value-stdin")
	if len(terminal.prompts) != 1 || terminal.prompts[0] != "Token for Secret token in context alpha: " || reads != 0 ||
		strings.Contains(stdout+stderr, "typed-token") {
		t.Fatalf("a token at a terminal: prompts %q, %d reads", terminal.prompts, reads)
	}
	if stdout, stderr = contextRun(t, services, 0, "secret", "show", "--name", "token", "--part", "value"); stdout != "typed-token" || stderr != "" {
		t.Fatal("the typed token was not stored without its line feed")
	}

	var errOut bytes.Buffer
	process, _ := interactiveProcess(cli.ClassifyInvocation([]string{"secret", "set", "--name", "token", "--value-stdin"}), io.Discard, &errOut, controller.Route{})
	want := newSecretTerminal(os.Stdin, &errOut)
	if process.SecretTerminal != want {
		t.Fatalf("an interactive process bound %#v as the secret terminal, want %#v", process.SecretTerminal, want)
	}
	local, release := localServiceDependencies(process)
	defer release()
	if local.SecretTerminal != want {
		t.Fatalf("the local services bound %#v as the secret terminal, want %#v", local.SecretTerminal, want)
	}
}
