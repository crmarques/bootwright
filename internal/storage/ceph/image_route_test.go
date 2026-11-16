package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestImageRouteComesOnlyFromItsFrozenApplyDependencies(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://ambient.invalid:9999")
	t.Setenv("NO_PROXY", "*")
	block := imageBlock(t, "example", PinnedRepository+":"+PinnedTag)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: resolvedEvidence(t, block.RequestDigest, resolvedDigest)}}
	execution := imageExecution(block)
	execution.ApplyProved = []lifecycle.BlockEvidence{{Operation: "original-frozen-controller"}}
	selected := prerequisites.SetupEgress{HTTPSProxy: "http://original.example.test:3128", NoProxy: []string{".internal.test"}}
	capability := NewImage(runner).WithRoute(func(machine string, records []lifecycle.BlockEvidence) (prerequisites.SetupEgress, error) {
		if machine != "controller" || !reflect.DeepEqual(records, execution.ApplyProved) {
			t.Fatal("route proof did not receive the original dependencies")
		}
		return selected, nil
	})
	if _, err := capability.Apply(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	var got prerequisites.SetupEgress
	if err := json.Unmarshal([]byte(runner.request.MaterialValues["egress"]), &got); err != nil || !reflect.DeepEqual(got, selected) {
		t.Fatalf("lent route = %+v (%v)", got, err)
	}
	if string(runner.request.Canonical) != string(block.Request) {
		t.Fatal("route lending rewrote the frozen image request")
	}
	refused := NewImage(runner).WithRoute(func(string, []lifecycle.BlockEvidence) (prerequisites.SetupEgress, error) {
		return prerequisites.SetupEgress{}, errors.New("unproved original route")
	})
	if _, err := refused.Apply(context.Background(), execution); err == nil || runner.calls != 1 {
		t.Fatal("an unproved route reached the adapter")
	}
	if _, err := NewImage(runner).Apply(context.Background(), execution); err == nil || runner.calls != 1 {
		t.Fatal("missing route proof silently selected direct access")
	}
	runner.result.Evidence = json.RawMessage(`{"absent":true,"postcondition":true,"request":"` + block.RequestDigest + `"}`)
	if _, err := refused.Observe(context.Background(), execution); err != nil || runner.request.MaterialValues != nil {
		t.Fatal("observation consumed an acquisition route:", err)
	}
	if _, err := refused.Destroy(context.Background(), execution); err != nil || runner.request.MaterialValues != nil {
		t.Fatal("removal consumed an acquisition route:", err)
	}
}
