package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

func plannedBlocks(t *testing.T) map[string]reconciliation.Block {
	t.Helper()
	blocks := map[string]reconciliation.Block{}
	for _, block := range plannedCephPlan(t, childOf(api.StorageRBDImage, "disk", "poolRef", "volumes", "sizeBytes", 10737418240)).Blocks {
		blocks[block.ID] = block
	}
	return blocks
}

func plannedCephPlan(t *testing.T, extra ...api.Object) reconciliation.Plan {
	t.Helper()
	return plannedCephPlanWith(t, nil, extra...)
}

func plannedCephPlanWith(t *testing.T, change func(api.Value) api.Value, extra ...api.Object) reconciliation.Plan {
	t.Helper()
	catalog := api.NewCatalog(append(pinnedCatalog(t, change).Objects(), extra...))
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	runner := &recordingRunner{}
	var definitions []reconciliation.BlockDefinition
	for _, capability := range []lifecycle.Capability{NewImage(runner), NewHost(runner), New(api.StorageCluster, runner), New(api.StoragePool, runner), New(api.StorageClient, runner), New(api.StorageRBDImage, runner), New(api.StorageFilesystem, runner), New(api.StorageSubvolume, runner)} {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		definitions = append(definitions, plan.Definitions...)
	}
	for _, kind := range []string{"Machine", "MachineInstallProfile"} {
		for _, name := range []string{"node-1", "node-2", "node-3", "rhel"} {
			definitions = append(definitions, reconciliation.BlockDefinition{ID: strings.ToLower(kind) + "-" + name, Description: "synthetic " + name, Stage: reconciliation.StageMachines, Kind: kind, Object: name, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if runner.calls != 0 {
		t.Fatal("planning reached the runner")
	}
	return plan
}

func TestPlanGoldenCarriesHostBlocksTheirDependenciesAndExclusivity(t *testing.T) {
	blocks := plannedBlocks(t)
	type row struct {
		Dependencies   []string `json:"dependencies"`
		Exclusive      []string `json:"exclusive"`
		ID             string   `json:"id"`
		Implementation string   `json:"implementation"`
		Stage          string   `json:"stage"`
	}
	rows := []row{}
	for _, id := range slices.Sorted(func(yield func(string) bool) {
		for id := range blocks {
			if strings.HasPrefix(id, "ceph-") && !yield(id) {
				return
			}
		}
	}) {
		block := blocks[id]
		rows = append(rows, row{Dependencies: block.Dependencies, Exclusive: append([]string{}, block.Exclusive...), ID: id, Implementation: block.Implementation, Stage: string(block.Stage)})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "plan-blocks", data, false)
	hosts := []string{}
	for _, machineName := range []string{"node-1", "node-2", "node-3"} {
		id := HostBlockID("example", machineName)
		hosts = append(hosts, id)
		block, ok := blocks[id]
		if !ok || block.Implementation != HostImplementation || block.Stage != reconciliation.StageClusters || !slices.Equal(block.Exclusive, []string{"ceph-host/" + machineName}) {
			t.Fatalf("host block %s: %+v", id, block)
		}
		for _, want := range []string{ImageBlockID("example"), "machine-" + machineName, "machineinstallprofile-rhel"} {
			if !slices.Contains(block.Dependencies, want) {
				t.Fatalf("%s does not depend on %s: %v", id, want, block.Dependencies)
			}
		}
		for _, other := range block.Dependencies {
			if strings.HasPrefix(other, "ceph-host-") || other == "ceph-cluster-example" {
				t.Fatalf("%s is serialized behind %s, so the hosts cannot run concurrently", id, other)
			}
		}
	}
	for _, id := range append([]string{ImageBlockID("example")}, hosts...) {
		if !slices.Contains(blocks["ceph-cluster-example"].Dependencies, id) {
			t.Fatalf("the cluster block does not depend on %s: %v", id, blocks["ceph-cluster-example"].Dependencies)
		}
	}
	request, err := DecodeRequest(blocks["ceph-cluster-example"].Request)
	if err != nil || request.Packages != nil {
		t.Fatalf("the cluster block still carries the package transaction: %+v %v", request.Packages, err)
	}
}

func TestHostPlanSecretsAreItsOwnHostsOnly(t *testing.T) {
	catalog := pinnedCatalog(t, nil)
	plan, err := NewHost(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(plan.Definitions) != 3 || slices.Contains(plan.Secrets, "cluster-key") || !slices.Contains(plan.Secrets, "registry") {
		t.Fatalf("host secrets %v over %d blocks", plan.Secrets, len(plan.Definitions))
	}
	for _, definition := range plan.Definitions {
		r, err := DecodeHostRequest(definition.Request)
		if err != nil || r.Cluster.Owner != "" || r.Cluster.FSID != "" || r.Cluster.Image != PinnedRepository+":"+PinnedTag || len(definition.Consumes) != 0 {
			t.Fatalf("host request %+v %v", r, err)
		}
	}
}

func TestDeadlinesFitTheRunCeilingForThreeAndTheLargestAdmittedCluster(t *testing.T) {
	if hostDeadline("apply") != 8365*time.Second || hostDeadline("apply") > lifecycle.MaxDeadline {
		t.Fatalf("host block deadline %s", hostDeadline("apply"))
	}
	if hostDeadline("observe") > lifecycle.MaxDeadline || hostDeadline("destroy") > lifecycle.MaxDeadline {
		t.Fatal("a host read exceeds the run ceiling")
	}
	r := packageRequest(t)
	r.ClusterSSHTransport = PinnedTransport
	three, err := cephDeadline(r, "apply")
	if err != nil || three > lifecycle.MaxDeadline || three != time.Duration((16+3)*660+1860+2*900+60)*time.Second {
		t.Fatalf("three-host cluster deadline %s: %v", three, err)
	}
	largest := len(r.Hosts)
	for {
		grown := r
		grown.Hosts = slices.Repeat(r.Hosts[:1], largest+1)
		if _, err := cephDeadline(grown, "apply"); err != nil {
			break
		}
		largest++
	}
	maximum := r
	maximum.Hosts = slices.Repeat(r.Hosts[:1], largest)
	if deadline, err := cephDeadline(maximum, "apply"); err != nil || deadline > lifecycle.MaxDeadline || largest != 6 {
		t.Fatalf("the largest admitted cluster (%d hosts) takes %s: %v", largest, deadline, err)
	}
	for _, operation := range []string{"observe", "destroy"} {
		if deadline, err := cephDeadline(maximum, operation); err != nil || deadline > lifecycle.MaxDeadline {
			t.Fatalf("%s %s: %v", operation, deadline, err)
		}
	}
}

// The cluster deadline counts the budgets the cluster worker holds its own
// calls to, so each one is read from ceph_core.py rather than restated.
func TestTheClusterDeadlineCountsTheWorkersOwnBudgets(t *testing.T) {
	const path = "collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py"
	source := string(ansible.Assets()[path])
	for _, line := range []string{fmt.Sprintf("BOOTSTRAP_BUDGET = %d", bootstrapCall-60), "BOOTSTRAP_CALL = BOOTSTRAP_BUDGET + 60",
		fmt.Sprintf("READINESS_TIMEOUT = %d", readinessWait)} {
		if !strings.Contains(source, "\n"+line+"\n") {
			t.Fatalf("%s does not declare %q", path, line)
		}
	}
}

type capturingRunner struct {
	recordingRunner
	known, registry []byte
}

func (r *capturingRunner) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	known, _ := request.Material[knownHostsMaterial].Part(secrets.ValuePart)
	registry, _ := request.Material[registryMaterial].Part(secrets.ValuePart)
	r.known, r.registry = bytes.Clone(known), bytes.Clone(registry)
	return r.recordingRunner.Run(ctx, request)
}

type hostProofs struct {
	tree PackageTree
	disk bool
}

func (p hostProofs) DataDisk(reconciliation.Block, []byte, string, string, int64, bool) bool {
	return p.disk
}
func (hostProofs) HostKey([]lifecycle.BlockEvidence, string, string) (trust.HostKey, error) {
	return trust.HostKey{}, failure("lifecycle.state", "no installation proof in this test", "")
}
func (p hostProofs) PackageTree([]lifecycle.BlockEvidence, string) (PackageTree, error) {
	return p.tree, nil
}

func hostExecution(t *testing.T, host string) (lifecycle.Execution, HostRequest, hostProofs) {
	t.Helper()
	cluster := packageRequest(t)
	cluster.ClusterSSHTransport = PinnedTransport
	for i := range cluster.Hosts {
		cluster.Hosts[i].HostKeyRef = cluster.Hosts[i].Name + "-host-key"
	}
	r := HostRequest{Version: HostVersion, Cluster: scopeOf(cluster), Host: host}
	canonical, err := r.canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: HostBlockID("example", host), Kind: string(api.StorageCluster), Object: "example", Implementation: HostImplementation, ContentDigest: HostContentDigest(), Request: canonical}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := r.target()
	proofs := []lifecycle.BlockEvidence{imageProof(t, "example", cluster.Image, resolvedDigest),
		{Kind: "Machine", Object: target.Machine, Implementation: "synthetic", Verb: reconciliation.Apply, State: reconciliation.BlockDone,
			Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "Machine", Object: target.Machine, Implementation: "synthetic"}}}}
	e := withMaterial(lifecycle.Execution{ApplyOperation: "apply-original", Operation: "apply-original", Context: "example", ApplyProved: proofs,
		Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}}, cluster)
	e.Material["serving-tls"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("public-certificate"), secrets.PrivateKeyPart: []byte("private-tls-key")})
	source := cluster.Packages.Sources[0]
	return e, r, hostProofs{tree: PackageTree{Context: "synthetic", Media: source.Media, MediaSHA256: strings.Repeat("a", 64), MediaSize: 123456, Path: source.PublicationPath}, disk: true}
}

func preparedEvidence(t *testing.T, e lifecycle.Execution, r HostRequest, change func(*HostEvidence)) json.RawMessage {
	t.Helper()
	target, _ := r.target()
	evidence := HostEvidence{Version: HostEvidenceVersion, Request: e.Block.RequestDigest, Host: r.Host, Owner: e.ApplyOperation, FSID: FSIDFor(e.ApplyOperation, "example"),
		Image: PinnedRepository + "@" + resolvedDigest, Ready: true, ImageID: strings.Repeat("c", 64), UID: 167, GID: 167, Label: SharedLabel,
		PolicySHA256: strings.Repeat("f", 64), CephadmSHA256: PinnedCephadmSHA256, PlanDigest: strings.Repeat("a", 64), Route: true}
	for _, device := range target.Devices {
		evidence.Devices = append(evidence.Devices, HostDevice{Path: device.Path, Serial: device.Serial, DeviceID: "QEMU_QEMU_HARDDISK_" + device.Serial})
	}
	if change != nil {
		change(&evidence)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHostApplyLendsOnlyItsHostsMaterialAndTheRegistryAsAProtectedFile(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-2")
	runner := &capturingRunner{recordingRunner: recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: preparedEvidence(t, e, r, nil)}}}
	result, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.calls != 1 {
		t.Fatalf("host apply %+v %v", result, diagnostics.Of(err))
	}
	sent := runner.request
	if !sent.NativeErrors {
		t.Fatal("Ceph invocation did not opt in to typed native errors")
	}
	if sent.Implementation != HostImplementation || sent.Operation != "apply" || sent.Variable != "bootwright_ceph_host" || sent.Deadline != 8365*time.Second ||
		sent.Placement != (machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"}) {
		t.Fatalf("host run %+v", sent)
	}
	variables := []string{}
	for _, file := range sent.Materials {
		variables = append(variables, file.Variable)
		if file.Secret == r.Cluster.ClusterKeyRef {
			t.Fatal("the host block was lent the cluster key")
		}
	}
	index, _ := r.source()
	if !slices.Equal(variables, []string{"sshKey", "knownHosts", packageCertificateVariable(index), "registryJSON"}) {
		t.Fatalf("host material %v", variables)
	}
	target, _ := r.target()
	if bytes.Count(runner.known, []byte("\n")) != 1 || !bytes.HasPrefix(runner.known, []byte(target.Address+" ")) {
		t.Fatalf("the host block trusts more than its own host: %q", runner.known)
	}
	if !bytes.Contains(runner.registry, []byte("registry-password")) {
		t.Fatal("the registry credential was not lent for the pull")
	}
	if bytes.Contains(sent.Canonical, []byte("registry-password")) || bytes.Contains(result.Evidence, []byte("registry-password")) {
		t.Fatal("the registry credential reached the request or the evidence")
	}
	loaned, _ := sent.Material[registryMaterial].Part(secrets.ValuePart)
	if bytes.ContainsFunc(loaned, func(r rune) bool { return r != 0 }) {
		t.Fatal("the registry loan outlived the run")
	}
	decoded, err := DecodeHostRequest(sent.Canonical)
	if err != nil || decoded.Cluster.Image != PinnedRepository+"@"+resolvedDigest || decoded.Cluster.Owner != "apply-original" || decoded.Cluster.Packages.Sources[0].MediaSHA256 == "" {
		t.Fatalf("the run request %+v %v", decoded, err)
	}
}

func TestHostApplyRefusesWithoutItsDependenciesEvidence(t *testing.T) {
	for name, damage := range map[string]func(*lifecycle.Execution, *hostProofs){
		"image":   func(e *lifecycle.Execution, _ *hostProofs) { e.ApplyProved = e.ApplyProved[1:] },
		"disk":    func(_ *lifecycle.Execution, p *hostProofs) { p.disk = false },
		"tree":    func(_ *lifecycle.Execution, p *hostProofs) { p.tree.Path += "/other" },
		"owner":   func(e *lifecycle.Execution, _ *hostProofs) { e.ApplyOperation = "" },
		"another": func(e *lifecycle.Execution, _ *hostProofs) { e.Block.Object = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			e, _, proofs := hostExecution(t, "node-1")
			damage(&e, &proofs)
			runner := &recordingRunner{}
			if result, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e); err == nil || runner.calls != 0 || result.Outcome != reconciliation.OutcomeFailed {
				t.Fatalf("host apply without evidence ran: %+v %v", result, err)
			}
		})
	}
}

func TestHostEvidenceEffects(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-3")
	for name, test := range map[string]struct {
		change func(*HostEvidence)
		want   reconciliation.EffectState
	}{
		"prepared": {nil, reconciliation.EffectCompleted},
		"absent": {func(v *HostEvidence) {
			*v = HostEvidence{Version: v.Version, Request: v.Request, Host: v.Host, Owner: v.Owner, FSID: v.FSID, Image: v.Image}
		}, reconciliation.EffectNoEffect},
		"packages only": {func(v *HostEvidence) {
			*v = HostEvidence{Version: v.Version, Request: v.Request, Host: v.Host, Owner: v.Owner, FSID: v.FSID, Image: v.Image, PlanDigest: v.PlanDigest}
		}, reconciliation.EffectPartial},
		"no route":          {func(v *HostEvidence) { v.Ready, v.Route = false, false }, reconciliation.EffectPartial},
		"another owner":     {func(v *HostEvidence) { v.Owner = "apply-other" }, reconciliation.EffectUnknown},
		"another image":     {func(v *HostEvidence) { v.Image = PinnedRepository + "@sha256:" + strings.Repeat("d", 64) }, reconciliation.EffectUnknown},
		"another host":      {func(v *HostEvidence) { v.Host = "node-1" }, reconciliation.EffectUnknown},
		"ready but unbound": {func(v *HostEvidence) { v.CephadmSHA256 = "" }, reconciliation.EffectUnknown},
		"foreign label":     {func(v *HostEvidence) { v.Label = "system_u:object_r:user_tmp_t:s0" }, reconciliation.EffectUnknown},
		"guessed device":    {func(v *HostEvidence) { v.Devices[0].Serial = "guessed" }, reconciliation.EffectUnknown},
		"removed":           {func(v *HostEvidence) { v.Removed = true }, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: preparedEvidence(t, e, r, test.change)}}
			observation, err := NewHost(runner).WithProofs(proofs).Observe(context.Background(), e)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observed %q, want %q: %v", observation.Effect, test.want, err)
			}
			if len(runner.request.Materials) != 2 || runner.request.Deadline != 720*time.Second {
				t.Fatalf("observation lent %+v within %s", runner.request.Materials, runner.request.Deadline)
			}
		})
	}
}

func TestHostReleaseMakesNoEffectAndItsRemovalContactsNothing(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-1")
	released := preparedEvidence(t, e, r, func(v *HostEvidence) {
		*v = HostEvidence{Version: v.Version, Request: v.Request, Host: v.Host, Owner: v.Owner, FSID: v.FSID, Image: v.Image, Removed: true, Devices: []HostDevice{}}
	})
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: released}}
	result, err := NewHost(runner).WithProofs(proofs).Destroy(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.request.Operation != "destroy" || len(runner.request.Materials) != 0 {
		t.Fatalf("release %+v %v %+v", result, err, runner.request.Materials)
	}
	runner.result.Outcome = "changed"
	if _, err := NewHost(runner).WithProofs(proofs).Destroy(context.Background(), e); err == nil {
		t.Fatal("a release that reported an effect was accepted")
	}
	calls := runner.calls
	observation, err := NewHost(runner).ObserveRemoval(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || runner.calls != calls {
		t.Fatalf("removal observation %+v %v", observation, err)
	}
	removal, err := NewHost(nil).Removal(context.Background(), e.Block)
	if err != nil || len(removal.Consumes) != 0 {
		t.Fatalf("host release consumes %v: %v", removal.Consumes, err)
	}
	if q, err := (HostCapability{}).Quiescent(context.Background(), lifecycle.Probe{}); err != nil || q.State != lifecycle.Quiescent {
		t.Fatalf("host quiescence %+v %v", q, err)
	}
}

func hostProof(t *testing.T, host string, change func(*HostEvidence)) lifecycle.BlockEvidence {
	t.Helper()
	e, r, _ := hostExecution(t, host)
	return lifecycle.BlockEvidence{Kind: string(api.StorageCluster), Object: "example", Implementation: HostImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone,
		Evidence: preparedEvidence(t, e, r, change), Block: e.Block}
}

func TestProvedHostReadsOnlyTheOriginalPreparationOfThatMachine(t *testing.T) {
	image := PinnedRepository + "@" + resolvedDigest
	proofs := []lifecycle.BlockEvidence{hostProof(t, "node-1", nil), hostProof(t, "node-2", nil)}
	if evidence, err := ProvedHost(proofs, "example", "node-2", image); err != nil || evidence.Host != "node-2" {
		t.Fatalf("proved host %+v %v", evidence, err)
	}
	for name, test := range map[string]struct {
		proofs  []lifecycle.BlockEvidence
		machine string
		image   string
	}{
		"missing":       {proofs[:1], "node-2", image},
		"ambiguous":     {append(slices.Clone(proofs), proofs[1]), "node-2", image},
		"another image": {proofs, "node-2", PinnedRepository + "@sha256:" + strings.Repeat("d", 64)},
		"unprepared":    {[]lifecycle.BlockEvidence{hostProof(t, "node-2", func(v *HostEvidence) { v.Route = false })}, "node-2", image},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ProvedHost(test.proofs, "example", test.machine, test.image); err == nil {
				t.Fatal("an unproved host preparation was accepted")
			}
		})
	}
}

func TestHostReleaseImpactNamesTheRetainedRuntimeAndPackageRecords(t *testing.T) {
	catalog := pinnedCatalog(t, nil)
	input := lifecycle.PlanInput{Verb: reconciliation.Destroy, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := NewHost(&recordingRunner{}).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(plan.Definitions) == 0 {
		t.Fatal("no host release was planned")
	}
	for _, definition := range plan.Definitions {
		if len(definition.Impacts) != 1 || !strings.HasPrefix(definition.Impacts[0], "release-host ") {
			t.Fatalf("%s impacts %q", definition.ID, definition.Impacts)
		}
		for _, want := range []string{"packages", "image", "host runtime and package records retained"} {
			if !strings.Contains(definition.Impacts[0], want) {
				t.Fatalf("%s impact %q does not name %q", definition.ID, definition.Impacts[0], want)
			}
		}
	}
}

func jsonNames(t reflect.Type) []string {
	names := []string{}
	for field := range t.Fields() {
		names = append(names, strings.Split(field.Tag.Get("json"), ",")[0])
	}
	slices.Sort(names)
	return names
}

func reachableTypes(t reflect.Type, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		reachableTypes(t.Elem(), seen)
	case reflect.Map:
		reachableTypes(t.Key(), seen)
		reachableTypes(t.Elem(), seen)
	case reflect.Struct:
		for field := range t.Fields() {
			reachableTypes(field.Type, seen)
		}
	}
}

func withoutMembers(t *testing.T, data []byte, drop ...string) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range drop {
		delete(fields, name)
	}
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestHostRequestCarriesNoResourcePayload(t *testing.T) {
	blocks := plannedBlocks(t)
	for _, machineName := range []string{"node-1", "node-2", "node-3"} {
		block := blocks[HostBlockID("example", machineName)]
		var request, cluster map[string]json.RawMessage
		if err := json.Unmarshal(block.Request, &request); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(request["cluster"], &cluster); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"action", "pool", "client"} {
			if _, ok := cluster[name]; ok {
				t.Fatalf("%s: the host request carries the core request's %s", block.ID, name)
			}
		}
		if string(cluster["version"]) != `"`+Version+`"` || string(request["version"]) != `"`+HostVersion+`"` {
			t.Fatalf("%s: versions %s %s", block.ID, cluster["version"], request["version"])
		}
		if machineName == "node-1" {
			matchesGolden(t, "request-host", block.Request, false)
		}
	}
	for _, name := range jsonNames(reflect.TypeFor[ClusterScope]()) {
		if slices.Contains([]string{"action", "pool", "client"}, name) {
			t.Fatalf("ClusterScope has the resource member %s", name)
		}
	}
}

func TestRequestSchemaChangeLeavesHostDigest(t *testing.T) {
	seen := map[reflect.Type]bool{}
	reachableTypes(reflect.TypeFor[HostRequest](), seen)
	for _, forbidden := range []reflect.Type{reflect.TypeFor[Request](), reflect.TypeFor[Pool](), reflect.TypeFor[Client]()} {
		if seen[forbidden] {
			t.Fatalf("the host request reaches %s, so its schema moves the host contract", forbidden)
		}
	}
	want := []string{"ansiblePackageVersion", "auxiliary", "bootstrap", "callHome", "cephadm", "cluster", "clusterKeyRef", "clusterNetworks", "clusterSSHTransport", "clusterSSHUser",
		"config", "controller", "fsid", "hosts", "image", "monIP", "nfsGaneshaPackageVersion", "owner", "packageRepository", "packageRepositoryKey", "packages", "publicNetworks", "registry",
		"registryRef", "registryTrustBundleRef", "release", "security", "services", "version"}
	slices.Sort(want)
	if got := jsonNames(reflect.TypeFor[ClusterScope]()); !slices.Equal(got, want) {
		t.Fatalf("ClusterScope members %v, want %v", got, want)
	}
	r := packageRequest(t)
	r.Owner, r.FSID = "apply-original", FSIDFor("apply-original", r.Cluster)
	r.Config = map[string]map[string]string{"global": {"osd_pool_default_size": "2"}}
	r.Cephadm = Tool{Path: "/usr/sbin/cephadm", PackageVersion: "synthetic-cephadm-1"}
	r.Release, r.AnsiblePackageVersion = "synthetic-release", "synthetic-ansible-1"
	r.Auxiliary = []AuxiliaryReference{{Name: "haproxy", Image: PinnedRegistry + "/cp/ibm-ceph/haproxy-synthetic@sha256:" + strings.Repeat("d", 64)}}
	r.PackageRepository, r.NFSGaneshaPackageVersion = "https://publisher.example.test/ceph", "synthetic-ganesha-1"
	r.MonIP, r.ClusterSSHUser, r.CallHome = "192.0.2.1", "operator", "enabled"
	r.RegistryTrustBundleRef = "registry-ca"
	r.Security = &Security{FIPS: true, CephXKeyType: "aes"}
	r.PublicNetworks, r.ClusterNetworks = []string{"192.0.2.0/24"}, []string{"198.51.100.0/24"}
	r.Action, r.Pool, r.Client = "pool", &Pool{Name: "volumes"}, &Client{Name: "consumer", Pool: "volumes", Access: "read-only"}
	scope := scopeOf(r)
	for field, value := range reflect.ValueOf(scope).Fields() {
		if value.IsZero() {
			t.Fatalf("the fixture leaves ClusterScope.%s unset", field.Name)
		}
	}
	scoped, err := reconciliation.Freeze(scope, "Ceph scope")
	if err != nil {
		t.Fatal(err)
	}
	full, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := withoutMembers(t, scoped), withoutMembers(t, full, "action", "pool", "client", "delegated"); got != want {
		t.Fatalf("scopeOf lost a member:\n%s\n%s", got, want)
	}
}

func TestHostApplyLendsThePackageRepositoryTrustBundle(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-2")
	r.Cluster.Packages.PackageRepositoryTrustBundleRef = "repository-ca"
	canonical, err := r.canonical()
	if err != nil {
		t.Fatal(err)
	}
	e.Block.Request = canonical
	if e.Block.RequestDigest, err = reconciliation.RequestDigest(e.Block.BlockDefinition); err != nil {
		t.Fatal(err)
	}
	runner := &capturingRunner{recordingRunner: recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: preparedEvidence(t, e, r, nil)}}}
	if _, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e); err == nil || runner.calls != 0 {
		t.Fatalf("host preparation ran without its bound repository trust bundle: %v", err)
	}
	e.Material["repository-ca"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(gatewayCertificatePEM)})
	if _, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e); err != nil || runner.calls != 1 {
		t.Fatalf("host apply %v", diagnostics.Of(err))
	}
	index, _ := r.source()
	variables := []string{}
	var bundle lifecycle.MaterialFile
	for _, file := range runner.request.Materials {
		variables = append(variables, file.Variable)
		if file.Variable == "packageRepositoryCA" {
			bundle = file
		}
	}
	if !slices.Equal(variables, []string{"sshKey", "knownHosts", packageCertificateVariable(index), "packageRepositoryCA", "registryJSON"}) ||
		bundle != (lifecycle.MaterialFile{Name: "package-repository-ca", Secret: "repository-ca", Part: secrets.CertificatePart, Variable: "packageRepositoryCA"}) {
		t.Fatalf("host material %+v", runner.request.Materials)
	}
	observer := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: preparedEvidence(t, e, r, nil)}}
	if _, err := NewHost(observer).WithProofs(proofs).Observe(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	for _, file := range observer.request.Materials {
		if file.Secret == "repository-ca" {
			t.Fatalf("observation lent the repository bundle %+v", observer.request.Materials)
		}
	}
}

func TestHostRequestCarriesTheRegistryTrustBundle(t *testing.T) {
	catalog := withRegistryTrust(pinnedCatalog(t, nil))
	plan, err := NewHost(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil || len(plan.Definitions) != 3 || !slices.Contains(plan.Secrets, "registry-ca") {
		t.Fatalf("host plan %v %v", plan.Secrets, diagnostics.Of(err))
	}
	for _, definition := range plan.Definitions {
		if r, err := DecodeHostRequest(definition.Request); err != nil || r.Cluster.RegistryTrustBundleRef != "registry-ca" {
			t.Fatalf("host request %q %v", r.Cluster.RegistryTrustBundleRef, err)
		}
	}
	e, r, proofs := hostExecution(t, "node-2")
	r.Cluster.RegistryTrustBundleRef = "registry-ca"
	canonical, err := r.canonical()
	if err != nil {
		t.Fatal(err)
	}
	e.Block.Request = canonical
	if e.Block.RequestDigest, err = reconciliation.RequestDigest(e.Block.BlockDefinition); err != nil {
		t.Fatal(err)
	}
	runner := &capturingRunner{recordingRunner: recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: preparedEvidence(t, e, r, nil)}}}
	if _, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e); err == nil || runner.calls != 0 {
		t.Fatalf("host preparation ran without its bound trust bundle: %v", err)
	}
	e.Material["registry-ca"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(gatewayCertificatePEM)})
	if _, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e); err != nil || runner.calls != 1 {
		t.Fatalf("host apply %v", diagnostics.Of(err))
	}
	index, _ := r.source()
	variables := []string{}
	for _, file := range runner.request.Materials {
		variables = append(variables, file.Variable)
	}
	last := runner.request.Materials[len(runner.request.Materials)-1]
	if !slices.Equal(variables, []string{"sshKey", "knownHosts", packageCertificateVariable(index), "registryJSON", "registryCA"}) || last.Secret != "registry-ca" || last.Part != secrets.CertificatePart {
		t.Fatalf("host material %+v", runner.request.Materials)
	}
}

func (hostProofs) FIPS([]lifecycle.BlockEvidence, string, string) error { return nil }

func (hostProofs) PhysicalMachine(reconciliation.Block, []byte) (PhysicalIdentity, bool, error) {
	return PhysicalIdentity{}, false, failure("lifecycle.state", "physical proof unavailable", "")
}
