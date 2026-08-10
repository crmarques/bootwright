package oc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
)

func TestCommandRunnerReportsLogAppendError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Command: "true",
		LogPath: filepath.Join(blocker, "oc.log"),
	}

	out, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "namespace"}, nil)
	if err == nil {
		t.Fatal("Run succeeded despite unwritable log path")
	}
	if len(out) != 0 {
		t.Fatalf("Run output = %q, want empty", out)
	}
	if got := err.Error(); !strings.Contains(got, "append oc log") || !strings.Contains(got, "not a directory") {
		t.Fatalf("Run error did not explain log append failure: %v", err)
	}
}

func TestCommandRunnerReportsCommandAndLogErrors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Command: "false",
		LogPath: filepath.Join(blocker, "oc.log"),
	}

	_, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "namespace"}, nil)
	if err == nil {
		t.Fatal("Run succeeded despite command and log failures")
	}
	if got := err.Error(); !strings.Contains(got, "run false") || !strings.Contains(got, "also failed to append oc log") {
		t.Fatalf("Run error did not include command and log failures: %v", err)
	}
}

func TestCommandRunnerReturnsStdoutWithoutStderr(t *testing.T) {
	runner := scriptedGetRunner(t,
		`{"metadata":{"name":"installed"}}`,
		"Warning: v1 ClusterServiceVersion is deprecated\n",
	)
	ctx := context.Background()

	out, err := runner.Run(ctx, "/tmp/kubeconfig", []string{"get", "namespace", "installed", "-o", "json"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(string(out), "Warning") {
		t.Fatalf("Run returned stderr in its output: %q", out)
	}

	obj, err := getNamedResource(ctx, runner, "/tmp/kubeconfig", "namespace", "", "installed")
	if err != nil {
		t.Fatalf("getNamedResource: %v", err)
	}
	if got := nestedString(obj, "metadata", "name"); got != "installed" {
		t.Fatalf("decoded name = %q, want installed", got)
	}
}

func TestReadinessChecksDecodeDespiteStderrWarnings(t *testing.T) {
	ctx := context.Background()

	existsRunner := scriptedGetRunner(t,
		`{"metadata":{"name":"installed"}}`,
		"Warning: server uses an insecure TLS certificate\n",
	)
	ready, detail, err := Ready(ctx, existsRunner, "/tmp/kubeconfig", readyExtensionPlan().Extension)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !ready {
		t.Fatalf("Ready = false, detail %q", detail)
	}

	csvRunner := scriptedCSVRunner(t)
	ready, detail, err = csvSucceeded(ctx, csvRunner, "/tmp/kubeconfig", "installed", "ready-operator")
	if err != nil {
		t.Fatalf("csvSucceeded: %v", err)
	}
	if !ready {
		t.Fatalf("csvSucceeded = false, detail %q", detail)
	}
}

func scriptedGetRunner(t *testing.T, stdout, stderr string) CommandRunner {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *get*) printf '%%s' %s; printf '%%s' %s 1>&2; exit 0 ;;
  *) printf 'unexpected args: %%s\n' "$*" 1>&2; exit 1 ;;
esac
`, shellQuote(stdout), shellQuote(stderr))
	return CommandRunner{Command: writeScript(t, script), Stdout: io.Discard, Stderr: io.Discard}
}

func scriptedCSVRunner(t *testing.T) CommandRunner {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *subscription*) printf '%%s' %s; printf '%%s' %s 1>&2; exit 0 ;;
  *clusterserviceversion*) printf '%%s' %s; printf '%%s' %s 1>&2; exit 0 ;;
  *) printf 'unexpected args: %%s\n' "$*" 1>&2; exit 1 ;;
esac
`,
		shellQuote(`{"status":{"installedCSV":"ready-operator.v1.0.0"}}`),
		shellQuote("Warning: deprecated apiVersion\n"),
		shellQuote(`{"spec":{"version":"1.0.0"},"status":{"phase":"Succeeded"}}`),
		shellQuote("Warning: deprecated apiVersion\n"),
	)
	return CommandRunner{Command: writeScript(t, script), Stdout: io.Discard, Stderr: io.Discard}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oc")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type barrierGetRunner struct {
	want     int
	release  chan struct{}
	closer   sync.Once
	notFound map[string]bool

	mu      sync.Mutex
	arrived int
}

func (r *barrierGetRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	if len(args) < 3 || args[0] != "get" {
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
	name := args[2]
	r.mu.Lock()
	r.arrived++
	reached := r.arrived >= r.want
	r.mu.Unlock()
	if reached {
		r.closer.Do(func() { close(r.release) })
	}
	select {
	case <-r.release:
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("readiness checks did not overlap: only %d of %d checks were in flight", r.arrived, r.want)
	}
	if r.notFound[name] {
		return nil, fmt.Errorf("namespaces %q not found", name)
	}
	return []byte(`{"metadata":{"name":"` + name + `"}}`), nil
}

func namespaceReadinessExtension(names ...string) v1alpha1.ClusterAddon {
	var checks []v1alpha1.ClusterAddonReadinessCheck
	for _, name := range names {
		checks = append(checks, v1alpha1.ClusterAddonReadinessCheck{
			ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{
				APIVersion: "v1", Kind: "Namespace", Name: name,
			},
		})
	}
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "multi-check"},
		Spec: v1alpha1.ClusterAddonSpec{
			Readiness: v1alpha1.ClusterAddonReadiness{Timeout: "30m", Checks: checks},
		},
	}
}

func TestReadyRunsDeclaredChecksConcurrently(t *testing.T) {
	extension := namespaceReadinessExtension("a", "b", "c")
	runner := &barrierGetRunner{want: 3, release: make(chan struct{})}

	ready, observed, err := Ready(context.Background(), runner, "/tmp/kubeconfig", extension)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !ready {
		t.Fatalf("Ready = false, observed %q", observed)
	}
	want := "Namespace/a Exists; Namespace/b Exists; Namespace/c Exists"
	if observed != want {
		t.Fatalf("observed = %q, want %q", observed, want)
	}
}

func TestReadyReportsLowestIndexBlockerRegardlessOfCompletionOrder(t *testing.T) {
	extension := namespaceReadinessExtension("a", "b", "c")
	runner := &barrierGetRunner{want: 3, release: make(chan struct{}), notFound: map[string]bool{"b": true, "c": true}}

	ready, observed, err := Ready(context.Background(), runner, "/tmp/kubeconfig", extension)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if ready {
		t.Fatal("Ready = true despite two failing checks")
	}
	want := "Namespace/a Exists; Namespace/b NotFound"
	if observed != want {
		t.Fatalf("observed = %q, want %q (the lowest-index failing check is the blocker and later checks stay unreported)", observed, want)
	}
}

type csvEvidenceRunner struct {
	delays         map[string]time.Duration
	omitCSVVersion bool
}

func (r csvEvidenceRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	if len(args) < 3 || args[0] != "get" {
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
	name := args[2]
	switch args[1] {
	case "subscription.operators.coreos.com":
		return []byte(`{"status":{"installedCSV":"` + name + `.v1"}}`), nil
	case "clusterserviceversion.operators.coreos.com":
		if delay := r.delays[name]; delay > 0 {
			time.Sleep(delay)
		}
		if r.omitCSVVersion {
			return []byte(`{"status":{"phase":"Succeeded"}}`), nil
		}
		return []byte(`{"spec":{"version":"1.2.3"},"status":{"phase":"Succeeded"}}`), nil
	default:
		return []byte(`{"metadata":{"name":"` + name + `"}}`), nil
	}
}

func TestObserveReadinessReturnsCSVObservationsInDeclaredOrder(t *testing.T) {
	extension := v1alpha1.ClusterAddon{
		Spec: v1alpha1.ClusterAddonSpec{Readiness: v1alpha1.ClusterAddonReadiness{Checks: []v1alpha1.ClusterAddonReadinessCheck{
			{CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "ns-b", Subscription: "operator-b"}},
			{ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{APIVersion: "v1", Kind: "Namespace", Name: "installed"}},
			{CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "ns-a", Subscription: "operator-a"}},
		}}},
	}
	runner := csvEvidenceRunner{delays: map[string]time.Duration{"operator-b.v1": 20 * time.Millisecond}}

	result, err := ObserveReadiness(context.Background(), runner, "/tmp/kubeconfig", extension)
	if err != nil || !result.Ready {
		t.Fatalf("ObserveReadiness = %+v, err=%v", result, err)
	}
	if len(result.CSVObservations) != 2 {
		t.Fatalf("CSV observations = %+v, want two", result.CSVObservations)
	}
	if got := []string{result.CSVObservations[0].Subscription, result.CSVObservations[1].Subscription}; !reflect.DeepEqual(got, []string{"operator-b", "operator-a"}) {
		t.Fatalf("CSV observation order = %v, want declaration order", got)
	}
	for _, observation := range result.CSVObservations {
		if observation.InstalledCSV == "" || observation.Version != "1.2.3" || observation.ObservedAt.IsZero() {
			t.Fatalf("incomplete CSV observation: %+v", observation)
		}
	}
}

func TestObserveReadinessRequiresCSVSpecVersion(t *testing.T) {
	ready, detail, observation, err := observeCSV(context.Background(), csvEvidenceRunner{omitCSVVersion: true}, "/tmp/kubeconfig", "operators", "operator")
	if err != nil {
		t.Fatalf("observeCSV: %v", err)
	}
	if ready || observation != nil || !strings.Contains(detail, "spec.version unavailable") {
		t.Fatalf("observeCSV = ready=%v detail=%q observation=%+v", ready, detail, observation)
	}
}

func TestWaitIntervalScalesWithTimeoutWithinBounds(t *testing.T) {
	cases := []struct {
		timeout time.Duration
		want    time.Duration
	}{
		{timeout: 500 * time.Millisecond, want: 500 * time.Millisecond},
		{timeout: 5 * time.Second, want: time.Second},
		{timeout: 30 * time.Second, want: 5 * time.Second},
		{timeout: 5 * time.Minute, want: 5 * time.Second},
		{timeout: 20 * time.Minute, want: 12 * time.Second},
		{timeout: 45 * time.Minute, want: 15 * time.Second},
		{timeout: 4 * time.Hour, want: 15 * time.Second},
	}
	for _, tc := range cases {
		if got := WaitInterval(tc.timeout); got != tc.want {
			t.Errorf("WaitInterval(%s) = %s, want %s", tc.timeout, got, tc.want)
		}
	}
	if got := WaitInterval(45 * time.Minute); got > 15*time.Second {
		t.Fatalf("WaitInterval must stay under the 30s progress heartbeat, got %s", got)
	}
}

func TestApplySkipsReadyExtensionRecord(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	writeReadyExtensionRecord(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &recordingRunner{}

	result, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir,
		Kubeconfig:  kubeconfig,
		RunID:       "run",
		StartedAt:   time.Now(),
	}, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.Skipped || result.Reason == "" {
		t.Fatalf("Apply result = %+v, want skipped with reason", result)
	}
	if runner.applyCalls != 0 {
		t.Fatalf("apply calls = %d, want 0", runner.applyCalls)
	}
	if runner.getCalls == 0 {
		t.Fatal("readiness was not checked")
	}
}

func TestApplyReAppliesChecklessAddonDespiteReadyRecord(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	plan.Extension.Spec.Readiness.Checks = nil
	writeReadyExtensionRecord(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &sequencingRunner{}

	result, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir,
		Kubeconfig:  kubeconfig,
		RunID:       "run",
		StartedAt:   time.Now(),
	}, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Skipped {
		t.Fatalf("Apply skipped a check-less add-on on a vacuous readiness pre-check: %+v", result)
	}
	applied := 0
	for _, e := range runner.events {
		if strings.HasPrefix(e, "apply:") {
			applied++
		}
	}
	if applied == 0 {
		t.Fatalf("check-less add-on was not re-applied; events=%v", runner.events)
	}
}

func TestWaitSkipsReadyExtensionRecord(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	writeReadyExtensionRecord(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &recordingRunner{}

	result, err := Wait(context.Background(), runner, RunConfig{
		ClustersDir: dir,
		Kubeconfig:  kubeconfig,
		RunID:       "run",
		StartedAt:   time.Now(),
	}, plan)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Skipped || result.Reason == "" {
		t.Fatalf("Wait result = %+v, want skipped with reason", result)
	}
	if runner.applyCalls != 0 {
		t.Fatalf("apply calls = %d, want 0", runner.applyCalls)
	}
	if runner.getCalls == 0 {
		t.Fatal("readiness was not checked")
	}
}

func TestReadySkipRefreshesLegacyCSVRecord(t *testing.T) {
	operations := map[string]func(context.Context, OCRunner, RunConfig, extensionplan.ExtensionPlan) (TaskResult, error){
		"apply": Apply,
		"wait":  Wait,
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			plan := gatedOLMPlan("30m")
			writeReadyExtensionRecord(t, dir, plan)
			kubeconfig := filepath.Join(dir, "kubeconfig")
			if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
				t.Fatalf("write kubeconfig: %v", err)
			}
			runner := &phasedCSVRunner{succeedAfter: 1}

			result, err := operation(context.Background(), runner, RunConfig{
				ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run", StartedAt: time.Now(),
			}, plan)
			if err != nil || !result.Skipped {
				t.Fatalf("%s result=%+v err=%v", name, result, err)
			}
			record, found, err := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
			if err != nil || !found {
				t.Fatalf("LoadRecord: found=%v err=%v", found, err)
			}
			if len(record.CSVObservations) != 1 || record.CSVObservations[0].Version != "1.0.0" || record.CSVObservations[0].ObservedAt.IsZero() {
				t.Fatalf("refreshed CSV observations = %+v", record.CSVObservations)
			}
		})
	}
}

func TestReadySkipRefusesWhenCSVRefreshCannotBeSaved(t *testing.T) {
	operations := map[string]func(context.Context, OCRunner, RunConfig, extensionplan.ExtensionPlan) (TaskResult, error){
		"apply": Apply,
		"wait":  Wait,
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			plan := gatedOLMPlan("30m")
			writeReadyExtensionRecord(t, dir, plan)
			kubeconfig := filepath.Join(dir, "kubeconfig")
			if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
				t.Fatalf("write kubeconfig: %v", err)
			}
			runner := &phasedCSVRunner{succeedAfter: 1}
			result, err := operation(context.Background(), runner, RunConfig{
				ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run", StartedAt: time.Now(),
				recordWriter: func(string, extensionrecords.Record) error {
					return errors.New("forced CSV refresh write failure")
				},
			}, plan)
			if err == nil || !strings.Contains(err.Error(), "forced CSV refresh write failure") {
				t.Fatalf("%s error = %v, want refresh save failure", name, err)
			}
			if result.Skipped {
				t.Fatalf("%s reported a ready skip despite refresh save failure: %+v", name, result)
			}
		})
	}
}

type deadlineIgnoringReadyRunner struct {
	calls int
}

func (r *deadlineIgnoringReadyRunner) Run(ctx context.Context, _ string, _ []string, _ []byte) ([]byte, error) {
	r.calls++
	if r.calls == 1 {
		<-ctx.Done()
		return []byte(`{"metadata":{"name":"installed"}}`), nil
	}
	return nil, errors.New("not found")
}

func TestWaitDoesNotAcceptReadyProbeAfterOverallDeadline(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	plan.Extension.Spec.Readiness.Timeout = "20ms"
	writeReadyExtensionRecord(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	result, err := Wait(context.Background(), &deadlineIgnoringReadyRunner{}, RunConfig{
		ClustersDir: dir,
		Kubeconfig:  kubeconfig,
		RunID:       "run",
		StartedAt:   time.Now(),
	}, plan)
	if err == nil || !strings.Contains(err.Error(), "exhausted its 20ms overall readiness budget") {
		t.Fatalf("Wait result=%+v error=%v, want the shared deadline refusal", result, err)
	}
	if result.Skipped {
		t.Fatalf("Wait accepted a ready result observed after its deadline: %+v", result)
	}
}

func TestApplyOLMWaitsForCSVBeforeCustomResources(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "gated"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLM,
			OLM: &v1alpha1.ClusterAddonOLMSpec{
				Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "csv-ns", Create: true},
				Subscription: v1alpha1.ClusterAddonOLMSubscription{
					Name: "csv-op", Package: "csv-op", Channel: "stable",
					Source: "redhat-operators", SourceNamespace: "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
				CustomResources: []map[string]any{{
					"apiVersion": "nmstate.io/v1",
					"kind":       "NMState",
					"metadata":   map[string]any{"name": "nmstate"},
				}},
			},
			Readiness: v1alpha1.ClusterAddonReadiness{
				Timeout: "30m",
				Checks: []v1alpha1.ClusterAddonReadinessCheck{{
					CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "csv-ns", Subscription: "csv-op"},
				}},
			},
		},
	}
	plan := extensionplan.ExtensionPlan{
		Name: "gated", Binding: "binding", Cluster: "demo", Extension: extension,
		Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"},
	}
	runner := &sequencingRunner{}
	if _, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
	}, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var applied []string
	for _, e := range runner.events {
		if strings.HasPrefix(e, "apply:") {
			applied = append(applied, strings.TrimPrefix(e, "apply:"))
		}
	}
	wantApplied := []string{"Namespace", "Subscription", "NMState"}
	if !reflect.DeepEqual(applied, wantApplied) {
		t.Fatalf("applied kinds = %v, want %v", applied, wantApplied)
	}
	subIdx, crIdx, gateIdx := -1, -1, -1
	for i, e := range runner.events {
		switch {
		case e == "apply:Subscription":
			subIdx = i
		case e == "apply:NMState":
			crIdx = i
		case e == "get:csv" && subIdx >= 0 && crIdx < 0:
			gateIdx = i
		}
	}
	if !(subIdx >= 0 && gateIdx > subIdx && crIdx > gateIdx) {
		t.Fatalf("expected get:csv between Subscription and NMState apply; events=%v", runner.events)
	}
}

type sequencingRunner struct {
	events []string
}

func (r *sequencingRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	switch args[0] {
	case "apply":
		r.events = append(r.events, "apply:"+kindFromManifest(input))
		return nil, nil
	case "get":
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "subscription"):
			r.events = append(r.events, "get:subscription")
			return []byte(`{"status":{"installedCSV":"csv-op.v1"}}`), nil
		case strings.Contains(joined, "clusterserviceversion"):
			r.events = append(r.events, "get:csv")
			return []byte(`{"spec":{"version":"1.0.0"},"status":{"phase":"Succeeded"}}`), nil
		default:
			r.events = append(r.events, "get:other")
			return []byte(`{"metadata":{"name":"x"}}`), nil
		}
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func kindFromManifest(input []byte) string {
	for _, line := range strings.Split(string(input), "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "kind:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "kind:"))
		}
	}
	return ""
}

func TestCommandRunnerQuietWriterWithOutputDoesNotPanic(t *testing.T) {
	script := "#!/bin/sh\nprintf 'NotFound text\\n'\nprintf 'no matches for kind\\n' 1>&2\nexit 0\n"
	logPath := filepath.Join(t.TempDir(), "oc.log")
	runner := CommandRunner{Command: writeScript(t, script), LogPath: logPath}
	out, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "x"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(out) != "NotFound text\n" {
		t.Fatalf("out = %q, want stdout only", string(out))
	}
	if info, statErr := os.Stat(logPath); statErr != nil || info.Size() == 0 {
		t.Fatalf("log should capture both streams: size err=%v", statErr)
	}
}

func gatedOLMPlan(timeout string) extensionplan.ExtensionPlan {
	return extensionplan.ExtensionPlan{
		Name: "gated", Binding: "binding", Cluster: "demo",
		Extension: v1alpha1.ClusterAddon{
			Metadata: v1alpha1.Metadata{Name: "gated"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type: v1alpha1.ClusterAddonTypeOLM,
				OLM: &v1alpha1.ClusterAddonOLMSpec{
					Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "csv-ns", Create: true},
					Subscription: v1alpha1.ClusterAddonOLMSubscription{
						Name: "csv-op", Package: "csv-op", Channel: "stable",
						Source: "redhat-operators", SourceNamespace: "openshift-marketplace",
						InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
					},
					CustomResources: []map[string]any{{
						"apiVersion": "nmstate.io/v1",
						"kind":       "NMState",
						"metadata":   map[string]any{"name": "nmstate"},
					}},
				},
				Readiness: v1alpha1.ClusterAddonReadiness{
					Timeout: timeout,
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{
						CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "csv-ns", Subscription: "csv-op"},
					}},
				},
			},
		},
		Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"},
	}
}

type phasedCSVRunner struct {
	events       []string
	csvReads     int
	succeedAfter int
}

func (r *phasedCSVRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	switch args[0] {
	case "apply":
		r.events = append(r.events, "apply:"+kindFromManifest(input))
		return nil, nil
	case "get":
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "subscription"):
			r.events = append(r.events, "get:subscription")
			return []byte(`{"status":{"installedCSV":"csv-op.v1"}}`), nil
		case strings.Contains(joined, "clusterserviceversion"):
			r.csvReads++
			r.events = append(r.events, "get:csv")
			phase := "Installing"
			if r.succeedAfter > 0 && r.csvReads >= r.succeedAfter {
				phase = "Succeeded"
			}
			return []byte(`{"spec":{"version":"1.0.0"},"status":{"phase":"` + phase + `"}}`), nil
		default:
			r.events = append(r.events, "get:other")
			return []byte(`{"metadata":{"name":"x"}}`), nil
		}
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func TestApplyOLMGatePollsUntilCSVSucceeds(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &phasedCSVRunner{succeedAfter: 3}
	if _, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
	}, gatedOLMPlan("30m")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	csvBeforeCR, sawCR := 0, false
	for _, e := range runner.events {
		switch e {
		case "get:csv":
			if !sawCR {
				csvBeforeCR++
			}
		case "apply:NMState":
			sawCR = true
		}
	}
	if !sawCR {
		t.Fatalf("custom resource never applied; events=%v", runner.events)
	}
	if csvBeforeCR < 2 {
		t.Fatalf("gate did not poll while CSV pending (csv reads before CR=%d); events=%v", csvBeforeCR, runner.events)
	}
}

func TestCSVObservationIsPersistedOnlyWithFinalReady(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	plan := gatedOLMPlan("30m")
	runner := &phasedCSVRunner{succeedAfter: 1}
	cfg := RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
	}
	if _, err := Apply(context.Background(), runner, cfg, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	record, found, err := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if err != nil || !found {
		t.Fatalf("LoadRecord after Apply: found=%v err=%v", found, err)
	}
	if record.Status != extensionrecords.RecordStatusWaiting || len(record.CSVObservations) != 0 {
		t.Fatalf("post-Apply record = %+v, want waiting without CSV evidence", record)
	}
	if _, err := Wait(context.Background(), runner, cfg, plan); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	record, found, err = extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if err != nil || !found {
		t.Fatalf("LoadRecord after Wait: found=%v err=%v", found, err)
	}
	if record.Status != extensionrecords.RecordStatusReady || len(record.CSVObservations) != 1 || record.CSVObservations[0].InstalledCSV != "csv-op.v1" || record.CSVObservations[0].Version != "1.0.0" {
		t.Fatalf("final ready record CSV evidence = %+v", record)
	}
}

func TestApplyOLMGateTimeoutRecordsGateFailureNotApplyFailure(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &phasedCSVRunner{succeedAfter: 0}
	plan := gatedOLMPlan("50ms")
	_, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
	}, plan)
	if err == nil || !strings.Contains(err.Error(), "did not reach Succeeded") {
		t.Fatalf("err = %v, want a CSV-gate timeout", err)
	}
	record, found, lerr := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if lerr != nil || !found {
		t.Fatalf("LoadRecord found=%v err=%v", found, lerr)
	}
	if !strings.Contains(record.LastObserved, "operator CSV for Subscription/csv-ns/csv-op") {
		t.Fatalf("LastObserved = %q, want CSV-gate summary", record.LastObserved)
	}
	if strings.Contains(record.LastObserved, "oc apply failed") {
		t.Fatalf("LastObserved must not blame oc apply for a CSV-gate timeout: %q", record.LastObserved)
	}
	foundSub := false
	for _, o := range record.ObservedResources {
		if o == "Subscription/csv-ns/csv-op" {
			foundSub = true
		}
	}
	if !foundSub {
		t.Fatalf("the applied Subscription should be in ObservedResources: %v", record.ObservedResources)
	}
}

func TestApplyAndWaitShareOneReadinessDeadline(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	plan := gatedOLMPlan("300ms")
	plan.Extension.Spec.Readiness.Checks = append(plan.Extension.Spec.Readiness.Checks,
		v1alpha1.ClusterAddonReadinessCheck{Condition: &v1alpha1.ClusterAddonConditionReadiness{
			APIVersion: "example.io/v1",
			Kind:       "Example",
			Name:       "pending",
			Condition:  v1alpha1.ClusterAddonConditionRequirement{Type: "Ready", Status: "True"},
		}})
	runner := &phasedCSVRunner{succeedAfter: 1}
	cfg := RunConfig{
		ClustersDir:  dir,
		Kubeconfig:   kubeconfig,
		RunID:        "run",
		StartedAt:    time.Now().Add(-240 * time.Millisecond),
		PollInterval: 5 * time.Millisecond,
	}
	started := time.Now()
	if _, err := Apply(context.Background(), runner, cfg, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	_, err := Wait(context.Background(), runner, cfg, plan)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "exhausted its 300ms overall readiness budget") {
		t.Fatalf("Wait error = %v, want the declared overall readiness timeout", err)
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("Apply+Wait used a fresh timeout instead of the remaining task budget: elapsed=%s", elapsed)
	}
}

func TestInvalidReadinessTimeoutFailsBeforeRecordsOrClusterMutation(t *testing.T) {
	operations := map[string]func(context.Context, OCRunner, RunConfig, extensionplan.ExtensionPlan) (TaskResult, error){
		"apply": Apply,
		"wait":  Wait,
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			kubeconfig := filepath.Join(dir, "kubeconfig")
			if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
				t.Fatalf("write kubeconfig: %v", err)
			}
			plan := gatedOLMPlan("not-a-duration")
			runner := &phasedCSVRunner{succeedAfter: 1}
			_, err := operation(context.Background(), runner, RunConfig{
				ClustersDir: dir,
				Kubeconfig:  kubeconfig,
				RunID:       "run",
				StartedAt:   time.Now(),
			}, plan)
			if err == nil || !strings.Contains(err.Error(), "invalid duration") {
				t.Fatalf("%s error = %v, want invalid duration", name, err)
			}
			if len(runner.events) != 0 {
				t.Fatalf("%s reached the cluster before validating its timeout: %v", name, runner.events)
			}
			if _, found, loadErr := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name); loadErr != nil || found {
				t.Fatalf("%s wrote a record before validating its timeout: found=%v err=%v", name, found, loadErr)
			}
		})
	}
}

type recordingRunner struct {
	applyCalls int
	getCalls   int
}

func (r *recordingRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	switch args[0] {
	case "apply":
		r.applyCalls++
		return nil, fmt.Errorf("apply should have been skipped")
	case "get":
		r.getCalls++
		return []byte(`{"metadata":{"name":"installed"}}`), nil
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func readyExtensionPlan() extensionplan.ExtensionPlan {
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "ready-extension"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLM,
			OLM: &v1alpha1.ClusterAddonOLMSpec{
				Namespace: v1alpha1.ClusterAddonOLMNamespace{
					Name:   "installed",
					Create: true,
				},
				Subscription: v1alpha1.ClusterAddonOLMSubscription{
					Name:                "ready-operator",
					Package:             "ready-operator",
					Channel:             "stable",
					Source:              "redhat-operators",
					SourceNamespace:     "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
			},
			Readiness: v1alpha1.ClusterAddonReadiness{
				Timeout: "30m",
				Checks: []v1alpha1.ClusterAddonReadinessCheck{{
					ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{
						APIVersion: "v1",
						Kind:       "Namespace",
						Name:       "installed",
					},
				}},
			},
		},
	}
	return extensionplan.ExtensionPlan{
		Name:      "ready-extension",
		Binding:   "binding",
		Cluster:   "demo",
		Extension: extension,
		Policy:    addons.ClusterAddonPolicy{FieldManager: "bootwright"},
	}
}

func writeReadyExtensionRecord(t *testing.T, clustersDir string, plan extensionplan.ExtensionPlan) {
	t.Helper()
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy, plan.Inputs)
	if err != nil {
		t.Fatalf("DesiredHash: %v", err)
	}
	if err := extensionrecords.SaveRecord(clustersDir, extensionrecords.Record{
		Cluster:     plan.Cluster,
		Extension:   plan.Name,
		DesiredHash: hash,
		Status:      extensionrecords.RecordStatusReady,
		Phase:       extensionrecords.RecordPhaseComplete,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
}

type leakyApplyRunner struct {
	secret string
}

func (r *leakyApplyRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	switch args[0] {
	case "apply":
		out := "admission webhook denied the request: " + r.secret
		return []byte(out), fmt.Errorf("run oc %s: exit status 1: %s", strings.Join(args, " "), out)
	case "get":
		return nil, fmt.Errorf("not found")
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func TestApplyDoesNotPersistRawOutputInFailedRecord(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	const secret = "s3cr3t-token-DO-NOT-LEAK"
	runner := &leakyApplyRunner{secret: secret}

	_, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir,
		Kubeconfig:  kubeconfig,
		RunID:       "run",
		StartedAt:   time.Now(),
	}, plan)
	if err == nil {
		t.Fatal("Apply succeeded despite an apply failure")
	}
	if !strings.Contains(err.Error(), secret) {
		t.Fatalf("returned error dropped the raw output that belongs in the apply log: %v", err)
	}

	record, found, err := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if err != nil || !found {
		t.Fatalf("LoadRecord: found=%v err=%v", found, err)
	}
	if record.Status != extensionrecords.RecordStatusFailed {
		t.Fatalf("record status = %q, want failed", record.Status)
	}
	if strings.Contains(record.LastObserved, secret) {
		t.Fatalf("observed-state record leaked secret bytes: %q", record.LastObserved)
	}
	if !strings.Contains(record.LastObserved, "apply log") {
		t.Fatalf("record.LastObserved should point at the apply log: %q", record.LastObserved)
	}
}
