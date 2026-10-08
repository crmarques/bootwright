package lifecycle_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const (
	installedCluster = "sno"
	adminAccess      = "apiVersion: v1\nkind: Config\n# agent-install-admin-canary\n"
	installedRelease = "4.21.15"
	installIdentity  = "d21b91799c1b2d06e949151732b30343fc42b122f785c10685fad98570b00a02"
)

// installedRequest is the frozen request of one single-node agent
// installation whose node is a libvirt machine, placed on the controller.
func installedRequest() agentinstall.InstallRequest {
	return agentinstall.InstallRequest{
		Budgets:   agentinstall.InstallBudgets{BootSeconds: 900, BootstrapSeconds: 1800, InstallSeconds: 3600},
		Endpoints: []agentinstall.Endpoint{{Address: "192.0.2.20", Name: "api.sno.lab.test"}},
		Identity:  agentinstall.Identity{Block: agentinstall.InstallBlockID(installedCluster), Cluster: installedCluster, Context: lifecycle.JourneyContext},
		Image:     agentinstall.Publication{Path: "/var/lib/bootwright/artifacts/lab/sno", URL: "https://192.0.2.1:8443/lab/sno"},
		Nodes: []agentinstall.Node{{
			Address: "192.0.2.20", Machine: "sno-01", Name: "sno-01", Substrate: "libvirt",
			Controller: agentinstall.Controller{CredentialsRef: "lab-bmc-credentials", Endpoint: "https://192.0.2.1:8000/redfish/v1/Systems/sno-01"},
		}},
		Placement: machineref.Placement{Connection: machineref.ConnectionLocal, Machine: "controller"},
		Release:   agentinstall.Release{Distribution: "openshift", Version: installedRelease},
		Tool:      agentinstall.Tool{Compatibility: "openshift", Kind: "openshift-install", Version: installedRelease},
		Version:   "cluster-install-agent-v3",
		WorkRoot:  "/var/lib/bootwright/work/lab/sno",
	}
}

// installer is the installation adapter double: what each run returns from
// the cluster it reads. completed is the cluster answering as this build, its
// installation finished at the declared release, with no media inserted;
// otherwise its one node still presents the image this cluster published, as
// an installation stopped during boot leaves it. Every apply, and every apply
// observation that reads the installation complete, hands over the
// administrator kubeconfig, as the adapter writes it.
type installer struct {
	mutex     sync.Mutex
	completed bool
	// atRemoval records what custody held when the removal's own run began.
	atRemoval func()
	calls     []string
}

func (i *installer) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	i.mutex.Lock()
	call := run.Operation
	if run.Operation == "observe" && len(run.Outputs) == 0 {
		call = "observe-removal"
	}
	i.calls = append(i.calls, call)
	completed, atRemoval := i.completed, i.atRemoval
	i.mutex.Unlock()
	evidence := agentinstall.InstallEvidence{
		Cluster: installIdentity, Completed: true, Identity: installIdentity, Media: []string{}, Missing: []string{},
		OwnMedia: []string{}, Postcondition: true, Powered: []string{"sno-01"}, Release: installedRelease, Request: run.Digest,
	}
	if !completed {
		evidence.Cluster, evidence.Completed, evidence.Postcondition = "", false, false
		evidence.Media, evidence.OwnMedia = []string{"sno-01"}, []string{"sno-01"}
	}
	var produced []lifecycle.Produced
	switch run.Operation {
	case "destroy":
		if atRemoval != nil {
			atRemoval()
		}
		evidence = agentinstall.InstallEvidence{Absent: true, Media: []string{}, Missing: []string{}, OwnMedia: []string{}, Postcondition: true, Powered: []string{}, Request: run.Digest}
		return lifecycle.RunResult{Outcome: "changed", Evidence: installEvidenceOf(evidence)}, nil
	case "apply":
		produced = adminKubeconfig()
		return lifecycle.RunResult{Outcome: "changed", Evidence: installEvidenceOf(evidence), Produced: produced}, nil
	}
	if len(run.Outputs) != 0 {
		produced = adminKubeconfig()
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: installEvidenceOf(evidence), Produced: produced}, nil
}

func (i *installer) took() []string {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	calls := i.calls
	i.calls = nil
	return calls
}

func adminKubeconfig() []lifecycle.Produced {
	return []lifecycle.Produced{{Name: agentinstall.KubeconfigOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(adminAccess)})}}
}

func installEvidenceOf(evidence agentinstall.InstallEvidence) json.RawMessage {
	data, _ := json.Marshal(evidence)
	return data
}

// retainedInstaller is the installation capability over a controller stage
// that already published the release's installer and clients, so each run
// locates them as the stage's own block would answer.
type retainedInstaller struct{ agentinstall.InstallCapability }

func located(execution lifecycle.Execution) lifecycle.Execution {
	execution.LocateTool = func(_ context.Context, tool controller.InstalledTool) (string, error) {
		return "/var/lib/bootwright/controller/clients/tools/" + tool.Kind + "/" + tool.Version + "/" + tool.Executable, nil
	}
	return execution
}

func (c retainedInstaller) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.InstallCapability.Apply(ctx, located(execution))
}

func (c retainedInstaller) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.InstallCapability.Destroy(ctx, located(execution))
}

func (c retainedInstaller) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.InstallCapability.Observe(ctx, located(execution))
}

func (c retainedInstaller) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.InstallCapability.ObserveRemoval(ctx, located(execution))
}

// failedPublicationJourney plans the one installation, whose apply the
// adapter completes and hands its administrator kubeconfig while custody
// refuses to publish it, so the block is unknown and custody holds nothing
// before any journey begins.
func failedPublicationJourney(t *testing.T) (*lifecycle.CapabilityJourney, *installer) {
	t.Helper()
	request := installedRequest()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	adapter := &installer{completed: true}
	journey := lifecycle.NewCapabilityJourney(t, retainedInstaller{agentinstall.NewInstall(adapter)},
		lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
		lifecycle.CapabilityPlan{
			Definitions: []reconciliation.BlockDefinition{{
				ID: request.Identity.Block, Description: "install the cluster sno", Stage: reconciliation.StageClusters,
				Kind: agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.InstallImplementation,
				ContentDigest: strings.Repeat("c", 64), Request: canonical,
				Groups: []reconciliation.Group{{ID: "boot-machines", Description: "boot each node", Machines: []string{"sno-01"}}},
			}},
			Secrets: request.SecretReferences(),
		})
	refusal := secretstore.Failure("store.conflict", "secret publication was not committed")
	journey.FailPublication(refusal)
	_, err = journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if got := diagnostics.Of(err); len(got) == 0 || got[0].Code != "secret.store.conflict" {
		t.Fatalf("the apply = %+v (%v), want custody's refusal", got, err)
	}
	if calls := adapter.took(); !slices.Equal(calls, []string{"apply"}) || len(journey.Kept()) != 0 {
		t.Fatalf("the apply ran %v and custody holds %v", calls, journey.Kept())
	}
	journey.FailPublication(nil)
	return journey, adapter
}

// An agent installation whose apply completed while custody refused its
// administrator kubeconfig is unknown with that access held nowhere else. A
// destroy that supersedes it first reads the installation by the apply's own
// observation (D123): one that proves it complete captures the kubeconfig the
// observation hands over exactly as a completed apply does, before the
// removal's inverse ejects anything, and the completed removal then withdraws
// it. An installation the observation does not prove complete is resolved by
// the removal's own check (D119), which captures nothing.
func TestAnAgentInstallWhosePublicationFailedIsCapturedBeforeItsRemoval(t *testing.T) {
	block := agentinstall.InstallBlockID(installedCluster)
	access := []string{block + "/" + agentinstall.KubeconfigOutput + "=" + adminAccess}
	for _, tc := range []struct {
		name      string
		completed bool
		calls     []string
		held      []string
		journal   []string
	}{
		{"its observation proves the installation complete", true, []string{"observe", "destroy"}, access, []string{"produce " + block, "withdraw"}},
		{"its observation reads the installation part way", false, []string{"observe", "observe-removal", "destroy"}, []string{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journey, adapter := failedPublicationJourney(t)
			held := []string{}
			adapter.mutex.Lock()
			adapter.completed = tc.completed
			adapter.atRemoval = func() { held = journey.Kept() }
			adapter.mutex.Unlock()
			result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
			if err != nil || result.Receipt.Verb != "destroy" || result.Receipt.State != "done" {
				t.Fatalf("the removal = %+v, %+v (%v)", result, diagnostics.Of(err), err)
			}
			if calls := adapter.took(); !slices.Equal(calls, tc.calls) {
				t.Fatalf("the removal ran %v, want %v", calls, tc.calls)
			}
			if !slices.Equal(held, tc.held) {
				t.Fatalf("custody held %v when the removal's inverse ran, want %v", held, tc.held)
			}
			if journal := journey.CustodyJournal(); !slices.Equal(journal, tc.journal) || len(journey.Kept()) != 0 {
				t.Fatalf("custody committed %v and keeps %v, want %v and nothing", journal, journey.Kept(), tc.journal)
			}
		})
	}
}
