package privilege

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type executorFunc func(context.Context, Command) (int, error)

func (f executorFunc) Run(ctx context.Context, c Command) (int, error) { return f(ctx, c) }

type delayFunc func(context.Context, time.Duration) error

func (f delayFunc) Wait(ctx context.Context, d time.Duration) error { return f(ctx, d) }

func TestSupervisorHandsFileStreamsToTheChildUnwrapped(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	var buffered bytes.Buffer
	var child Command
	executor := executorFunc(func(_ context.Context, c Command) (int, error) {
		if reflect.DeepEqual(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) {
			return 1, nil
		}
		child = c
		return 0, nil
	})
	delay := delayFunc(func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	})
	options := SudoOptions{Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo", Executor: executor, Delay: delay, Output: write, Error: &buffered}
	if code, err := NewSupervisor(options).Run(context.Background(), nil); err != nil || code != 0 {
		t.Fatal(code, err)
	}
	if child.Output != io.Writer(write) {
		t.Fatalf("file stream wrapped as %T", child.Output)
	}
	if _, wrapped := child.Error.(synchronizedWriter); !wrapped {
		t.Fatalf("buffered stream not synchronized: %T", child.Error)
	}
}

func TestSupervisorRefreshUsesSameExecutorAndOriginalArgumentVector(t *testing.T) {
	var mu sync.Mutex
	var calls []Command
	refreshed := make(chan struct{})
	active := make(chan struct{})
	var out, stderr bytes.Buffer
	executor := executorFunc(func(ctx context.Context, c Command) (int, error) {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if reflect.DeepEqual(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) {
			if c.Input != nil {
				t.Fatal("policy query received application stdin")
			}
			return 1, nil
		}
		if reflect.DeepEqual(c.Arguments, []string{"-n", "-u", "#0", "-v"}) {
			if c.Input != nil {
				t.Error("refresh received application stdin")
			}
			close(refreshed)
			return 1, nil
		}
		payload, err := io.ReadAll(c.Input)
		if err != nil || string(payload) != "stdin-payload\n" {
			t.Fatalf("stdin forwarding %q %v", payload, err)
		}
		close(active)
		select {
		case <-refreshed:
		case <-ctx.Done():
			return 1, ctx.Err()
		}
		io.WriteString(c.Output, "completed\n")
		return 17, nil
	})
	var waits int
	delay := delayFunc(func(ctx context.Context, d time.Duration) error {
		waits++
		if d != 30*time.Second {
			t.Errorf("interval %v", d)
		}
		select {
		case <-active:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	supervisor := NewSupervisor(SudoOptions{Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo", Executor: executor, Delay: delay, NonInteractive: true, Input: strings.NewReader("stdin-payload\n"), Output: &out, Error: &stderr})
	args := []string{"context", "init", "--name", "test"}
	code, err := supervisor.Run(context.Background(), args)
	if err != nil || code != 17 {
		t.Fatalf("result %d %v", code, err)
	}
	if out.String() != "completed\n" || waits != 1 {
		t.Fatalf("output %q waits %d", out.String(), waits)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("calls %v", calls)
	}
	want := []string{"-u", "#0", "-n", "--", "/opt/bootwright", "context", "init", "--name", "test"}
	if !reflect.DeepEqual(calls[1].Arguments, want) {
		t.Fatalf("args %v", calls[1].Arguments)
	}
	for _, c := range calls {
		if c.Executable != "/usr/bin/sudo" {
			t.Fatal(c.Executable)
		}
		for _, env := range c.Environment {
			if env == "HOME=" {
				t.Fatal("home leaked")
			}
		}
	}
}

func TestSupervisorCancellationJoinsRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	stopped := make(chan struct{})
	executor := executorFunc(func(ctx context.Context, c Command) (int, error) {
		if reflect.DeepEqual(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) {
			return 1, nil
		}
		cancel()
		return 130, nil
	})
	delay := delayFunc(func(ctx context.Context, _ time.Duration) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	code, err := NewSupervisor(SudoOptions{Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo", Executor: executor, Delay: delay}).Run(ctx, nil)
	if code != 130 || err != nil {
		t.Fatalf("result %d %v", code, err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("refresh not joined")
	}
}

func TestRefreshTimeoutPolicy(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       time.Duration
	}{
		{"positive", "timestamp_timeout=2.5", 75 * time.Second},
		{"zero", "timestamp_timeout=0", 0},
		{"negative", "timestamp_timeout=-1", 0},
		{"unknown", "env_reset", 30 * time.Second},
		{"fraction", "timestamp_timeout=0.01", 300 * time.Millisecond},
		{"last override", "timestamp_timeout=2,timestamp_timeout=4", 2 * time.Minute},
		{"malformed", "timestamp_timeout=NaN", 30 * time.Second},
		{"negated", "!timestamp_timeout", 0},
		{"quoted value", `env_keep="X,timestamp_timeout=0,FOO"`, 30 * time.Second},
		{"quoted value before timeout", `env_keep="X,timestamp_timeout=0,FOO",timestamp_timeout=2`, time.Minute},
		{"malformed quoting", `env_keep="X,timestamp_timeout=0`, 30 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := []byte("Matching Defaults entries for operator on host:\n    " + test.body + "\n\nUser operator may run:\n")
			if got := refreshInterval(report); got != test.want {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
	if got := refreshInterval([]byte("Matching Defaults entries for operator on host:\n    timestamp_timeout=2\n\nRunas and Command-specific defaults for operator:\n")); got != 30*time.Second {
		t.Fatal(got)
	}
}

func TestSupervisorCanceledBeforeAnyProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := executorFunc(func(context.Context, Command) (int, error) { t.Fatal("unexpected process"); return 0, nil })
	_, err := NewSupervisor(SudoOptions{Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo", Executor: executor, Delay: Timer{}}).Run(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// The elevated child runs the operator's own SSH sessions, so it needs the
// terminal identity the caller had. Nothing else ambient crosses with it.
func TestSupervisorForwardsOnlyASafeTerminalIdentity(t *testing.T) {
	for _, test := range []struct {
		name, terminal string
		want           bool
	}{
		{"ordinary type", "xterm-256color", true},
		{"unset", "", false},
		{"injected assignment", "xterm\nLD_PRELOAD=/evil.so", false},
		{"injected separator", "xterm=1 2", false},
		{"unbounded", strings.Repeat("x", 65), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var child Command
			executor := executorFunc(func(_ context.Context, c Command) (int, error) {
				if reflect.DeepEqual(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) {
					return 1, nil
				}
				child = c
				return 0, nil
			})
			delay := delayFunc(func(ctx context.Context, _ time.Duration) error {
				<-ctx.Done()
				return ctx.Err()
			})
			options := SudoOptions{
				Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo",
				Executor: executor, Delay: delay, Terminal: test.terminal,
			}
			if _, err := NewSupervisor(options).Run(context.Background(), []string{"machine", "rsh"}); err != nil {
				t.Fatal(err)
			}
			forwarded := slices.Contains(child.Environment, "TERM="+test.terminal)
			if forwarded != test.want {
				t.Fatalf("environment = %v", child.Environment)
			}
			for _, entry := range child.Environment {
				if strings.Count(entry, "=") > 0 && strings.ContainsAny(entry, "\n\r") {
					t.Fatalf("environment carries a separator: %q", entry)
				}
			}
		})
	}
}
