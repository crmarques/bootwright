package comparisonlocal

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

func launchedBy(t *testing.T, runner func(context.Context, nativeexec.Command) nativeexec.Result) Service {
	t.Helper()
	return Service{
		Trees: forbiddenTrees{t}, Account: func(context.Context) (Account, error) { return synthetic, nil },
		effective: func() int { return 0 }, launcher: nativeexec.Launcher{Runner: runner},
	}
}

func TestRootRefusesAnAccountHelperThatFailsOrOverflows(t *testing.T) {
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
		{"failed", answer(`{"directory":"/srv/cmp","failed":false}`, nativeexec.Result{Outcome: nativeexec.Exited, ExitCode: 1}), errHelper.Error()},
		{"unstarted", answer("", nativeexec.Result{Outcome: nativeexec.StartFailed, ExitCode: -1}), errHelper.Error()},
		{"overflowed", answer(strings.Repeat(" ", maximumResponse+1), exited), errHelper.Error()},
		{"garbled", answer("{", exited), "the invoking account's file helper returned no usable response"},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, err := launchedBy(t, row.runner).WriteTrees(context.Background(), "/srv/cmp", nil)
			if found := refusalCode(t, err); found.Code != "render.publish" || found.Message != row.want {
				t.Fatalf("diagnostic %+v, want %q", found, row.want)
			}
		})
	}
}

func TestACancelledAccountHelperReturnsTheCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := launchedBy(t, func(context.Context, nativeexec.Command) nativeexec.Result {
		cancel()
		return nativeexec.Result{Outcome: nativeexec.Cancelled, ExitCode: -1, Err: context.Canceled}
	})
	if _, err := service.WriteTrees(ctx, "/srv/cmp", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled helper gave %v", err)
	}
}

func TestTheAccountHelperIsBoundedByItsDeadline(t *testing.T) {
	service := launchedBy(t, func(ctx context.Context, command nativeexec.Command) nativeexec.Result {
		deadline, ok := ctx.Deadline()
		if !ok || deadline.IsZero() {
			t.Fatal("the helper runs without a deadline")
		}
		command.Stdout.Write([]byte(`{"directory":"/srv/cmp","failed":false}`))
		return nativeexec.Result{Outcome: nativeexec.Exited}
	})
	if got, err := service.WriteTrees(context.Background(), "/srv/cmp", nil); err != nil || got != "/srv/cmp" {
		t.Fatalf("WriteTrees = %q, %v", got, err)
	}
}
