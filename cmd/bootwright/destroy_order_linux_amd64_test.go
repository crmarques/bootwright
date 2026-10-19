//go:build linux && amd64

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// TestDestroyOrderGoldenBeforeAndAfterBlockIDDependencies records, for each
// Ceph example, every Ceph block with its wave in the apply plan and in the
// removal plan built from it. The goldens were recorded before the cluster
// children named the cluster block by its identity, so an unchanged golden
// proves that change moves no block in either order.
func TestDestroyOrderGoldenBeforeAndAfterBlockIDDependencies(t *testing.T) {
	for _, name := range []string{"libvirt-3-nodes-ceph", "libvirt-3-nodes-ceph-block-file"} {
		t.Run(name, func(t *testing.T) {
			state, _ := compileAcceptance(t, exampleDirectory(t, name))
			apply, err := reconciliation.NewPlan(reconciliation.Apply, productionDefinitions(t, state, name))
			if err != nil {
				t.Fatal(err)
			}
			removal, err := apply.Inverse()
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			for _, plan := range []reconciliation.Plan{apply, removal} {
				fmt.Fprintf(&text, "%s:\n", plan.Verb)
				wave := map[string]int{}
				for _, block := range plan.Blocks {
					for _, dependency := range block.Dependencies {
						wave[block.ID] = max(wave[block.ID], wave[dependency]+1)
					}
					if strings.HasPrefix(block.ID, "ceph-") {
						fmt.Fprintf(&text, "  %d %s\n", wave[block.ID], block.ID)
					}
				}
			}
			matchesTextGolden(t, "destroy-order-"+name, []byte(text.String()))
		})
	}
}
