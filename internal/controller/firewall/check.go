package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Socket struct {
	Object, Block, Address, Protocol string
	Port                             int
}

type Observation struct {
	Interface, Zone, Status, Reason string
}

type Check struct {
	Socket      Socket
	Observation Observation
}

const ManualRemedy = "verify the controller interface's runtime firewall permits the declared port from every BMC, installer and service client; configure the operator-owned firewall, then repeat bootwright preflight --stage infrastructure-services"

func (c Check) Summary() string {
	address := c.Socket.Address
	if strings.Contains(address, ":") {
		address = "[" + address + "]"
	}
	return fmt.Sprintf("%s %s:%d/%s: %s (interface %s, zone %s); %s", c.Socket.Object, address, c.Socket.Port, c.Socket.Protocol, c.Observation.Status, c.Observation.Interface, c.Observation.Zone, c.Observation.Reason)
}

const inspectionBudget = 45 * time.Second

func CheckSockets(ctx context.Context, inspector Inspector, sockets []Socket) ([]Check, error) {
	return checkSockets(ctx, inspector, sockets, inspectionBudget)
}

func inspectable(socket Socket, address netip.Addr, parseErr error) bool {
	return socket.Address != "" && (parseErr != nil || !address.IsUnspecified()) && socket.Port > 0 && socket.Port <= 65535 && (socket.Protocol == "tcp" || socket.Protocol == "udp")
}

func checkSockets(ctx context.Context, inspector Inspector, sockets []Socket, budget time.Duration) ([]Check, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if session, ok := inspector.(Sessioner); ok {
		inspector = session.Session()
	}
	ordered := slices.Clone(sockets)
	slices.SortFunc(ordered, func(a, b Socket) int {
		return strings.Compare(fmt.Sprintf("%s/%s/%s/%05d", a.Object, a.Address, a.Protocol, a.Port), fmt.Sprintf("%s/%s/%s/%05d", b.Object, b.Address, b.Protocol, b.Port))
	})
	checks := make([]Check, 0, len(ordered))
	for _, socket := range ordered {
		if err := parent.Err(); err != nil {
			return nil, err
		}
		address, err := netip.ParseAddr(socket.Address)
		if err == nil && address.IsLoopback() {
			continue
		}
		observed := Observation{Status: "unknown", Reason: "read-only firewall inspection is unavailable"}
		if inspector != nil && ctx.Err() != nil && inspectable(socket, address, err) {
			observed = Observation{Status: "unknown", Reason: "the shared firewall inspection budget was exhausted before this socket was inspected"}
		} else if inspector != nil && inspectable(socket, address, err) {
			observed, err = inspector.Inspect(ctx, socket)
			if err != nil {
				if parent.Err() != nil {
					return nil, parent.Err()
				}
				observed = Observation{Status: "unknown", Reason: "read-only firewall inspection did not complete"}
			}
		}
		if observed.Status != "open" && observed.Status != "closed" && observed.Status != "unknown" {
			observed = Observation{Status: "unknown", Reason: "firewall inspection returned no qualified result"}
		}
		checks = append(checks, Check{Socket: socket, Observation: observed})
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	return checks, nil
}

func AdmitRequired(checks []Check, required map[string]bool) error {
	var unresolved *Check
	for index := range checks {
		check := &checks[index]
		if !required[check.Socket.Block] || check.Observation.Status == "open" {
			continue
		}
		if check.Observation.Status == "closed" {
			return diagnostics.NewFailureWithRemediation("preflight.failed", "required controller listener firewall admission is closed", check.Socket.Object, check.Summary()+"; "+ManualRemedy)
		}
		if unresolved == nil {
			unresolved = check
		}
	}
	if unresolved != nil {
		return diagnostics.NewFailureWithRemediation("preflight.unknown", "required controller listener firewall admission is unknown", unresolved.Socket.Object, unresolved.Summary()+"; "+ManualRemedy)
	}
	return nil
}
