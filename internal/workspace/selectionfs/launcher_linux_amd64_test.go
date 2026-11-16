//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativeexec"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func stateMessage(err error) string {
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "context.state" {
		return ""
	}
	return reported[0].Message
}

func rootServed(t *testing.T, runner func(context.Context, nativeexec.Command) nativeexec.Result) (*Store, string) {
	t.Helper()
	store, home := fixture(t)
	store.effective = func() int { return 0 }
	store.launcher = nativeexec.Launcher{Runner: runner}
	return store, home
}

func helperFor(t *testing.T, calls *int) func(context.Context, nativeexec.Command) nativeexec.Result {
	return func(ctx context.Context, command nativeexec.Command) nativeexec.Result {
		t.Helper()
		*calls++
		credential := command.Credential
		if command.Path != nativeexec.SelfExecutable || len(command.Args) != 1 || command.Args[0] != HelperMode || credential == nil ||
			credential.Uid != uint32(os.Getuid()) || credential.Gid != uint32(os.Getegid()) || command.ParentDeath != syscall.SIGKILL {
			t.Fatalf("root launched %+v", command)
		}
		command.Credential = nil
		return nativeexec.Run(ctx, command)
	}
}

func TestRootServesTheSelectionThroughTheAccountHelper(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the helper never serves root")
	}
	calls := 0
	store, _ := rootServed(t, helperFor(t, &calls))
	ctx := context.Background()
	selection := contexts.Selection{Version: contexts.SelectionVersion, Name: "test"}
	if err := store.Write(ctx, selection); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx); err != nil || got != selection {
		t.Fatalf("read through the helper: %v %v", got, err)
	}
	if err := store.Clear(ctx, selection); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx); err != nil || got != (contexts.Selection{}) {
		t.Fatalf("cleared through the helper: %v %v", got, err)
	}
	if calls != 4 {
		t.Fatalf("root launched %d helpers, want 4", calls)
	}
}

func TestRootReportsAHelperThatFailsOverflowsOrIsCancelled(t *testing.T) {
	answer := func(stdout string, result nativeexec.Result) func(context.Context, nativeexec.Command) nativeexec.Result {
		return func(_ context.Context, command nativeexec.Command) nativeexec.Result {
			if _, err := io.Copy(io.Discard, command.Stdin); err != nil {
				t.Fatal(err)
			}
			command.Stdout.Write([]byte(stdout))
			return result
		}
	}
	exited := nativeexec.Result{Outcome: nativeexec.Exited}
	for _, row := range []struct {
		name   string
		runner func(context.Context, nativeexec.Command) nativeexec.Result
		want   string
	}{
		{"failed", answer("", nativeexec.Result{Outcome: nativeexec.Exited, ExitCode: 1}), "selection account helper failed"},
		{"unstarted", answer("", nativeexec.Result{Outcome: nativeexec.StartFailed, ExitCode: -1}), "selection account helper failed"},
		{"overflowed", answer(strings.Repeat("x", maximumRecord+1), exited), "selection account helper returned no usable response"},
		{"garbled", answer("{", exited), "selection account helper returned no usable response"},
	} {
		t.Run(row.name, func(t *testing.T) {
			store, _ := rootServed(t, row.runner)
			if _, err := store.Read(context.Background()); stateMessage(err) != row.want {
				t.Fatalf("Read = %v, want %q", err, row.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	store, _ := rootServed(t, func(context.Context, nativeexec.Command) nativeexec.Result {
		cancel()
		return nativeexec.Result{Outcome: nativeexec.Cancelled, ExitCode: -1, Err: context.Canceled}
	})
	if _, err := store.Read(ctx); err != context.Canceled {
		t.Fatalf("a cancelled helper gave %v", err)
	}
}

func TestOnlyRootWithAUsableAccountLaunchesAHelper(t *testing.T) {
	forbidden := func(context.Context, nativeexec.Command) nativeexec.Result {
		t.Fatal("a helper was launched")
		return nativeexec.Result{}
	}
	for _, row := range []struct {
		name      string
		effective int
		options   Options
	}{
		{"another unprivileged account", 4242, Options{UID: 1000, GID: 1000}},
		{"root naming a negative user", 0, Options{UID: -1, GID: 0}},
		{"root naming a negative group", 0, Options{UID: 1000, GID: -1}},
	} {
		t.Run(row.name, func(t *testing.T) {
			store := New(row.options)
			store.effective = func() int { return row.effective }
			store.launcher = nativeexec.Launcher{Runner: forbidden}
			if _, err := store.Read(context.Background()); stateMessage(err) != "selection account identity is unavailable" {
				t.Fatalf("Read = %v", err)
			}
		})
	}
}

func TestTheHelperRequestCarriesTheAccountAndAction(t *testing.T) {
	var request bytes.Buffer
	store, _ := rootServed(t, func(_ context.Context, command nativeexec.Command) nativeexec.Result {
		if _, err := io.Copy(&request, command.Stdin); err != nil {
			t.Fatal(err)
		}
		command.Stdout.Write([]byte(`{"selection":{},"failed":false}`))
		return nativeexec.Result{Outcome: nativeexec.Exited}
	})
	if _, err := store.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.String(), `"action":"read"`) || !strings.Contains(request.String(), `"UID":`+strconv.Itoa(os.Getuid())) {
		t.Fatalf("request %s", request.String())
	}
}
