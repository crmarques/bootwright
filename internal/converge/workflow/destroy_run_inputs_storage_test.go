package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
)

func storageDestroyCacheFixture(t *testing.T) (RunOptions, *destroyRunInputs, map[string]StorageDestroyClusterResult, map[string]string) {
	t.Helper()
	baseDir := t.TempDir()
	state := storageSSHState()
	opts := destroyInputCacheTestOptions(t, baseDir, state)
	opts.Limit = render.GroupStorageHosts
	opts.ArtifactsRoot = filepath.Join(baseDir, "artifacts")
	seeds := StorageDestroyExpectedSeedHosts(state, []string{"ceph"})
	result := storageDestroyResult("ceph", []string{"ceph-0"}, nil).Clusters[0]
	result.FSID = storageDestroyTestFSIDA
	results := map[string]StorageDestroyClusterResult{"ceph": result}
	if err := ownership.SaveResource(opts.OwnershipDir, ownership.ResourceRecord{
		Kind:    string(ownership.KindStorageCluster),
		Name:    "ceph",
		Context: opts.ContextName,
		Cluster: "ceph",
		Host:    seeds["ceph"],
		Attributes: map[string]string{
			"seedHost": seeds["ceph"],
			"fsid":     result.FSID,
		},
	}); err != nil {
		t.Fatalf("save storage ownership: %v", err)
	}
	inputs, err := newDestroyRunInputs(opts.RunsDir, "destroy-storage-cache", opts)
	if err != nil {
		t.Fatalf("newDestroyRunInputs: %v", err)
	}
	t.Cleanup(func() {
		if err := inputs.close(); err != nil {
			t.Errorf("close destroy inputs: %v", err)
		}
	})
	opts.destroyRunInputs = inputs
	return opts, inputs, results, seeds
}

func cachedStorageOwnershipRecord(t *testing.T, inputs *destroyRunInputs) ownership.ResourceRecord {
	t.Helper()
	records, err := inputs.ownershipSnapshot()
	if err != nil {
		t.Fatalf("ownershipSnapshot: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ownership records = %+v, want one storage record", records)
	}
	return records[0]
}

func TestStorageDestroyOwnershipMutationsAdvanceCacheGenerations(t *testing.T) {
	opts, inputs, results, seeds := storageDestroyCacheFixture(t)
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != "" {
		t.Fatalf("initial storage destroy status = %q", status)
	}

	manifest, err := prepareStorageDestroyOwnershipRelease(opts, results, seeds)
	if err != nil {
		t.Fatalf("prepare ownership release: %v", err)
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != storageDestroyStatusProofValidated {
		t.Fatalf("prepared storage destroy status = %q, want %q", status, storageDestroyStatusProofValidated)
	}

	if err := resetStorageDestroyOwnershipProof(opts, results, seeds, manifest); err != nil {
		t.Fatalf("reset ownership proof: %v", err)
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != "" {
		t.Fatalf("reset storage destroy status = %q, want empty", status)
	}

	manifest, err = prepareStorageDestroyOwnershipRelease(opts, results, seeds)
	if err != nil {
		t.Fatalf("prepare ownership release again: %v", err)
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != storageDestroyStatusProofValidated {
		t.Fatalf("reprepared storage destroy status = %q, want %q", status, storageDestroyStatusProofValidated)
	}
	if err := markStorageDestroyOwnershipReleased(opts, results, seeds, manifest); err != nil {
		t.Fatalf("mark ownership released: %v", err)
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != storageDestroyStatusEvidenceReleased {
		t.Fatalf("released storage destroy status = %q, want %q", status, storageDestroyStatusEvidenceReleased)
	}

	if got := opts.DestroyRunInputCounters.Counts().OwnershipLoads; got != 5 {
		t.Fatalf("ownership loads = %d, want one load per mutation generation", got)
	}
}

func TestStorageDestroyOwnershipMutationInvalidatesPartialError(t *testing.T) {
	opts, inputs, _, _ := storageDestroyCacheFixture(t)
	record := cachedStorageOwnershipRecord(t, inputs)
	mutationErr := errors.New("partial storage ownership mutation")
	err := withDestroyOwnershipMutation(inputs, func() error {
		record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusProofValidated
		if err := ownership.SaveResource(opts.OwnershipDir, record); err != nil {
			return err
		}
		if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != storageDestroyStatusProofValidated {
			t.Fatalf("mid-mutation storage destroy status = %q, want %q", status, storageDestroyStatusProofValidated)
		}
		record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusPartial
		if err := ownership.SaveResource(opts.OwnershipDir, record); err != nil {
			return err
		}
		return mutationErr
	})
	if !errors.Is(err, mutationErr) {
		t.Fatalf("mutation error = %v, want %v", err, mutationErr)
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != storageDestroyStatusPartial {
		t.Fatalf("partially written storage destroy status = %q, want %q", status, storageDestroyStatusPartial)
	}
	if got := opts.DestroyRunInputCounters.Counts().OwnershipLoads; got != 3 {
		t.Fatalf("ownership loads = %d, want reload after partial mutation error", got)
	}
}

func TestStorageDestroyReleasePreRunFailureReloadsResetGeneration(t *testing.T) {
	opts, inputs, results, seeds := storageDestroyCacheFixture(t)
	_ = cachedStorageOwnershipRecord(t, inputs)
	manifest, err := prepareStorageDestroyOwnershipRelease(opts, results, seeds)
	if err != nil {
		t.Fatalf("prepare ownership release: %v", err)
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != storageDestroyStatusProofValidated {
		t.Fatalf("prepared storage destroy status = %q, want %q", status, storageDestroyStatusProofValidated)
	}
	opts.State.StorageClusters[0].Spec.Ceph.Topology.Nodes[0].MachineRef.Name = "missing-machine"
	runner := &fakeRunner{}
	taskRoot := filepath.Join(opts.RunsDir, "storage-release-task")
	err = finalizeStorageDestroyTask(
		context.Background(),
		io.Discard,
		io.Discard,
		taskRoot,
		opts,
		results,
		seeds,
		manifest,
		filepath.Join(taskRoot, "release-manifest.json"),
		func(io.Writer, io.Writer) ansible.Runner { return runner },
	)
	if err == nil || !strings.Contains(err.Error(), "missing-machine") {
		t.Fatalf("release error = %v, want pre-run unresolved-machine failure", err)
	}
	if runner.runCalled {
		t.Fatal("storage release runner was called after input rendering failed")
	}
	if status := cachedStorageOwnershipRecord(t, inputs).Attributes[storageDestroyStatusAttr]; status != "" {
		t.Fatalf("reset storage destroy status = %q, want empty", status)
	}
	if got := opts.DestroyRunInputCounters.Counts().OwnershipLoads; got != 3 {
		t.Fatalf("ownership loads = %d, want reload after the release reset", got)
	}
	if _, statErr := os.Stat(filepath.Join(taskRoot, "release-artifacts", StorageDestroyReleaseValidationFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("failed release unexpectedly produced validation evidence: %v", statErr)
	}
}
