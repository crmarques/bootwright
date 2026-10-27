//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const metalCompactNode = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: compact-01
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: lab-metal
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
  network:
    configRef: lab-guests
    attachmentRef: lab-metal
    installAddressRef: ip
    addresses:
      - name: fqdn
        address: compact-01.lab.example.test
      - name: ip
        address: 198.51.100.21/24
        interface: enp1s0
  hardware:
    nics:
      - name: enp1s0
        macAddress: 52:54:00:9a:1b:21
    management:
      bmc:
        address: https://bmc-01.example.test/redfish/v1/Systems/1
`

const metalProvider = `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-metal
spec:
  baremetal:
    boot:
      method: redfishVirtualMedia
    defaults:
      bmc:
        credentialsRef: lab-bmc-credentials
        tls:
          trustBundleRef: lab-bmc-ca
  networkAttachments:
    - name: lab-metal
      baremetal: {}
`

const metalTrust = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: lab-bmc-ca
spec:
  type: caBundle
  source:
    contextStore: {}
`

// metalCompactInput is lab-compact-df with one compact node on a bare-metal
// provider, which no destroy of the context deletes, and the platform its mixed
// providers need.
func metalCompactInput(t *testing.T, input string) {
	t.Helper()
	copySources(t, exampleDirectory(t, "lab-compact-df"), input)
	cluster := filepath.Join(input, "clusters", "compact", "cluster.yaml")
	data, err := os.ReadFile(cluster)
	if err != nil || strings.Count(string(data), "    mode: connected\n") != 1 {
		t.Fatalf("the compact cluster is not the declaration this variant edits (%v)", err)
	}
	platform := "    mode: connected\n\n    platform:\n      type: baremetal\n\n      baremetal:\n        provisioningNetwork: disabled\n"
	if err := os.WriteFile(cluster, []byte(strings.Replace(string(data), "    mode: connected\n", platform, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"infra/machines/compact-01.yaml":     metalCompactNode,
		"infra/providers/lab-metal.yaml":     metalProvider,
		"secret-descriptors/lab-bmc-ca.yaml": metalTrust,
	} {
		if err := os.WriteFile(filepath.Join(input, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// The B804 exit: a lab-compact-df whose compact cluster keeps a bare-metal node
// plans with the surviving-consumer warning, and its destroy, through this
// build's capabilities, refuses before registration naming the cluster, the
// export and the core cluster, and registers nothing.
func TestADestroyRefusesWhileAPhysicalCompactConsumerSurvives(t *testing.T) {
	parent := t.TempDir()
	input, root := filepath.Join(parent, "input"), filepath.Join(parent, "state")
	metalCompactInput(t, input)
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{input})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	state, _ := compileAcceptance(t, sources)
	applied, err := reconciliation.NewPlan(reconciliation.Apply, productionDefinitions(t, state, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	deps.Controller.Lock = testRuntimeLock
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository, Inputs: contexts.Inputs{Repository: repository, Selection: deps.Selection},
		Host: testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection, Setup: repository,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer,
		Capabilities: buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}),
	}
	services := assembleServices(deps)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	const pair = "ContainerCluster/compact consumes StorageExport/compact-storage"
	if _, stderr := contextRun(t, services, 0, "plan", "--context", "alpha"); !strings.Contains(stderr, "lifecycle.surviving-consumer") || !strings.Contains(humanContent(stderr), humanContent(pair+" and outlives")) {
		t.Fatalf("the apply plan warns %q", stderr)
	}
	appliedDone(t, repository, applied)
	if err := repository.MutateSetup(context.Background(), func(tx prerequisites.SetupTransaction) error {
		return tx.Record(context.Background(), prerequisites.SetupState{Format: prerequisites.SetupStateFormat, Receipt: &prerequisites.HostReceipt{
			Platform: prerequisites.ReceiptPlatform{ID: "fedora", VersionID: "44", Like: []string{}, Architecture: "amd64", Glibc: "2.43", Tested: true},
			Lock:     testRuntimeLock.Digest, Closure: strings.Repeat("b", 64), Packages: []prerequisites.ReceiptPackage{},
			Runtime: prerequisites.ReceiptRuntime{Digest: strings.Repeat("c", 64), Interpreter: "/usr/bin/python3.14", Python: "3.14.8", ABI: "3.14", AnsibleCore: testRuntimeLock.AnsibleCore},
		}})
	}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	before := stateFingerprint(t, root)
	_, stderr := contextRun(t, services, 1, "destroy", "--context", "alpha", "--authorize", reviewedDataLoss(t, services, "alpha"), "--yes")
	for _, want := range []string{"lifecycle.live", pair + " and survives this destroy, which takes back StorageExport/compact-storage,", "StorageCluster/ceph-lab"} {
		if !strings.Contains(humanContent(stderr), humanContent(want)) {
			t.Fatalf("the destroy refusal does not name %q:\n%s", want, stderr)
		}
	}
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("the refused destroy registered an operation or wrote state")
	}
}

// appliedDone records plan as a completed apply of the context's current input,
// every block done, under the evidence a completed apply publishes.
func appliedDone(t *testing.T, repository interface {
	MutateLifecycle(context.Context, string, func(lifecycle.Transaction) error) error
}, plan reconciliation.Plan) {
	t.Helper()
	evidence, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationDone)
	if err != nil {
		t.Fatal(err)
	}
	published, err := evidence.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	runs := make([]string, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		runs = append(runs, block.ID)
	}
	ctx := context.Background()
	if err := repository.MutateLifecycle(ctx, "alpha", func(tx lifecycle.Transaction) error {
		if err := tx.PublishEvidence(ctx, published); err != nil {
			return err
		}
		registerDoneUnder(ctx, t, operationstore.New(tx.Operations(), exitStamp), tx.Identity().Revision, inputDigestOf(tx.Inputs()),
			"op-"+strings.Repeat("01", 16), "", plan, runs...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
