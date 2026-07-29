package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

type safetyVerdict string

const (
	verdictUsageError safetyVerdict = "usage-error"
	verdictRefusal    safetyVerdict = "fail-closed refusal"
	verdictAccepted   safetyVerdict = "accepted (no gate refusal)"
	verdictOutOfSync  safetyVerdict = "read-only out-of-sync report"
	verdictAuthorized safetyVerdict = "authorized mutation (no gate refusal)"
	verdictPrompted   safetyVerdict = "held at the interactive confirmation"
)

var gateRefusalMarkers = []string{"refusing to", "refuses to", "fails closed", "does not authorize data loss"}

type safetyCase struct {
	name    string
	seed    func(t *testing.T, ctx workspace.Context)
	args    []string
	verdict safetyVerdict
	want    []string
	deny    []string
}

func TestApplyDestroySafetyMatrix(t *testing.T) {
	for _, tc := range safetyMatrixCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := initAdvancedBaselineContext(t)
			if tc.seed != nil {
				tc.seed(t, ctx)
			}
			stdout, stderr, code := runCLI(t, tc.args...)
			out := stdout + stderr
			switch tc.verdict {
			case verdictUsageError:
				if code != 2 {
					t.Fatalf("want usage error (exit 2), got exit %d\n%s", code, out)
				}
			case verdictRefusal:
				if code == 0 {
					t.Fatalf("want a fail-closed refusal, got exit 0\n%s", out)
				}
				if !strings.Contains(out, "bootwright ") {
					t.Errorf("a refusal must name the exact command to proceed intentionally; got:\n%s", out)
				}
			case verdictAccepted:
				if code != 0 {
					t.Fatalf("want the run accepted past the safety gates (exit 0), got exit %d\n%s", code, out)
				}
			case verdictOutOfSync:
				if code != 3 {
					t.Fatalf("want the read-only out-of-sync exit code 3, got exit %d\n%s", code, out)
				}
				assertNoRuntimeRecords(t, ctx)
			case verdictPrompted:
				if code == 0 {
					t.Fatalf("want the run held at an interactive confirmation, got exit 0\n%s", out)
				}
			case verdictAuthorized:
				for _, marker := range gateRefusalMarkers {
					if strings.Contains(out, marker) {
						t.Fatalf("run is authorized by its records and flags, but a safety gate refused it (%q):\n%s", marker, out)
					}
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output must contain %q; got:\n%s", want, out)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(out, deny) {
					t.Errorf("output must not contain %q; got:\n%s", deny, out)
				}
			}
		})
	}
}

func safetyMatrixCases() []safetyCase {
	return append(append(append(
		safetyFlagCoherenceCases(),
		safetyAuthorizationTokenCases()...),
		safetyScopeClosureCases()...),
		safetyStartingStateCases()...)
}

func safetyFlagCoherenceCases() []safetyCase {
	return []safetyCase{{
		name:    "apply/retired --expect-new is an unknown flag/greenfield/full",
		args:    []string{"apply", "--expect-new", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "apply/retired --converge-drifted is an unknown flag/greenfield/full",
		args:    []string{"apply", "--converge-drifted", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "apply/retired --confirm-data-loss is an unknown flag/greenfield/full",
		args:    []string{"apply", "--confirm-data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "destroy/retired --force is an unknown flag/any/full",
		args:    []string{"destroy", "--force", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "destroy/retired --include-unowned is an unknown flag/any/full",
		args:    []string{"destroy", "--include-unowned", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "destroy/retired --skip-unreachable is an unknown flag/any/full",
		args:    []string{"destroy", "--skip-unreachable", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "apply/retired --mode value/greenfield/full",
		args:    []string{"apply", "--mode", "override", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--mode must be one of", "create", "reconcile", "rebuild"},
	}, {
		name:    "apply/unknown --authorize token/greenfield/full",
		args:    []string{"apply", "--authorize", "force", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"is not an authorization token", "data-loss", "shared-infra"},
	}, {
		name:    "destroy/unknown --authorize token in a comma list/any/full",
		args:    []string{"destroy", "--authorize", "data-loss,everything", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"is not an authorization token"},
	}, {
		name:    "apply/machines+clusters/greenfield/mixed",
		args:    []string{"apply", "--machines", "dc1-metal-master-0", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"mutually exclusive"},
	}, {
		name:    "apply/reclaim-devices outside deps/greenfield/stage add-ons",
		args:    []string{"apply", "--stage", "add-ons", "--reclaim-devices", "/dev/sdb", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"deps phase", "--stage deps"},
	}, {
		name:    "apply/rebuild skipping the device gate/greenfield/stage base",
		args:    []string{"apply", "--mode", "rebuild", "--stage", "base", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--mode rebuild --through base"},
	}, {
		name:    "destroy/sub-phase stage/any/fabric",
		args:    []string{"destroy", "--stage", "fabric", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"apply-only"},
	}, {
		name:    "destroy/purge-history with the artifact-server literal/any/infra",
		args:    []string{"destroy", "--stage", "infra", "--clusters", "artifact-server", "--purge-history", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--purge-history"},
	}, {
		name:    "apply/json without dry-run/greenfield/full",
		args:    []string{"apply", "--output", "json", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--dry-run"},
	}}
}

func safetyAuthorizationTokenCases() []safetyCase {
	return []safetyCase{{
		name:    "destroy/data-loss: --yes alone no longer authorizes an OSD zap",
		args:    []string{"destroy", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage", "--yes does not authorize data loss", "--authorize data-loss"},
	}, {
		name:    "destroy/data-loss: the interactive data-loss prompt still stands without --yes",
		args:    []string{"destroy", "--ask-become-pass=false"},
		verdict: verdictPrompted,
		want:    []string{"Confirm this DESTRUCTIVE action (accept data loss)?"},
		deny:    []string{"--yes does not authorize data loss"},
	}, {
		name:    "destroy/data-loss: the token clears the data-loss refusal and the prompt",
		args:    []string{"destroy", "--authorize", "data-loss", "--ask-become-pass=false"},
		verdict: verdictPrompted,
		want:    []string{"Continue with destroy?"},
		deny:    []string{"--yes does not authorize data loss", "Confirm this DESTRUCTIVE action"},
	}, {
		name:    "apply/data-loss: an unused token is a warning, never an authorization",
		args:    []string{"apply", "--stage", "clusters", "--clusters", "ceph-storage", "--authorize", "data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize data-loss had no effect"},
		deny:    []string{"will DESTROY data"},
	}, {
		name:    "destroy/protected: a protected Environment refuses and names its token",
		seed:    seedProtectedEnvironment,
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"destroyProtection=protected", "--authorize protected"},
	}, {
		name:    "destroy/protected: data-loss does not stand in for protected",
		seed:    seedProtectedEnvironment,
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--authorize", "data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize protected"},
	}, {
		name: "destroy/installed-cluster-node: a node of an installed cluster refuses and names its token",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp", "--authorize installed-cluster-node"},
	}, {
		name:    "destroy/unreadable-records: a corrupt ownership record refuses and names its token",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"destroy", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"could not be read", "--authorize unreadable-records"},
	}, {
		name:    "destroy/shared-infra: a storage consumer conflict refuses and names its token",
		args:    []string{"destroy", "--stage", "infra", "--clusters", "ceph-storage", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage", "--authorize shared-infra"},
	}, {
		name:    "destroy/unowned-vms: the token is inert outside the machine layer and says so",
		args:    []string{"destroy", "--stage", "clusters", "--authorize", "unowned-vms", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize unowned-vms had no effect"},
	}, {
		name:    "destroy/unowned-networks: authorizing VMs never authorizes networks",
		args:    []string{"destroy", "--stage", "infra", "--authorize", "unowned-vms", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_authorize_unowned_vms=true"},
		deny:    []string{"bootwright_destroy_authorize_unowned_networks=true"},
	}, {
		name:    "destroy/unreachable-nodes: the token alone arms the skip, with no second flag",
		args:    []string{"destroy", "--stage", "infra", "--authorize", "unreachable-nodes", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_skip_unreachable=true"},
	}}
}

func safetyScopeClosureCases() []safetyCase {
	return []safetyCase{{
		name:    "apply/dry-run/greenfield/full graph is a read-only preview",
		args:    []string{"apply", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		deny:    []string{"DESTROY"},
	}, {
		name:    "plan/greenfield/one DC previews without mutating",
		args:    []string{"plan", "--clusters", "dc1-metal-ocp,dc1-child-ocp", "--ask-become-pass=false"},
		verdict: verdictAccepted,
	}, {
		name:    "diff/recorded/greenfield/full reports never-applied and writes no records",
		args:    []string{"diff", "--recorded"},
		verdict: verdictOutOfSync,
		want:    []string{"ceph-storage", "never applied"},
	}, {
		name:    "destroy/dry-run/greenfield/one DC leaves the stretched Ceph out of the work set",
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp,dc1-child-ocp", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_storage_scope="},
		deny:    []string{"bootwright_destroy_storage_scope=ceph-storage"},
	}, {
		name:    "destroy/stretched Ceph alone while both DCs still consume it",
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "ceph-storage", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage", "dc1-metal-ocp", "dc2-metal-ocp"},
	}, {
		name: "destroy/host cluster while its nested guest is installed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-child-ocp", "no --authorize token widens"},
	}, {
		name: "destroy/machines of a host cluster while its nested guest is installed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--authorize", "installed-cluster-node", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-child-ocp"},
	}}
}

func safetyStartingStateCases() []safetyCase {
	return []safetyCase{{
		name: "apply/mode create over an existing install record",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--mode", "create", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp"},
	}, {
		name: "apply/cluster-wide substrate release authorizes and discloses the rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			if err := workflow.MarkSubstrateReleased(ctx.RunsDir, "dc1-child-ocp", time.Now()); err != nil {
				t.Fatalf("MarkSubstrateReleased: %v", err)
			}
		},
		args:    []string{"apply", "--stage", "infra", "--clusters", "dc1-child-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"destroyed substrate", "dc1-child-ocp", "re-creates the released machines"},
	}, {
		name: "apply/machine-granular substrate release names only the released machine",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			if err := workflow.MarkSubstrateMachinesReleased(ctx.RunsDir, "dc1-child-ocp", []string{"dc1-child-ocp-infra-master-0"}, time.Now()); err != nil {
				t.Fatalf("MarkSubstrateMachinesReleased: %v", err)
			}
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"destroyed substrate", "machine(s) dc1-child-ocp-infra-master-0"},
		deny:    []string{"dc1-child-ocp-infra-master-1"},
	}, {
		name: "apply/no release leaves an unreleased machine unauthorized for rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		deny:    []string{"destroyed substrate"},
	}, {
		name: "apply/renamed ContainerCluster orphaning a provisioned one",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp-old", "the signature of a rename"},
	}, {
		name: "apply/renamed StorageCluster orphaning an owned Ceph cluster",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedStorageOwnership(t, ctx, "ceph-storage-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "ceph-storage", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage-old", "orphan the old Ceph cluster"},
	}}
}

func initAdvancedBaselineContext(t *testing.T) workspace.Context {
	t.Helper()
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	if stdout, stderr, code := runCLI(t, "context", "init", "--name", "matrix", "-f", example); code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("matrix")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func assertNoRuntimeRecords(t *testing.T, ctx workspace.Context) {
	t.Helper()
	if workflow.HasConvergeSafetyRecords(ctx.RunsDir) {
		t.Error("a read-only command must not write convergence-safety records")
	}
	for _, dir := range []string{"substrate-release", "current"} {
		if _, err := os.Stat(filepath.Join(ctx.RunsDir, dir)); err == nil {
			t.Errorf("a read-only command must not write runs/%s", dir)
		}
	}
}

func seedInstalledCluster(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	now := time.Now().UTC()
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, workflow.ClusterInstallRecord{
		Cluster:     cluster,
		DesiredHash: "sha256:seeded",
		HashSchema:  workflow.ConvergeHashSchema,
		Status:      workflow.ClusterInstallStatusInstalled,
		Phase:       workflow.ClusterInstallPhaseComplete,
		UpdatedAt:   now,
		InstalledAt: &now,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord(%s): %v", cluster, err)
	}
	kubeconfig := filepath.Join(ctx.ClustersDir, cluster, "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
}

func seedKubeVirtReadyHost(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	seedInstalledCluster(t, ctx, cluster)
	if err := extensionrecords.SaveRecord(ctx.ClustersDir, extensionrecords.Record{
		Cluster:   cluster,
		Extension: "openshift-virtualization",
		Status:    extensionrecords.RecordStatusReady,
		Phase:     extensionrecords.RecordPhaseComplete,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRecord(%s): %v", cluster, err)
	}
}

func seedStorageOwnership(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	if err := ownership.SaveResource(ctx.OwnershipDir, ownership.ResourceRecord{
		Kind:      string(ownership.KindStorageCluster),
		Name:      cluster,
		Owner:     ownership.Owner,
		Context:   ctx.Name,
		Cluster:   cluster,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveResource(%s): %v", cluster, err)
	}
}

func seedProtectedEnvironment(t *testing.T, ctx workspace.Context) {
	t.Helper()
	path := filepath.Join(ctx.InputDir, "environment.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read context environment: %v", err)
	}
	body := strings.Replace(string(data),
		"  domains:",
		"  safety:\n    destroyProtection: "+v1alpha1.EnvironmentDestroyProtectionProtected+"\n  domains:", 1)
	if body == string(data) {
		t.Fatal("context environment did not contain spec.domains")
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write protected environment: %v", err)
	}
}

func seedUnreadableOwnershipRecord(t *testing.T, ctx workspace.Context) {
	t.Helper()
	seedStorageOwnership(t, ctx, "ceph-storage")
	corrupt := filepath.Join(ctx.OwnershipDir, ownership.ResourceDirName, "corrupt.json")
	if err := os.MkdirAll(filepath.Dir(corrupt), 0o700); err != nil {
		t.Fatalf("mkdir ownership resources dir: %v", err)
	}
	if err := os.WriteFile(corrupt, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write corrupt ownership record: %v", err)
	}
}
