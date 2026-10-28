//go:build linux && amd64

package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

func TestExampleNVMeOFPlansOwnedImageAndGateway(t *testing.T) {
	sources := exampleDirectory(t, "libvirt-5-nodes-ceph-nvmeof")
	state, _ := compileAcceptance(t, sources)
	guests := func(catalog api.Catalog) int {
		count := 0
		for _, machine := range catalog.OfKind(api.Machine) {
			if machine.Spec().Get("substrate", "libvirt", "profileRef").Text() != "" {
				count++
			}
		}
		return count
	}
	if authored, effective := guests(profileDeclarations(t, sources)), guests(state.Effective()); authored != 6 || effective != 6 {
		t.Fatalf("P4 requires six budgeted guests, the standalone initiator retained beside five storage guests, got %d/%d", authored, effective)
	}
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("refused %+v", refused)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, productionDefinitions(t, state, "libvirt-5-nodes-ceph-nvmeof"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	index := slices.IndexFunc(plan.Blocks, func(b reconciliation.Block) bool { return b.ID == "ceph-nvmeof-application" })
	if index < 0 {
		t.Fatal("no NVMe-oF subsystem block")
	}
	for _, id := range []string{"ceph-image-ceph-lab", "ceph-cluster-ceph-lab", ceph.ServiceUnitBlockID("ceph-lab", ceph.ServiceUnit{Type: "nvmeof", ID: "nvme-config.primary"}), "ceph-rbd-rbd-disk"} {
		if !slices.Contains(plan.Blocks[index].Dependencies, id) {
			t.Errorf("missing dependency %s: %v", id, plan.Blocks[index].Dependencies)
		}
	}
}

// An nvmeof service without a usable spec.pool never reaches a plan: the
// compiler refuses it by name at the service's own pool field.
func TestExampleNVMeOFRefusesAGatewayServiceWithoutAUsablePool(t *testing.T) {
	for _, test := range []struct {
		name, replacement, code string
	}{
		{"empty pool", "        pool: \"\"\n", "api.invariant"},
		{"absent pool", "", "api.required"},
		{"undeclared pool", "        pool: nvme-absent\n", "api.invariant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := exampleDirectory(t, "libvirt-5-nodes-ceph-nvmeof")
			index := slices.IndexFunc(sources.Files, func(file desiredstate.SourceFile) bool {
				return file.Path() == filepath.Join(sources.Roots[0], "storage", "cluster.yaml")
			})
			const declared = "        pool: nvme-config\n"
			if index < 0 || strings.Count(string(sources.Files[index].Bytes()), declared) != 1 {
				t.Fatalf("the example cluster does not declare %q exactly once", declared)
			}
			body := strings.Replace(string(sources.Files[index].Bytes()), declared, test.replacement, 1)
			sources.Files[index] = desiredstate.NewSourceFile(sources.Files[index].Path(), []byte(body))
			_, _, err := wireCompiler().Compile(context.Background(), sources)
			refused := diagnostics.Of(err)
			if len(refused) != 1 || refused[0].Code != test.code || refused[0].Field != "$.spec.services[3].spec.pool" || refused[0].Remediation == "" {
				t.Fatalf("compiled with %s, refusals %+v", test.name, refused)
			}
		})
	}
}
