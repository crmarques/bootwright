package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

const smbProducedSentinel = "smb-produced-Lr4k70"

func smbShareRun(t *testing.T) (SMBRequest, lifecycle.Execution) {
	t.Helper()
	catalog := smbGraph(t)
	r := builtSMB(t, catalog, sambaBase(t, catalog), "smb-docs")
	return r, smbExecution(t, r, "pw")
}

func smbProduced() []lifecycle.Produced {
	return []lifecycle.Produced{{Name: "unexpected", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(smbProducedSentinel)})}}
}

func producedCleared(t *testing.T, produced []lifecycle.Produced) {
	t.Helper()
	value, _ := produced[0].Material.Part(secrets.ValuePart)
	if !bytes.Equal(value, make([]byte, len(smbProducedSentinel))) {
		t.Fatal("the produced value was not cleared")
	}
}

func TestSMBApplyProvesItsPostcondition(t *testing.T) {
	r, e := smbShareRun(t)
	partial := func(s *SMBEvidence) { s.Ready, s.Partial = false, true }
	cases := map[string]lifecycle.RunResult{
		"partial":         {Outcome: "changed", Evidence: smbReady(t, e, r, partial)},
		"another owner":   {Outcome: "changed", Evidence: smbReady(t, e, r, func(s *SMBEvidence) { s.Owner = "apply-other" })},
		"absent":          {Outcome: "changed", Evidence: smbReady(t, e, r, func(s *SMBEvidence) { s.Exists, s.Ready, s.Resource, s.Daemons = false, false, "", []SMBDaemon{} })},
		"malformed":       {Outcome: "changed", Evidence: json.RawMessage(`{"version":"ceph-smb-evidence-v1"}`)},
		"no evidence":     {Outcome: "changed"},
		"produced values": {Outcome: "changed", Evidence: smbReady(t, e, r, nil), Produced: smbProduced()},
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{result: result}
			got, err := NewSMB(runner).Apply(context.Background(), e)
			refusedWith(t, err, "lifecycle.unknown", "Ceph SMB did not prove the requested postcondition", "")
			if got.Outcome != reconciliation.OutcomeUnknown || runner.calls != 1 || len(got.Evidence) != 0 {
				t.Fatalf("apply = %+v, calls %d", got, runner.calls)
			}
			if len(result.Produced) != 0 {
				producedCleared(t, result.Produced)
			}
		})
	}
	for _, outcome := range []string{"failed", "skipped", ""} {
		runner := &recordingRunner{result: lifecycle.RunResult{Outcome: outcome, Evidence: smbReady(t, e, r, nil)}}
		got, err := NewSMB(runner).Apply(context.Background(), e)
		refusedWith(t, err, "lifecycle.unknown", "Ceph SMB returned an unknown outcome", "")
		if got.Outcome != reconciliation.OutcomeUnknown {
			t.Fatalf("outcome %q applies as %+v", outcome, got)
		}
	}
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: smbReady(t, e, r, nil)}}
	got, err := NewSMB(runner).Apply(context.Background(), e)
	if err != nil || got.Outcome != reconciliation.OutcomeChanged || string(got.Evidence) != string(runner.result.Evidence) {
		t.Fatalf("ready apply = %+v (%v)", got, diagnostics.Of(err))
	}
}

func TestSMBObserveReportsTheProvedEffect(t *testing.T) {
	r, e := smbShareRun(t)
	deadline, err := smbDeadline(r, "observe")
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		change func(*SMBEvidence)
		want   reconciliation.EffectState
	}{
		"ready":   {nil, reconciliation.EffectCompleted},
		"partial": {func(s *SMBEvidence) { s.Ready, s.Partial = false, true }, reconciliation.EffectPartial},
		"absent":  {func(s *SMBEvidence) { s.Exists, s.Ready, s.Resource, s.Daemons = false, false, "", []SMBDaemon{} }, reconciliation.EffectNoEffect},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: smbReady(t, e, r, test.change)}}
			got, err := NewSMB(runner).Observe(context.Background(), e)
			if err != nil || got.Effect != test.want || string(got.Evidence) != string(runner.result.Evidence) {
				t.Fatalf("observe = %+v (%v), want %s", got, diagnostics.Of(err), test.want)
			}
			call := runner.request
			if call.Operation != "observe" || call.Deadline != deadline || call.Implementation != SMBImplementation || call.Variable != "bootwright_ceph_smb" || !call.NativeErrors || call.Placement.Machine != "controller" {
				t.Fatalf("the observation invocation is %+v", call)
			}
		})
	}
	for name, result := range map[string]lifecycle.RunResult{
		"another request": {Evidence: smbReady(t, e, r, func(s *SMBEvidence) { s.Request = strings.Repeat("0", 64) })},
		"another owner":   {Evidence: smbReady(t, e, r, func(s *SMBEvidence) { s.Owner = "apply-other" })},
		"another share":   {Evidence: smbReady(t, e, r, func(s *SMBEvidence) { s.Resource = "ceph.smb.share.files.other" })},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := NewSMB(&recordingRunner{result: result}).Observe(context.Background(), e)
			refusedWith(t, err, "lifecycle.unknown", "Ceph SMB evidence does not prove the frozen ownership and resource scope", "")
			if got.Effect != reconciliation.EffectUnknown || len(got.Evidence) != 0 {
				t.Fatalf("tampered observation = %+v", got)
			}
		})
	}
	produced := smbProduced()
	got, err := NewSMB(&recordingRunner{result: lifecycle.RunResult{Evidence: smbReady(t, e, r, nil), Produced: produced}}).Observe(context.Background(), e)
	refusedWith(t, err, "lifecycle.unknown", "Ceph SMB returned a value it never produces", "")
	if got.Effect != reconciliation.EffectUnknown || len(got.Produced) != 0 {
		t.Fatalf("an observation with produced values = %+v", got)
	}
	producedCleared(t, produced)
}

func TestSMBObserveLendsNoBoundSecret(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin())
	base := smbBase(t, catalog)
	want := []lifecycle.MaterialFile{{Name: "fleet-key", Secret: "fleet-key", Part: secrets.PrivateKeyPart, Variable: "sshKey"}, {Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"}}
	for _, name := range []string{"smb-users", "smb-join"} {
		t.Run(name, func(t *testing.T) {
			r := builtSMB(t, catalog, base, name)
			e := smbExecution(t, r, "pass\nword")
			runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: smbReady(t, e, r, nil)}}
			got, err := NewSMB(runner).Observe(context.Background(), e)
			if err != nil || got.Effect != reconciliation.EffectCompleted || runner.calls != 1 {
				t.Fatalf("observe = %+v (%v), calls %d", got, diagnostics.Of(err), runner.calls)
			}
			if !slices.Equal(runner.request.Materials, want) {
				t.Fatalf("the observation lends %+v, want %+v", runner.request.Materials, want)
			}
		})
	}
}

func TestSMBServiceClusteredFollowsClusteringAndCount(t *testing.T) {
	for name, test := range map[string]struct {
		placement api.Value
		native    []any
		want      bool
	}{
		"never":                 {requestMap("label", "mon", "count", 2), []any{"clustering", "never"}, false},
		"default with count 1":  {requestMap("label", "mon", "count", 1), nil, false},
		"default with count 2":  {requestMap("label", "mon", "count", 2), nil, true},
		"default without count": {requestMap("label", "mon"), nil, true},
		"always with count 1":   {requestMap("label", "mon", "count", 1), []any{"clustering", "always"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbFiles(test.placement, test.native...), smbDocs())
			base := sambaBase(t, catalog)
			for _, object := range []string{"smb-files", "smb-docs"} {
				canonical, err := builtSMB(t, catalog, base, object).Canonical()
				if err != nil {
					t.Fatal(diagnostics.Of(err))
				}
				decoded, err := DecodeSMBRequest(canonical)
				if err != nil {
					t.Fatal(diagnostics.Of(err))
				}
				if decoded.Service == nil || decoded.Service.Clustered != test.want {
					t.Fatalf("%s froze service %+v, want clustered %t", object, decoded.Service, test.want)
				}
			}
		})
	}
}

func TestSMBPrepareRefusesAnotherMachineSet(t *testing.T) {
	r, _ := smbShareRun(t)
	for name, machines := range map[string][]string{"a dropped node": {"node-1", "node-2"}, "an added node": {"node-1", "node-2", "node-3", "node-4"}} {
		t.Run(name, func(t *testing.T) {
			changed := r
			changed.Machines = machines
			runner := &recordingRunner{}
			got, err := NewSMB(runner).Apply(context.Background(), smbExecution(t, changed, "pw"))
			refusedWith(t, err, "lifecycle.state", "StorageSMBResource smb-docs froze another Machine set than the creation proof of StorageCluster example", "restore the original apply evidence")
			if got.Outcome != reconciliation.OutcomeFailed || runner.calls != 0 {
				t.Fatalf("apply = %+v, calls %d", got, runner.calls)
			}
		})
	}
}

func TestSMBKnownHostsHoldEveryProvedHostKey(t *testing.T) {
	r, e := smbShareRun(t)
	key, err := trust.ParseAuthorizedKey(string(hostPublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, h := range sambaCore().Hosts {
		want += key.Line(h.Address, h.Port) + "\n"
	}
	runner := &capturingRunner{}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: smbReady(t, e, r, nil)}
	if _, err := NewSMB(runner).Apply(context.Background(), e); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if string(runner.known) != want || strings.Count(want, " ssh-ed25519 ") != 3 {
		t.Fatalf("known hosts %q, want %q", runner.known, want)
	}
	for name, test := range map[string]struct {
		key     []byte
		message string
	}{
		"an unbound key": {nil, "a required Ceph credential component is not bound"},
		"an invalid key": {[]byte("not-a-key"), "the delivered Ceph host public key is invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			e := smbExecution(t, r, "pw")
			if test.key == nil {
				delete(e.Material, "node-2-host-key")
			} else {
				e.Material["node-2-host-key"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: test.key})
			}
			runner := &capturingRunner{}
			got, err := NewSMB(runner).Apply(context.Background(), e)
			refusedWith(t, err, "lifecycle.state", test.message, "")
			if runner.calls != 0 || got.Outcome == reconciliation.OutcomeChanged {
				t.Fatalf("apply = %+v, calls %d", got, runner.calls)
			}
		})
	}
}
