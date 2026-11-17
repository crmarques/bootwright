package firewall

import (
	"context"
	"testing"
	"time"
)

type sessionProbe struct {
	sessions, inspections int
}

func (p *sessionProbe) Inspect(context.Context, Socket) (Observation, error) {
	return Observation{Status: "open"}, nil
}

func (p *sessionProbe) Session() Inspector {
	p.sessions++
	return sessionInspector{p}
}

type sessionInspector struct{ probe *sessionProbe }

func (s sessionInspector) Inspect(context.Context, Socket) (Observation, error) {
	s.probe.inspections++
	return Observation{Status: "open"}, nil
}

func TestACheckOpensOneInspectionSession(t *testing.T) {
	probe := &sessionProbe{}
	sockets := []Socket{{Address: "192.0.2.1", Protocol: "tcp", Port: 80}, {Address: "192.0.2.1", Protocol: "tcp", Port: 81}}
	checks, err := CheckSockets(context.Background(), probe, sockets)
	if err != nil || len(checks) != 2 || probe.sessions != 1 || probe.inspections != 2 {
		t.Fatalf("checks=%+v sessions=%d inspections=%d err=%v", checks, probe.sessions, probe.inspections, err)
	}
}

func TestAnExhaustedBudgetIsNamedAndOtherUnavailableReasonsStay(t *testing.T) {
	budget := Observation{Status: "unknown", Reason: "the shared firewall inspection budget was exhausted before this socket was inspected"}
	unavailable := Observation{Status: "unknown", Reason: "read-only firewall inspection is unavailable"}
	probe := &observed{status: "open"}
	checks, err := checkSockets(context.Background(), probe, []Socket{{Address: "192.0.2.1", Protocol: "tcp", Port: 80}}, -time.Second)
	if err != nil || len(checks) != 1 || checks[0].Observation != budget || probe.calls != 0 {
		t.Fatalf("checks=%+v calls=%d err=%v", checks, probe.calls, err)
	}
	checks, err = checkSockets(context.Background(), nil, []Socket{{Address: "192.0.2.1", Protocol: "tcp", Port: 80}}, time.Minute)
	if err != nil || checks[0].Observation != unavailable {
		t.Fatalf("checks=%+v err=%v", checks, err)
	}
	checks, err = checkSockets(context.Background(), probe, []Socket{{Address: "192.0.2.1", Protocol: "icmp", Port: 80}}, time.Minute)
	if err != nil || checks[0].Observation != unavailable || probe.calls != 0 {
		t.Fatalf("checks=%+v err=%v", checks, err)
	}
}
