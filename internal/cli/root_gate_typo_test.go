package cli

import (
	"strings"
	"testing"
	"time"
)

// TestGateStaysRootlessForTyposAndBogusStages verifies that an invocation cobra
// would reject (an unknown top-level command, an unknown subcommand, or a bogus
// apply --stage/--through value) does NOT escalate to sudo, so the caller sees
// cobra's error instead of a doomed sudo password prompt. It also pins the
// read-only inspectors and the known subcommands that must still escalate.
func TestGateStaysRootlessForTyposAndBogusStages(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		// Unknown top-level tokens (typos) stay rootless.
		{args: []string{"statsu"}, want: false},
		{args: []string{"frobnicate"}, want: false},
		// Read-only inspectors that read the root-owned context escalate.
		{args: []string{"plan"}, want: true},
		{args: []string{"diff"}, want: true},
		{args: []string{"status"}, want: true},
		// Unknown subcommands of a dispatcher family stay rootless.
		{args: []string{"machine", "lst"}, want: false},
		{args: []string{"context", "lst"}, want: false},
		{args: []string{"secret", "lst"}, want: false},
		{args: []string{"media", "lst"}, want: false},
		{args: []string{"cluster", "lst"}, want: false},
		// Known subcommands that read/write the root-owned context still escalate.
		{args: []string{"secret", "generate"}, want: true},
		{args: []string{"secret", "list"}, want: true},
		{args: []string{"secret", "check"}, want: true},
		{args: []string{"secret", "encryption", "status"}, want: true},
		// apply escalates for a valid stage, stays rootless for a bogus one.
		{args: []string{"apply"}, want: true},
		{args: []string{"apply", "--stage", "infra"}, want: true},
		{args: []string{"apply", "--through", "base"}, want: true},
		{args: []string{"apply", "--stage", "bogus"}, want: false},
		{args: []string{"apply", "--stage=bogus"}, want: false},
		{args: []string{"apply", "--through", "bogus"}, want: false},
		{args: []string{"apply", "--through=bogus"}, want: false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := argsNeedLocalRoot(tc.args); got != tc.want {
				t.Fatalf("argsNeedLocalRoot(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestDispatcherUsageErrorsExitTwo verifies the exit-code contract: a bare
// dispatcher (missing subcommand) and an unknown positional are usage errors
// that exit 2, not a silent success (0) or a run failure (1).
func TestDispatcherUsageErrorsExitTwo(t *testing.T) {
	cases := [][]string{
		{"preflight"},        // gate family: bare invocation must fail, not pass green
		{"machine"},          // bare dispatcher
		{"preflight", "hub"}, // unknown subcommand positional
		{"render", "bogus"},  // unknown render target positional
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, code := runCLI(t, args...)
			if code != 2 {
				t.Fatalf("bootwright %s exit = %d, want 2", strings.Join(args, " "), code)
			}
		})
	}
}

func TestStatusWatchRefreshPreamble(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	// On a TTY every refresh clears the screen and homes the cursor so reports
	// redraw in place instead of stacking onto scrollback.
	if got := statusWatchRefreshPreamble(true, true, now); got != "\x1b[H\x1b[2J" {
		t.Fatalf("tty first preamble = %q, want clear-screen escape", got)
	}
	if got := statusWatchRefreshPreamble(true, false, now); got != "\x1b[H\x1b[2J" {
		t.Fatalf("tty subsequent preamble = %q, want clear-screen escape", got)
	}
	// Piped: the first refresh emits nothing extra; later refreshes get a
	// timestamped separator, not an ANSI escape.
	if got := statusWatchRefreshPreamble(false, true, now); got != "" {
		t.Fatalf("piped first preamble = %q, want empty", got)
	}
	got := statusWatchRefreshPreamble(false, false, now)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("piped preamble %q must not emit ANSI cursor control", got)
	}
	if !strings.Contains(got, "status refresh") || !strings.Contains(got, "2026-07-06T12:00:00Z") {
		t.Fatalf("piped preamble %q missing timestamped separator", got)
	}
}
