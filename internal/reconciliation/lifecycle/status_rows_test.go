package lifecycle

import (
	"context"
	"errors"
	"maps"
	"path"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const clusterImplementation = "agent-install-v1"

// rowsDefinitions are an artifact server and a cluster whose install waits on
// its boot media, which waits on the artifact server, so an apply realizes
// them in that order and a removal takes the install back first. The media
// and the install sit in two stages so a stage selection can stop between
// them; the agent installer stages both as clusters, and status reads blocks,
// not stages.
func rowsDefinitions() []reconciliation.BlockDefinition {
	return []reconciliation.BlockDefinition{
		definition("artifacts"),
		clusterBlock("media-sno", reconciliation.StageMachines, "artifacts"),
		clusterBlock("install-sno", reconciliation.StageClusters, "media-sno"),
	}
}

func clusterBlock(id string, stage reconciliation.Stage, dependency string) reconciliation.BlockDefinition {
	block := stagedDefinition(id, stage, dependency)
	block.Kind, block.Object, block.Implementation = string(api.ContainerCluster), "sno", clusterImplementation
	return block
}

// boundCapabilities resolves each binding to its own capability, in binding
// order, as the executable's resolver does.
type boundCapabilities struct {
	bindings     []CapabilityBinding
	capabilities []Capability
}

func (b boundCapabilities) Bindings() []CapabilityBinding { return b.bindings }

func (b boundCapabilities) Resolve(kind, implementation string) (Capability, bool) {
	for index, binding := range b.bindings {
		if binding == (CapabilityBinding{Kind: kind, Implementation: implementation}) {
			return b.capabilities[index], true
		}
	}
	return nil, false
}

// rowsHarness applies a selected input declaring a managed ArtifactServer for
// each artifact server block and the ContainerClusters sno and edge, which a
// capability claims, and reads status over the same input with a
// StorageCluster added, which none claims, and a managed ArtifactServer
// mirror, which no operation names, so that apply never refuses either.
type rowsHarness struct {
	*harness
	applied, declared testCompiler
}

func newRowsHarness(t *testing.T, definitions []reconciliation.BlockDefinition) *rowsHarness {
	t.Helper()
	h := newPlannedHarness(t, definitions)
	objects := []api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		api.NewObject(api.ContainerCluster, "sno", api.Value{}, api.MapValue()),
		api.NewObject(api.ContainerCluster, "edge", api.Value{}, api.MapValue()),
	}
	for _, block := range definitions {
		if block.Kind == string(api.ArtifactServer) {
			objects = append(objects, api.NewObject(api.ArtifactServer, block.Object, api.Value{}, api.MapValue(
				api.FieldValue{Name: "management", Value: api.StringValue("managed")},
				api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")},
			)))
		}
	}
	applied := api.NewCatalog(objects)
	declared := api.NewCatalog(append(objects,
		api.NewObject(api.StorageCluster, "ceph", api.Value{}, api.MapValue()),
		api.NewObject(api.ArtifactServer, "mirror", api.Value{}, api.MapValue(
			api.FieldValue{Name: "management", Value: api.StringValue("managed")},
			api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")},
		)),
	))
	r := &rowsHarness{
		harness: h,
		applied: testCompiler{state: compilation.NewState(applied, applied, nil)}, declared: testCompiler{state: compilation.NewState(declared, declared, nil)},
	}
	h.service.compiler = r.applied
	h.service.capabilities = boundCapabilities{
		bindings: []CapabilityBinding{
			{Kind: string(api.ArtifactServer), Implementation: "artifact-server-nginx-v1"},
			{Kind: string(api.ContainerCluster), Implementation: clusterImplementation},
		},
		capabilities: []Capability{h.capability, plannedCapability{Capability: h.capability}},
	}
	return r
}

// rows reads every cluster and shared service row status reports over the
// declared input while the capabilities refuse the objects named, and refuse
// the ContainerCluster edge and the ArtifactServer mirror whatever else they
// refuse.
func (r *rowsHarness) rows(t *testing.T, refused ...Refusal) map[string]RealizationStatus {
	t.Helper()
	r.service.compiler = r.declared
	r.capability.unsupported = append([]Refusal{
		{Kind: string(api.ContainerCluster), Name: "edge", Reason: "its nodes select an install profile"},
		{Kind: string(api.ArtifactServer), Name: "mirror", Reason: "its listener selects a client certificate"},
	}, refused...)
	defer func() { r.service.compiler, r.capability.unsupported = r.applied, nil }()
	status, err := r.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]RealizationStatus{}
	for _, cluster := range append(status.Clusters, status.StorageClusters...) {
		rows[cluster.Kind+"/"+cluster.Name] = cluster.Status
	}
	for _, service := range status.Shared {
		rows[service.Kind+"/"+service.Name] = service.Status
	}
	return rows
}

// Status reports each shared service and cluster by what the current
// operation's verb proved about every block of it. An apply reports an
// unproved block as unknown, then a failed one as failed, then one not yet
// started as pending, and done only once every block is. A removal reports
// what it took back as pending, its failed or unproved block as such, and an
// object whose removal has not started by what the apply it removes proved,
// so a removal that may have left an object half gone never reads done. A
// completed removal whose record of a block does not read done, which only a
// lost record leaves, reads that block's object unknown.
// An object an earlier removal released, or one the operation names, reads
// what the records prove even when its capability now refuses it. Nothing
// else changes what a declaration gives an object: a kind no capability claims
// and a cluster or managed service its capability refuses are unsupported,
// and the rest pending.
func TestStatusRowsReadTheOperationsVerb(t *testing.T) {
	ctx := context.Background()
	destroy := func(t *testing.T, h *rowsHarness, outcomes ...reconciliation.Outcome) {
		t.Helper()
		h.capability.outcomes = nil
		for _, outcome := range outcomes {
			h.capability.outcomes = append(h.capability.outcomes, Result{Outcome: outcome})
		}
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("a removal whose block did not complete reported success")
		}
		h.capability.outcomes = nil
	}
	refuse := func(kind api.Kind, name string) []Refusal {
		return []Refusal{{Kind: string(kind), Name: name, Reason: "its shape is outside the supported one"}}
	}
	failedArtifactsRemoval := func(t *testing.T, h *rowsHarness) {
		completeApply(t, h.harness)
		destroy(t, h, reconciliation.OutcomeChanged, reconciliation.OutcomeChanged, reconciliation.OutcomeFailed)
		requireReached(t, h.harness, reconciliation.OperationFailed, map[string]reconciliation.BlockState{
			"install-sno": reconciliation.BlockDone, "media-sno": reconciliation.BlockDone, "artifacts": reconciliation.BlockFailed,
		})
	}
	for _, row := range []struct {
		name      string
		arrange   func(*testing.T, *rowsHarness)
		refused   []Refusal
		artifacts RealizationStatus
		cluster   RealizationStatus
	}{
		{name: "no operation", arrange: func(*testing.T, *rowsHarness) {}, artifacts: RealizationPending, cluster: RealizationPending},
		{
			name:      "no operation over an artifact server its capability refuses",
			arrange:   func(*testing.T, *rowsHarness) {},
			refused:   refuse(api.ArtifactServer, "artifacts"),
			artifacts: RealizationUnsupported, cluster: RealizationPending,
		},
		{
			name:      "a completed apply",
			arrange:   func(t *testing.T, h *rowsHarness) { completeApply(t, h.harness) },
			artifacts: RealizationDone, cluster: RealizationDone,
		},
		{
			name:      "a completed apply whose cluster its capability now refuses",
			arrange:   func(t *testing.T, h *rowsHarness) { completeApply(t, h.harness) },
			refused:   refuse(api.ContainerCluster, "sno"),
			artifacts: RealizationDone, cluster: RealizationDone,
		},
		{
			name:      "a completed apply whose artifact server its capability now refuses",
			arrange:   func(t *testing.T, h *rowsHarness) { completeApply(t, h.harness) },
			refused:   refuse(api.ArtifactServer, "artifacts"),
			artifacts: RealizationDone, cluster: RealizationDone,
		},
		{
			name:      "a failed apply",
			arrange:   func(t *testing.T, h *rowsHarness) { failApply(t, h.harness, "artifacts") },
			artifacts: RealizationFailed, cluster: RealizationPending,
		},
		{
			name: "an apply holding a running block",
			arrange: func(t *testing.T, h *rowsHarness) {
				completeApply(t, h.harness)
				leaveExecutorDead(t, h.harness, "artifacts")
			},
			artifacts: RealizationUnknown, cluster: RealizationDone,
		},
		{
			name: "an unknown apply",
			arrange: func(t *testing.T, h *rowsHarness) {
				applyChained(t, h.harness, map[string]Result{"artifacts": {Outcome: reconciliation.OutcomeUnknown}})
			},
			artifacts: RealizationUnknown, cluster: RealizationPending,
		},
		{
			name: "an apply whose cluster media is done and install failed",
			arrange: func(t *testing.T, h *rowsHarness) {
				applyChained(t, h.harness, map[string]Result{"install-sno": {Outcome: reconciliation.OutcomeFailed}})
			},
			artifacts: RealizationDone, cluster: RealizationFailed,
		},
		{
			name: "an apply paused after the cluster media, before its install",
			arrange: func(t *testing.T, h *rowsHarness) {
				applyChained(t, h.harness, nil, string(reconciliation.StageInfraComponents), string(reconciliation.StageMachines))
				requireReached(t, h.harness, reconciliation.OperationPaused, map[string]reconciliation.BlockState{
					"artifacts": reconciliation.BlockDone, "media-sno": reconciliation.BlockDone, "install-sno": reconciliation.BlockPending,
				})
			},
			artifacts: RealizationDone, cluster: RealizationPending,
		},
		{
			name:      "a completed destroy",
			arrange:   func(t *testing.T, h *rowsHarness) { completeRemoval(t, h.harness) },
			artifacts: RealizationPending, cluster: RealizationPending,
		},
		{
			name: "a completed destroy whose artifact server's block record was lost",
			arrange: func(t *testing.T, h *rowsHarness) {
				completeRemoval(t, h.harness)
				lose(h.harness, path.Join(currentOperation(t, h.harness), "blocks", "artifacts")+"/")
				requireReached(t, h.harness, reconciliation.OperationDone, map[string]reconciliation.BlockState{
					"install-sno": reconciliation.BlockDone, "media-sno": reconciliation.BlockDone, "artifacts": reconciliation.BlockPending,
				})
			},
			artifacts: RealizationUnknown, cluster: RealizationPending,
		},
		{
			name: "a completed destroy whose cluster install's block record was lost",
			arrange: func(t *testing.T, h *rowsHarness) {
				completeRemoval(t, h.harness)
				lose(h.harness, path.Join(currentOperation(t, h.harness), "blocks", "install-sno")+"/")
			},
			artifacts: RealizationPending, cluster: RealizationUnknown,
		},
		{
			name:      "a failed destroy whose artifact server's removal failed",
			arrange:   failedArtifactsRemoval,
			artifacts: RealizationFailed, cluster: RealizationPending,
		},
		{
			name: "a failed destroy that stopped before the artifact server's removal started",
			arrange: func(t *testing.T, h *rowsHarness) {
				completeApply(t, h.harness)
				destroy(t, h, reconciliation.OutcomeFailed)
				requireReached(t, h.harness, reconciliation.OperationFailed, map[string]reconciliation.BlockState{
					"install-sno": reconciliation.BlockFailed, "media-sno": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
				})
			},
			artifacts: RealizationDone, cluster: RealizationFailed,
		},
		{
			name: "a destroy whose cluster install's removal lost its outcome",
			arrange: func(t *testing.T, h *rowsHarness) {
				completeApply(t, h.harness)
				destroy(t, h, reconciliation.OutcomeUnknown)
				requireReached(t, h.harness, reconciliation.OperationUnknown, map[string]reconciliation.BlockState{
					"install-sno": reconciliation.BlockUnknown, "media-sno": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
				})
			},
			artifacts: RealizationDone, cluster: RealizationUnknown,
		},
		{
			name: "a destroy killed while it removed the artifact server",
			arrange: func(t *testing.T, h *rowsHarness) {
				failedArtifactsRemoval(t, h)
				leaveExecutorDead(t, h.harness, "artifacts")
				requireReached(t, h.harness, reconciliation.OperationRunning, map[string]reconciliation.BlockState{
					"install-sno": reconciliation.BlockDone, "media-sno": reconciliation.BlockDone, "artifacts": reconciliation.BlockRunning,
				})
			},
			artifacts: RealizationUnknown, cluster: RealizationPending,
		},
		{
			name: "a destroy killed while it removed the cluster install",
			arrange: func(t *testing.T, h *rowsHarness) {
				completeApply(t, h.harness)
				destroy(t, h, reconciliation.OutcomeFailed)
				leaveExecutorDead(t, h.harness, "install-sno")
				requireReached(t, h.harness, reconciliation.OperationRunning, map[string]reconciliation.BlockState{
					"install-sno": reconciliation.BlockRunning, "media-sno": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
				})
			},
			artifacts: RealizationDone, cluster: RealizationUnknown,
		},
		{
			name: "a destroy over the cluster an earlier attempt released",
			arrange: func(t *testing.T, h *rowsHarness) {
				failedArtifactsRemoval(t, h)
				destroy(t, h, reconciliation.OutcomeFailed)
				status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
				if err != nil || status.Lifecycle == nil || len(status.Lifecycle.Blocks) != 1 || status.Lifecycle.Blocks[0].ID != "artifacts" {
					t.Fatalf("the superseding removal = %+v (%v), want one naming only the artifact server", status.Lifecycle, err)
				}
			},
			refused:   refuse(api.ContainerCluster, "sno"),
			artifacts: RealizationFailed, cluster: RealizationPending,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			h := newRowsHarness(t, rowsDefinitions())
			row.arrange(t, h)
			want := map[string]RealizationStatus{
				"ArtifactServer/artifacts": row.artifacts, "ContainerCluster/sno": row.cluster,
				"ContainerCluster/edge": RealizationUnsupported, "StorageCluster/ceph": RealizationUnsupported,
				"ArtifactServer/mirror": RealizationUnsupported,
			}
			if got := h.rows(t, row.refused...); !maps.Equal(got, want) {
				t.Fatalf("rows = %v, want %v", got, want)
			}
		})
	}
}

// A removal that replaced a failed one never reads done an object the earlier
// attempt took part of back or may have failed on. One it names fewer blocks
// of than the apply owned reads pending, and one it has not started reads
// unknown, because no record keeps what the earlier attempt did to it.
func TestStatusRowsAfterAReplacingRemoval(t *testing.T) {
	failRemoval := func(t *testing.T, h *rowsHarness, outcomes ...reconciliation.Outcome) {
		t.Helper()
		h.capability.outcomes = nil
		for _, outcome := range outcomes {
			h.capability.outcomes = append(h.capability.outcomes, Result{Outcome: outcome})
		}
		if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("a removal whose block failed reported success")
		}
		h.capability.outcomes = nil
	}
	for _, row := range []struct {
		name          string
		definitions   []reconciliation.BlockDefinition
		first, second map[string]reconciliation.BlockState
		want          map[string]RealizationStatus
	}{
		{
			name: "the first attempt took the cluster install back",
			definitions: []reconciliation.BlockDefinition{
				definition("artifacts"),
				clusterBlock("media-sno", reconciliation.StageMachines, "artifacts"),
				stagedDefinition("other", reconciliation.StageMachines, "media-sno"),
				clusterBlock("install-sno", reconciliation.StageClusters, "other"),
			},
			first: map[string]reconciliation.BlockState{
				"install-sno": reconciliation.BlockDone, "other": reconciliation.BlockFailed,
				"media-sno": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
			},
			second: map[string]reconciliation.BlockState{
				"other": reconciliation.BlockFailed, "media-sno": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
			},
			want: map[string]RealizationStatus{
				"ContainerCluster/sno": RealizationPending, "ArtifactServer/other": RealizationFailed, "ArtifactServer/artifacts": RealizationUnknown,
			},
		},
		{
			name:        "the first attempt failed on a service the second has not started",
			definitions: append(rowsDefinitions(), definition("other")),
			first: map[string]reconciliation.BlockState{
				"install-sno": reconciliation.BlockDone, "other": reconciliation.BlockFailed,
				"media-sno": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
			},
			second: map[string]reconciliation.BlockState{
				"media-sno": reconciliation.BlockFailed, "other": reconciliation.BlockPending, "artifacts": reconciliation.BlockPending,
			},
			want: map[string]RealizationStatus{
				"ContainerCluster/sno": RealizationFailed, "ArtifactServer/other": RealizationUnknown, "ArtifactServer/artifacts": RealizationUnknown,
			},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			h := newRowsHarness(t, row.definitions)
			completeApply(t, h.harness)
			failRemoval(t, h, reconciliation.OutcomeChanged, reconciliation.OutcomeFailed)
			requireReached(t, h.harness, reconciliation.OperationFailed, row.first)
			failRemoval(t, h, reconciliation.OutcomeFailed)
			requireReached(t, h.harness, reconciliation.OperationFailed, row.second)
			want := maps.Clone(row.want)
			maps.Copy(want, map[string]RealizationStatus{
				"ContainerCluster/edge": RealizationUnsupported, "StorageCluster/ceph": RealizationUnsupported, "ArtifactServer/mirror": RealizationUnsupported,
			})
			if got := h.rows(t); !maps.Equal(got, want) {
				t.Fatalf("rows = %v, want %v", got, want)
			}
		})
	}
}

// requireReached holds the current operation to the state it records and each
// named block to the state its record reads, a lost record reading pending.
func requireReached(t *testing.T, h *harness, state reconciliation.OperationState, blocks map[string]reconciliation.BlockState) {
	t.Helper()
	operation, states := durableOperation(t, h)
	if operation.State != state {
		t.Fatalf("the %s records %s, want %s", operation.Verb, operation.State, state)
	}
	for block, want := range blocks {
		reached := states[block]
		if reached == "" {
			reached = reconciliation.BlockPending
		}
		if reached != want {
			t.Fatalf("block %s reads %s, want %s", block, reached, want)
		}
	}
}

// Status counts the Secret bindings the current operation's record holds, one
// per operation however many Secrets the input declares, and 0 once a
// completed removal finalized, which its pristine evidence proves without a
// keyring read. A completed removal whose release failed still holds its
// binding.
func TestStatusCountsBindings(t *testing.T) {
	ctx := context.Background()
	for _, row := range []struct {
		name     string
		arrange  func(*testing.T, *harness)
		bindings int
	}{
		{name: "no operation", arrange: func(*testing.T, *harness) {}},
		{name: "a completed apply", arrange: func(t *testing.T, h *harness) { completeApply(t, h) }, bindings: 1},
		{name: "a failed destroy", arrange: failedChainedRemoval, bindings: 1},
		{name: "a completed destroy", arrange: completeRemoval},
		{
			name: "a completed destroy whose release failed",
			arrange: func(t *testing.T, h *harness) {
				completeApply(t, h)
				h.binder.releaseErr = errors.New("the custody store cannot drop the binding")
				if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
					t.Fatal("a removal whose release failed reported success")
				}
				h.binder.releaseErr = nil
				if operation, _ := durableOperation(t, h); operation.Verb != reconciliation.Destroy || operation.State != reconciliation.OperationDone {
					t.Fatalf("the removal reads %s %s", operation.Verb, operation.State)
				}
			},
			bindings: 1,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			catalog := api.NewCatalog([]api.Object{
				api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
					api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
				)),
				api.NewObject(api.Secret, "artifact-server-tls", api.Value{}, api.MapValue()),
				api.NewObject(api.Secret, "registry-pull", api.Value{}, api.MapValue()),
				api.NewObject(api.Secret, "bmc-password", api.Value{}, api.MapValue()),
			})
			h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
			row.arrange(t, h)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || status.Secrets != (SecretSummary{Declared: 3, Bindings: row.bindings}) {
				t.Fatalf("status counts %+v (%v), want 3 declared and %d bindings", status.Secrets, err, row.bindings)
			}
		})
	}
}
