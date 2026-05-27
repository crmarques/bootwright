package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestParseSudoTimestampTimeout(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   time.Duration
		ok     bool
	}{
		{name: "minutes", output: "Authentication timestamp timeout: 5.0 minutes\n", want: 5 * time.Minute, ok: true},
		{name: "seconds", output: "Authentication timestamp timeout: 90 seconds\n", want: 90 * time.Second, ok: true},
		{name: "hours", output: "Authentication timestamp timeout: 1 hour\n", want: time.Hour, ok: true},
		{name: "missing", output: "Sudo version 1.9.15\n", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSudoTimestampTimeout(tc.output)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("parseSudoTimestampTimeout() = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLocalSudoSessionKeepAliveRefreshesNonInteractiveSession(t *testing.T) {
	calls := make(chan []string, 4)
	session := &localSudoSession{
		authMethod:     localSudoAuthNonInteractive,
		keepAliveEvery: 10 * time.Millisecond,
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name != "sudo" {
				t.Fatalf("command name = %q, want sudo", name)
			}
			calls <- append([]string(nil), args...)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLocalSudoSessionHelperProcess", "--")
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_LOCAL_SUDO_SESSION_HELPER=1")
			return cmd
		},
	}

	stop := session.keepAlive(context.Background())
	defer stop()

	select {
	case got := <-calls:
		if !reflect.DeepEqual(got, []string{"-n", "-v"}) {
			t.Fatalf("keepalive sudo args = %v, want -n -v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sudo keepalive")
	}
}

func TestLocalSudoSessionKeepAliveRefreshesWithCapturedPassword(t *testing.T) {
	calls := make(chan []string, 1)
	session := &localSudoSession{
		password: "secret",
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name != "sudo" {
				t.Fatalf("command name = %q, want sudo", name)
			}
			calls <- append([]string(nil), args...)
			cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestLocalSudoSessionHelperProcess", "--"}, args...)...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_LOCAL_SUDO_SESSION_HELPER=1")
			return cmd
		},
	}

	if err := session.refreshKeepAlive(context.Background()); err != nil {
		t.Fatalf("refreshKeepAlive: %v", err)
	}
	got := <-calls
	if !reflect.DeepEqual(got, []string{"-S", "-p", "", "-v"}) {
		t.Fatalf("keepalive sudo args = %v, want password refresh", got)
	}
}

func TestLocalSudoSessionHelperProcess(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_LOCAL_SUDO_SESSION_HELPER") != "1" {
		return
	}
	args := os.Args
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		os.Exit(2)
	}
	sudoArgs := args[sep+1:]
	if reflect.DeepEqual(sudoArgs, []string{"-S", "-p", "", "-v"}) {
		body, err := io.ReadAll(os.Stdin)
		if err != nil || string(body) != "secret\n" {
			os.Exit(2)
		}
	}
	os.Exit(0)
}
