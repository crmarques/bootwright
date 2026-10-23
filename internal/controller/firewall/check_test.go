package firewall

import (
	"context"
	"testing"
)

type observed struct {
	calls  int
	status string
}

func (o *observed) Inspect(context.Context, Socket) (Observation, error) {
	o.calls++
	return Observation{Status: o.status}, nil
}

func TestLoopbackOmissionAndUnknownNeverPass(t *testing.T) {
	probe := &observed{status: "ready"}
	checks, err := CheckSockets(context.Background(), probe, []Socket{{Address: "127.0.0.1", Protocol: "tcp", Port: 80}, {Address: "192.0.2.1", Protocol: "tcp", Port: 80}})
	if err != nil || len(checks) != 1 || probe.calls != 1 || checks[0].Observation.Status != "unknown" {
		t.Fatalf("checks=%+v, calls=%d, err=%v", checks, probe.calls, err)
	}
	checks, err = CheckSockets(context.Background(), nil, []Socket{{Address: "192.0.2.1", Protocol: "tcp", Port: 80}})
	if err != nil || checks[0].Observation.Status != "unknown" {
		t.Fatal(checks, err)
	}
}
