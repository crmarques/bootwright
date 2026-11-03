//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type e2eRender struct {
	name string
	sets []string
	// refuses marks a render whose input a plan refuses today: the text every
	// refusing directory must carry. The planning test fails when a
	// directory refuses otherwise or when none refuses, so the mark leaves
	// with the defect it names.
	refuses string
}

var e2eRenders = map[string][]e2eRender{
	"lab-rhel":                       {{name: "default"}},
	"lab-sno":                        {{name: "default"}},
	"lab-sno-fips-controller":        {{name: "default"}, {name: "tpm2 on", sets: []string{"security.tpm2=true"}}},
	"lab-network-variants":           {{name: "default"}},
	"lab-3-nodes-ceph-host-variants": {{name: "default"}},
	"lab-3-nodes-ocp":                {{name: "metallb off", sets: []string{"addons.metallb=false"}}},
	"lab-3-nodes-ocp-features":       {{name: "mirror off", sets: []string{"mirror.enabled=false"}}},
	"lab-5-nodes-ceph-stretch": {
		{name: "default"},
		{name: "file off", sets: []string{"ceph.services.file=false"}},
		{name: "object off", sets: []string{"ceph.services.object=false"}},
		{name: "file on", sets: []string{"ceph.services.file=true"}},
		{name: "object on", sets: []string{"ceph.services.object=true"}},
		{name: "file and object on", sets: []string{"ceph.services.file=true", "ceph.services.object=true"}},
	},
	"lab-3-nodes-ceph": {
		{name: "default"},
		{name: "block off", sets: []string{"ceph.services.block=false"}},
		{name: "file off", sets: []string{"ceph.services.file=false"}},
		{name: "object off", sets: []string{"ceph.services.object=false"}},
		{name: "nfs off", sets: []string{"ceph.services.nfs=false"}},
		{name: "smb off", sets: []string{"ceph.services.smb=false"}},
		{name: "observability off", sets: []string{"ceph.services.observability=false"}},
		{name: "nvmeof on", sets: []string{"ceph.services.nvmeof=true"}},
		{name: "nvmeof on block off", sets: []string{"ceph.services.nvmeof=true", "ceph.services.block=false"}},
		{name: "nvmeof on file off", sets: []string{"ceph.services.nvmeof=true", "ceph.services.file=false"}},
		{name: "nvmeof on object off", sets: []string{"ceph.services.nvmeof=true", "ceph.services.object=false"}},
		{name: "nvmeof on observability off", sets: []string{"ceph.services.nvmeof=true", "ceph.services.observability=false"}},
		{name: "nvmeof only", sets: []string{
			"ceph.services.nvmeof=true",
			"ceph.services.block=false", "ceph.services.file=false", "ceph.services.object=false",
			"ceph.services.nfs=false", "ceph.services.smb=false", "ceph.services.observability=false",
		}},
		{name: "every group off", sets: []string{
			"ceph.services.block=false", "ceph.services.file=false", "ceph.services.object=false",
			"ceph.services.nfs=false", "ceph.services.smb=false", "ceph.services.observability=false",
		}},
	},
}

func e2eCaseNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "e2e"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, required := range []string{"values.yaml", "templates"} {
			if _, err := os.Stat(filepath.Join("..", "..", "e2e", entry.Name(), required)); err != nil {
				t.Fatalf("e2e/%s is not a case: %v", entry.Name(), err)
			}
		}
		names = append(names, entry.Name())
	}
	return names
}

func e2eInputDirectories(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var directories []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			return []string{root}
		}
		if entry.IsDir() {
			directories = append(directories, filepath.Join(root, entry.Name()))
		}
	}
	if len(directories) == 0 {
		t.Fatalf("%s rendered no input", root)
	}
	return directories
}

func renderE2ECase(t *testing.T, script, name string, render e2eRender) []string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "input")
	arguments := []string{name, "--out-dir", output}
	for _, set := range render.sets {
		arguments = append(arguments, "--set", set)
	}
	if combined, err := exec.Command(script, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("render.sh %s: %v\n%s", strings.Join(arguments, " "), err, combined)
	}
	return e2eInputDirectories(t, output)
}

func e2eRenderScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("rendering the e2e cases needs helm on PATH: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "e2e", "render.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func TestEveryE2ECaseRendersToValidInput(t *testing.T) {
	script := e2eRenderScript(t)
	names := e2eCaseNames(t)
	for name := range e2eRenders {
		if !slices.Contains(names, name) {
			t.Fatalf("e2eRenders names %q, which is not a case under e2e/", name)
		}
	}
	for _, name := range names {
		if len(e2eRenders[name]) == 0 {
			t.Fatalf("e2e/%s has no entry in e2eRenders; add the renders its runbook selects", name)
		}
	}
	for _, name := range names {
		for _, render := range e2eRenders[name] {
			t.Run(name+"/"+render.name, func(t *testing.T) {
				for _, directory := range renderE2ECase(t, script, name, render) {
					var out, errOut bytes.Buffer
					code := run(context.Background(), []string{"validate", "-f", directory, "--output", "json"}, &out, &errOut)
					if code != 0 {
						var envelope struct {
							Diagnostics []struct {
								Code    string
								Message string
								Source  struct{ Path, Kind, Name string }
							}
						}
						if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
							t.Fatalf("validate %s exited %d: %s %s", directory, code, out.String(), errOut.String())
						}
						for _, diagnostic := range envelope.Diagnostics {
							t.Errorf("validate %s: %s %s/%s: %s", filepath.Base(directory), diagnostic.Code, diagnostic.Source.Kind, diagnostic.Source.Name, diagnostic.Message)
						}
						t.Fatalf("validate %s exited %d", directory, code)
					}
				}
			})
		}
	}
}

// Validation proves an input is well formed; only planning proves the
// production capabilities admit it, so each render is planned the way its
// runbook applies it: every rendered directory is one context, planned alone.
// A claim another context holds is invisible to that plan, so
// TestEveryE2ECaseContextsClaimDisjointHostReservations compares the contexts.
func TestEveryE2ECaseRenderPlansWithoutRefusal(t *testing.T) {
	script := e2eRenderScript(t)
	for _, name := range e2eCaseNames(t) {
		for _, render := range e2eRenders[name] {
			t.Run(name+"/"+render.name, func(t *testing.T) {
				refusing := 0
				for _, directory := range renderE2ECase(t, script, name, render) {
					refusal, _ := planE2EInput(t, directory)
					switch {
					case refusal == "":
					case render.refuses != "" && strings.Contains(refusal, render.refuses):
						refusing++
					default:
						t.Errorf("%s: the plan refuses: %s", filepath.Base(directory), refusal)
					}
				}
				if render.refuses != "" && refusing == 0 {
					t.Errorf("no directory refuses with %q; drop the refuses mark", render.refuses)
				}
			})
		}
	}
}

// Every directory of one render is a context of one controller host, so the
// host reservations their plans claim must not conflict, which the controller
// applies when it publishes the second context's claims.
func TestEveryE2ECaseContextsClaimDisjointHostReservations(t *testing.T) {
	script := e2eRenderScript(t)
	for _, name := range e2eCaseNames(t) {
		for _, render := range e2eRenders[name] {
			t.Run(name+"/"+render.name, func(t *testing.T) {
				var contexts [][]prerequisites.HostReservation
				var names []string
				for _, directory := range renderE2ECase(t, script, name, render) {
					refusal, reservations := planE2EInput(t, directory)
					if refusal != "" {
						t.Skipf("%s: the plan refuses (%s); the planning test owns that", filepath.Base(directory), refusal)
					}
					contexts = append(contexts, reservations)
					names = append(names, filepath.Base(directory))
				}
				for i := range contexts {
					for j := range contexts {
						if i == j {
							continue
						}
						for _, conflict := range prerequisites.Conflicts(contexts[i], contexts[j]) {
							t.Errorf("%s then %s: %s", names[i], names[j], prerequisites.ConflictDiagnostic(conflict).Message)
						}
					}
				}
			})
		}
	}
}

func planE2EInput(t *testing.T, directory string) (string, []prerequisites.HostReservation) {
	t.Helper()
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	state, _ := compileAcceptance(t, sources)
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		return fmt.Sprintf("the capabilities refuse %+v", refused), nil
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: filepath.Base(directory)}}
	var definitions []reconciliation.BlockDefinition
	var reservations []prerequisites.HostReservation
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			return fmt.Sprintf("%v: %+v", binding, diagnostics.Of(err)), nil
		}
		definitions = append(definitions, contribution.Definitions...)
		reservations = append(reservations, contribution.Reservations...)
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		return err.Error(), nil
	}
	return "", reservations
}
