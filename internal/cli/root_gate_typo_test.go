package cli

import (
	"strings"
	"testing"
	"time"
)

func TestGateStaysRootlessForTyposAndBogusStages(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"statsu"}, want: false},
		{args: []string{"frobnicate"}, want: false},
		{args: []string{"plan"}, want: true},
		{args: []string{"diff"}, want: true},
		{args: []string{"status"}, want: true},
		{args: []string{"machine", "lst"}, want: false},
		{args: []string{"context", "lst"}, want: false},
		{args: []string{"secret", "lst"}, want: false},
		{args: []string{"media", "lst"}, want: false},
		{args: []string{"cluster", "lst"}, want: false},
		{args: []string{"secret", "generate"}, want: true},
		{args: []string{"secret", "list"}, want: true},
		{args: []string{"secret", "check"}, want: true},
		{args: []string{"secret", "encryption", "status"}, want: true},
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

func TestDispatcherUsageErrorsExitTwo(t *testing.T) {
	cases := [][]string{
		{"preflight"},
		{"machine"},
		{"preflight", "hub"},
		{"render", "bogus"},
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
	if got := statusWatchRefreshPreamble(true, true, now); got != "\x1b[H\x1b[2J" {
		t.Fatalf("tty first preamble = %q, want clear-screen escape", got)
	}
	if got := statusWatchRefreshPreamble(true, false, now); got != "\x1b[H\x1b[2J" {
		t.Fatalf("tty subsequent preamble = %q, want clear-screen escape", got)
	}
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
