//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const managedInfraComponentsFiles = 7

func TestManagedInfraComponentsExampleIsAdmissibleAndSupported(t *testing.T) {
	sources := exampleDirectory(t, "managed-infra-components")
	if len(sources.Files) != managedInfraComponentsFiles {
		t.Fatalf("discovery: got %d files, want %d", len(sources.Files), managedInfraComponentsFiles)
	}
	out, errOut := contextRun(t, isolatedServices(t), 0, "validate", "-f", sources.Roots[0])
	if errOut != "" || !strings.Contains(out, "objects decoded: 7") {
		t.Fatalf("validate: out=%q err=%q", out, errOut)
	}
	state, _ := compileAcceptance(t, sources)
	if unsupported := lifecycle.Unrealizable(state.Effective(), buildCapabilities(systemClock{}).Kinds()); len(unsupported) != 0 {
		t.Fatalf("the example declares objects no capability claims: %v", unsupported)
	}
	for _, object := range []struct {
		kind api.Kind
		name string
	}{
		{api.Machine, "bastion"}, {api.Proxy, "lab-proxy"},
		{api.DNSServer, "lab-dns"}, {api.NTPServer, "lab-ntp"},
		{api.ArtifactServer, "lab-artifacts"},
	} {
		requireObject(t, state.Effective(), object.kind, object.name)
	}
}

// Each capability must derive a complete frozen request from the example
// alone, because that request is what an operator's apply would freeze.
func TestManagedInfraComponentsExamplePlansOneBlockPerService(t *testing.T) {
	sources := exampleDirectory(t, "managed-infra-components")
	state, _ := compileAcceptance(t, sources)
	resolver := buildCapabilities(systemClock{})
	input := lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "bastion",
		Context: lifecycle.ContextIdentity{Name: "managed-infra"},
	}
	var definitions []reconciliation.BlockDefinition
	reservations := map[string][]string{}
	for _, kind := range resolver.Kinds() {
		capability, ok := resolver.Resolve(kind, "")
		if !ok {
			t.Fatalf("%s does not resolve", kind)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", kind, err)
		}
		definitions = append(definitions, contribution.Definitions...)
		for _, reservation := range contribution.Reservations {
			if reservation.Context != "managed-infra" {
				t.Fatalf("%s reserved for %q", kind, reservation.Context)
			}
			reservations[reservation.Service] = reservation.Keys
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal("the example produced a block the plan model refuses:", err)
	}
	blocks := []string{}
	for _, block := range plan.Blocks {
		blocks = append(blocks, block.ID)
		if block.Stage != reconciliation.StageInfraComponents {
			t.Fatalf("%s belongs to stage %q", block.ID, block.Stage)
		}
		if len(block.Dependencies) != 0 {
			t.Fatalf("%s depends on %v", block.ID, block.Dependencies)
		}
	}
	if !slices.Equal(blocks, []string{"artifact-server-lab-artifacts", "dns-lab-dns", "ntp-lab-ntp", "proxy-lab-proxy"}) {
		t.Fatalf("blocks = %v", blocks)
	}
	for service, want := range map[string][]string{
		"lab-proxy":     {"socket:192.0.2.1:3128"},
		"lab-dns":       {"socket:192.0.2.1:53"},
		"lab-ntp":       {"socket:192.0.2.1:123"},
		"lab-artifacts": {"socket:192.0.2.1:8443", "socket:192.0.2.1:8080"},
	} {
		for _, key := range want {
			if !slices.Contains(reservations[service], key) {
				t.Fatalf("%s reserved %v, missing %q", service, reservations[service], key)
			}
		}
	}
}

// The derived configuration is what the adapter renders, so the frozen request
// must already carry the graph's own clients, records and upstream sources.
func TestManagedInfraComponentsRequestsCarryTheirDerivedIntent(t *testing.T) {
	sources := exampleDirectory(t, "managed-infra-components")
	state, _ := compileAcceptance(t, sources)
	resolver := buildCapabilities(systemClock{})
	requests := map[string]managedservice.Request{}
	for _, kind := range []string{"Proxy", "DNSServer", "NTPServer"} {
		capability, _ := resolver.Resolve(kind, "")
		contribution, err := capability.Plan(context.Background(), lifecycle.PlanInput{
			Verb: reconciliation.Apply, State: state, Controller: "bastion",
			Context: lifecycle.ContextIdentity{Name: "managed-infra"},
		})
		if err != nil || len(contribution.Definitions) != 1 {
			t.Fatalf("%s contributed %d blocks (%v)", kind, len(contribution.Definitions), err)
		}
		version := map[string]string{"Proxy": "proxy-squid-v1", "DNSServer": "dns-server-dnsmasq-v1", "NTPServer": "ntp-server-chrony-v1"}[kind]
		request, err := managedservice.DecodeRequest(contribution.Definitions[0].Request, version)
		if err != nil {
			t.Fatalf("%s request: %v", kind, err)
		}
		requests[kind] = request
	}
	clients := []string{"127.0.0.1/32", "192.0.2.1/32", "::1/128"}
	if !slices.Equal(requests["Proxy"].Clients, clients) || !slices.Equal(requests["NTPServer"].Clients, clients) {
		t.Fatalf("clients = %v and %v", requests["Proxy"].Clients, requests["NTPServer"].Clients)
	}
	records := requests["DNSServer"].Records
	if len(records) != 1 || records[0].Name != "bastion.lab.example.test" || !slices.Equal(records[0].Addresses, []string{"192.0.2.1"}) {
		t.Fatalf("records = %+v", records)
	}
	if !slices.Equal(requests["DNSServer"].Forwarders, []string{"192.0.2.53"}) {
		t.Fatalf("forwarders = %v", requests["DNSServer"].Forwarders)
	}
	if !slices.Equal(requests["NTPServer"].Sources, []string{"192.0.2.123"}) {
		t.Fatalf("sources = %v", requests["NTPServer"].Sources)
	}
	for kind, request := range requests {
		if request.Placement.Connection != "local" || request.Placement.Machine != "bastion" {
			t.Fatalf("%s placement = %+v", kind, request.Placement)
		}
		if !strings.Contains(request.Image, "@sha256:") {
			t.Fatalf("%s image is not digest pinned: %q", kind, request.Image)
		}
	}
}
