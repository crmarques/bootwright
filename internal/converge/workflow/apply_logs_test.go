package workflow

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestApplyClusterLogPathLivesUnderItsClusterDir(t *testing.T) {
	runsDir := filepath.Join("ctx", "runs")
	got := ApplyClusterLogPath(runsDir, "apply-1", "dc1-ocp")
	want := filepath.Join(runsDir, "history", "apply-1", "clusters", "dc1-ocp", "cluster.log")
	if got != want {
		t.Fatalf("ApplyClusterLogPath = %q, want %q", got, want)
	}
	if filepath.Dir(got) != ApplyClusterLogDir(runsDir, "apply-1", "dc1-ocp") {
		t.Fatalf("cluster log %q not inside its cluster dir", got)
	}
}

func TestTaskLogPathNestsClusterStepsAndAddons(t *testing.T) {
	runsDir := filepath.Join("ctx", "runs")
	clusterDir := ApplyClusterLogDir(runsDir, "apply-1", "dc1-ocp")
	for _, tc := range []struct {
		name  string
		entry TaskLedgerEntry
		want  string
	}{
		{
			name:  "non-cluster task stays flat",
			entry: TaskLedgerEntry{ID: "provider.kvm-host"},
			want:  filepath.Join(runsDir, "history", "apply-1", "tasks", "provider.kvm-host", ansible.OutputLogName),
		},
		{
			name:  "cluster task nests under steps",
			entry: TaskLedgerEntry{ID: "wait.dc1-ocp", Cluster: "dc1-ocp"},
			want:  filepath.Join(clusterDir, "steps", "wait.dc1-ocp", ansible.OutputLogName),
		},
		{
			name:  "add-on task nests under addons by name",
			entry: TaskLedgerEntry{ID: "addon.dc1-ocp.gitops", Cluster: "dc1-ocp", Addon: "gitops"},
			want:  filepath.Join(clusterDir, "addons", "gitops", ansible.OutputLogName),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TaskLogPath(runsDir, "apply-1", tc.entry); got != tc.want {
				t.Fatalf("TaskLogPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyClusterMarkerLines(t *testing.T) {
	now := time.Date(2026, 6, 24, 9, 30, 0, 0, time.UTC)
	if got, want := applyClusterInitiatedLine(now, "dc1-ocp", "/runs/history/apply-1/bootwright-dc1-ocp.log"),
		"2026-06-24T09:30:00Z dc1-ocp apply initiated. flow logs in: /runs/history/apply-1/bootwright-dc1-ocp.log"; got != want {
		t.Fatalf("initiated line = %q, want %q", got, want)
	}
	if got, want := applyClusterFinishedLine(now, "dc1-ocp", true), "2026-06-24T09:30:00Z dc1-ocp apply finished successfully"; got != want {
		t.Fatalf("finished-ok line = %q, want %q", got, want)
	}
	if got, want := applyClusterFinishedLine(now, "dc1-ocp", false), "2026-06-24T09:30:00Z dc1-ocp apply failed"; got != want {
		t.Fatalf("finished-failed line = %q, want %q", got, want)
	}
}

func TestClusterApplyTerminal(t *testing.T) {
	cases := []struct {
		name     string
		statuses []TaskStatus
		wantDone bool
		wantOK   bool
	}{
		{name: "no tasks", statuses: nil, wantDone: false, wantOK: false},
		{name: "still running", statuses: []TaskStatus{TaskStatusOK, TaskStatusRunning}, wantDone: false, wantOK: false},
		{name: "all clean", statuses: []TaskStatus{TaskStatusOK, TaskStatusSkipped}, wantDone: true, wantOK: true},
		{name: "one failed", statuses: []TaskStatus{TaskStatusOK, TaskStatusFailed}, wantDone: true, wantOK: false},
		{name: "one blocked", statuses: []TaskStatus{TaskStatusOK, TaskStatusBlocked}, wantDone: true, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := make([]TaskLedgerEntry, 0, len(tc.statuses))
			for i, status := range tc.statuses {
				entries = append(entries, TaskLedgerEntry{ID: "t" + string(rune('a'+i)), Cluster: "dc1", Status: status})
			}
			ledger := RunLedger{Tasks: entries}
			done, ok := clusterApplyTerminal(ledger, "dc1")
			if done != tc.wantDone || ok != tc.wantOK {
				t.Fatalf("clusterApplyTerminal = (%v, %v), want (%v, %v)", done, ok, tc.wantDone, tc.wantOK)
			}
		})
	}
}
