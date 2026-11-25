package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func suspensionEvidence(t *testing.T, digest, state string, changed bool, baseline []SuspensionDaemon) json.RawMessage {
	t.Helper()
	if baseline == nil {
		baseline = []SuspensionDaemon{}
	}
	data, err := json.Marshal(SignOnSuspensionEvidence{Baseline: baseline, Changed: changed, Request: digest, State: state, Version: SignOnSuspensionVersion})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func lentVariables(request lifecycle.RunRequest) []string {
	variables := []string{}
	for _, file := range request.Materials {
		variables = append(variables, file.Variable)
	}
	return variables
}

var suspensionBaseline = []SuspensionDaemon{{Configured: "2026-11-25T10:00:00.000000Z", Name: "grafana.node-1"}, {Configured: "2026-11-25T10:00:00.000000Z", Name: "mgmt-gateway.node-1"}}

func TestSignOnSuspensionPreparesReadOnlyWithHostAccessAlone(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: suspensionEvidence(t, e.Block.RequestDigest, signOnPrepared, false, suspensionBaseline)}}
	preparation, err := NewSignOn(runner).PrepareSignOnSuspension(context.Background(), e)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if string(preparation) != `{"baseline":[{"configured":"2026-11-25T10:00:00.000000Z","name":"grafana.node-1"},{"configured":"2026-11-25T10:00:00.000000Z","name":"mgmt-gateway.node-1"}]}` {
		t.Fatalf("the preparation is %s", preparation)
	}
	if runner.request.Operation != "suspend_prepare" || runner.request.Deadline != 1380*time.Second || !slices.Equal(lentVariables(runner.request), []string{"sshKey", "knownHosts"}) {
		t.Fatalf("the preparation ran %s for %v lending %q", runner.request.Operation, runner.request.Deadline, lentVariables(runner.request))
	}
	for name, evidence := range map[string]json.RawMessage{
		"another request": suspensionEvidence(t, strings.Repeat("0", 64), signOnPrepared, false, suspensionBaseline),
		"another state":   suspensionEvidence(t, e.Block.RequestDigest, signOnSuspended, false, nil),
		"unsorted":        suspensionEvidence(t, e.Block.RequestDigest, signOnPrepared, false, []SuspensionDaemon{suspensionBaseline[1], suspensionBaseline[0]}),
		"a bad time":      suspensionEvidence(t, e.Block.RequestDigest, signOnPrepared, false, []SuspensionDaemon{{Configured: "yesterday", Name: "grafana.node-1"}}),
		"a member more":   json.RawMessage(strings.Replace(string(suspensionEvidence(t, e.Block.RequestDigest, signOnPrepared, false, nil)), `"version"`, `"extra":1,"version"`, 1)),
	} {
		runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}}
		if _, err := NewSignOn(runner).PrepareSignOnSuspension(context.Background(), e); err == nil {
			t.Fatalf("a preparation with %s was accepted", name)
		}
	}
}

func TestSignOnSuspendLendsNoSignOnLeafAndNamesTheLocalLogins(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	preparation := json.RawMessage(`{"baseline":[{"configured":"2026-11-25T10:00:00.000000Z","name":"grafana.node-1"}]}`)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: suspensionEvidence(t, e.Block.RequestDigest, signOnSuspended, true, nil)}}
	proof, err := NewSignOn(runner).SuspendSignOn(context.Background(), e, preparation)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !proof.Changed || proof.Provider != "corp-sso" || !slices.Equal(proof.LocalLogins, []string{"dashboard", "prometheus"}) {
		t.Fatalf("the suspension proved %+v", proof)
	}
	if runner.request.Operation != "suspend" || runner.request.Deadline != 3600*time.Second || !slices.Equal(lentVariables(runner.request), []string{"sshKey", "knownHosts"}) ||
		runner.request.MaterialValues["signOnBaseline"] != string(preparation) || len(runner.request.MaterialValues) != 1 {
		t.Fatalf("the suspension ran %s for %v lending %q and %v", runner.request.Operation, runner.request.Deadline, lentVariables(runner.request), runner.request.MaterialValues)
	}
	for name, bad := range map[string]json.RawMessage{
		"no preparation":     nil,
		"another member":     json.RawMessage(`{"baseline":[],"x":1}`),
		"an invalid daemon":  json.RawMessage(`{"baseline":[{"configured":"","name":"Grafana"}]}`),
		"a list preparation": json.RawMessage(`[]`),
	} {
		runner := &recordingRunner{result: runner.result}
		if _, err := NewSignOn(runner).SuspendSignOn(context.Background(), e, bad); err == nil || runner.calls != 0 {
			t.Fatalf("a suspension with %s ran %d times (%v)", name, runner.calls, err)
		}
	}
	unproved := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: suspensionEvidence(t, e.Block.RequestDigest, signOnPrepared, false, nil)}}
	if _, err := NewSignOn(unproved).SuspendSignOn(context.Background(), e, preparation); err == nil {
		t.Fatal("a suspension that proved no suspended state was accepted")
	}
}

func TestSignOnResumeReappliesWithEveryLeafAndProvesSignOn(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	_, unit, err := NewSignOn(nil).prepare(context.Background(), e)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: signOnReady(t, e, unit, nil)}}
	proof, err := NewSignOn(runner).ResumeSignOn(context.Background(), e)
	if err != nil || !proof.Changed || proof.Provider != "corp-sso" {
		t.Fatalf("the resume proved %+v (%+v)", proof, diagnostics.Of(err))
	}
	if runner.request.Operation != "resume" || runner.request.Deadline != 5580*time.Second ||
		!slices.Equal(lentVariables(runner.request), []string{"sshKey", "knownHosts", "oauth2ProxyCABundle", "oauth2ProxyClientSecret", "oauth2ProxyCookieSecret", "oauth2ProxyCertificate", "oauth2ProxyKey"}) {
		t.Fatalf("the resume ran %s for %v lending %q", runner.request.Operation, runner.request.Deadline, lentVariables(runner.request))
	}
	off := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: signOnReady(t, e, unit, func(e *Evidence) { e.SSO = "" })}}
	if _, err := NewSignOn(off).ResumeSignOn(context.Background(), e); err == nil {
		t.Fatal("a resume that proved no single sign-on was accepted")
	}
}

func TestASuspendedSignOnObservesCompletedAndAnInterruptedOneRunsNothing(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	e.SignOn = lifecycle.SignOnSuspended
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: suspensionEvidence(t, e.Block.RequestDigest, signOnSuspended, false, nil)}}
	observation, err := NewSignOn(runner).Observe(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted {
		t.Fatalf("a suspended sign-on observes %+v (%+v)", observation, diagnostics.Of(err))
	}
	if runner.request.Operation != "observe" || runner.request.MaterialValues["signOn"] != "suspended" || !slices.Equal(lentVariables(runner.request), []string{"sshKey", "knownHosts"}) {
		t.Fatalf("the suspended observation ran %s with %v lending %q", runner.request.Operation, runner.request.MaterialValues, lentVariables(runner.request))
	}
	for _, state := range []string{lifecycle.SignOnSuspending, lifecycle.SignOnResuming} {
		e.SignOn = state
		runner := &recordingRunner{result: runner.result}
		if observation, err := NewSignOn(runner).Observe(context.Background(), e); err == nil || observation.Effect != reconciliation.EffectUnknown || runner.calls != 0 {
			t.Fatalf("a %s sign-on observes %+v after %d runs (%v)", state, observation, runner.calls, err)
		}
	}
}

func TestSignOnCluster(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	if cluster, err := NewSignOn(nil).SignOnCluster(e.Block); err != nil || cluster != "example" {
		t.Fatalf("the sign-on names cluster %q (%v)", cluster, err)
	}
	if _, err := NewSignOn(nil).SignOnCluster(reconciliation.Block{}); err == nil {
		t.Fatal("an undecodable block names a cluster")
	}
}

type resumeRefuser struct{ err error }

func (r resumeRefuser) Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return lifecycle.RunResult{}, r.err
}

func TestAResumeTheProviderRefusedBeforeAnyEffectSaysSo(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	unreachable := signOnReasons()["sign-on-provider-unreachable"]
	if _, err := NewSignOn(resumeRefuser{unreachable}).ResumeSignOn(context.Background(), e); !errors.Is(err, lifecycle.ErrSignOnUnchanged) {
		t.Fatalf("an unreachable provider resumes as %v", err)
	}
	if _, err := NewSignOn(resumeRefuser{signOnReasons()["sign-on-unenforced"]}).ResumeSignOn(context.Background(), e); err == nil || errors.Is(err, lifecycle.ErrSignOnUnchanged) {
		t.Fatalf("a refusal after effects resumes as %v", err)
	}
}
