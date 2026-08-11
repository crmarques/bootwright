package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/render"
)

func TestNonStorageDestroyCompletionRetainsEvidenceForUnreachableHosts(t *testing.T) {
	dir := t.TempDir()
	opts := RunOptions{
		ArtifactsRoot: filepath.Join(dir, "artifacts"),
		ExtraVarPairs: []string{storageDestroySkipUnreachableExtraVar + "=true"},
	}
	if err := os.MkdirAll(opts.ArtifactsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"host":"host-b","status":"unreachable"}` + "\n" +
		`{"host":"host-a","status":"ok"}` + "\n" +
		`{"host":"host-a","status":"unreachable"}` + "\n" +
		`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a","host-b"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":1},"host-b":{"ok":0,"failed":0,"skipped":0,"unreachable":1}}}` + "\n"
	if err := os.WriteFile(filepath.Join(opts.ArtifactsRoot, ansible.RunResultName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "destroy.machine-infra", Kind: DestroyTaskKindMachineInfra, ResourceKeys: []string{"machine:host-a", "machine:host-b"}}}
	err := requireNonStorageDestroyCompletion(opts, task, false, []string{"host-a", "host-b"})
	var partial *PartialDestroyOutcomeError
	if !errors.As(err, &partial) {
		t.Fatalf("completion error = %T %v, want PartialDestroyOutcomeError", err, err)
	}
	if !reflect.DeepEqual(partial.Hosts, []string{"host-a", "host-b"}) {
		t.Fatalf("partial hosts = %v", partial.Hosts)
	}
	if partial.Remedy().Action != remedy.ActionRetrySameInvocation {
		t.Fatalf("partial remedy = %+v", partial.Remedy())
	}
	wrapped := failedDestroyTaskResult(opts, task, false, err)
	var wrappedPartial *PartialDestroyOutcomeError
	if !errors.As(wrapped.err, &wrappedPartial) {
		t.Fatalf("destroy task wrapper hid typed partial outcome: %T %v", wrapped.err, wrapped.err)
	}
}

func TestNonStorageDestroyCompletionRequiresTerminalProof(t *testing.T) {
	dir := t.TempDir()
	opts := RunOptions{
		ArtifactsRoot: filepath.Join(dir, "artifacts"),
		ExtraVarPairs: []string{storageDestroySkipUnreachableExtraVar + "=true"},
	}
	if err := os.MkdirAll(opts.ArtifactsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(opts.ArtifactsRoot, ansible.RunResultName)
	if err := os.WriteFile(path, []byte(`{"host":"host-a","status":"ok"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "destroy.provider-services", Kind: DestroyTaskKindProviderServices, ResourceKeys: []string{"host:host-a"}}}
	err := requireNonStorageDestroyCompletion(opts, task, false, []string{"host-a"})
	var partial *PartialDestroyOutcomeError
	if !errors.As(err, &partial) || !strings.Contains(err.Error(), "no terminal proof") {
		t.Fatalf("completion error = %T %v, want missing-terminal partial", err, err)
	}
}

func TestNonStorageDestroyCompletionAcceptsPositiveTerminalProof(t *testing.T) {
	dir := t.TempDir()
	opts := RunOptions{
		ArtifactsRoot: filepath.Join(dir, "artifacts"),
		ExtraVarPairs: []string{storageDestroySkipUnreachableExtraVar + "=true"},
	}
	if err := os.MkdirAll(opts.ArtifactsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"host":"host-a","status":"ok","completion":true}` + "\n" +
		`{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":{"ok":1,"failed":0,"skipped":0,"unreachable":0,"probeUnreachable":0,"completed":1}}}` + "\n"
	if err := os.WriteFile(filepath.Join(opts.ArtifactsRoot, ansible.RunResultName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "destroy.machine-infra", Kind: DestroyTaskKindMachineInfra, ResourceKeys: []string{"machine:host-a"}}}
	if err := requireNonStorageDestroyCompletion(opts, task, false, []string{"host-a"}); err != nil {
		t.Fatalf("positive completion proof refused: %v", err)
	}
}

func TestNonStorageDestroyCompletionClassifiesReachabilityEvidence(t *testing.T) {
	cases := []struct {
		name    string
		events  string
		summary string
		wantErr string
	}{
		{
			name:    "diagnostic probe without completion remains partial",
			events:  `{"host":"host-a","status":"probe-unreachable"}` + "\n",
			summary: `{"ok":0,"failed":0,"skipped":0,"unreachable":0,"probeUnreachable":1,"completed":0}`,
			wantErr: "unreachable host(s) host-a",
		},
		{
			name: "diagnostic probe discharged by exact completion",
			events: `{"host":"host-a","status":"probe-unreachable"}` + "\n" +
				`{"host":"host-a","status":"ok","completion":true}` + "\n",
			summary: `{"ok":1,"failed":0,"skipped":0,"unreachable":0,"probeUnreachable":1,"completed":1}`,
		},
		{
			name: "decisive unreachable contradicts completion",
			events: `{"host":"host-a","status":"unreachable"}` + "\n" +
				`{"host":"host-a","status":"ok","completion":true}` + "\n",
			summary: `{"ok":1,"failed":0,"skipped":0,"unreachable":1,"probeUnreachable":0,"completed":1}`,
			wantErr: "both decisive unreachable and completion evidence",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := RunOptions{ArtifactsRoot: filepath.Join(dir, "artifacts")}
			if err := os.MkdirAll(opts.ArtifactsRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			body := tc.events + `{"schemaVersion":1,"status":"terminal","processedHosts":["host-a"],"hosts":{"host-a":` + tc.summary + `}}` + "\n"
			if err := os.WriteFile(filepath.Join(opts.ArtifactsRoot, ansible.RunResultName), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			task := ApplyTask{Entry: TaskLedgerEntry{ID: "destroy.provider-services", Kind: DestroyTaskKindProviderServices}}
			err := requireNonStorageDestroyCompletion(opts, task, false, []string{"host-a"})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("completion refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("completion error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestNonStorageDestroyCompletionIsRequiredWithoutUnreachableAuthorization(t *testing.T) {
	dir := t.TempDir()
	opts := RunOptions{ArtifactsRoot: filepath.Join(dir, "artifacts")}
	if err := os.MkdirAll(opts.ArtifactsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "destroy.provider-services", Kind: DestroyTaskKindProviderServices}}
	err := requireNonStorageDestroyCompletion(opts, task, false, []string{"provider-host"})
	var partial *PartialDestroyOutcomeError
	if !errors.As(err, &partial) {
		t.Fatalf("ordinary destroy missing proof error = %T %v, want typed partial", err, err)
	}
}

func TestExpectedNonStorageDestroyHostsUsesFullScopeCompletionGroup(t *testing.T) {
	state := minimalState()
	members := render.HostGroupMembers(state)
	var group string
	var expected []string
	for candidate, hosts := range members {
		if len(hosts) > 0 {
			group = candidate
			expected = append([]string(nil), hosts...)
			break
		}
	}
	if group == "" {
		t.Skip("minimal fixture has no inventory hosts")
	}
	task := ApplyTask{
		Entry:               TaskLedgerEntry{ID: "destroy.full-scope", Kind: DestroyTaskKindProviderServices},
		CompletionHostLimit: group,
		State:               state,
	}
	hosts, err := expectedNonStorageDestroyHosts(RunOptions{}, task)
	if err != nil {
		t.Fatalf("full-scope completion hosts: %v", err)
	}
	sort.Strings(expected)
	if !reflect.DeepEqual(hosts, expected) {
		t.Fatalf("full-scope completion hosts = %v, want %v", hosts, expected)
	}
}

func TestExpectedNonStorageDestroyHostsIntersectsBroadTaskLimitWithPlayHosts(t *testing.T) {
	state := minimalState()
	members := render.HostGroupMembers(state)
	var completionGroup string
	var otherGroup string
	var expected []string
	for candidate, hosts := range members {
		if len(hosts) == 0 {
			continue
		}
		candidateSet := map[string]bool{}
		for _, host := range hosts {
			candidateSet[host] = true
		}
		for other, otherHosts := range members {
			for _, host := range otherHosts {
				if !candidateSet[host] {
					completionGroup = candidate
					otherGroup = other
					expected = append([]string(nil), hosts...)
					break
				}
			}
			if otherGroup != "" {
				break
			}
		}
		if otherGroup != "" {
			break
		}
	}
	if completionGroup == "" {
		t.Skip("minimal fixture has no disjoint completion and task-limit hosts")
	}
	task := ApplyTask{
		Entry:               TaskLedgerEntry{ID: "destroy.disjoint", Kind: DestroyTaskKindProviderServices},
		Limit:               completionGroup + ":" + otherGroup,
		CompletionHostLimit: completionGroup,
		State:               state,
	}
	hosts, err := expectedNonStorageDestroyHosts(RunOptions{}, task)
	if err != nil {
		t.Fatalf("resolve intersected completion hosts: %v", err)
	}
	sort.Strings(expected)
	if !reflect.DeepEqual(hosts, expected) {
		t.Fatalf("intersected completion hosts = %v, want play hosts %v", hosts, expected)
	}
}
