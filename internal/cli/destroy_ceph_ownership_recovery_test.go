package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
)

func TestDestroyRecoverCephOwnershipValidatesAndEmitsConfirmedFSID(t *testing.T) {
	ctx := initTestContext(t, "006-ceph-3nodes-libvirt-managed-os")
	const (
		cluster  = "ceph-libvirt"
		seedHost = "storage__ceph-libvirt__ceph-0"
		fsid     = "2088ddee-875b-11f1-9b98-303ea72d7724"
	)

	help, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(help, "--recover-ceph-ownership") {
		t.Fatalf("destroy help must document --recover-ceph-ownership:\n%s", help)
	}

	_, stderr, code = runCLI(t,
		"destroy",
		"--stage", "infra",
		"--recover-ceph-ownership", cluster+"="+fsid,
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 2 || !strings.Contains(stderr, "clusters stage") {
		t.Fatalf("infra-only recovery code=%d stderr=%q", code, stderr)
	}

	_, stderr, code = runCLI(t,
		"destroy",
		"--clusters", cluster,
		"--recover-ceph-ownership", cluster+"=not-a-uuid",
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 2 || !strings.Contains(stderr, "must be a UUID") {
		t.Fatalf("invalid-fsid recovery code=%d stderr=%q", code, stderr)
	}

	stdout, stderr, code := runCLI(t,
		"destroy",
		"--clusters", cluster,
		"--recover-ceph-ownership", cluster+"="+fsid,
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("unrecorded recovery exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode unrecorded dry-run json: %v\n%s", err, stdout)
	}
	want := `{"` + converge.DestroyCephOwnershipRecoveryExtraVar + `":{"` + cluster + `":"` + fsid + `"}}`
	if !slices.Contains(report.ExtraVars, want) {
		t.Fatalf("unrecorded recovery extra var missing %q from %v", want, report.ExtraVars)
	}

	record := ownership.ResourceRecord{
		Kind:    string(ownership.KindStorageCluster),
		Name:    cluster,
		Owner:   ownership.Owner,
		Context: ctx.Name,
		Host:    seedHost + "-wrong",
		Cluster: cluster,
		Attributes: map[string]string{
			"seedHost": seedHost + "-wrong",
		},
	}
	if err := ownership.SaveResource(ctx.OwnershipDir, record); err != nil {
		t.Fatalf("save storage ownership record: %v", err)
	}

	_, stderr, code = runCLI(t,
		"destroy",
		"--clusters", cluster,
		"--recover-ceph-ownership", cluster+"="+fsid,
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 1 || !strings.Contains(stderr, "ownership evidence conflicts") {
		t.Fatalf("conflicting recovery code=%d stderr=%q", code, stderr)
	}

	record.Host = seedHost
	record.Attributes["seedHost"] = seedHost
	if err := ownership.SaveResource(ctx.OwnershipDir, record); err != nil {
		t.Fatalf("replace storage ownership record: %v", err)
	}

	stdout, stderr, code = runCLI(t,
		"destroy",
		"--clusters", cluster,
		"--recover-ceph-ownership", cluster+"="+fsid,
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("recorded recovery exited %d, stderr=%q", code, stderr)
	}
	report = scopeDryRunReport{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode dry-run json: %v\n%s", err, stdout)
	}
	if !slices.Contains(report.ExtraVars, want) {
		t.Fatalf("recovery extra var missing %q from %v", want, report.ExtraVars)
	}
}
