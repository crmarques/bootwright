package power

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// grouped reports the read role's one group to each run's progress, as its
// group records do, and fails the run at position failAt (counting from one)
// once it has.
type grouped struct {
	surveyor
	failAt int
	err    error
}

func (g *grouped) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	if request.Progress != nil {
		request.Progress(ctx, "read-state", "running")
	}
	if len(g.runs)+1 == g.failAt {
		g.runs = append(g.runs, request)
		return lifecycle.RunResult{}, g.err
	}
	result, err := g.surveyor.Run(ctx, request)
	if err == nil && request.Progress != nil {
		request.Progress(ctx, "read-state", "ok")
	}
	return result, err
}

// A reading waits on every controller one host reaches, so each host is one
// check: it opens running, its groups are sub-steps, and it closes with how
// many of its Machines it read, or failed with the error the reading returns.
// It names no log location, because a reading keeps none.
func TestAPowerReadReportsOneCheckPerHost(t *testing.T) {
	check := func(host string, position int, group, detail, status string) lifecycle.ProgressEvent {
		return lifecycle.ProgressEvent{Phase: lifecycle.CheckPhase, Block: "power-read",
			Description: "Read the power state through Machine/" + host, Group: group, Detail: detail,
			Status: status, Position: position, Total: 2}
	}
	reports := map[string]string{"guest": machine.PowerOn, "metal": machine.PowerOff}

	reporter := &announced{}
	runner := &grouped{surveyor: surveyor{reports: reports}}
	if _, err := New(stateSource{}, evidenceSource{}, &pins{}, &boundary{}, runner, nil, reporter, nil).
		Read(context.Background(), "lab", selected()); err != nil {
		t.Fatal(err)
	}
	want := []lifecycle.ProgressEvent{
		check("controller", 1, "", "", "running"),
		check("controller", 1, "read-state", "read-state", "running"),
		check("controller", 1, "read-state", "read-state", "ok"),
		check("controller", 1, "", "1 of 1 machines read", "ok"),
		check("host", 2, "", "", "running"),
		check("host", 2, "read-state", "read-state", "running"),
		check("host", 2, "read-state", "read-state", "ok"),
		check("host", 2, "", "1 of 1 machines read", "ok"),
	}
	if !reflect.DeepEqual(reporter.events, want) {
		t.Fatalf("progress =\n%+v\nwant\n%+v", reporter.events, want)
	}
	if slices.Contains(reporter.order, "logs") {
		t.Fatalf("a reading named a log location: %v", reporter.order)
	}

	unreachable := errors.New("the controller host cannot be reached")
	reporter = &announced{}
	runner = &grouped{surveyor: surveyor{reports: reports}, failAt: 2, err: unreachable}
	if _, err := New(stateSource{}, evidenceSource{}, &pins{}, &boundary{}, runner, nil, reporter, nil).
		Read(context.Background(), "lab", selected()); !errors.Is(err, unreachable) {
		t.Fatalf("err = %v, want the run's own failure", err)
	}
	want = append(want[:5:5], check("host", 2, "read-state", "read-state", "running"), check("host", 2, "", "", "failed"))
	if !reflect.DeepEqual(reporter.events, want) {
		t.Fatalf("progress =\n%+v\nwant\n%+v", reporter.events, want)
	}
}
