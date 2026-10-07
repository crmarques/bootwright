//go:build linux && amd64

package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A controller shape the API admits but this executable cannot realize is
// reported through the engine's unsupported port, which a plan and an apply
// read before registration and refuse with lifecycle.unsupported, naming the
// Environment, the reason and the remedy. The controller stage's own
// planning refuses it with the same words, so no path plans it.
func TestUnsupportedControllerShapesRefuseBeforeRegistration(t *testing.T) {
	for _, test := range []struct {
		name, old, new string
		reason, remedy string
	}{
		{
			name: "an extra capability", old: "    - libvirt\n", new: "    - libvirt\n    - ceph-node\n",
			reason: "this executable's controller stage prepares only the container-runtime and libvirt capabilities, and Machine/controller declares another at spec.capabilities[2]",
			remedy: "remove spec.capabilities[2] from Machine/controller",
		},
		{
			name: "a managed Proxy", old: "    direct: {}\n", new: "    proxyRef: lab-proxy\n",
			reason: "this executable's controller stage acquires only directly or through an external Proxy that is already ready, and Machine/controller selects the managed Proxy/lab-proxy",
			remedy: "select direct: {} or an external Proxy in spec.proxy on Machine/controller",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := exampleDirectory(t, "lab-rhel")
			index := slices.IndexFunc(sources.Files, func(file desiredstate.SourceFile) bool {
				return file.Path() == filepath.Join(sources.Roots[0], "infra", "machines", "controller.yaml")
			})
			if index < 0 || strings.Count(string(sources.Files[index].Bytes()), test.old) != 1 {
				t.Fatalf("the controller Machine does not hold %q exactly once", test.old)
			}
			body := strings.Replace(string(sources.Files[index].Bytes()), test.old, test.new, 1)
			sources.Files[index] = desiredstate.NewSourceFile(sources.Files[index].Path(), []byte(body))
			state, _ := compileAcceptance(t, sources)
			capability, ok := buildCapabilities(systemClock{}, exampleControllerPorts(t), nil).Resolve(clients.Kind, clients.Implementation)
			if !ok {
				t.Fatal("the controller stage capability does not resolve")
			}
			reporter, ok := capability.(lifecycle.UnsupportedReporter)
			if !ok {
				t.Fatal("the controller stage capability reports nothing it cannot realize")
			}
			want := []lifecycle.Refusal{{Kind: "Environment", Name: "lab-rhel", Reason: test.reason, Remediation: test.remedy}}
			if got := reporter.Unsupported(state); !slices.Equal(got, want) {
				t.Fatalf("unsupported = %+v, want %+v", got, want)
			}
			_, err := capability.Plan(context.Background(), lifecycle.PlanInput{
				Verb: reconciliation.Apply, State: state, Controller: "controller",
				Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
			})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "controller.unsupported" || reported[0].Message != test.reason || reported[0].Remediation != test.remedy {
				t.Fatalf("planning refused %+v", reported)
			}
		})
	}
}
