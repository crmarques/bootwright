package preflight

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func TestGuestPrerequisitesUseFixedReadsAndNeverReturnRawOutput(t *testing.T) {
	host := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue(api.FieldValue{Name: "network", Value: api.MapValue(api.FieldValue{Name: "addresses", Value: api.ListValue(api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("fqdn")}, api.FieldValue{Name: "address", Value: api.StringValue("guest.example.test")}))})}))
	calls := 0
	checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
		calls++
		if request.ContextName != "synthetic" || request.Machine != "guest" {
			t.Fatal(request)
		}
		var output string
		switch request.Argv[0] {
		case "/usr/bin/hostname":
			output = "guest.example.test\n"
		case "/usr/bin/uname":
			output = "synthetic-kernel\n"
		case "/usr/bin/cat":
			output = "ID=synthetic\n"
		case "/usr/bin/podman":
			output = "synthetic-runtime\n"
		case "/usr/bin/timedatectl":
			output = "yes\n"
		case "/usr/bin/id":
			output = "0\n"
		case "/usr/bin/getent":
			output = "192.0.2.1 STREAM guest.example.test\n"
		default:
			t.Fatalf("unexpected command %v", request.Argv)
		}
		return []byte(output), nil
	})}
	rows, err := checker.CheckGuest(context.Background(), "synthetic", host, PreflightRequest{})
	if err != nil || calls != 7 {
		t.Fatalf("%d %v", calls, err)
	}
	for _, row := range rows {
		if row.Status != capacity.Passed || strings.Contains(row.Reason, "synthetic-") {
			t.Fatalf("%+v", row)
		}
	}
	checker.Commands = CommandFunc(func(context.Context, CommandRequest) ([]byte, error) { return nil, errors.New("secret source error") })
	rows, err = checker.CheckGuest(context.Background(), "synthetic", host, PreflightRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status != capacity.Unknown || strings.Contains(row.Reason, "secret") {
			t.Fatalf("%+v", row)
		}
	}
}

func TestUnknownHostKeyObservationNeverContinuesWithCredentials(t *testing.T) {
	host := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue())
	for _, observe := range []bool{false, true} {
		calls := 0
		checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
			calls++
			if request.ObserveUntrusted != observe {
				t.Fatal("ignored first-use observation preference")
			}
			return nil, UntrustedKey{Fingerprint: "SHA256:synthetic"}
		})}
		rows, err := checker.CheckGuest(context.Background(), "synthetic", host, PreflightRequest{TrustOnFirstUse: observe})
		if err != nil || calls != 1 || len(rows) != 1 || rows[0].Status != capacity.Unknown || rows[0].ID != "host.trust" || !strings.Contains(rows[0].Next, "--name guest") {
			t.Fatalf("%+v %d %v", rows, calls, err)
		}
	}
}
