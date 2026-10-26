package reconciliation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestPlanRequestAggregateBound(t *testing.T) {
	var definitions []BlockDefinition
	for index := 0; index < MaxPlanRequestBytes/MaxRequestBytes; index++ {
		d := definition(fmt.Sprintf("block-%d", index))
		d.Request = json.RawMessage(`{"payload":"` + strings.Repeat("x", MaxRequestBytes-len(`{"payload":""}`)) + `"}`)
		definitions = append(definitions, d)
	}
	plan, err := NewPlan(Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Inverse(); err != nil {
		t.Fatal(err)
	}
	definitions = append(definitions, definition("excess"))
	_, err = NewPlan(Apply, definitions)
	issues := diagnostics.Of(err)
	if len(issues) != 1 || issues[0].Code != "lifecycle.state" || !strings.Contains(issues[0].Message, "aggregate request") {
		t.Fatalf("aggregate refusal = %v", err)
	}
}

func TestPlanCollectionBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit int
		set   func(*BlockDefinition, int)
	}{
		{"impact", MaxImpacts, func(d *BlockDefinition, n int) {
			d.Impacts = make([]string, n)
			for i := range d.Impacts {
				d.Impacts[i] = "power-on"
			}
		}},
		{"group", MaxGroups, func(d *BlockDefinition, n int) {
			d.Groups = make([]Group, n)
			for i := range d.Groups {
				d.Groups[i] = Group{ID: fmt.Sprintf("group-%d", i), Description: "install", Machines: []string{"machine"}}
			}
		}},
		{"Machine", MaxMachines, func(d *BlockDefinition, n int) {
			d.Groups[0].Machines = make([]string, n)
			for i := range d.Groups[0].Machines {
				d.Groups[0].Machines[i] = fmt.Sprintf("machine-%04d", i)
			}
		}},
		{"exclusive", MaxExclusive, func(d *BlockDefinition, n int) {
			d.Exclusive = make([]string, n)
			for i := range d.Exclusive {
				d.Exclusive[i] = fmt.Sprintf("resource-%04d", i)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := definition("valid")
			test.set(&d, test.limit)
			if _, err := NewPlan(Apply, []BlockDefinition{d}); err != nil {
				t.Fatal(err)
			}
			test.set(&d, test.limit+1)
			_, err := NewPlan(Apply, []BlockDefinition{d})
			issues := diagnostics.Of(err)
			if len(issues) != 1 || issues[0].Code != "lifecycle.state" || !strings.Contains(issues[0].Message, test.name) {
				t.Fatalf("bound refusal = %v", err)
			}
		})
	}
}
