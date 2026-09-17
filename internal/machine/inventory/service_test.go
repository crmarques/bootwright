package inventory

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
)

type effectiveState struct{ context string }

func (s *effectiveState) RenderEffective(_ context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	s.context = request.ContextName
	return &compilation.EffectiveResult{Effective: catalog()}, nil
}

type ownership struct{}

func (ownership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return map[string]machine.OwnershipState{"Machine/guest": {Verb: "apply", State: "done"}}, nil
}

type controllers struct {
	calls    int
	context  string
	asked    []string
	readings map[string]string
	refuse   error
}

func (c *controllers) Read(_ context.Context, contextName string, names []string) (map[string]string, error) {
	c.calls++
	c.context, c.asked = contextName, slices.Clone(names)
	if c.refuse != nil {
		return nil, c.refuse
	}
	return c.readings, nil
}

func service(power PowerReader) Service {
	return New(&effectiveState{}, ownership{}, power, func(context.Context) (string, error) { return "lab", nil })
}

// The one host contact this command makes is opt-in, so an ordinary listing
// never reaches a management controller and never waits on one.
func TestListReadsNoControllerUnlessTheInvocationAsked(t *testing.T) {
	readers := &controllers{}
	result, err := service(readers).List(context.Background(), ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if readers.calls != 0 {
		t.Fatalf("a listing contacted %d controllers", readers.calls)
	}
	if result.PowerRead || result.Context != "lab" {
		t.Fatalf("result = %+v", result)
	}
	if !slices.ContainsFunc(result.Machines, func(row MachineRow) bool { return row.Lifecycle == LifecycleApplied }) {
		t.Fatal("the applied Machine did not report its lifecycle position")
	}
}

// A reading is asked for by name, in the exact context the listing resolved,
// and lands on the row of the Machine its controller answered for.
func TestListCarriesEachReadingToItsOwnRow(t *testing.T) {
	readers := &controllers{readings: map[string]string{"guest": machine.PowerOn, "host": machine.PowerUnknown}}
	result, err := service(readers).List(context.Background(), ListRequest{Power: true})
	if err != nil {
		t.Fatal(err)
	}
	if readers.calls != 1 || readers.context != "lab" {
		t.Fatalf("readings were taken %d times in context %q", readers.calls, readers.context)
	}
	if !slices.Equal(readers.asked, []string{"guest", "host", "node"}) {
		t.Fatalf("the reading asked about %v", readers.asked)
	}
	if !result.PowerRead {
		t.Fatal("a result carrying readings did not report that controllers were asked")
	}
	want := map[string]string{"guest": machine.PowerOn, "host": machine.PowerUnknown, "node": ""}
	for _, row := range result.Machines {
		if row.Power != want[row.Name] {
			t.Fatalf("%s reported power %q, want %q", row.Name, row.Power, want[row.Name])
		}
	}
}

// A cluster selection filters presentation alone, so only the Machines the
// listing reports are the Machines a reading asks about.
func TestListReadsOnlyTheSelectedMachines(t *testing.T) {
	readers := &controllers{readings: map[string]string{}}
	if _, err := service(readers).List(context.Background(), ListRequest{Clusters: []string{"ocp"}, Power: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(readers.asked, []string{"node"}) {
		t.Fatalf("the reading asked about %v", readers.asked)
	}
}

// A reading that refuses refuses the command: an inspection that was asked for
// live state must not quietly present the listing without it.
func TestListRefusesWhenTheReadingCannotBeTaken(t *testing.T) {
	refusal := errors.New("no runtime")
	if _, err := service(&controllers{refuse: refusal}).List(context.Background(), ListRequest{Power: true}); !errors.Is(err, refusal) {
		t.Fatalf("err = %v", err)
	}
}
