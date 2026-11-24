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
	rows, err := checker.CheckGuest(context.Background(), "synthetic", host, HostRequirements{}, PreflightRequest{})
	if err != nil || calls != 6 {
		t.Fatalf("%d %v", calls, err)
	}
	for _, row := range rows {
		if row.Status != capacity.Passed || strings.Contains(row.Reason, "synthetic-") {
			t.Fatalf("%+v", row)
		}
	}
	checker.Commands = CommandFunc(func(context.Context, CommandRequest) ([]byte, error) { return nil, errors.New("secret source error") })
	rows, err = checker.CheckGuest(context.Background(), "synthetic", host, HostRequirements{}, PreflightRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status != capacity.Unknown || strings.Contains(row.Reason, "secret") {
			t.Fatalf("%+v", row)
		}
	}
}

func TestHostWithoutTheContainerRuntimeIsReadyForTheHostPreparationThatInstallsIt(t *testing.T) {
	host := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue())
	answers := map[string]string{"/usr/bin/hostname": "guest\n", "/usr/bin/uname": "kernel\n", "/usr/bin/cat": "ID=rhel\n", "/usr/bin/timedatectl": "yes\n", "/usr/bin/id": "0\n", "/usr/bin/getent": "192.0.2.1 STREAM guest\n"}
	checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
		answer, found := answers[request.Argv[0]]
		if !found {
			return nil, errors.New("verified bounded prerequisite command did not complete")
		}
		return []byte(answer), nil
	})}
	rows, err := checker.CheckGuest(context.Background(), "synthetic", host, HostRequirements{}, PreflightRequest{})
	if err != nil || len(rows) != len(answers) {
		t.Fatalf("%+v %v", rows, err)
	}
	for _, row := range rows {
		if row.Status != capacity.Passed || row.ID == "host.runtime" {
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
		rows, err := checker.CheckGuest(context.Background(), "synthetic", host, HostRequirements{}, PreflightRequest{ObserveHostKeys: observe})
		if err != nil || calls != 1 || len(rows) != 1 || rows[0].Status != capacity.Unknown || rows[0].ID != "host.trust" || !strings.Contains(rows[0].Next, "--name guest") {
			t.Fatalf("%+v %d %v", rows, calls, err)
		}
	}
}

func TestAHostnameApplySetsIsPendingNotFailed(t *testing.T) {
	host := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue(api.FieldValue{Name: "network", Value: api.MapValue(api.FieldValue{Name: "addresses", Value: api.ListValue(api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("fqdn")}, api.FieldValue{Name: "address", Value: api.StringValue("guest.example.test")}))})}))
	checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
		answers := map[string]string{"/usr/bin/hostname": "other\n", "/usr/bin/uname": "kernel\n", "/usr/bin/cat": "ID=rhel\n", "/usr/bin/timedatectl": "yes\n", "/usr/bin/id": "0\n", "/usr/bin/getent": "192.0.2.1 STREAM guest.example.test\n"}
		return []byte(answers[request.Argv[0]]), nil
	})}
	for _, test := range []struct {
		name    string
		applied string
		want    string
	}{
		{"apply sets the declared name", "guest.example.test", capacity.Pending},
		{"apply sets no name", "", capacity.Failed},
	} {
		rows, err := checker.CheckGuest(context.Background(), "synthetic", host, HostRequirements{AppliedHostname: test.applied}, PreflightRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			want := capacity.Passed
			if row.ID == "host.identity" {
				want = test.want
			}
			if row.Status != want {
				t.Fatalf("%s: %+v, want %s", test.name, row, want)
			}
			if row.ID == "host.identity" && test.want == capacity.Pending && (row.Reason != "apply sets the hostname to guest.example.test" || !strings.Contains(row.Next, "--stage machines")) {
				t.Fatalf("%s: %+v", test.name, row)
			}
		}
	}
}
