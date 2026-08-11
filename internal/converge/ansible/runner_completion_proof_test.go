package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRunResultRequiresMatchingLastTerminalProof(t *testing.T) {
	validEvent := `{"host":"host-a","status":"ok"}` + "\n"
	validTerminal := `{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"completed":0}}}` + "\n"
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "truncated after valid event", body: validEvent, want: "no terminal proof"},
		{name: "missing terminal after two events", body: validEvent + `{"host":"host-b","status":"unreachable"}` + "\n", want: "no terminal proof"},
		{name: "terminal not last", body: validTerminal + validEvent, want: "not the last record"},
		{name: "duplicate terminal", body: validEvent + validTerminal + validTerminal, want: "duplicate terminal proof"},
		{name: "summary omits event", body: validEvent + `{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{}}` + "\n", want: "host set does not match"},
		{name: "summary count mismatch", body: validEvent + `{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":2,"failed":0,"skipped":0,"unreachable":0,"completed":0}}}` + "\n", want: "does not match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), RunResultName)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadRunResult(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ReadRunResult error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReadRunResultBindsTerminalProofToEveryExpectedHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunResultName)
	body := `{"host":"host-a","status":"ok","completion":true}` + "\n" +
		`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"completed":1}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRunResultForHosts(path, []string{"host-a", "host-b"}); err == nil || !strings.Contains(err.Error(), "do not match selected hosts") {
		t.Fatalf("omitted selected host error = %v", err)
	}
}

func TestReadRunResultRejectsProcessedHostWithNoCompletionEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunResultName)
	body := `{"host":"host-a","status":"ok"}` + "\n" +
		`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a","host-b"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"completed":0},"host-b":{"ok":0,"failed":0,"skipped":0,"unreachable":0,"completed":0}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRunResultForHosts(path, []string{"host-a", "host-b"}); err == nil || !strings.Contains(err.Error(), "completion events") {
		t.Fatalf("missing completion error = %v", err)
	}
}

func TestReadRunResultRejectsUnexpectedHostForExplicitEmptySelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunResultName)
	body := `{"host":"host-a","status":"ok"}` + "\n" +
		`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"completed":0}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRunResultForHosts(path, []string{}); err == nil || !strings.Contains(err.Error(), "do not match selected hosts") {
		t.Fatalf("unexpected host for empty selection error = %v", err)
	}
}

func TestReadRunResultAcceptsTerminalSummaryAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RunResultName)
	body := `{"host":"host-b","status":"unreachable"}` + "\n" +
		`{"host":"host-a","status":"ok"}` + "\n" +
		`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a","host-b"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"completed":0},"host-b":{"ok":0,"failed":0,"skipped":0,"unreachable":1,"completed":0}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := ReadRunResult(path)
	if err != nil || len(records) != 2 {
		t.Fatalf("ReadRunResult = %+v, %v", records, err)
	}
	link := filepath.Join(dir, "linked-result.jsonl")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRunResult(link); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestReadRunResultForHostsRequiresOneSuccessfulDestroyCompletionPerReachableHost(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "valid completion",
			body: `{"host":"host-a","status":"ok","completion":true}` + "\n" +
				`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"completed":1}}}` + "\n",
		},
		{
			name: "positive absence completion follows diagnostic reachability probe",
			body: `{"host":"host-a","status":"probe-unreachable"}` + "\n" +
				`{"host":"host-a","status":"ok","completion":true}` + "\n" +
				`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"probeUnreachable":1,"completed":1}}}` + "\n",
		},
		{
			name: "failed before completion",
			body: `{"host":"host-a","status":"failed"}` + "\n" +
				`{"host":"host-a","status":"ok","completion":true}` + "\n" +
				`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":1,"skipped":0,"unreachable":0,"completed":1}}}` + "\n",
			want: "failed destroy events",
		},
		{
			name: "skipped only",
			body: `{"host":"host-a","status":"skipped"}` + "\n" +
				`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":0,"failed":0,"skipped":1,"unreachable":0,"completed":0}}}` + "\n",
			want: "completion events",
		},
		{
			name: "duplicate completion",
			body: `{"host":"host-a","status":"ok","completion":true}` + "\n" +
				`{"host":"host-a","status":"ok","completion":true}` + "\n" +
				`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":2,"failed":0,"skipped":0,"unreachable":0,"completed":2}}}` + "\n",
			want: "want 1",
		},
		{
			name: "completion on skipped event",
			body: `{"host":"host-a","status":"skipped","completion":true}` + "\n" +
				`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":0,"failed":0,"skipped":1,"unreachable":0,"completed":0}}}` + "\n",
			want: "completion on skipped",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), RunResultName)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := ReadRunResultForHosts(path, []string{"host-a"})
			if tc.want == "" {
				if err != nil {
					t.Fatalf("valid completion refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
