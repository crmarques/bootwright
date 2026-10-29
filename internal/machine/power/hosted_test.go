package power

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// token stands in for the minted ServiceAccount kubeconfig; no output,
// record or request may ever carry it.
const token = "apiVersion: v1\nusers:\n- user:\n    token: minted-sa-token-0123456789"

// kubevirtCatalog holds two KubeVirt providers on one namespace: ext reaches
// an operator's host through a declared kubeconfig, and kv a host cluster
// this context builds. node-0 and node-1 are hosted, vm-0 is external.
func kubevirtCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{
		object(api.Environment, "lab", m("controller", m("machineRef", "controller"))),
		object(api.Machine, "controller", m("os", m("provided", true), "access", m("local", true))),
		object(api.NetworkConfig, "guests", m("nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet", "state", "up"))))),
		object(api.InfraProvider, "ext", m("kubevirt", m("kubeconfigRef", "ext-kubeconfig", "namespace", "guests"))),
		object(api.InfraProvider, "kv", m("kubevirt", m("hostClusterRef", "host", "namespace", "nested"))),
		object(api.Machine, "node-0", m("substrate", m("providerRef", "kv"), "os", m("provided", false), "network", m("configRef", "guests"))),
		object(api.Machine, "node-1", m("substrate", m("providerRef", "kv"), "os", m("provided", false), "network", m("configRef", "guests"))),
		object(api.Machine, "vm-0", m("substrate", m("providerRef", "ext"), "os", m("provided", false), "network", m("configRef", "guests"))),
	})
}

func kubevirtRealized() evidenceSource {
	done := machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockDone}
	return evidenceSource{states: map[string]machine.OwnershipState{"node-0": done, "node-1": done, "vm-0": done}}
}

// vmPin is the identity a KubeVirt machine block records: the host's
// kube-system UID and the VirtualMachine's UID.
var vmPin = machine.HardwareIdentity{UUID: "9a1f2c3d-0000-4000-8000-00000000c1a5", Serial: "6b2e4f80-0000-4000-8000-0000000000aa"}

// hostLender mints the token-only kubeconfig a hosted run is lent, recording each
// lend, or fails as a host that cannot be reached.
type hostLender struct {
	lends []HostLend
	lent  []secrets.Material
	err   error
}

func (l *hostLender) LendHostAccess(_ context.Context, lend HostLend) (secrets.Material, error) {
	l.lends = append(l.lends, lend)
	if l.err != nil {
		return secrets.Material{}, l.err
	}
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(token)})
	l.lent = append(l.lent, material)
	return material, nil
}

func (l *hostLender) cleared(t *testing.T) {
	t.Helper()
	for _, material := range l.lent {
		if value, _ := material.Part(secrets.ValuePart); !bytes.Equal(value, make([]byte, len(token))) {
			t.Fatal("a lent token outlived the run it was lent to")
		}
	}
}

// hostedAdapter answers a power or reading run as the adapter would, holding
// the bytes each run was lent while it runs, and refuses as a port that finds
// another cluster or VirtualMachine when refuse is set.
type hostedAdapter struct {
	runs    []lifecycle.RunRequest
	user    []string
	power   map[string]string
	refuse  bool
	request []byte
}

func (a *hostedAdapter) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.runs = append(a.runs, request)
	for _, file := range request.Materials {
		if strings.HasPrefix(file.Variable, "controllerUser") {
			value, _ := request.Material[file.Secret].Part(file.Part)
			a.user = append(a.user, string(value))
		}
	}
	a.request = append(a.request, request.Canonical...)
	if a.refuse {
		return lifecycle.RunResult{}, request.Refusals[identityMismatch]
	}
	if request.Implementation == ReadImplementation {
		var survey ReadSurvey
		if err := json.Unmarshal(request.Canonical, &survey); err != nil {
			return lifecycle.RunResult{}, err
		}
		var readings []Reading
		for _, target := range survey.Targets {
			readings = append(readings, Reading{Machine: target.Object, Power: a.power[target.Object]})
		}
		evidence, err := json.Marshal(ReadEvidence{Machines: readings, Request: request.Digest})
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}, err
	}
	var frozen Request
	if err := json.Unmarshal(request.Canonical, &frozen); err != nil {
		return lifecycle.RunResult{}, err
	}
	evidence, err := json.Marshal(Evidence{Machine: frozen.Identity.Object, Postcondition: true, Power: expected(frozen.Verb), Previous: "on", Request: request.Digest, Changed: true})
	return lifecycle.RunResult{Outcome: "changed", Evidence: evidence}, err
}

func hostedService(runtime Runtime, runner Runner, access HostAccess) Service {
	return New(stripped{catalog: kubevirtCatalog()}, kubevirtRealized(), &pins{identity: vmPin, found: true}, runtime, runner, nil, &answer{}, nil, nil).WithHostAccess(access)
}

// Every power verb on a Machine of a host cluster this context builds is lent
// one token minted for that run alone, bound to its request digest and sized
// to its deadline, reaches the port with the Machine's pin, and the token is
// cleared when the run returns; no Secret declaration is bound, the run holds
// no material but the lent kubeconfig, and neither the frozen request nor the
// result carries the token.
func TestEveryPowerVerbOnAHostedMachineIsLentOneTokenItClears(t *testing.T) {
	for name, request := range map[string]PowerRequest{
		"start":        {Verb: Start},
		"stop":         {Verb: Stop},
		"restart":      {Verb: Restart},
		"stop --force": {Verb: Stop, Force: true},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, runner, access := &boundary{}, &hostedAdapter{}, &hostLender{}
			request.ContextName, request.Names, request.SkipConfirmation = "lab", []string{"node-0"}, true
			result, err := hostedService(runtime, runner, access).converge(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(access.lends) != 1 || len(runner.runs) != 1 {
				t.Fatalf("lends %d, runs %d, want one of each", len(access.lends), len(runner.runs))
			}
			lend, run := access.lends[0], runner.runs[0]
			if lend.Cluster != "host" || lend.Namespace != "nested" || lend.Machine != "node-0" || lend.Digest != run.Digest ||
				lend.Deadline != 21*time.Minute || run.Deadline != lend.Deadline || lend.Placement.Machine != "controller" {
				t.Fatalf("lend = %+v for run deadline %s", lend, run.Deadline)
			}
			if len(runner.user) != 1 || runner.user[0] != token {
				t.Fatal("the power run was not lent the minted kubeconfig as its controller user")
			}
			if run.MaterialValues["pinnedUUIDBase64"] == "" || run.MaterialValues["pinnedSerialBase64"] == "" || run.Refusals[identityMismatch] == nil {
				t.Fatalf("the hosted run carries no pin: %+v", run.MaterialValues)
			}
			var frozen Request
			if err := json.Unmarshal(run.Canonical, &frozen); err != nil || frozen.Controller.HostCluster != "host" ||
				frozen.Controller.CredentialsRef != "" || frozen.Force != request.Force {
				t.Fatalf("frozen request = %s (%v)", run.Canonical, err)
			}
			if len(run.Material) != 1 || len(runtime.requested) != 0 {
				t.Fatalf("a hosted run bound declared Secrets %q", runtime.requested)
			}
			access.cleared(t)
			encoded, _ := json.Marshal(result)
			for _, carried := range [][]byte{runner.request, encoded} {
				if bytes.Contains(carried, []byte("minted-sa-token")) {
					t.Fatalf("the token reached %s", carried)
				}
			}
		})
	}
}

// A port that finds another cluster behind the minted access, or a
// VirtualMachine other than the recorded one, refuses every verb before any
// power request, naming the provider's access as what to correct, and the
// token is still cleared.
func TestASwappedClusterOrForeignVirtualMachineRefusesEveryHostedVerb(t *testing.T) {
	for _, verb := range []string{Start, Stop, Restart} {
		for _, force := range []bool{false, true} {
			if verb == Start && force {
				continue
			}
			runner, access := &hostedAdapter{refuse: true}, &hostLender{}
			_, err := hostedService(&boundary{}, runner, access).converge(context.Background(),
				PowerRequest{ContextName: "lab", Names: []string{"node-0"}, Verb: verb, Force: force, SkipConfirmation: true})
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "lifecycle.state" || !strings.Contains(found[0].Remediation, "hostClusterRef") ||
				strings.Contains(found[0].Remediation, "bmc.address") || found[0].Object == nil || found[0].Object.Name != "node-0" {
				t.Fatalf("%s (force %t) refused %+v (%v)", verb, force, found, err)
			}
			access.cleared(t)
		}
	}
}

// A host whose lend fails leaves the power run unsent and fails the command;
// an executable that binds no hostLender refuses before anything is confirmed.
func TestAHostedPowerRunWaitsOnItsLend(t *testing.T) {
	unreachable := errors.New("lending the access of host cluster host failed")
	runner := &hostedAdapter{}
	_, err := hostedService(&boundary{}, runner, &hostLender{err: unreachable}).Stop(context.Background(),
		PowerRequest{ContextName: "lab", Names: []string{"node-0"}, SkipConfirmation: true})
	if !errors.Is(err, unreachable) || len(runner.runs) != 0 {
		t.Fatalf("an unlent run reported %v after %d runs", err, len(runner.runs))
	}
	confirmer := &answer{}
	service := New(stripped{catalog: kubevirtCatalog()}, kubevirtRealized(), &pins{identity: vmPin, found: true}, &boundary{}, runner, nil, confirmer, nil, nil)
	_, err = service.Stop(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"node-0"}})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "machine.power" || !strings.Contains(found[0].Message, "OpenShiftCluster/host") ||
		len(confirmer.asked) != 0 || len(runner.runs) != 0 {
		t.Fatalf("an unbound hostLender refused %+v after asking %q", found, confirmer.asked)
	}
}

// The reading lends one token per host namespace and reads each hosted
// Machine through it with its pin; an external KubeVirt Machine reads with
// its own pin and declared kubeconfig; a host that lends nothing leaves its
// Machines unknown and denies no other reading.
func TestTheLiveReadReportsPinnedKubeVirtMachinesAndAnUnlentHostUnknown(t *testing.T) {
	names := []string{"node-0", "node-1", "vm-0"}
	runner, access := &hostedAdapter{power: map[string]string{"node-0": "on", "node-1": "off", "vm-0": "on"}}, &hostLender{}
	readings, err := hostedService(&boundary{}, runner, access).Read(context.Background(), "lab", names)
	if err != nil {
		t.Fatal(err)
	}
	if readings["node-0"] != "on" || readings["node-1"] != "off" || readings["vm-0"] != "on" || len(runner.runs) != 2 || len(access.lends) != 1 {
		t.Fatalf("readings %v over %d runs and %d lends", readings, len(runner.runs), len(access.lends))
	}
	if lend := access.lends[0]; lend.Cluster != "host" || lend.Namespace != "nested" || lend.Machine != "node-0" || lend.Deadline != 9*time.Minute || lend.Digest != runner.runs[1].Digest {
		t.Fatalf("lend = %+v", lend)
	}
	for _, run := range runner.runs {
		var survey ReadSurvey
		if err := json.Unmarshal(run.Canonical, &survey); err != nil {
			t.Fatal(err)
		}
		for index, target := range survey.Targets {
			if target.PinnedUUIDVariable == "" || run.MaterialValues[target.PinnedUUIDVariable] == "" || run.MaterialValues[target.PinnedSerialVariable] == "" {
				t.Fatalf("target %d of %s carries no pin: %+v", index, run.Canonical, run.MaterialValues)
			}
		}
	}
	if runner.user[0] == token || runner.user[1] != token || runner.user[2] != token {
		t.Fatal("a reading was lent another Machine's access")
	}
	access.cleared(t)

	runner, access = &hostedAdapter{power: map[string]string{"vm-0": "off"}}, &hostLender{err: errors.New("the host does not answer")}
	readings, err = hostedService(&boundary{}, runner, access).Read(context.Background(), "lab", names)
	if err != nil || readings["node-0"] != machine.PowerUnknown || readings["node-1"] != machine.PowerUnknown || readings["vm-0"] != "off" || len(runner.runs) != 1 {
		t.Fatalf("an unreachable host read %v (%v) over %d runs", readings, err, len(runner.runs))
	}
	failing := &failingHosted{hostedAdapter: hostedAdapter{power: map[string]string{"vm-0": "on"}}}
	readings, err = hostedService(&boundary{}, failing, &hostLender{}).Read(context.Background(), "lab", names)
	if err != nil || readings["node-0"] != machine.PowerUnknown || readings["vm-0"] != "on" {
		t.Fatalf("a hosted run that ended unanswered read %v (%v)", readings, err)
	}
}

// A port-pinned target reads with its own pin variables, and one whose block
// recorded no identity reads with none, so the port refuses it rather than
// read an unproved VirtualMachine; a pin that cannot be read refuses.
func TestASurveyCarriesEachPinnedTargetsOwnIdentity(t *testing.T) {
	pinned := func(name string) (machine.HardwareIdentity, error) {
		if name == "node-1" {
			return machine.HardwareIdentity{}, nil
		}
		return vmPin, nil
	}
	surveys, err := readSurveysFor(kubevirtCatalog(), "lab", []string{"node-0", "node-1", "vm-0"}, realizedAs(kubevirtRealized().states), pinned)
	if err != nil || len(surveys) != 2 {
		t.Fatalf("surveys = %+v (%v)", surveys, err)
	}
	hosted := surveys[1]
	if hosted.Targets[0].PinnedUUIDVariable != "pinnedUUIDBase640" || hosted.Targets[1].PinnedUUIDVariable != "" ||
		readValues(hosted)["pinnedSerialBase640"] == "" || len(readValues(hosted)) != 2 {
		t.Fatalf("hosted survey = %+v, values %v", hosted, readValues(hosted))
	}
	unread := errors.New("the evidence cannot be read")
	if _, err := readSurveysFor(kubevirtCatalog(), "lab", []string{"vm-0"}, realizedAs(kubevirtRealized().states),
		func(string) (machine.HardwareIdentity, error) { return machine.HardwareIdentity{}, unread }); !errors.Is(err, unread) {
		t.Fatalf("an unreadable pin reported %v", err)
	}
}

// The hosted power request and the pinned and hosted surveys are what the
// adapter validates, so their exact bytes are pinned.
func TestHostedPowerRequestsMatchTheirGoldens(t *testing.T) {
	request, _, err := requestFor(kubevirtCatalog(), "lab", "node-0", Restart, false, realizedAs(kubevirtRealized().states))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-power-hosted", canonical, false)
	readsBack[Request](t, "request-power-hosted")
	surveys, err := readSurveysFor(kubevirtCatalog(), "lab", []string{"node-0", "node-1", "vm-0"}, realizedAs(kubevirtRealized().states),
		func(string) (machine.HardwareIdentity, error) { return vmPin, nil })
	if err != nil || len(surveys) != 2 {
		t.Fatalf("surveys = %+v (%v)", surveys, err)
	}
	for index, name := range []string{"survey-read-pinned", "survey-read-hosted"} {
		canonical, err := surveys[index].Canonical()
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, name, canonical, false)
		readsBack[ReadSurvey](t, name)
	}
}

// failingHosted ends every hosted run without an answer, as a run past its
// deadline does, and answers every other run.
type failingHosted struct{ hostedAdapter }

func (f *failingHosted) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	if _, lent := request.Material[lentKubeconfig]; lent {
		return lifecycle.RunResult{}, context.DeadlineExceeded
	}
	return f.hostedAdapter.Run(ctx, request)
}
