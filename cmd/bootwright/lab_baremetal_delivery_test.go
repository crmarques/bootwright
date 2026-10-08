//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// installationPlan is the installation capability's own contribution to the
// example's plan under verb.
func installationPlan(t *testing.T, verb reconciliation.Verb) []reconciliation.BlockDefinition {
	t.Helper()
	capability, ok := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}).Resolve(installation.Kind, installation.Implementation)
	if !ok {
		t.Fatal("the installation capability does not resolve")
	}
	input := baremetalInput(t, verb)
	if reporter, ok := capability.(lifecycle.UnsupportedReporter); !ok || len(reporter.Unsupported(input.State)) != 0 {
		t.Fatalf("the installation refuses the example: %+v", reporter.Unsupported(input.State))
	}
	contribution, err := capability.Plan(context.Background(), input)
	if err != nil {
		t.Fatalf("installation plan: %v", diagnostics.Of(err))
	}
	return contribution.Definitions
}

// The example's one Machine is installed: its installation plans, consumes
// data-loss on apply and nothing on its removal, waits for the claim and the
// artifact server, and publishes its installer image only beneath its private
// subtree, fetched by a controller reached over https and verified against
// the authority the context carries.
func TestLabBaremetalExamplePlansItsInstallation(t *testing.T) {
	for verb, consumes := range map[reconciliation.Verb][]string{reconciliation.Apply: {"data-loss"}, reconciliation.Destroy: nil} {
		blocks := installationPlan(t, verb)
		if len(blocks) != 1 || blocks[0].ID != "os-install-metal-01" {
			t.Fatalf("%s blocks = %+v", verb, blocks)
		}
		if !slices.Equal(blocks[0].Consumes, consumes) {
			t.Fatalf("%s consumes %v, want %v", verb, blocks[0].Consumes, consumes)
		}
		if verb != reconciliation.Apply {
			continue
		}
		for _, required := range []reconciliation.ObjectRef{{Kind: "Machine", Object: "metal-01"}, {Kind: "ArtifactServer", Object: "lab-artifacts"}} {
			if !slices.Contains(blocks[0].Requires, required) {
				t.Fatalf("the installation requires %+v, not %+v", blocks[0].Requires, required)
			}
		}
		request, err := installation.DecodeRequest(blocks[0].Request)
		if err != nil {
			t.Fatalf("decoding: %v", diagnostics.Of(err))
		}
		if request.Image != nil {
			t.Fatalf("a delivered-key installation froze a public image: %+v", request.Image)
		}
		if request.Private == nil || !strings.HasSuffix(request.Private.Path, "/public/private/os/metal-01") ||
			request.Private.URL != "https://192.0.2.1:8443/private/os/metal-01" {
			t.Fatalf("private = %+v", request.Private)
		}
		controller := request.Target.Controller
		if !controller.TLSVerify || controller.TrustBundleRef != "lab-bmc-ca" || request.Target.HostKeyType != "ed25519" {
			t.Fatalf("target = %+v", request.Target)
		}
	}
}

// B39's rule: no description, impact or public publication names the private
// subtree's URL, and none names a token. The plan names only the parent the
// attempt mints its token beneath, which the request freezes as Private while
// its Kickstart names the private URL only as the placeholder the attempt
// substitutes.
func TestLabBaremetalExampleKeepsPrivateURLsOutOfPublicArtifacts(t *testing.T) {
	token := regexp.MustCompile(`[0-9a-f]{64}`)
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		for _, block := range installationPlan(t, verb) {
			if strings.Contains(block.Description, "/private/") {
				t.Fatalf("the description names the private subtree: %q", block.Description)
			}
			for _, impact := range block.Impacts {
				if strings.Contains(impact, "://") || token.MatchString(impact) {
					t.Fatalf("an impact names a URL or a token: %q", impact)
				}
			}
			request, err := installation.DecodeRequest(block.Request)
			if err != nil {
				t.Fatalf("decoding: %v", diagnostics.Of(err))
			}
			if request.Image != nil {
				t.Fatalf("the request carries a public image: %+v", request.Image)
			}
			if request.Tree == nil || strings.Contains(request.Tree.Path, "/private/") || strings.Contains(request.Tree.URL, "/private/") {
				t.Fatalf("the public tree names the private subtree: %+v", request.Tree)
			}
			if !strings.Contains(request.Kickstart, installation.PrivateURLToken) || strings.Contains(request.Kickstart, request.Private.URL) {
				t.Fatal("the kickstart does not name the private URL only as its placeholder")
			}
			var fields map[string]any
			if err := json.Unmarshal(block.Request, &fields); err != nil {
				t.Fatal(err)
			}
			for name, value := range fields {
				encoded, _ := json.Marshal(value)
				if name != "bootMedia" && name != "treeMedia" && token.Match(encoded) {
					t.Fatalf("request field %s carries a token-shaped run: %s", name, encoded)
				}
			}
		}
	}
}

// rewritten is the example with one replacement in the file that holds old.
func rewritten(t *testing.T, old, replacement string) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, "lab-baremetal")
	replaced := 0
	for index, file := range sources.Files {
		body := string(file.Bytes())
		if !strings.Contains(body, old) {
			continue
		}
		sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(body, old, replacement, 1)))
		replaced++
	}
	if replaced != 1 {
		t.Fatalf("%d files hold %q", replaced, old)
	}
	return sources
}

// A delivered-key installation hands its controller the private installer
// image URL, so the example with its verification removed, or its controller
// addressed over plain http, refuses before registration naming the Machine
// with the one reason and remedy the managed-OS refusal table gives.
func TestLabBaremetalExampleRefusesAnUnverifiedController(t *testing.T) {
	const reason = "a Machine whose installation delivers private material hands its controller the private installer image URL, " +
		"so the controller must be reached over https with its certificate verified"
	const remedy = "address the controller of Machine/metal-01 with an https:// spec.hardware.management.bmc.address and remove the tls.verify opt-out " +
		"on Machine/metal-01 or in spec.baremetal.defaults.bmc of InfraProvider/lab-metal; " +
		"name the authority that issued its certificate in tls.trustBundleRef when the system trust store does not hold it"
	for name, sources := range map[string]func(*testing.T) desiredstate.Sources{
		"verification disabled": func(t *testing.T) desiredstate.Sources {
			return rewritten(t, "trustBundleRef: lab-bmc-ca", "verify: false")
		},
		"an http controller": func(t *testing.T) desiredstate.Sources {
			return rewritten(t, "address: https://bmc-01.metal.example.test/", "address: http://bmc-01.metal.example.test/")
		},
	} {
		t.Run(name, func(t *testing.T) {
			state, _, err := wireCompiler().Compile(context.Background(), sources(t))
			if err != nil {
				t.Fatalf("admission: %v", diagnostics.Of(err))
			}
			capability, _ := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}).Resolve(installation.Kind, installation.Implementation)
			unsupported := capability.(lifecycle.UnsupportedReporter).Unsupported(state)
			if len(unsupported) != 1 || unsupported[0].Kind != "Machine" || unsupported[0].Name != "metal-01" ||
				unsupported[0].Reason != reason || unsupported[0].Remediation != remedy {
				t.Fatalf("unsupported = %+v", unsupported)
			}
			_, err = capability.Plan(context.Background(), lifecycle.PlanInput{
				Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "metal"}, State: state, Controller: "controller",
			})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" || reported[0].Message != reason || reported[0].Remediation != remedy {
				t.Fatalf("plan refusal = %#v", reported)
			}
		})
	}
}

// The example's installation delivers private material, so it refuses before
// registration when its controller would fetch the installer image without
// verifying the artifact server: whatever answered would be the installer
// that receives the key.
func TestLabBaremetalExampleRefusesItsInstallation(t *testing.T) {
	const address = "        address: https://bmc-01.metal.example.test/redfish/v1/Systems/1\n"
	sources := rewritten(t, address, address+"        virtualMedia:\n          tls:\n            trust: disable-verification\n")
	if len(sources.Files) != baremetalExampleFiles {
		t.Fatalf("example discovery: got %d files, want %d", len(sources.Files), baremetalExampleFiles)
	}
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		t.Fatalf("admission: %v", diagnostics.Of(err))
	}
	capability, _ := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}).Resolve(installation.Kind, installation.Implementation)
	unsupported := capability.(lifecycle.UnsupportedReporter).Unsupported(state)
	if len(unsupported) != 1 || unsupported[0].Name != "metal-01" ||
		unsupported[0].Reason != "a Machine that delivers private material through its installation cannot let its controller fetch without verifying the artifact server" {
		t.Fatalf("unsupported = %+v", unsupported)
	}
}
