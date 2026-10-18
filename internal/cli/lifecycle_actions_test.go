package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestLifecycleActionUsesFrozenMetadata(t *testing.T) {
	for _, tc := range []struct{ name, kind, implementation, verb, description, action, object string }{
		{"dns", "DNSServer", "dns-server-dnsmasq-v1", "apply", "resolve names for demo-dns", "Start DNS service", "demo-dns"},
		{"future dns", "DNSServer", "dns-future-v1", "apply", "perform future DNS action on demo-dns", "", "demo-dns"},
		{"vm", "Machine", "machine-libvirt-v1", "apply", "realize the virtual machine demo-node and its controller", "Create virtual machine and controller", "demo-node"},
		{"os", "Machine", "os-install-anaconda-v1", "apply", "install the operating system of demo-node", "Install operating system", "demo-node"},
		{"media", "ContainerCluster", "cluster-media-agent-v1", "apply", "build the boot media of demo-cluster", "Build boot media", "demo-cluster"},
		{"cluster", "ContainerCluster", "cluster-install-agent-v1", "apply", "install the cluster demo-cluster", "Install container cluster", "demo-cluster"},
		{"cluster destroy", "ContainerCluster", "cluster-install-agent-v1", "destroy", "release the boot media of demo-cluster", "Release boot media", "demo-cluster"},
		{"baremetal destroy", "Machine", "machine-baremetal-v1", "destroy", "release the claim on demo-node and retain the machine", "", "demo-node"},
		{"ceph host", "StorageCluster", "storage-ceph-host-v1", "apply", "prepare host demo-node for demo-storage", "", "demo-storage"},
		{"future machine", "Machine", "machine-future-v1", "apply", "perform future action on demo-node", "", "demo-node"},
		{"unknown kind", "Future", "future-v1", "apply", "perform future action on demo-object", "", "demo-object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := lifecycle.PlanStep{ID: "opaque-id", Kind: tc.kind, Implementation: tc.implementation, Object: tc.object, Description: tc.description}
			action, target := lifecycleAction(step, tc.verb)
			if tc.action == "" {
				if action != tc.description || target != "" {
					t.Fatalf("fallback = %q | %q", action, target)
				}
			} else if action != tc.action || target != step.Object {
				t.Fatalf("action = %q | %q", action, target)
			}
		})
	}
}

func TestLifecycleActionPlanAndEffectsShareIdentityOnce(t *testing.T) {
	var output bytes.Buffer
	opts := PresentationOptions{Format: "text", Color: "always", Width: 200}
	out := WithPresentation(&output, opts)
	plan, progress := NewLifecyclePresenters(out, nil, false)
	step := lifecycle.PlanStep{ID: "opaque", Kind: "DNSServer", Implementation: "dns-server-dnsmasq-v1", Object: "demo-dns", Description: "resolve names for demo-dns", State: "pending"}
	if err := plan.PresentLifecyclePlan(context.Background(), lifecycle.PlanResult{Verb: "apply", Steps: []lifecycle.PlanStep{step}}); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		plain := ansi.Strip(output.String())
		if strings.Count(plain, "demo-dns") != 1 || !strings.Contains(plain, "Start DNS service | demo-dns") {
			t.Fatalf("output = %q", plain)
		}
		if !strings.Contains(output.String(), styleIdentity("demo-dns", opts)) {
			t.Fatal("missing styled identity")
		}
	}
	check()
	output.Reset()
	progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: step.ID, Description: step.Description, Status: "done"})
	progress.Finish()
	check()
	output.Reset()
	progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Phase: lifecycle.CheckPhase, Block: step.ID, Description: "check DNS listener", Status: "done"})
	progress.Finish()
	plain := ansi.Strip(output.String())
	if !strings.Contains(plain, "check DNS listener") || strings.Contains(plain, "demo-dns") || strings.Contains(plain, "Start DNS") {
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
	got := lifecycleActionLabel(description, "", object, opts)
	if ansi.Strip(got) != escapeDisplayLine(description) || strings.Count(got, styleIdentity(escapeDisplayLine(object), opts)) != 1 {
		t.Fatalf("escaped fallback = %q", got)
	}
}
