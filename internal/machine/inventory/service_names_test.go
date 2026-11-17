package inventory

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

func TestListSelectsTheNamedMachinesAndRefusesAnUnknownName(t *testing.T) {
	listing := service(&controllers{})
	all, err := listing.List(context.Background(), ListRequest{})
	if err != nil || len(all.Machines) < 2 {
		t.Fatalf("an omitted selector must keep every Machine: %+v err=%v", all, err)
	}
	result, err := listing.List(context.Background(), ListRequest{Names: []string{"guest"}})
	if err != nil || !slices.Equal(Names(result.Machines), []string{"guest"}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, names := range [][]string{{"guest", "absent"}, {"gue"}} {
		_, err = listing.List(context.Background(), ListRequest{Names: names})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "access.target" || reported[0].Message != "the context lab selects no Machine named "+names[len(names)-1] {
			t.Fatalf("names %q: err = %v, diagnostics = %+v", names, err, reported)
		}
	}
}

func TestListAsksOnlyTheNamedMachinesForPower(t *testing.T) {
	readers := &controllers{readings: map[string]string{"guest": machine.PowerOn}}
	result, err := service(readers).List(context.Background(), ListRequest{Names: []string{"guest"}, Power: true})
	if err != nil || !slices.Equal(readers.asked, []string{"guest"}) || result.Machines[0].Power != machine.PowerOn {
		t.Fatalf("asked %q, result=%+v err=%v", readers.asked, result, err)
	}
}
