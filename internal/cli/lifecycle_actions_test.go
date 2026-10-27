package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Every planned action reads as its block's frozen description, which the
// capability that planned it wrote, so the CLI keeps no label of its own for
// any implementation and a new implementation needs no CLI edit.
func TestEveryActionIsItsFrozenDescriptionWithItsObjectHighlightedOnce(t *testing.T) {
	for _, tc := range []struct{ kind, implementation, verb, description, object string }{
		{"DNSServer", "dns-server-dnsmasq-v1", "apply", "resolve names for demo-dns", "demo-dns"},
		{"Machine", "machine-libvirt-v1", "destroy", "remove the virtual machine demo-node and its emulated BMC", "demo-node"},
		{"CephCluster", "storage-ceph-host-v1", "apply", "prepare host demo-node for demo-storage", "demo-storage"},
		{"Future", "future-v1", "apply", "perform future action on demo-object", "demo-object"},
	} {
		var output bytes.Buffer
		opts := PresentationOptions{Format: "text", Color: "always", Width: 200}
		plan, progress := NewLifecyclePresenters(WithPresentation(&output, opts), nil, false)
		step := lifecycle.PlanStep{ID: "opaque", Kind: tc.kind, Implementation: tc.implementation, Object: tc.object, Description: tc.description, State: "pending"}
		check := func(phase string) {
			t.Helper()
			raw := output.String()
			if !strings.Contains(ansi.Strip(raw), tc.description) || strings.Contains(ansi.Strip(raw), " | ") || strings.Count(raw, styleIdentity(tc.object, opts)) != 1 {
				t.Fatalf("%s %s %s = %q", tc.implementation, tc.verb, phase, raw)
			}
		}
		if err := plan.PresentLifecyclePlan(context.Background(), lifecycle.PlanResult{Verb: tc.verb, Steps: []lifecycle.PlanStep{step}}); err != nil {
			t.Fatal(err)
		}
		check("plan")
		output.Reset()
		progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: step.ID, Description: step.Description, Status: "done"})
		progress.Finish()
		check("progress")
	}
}

func TestACheckLabelIsNeverReplacedByItsAction(t *testing.T) {
	var output bytes.Buffer
	opts := PresentationOptions{Format: "text", Color: "always", Width: 200}
	plan, progress := NewLifecyclePresenters(WithPresentation(&output, opts), nil, false)
	step := lifecycle.PlanStep{ID: "opaque", Kind: "DNSServer", Implementation: "dns-server-dnsmasq-v1", Object: "demo-dns", Description: "resolve names for demo-dns", State: "pending"}
	if err := plan.PresentLifecyclePlan(context.Background(), lifecycle.PlanResult{Verb: "apply", Steps: []lifecycle.PlanStep{step}}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Phase: lifecycle.CheckPhase, Block: step.ID, Description: "check DNS listener", Status: "done"})
	progress.Finish()
	plain := ansi.Strip(output.String())
	if !strings.Contains(plain, "check DNS listener") || strings.Contains(plain, "demo-dns") || strings.Contains(plain, "resolve names") {
		t.Fatalf("check label replaced: %q", plain)
	}
}

func TestLifecycleFallbackHighlightsIdentityWithoutChangingDescription(t *testing.T) {
	var output bytes.Buffer
	opts := PresentationOptions{Format: "text", Color: "always", Width: 200}
	out := WithPresentation(&output, opts)
	plan, progress := NewLifecyclePresenters(out, nil, false)
	step := lifecycle.PlanStep{ID: "opaque", Kind: "InfraProvider", Implementation: "future-v1", Object: "demo-provider", Description: "prepare demo-provider on controller-machine and retain its shared clients", State: "pending"}
	assertDescription := func() {
		t.Helper()
		raw := output.String()
		if !strings.Contains(ansi.Strip(raw), step.Description) || strings.Contains(raw, " | ") || strings.Count(raw, styleIdentity(step.Object, opts)) != 1 {
			t.Fatalf("fallback description = %q", raw)
		}
	}
	if err := plan.PresentLifecyclePlan(context.Background(), lifecycle.PlanResult{Verb: "destroy", Steps: []lifecycle.PlanStep{step}}); err != nil {
		t.Fatal(err)
	}
	assertDescription()
	output.Reset()
	progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: step.ID, Description: step.Description, Status: "done"})
	progress.Finish()
	assertDescription()
}

func TestLifecycleFallbackIdentityEscapesDataOnce(t *testing.T) {
	opts := PresentationOptions{Color: "always"}
	object := "demo\nprovider"
	description := "prepare " + object + " and retain shared clients"
	got := lifecycleActionLabel(description, object, opts)
	if ansi.Strip(got) != escapeDisplayLine(description) || strings.Count(got, styleIdentity(escapeDisplayLine(object), opts)) != 1 {
		t.Fatalf("escaped fallback = %q", got)
	}
}

func TestLifecycleFallbackHighlightsOnlyTheFirstWholeWordOfTheObject(t *testing.T) {
	opts := PresentationOptions{Color: "always"}
	for _, tc := range []struct{ label, object, want string }{
		{"prepare demo-node for demo", "demo", "prepare demo-node for " + styleIdentity("demo", opts)},
		{"prepare demo and demo again", "demo", "prepare " + styleIdentity("demo", opts) + " and demo again"},
		{"prepare demo-node", "demo", "prepare demo-node"},
		{"prepare demo.example", "demo", "prepare demo.example"},
	} {
		if got := lifecycleActionLabel(tc.label, tc.object, opts); got != tc.want {
			t.Errorf("%q highlighting %q = %q, want %q", tc.label, tc.object, got, tc.want)
		}
	}
}
