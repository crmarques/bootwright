package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// renamedExample reads a copy of the example with its Environment renamed, so
// two examples that differ only by their bindings compare block for block.
func renamedExample(t *testing.T, name, environment string) desiredstate.Sources {
	t.Helper()
	exampleDirectory(t, name)
	copied := filepath.Join(t.TempDir(), "input")
	if err := os.CopyFS(copied, os.DirFS(examplePath(t, name))); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(copied, "environment.yaml")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(string(data), "name: "+name, "name: "+environment, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{copied})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
}

func exampleApplyPlan(t *testing.T, sources desiredstate.Sources, name string) (reconciliation.Plan, lifecycle.CapabilityResolver) {
	t.Helper()
	state, _ := compileAcceptance(t, sources)
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: name}}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved binding", binding)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(binding, err)
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	return plan, resolver
}

// The metallb binding lab-sno-ordered adds to lab-sno-nmstate is an addition to
// the plan that example froze: the real compiler and capabilities keep every
// frozen block and add one instance block that waits for the frozen NMState
// instance and the cluster installation as prerequisites. The harness runs no
// apply, so this tests the planning of the addition and the capability's
// admission of it, not a registered second operation.
func TestTheOrderedExampleIsAnAdditionToTheNMStateExample(t *testing.T) {
	root, _ := exampleApplyPlan(t, renamedExample(t, "lab-sno-nmstate", "lab-sno-ordered"), "lab-sno-ordered")
	fresh, resolver := exampleApplyPlan(t, exampleDirectory(t, "lab-sno-ordered"), "lab-sno-ordered")
	delta := reconciliation.Compare(root, fresh)
	if first, differs := delta.First(); differs && first.Change != reconciliation.Added || delta.Count(reconciliation.Added) != 1 {
		t.Fatalf("the ordered example differs from the NMState one by %+v", delta)
	}
	added, err := reconciliation.Additive(fresh, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(added.Blocks) != 1 || added.Blocks[0].ID != "addon-metallb-sno" {
		t.Fatalf("the addition holds %+v, want only addon-metallb-sno", added.Blocks)
	}
	block := added.Blocks[0]
	for _, prerequisite := range []string{"addon-nmstate-sno", agentinstall.InstallBlockID("sno")} {
		if !slices.Contains(block.Prerequisites, prerequisite) {
			t.Errorf("the added block's prerequisites %v omit %s", block.Prerequisites, prerequisite)
		}
	}
	if len(block.Dependencies) != 0 {
		t.Errorf("the added block waits in its own plan for %v", block.Dependencies)
	}
	whole, err := reconciliation.Lineage(root, added)
	if err != nil {
		t.Fatal(err)
	}
	wholeDigest, _ := whole.Digest()
	freshDigest, _ := fresh.Digest()
	if wholeDigest != freshDigest {
		t.Fatal("the lineage of the NMState plan and the addition is not the ordered plan")
	}
	capability, ok := resolver.Resolve(block.Kind, block.Implementation)
	if !ok {
		t.Fatal("no capability for the added block")
	}
	if err := capability.(lifecycle.DeltaAdmitter).AdmitAdded(root, block); err != nil {
		t.Fatalf("the capability refuses the addition: %v", err)
	}
	if scoped, ok := capability.(lifecycle.ApplyOnlyMaterial); !ok || !scoped.ReadsMaterialOnlyInApply(block) {
		t.Fatal("the capability does not state that it reads bound material only in its own apply")
	}
}
