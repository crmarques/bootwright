package oc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// TestCommandRunnerReturnsStdoutWithoutStderr guards that a successful oc get
// yields only stdout: oc routinely writes deprecation/TLS/auth warnings to
// stderr on a successful `get -o json`, and folding them into the returned bytes
// corrupts the JSON that readiness checks unmarshal.
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

// TestReadinessChecksDecodeDespiteStderrWarnings exercises the readiness
// consumers end to end against a CommandRunner whose oc emits a stderr warning
// alongside valid JSON, proving Ready/csvSucceeded report ready instead of
// failing to decode a warning-corrupted buffer.
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

// scriptedGetRunner returns a CommandRunner backed by a script that, for an oc
// `get`, writes stdout and stderr verbatim and exits 0.
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

// scriptedCSVRunner returns a CommandRunner backed by a script that mimics the
// two oc gets csvSucceeded performs (Subscription, then CSV), each pairing valid
// JSON on stdout with a stderr warning.
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
		shellQuote(`{"status":{"phase":"Succeeded"}}`),
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
					Type:       v1alpha1.ClusterAddonReadinessResourceExists,
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       "installed",
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
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy)
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
		// Mimic oc echoing the rejected object's fields, including secret bytes.
		out := "admission webhook denied the request: " + r.secret
		return []byte(out), fmt.Errorf("run oc %s: exit status 1: %s", strings.Join(args, " "), out)
	case "get":
		// Force the readiness pre-check to report not-ready so Apply proceeds.
		return nil, fmt.Errorf("not found")
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

// TestApplyDoesNotPersistRawOutputInFailedRecord guards the "never secret bytes"
// contract for observed-state records: when oc apply fails with output that echoes
// user-inlined secret bytes, the failure must be summarized in the record (naming
// the failed resource and pointing at the apply log) rather than stored verbatim.
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
	// The raw output (with the secret) must still reach the caller -> apply log.
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
